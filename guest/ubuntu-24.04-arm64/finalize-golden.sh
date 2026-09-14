#!/usr/bin/env bash

# Current generic-golden finalization. Run only inside the accepted build
# candidate, then shut it down without another boot. The Task 0 spike finalizer
# remains historical evidence and is intentionally separate.
set -euo pipefail

readonly acknowledgement="--acknowledge-generic-golden-finalization"
readonly helper_sha256="b350cc7fe6d861f39107b4922b4f8987e56e687c7c802dfe8c106fec71f7df60"

die() { printf 'generic golden finalization: %s\n' "$*" >&2; exit 1; }

path_in_root() {
  local root="$1" absolute="$2"
  printf '%s%s\n' "${root%/}" "$absolute"
}

password_field() {
  local shadow="$1"
  awk -F: '$1 == "boxwarden" {count++; field=$2} END {if (count != 1) exit 1; print field}' "$shadow"
}

require_generic_trust_absent() {
  local root="$1" parent component directory entries
  parent="$(path_in_root "$root" /etc/ssh/boxwarden)"
  for component in /etc /etc/ssh /etc/ssh/boxwarden; do
    directory="$(path_in_root "$root" "$component")"
    [[ -d "$directory" && ! -L "$directory" ]] || die "generic SSH trust ancestor ${component} is missing or unsafe"
    [[ "$(stat -c '%u:%g:%a' "$directory")" == 0:0:755 ]] || die "generic SSH trust ancestor ${component} has unsafe metadata"
  done
  entries="$(find "$parent" -mindepth 1 -print -quit)" || die 'cannot enumerate generic SSH trust parent'
  [[ -z "$entries" ]] || die 'generic SSH trust parent contains active or unexpected material'
  for account in root boxwarden; do
    local ssh_dir
    ssh_dir="$(path_in_root "$root" "/${account}/.ssh")"
    if [[ "$account" == boxwarden ]]; then
      ssh_dir="$(path_in_root "$root" /home/boxwarden/.ssh)"
    fi
    [[ ! -e "$ssh_dir" && ! -L "$ssh_dir" ]] || die "unexpected ${account} SSH authentication state"
  done
}

require_sshd_policy() {
  local output line
  sshd -t || die 'sshd configuration is invalid'
  output="$(sshd -T -C user=boxwarden,host=localhost,addr=127.0.0.1)" || die 'cannot inspect effective sshd policy'
  while IFS= read -r line; do
    [[ "$(printf '%s\n' "$output" | grep -Fxc -- "$line")" == 1 ]] || die "effective sshd policy differs at ${line%% *}"
  done <<'SSHD_EXPECTED'
pubkeyauthentication yes
trustedusercakeys /etc/ssh/boxwarden/active/trusted-user-ca.pub
authorizedprincipalsfile /etc/ssh/boxwarden/active/authorized_principals/%u
authorizedkeysfile none
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
SSHD_EXPECTED
}

replace_hostname() {
  local root="$1" from="$2" to="$3" hosts temporary line
  hosts="$(path_in_root "$root" /etc/hosts)"
  [[ -f "$hosts" && ! -L "$hosts" ]] || die 'hosts file is missing or unsafe'
  temporary="${hosts}.boxwarden-new"
  [[ ! -e "$temporary" && ! -L "$temporary" ]] || die 'hosts staging path already exists'
  : >"$temporary"
  while IFS= read -r line || [[ -n "$line" ]]; do
    printf '%s\n' "${line//${from}/${to}}" >>"$temporary"
  done <"$hosts"
  chmod 0644 "$temporary"
  mv "$temporary" "$hosts"
}

