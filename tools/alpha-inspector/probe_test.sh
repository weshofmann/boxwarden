#!/bin/bash
set -euo pipefail

# Host capability test only. The helper has no VM start path and this script
# attaches no real workspace disk. Supply a previously verified Ubuntu ARM64 ISO.
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
probe_dir="$(mktemp -d /private/tmp/boxwarden-inspector-probe.XXXXXX)"
trap 'rm -rf -- "$probe_dir"' EXIT

bsdtar -xf "$BOXWARDEN_ALPHA_UBUNTU_ISO" -C "$probe_dir" casper/vmlinuz casper/initrd
python3 - "$probe_dir/synthetic.raw" <<'PY'
import os
import sys

fd = os.open(sys.argv[1], os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600)
try:
    os.ftruncate(fd, 8 * 1024 * 1024)
finally:
    os.close(fd)
PY

swiftc -module-cache-path "$probe_dir/module-cache" \
  -o "$probe_dir/alpha-inspector" "$script_dir/main.swift" "$script_dir/boot.swift"
codesign --force --sign - --entitlements "$script_dir/virtualization.entitlements" \
  "$probe_dir/alpha-inspector"
codesign --verify --strict "$probe_dir/alpha-inspector"

"$probe_dir/alpha-inspector" probe \
  "$probe_dir/casper/vmlinuz" \
  "$probe_dir/casper/initrd" \
  "$probe_dir/synthetic.raw" > "$probe_dir/evidence.json"

python3 - "$probe_dir/evidence.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    result = json.load(handle)
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
for key, value in expected.items():
    if result.get(key) != value:
        raise SystemExit(f"{key}: expected {value!r}, got {result.get(key)!r}")
print(json.dumps(result, sort_keys=True))
PY
