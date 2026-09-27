#!/bin/sh
set -eu
[ "$#" -eq 2 ] || { echo 'usage: sign.sh STAGED_EXECUTABLE METADATA_DIRECTORY' >&2; exit 2; }
base=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
codesign --force --sign - --entitlements "$base/boxwarden.entitlements" --identifier org.boxwarden.tart.controlled-clipboard "$1"
codesign --verify --strict "$1"
codesign -d --entitlements :- "$1" > "$2/signed-entitlements.plist" 2> "$2/entitlements-inspection.txt"
python3 - "$2/signed-entitlements.plist" <<'CHECK'
import plistlib, sys
with open(sys.argv[1], 'rb') as f:
    actual = plistlib.load(f)
if actual != {'com.apple.security.virtualization': True}:
    sys.exit('staged signature entitlement mismatch')
CHECK
