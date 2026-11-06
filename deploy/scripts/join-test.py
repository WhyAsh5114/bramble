#!/usr/bin/env python3
"""Exercise the installed device CLI/API in a dedicated HOME (ETH-250)."""

import argparse
import functools
import http.server
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request
import uuid


TAILNET = "bramble-dev-c91e55e9.eth"
REGISTRY = "0xEdA4b78CcA4472522bf2b521A6D2987306579058"
REPO = Path(__file__).resolve().parents[2]
MARKER = "eth-250-clean-home-v1"


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def base_url(value):
    u = urllib.parse.urlsplit(value)
    require(
        u.hostname and not u.username and not u.password
        and not u.query and not u.fragment and u.path in ("", "/"),
        "Use an install origin without credentials, path, query or fragment.",
    )
    require(
        u.scheme == "https" or (u.scheme == "http" and u.hostname == "127.0.0.1"),
        "Use HTTPS, or HTTP on 127.0.0.1 for a local dry run.",
    )
    return value.rstrip("/")


def public_config(cfg):
    # Preserve only documented public fields, never an unexpected server field.
    allowed = ("version", "tailnet", "chainId", "registry", "relayRegistry", "joinApi",
               "rpcUrl", "rendezvous", "stun", "dataRelays", "relayPeers", "services",
               "registryDeployBlock", "relayDeployBlock", "sponsoredRelay", "relayFirst",
               "rendezvousPaymentUrl", "peers")
    for value in [cfg.get("rpcUrl"), cfg.get("joinApi"), cfg.get("rendezvousPaymentUrl"),
                  *cfg.get("dataRelays", {}).values()]:
        if value:
            u = urllib.parse.urlsplit(value)
            require(not u.username and not u.password and not u.query and not u.fragment,
                    "Config URL must not contain credentials, query or fragment.")
            require(all(len(part) < 24 for part in u.path.split("/")), "Config URL has an opaque path segment.")
    return {key: cfg[key] for key in allowed if key in cfg}


def request(url, body=None, timeout=20):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    # Do not inherit proxy variables or send local requests through a proxy.
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=timeout) as r:
        return json.load(r)


def clean_env(root):
    return {
        "PATH": os.defpath,
        "HOME": str(root / "home"),
        "BRAMBLE_HOME": str(root / "home/.bramble"),
        "TMPDIR": str(root / "tmp"),
        "LANG": "C.UTF-8",
    }


def check_interface():
    # The current uninstaller removes this global interface even for a userspace install.
    require(not Path("/sys/class/net/bramble0").exists(), "bramble0 already exists; stop here to protect it.")


def load(root):
    require(not root.is_symlink(), "Run directory must not be a symlink.")
    require((root / "marker").read_text().strip() == MARKER, "Not an ETH-250 run directory.")
    require(not (root / "home").is_symlink(), "HOME must not be a symlink.")
    require(not (root / "home/.bramble").is_symlink(), "BRAMBLE_HOME must not be a symlink.")
    return json.loads((root / "run.json").read_text())


def cli(root, *args):
    subprocess.run([str(root / "home/.bramble/bin/bramble"), *args], env=clean_env(root), check=True)


def state(root):
    load(root)
    url = (root / "home/.bramble/ui.url").read_text().strip().rstrip("/")
    u = urllib.parse.urlsplit(url)
    require(u.scheme == "http" and u.hostname == "127.0.0.1" and u.port, "Unexpected local UI URL.")
    return url, request(url + "/api/state")


def report(root, name, value):
    (root / name).write_text(json.dumps(value, indent=2) + "\n")
    print(json.dumps(value, indent=2), flush=True)


