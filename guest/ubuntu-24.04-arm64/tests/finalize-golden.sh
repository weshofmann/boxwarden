#!/usr/bin/env bash
set -euo pipefail

guest_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
finalizer="${guest_dir}/finalize-golden.sh"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/boxwarden-golden-finalize-test.XXXXXX")"
trap 'rm -rf -- "${test_dir}"' EXIT
stub_bin="${test_dir}/bin"
mkdir -p "${stub_bin}"
umask 022

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

cat >"${stub_bin}/sha256sum" <<'EOF'
#!/usr/bin/env bash
exec shasum -a 256 "$@"
EOF
cat >"${stub_bin}/stat" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 3 && "$1" == -c && "$2" == '%u:%g:%a' ]] || exit 2
python3 - "$3" <<'PY'
import os
import stat
import sys

path = sys.argv[1]
info = os.lstat(path)
uid = 1000 if os.environ.get('BW_TEST_TRUST_BAD_OWNER') == '1' and path.endswith('/etc/ssh/boxwarden') else 0
print(f'{uid}:0:{stat.S_IMODE(info.st_mode):o}')
PY
EOF
cat >"${stub_bin}/find" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${BW_TEST_FAIL_FIND:-}" == trust && "$1" == "$BW_TEST_ROOT/etc/ssh/boxwarden" ]]; then
  exit 1
fi
if [[ "${BW_TEST_FAIL_FIND:-}" == hostkeys && "$1" == "$BW_TEST_ROOT/etc/ssh" ]]; then
  exit 1
fi
exec /usr/bin/find "$@"
EOF
cat >"${stub_bin}/sshd" <<'EOF'
#!/usr/bin/env bash
[[ -f "$BW_TEST_ROOT/etc/ssh/ssh_host_ed25519_key" ]] || exit 1
case "$1" in
  -t) exit 0 ;;
  -T) cat "$BW_TEST_SSHD_OUTPUT" ;;
  *) exit 2 ;;
esac
EOF
cat >"${stub_bin}/usermod" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 3 && "$1" == -p && "$2" == '!' && "$3" == boxwarden ]] || exit 2
[[ "${BW_TEST_FAIL_USERMOD:-0}" != 1 ]] || exit 1
[[ "${BW_TEST_SKIP_USERMOD:-0}" != 1 ]] || exit 0
cp "$BW_TEST_ROOT/etc/shadow" "$BW_TEST_ROOT/etc/shadow-"
awk -F: 'BEGIN {OFS=":"} $1=="boxwarden" {$2="!"} {print}' \
  "$BW_TEST_ROOT/etc/shadow" >"$BW_TEST_ROOT/etc/shadow.new"
mv "$BW_TEST_ROOT/etc/shadow.new" "$BW_TEST_ROOT/etc/shadow"
EOF
cat >"${stub_bin}/cloud-init" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == 'clean --logs --seed' ]] || exit 2
[[ "${BW_TEST_FAIL_CLOUD_INIT:-0}" != 1 ]] || exit 1
rm -rf "$BW_TEST_ROOT/var/lib/cloud/instances" "$BW_TEST_ROOT/var/lib/cloud/instance" "$BW_TEST_ROOT/var/lib/cloud/seed"
EOF
cat >"${stub_bin}/journalctl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"${stub_bin}/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'mask --runtime --now systemd-random-seed.service')
    [[ "${BW_TEST_FAIL_RANDOM_STOP:-0}" != 1 ]] || exit 1
    mkdir -p "$BW_TEST_ROOT/run/systemd/system"
    ln -s /dev/null "$BW_TEST_ROOT/run/systemd/system/systemd-random-seed.service"
    printf '%s\n' saved-during-stop >"$BW_TEST_ROOT/var/lib/systemd/random-seed"
    ;;
  'is-active --quiet systemd-random-seed.service') exit 3 ;;
  'show --property=ActiveState --value systemd-random-seed.service')
    [[ "${BW_TEST_FAIL_RANDOM_STATE:-0}" != 1 ]] || exit 1
    printf 'inactive\n'
    ;;
  *) exit 2 ;;
