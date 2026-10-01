#!/usr/bin/env python3
"""Finite, clone-only N1 guest metadata observer.

The only live capabilities are a filtered AF_PACKET metadata tap and two fixed,
read-only `ip -j` queries for the bound peer. No packet bytes are retained.
"""
from __future__ import annotations

import ctypes
import ipaddress
import math
import json
import os
import re
import selectors
import socket
import struct
import subprocess
import sys
import time
from typing import Callable

MAX_WATCH_BYTES = 8192
MAX_COMMAND_OUTPUT_BYTES = 4096
IP_COMMAND_TIMEOUT = 2.0
MAX_EVENTS = 4096
ARP_SNAPLEN = 42
IP_SNAPLEN = 154  # buffer ceiling; cBPF returns shorter protocol-specific lengths
ETH_P_ALL = 0x0003
AF_PACKET = getattr(socket, "AF_PACKET", 17)
ETH_P_IP = 0x0800
ETH_P_ARP = 0x0806
IPPROTO_ICMP = 1
IPPROTO_TCP = 6
SOL_PACKET = 263
PACKET_STATISTICS = 6
PACKET_HOST = 0
PACKET_BROADCAST = 1
PACKET_OUTGOING = 4
MSG_TRUNC = getattr(socket, "MSG_TRUNC", 0x20)
SO_ATTACH_FILTER = getattr(socket, "SO_ATTACH_FILTER", 26)

class WatchError(ValueError):
    """The stdin watch is malformed or outside the fixed observer schema."""

class ObserverError(RuntimeError):
    """A local observer setup operation failed; raw OS errors are not exposed."""

# Codes are intentionally finite and stable for the future diagnostic driver.
EVENT_CODES = (
    "arp_local_request", "arp_local_reply",
    "arp_peer_request_expected_unicast", "arp_peer_request_unexpected_unicast",
    "arp_peer_request_expected_broadcast", "arp_peer_request_unexpected_broadcast",
    "arp_peer_reply_expected_unicast", "arp_peer_reply_unexpected_unicast",
    "arp_peer_reply_expected_broadcast", "arp_peer_reply_unexpected_broadcast",
    "tcp22_syn_out", "tcp22_syn_in", "tcp22_synack_out", "tcp22_synack_in",
    "tcp22_rst_out", "tcp22_rst_in", "tcp22_other_out", "tcp22_other_in",
    "icmp_unreachable_tcp22",
)
INCOMPLETE_CODES = frozenset({
    "capture_setup", "capture_error", "counter_overflow", "packet_drops",
    "truncated_packet", "unsupported_header", "stopped_early",
    "route_query_incomplete", "neighbor_query_incomplete", "ready_emit_failed",
    "statistics_unavailable", "observer_clock_error", "ready_missing",
    "packet_accounting_mismatch", "packet_direction_invalid", "packet_metadata_invalid",
    "readiness_late", "observer_interval_overrun", "packet_outside_interval",
})
IP_ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C", "LC_ALL": "C"}
_NEIGHBOR_STATES = frozenset({"NONE", "INCOMPLETE", "REACHABLE", "STALE", "DELAY", "PROBE", "FAILED", "NOARP", "PERMANENT"})
_IFACE = re.compile(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}\Z", re.ASCII)
_UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\Z", re.ASCII)
_BACKEND = re.compile(r"[A-Za-z0-9_-]{1,64}\Z", re.ASCII)
_MAC = re.compile(r"(?:[0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}\Z", re.ASCII)


def _pairs_no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise WatchError("duplicate JSON object key")
        result[key] = value
    return result


def _bad_constant(_value):
    raise WatchError("non-finite JSON number")


def _exact_keys(value, expected, label):
    if type(value) is not dict or set(value) != set(expected):
        raise WatchError(f"invalid {label} fields")


def _private_lease(value, label):
    if type(value) is not str or len(value) > 15:
        raise WatchError(f"invalid {label}")
    try:
        address = ipaddress.IPv4Address(value)
    except ipaddress.AddressValueError as exc:
        raise WatchError(f"invalid {label}") from exc
    pools = (ipaddress.IPv4Network("10.0.0.0/8"),
             ipaddress.IPv4Network("172.16.0.0/12"),
             ipaddress.IPv4Network("192.168.0.0/16"))
    if not any(address in pool for pool in pools):
        raise WatchError(f"{label} must be an RFC1918 lease")
    return address


def _mac(value, label):
    if type(value) is not str or not _MAC.fullmatch(value):
        raise WatchError(f"invalid {label}")
    raw = bytes.fromhex(value.replace(":", ""))
    if raw == b"\x00" * 6 or raw[0] & 1:
        raise WatchError(f"{label} must be a unicast MAC")
    return raw


