# Install host

Reference for what the relays machine serves so a new device can install Bramble, and for the install scripts. The URL layout is the ETH-177 shared contract v1. The node config schema is defined in `docs/reference/node-config.md` (ETH-180); this page only lists how the generator fills it.

Code: `scripts/release/install.sh`, `scripts/release/uninstall.sh`, `scripts/release/install.test.sh`, `deploy/scripts/gen-node-config.mjs`, `deploy/scripts/publish-release.sh`, `deploy/scripts/smoke-public.sh`, `deploy/caddy/Caddyfile`.

## URLs

The host is the public console name (for example `console.13-233-181-104.sslip.io`). Caddy serves these paths as static files from `/var/lib/bramble-dist`. Every other path keeps going to the approval console.

| Path                               | Content                                                    | Content-Type                        |
| ---------------------------------- | ---------------------------------------------------------- | ----------------------------------- |
| `/install.sh`                      | Installer, with the install host baked in                  | `text/x-shellscript; charset=utf-8` |
| `/uninstall.sh`                    | Uninstaller                                                | `text/x-shellscript; charset=utf-8` |
| `/dl/bramble_<os>_<arch>.tar.gz`   | Release bundle (`linux`, `darwin`; `amd64`, `arm64`)       | `application/gzip`                  |
| `/dl/SHA256SUMS`                   | `sha256sum` lines for the bundles                          | `application/octet-stream`          |
| `/.well-known/bramble.json`        | Public node config (schema: `docs/reference/node-config.md`) | `application/json`                |

Every response carries `Cache-Control: no-cache` and `X-Content-Type-Options: nosniff`. A directory under `/dl/` returns 404 (no listing). `/dl/` holds only the current release. The trust anchor is the HTTPS certificate: `SHA256SUMS` guards against a corrupt or truncated download, not against a compromised host.

## `install.sh`

```sh
curl -fsSL https://<host>/install.sh | sh
curl -fsSL https://<host>/install.sh | sh -s -- --no-open
```

The script is POSIX `sh`. It needs `curl`, `tar` and `sha256sum` or `shasum`. It runs as the user and does these steps in order:

1. Detects the OS (`linux`, `darwin`) and CPU (`amd64`, `arm64`); exits on anything else.
2. Prints what it will do.
3. Fetches `/.well-known/bramble.json` and exits if it has no `tailnet`. Prints the tailnet name, version and join API.
4. Downloads `/dl/SHA256SUMS` and `/dl/bramble_<os>_<arch>.tar.gz`, and exits with nothing installed if the SHA-256 differs or `SHA256SUMS` has no entry.
5. Refuses an archive with nested or absolute paths, or without a `bramble` binary.
6. Stops a previous `bramble` (the pid in `bramble.pid`, only when its process name contains `bramble`).
7. Copies each file to `~/.bramble/bin/` by renaming it into place, and writes `~/.bramble/config.url`.
8. Starts `bramble` in the background with output in `~/.bramble/logs/bramble.out`, waits up to 20 s for `~/.bramble/ui.url`, prints the address, and opens it with `open` (macOS) or `xdg-open` (Linux with `DISPLAY` or `WAYLAND_DISPLAY`).

It uses no root by default. It does not edit shell profiles; it prints the `PATH` line instead.

| Flag         | Variable                | Effect                                                                                                   |
| ------------ | ----------------------- | -------------------------------------------------------------------------------------------------------- |
| `--no-start` | `BRAMBLE_NO_START=1`    | Install only                                                                                             |
| `--no-open`  | `BRAMBLE_NO_OPEN=1`     | Start, print the address, do not open a browser                                                          |
| `--tun`      | `BRAMBLE_INSTALL_TUN=1` | Unsupported: print a warning and start with the user-space network stack |
|              | `BRAMBLE_HOME`          | Data directory. Default `~/.bramble`                                                                     |
|              | `BRAMBLE_INSTALL_BASE`  | Replace `https://<host>`. Plain `http` requires the exact authority `127.0.0.1` or `localhost`, optionally followed by a numeric port from 1 to 65535; no userinfo, path, query or fragment (used by tests)  |

TUN is unsupported until Keystone option A, the ambient-capability fix, lands after the demo. Neither `--tun` nor `BRAMBLE_INSTALL_TUN=1` grants capabilities or passes `-tun` to `bramble up`. The installer uses no `sudo` and starts `bramble` with the user-space network stack (ETH-180, decision D10).

Files the launcher and installer share, all under `~/.bramble/` (names from `node-daemon/internal/daemon/store.go`): `config.url` (written by the installer), `ui.url` and `bramble.pid` (written by `bramble` while it runs), `logs/`.

