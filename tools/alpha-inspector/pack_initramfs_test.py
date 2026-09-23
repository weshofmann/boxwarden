import os
from pathlib import Path
import tempfile
import unittest

from pack_initramfs import append_probe


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


if __name__ == "__main__":
    unittest.main()
