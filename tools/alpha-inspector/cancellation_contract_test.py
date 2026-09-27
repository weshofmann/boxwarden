#!/usr/bin/env python3
"""Source gate for cancellation ordering; real VZ shutdown needs qualification."""
from pathlib import Path
import unittest

BOOT = Path(__file__).with_name("boot.swift").read_text()

class CancellationContractTest(unittest.TestCase):
    def test_handler_installed_before_vm_start_and_only_wakes(self):
        self.assertIn("let cancellation = BootCancellation", BOOT)
        self.assertIn("source.setEventHandler {", BOOT)
        self.assertLess(BOOT.index("let cancellation = BootCancellation"), BOOT.index("vm.start"))
        handler = BOOT.split("source.setEventHandler {")[1].split("source.resume()")[0]
        self.assertNotIn("vm.", handler)
        self.assertIn("self.cancelled = true", handler)
        self.assertIn("wake.signal()", handler)

    def test_receipt_follows_ordered_stop_and_pipe_completion(self):
        lifecycle = BOOT.split("func bootProof")[1]
        self.assertIn("var wasCancelled", lifecycle)
        self.assertLess(lifecycle.index("started.wait"), lifecycle.index("var wasCancelled"))
        self.assertLess(lifecycle.index("requestAndForceStop"), lifecycle.index("consolePump.done.wait"))
        self.assertLess(lifecycle.index("exportPump.done.wait"), lifecycle.index('"BOOT_CANCELLED "'))
        self.assertIn("if wasCancelled { throw BootFailure.cancelled }", lifecycle)

if __name__ == "__main__":
    unittest.main()
