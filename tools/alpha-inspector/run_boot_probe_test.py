import hashlib
import json
import struct
import unittest

from run_boot_probe import parse_stream, check_kernel_provenance


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


if __name__ == "__main__":
    unittest.main()
