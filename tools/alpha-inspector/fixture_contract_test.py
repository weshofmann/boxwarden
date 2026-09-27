import hashlib
from pathlib import Path
import tempfile
import unittest

from fixture_contract import check_ext4_structure, copy_admitted_fixture, fixture_request, FIXTURE_UUID, FIXTURE_CONTENT, SIZE


def structural_image():
    data = bytearray(SIZE)
    sb = memoryview(data)[1024:2048]
    sb[0x38:0x3a] = b"\x53\xef"
    sb[0x3a] = 1
    sb[0x5c] = 4
    sb[0x60] = 0x40
    sb[0x68:0x78] = bytes.fromhex(FIXTURE_UUID.replace("-", ""))
    return data


class FixtureContractTests(unittest.TestCase):
    def test_request_binds_exact_content_and_whole_disk(self):
        request = fixture_request()
        self.assertEqual(request["filesystem_uuid"], FIXTURE_UUID)
        self.assertEqual(request["size_bytes"], SIZE)
        self.assertEqual(request["scope"], "whole-device")
        self.assertEqual(request["file"]["path"], "proof.txt")
        self.assertEqual(request["file"]["sha256"], hashlib.sha256(FIXTURE_CONTENT).hexdigest())

    def test_rejects_wrong_identity_or_digest(self):
        image = structural_image()
        check_ext4_structure(image)
        for offset in (1024 + 0x38, 1024 + 0x3a, 1024 + 0x5c,
                       1024 + 0x60, 1024 + 0x68):
            changed = image.copy()
            changed[offset] = 0
            with self.assertRaises(ValueError):
                check_ext4_structure(changed)

    def test_copy_structure_requires_supplied_uuid(self):
        image = structural_image()
        copy_uuid = "e915855e-801c-405b-9fb8-7c8b62bd8f45"
        image[1024 + 0x68:1024 + 0x78] = bytes.fromhex(copy_uuid.replace("-", ""))
        check_ext4_structure(image, copy_uuid)
        with self.assertRaises(ValueError):
            check_ext4_structure(image)
        with self.assertRaises(ValueError):
            check_ext4_structure(image, "invalid")

    def test_copy_admits_only_private_synthetic_source(self):
        image = structural_image()
        with tempfile.TemporaryDirectory(prefix="boxwarden-inspector-fixture.", dir="/private/tmp") as source_dir:
            source = Path(source_dir) / "synthetic-ext4.raw"
            source.write_bytes(image)
            source.chmod(0o600)
            with tempfile.TemporaryDirectory(prefix="boxwarden-inspector-boot.", dir="/private/tmp") as destination_dir:
                destination = Path(destination_dir) / "synthetic.raw"
                digest = hashlib.sha256(image).hexdigest()
                copy_admitted_fixture(source, destination, digest)
                self.assertEqual(destination.read_bytes(), image)
                with self.assertRaises(ValueError):
                    copy_admitted_fixture(source, Path(destination_dir) / "second.raw", "0" * 64)
                with self.assertRaises(ValueError):
                    copy_admitted_fixture(source, Path(source_dir) / "not-a-boot-disk.raw", digest)


if __name__ == "__main__":
    unittest.main()
