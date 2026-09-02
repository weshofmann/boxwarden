#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/../../.." && pwd -P)"
evidence_file="${1:-${repo_root}/docs/evidence/m1a-bootstrap-spike.md}"

failures=0
test_tmp="$(mktemp -d "${TMPDIR:-/tmp}/boxwarden-task0-tests.XXXXXX")"
trap 'rm -rf "${test_tmp}"' EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  failures=$((failures + 1))
}

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "missing file: ${path#"${repo_root}/"}"
}

require_executable() {
  local path="$1"
  [[ -x "${path}" ]] || fail "not executable: ${path#"${repo_root}/"}"
}

require_absent() {
  local pattern="$1"
  local path="$2"
  local message="$3"

  if grep -Fq -- "${pattern}" "${path}"; then
    fail "${message}"
  fi
}

evidence_status() {
  local key="$1"

  awk -F '|' -v wanted="${key}" '
    function trim(value) {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      return value
    }
    /^\|/ {
      key = trim($2)
      status = trim($3)
      if (key == wanted) {
        matches += 1
        matched_status = status
      }
    }
    END {
      if (matches == 1 && matched_status ~ /^(OBSERVED|VENDOR-DOCUMENTED|INFERRED|NOT YET PROVEN)$/) {
        print matched_status
        exit 0
      }
      exit 1
    }
  ' "${evidence_file}"
}

require_observed_evidence_row() {
  local key="$1"
  local status

  if status="$(evidence_status "${key}")"; then
    [[ "${status}" == "OBSERVED" ]] || \
      fail "required core evidence is not OBSERVED: ${key} (${status})"
  else
    fail "missing or duplicate classified core evidence row: ${key}"
  fi
}

require_deferred_evidence_row() {
  local key="$1"
  local status

  if status="$(evidence_status "${key}")"; then
    case "${status}" in
      "NOT YET PROVEN"|OBSERVED)
        ;;
      *)
        fail "deferred environmental evidence has invalid status: ${key} (${status})"
        ;;
    esac
  else
    fail "missing or duplicate classified deferred evidence row: ${key}"
  fi
}

required_files=(
  "${repo_root}/scripts/spike/bootstrap-tart.sh"
  "${repo_root}/scripts/spike/finalize-clone.sh"
  "${repo_root}/guest/ubuntu-24.04-arm64/autoinstall/user-data"
  "${repo_root}/guest/ubuntu-24.04-arm64/autoinstall/meta-data"
  "${evidence_file}"
)

for path in "${required_files[@]}"; do
  require_file "${path}"
done

