# Run the approval console as persistent user services

Use this procedure on the admin machine for demo beats 2 (approve) and 5 (revoke).
The user units bind the console to `127.0.0.1:3011` and forward the relay's
`127.0.0.1:3001` to it. The existing public Caddy console uses that relay port.
Run these commands as the admin user. Node.js, pnpm, Bun, OpenSSL and the
`bramble-relays` SSH alias must already work for that user.

1. Create an independent runtime clone. Keep it outside agent worktrees and
   temporary directories. Use a reviewed commit from `origin/hackathon`.

   ```sh
   git clone --branch hackathon https://github.com/WhyAsh5114/bramble.git ~/bramble-work/approval-runtime
   cd ~/bramble-work/approval-runtime
   pnpm install --frozen-lockfile
   pnpm exec turbo run build --filter=@bramble/approval...
   ```

2. Keep the operator-provided environment in `~/.config/bramble/approval.env`
   with mode `600`. Use the variable names in `deploy/env/admin.env.example`. Preserve
   the existing `APPROVAL_DEMO_TOKEN` and Sepolia settings during migration;
   never print them or generate a replacement token. Set `PATH` to stable
   absolute directories containing Node.js and Bun, plus `/usr/bin:/bin`.
   Set `APPROVAL_GRANTER_LABEL` to the enrolled granter authorized by the
   gateway's `acl-granters` record.

   For demo beats 1, 3 and 5, configure the two read-only activity sources as
   described in [Activity source configuration](ACTIVITY-SOURCES.md) before
   the coordinated restart. The request API can answer successfully while both
   sources remain unconfigured. The console reverse tunnel carries browser
   requests to approval; it does not connect approval to the gateway status API.

   For the software ring demo, set `BRAMBLE_RING_CLI` to the absolute path of
   `deploy/scripts/software-ring.sh` in the runtime clone. Set
   `BRAMBLE_SOFTWARE_RING_KEY_FILE` to `~/.config/bramble/software-ring.key`
   and `BRAMBLE_GRANTER_KEY_RING_PATH` to `~/.config/bramble/granter-key.ring`,
   expanding `~` to the real home path in the environment file. Preserve both
   ring files together with mode `600`. If either file is lost, enroll a new
   granter and update the gateway authorization before using its new ring.

3. Before replacing a running console, finish any pending approve/revoke
   operations. Restarting drops in-memory requests and duration timers.
   Install the user units from the runtime clone:

   ```sh
   mkdir -p ~/.config/systemd/user
   install -m 644 deploy/systemd/user/bramble-approval.service ~/.config/systemd/user/
   install -m 644 deploy/systemd/user/bramble-console-tunnel.service ~/.config/systemd/user/
   export XDG_RUNTIME_DIR=/run/user/$(id -u)
   systemctl --user daemon-reload
   systemctl --user enable bramble-approval bramble-console-tunnel
   systemctl --user restart bramble-approval
   ```

4. Stop the existing ad hoc SSH console tunnel, if present, before starting
   `systemctl --user start bramble-console-tunnel`. Its remote port must be
   free. Do not stop or restart gateway or worker node units for this migration.
   Enable user lingering with `loginctl enable-linger` if the admin user's
   services must start at boot without an interactive login.

5. Check service state and the public console:

   ```sh
   systemctl --user is-active bramble-approval bramble-console-tunnel
   systemctl --user is-enabled bramble-approval bramble-console-tunnel
   curl --fail --silent --show-error https://console.13-233-181-104.sslip.io/api/requests
   journalctl --user -u bramble-approval -n 20
   ```

   Submit a request through the public API with the existing bearer token,
   approve it, and revoke it. Record confirmation times and transaction hashes.
   Run the read-only source check in [Activity source configuration](ACTIVITY-SOURCES.md)
   as well. A `200` page load alone does not verify the signer or feed sources.
   Never include the token, private keys or authenticated RPC URLs in evidence
   or journal excerpts.
