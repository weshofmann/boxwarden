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
