#!/usr/bin/env bash
set -euo pipefail

guest_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
renderer="${guest_dir}/render-golden-seed.sh"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/boxwarden-golden-render-test.XXXXXX")"
trap 'rm -rf -- "$test_dir"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

zoneinfo="${test_dir}/zoneinfo"
localtime="${test_dir}/localtime"
mkdir -p "$zoneinfo/America"
printf 'fixture zone\n' >"$zoneinfo/America/Denver"
ln -s "$zoneinfo/America/Denver" "$localtime"
hash_file="${test_dir}/hash"
fixture_hash="\$6\$fixture\$$(printf 'A%.0s' {1..86})"
printf '%s\n' "$fixture_hash" >"$hash_file"
chmod 0600 "$hash_file"
output="${test_dir}/rendered"
BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="$localtime" \
  bash "$renderer" run-1 "$hash_file" "$output" >"${test_dir}/render.out" || fail 'current seed rendering failed'
[[ -f "$output/user-data" && -f "$output/meta-data" ]] || fail 'seed output is incomplete'
grep -Fq 'timezone: "America/Denver"' "$output/user-data" || fail 'host IANA zone was not rendered'
grep -Fq 'hostname: boxwarden-task0-run-1' "$output/user-data" || fail 'build run was not rendered'
grep -Fq "$fixture_hash" "$output/user-data" || fail 'temporary verifier was not rendered'
grep -Fq '__BOXWARDEN_FINALIZER_SHA256__' "$output/user-data" || fail 'finalizer slot was prematurely replaced'
! grep -Eq '__BOXWARDEN_(RUN_ID|TIMEZONE|PASSWORD_HASH|INSTANCE_ID)__' "$output/user-data" "$output/meta-data" || fail 'seed retains an unrendered build field'
grep -Fq 'boxwarden-task0-run-1' "$output/meta-data" || fail 'instance ID did not track build run'

fresh_run=run-0123456789ab
BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="$localtime" \
  bash "$renderer" "$fresh_run" "$hash_file" "${test_dir}/fresh-rendered" >"${test_dir}/fresh-render.out" || fail 'fresh alpha run ID was rejected'
grep -Fq "hostname: boxwarden-task0-${fresh_run}" "${test_dir}/fresh-rendered/user-data" || fail 'fresh alpha hostname was not rendered'
grep -Fq "boxwarden-task0-${fresh_run}" "${test_dir}/fresh-rendered/meta-data" || fail 'fresh alpha instance ID was not rendered'
for invalid_run in run-0123456789ABC run-0123456789a run-0123456789abc run-3; do
  if BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="$localtime" \
    bash "$renderer" "$invalid_run" "$hash_file" "${test_dir}/invalid-${invalid_run}" >"${test_dir}/invalid-run.out" 2>&1; then
    fail "invalid build run ID accepted: ${invalid_run}"
  fi
done

if BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="$localtime" \
  bash "$renderer" run-1 "$hash_file" "$output" >"${test_dir}/overwrite.out" 2>&1; then
  fail 'renderer overwrote an existing seed'
fi
chmod 0644 "$hash_file"
if BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="$localtime" \
  bash "$renderer" run-1 "$hash_file" "${test_dir}/readable-output" >"${test_dir}/readable.out" 2>&1; then
  fail 'renderer accepted a world-readable password verifier'
fi
chmod 0600 "$hash_file"
printf '%s\n' invalid >"${test_dir}/bad-hash"
if BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="$localtime" \
  bash "$renderer" run-1 "${test_dir}/bad-hash" "${test_dir}/bad-output" >"${test_dir}/bad-hash.out" 2>&1; then
  fail 'renderer accepted a non-crypt password field'
fi
printf 'outside\n' >"${test_dir}/outside"
rm "$localtime"
ln -s "${test_dir}/outside" "$localtime"
if BW_GOLDEN_RENDER_TEST_MODE=1 BW_GOLDEN_TEST_ZONEINFO_ROOT="$zoneinfo" BW_GOLDEN_TEST_LOCALTIME="$localtime" \
  bash "$renderer" run-1 "$hash_file" "${test_dir}/outside-output" >"${test_dir}/outside.out" 2>&1; then
  fail 'renderer accepted localtime outside trusted zoneinfo'
fi

printf 'generic golden seed rendering checks passed\n'