install_firstboot_identity() {
  local root="$1" helper unit wants service drop_in
  helper="$(path_in_root "$root" /usr/local/libexec/boxwarden-firstboot-identity)"
  unit="$(path_in_root "$root" /etc/systemd/system/boxwarden-firstboot-identity.service)"
  wants="$(path_in_root "$root" /etc/systemd/system/multi-user.target.wants)"
  [[ ! -e "$helper" && ! -L "$helper" && ! -e "$unit" && ! -L "$unit" ]] || die 'first-boot identity target already exists'
  install -d -m 0755 "$(dirname "$helper")" "$(dirname "$unit")" "$wants"
  cat >"$helper" <<'FIRSTBOOT'
#!/usr/bin/env bash
set -euo pipefail

firstboot_die() { printf 'Boxwarden first boot: %s\n' "$*" >&2; exit 1; }
root=/
if [[ "${1:-}" == --root && "$#" == 2 && "${BOXWARDEN_FINALIZE_TEST_ROOT:-0}" == 1 ]]; then
  root="$2"
elif (( $# != 0 )); then
  firstboot_die 'unexpected arguments'
fi
[[ "$root" == / || ( "$root" == /* && -d "$root" && ! -L "$root" ) ]] || firstboot_die 'invalid root'
[[ "$root" != / || "$EUID" -eq 0 ]] || firstboot_die 'must run as root'
p() { printf '%s%s\n' "${root%/}" "$1"; }
marker="$(p /var/lib/boxwarden/golden-clone-ready)"
[[ -f "$marker" && ! -L "$marker" ]] || exit 0

systemd-machine-id-setup
machine_id="$(cat "$(p /etc/machine-id)")"
[[ "$machine_id" =~ ^[0-9a-f]{32}$ && "$machine_id" != 00000000000000000000000000000000 ]] || firstboot_die 'machine ID was not initialized'
new_hostname="boxwarden-${machine_id:0:12}"
hostnamectl set-hostname "$new_hostname"
[[ "$(cat "$(p /etc/hostname)")" == "$new_hostname" ]] || firstboot_die 'hostname update failed'
hosts="$(p /etc/hosts)"
[[ -f "$hosts" && ! -L "$hosts" ]] || firstboot_die 'hosts file is missing or unsafe'
temporary="${hosts}.boxwarden-new"
[[ ! -e "$temporary" && ! -L "$temporary" ]] || firstboot_die 'hosts staging path already exists'
: >"$temporary"
while IFS= read -r line || [[ -n "$line" ]]; do
  printf '%s\n' "${line//boxwarden-golden/${new_hostname}}" >>"$temporary"
done <"$hosts"
chmod 0644 "$temporary"
mv "$temporary" "$hosts"

ssh-keygen -A
for key_type in ecdsa ed25519 rsa; do
  [[ -s "$(p "/etc/ssh/ssh_host_${key_type}_key")" && -s "$(p "/etc/ssh/ssh_host_${key_type}_key.pub")" ]] || firstboot_die "missing ${key_type} host key"
done
rm -f -- "$marker"
FIRSTBOOT
  chmod 0755 "$helper"
  cat >"$unit" <<'UNIT'
[Unit]
Description=Regenerate generic Boxwarden clone identity
ConditionPathExists=/var/lib/boxwarden/golden-clone-ready
After=local-fs.target systemd-machine-id-commit.service
Before=NetworkManager.service ssh.service display-manager.service gdm3.service serial-getty@hvc0.service

[Service]
Type=oneshot
ExecStart=/usr/local/libexec/boxwarden-firstboot-identity

[Install]
WantedBy=multi-user.target
UNIT
  chmod 0644 "$unit"
  [[ ! -e "$wants/boxwarden-firstboot-identity.service" && ! -L "$wants/boxwarden-firstboot-identity.service" ]] || die 'first-boot unit enablement already exists'
  ln -s ../boxwarden-firstboot-identity.service "$wants/boxwarden-firstboot-identity.service"
  # Ordering alone does not propagate a oneshot failure. Hold each service
  # that can consume clone identity until regeneration has succeeded.
  for service in NetworkManager ssh gdm3 display-manager serial-getty@hvc0; do
    drop_in="$(path_in_root "$root" "/etc/systemd/system/${service}.service.d/20-boxwarden-firstboot-identity.conf")"
    [[ ! -e "$drop_in" && ! -L "$drop_in" ]] || die "${service} first-boot dependency already exists"
    install -d -m 0755 "$(dirname "$drop_in")"
    cat >"$drop_in" <<'DEPENDENCY'
[Unit]
Requires=boxwarden-firstboot-identity.service
After=boxwarden-firstboot-identity.service
DEPENDENCY
    chmod 0644 "$drop_in"
  done
}

finalize_golden() {
  local root="$1" helper shadow field old_hostname build_hostname build_run_id machine_id dbus marker host_keys random_seed_state
  helper="$(path_in_root "$root" /usr/local/libexec/boxwarden-guest-bootstrap)"
  shadow="$(path_in_root "$root" /etc/shadow)"
  machine_id="$(path_in_root "$root" /etc/machine-id)"
  dbus="$(path_in_root "$root" /var/lib/dbus/machine-id)"
  marker="$(path_in_root "$root" /var/lib/boxwarden/golden-clone-ready)"
  [[ ! -e "$marker" && ! -L "$marker" ]] || die 'clone-ready marker already exists'
  [[ -f "$helper" && -x "$helper" && ! -L "$helper" ]] || die 'fixed guest bootstrap helper is missing or unsafe'
  [[ "$(sha256sum "$helper" | awk '{print $1}')" == "$helper_sha256" ]] || die 'fixed guest bootstrap helper digest differs from lock'
  [[ -f "$shadow" && ! -L "$shadow" ]] || die 'shadow account database is missing or unsafe'
  field="$(password_field "$shadow")" || die 'expected one workstation shadow account'
  [[ -n "$field" ]] || die 'workstation account has an empty password field'
  require_generic_trust_absent "$root"
  require_sshd_policy
  [[ -f "$machine_id" && ! -L "$machine_id" ]] || die 'machine-id is missing or unsafe'
  old_hostname="$(cat "$(path_in_root "$root" /etc/hostname)")"
  [[ -f "$(path_in_root "$root" /etc/boxwarden-task0-spike)" ]] || die 'build marker is missing'
  build_run_id="$(cat "$(path_in_root "$root" /etc/boxwarden-task0-spike)")"
  [[ "$build_run_id" =~ ^run-[12]$ ]] || die 'unexpected build marker'
  build_hostname="boxwarden-task0-${build_run_id}"
  [[ "$old_hostname" == "$build_hostname" || "$old_hostname" == boxwarden-golden ]] || die 'unexpected build hostname'

  # Replace the verifier, rather than prefixing it with ! (which would leave
  # recoverable password material in the active shadow entry).
  usermod -p '!' boxwarden
  [[ "$(password_field "$shadow")" == '!' ]] || die 'workstation password verifier was not removed'
  rm -f -- "$(path_in_root "$root" /etc/shadow-)"
  rm -f -- "$(path_in_root "$root" /var/backups/shadow.bak)"
  printf '%s\n' boxwarden-golden >"$(path_in_root "$root" /etc/hostname)"
  replace_hostname "$root" "$build_hostname" boxwarden-golden

  rm -f -- "$(path_in_root "$root" /etc/ssh/)"ssh_host_*_key "$(path_in_root "$root" /etc/ssh/)"ssh_host_*_key.pub
  rm -f -- "$(path_in_root "$root" /var/lib/NetworkManager/secret_key)" \
    "$(path_in_root "$root" /var/lib/NetworkManager/)"*lease* \
    "$(path_in_root "$root" /var/lib/dhcp/)"*
  # The unit normally saves a new seed in ExecStop during poweroff. Stop it
  # now, retain a runtime-only mask through shutdown, then remove that save.
  systemctl mask --runtime --now systemd-random-seed.service || die 'random-seed service could not be stopped and masked'
  [[ -L "$(path_in_root "$root" /run/systemd/system/systemd-random-seed.service)" &&
    "$(readlink "$(path_in_root "$root" /run/systemd/system/systemd-random-seed.service)")" == /dev/null ]] || die 'random-seed runtime mask is missing'
  random_seed_state="$(systemctl show --property=ActiveState --value systemd-random-seed.service)" || die 'cannot inspect random-seed service state'
  [[ "$random_seed_state" == inactive ]] || die 'random-seed service is not inactive'
  rm -f -- "$(path_in_root "$root" /var/lib/systemd/random-seed)"
  if command -v cloud-init >/dev/null 2>&1; then
    cloud-init clean --logs --seed || die 'cloud-init cleanup failed'
  fi
  rm -rf -- "$(path_in_root "$root" /var/lib/cloud/instances)" \
    "$(path_in_root "$root" /var/lib/cloud/instance)" \
    "$(path_in_root "$root" /var/lib/cloud/seed)"
  rm -f -- "$(path_in_root "$root" /root/.bash_history)" "$(path_in_root "$root" /home/boxwarden/.bash_history)"
  rm -rf -- "$(path_in_root "$root" /root/.cache)" "$(path_in_root "$root" /home/boxwarden/.cache)" \
    "$(path_in_root "$root" /root/.mozilla)" "$(path_in_root "$root" /home/boxwarden/.mozilla)" \
    "$(path_in_root "$root" /root/.config/chromium)" "$(path_in_root "$root" /home/boxwarden/.config/chromium)" \
    "$(path_in_root "$root" /root/.config/google-chrome)" "$(path_in_root "$root" /home/boxwarden/.config/google-chrome)"
  rm -rf -- "$(path_in_root "$root" /var/log/installer)"
  if command -v journalctl >/dev/null 2>&1; then
    journalctl --rotate || die 'journal rotation failed'
    journalctl --vacuum-time=1s || die 'journal cleanup failed'
  fi
  find "$(path_in_root "$root" /var/log)" -type f -exec truncate -s 0 {} +

  rm -f -- "$dbus"
  : >"$machine_id"
  ln -s /etc/machine-id "$dbus"

  install_firstboot_identity "$root"
  rm -f -- "$(path_in_root "$root" /etc/boxwarden-task0-spike)"

  [[ "$(password_field "$shadow")" == '!' && ! -e "$(path_in_root "$root" /etc/shadow-)" && ! -e "$(path_in_root "$root" /var/backups/shadow.bak)" ]] || die 'build password material remains in account files'
  [[ "$(cat "$(path_in_root "$root" /etc/hostname)")" == boxwarden-golden ]] || die 'build hostname remains'
  [[ ! -e "$(path_in_root "$root" /etc/boxwarden-task0-spike)" ]] || die 'build marker remains'
  [[ -f "$machine_id" && ! -s "$machine_id" && "$(readlink "$dbus")" == /etc/machine-id ]] || die 'machine ID was not reset'
  host_keys="$(find "$(path_in_root "$root" /etc/ssh)" -maxdepth 1 -name 'ssh_host_*_key*' -print -quit)" || die 'cannot enumerate SSH host keys'
  [[ -z "$host_keys" ]] || die 'SSH host key remains'
  [[ ! -e "$(path_in_root "$root" /var/lib/NetworkManager/secret_key)" && ! -e "$(path_in_root "$root" /var/lib/systemd/random-seed)" ]] || die 'machine seed remains'
  require_generic_trust_absent "$root"
  sync
  install -d -m 0755 "$(dirname "$marker")"
  : >"$marker"
  chmod 0644 "$marker"
  printf 'generic golden clone-ready; power off without another boot\n'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  [[ "${1:-}" == "$acknowledgement" ]] || die "usage: finalize-golden.sh ${acknowledgement}"
  shift
  root=/
  if [[ "${1:-}" == --root && "$#" == 2 && "${BOXWARDEN_FINALIZE_TEST_ROOT:-0}" == 1 ]]; then
    root="$2"
  elif (( $# != 0 )); then
    die 'unexpected arguments'
  fi
  [[ "$root" == / || ( "$root" == /* && -d "$root" && ! -L "$root" ) ]] || die 'invalid root'
  [[ "$root" != / || "$EUID" -eq 0 ]] || die 'must run as root'
  if [[ "$root" == / ]]; then
    PATH=/usr/sbin:/usr/bin:/sbin:/bin
    export PATH
  fi
  finalize_golden "$root"
fi
