import hashlib
import importlib.util
import io
import tempfile
import unittest
from pathlib import Path
from unittest import mock


HELPER = Path(__file__).resolve().parents[1] / "install-pinned-chatgpt.py"
SPEC = importlib.util.spec_from_file_location("boxwarden_chatgpt_installer", HELPER)
installer = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(installer)


class FakeResponse(io.BytesIO):
    def __init__(self, data):
        super().__init__(data)
        self.url = installer.PACKAGE_URL

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()


class PinnedChatGPTInstallTests(unittest.TestCase):
    def setUp(self):
        self.private = tempfile.TemporaryDirectory()
        self.addCleanup(self.private.cleanup)
        self.root = Path(self.private.name)
        self.defaults = self.root / "etc/default/chatgpt"
        self.defaults.parent.mkdir(parents=True)
        self.sources = self.root / "etc/apt/sources.list.d/chatgpt.sources"
        self.temp_root = self.root / "tmp"
        self.temp_root.mkdir()

    def test_digest_mismatch_never_invokes_guest_package_manager(self):
        with mock.patch.object(installer, "DEFAULTS_FILE", self.defaults), \
             mock.patch.object(installer, "SOURCES_FILE", self.sources), \
             mock.patch.object(installer, "TEMP_ROOT", self.temp_root), \
             mock.patch.object(installer, "PACKAGE_SIZE", 4), \
             mock.patch.object(installer, "PACKAGE_SHA256", "0" * 64), \
             mock.patch.object(installer, "urlopen", return_value=FakeResponse(b"test")), \
             mock.patch.object(installer.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "digest"):
                installer.install()
        run.assert_not_called()
        self.assertFalse(self.defaults.exists())

    def test_exact_package_is_checked_before_apt_and_repository_stays_disabled(self):
        package = b"synthetic package bytes"
        commands = []

        def run(argv, **_):
            commands.append(argv)
            if argv[0] == "/usr/bin/dpkg-deb":
                return mock.Mock(stdout="Package: chatgpt\nVersion: 26.917.71314\nArchitecture: arm64\n")
            if argv[0] == "/usr/bin/apt-get":
                self.assertEqual(argv[1:-1], ["install", "-y", "--no-remove", "--no-install-recommends"])
                self.assertEqual(self.defaults.read_text(), 'repo_add_once="false"\n')
                self.assertFalse(self.sources.exists())
                return mock.Mock(stdout="")
            if argv[0] == "/usr/bin/dpkg-query":
                return mock.Mock(stdout="26.917.71314 arm64")
            self.fail(f"unexpected command {argv}")

        with mock.patch.object(installer, "DEFAULTS_FILE", self.defaults), \
             mock.patch.object(installer, "SOURCES_FILE", self.sources), \
             mock.patch.object(installer, "TEMP_ROOT", self.temp_root), \
             mock.patch.object(installer, "PACKAGE_SIZE", len(package)), \
             mock.patch.object(installer, "PACKAGE_SHA256", hashlib.sha256(package).hexdigest()), \
             mock.patch.object(installer, "urlopen", return_value=FakeResponse(package)), \
             mock.patch.object(installer.subprocess, "run", side_effect=run):
            installer.install()
        self.assertEqual([argv[0] for argv in commands],
                         ["/usr/bin/dpkg-deb", "/usr/bin/apt-get", "/usr/bin/dpkg-query"])
        self.assertFalse(self.sources.exists())

    def test_existing_chatgpt_repository_stops_before_download(self):
        self.sources.parent.mkdir(parents=True)
        self.sources.write_text("unexpected source\n")
        with mock.patch.object(installer, "SOURCES_FILE", self.sources), \
             mock.patch.object(installer, "urlopen") as urlopen, \
             mock.patch.object(installer.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "source already exists"):
                installer.install()
        urlopen.assert_not_called()
        run.assert_not_called()

    def test_package_identity_mismatch_stops_before_apt(self):
        package = b"synthetic package bytes"

        def run(argv, **_):
            self.assertEqual(argv[0], "/usr/bin/dpkg-deb")
            return mock.Mock(stdout="Package: chatgpt\nVersion: wrong-version\nArchitecture: arm64\n")

        with mock.patch.object(installer, "DEFAULTS_FILE", self.defaults), \
             mock.patch.object(installer, "SOURCES_FILE", self.sources), \
             mock.patch.object(installer, "TEMP_ROOT", self.temp_root), \
             mock.patch.object(installer, "PACKAGE_SIZE", len(package)), \
             mock.patch.object(installer, "PACKAGE_SHA256", hashlib.sha256(package).hexdigest()), \
             mock.patch.object(installer, "urlopen", return_value=FakeResponse(package)), \
             mock.patch.object(installer.subprocess, "run", side_effect=run) as command:
            with self.assertRaisesRegex(RuntimeError, "identity"):
                installer.install()
        self.assertEqual(command.call_count, 1)
        self.assertFalse(self.defaults.exists())


if __name__ == "__main__":
    unittest.main()
