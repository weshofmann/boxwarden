#!/usr/bin/env python3
"""Behavioral tests for the fixed guest-only preparation helper."""

import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest


HELPER = Path(__file__).resolve().parents[1] / "recipe-prepare.py"


def load_helper():
    spec = importlib.util.spec_from_file_location("boxwarden_recipe_prepare", HELPER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class RecipePrepareTests(unittest.TestCase):
    def test_bounded_payload_keeps_order_and_exact_argv(self):
        helper = load_helper()
        payload = helper.parse_payload(json.dumps({
            "version": 1,
            "preparation_key": "a" * 64,
            "apt_packages": ["git", "nodejs"],
            "steps": [{"id": "sample", "argv": ["/bin/echo", "one word", "; touch /tmp/should-not-run"]}],
        }).encode())
        self.assertEqual(helper.command_plan(payload), [
            ("apt-update", ["/usr/bin/apt-get", "update"]),
            ("apt-install", ["/usr/bin/apt-get", "install", "-y", "--no-remove", "--no-install-recommends", "git", "nodejs"]),
            ("sample", ["/bin/echo", "one word", "; touch /tmp/should-not-run"]),
        ])

    def test_duplicate_or_extra_keys_and_shell_package_are_rejected(self):
        helper = load_helper()
        for raw in (
            b'{"version":1,"version":1,"preparation_key":"' + b"a" * 64 + b'","apt_packages":[],"steps":[]}',
            b'{"version":1,"preparation_key":"' + b"a" * 64 + b'","apt_packages":[],"steps":[],"host_command":"true"}',
            b'{"version":1,"preparation_key":"' + b"a" * 64 + b'","apt_packages":["git;reboot"],"steps":[]}',
        ):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                helper.parse_payload(raw)

    def test_guest_step_executes_literal_argv_with_closed_environment(self):
        helper = load_helper()
        with tempfile.TemporaryDirectory() as root:
            report = Path(root) / "report.json"
            child = Path(root) / "child.py"
            child.write_text("import json, os, sys\nfrom pathlib import Path\nPath(sys.argv[1]).write_text(json.dumps({'argv':sys.argv[2:], 'host_var':os.environ.get('BOXWARDEN_TEST_HOST_SECRET')}))\n")
            payload = helper.parse_payload(json.dumps({
                "version": 1,
                "preparation_key": "b" * 64,
                "apt_packages": [],
                "steps": [{"id": "literal-argv", "argv": [sys.executable, str(child), str(report), "one word", "$(touch /tmp/should-not-run)"]}],
            }).encode())
            previous = os.environ.get("BOXWARDEN_TEST_HOST_SECRET")
            os.environ["BOXWARDEN_TEST_HOST_SECRET"] = "must-not-pass"
            try:
                helper.execute_payload(payload)
            finally:
                if previous is None:
                    del os.environ["BOXWARDEN_TEST_HOST_SECRET"]
                else:
                    os.environ["BOXWARDEN_TEST_HOST_SECRET"] = previous
            result = json.loads(report.read_text())
            self.assertEqual(result, {"argv": ["one word", "$(touch /tmp/should-not-run)"], "host_var": None})

    def test_nonzero_step_stops_later_commands(self):
        helper = load_helper()
        with tempfile.TemporaryDirectory() as root:
            marker = Path(root) / "later"
            payload = helper.parse_payload(json.dumps({
                "version": 1,
                "preparation_key": "c" * 64,
                "apt_packages": [],
                "steps": [
                    {"id": "fails", "argv": [sys.executable, "-c", "raise SystemExit(7)"]},
                    {"id": "later", "argv": [sys.executable, "-c", "from pathlib import Path; Path(__import__('sys').argv[1]).touch()", str(marker)]},
                ],
            }).encode())
            with self.assertRaisesRegex(RuntimeError, "fails"):
                helper.execute_payload(payload)
            self.assertFalse(marker.exists())


if __name__ == "__main__":
    unittest.main()
