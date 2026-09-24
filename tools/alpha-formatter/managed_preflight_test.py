#!/usr/bin/env python3
"""No-start proof that a signed managed formatter runner binds one state root."""

import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import tempfile

from bind_root import write_binding


def command(binary: Path, mode: str, kernel: Path, initrd: Path, raw: Path, marker: str) -> list[str]:
    info = raw.stat()
    return [str(binary), mode, str(kernel), str(initrd), str(raw),
            str(info.st_dev), str(info.st_ino), str(info.st_size),
            secrets.token_hex(16), "12345678-1234-1234-1234-123456789abc", marker]


def create_target(root: Path, domain: str) -> tuple[Path, str]:
    volumes = root / "volumes"
    volumes.mkdir(mode=0o700)
    raw = volumes / "12345678-1234-1234-1234-123456789abc.raw"
    marker = secrets.token_hex(32)
    fd = os.open(raw, os.O_CREAT | os.O_EXCL | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        os.ftruncate(fd, 64 * 1024 * 1024)
        os.write(fd, bytes.fromhex(marker))
        os.fsync(fd)
    finally:
        os.close(fd)
    info = raw.stat()
    journal = volumes / (raw.stem + ".format.json")
    fd = os.open(journal, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as output:
        json.dump({"version": 1, "domain": domain, "volume_id": raw.stem,
                   "filesystem_uuid": raw.stem, "size_bytes": info.st_size,
                   "state": "formatting", "identity": {"device": info.st_dev,
                   "inode": info.st_ino}}, output)
    return raw, marker


def main(kernel: Path, initrd: Path) -> None:
    source = Path(__file__).resolve().parent
    with tempfile.TemporaryDirectory(prefix="boxwarden-alpha-formatter-managed-test.", dir="/private/tmp") as directory:
        work = Path(directory)
        root = work / "state"
        root.mkdir(mode=0o700)
        binding = work / "binding.swift"
        write_binding(root, "alpha", binding)
        binary = work / "formatter-host"
        subprocess.run(["swiftc", "-D", "MANAGED_BOUND", "-module-cache-path", str(work / "swift-cache"),
                        "-o", str(binary), str(source / "main.swift"), str(source / "boot.swift"),
                        str(binding)], check=True)
        subprocess.run(["codesign", "--force", "--sign", "-", "--entitlements",
                        str(source.parent / "alpha-inspector" / "virtualization.entitlements"), str(binary)], check=True)
        raw, marker = create_target(root, "alpha")
        request = command(binary, "preflight-managed", kernel, initrd, raw, marker)
        admitted = subprocess.run(request, capture_output=True, text=True, timeout=30)
        if admitted.returncode != 0 or json.loads(admitted.stdout).get("validated") is not True:
            raise AssertionError(f"bound managed preflight rejected: {admitted.stderr}")
        request[1] = "preflight-run"
        if subprocess.run(request, capture_output=True, timeout=30).returncode == 0:
            raise AssertionError("synthetic mode accepted a managed state root")
        foreign = work / "foreign"
        foreign.mkdir(mode=0o700)
        other, other_marker = create_target(foreign, "alpha")
        if subprocess.run(command(binary, "preflight-managed", kernel, initrd, other, other_marker),
                          capture_output=True, timeout=30).returncode == 0:
            raise AssertionError("managed mode accepted another private root")
        journal = root / "volumes" / (raw.stem + ".format.json")
        journal.write_text(journal.read_text().replace('"alpha"', '"other"'))
        if subprocess.run(command(binary, "preflight-managed", kernel, initrd, raw, marker),
                          capture_output=True, timeout=30).returncode == 0:
            raise AssertionError("managed mode accepted another domain journal")
    print("signed managed runner admits only the bound state root and domain")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        raise SystemExit("usage: managed_preflight_test.py <verified-kernel> <verified-initrd>")
    main(Path(sys.argv[1]), Path(sys.argv[2]))
