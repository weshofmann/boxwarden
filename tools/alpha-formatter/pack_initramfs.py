#!/usr/bin/env python3
"""Append the fixed formatter and static checker to the pinned Ubuntu initrd.

The caller verifies the source ISO and Ubuntu package digests. Linux unpacks
concatenated initramfs members; the Canonical bytes remain unchanged.
"""

import os
from pathlib import Path
import shutil
import sys


MAX_ORIGINAL_BYTES = 256 * 1024 * 1024
MAX_GUEST_BYTES = 16 * 1024 * 1024
MAX_CHECKER_BYTES = 4 * 1024 * 1024


def _pad(output, length: int) -> None:
    output.write(b"\x00" * ((-length) % 4))


def _entry(output, name: bytes, body: bytes, inode: int, mode: int) -> None:
    fields = (inode, mode, 0, 0, 1, 0, len(body), 0, 0, 0, 0, len(name) + 1, 0)
    header = b"070701" + b"".join(f"{field:08x}".encode("ascii") for field in fields)
    output.write(header)
    output.write(name + b"\x00")
    _pad(output, len(header) + len(name) + 1)
    output.write(body)
    _pad(output, len(body))


def append_formatter(original: Path, guest: Path, checker: Path, output: Path) -> None:
    sources = ((original, MAX_ORIGINAL_BYTES), (guest, MAX_GUEST_BYTES), (checker, MAX_CHECKER_BYTES))
    for source, limit in sources:
        if not source.is_file() or not 0 < source.stat().st_size <= limit:
            raise ValueError("formatter initrd source is missing or exceeds its fixed bound")
    if len({path.resolve() for path in (original, guest, checker, output)}) != 4:
        raise ValueError("formatter initrd paths must be distinct")
    descriptor = os.open(output, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as destination, original.open("rb") as source:
            shutil.copyfileobj(source, destination, 1024 * 1024)
            _pad(destination, original.stat().st_size)
            _entry(destination, b"alpha-formatter", guest.read_bytes(), 1, 0o100755)
            _entry(destination, b"usr/sbin/e2fsck", checker.read_bytes(), 2, 0o100755)
            _entry(destination, b"TRAILER!!!", b"", 3, 0)
            destination.flush()
            os.fsync(destination.fileno())
    except BaseException:
        output.unlink(missing_ok=True)
        raise


if __name__ == "__main__":
    if len(sys.argv) != 5:
        raise SystemExit("usage: pack_initramfs.py <verified-initrd> <guest-binary> <verified-static-e2fsck> <new-output>")
    append_formatter(*(Path(value) for value in sys.argv[1:]))
