#!/usr/bin/env bash
set -euo pipefail

guest_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
remaster="${guest_dir}/remaster-golden-iso.sh"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/boxwarden-golden-remaster-test.XXXXXX")"
trap 'rm -rf -- "$test_dir"' EXIT
stub_bin="${test_dir}/bin"
mkdir -p "$stub_bin"
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

cat >"${stub_bin}/xorriso" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == -osirrox ]]; then
  printf 'linux /casper/vmlinuz ---\n' >"${!#}"
else
  printf '%s\n' "$@" >"$BW_TEST_XORRISO_LOG"
  source_file=''
  while (( $# > 1 )); do
    if [[ "$1" == -map && "$3" == /autoinstall.yaml ]]; then
      source_file="$2"
      break
    fi
    shift
  done
  [[ -n "$source_file" ]] || exit 2
  cp "$source_file" "$BW_TEST_MAPPED_USER_DATA"
fi
EOF
chmod 0755 "${stub_bin}/xorriso"

source_iso="${test_dir}/source.iso"
output_iso="${test_dir}/output.iso"
log="${test_dir}/xorriso-argv"
mapped="${test_dir}/mapped-user-data"
: >"$source_iso"
zoneinfo="${test_dir}/zoneinfo"
mkdir -p "$zoneinfo/America"
: >"$zoneinfo/America/Denver"
ln -s "$zoneinfo/America/Denver" "${test_dir}/localtime"
printf '$6$fixture$%s\n' "$(printf 'A%.0s' {1..86})" >"${test_dir}/hash"
chmod 0600 "${test_dir}/hash"
BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="${test_dir}/localtime" \
  bash "${guest_dir}/render-golden-seed.sh" run-1 "${test_dir}/hash" "${test_dir}/rendered" >"${test_dir}/render.out" || fail 'current renderer failed before fake remaster'
rendered="${test_dir}/rendered/user-data"

if ! PATH="${stub_bin}:${PATH}" BW_TEST_XORRISO_LOG="$log" BW_TEST_MAPPED_USER_DATA="$mapped" \
  bash "$remaster" "$source_iso" "$rendered" "$output_iso"; then
  fail 'current remaster rejected the generated source fixture'
fi
helper="${guest_dir}/artifacts/boxwarden-guest-bootstrap"
finalizer="${guest_dir}/finalize-golden.sh"
for required in "$helper" /boxwarden-artifacts/boxwarden-guest-bootstrap \
  "$finalizer" /boxwarden-artifacts/finalize-golden.sh /autoinstall.yaml; do
  grep -Fxq -- "$required" "$log" || fail "ISO did not map ${required}"
done
expected_digest="$(shasum -a 256 "$finalizer" | awk '{print $1}')"
grep -Fq "'${expected_digest}'" "$mapped" || fail 'mapped autoinstall is not bound to finalizer bytes'
! grep -Fq __BOXWARDEN_ "$mapped" || fail 'mapped autoinstall retains a build placeholder'

sed '/__BOXWARDEN_FINALIZER_SHA256__/d' "$rendered" >"${test_dir}/missing-finalizer-lock"
if PATH="${stub_bin}:${PATH}" BW_TEST_XORRISO_LOG="$log" BW_TEST_MAPPED_USER_DATA="$mapped" \
  bash "$remaster" "$source_iso" "${test_dir}/missing-finalizer-lock" "$output_iso" >"${test_dir}/bad.out" 2>&1; then
  fail 'remaster accepted a user-data definition without finalizer binding'
fi

printf 'generic golden fake-ISO mapping checks passed\n'