user_data="${repo_root}/guest/ubuntu-24.04-arm64/autoinstall/user-data"
if [[ -f "${user_data}" ]]; then
  grep -Fq 'timezone: "__BOXWARDEN_TIMEZONE__"' "${user_data}" || \
    fail "autoinstall does not defer the guest time zone to host-derived seed rendering"
  require_absent 'timezone: Etc/UTC' "${user_data}" \
    "autoinstall still fixes every guest to UTC instead of the host time zone"
  grep -Fq 'GRUB_TIMEOUT=1' "${user_data}" || \
    fail "autoinstall does not limit the installed guest's normal GRUB delay to one second"
  grep -Fq 'GRUB_RECORDFAIL_TIMEOUT=1' "${user_data}" || \
    fail "autoinstall does not limit the installed guest's recordfail GRUB delay to one second"
  grep -Fq 'curtin in-target -- update-grub' "${user_data}" || \
    fail "autoinstall does not regenerate the installed GRUB configuration after setting its timeout"
  grep -Fq 'name: "en*"' "${user_data}" || \
    fail "autoinstall network policy does not match portable Ethernet interface names"
  grep -Fq 'dhcp4: true' "${user_data}" || \
    fail "autoinstall network policy does not retain DHCPv4"
  require_absent 'use-dns: false' "${user_data}" \
    "autoinstall disables DHCP-provided DNS required for VPN, split-DNS, and DNS64 compatibility"
  require_absent '1.1.1.1' "${user_data}" \
    "autoinstall hard-codes a public resolver instead of inheriting the host network environment"
  grep -Fq 'uri: http://ports.ubuntu.com/ubuntu-ports' "${user_data}" || \
    fail "autoinstall does not pin the official Ubuntu ARM64 ports mirror"
  grep -Fq 'arches: [arm64]' "${user_data}" || \
    fail "autoinstall ports mirror is not scoped to ARM64"
  grep -Fq 'fallback: abort' "${user_data}" || \
    fail "autoinstall may silently fall back to offline-only package metadata"
  grep -Fq 'geoip: false' "${user_data}" || \
    fail "autoinstall still permits nondeterministic geo-IP mirror selection"
  grep -Eq '^[[:space:]]+id: ubuntu-desktop$' "${user_data}" || \
    fail "autoinstall does not select Canonical's full Desktop source"
  require_absent 'id: ubuntu-desktop-minimal' "${user_data}" \
    "autoinstall still selects the provisional minimal Desktop source"
  grep -Fq 'gnome-initial-setup-done' "${user_data}" || \
    fail "autoinstall does not suppress the post-install GNOME welcome wizard"
  grep -Fq 'AutomaticLoginEnable = true' "${user_data}" || \
    fail "autoinstall does not enable deterministic GNOME automatic login"
  grep -Fq 'AutomaticLogin = boxwarden' "${user_data}" || \
    fail "autoinstall does not automatically log in the Task 0 workstation account"
  grep -Fq 'boxwarden ALL=(ALL:ALL) NOPASSWD: ALL' "${user_data}" || \
    fail "autoinstall does not give the disposable workstation account unrestricted passwordless sudo"
  grep -Fq 'curtin in-target -- visudo -cf /etc/sudoers.d/90-boxwarden' "${user_data}" || \
    fail "autoinstall does not validate the passwordless-sudo policy before reboot"
  grep -Fq 'serial-getty@hvc0.service.d/10-boxwarden-autologin.conf' "${user_data}" || \
    fail "autoinstall does not configure the qualified Tart hvc0 recovery console"
  grep -Fq -- '--autologin boxwarden' "${user_data}" || \
    fail "autoinstall serial recovery console does not automatically log in the workstation account"
  grep -Fq 'systemctl enable serial-getty@hvc0.service' "${user_data}" || \
    fail "autoinstall does not enable the serial recovery getty"
  grep -Fq 'idle-delay=uint32 0' "${user_data}" || \
    fail "autoinstall does not disable GNOME idle blanking"
  grep -Fq 'idle-activation-enabled=false' "${user_data}" || \
    fail "autoinstall does not disable GNOME screensaver activation"
  grep -Fq 'lock-enabled=false' "${user_data}" || \
    fail "autoinstall does not disable automatic GNOME screen locking"
  grep -Fq 'ubuntu-lock-on-suspend=false' "${user_data}" || \
    fail "autoinstall does not disable Ubuntu's suspend-triggered screen locking"
  grep -Fq 'sleep-inactive-ac-timeout=0' "${user_data}" || \
    fail "autoinstall does not disable automatic suspend on AC power"
  grep -Fq 'sleep-inactive-battery-timeout=0' "${user_data}" || \
    fail "autoinstall does not disable automatic suspend on battery power"
  grep -Fq "sleep-inactive-ac-type='nothing'" "${user_data}" || \
    fail "autoinstall does not make the AC idle-power action inert"
  grep -Fq "sleep-inactive-battery-type='nothing'" "${user_data}" || \
    fail "autoinstall does not make the battery idle-power action inert"
  grep -Fq 'curtin in-target -- dconf update' "${user_data}" || \
    fail "autoinstall does not compile the machine-wide GNOME policy database"
  grep -Fq '/org/gnome/desktop/session/idle-delay' "${user_data}" || \
    fail "autoinstall does not lock the no-idle-blanking policy against drift"
  grep -Fq '/org/gnome/desktop/screensaver/idle-activation-enabled' "${user_data}" || \
    fail "autoinstall does not lock the no-screensaver policy against drift"
  grep -Fq '/org/gnome/desktop/screensaver/lock-enabled' "${user_data}" || \
    fail "autoinstall does not lock the no-auto-lock policy against drift"
  grep -Fq '/org/gnome/desktop/screensaver/ubuntu-lock-on-suspend' "${user_data}" || \
    fail "autoinstall does not lock the no-suspend-lock policy against drift"
  grep -Fq '/org/gnome/settings-daemon/plugins/power/sleep-inactive-ac-timeout' "${user_data}" || \
    fail "autoinstall does not lock the no-AC-suspend policy against drift"
  grep -Fq '/org/gnome/settings-daemon/plugins/power/sleep-inactive-battery-timeout' "${user_data}" || \
    fail "autoinstall does not lock the no-battery-suspend policy against drift"
