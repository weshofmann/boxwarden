import gzip
import hashlib
from pathlib import Path
import tempfile
import unittest

from kernel_image import unpack_kernel, validate_image


class KernelImageTests(unittest.TestCase):
    def test_unpack_exact_arm64_image(self):
        image = bytearray(128)
        image[56:60] = b"ARM\x64"
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "vmlinuz"
            output = Path(directory) / "kernel-image"
            source.write_bytes(gzip.compress(image, mtime=0))
            unpack_kernel(source, output, expected_source=hashlib.sha256(source.read_bytes()).hexdigest(),
                          expected_image=hashlib.sha256(image).hexdigest())
            self.assertEqual(output.read_bytes(), image)
            with self.assertRaises(FileExistsError):
                unpack_kernel(source, output, expected_source=hashlib.sha256(source.read_bytes()).hexdigest(),
                              expected_image=hashlib.sha256(image).hexdigest())

    def test_rejects_bad_magic_and_gzip_trailing_data(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "vmlinuz"
            output = Path(directory) / "kernel-image"
            bad_image = bytes(128)
            source.write_bytes(gzip.compress(bad_image, mtime=0))
            with self.assertRaises(ValueError):
                unpack_kernel(source, output, expected_source=hashlib.sha256(source.read_bytes()).hexdigest(),
                              expected_image=hashlib.sha256(bad_image).hexdigest())
            self.assertFalse(output.exists())
            source.write_bytes(gzip.compress(bad_image, mtime=0) + b"tail")
            with self.assertRaises(ValueError):
                unpack_kernel(source, output, expected_source=hashlib.sha256(source.read_bytes()).hexdigest(),
                              expected_image=hashlib.sha256(bad_image).hexdigest())
            with self.assertRaises(ValueError):
                validate_image(source.read_bytes())


if __name__ == "__main__":
    unittest.main()
