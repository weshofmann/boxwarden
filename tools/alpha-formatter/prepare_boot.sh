#!/bin/bash
set -euo pipefail
umask 077

# Prepare source artifacts only. This script never starts a VM or touches a disk.
if [[ "$#" != 2 || ! -f "$1" || ! -f "$2" ]]; then
  echo 'usage: prepare_boot.sh <verified Ubuntu 24.04.4 ARM64 ISO> <e2fsck-static arm64 deb>' >&2
  exit 2
fi
iso=$1
checker_deb=$2
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/../.." && pwd)"
expected_iso=c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe
expected_deb=0a3d402fd7c7c07f63b8104d84a47378f57022e7c816190e6cc99950492d8cae
expected_checker=e5e8f8b30641fab6e3f402c9746ce0537ad95c528e96cde2e446ca0968e63279
read -r actual_iso _ < <(shasum -a 256 "$iso")
read -r actual_deb _ < <(shasum -a 256 "$checker_deb")
if [[ "$actual_iso" != "$expected_iso" || "$actual_deb" != "$expected_deb" ]]; then
  echo 'formatter source digest mismatch' >&2
  exit 1
fi

output_dir="$(mktemp -d /private/tmp/boxwarden-alpha-formatter.XXXXXX)"
prepared=0
trap 'if [[ "$prepared" != 1 ]]; then rm -rf -- "$output_dir"; fi' EXIT
bsdtar -xf "$iso" -C "$output_dir" casper/vmlinuz casper/initrd
python3 "$repo_root/tools/alpha-inspector/kernel_image.py" \
  "$output_dir/casper/vmlinuz" "$output_dir/kernel-image" \
  000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617 \
  a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d
ar -p "$checker_deb" data.tar.zst | bsdtar -xOf - ./usr/sbin/e2fsck.static > "$output_dir/e2fsck.static"
read -r actual_checker _ < <(shasum -a 256 "$output_dir/e2fsck.static")
if [[ "$actual_checker" != "$expected_checker" ]]; then
  echo 'extracted static e2fsck digest mismatch' >&2
  exit 1
fi
GOCACHE="$output_dir/gocache" GOMODCACHE="$output_dir/modcache" GOTOOLCHAIN=local \
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -o "$output_dir/alpha-formatter" "$repo_root/tools/alpha-formatter/guest"
python3 "$script_dir/pack_initramfs.py" \
  "$output_dir/casper/initrd" "$output_dir/alpha-formatter" \
  "$output_dir/e2fsck.static" "$output_dir/formatter-initrd"
file "$output_dir/alpha-formatter" "$output_dir/e2fsck.static"
shasum -a 256 "$output_dir/kernel-image" "$output_dir/formatter-initrd"
printf 'prepared formatter boot artifacts: %s\n' "$output_dir"
prepared=1
