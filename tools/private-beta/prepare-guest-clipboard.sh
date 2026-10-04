#!/bin/bash
set -euo pipefail
umask 077
if [[ $# != 2 ]]; then
  echo 'usage: bash prepare-guest-clipboard.sh /absolute/actual/go /absolute/NEW-PRIVATE-SOURCE' >&2
  exit 2
fi
go_bin=$1
output=$2
if [[ -e "$output" || -L "$output" ]]; then
  echo 'output must not exist' >&2
  exit 2
fi
if [[ "$output" != /* || ! -d "$(dirname "$output")" ]]; then
  echo 'output requires an absolute path with an existing parent' >&2
  exit 2
fi
if [[ "$go_bin" != /* || ! -f "$go_bin" || ! -x "$go_bin" || -L "$go_bin" || $(basename "$go_bin") != go ]]; then
  echo 'supply the actual absolute Go executable, not a shim' >&2
  exit 2
fi
package="$(cd "$(dirname "$0")" && pwd -P)"
source="$package/support/source"
if [[ ! -f "$source/go.mod" || ! -f "$source/guest/ubuntu-24.04-arm64/clipboard.py" ]]; then
  echo 'run the script shipped beside the package support/source directory' >&2
  exit 2
fi
export PATH="$(dirname "$go_bin"):/usr/bin:/bin"
export GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
if [[ $("$go_bin" version) != 'go version go1.27.0 '* ]]; then
  echo 'guest helper preparation requires Go 1.27.0' >&2
  exit 2
fi
temporary=$(mktemp -d /private/tmp/boxwarden-beta-guest-clipboard.XXXXXX)
trap 'rm -rf -- "$temporary"' EXIT
export GOCACHE="$temporary/gocache" GOMODCACHE="$temporary/modcache"
assets="$temporary/assets"
mkdir -m 700 "$assets"
(
  cd "$source"
  # Match guest/ubuntu-24.04-arm64/artifacts.lock.json; do not edit shipped assets.
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 "$go_bin" build -mod=readonly -trimpath \
    -buildvcs=false -ldflags=-buildid= -o "$temporary/boxwarden-guest-bootstrap" ./cmd/boxwarden-guest-bootstrap
)
(
  cd "$temporary"
  shasum -a 256 boxwarden-guest-bootstrap > "$assets/BOOTSTRAP.sha256"
)
# The existing explicit importer admits at most 4 MiB per file. Compress the
# generic helper, never expand that transfer boundary or ship compiler caches.
gzip -n -c "$temporary/boxwarden-guest-bootstrap" > "$assets/boxwarden-guest-bootstrap.gz"
cp "$source/guest/ubuntu-24.04-arm64/clipboard.py" "$assets/boxwarden-guest-clipboard.py"
chmod 600 "$assets/boxwarden-guest-bootstrap.gz" "$assets/boxwarden-guest-clipboard.py" "$assets/BOOTSTRAP.sha256"
for name in boxwarden-guest-bootstrap.gz boxwarden-guest-clipboard.py BOOTSTRAP.sha256; do
  if [[ $(wc -c < "$assets/$name") -gt $((4 * 1024 * 1024)) ]]; then
    echo 'prepared asset exceeds the existing 4 MiB importer per-file limit' >&2
    exit 1
  fi
done
(
  cd "$assets"
  shasum -a 256 boxwarden-guest-bootstrap.gz boxwarden-guest-clipboard.py BOOTSTRAP.sha256 > SHA256SUMS
  shasum -a 256 -c SHA256SUMS > /dev/null
)
# Reserve the exact new directory only after preparation succeeds. Never replace
# an existing source directory or publish compiler caches as imported guest data.
mkdir -m 700 "$output"
for name in boxwarden-guest-bootstrap.gz boxwarden-guest-clipboard.py BOOTSTRAP.sha256 SHA256SUMS; do
  mv "$assets/$name" "$output/$name"
done
printf 'guest clipboard source: %s\n' "$output"
printf 'Import this directory explicitly with workspace import; this script installs nothing.\n'
