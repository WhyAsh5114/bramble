#!/usr/bin/env bash
# Hardens a fresh Ubuntu 24.04 EC2 host for Dokploy and installs Docker with gVisor.
# Run once as root. Idempotent where practical. See README.md next to this file.
#
# DATA_DEVICE: the second EBS volume. It becomes XFS with project quotas at /data,
# so Docker's overlay2 can cap each container's writable layer (storage-opt size).
set -euo pipefail

DATA_DEVICE="${DATA_DEVICE:-/dev/nvme1n1}"
ADMIN_USER="${ADMIN_USER:-ubuntu}"
REBOOT_TIME_UTC="${REBOOT_TIME_UTC:-22:30}" # 04:00 IST

export DEBIAN_FRONTEND=noninteractive
log() { printf '\n== %s\n' "$*"; }

log "packages"
apt-get update -q
apt-get -yq -o Dpkg::Options::=--force-confold full-upgrade
apt-get -yq install unattended-upgrades needrestart fail2ban auditd xfsprogs ufw \
  apparmor-utils lynis zram-tools ca-certificates curl gnupg jq

log "data volume"
if ! blkid "$DATA_DEVICE" >/dev/null 2>&1; then
  mkfs.xfs -L data "$DATA_DEVICE"
fi
mkdir -p /data
grep -q 'LABEL=data' /etc/fstab ||
  echo 'LABEL=data /data xfs defaults,noatime,prjquota,nofail 0 2' >>/etc/fstab
mountpoint -q /data || mount /data
mkdir -p /data/docker /data/projects
ln -sfn /data/projects /srv/projects

log "zram swap"
printf 'ALGO=zstd\nPERCENT=50\nPRIORITY=100\n' >/etc/default/zramswap
systemctl restart zramswap

log "automatic security updates"
cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
cat >/etc/apt/apt.conf.d/52projects-unattended <<EOF
Unattended-Upgrade::Allowed-Origins {
  "\${distro_id}:\${distro_codename}-security";
  "\${distro_id}ESMApps:\${distro_codename}-apps-security";
  "\${distro_id}ESM:\${distro_codename}-infra-security";
};
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "${REBOOT_TIME_UTC}";
EOF
sed -i 's/^#\?\s*\$nrconf{restart}.*/$nrconf{restart} = '"'"'a'"'"';/' /etc/needrestart/needrestart.conf

log "kernel hardening"
cat >/etc/sysctl.d/90-projects-hardening.conf <<'EOF'
kernel.kptr_restrict = 2
kernel.dmesg_restrict = 1
kernel.unprivileged_bpf_disabled = 1
net.core.bpf_jit_harden = 2
kernel.yama.ptrace_scope = 1
kernel.kexec_load_disabled = 1
kernel.sysrq = 0
fs.protected_hardlinks = 1
fs.protected_symlinks = 1
fs.protected_fifos = 2
fs.protected_regular = 2
fs.suid_dumpable = 0
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.all.accept_source_route = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv4.conf.all.log_martians = 1
# Loose reverse-path filtering: Tailscale and Docker route asymmetrically.
net.ipv4.conf.all.rp_filter = 2
net.ipv4.conf.default.rp_filter = 2
# The relay forwards UDP for many peers.
net.core.rmem_max = 7500000
net.core.wmem_max = 7500000
EOF
sysctl --system >/dev/null

log "ssh"
cat >/etc/ssh/sshd_config.d/90-projects.conf <<EOF
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers ${ADMIN_USER}
X11Forwarding no
AllowAgentForwarding no
MaxAuthTries 3
LoginGraceTime 20
EOF
systemctl try-reload-or-restart ssh.service || true

log "host firewall (INPUT). Docker-published ports are handled in DOCKER-USER below."
ufw --force reset >/dev/null
ufw default deny incoming
ufw default allow outgoing
ufw default deny routed
ufw allow in on tailscale0 comment 'admin over Tailscale'
ufw allow 41641/udp comment 'tailscale direct'
ufw allow 80/tcp comment 'traefik http'
ufw allow 443/tcp comment 'traefik https'
ufw allow 443/udp comment 'traefik http3'
ufw allow 9420/tcp comment 'bramble rendezvous'
ufw allow 3478/udp comment 'bramble stun'
ufw allow 40000:40999/udp comment 'bramble data relay'
ufw --force enable

