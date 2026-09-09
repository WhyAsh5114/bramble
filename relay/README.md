# relay

The dumb rendezvous relay (`docs/11_DAY0_GATES.md` Gate 0.2, `docs/adr/0003-rendezvous-relay-split.md`). Forwards opaque candidate-exchange blobs between two node agents; never decides who may talk to whom.

```bash
go build -o bin/relay .
./bin/relay -addr :9420
```

## Metering (`-meter`, `docs/adr/0007-rendezvous-relay-metering.md`)

Off by default — an unmetered relay behaves exactly as before Section B, no token needed.

```
-meter                 require a paid rendezvous token per hello (default false)
-payee <account id>    Hedera account to receive rendezvous fees (required with -meter)
-sidecar-dir <path>    relay-sidecar/ project directory (default ../relay-sidecar)
-sidecar-port <port>   local port relay-sidecar listens on (default 7891)
```

With `-meter`, the relay generates a fresh HMAC secret at startup, spawns `relay-sidecar` (`../relay-sidecar` by default) as a child process, hands it the secret over an env var, and requires every `hello` to carry a valid, unexpired, not-already-used rendezvous token — minted by `relay-sidecar` only after a real x402/Hedera payment settles. Token verification itself is entirely local (no callback to the sidecar per connection) — see `relay/token.go` and `relay-sidecar/README.md` for the live proof.

```bash
./bin/relay -addr :9420 -meter -payee 0.0.XXXXXXX -sidecar-dir ../relay-sidecar
```
