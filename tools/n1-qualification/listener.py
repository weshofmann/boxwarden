#!/usr/bin/env python3
"""Owned fixed-response TCP/UDP fixture. No files, commands, credentials or proxy.

Foreground only; exits after at most 120 seconds. JSON records observations,
not a containment verdict. Bind only an explicitly identified owned host address.
"""
import argparse
import ipaddress
import json
import select
import socket
import time

RESPONSE = b"boxwarden-n1-owned-fixture\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bind", required=True)
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--duration", type=int, default=60)
    args = parser.parse_args()
    try:
        address = ipaddress.IPv4Address(args.bind)
    except ipaddress.AddressValueError:
        parser.error("bind must be an explicit owned IPv4 address")
    if address.is_unspecified or address.is_multicast or int(address) == 0xffffffff:
        parser.error("wildcard, multicast and broadcast binds are prohibited")
    if args.port != 0 and not 1024 <= args.port <= 65535:
        parser.error("port must be zero (ephemeral) or 1024..65535")
    if not 1 <= args.duration <= 120:
        parser.error("duration must be 1..120 seconds")
    counts = {"tcp_responses": 0, "udp_responses": 0, "closed": False}
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as tcp, socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as udp:
        # No reuse: a pre-existing service is a conflict, never a fixture.
        tcp.bind((str(address), args.port))
        port = tcp.getsockname()[1]
        udp.bind((str(address), port))
        tcp.listen(4)
        tcp.setblocking(False)
        udp.setblocking(False)
        deadline = time.monotonic() + args.duration
        print(json.dumps({"bind": str(address), "port": port}), flush=True)
        while time.monotonic() < deadline:
            readable, _, _ = select.select([tcp, udp], [], [], max(0, deadline - time.monotonic()))
            for server in readable:
                if time.monotonic() >= deadline:
                    break
                try:
                    if server is tcp:
                        conn, _ = tcp.accept()
                        with conn:
                            conn.settimeout(min(.1, max(.001, deadline - time.monotonic())))
                            conn.sendall(RESPONSE)
                            counts["tcp_responses"] += 1
                    else:
                        _, peer = udp.recvfrom(64)
                        udp.sendto(RESPONSE, peer)
                        counts["udp_responses"] += 1
                except (BlockingIOError, ConnectionError, socket.timeout):
                    continue
    counts["closed"] = True
    print(json.dumps(counts), flush=True)


if __name__ == "__main__":
    main()
