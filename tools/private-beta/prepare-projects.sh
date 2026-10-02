#!/bin/bash
set -euo pipefail
umask 077
if [[ $# != 4 ]]; then
  echo 'usage: bash prepare-projects.sh /absolute/config.json /absolute/ubuntu.iso /absolute/e2fsck-static.deb /absolute/go' >&2
  exit 2
fi
package="$(cd "$(dirname "$0")" && pwd -P)"
config=$1
iso=$2
checker=$3
go_bin=$4
for path in "$config" "$iso" "$checker" "$go_bin"; do
  if [[ "$path" != /* || ! -f "$path" ]]; then
    echo 'all four inputs must be existing absolute files' >&2
    exit 2
  fi
done
if [[ ! -x "$go_bin" || -L "$go_bin" || $(basename "$go_bin") != go ]]; then
  echo 'supply the actual Go executable, not a shim' >&2
  exit 2
fi
export PATH="$(dirname "$go_bin"):/usr/bin:/bin"
export GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
formatter="$package/formatter"
if [[ ! -e "$formatter" && ! -L "$formatter" ]]; then
  log=$(mktemp /private/tmp/boxwarden-beta-formatter.XXXXXX)
  candidate=""
  trap 'rm -f -- "$log"; if [[ -n "$candidate" ]]; then rm -rf -- "$candidate"; fi' EXIT
  bash "$package/support/source/tools/alpha-formatter/prepare_boot.sh" "$iso" "$checker" "$config" alpha | tee "$log"
  prepared=$(sed -n 's/^prepared formatter boot artifacts: //p' "$log")
  if [[ ! "$prepared" =~ ^/private/tmp/boxwarden-alpha-formatter\.[A-Za-z0-9]+$ || ! -d "$prepared" ]]; then
    echo 'formatter preparation did not return its private output' >&2
    exit 1
  fi
  # Retain only the six admitted artifacts and manifest; compiler caches are disposable.
  candidate=$(mktemp -d "$package/.formatter.XXXXXX")
  for file in kernel-image formatter-initrd alpha-formatter e2fsck.static alpha-formatter-host binding.swift manifest.json; do
    cp "$prepared/$file" "$candidate/$file"
    cmp "$prepared/$file" "$candidate/$file"
  done
  chmod -R go-rwx "$candidate"
  # Darwin's exclusive directory rename preserves any target appearing during
  # preparation. Publish only a complete bundle, never a partial retry marker.
  python3 - "$candidate" "$formatter" <<'PY'
import ctypes, os, sys
libc = ctypes.CDLL(None, use_errno=True)
rename = libc.renamex_np
rename.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_uint]
rename.restype = ctypes.c_int
if rename(os.fsencode(sys.argv[1]), os.fsencode(sys.argv[2]), 0x4):
    raise OSError(ctypes.get_errno(), "publish formatter without replacement")
PY
  candidate=""
  rm -rf -- "$prepared"
fi
"$package/bin/boxwarden" --config "$config" --domain alpha project setup \
  --source-root "$package/support/source" --formatter-bundle "$formatter" --iso "$iso" --go "$go_bin"
