#!/bin/bash
set -euo pipefail
umask 077
operation=setup
if [[ ${1:-} == --update ]]; then
  operation=setup-update
  shift
fi
if [[ $# != 5 && $# != 7 ]]; then
  echo 'usage: bash prepare-projects.sh [--update] /absolute/config.json /absolute/ubuntu.iso /absolute/e2fsck-static.deb /absolute/go /absolute/zstd [/absolute/openssl /absolute/xorriso]' >&2
  exit 2
fi
package="$(cd "$(dirname "$0")" && pwd -P)"
config=$1
iso=$2
checker=$3
go_bin=$4
zstd_bin=$5
for path in "$config" "$iso" "$checker" "$go_bin" "$zstd_bin"; do
  if [[ "$path" != /* || ! -f "$path" ]]; then
    echo 'all five inputs must be existing absolute files' >&2
    exit 2
  fi
done
if [[ ! -x "$go_bin" || -L "$go_bin" || $(basename "$go_bin") != go ]]; then
  echo 'supply the actual Go executable, not a shim' >&2
  exit 2
fi
if [[ ! -x "$zstd_bin" || -L "$zstd_bin" || $(basename "$zstd_bin") != zstd ]]; then
  echo 'supply the actual zstd executable, not a shim' >&2
  exit 2
fi
export PATH="$(dirname "$go_bin"):$(dirname "$zstd_bin"):/usr/bin:/bin"
export GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
setup_args=(--source-root "$package/support/source" --formatter-bundle "$package/formatter" --iso "$iso" --go "$go_bin")
if [[ $# == 7 ]]; then
  openssl_bin=$6
  xorriso_bin=$7
  for name in openssl xorriso; do
    if [[ "$name" == openssl ]]; then path=$openssl_bin; else path=$xorriso_bin; fi
    if [[ "$path" != /* || ! -f "$path" || ! -x "$path" || -L "$path" || $(basename "$path") != "$name" ]]; then
      printf 'supply the exact existing non-symlink %s executable; no tool is installed automatically\n' "$name" >&2
      exit 2
    fi
  done
  printf 'Checking exact recipe preparation tools before formatter preparation...\n'
  # Probe only synthetic input. Apple /usr/bin/openssl lacks this capability.
  expected_probe='$6$boxwarden-prereq$bsvT6K3VcjFnqFANCjJcS./f/0oensl45IiNphHg.TT7aDUUsaKlss3qt2ek23fOdujPaG.Lttdp6kxOGRqq50'
  if ! openssl_probe=$("$openssl_bin" passwd -6 -salt boxwarden-prerequisite synthetic-prerequisite) || [[ "$openssl_probe" != "$expected_probe" ]]; then
    echo 'OpenSSL must support SHA-512 passwd (-6); supply an already-installed compatible openssl executable' >&2
    exit 2
  fi
  if ! xorriso_probe=$("$xorriso_bin" -version 2>&1) || [[ ! "$xorriso_probe" =~ xorriso[[:space:]]version[[:space:]]*: ]]; then
    echo 'xorriso -version did not report a usable xorriso; supply the exact already-installed executable' >&2
    exit 2
  fi
  tool_hashes=$(python3 - "$openssl_bin" "$xorriso_bin" <<'PYHASH'
import hashlib, sys
values = []
for path in sys.argv[1:]:
    digest = hashlib.sha256()
    with open(path, "rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    values.append(digest.hexdigest())
print(" ".join(values))
PYHASH
)
  read -r openssl_sha256 xorriso_sha256 <<< "$tool_hashes"
  setup_args+=(--openssl "$openssl_bin" --openssl-sha256 "$openssl_sha256" --xorriso "$xorriso_bin" --xorriso-sha256 "$xorriso_sha256")
fi
formatter="$package/formatter"
if [[ ! -e "$formatter" && ! -L "$formatter" ]]; then
  printf 'Preparing formatter assets; pinned ISO/checker admission runs before extraction or compilation...\n'
  log=$(mktemp /private/tmp/boxwarden-beta-formatter.XXXXXX)
  candidate=""
  trap 'rm -f -- "$log"; if [[ -n "$candidate" ]]; then rm -rf -- "$candidate"; fi' EXIT
  (
    cd "$package/support/source"
    bash tools/alpha-formatter/prepare_boot.sh "$iso" "$checker" "$config" alpha
  ) | tee "$log"
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
  printf 'Formatter assets prepared.\n'
else
  printf 'Using prepared formatter assets.\n'
fi
printf 'Recording project %s...\n' "$operation"
"$package/bin/boxwarden" --config "$config" --domain alpha project "$operation" "${setup_args[@]}"
