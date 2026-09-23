#!/bin/bash
set -euo pipefail
umask 077

# Prepares a synthetic-only boot probe and validates its VZ configuration.
# It intentionally does not invoke the helper's boot-probe command.
if [[ -z "${BOXWARDEN_ALPHA_UBUNTU_ISO:-}" || ! -f "$BOXWARDEN_ALPHA_UBUNTU_ISO" ]]; then
  echo 'BOXWARDEN_ALPHA_UBUNTU_ISO must name the verified Ubuntu ARM64 ISO' >&2
  exit 2
fi
expected_iso_sha256=c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe
read -r actual_iso_sha256 _ < <(shasum -a 256 "$BOXWARDEN_ALPHA_UBUNTU_ISO")
if [[ "$actual_iso_sha256" != "$expected_iso_sha256" ]]; then
  echo 'Ubuntu ISO digest does not match the pinned 24.04.4 ARM64 source' >&2
  exit 1
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
probe_dir="$(mktemp -d /private/tmp/boxwarden-inspector-boot.XXXXXX)"
prepared=0
trap 'if [[ "$prepared" != 1 ]]; then rm -rf -- "$probe_dir"; fi' EXIT

bsdtar -xf "$BOXWARDEN_ALPHA_UBUNTU_ISO" -C "$probe_dir" casper/vmlinuz casper/initrd
python3 "$script_dir/kernel_image.py" \
  "$probe_dir/casper/vmlinuz" "$probe_dir/kernel-image" \
  000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617 \
  a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d
GOCACHE="$probe_dir/gocache" GOMODCACHE="$probe_dir/modcache" GOTOOLCHAIN=local \
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -o "$probe_dir/alpha-probe" "$repo_root/tools/alpha-inspector/guest"
python3 "$script_dir/pack_initramfs.py" \
  "$probe_dir/casper/initrd" "$probe_dir/alpha-probe" "$probe_dir/inspector-initrd"
fixture_kind=zero
if [[ -n "${BOXWARDEN_ALPHA_EXT4_FIXTURE_SOURCE:-}" || -n "${BOXWARDEN_ALPHA_EXT4_FIXTURE_SHA256:-}" ]]; then
  if [[ -z "${BOXWARDEN_ALPHA_EXT4_FIXTURE_SOURCE:-}" || -z "${BOXWARDEN_ALPHA_EXT4_FIXTURE_SHA256:-}" ]]; then
    echo 'both ext4 fixture source and SHA-256 are required' >&2
    exit 2
  fi
  python3 "$script_dir/fixture_contract.py" \
    "$BOXWARDEN_ALPHA_EXT4_FIXTURE_SOURCE" "$probe_dir/synthetic.raw" \
    "$BOXWARDEN_ALPHA_EXT4_FIXTURE_SHA256"
  fixture_kind=ext4
else
  python3 - "$probe_dir/synthetic.raw" <<'PY'
import os
from pathlib import Path
import sys

disk = Path(sys.argv[1])
fd = os.open(disk, os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600)
try:
    os.ftruncate(fd, 8 * 1024 * 1024)
    os.fsync(fd)
finally:
    os.close(fd)
PY
fi
python3 - "$probe_dir/transaction.txt" <<'PY'
from pathlib import Path
import secrets
import sys
Path(sys.argv[1]).write_text(secrets.token_hex(16) + "\n", encoding="ascii")
PY

swiftc -module-cache-path "$probe_dir/swift-cache" \
  -o "$probe_dir/alpha-inspector" "$script_dir/main.swift" "$script_dir/boot.swift"
codesign --force --sign - --entitlements "$script_dir/virtualization.entitlements" \
  "$probe_dir/alpha-inspector"
codesign --verify --strict "$probe_dir/alpha-inspector"

transaction_hex="$(cat "$probe_dir/transaction.txt")"
preflight_command=preflight
if [[ "$fixture_kind" == ext4 ]]; then preflight_command=preflight-ext4; fi
"$probe_dir/alpha-inspector" "$preflight_command" \
  "$probe_dir/kernel-image" \
  "$probe_dir/inspector-initrd" \
  "$probe_dir/synthetic.raw" \
  "$transaction_hex" > "$probe_dir/preflight.json"

python3 - "$probe_dir" "$fixture_kind" <<'PY'
import hashlib
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
fixture_kind = sys.argv[2]
preflight = json.loads((root / "preflight.json").read_text(encoding="utf-8"))
expected = {
    "validated": True,
    "network_devices": 0,
    "runtime_network_devices": 0,
    "storage_devices": 1,
    "storage_read_only": True,
    "serial_ports": 2,
    "socket_devices": 0,
    "shared_directory_devices": 0,
    "vm_state": "stopped",
}
if any(preflight.get(key) != value for key, value in expected.items()):
    raise SystemExit(f"preflight configuration mismatch: {preflight}")
files = ("casper/vmlinuz", "kernel-image", "casper/initrd", "alpha-probe", "inspector-initrd", "alpha-inspector", "synthetic.raw")
digests = {}
for name in files:
    digest = hashlib.sha256()
    with (root / name).open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    digests[name] = digest.hexdigest()
disk = (root / "synthetic.raw").stat()
manifest = {
    "directory": str(root),
    "transaction": (root / "transaction.txt").read_text(encoding="ascii").strip(),
    "disk_device": disk.st_dev,
    "disk_inode": disk.st_ino,
    "disk_size": disk.st_size,
    "kernel_source": "casper/vmlinuz",
    "kernel_format": "arm64-linux-image",
    "fixture_kind": fixture_kind,
    "digests": digests,
    "preflight": preflight,
    "vm_started": False,
}
with (root / "manifest.json").open("x", encoding="utf-8") as handle:
    json.dump(manifest, handle, sort_keys=True)
    handle.write("\n")
print(json.dumps(manifest, sort_keys=True))
PY
prepared=1
