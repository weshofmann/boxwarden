#!/usr/bin/env python3
"""Deterministic fixtures for the finite N1 guest metadata observer."""
import json
import io
import socket
import struct
import sys
import unittest
from unittest import mock

import guest_metadata_observer as observer

LOCAL_IP = "192.168.64.3"
PEER_IP = "192.168.64.2"
GATEWAY_IP = "192.168.64.1"
LOCAL_MAC = "02:00:00:00:00:03"
PEER_MAC = "02:00:00:00:00:02"
OTHER_MAC = "02:00:00:00:00:04"
BROADCAST = b"\xff" * 6


def watch(**changes):
    value = {
        "version": 1,
        "domain": "n1qualification",
        "role": "candidate",
        "interface": "enp0s1",
        "candidate": {
            "session_uuid": "00000000-0000-4000-8000-000000000003",
            "generation_uuid": "10000000-0000-4000-8000-000000000003",
            "backend": "candidate-vm",
            "address": LOCAL_IP,
            "mac": LOCAL_MAC,
        },
        "control": {
            "session_uuid": "00000000-0000-4000-8000-000000000002",
            "generation_uuid": "10000000-0000-4000-8000-000000000002",
            "backend": "control-vm",
            "address": PEER_IP,
            "mac": PEER_MAC,
        },
        "gateway": GATEWAY_IP,
        "duration_ms": 1000,
    }
    value.update(changes)
    return value


def arp(sender_ip=LOCAL_IP, target_ip=PEER_IP, *, sender_mac=LOCAL_MAC,
        ethernet_source=None, ethernet_dest=BROADCAST, operation=1):
    smac = bytes.fromhex(sender_mac.replace(":", ""))
    esrc = smac if ethernet_source is None else bytes.fromhex(ethernet_source.replace(":", ""))
    frame = bytearray(42)
    frame[0:6] = ethernet_dest
    frame[6:12] = esrc
    frame[12:14] = b"\x08\x06"
    frame[14:22] = b"\x00\x01\x08\x00\x06\x04" + struct.pack("!H", operation)
    frame[22:28] = smac
    frame[28:32] = socket.inet_aton(sender_ip)
    frame[32:38] = b"\x00" * 6
    frame[38:42] = socket.inet_aton(target_ip)
    return bytes(frame)