def parse_watch(raw: bytes) -> dict:
    """Parse one bounded, duplicate-free, exact-schema watch from stdin bytes."""
    if type(raw) is not bytes or len(raw) > MAX_WATCH_BYTES:
        raise WatchError("watch size is outside bounds")
    try:
        value = json.loads(raw.decode("utf-8", "strict"), object_pairs_hook=_pairs_no_duplicates,
                           parse_constant=_bad_constant)
    except (ValueError, RecursionError) as exc:
        # JSON's integer conversion limit raises plain ValueError on 3.11+.
        # All decoder failures stay inside the fixed invalid-watch boundary.
        raise WatchError("watch is not valid bounded JSON") from exc
    _exact_keys(value, {"version", "domain", "role", "interface", "candidate", "control", "gateway", "duration_ms"}, "watch")
    if type(value["version"]) is not int or value["version"] != 1:
        raise WatchError("unsupported watch version")
    if value["domain"] != "n1qualification":
        raise WatchError("watch domain is not admitted")
    if value["role"] not in ("candidate", "control"):
        raise WatchError("watch role is not admitted")
    if type(value["interface"]) is not str or not _IFACE.fullmatch(value["interface"]):
        raise WatchError("invalid interface")
    if type(value["duration_ms"]) is not int or not 1 <= value["duration_ms"] <= 30000:
        raise WatchError("duration is outside bounds")
    sessions = []
    uuids = []
    backends = []
    for label in ("candidate", "control"):
        item = value[label]
        _exact_keys(item, {"session_uuid", "generation_uuid", "backend", "address", "mac"}, label)
        for field in ("session_uuid", "generation_uuid"):
            candidate_uuid = item[field]
            if type(candidate_uuid) is not str or not _UUID.fullmatch(candidate_uuid) or candidate_uuid == "00000000-0000-0000-0000-000000000000":
                raise WatchError(f"invalid {label} {field}")
            uuids.append(candidate_uuid)
        if type(item["backend"]) is not str or not _BACKEND.fullmatch(item["backend"]):
            raise WatchError(f"invalid {label} backend")
        backends.append(item["backend"])
        addr = _private_lease(item["address"], f"{label} address")
        mac = _mac(item["mac"], f"{label} MAC")
        sessions.append({**item, "_ip": addr, "_mac": mac})
    gateway = _private_lease(value["gateway"], "gateway")
    if len(set(uuids)) != len(uuids) or len(set(backends)) != 2:
        raise WatchError("candidate and control bindings must be distinct")
    candidate, control = sessions
    if candidate["_ip"] == control["_ip"] or candidate["_mac"] == control["_mac"]:
        raise WatchError("candidate and control leases must be distinct")
    if gateway in (candidate["_ip"], control["_ip"]):
        raise WatchError("gateway must be distinct from both leases")
    return {**value, "candidate": candidate, "control": control, "_gateway": gateway}


def _assembler():
    """Resolve forward-only jumps, using JA trampolines for long branches."""
    code, labels = [], {}

    def label(name):
        if name in labels:
            raise ValueError("duplicate BPF label")
        labels[name] = len(code)

    def ins(op, k=0, jt=0, jf=0, target=None, target_false=None):
        code.append((op, k, jt, jf, target, target_false))

    def finish():
        offsets = [0]
        for op, *_rest in code:
            offsets.append(offsets[-1] + (3 if op & 7 == 5 and op != 5 else 1))
        result = []
        for index, (op, k, jt, jf, yes, no) in enumerate(code):
            pc = offsets[index]
            if op & 7 == 5 and op != 5:
                result.append((op, 0, 1, k))
                for at, target, default in ((pc + 1, yes, jt), (pc + 2, no, jf)):
                    destination = offsets[labels[target]] if target is not None else offsets[index + 1 + default]
                    distance = destination - at - 1
                    if distance < 0:
                        raise ValueError("backward BPF jump")
                    result.append((5, 0, 0, distance))
            else:
                result.append((op, jt, jf, k))
        return result

    return label, ins, finish


