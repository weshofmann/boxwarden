#!/bin/sh
# Inputs must be an exact disposable pinned Tart checkout and a separate stage.
set -eu
[ "$#" -eq 2 ] || { echo 'usage: build.sh SOURCE_CHECKOUT STAGE_DIRECTORY' >&2; exit 2; }
base=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source_tree=$(CDPATH= cd -- "$1" && pwd)
stage=$2
[ ! -e "$stage.tar.gz" ] || { echo 'stage archive must not exist' >&2; exit 2; }
[ ! -e "$stage.tar.gz.sha256" ] || { echo 'archive identity must not exist' >&2; exit 2; }
[ ! -e "$stage" ] || { echo 'stage must not exist' >&2; exit 2; }
[ "$(git -C "$source_tree" rev-parse HEAD)" = 8aa377b71ebfd90b2df9803d3e20033f58d6800c ]
[ "$(shasum -a 256 "$source_tree/LICENSE" | cut -d ' ' -f 1)" = 2b20b2bd5ed91350b37b724bbf99e6ccbeefc5b4779578f375de47c687f60055 ]
[ "$(shasum -a 256 "$source_tree/Package.resolved" | cut -d ' ' -f 1)" = 633b12e9adb9f5863783a28fce2a33fe51aa5f599cfb7c1d1aae775db88314ba ]
# Require a fresh source baseline; never adopt unrelated source modifications.
[ -z "$(git -C "$source_tree" status --porcelain --untracked-files=all)" ]
git -C "$source_tree" apply --unidiff-zero --check "$base/tart-2.32.1.patch"
git -C "$source_tree" apply --unidiff-zero "$base/tart-2.32.1.patch"
(cd "$source_tree" && swift build -c release --disable-automatic-resolution)
[ "$(shasum -a 256 "$source_tree/Package.resolved" | cut -d ' ' -f 1)" = 633b12e9adb9f5863783a28fce2a33fe51aa5f599cfb7c1d1aae775db88314ba ]
mkdir -p "$stage"
cp "$source_tree/.build/release/tart" "$stage/tart-boxwarden-clipboard"
cp "$source_tree/LICENSE" "$stage/Tart-LICENSE"
"$base/sign.sh" "$stage/tart-boxwarden-clipboard" "$stage"
cp "$base/boxwarden.entitlements" "$stage/boxwarden.entitlements"
"$stage/tart-boxwarden-clipboard" --version > "$stage/version.txt"
swift --version > "$stage/swift-version.txt"
codesign -dvv "$stage/tart-boxwarden-clipboard" 2> "$stage/signing.txt"
shasum -a 256 "$base/tart-2.32.1.patch" "$source_tree/Package.resolved" "$base/boxwarden.entitlements" "$stage/signed-entitlements.plist" "$stage/tart-boxwarden-clipboard" > "$stage/identity.sha256"
python3 "$base/package.py" "$stage" "$stage.tar.gz"
