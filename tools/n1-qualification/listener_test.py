"""Host-only synthetic fixture tests; these do not qualify guest containment."""
import json
from pathlib import Path
import socket
import select
import subprocess
import sys
import unittest

SCRIPT = Path(__file__).with_name("listener.py")
RESPONSE = b"boxwarden-n1-owned-fixture\n"

class ListenerTests(unittest.TestCase):
    def test_owned_tcp_udp_positive_control_and_bounded_exit(self):
        proc = subprocess.Popen([sys.executable, str(SCRIPT), "--bind", "127.0.0.1", "--duration", "1"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        def cleanup():
            if proc.poll() is None: proc.kill()
            proc.communicate()
        self.addCleanup(cleanup)
        self.assertTrue(select.select([proc.stdout], [], [], 3)[0], "fixture did not announce readiness")
        ready = json.loads(proc.stdout.readline())
        self.assertEqual(ready["bind"], "127.0.0.1")
        self.assertGreater(ready["port"], 1023)
        endpoint = (ready["bind"], ready["port"])
        with socket.create_connection(endpoint, timeout=1) as conn:
            with conn.makefile("rb") as response:
                self.assertEqual(response.read(len(RESPONSE)), RESPONSE)
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as conn:
            conn.settimeout(1)
            conn.sendto(b"different synthetic request", endpoint)
            self.assertEqual(conn.recv(128), RESPONSE)
        output, err = proc.communicate(timeout=4)
        self.assertEqual(proc.returncode, 0, err)
        observed = json.loads(output)
        self.assertEqual(observed, {"tcp_responses": 1, "udp_responses": 1, "closed": True})
        with self.assertRaises(OSError):
            socket.create_connection(endpoint, timeout=.2)

    def test_rejects_wildcard_multicast_and_unbounded_lifetime(self):
        for args in [["--bind", "0.0.0.0"], ["--bind", "224.0.0.251"], ["--bind", "127.0.0.1", "--duration", "121"], ["--bind", "127.0.0.1", "--duration", "0"], ["--bind", "127.0.0.1", "--port", "53"]]:
            with self.subTest(args=args):
                p = subprocess.run([sys.executable,str(SCRIPT)] + args, capture_output=True, text=True, timeout=3)
                self.assertNotEqual(p.returncode, 0)
                self.assertNotIn('"port":',p.stdout)

if __name__ == "__main__": unittest.main()
