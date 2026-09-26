"""Execute the exact autoinstall EFI correction against disposable fstabs."""
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import unittest

SEED = pathlib.Path(__file__).resolve().parents[1] / "autoinstall/user-data"
ENTRY = b"/dev/disk/by-uuid/1234-ABCD /boot/efi vfat defaults 0 1\n"
UPDATED = ENTRY.replace(b"defaults", b"defaults,x-systemd.device-timeout=10min")


def installer_body():
    seed = SEED.read_text()
    match = re.search(r"    - \|\n      python3 - /target/etc/fstab <<'BW_EFI_PY'\n(.*?)      BW_EFI_PY\n", seed, re.S)
    # The baseline has no correction: exercising that no-op must fail the
    # behavioral assertions, rather than fail only because an import is absent.
    return "" if match is None else "\n".join(line[6:] for line in match.group(1).splitlines())


class EFIDeviceWaitTests(unittest.TestCase):
    def run_fixture(self, data, symlink=False, hardlink=False, mode=0o644):
        with tempfile.TemporaryDirectory() as tmp:
            actual = pathlib.Path(tmp) / "actual"
            actual.write_bytes(data)
            actual.chmod(mode)
            if hardlink:
                os.link(actual, pathlib.Path(tmp) / "alias")
            target = pathlib.Path(tmp) / "fstab"
            if symlink:
                target.symlink_to(actual)
            else:
                target = actual
            before = actual.stat()
            result = subprocess.run([sys.executable, "-", str(target)], input=installer_body().encode(), capture_output=True, timeout=5)
            after = actual.stat()
            return result.returncode, actual.read_bytes(), (after.st_uid, after.st_gid, after.st_mode & 0o777), (before.st_uid, before.st_gid, mode)

    def test_required_efi_wait_preserves_other_rows_and_metadata(self):
        prefix = b"# installer fstab\nUUID=root / ext4 defaults 0 1\n"
        suffix = b"/swap.img none swap sw 0 0\n"
        rc, data, after, before = self.run_fixture(prefix + ENTRY + suffix)
        self.assertEqual(rc, 0)
        self.assertEqual(data, prefix + UPDATED + suffix)
        self.assertEqual(after, before)

    def test_whitespace_and_uuid_source_are_preserved(self):
        entry = b"UUID=ABCD-1234\t/boot/efi\tvfat\tdefaults\t0 1 # EFI\n"
        rc, data, _, _ = self.run_fixture(entry)
        self.assertEqual(rc, 0)
        self.assertEqual(data, entry.replace(b"defaults", b"defaults,x-systemd.device-timeout=10min"))

    def test_repeated_application_is_idempotent(self):
        rc, data, _, _ = self.run_fixture(UPDATED)
        self.assertEqual((rc, data), (0, UPDATED))

    def test_existing_private_mode_is_preserved(self):
        rc, data, after, before = self.run_fixture(ENTRY, mode=0o640)
        self.assertEqual((rc, data, after), (0, UPDATED, before))

    def test_unexpected_entries_fail_without_modification(self):
        for data in [b"UUID=root / ext4 defaults 0 1\n", ENTRY * 2, ENTRY.replace(b"vfat", b"ext4"), ENTRY.replace(b"defaults", b"defaults,nofail"), ENTRY.replace(b"1234-ABCD", b"bad"), ENTRY.replace(b"0 1", b"0 0"), b"#" * 65537 + b"\n" + ENTRY]:
            with self.subTest(data=data[:100]):
                rc, after, _, _ = self.run_fixture(data)
                self.assertNotEqual(rc, 0)
                self.assertEqual(after, data)

    def test_symlink_fails_without_modifying_referent(self):
        rc, data, _, _ = self.run_fixture(ENTRY, symlink=True)
        self.assertNotEqual(rc, 0)
        self.assertEqual(data, ENTRY)

    def test_multiple_links_fail_without_modifying_alias(self):
        rc, data, _, _ = self.run_fixture(ENTRY, hardlink=True)
        self.assertNotEqual(rc, 0)
        self.assertEqual(data, ENTRY)

    def test_fifo_is_rejected_without_waiting_for_a_writer(self):
        with tempfile.TemporaryDirectory() as tmp:
            target = pathlib.Path(tmp) / "fstab"
            os.mkfifo(target)
            result = subprocess.run([sys.executable, "-", str(target)], input=installer_body().encode(), capture_output=True, timeout=2)
            self.assertNotEqual(result.returncode, 0)

    def test_missing_file_and_directory_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            for target in (pathlib.Path(tmp) / "missing", pathlib.Path(tmp)):
                result = subprocess.run([sys.executable, "-", str(target)], input=installer_body().encode(), capture_output=True, timeout=2)
                self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
