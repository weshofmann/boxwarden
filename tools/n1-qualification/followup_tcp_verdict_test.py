"""Deterministic false-pass regressions for the one N1 cross-guest TCP interval."""

import json
from pathlib import Path
import subprocess
import sys
import unittest


SCRIPT = Path(__file__).with_name("followup_tcp_verdict.py")
CONTROL_ID = "11111111-1111-4111-8111-111111111111"
CONTROL_GENERATION = "22222222-2222-4222-8222-222222222222"
CANDIDATE_ID = "33333333-3333-4333-8333-333333333333"
CANDIDATE_GENERATION = "44444444-4444-4444-8444-444444444444"


def binding(session_id, generation, address):
    return {
        "domain": "n1qualification",
        "session_id": session_id,
        "backend_object": "boxwarden-n1qualification-" + session_id.replace("-", ""),
        "generation": generation,
        "address": address,
    }


def valid_receipt():
    control = binding(CONTROL_ID, CONTROL_GENERATION, "192.168.2.83")
    candidate = binding(CANDIDATE_ID, CANDIDATE_GENERATION, "192.168.2.84")
    return {
        "version": 1,
        "control": control,
        "candidate": candidate,
        "endpoint": {"address": control["address"], "port": 22},
        "host_before": {**control, "port": 22, "ssh_exit": 0, "identity_ok": True},
        "candidate_probe": {
            **candidate,
            "ssh_exit": 0,
            "target_address": control["address"],
            "target_port": 22,
            "connect": "timeout",
        },
        "host_after": {**control, "port": 22, "ssh_exit": 0, "identity_ok": True},
        "candidate_health": {
            **candidate,
            "ready": True,
            "management": True,
            "gateway_dns": True,
            "public_https": True,
        },
    }


def verdict(receipt):
    completed = subprocess.run(
        [sys.executable, str(SCRIPT)],
        input=json.dumps(receipt),
        text=True,
        capture_output=True,
        timeout=3,
    )
    assert completed.stdout, completed.stderr
    return completed.returncode, json.loads(completed.stdout)


class FollowupTCPVerdictTests(unittest.TestCase):
    def test_only_timeout_with_both_host_positives_and_candidate_health_passes(self):
        # Catches a verdict that treats a candidate timeout alone as evidence.
        code, output = verdict(valid_receipt())
        self.assertEqual((code, output["verdict"]), (0, "PASS"))
        self.assertEqual(output["scope"], "one cross-guest TCP endpoint; no UDP, VPN or IPv6 claim")

    def test_completed_connect_fails_even_without_banner(self):
        # Catches an implementation that waits for a banner before failing.
        record = valid_receipt()
        record["candidate_probe"]["connect"] = "connected"
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (1, "FAIL"))

    def test_missing_host_positive_invalidates_interval(self):
        # Catches accepting candidate timeout when the owned endpoint was unproven.
        for phase in ("host_before", "host_after"):
            with self.subTest(phase=phase):
                record = valid_receipt()
                record[phase]["ssh_exit"] = 255
                code, output = verdict(record)
                self.assertEqual((code, output["verdict"]), (2, "INVALID"))

    def test_connection_refused_and_other_errors_are_not_denial_passes(self):
        # The archived probe's `not ok and no payload` assertion missed this.
        record = valid_receipt()
        record["candidate_probe"]["connect"] = "error"
        record["candidate_probe"]["error"] = "ConnectionRefusedError: refused"
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (3, "UNQUALIFIED"))

    def test_stale_positive_generation_invalidates_interval(self):
        # Catches a host positive from an earlier, no longer bound generation.
        record = valid_receipt()
        record["host_after"]["generation"] = CANDIDATE_GENERATION
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (2, "INVALID"))

    def test_public_control_target_is_outside_cross_guest_topology(self):
        # Catches accepting a public host as the supposed control guest.
        record = valid_receipt()
        record["control"]["address"] = "8.8.8.8"
        record["endpoint"]["address"] = "8.8.8.8"
        record["host_before"]["address"] = "8.8.8.8"
        record["host_after"]["address"] = "8.8.8.8"
        record["candidate_probe"]["target_address"] = "8.8.8.8"
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (2, "INVALID"))

    def test_failed_candidate_health_cannot_pass(self):
        # A timeout from an unhealthy candidate does not prove isolation.
        record = valid_receipt()
        record["candidate_health"]["public_https"] = False
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (3, "UNQUALIFIED"))

    def test_candidate_management_failure_cannot_pass(self):
        # A failed host-to-candidate SSH command cannot be read as a timeout.
        record = valid_receipt()
        record["candidate_probe"]["ssh_exit"] = 255
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (3, "UNQUALIFIED"))

    def test_unbound_candidate_health_cannot_pass(self):
        # Catches reusing anonymous all-true health from a different interval.
        record = valid_receipt()
        for key in binding(CANDIDATE_ID, CANDIDATE_GENERATION, "192.168.2.84"):
            del record["candidate_health"][key]
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (2, "INVALID"))

    def test_stale_candidate_health_generation_cannot_pass(self):
        # Catches health from the previous candidate generation after restart.
        record = valid_receipt()
        record["candidate_health"]["generation"] = CONTROL_GENERATION
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (2, "INVALID"))
        self.assertIn("bound", output["reason"])

    def test_completed_connect_still_fails_if_ssh_exits_nonzero_afterward(self):
        # A post-connect transport failure cannot erase an observed TCP breach.
        record = valid_receipt()
        record["candidate_probe"]["connect"] = "connected"
        record["candidate_probe"]["ssh_exit"] = 255
        code, output = verdict(record)
        self.assertEqual((code, output["verdict"]), (1, "FAIL"))


if __name__ == "__main__":
    unittest.main()
