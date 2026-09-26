import importlib.util
from pathlib import Path
import tempfile
import unittest


MODULE = Path(__file__).with_name("pack_initramfs.py")
SPEC = importlib.util.spec_from_file_location("formatter_packer", MODULE)
packer = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(packer)


def appended_members(raw: bytes, offset: int) -> dict[str, bytes]:
    members = {}
    cursor = (offset + 3) & ~3
    while True:
        header = raw[cursor:cursor + 110]
        if header[:6] != b"070701":
            raise AssertionError("missing newc header")
        size = int(header[54:62], 16)
        name_size = int(header[94:102], 16)
        cursor += 110
        name = raw[cursor:cursor + name_size - 1].decode("ascii")
        cursor = (cursor + name_size + 3) & ~3
        body = raw[cursor:cursor + size]
        cursor = (cursor + size + 3) & ~3
        if name == "TRAILER!!!":
            if cursor != len(raw):
                raise AssertionError("trailing bytes")
            return members
        members[name] = body


class FormatterInitramfsTest(unittest.TestCase):
    def test_appends_only_fixed_guest_and_checker_paths(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original = root / "original"
            original.write_bytes(b"signed-initrd")
            guest = root / "guest"
            guest.write_bytes(b"static-arm64-guest")
            checker = root / "checker"
            checker.write_bytes(b"static-arm64-e2fsck")
            output = root / "output"

            packer.append_formatter(original, guest, checker, output)

            result = output.read_bytes()
            self.assertEqual(result[:len(original.read_bytes())], original.read_bytes())
            self.assertEqual(appended_members(result, original.stat().st_size), {
                "alpha-formatter": guest.read_bytes(),
                "usr/sbin/e2fsck": checker.read_bytes(),
            })
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)

    def test_rejects_existing_output_without_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ("original", "guest", "checker"):
                (root / name).write_bytes(b"data")
            output = root / "output"
            output.write_bytes(b"existing")
            with self.assertRaises(FileExistsError):
                packer.append_formatter(root / "original", root / "guest", root / "checker", output)
            self.assertEqual(output.read_bytes(), b"existing")


if __name__ == "__main__":
    unittest.main()