log "audit rules"
cat >/etc/audit/rules.d/90-projects.rules <<'EOF'
-w /var/run/docker.sock -p rwa -k docker-socket
-w /etc/docker -p wa -k docker-config
-w /usr/bin/dockerd -p x -k docker-daemon
-w /etc/passwd -p wa -k identity
-w /etc/shadow -p wa -k identity
-w /etc/sudoers -p wa -k sudoers
-w /etc/sudoers.d -p wa -k sudoers
-w /etc/ssh/sshd_config -p wa -k sshd
-w /etc/ssh/sshd_config.d -p wa -k sshd
EOF
augenrules --load >/dev/null || true

log "docker"
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" >/etc/apt/sources.list.d/docker.list

log "gvisor"
curl -fsSL https://gvisor.dev/archive.key | gpg --batch --yes --dearmor -o /usr/share/keyrings/gvisor-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" >/etc/apt/sources.list.d/gvisor.list

apt-get update -q
apt-get -yq install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin runsc

mkdir -p /etc/docker
cat >/etc/docker/daemon.json <<'EOF'
{
  "data-root": "/data/docker",
  "storage-driver": "overlay2",
  "log-driver": "json-file",
  "log-opts": { "max-size": "10m", "max-file": "3" },
  "no-new-privileges": true,
  "userland-proxy": false,
  "default-ulimits": { "nofile": { "Name": "nofile", "Soft": 65536, "Hard": 65536 } },
  "runtimes": {
    "runsc": { "path": "/usr/bin/runsc" }
  }
}
EOF
systemctl enable --now docker
systemctl restart docker

log "DOCKER-USER rules (applied after every docker start)"
install -m 0755 /dev/stdin /usr/local/sbin/projects-docker-user <<'EOF'
#!/usr/bin/env bash
# Forwarded (published) container traffic skips ufw's INPUT chain. These rules run first.
set -euo pipefail
ext=$(ip -o -4 route show to default | awk '{print $5; exit}')
iptables -N DOCKER-USER 2>/dev/null || true
iptables -F DOCKER-USER
# Containers never reach the instance metadata service.
iptables -A DOCKER-USER -d 169.254.169.254/32 -j DROP
# Admin ports (Dokploy UI, Traefik dashboard) only from Tailscale.
iptables -A DOCKER-USER -i "$ext" -p tcp -m multiport --dports 3000,8080 -j DROP
iptables -A DOCKER-USER -j RETURN
# Host-network containers run as non-root users; only root on the host may reach metadata.
iptables -C OUTPUT -d 169.254.169.254/32 -m owner ! --uid-owner 0 -j DROP 2>/dev/null ||
  iptables -I OUTPUT -d 169.254.169.254/32 -m owner ! --uid-owner 0 -j DROP
EOF
cat >/etc/systemd/system/projects-docker-user.service <<'EOF'
[Unit]
Description=Firewall rules for Docker-published ports
After=docker.service
PartOf=docker.service
Requires=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/projects-docker-user
RemainAfterExit=yes

[Install]
WantedBy=docker.service
EOF
systemctl daemon-reload
systemctl enable --now projects-docker-user.service

log "fail2ban"
cat >/etc/fail2ban/jail.d/90-projects.local <<'EOF'
[DEFAULT]
bantime = 1h
findtime = 10m
maxretry = 5
backend = systemd

[sshd]
enabled = true

[recidive]
enabled = true
backend = auto
logpath = /var/log/fail2ban.log
bantime = 1w
findtime = 1d
maxretry = 3
EOF
systemctl enable --now fail2ban
systemctl restart fail2ban

log "tailscale (not logged in yet)"
command -v tailscale >/dev/null || curl -fsSL https://tailscale.com/install.sh | sh

log "done"
docker info --format 'docker {{.ServerVersion}} root={{.DockerRootDir}} runtimes={{range $k, $v := .Runtimes}}{{$k}} {{end}}'
