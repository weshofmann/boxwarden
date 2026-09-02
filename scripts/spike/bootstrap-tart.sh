#!/usr/bin/env bash

set -euo pipefail

readonly spike_prefix="boxwarden-m1a-spike-"
readonly default_min_free_gib=12
readonly default_hostdir_spike_root="/private/tmp/boxwarden-host-directory-capability-runtime"
readonly hostdir_mount_tag="boxwarden-host-tree-v1"

serial_relay_pid=""
serial_tart_path=""
serial_operator_path=""
serial_screen_pid=""
serial_screen_name=""
serial_screen_session_path=""

usage() {
  cat <<'EOF'
Usage:
  bootstrap-tart.sh preflight
  bootstrap-tart.sh inventory OUTPUT.json
  bootstrap-tart.sh issue-cert CA_PRIVATE_KEY SESSION_PUBLIC_KEY KEY_ID
  bootstrap-tart.sh host-timezone
  bootstrap-tart.sh render-seed RUN_ID CA_PUBLIC_KEY PASSWORD_HASH TIMEZONE OUTPUT_DIR
  bootstrap-tart.sh rewrite-grub INPUT.cfg OUTPUT.cfg
  bootstrap-tart.sh remaster-iso SOURCE.iso USER_DATA OUTPUT.iso
  bootstrap-tart.sh create VM
  bootstrap-tart.sh run-install VM INSTALL.iso SERIAL_DIR
  bootstrap-tart.sh run VM SERIAL_DIR
  bootstrap-tart.sh run-ro-share VM HOST_DIRECTORY SERIAL_DIR
  bootstrap-tart.sh clone SOURCE_VM DESTINATION_VM
  bootstrap-tart.sh ip VM
  bootstrap-tart.sh stop VM
  bootstrap-tart.sh delete VM

Every VM name must start with boxwarden-m1a-spike-. TART_NO_AUTO_PRUNE is
forced for clones so Task 0 never evicts pre-existing cache entries.
SERIAL_DIR must be an absolute host-local path. Run commands create a private
reconnectable relay there and print the persistent Screen attach command before
Tart starts. Detach from that session with Ctrl-A d; do not kill its window.
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command is unavailable: $1"
}

validate_vm_name() {
  local vm="$1"
  [[ "${vm}" =~ ^boxwarden-m1a-spike-[a-z0-9][a-z0-9-]{0,62}$ ]] || \
    die "refusing non-spike VM name: ${vm}"
}

vm_exists() {
  tart list --source local --quiet | grep -Fqx -- "$1"
}

require_free_space() {
  local minimum_gib="${1:-${BW_SPIKE_MIN_FREE_GIB:-${default_min_free_gib}}}"
  local tart_home="${TART_HOME:-${HOME}/.tart}"
  local available_kib
  local required_kib

  [[ "${minimum_gib}" =~ ^[0-9]+$ ]] || die "free-space threshold must be an integer GiB value"
  available_kib="$(df -Pk "${tart_home}" | awk 'NR == 2 {print $4}')"
  required_kib=$((minimum_gib * 1024 * 1024))

  ((available_kib >= required_kib)) || \
    die "only $((available_kib / 1024 / 1024)) GiB free; ${minimum_gib} GiB required"
}

escape_sed_replacement() {
  sed 's/[\/&]/\\&/g' <<<"$1"
}

preflight() {
  local screen_version_output

  require_command tart
  require_command softnet
  require_command curl
  require_command realpath
  require_command shasum
  require_command xorriso
  require_command socat
  require_command screen
  require_command ssh-keygen
  [[ "$(uname -m)" == "arm64" ]] || die "Task 0 requires an arm64 host"
  require_free_space "${BW_SPIKE_PREFLIGHT_FREE_GIB:-20}"
  screen_version_output="$(screen --version 2>&1 || true)"
  [[ "${screen_version_output}" == Screen\ version* ]] || \
    die "GNU Screen did not report a recognizable version"
  tart --version
  brew list --formula --versions tart softnet xorriso socat
  printf '%s\n' "${screen_version_output}"
  df -h "${TART_HOME:-${HOME}/.tart}"
}

