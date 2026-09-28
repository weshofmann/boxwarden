#!/bin/bash
set -euo pipefail
base="$(cd "$(dirname "$0")/../.." && pwd -P)"
temporary="$(mktemp -d "${TMPDIR:-/private/tmp}/boxwarden-menu-tests.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT
swiftc -parse-as-library "$base/host/clipboard-menu/Sources/MenuModel.swift" \
  "$base/host/clipboard-menu/Tests/MenuModelTests.swift" -o "$temporary/model-tests"
"$temporary/model-tests"
swiftc -parse-as-library "$base/host/clipboard-menu/Sources/MenuModel.swift" \
  "$base/host/clipboard-menu/Sources/CLIClient.swift" \
  "$base/host/clipboard-menu/Tests/CLIClientTests.swift" -o "$temporary/client-tests"
"$temporary/client-tests"
swiftc -typecheck "$base/host/clipboard-menu/Sources/MenuModel.swift" \
  "$base/host/clipboard-menu/Sources/CLIClient.swift" \
  "$base/host/clipboard-menu/Sources/MenuApp.swift"
swiftc -D MENU_TEST -parse-as-library -framework AppKit \
  "$base/host/clipboard-menu/Sources/MenuModel.swift" \
  "$base/host/clipboard-menu/Sources/CLIClient.swift" \
  "$base/host/clipboard-menu/Sources/MenuApp.swift" \
  "$base/host/clipboard-menu/Tests/MenuValidationTests.swift" -o "$temporary/menu-validation-tests"
"$temporary/menu-validation-tests"