fi

if [[ -f "${repo_root}/scripts/spike/bootstrap-tart.sh" ]]; then
  bash -n "${repo_root}/scripts/spike/bootstrap-tart.sh" || fail "bootstrap-tart.sh has invalid shell syntax"
  require_executable "${repo_root}/scripts/spike/bootstrap-tart.sh"

  zoneinfo_root="${test_tmp}/zoneinfo"
  localtime_path="${test_tmp}/localtime"
  mkdir -p "${zoneinfo_root}/America"
  : >"${zoneinfo_root}/America/Denver"
  ln -s "${zoneinfo_root}/America/Denver" "${localtime_path}"

  if detected_timezone="$(
    BW_SPIKE_LOCALTIME_PATH="${localtime_path}" \
    BW_SPIKE_ZONEINFO_ROOT="${zoneinfo_root}" \
      "${repo_root}/scripts/spike/bootstrap-tart.sh" host-timezone
  )"; then
    [[ "${detected_timezone}" == "America/Denver" ]] || \
      fail "host time-zone detection returned '${detected_timezone}' instead of America/Denver"
  else
    fail "host time-zone detection rejected a valid zoneinfo-backed localtime link"
  fi

  rm -f "${localtime_path}"
  : >"${test_tmp}/outside-zone"
  ln -s "${test_tmp}/outside-zone" "${localtime_path}"
  if BW_SPIKE_LOCALTIME_PATH="${localtime_path}" \
    BW_SPIKE_ZONEINFO_ROOT="${zoneinfo_root}" \
      "${repo_root}/scripts/spike/bootstrap-tart.sh" host-timezone \
      >"${test_tmp}/outside-zone.out" 2>"${test_tmp}/outside-zone.err"; then
    fail "host time-zone detection accepted a localtime target outside the trusted zoneinfo tree"
  fi

  ca_public_key_path="${test_tmp}/user-ca.pub"
  password_hash_path="${test_tmp}/password.hash"
  rendered_seed_dir="${test_tmp}/rendered-seed"
  printf '%s\n' \
    'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBwRFasUlbn5G0Ui17nsul/wmOFCPojDG9WnMGaJ6q8F task0-test-ca' \
    >"${ca_public_key_path}"
  printf '%s\n' '$6$task0$abcdefghijklmnopqrstuvwxyz' >"${password_hash_path}"

  if BW_SPIKE_ZONEINFO_ROOT="${zoneinfo_root}" \
    "${repo_root}/scripts/spike/bootstrap-tart.sh" render-seed \
      run-1 "${ca_public_key_path}" "${password_hash_path}" \
      America/Denver "${rendered_seed_dir}"; then
    grep -Fq 'timezone: "America/Denver"' "${rendered_seed_dir}/user-data" || \
      fail "rendered seed does not contain the explicitly selected host time zone"
    require_absent '__BOXWARDEN_TIMEZONE__' "${rendered_seed_dir}/user-data" \
      "rendered seed retains the unresolved time-zone placeholder"
  else
    fail "seed rendering rejected a valid host time zone"
  fi

  if BW_SPIKE_ZONEINFO_ROOT="${zoneinfo_root}" \
    "${repo_root}/scripts/spike/bootstrap-tart.sh" render-seed \
      run-1 "${ca_public_key_path}" "${password_hash_path}" \
      ../outside-zone "${test_tmp}/invalid-render" \
      >"${test_tmp}/invalid-render.out" 2>"${test_tmp}/invalid-render.err"; then
    fail "seed rendering accepted a time-zone path traversal"
  fi

  require_absent '--net-softnet-block=@host' "${repo_root}/scripts/spike/bootstrap-tart.sh" \
    "candidate launch still blocks the vmnet gateway and its required DNS proxy"
  require_absent '--net-softnet-allow=0.0.0.0/0' "${repo_root}/scripts/spike/bootstrap-tart.sh" \
    "candidate launch disables Softnet bridge isolation"
  require_absent '--net-bridged' "${repo_root}/scripts/spike/bootstrap-tart.sh" \
    "candidate launch uses physical host bridging instead of shared NAT"
  require_absent '--net-host' "${repo_root}/scripts/spike/bootstrap-tart.sh" \
    "candidate launch uses host networking instead of shared NAT"
  require_absent '    --serial \' "${repo_root}/scripts/spike/bootstrap-tart.sh" \
    "candidate launch still relies on Tart's unreadable one-shot host PTY"
  grep -Fq 'require_command socat' "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "candidate launch does not require the qualified host serial relay"
  grep -Fq 'require_command screen' "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "candidate launch does not require the persistent serial terminal holder"
  grep -Fq 'screen --version 2>&1 || true' "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "preflight still treats macOS GNU Screen's nonzero version status as a fatal tool failure"
  grep -Fq 'GNU Screen did not report a recognizable version' \
    "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "preflight does not validate GNU Screen's version output after tolerating its status quirk"
  grep -Fq -- '--serial-path="${serial_tart_path}"' \
    "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "candidate launch does not bind Tart to the retained host-owned serial path"
  (( $(grep -Fo 'ignoreeof' "${repo_root}/scripts/spike/bootstrap-tart.sh" | wc -l) >= 2 )) || \
    fail "serial relay is not configured to survive operator-client disconnects"
  grep -Fq 'chmod 600 "${serial_tart_device}" "${serial_operator_device}"' \
    "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "serial relay does not make both macOS PTY devices owner-readable"
  grep -Fq 'run_with_serial "${vm}" "${serial_dir}" --disk="${iso}:ro"' \
    "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "installer launch does not use the managed serial relay"
  grep -Fq 'run_with_serial "${vm}" "${serial_dir}"' \
    "${repo_root}/scripts/spike/bootstrap-tart.sh" || \
    fail "normal launch does not use the managed serial relay"

  if command -v socat >/dev/null 2>&1; then
    fake_bin="${test_tmp}/fake-bin"
    fake_tart_log="${test_tmp}/fake-tart.log"
    fake_vm="boxwarden-m1a-spike-serial-test"
    serial_runtime="${test_tmp}/serial-runtime"
    mkdir -p "${fake_bin}"
    cat >"${fake_bin}/tart" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  list)
    printf '%s\n' "${FAKE_TART_VM}"
    ;;
  run)
    printf '%s\n' "$@" >"${FAKE_TART_LOG}"
    serial_path=""
    for argument in "$@"; do
      case "${argument}" in
        --serial-path=*) serial_path="${argument#--serial-path=}" ;;
      esac
    done
    [[ -n "${serial_path}" ]]
    operator_path="$(dirname "${serial_path}")/operator-serial"
    screen_session_path="$(dirname "${serial_path}")/screen-session"
    serial_device="$(readlink "${serial_path}")"
    operator_device="$(readlink "${operator_path}")"
    [[ -r "${serial_device}" && -w "${serial_device}" ]]
    [[ -r "${operator_device}" && -w "${operator_device}" ]]
    [[ -r "${screen_session_path}" ]]
    screen_session="$(<"${screen_session_path}")"
    [[ "${screen_session}" == bw-task0-* ]]
    screen_listing="$(screen -ls 2>/dev/null || true)"
    grep -Fq ".${screen_session}" <<<"${screen_listing}"
    printf 'serial-holder-probe\r\n' >"${serial_path}"
    sleep 0.2
    screen_listing="$(screen -ls 2>/dev/null || true)"
    grep -Fq ".${screen_session}" <<<"${screen_listing}"
    printf 'screen_session=%s\n' "${screen_session}" >>"${FAKE_TART_LOG}"
    ;;
  *)
    exit 2
    ;;