def build_filter(watch: dict) -> list[tuple[int, int, int, int]]:
    """Select only pair headers; unsupported pair formats remain incomplete.

    Up to two Ethernet tags are decoded solely to establish the same IP pair.
    No broad VLAN/ICMP marker admits unrelated packets. Classic BPF cannot prove
    a tuple whose bytes are absent; such packets are excluded, not counted zero.
    """
    candidate = int(watch["candidate"]["_ip"])
    control = int(watch["control"]["_ip"])
    local = int(watch[watch["role"]]["_ip"])
    peer = control if watch["role"] == "candidate" else candidate
    gateway = int(watch["_gateway"])
    label, ins, finish = _assembler()

    def check(op, value):
        ins(op, value, target_false="reject")

    def return_ip_header(offset):
        # Unsupported advertised lengths cannot justify copying options or
        # padding. The fixed 20-byte IPv4 header alone identifies the pair.
        ins(0x06, offset + 20)

    def tcp(prefix, offset, source, destination, port_offset):
        label(prefix)
        ins(0x20, offset + 12)
        check(0x15, source)
        ins(0x20, offset + 16)
        check(0x15, destination)
        # Fragments establish the pair but cannot establish the port: retain
        # only the fixed IP header and invalidate coverage in the classifier.
        ins(0x28, offset + 6)
        ins(0x45, 0x3fff, target_false=prefix + "normal")
        ins(0x06, offset + 20)
        label(prefix + "normal")
        ins(0x30, offset)
        ins(0x54, 0xf0)
        check(0x15, 0x40)
        ins(0xb1, offset)
        ins(0x87)
        ins(0x35, 20, target=prefix + "ihl")
        ins(0x06, offset + 20)
        label(prefix + "ihl")
        ins(0x04, offset + 4)
        ins(0x07)
        ins(0x80)
        ins(0x3d, target=prefix + "ports")
        return_ip_header(offset)
        label(prefix + "ports")
        ins(0xb1, offset)
        ins(0x48, offset + port_offset)
        check(0x15, 22)
        # The pair and TCP22 port are known. Missing later TCP fields must
        # produce an incomplete marker instead of an out-of-bounds rejection.
        ins(0x87)
        ins(0x04, offset + 20)
        ins(0x07)
        ins(0x80)
        ins(0x3d, target=prefix + "minimum_tcp_present")
        return_ip_header(offset)
        label(prefix + "minimum_tcp_present")
        ins(0xb1, offset)
        # A malformed short TCP header must not turn following data into a
        # userspace header capture. Examine the data offset inside the filter.
        ins(0x40, offset + 12)
        ins(0x74, 28)
        ins(0x35, 5, target=prefix + "tcp_header")
        return_ip_header(offset)
        label(prefix + "tcp_header")
        ins(0x87)
        ins(0x04, 20)
        ins(0x07)
        ins(0x28, offset + 2)
        ins(0x3d, target=prefix + "total_length")
        return_ip_header(offset)
        label(prefix + "total_length")
        ins(0xb1, offset)
        ins(0x87)
        ins(0x04, offset + 20)
        ins(0x16)

    def emit(prefix, offset):
        label(prefix + "type")
        ins(0x28, offset - 2)
        ins(0x15, ETH_P_ARP, target=prefix + "arp")
        ins(0x15, ETH_P_IP, target=prefix + "ip", target_false="reject")
        label(prefix + "arp")
        # Only Ethernet/IPv4 ARP has these source/target field positions.
        ins(0x20, offset)
        check(0x15, 0x00010800)
        ins(0x28, offset + 4)
        check(0x15, 0x0604)
        ins(0x20, offset + 14)
        ins(0x15, candidate, target=prefix + "arp_forward")
        check(0x15, control)
        ins(0x20, offset + 24)
        check(0x15, candidate)
        ins(0x06, offset + 28)
        label(prefix + "arp_forward")
        ins(0x20, offset + 24)
        check(0x15, control)
        ins(0x06, offset + 28)
        label(prefix + "ip")
        ins(0x30, offset + 9)
        ins(0x15, IPPROTO_TCP, target=prefix + "tcp_dispatch")
        ins(0x15, IPPROTO_ICMP, target=prefix + "icmp", target_false="reject")
        label(prefix + "tcp_dispatch")
        ins(0x20, offset + 12)
        ins(0x15, candidate, target=prefix + "forward", target_false=prefix + "reverse")
        tcp(prefix + "forward", offset, candidate, control, 2)
        tcp(prefix + "reverse", offset, control, candidate, 0)
        label(prefix + "icmp")
        ins(0x20, offset + 12)
        ins(0x15, peer, target=prefix + "icmp_dest")
        check(0x15, gateway)
        label(prefix + "icmp_dest")
        ins(0x20, offset + 16)
        check(0x15, local)
        ins(0x28, offset + 6)
        # A fragmented quote cannot be safely established; exclude it.
        ins(0x45, 0x3fff, target="reject")
        ins(0x30, offset)
        ins(0x54, 0xf0)
        check(0x15, 0x40)
        ins(0xb1, offset)
        ins(0x87)
        check(0x35, 20)
        ins(0x40, offset)  # ICMP type/code/checksum word
        ins(0x54, 0xff000000)
        check(0x15, 0x03000000)
        ins(0x87)
        ins(0x04, offset + 8)
        ins(0x02, 0)  # ST quote base
        ins(0x04, 20)
        ins(0x14, offset)
        ins(0x07)
        ins(0x28, offset + 2)
        ins(0x3d, target=prefix + "quote_within_ip", target_false="reject")
        label(prefix + "quote_within_ip")
        ins(0x61, 0)
        ins(0x40, 12)
        check(0x15, candidate)
        ins(0x40, 16)
        check(0x15, control)
        ins(0x40, 8)
        ins(0x54, 0x00ff0000)
        check(0x15, IPPROTO_TCP << 16)
        ins(0x48, 6)
        ins(0x45, 0x3fff, target_false=prefix + "quote_unfragmented")
        ins(0x60, 0)
        ins(0x04, 20)
        ins(0x16)
        label(prefix + "quote_unfragmented")
        ins(0x48, 2)
        ins(0x02, 4)  # ST quoted IPv4 declared total length
        ins(0x40, 0)
        ins(0x74, 24)
        ins(0x02, 1)  # ST inner version/IHL
        ins(0x54, 0xf0)
        check(0x15, 0x40)
        ins(0x60, 1)
        ins(0x54, 15)
        ins(0x64, 2)
        check(0x35, 20)
        ins(0x02, 3)  # ST quoted IHL
        ins(0x04, 20)
        ins(0x07)
        ins(0x60, 4)
        ins(0x3d, target=prefix + "quote_tcp_length")
        ins(0x60, 0)
        ins(0x04, 20)
        ins(0x16)
        label(prefix + "quote_tcp_length")
        ins(0x61, 0)
        ins(0x60, 3)
        ins(0x0c)  # ADD X (quote base)
        ins(0x02, 2)
        ins(0x04, 4)
        ins(0x07)
        ins(0x80)
        ins(0x3d, target=prefix + "quote_ports_present")
        ins(0x60, 0)
        ins(0x04, 20)
        ins(0x16)
        label(prefix + "quote_ports_present")
        ins(0x60, 2)
        ins(0x07)
        ins(0x48, 2)
        check(0x15, 22)
        ins(0x87)
        ins(0x04, 4)
        ins(0x14, offset)
        ins(0x07)
        ins(0x28, offset + 2)
        ins(0x3d, target=prefix + "quote_ports_within_ip")
        ins(0x60, 0)
        ins(0x04, 20)
        ins(0x16)
        label(prefix + "quote_ports_within_ip")
        ins(0x60, 2)
        ins(0x04, 4)
        ins(0x16)  # through quoted TCP ports, never its payload

    # Dispatch known tag depths before the packet-specific programs.
    for depth, offset in enumerate((14, 18, 22)):
        label("depth" + str(depth))
        ins(0x28, offset - 2)
        ins(0x15, ETH_P_ARP, target=f"p{depth}_type")
        ins(0x15, ETH_P_IP, target=f"p{depth}_type")
        if depth < 2:
            ins(0x15, 0x8100, target="depth" + str(depth + 1))
            ins(0x15, 0x88a8, target="depth" + str(depth + 1), target_false="reject")
        else:
            ins(0x15, 0, target_false="reject", target="reject")
    for depth, offset in enumerate((14, 18, 22)):
        emit(f"p{depth}_", offset)
    label("reject")
    ins(0x06, 0)
    program = finish()
    return program

