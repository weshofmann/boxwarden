#!/usr/bin/env python3
"""Run one retained, private synthetic formatter VM proof on a new raw file."""

import hashlib
import json
import os
from pathlib import Path
import plistlib
import secrets
import stat
import subprocess
import sys
import tempfile
import uuid


SIZE = 64 * 1024 * 1024
KERNEL_SHA256 = "a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d"
ISO_SHA256 = "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"
DEB_SHA256 = "0a3d402fd7c7c07f63b8104d84a47378f57022e7c816190e6cc99950492d8cae"
CHECKER_SHA256 = "e5e8f8b30641fab6e3f402c9746ce0537ad95c528e96cde2e446ca0968e63279"
ARTIFACTS = ("kernel-image", "formatter-initrd", "alpha-formatter", "e2fsck.static", "alpha-formatter-host")


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def write_new(path: Path, value: dict) -> None:
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "w") as output:
        json.dump(value, output, sort_keys=True)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())


def invoke(command: list[str], stdout: Path, stderr: Path, timeout: int) -> int:
    with os.fdopen(os.open(stdout, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as out:
        with os.fdopen(os.open(stderr, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "wb") as err:
            result = subprocess.run(command, stdout=out, stderr=err, timeout=timeout)
            out.flush()
            os.fsync(out.fileno())
            err.flush()
            os.fsync(err.fileno())
            return result.returncode


def admitted_bundle(bundle: Path) -> tuple[Path, Path, Path, dict]:
    info = bundle.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError("formatter bundle is not a private directory")
    manifest_path = bundle / "manifest.json"
    info = manifest_path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077 or info.st_nlink != 1 or info.st_size > 4096:
        raise ValueError("formatter manifest is not a bounded private regular file")
    manifest = json.loads(manifest_path.read_text())
    if (manifest.get("version") != 1 or manifest.get("iso_sha256") != ISO_SHA256
            or manifest.get("checker_deb_sha256") != DEB_SHA256
            or manifest.get("runner_entitlements") != {"com.apple.security.virtualization": True}
            or not isinstance(manifest.get("files"), dict)
            or set(manifest["files"]) != set(ARTIFACTS)
            or manifest["files"]["kernel-image"] != KERNEL_SHA256
            or manifest["files"]["e2fsck.static"] != CHECKER_SHA256):
        raise ValueError("formatter preparation manifest differs")
    repo_root = Path(__file__).resolve().parents[2]
    revision = subprocess.run(["git", "-C", str(repo_root), "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    source_status = subprocess.run(["git", "-C", str(repo_root), "status", "--porcelain"], capture_output=True, text=True, check=True).stdout
    if manifest.get("source_commit") != revision or source_status:
        raise ValueError("formatter preparation was not built from this exact published source")
    for name in ARTIFACTS:
        path = bundle / name
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_nlink != 1 or info.st_mode & 0o022:
            raise ValueError(f"formatter artifact {name} is not an admitted regular file")
        if digest(path) != manifest["files"][name]:
            raise ValueError(f"formatter artifact {name} digest differs")
    runner = bundle / "alpha-formatter-host"
    subprocess.run(["codesign", "--verify", "--strict", str(runner)], check=True, capture_output=True)
    entitlements = subprocess.run(["codesign", "-d", "--entitlements", ":-", str(runner)], check=True, capture_output=True)
    if plistlib.loads(entitlements.stdout) != {"com.apple.security.virtualization": True}:
        raise ValueError("formatter runner has unexpected entitlements")
    return runner, bundle / "kernel-image", bundle / "formatter-initrd", manifest


def main(bundle: Path) -> None:
    runner, kernel, initrd, manifest = admitted_bundle(bundle)
    root = Path(tempfile.mkdtemp(prefix="boxwarden-alpha-formatter-boot.", dir="/private/tmp"))
    volumes = root / "volumes"
    volumes.mkdir(mode=0o700)
    filesystem_uuid = str(uuid.uuid4())
    volume_uuid = str(uuid.uuid4())
    raw = volumes / f"{volume_uuid}.raw"
    marker = secrets.token_hex(32)
    transaction = secrets.token_hex(16)
    descriptor = os.open(raw, os.O_CREAT | os.O_EXCL | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        os.ftruncate(descriptor, SIZE)
        os.write(descriptor, bytes.fromhex(marker))
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    info = raw.stat()
    write_new(volumes / f"{volume_uuid}.format.json", {
        "version": 1, "domain": "alpha", "volume_id": volume_uuid,
        "filesystem_uuid": filesystem_uuid, "size_bytes": SIZE,
        "state": "formatting", "identity": {"device": info.st_dev, "inode": info.st_ino},
    })
    before = digest(raw)
    command = [str(runner), "preflight-run", str(kernel), str(initrd), str(raw),
               str(info.st_dev), str(info.st_ino), str(SIZE), transaction,
               filesystem_uuid, marker]
    write_new(root / "planned.json", {
        "preparation_manifest_sha256": digest(bundle / "manifest.json"),
        "runner_sha256": manifest["files"]["alpha-formatter-host"],
        "kernel_sha256": KERNEL_SHA256,
        "initrd_sha256": manifest["files"]["formatter-initrd"], "disk_device": info.st_dev,
        "disk_inode": info.st_ino, "disk_size": SIZE,
        "disk_before_sha256": before, "filesystem_uuid": filesystem_uuid,
        "transaction": transaction, "phase": "planned",
    })
    print(f"private formatter boot probe: {root}", flush=True)
    if invoke(command, root / "preflight.stdout", root / "preflight.stderr", 30) != 0:
        raise RuntimeError("formatter preflight failed; private evidence retained")
    preflight = json.loads((root / "preflight.stdout").read_text())
    if preflight != {"validated": True, "network_devices": 0, "storage_devices": 1,
                     "storage_read_only": False, "serial_ports": 2, "socket_devices": 0,
                     "shared_directory_devices": 0, "vm_state": "stopped"}:
        raise RuntimeError("formatter preflight evidence differs; private evidence retained")
    after_preflight = raw.stat()
    if (after_preflight.st_dev, after_preflight.st_ino, after_preflight.st_size) != (info.st_dev, info.st_ino, SIZE) or digest(raw) != before:
        raise RuntimeError("formatter preflight changed the synthetic raw file; private evidence retained")
    command[1] = "run"
    write_new(root / "run-intent.json", {"phase": "run-requested", "transaction": transaction})
    if invoke(command, root / "run.stdout", root / "run.stderr", 180) != 0:
        raise RuntimeError("formatter boot failed; private evidence retained")
    host = json.loads((root / "run.stdout").read_text())
    if (host.get("version") != 1 or host.get("transaction") != transaction
            or host.get("vm_stopped") is not True
            or host.get("runtime_network_devices") != 0
            or host.get("storage_devices") != 1
            or host.get("storage_read_only") is not False
            or host.get("serial_ports") != 2
            or host.get("socket_devices") != 0
            or host.get("shared_directory_devices") != 0
            or host.get("observed_uuid") != filesystem_uuid
            or host.get("whole_device") is not True
            or host.get("filesystem_clean") is not True):
        raise RuntimeError("formatter host report differs; private evidence retained")
    with raw.open("rb") as disk:
        disk.seek(1024)
        header = disk.read(1024)
    if header[56:58] != b"\x53\xef" or header[104:120] != uuid.UUID(filesystem_uuid).bytes:
        raise RuntimeError("formatted ext4 superblock differs; private evidence retained")
    after = raw.stat()
    if (after.st_dev, after.st_ino, after.st_size) != (info.st_dev, info.st_ino, SIZE):
        raise RuntimeError("raw file identity changed; private evidence retained")
    write_new(root / "result.json", {
        "vm_stopped_and_runner_reaped": True,
        "disk_after_sha256": digest(raw),
        "ext4_uuid": filesystem_uuid,
        "host_report": host,
    })
    print("synthetic formatter boot, guest e2fsck, VM stop, and host ext4 header checks passed")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: run_boot_probe.py <private-prepared-formatter-bundle>")
    main(Path(sys.argv[1]))