esac
EOF
    chmod +x "${fake_bin}/tart"

    if PATH="${fake_bin}:${PATH}" \
      FAKE_TART_VM="${fake_vm}" \
      FAKE_TART_LOG="${fake_tart_log}" \
      TART_HOME="${test_tmp}" \
      BW_SPIKE_MIN_FREE_GIB=0 \
      "${repo_root}/scripts/spike/bootstrap-tart.sh" run \
      "${fake_vm}" "${serial_runtime}" >"${test_tmp}/managed-serial-test.out"; then
      grep -Fq -- "--serial-path=${serial_runtime}/tart-serial" "${fake_tart_log}" || \
        fail "normal launch did not pass the managed serial path to Tart"
      grep -Fq -- '--net-softnet' "${fake_tart_log}" || \
        fail "managed serial launch lost the Softnet network policy"
      grep -Fq -- '--no-audio' "${fake_tart_log}" || \
        fail "managed serial launch lost the audio-sharing prohibition"
      grep -Fq -- '--no-clipboard' "${fake_tart_log}" || \
        fail "managed serial launch lost the clipboard-sharing prohibition"
      screen_session="$(awk -F= '/^screen_session=/ {print $2}' "${fake_tart_log}")"
      [[ -n "${screen_session}" ]] || \
        fail "managed serial launch did not expose its persistent Screen session to the lifecycle test"
      screen_listing="$(screen -ls 2>/dev/null || true)"
      grep -Fq ".${screen_session}" <<<"${screen_listing}" && \
        fail "managed serial launch left its persistent Screen session behind after Tart exit"
      [[ ! -e "${serial_runtime}/tart-serial" && \
         ! -L "${serial_runtime}/tart-serial" ]] || \
        fail "managed serial launch left the Tart PTY link behind after exit"
      [[ ! -e "${serial_runtime}/operator-serial" && \
         ! -L "${serial_runtime}/operator-serial" ]] || \
        fail "managed serial launch left the operator PTY link behind after exit"
      [[ ! -e "${serial_runtime}/screen-session" ]] || \
        fail "managed serial launch left the Screen session metadata behind after exit"
    else
      if [[ -f "${serial_runtime}/screen-control.log" ]]; then
        sed -n '1,40p' "${serial_runtime}/screen-control.log" >&2
      fi
      fail "managed serial launch failed against the fake Tart lifecycle"
    fi

    hostdir_outside="${test_tmp}/outside-source"
    hostdir_serial_runtime="${test_tmp}/hostdir-serial-runtime"
    mkdir -p "${test_tmp}/host-directory-capability/source" "${hostdir_outside}"
    hostdir_spike_root="$(realpath "${test_tmp}/host-directory-capability")"
    hostdir_source="${hostdir_spike_root}/source"
    hostdir_symlink="${hostdir_spike_root}/source-link"
    ln -s "${hostdir_source}" "${hostdir_symlink}"

    if PATH="${fake_bin}:${PATH}" \
      FAKE_TART_VM="${fake_vm}" \
      FAKE_TART_LOG="${fake_tart_log}" \
      TART_HOME="${test_tmp}" \
      BW_SPIKE_MIN_FREE_GIB=0 \
      BW_HOSTDIR_SPIKE_ROOT="${hostdir_spike_root}" \
      "${repo_root}/scripts/spike/bootstrap-tart.sh" run-ro-share \
      "${fake_vm}" "${hostdir_source}" "${hostdir_serial_runtime}" \
      >"${test_tmp}/managed-ro-share-test.out"; then
      grep -Fqx -- "--dir=${hostdir_source}:ro,tag=boxwarden-host-tree-v1" \
        "${fake_tart_log}" || \
        fail "read-only host-tree launch did not pass the exact fixed-tag share to Tart"
      [[ "$(grep -Fc -- '--dir=' "${fake_tart_log}")" == 1 ]] || \
        fail "read-only host-tree launch passed more than one directory share"
      grep -Fq -- '--net-softnet' "${fake_tart_log}" || \
        fail "read-only host-tree launch lost the Softnet policy"
      grep -Fq -- '--no-audio' "${fake_tart_log}" || \
        fail "read-only host-tree launch lost the audio-sharing prohibition"
      grep -Fq -- '--no-clipboard' "${fake_tart_log}" || \
        fail "read-only host-tree launch lost the clipboard-sharing prohibition"
      grep -Fq -- "--serial-path=${hostdir_serial_runtime}/tart-serial" \
        "${fake_tart_log}" || \
        fail "read-only host-tree launch did not use the managed serial path"
    else
      fail "read-only host-tree launch failed against the fake Tart lifecycle"
    fi

    if PATH="${fake_bin}:${PATH}" \
      FAKE_TART_VM="${fake_vm}" \
      FAKE_TART_LOG="${fake_tart_log}" \
      TART_HOME="${test_tmp}" \
      BW_SPIKE_MIN_FREE_GIB=0 \
      BW_HOSTDIR_SPIKE_ROOT="${hostdir_spike_root}" \
      "${repo_root}/scripts/spike/bootstrap-tart.sh" run-ro-share \
      "${fake_vm}" "${hostdir_symlink}" "${test_tmp}/symlink-serial" \
      >"${test_tmp}/symlink-share.out" 2>&1; then
      fail "read-only host-tree launch accepted a symlinked source root"
    fi

    if PATH="${fake_bin}:${PATH}" \
      FAKE_TART_VM="${fake_vm}" \
      FAKE_TART_LOG="${fake_tart_log}" \
      TART_HOME="${test_tmp}" \
      BW_SPIKE_MIN_FREE_GIB=0 \
      BW_HOSTDIR_SPIKE_ROOT="${hostdir_spike_root}" \
      "${repo_root}/scripts/spike/bootstrap-tart.sh" run-ro-share \
      "${fake_vm}" "${hostdir_outside}" "${test_tmp}/outside-serial" \
      >"${test_tmp}/outside-share.out" 2>&1; then
      fail "read-only host-tree launch accepted a source outside the synthetic spike root"
    fi
  else
    fail "socat is required for the Task 0 managed serial-relay test"
  fi

  cat >"${test_tmp}/grub.cfg" <<'EOF'
