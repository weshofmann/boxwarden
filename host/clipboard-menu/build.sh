#!/bin/bash
set -euo pipefail
base="$(cd "$(dirname "$0")/../.." && pwd -P)"
output="$base/.build/Boxwarden Clipboard.app"
stage="$(mktemp -d "${TMPDIR:-/private/tmp}/boxwarden-menu.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
app="$stage/Boxwarden Clipboard.app"
mkdir -p "$app/Contents/MacOS"
cp "$base/host/clipboard-menu/Info.plist" "$app/Contents/Info.plist"
(
  cd "$base"
  go build -trimpath -o "$app/Contents/MacOS/boxwarden" ./cmd/boxwarden
)
swiftc -O -framework AppKit \
  "$base/host/clipboard-menu/Sources/MenuModel.swift" \
  "$base/host/clipboard-menu/Sources/CLIClient.swift" \
  "$base/host/clipboard-menu/Sources/MenuApp.swift" \
  -o "$app/Contents/MacOS/Boxwarden Clipboard"
codesign --force --sign - "$app/Contents/MacOS/boxwarden"
codesign --force --sign - "$app"
mkdir -p "$(dirname "$output")"
if [[ -e "$output" || -L "$output" ]]; then
  if [[ ! -d "$output" || -L "$output" ]]; then
    echo "existing output is not an app directory: $output" >&2
    exit 1
  fi
  if ! rm -rf -- "$output"; then
    echo "could not remove previous app: $output" >&2
    exit 1
  fi
  if [[ -e "$output" || -L "$output" ]]; then
    echo "previous app remains at output path: $output" >&2
    exit 1
  fi
fi
mv "$app" "$output"
if [[ ! -f "$output/Contents/Info.plist" || ! -x "$output/Contents/MacOS/Boxwarden Clipboard" || ! -x "$output/Contents/MacOS/boxwarden" ]]; then
  echo "staged app bundle is incomplete: $output" >&2
  exit 1
fi
codesign --verify --strict "$output"
echo "$output"