inventory() {
  local output="$1"
  [[ ! -e "${output}" ]] || die "refusing to overwrite inventory: ${output}"
  umask 077
  tart list --format json >"${output}"
}

zoneinfo_root() {
  local configured_root="${BW_SPIKE_ZONEINFO_ROOT:-/var/db/timezone/zoneinfo}"

  require_command realpath
  [[ -d "${configured_root}" ]] || die "host zoneinfo root is unavailable: ${configured_root}"
  realpath "${configured_root}"
}

validate_timezone() {
  local timezone="$1"
  local root
  local resolved_zone

  [[ -n "${timezone}" ]] || die "time zone must not be empty"
  [[ "${timezone}" != /* ]] || die "time zone must be an IANA name, not an absolute path: ${timezone}"
  [[ "${timezone}" =~ ^[A-Za-z0-9._+-]+(/[A-Za-z0-9._+-]+)*$ ]] || \
    die "invalid IANA time-zone name: ${timezone}"
  [[ "/${timezone}/" != *"/../"* && "/${timezone}/" != *"/./"* ]] || \
    die "time zone contains a path traversal component: ${timezone}"

  root="$(zoneinfo_root)"
  [[ -e "${root}/${timezone}" ]] || die "time zone is absent from host zoneinfo: ${timezone}"
  resolved_zone="$(realpath "${root}/${timezone}")"
  case "${resolved_zone}" in
    "${root}"/*) ;;
    *) die "time zone resolves outside host zoneinfo: ${timezone}" ;;
  esac
  [[ -f "${resolved_zone}" ]] || die "time zone is not a zoneinfo file: ${timezone}"
}

host_timezone() {
  local localtime_path="${BW_SPIKE_LOCALTIME_PATH:-/etc/localtime}"
  local root
  local resolved_localtime
  local timezone

  [[ -e "${localtime_path}" ]] || die "host localtime is unavailable: ${localtime_path}"
  root="$(zoneinfo_root)"
  resolved_localtime="$(realpath "${localtime_path}")"
  case "${resolved_localtime}" in
    "${root}"/*) timezone="${resolved_localtime#"${root}"/}" ;;
    *) die "host localtime does not resolve inside the configured zoneinfo tree: ${localtime_path}" ;;
  esac

  validate_timezone "${timezone}"
  printf '%s\n' "${timezone}"
}

screen_session_exists() {
  local listing

  listing="$(screen -ls 2>/dev/null || true)"
  grep -Fq ".${serial_screen_name}" <<<"${listing}"
}

issue_cert() {
  local ca_private_key="$1"
  local session_public_key="$2"
  local key_id="$3"

  [[ -f "${ca_private_key}" ]] || die "missing CA private key: ${ca_private_key}"
  [[ -f "${session_public_key}" ]] || die "missing session public key: ${session_public_key}"
  [[ "${key_id}" =~ ^boxwarden-task0-run-[12]$ ]] || die "invalid Task 0 certificate key ID: ${key_id}"

  ssh-keygen \
    -q \
    -s "${ca_private_key}" \
    -I "${key_id}" \
    -n boxwarden-task0 \
    -V -5m:+2h \
    -O clear \
    -O permit-pty \
    "${session_public_key}"
}

render_seed() {
  local run_id="$1"
  local ca_public_key_path="$2"
  local password_hash_path="$3"
  local timezone="$4"
  local output_dir="$5"
  local script_dir
  local repo_root
  local template
  local ca_public_key
  local password_hash
  local escaped_run_id
  local escaped_ca
  local escaped_password_hash
  local escaped_timezone

  [[ "${run_id}" =~ ^run-[12]$ ]] || die "run ID must be run-1 or run-2"
  [[ -f "${ca_public_key_path}" ]] || die "missing CA public key: ${ca_public_key_path}"
  [[ -f "${password_hash_path}" ]] || die "missing password hash: ${password_hash_path}"
  validate_timezone "${timezone}"

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
  repo_root="$(cd "${script_dir}/../.." && pwd -P)"
  template="${repo_root}/guest/ubuntu-24.04-arm64/autoinstall/user-data"
  [[ -f "${template}" ]] || die "missing seed template: ${template}"

  ca_public_key="$(<"${ca_public_key_path}")"
  password_hash="$(<"${password_hash_path}")"
  [[ "${ca_public_key}" == ssh-ed25519\ * ]] || die "Task 0 requires an Ed25519 SSH user CA"
  [[ "${password_hash}" == \$6\$* ]] || die "password hash must use SHA-512 crypt"

  escaped_run_id="$(escape_sed_replacement "${run_id}")"
  escaped_ca="$(escape_sed_replacement "${ca_public_key}")"
  escaped_password_hash="$(escape_sed_replacement "${password_hash}")"
  escaped_timezone="$(escape_sed_replacement "${timezone}")"

  umask 077
  mkdir -p "${output_dir}"
  sed \
    -e "s/__BOXWARDEN_RUN_ID__/${escaped_run_id}/g" \
    -e "s/__BOXWARDEN_SSH_CA_PUBLIC_KEY__/${escaped_ca}/g" \
    -e "s/__BOXWARDEN_PASSWORD_HASH__/${escaped_password_hash}/g" \
    -e "s/__BOXWARDEN_TIMEZONE__/${escaped_timezone}/g" \
    "${template}" >"${output_dir}/user-data"
  sed \
    -e "s/__BOXWARDEN_INSTANCE_ID__/boxwarden-task0-${escaped_run_id}/g" \
    "${repo_root}/guest/ubuntu-24.04-arm64/autoinstall/meta-data" >"${output_dir}/meta-data"
}

rewrite_grub() {
  local input_cfg="$1"
  local output_cfg="$2"

  [[ -f "${input_cfg}" ]] || die "missing GRUB configuration: ${input_cfg}"
  [[ ! -e "${output_cfg}" ]] || die "refusing to overwrite GRUB configuration: ${output_cfg}"

  sed -E \
    's/^([[:space:]]*linux[[:space:]]+[^[:space:]]+)[[:space:]]+---/\1 autoinstall ---/' \
    "${input_cfg}" >"${output_cfg}"
  grep -Eq '^[[:space:]]*linux[[:space:]]+[^[:space:]]+[[:space:]]+autoinstall[[:space:]]+---([[:space:]]|$)' \
    "${output_cfg}" || die "failed to add autoinstall before the GRUB argument separator"
}

remaster_iso() {
  local source_iso="$1"
  local user_data="$2"
  local output_iso="$3"
  local work_dir
  local grub_cfg

  require_command xorriso
  [[ -f "${source_iso}" ]] || die "missing source ISO: ${source_iso}"
  [[ -f "${user_data}" ]] || die "missing rendered user-data: ${user_data}"
  [[ ! -e "${output_iso}" ]] || die "refusing to overwrite ISO: ${output_iso}"
  require_free_space

  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/boxwarden-task0-remaster.XXXXXX")"
  trap 'rm -rf -- "${work_dir}"' EXIT
  grub_cfg="${work_dir}/grub.cfg"

  xorriso -osirrox on -indev "${source_iso}" -extract /boot/grub/grub.cfg "${grub_cfg}"
  rewrite_grub "${grub_cfg}" "${work_dir}/grub.autoinstall.cfg"

  xorriso \
    -indev "${source_iso}" \
    -outdev "${output_iso}" \
    -boot_image any replay \
    -map "${user_data}" /autoinstall.yaml \
    -map "${work_dir}/grub.autoinstall.cfg" /boot/grub/grub.cfg \
    -commit \
    -end

  rm -rf -- "${work_dir}"
  trap - EXIT
}

create_vm() {
  local vm="$1"
  validate_vm_name "${vm}"
  vm_exists "${vm}" && die "VM already exists: ${vm}"
  require_free_space
  tart create "${vm}" --linux --disk-size 30
  tart set "${vm}" --cpu 4 --memory 4096 --display 1600x1000
}

cleanup_serial_relay() {
  if [[ -n "${serial_screen_name}" ]]; then
    screen -S "${serial_screen_name}" -X quit >/dev/null 2>&1 || true
  fi
  if [[ -n "${serial_screen_pid}" ]]; then
    wait "${serial_screen_pid}" >/dev/null 2>&1 || true
    serial_screen_pid=""
  fi
  serial_screen_name=""

  if [[ -n "${serial_screen_session_path}" ]]; then
    rm -f -- "${serial_screen_session_path}"
    serial_screen_session_path=""
  fi

  if [[ -n "${serial_relay_pid}" ]]; then
    if kill -0 "${serial_relay_pid}" >/dev/null 2>&1; then
      kill "${serial_relay_pid}" >/dev/null 2>&1 || true
    fi
    wait "${serial_relay_pid}" >/dev/null 2>&1 || true
    serial_relay_pid=""
  fi

  if [[ -n "${serial_tart_path}" ]]; then
    rm -f -- "${serial_tart_path}"
    serial_tart_path=""
  fi
  if [[ -n "${serial_operator_path}" ]]; then
    rm -f -- "${serial_operator_path}"
    serial_operator_path=""
  fi
}

start_serial_relay() {
  local serial_dir="$1"
  local serial_log
  local serial_tart_device
  local serial_operator_device
  local screen_control_log
  local attempt

  require_command socat
  require_command screen
  [[ "${serial_dir}" == /* ]] || die "serial runtime directory must be absolute: ${serial_dir}"
  [[ "${serial_dir}" != "/" ]] || die "refusing root as the serial runtime directory"
  [[ ! -L "${serial_dir}" ]] || die "serial runtime directory must not be a symlink: ${serial_dir}"
  if [[ -e "${serial_dir}" ]]; then
    [[ -d "${serial_dir}" ]] || die "serial runtime path is not a directory: ${serial_dir}"
  else
    umask 077
    mkdir -p "${serial_dir}"
  fi
  chmod 700 "${serial_dir}"

  serial_tart_path="${serial_dir}/tart-serial"
  serial_operator_path="${serial_dir}/operator-serial"
  serial_screen_session_path="${serial_dir}/screen-session"
  serial_log="${serial_dir}/socat.log"
  screen_control_log="${serial_dir}/screen-control.log"
  [[ ! -e "${serial_tart_path}" && ! -L "${serial_tart_path}" ]] || \
    die "refusing to replace existing Tart serial path: ${serial_tart_path}"
  [[ ! -e "${serial_operator_path}" && ! -L "${serial_operator_path}" ]] || \
    die "refusing to replace existing operator serial path: ${serial_operator_path}"
  [[ ! -e "${serial_screen_session_path}" && ! -L "${serial_screen_session_path}" ]] || \
    die "refusing to replace existing Screen session metadata: ${serial_screen_session_path}"
  [[ ! -L "${serial_log}" ]] || die "serial relay log must not be a symlink: ${serial_log}"
  [[ ! -L "${screen_control_log}" ]] || die "Screen control log must not be a symlink: ${screen_control_log}"

  umask 077
  socat -d -d \
    "pty,raw,echo=0,ignoreeof,link=${serial_tart_path}" \
    "pty,raw,echo=0,ignoreeof,link=${serial_operator_path}" \
    >>"${serial_log}" 2>&1 &
  serial_relay_pid=$!
  trap cleanup_serial_relay EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  for attempt in {1..50}; do
    if [[ -L "${serial_tart_path}" && -L "${serial_operator_path}" ]]; then
      break
    fi
    kill -0 "${serial_relay_pid}" >/dev/null 2>&1 || \
      die "serial relay exited before creating both PTY paths; inspect ${serial_log}"
    sleep 0.1
  done
  [[ -L "${serial_tart_path}" && -L "${serial_operator_path}" ]] || \
    die "serial relay did not create both PTY paths; inspect ${serial_log}"

  serial_tart_device="$(readlink "${serial_tart_path}")"
  serial_operator_device="$(readlink "${serial_operator_path}")"
  [[ "${serial_tart_device}" == /dev/ttys* && -c "${serial_tart_device}" ]] || \
    die "serial relay produced an unexpected Tart PTY device: ${serial_tart_device}"
  [[ "${serial_operator_device}" == /dev/ttys* && -c "${serial_operator_device}" ]] || \
    die "serial relay produced an unexpected operator PTY device: ${serial_operator_device}"
  chmod 600 "${serial_tart_device}" "${serial_operator_device}"

  serial_screen_name="bw-task0-${PPID}-$$"
  printf '%s\n' "${serial_screen_name}" >"${serial_screen_session_path}"
  chmod 600 "${serial_screen_session_path}"
  TERM=xterm-256color screen -DmS "${serial_screen_name}" \
    "${serial_operator_path}" 115200 >>"${screen_control_log}" 2>&1 &
  serial_screen_pid=$!

  for attempt in {1..50}; do
    if kill -0 "${serial_screen_pid}" >/dev/null 2>&1 && \
      screen_session_exists; then
      break
    fi
    kill -0 "${serial_screen_pid}" >/dev/null 2>&1 || \
      die "persistent Screen holder exited before becoming attachable; inspect ${screen_control_log}"
    sleep 0.1
  done
  kill -0 "${serial_screen_pid}" >/dev/null 2>&1 || \
    die "persistent Screen holder exited before Tart start; inspect ${screen_control_log}"
  if ! screen_session_exists; then
    screen -ls >>"${screen_control_log}" 2>&1 || true
    die "persistent Screen holder did not become attachable; inspect ${screen_control_log}"
  fi

  printf 'Tart serial path: %s\n' "${serial_tart_path}"
  printf 'Serial attach command: TERM=xterm-256color screen -r %s\n' "${serial_screen_name}"
  printf 'Detach without stopping the holder: Ctrl-A d\n'
}

run_with_serial() {
  local vm="$1"
  local serial_dir="$2"
  local tart_status=0
  shift 2

  start_serial_relay "${serial_dir}"
  tart run \
    "$@" \
    --serial-path="${serial_tart_path}" \
    --net-softnet \
    --no-audio \
    --no-clipboard \
    "${vm}" || tart_status=$?

  cleanup_serial_relay
  trap - EXIT INT TERM
  return "${tart_status}"
}

run_install() {
  local vm="$1"
  local iso="$2"
  local serial_dir="$3"
  validate_vm_name "${vm}"
  vm_exists "${vm}" || die "VM does not exist: ${vm}"
  [[ -f "${iso}" ]] || die "missing install ISO: ${iso}"
  require_free_space
  run_with_serial "${vm}" "${serial_dir}" --disk="${iso}:ro"
}

run_vm() {
  local vm="$1"
  local serial_dir="$2"
  validate_vm_name "${vm}"
  vm_exists "${vm}" || die "VM does not exist: ${vm}"
  require_free_space
  run_with_serial "${vm}" "${serial_dir}"
}

canonical_hostdir_source() {
  local source="$1"
  local spike_root="${BW_HOSTDIR_SPIKE_ROOT:-${default_hostdir_spike_root}}"
  local canonical_root
  local canonical_source

  require_command realpath
  [[ "${spike_root}" == /* && "${spike_root}" != "/" ]] || \
    die "host-directory spike root must be an absolute non-root path: ${spike_root}"
  [[ "${source}" == /* && "${source}" != "/" ]] || \
    die "host-directory source must be an absolute non-root path: ${source}"
  [[ -d "${spike_root}" && ! -L "${spike_root}" ]] || \
    die "host-directory spike root must be a real directory: ${spike_root}"
  [[ -d "${source}" && ! -L "${source}" ]] || \
    die "host-directory source must be a real directory: ${source}"

  canonical_root="$(realpath "${spike_root}")"
  canonical_source="$(realpath "${source}")"
  [[ "${spike_root}" == "${canonical_root}" ]] || \
    die "host-directory spike root is not canonical: ${spike_root}"
  [[ "${source}" == "${canonical_source}" ]] || \
    die "host-directory source is not canonical: ${source}"
  case "${canonical_source}" in
    "${canonical_root}"/*) ;;
    *) die "host-directory source is outside the synthetic spike root: ${source}" ;;
  esac

  printf '%s\n' "${canonical_source}"
}

run_ro_share() {
  local vm="$1"
  local host_directory="$2"
  local serial_dir="$3"
  local canonical_source

  validate_vm_name "${vm}"
  vm_exists "${vm}" || die "VM does not exist: ${vm}"
  require_free_space
  canonical_source="$(canonical_hostdir_source "${host_directory}")"
  run_with_serial "${vm}" "${serial_dir}" \
    --dir="${canonical_source}:ro,tag=${hostdir_mount_tag}"
}

clone_vm() {
  local source_vm="$1"
  local destination_vm="$2"
  validate_vm_name "${source_vm}"
  validate_vm_name "${destination_vm}"
  vm_exists "${source_vm}" || die "source VM does not exist: ${source_vm}"
  vm_exists "${destination_vm}" && die "destination VM already exists: ${destination_vm}"
  require_free_space
  TART_NO_AUTO_PRUNE=1 tart clone "${source_vm}" "${destination_vm}"
  tart set "${destination_vm}" --random-mac
}

ip_vm() {
  local vm="$1"
  validate_vm_name "${vm}"
  vm_exists "${vm}" || die "VM does not exist: ${vm}"
  tart ip --resolver=dhcp --wait=60 "${vm}"
}

stop_vm() {
  local vm="$1"
  validate_vm_name "${vm}"
  vm_exists "${vm}" || die "VM does not exist: ${vm}"
  tart stop --timeout=30 "${vm}"
}

delete_vm() {
  local vm="$1"
  validate_vm_name "${vm}"
  vm_exists "${vm}" || die "VM does not exist: ${vm}"
  tart delete "${vm}"
}

(( $# > 0 )) || { usage >&2; exit 2; }
command_name="$1"
shift

case "${command_name}" in
  preflight)
    (( $# == 0 )) || die "preflight accepts no arguments"
    preflight
    ;;
  inventory)
    (( $# == 1 )) || die "inventory requires OUTPUT.json"
    inventory "$@"
    ;;
  issue-cert)
    (( $# == 3 )) || die "issue-cert requires CA_PRIVATE_KEY SESSION_PUBLIC_KEY KEY_ID"
    issue_cert "$@"
    ;;
  host-timezone)
    (( $# == 0 )) || die "host-timezone accepts no arguments"
    host_timezone
    ;;
  render-seed)
    (( $# == 5 )) || die "render-seed requires RUN_ID CA_PUBLIC_KEY PASSWORD_HASH TIMEZONE OUTPUT_DIR"
    render_seed "$@"
    ;;
  rewrite-grub)
    (( $# == 2 )) || die "rewrite-grub requires INPUT.cfg OUTPUT.cfg"
    rewrite_grub "$@"
    ;;
  remaster-iso)
    (( $# == 3 )) || die "remaster-iso requires SOURCE.iso USER_DATA OUTPUT.iso"
    remaster_iso "$@"
    ;;
  create)
    (( $# == 1 )) || die "create requires VM"
    create_vm "$@"
    ;;
  run-install)
    (( $# == 3 )) || die "run-install requires VM INSTALL.iso SERIAL_DIR"
    run_install "$@"
    ;;
  run)
    (( $# == 2 )) || die "run requires VM SERIAL_DIR"
    run_vm "$@"
    ;;
  run-ro-share)
    (( $# == 3 )) || die "run-ro-share requires VM HOST_DIRECTORY SERIAL_DIR"
    run_ro_share "$@"
    ;;
  clone)
    (( $# == 2 )) || die "clone requires SOURCE_VM DESTINATION_VM"
    clone_vm "$@"
    ;;
  ip)
    (( $# == 1 )) || die "ip requires VM"
    ip_vm "$@"
    ;;
  stop)
    (( $# == 1 )) || die "stop requires VM"
    stop_vm "$@"
    ;;
  delete)
    (( $# == 1 )) || die "delete requires VM"
    delete_vm "$@"
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    usage >&2
    die "unknown command: ${command_name}"
    ;;
esac