esac
EOF
cat >"${stub_bin}/systemd-machine-id-setup" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 0 ]] || exit 2
if [[ ! -s "$BW_TEST_ROOT/etc/machine-id" ]]; then
  printf '%s\n' "$BW_TEST_MACHINE_ID" >"$BW_TEST_ROOT/etc/machine-id"
fi
EOF
cat >"${stub_bin}/hostnamectl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 2 && "$1" == set-hostname ]] || exit 2
printf '%s\n' "$2" >"$BW_TEST_ROOT/etc/hostname"
EOF
cat >"${stub_bin}/ssh-keygen" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 1 && "$1" == -A ]] || exit 2
[[ "${BW_TEST_FAIL_SSHKEYGEN:-0}" != 1 ]] || exit 1
host="$(cat "$BW_TEST_ROOT/etc/hostname")"
for key_type in ecdsa ed25519 rsa; do
  printf 'private-%s-%s\n' "$BW_TEST_MACHINE_ID" "$key_type" >"$BW_TEST_ROOT/etc/ssh/ssh_host_${key_type}_key"
  printf 'public-%s-%s %s\n' "$BW_TEST_MACHINE_ID" "$key_type" "$host" >"$BW_TEST_ROOT/etc/ssh/ssh_host_${key_type}_key.pub"
