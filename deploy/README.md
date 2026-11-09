# deploy

Everything bramble hosts runs on one EC2 instance, `projects` (ap-south-1, t4g.large, Ubuntu 24.04 arm64), which also
hosts unrelated personal projects.

| Path                              | What                                                                                             |
| --------------------------------- | ------------------------------------------------------------------------------------------------ |
| `projects-host/bootstrap.sh`      | Host hardening: Docker on an XFS data volume with project quotas, gVisor, ufw, fail2ban, updates |
| `projects-host/relay/compose.yml` | The free relay (`mumbai.relay.bramble.eth`) under gVisor, with its byte budget                   |
| `projects-host/landing/`          | get-bramble.whyash5114.com: landing page and install scripts behind Dokploy's Traefik            |

Admin access (Dokploy on :3000, SSH) is over Tailscale only; the security group opens 80 and 443 for the landing page
and 9420/tcp, 3478/udp and 40000-40999/udp for the relay. Daily EBS snapshots are kept for seven days.

To update an app on the host: pull the repository in `/data/projects/bramble` and run `docker compose up -d --build`
in its directory (set `RELAY_PUBLIC_HOST` for the relay).
