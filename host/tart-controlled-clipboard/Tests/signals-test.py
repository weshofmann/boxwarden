"""Actual parent signals, real owned resistant child, no VM or clipboard access."""
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time


def alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False


for termination in [signal.SIGTERM, signal.SIGINT, signal.SIGHUP]:
    with tempfile.TemporaryDirectory(prefix='bw-signal-') as temporary:
        root = Path(temporary)
        marker = root / 'child-pid'
        child = root / 'child'
        # Temporary paths are generated locally and contain no quote characters.
        child.write_text(f"#!/bin/sh\ntrap '' TERM\nprintf '%s' $$ > '{marker}'\nexec /bin/sleep 60\n")
        child.chmod(0o700)
        parent = subprocess.Popen([sys.argv[1], str(child)], stdin=subprocess.DEVNULL,
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        child_pid = None
        try:
            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                if marker.exists() and marker.read_text():
                    child_pid = int(marker.read_text())
                    break
                if parent.poll() is not None:
                    raise AssertionError('synthetic parent exited before child readiness')
                time.sleep(.01)
            assert child_pid is not None, 'synthetic child did not become ready'
            parent.send_signal(termination)
            result = parent.wait(timeout=4)
            assert result == 0 and not alive(child_pid), (
                f'normal parent {termination.name} bypassed drain: exit={result}, '
                f'owned TERM-resistant child alive={alive(child_pid)}')
        finally:
            if parent.poll() is None:
                parent.kill()
                parent.wait(timeout=2)
            if child_pid is not None and alive(child_pid):
                os.kill(child_pid, signal.SIGKILL)
print('PASS: actual parent TERM/INT/HUP close admission and reap resistant own child before exit')