done
EOF
chmod 0755 "${stub_bin}"/*

sshd_good="${test_dir}/sshd-good"
cat >"${sshd_good}" <<'EOF'
pubkeyauthentication yes
trustedusercakeys /etc/ssh/boxwarden/active/trusted-user-ca.pub
authorizedprincipalsfile /etc/ssh/boxwarden/active/authorized_principals/%u
authorizedkeysfile .ssh/authorized_keys
permituserenvironment no
permituserrc no
passwordauthentication no
kbdinteractiveauthentication no
permitrootlogin no
x11forwarding no
allowagentforwarding no
allowtcpforwarding no
allowstreamlocalforwarding no
gatewayports no
permittunnel no
EOF

make_fixture() {
  local root="$1" run_id="${2:-run-1}"
  mkdir -p "$root/etc/ssh/boxwarden" "$root/etc/ssh/sshd_config.d" \
    "$root/etc/systemd/system" "$root/etc/gdm3" "$root/etc/sudoers.d" \
    "$root/usr/local/libexec" "$root/var/lib/NetworkManager" \
    "$root/var/lib/dhcp" "$root/var/lib/systemd" "$root/var/backups" \
    "$root/var/lib/cloud/instances" "$root/var/lib/cloud/seed" \
    "$root/var/log/installer" "$root/root/.cache" \
    "$root/home/boxwarden/.cache" "$root/home/boxwarden/.mozilla"
  printf '%s\n' 'boxwarden:$6$fixture$BUILD_VERIFIER:20000:0:99999:7:::' >"$root/etc/shadow"
  cp "$root/etc/shadow" "$root/var/backups/shadow.bak"
  printf '%s\n' 'boxwarden:x:1000:1000:Boxwarden:/home/boxwarden:/bin/bash' >"$root/etc/passwd"
  printf '%s\n' "boxwarden-task0-${run_id}" >"$root/etc/hostname"
  printf '%s\n' '127.0.0.1 localhost' "127.0.1.1 boxwarden-task0-${run_id}" >"$root/etc/hosts"
  printf '%s\n' "$run_id" >"$root/etc/boxwarden-task0-spike"
  printf '%s\n' 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' >"$root/etc/machine-id"
  printf '%s\n' 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' >"$root/var/lib/dbus-machine-id-placeholder"
  mkdir -p "$root/var/lib/dbus"
  mv "$root/var/lib/dbus-machine-id-placeholder" "$root/var/lib/dbus/machine-id"
  printf '%s\n' 'old-key' >"$root/etc/ssh/ssh_host_ed25519_key"
  printf '%s\n' 'old-pub' >"$root/etc/ssh/ssh_host_ed25519_key.pub"
  printf '%s\n' 'old-secret' >"$root/var/lib/NetworkManager/secret_key"
  printf '%s\n' 'old-lease' >"$root/var/lib/NetworkManager/dhclient-old.lease"
  printf '%s\n' 'old-duid' >"$root/var/lib/dhcp/dhclient.leases"
  printf '%s\n' 'old-random' >"$root/var/lib/systemd/random-seed"
  printf '%s\n' 'old-cloud' >"$root/var/lib/cloud/instances/iid"
  printf '%s\n' 'old-seed' >"$root/var/lib/cloud/seed/user-data"
  printf '%s\n' 'old-install-log' >"$root/var/log/installer/autoinstall-user-data"
  printf '%s\n' 'old-history' >"$root/home/boxwarden/.bash_history"
  printf '%s\n' 'old-cache' >"$root/home/boxwarden/.cache/item"
  printf '%s\n' 'old-profile' >"$root/home/boxwarden/.mozilla/profile"
  printf '%s\n' 'AutomaticLoginEnable = true' 'AutomaticLogin = boxwarden' >"$root/etc/gdm3/custom.conf"
  printf '%s\n' 'boxwarden ALL=(ALL:ALL) NOPASSWD: ALL' >"$root/etc/sudoers.d/90-boxwarden"
  cp "$guest_dir/artifacts/boxwarden-guest-bootstrap" "$root/usr/local/libexec/boxwarden-guest-bootstrap"
}

run_finalizer() {
  local root="$1"
  BW_TEST_ROOT="$root" BW_TEST_SSHD_OUTPUT="${BW_TEST_SSHD_OUTPUT:-$sshd_good}" \
    BOXWARDEN_FINALIZE_TEST_ROOT=1 PATH="${stub_bin}:${PATH}" \
    bash "$finalizer" --acknowledge-generic-golden-finalization --root "$root"
}

run_firstboot() {
  local root="$1" machine_id="$2"
  BW_TEST_ROOT="$root" BW_TEST_MACHINE_ID="$machine_id" \
    BOXWARDEN_FINALIZE_TEST_ROOT=1 PATH="${stub_bin}:${PATH}" \
    bash "$root/usr/local/libexec/boxwarden-firstboot-identity" --root "$root"
}

good="$test_dir/good"
make_fixture "$good"
run_finalizer "$good" >"$test_dir/good.out" || fail 'finalizer rejected a valid candidate fixture'

fresh_run="$test_dir/fresh-run"
make_fixture "$fresh_run" run-0123456789ab
run_finalizer "$fresh_run" >"$test_dir/fresh-run.out" || fail 'finalizer rejected a valid public preparation run ID'
[[ -f "$fresh_run/var/lib/boxwarden/golden-clone-ready" && ! -e "$fresh_run/etc/boxwarden-task0-spike" ]] || fail 'public preparation run was not finalized'
! grep -Fq boxwarden-task0-run-0123456789ab "$fresh_run/etc/hosts" || fail 'public preparation hostname survived finalization'

invalid_run="$test_dir/invalid-run"
make_fixture "$invalid_run" run-0123456789ABC
if run_finalizer "$invalid_run" >"$test_dir/invalid-run.out" 2>&1; then fail 'malformed public preparation run ID was admitted'; fi
[[ ! -e "$invalid_run/var/lib/boxwarden/golden-clone-ready" ]] || fail 'malformed run produced clone-ready marker'

builder_ssh="$test_dir/builder-ssh"
make_fixture "$builder_ssh"
mkdir -p "$builder_ssh/root/.ssh" "$builder_ssh/home/boxwarden/.ssh"
printf '%s\n' 'builder-root-key' >"$builder_ssh/root/.ssh/authorized_keys"
printf '%s\n' 'builder-workstation-key' >"$builder_ssh/home/boxwarden/.ssh/authorized_keys"
run_finalizer "$builder_ssh" >"$test_dir/builder-ssh.out" || fail 'finalizer rejected removable builder SSH state'
[[ ! -e "$builder_ssh/root/.ssh" && ! -e "$builder_ssh/home/boxwarden/.ssh" ]] || fail 'builder SSH authentication state survived finalization'
[[ "$(awk -F: '$1=="boxwarden" {print $2}' "$good/etc/shadow")" == '!' ]] || fail 'build password verifier survived'
[[ ! -e "$good/etc/shadow-" ]] || fail 'shadow backup retained build password verifier'
[[ ! -e "$good/var/backups/shadow.bak" ]] || fail 'periodic shadow backup retained build password verifier'
[[ ! -e "$good/etc/boxwarden-task0-spike" ]] || fail 'build marker survived'
[[ "$(cat "$good/etc/hostname")" == boxwarden-golden ]] || fail 'build hostname survived'
! grep -Fq boxwarden-task0-run-1 "$good/etc/hosts" || fail 'hosts retained build hostname'
[[ ! -e "$good/etc/ssh/boxwarden/active" ]] || fail 'active trust appeared'
[[ ! -e "$good/etc/ssh/ssh_host_ed25519_key" && ! -e "$good/etc/ssh/ssh_host_ed25519_key.pub" ]] || fail 'source SSH keys survived'
[[ -f "$good/etc/machine-id" && ! -s "$good/etc/machine-id" ]] || fail 'machine-id is not empty regular file'
[[ "$(readlink "$good/var/lib/dbus/machine-id")" == /etc/machine-id ]] || fail 'D-Bus machine-id is not linked to canonical ID'
[[ ! -e "$good/var/lib/NetworkManager/secret_key" && ! -e "$good/var/lib/NetworkManager/dhclient-old.lease" ]] || fail 'NetworkManager identity survived'
[[ ! -e "$good/var/lib/dhcp/dhclient.leases" && ! -e "$good/var/lib/systemd/random-seed" ]] || fail 'DHCP or random seed survived'
[[ "$(readlink "$good/run/systemd/system/systemd-random-seed.service")" == /dev/null ]] || fail 'seed service was not masked through candidate shutdown'
[[ ! -e "$good/var/lib/cloud/instances/iid" && ! -e "$good/var/lib/cloud/seed/user-data" ]] || fail 'cloud instance/seed survived'
[[ ! -e "$good/var/log/installer/autoinstall-user-data" && ! -e "$good/home/boxwarden/.bash_history" ]] || fail 'build log/history survived'
[[ ! -e "$good/home/boxwarden/.cache/item" && ! -e "$good/home/boxwarden/.mozilla/profile" ]] || fail 'profile/cache survived'
[[ -f "$good/var/lib/boxwarden/golden-clone-ready" ]] || fail 'clone-ready marker missing'
[[ -x "$good/usr/local/libexec/boxwarden-firstboot-identity" ]] || fail 'first-boot identity helper missing'
[[ -f "$good/etc/systemd/system/boxwarden-firstboot-identity.service" ]] || fail 'first-boot identity unit missing'
for service in NetworkManager ssh gdm3 display-manager serial-getty@hvc0; do
  drop_in="$good/etc/systemd/system/${service}.service.d/20-boxwarden-firstboot-identity.conf"
  [[ -f "$drop_in" ]] || fail "${service} does not depend on first-boot identity"
  grep -Fxq 'Requires=boxwarden-firstboot-identity.service' "$drop_in" || fail "${service} does not require successful identity generation"
  grep -Fxq 'After=boxwarden-firstboot-identity.service' "$drop_in" || fail "${service} can start before identity generation"
done
[[ "$(cat "$good/etc/gdm3/custom.conf")" == *'AutomaticLogin = boxwarden'* ]] || fail 'GUI autologin changed'
[[ "$(cat "$good/etc/sudoers.d/90-boxwarden")" == *'NOPASSWD: ALL'* ]] || fail 'passwordless sudo changed'

run_firstboot "$good" bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb >"$test_dir/firstboot.out" || fail 'first boot failed'
[[ "$(cat "$good/etc/machine-id")" == bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ]] || fail 'first boot did not generate machine ID'
[[ "$(cat "$good/etc/hostname")" == boxwarden-bbbbbbbbbbbb ]] || fail 'first boot did not derive hostname'
[[ "$(cat "$good/etc/ssh/ssh_host_ed25519_key.pub")" == *'boxwarden-bbbbbbbbbbbb'* ]] || fail 'SSH host keys were generated before hostname changed'
[[ ! -e "$good/var/lib/boxwarden/golden-clone-ready" ]] || fail 'first-boot marker survived success'

second="$test_dir/second"
make_fixture "$second"
run_finalizer "$second" >"$test_dir/second.out" || fail 'second fixture finalization failed'
run_firstboot "$second" cccccccccccccccccccccccccccccccc >"$test_dir/second-firstboot.out" || fail 'second first boot failed'
[[ "$(cat "$good/etc/machine-id")" != "$(cat "$second/etc/machine-id")" ]] || fail 'two clone machine IDs match'
[[ "$(cat "$good/etc/ssh/ssh_host_ed25519_key.pub")" != "$(cat "$second/etc/ssh/ssh_host_ed25519_key.pub")" ]] || fail 'two clone host keys match'

active="$test_dir/active"
make_fixture "$active"
mkdir -p "$active/etc/ssh/boxwarden/active"
if run_finalizer "$active" >"$test_dir/active.out" 2>&1; then fail 'active domain trust was admitted'; fi
[[ ! -e "$active/var/lib/boxwarden/golden-clone-ready" ]] || fail 'active trust produced clone-ready marker'
[[ "$(awk -F: '$1=="boxwarden" {print $2}' "$active/etc/shadow")" == '$6$fixture$BUILD_VERIFIER' ]] || fail 'active-trust refusal mutated account'

bad_trust_mode="$test_dir/bad-trust-mode"
make_fixture "$bad_trust_mode"
chmod 0777 "$bad_trust_mode/etc/ssh/boxwarden"
if run_finalizer "$bad_trust_mode" >"$test_dir/bad-trust-mode.out" 2>&1; then fail 'writable generic trust parent was admitted'; fi
[[ ! -e "$bad_trust_mode/var/lib/boxwarden/golden-clone-ready" ]] || fail 'writable trust parent produced clone-ready marker'

bad_trust_owner="$test_dir/bad-trust-owner"
make_fixture "$bad_trust_owner"
if BW_TEST_TRUST_BAD_OWNER=1 run_finalizer "$bad_trust_owner" >"$test_dir/bad-trust-owner.out" 2>&1; then fail 'nonroot generic trust parent was admitted'; fi
[[ ! -e "$bad_trust_owner/var/lib/boxwarden/golden-clone-ready" ]] || fail 'nonroot trust parent produced clone-ready marker'

failed_trust_scan="$test_dir/failed-trust-scan"
make_fixture "$failed_trust_scan"
if BW_TEST_FAIL_FIND=trust run_finalizer "$failed_trust_scan" >"$test_dir/failed-trust-scan.out" 2>&1; then fail 'failed trust enumeration was admitted'; fi
[[ ! -e "$failed_trust_scan/var/lib/boxwarden/golden-clone-ready" ]] || fail 'failed trust scan produced clone-ready marker'

failed_hostkey_scan="$test_dir/failed-hostkey-scan"
make_fixture "$failed_hostkey_scan"
if BW_TEST_FAIL_FIND=hostkeys run_finalizer "$failed_hostkey_scan" >"$test_dir/failed-hostkey-scan.out" 2>&1; then fail 'failed host-key enumeration was admitted'; fi
[[ ! -e "$failed_hostkey_scan/var/lib/boxwarden/golden-clone-ready" ]] || fail 'failed host-key scan produced clone-ready marker'

wrong_helper="$test_dir/wrong-helper"
make_fixture "$wrong_helper"
printf 'wrong\n' >"$wrong_helper/usr/local/libexec/boxwarden-guest-bootstrap"
if run_finalizer "$wrong_helper" >"$test_dir/wrong-helper.out" 2>&1; then fail 'wrong guest helper was admitted'; fi
[[ ! -e "$wrong_helper/var/lib/boxwarden/golden-clone-ready" ]] || fail 'wrong helper produced clone-ready marker'

bad_sshd="$test_dir/bad-sshd"
make_fixture "$bad_sshd"
sed 's#^authorizedkeysfile .ssh/authorized_keys$#authorizedkeysfile none#' "$sshd_good" >"$test_dir/sshd-bad"
if BW_TEST_SSHD_OUTPUT="$test_dir/sshd-bad" run_finalizer "$bad_sshd" >"$test_dir/bad-sshd.out" 2>&1; then fail 'weak sshd policy was admitted'; fi
[[ ! -e "$bad_sshd/var/lib/boxwarden/golden-clone-ready" ]] || fail 'weak sshd produced clone-ready marker'

noop_user="$test_dir/noop-user"
make_fixture "$noop_user"
if BW_TEST_SKIP_USERMOD=1 run_finalizer "$noop_user" >"$test_dir/noop-user.out" 2>&1; then fail 'build password verifier remained usable'; fi
[[ ! -e "$noop_user/var/lib/boxwarden/golden-clone-ready" ]] || fail 'password verifier produced clone-ready marker'

failed_seed_stop="$test_dir/failed-seed-stop"
make_fixture "$failed_seed_stop"
if BW_TEST_FAIL_RANDOM_STOP=1 run_finalizer "$failed_seed_stop" >"$test_dir/failed-seed-stop.out" 2>&1; then fail 'failed random-seed service stop reported clone-ready'; fi
[[ ! -e "$failed_seed_stop/var/lib/boxwarden/golden-clone-ready" ]] || fail 'failed seed stop produced clone-ready marker'

failed_seed_state="$test_dir/failed-seed-state"
make_fixture "$failed_seed_state"
if BW_TEST_FAIL_RANDOM_STATE=1 run_finalizer "$failed_seed_state" >"$test_dir/failed-seed-state.out" 2>&1; then fail 'unreadable random-seed service state reported clone-ready'; fi
[[ ! -e "$failed_seed_state/var/lib/boxwarden/golden-clone-ready" ]] || fail 'unreadable seed state produced clone-ready marker'

interrupted="$test_dir/interrupted"
make_fixture "$interrupted"
if BW_TEST_FAIL_CLOUD_INIT=1 run_finalizer "$interrupted" >"$test_dir/interrupted.out" 2>&1; then fail 'failed cleanup reported clone-ready'; fi
[[ ! -e "$interrupted/var/lib/boxwarden/golden-clone-ready" ]] || fail 'partial finalization produced clone-ready marker'
if run_finalizer "$interrupted" >"$test_dir/retry.out" 2>&1; then fail 'hostkey-cleared partial candidate was retried as safe'; fi
[[ ! -e "$interrupted/var/lib/boxwarden/golden-clone-ready" ]] || fail 'partial retry produced clone-ready marker'

firstboot_retry="$test_dir/firstboot-retry"
make_fixture "$firstboot_retry"
run_finalizer "$firstboot_retry" >"$test_dir/firstboot-retry-finalize.out" || fail 'first-boot retry fixture did not finalize'
if BW_TEST_FAIL_SSHKEYGEN=1 run_firstboot "$firstboot_retry" dddddddddddddddddddddddddddddddd >"$test_dir/failed-firstboot.out" 2>&1; then fail 'failed key generation completed first boot'; fi
[[ -e "$firstboot_retry/var/lib/boxwarden/golden-clone-ready" ]] || fail 'failed first boot lost retry marker'
run_firstboot "$firstboot_retry" dddddddddddddddddddddddddddddddd >"$test_dir/retry-firstboot.out" || fail 'first-boot retry failed'
[[ ! -e "$firstboot_retry/var/lib/boxwarden/golden-clone-ready" ]] || fail 'successful retry retained marker'

printf 'generic golden finalization fixture checks passed\n'
