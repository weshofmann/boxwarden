import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from pack_initramfs import append_probe


def appended_members(data, original_size):
    offset = original_size + (-original_size) % 4
    members = []
    while True:
        header = data[offset:offset + 110]
        if len(header) != 110 or header[:6] != b"070701":
            raise AssertionError("invalid appended newc header")
        mode = int(header[14:22], 16)
        size = int(header[54:62], 16)
        name_size = int(header[94:102], 16)
        offset += 110
        name = data[offset:offset + name_size]
        if len(name) != name_size or not name.endswith(b"\x00"):
            raise AssertionError("invalid appended newc name")
        offset += name_size
        offset += (-offset) % 4
        payload = data[offset:offset + size]
        if len(payload) != size:
            raise AssertionError("truncated appended newc payload")
        offset += size
        offset += (-offset) % 4
        members.append((name[:-1], mode, payload))
        if name == b"TRAILER!!!\x00":
            if offset != len(data):
                raise AssertionError("trailing appended newc bytes")
            return members


class PackInitramfsTest(unittest.TestCase):
    def test_appends_root_owned_executable_newc_member(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original = root / "original"
            guest = root / "guest"
            output = root / "combined"
            original.write_bytes(b"signed-initrd")
            guest.write_bytes(b"guest-program")

            append_probe(original, guest, output)

            data = output.read_bytes()
            self.assertTrue(data.startswith(b"signed-initrd\x00\x00\x00070701"))
            header = data[16:126]
            self.assertEqual(int(header[14:22], 16), 0o100755)  # c_mode
            self.assertEqual(int(header[22:30], 16), 0)  # c_uid
            self.assertEqual(int(header[30:38], 16), 0)  # c_gid
            self.assertEqual(int(header[54:62], 16), len(b"guest-program"))
            self.assertEqual(int(header[94:102], 16), len(b"alpha-probe") + 1)
            self.assertIn(b"alpha-probe\x00", data)
            self.assertIn(b"guest-program", data)
            self.assertIn(b"TRAILER!!!\x00", data)
            self.assertEqual(os.stat(output).st_mode & 0o777, 0o600)

    def test_refuses_to_replace_existing_output(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original = root / "original"
            guest = root / "guest"
            output = root / "combined"
            original.write_bytes(b"signed-initrd")
            guest.write_bytes(b"guest-program")
            output.write_bytes(b"keep")
            with self.assertRaises(FileExistsError):
                append_probe(original, guest, output)
            self.assertEqual(output.read_bytes(), b"keep")

    def test_appends_private_bounded_export_request_as_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            original, guest, request, output = (root / name for name in ("original", "guest", "request", "combined"))
            original.write_bytes(b"signed-initrd")
            guest.write_bytes(b"guest-program")
            request.write_bytes(b'{"version":1}')
            request.chmod(0o600)

            append_probe(original, guest, output, request)

            data = output.read_bytes()
            members = appended_members(data, len(b"signed-initrd"))
            self.assertEqual(members, [
                (b"alpha-probe", 0o100755, b"guest-program"),
                (b"alpha-export-request.json", 0o100400, b'{"version":1}'),
                (b"TRAILER!!!", 0, b""),
            ])
            with patch("pack_initramfs._check_no_acl", side_effect=ValueError("extended ACL")):
                with self.assertRaises(ValueError):
                    append_probe(original, guest, root / "acl-rejected", request)
            request.chmod(0o644)
            with self.assertRaises(ValueError):
                append_probe(original, guest, root / "rejected", request)


if __name__ == "__main__":
    unittest.main()
