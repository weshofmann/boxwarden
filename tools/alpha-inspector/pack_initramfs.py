#!/usr/bin/env python3
"""Append a deterministic, root-owned alpha probe to a verified initramfs.

Linux permits concatenated newc and compressed cpio members. The original
Canonical initrd remains byte-for-byte intact; `rdinit=/alpha-probe` selects
only this appended program at boot.
"""

import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
from typing import Optional


MAX_ORIGINAL_BYTES = 256 * 1024 * 1024
MAX_GUEST_BYTES = 16 * 1024 * 1024
MAX_REQUEST_BYTES = 128 * 1024


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


def _check_no_acl(path: Path) -> None:
    if sys.platform != "darwin":
        return  # M1A host admission uses the macOS ACL inspector.
    result = subprocess.run(
        ["/bin/ls", "-lde", str(path)], capture_output=True, timeout=5,
        env={"LC_ALL": "C", "LANG": "C"}, check=True,
    )
    if len(result.stdout) > 8 * 1024 or len(result.stderr) > 8 * 1024:
        raise ValueError("export request ACL inspection exceeded bound")
    line, _, entries = result.stdout.partition(b"\n")
    fields = line.split()
    if not fields or fields[0].endswith(b"+") or entries.strip():
        raise ValueError("export request has extended ACL or malformed inspection")


def append_probe(original: Path, guest: Path, output: Path, request: Optional[Path] = None) -> None:
    if not original.is_file() or not guest.is_file():
        raise ValueError("original initrd and guest binary must be regular files")
    original_size = original.stat().st_size
    guest_size = guest.stat().st_size
    if not 0 < original_size <= MAX_ORIGINAL_BYTES or not 0 < guest_size <= MAX_GUEST_BYTES:
        raise ValueError("initrd or guest binary exceeds fixed bounds")
    guest_bytes = guest.read_bytes()
    request_bytes = None
    if request is not None:
        if not request.is_absolute():
            raise ValueError("export request must use an absolute path")
        descriptor = os.open(request, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as source:
            request_info = os.fstat(source.fileno())
            if (not stat.S_ISREG(request_info.st_mode) or request_info.st_nlink != 1
                    or request_info.st_uid != os.getuid() or stat.S_IMODE(request_info.st_mode) != 0o600
                    or not 0 < request_info.st_size <= MAX_REQUEST_BYTES):
                raise ValueError("export request must be a private one-link bounded regular file")
            request_bytes = source.read(MAX_REQUEST_BYTES + 1)
            after = os.fstat(source.fileno())
        pathname = request.lstat()
        _check_no_acl(request)
        acl_after = request.lstat()
        if (len(request_bytes) != request_info.st_size
                or (request_info.st_dev, request_info.st_ino, request_info.st_size, request_info.st_mode)
                != (after.st_dev, after.st_ino, after.st_size, after.st_mode)
                or (request_info.st_dev, request_info.st_ino, request_info.st_size, request_info.st_mode)
                != (pathname.st_dev, pathname.st_ino, pathname.st_size, pathname.st_mode)
                or (request_info.st_dev, request_info.st_ino, request_info.st_size, request_info.st_mode)
                != (acl_after.st_dev, acl_after.st_ino, acl_after.st_size, acl_after.st_mode)):
            raise ValueError("export request changed while reading")
    descriptor = os.open(output, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as destination, original.open("rb") as source:
            shutil.copyfileobj(source, destination, 1024 * 1024)
            _pad(destination, original_size)
            _entry(destination, b"alpha-probe", guest_bytes, inode=1, mode=0o100755)
            if request_bytes is not None:
                _entry(destination, b"alpha-export-request.json", request_bytes, inode=2, mode=0o100400)
            _entry(destination, b"TRAILER!!!", b"", inode=3, mode=0)
            destination.flush()
            os.fsync(destination.fileno())
    except BaseException:
        output.unlink(missing_ok=True)
        raise


if __name__ == "__main__":
    if len(sys.argv) not in (4, 5):
        raise SystemExit("usage: pack_initramfs.py <verified-initrd> <guest-binary> <new-output> [private-export-request]")
    append_probe(*(Path(argument) for argument in sys.argv[1:]))
