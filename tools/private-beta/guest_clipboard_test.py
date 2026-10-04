"""Guest asset preparation checks; no guest, clipboard, or host install."""
import hashlib
import gzip
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("prepare-guest-clipboard.sh")
REPOSITORY = Path(__file__).resolve().parents[2]


class GuestClipboardPreparationTests(unittest.TestCase):
    def test_existing_output_is_preserved(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / "existing"
            output.mkdir()
            sentinel = output / "sentinel"
            sentinel.write_text("preserve")
            for existing in (output, sentinel, root / "broken-link"):
                if existing.name == "broken-link":
                    existing.symlink_to(root / "absent")
                result = subprocess.run(["/bin/bash", str(SCRIPT), "/unused/go", str(existing)],
                                        capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("output must not exist", result.stderr)
                self.assertEqual(sentinel.read_text(), "preserve")

    @unittest.skipUnless(os.environ.get("GO_BIN"), "set GO_BIN to actual Go for asset build")
    def test_builds_current_packaged_assets_without_modifying_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = root / "package with spaces"
            source = package / "support/source"
            source.mkdir(parents=True)
            archived = subprocess.check_output(["git", "-C", str(REPOSITORY), "archive", "HEAD"])
            subprocess.run(["/usr/bin/tar", "-xf", "-", "-C", str(source)], input=archived, check=True)
            if SCRIPT.exists():
                shutil.copy(SCRIPT, package / SCRIPT.name)
            original = source / "guest/ubuntu-24.04-arm64/artifacts/boxwarden-guest-bootstrap"
            original_bytes = original.read_bytes()
            output = root / "new-private-source"
            result = subprocess.run(["/bin/bash", str(package / SCRIPT.name), os.environ["GO_BIN"], str(output)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(sorted(path.name for path in output.iterdir()),
                             ["BOOTSTRAP.sha256", "SHA256SUMS", "boxwarden-guest-bootstrap.gz", "boxwarden-guest-clipboard.py"])
            self.assertTrue(all(path.stat().st_size <= 4 << 20 for path in output.iterdir()),
                            "every imported file must fit the existing 4 MiB per-file cap")
            compressed = (output / "boxwarden-guest-bootstrap.gz").read_bytes()
            self.assertEqual(compressed[4:8], b"\0\0\0\0", "gzip must omit source timestamp")
            bootstrap = gzip.decompress(compressed)
            self.assertEqual(bootstrap[:4], b"\x7fELF")
            self.assertEqual(struct.unpack_from("<H", bootstrap, 18)[0], 183, "ELF must be AArch64")
            self.assertEqual(bootstrap, original_bytes, "build differs from pinned current helper")
            self.assertEqual(original.read_bytes(), original_bytes, "preparation mutated shipped source")
            self.assertEqual((output / "boxwarden-guest-clipboard.py").read_bytes(),
                             (source / "guest/ubuntu-24.04-arm64/clipboard.py").read_bytes())
            manifest = (output / "SHA256SUMS").read_text().splitlines()
            self.assertEqual(len(manifest), 3)
            for line in manifest:
                digest, name = line.split("  ")
                self.assertEqual(hashlib.sha256((output / name).read_bytes()).hexdigest(), digest)
            self.assertEqual((output / "BOOTSTRAP.sha256").read_text(),
                             hashlib.sha256(bootstrap).hexdigest() + "  boxwarden-guest-bootstrap\n")
            self.assertIn(str(output), result.stdout)


if __name__ == "__main__":
    unittest.main()
