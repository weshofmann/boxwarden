#!/usr/bin/env python3
"""Build interface checks; never launch the app or access a pasteboard."""
import os
import hashlib
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest


class BuildInterfaceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="bw-menu-build-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.marker = self.root / "compiler-started"
        self.tools = self.root / "tools"
        self.tools.mkdir()
        # Invalid requests must fail before starting a compiler or staging an app.
        for name in ("go", "swiftc"):
            tool = self.tools / name
            tool.write_text('#!/bin/sh\ntouch "$BUILD_TEST_MARKER"\nexit 97\n')
            tool.chmod(0o700)
        self.environment = dict(os.environ, PATH=str(self.tools) + ":/usr/bin:/bin",
                                BUILD_TEST_MARKER=str(self.marker))
        self.script = Path(__file__).resolve().parents[1] / "build.sh"

    def refused(self, arguments):
        self.marker.unlink(missing_ok=True)
        result = subprocess.run(["/bin/bash", str(self.script), *arguments],
                                env=self.environment, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertFalse(self.marker.exists(), "invalid request started a compiler")

    def test_project_app_is_regular_native_window_with_separate_identity(self):
        with (self.script.parent / "ProjectInfo.plist").open("rb") as handle:
            info = plistlib.load(handle)
        self.assertEqual(info["CFBundleExecutable"], "Boxwarden Projects")
        self.assertEqual(info["CFBundleIdentifier"], "org.boxwarden.project-manager")
        self.assertFalse(info.get("LSUIElement", False))

    def test_malformed_arguments_fail_before_build(self):
        for arguments in (["--unknown"], ["--cli"], ["--output"], ["--version"],
                          ["--build"], ["--bundle-id"], ["--bundle-id", "bad id"],
                          ["--bundle-id", "../escape"], ["--bundle-id", "org..boxwarden"],
                          ["--bundle-id", "org.test", "--bundle-id", "org.other"],
                          ["--app", "unknown"], ["--version", "v0.2.0"],
                          ["--version", "0.02.0"], ["--version", "0.2"],
                          ["--build", "-1"], ["--build", "1.2"],
                          ["--output", "relative.app"], ["--cli", "relative-cli"]):
            with self.subTest(arguments=arguments):
                self.refused(arguments)

    def test_custom_output_never_replaces_existing_file_directory_or_link(self):
        existing = self.root / "Existing.app"
        existing.mkdir()
        sentinel = existing / "preserve-me"
        sentinel.write_text("operator-owned")
        self.refused(["--output", str(existing)])
        self.assertEqual(sentinel.read_text(), "operator-owned")
        file = self.root / "file.app"
        file.write_text("operator-owned")
        self.refused(["--output", str(file)])
        self.assertEqual(file.read_text(), "operator-owned")
        link = self.root / "link.app"
        link.symlink_to(self.root / "missing")
        self.refused(["--output", str(link)])
        self.assertTrue(link.is_symlink())

    def test_non_macho_cli_is_refused(self):
        cli = self.root / "synthetic-cli"
        cli.write_text("#!/bin/sh\nexit 0\n")
        cli.chmod(0o700)
        self.refused(["--cli", str(cli), "--output", str(self.root / "New.app")])

    def compiled_cli(self, architecture="arm64"):
        source = self.root / "synthetic.c"
        source.write_text("int main(void) { return 0; }\n")
        cli = self.root / "synthetic-cli"
        subprocess.run(["/usr/bin/clang", "-arch", architecture, str(source), "-o", str(cli)],
                       check=True, capture_output=True)
        subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", str(cli)],
                       check=True, capture_output=True)
        return cli

    def test_unsigned_and_other_architecture_cli_are_refused(self):
        cli = self.compiled_cli()
        subprocess.run(["/usr/bin/codesign", "--remove-signature", str(cli)],
                       check=True, capture_output=True)
        self.refused(["--cli", str(cli), "--output", str(self.root / "Unsigned.app")])
        cli = self.compiled_cli("x86_64")
        self.refused(["--cli", str(cli), "--output", str(self.root / "Intel.app")])

    def test_signed_cli_is_preserved_in_versioned_custom_app(self):
        self.check_packaged_cli("clipboard")

    def test_project_window_preserves_cli_on_case_insensitive_filesystem(self):
        self.check_packaged_cli("project-manager")

    def test_isolated_project_bundle_keeps_default_identity_unchanged(self):
        self.check_packaged_cli("project-manager", "org.boxwarden.project-manager.test-reliability")
        with (self.script.parent / "ProjectInfo.plist").open("rb") as handle:
            self.assertEqual(plistlib.load(handle)["CFBundleIdentifier"], "org.boxwarden.project-manager")

    def check_packaged_cli(self, app_kind, bundle_id=None):
        cli = self.compiled_cli()
        original = cli.read_bytes()
        output = self.root / "New App.app"
        # No go exists in this path: supplied CLI packaging must not rebuild it.
        extra = ["--bundle-id", bundle_id] if bundle_id else []
        result = subprocess.run(["/bin/bash", str(self.script), *extra, "--app", app_kind, "--cli", str(cli),
                                 "--output", str(output), "--version", "0.2.0-beta.1+fixture",
                                 "--build", "7"], env=dict(os.environ, PATH="/usr/bin:/bin"),
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), str(output))
        self.assertEqual(hashlib.sha256((output / "Contents/MacOS/boxwarden").read_bytes()).hexdigest(), hashlib.sha256(original).hexdigest())
        self.assertEqual(cli.read_bytes(), original)
        with (output / "Contents/Info.plist").open("rb") as handle:
            info = plistlib.load(handle)
        self.assertEqual(info["CFBundleShortVersionString"], "0.2.0-beta.1+fixture")
        self.assertEqual(info["CFBundleVersion"], "7")
        if bundle_id:
            self.assertEqual(info["CFBundleIdentifier"], bundle_id)
        self.assertNotEqual(info["CFBundleExecutable"].lower(), "boxwarden")
        subprocess.run(["/usr/bin/codesign", "--verify", "--strict", str(output)],
                       check=True, capture_output=True)


if __name__ == "__main__":
    unittest.main()
