#!/usr/bin/env python3
"""Bounded synthetic inspector runner. --run is the only VM-starting mode.

This is a capability experiment, not the controlled-export acceptance path.
It never names or opens a workspace disk.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import struct
import subprocess
import sys
from kernel_image import decode_kernel, MAX_COMPRESSED, MAX_IMAGE


EXPECTED = {
    "validated": True, "network_devices": 0, "runtime_network_devices": 0,
    "storage_devices": 1, "storage_read_only": True, "serial_ports": 2,
    "socket_devices": 0, "shared_directory_devices": 0, "vm_state": "stopped",
}
SOURCE_KERNEL_SHA256 = "000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617"
IMAGE_KERNEL_SHA256 = "a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d"
FILES = ("casper/vmlinuz", "kernel-image", "casper/initrd", "alpha-probe",
         "inspector-initrd", "alpha-inspector", "synthetic.raw")
MAX_STREAM = 64 * 1024
MAX_LOG = 16 * 1024


def sha256_file(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def check_kernel_provenance(source, artifact, source_digest, image_digest):
    if source.stat().st_size > MAX_COMPRESSED or artifact.stat().st_size > MAX_IMAGE:
        raise ValueError("kernel source or Image exceeds fixed limit")
    compressed = source.read_bytes()
    if hashlib.sha256(compressed).hexdigest() != source_digest:
        raise ValueError("compressed kernel source digest mismatch")
    image = decode_kernel(compressed)
    if hashlib.sha256(image).hexdigest() != image_digest:
        raise ValueError("uncompressed ARM64 Image digest mismatch")
    if artifact.read_bytes() != image:
        raise ValueError("kernel-image differs from decoded pinned source")


def safe_file(root, relative):
    current = root
    for part in Path(relative).parts[:-1]:
        current = current / part
        info = current.lstat()
        if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
            raise ValueError(f"unsafe parent: {relative}")
    path = root / relative
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode) or info.st_nlink != 1:
        raise ValueError(f"unsafe file: {relative}")
    if info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError(f"unsafe file owner or mode: {relative}")
    return path, info


def parse_stream(stream, transaction_hex):
    if len(stream) > MAX_STREAM or stream[:6] != b"BWEX\x00\x01" or len(stream) < 22:
        raise ValueError("invalid stream header or size")
    if stream[6:22] != bytes.fromhex(transaction_hex):
        raise ValueError("transaction mismatch")
    offset = 22
    body = bytearray()
    length = None
    for wanted in (2, 3, 4, 5):
        if len(stream) - offset < 47:
            raise ValueError("truncated record")
        kind, name_length, chunk_length, declared = struct.unpack_from(">BHIQ", stream, offset)
        digest = stream[offset + 15:offset + 47]
        offset += 47
        if kind != wanted or len(stream) - offset < name_length + chunk_length:
            raise ValueError("unexpected or truncated record")
        name = stream[offset:offset + name_length]
        offset += name_length
        chunk = stream[offset:offset + chunk_length]
        offset += chunk_length
        if kind == 2:
            if name != b"report.json" or chunk or declared == 0 or declared > 4096 or digest != bytes(32):
                raise ValueError("invalid report declaration")
            length = declared
        elif kind == 3:
            if name or not chunk or declared != 0 or digest != bytes(32):
                raise ValueError("invalid report chunk")
            body.extend(chunk)
        elif kind == 4:
            if name or chunk or declared != 0 or len(body) != length or hashlib.sha256(body).digest() != digest:
                raise ValueError("invalid report digest")
        elif name or chunk or declared != length or digest != bytes(32) or offset != len(stream):
            raise ValueError("invalid terminal or trailing bytes")
    report = json.loads(body)
    if report != {
        "disk_prefix_sha256": hashlib.sha256(bytes(4096)).hexdigest(),
        "network_interfaces": ["lo"], "read_only": True,
    }:
        raise ValueError("unexpected guest observation")
    return report


def admit(root):
    root = root.absolute()
    if root.parent != Path("/private/tmp") or not re.fullmatch(r"boxwarden-inspector-boot\.[A-Za-z0-9]{6}", root.name):
        raise ValueError("not a synthetic probe directory")
    info = root.lstat()
    if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError("probe directory is not private and owned")
    manifest_path, _ = safe_file(root, "manifest.json")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if (manifest.get("directory") != str(root) or manifest.get("vm_started") is not False
            or manifest.get("preflight") != EXPECTED or manifest.get("kernel_source") != "casper/vmlinuz"
            or manifest.get("kernel_format") != "arm64-linux-image"):
        raise ValueError("manifest identity or fixed configuration mismatch")
    transaction = manifest.get("transaction")
    if not isinstance(transaction, str) or re.fullmatch(r"[0-9a-f]{32}", transaction) is None or transaction == "0" * 32:
        raise ValueError("invalid transaction")
    transaction_file, _ = safe_file(root, "transaction.txt")
    if transaction_file.read_text(encoding="ascii") != transaction + "\n":
        raise ValueError("transaction file mismatch")
    if set(manifest.get("digests", {})) != set(FILES):
        raise ValueError("unexpected digest inventory")
    if (manifest["digests"]["casper/vmlinuz"] != SOURCE_KERNEL_SHA256
            or manifest["digests"]["kernel-image"] != IMAGE_KERNEL_SHA256):
        raise ValueError("unpinned kernel source or Image digest")
    for relative in FILES:
        path, info = safe_file(root, relative)
        if sha256_file(path) != manifest["digests"][relative]:
            raise ValueError(f"digest mismatch: {relative}")
        if relative == "synthetic.raw":
            if (info.st_dev, info.st_ino, info.st_size) != (
                manifest["disk_device"], manifest["disk_inode"], 8 * 1024 * 1024
            ):
                raise ValueError("synthetic disk identity changed")
    check_kernel_provenance(root / "casper/vmlinuz", root / "kernel-image",
                            SOURCE_KERNEL_SHA256, IMAGE_KERNEL_SHA256)
    subprocess.run(["codesign", "--verify", "--strict", str(root / "alpha-inspector")], check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=10)
    fs = os.statvfs(root)
    available = fs.f_bavail * fs.f_frsize
    floor = max(20 * 1024**3, fs.f_blocks * fs.f_frsize // 10)
    if available < floor + 64 * 1024 * 1024:
        raise ValueError("free-space policy failed")
    physical = int(subprocess.check_output(["sysctl", "-n", "hw.memsize"], timeout=5))
    if physical < 4 * 1024**3:
        raise ValueError("2 GiB guest exceeds half of physical RAM")
    command = [str(root / "alpha-inspector"), "preflight", str(root / "kernel-image"),
               str(root / "inspector-initrd"), str(root / "synthetic.raw"), transaction]
    observed = json.loads(subprocess.check_output(command, timeout=15))
    if observed != EXPECTED:
        raise ValueError("fresh VZ configuration validation mismatch")
    return manifest, command, available, floor


def create_output(path):
    return os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb")


def run(root, manifest, preflight_command):
    command = preflight_command.copy()
    command[1] = "boot-probe"
    disk = root / "synthetic.raw"
    before = sha256_file(disk)
    timeout = False
    with create_output(root / "stream.bin") as stream, create_output(root / "boot.log") as log:
        process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=stream, stderr=log,
                                   close_fds=True, start_new_session=True)
        try:
            result = process.wait(timeout=80)
        except subprocess.TimeoutExpired:
            timeout = True
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            result = process.returncode
    # These checks run even if the helper failed or the outer deadline fired.
    admit(root)
    if sha256_file(disk) != before:
        raise ValueError("synthetic disk changed during boot")
    stream_size = (root / "stream.bin").stat().st_size
    log_size = (root / "boot.log").stat().st_size
    if timeout or result != 0 or stream_size > MAX_STREAM or log_size > MAX_LOG:
        raise ValueError(f"boot failed or exceeded bounds: exit={result}, timeout={timeout}, stream={stream_size}, log={log_size}")
    stream_bytes = (root / "stream.bin").read_bytes()
    log_bytes = (root / "boot.log").read_bytes()
    lines = log_bytes.splitlines()
    evidence_lines = [line for line in lines if line.startswith(b"BOOT_EVIDENCE ")]
    if len(evidence_lines) != 1:
        raise ValueError("missing or duplicate host boot evidence")
    evidence = json.loads(evidence_lines[0][len(b"BOOT_EVIDENCE "):])
    if evidence.get("vm_state") != "stopped" or evidence.get("runtime_network_devices") != 0 or evidence.get("export_bytes") != len(stream_bytes):
        raise ValueError("unexpected host boot evidence")
    report = parse_stream(stream_bytes, manifest["transaction"])
    return {"host": evidence, "guest": report, "disk_unchanged": True, "helper_reaped": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--preflight-only", action="store_true")
    mode.add_argument("--run", action="store_true", help="STARTS the synthetic Virtualization.framework VM")
    args = parser.parse_args()
    root = args.directory.absolute()
    manifest, preflight, available, floor = admit(root)
    if args.preflight_only:
        preflight[1] = "boot-probe"
        print(json.dumps({"vm_started": False, "launch_argv": preflight,
                          "disk_sha256": manifest["digests"]["synthetic.raw"],
                          "free_bytes": available, "free_floor_bytes": floor,
                          "cpu": 2, "memory_bytes": 2 * 1024**3,
                          "deadline_seconds": 80}, sort_keys=True))
    else:
        print(json.dumps(run(root, manifest, preflight), sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print(f"synthetic boot probe failed: {error}", file=sys.stderr)
        sys.exit(1)