def _u16(data, offset):
    if offset < 0 or offset + 2 > len(data):
        raise IndexError
    return struct.unpack_from("!H", data, offset)[0]


def _u32(data, offset):
    if offset < 0 or offset + 4 > len(data):
        raise IndexError
    return struct.unpack_from("!I", data, offset)[0]


def _ether_type(header):
    if len(header) < 14:
        raise IndexError
    return _u16(header, 12)


def _same_ip_pair(watch, source, target):
    local = watch[watch["role"]]["_ip"]
    peer = watch["control" if watch["role"] == "candidate" else "candidate"]["_ip"]
    return (source == local and target == peer) or (source == peer and target == local)


def classify_header(watch: dict, header: bytes, packet_type: int) -> str | None:
    """Return one finite metadata code; never return packet bytes or addresses."""
    try:
        if type(packet_type) is not int or packet_type not in (PACKET_HOST, PACKET_BROADCAST, PACKET_OUTGOING):
            return "packet_direction_invalid"
        outgoing = packet_type == PACKET_OUTGOING
        ether_type = _ether_type(header)
        local = watch[watch["role"]]
        peer = watch["control" if watch["role"] == "candidate" else "candidate"]
        if ether_type == ETH_P_ARP:
            if len(header) < 42:
                return "unsupported_header"
            if header[14:20] != b"\x00\x01\x08\x00\x06\x04":
                return "unsupported_header"
            operation = _u16(header, 20)
            if operation not in (1, 2):
                return "unsupported_header"
            ethernet_source = header[6:12]
            sender_mac = header[22:28]
            sender_ip = ipaddress.IPv4Address(header[28:32])
            target_ip = ipaddress.IPv4Address(header[38:42])
            if not _same_ip_pair(watch, sender_ip, target_ip):
                return None
            if outgoing != (sender_ip == local["_ip"]):
                return "packet_direction_invalid"
            if ethernet_source != sender_mac:
                return "unsupported_header"
            if sender_ip == local["_ip"]:
                if sender_mac != local["_mac"]:
                    return "unsupported_header"
                return "arp_local_request" if operation == 1 else "arp_local_reply"
            if sender_ip != peer["_ip"]:
                return None
            expected = sender_mac == peer["_mac"]
            broadcast = header[:6] == b"\xff" * 6
            if not broadcast and header[:6] != local["_mac"]:
                return "unsupported_header"
            kind = "reply" if operation == 2 else "request"
            mac = "expected" if expected else "unexpected"
            dest = "broadcast" if broadcast else "unicast"
            return f"arp_peer_{kind}_{mac}_{dest}"
        if ether_type != ETH_P_IP or len(header) < 34:
            return "unsupported_header"
        first = header[14]
        version, ihl = first >> 4, (first & 0x0f) * 4
        if version != 4 or ihl < 20:
            return "unsupported_header"
        source = ipaddress.IPv4Address(header[26:30])
        target = ipaddress.IPv4Address(header[30:34])
        protocol = header[23]
        if not _same_ip_pair(watch, source, target) and not (protocol == IPPROTO_ICMP and target == watch[watch["role"]]["_ip"]):
            return None
        if _u16(header, 20) & 0x3fff:
            return "unsupported_header"
        if len(header) < 14 + ihl:
            return "unsupported_header"
        if protocol == IPPROTO_TCP:
            if not _same_ip_pair(watch, source, target):
                return None
            total_len = _u16(header, 16)
            if total_len < ihl + 20:
                return "unsupported_header"
            if len(header) < 14 + ihl + 20:
                return "unsupported_header"
            sport, dport = _u16(header, 14 + ihl), _u16(header, 16 + ihl)
            candidate_ip = watch["candidate"]["_ip"]
            control_ip = watch["control"]["_ip"]
            if source == candidate_ip and target == control_ip:
                if dport != 22:
                    return None
            elif source == control_ip and target == candidate_ip:
                if sport != 22:
                    return None
            else:
                return None
            if outgoing != (source == local["_ip"]):
                return "packet_direction_invalid"
            if ihl != 20:
                return "unsupported_header"
            tcp_offset = 14 + ihl
            tcp_words = header[tcp_offset + 12] >> 4
            if tcp_words < 5 or tcp_words * 4 > total_len - ihl:
                return "unsupported_header"
            flags = header[tcp_offset + 13]
            direction = "out" if source == watch[watch["role"]]["_ip"] else "in"
            if flags & 0x04:
                state = "rst"
            elif flags & 0x12 == 0x12:
                state = "synack"
            elif flags & 0x02:
                state = "syn"
            else:
                state = "other"
            return f"tcp22_{state}_{direction}"
        if protocol == IPPROTO_ICMP:
            if target != watch[watch["role"]]["_ip"]:
                return None
            gateway = watch["_gateway"]
            peer_ip = peer["_ip"]
            if source not in (gateway, peer_ip):
                return None
            if outgoing:
                return "packet_direction_invalid"
            if ihl != 20:
                return "unsupported_header"
            if len(header) < 14 + 20 + 8:
                return "unsupported_header"
            total_len = _u16(header, 16)
            if total_len < 20 + 8:
                return "unsupported_header"
            if header[34] != 3:
                return None
            quote = 42
            if len(header) < quote + 20:
                return "unsupported_header"
            inner_first = header[quote]
            if inner_first != 0x45 or _u16(header, quote + 6) & 0x3fff or _u16(header, quote + 2) < 40:
                return "unsupported_header"
            if header[quote + 9] != IPPROTO_TCP:
                return None
            inner_source = ipaddress.IPv4Address(header[quote + 12:quote + 16])
            inner_target = ipaddress.IPv4Address(header[quote + 16:quote + 20])
            if inner_source != watch["candidate"]["_ip"] or inner_target != watch["control"]["_ip"]:
                return None
            if len(header) < quote + 24 or total_len < 52:
                return "unsupported_header"
            if _u16(header, quote + 22) != 22:
                return None
            return "icmp_unreachable_tcp22"
        return None
    except (IndexError, ValueError, struct.error):
        return "unsupported_header"


