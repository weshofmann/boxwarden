#!/usr/bin/env python3
"""Admit a separately formatted private synthetic ext4 candidate by digest.

This checks identity and copies bytes; it does not format, mount, traverse,
or establish filesystem health. A trusted Linux formatter adapter is pending.
"""

import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys


FIXTURE_UUID = "2f1c6b88-9849-4c5d-9d20-f3bc30bd77a1"
FIXTURE_CONTENT = b"boxwarden synthetic ext4 proof v1\n"
SIZE = 64 * 1024 * 1024


def fixture_request():
    """Fixed input for a future admitted, isolated Linux formatter adapter."""
    return {
        "version": 1,
        "scope": "whole-device",
        "size_bytes": SIZE,
        "filesystem": "ext4",
        "filesystem_uuid": FIXTURE_UUID,
        "file": {
            "path": "proof.txt",
            "content_utf8": FIXTURE_CONTENT.decode("ascii"),
            "sha256": hashlib.sha256(FIXTURE_CONTENT).hexdigest(),
        },
        "required_formatter_evidence": [
            "exact-disk-identity", "mkfs-ext4-whole-device", "proof-file-written",
            "clean-unmount", "e2fsck-clean", "formatter-stopped-and-reaped",
        ],
    }


def check_ext4_structure(image, expected_uuid=FIXTURE_UUID):
    if len(image) != SIZE:
        raise ValueError("synthetic ext4 image size mismatch")
    if not isinstance(expected_uuid, str) or re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", expected_uuid) is None:
        raise ValueError("invalid expected ext4 UUID")
    sb = memoryview(image)[1024:2048]
    if sb[0x38:0x3a].tobytes() != b"\x53\xef":
        raise ValueError("ext4 magic mismatch")
    if sb[0x68:0x78].tobytes() != bytes.fromhex(expected_uuid.replace("-", "")):
        raise ValueError("synthetic ext4 UUID mismatch")
    if not sb[0x3a] & 1 or not sb[0x5c] & 4 or not sb[0x60] & 0x40:
        raise ValueError("synthetic ext4 is not clean, journaled, and extent-based")


def copy_admitted_fixture(source: Path, destination: Path, expected_sha256: str, expected_uuid=FIXTURE_UUID):
    source = source.absolute()
    destination = destination.absolute()
    if (source.parent.parent != Path("/private/tmp")
            or re.fullmatch(r"boxwarden-inspector-fixture\.[A-Za-z0-9_]{6,16}", source.parent.name) is None
            or source.name != "synthetic-ext4.raw"):
        raise ValueError("fixture source is not a private synthetic path")
    if re.fullmatch(r"[0-9a-f]{64}", expected_sha256) is None:
        raise ValueError("invalid exact fixture digest")
    if (destination.parent.parent != Path("/private/tmp")
            or re.fullmatch(r"boxwarden-inspector-boot\.[A-Za-z0-9_]{6,16}", destination.parent.name) is None
            or destination.name != "synthetic.raw"):
        raise ValueError("fixture destination is not a private probe disk")
    destination_dir = destination.parent.lstat()
    if (not stat.S_ISDIR(destination_dir.st_mode) or stat.S_ISLNK(destination_dir.st_mode)
            or destination_dir.st_uid != os.getuid() or destination_dir.st_mode & 0o077):
        raise ValueError("fixture destination directory is not private")
    directory = source.parent.lstat()
    if (not stat.S_ISDIR(directory.st_mode) or stat.S_ISLNK(directory.st_mode)
            or directory.st_uid != os.getuid() or directory.st_mode & 0o077):
        raise ValueError("fixture source directory is not private")
    info = source.lstat()
    if (not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode)
            or info.st_uid != os.getuid() or info.st_nlink != 1
            or info.st_mode & 0o077 or info.st_size != SIZE):
        raise ValueError("fixture source file identity is unsafe")
    descriptor = os.open(source, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        observed = os.fstat(descriptor)
        if (observed.st_dev, observed.st_ino, observed.st_size) != (info.st_dev, info.st_ino, SIZE):
            raise ValueError("fixture source changed during admission")
        with os.fdopen(descriptor, "rb", closefd=False) as handle:
            image = handle.read(SIZE + 1)
    finally:
        os.close(descriptor)
    if hashlib.sha256(image).hexdigest() != expected_sha256:
        raise ValueError("fixture source digest mismatch")
    check_ext4_structure(image, expected_uuid)
    output = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(output, "wb") as handle:
            handle.write(image)
            handle.flush()
            os.fsync(handle.fileno())
    except BaseException:
        destination.unlink(missing_ok=True)
        raise


if __name__ == "__main__":
    if len(sys.argv) == 2 and sys.argv[1] == "--request":
        print(json.dumps(fixture_request(), sort_keys=True))
        raise SystemExit(0)
    if len(sys.argv) not in (4, 5):
        raise SystemExit("usage: fixture_contract.py --request | <private-synthetic-ext4.raw> <destination-synthetic.raw> <sha256> [expected-uuid]")
    copy_admitted_fixture(Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3],
                          sys.argv[4] if len(sys.argv) == 5 else FIXTURE_UUID)
