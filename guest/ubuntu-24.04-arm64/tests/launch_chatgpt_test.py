import importlib.util
import os
import stat
import tempfile
import unittest
from pathlib import Path
from unittest import mock


HELPER = Path(__file__).resolve().parents[1] / "launch-chatgpt.py"
SPEC = importlib.util.spec_from_file_location("boxwarden_chatgpt_launcher", HELPER)
launcher = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(launcher)


class ChatGPTLaunchTests(unittest.TestCase):
    def setUp(self):
        self.private = tempfile.TemporaryDirectory()
        self.addCleanup(self.private.cleanup)
        self.runtime_root = Path(self.private.name)
        self.uid = os.getuid()
        self.runtime = self.runtime_root / str(self.uid)
        self.runtime.mkdir(mode=0o700)
        self.bus_path = self.runtime / "bus"

    def bus_patch(self):
        original = Path.lstat

        def lstat(path):
            if path == self.bus_path:
                return mock.Mock(st_mode=stat.S_IFSOCK | 0o600, st_uid=self.uid)
            return original(path)

        return mock.patch.object(Path, "lstat", lstat)

    def test_missing_session_bus_prevents_launch(self):
        with mock.patch.object(launcher, "RUNTIME_ROOT", self.runtime_root), \
             mock.patch.object(launcher.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "bus"):
                launcher.launch()
        run.assert_not_called()

    def test_inactive_graphical_session_prevents_launch(self):
        with self.bus_patch(), mock.patch.object(launcher, "RUNTIME_ROOT", self.runtime_root), \
             mock.patch.object(launcher.subprocess, "run", return_value=mock.Mock(returncode=3, stdout="")) as run:
            with self.assertRaisesRegex(RuntimeError, "graphical session"):
                launcher.launch()
        self.assertEqual(run.call_count, 1)

    def test_active_desktop_starts_exact_user_unit(self):
        calls = []

        def run(argv, **kwargs):
            calls.append((argv, kwargs))
            if argv[1:4] == ["--user", "is-active", "--quiet"]:
                return mock.Mock(returncode=3 if argv[-1] == "boxwarden-chatgpt.service" else 0,
                                 stdout="")
            if argv[1:3] == ["--user", "show-environment"]:
                return mock.Mock(returncode=0, stdout="DISPLAY=:0\nXAUTHORITY=/run/user/test\n")
            if argv[0] == "/usr/bin/systemd-run":
                return mock.Mock(returncode=0, stdout="")
            self.fail(f"unexpected command: {argv}")

        with self.bus_patch(), mock.patch.object(launcher, "RUNTIME_ROOT", self.runtime_root), \
             mock.patch.object(launcher.subprocess, "run", side_effect=run):
            launcher.launch()
        self.assertEqual(calls[-1][0], [
            "/usr/bin/systemd-run", "--user", "--collect", "--service-type=exec",
            "--unit=boxwarden-chatgpt", "--property=PartOf=graphical-session.target",
            "/usr/bin/chatgpt",
        ])
        self.assertEqual(calls[-1][1]["env"]["XDG_RUNTIME_DIR"], str(self.runtime))
        self.assertEqual(calls[-1][1]["env"]["DBUS_SESSION_BUS_ADDRESS"],
                         "unix:path=" + str(self.runtime / "bus"))

    def test_missing_display_prevents_launch(self):
        calls = []

        def run(argv, **_):
            calls.append(argv)
            if argv[2] == "show-environment":
                return mock.Mock(returncode=0, stdout="LANG=C.UTF-8\n")
            return mock.Mock(returncode=0, stdout="")

        with self.bus_patch(), mock.patch.object(launcher, "RUNTIME_ROOT", self.runtime_root), \
             mock.patch.object(launcher.subprocess, "run", side_effect=run):
            with self.assertRaisesRegex(RuntimeError, "display"):
                launcher.launch()
        self.assertEqual(len(calls), 2)

    def test_existing_active_unit_is_not_started_twice(self):
        calls = []

        def run(argv, **_):
            calls.append(argv)
            if argv[2] == "show-environment":
                return mock.Mock(returncode=0, stdout="DISPLAY=:0\n")
            return mock.Mock(returncode=0, stdout="")

        with self.bus_patch(), mock.patch.object(launcher, "RUNTIME_ROOT", self.runtime_root), \
             mock.patch.object(launcher.subprocess, "run", side_effect=run):
            launcher.launch()
        self.assertEqual(len(calls), 3)


if __name__ == "__main__":
    unittest.main()
