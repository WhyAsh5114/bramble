#!/usr/bin/env python3
"""Classify the NAT mapping seen by one UDP socket (RFC 5389 Binding requests).

Sends a Binding request to each given STUN server from ONE bound socket and
compares the mapped endpoints (IP and port). The same endpoint for every server
means endpoint-independent mapping (hole punching can work). A different IP or
port for any server means endpoint-dependent mapping (symmetric; expect the
relay fallback).

Usage: stun-probe.py [--bind-port N] host:port host:port [...]
       stun-probe.py --selftest
Run inside the NAT box: sudo ip netns exec bramble-node python3 stun-probe.py ...
This probe uses its own socket, not the WireGuard socket. It diagnoses the NAT;
the W8 discovery gate must still query from the WireGuard socket itself.
"""
import os
import socket
import struct
import sys
import threading

MAGIC = 0x2112A442


def request():
    txn = os.urandom(12)
    return txn, struct.pack("!HHI", 0x0001, 0, MAGIC) + txn


def parse(data, txn):
    if len(data) < 20:
        return None
    mtype, length, cookie = struct.unpack("!HHI", data[:8])
    if mtype != 0x0101 or cookie != MAGIC or data[8:20] != txn:
        return None
    pos, end = 20, min(20 + length, len(data))
    mapped = None
    while pos + 4 <= end:
        atype, alen = struct.unpack("!HH", data[pos:pos + 4])
        val = data[pos + 4:pos + 4 + alen]
        if atype == 0x0020 and alen >= 8 and val[1] == 1:  # XOR-MAPPED-ADDRESS, IPv4
            port = struct.unpack("!H", val[2:4])[0] ^ (MAGIC >> 16)
            ip = socket.inet_ntoa(struct.pack("!I", struct.unpack("!I", val[4:8])[0] ^ MAGIC))
            return ip, port
        if atype == 0x0001 and alen >= 8 and val[1] == 1 and mapped is None:  # MAPPED-ADDRESS
            mapped = (socket.inet_ntoa(val[4:8]), struct.unpack("!H", val[2:4])[0])
        pos += 4 + alen + (-alen % 4)
    return mapped


def probe(servers, bind_port=0, timeout=3.0):
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(("0.0.0.0", bind_port))
    sock.settimeout(timeout)
    local_port = sock.getsockname()[1]
    results = []
    for server in servers:
        host, _, port = server.rpartition(":")
        addr = (socket.gethostbyname(host), int(port))
        mapped = None
        for _ in range(3):
            txn, msg = request()
            sock.sendto(msg, addr)
            try:
                while True:
                    data, _src = sock.recvfrom(2048)
                    mapped = parse(data, txn)
                    if mapped:
                        break
            except socket.timeout:
                continue
            if mapped:
                break
        results.append((server, mapped))
    return local_port, results


def verdict(results):
    mapped = [m for _, m in results]
    if any(m is None for m in mapped):
        return "unknown: at least one server did not answer", 2
    if len({(m[0], m[1]) for m in mapped}) == 1:
        return "endpoint-independent mapping (cone-like): hole punching can work", 0
    return "endpoint-dependent mapping (symmetric-like): expect the relay fallback", 1


def fake_server(sock, mapped_port_for, mapped_ip="203.0.113.7"):
    while True:
        try:
            data, src = sock.recvfrom(2048)
        except OSError:
            return
        port = mapped_port_for(sock.getsockname()[1])
        attr = struct.pack("!BBH", 0, 1, port ^ (MAGIC >> 16)) + struct.pack("!I", struct.unpack("!I", socket.inet_aton(mapped_ip))[0] ^ MAGIC)
        body = struct.pack("!HH", 0x0020, 8) + attr
        sock.sendto(struct.pack("!HHI", 0x0101, len(body), MAGIC) + data[8:20] + body, src)


def selftest():
    def start(fn, ip):
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.bind(("127.0.0.1", 0))
        threading.Thread(target=fake_server, args=(s, fn, ip), daemon=True).start()
        return s, f"127.0.0.1:{s.getsockname()[1]}"
    ok = True
    same, other = "203.0.113.7", "203.0.113.8"
    cases = (
        ("same mapping", lambda p: 40000, same, same, 0),
        ("different port", lambda p: 40000 + p % 1000, same, same, 1),
        ("different address, same port", lambda p: 40000, same, other, 1),
    )
    for name, fn, ip1, ip2, want in cases:
        (s1, a1), (s2, a2) = start(fn, ip1), start(fn, ip2)
        _, results = probe([a1, a2], timeout=1.0)
        text, code = verdict(results)
        print(("PASS " if code == want else "FAIL ") + f"{name}: {text}")
        ok = ok and code == want
        s1.close()
        s2.close()
    return 0 if ok else 1


def main(argv):
    if argv == ["--selftest"]:
        return selftest()
    bind_port = 0
    if argv[:1] == ["--bind-port"]:
        bind_port, argv = int(argv[1]), argv[2:]
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    local_port, results = probe(argv, bind_port)
    print(f"local socket port {local_port}")
    for server, mapped in results:
        print(f"  {server} -> {mapped[0]}:{mapped[1]}" if mapped else f"  {server} -> no answer")
    text, code = verdict(results)
    print(text)
    return code


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
