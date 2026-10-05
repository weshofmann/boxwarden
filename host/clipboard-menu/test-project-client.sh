#!/bin/bash
set -euo pipefail
base="$(cd "$(dirname "$0")/../.." && pwd -P)"
temporary="$(mktemp -d "${TMPDIR:-/private/tmp}/boxwarden-project-client-tests.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT
swiftc -parse-as-library "$base/host/clipboard-menu/Sources/ProjectProtocol.swift" \
  "$base/host/clipboard-menu/Sources/ProjectActivity.swift" \
  "$base/host/clipboard-menu/Sources/ProjectClient.swift" \
  "$base/host/clipboard-menu/Tests/ProjectClientTests.swift" -o "$temporary/tests"
"$temporary/tests"

swiftc -parse-as-library "$base/host/clipboard-menu/Sources/ProjectProtocol.swift" \
  "$base/host/clipboard-menu/Sources/ProjectActivity.swift" \
  "$base/host/clipboard-menu/Sources/ProjectClient.swift" \
  "$base/host/clipboard-menu/Tests/ProjectFirstRunTests.swift" -o "$temporary/first-run-tests"
"$temporary/first-run-tests"
