#!/bin/sh
set -eu
base=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
temporary=$(mktemp -d "${TMPDIR:-/private/tmp}/boxwarden-ui-tests.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
swiftc -typecheck "$base/Sources/ClipboardInvocation.swift" "$base/Sources/ClipboardUI.swift" "$base/Tests/UIStubs.swift"
swiftc -parse-as-library "$base/Sources/ClipboardInvocation.swift" "$base/Tests/main.swift" -o "$temporary/tests"
"$temporary/tests"
swiftc -parse-as-library "$base/Sources/ClipboardInvocation.swift" "$base/Tests/shutdown-main.swift" -o "$temporary/shutdown-tests"
"$temporary/shutdown-tests"
swiftc -parse-as-library "$base/Sources/ClipboardInvocation.swift" "$base/Tests/signal-main.swift" -o "$temporary/signal-tests"
python3 "$base/Tests/signals-test.py" "$temporary/signal-tests"
"$base/Tests/signing.sh"
python3 "$base/Tests/package-test.py"
