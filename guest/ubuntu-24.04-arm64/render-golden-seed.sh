#!/usr/bin/env bash

# Render the current generic-golden autoinstall seed from the trusted host's
# validated IANA zone and a private temporary SHA-512 crypt verifier file.
set -euo pipefail

die() { printf 'generic golden seed: %s\n' "$*" >&2; exit 1; }
(( $# == 3 )) || die 'usage: render-golden-seed.sh RUN_ID PASSWORD_HASH_FILE OUTPUT_DIR'
run_id="$1"
hash_file="$2"
output_dir="$3"
[[ "$run_id" =~ ^run-[12]$ ]] || die 'build run ID must be run-1 or run-2'
[[ -f "$hash_file" && ! -L "$hash_file" ]] || die 'password hash file is missing or unsafe'
[[ ! -e "$output_dir" && ! -L "$output_dir" ]] || die 'refusing to overwrite seed output'
case "$(uname -s)" in
  Darwin) hash_metadata="$(stat -f '%u:%Lp' "$hash_file")" || die 'cannot inspect password hash metadata' ;;
  Linux) hash_metadata="$(stat -c '%u:%a' "$hash_file")" || die 'cannot inspect password hash metadata' ;;
  *) die 'unsupported seed-rendering host' ;;
esac
[[ "$hash_metadata" == "$(id -u):600" || "$hash_metadata" == "$(id -u):400" ]] || die 'password hash file must be operator-owned and mode 0600 or 0400'

password_hash="$(cat "$hash_file")" || die 'cannot read password hash file'
[[ "$password_hash" =~ ^\$6\$(rounds=[0-9]+\$)?[A-Za-z0-9./]{1,16}\$[A-Za-z0-9./]{86}$ ]] || die 'password field is not SHA-512 crypt'

localtime_path=/etc/localtime
zoneinfo_root=/var/db/timezone/zoneinfo
if [[ "${BW_GOLDEN_RENDER_TEST_MODE:-0}" == 1 ]]; then
  localtime_path="${BW_GOLDEN_TEST_LOCALTIME:-$localtime_path}"
  zoneinfo_root="${BW_GOLDEN_TEST_ZONEINFO_ROOT:-$zoneinfo_root}"
fi
[[ -f "$localtime_path" && -d "$zoneinfo_root" ]] || die 'trusted host time-zone inputs are unavailable'
root="$(realpath "$zoneinfo_root")" || die 'cannot resolve host zoneinfo root'
localtime="$(realpath "$localtime_path")" || die 'cannot resolve host localtime'
case "$localtime" in
  "$root"/*) zone="${localtime#"$root"/}" ;;
  *) die 'host localtime resolves outside zoneinfo' ;;
esac
[[ -f "$localtime" && "$zone" =~ ^[A-Za-z0-9._+-]+(/[A-Za-z0-9._+-]+)*$ ]] || die 'host zone is not a valid IANA file'
[[ "/${zone}/" != *"/../"* && "/${zone}/" != *"/./"* ]] || die 'host zone contains a traversal component'

guest_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
umask 077
mkdir -m 0700 "$output_dir" || die 'cannot create seed output directory'
complete=0
trap 'if [[ "$complete" == 0 ]]; then rm -rf -- "$output_dir"; fi' EXIT
sed \
  -e "s#__BOXWARDEN_RUN_ID__#${run_id}#g" \
  -e "s#__BOXWARDEN_PASSWORD_HASH__#${password_hash}#g" \
  -e "s#__BOXWARDEN_TIMEZONE__#${zone}#g" \
  "$guest_dir/autoinstall/user-data" >"$output_dir/user-data"
sed "s#__BOXWARDEN_INSTANCE_ID__#boxwarden-task0-${run_id}#g" \
  "$guest_dir/autoinstall/meta-data" >"$output_dir/meta-data"
if grep -Eq '__BOXWARDEN_(RUN_ID|PASSWORD_HASH|TIMEZONE|INSTANCE_ID)__' "$output_dir/user-data" "$output_dir/meta-data"; then
  die 'rendered seed retains a build placeholder'
fi
complete=1
printf 'rendered generic golden seed for %s\n' "$zone"