menuentry "Try or Install Ubuntu" {
	linux	/casper/vmlinuz  --- quiet splash console=tty0
	initrd	/casper/initrd
}
EOF
  if "${repo_root}/scripts/spike/bootstrap-tart.sh" rewrite-grub \
    "${test_tmp}/grub.cfg" "${test_tmp}/grub.autoinstall.cfg"; then
    grep -Fq $'\tlinux\t/casper/vmlinuz autoinstall --- quiet splash console=tty0' \
      "${test_tmp}/grub.autoinstall.cfg" || \
      fail "GRUB rewrite did not preserve Noble arguments and add autoinstall before ---"
  else
    fail "GRUB rewrite command failed for the Noble Desktop boot-line shape"
  fi
fi

if [[ -f "${repo_root}/scripts/spike/finalize-clone.sh" ]]; then
  bash -n "${repo_root}/scripts/spike/finalize-clone.sh" || fail "finalize-clone.sh has invalid shell syntax"
  require_executable "${repo_root}/scripts/spike/finalize-clone.sh"
  require_absent 'systemctl mask "serial-getty@${serial_device}.service"' \
    "${repo_root}/scripts/spike/finalize-clone.sh" \
    "clone finalization disables the approved host-local recovery shell"
fi

if [[ -f "${evidence_file}" ]]; then
  grep -Eq '^task0_execution_state:[[:space:]]+PASS_WITH_CONDITIONS$' "${evidence_file}" || \
    fail "Task 0 evidence is not marked PASS_WITH_CONDITIONS"

  declared_deferred_evidence="$(
    awk '
      /^task0_deferred_evidence:[[:space:]]*$/ {
        in_deferred = 1
        next
      }
      in_deferred && /^  - [a-z0-9_.-]+[[:space:]]*$/ {
        value = $0
        sub(/^  - /, "", value)
        sub(/[[:space:]]+$/, "", value)
        print value
        next
      }
      in_deferred {
        exit
      }
    ' "${evidence_file}"
  )"
  expected_deferred_evidence=$'ipv6_only_upstream\nipv4_only_destination\nipv6_only_destination'
  [[ "${declared_deferred_evidence}" == "${expected_deferred_evidence}" ]] || \
    fail "PASS_WITH_CONDITIONS does not declare the exact approved deferred evidence set"

  core_evidence_keys=(
    host_toolchain_versions
    softnet_host_privilege
    iso_identity
    unattended_install
    install_reboot_detection
    gui_display_input
    wayland
    xwayland
    ssh
    guest_root_access
    guest_to_internet
    guest_to_host
    guest_to_private
    guest_to_link_local
    session_to_session
    softnet_anti_spoofing
    dhcp
    dns
    vpn_custom_split_dns
    lease_renewal
    management_address
    host_timezone
    installed_grub_timeout
    ssh_user_ca
    host_key_bootstrap
    serial_recovery_console
    graphical_lifetime
    two_clone_identity
    latency_measurements
    second_run
  )

  for key in "${core_evidence_keys[@]}"; do
    require_observed_evidence_row "${key}"
  done

  deferred_evidence_keys=(
    ipv6_only_upstream
    ipv4_only_destination
    ipv6_only_destination
  )

  for key in "${deferred_evidence_keys[@]}"; do
    require_deferred_evidence_row "${key}"
  done

  clone_keys=(
    clone_identity.mac
    clone_identity.machine_id
    clone_identity.ssh_host_keys
    clone_identity.dhcp_duid
    clone_identity.hostname
    clone_identity.random_seed
    clone_identity.management_address
    clone_identity.source_unchanged
  )

  for key in "${clone_keys[@]}"; do
    require_observed_evidence_row "${key}"
  done

  require_observed_evidence_row clean_run.1
  require_observed_evidence_row clean_run.2

  while IFS= read -r key; do
    case "${key}" in
      ipv6_only_upstream|ipv4_only_destination|ipv6_only_destination)
        ;;
      *)
        fail "unexpected NOT YET PROVEN evidence row: ${key}"
        ;;
    esac
  done < <(
    awk -F '|' '
      function trim(value) {
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
        return value
      }
      /^\|/ {
        key = trim($2)
        status = trim($3)
        if (status == "NOT YET PROVEN") {
          print key
        }
      }
    ' "${evidence_file}"
  )
fi

if ((failures > 0)); then
  printf '%d evidence validation failure(s)\n' "${failures}" >&2
  exit 1
fi

printf 'Task 0 PASS WITH CONDITIONS evidence check passed: %s\n' \
  "${evidence_file#"${repo_root}/"}"
