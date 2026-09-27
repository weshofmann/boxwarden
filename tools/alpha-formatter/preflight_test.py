#!/usr/bin/env python3
"""Run the signed formatter's no-start VZ preflight on a private synthetic raw file."""

import hashlib
from contextlib import nullcontext
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def main(binary: Path, kernel: Path, initrd: Path) -> None:
    with nullcontext(tempfile.mkdtemp(prefix="boxwarden-alpha-formatter-boot.", dir="/private/tmp")) as directory:
        volumes = Path(directory) / "volumes"
        volumes.mkdir(mode=0o700)
        raw = volumes / "12345678-1234-1234-1234-123456789abc.raw"
        marker = secrets.token_hex(32)
        descriptor = os.open(raw, os.O_CREAT | os.O_EXCL | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        try:
            os.ftruncate(descriptor, 64 * 1024 * 1024)
            os.write(descriptor, bytes.fromhex(marker))
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        identity = raw.stat()
        before = digest(raw)
        command = [str(binary), "preflight", str(kernel), str(initrd), str(raw),
                   str(identity.st_dev), str(identity.st_ino), str(identity.st_size),
                   secrets.token_hex(16), "12345678-1234-1234-1234-123456789abc", marker]
        result = subprocess.run(command, capture_output=True, text=True, timeout=30)
        if result.returncode != 0:
            raise AssertionError(f"VZ preflight failed: {result.stderr.strip()}")
        report = json.loads(result.stdout)
        expected = {"validated": True, "network_devices": 0, "storage_devices": 1,
                    "storage_read_only": False, "serial_ports": 2, "socket_devices": 0,
                    "shared_directory_devices": 0, "vm_state": "stopped"}
        if report != expected:
            raise AssertionError(f"unexpected VZ preflight: {report}")
        volume_id = raw.stem
        journal = volumes / f"{volume_id}.format.json"
        journal.write_text(json.dumps({
            "version": 1, "domain": "alpha", "volume_id": volume_id,
            "filesystem_uuid": "12345678-1234-1234-1234-123456789abc",
            "size_bytes": identity.st_size, "state": "formatting",
            "identity": {"device": identity.st_dev, "inode": identity.st_ino},
        }), encoding="utf-8")
        journal.chmod(0o600)
        command[1] = "preflight-run"
        admitted = subprocess.run(command, capture_output=True, text=True, timeout=30)
        if admitted.returncode != 0 or json.loads(admitted.stdout) != expected:
            raise AssertionError(f"creating-journal VZ preflight failed: {admitted.stderr.strip()}")
        foreign = Path(directory).with_name("boxwarden-alpha-formatter-foreign." + Path(directory).name.rsplit(".", 1)[-1])
        Path(directory).rename(foreign)
        try:
            foreign_command = command.copy()
            foreign_command[4] = str(foreign / "volumes" / raw.name)
            if subprocess.run(foreign_command, capture_output=True, text=True, timeout=30).returncode == 0:
                raise AssertionError("caller-selected private root passed formatter launch admission")
        finally:
            foreign.rename(directory)
        subprocess.run(["chmod", "+a", "everyone deny read", str(journal)], check=True)
        try:
            if subprocess.run(command, capture_output=True, text=True, timeout=30).returncode == 0:
                raise AssertionError("extended ACL on journal passed formatter launch admission")
        finally:
            subprocess.run(["chmod", "-a#", "0", str(journal)], check=True)
        wrong_inode = command.copy()
        wrong_inode[6] = str(identity.st_ino + 1)
        rejected = subprocess.run(wrong_inode, capture_output=True, text=True, timeout=30)
        if rejected.returncode == 0:
            raise AssertionError("wrong raw-file inode passed VZ preflight")
        zero_marker = command.copy()
        zero_marker[10] = "0" * 64
        rejected_marker = subprocess.run(zero_marker, capture_output=True, text=True, timeout=30)
        if rejected_marker.returncode == 0:
            raise AssertionError("all-zero marker passed creating-journal VZ preflight")
        journal.write_text(journal.read_text().replace('"formatting"', '"verified"'), encoding="utf-8")
        wrong_journal = subprocess.run(command, capture_output=True, text=True, timeout=30)
        if wrong_journal.returncode == 0:
            raise AssertionError("verified journal passed creating-journal VZ preflight")
        if digest(raw) != before:
            raise AssertionError("VZ preflight changed the synthetic raw file")
        print(json.dumps(report, sort_keys=True))
        shutil.rmtree(Path(directory))


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit("usage: preflight_test.py <signed-runner> <verified-kernel> <verified-initrd>")
    main(*(Path(value) for value in sys.argv[1:]))