def _capture_packet_type(watch, header, address):
    """Validate the Linux AF_PACKET address; missing direction is never HOST."""
    if type(address) is not tuple or len(address) != 5:
        return None
    interface, protocol, packet_type, hardware_type, hardware_address = address
    if (type(interface) is not str or interface != watch["interface"] or
            type(protocol) is not int or protocol not in (ETH_P_IP, ETH_P_ARP, 0x8100, 0x88a8) or
            type(packet_type) is not int or packet_type not in (PACKET_HOST, PACKET_BROADCAST, PACKET_OUTGOING) or
            type(hardware_type) is not int or hardware_type != 1 or
            type(hardware_address) is not bytes or len(hardware_address) != 6):
        return None
    try:
        if protocol != _ether_type(header):
            # VLAN/offload representation disagreement is unsupported capture
            # coverage, never evidence for a protocol's presence or absence.
            return "unsupported_header"
    except (IndexError, struct.error):
        return None
    return packet_type


def _parse_json_output(raw):
    if type(raw) is not bytes or len(raw) > MAX_COMMAND_OUTPUT_BYTES:
        raise ValueError
    try:
        value = json.loads(raw.decode("utf-8", "strict"), object_pairs_hook=_pairs_no_duplicates, parse_constant=_bad_constant)
    except (ValueError, RecursionError):
        raise ValueError from None
    if type(value) is not list or len(value) > 1 or any(type(row) is not dict for row in value):
        raise ValueError
    return value


def _command_result(runner, argv):
    try:
        result = runner(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE, timeout=IP_COMMAND_TIMEOUT,
                        close_fds=True, env=dict(IP_ENV))
    except Exception:
        return None
    if getattr(result, "returncode", 1) != 0:
        return None
    return getattr(result, "stdout", None)


def _query_network_state(watch: dict, runner=None) -> dict:
    """Run only fixed route/neighbor reads and return a schema-sanitized record."""
    if runner is None:
        runner = _bounded_ip_command
    local = watch[watch["role"]]
    peer = watch["control" if watch["role"] == "candidate" else "candidate"]
    interface, peer_ip = watch["interface"], str(peer["_ip"])
    route_raw = _command_result(runner, ["/usr/sbin/ip", "-j", "route", "get", peer_ip])
    neigh_raw = _command_result(runner, ["/usr/sbin/ip", "-j", "neigh", "show", "to", peer_ip, "dev", interface])
    route = {"status": "unavailable", "device_matches": False, "source_matches": False,
             "gateway_matches": None, "metric": None}
    neighbor = {"status": "unavailable", "state": "UNKNOWN", "mac_matches": None}
    try:
        rows = _parse_json_output(route_raw)
        if len(rows) != 1:
            raise ValueError
        row = rows[0]
        dst = row.get("dst")
        dev = row.get("dev")
        if "prefsrc" not in row:
            raise ValueError
        source_values = [row[key] for key in ("prefsrc", "src") if key in row]
        if type(dst) is not str or type(dev) is not str or not source_values or any(type(item) is not str for item in source_values):
            raise ValueError
        if any(item != source_values[0] for item in source_values[1:]):
            raise ValueError
        if ipaddress.IPv4Address(dst) != peer["_ip"]:
            raise ValueError
        source = ipaddress.IPv4Address(source_values[0])
        gateway = row.get("gateway")
        if gateway is not None:
            if type(gateway) is not str:
                raise ValueError
            ipaddress.IPv4Address(gateway)
        metric = row.get("metric")
        if metric is not None and (type(metric) is not int or not 0 <= metric <= 2**31 - 1):
            raise ValueError
        route = {"status": "ok", "device_matches": dev == interface,
                 "source_matches": source == local["_ip"],
                 "gateway_matches": (ipaddress.IPv4Address(gateway) == watch["_gateway"] if gateway is not None else None),
                 "metric": metric}
    except (ValueError, TypeError):
        route["status"] = "invalid"
    try:
        rows = _parse_json_output(neigh_raw)
        if not rows:
            neighbor = {"status": "absent", "state": "NONE", "mac_matches": None}
        elif len(rows) != 1:
            raise ValueError
        else:
            row = rows[0]
            dst, dev = row.get("dst"), row.get("dev")
            if type(dst) is not str or type(dev) is not str or ipaddress.IPv4Address(dst) != peer["_ip"] or dev != interface:
                raise ValueError
            state = row.get("state")
            if type(state) is list and len(state) == 1:
                state = state[0]
            if type(state) is not str or state not in _NEIGHBOR_STATES:
                raise ValueError
            lladdr = row.get("lladdr")
            if lladdr is None:
                mac_matches = None
            elif type(lladdr) is str and _MAC.fullmatch(lladdr):
                mac_matches = bytes.fromhex(lladdr.replace(":", "")) == peer["_mac"]
            else:
                raise ValueError
            neighbor = {"status": "present", "state": state, "mac_matches": mac_matches}
    except (ValueError, TypeError):
        neighbor = {"status": "invalid", "state": "UNKNOWN", "mac_matches": None}
    return {"route": route, "neighbor": neighbor}