def start(root, base, manual_start):
    check_interface()
    require(not root.exists(), "Choose a new run directory; an existing HOME is never reused.")
    cfg = public_config(request(base + "/.well-known/bramble.json"))
    require(cfg.get("tailnet") == TAILNET, "Install host does not serve the expected tailnet.")
    require(cfg.get("version") and "stub" not in cfg["version"].lower(), "Release version is missing or a stub.")
    require(cfg.get("registry", "").lower() == REGISTRY.lower(), "Unexpected tailnet registry.")
    require(cfg.get("chainId") == 11155111, "Expected Sepolia chainId 11155111.")
    require(cfg.get("joinApi", "").rstrip("/") == base, "Join API must be on the chosen install origin.")
    root.mkdir(mode=0o700, parents=True)
    (root / "home").mkdir(mode=0o700)
    (root / "tmp").mkdir(mode=0o700)
    (root / "marker").write_text(MARKER + "\n")
    report(root, "run.json", {"base": base, "manualStart": manual_start,
                             "deviceLabel": "zephyrus-" + uuid.uuid4().hex[:12]})
    report(root, "config.json", cfg)
    env = clean_env(root)
    env["BRAMBLE_INSTALL_BASE"] = base
    # Cache only the public uninstaller, so cleanup works after the local server stops.
    subprocess.run(["curl", "-fsSL", "--max-time", "30", base + "/uninstall.sh", "-o", str(root / "uninstall.sh")],
                   env=env, check=True)
    print("Installing through curl | sh in " + str(root / "home"), flush=True)
    with (root / "install.log").open("w") as log:
        fetch = subprocess.Popen(["curl", "-fsSL", "--max-time", "60", base + "/install.sh"], env=env, stdout=subprocess.PIPE)
        try:
            result = subprocess.run(["sh", "-s", "--", "--no-start" if manual_start else "--no-open"],
                                    env=env, stdin=fetch.stdout, stdout=log, stderr=subprocess.STDOUT)
        finally:
            fetch.stdout.close()
            fetch.wait()
    print((root / "install.log").read_text(), flush=True)
    require(fetch.returncode == 0 and result.returncode == 0, "Installer failed; see install.log. Run uninstall on this directory.")
    if manual_start:
        cli(root, "up", "-d", "-open=false")
    url, st = state(root)
    require(st["phase"] == "setup", "Expected the first-run setup screen in a clean HOME.")
    print("Open " + url, flush=True)
    report(root, "first-run.json", st)


def join(root):
    url, st = state(root)
    require(st["phase"] in ("setup", "ready", "error") and not st.get("joinId") and not st.get("tailnet"),
            "A join has already started; use status instead of submitting twice.")
    if not st.get("name"):
        request(url + "/api/setup", {"name": load(root)["deviceLabel"]})
    st = request(url + "/api/join", {"tailnet": TAILNET})
    require(st["phase"] == "pending" and st.get("joinId"), "Expected a pending join request.")
    report(root, "pending.json", st)
    print("Ask Infra-Claude to coordinate approval of this label, joinId and fingerprint. Do not approve unrelated requests.", flush=True)


def status(root):
    cli(root, "status")
    _, st = state(root)
    report(root, "state.json", st)
    return st


def test(root, expect_pending=False):
    url, st = state(root)
    if not expect_pending:
        require(st["phase"] == "connected", "Wait for connected before testing the mesh.")
        require(st.get("meshIp") and st.get("peers"), "Connected must include a mesh IP and at least one peer.")
    result = request(url + "/api/test", {}, timeout=110)
    report(root, "test.json", result)
    if expect_pending:
        require(st["phase"] == "pending" and result.get("ok") is False,
                "The local dry run must remain pending and cannot claim connectivity.")
    else:
        require(result.get("ok") is True and 200 <= result.get("status", 0) < 300,
                "Connectivity failed: HTTP 200 from /api/test alone is not a pass.")


