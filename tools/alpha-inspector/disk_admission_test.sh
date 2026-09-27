#!/bin/bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
fixture_dir="$(mktemp -d /private/tmp/boxwarden-inspector-fixture.XXXXXX)"
boot_dir="$(mktemp -d /private/tmp/boxwarden-inspector-boot.XXXXXX)"
trap 'rm -rf -- "$fixture_dir" "$boot_dir"' EXIT

python3 - "$fixture_dir/source.raw" <<'PY'
import os
import sys

descriptor = os.open(sys.argv[1], os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600)
try:
    os.ftruncate(descriptor, 64 * 1024 * 1024)
finally:
    os.close(descriptor)
PY
ln "$fixture_dir/source.raw" "$boot_dir/synthetic.raw"
swiftc -module-cache-path "$fixture_dir/swift-cache" \
  -o "$fixture_dir/alpha-inspector" "$script_dir/main.swift" "$script_dir/boot.swift"

if "$fixture_dir/alpha-inspector" preflight-copy \
  "$fixture_dir/missing-kernel" "$fixture_dir/missing-initrd" \
  "$boot_dir/synthetic.raw" 0123456789abcdef0123456789abcdef \
  > "$fixture_dir/stdout" 2> "$fixture_dir/stderr"; then
  echo 'hardlinked disk unexpectedly passed copy admission' >&2
  exit 1
fi
if ! grep -Fq 'probe accepts only' "$fixture_dir/stderr"; then
  echo 'hardlinked disk failed for a reason other than unsafe disk admission' >&2
  exit 1
fi

rm "$boot_dir/synthetic.raw"
cp "$fixture_dir/source.raw" "$boot_dir/synthetic.raw"
chmod 600 "$boot_dir/synthetic.raw"
"$fixture_dir/alpha-inspector" preflight-copy \
  "$fixture_dir/missing-kernel" "$fixture_dir/missing-initrd" \
  "$boot_dir/synthetic.raw" 0123456789abcdef0123456789abcdef \
  > "$fixture_dir/control-stdout" 2> "$fixture_dir/control-stderr" || true
if grep -Fq 'probe accepts only' "$fixture_dir/control-stderr"; then
  echo 'private one-link disk was incorrectly rejected at copy admission' >&2
  exit 1
fi

echo 'hardlinked copy rejected; private one-link control passed disk admission'

export_root="$fixture_dir/exports"
export_dir="$export_root/01234567-89ab-cdef-0123-456789abcdef"
mkdir -m 700 -p "$export_dir"
chmod 700 "$export_root" "$export_dir"
snapshot="$export_dir/snapshot.raw"
cp "$fixture_dir/source.raw" "$snapshot"
chmod 600 "$snapshot"
device="$(stat -f %d "$snapshot")"
inode="$(stat -f %i "$snapshot")"
expect_export_rejected() {
  if "$fixture_dir/alpha-inspector" preflight-export \
      "$fixture_dir/missing-kernel" "$fixture_dir/missing-initrd" \
      "$snapshot" 0123456789abcdef0123456789abcdef \
      "$device" "$inode" 67108864 \
      > "$fixture_dir/export-stdout" 2> "$fixture_dir/export-stderr"; then
    echo 'unsafe export snapshot unexpectedly passed preflight' >&2
    exit 1
  fi
  if ! grep -Fq 'probe accepts only' "$fixture_dir/export-stderr"; then
    echo 'unsafe export snapshot failed for a reason other than disk admission' >&2
    exit 1
  fi
}
expect_export_rejected_with_wrong_inode() {
  inode="$((inode + 1))"
  expect_export_rejected
  inode="$((inode - 1))"
}
expect_export_rejected_with_wrong_inode
wrong_tx_snapshot="$export_root/12345678-1234-1234-1234-123456789abc/snapshot.raw"
mkdir -m 700 "$(dirname "$wrong_tx_snapshot")"
mv "$snapshot" "$wrong_tx_snapshot"
snapshot="$wrong_tx_snapshot"
expect_export_rejected
mv "$snapshot" "$export_dir/snapshot.raw"
snapshot="$export_dir/snapshot.raw"
ln "$snapshot" "$export_dir/linked.raw"
expect_export_rejected
rm "$export_dir/linked.raw"
chmod 755 "$export_dir"
expect_export_rejected
chmod 700 "$export_dir"
"$fixture_dir/alpha-inspector" preflight-export \
  "$fixture_dir/missing-kernel" "$fixture_dir/missing-initrd" \
  "$snapshot" 0123456789abcdef0123456789abcdef \
  "$device" "$inode" 67108864 \
  > "$fixture_dir/export-control-stdout" 2> "$fixture_dir/export-control-stderr" || true
if grep -Fq 'probe accepts only' "$fixture_dir/export-control-stderr"; then
  echo 'private exact one-link export snapshot was incorrectly rejected' >&2
  exit 1
fi

echo 'export identity, transaction, hardlink, and parent-mode drift rejected; exact private control passed disk admission'