def _bounded_ip_command(argv, **kwargs):
    """Bound pipe bytes and lifetime, including a child-inherited open pipe."""
    route = type(argv) is list and len(argv) == 5 and argv[:4] == ["/usr/sbin/ip", "-j", "route", "get"]
    neighbor = type(argv) is list and len(argv) == 8 and argv[:5] == ["/usr/sbin/ip", "-j", "neigh", "show", "to"] and argv[6] == "dev"
    if not (route or neighbor):
        raise ValueError("unrecognized fixed command")
    _private_lease(argv[4] if route else argv[5], "peer")
    if neighbor and (type(argv[7]) is not str or not _IFACE.fullmatch(argv[7])):
        raise ValueError("invalid interface")
    proc = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, close_fds=True, env=dict(IP_ENV))
    selector = None
    buffers = {"stdout": bytearray(), "stderr": bytearray()}
    invalid = False
    returncode = 1
    try:
        selector = selectors.DefaultSelector()
        for stream, name in ((proc.stdout, "stdout"), (proc.stderr, "stderr")):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, name)
        deadline = time.monotonic() + IP_COMMAND_TIMEOUT
        while selector.get_map():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                invalid = True
                break
            for key, _mask in selector.select(timeout=min(0.05, remaining)):
                try:
                    chunk = os.read(key.fd, 1024)
                except BlockingIOError:
                    continue
                if not chunk:
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                target = buffers[key.data]
                if len(target) + len(chunk) > MAX_COMMAND_OUTPUT_BYTES:
                    invalid = True
                    break
                target.extend(chunk)
            if invalid:
                break
        if invalid:
            proc.kill()
        try:
            returncode = proc.wait(timeout=0.1)
        except subprocess.TimeoutExpired:
            invalid = True
            proc.kill()
            try:
                proc.wait(timeout=0.1)
            except subprocess.TimeoutExpired:
                pass
    except Exception:
        invalid = True
        proc.kill()
        try:
            proc.wait(timeout=0.1)
        except subprocess.TimeoutExpired:
            pass
    finally:
        if selector is not None:
            selector.close()
        for stream in (proc.stdout, proc.stderr):
            if stream is not None and not stream.closed:
                stream.close()
    return subprocess.CompletedProcess(argv, 1 if invalid else returncode,
                                       b"" if invalid else bytes(buffers["stdout"]), b"")


def _binding(watch):
    # Re-emit only validated, known fields; internal parsed objects never escape.
    return {name: {field: watch[name][field] for field in ("session_uuid", "generation_uuid", "backend", "address", "mac")}
            for name in ("candidate", "control")}


def _interval_fields(watch):
    # These caps are tied to the requested interval and the 250ms recv quantum.
    # The original deadline never moves after readiness or a scheduling stall.
    return {"interval_origin": "ready_emit_start",
            "readiness_budget_us": min(100000, watch["duration_ms"] * 250),
            "closing_tolerance_us": min(250000, watch["duration_ms"] * 250)}


def _checked_clock(clock, previous=None):
    value = clock()
    if type(value) not in (int, float) or not math.isfinite(value):
        raise ObserverError("invalid monotonic clock")
    value = float(value)
    if previous is not None and value < previous:
        raise ObserverError("monotonic clock regressed")
    return value


def _clock_offset_units(later, origin, units):
    # Validate the derived delta before scaling: two finite samples can still
    # subtract to infinity, and scaling a huge finite delta can overflow.
    delta = later - origin
    if not math.isfinite(delta) or not 0.0 <= delta <= 60.0:
        raise ObserverError("invalid monotonic interval")
    return int(round(delta * units))


def _ready_record(watch):
    return {"record": "ready", "version": 1, "domain": "n1qualification",
            "role": watch["role"], "interface": watch["interface"],
            "duration_ms": watch["duration_ms"], "gateway": watch["gateway"], "binding": _binding(watch),
            **_interval_fields(watch)}


