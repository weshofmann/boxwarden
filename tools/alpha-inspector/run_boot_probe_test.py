import hashlib
import json
import struct
import unittest

from run_boot_probe import parse_stream, check_kernel_provenance
from fixture_contract import FIXTURE_UUID, FIXTURE_CONTENT


class StreamTests(unittest.TestCase):
    def test_kernel_provenance_requires_exact_uncompressed_image(self):
        import gzip
        from pathlib import Path
        import tempfile

        image = bytearray(128)
        image[56:60] = b"ARM\x64"
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "vmlinuz"
            artifact = Path(directory) / "kernel-image"
            source.write_bytes(gzip.compress(image, mtime=0))
            artifact.write_bytes(image)
            source_digest = hashlib.sha256(source.read_bytes()).hexdigest()
            image_digest = hashlib.sha256(image).hexdigest()
            check_kernel_provenance(source, artifact, source_digest, image_digest)
            artifact.write_bytes(bytes(128))
            with self.assertRaises(ValueError):
                check_kernel_provenance(source, artifact, source_digest, image_digest)

    def test_exact_synthetic_report(self):
        transaction = bytes.fromhex("01" * 16)
        body = json.dumps({
            "disk_prefix_sha256": hashlib.sha256(bytes(4096)).hexdigest(),
            "network_interfaces": ["lo"],
            "read_only": True,
        }, separators=(",", ":")).encode()

        def record(kind, path=b"", chunk=b"", size=0, digest=bytes(32)):
            return struct.pack(">BH IQ", kind, len(path), len(chunk), size) + digest + path + chunk

        stream = (b"BWEX" + struct.pack(">H", 1) + transaction
                  + record(2, b"report.json", size=len(body))
                  + record(3, chunk=body)
                  + record(4, digest=hashlib.sha256(body).digest())
                  + record(5, size=len(body)))
        self.assertEqual(parse_stream(stream, transaction.hex()), json.loads(body))
        with self.assertRaises(ValueError):
            parse_stream(stream + b"x", transaction.hex())
        with self.assertRaises(ValueError):
            parse_stream(stream, "02" * 16)
        with self.assertRaises(ValueError):
            parse_stream(stream[:-1], transaction.hex())

    def test_ext4_report_is_bound_to_allowlisted_file_and_mount(self):
        transaction = bytes.fromhex("03" * 16)
        report = {
            "disk_prefix_sha256": "a" * 64,
            "network_interfaces": ["lo"],
            "read_only": True,
            "fixture_uuid": FIXTURE_UUID,
            "fixture_content": FIXTURE_CONTENT.decode(),
            "mount_options": ["ro", "noload", "nodev", "nosuid", "noexec"],
        }

        def stream_for(body):
            def frame(kind, path=b"", chunk=b"", declared=0, digest=bytes(32)):
                return struct.pack(">BHIQ", kind, len(path), len(chunk), declared) + digest + path + chunk
            return (b"BWEX\x00\x01" + transaction
                    + frame(2, b"report.json", declared=len(body))
                    + frame(3, chunk=body)
                    + frame(4, digest=hashlib.sha256(body).digest())
                    + frame(5, declared=len(body)))

        body = json.dumps(report, separators=(",", ":")).encode()
        self.assertEqual(parse_stream(stream_for(body), transaction.hex(), "ext4", "a" * 64), report)
        report["fixture_content"] = "changed\n"
        with self.assertRaises(ValueError):
            parse_stream(stream_for(json.dumps(report).encode()), transaction.hex(), "ext4", "a" * 64)

    def test_copy_report_requires_exact_expected_uuid_and_file_digest(self):
        transaction = bytes.fromhex("04" * 16)
        expected = {
            "disk_prefix_sha256": "b" * 64,
            "network_interfaces": ["lo"],
            "read_only": True,
            "fixture_uuid": "e915855e-801c-405b-9fb8-7c8b62bd8f45",
            "copy_file_sha256": "c" * 64,
            "copy_file_size": 57,
            "mount_options": ["ro", "noload", "nodev", "nosuid", "noexec"],
        }

        def stream_for(report):
            body = json.dumps(report, separators=(",", ":")).encode()
            def frame(kind, path=b"", chunk=b"", declared=0, digest=bytes(32)):
                return struct.pack(">BHIQ", kind, len(path), len(chunk), declared) + digest + path + chunk
            return (b"BWEX\x00\x01" + transaction
                    + frame(2, b"report.json", declared=len(body))
                    + frame(3, chunk=body)
                    + frame(4, digest=hashlib.sha256(body).digest())
                    + frame(5, declared=len(body)))

        self.assertEqual(parse_stream(stream_for(expected), transaction.hex(), "copy", "b" * 64,
                                      {"uuid": expected["fixture_uuid"], "file_sha256": "c" * 64,
                                       "file_size": 57}), expected)
        with self.assertRaises(ValueError):
            parse_stream(stream_for({**expected, "copy_file_sha256": "d" * 64}), transaction.hex(),
                         "copy", "b" * 64, {"uuid": expected["fixture_uuid"],
                                           "file_sha256": "c" * 64, "file_size": 57})


if __name__ == "__main__":
    unittest.main()
