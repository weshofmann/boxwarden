"""Execute installer service-limit writes against a disposable target tree."""
import configparser
import pathlib
import re
import subprocess
import tempfile
import unittest

SEED = pathlib.Path(__file__).resolve().parents[1] / "autoinstall/user-data"
UNITS = ("snapd.service", "udisks2.service")


def installer_body():
    match = re.search(r"      sh -s -- /target <<'BW_SERVICE_WAIT_SH'\n(.*?)      BW_SERVICE_WAIT_SH\n", SEED.read_text(), re.S)
    return "" if match is None else "\n".join(line[6:] for line in match.group(1).splitlines())


class ServiceStartWaitTests(unittest.TestCase):
    def apply(self, root):
        return subprocess.run(["/bin/sh", "-s", "--", str(root)], input=installer_body().encode(), capture_output=True, timeout=5)

    def paths(self, root):
        return [root / "etc/systemd/system" / (unit + ".d") / "10-boxwarden-start-timeout.conf" for unit in UNITS]

    def test_installer_writes_finite_start_limits_for_exact_two_services(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self.assertEqual(self.apply(root).returncode, 0)
            paths = self.paths(root)
            self.assertTrue(all(path.is_file() for path in paths), "installer must create both service dropins")
            for path in paths:
                cfg = configparser.ConfigParser()
                cfg.read(path)
                self.assertEqual(cfg.sections(), ["Service"])
                self.assertEqual(dict(cfg["Service"]), {"timeoutstartsec": "10min"})
                self.assertEqual(path.stat().st_mode & 0o777, 0o644)
                self.assertEqual(path.parent.stat().st_mode & 0o777, 0o755)
            self.assertEqual(set(root.rglob("*.conf")), set(paths))

    def test_repeated_application_retains_the_same_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            self.assertEqual(self.apply(root).returncode, 0)
            self.assertTrue(all(path.is_file() for path in self.paths(root)), "service dropins required")
            before = {path: path.read_bytes() for path in self.paths(root)}
            self.assertEqual(self.apply(root).returncode, 0)
            self.assertEqual({path: path.read_bytes() for path in self.paths(root)}, before)

    def test_other_units_and_global_limits_are_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            paths = {root / "etc/systemd/system.conf": b"[Manager]\nDefaultTimeoutStartSec=90s\n", root / "etc/systemd/system/snapd.seeded.service.d/operator.conf": b"[Service]\nTimeoutStartSec=1min\n"}
            for path, data in paths.items():
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
            self.assertEqual(self.apply(root).returncode, 0)
            self.assertTrue(all(path.is_file() for path in self.paths(root)), "service dropins required")
            self.assertEqual({path: path.read_bytes() for path in paths}, paths)


if __name__ == "__main__":
    unittest.main()
