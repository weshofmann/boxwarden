#!/usr/bin/env python3
"""Append a deterministic, root-owned alpha probe to a verified initramfs.

Linux permits concatenated newc and compressed cpio members. The original
Canonical initrd remains byte-for-byte intact; `rdinit=/alpha-probe` selects
only this appended program at boot.
"""

import os
from pathlib import Path
import shutil
import sys


MAX_ORIGINAL_BYTES = 256 * 1024 * 1024
MAX_GUEST_BYTES = 16 * 1024 * 1024


def _header(name: bytes, *, inode: int, mode: int, size: int) -> bytes:
    fields = (
        inode, mode, 0, 0, 1, 0, size,
        0, 0, 0, 0, len(name) + 1, 0,
    )
    return b"070701" + b"".join(f"{field:08x}".encode("ascii") for field in fields)


def _pad(output, length: int) -> int:
    count = (-length) % 4
    output.write(b"\x00" * count)
    return length + count


def _entry(output, name: bytes, data: bytes, *, inode: int, mode: int) -> None:
    header = _header(name, inode=inode, mode=mode, size=len(data))
    output.write(header)
    output.write(name + b"\x00")
    _pad(output, len(header) + len(name) + 1)
    output.write(data)
    _pad(output, len(data))


def append_probe(original: Path, guest: Path, output: Path) -> None:
    if not original.is_file() or not guest.is_file():
        raise ValueError("original initrd and guest binary must be regular files")
    original_size = original.stat().st_size
    guest_size = guest.stat().st_size
    if not 0 < original_size <= MAX_ORIGINAL_BYTES or not 0 < guest_size <= MAX_GUEST_BYTES:
        raise ValueError("initrd or guest binary exceeds fixed bounds")
    guest_bytes = guest.read_bytes()
    descriptor = os.open(output, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as destination, original.open("rb") as source:
            shutil.copyfileobj(source, destination, 1024 * 1024)
            _pad(destination, original_size)
            _entry(destination, b"alpha-probe", guest_bytes, inode=1, mode=0o100755)
            _entry(destination, b"TRAILER!!!", b"", inode=2, mode=0)
            destination.flush()
            os.fsync(destination.fileno())
    except BaseException:
        output.unlink(missing_ok=True)
        raise


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit("usage: pack_initramfs.py <verified-initrd> <guest-binary> <new-output>")
    append_probe(*(Path(argument) for argument in sys.argv[1:]))
