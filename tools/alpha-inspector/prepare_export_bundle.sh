#!/bin/bash
set -euo pipefail
umask 077

# Prepare private source artifacts for one export transaction. This does not
# launch a VM, attach a managed disk, or publish files to the destination.
if [[ "$#" != 2 || ! -f "$1" || ! -f "$2" ]]; then
  echo 'usage: prepare_export_bundle.sh <verified Ubuntu 24.04.4 ARM64 ISO> <private journal-derived request.json>' >&2
  exit 2
fi
iso=$1
request=$2
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
qualification=production
if [[ -n "$(git -C "$repo_root" status --porcelain --untracked-files=all)" ]]; then
  if [[ "${BOXWARDEN_ALPHA_BUNDLE_TEST_ONLY:-}" != 1 ]]; then
    echo 'the alpha worktree must be committed before preparing an export inspector bundle' >&2
    exit 1
  fi
  qualification=test-only-uncommitted
fi
source_commit="$(git -C "$repo_root" rev-parse HEAD)"
expected_iso=c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe
read -r actual_iso _ < <(shasum -a 256 "$iso")
if [[ "$actual_iso" != "$expected_iso" ]]; then
  echo 'inspector ISO source digest mismatch' >&2
  exit 1
fi

output_dir="$(mktemp -d /private/tmp/boxwarden-alpha-inspector-export.XXXXXX)"
prepared=0
trap 'if [[ "$prepared" != 1 ]]; then rm -rf -- "$output_dir"; fi' EXIT
python3 - "$script_dir" "$request" "$output_dir/request.json" <<'PY'
import os
from pathlib import Path
import stat
import sys

sys.path.insert(0, sys.argv[1])
from pack_initramfs import MAX_REQUEST_BYTES, _check_no_acl

source = Path(sys.argv[2])
destination = Path(sys.argv[3])
if not source.is_absolute():
    raise SystemExit("inspector request path must be absolute")
descriptor = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
with os.fdopen(descriptor, "rb") as opened:
    before = os.fstat(opened.fileno())
    if (not stat.S_ISREG(before.st_mode) or before.st_uid != os.getuid()
            or before.st_nlink != 1 or stat.S_IMODE(before.st_mode) != 0o600
            or not 0 < before.st_size <= MAX_REQUEST_BYTES):
        raise SystemExit("inspector request is not exact private data")
    _check_no_acl(source)
    raw = opened.read(MAX_REQUEST_BYTES + 1)
    after = os.fstat(opened.fileno())
    path = source.lstat()
    if (len(raw) != before.st_size or
            (before.st_dev, before.st_ino, before.st_size, before.st_mode) !=
            (after.st_dev, after.st_ino, after.st_size, after.st_mode) or
            (before.st_dev, before.st_ino, before.st_size, before.st_mode) !=
            (path.st_dev, path.st_ino, path.st_size, path.st_mode)):
        raise SystemExit("inspector request changed while copying")
out_fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
with os.fdopen(out_fd, "wb") as output:
    output.write(raw)
    output.flush()
    os.fsync(output.fileno())
PY

bsdtar -xf "$iso" -C "$output_dir" casper/vmlinuz casper/initrd
python3 "$script_dir/kernel_image.py" \
  "$output_dir/casper/vmlinuz" "$output_dir/kernel-image" \
  000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617 \
  a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d
go_binary="$(command -v go)"
(
  cd "$repo_root"
  env -i PATH=/usr/bin:/bin GOCACHE="$output_dir/gocache" GOMODCACHE="$output_dir/modcache" \
    GOTOOLCHAIN=local GOENV=off GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    "$go_binary" build -o "$output_dir/alpha-probe" ./tools/alpha-inspector/guest
)
python3 "$script_dir/pack_initramfs.py" \
  "$output_dir/casper/initrd" "$output_dir/alpha-probe" \
  "$output_dir/inspector-initrd" "$output_dir/request.json"
swiftc -module-cache-path "$output_dir/swift-cache" \
  -o "$output_dir/alpha-inspector" "$script_dir/main.swift" "$script_dir/boot.swift"
codesign --force --sign - --entitlements "$script_dir/virtualization.entitlements" \
  "$output_dir/alpha-inspector"
codesign --verify --strict "$output_dir/alpha-inspector"

python3 - "$output_dir" "$repo_root" "$source_commit" "$expected_iso" "$qualification" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import stat
import subprocess
import sys

root = Path(sys.argv[1])
source_root = Path(sys.argv[2])
source_commit, iso_digest, qualification = sys.argv[3:]
runner = root / "alpha-inspector"
result = subprocess.run(
    ["codesign", "-d", "--entitlements", ":-", str(runner)],
    capture_output=True, check=True, timeout=10,
)
if plistlib.loads(result.stdout) != {"com.apple.security.virtualization": True}:
    raise SystemExit("inspector helper has unexpected entitlements")

def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()

names = ("casper/vmlinuz", "casper/initrd", "kernel-image", "alpha-probe",
         "inspector-initrd", "alpha-inspector", "request.json")
files = {name: digest(root / name) for name in names}
required_sources = {
    "go.mod", "tools/alpha-inspector/main.swift",
    "tools/alpha-inspector/boot.swift",
    "tools/alpha-inspector/virtualization.entitlements",
    "tools/alpha-inspector/pack_initramfs.py",
    "tools/alpha-inspector/kernel_image.py",
    "tools/alpha-inspector/prepare_export_bundle.sh",
}
tracked = subprocess.run(
    ["git", "-C", str(source_root), "ls-files", "-z", "--", *sorted(required_sources),
     "tools/alpha-inspector/guest"], capture_output=True, check=True, timeout=10,
).stdout
source_names = [os.fsdecode(name) for name in tracked.split(b"\x00") if name]
if (len(source_names) > 64 or len(source_names) != len(set(source_names))
        or not required_sources.issubset(source_names)
        or not any(name.startswith("tools/alpha-inspector/guest/") for name in source_names)
        or any(not (name in required_sources or
                    name.startswith("tools/alpha-inspector/guest/") and name.endswith(".go"))
               for name in source_names)):
    raise SystemExit("inspector tracked source inventory differs from build inputs")
source_inputs = {}
for name in source_names:
    path = source_root / name
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        raise SystemExit("inspector tracked source is not a one-link regular file")
    source_inputs[name] = digest(path)
if (files["casper/vmlinuz"] != "000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617"
        or files["kernel-image"] != "a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d"):
    raise SystemExit("inspector kernel digest differs from verified source")
manifest = {
    "version": 1, "source_commit": source_commit, "iso_sha256": iso_digest,
    "files": files, "runner_entitlements": {"com.apple.security.virtualization": True},
    "qualification": qualification, "source_inputs": source_inputs,
}
descriptor = os.open(root / "manifest.json", os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
with os.fdopen(descriptor, "w", encoding="utf-8") as output:
    json.dump(manifest, output, sort_keys=True)
    output.write("\n")
    output.flush()
    os.fsync(output.fileno())
for name in ("gocache", "modcache", "swift-cache"):
    shutil.rmtree(root / name, ignore_errors=True)
directory_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
try:
    os.fsync(directory_fd)
finally:
    os.close(directory_fd)
PY
printf 'prepared private export inspector artifacts: %s\n' "$output_dir"
prepared=1
