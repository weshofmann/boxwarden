#!/usr/bin/env bash

# Current generic-golden ISO assembly. The Task 0 spike remaster remains
# historical; this path includes the current locked bootstrap and finalizer.
set -euo pipefail

die() { printf 'generic golden remaster: %s\n' "$*" >&2; exit 1; }
(( $# == 4 )) || die 'usage: remaster-golden-iso.sh SOURCE.iso RENDERED_USER_DATA RECIPE_PREPARATION.json OUTPUT.iso'
source_iso="$1"
rendered_user_data="$2"
preparation_json="$3"
output_iso="$4"
guest_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
helper="${guest_dir}/artifacts/boxwarden-guest-bootstrap"
finalizer="${guest_dir}/finalize-golden.sh"
recipe_helper="${guest_dir}/recipe-prepare.py"
chatgpt_installer="${guest_dir}/install-pinned-chatgpt.py"
lock="${guest_dir}/artifacts.lock.json"

command -v xorriso >/dev/null 2>&1 || die 'xorriso is unavailable'
[[ -f "$source_iso" && -f "$rendered_user_data" && -f "$preparation_json" && ! -L "$preparation_json" && ! -e "$output_iso" && ! -L "$output_iso" ]] || die 'source, rendered user-data, recipe preparation, or output path is invalid'
[[ -f "$helper" && -x "$helper" && -f "$finalizer" && -f "$recipe_helper" && -f "$chatgpt_installer" ]] || die 'current guest artifacts are missing'
[[ "$(wc -c <"$preparation_json" | tr -d ' ')" -ge 1 && "$(wc -c <"$preparation_json" | tr -d ' ')" -le 1048576 ]] || die 'recipe preparation exceeds bound'
expected_helper="$(sed -n 's/.*"sha256": "\([0-9a-f]\{64\}\)".*/\1/p' "$lock")"
[[ "$expected_helper" =~ ^[0-9a-f]{64}$ ]] || die 'helper lock is invalid'
[[ "$(shasum -a 256 "$helper" | awk '{print $1}')" == "$expected_helper" ]] || die 'helper differs from its lock'
grep -Fq "'${expected_helper}'" "$rendered_user_data" || die 'rendered user-data does not bind locked helper'
[[ "$(grep -Fo '__BOXWARDEN_FINALIZER_SHA256__' "$rendered_user_data" | wc -l | tr -d ' ')" == 1 ]] || die 'rendered user-data lacks one finalizer digest slot'
[[ "$(grep -Fo '__BOXWARDEN_RECIPE_HELPER_SHA256__' "$rendered_user_data" | wc -l | tr -d ' ')" == 1 ]] || die 'rendered user-data lacks one recipe helper digest slot'
[[ "$(grep -Fo '__BOXWARDEN_CHATGPT_INSTALLER_SHA256__' "$rendered_user_data" | wc -l | tr -d ' ')" == 1 ]] || die 'rendered user-data lacks one ChatGPT installer digest slot'
[[ "$(grep -Fo '__BOXWARDEN_RECIPE_PAYLOAD_SHA256__' "$rendered_user_data" | wc -l | tr -d ' ')" == 1 ]] || die 'rendered user-data lacks one recipe payload digest slot'
for required in __BOXWARDEN_RUN_ID__ __BOXWARDEN_TIMEZONE__ __BOXWARDEN_PASSWORD_HASH__; do
  ! grep -Fq "$required" "$rendered_user_data" || die "rendered user-data retains ${required}"
done
grep -Fq '/cdrom/boxwarden-artifacts/finalize-golden.sh' "$rendered_user_data" || die 'rendered user-data does not install current finalizer'

# Render into owner-private temporary storage because the user-data carries a
# temporary password verifier until finalization of the installed candidate.
umask 077
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/boxwarden-golden-remaster.XXXXXX")"
trap 'rm -rf -- "$work_dir"' EXIT
finalizer_sha="$(shasum -a 256 "$finalizer" | awk '{print $1}')"
recipe_helper_sha="$(shasum -a 256 "$recipe_helper" | awk '{print $1}')"
chatgpt_installer_sha="$(shasum -a 256 "$chatgpt_installer" | awk '{print $1}')"
preparation_sha="$(shasum -a 256 "$preparation_json" | awk '{print $1}')"
sed -e "s/__BOXWARDEN_FINALIZER_SHA256__/${finalizer_sha}/" \
    -e "s/__BOXWARDEN_RECIPE_HELPER_SHA256__/${recipe_helper_sha}/" \
    -e "s/__BOXWARDEN_CHATGPT_INSTALLER_SHA256__/${chatgpt_installer_sha}/" \
    -e "s/__BOXWARDEN_RECIPE_PAYLOAD_SHA256__/${preparation_sha}/" \
    "$rendered_user_data" >"$work_dir/user-data"
! grep -Fq __BOXWARDEN_ "$work_dir/user-data" || die 'mapped user-data retains a placeholder'

xorriso -osirrox on -indev "$source_iso" -extract /boot/grub/grub.cfg "$work_dir/grub.cfg"
sed -E 's/^([[:space:]]*linux[[:space:]]+[^[:space:]]+)[[:space:]]+---/\1 autoinstall ---/' \
  "$work_dir/grub.cfg" >"$work_dir/grub.autoinstall.cfg"
grep -Eq '^[[:space:]]*linux[[:space:]]+[^[:space:]]+[[:space:]]+autoinstall[[:space:]]+---([[:space:]]|$)' \
  "$work_dir/grub.autoinstall.cfg" || die 'GRUB autoinstall boot entry is missing'

xorriso \
  -indev "$source_iso" \
  -outdev "$output_iso" \
  -boot_image any replay \
  -map "$work_dir/user-data" /autoinstall.yaml \
  -map "$helper" /boxwarden-artifacts/boxwarden-guest-bootstrap \
  -map "$finalizer" /boxwarden-artifacts/finalize-golden.sh \
  -map "$recipe_helper" /boxwarden-artifacts/recipe-prepare.py \
  -map "$chatgpt_installer" /boxwarden-artifacts/install-pinned-chatgpt.py \
  -map "$preparation_json" /boxwarden-artifacts/recipe-prepare.json \
  -map "$work_dir/grub.autoinstall.cfg" /boot/grub/grub.cfg \
  -commit \
  -end
