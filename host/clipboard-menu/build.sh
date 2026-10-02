#!/bin/bash
set -euo pipefail
base="$(cd "$(dirname "$0")/../.." && pwd -P)"
output="$base/.build/Boxwarden Clipboard.app"
custom_output=false
cli=""
version=""
build=""
fail() { echo "$*" >&2; exit 1; }
while [[ $# -gt 0 ]]; do
  [[ $# -ge 2 && -n "$2" ]] || fail "expected a value for $1"
  case "$1" in
    --cli) [[ -z "$cli" ]] || fail "duplicate --cli"; cli="$2" ;;
    --output) [[ "$custom_output" == false ]] || fail "duplicate --output"; output="$2"; custom_output=true ;;
    --version) [[ -z "$version" ]] || fail "duplicate --version"; version="$2" ;;
    --build) [[ -z "$build" ]] || fail "duplicate --build"; build="$2" ;;
    *) fail "unknown argument: $1" ;;
  esac
  shift 2
done
number='(0|[1-9][0-9]*)'
identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
semver="^$number\\.$number\\.$number(-$identifier(\\.$identifier)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"
[[ -z "$version" || "$version" =~ $semver ]] || fail "--version requires SemVer without a v prefix"
[[ -z "$build" || "$build" =~ ^[0-9]+$ ]] || fail "--build requires a numeric build number"
[[ "$output" == /* && "$output" == *.app ]] || fail "--output requires an absolute .app path"
if [[ "$custom_output" == true && ( -e "$output" || -L "$output" ) ]]; then
  fail "custom output already exists: $output"
fi
[[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]] || fail "build requires native macOS arm64"
if [[ -n "$cli" ]]; then
  [[ "$cli" == /* && -f "$cli" && -x "$cli" ]] || fail "--cli requires an absolute executable file"
  [[ "$(/usr/bin/file -b "$cli")" == *Mach-O*executable* && "$(/usr/bin/lipo -archs "$cli")" == arm64 ]] || fail "--cli requires a native Mach-O arm64 executable"
  /usr/bin/codesign --verify --strict "$cli" || fail "--cli signature verification failed"
fi
stage="$(mktemp -d "${TMPDIR:-/private/tmp}/boxwarden-menu.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
app="$stage/Boxwarden Clipboard.app"
mkdir -p "$app/Contents/MacOS"
cp "$base/host/clipboard-menu/Info.plist" "$app/Contents/Info.plist"
if [[ -n "$version" ]]; then
  /usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $version" "$app/Contents/Info.plist"
fi
if [[ -n "$build" ]]; then
  /usr/libexec/PlistBuddy -c "Set :CFBundleVersion $build" "$app/Contents/Info.plist"
fi
if [[ -n "$cli" ]]; then
  cp "$cli" "$app/Contents/MacOS/boxwarden"
else
  (
    cd "$base"
    CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -o "$app/Contents/MacOS/boxwarden" ./cmd/boxwarden
  )
fi
swiftc -O -target arm64-apple-macos13.0 -module-cache-path "$stage/module-cache" -framework AppKit \
  "$base/host/clipboard-menu/Sources/MenuModel.swift" \
  "$base/host/clipboard-menu/Sources/CLIClient.swift" \
  "$base/host/clipboard-menu/Sources/MenuApp.swift" \
  -o "$app/Contents/MacOS/Boxwarden Clipboard"
if [[ -z "$cli" ]]; then
  codesign --force --sign - "$app/Contents/MacOS/boxwarden"
fi
codesign --force --sign - "$app"
mkdir -p "$(dirname "$output")"
if [[ -e "$output" || -L "$output" ]]; then
  [[ "$custom_output" == false ]] || fail "custom output appeared during build: $output"
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
# Reserve a custom destination with an exclusive mkdir, so an app that appears
# during compilation cannot become an mv destination. The default is rebuildable.
if [[ "$custom_output" == true ]]; then
  mkdir "$output" || fail "could not reserve new custom output: $output"
  mv "$app/Contents" "$output/Contents"
else
  mv "$app" "$output"
fi
if [[ ! -f "$output/Contents/Info.plist" || ! -x "$output/Contents/MacOS/Boxwarden Clipboard" || ! -x "$output/Contents/MacOS/boxwarden" ]]; then
  echo "staged app bundle is incomplete: $output" >&2
  exit 1
fi
codesign --verify --strict "$output"
echo "$output"