def ipv4_tcp(src=LOCAL_IP, dst=PEER_IP, sport=40123, dport=22, flags=0x02, *, options=b""):
    ihl = 20 + len(options)
    assert ihl % 4 == 0
    tcp = struct.pack("!HHIIHHHH", sport, dport, 1, 0, (5 << 12) | flags, 4096, 0, 0)
    ip = bytearray(20)
    ip[0] = 0x40 | (ihl // 4)
    ip[2:4] = struct.pack("!H", ihl + len(tcp))
    ip[8] = 64
    ip[9] = 6
    ip[12:16] = socket.inet_aton(src)
    ip[16:20] = socket.inet_aton(dst)
    return b"\x02\x00\x00\x00\x00\x03\x02\x00\x00\x00\x00\x02\x08\x00" + bytes(ip) + options + tcp


def ipv4_icmp_unreachable(*, outer_src=GATEWAY_IP, outer_dst=LOCAL_IP,
                          quoted_src=LOCAL_IP, quoted_dst=PEER_IP,
                          quoted_dport=22, outer_options=b"", inner_options=b"", inner_fragment=0):
    quoted_ip = bytearray(20)
    quoted_ip[0] = 0x45 + len(inner_options) // 4
    quoted_ip[6:8] = struct.pack("!H", inner_fragment)
    quoted_ip[2:4] = struct.pack("!H", 40 + len(inner_options))
    quoted_ip[9] = 6
    quoted_ip[12:16] = socket.inet_aton(quoted_src)
    quoted_ip[16:20] = socket.inet_aton(quoted_dst)
    quoted_tcp = struct.pack("!HH", 40123, quoted_dport)
    icmp = b"\x03\x03\x00\x00\x00\x00\x00\x00" + bytes(quoted_ip) + inner_options + quoted_tcp
    outer_ip = bytearray(20)
    outer_ip[0] = 0x45 + len(outer_options) // 4
    outer_ip[2:4] = struct.pack("!H", 20 + len(outer_options) + len(icmp))
    outer_ip[8] = 64
    outer_ip[9] = 1
    outer_ip[12:16] = socket.inet_aton(outer_src)
    outer_ip[16:20] = socket.inet_aton(outer_dst)
    return b"\x02\x00\x00\x00\x00\x03\x02\x00\x00\x00\x00\x02\x08\x00" + bytes(outer_ip) + outer_options + icmp


def run_cbpf(program, packet):
    """Small classic-BPF interpreter for the opcodes emitted by build_filter."""
    a = x = 0
    mem = [0] * 16
    pc = 0
    steps = 0
    while pc < len(program):
        steps += 1
        if steps > len(program) + 1:
            raise AssertionError("BPF program did not terminate")
        code, jt, jf, k = program[pc]
        cls, mode, size, op = code & 0x07, code & 0xe0, code & 0x18, code & 0xf0
        if code == 0x20:  # BPF_LD | BPF_W | BPF_ABS
            if k + 4 > len(packet):
                return 0
            a = struct.unpack_from("!I", packet, k)[0]
        elif code == 0x28:  # BPF_LD | BPF_H | BPF_ABS
            if k + 2 > len(packet):
                return 0
            a = struct.unpack_from("!H", packet, k)[0]
        elif code == 0x30:  # BPF_LD | BPF_B | BPF_ABS
            if k >= len(packet):
                return 0
            a = packet[k]
        elif code == 0xb1:  # BPF_LDX | BPF_B | BPF_MSH
            if k >= len(packet):
                return 0
            x = (packet[k] & 0x0f) << 2
        elif code == 0x40:  # BPF_LD | BPF_W | BPF_IND
            at = x + k
            if at + 4 > len(packet):
                return 0
            a = struct.unpack_from("!I", packet, at)[0]
        elif code == 0x50:  # BPF_LD | BPF_B | BPF_IND
            at = x + k
            if at >= len(packet):
                return 0
            a = packet[at]
        elif code == 0x48:  # BPF_LD | BPF_H | BPF_IND
            at = x + k
            if at + 2 > len(packet):
                return 0
            a = struct.unpack_from("!H", packet, at)[0]
        elif code == 0x00:  # BPF_LD | BPF_W | BPF_IMM
            a = k
        elif code == 0x05:  # BPF_JMP | BPF_JA
            pc += k + 1
            continue
        elif code in (0x15, 0x25, 0x35, 0x45):
            test = {0x15: a == k, 0x25: a > k, 0x35: a >= k, 0x45: (a & k) != 0}[code]
            pc += (jt if test else jf) + 1
            continue
        elif code == 0x02:
            mem[k] = a
        elif code == 0x60:
            a = mem[k]
        elif code == 0x61:
            x = mem[k]
        elif code == 0x14:
            a = (a - k) & 0xffffffff
        elif code == 0x54:
            a &= k
        elif code == 0x64:
            a = (a << k) & 0xffffffff
        elif code == 0x74:
            a >>= k
        elif code == 0x0c:
            a = (a + x) & 0xffffffff
        elif code == 0x80:  # LD LEN
            a = len(packet)
        elif code == 0x07:  # TAX
            x = a
        elif code == 0x3d:  # JGE X
            pc += (jt if a >= x else jf) + 1
            continue
        elif code == 0x87:  # BPF_MISC | BPF_TXA
            a = x
        elif code == 0x04:  # BPF_ALU | BPF_ADD | BPF_K
            a = (a + k) & 0xffffffff
        elif code == 0x16:  # BPF_RET | BPF_A
            return a
        elif code == 0x06:  # BPF_RET | BPF_K
            return k
        else:
            raise AssertionError(f"unsupported cBPF opcode {code:#x}")
        pc += 1
    raise AssertionError("BPF program fell off end")


class FakeSocket:
    def __init__(self, events, packets=(), drops=0, stats_packets=0):
        self.events = events
        self.packets = list(packets)
        self.drops = drops
        self.stats_packets = stats_packets
        self.stats_reads = 0
        self.closed = False

    def bind(self, address):
        self.events.append(("bind", address))

    def settimeout(self, timeout):
        self.events.append(("timeout", timeout))

    def recvmsg(self, size, ancbufsize=0):
        if self.packets:
            item = self.packets.pop(0)
            if isinstance(item, Exception):
                raise item
            return item
        raise socket.timeout()

    def getsockopt(self, level, option, size=0):
        self.events.append(("stats", level, option))
        if level == observer.SOL_PACKET and option == observer.PACKET_STATISTICS:
            self.stats_reads += 1
            if self.stats_reads == 1:
                return struct.pack("=II", 0, 0)
            return struct.pack("=II", self.stats_packets, self.drops)
        raise AssertionError("unexpected getsockopt")

    def close(self):
        self.closed = True
        self.events.append(("close",))


class GuestMetadataObserverTests(unittest.TestCase):
    def test_watch_schema_is_strict_and_rejects_duplicate_keys(self):
        valid = json.dumps(watch(), separators=(",", ":")).encode()
        self.assertEqual(observer.parse_watch(valid)["role"], "candidate")
        with self.assertRaises(observer.WatchError):
            observer.parse_watch(valid[:-1] + b',"role":"control"}')
        with self.assertRaises(observer.WatchError):
            observer.parse_watch(valid + b" " * (observer.MAX_WATCH_BYTES + 1))
        for changed in [
            {**watch(), "extra": "ignored?"},
            {**watch(), "duration_ms": True},
            {**watch(), "role": "host"},
            {**watch(), "interface": "../../eth0"},
            {**watch(), "duration_ms": 30001},
            {**watch(), "candidate": {**watch()["candidate"], "address": "127.0.0.1"}},
            {**watch(), "control": {**watch()["control"], "mac": "03:00:00:00:00:02"}},
        ]:
            with self.subTest(changed=changed):
                with self.assertRaises(observer.WatchError):
                    observer.parse_watch(json.dumps(changed).encode())

    def test_filter_matches_only_pair_arp_tcp22_and_quoted_unreachable(self):
        w = observer.parse_watch(json.dumps(watch()).encode())
        program = observer.build_filter(w)
        self.assertTrue(all(len(insn) == 4 for insn in program))
        self.assertEqual(run_cbpf(program, arp()), observer.ARP_SNAPLEN)
        self.assertEqual(run_cbpf(program, arp(LOCAL_IP, PEER_IP, operation=2)), observer.ARP_SNAPLEN)
        self.assertEqual(run_cbpf(program, arp(PEER_IP, LOCAL_IP, sender_mac=PEER_MAC, operation=2)), observer.ARP_SNAPLEN)
        self.assertEqual(run_cbpf(program, ipv4_tcp()), 54)
        self.assertEqual(run_cbpf(program, ipv4_tcp(PEER_IP, LOCAL_IP, 22, 40123, 0x12)), 54)
        self.assertEqual(run_cbpf(program, ipv4_icmp_unreachable()), 66)
        icmp_payload = ipv4_icmp_unreachable() + b"ICMP-PAYLOAD-CANARY"
        icmp_snap = run_cbpf(program, icmp_payload)
        self.assertEqual(icmp_snap, 66)
        self.assertNotIn(b"ICMP-PAYLOAD-CANARY", icmp_payload[:icmp_snap])
        self.assertEqual(run_cbpf(program, arp(LOCAL_IP, GATEWAY_IP)), 0)
        self.assertEqual(run_cbpf(program, arp("192.168.64.99", PEER_IP)), 0)
        self.assertEqual(run_cbpf(program, ipv4_tcp(LOCAL_IP, "192.168.64.99")), 0)
        self.assertEqual(run_cbpf(program, ipv4_tcp(LOCAL_IP, PEER_IP, 40123, 443)), 0)
        self.assertEqual(run_cbpf(program, ipv4_tcp(LOCAL_IP, PEER_IP, 22, 443)), 0)
        self.assertEqual(run_cbpf(program, ipv4_tcp(PEER_IP, LOCAL_IP, 443, 22)), 0)
        self.assertEqual(run_cbpf(program, ipv4_icmp_unreachable(quoted_dport=443)), 0)
        tagged = lambda packet: packet[:12] + b"\x81\x00\x00\x01" + packet[12:]
        for packet in (arp(), ipv4_tcp(), ipv4_icmp_unreachable()):
            for depth in (1, 2):
                frame = packet
                for _ in range(depth):
                    frame = tagged(frame)
                snap = run_cbpf(program, frame)
                self.assertEqual(snap, len(packet) + depth * 4)
                self.assertEqual(observer.classify_header(w, frame[:snap], observer.PACKET_HOST), "unsupported_header")
        for packet in (arp("192.168.64.99", PEER_IP), ipv4_tcp(LOCAL_IP, "192.168.64.99"), ipv4_icmp_unreachable(quoted_dst="192.168.64.99")):
            self.assertEqual(run_cbpf(program, tagged(tagged(packet))), 0)
        vlan = b"\x02\x00\x00\x00\x00\x03\x02\x00\x00\x00\x00\x02\x81\x00\x00\x01\x08\x00"
        self.assertEqual(run_cbpf(program, vlan), 0)
        self.assertEqual(observer.classify_header(w, vlan, observer.PACKET_HOST), "unsupported_header")
        self.assertEqual(run_cbpf(program, ipv4_icmp_unreachable(quoted_dst="192.168.64.99")), 0)
        self.assertEqual(run_cbpf(program, ipv4_icmp_unreachable(outer_dst="192.168.64.99")), 0)

    def test_filter_accepts_cached_neighbor_syn_without_arp(self):
        w = observer.parse_watch(json.dumps(watch()).encode())
        program = observer.build_filter(w)
        self.assertEqual(run_cbpf(program, ipv4_tcp(flags=0x02)), 54)
        payload = ipv4_tcp(flags=0x02) + b"PAYLOAD-CANARY"
        snap = run_cbpf(program, payload)
        self.assertEqual(snap, 54)
        self.assertNotIn(b"PAYLOAD-CANARY", payload[:snap])
        self.assertEqual(run_cbpf(program, ipv4_tcp(options=b"\x01\x01\x01\x01")), 58)

    def test_header_classification_distinguishes_peer_mac_and_broadcast(self):
        w = observer.parse_watch(json.dumps(watch()).encode())
        self.assertEqual(observer.classify_header(w, arp(PEER_IP, LOCAL_IP, sender_mac=PEER_MAC, operation=2), observer.PACKET_HOST), "arp_peer_reply_expected_broadcast")
        self.assertEqual(observer.classify_header(w, arp(PEER_IP, LOCAL_IP, sender_mac=OTHER_MAC, operation=2), observer.PACKET_HOST), "arp_peer_reply_unexpected_broadcast")
        self.assertEqual(observer.classify_header(w, arp(PEER_IP, LOCAL_IP, sender_mac=PEER_MAC, ethernet_dest=bytes.fromhex(LOCAL_MAC.replace(":", "")), operation=2), observer.PACKET_HOST), "arp_peer_reply_expected_unicast")
        mismatch = arp(PEER_IP, LOCAL_IP, sender_mac=PEER_MAC, ethernet_source=OTHER_MAC, operation=2)
        self.assertEqual(observer.classify_header(w, mismatch, observer.PACKET_HOST), "unsupported_header")
        self.assertEqual(observer.classify_header(w, ipv4_tcp(flags=0x02), observer.PACKET_OUTGOING), "tcp22_syn_out")
        self.assertEqual(observer.classify_header(w, ipv4_icmp_unreachable(), observer.PACKET_HOST), "icmp_unreachable_tcp22")
        self.assertIsNone(observer.classify_header(w, ipv4_icmp_unreachable(quoted_dst="192.168.64.99"), observer.PACKET_HOST))
        self.assertEqual(observer.classify_header(w, ipv4_tcp(options=b"\x01\x01\x01\x01"), observer.PACKET_OUTGOING), "unsupported_header")
        control_watch = observer.parse_watch(json.dumps(watch(role="control")).encode())
        control_filter = observer.build_filter(control_watch)
        self.assertEqual(run_cbpf(control_filter, ipv4_tcp(LOCAL_IP, PEER_IP, 40123, 22, 0x02)), 54)
        self.assertEqual(observer.classify_header(control_watch, ipv4_tcp(LOCAL_IP, PEER_IP, 40123, 22, 0x02), observer.PACKET_HOST), "tcp22_syn_in")
        response = ipv4_tcp(PEER_IP, LOCAL_IP, 22, 40123, 0x12)
        self.assertEqual(run_cbpf(control_filter, response), 54)
        self.assertEqual(observer.classify_header(control_watch, response, observer.PACKET_OUTGOING), "tcp22_synack_out")
        control_unreachable = ipv4_icmp_unreachable(outer_dst=PEER_IP)
        self.assertEqual(run_cbpf(control_filter, control_unreachable), 66)
        self.assertEqual(observer.classify_header(control_watch, control_unreachable, observer.PACKET_HOST), "icmp_unreachable_tcp22")

    def test_fixed_read_only_route_and_neighbor_argv_and_sanitized_shape(self):
        w = observer.parse_watch(json.dumps(watch()).encode())
        calls = []

        class Completed:
            returncode = 0
            stderr = b"ignored diagnostic text"

            def __init__(self, stdout):
                self.stdout = stdout

        def runner(argv, **kwargs):
            calls.append((argv, kwargs))
            if argv[2:4] == ["route", "get"]:
                return Completed(b'[{"dst":"192.168.64.2","dev":"enp0s1","prefsrc":"192.168.64.3","gateway":"192.168.64.1","metric":100}]')
            return Completed(b'[{"dst":"192.168.64.2","dev":"enp0s1","lladdr":"02:00:00:00:00:02","state":["REACHABLE"]}]')

        result = observer._query_network_state(w, runner=runner)
        self.assertEqual([c[0] for c in calls], [
            ["/usr/sbin/ip", "-j", "route", "get", PEER_IP],
            ["/usr/sbin/ip", "-j", "neigh", "show", "to", PEER_IP, "dev", "enp0s1"],
        ])
        for _argv, kwargs in calls:
            self.assertEqual(kwargs["stdin"], observer.subprocess.DEVNULL)
            self.assertTrue(kwargs["close_fds"])
            self.assertLessEqual(kwargs["timeout"], observer.IP_COMMAND_TIMEOUT)
            self.assertEqual(set(kwargs["env"]), {"PATH", "LANG", "LC_ALL"})
        self.assertEqual(result["route"]["source_matches"], True)
        self.assertEqual(result["neighbor"]["state"], "REACHABLE")
        self.assertNotIn("02:00:00:00:00:02", json.dumps(result))
        self.assertNotIn("ignored diagnostic text", json.dumps(result))

        def conflicting_runner(argv, **_kwargs):
            if argv[2] == "route":
                return Completed(b'[{"dst":"192.168.64.2","dev":"enp0s1","prefsrc":"192.168.64.3","src":"192.168.64.4"}]')
            return Completed(b"[]")
        conflicted = observer._query_network_state(w, runner=conflicting_runner)
        self.assertEqual(conflicted["route"]["status"], "invalid")

    def test_observer_attaches_filter_before_bind_and_requires_ready_stop_and_clean_loss(self):
        w = observer.parse_watch(json.dumps(watch()).encode())
        events = []
        fake = FakeSocket(events)
        times = iter([0.0, 0.0, 0.0, 1.1, 1.1])
        ready = []
        result = observer._observe(w, socket_factory=lambda *a: (events.append(("socket", a)) or fake),
                                   attach_filter=lambda sock, program: events.append(("attach", sock, program)),
                                   runner=fake_runner,
                                   clock=lambda: next(times), sleep=lambda _: None,
                                   on_ready=lambda record: ready.append(record))
        self.assertEqual([event[0] for event in events[:4]], ["socket", "attach", "stats", "bind"])
        self.assertEqual(events[0][1][2], 0)  # no packet delivery before filter attachment
        self.assertEqual(events[3][1], ("enp0s1", observer.ETH_P_ALL))  # bind takes host-order protocol
        self.assertEqual(len(ready), 1)
        self.assertEqual(ready[0]["record"], "ready")
        self.assertTrue(result["complete"])
        self.assertTrue(result["deadline_reached"])
        self.assertEqual(result["elapsed_ms"], 1100)
        self.assertEqual(result["incomplete_reasons"], [])
        self.assertTrue(result["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"])
        self.assertTrue(fake.closed)

    def test_drop_truncation_unknown_matching_header_and_missing_readiness_are_incomplete(self):
        w = observer.parse_watch(json.dumps(watch()).encode())
        scenarios = [
            {"drops": 2},
            {"stats_packets": 1},  # kernel saw a queued packet that recvmsg did not drain
            {"packets": [(b"x" * 60, [], observer.MSG_TRUNC, None)]},
            {"packets": [(b"\x02" * 42, [], 0, None)]},
        ]
        for scenario in scenarios:
            with self.subTest(scenario=scenario):
                events = []
                fake = FakeSocket(events, **scenario)
                times = iter([0.0, 0.0, 0.0, 0.0, 1.1, 1.1])
                result = observer._observe(w, socket_factory=lambda *a: fake,
                    attach_filter=lambda *_a: None, runner=fake_runner,
                    clock=lambda: next(times), sleep=lambda _: None, on_ready=lambda _: None)
                self.assertFalse(result["complete"])
        events = []
        fake = FakeSocket(events)
        failed = observer._observe(w, socket_factory=lambda *a: fake,
                attach_filter=lambda *_a: None, runner=fake_runner,
                clock=lambda: 0.0, sleep=lambda _: None,
                on_ready=lambda _record: (_ for _ in ()).throw(RuntimeError("sink")))
        self.assertFalse(failed["ready"])
        self.assertIn("ready_emit_failed", failed["incomplete_reasons"])
        self.assertTrue(fake.closed)
        fake = FakeSocket([])
        missing = observer._observe(w, socket_factory=lambda *a: fake,
                attach_filter=lambda *_a: None, runner=fake_runner,
                clock=lambda: 0.0, sleep=lambda _: None, on_ready=None)
        self.assertFalse(missing["ready"])
        self.assertIn("ready_missing", missing["incomplete_reasons"])

    def test_stopped_or_unready_window_is_incomplete(self):
        w = observer.parse_watch(json.dumps(watch()).encode())
        events = []
        fake = FakeSocket(events)
        times = iter([0.0, 0.0, 0.0, 0.2, 0.2])
        result = observer._observe(w, socket_factory=lambda *a: fake,
            attach_filter=lambda *_a: None, runner=fake_runner,
            clock=lambda: next(times), sleep=lambda _: None, on_ready=lambda _: None,
            stop_requested=lambda: True)
        self.assertIn("stopped_early", result["incomplete_reasons"])
        self.assertFalse(result["deadline_reached"])
        self.assertEqual(result["elapsed_ms"], 200)

    def test_cli_emits_only_bounded_ready_and_final_records(self):
        raw_watch = json.dumps(watch()).encode()
        output, errors = io.StringIO(), io.StringIO()

        def fake_observe(parsed, **kwargs):
            kwargs["on_ready"](observer._ready_record(parsed))
            return observer._summary_record(parsed, ready=True, complete=False,
                deadline_reached=False, elapsed_ms=0, reasons={"stopped_early"},
                counters={code: 0 for code in observer.EVENT_CODES}, drops=0,
                packets=0, states={}, overflow=False)

        with mock.patch.object(observer, "_observe", side_effect=fake_observe):
            status = observer.main(["observe"], io.BytesIO(raw_watch), output, errors)
        records = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual(status, 1)
        self.assertEqual([record["record"] for record in records], ["ready", "summary"])
        self.assertEqual(set(records[0]), {"record", "version", "domain", "role", "interface", "duration_ms", "gateway", "binding", "interval_origin", "readiness_budget_us", "closing_tolerance_us"})
        self.assertEqual(records[1]["deadline_reached"], False)
        self.assertEqual(records[1]["elapsed_ms"], 0)
        self.assertEqual(errors.getvalue(), "")
        self.assertNotIn("_ip", output.getvalue())
        self.assertNotIn("_mac", output.getvalue())

    def test_icmp_options_fragments_and_truncated_quotes_never_leak_payload(self):
        w = self.parsed(); program = observer.build_filter(w)
        for outer, inner in ((b"\x01" * 4, b""), (b"", b"\x01" * 4), (b"\x01" * 40, b"\x01" * 40)):
            frame = ipv4_icmp_unreachable(outer_options=outer, inner_options=inner)
            snap = run_cbpf(program, frame)
            self.assertEqual(snap, len(frame))
            self.assertEqual(observer.classify_header(w, frame[:snap], observer.PACKET_HOST), "unsupported_header")
            unrelated = ipv4_icmp_unreachable(outer_options=outer, inner_options=inner, quoted_dst="192.168.64.99")
            self.assertEqual(run_cbpf(program, unrelated), 0)
        fragmented = ipv4_icmp_unreachable(inner_fragment=1) + b"PAYLOAD-CANARY"
        snap = run_cbpf(program, fragmented)
        self.assertLessEqual(snap, 62)
        self.assertEqual(observer.classify_header(w, fragmented[:snap], observer.PACKET_HOST), "unsupported_header")
        malformed_outer = bytearray(ipv4_icmp_unreachable())
        malformed_outer[16:18] = struct.pack("!H", 28)
        self.assertEqual(run_cbpf(program, malformed_outer), 0)
        malformed_inner = bytearray(ipv4_icmp_unreachable())
        malformed_inner[44:46] = struct.pack("!H", 20)
        snap = run_cbpf(program, malformed_inner)
        self.assertEqual(snap, 62)
        self.assertEqual(observer.classify_header(w, malformed_inner[:snap], observer.PACKET_HOST), "unsupported_header")
        truncated = ipv4_icmp_unreachable()[:62]
        snap = run_cbpf(program, truncated)
        self.assertEqual(snap, 62)
        self.assertEqual(observer.classify_header(w, truncated[:snap], observer.PACKET_HOST), "unsupported_header")

    def test_malformed_advertised_ihl_never_captures_post_header_marker(self):
        w = self.parsed(); program = observer.build_filter(w)
        frame = bytearray(ipv4_tcp()[:34])
        frame[14] = 0x4f  # advertises 60-byte header
        frame[16:18] = struct.pack("!H", 20)  # actual declared IP packet is 20 bytes
        frame.extend(b"POST-HEADER-CANARY")
        snap = run_cbpf(program, frame)
        self.assertEqual(snap, 34)
        self.assertNotIn(b"POST-HEADER-CANARY", frame[:snap])
        self.assertEqual(observer.classify_header(w, frame[:snap], observer.PACKET_HOST), "unsupported_header")
        # Also exercise the declared-length fallback after purported TCP ports
        # and data offset are present in padding outside the declared packet.
        padding = bytearray(40 + 20)
        padding[40:44] = struct.pack("!HH", 40123, 22)
        padding[52] = 0x50
        frame = frame[:34] + padding + b"POST-HEADER-CANARY"
        snap = run_cbpf(program, frame)
        self.assertEqual(snap, 34)

    def test_review_R2_actual_statistics_and_socket_close_define_closing_time(self):
        for phase in ("statistics", "close"):
            for close_at, complete in ((1.25, True), (1.3, False)):
                with self.subTest(phase=phase, close_at=close_at):
                    state = {"now": 0.0, "queries": 0}; events = []
                    class ClosingSocket(FakeSocket):
                        def recvmsg(self, *_args):
                            state["now"] = 1.0
                            raise socket.timeout()
                        def getsockopt(self, *args):
                            result = super().getsockopt(*args)
                            if self.stats_reads == 2:
                                events.append("final_statistics")
                                if phase == "statistics": state["now"] = close_at
                            return result
                        def close(self):
                            events.append("capture_close")
                            if phase == "close": state["now"] = close_at
                            super().close()
                    def clock():
                        events.append("clock")
                        return state["now"]
                    def runner(argv, **kwargs):
                        state["queries"] += 1
                        if state["queries"] > 2:
                            events.append("after_query")
                            state["now"] = 99.0  # helper time lies outside capture closure
                        return fake_runner(argv, **kwargs)
                    fake = ClosingSocket([])
                    result = observer._observe(self.parsed(), socket_factory=lambda *_: fake,
                        attach_filter=lambda *_: None, runner=runner, clock=clock,
                        on_ready=lambda _: None)
                    self.assertTrue(fake.closed)
                    self.assertEqual(result["complete"], complete)
                    self.assertEqual(result["elapsed_ms"], round(close_at * 1000))
                    self.assertEqual(result["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"], complete)
                    self.assertTrue(result["deadline_reached"])
                    self.assertEqual(sum(result["counters"].values()), 0)
                    self.assertIn("clock", events[events.index("capture_close") + 1:events.index("after_query")])
                    if not complete:
                        self.assertIn("observer_interval_overrun", result["incomplete_reasons"])
                    else:
                        self.assertEqual(result["incomplete_reasons"], [])

    def test_review_R2_final_clock_and_cleanup_failures_are_independently_incomplete(self):
        cases = [
            (0.9, False, False, 0, 0, {"observer_clock_error"}),
            (float("nan"), False, False, 0, 0, {"observer_clock_error"}),
            (float("inf"), False, False, 0, 0, {"observer_clock_error"}),
            (True, False, False, 0, 0, {"observer_clock_error"}),
            (1.0, False, True, 0, 0, {"capture_error"}),
            (1.0, True, False, 0, 0, {"statistics_unavailable"}),
            (1.0, False, False, 1, 1, {"packet_drops", "packet_accounting_mismatch"}),
            (float("nan"), True, True, 0, 0,
                {"observer_clock_error", "statistics_unavailable", "capture_error"}),
        ]
        for final_time, fail_stats, fail_close, drops, packets, expected in cases:
            with self.subTest(final_time=final_time, fail_stats=fail_stats, fail_close=fail_close):
                state = {"now": 0.0, "clock_after_close": False}
                class ClosingSocket(FakeSocket):
                    def recvmsg(self, *_args):
                        state["now"] = 1.0
                        raise socket.timeout()
                    def getsockopt(self, *args):
                        result = super().getsockopt(*args)
                        if self.stats_reads == 2 and fail_stats:
                            raise OSError("PRIVATE-STATISTICS-CANARY")
                        return result
                    def close(self):
                        super().close()
                        state["now"] = final_time
                        if fail_close: raise OSError("PRIVATE-CLOSE-CANARY")
                fake = ClosingSocket([], drops=drops, stats_packets=packets)
                def clock():
                    if fake.closed: state["clock_after_close"] = True
                    return state["now"]
                result = observer._observe(self.parsed(), socket_factory=lambda *_: fake,
                    attach_filter=lambda *_: None, runner=fake_runner, clock=clock,
                    on_ready=lambda _: None)
                self.assertTrue(fake.closed)
                self.assertTrue(state["clock_after_close"])
                self.assertFalse(result["complete"])
                self.assertTrue(expected.issubset(result["incomplete_reasons"]))
                self.assertFalse(result["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"])
                self.assertNotIn("PRIVATE-", json.dumps(result))
                self.assertLessEqual(result["elapsed_ms"], 60000)

    def test_review_R2_extreme_finite_clock_offsets_remain_bounded_and_close(self):
        for phase, start in (("readiness", 0.0), ("close", 0.0), ("readiness", -1e308)):
            with self.subTest(phase=phase, start=start):
                state = {"now": start}
                class ClosingSocket(FakeSocket):
                    def recvmsg(self, *_args):
                        state["now"] = 1.0
                        raise socket.timeout()
                    def close(self):
                        super().close()
                        if phase == "close": state["now"] = 1e308
                fake = ClosingSocket([])
                result = observer._observe(self.parsed(), socket_factory=lambda *_: fake,
                    attach_filter=lambda *_: None, runner=fake_runner,
                    clock=lambda: state["now"],
                    on_ready=lambda _: state.update(now=1e308) if phase == "readiness" else None)
                self.assertTrue(fake.closed)
                self.assertFalse(result["complete"])
                self.assertIn("observer_clock_error", result["incomplete_reasons"])
                self.assertIn("observer_interval_overrun", result["incomplete_reasons"])
                self.assertTrue(0 <= result["elapsed_ms"] <= 60000)
                self.assertTrue(0 <= result["readiness_delay_us"] <= 60000000)

    def test_review_I5_bounded_long_integer_uses_only_fixed_cli_error(self):
        raw = json.dumps(watch(), separators=(",", ":")).encode().replace(b'"version":1', b'"version":' + b"7" * 5000, 1)
        self.assertLess(len(raw), observer.MAX_WATCH_BYTES)
        with self.assertRaises(observer.WatchError): observer.parse_watch(raw)
        out, err = io.StringIO(), io.StringIO()
        self.assertEqual(observer.main(["observe"], io.BytesIO(raw), out, err), 2)
        self.assertEqual(out.getvalue(), "")
        self.assertEqual(err.getvalue(), '{"code":"invalid_watch","record":"error"}\n')

    def test_review_I5_sibling_ip_json_normalizes_integer_conversion_error(self):
        # ip JSON's smaller byte bound can still reach the same decoder class
        # when current Python's integer limit is configured below that bound.
        if hasattr(sys, "set_int_max_str_digits"):
            previous = sys.get_int_max_str_digits()
            try:
                sys.set_int_max_str_digits(640)
                hostile = b'[{"metric":' + b"7" * 1000 + b'}]'
                self.assertLess(len(hostile), observer.MAX_COMMAND_OUTPUT_BYTES)
                with self.assertRaises(ValueError) as caught:
                    observer._parse_json_output(hostile)
                self.assertEqual(str(caught.exception), "")
            finally:
                sys.set_int_max_str_digits(previous)

    def test_review_I4_candidate_scoped_absence_is_conditional_and_bounded(self):
        zero = {code: 0 for code in observer.EVENT_CODES}
        def summary(role="candidate", **changes):
            fields = dict(ready=True, complete=True, deadline_reached=True, elapsed_ms=1000,
                reasons=set(), counters=zero, drops=0, packets=0, states={}, overflow=False)
            fields.update(changes)
            return observer._summary_record(self.parsed(role=role), **fields)
        expected_scope = "supported_well_formed_outgoing_candidate_arp_or_tcp22_syn_at_guest_tap"
        result = summary()
        inference = result["negative_inference"]
        self.assertEqual(inference["scope"], expected_scope)
        self.assertTrue(inference["candidate_pre_emission_absence_if_driver_controls_pass"])
        self.assertTrue(inference["driver_controls_required"])
        self.assertTrue(inference["qualified_guest_capture_required"])
        for key in ("global_absence", "downstream_absence", "enqueue", "delivery"):
            self.assertIs(inference[key], False)
        self.assertIs(result["zero_count_attribution"], False)
        for changes in ({"role": "control"}, {"complete": False}, {"ready": False},
                        {"deadline_reached": False}, {"drops": 1}, {"overflow": True},
                        {"reasons": {"packet_outside_interval"}}, {"reasons": {"unsupported_header"}},
                        {"reasons": {"packet_direction_invalid"}}, {"reasons": {"packet_accounting_mismatch"}}):
            self.assertFalse(summary(**changes)["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"])
        for code in ("arp_local_request", "arp_local_reply", "tcp22_syn_out", "tcp22_synack_out"):
            counts = dict(zero, **{code: 1})
            self.assertFalse(summary(counters=counts, packets=1)["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"])
        counts = dict(zero, tcp22_syn_in=1)
        self.assertTrue(summary(counters=counts, packets=1)["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"])

    def test_review_I3_original_deadline_readiness_budget_and_closing_bounds(self):
        for duration, expected_ready, expected_close in ((1, 250, 250), (30000, 100000, 250000)):
            record = observer._ready_record(self.parsed(duration_ms=duration))
            self.assertEqual(record["interval_origin"], "ready_emit_start")
            self.assertEqual(record["readiness_budget_us"], expected_ready)
            self.assertEqual(record["closing_tolerance_us"], expected_close)

    def test_review_I3_expired_readiness_and_late_packets_cannot_complete(self):
        cases = [
            (1, 59.0, None, None, False, 0),
            (1000, 2.0, None, None, False, 0),
            (1, 0.000251, None, None, False, 0),
            (30000, 0.100001, None, None, False, 0),
            (1, 0.0, None, 0.001, True, 0),
            (30000, 0.1, None, 30.25, True, 0),
            (1000, 0.0, None, 1.250001, False, 0),
            (1000, 0.0, 0.999, 1.0, True, 1),
            (1000, 0.0, 1.0, 1.0, False, 0),
            (1000, 0.0, 1.0001, 1.0001, False, 0),
            (1000, 0.0, 2.0, 2.0, False, 0),
        ]
        for duration, ready_at, receive_at, close_at, complete, count in cases:
            with self.subTest(duration=duration, ready=ready_at, receive=receive_at, close=close_at):
                state = {"now": 0.0, "received": False}
                class MovingSocket(FakeSocket):
                    def recvmsg(self, *_args):
                        if receive_at is not None and not state["received"]:
                            state["received"] = True; state["now"] = receive_at
                            return ipv4_tcp(), [], 0, ("enp0s1", observer.ETH_P_IP, observer.PACKET_OUTGOING, 1, bytes.fromhex(LOCAL_MAC.replace(":", "")))
                        state["now"] = close_at
                        raise socket.timeout()
                fake = MovingSocket([], stats_packets=0 if receive_at is None else 1)
                result = observer._observe(self.parsed(duration_ms=duration), socket_factory=lambda *_: fake,
                    attach_filter=lambda *_: None, runner=fake_runner,
                    clock=lambda: state["now"], on_ready=lambda _: state.update(now=ready_at))
                self.assertEqual(result["complete"], complete)
                self.assertEqual(result["counters"]["tcp22_syn_out"], count)
                if ready_at >= duration / 1000 or ready_at > min(0.1, duration / 4000):
                    self.assertFalse(result["ready"])
                    self.assertIn("readiness_late", result["incomplete_reasons"])
                if receive_at is not None and receive_at >= duration / 1000:
                    self.assertIn("packet_outside_interval", result["incomplete_reasons"])
                if close_at is not None and close_at > duration / 1000 + min(0.25, duration / 4000):
                    self.assertIn("observer_interval_overrun", result["incomplete_reasons"])

    def test_review_I2_identifiable_tcp_truncations_all_roles_directions_tags(self):
        tag = lambda frame: frame[:12] + b"\x81\x00\x00\x01" + frame[12:]
        for role in ("candidate", "control"):
            w = self.parsed(role=role); program = observer.build_filter(w)
            for forward in (True, False):
                full = ipv4_tcp() if forward else ipv4_tcp(PEER_IP, LOCAL_IP, 22, 40123, 0x04)
                packet_type = observer.PACKET_OUTGOING if forward == (role == "candidate") else observer.PACKET_HOST
                for depth in (0, 1, 2):
                    for length in (34, 36, 38, 40, 46, 49, 50, 53):
                        frame = full[:length]
                        for _ in range(depth): frame = tag(frame)
                        snap = run_cbpf(program, frame)
                        self.assertEqual(snap, 34 + depth * 4, (role, forward, depth, length))
                        self.assertEqual(observer.classify_header(w, frame[:snap], packet_type), "unsupported_header")
                    frame = full
                    for _ in range(depth): frame = tag(frame)
                    self.assertEqual(run_cbpf(program, frame), 54 + depth * 4)

    def test_review_I1_kernel_direction_and_missing_address_fail_closed(self):
        for role in ("candidate", "control"):
            w = self.parsed(role=role)
            local_ip, peer_ip = (LOCAL_IP, PEER_IP) if role == "candidate" else (PEER_IP, LOCAL_IP)
            local_mac, peer_mac = (LOCAL_MAC, PEER_MAC) if role == "candidate" else (PEER_MAC, LOCAL_MAC)
            local_tcp = ipv4_tcp() if role == "candidate" else ipv4_tcp(PEER_IP, LOCAL_IP, 22, 40123)
            peer_tcp = ipv4_tcp(PEER_IP, LOCAL_IP, 22, 40123) if role == "candidate" else ipv4_tcp()
            for frame in (arp(local_ip, peer_ip, sender_mac=local_mac), local_tcp,
                          local_tcp[:47] + b"\x04" + local_tcp[48:]):
                self.assertEqual(observer.classify_header(w, frame, observer.PACKET_HOST), "packet_direction_invalid")
            for frame in (arp(peer_ip, local_ip, sender_mac=peer_mac), peer_tcp,
                          peer_tcp[:47] + b"\x04" + peer_tcp[48:],
                          ipv4_icmp_unreachable(outer_dst=local_ip)):
                self.assertEqual(observer.classify_header(w, frame, observer.PACKET_OUTGOING), "packet_direction_invalid")
            for packet_type in (None, True, 2, 3, 99, "4"):
                self.assertEqual(observer.classify_header(w, local_tcp, packet_type), "packet_direction_invalid")
        valid = ("enp0s1", observer.ETH_P_IP, observer.PACKET_OUTGOING, 1, bytes.fromhex(LOCAL_MAC.replace(":", "")))
        for address in (None, (), ("enp0s1", 0, observer.PACKET_OUTGOING),
                        ("other0", *valid[1:]), (*valid[:2], True, *valid[3:]),
                        (*valid[:3], 0, valid[4]), (*valid[:4], "raw-mac")):
            fake = FakeSocket([], [(ipv4_tcp(), [], 0, address)], stats_packets=1)
            result = self.run_fake(fake, times=[0.0, 0.0, 0.0, 0.0, 1.0, 1.0])
            self.assertFalse(result["complete"])
            self.assertIn("packet_metadata_invalid", result["incomplete_reasons"])
            self.assertEqual(sum(result["counters"].values()), 0)

    def test_review_I1_offload_protocol_disagreement_is_unsupported_coverage(self):
        address = ("enp0s1", 0x8100, observer.PACKET_OUTGOING, 1, ipv4_tcp()[6:12])
        fake = FakeSocket([], [(ipv4_tcp(), [], 0, address)], stats_packets=1)
        result = self.run_fake(fake, times=[0.0, 0.0, 0.0, 0.0, 1.0, 1.0])
        self.assertFalse(result["complete"])
        self.assertIn("unsupported_header", result["incomplete_reasons"])
        self.assertFalse(result["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"])
        self.assertEqual(sum(result["counters"].values()), 0)

    def test_observer_owns_fixed_watch_snapshot_after_readiness(self):
        w = self.parsed()
        original = observer._binding(w)
        ticks = iter([0.0, 0.0, 1.1, 1.1])
        def ready(_record):
            w["candidate"]["address"] = "192.168.64.99"
            w["duration_ms"] = 30000
            w["role"] = "control"
        result = observer._observe(w, socket_factory=lambda *_a: FakeSocket([]),
            attach_filter=lambda *_a: None, runner=fake_runner,
            clock=lambda: next(ticks), on_ready=ready)
        self.assertEqual(result["binding"], original)
        self.assertEqual(result["duration_ms"], 1000)
        self.assertEqual(result["role"], "candidate")

    @unittest.skipUnless(sys.platform == "linux", "requires Linux kernel cBPF on unnamed AF_UNIX datagrams")
    def test_linux_kernel_verifies_and_executes_filter_without_network_capture(self):
        # Only unnamed local datagrams are used. This opens no AF_PACKET socket,
        # binds no network address and sends no packet on a network interface.
        for role in ("candidate", "control"):
            program = observer.build_filter(self.parsed(role=role))
            sender, receiver = socket.socketpair(socket.AF_UNIX, socket.SOCK_DGRAM)
            try:
                sender.settimeout(0.25); receiver.settimeout(0.025)
                observer._attach_filter(receiver, program)
                for packet in (arp(), ipv4_tcp(), ipv4_icmp_unreachable(outer_dst=LOCAL_IP if role == "candidate" else PEER_IP)):
                    payload = packet + b"PAYLOAD-CANARY"
                    expected = run_cbpf(program, payload)
                    sender.send(payload)
                    self.assertEqual(receiver.recv(4096), payload[:expected])
                for full in (ipv4_tcp(), ipv4_tcp(PEER_IP, LOCAL_IP, 22, 40123, 0x04)):
                    for depth in (0, 1, 2):
                        for length in (34, 36, 38, 40, 46, 49, 50, 53):
                            packet = full[:length]
                            for _ in range(depth):
                                packet = packet[:12] + b"\x81\x00\x00\x01" + packet[12:]
                            sender.send(packet)
                            self.assertEqual(receiver.recv(4096), packet[:34 + depth * 4])
                for packet in (arp("192.168.64.99", PEER_IP), ipv4_tcp(LOCAL_IP, "192.168.64.99"), ipv4_tcp(LOCAL_IP, PEER_IP, 22, 443)):
                    sender.send(packet)
                    with self.assertRaises(socket.timeout):
                        receiver.recv(4096)
            finally:
                sender.close(); receiver.close()

    def test_interpreter_load_widths_match_linux_uapi_encodings(self):
        packet = b"\x01\x23\x45\x67"
        for opcode, expected in ((0x20, 0x01234567), (0x28, 0x0123), (0x30, 0x01), (0x40, 0x01234567), (0x48, 0x0123), (0x50, 0x01)):
            self.assertEqual(run_cbpf([(opcode, 0, 0, 0), (0x16, 0, 0, 0)], packet), expected)

    def parsed(self, **changes):
        return observer.parse_watch(json.dumps(watch(**changes)).encode())

    def run_fake(self, fake=None, *, times=None, **kwargs):
        fake = fake or FakeSocket([])
        ticks = iter(times or [0.0, 0.0, 1.1, 1.1])
        return observer._observe(self.parsed(), socket_factory=lambda *_a: fake,
            attach_filter=lambda *_a: None, runner=fake_runner,
            clock=lambda: next(ticks), on_ready=lambda _: None, **kwargs)

    def test_bad_watch_types_identity_collisions_and_json_are_rejected(self):
        cases = [b"", b"null", b"[]", b"{", b"\xff", b'{"x":NaN}']
        for field, value in (("version", True), ("duration_ms", 0), ("duration_ms", 1.5),
                             ("interface", "-eth0"), ("interface", "..")):
            cases.append(json.dumps(watch(**{field: value})).encode())
        for field in ("session_uuid", "generation_uuid", "backend", "address", "mac"):
            v = watch()
            v["control"][field] = v["candidate"][field]
            cases.append(json.dumps(v).encode())
        cases.append(json.dumps(watch(gateway=LOCAL_IP)).encode())
        v = watch(); v["candidate"]["extra"] = 1
        cases.append(json.dumps(v).encode())
        v = watch(); v["candidate"]["session_uuid"] = "malformed"
        cases.append(json.dumps(v).encode())
        for value in cases:
            with self.subTest(raw=value), self.assertRaises(observer.WatchError):
                observer.parse_watch(value)

    def test_overflow_clock_and_kernel_statistics_fail_closed(self):
        packet = (ipv4_tcp(), [], 0, ("enp0s1", observer.ETH_P_IP, observer.PACKET_OUTGOING, 1, bytes.fromhex(LOCAL_MAC.replace(":", ""))))
        fake = FakeSocket([], [packet] * (observer.MAX_EVENTS + 1), stats_packets=observer.MAX_EVENTS + 1)
        ticks = [0.0] * (2 * observer.MAX_EVENTS + 5) + [1.1]
        result = self.run_fake(fake, times=ticks)
        self.assertTrue(result["overflow"])
        self.assertFalse(result["complete"])
        self.assertEqual(result["observed_packets"], observer.MAX_EVENTS)
        self.assertEqual(result["counters"]["tcp22_syn_out"], observer.MAX_EVENTS)
        self.assertIn("counter_overflow", result["incomplete_reasons"])
        for ticks in ([1.0, 0.9], [0.0, float("nan")], [True], [0.0, 70.0]):
            with self.subTest(ticks=ticks):
                result = self.run_fake(times=ticks)
                self.assertFalse(result["complete"])
                self.assertIn("observer_clock_error", result["incomplete_reasons"])
                self.assertLessEqual(result["elapsed_ms"], 60000)
        class NoStats(FakeSocket):
            def getsockopt(self, *_args):
                raise OSError("PRIVATE-ERROR-CANARY")
        result = self.run_fake(NoStats([]))
        self.assertFalse(result["complete"])
        self.assertIn("statistics_unavailable", result["incomplete_reasons"])
        self.assertNotIn("PRIVATE-ERROR-CANARY", json.dumps(result))

    def test_real_packet_accounting_and_metadata_only_receipt(self):
        frame = ipv4_tcp() + b"PAYLOAD-CANARY"
        filtered = frame[:run_cbpf(observer.build_filter(self.parsed()), frame)]
        fake = FakeSocket([], [(filtered, [], 0, ("enp0s1", observer.ETH_P_IP, observer.PACKET_OUTGOING, 1, bytes.fromhex(LOCAL_MAC.replace(":", ""))))], stats_packets=1)
        result = self.run_fake(fake, times=[0.0, 0.0, 0.0, 0.0, 1.1, 1.1])
        self.assertTrue(result["complete"])
        self.assertEqual(result["counters"]["tcp22_syn_out"], 1)
        self.assertFalse(result["negative_inference"]["candidate_pre_emission_absence_if_driver_controls_pass"])
        self.assertEqual(result["coverage_scope"], "identified_pair_headers")
        self.assertFalse(result["zero_count_attribution"])
        self.assertNotIn("PAYLOAD-CANARY", json.dumps(result))

    def test_fragments_truncated_pair_and_bad_tcp_headers_are_incomplete(self):
        w = self.parsed(); program = observer.build_filter(w)
        fragment = bytearray(ipv4_tcp())
        fragment[20:22] = b"\x20\x00"
        for frame in (bytes(fragment), ipv4_tcp()[:36], ipv4_tcp(options=b"\x01" * 40)):
            snap = run_cbpf(program, frame)
            self.assertGreater(snap, 0)
            self.assertEqual(observer.classify_header(w, frame[:snap], observer.PACKET_OUTGOING), "unsupported_header")
        malformed = bytearray(ipv4_tcp())
        malformed[46] = 0x30  # TCP header says 12 bytes, subsequent bytes are not headers
        malformed[48:] = b"PAYLOAD-CANARY"
        snap = run_cbpf(program, malformed)
        self.assertLessEqual(snap, 38)
        self.assertNotIn(b"PAYLOAD-CANARY", malformed[:snap])
        self.assertEqual(observer.classify_header(w, malformed[:snap], observer.PACKET_OUTGOING), "unsupported_header")

    def test_ip_output_duplicate_wrong_dst_src_only_and_hostile_fields(self):
        class Result:
            returncode = 0
            def __init__(self, stdout): self.stdout = stdout
        for raw in (b'[{"dst":"192.168.64.2","dst":"192.168.64.99","dev":"enp0s1","prefsrc":"192.168.64.3"}]',
                    b'[{"dst":"192.168.64.99","dev":"enp0s1","prefsrc":"192.168.64.3"}]',
                    b'[{"dst":"192.168.64.2","dev":"enp0s1","src":"192.168.64.3"}]',
                    b'x' * (observer.MAX_COMMAND_OUTPUT_BYTES + 1)):
            result = observer._query_network_state(self.parsed(), runner=lambda argv, **_: Result(raw if argv[2] == "route" else b"[]"))
            self.assertEqual(result["route"]["status"], "invalid")
        raw = b'[{"dst":"192.168.64.2","dev":"enp0s1","prefsrc":"192.168.64.3","error":"HOSTILE-TEXT","metric":true}]'
        result = observer._query_network_state(self.parsed(), runner=lambda argv, **_: Result(raw))
        self.assertNotIn("HOSTILE-TEXT", json.dumps(result))
        self.assertEqual(result["route"]["status"], "invalid")

    def test_bounded_helper_terminates_on_hostile_output_or_inherited_pipe(self):
        class Stream:
            closed = False
            def fileno(self): return 123
            def close(self): self.closed = True
        class Process:
            def __init__(self):
                self.stdout, self.stderr = Stream(), Stream()
                self.killed = 0; self.waits = []
            def kill(self): self.killed += 1
            def wait(self, timeout=None):
                self.waits.append(timeout)
                if timeout is None: raise AssertionError("unbounded child wait")
                return 0
        class Selector:
            def __init__(self): self.entries = {}; self.closed = False
            def register(self, stream, _event, name): self.entries[stream] = name
            def get_map(self): return self.entries
            def select(self, timeout):
                stream, name = next(iter(self.entries.items()))
                return [(type("Key", (), {"fd": 123, "data": name, "fileobj": stream})(), 1)]
            def unregister(self, stream): self.entries.pop(stream)
            def close(self): self.closed = True
        for mode, output in (("overflow", b"X" * 1024), ("deadline", b"inherited-open-pipe"), ("eof", b"")):
            process, selector = Process(), Selector()
            ticks = iter([0.0, 0.0, 3.0])
            current = (lambda: next(ticks)) if mode == "deadline" else (lambda: 0.0)
            with mock.patch.object(observer.subprocess, "Popen", return_value=process) as popen, \
                 mock.patch.object(observer.selectors, "DefaultSelector", return_value=selector), \
                 mock.patch.object(observer.os, "set_blocking"), \
                 mock.patch.object(observer.os, "read", return_value=output), \
                 mock.patch.object(observer.time, "monotonic", side_effect=current):
                result = observer._bounded_ip_command(["/usr/sbin/ip", "-j", "route", "get", PEER_IP])
            self.assertTrue(process.stdout.closed and process.stderr.closed and selector.closed)
            self.assertTrue(all(timeout is not None for timeout in process.waits))
            self.assertEqual(popen.call_args.kwargs["env"], observer.IP_ENV)
            self.assertLessEqual(len(result.stdout), observer.MAX_COMMAND_OUTPUT_BYTES)
            self.assertEqual(result.returncode, 0 if mode == "eof" else 1)
            self.assertEqual(process.killed, 0 if mode == "eof" else 1)



class _CommandResult:
    returncode = 0
    stderr = b""

    def __init__(self, argv):
        if argv[2] == "route":
            self.stdout = b'[{"dst":"192.168.64.2","dev":"enp0s1","prefsrc":"192.168.64.3","gateway":"192.168.64.1","metric":100}]'
        else:
            self.stdout = b'[{"dst":"192.168.64.2","dev":"enp0s1","lladdr":"02:00:00:00:00:02","state":["REACHABLE"]}]'


def fake_runner(argv, **_kwargs):
    return _CommandResult(argv)


if __name__ == "__main__":
    unittest.main()