def _summary_record(watch, *, ready, complete, deadline_reached, elapsed_ms, reasons, counters, drops, packets, states, overflow, readiness_delay_us=0):
    # This conditional candidate-only gate is deliberately narrower than generic
    # zero attribution. The driver still must bind the single connect, socket
    # controls, current pair/generations, before/after route/neighbor observations,
    # and qualified guest-kernel/offload support before drawing the tap inference.
    emitted_arp_or_syn = any(counters.get(code, 0) for code in
                            ("arp_local_request", "arp_local_reply", "tcp22_syn_out", "tcp22_synack_out"))
    candidate_absence = (watch["role"] == "candidate" and ready and complete and
                         deadline_reached and not reasons and not drops and not overflow and
                         0 <= packets <= MAX_EVENTS and not emitted_arp_or_syn)
    inference = {"scope": "supported_well_formed_outgoing_candidate_arp_or_tcp22_syn_at_guest_tap",
                 "candidate_pre_emission_absence_if_driver_controls_pass": bool(candidate_absence),
                 "driver_controls_required": True, "qualified_guest_capture_required": True,
                 "global_absence": False, "downstream_absence": False,
                 "enqueue": False, "delivery": False}
    return {"record": "summary", "version": 1, "domain": "n1qualification",
            "role": watch["role"], "interface": watch["interface"],
            "duration_ms": watch["duration_ms"], "gateway": watch["gateway"], "binding": _binding(watch),
            **_interval_fields(watch), "readiness_delay_us": int(readiness_delay_us),
            "coverage_scope": "identified_pair_headers", "zero_count_attribution": False,
            "negative_inference": inference,
            "ready": bool(ready), "deadline_reached": bool(deadline_reached),
            "elapsed_ms": int(elapsed_ms), "complete": bool(complete),
            "overflow": bool(overflow), "drops": int(drops), "observed_packets": int(packets),
            "incomplete_reasons": sorted(code for code in reasons if code in INCOMPLETE_CODES),
            "counters": {code: int(counters.get(code, 0)) for code in EVENT_CODES},
            "route_before": states.get("route_before", {}),
            "neighbor_before": states.get("neighbor_before", {}),
            "route_after": states.get("route_after", {}),
            "neighbor_after": states.get("neighbor_after", {})}


def _read_stats(sock):
    raw = sock.getsockopt(SOL_PACKET, PACKET_STATISTICS, 8)
    if type(raw) is not bytes or len(raw) < 8:
        raise ObserverError("packet statistics unavailable")
    packets, drops = struct.unpack_from("=II", raw)
    return packets, drops


def _attach_filter(sock, program):
    """Attach classic BPF before bind; ctypes keeps the sock_fprog pointer valid."""
    class BpfInsn(ctypes.Structure):
        _fields_ = [("code", ctypes.c_ushort), ("jt", ctypes.c_ubyte),
                    ("jf", ctypes.c_ubyte), ("k", ctypes.c_uint32)]
    class SockFprog(ctypes.Structure):
        _fields_ = [("len", ctypes.c_ushort), ("filter", ctypes.POINTER(BpfInsn))]
    if not program or len(program) > 4096:
        raise ObserverError("filter program is outside bounds")
    array_type = BpfInsn * len(program)
    instructions = array_type(*(BpfInsn(*item) for item in program))
    fprog = SockFprog(len(program), instructions)
    libc = ctypes.CDLL(None, use_errno=True)
    setsockopt = libc.setsockopt
    setsockopt.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.c_void_p, ctypes.c_uint]
    setsockopt.restype = ctypes.c_int
    result = setsockopt(sock.fileno(), socket.SOL_SOCKET, SO_ATTACH_FILTER,
                        ctypes.byref(fprog), ctypes.sizeof(fprog))
    if result != 0:
        raise ObserverError("could not attach metadata filter")


