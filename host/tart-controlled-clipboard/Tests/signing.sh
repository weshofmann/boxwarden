#!/bin/sh
# Exercises real ad-hoc signing on a synthetic executable; no VM or clipboard.
set -eu
base=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
temporary=$(mktemp -d "${TMPDIR:-/private/tmp}/boxwarden-signing-tests.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
printf 'print("synthetic signing fixture")\n' > "$temporary/main.swift"
swiftc "$temporary/main.swift" -o "$temporary/fixture"
sh "$base/sign.sh" "$temporary/fixture" "$temporary"
python3 - "$temporary/signed-entitlements.plist" <<'PY'
import plistlib,sys
try:
    with open(sys.argv[1], 'rb') as f:
        got=plistlib.load(f)
except Exception:
    print('FAIL: staged signature lacks required virtualization entitlement')
    sys.exit(1)
if got != {'com.apple.security.virtualization': True}:
    print('FAIL: signature must contain only required virtualization entitlement')
    sys.exit(1)
print('PASS: actual signature has virtualization without debug/restricted networking')
PY