## `uninstall.sh`

```sh
curl -fsSL https://<host>/uninstall.sh | sh -s -- --yes
```

It asks for confirmation on the terminal; without a terminal it exits unless you pass `--yes`. It then:

1. Treats `bramble.pid` as a hint. Before each signal, checks that the process executable is inside the resolved `bin/` directory. On Linux, it also accepts a shell whose first script argument is a regular, non-symlinked file inside that directory. It checks all process IDs to find helpers and ignores nonpositive IDs.
2. On Linux, removes the interface `bramble0` with `sudo ip link delete` if it exists. This is the only `sudo` call.
3. Deletes `~/.bramble`, including the device key. With `--keep-data` it deletes only `bin/`, `ui.url` and `bramble.pid`.

It resolves `BRAMBLE_HOME` and `$HOME` through physical paths before removal. It refuses `/`, `$HOME`, ancestors of `$HOME`, and equivalent paths with repeated slashes, dot segments or symlinks. It refuses a symlinked `bin/` directory or installation marker. The directory must contain the `bramble-install-v1` marker at `.bramble-install`, or the legacy layout of `config.url` and an executable, non-symlinked `bin/bramble`. `--keep-data` preserves the marker so a later full uninstall can remove the remaining data. Bramble installs no system service, so there is none to remove. The command `bramble uninstall` belongs to the launcher (ETH-180).

## Test

`scripts/release/install.test.sh` serves a stub bundle on loopback and checks `install.sh` and `uninstall.sh` under `sh`, `dash` and `busybox sh`: install, reinstall over a running process, uninstall, checksum mismatch, missing checksum entry, nested archive paths, a host without a tailnet, plain `http` to a remote host, the confirmation rules, stale PID preservation, installation evidence, and `--keep-data`. The native executable ownership (including a daemon running from a replaced binary, Linux only), protected path aliases, nonpositive PID, HTTP authority, and dot-segment probes run under `sh` only. It needs `python3`, `curl`, `tar` and `sha256sum`, and no network or root.

## Generator: environment to node config

`deploy/scripts/gen-node-config.mjs --version V --env FILE [--env FILE ...]` reads a fixed list of names from the env files and ignores every other line. It never reads `SEPOLIA_RPC_URL`, because on the admin machine that variable is the credentialed endpoint.

| Field           | Source variable                      | Default or rule                                                           |
| --------------- | ------------------------------------ | ------------------------------------------------------------------------- |
| `version`       | `--version`                          | Required                                                                  |
| `tailnet`       | `BRAMBLE_TAILNET_NAME`               | Required, ends in `.eth`, lower-cased                                     |
| `chainId`       | `BRAMBLE_CHAIN_ID`                   | Required integer                                                          |
| `rpcUrl`        | `BRAMBLE_PUBLIC_RPC_URL`             | `https://<BRAMBLE_RPC_HOST>/`                                             |
| `registry`      | `BRAMBLE_TAILNET_REGISTRY`           | Required, 20-byte hex                                                     |
| `relayRegistry` | `BRAMBLE_RELAY_REGISTRY`             | Optional                                                                  |
| `joinApi`       | `BRAMBLE_APPROVAL_URL`               | `https://<BRAMBLE_CONSOLE_HOST>`                                          |
| `rendezvous`    | `BRAMBLE_RENDEZVOUS`                 | Optional, one `host:port`                                                 |
| `stun`          | `BRAMBLE_STUN_SERVERS`               | Optional, comma-separated `host:port`                                     |
| `dataRelays`    | `BRAMBLE_DATA_RELAYS`                | Optional, comma-separated `label=url`                                     |
| `relayPeers`    | `BRAMBLE_RELAY_PEERS`                | Optional, comma-separated labels                                          |
| `services`      | `BRAMBLE_PUBLIC_SERVICES`            | Optional, comma-separated `peer:service[:/path]`                          |

The generator exits non-zero when:

- a required variable is missing or malformed;
- a URL has user info, a query string, a fragment, or a path segment of 24 or more URL-safe characters (the shape of an API key);
- the output holds a 64-digit hex string, `PRIVATE`, `Bearer` or `api_key`.

`publish-release.sh` runs the same style of check over `install.sh`, `uninstall.sh` and `.well-known/` before it uploads anything.

## Install-script placeholders

`publish-release.sh` replaces these literal strings in `install.sh` and `uninstall.sh` while it stages the upload:

| Placeholder                | Replaced with                                           |
| -------------------------- | ------------------------------------------------------- |
| `@@BRAMBLE_INSTALL_HOST@@` | The console DNS name, for example `console.example.com` |
| `@@BRAMBLE_VERSION@@`      | The `--version` value                                   |
