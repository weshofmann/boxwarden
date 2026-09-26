#!/usr/bin/env python3
"""Check the VZVirtualMachine serial-queue contract in the alpha helpers.

Virtualization.framework does not enforce this at Swift compile time, and a
real VM run is an attended qualification step. This check catches accidental
direct VM access on the caller thread before that step.
"""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
SOURCES = (
    ROOT / "tools/alpha-inspector/boot.swift",
    ROOT / "tools/alpha-formatter/boot.swift",
)
TOKEN = re.compile(
    r"queue\.(?:sync|async)(?:\(execute:\s*)?\s*\{|"
    r"\bVZVirtualMachine\s*\(|\bvm\.[A-Za-z_]\w*|"
    r"\b[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)?\.wait\s*\(|[{}]"
)


class VMQueueContractTest(unittest.TestCase):
    def test_vm_operations_run_on_serial_queue(self):
        for source in SOURCES:
            stack = []
            code = source.read_text()
            checked = 0
            for match in TOKEN.finditer(code):
                token = match.group()
                if token.startswith("queue."):
                    stack.append(True)
                elif token == "{":
                    stack.append(stack[-1] if stack else False)
                elif token == "}":
                    stack.pop()
                else:
                    line = code.count("\n", 0, match.start()) + 1
                    with self.subTest(source=source.relative_to(ROOT), line=line, operation=token):
                        if ".wait" in token:
                            self.assertFalse(stack and stack[-1], "semaphore wait blocks VM callbacks")
                        else:
                            checked += 1
                            self.assertTrue(stack and stack[-1], "VM access is outside its serial queue")
            self.assertFalse(stack, f"unbalanced braces in {source}")
            self.assertGreater(checked, 0, f"no VM accesses checked in {source}")


if __name__ == "__main__":
    unittest.main()