def uninstall(root):
    check_interface()
    load(root)
    pidfile = root / "home/.bramble/bramble.pid"
    pid = pidfile.read_text().strip() if pidfile.exists() else ""
    subprocess.run(["sh", str(root / "uninstall.sh"), "--yes"], env=clean_env(root), check=True)
    require(not (root / "home/.bramble").exists(), "Uninstall left the device directory behind.")
    if pid.isdigit() and Path("/proc").exists():
        try:
            executable = os.readlink("/proc/" + pid + "/exe")
        except FileNotFoundError:
            executable = ""
        require(not executable.startswith(str(root / "home/.bramble/bin") + "/"), "The device process is still running.")
    report(root, "uninstall.json", {"removed": True, "note": "Local removal does not revoke ENS enrollment."})


class LocalHost(http.server.SimpleHTTPRequestHandler):
    """Loopback release host and deliberately pending-only join fixture."""

    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path == "/.well-known/bramble.json":
            self.reply({"version": "eth-250-local", "tailnet": TAILNET, "chainId": 11155111,
                        "registry": REGISTRY, "joinApi": self.server.base,
                        "rpcUrl": self.server.base + "/rpc"})
        elif self.path == "/api/joins/local-pending":
            self.reply({"id": "local-pending", "status": "pending"})
        else:
            super().do_GET()

    def do_POST(self):
        if self.path == "/api/joins":
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            require(body["tailnet"] == TAILNET, "Unexpected local join tailnet.")
            self.reply({"id": "local-pending", "status": "pending"}, 201)
        else:
            self.send_error(403, "Local fixture never approves, writes ENS, or starts a remote connection.")

    def reply(self, data, code=200):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def local(root, release_dir, manual_start):
    require(not root.exists(), "Choose a new run directory.")
    # Keep the served files outside HOME, so uninstall evidence remains available.
    site = root.parent / (root.name + "-site")
    require(not site.exists(), "Local site directory already exists; choose a new run directory.")
    (site / "dl").mkdir(parents=True, mode=0o700)
    for p in [release_dir / "SHA256SUMS", *release_dir.glob("bramble_*.tar.gz")]:
        shutil.copyfile(p, site / "dl" / p.name)
    for name in ("install.sh", "uninstall.sh"):
        shutil.copyfile(REPO / "scripts/release" / name, site / name)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(LocalHost, directory=str(site)))
    server.base = "http://127.0.0.1:" + str(server.server_port)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        start(root, server.base, manual_start)
        join(root)
        time.sleep(3)  # Observe one real daemon join-status poll (default interval: 2 s).
        status(root)
        test(root, expect_pending=True)
        print("Verified: local install, CLI/API, pending join, pre-approval denial. Open: real approval, mesh, public flow.", flush=True)
    finally:
        try:
            if (root / "uninstall.sh").exists():
                uninstall(root)
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("action", choices=["start", "request", "status", "test", "uninstall", "local"])
    p.add_argument("--run-dir", required=True, type=Path, help="New absolute directory for the isolated HOME and public evidence")
    p.add_argument("--base", type=base_url, help="Install origin, used by start")
    p.add_argument("--release-dir", type=Path, help="Archive directory with SHA256SUMS, used by local")
    p.add_argument("--manual-start", action="store_true", help="Work around installer startup: --no-start then bramble up -d -open=false")
    args = p.parse_args()
    require(args.run_dir.is_absolute(), "--run-dir must be absolute.")
    require(args.run_dir.resolve() == args.run_dir, "Run directory must use a physical path without symlink parents or dot segments.")
    root = args.run_dir
    if args.action == "start":
        require(args.base, "start requires --base")
        start(root, args.base, args.manual_start)
    elif args.action == "local":
        require(args.release_dir and args.release_dir.is_dir(), "local requires --release-dir")
        local(root, args.release_dir, args.manual_start)
    else:
        load(root)
        {"request": join, "status": status, "test": test, "uninstall": uninstall}[args.action](root)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, ValueError, subprocess.SubprocessError) as error:
        print("join-test: " + str(error), file=sys.stderr)
        sys.exit(1)