def _observe(watch: dict, *, socket_factory=None, attach_filter=None, runner=None,
             clock=time.monotonic, sleep=time.sleep, on_ready=None, stop_requested=None):
    """Internal seam for deterministic fixtures; production uses Linux defaults."""
    # Own a fresh validated operation binding. A caller cannot retarget the
    # mutable parsed dictionary while the tap or readiness sink is running.
    public = {key: watch[key] for key in ("version", "domain", "role", "interface", "gateway", "duration_ms")}
    public.update(_binding(watch))
    watch = parse_watch(json.dumps(public, allow_nan=False).encode("utf-8"))
    if socket_factory is None:
        socket_factory = socket.socket
    if attach_filter is None:
        attach_filter = _attach_filter
    if runner is None:
        runner = _bounded_ip_command
    if stop_requested is None:
        stop_requested = lambda: False
    counters = {code: 0 for code in EVENT_CODES}
    reasons = set()
    states = {}
    packets = drops = 0
    overflow = False
    ready = False
    deadline_reached = False
    elapsed_ms = 0
    sock = None
    bound = False
    start = stop_time = deadline = None
    closing_tolerance = 0.0
    readiness_delay_us = 0
    before = _query_network_state(watch, runner=runner)
    states.update({"route_before": before["route"], "neighbor_before": before["neighbor"]})
    if before["route"]["status"] != "ok":
        reasons.add("route_query_incomplete")
    if before["neighbor"]["status"] in ("unavailable", "invalid"):
        reasons.add("neighbor_query_incomplete")
    try:
        # Protocol zero prevents packet delivery before the filter is attached.
        # Python converts the host-order protocol in bind() to network order.
        sock = socket_factory(AF_PACKET, socket.SOCK_RAW, 0)
        attach_filter(sock, build_filter(watch))
        try:
            _read_stats(sock)  # clear counters before binding starts the interval
        except Exception:
            reasons.add("statistics_unavailable")
        sock.bind((watch["interface"], ETH_P_ALL))
        bound = True
        sock.settimeout(0.25)
        try:
            start = _checked_clock(clock)
            stop_time = last = start
        except Exception:
            reasons.add("observer_clock_error")
        if start is not None:
            deadline = start + watch["duration_ms"] / 1000.0
            timing = _interval_fields(watch)
            ready_budget = timing["readiness_budget_us"] / 1000000.0
            closing_tolerance = timing["closing_tolerance_us"] / 1000000.0
            if on_ready is None:
                reasons.add("ready_missing")
            else:
                try:
                    on_ready(_ready_record(watch))
                except Exception:
                    reasons.add("ready_emit_failed")
            if not (reasons & {"ready_missing", "ready_emit_failed"}):
                try:
                    stop_time = last = _checked_clock(clock, last)
                    if last >= deadline or last - start > ready_budget:
                        reasons.add("readiness_late")
                    else:
                        ready = True
                    if last >= deadline:
                        deadline_reached = True
                    if last > deadline + closing_tolerance:
                        reasons.add("observer_interval_overrun")
                    readiness_delay_us = _clock_offset_units(last, start, 1000000)
                except Exception:
                    reasons.add("observer_clock_error")
            while ready:
                try:
                    stop_time = last = _checked_clock(clock, last)
                except Exception:
                    reasons.add("observer_clock_error")
                    break
                if last > deadline + closing_tolerance:
                    reasons.add("observer_interval_overrun")
                if last >= deadline:
                    deadline_reached = True
                    break
                try:
                    if stop_requested():
                        reasons.add("stopped_early")
                        break
                except Exception:
                    reasons.add("stopped_early")
                    break
                try:
                    sock.settimeout(max(0.000001, min(0.25, deadline - last)))
                    data, _ancillary, flags, address = sock.recvmsg(IP_SNAPLEN)
                except (socket.timeout, TimeoutError):
                    continue
                except Exception:
                    reasons.add("capture_error")
                    break
                if packets >= MAX_EVENTS:
                    overflow = True
                    reasons.add("counter_overflow")
                    break
                # Count the actual dispatch for kernel accounting, but a late
                # return cannot enter any protocol-event or inference counter.
                packets += 1
                try:
                    stop_time = last = _checked_clock(clock, last)
                except Exception:
                    reasons.add("observer_clock_error")
                    break
                if last >= deadline:
                    reasons.add("packet_outside_interval")
                    deadline_reached = True
                    if last > deadline + closing_tolerance:
                        reasons.add("observer_interval_overrun")
                    break
                if flags & MSG_TRUNC:
                    reasons.add("truncated_packet")
                packet_type = _capture_packet_type(watch, data, address)
                if packet_type is None:
                    reasons.add("packet_metadata_invalid")
                    continue
                if packet_type == "unsupported_header":
                    reasons.add("unsupported_header")
                    continue
                event = classify_header(watch, data, packet_type)
                if event in ("unsupported_header", "packet_direction_invalid"):
                    reasons.add(event)
                    continue
                if event is None:
                    reasons.add("unsupported_header")
                    continue
                if counters[event] >= MAX_EVENTS or sum(counters.values()) >= MAX_EVENTS:
                    overflow = True
                    reasons.add("counter_overflow")
                    continue
                counters[event] += 1
    except Exception:
        reasons.add("capture_setup" if not ready else "capture_error")
    finally:
        if sock is not None:
            if bound:
                try:
                    kernel_packets, drops = _read_stats(sock)
                    if drops:
                        reasons.add("packet_drops")
                    if kernel_packets != packets:
                        reasons.add("packet_accounting_mismatch")
                except Exception:
                    reasons.add("statistics_unavailable")
            try:
                sock.close()
            except Exception:
                reasons.add("capture_error")
        # Teardown belongs to the finite capture interval. Sample only after
        # both final statistics and the close attempt, independently of their
        # success, and before the separate after-route/neighbor queries.
        if start is not None and stop_time is not None:
            try:
                closed_at = _checked_clock(clock, stop_time)
                if deadline is not None and closed_at > deadline + closing_tolerance:
                    reasons.add("observer_interval_overrun")
                elapsed_ms = _clock_offset_units(closed_at, start, 1000)
            except Exception:
                reasons.add("observer_clock_error")
    after = _query_network_state(watch, runner=runner)
    states.update({"route_after": after["route"], "neighbor_after": after["neighbor"]})
    if after["route"]["status"] != "ok":
        reasons.add("route_query_incomplete")
    if after["neighbor"]["status"] in ("unavailable", "invalid"):
        reasons.add("neighbor_query_incomplete")
    if overflow:
        reasons.add("counter_overflow")
    complete = ready and deadline_reached and not reasons and packets <= MAX_EVENTS and not overflow
    return _summary_record(watch, ready=ready, complete=complete, deadline_reached=deadline_reached,
                           elapsed_ms=elapsed_ms, reasons=reasons, counters=counters, drops=drops, packets=packets,
                           states=states, overflow=overflow, readiness_delay_us=readiness_delay_us)


def observe(watch: dict) -> dict:
    """Observe one already validated watch using the finite Linux implementation."""
    return _observe(watch, on_ready=lambda _record: None)


def _emit(record, stream):
    stream.write(json.dumps(record, separators=(",", ":"), sort_keys=True) + "\n")
    stream.flush()


def main(argv=None, stdin=None, stdout=None, stderr=None):
    argv = sys.argv[1:] if argv is None else argv
    stdin = sys.stdin.buffer if stdin is None else stdin
    stdout = sys.stdout if stdout is None else stdout
    stderr = sys.stderr if stderr is None else stderr
    if argv != ["observe"]:
        _emit({"record": "error", "code": "invalid_command"}, stderr)
        return 2
    raw = stdin.read(MAX_WATCH_BYTES + 1)
    try:
        watch = parse_watch(raw)
    except WatchError:
        _emit({"record": "error", "code": "invalid_watch"}, stderr)
        return 2
    result = _observe(watch, on_ready=lambda record: _emit(record, stdout))
    _emit(result, stdout)
    return 0 if result["complete"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
