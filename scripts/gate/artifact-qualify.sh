#!/opt/homebrew/Cellar/bash/5.3.9/bin/bash
# =============================================================================
# Boxwarden M1A Artifact Construction + Qualification Script
#
# STATUS: FOR REVIEW — DO NOT EXECUTE WITHOUT EXPLICIT HUMAN APPROVAL
#
# Requires: Bash 5.3.9 at the absolute path above (associative arrays).
# System bash 3.2 on the qualified macOS host is NOT used.
#
# This script owns the entire lifecycle from candidate VM creation through
# qualification. The serial PTY binding to the exact candidate artifact is
# established by process ownership: the script creates the PTY pair, passes
# it to Tart via --serial-path, runs Tart in the background, retains its PID,
# and performs all serial observations while that exact Tart process is running.
# The stopped artifact whose payloads are hashed is the same Tart object that
# the script launched and for which it owns the serial topology.
#
# Phase structure:
#   0  Preflight:           validate identities, paths, no-overlap checks
#   C  Construct:           create candidate VM, run installation
#   R  Run:                 launch candidate with serial relay (background)
#   F  Finalize:            operator runs finalize-clone.sh; script waits
#   Q  Qualify:             seven framed serial checks
#   P  Poweroff+hash:       sync+poweroff, wait for Tart exit, hash payloads
#   E  Evidence:            write structured content-bound JSON record
#
# Serial binding: this script creates TWO PTY pairs via socat, passes the
# Tart PTY endpoint to tart run --serial-path, and retains the operator PTY.
# TART_PID is set from $!. The serial checks communicate over the operator
# PTY while TART_PID is live. The binding assertion: serial endpoint ->
# candidate artifact is provided by holding tart_pid for the process that
# received that exact --serial-path at launch.
#
# Usage (after explicit human approval):
#   /opt/homebrew/Cellar/bash/5.3.9/bin/bash artifact-qualify.sh \
#       --approve-execution \
#       --gate-tart-home   <GATE_TART_HOME>  \
#       --candidate-name   <tart-object-name> \
#       --ubuntu-iso       /path/to/ubuntu-24.04.4-desktop-arm64.iso \
#       --password-hash-file /path/to/password.hash \
#       --timezone         America/Denver \
#       --run-id           run-1 \
#       --serial-dir       <serial-runtime-dir> \
#       --output-dir       <evidence output dir> \
#       --boxwarden-src    /path/to/boxwarden-repo
#
# GATE_TART_HOME is shared with v2-generic-golden-gate.sh; no copy is made.
# All scratch files go under OUTPUT_DIR, never /tmp.
#
# Source:
#   Analysis worktree: weshofmann/research/v2-generic-golden-gate
#   PR #2 head:        5c09aad9d2c322e42e6e8d7fd38e9584396b3ab5
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Pinned qualified identities
# ---------------------------------------------------------------------------

# Bash: trusted-host procedure machinery, not a prequalified security boundary.
# Minimum required: version 5.x (associative arrays). Resolved and recorded at runtime.
readonly REQUIRED_BASH_MAJOR=5

# Tart 2.32.1: PREQUALIFIED identity from V3 attended gate and tool-provenance.md.
# The SHA-256 is the admission identity; the path is resolved at runtime.
# Changing this SHA-256 requires deliberate requalification.
readonly REQUIRED_TART_SHA256="05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d"
readonly REQUIRED_TART_VERSION="2.32.1"

# Python: trusted-host tool, not a prequalified security boundary.
# Minimum required: Python 3.x with json, re, base64, subprocess, os modules.
# Resolved and recorded at runtime.
readonly REQUIRED_PYTHON_MAJOR=3

readonly GATE_VM_PREFIX="boxwarden-m1a-gate-v2-"

# PR #1 exact source commit — the guest definition and build scripts must
# be verified from this exact commit.
readonly PR1_SOURCE_COMMIT="79aa1230e52486999f79b9193b7f85eade13e75c"

# Task 0 qualified canonical Ubuntu 24.04.4 Desktop ARM64 ISO SHA-256.
# The --ubuntu-iso supplied to this script MUST match this digest exactly.
readonly CANONICAL_UBUNTU_ISO_SHA256="c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"

# Socat: Task 0 qualified socat 1.8.1.3 as the two-PTY serial relay.
# Socat: Task 0 qualified version 1.8.1.3. Path and SHA-256 resolved at runtime.
# The V2 gate establishes the exact socat identity for this qualification run.
# SHA-256 binds the executable bytes only; dynamic libraries are not hashed here.
readonly REQUIRED_SOCAT_VERSION="1.8.1.3"

# xorriso: Task 0 qualified version 1.5.8.pl02. Path and SHA-256 resolved at runtime.
# Used for ISO remastering and embedded user-data extraction.
readonly REQUIRED_XORRISO_VERSION="1.5.8.pl02"

# Evidence record fields
readonly EVIDENCE_VERSION="1"
readonly EVIDENCE_SCHEMA="boxwarden-artifact-qualification-v1"

# Serial channel parameters (private scratch under OUTPUT_DIR, not /tmp)
readonly SERIAL_CMD_TIMEOUT_SECS=30
# Poweroff: we wait() directly on the background tart run process; no poll loop needed.
readonly SERIAL_MAX_RAW_BYTES=65536

# Framing protocol
readonly FRAME_STX=$'\x02'
readonly FRAME_ETX=$'\x03'
readonly FRAME_TAG="BWQF"

# ---------------------------------------------------------------------------
# Approval guard
# ---------------------------------------------------------------------------
APPROVED=0
GATE_TART_HOME=""
CANDIDATE_NAME=""
UBUNTU_ISO=""          # canonical Ubuntu source ISO — must match CANONICAL_UBUNTU_ISO_SHA256
PASSWORD_HASH_FILE=""  # path to SHA-512 crypt hash file (sensitive; never recorded in evidence)
TIMEZONE=""            # IANA timezone (e.g. America/Denver)
RUN_ID="run-1"         # run-1 or run-2 (Task 0 bootstrap-tart.sh convention)
SERIAL_DIR=""
OUTPUT_DIR=""
BOXWARDEN_SRC=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --approve-execution)   APPROVED=1; shift ;;
    --gate-tart-home)      GATE_TART_HOME="${2:?}"; shift 2 ;;
    --candidate-name)      CANDIDATE_NAME="${2:?}"; shift 2 ;;
    --ubuntu-iso)          UBUNTU_ISO="${2:?}"; shift 2 ;;
    --password-hash-file)  PASSWORD_HASH_FILE="${2:?}"; shift 2 ;;
    --timezone)            TIMEZONE="${2:?}"; shift 2 ;;
    --run-id)              RUN_ID="${2:?}"; shift 2 ;;
    --serial-dir)          SERIAL_DIR="${2:?}"; shift 2 ;;
    --output-dir)          OUTPUT_DIR="${2:?}"; shift 2 ;;
    --boxwarden-src)       BOXWARDEN_SRC="${2:?}"; shift 2 ;;
    *) printf 'ERROR: unknown argument: %s\n' "$1" >&2; exit 1 ;;
  esac
done

if [[ "${APPROVED}" -ne 1 ]]; then
  printf 'NOT APPROVED FOR EXECUTION\n' >&2
  printf 'Review only. Re-run with --approve-execution after explicit approval.\n' >&2
  exit 2
fi

# Verify Bash capability: require version 5.x (associative arrays)
_bash_major="${BASH_VERSINFO[0]}"
[[ "${_bash_major}" -ge "${REQUIRED_BASH_MAJOR}" ]] || {
  printf 'ERROR: Bash major version %s < required %s (associative arrays needed)\n' \
    "${_bash_major}" "${REQUIRED_BASH_MAJOR}" >&2
  exit 1
}
# ---------------------------------------------------------------------------
# Utility functions
# ---------------------------------------------------------------------------
QUALIFY_LOG=""
TART_PID=""          # PID of background tart run; cleared after process exits
SERIAL_RELAY_PID=""  # PID of socat relay

log() {
  local msg
  msg="[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"
  printf '%s\n' "${msg}"
  [[ -n "${QUALIFY_LOG}" ]] && printf '%s\n' "${msg}" >> "${QUALIFY_LOG}"
}

fail() {
  printf 'QUALIFY FAIL [%s]: %s\n' "${QUALIFY_PHASE}" "$*" \
    | tee -a "${QUALIFY_LOG:-/dev/stderr}" >&2
  exit 1
}

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

sha256_file() { shasum -a 256 "$1" | awk '{print $1}'; }

# Python invocation with isolated mode
# Resolve Python at startup before py() is called
_resolve_python() {
  local _p _ver
  for _p in /opt/homebrew/bin/python3 /usr/local/bin/python3 /usr/bin/python3 python3; do
    command -v "${_p}" >/dev/null 2>&1 || continue
    _p="$(command -v "${_p}")"
    _ver="$(env -i PATH=/usr/bin:/bin "${_p}" -c 'import sys; print(sys.version_info.major)' 2>/dev/null)"
    [[ "${_ver:-0}" -ge "${REQUIRED_PYTHON_MAJOR}" ]] 2>/dev/null && { printf '%s' "${_p}"; return 0; }
  done
  return 1
}
_python_resolved_path="$(_resolve_python)" || {
  printf 'ERROR: no Python %d.x found\n' "${REQUIRED_PYTHON_MAJOR}" >&2; exit 1
}
_python_resolved_sha256="$(shasum -a 256 "${_python_resolved_path}" 2>/dev/null | awk '{print $1}')"
_bash_resolved_path="${BASH}"
_bash_resolved_sha256="$(shasum -a 256 "${_bash_resolved_path}" 2>/dev/null | awk '{print $1}')"
py() { "${_python_resolved_path}" -I "$@"; }

# Canonicalize a path without following symlinks in intermediate components.
# Uses Python os.path.realpath which is available on this host.
canon_path() {
  py -c "import os,sys; print(os.path.realpath(sys.argv[1]))" -- "$1"
}

# Resolve qualified Tart by prequalified SHA-256 (admission identity = digest).
# Path is observed on the execution host, not pre-populated.
_resolve_tart() {
  local _candidate _sha
  for _candidate in \
    "/opt/homebrew/Cellar/tart/${REQUIRED_TART_VERSION}/libexec/tart.app/Contents/MacOS/tart" \
    "$(command -v tart 2>/dev/null || true)"; do
    [[ -f "${_candidate}" ]] || continue
    _sha="$(shasum -a 256 "${_candidate}" | awk '{print $1}')"
    if [[ "${_sha}" == "${REQUIRED_TART_SHA256}" ]]; then
      printf '%s' "${_candidate}"
      return 0
    fi
  done
  return 1
}
_tart_resolved_path="$(_resolve_tart)" || {
  printf 'ERROR: no Tart binary with prequalified SHA-256 %s found\n' "${REQUIRED_TART_SHA256}" >&2
  exit 1
}
_tart_resolved_sha256="${REQUIRED_TART_SHA256}"  # by construction: _resolve_tart verified this
_tart_ver_check="$(env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C "${_tart_resolved_path}" --version 2>/dev/null)"
[[ "${_tart_ver_check}" == "${REQUIRED_TART_VERSION}" ]] || {
  printf 'ERROR: Tart at %s reports version %s, expected %s\n' \
    "${_tart_resolved_path}" "${_tart_ver_check}" "${REQUIRED_TART_VERSION}" >&2
  exit 1
}

# Closed-environment Tart invocation using the runtime-resolved binary.
gate_tart() {
  env -i \
    HOME="${HOME}" USER="${USER}" LOGNAME="${USER}" \
    PATH="/usr/bin:/bin" \
    TART_HOME="${GATE_TART_HOME}" \
    TART_NO_AUTO_PRUNE=1 \
    LANG=C LC_ALL=C \
    "${_tart_resolved_path}" "$@"
}

# ---------------------------------------------------------------------------
# Stop-and-preserve trap
# ---------------------------------------------------------------------------
QUALIFY_PHASE="pre-init"

cleanup_on_failure() {
  local exit_code=$?
  {
    printf '\n=== QUALIFY FAIL in phase: %s (exit %d) ===\n' "${QUALIFY_PHASE}" "${exit_code}"
    printf 'Do NOT delete candidate VM, serial state, or output directory.\n'
    printf 'CANDIDATE: %s  GATE_TART_HOME: %s\n' "${CANDIDATE_NAME}" "${GATE_TART_HOME}"
    if [[ -n "${TART_PID}" ]]; then
      printf 'TART_PID %s may still be running — do not kill without recording state.\n' "${TART_PID}"
    fi
  } | tee -a "${QUALIFY_LOG:-/dev/stderr}" >&2
  # Do not auto-kill Tart or clean up serial — preserve evidence
  exit "${exit_code}"
}
trap cleanup_on_failure ERR

# ---------------------------------------------------------------------------
# Phase 0: Preflight
# ---------------------------------------------------------------------------
QUALIFY_PHASE="preflight"

[[ -n "${GATE_TART_HOME}" ]]    || die "--gate-tart-home is required"
[[ -n "${CANDIDATE_NAME}" ]]    || die "--candidate-name is required"
[[ -n "${UBUNTU_ISO}" ]]        || die "--ubuntu-iso is required (canonical Ubuntu 24.04.4 Desktop ARM64 ISO)"
[[ -n "${PASSWORD_HASH_FILE}" ]] || die "--password-hash-file is required (SHA-512 crypt hash; not recorded in evidence)"
[[ -n "${TIMEZONE}" ]]          || die "--timezone is required (e.g. America/Denver)"
[[ -n "${SERIAL_DIR}" ]]        || die "--serial-dir is required"
[[ -n "${OUTPUT_DIR}" ]]        || die "--output-dir is required"
[[ -n "${BOXWARDEN_SRC}" ]]     || die "--boxwarden-src is required (git repo containing PR #1 commit ${PR1_SOURCE_COMMIT})"

# All paths must be absolute
for _arg_name in GATE_TART_HOME CANDIDATE_NAME UBUNTU_ISO PASSWORD_HASH_FILE SERIAL_DIR OUTPUT_DIR BOXWARDEN_SRC; do
  _val="${!_arg_name}"
  [[ "${_val}" == /* ]] || die "--${_arg_name,,} must be absolute: ${_val}"
done

[[ "${CANDIDATE_NAME}" == ${GATE_VM_PREFIX}* ]] || \
  die "candidate name must start with '${GATE_VM_PREFIX}': ${CANDIDATE_NAME}"
[[ -f "${UBUNTU_ISO}" ]]           || die "Ubuntu ISO not found: ${UBUNTU_ISO}"
[[ -f "${PASSWORD_HASH_FILE}" ]]   || die "password hash file not found: ${PASSWORD_HASH_FILE}"

# Verify qualified binaries
# Tart: prequalified SHA-256 already verified via _resolve_tart() above.
# Python: resolved at startup via _resolve_python(). Confirm resolution succeeded.
[[ -n "${_python_resolved_path}" ]] || die "Python resolution failed"
[[ -n "${_tart_resolved_path}" ]]   || die "Tart resolution failed"

# Resolve socat at runtime: require version 1.8.1.3, record identity in evidence
_socat_resolved_path="$(command -v socat 2>/dev/null)" || true
[[ -f "${_socat_resolved_path}" ]] || die "socat not found in PATH; install socat ${REQUIRED_SOCAT_VERSION}"
_socat_resolved_sha256="$(sha256_file "${_socat_resolved_path}")"
_socat_ver="$(env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C "${_socat_resolved_path}" -V 2>&1 | awk '/socat version/ {print $3}' | head -1)"
[[ "${_socat_ver}" == "${REQUIRED_SOCAT_VERSION}" ]] || \
  die "socat version mismatch: expected ${REQUIRED_SOCAT_VERSION}, got ${_socat_ver}"

# Resolve xorriso at runtime: require version 1.5.8.pl02, record identity in evidence
_xorriso_resolved_path="$(command -v xorriso 2>/dev/null)" || true
[[ -f "${_xorriso_resolved_path}" ]] || die "xorriso not found in PATH; install xorriso ${REQUIRED_XORRISO_VERSION}"
_xorriso_resolved_sha256="$(sha256_file "${_xorriso_resolved_path}")"
_xorriso_ver="$(env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C "${_xorriso_resolved_path}" --version 2>&1 | head -1)"
[[ "${_xorriso_ver}" == *"${REQUIRED_XORRISO_VERSION}"* ]] || \
  die "xorriso version mismatch: expected ${REQUIRED_XORRISO_VERSION}, got ${_xorriso_ver}"

# Item 1: Verify boxwarden source repository contains the required PR #1 commit
[[ -d "${BOXWARDEN_SRC}/.git" ]] || \
  die "BOXWARDEN_SRC is not a git repository: ${BOXWARDEN_SRC}"
git -C "${BOXWARDEN_SRC}" cat-file -t "${PR1_SOURCE_COMMIT}" >/dev/null 2>&1 || \
  die "PR #1 source commit ${PR1_SOURCE_COMMIT} not found in BOXWARDEN_SRC"

# GATE_TART_HOME and OUTPUT_DIR must not overlap each other or normal Tart home
_normal_tart_home="$(canon_path "${HOME}/.tart")"
_canon_gate_tart="$(canon_path "${GATE_TART_HOME}")"
_canon_output="$(canon_path "${OUTPUT_DIR}")"
_canon_serial="$(canon_path "${SERIAL_DIR}")"

_overlap_check() {
  local _a="$1" _b="$2"
  if [[ "${_a}" == "${_b}" ]] || \
     [[ "${_a}" == "${_b}"/* ]] || \
     [[ "${_b}" == "${_a}"/* ]]; then
    die "path overlap: '${_a}' and '${_b}' must be distinct subtrees"
  fi
}
_overlap_check "${_canon_gate_tart}" "${_normal_tart_home}"
_overlap_check "${_canon_gate_tart}" "${_canon_output}"
_overlap_check "${_canon_gate_tart}" "${_canon_serial}"
_overlap_check "${_canon_output}"    "${_canon_serial}"

# GATE_TART_HOME must exist (created by caller before execution)
[[ -d "${GATE_TART_HOME}" ]] || die "GATE_TART_HOME does not exist: ${GATE_TART_HOME}"

# Candidate VM must not already exist
_pre_existing="$(gate_tart list --format json | \
  py -c "import sys,json; names=[v.get('Name') for v in json.load(sys.stdin)]; print('yes' if '${CANDIDATE_NAME}' in names else 'no')")"
[[ "${_pre_existing}" == "no" ]] || \
  die "candidate VM '${CANDIDATE_NAME}' already exists in GATE_TART_HOME"

# Item 3: At the start of artifact construction, GATE_TART_HOME must contain
# ZERO Tart objects — no leftover VMs from prior runs.
_initial_count="$(gate_tart list --format json | py -c "import sys,json; print(len(json.load(sys.stdin)))")"
[[ "${_initial_count}" -eq 0 ]] || \
  die "GATE_TART_HOME must contain zero Tart objects at construction start (found ${_initial_count})"

# Set up output directory and log
umask 077
mkdir -p "${OUTPUT_DIR}"
chmod 0700 "${OUTPUT_DIR}"
QUALIFY_LOG="${OUTPUT_DIR}/qualify.log"
touch "${QUALIFY_LOG}"
chmod 0600 "${QUALIFY_LOG}"

# Scratch directory for serial captures — under OUTPUT_DIR, not /tmp
SCRATCH_DIR="${OUTPUT_DIR}/scratch"
mkdir -p "${SCRATCH_DIR}"
chmod 0700 "${SCRATCH_DIR}"

STARTED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
log "=== Artifact Construction + Qualification started ==="
log "Bash:            ${BASH} (SHA-256: ${_bash_resolved_sha256})"
log "Tart:            ${_tart_resolved_path} (SHA-256: ${_tart_resolved_sha256})"
log "Python:          ${_python_resolved_path} (SHA-256: ${_python_resolved_sha256})"
log "Socat:           ${_socat_resolved_path} (SHA-256: ${_socat_resolved_sha256})"
log "Boxwarden src:   ${BOXWARDEN_SRC} at commit ${PR1_SOURCE_COMMIT}"
log "CANDIDATE:       ${CANDIDATE_NAME}"
log "GATE_TART_HOME:  ${GATE_TART_HOME}"
log "UBUNTU_ISO:      ${UBUNTU_ISO}"
log "TIMEZONE:        ${TIMEZONE}"
log "RUN_ID:          ${RUN_ID}"
log "SERIAL_DIR:      ${SERIAL_DIR}"
log "OUTPUT_DIR:      ${OUTPUT_DIR}"

# ---------------------------------------------------------------------------
# Phase C: Construct candidate VM and run installation
# ---------------------------------------------------------------------------
QUALIFY_PHASE="construct"

log "=== Phase C: build-input provenance and candidate construction ==="

# ---------------------------------------------------------------------------
# C-1: Verify source checkout at exact PR #1 commit
# ---------------------------------------------------------------------------
BUILD_WORKTREE="${SCRATCH_DIR}/build-src"
git -C "${BOXWARDEN_SRC}" worktree add --detach "${BUILD_WORKTREE}" "${PR1_SOURCE_COMMIT}" 2>&1 | \
  while IFS= read -r _line; do log "  git: ${_line}"; done

_actual_src_commit="$(git -C "${BUILD_WORKTREE}" rev-parse HEAD)"
[[ "${_actual_src_commit}" == "${PR1_SOURCE_COMMIT}" ]] || \
  fail "build worktree HEAD is ${_actual_src_commit}, expected ${PR1_SOURCE_COMMIT}"
_src_dirty="$(git -C "${BUILD_WORKTREE}" status --porcelain)"
[[ -z "${_src_dirty}" ]] || fail "build worktree is not clean: ${_src_dirty}"
log "Build source: commit ${_actual_src_commit}, worktree clean"

_user_data_template="${BUILD_WORKTREE}/guest/ubuntu-24.04-arm64/autoinstall/user-data"
_bootstrap_script="${BUILD_WORKTREE}/scripts/spike/bootstrap-tart.sh"
_finalize_script="${BUILD_WORKTREE}/scripts/spike/finalize-clone.sh"
[[ -f "${_user_data_template}" ]] || fail "user-data template not found in build worktree"
[[ -f "${_bootstrap_script}" ]]   || fail "bootstrap-tart.sh not found in build worktree"
[[ -f "${_finalize_script}" ]]    || fail "finalize-clone.sh not found in build worktree"

PROV_USER_DATA_TEMPLATE_SHA256="$(sha256_file "${_user_data_template}")"
PROV_BOOTSTRAP_SCRIPT_SHA256="$(sha256_file "${_bootstrap_script}")"
PROV_FINALIZE_SCRIPT_SHA256="$(sha256_file "${_finalize_script}")"

# Validate all source SHA-256s are 64 lowercase hex chars
validate_sha256_hex() {
  local label="$1" value="$2"
  py -I -c "
import sys, re
v = sys.argv[1]
if not re.fullmatch(r'[0-9a-f]{64}', v):
    print('FAIL: %s is not a valid SHA-256 hex digest: %r' % (sys.argv[2], v), file=sys.stderr)
    sys.exit(1)
" -- "${value}" "${label}" || fail "SHA-256 hex validation failed for ${label}"
}
validate_sha256_hex "user_data_template_sha256"    "${PROV_USER_DATA_TEMPLATE_SHA256}"
validate_sha256_hex "bootstrap_tart_sha256"        "${PROV_BOOTSTRAP_SCRIPT_SHA256}"
validate_sha256_hex "finalize_clone_sha256"        "${PROV_FINALIZE_SCRIPT_SHA256}"

log "Provenance — user-data template SHA-256:  ${PROV_USER_DATA_TEMPLATE_SHA256}"
log "Provenance — bootstrap-tart.sh SHA-256:   ${PROV_BOOTSTRAP_SCRIPT_SHA256}"
log "Provenance — finalize-clone.sh SHA-256:   ${PROV_FINALIZE_SCRIPT_SHA256}"

# ---------------------------------------------------------------------------
# C-2: Verify canonical Ubuntu source ISO
# The supplied --ubuntu-iso MUST match the Task 0 qualified SHA-256 exactly.
# ---------------------------------------------------------------------------
PROV_UBUNTU_ISO_SHA256="$(sha256_file "${UBUNTU_ISO}")"
validate_sha256_hex "canonical_ubuntu_iso_sha256" "${PROV_UBUNTU_ISO_SHA256}"
[[ "${PROV_UBUNTU_ISO_SHA256}" == "${CANONICAL_UBUNTU_ISO_SHA256}" ]] || \
  fail "Ubuntu ISO SHA-256 mismatch: expected ${CANONICAL_UBUNTU_ISO_SHA256}, got ${PROV_UBUNTU_ISO_SHA256}"
log "Canonical Ubuntu ISO SHA-256 verified: ${PROV_UBUNTU_ISO_SHA256}"

# ---------------------------------------------------------------------------
# C-3: Render user-data from the verified source checkout
# The bootstrap-tart.sh render-seed function is sourced from the verified
# worktree. The password hash is provided at runtime and NOT recorded in
# evidence (sensitive input).
# ---------------------------------------------------------------------------
[[ -f "${PASSWORD_HASH_FILE}" ]] || fail "password hash file not found: ${PASSWORD_HASH_FILE}"
[[ -n "${TIMEZONE}" ]]           || fail "--timezone is required (e.g. America/Denver)"
[[ "${RUN_ID}" =~ ^run-[12]$ ]]  || fail "--run-id must be run-1 or run-2"

RENDERED_SEED_DIR="${SCRATCH_DIR}/rendered-seed"
mkdir -p "${RENDERED_SEED_DIR}"
chmod 0700 "${RENDERED_SEED_DIR}"

# Source the render_seed and remaster_iso functions from the VERIFIED checkout.
# We call them directly with the verified script path, not via PATH.
# shellcheck source=/dev/null
source "${_bootstrap_script}"

# render_seed uses BASH_SOURCE internals to find the template; we cd into
# the checkout so its repo_root detection works correctly.
(
  cd "${BUILD_WORKTREE}"
  render_seed "${RUN_ID}" "${PASSWORD_HASH_FILE}" "${TIMEZONE}" "${RENDERED_SEED_DIR}"
)
RENDERED_USER_DATA="${RENDERED_SEED_DIR}/user-data"
[[ -f "${RENDERED_USER_DATA}" ]] || fail "render_seed did not produce user-data"

PROV_RENDERED_USER_DATA_SHA256="$(sha256_file "${RENDERED_USER_DATA}")"
validate_sha256_hex "rendered_user_data_sha256" "${PROV_RENDERED_USER_DATA_SHA256}"
log "Provenance — rendered user-data SHA-256: ${PROV_RENDERED_USER_DATA_SHA256}"

# ---------------------------------------------------------------------------
# C-4: Remaster ISO from the verified source checkout
# The remaster_iso function is sourced from the same verified checkout.
# ---------------------------------------------------------------------------
REMASTERED_ISO="${SCRATCH_DIR}/installer.iso"
(
  cd "${BUILD_WORKTREE}"
  # Export PATH so sourced remaster_iso finds the qualified xorriso binary
  _xorriso_dir="$(dirname "${_xorriso_resolved_path}")"
  export PATH="${_xorriso_dir}:${PATH}"
  remaster_iso "${UBUNTU_ISO}" "${RENDERED_USER_DATA}" "${REMASTERED_ISO}"
)
[[ -f "${REMASTERED_ISO}" ]] || fail "remaster_iso did not produce an ISO"

PROV_REMASTERED_ISO_SHA256="$(sha256_file "${REMASTERED_ISO}")"
validate_sha256_hex "remastered_iso_sha256" "${PROV_REMASTERED_ISO_SHA256}"
log "Provenance — remastered ISO SHA-256: ${PROV_REMASTERED_ISO_SHA256}"

# ---------------------------------------------------------------------------
# C-5: Extract embedded autoinstall/user-data from remastered ISO and verify
# byte-for-byte equality with the rendered user-data from this exact run.
# xorriso is required; failure here fails closed.
# ---------------------------------------------------------------------------
[[ -f "${_xorriso_resolved_path}" ]] || fail "xorriso not found at ${_xorriso_resolved_path}"

EXTRACTED_USER_DATA="${SCRATCH_DIR}/extracted-user-data.yaml"
if ! "${_xorriso_resolved_path}" -osirrox on -indev "${REMASTERED_ISO}" \
    -extract /autoinstall.yaml "${EXTRACTED_USER_DATA}" >/dev/null 2>&1; then
  fail "xorriso could not extract /autoinstall.yaml from remastered ISO"
fi
[[ -f "${EXTRACTED_USER_DATA}" ]] || fail "extracted user-data file not produced"

EXTRACTED_SHA256="$(sha256_file "${EXTRACTED_USER_DATA}")"
validate_sha256_hex "extracted_embedded_sha256" "${EXTRACTED_SHA256}"
log "Extracted embedded user-data SHA-256: ${EXTRACTED_SHA256}"

[[ "${EXTRACTED_SHA256}" == "${PROV_RENDERED_USER_DATA_SHA256}" ]] || \
  fail "PROVENANCE MISMATCH: embedded user-data SHA-256 (${EXTRACTED_SHA256}) != rendered user-data SHA-256 (${PROV_RENDERED_USER_DATA_SHA256})"
log "Byte-for-byte equality confirmed: embedded == rendered user-data"

# ---------------------------------------------------------------------------
# C-6: Create candidate VM and install from the verified remastered ISO
# ---------------------------------------------------------------------------
gate_tart create "${CANDIDATE_NAME}" --linux --disk-size 30
gate_tart set "${CANDIDATE_NAME}" --cpu 4 --memory 4096 --display 1600x1000
log "Candidate VM created: ${CANDIDATE_NAME}"

log "Running installation (blocking until installer powers off)..."
# run-install is fully blocking; install ISO causes an automatic shutdown
# at the end of unattended installation, returning tart run exit code 0.
# socat relay managed by start_serial_relay/cleanup (from bootstrap-tart.sh
# infrastructure reused here in-process).

# Create private serial runtime under scratch
SERIAL_RUNTIME="${SCRATCH_DIR}/serial-install"
mkdir -p "${SERIAL_RUNTIME}"
chmod 0700 "${SERIAL_RUNTIME}"

SERIAL_TART_LINK="${SERIAL_RUNTIME}/tart-serial"
SERIAL_OPERATOR_LINK="${SERIAL_RUNTIME}/operator-serial"
SERIAL_RELAY_LOG="${SERIAL_RUNTIME}/relay.log"

"${_socat_resolved_path}" -d -d \
  "pty,raw,echo=0,ignoreeof,link=${SERIAL_TART_LINK}" \
  "pty,raw,echo=0,ignoreeof,link=${SERIAL_OPERATOR_LINK}" \
  >>"${SERIAL_RELAY_LOG}" 2>&1 &
SERIAL_RELAY_PID=$!
log "Serial relay PID: ${SERIAL_RELAY_PID}"

# Wait for socat PTY links to appear (up to 10s)
_wait=0
until [[ -L "${SERIAL_TART_LINK}" && -L "${SERIAL_OPERATOR_LINK}" ]]; do
  sleep 0.2
  _wait=$(( _wait + 1 ))
  [[ "${_wait}" -lt 50 ]] || fail "serial PTY links did not appear within 10s"
done
chmod 0600 "$(readlink "${SERIAL_TART_LINK}")"
chmod 0600 "$(readlink "${SERIAL_OPERATOR_LINK}")"
log "Serial PTY links ready (install): tart=${SERIAL_TART_LINK} operator=${SERIAL_OPERATOR_LINK}"

# Launch installation as blocking foreground with serial
env -i \
  HOME="${HOME}" USER="${USER}" LOGNAME="${USER}" \
  PATH="/usr/bin:/bin" \
  TART_HOME="${GATE_TART_HOME}" \
  TART_NO_AUTO_PRUNE=1 \
  LANG=C LC_ALL=C \
  "${_tart_resolved_path}" run \
    --disk="${REMASTERED_ISO}:ro" \
    --serial-path="${SERIAL_TART_LINK}" \
    --net-softnet \
    --no-audio \
    --no-clipboard \
    "${CANDIDATE_NAME}"
log "Installation complete (tart run exited). Candidate is stopped."

# Cleanup install relay
kill "${SERIAL_RELAY_PID}" 2>/dev/null || true
wait "${SERIAL_RELAY_PID}" 2>/dev/null || true
SERIAL_RELAY_PID=""
rm -f "${SERIAL_TART_LINK}" "${SERIAL_OPERATOR_LINK}"

# Verify candidate is stopped
_post_install_state="$(gate_tart list --format json | \
  py -c "
import sys,json
for v in json.load(sys.stdin):
    if v.get('Name')=='${CANDIDATE_NAME}':
        print(v.get('State','unknown')); sys.exit(0)
print('not-found')
")"
[[ "${_post_install_state}" == "stopped" ]] || \
  fail "candidate state after installation: '${_post_install_state}', expected 'stopped'"
log "Candidate confirmed stopped after installation."

# ---------------------------------------------------------------------------
# Phase R: Launch candidate for finalization and qualification
# Binding assertion: this script creates the serial PTY pair, passes
# serial_tart_path to tart run, and captures TART_PID. All serial observations
# occur while TART_PID is live. The stopped artifact hashed in Phase P is
# the same VM object this script launched.
# ---------------------------------------------------------------------------
QUALIFY_PHASE="run"

log "=== Phase R: launch candidate for qualification ==="

SERIAL_QUALIFY_RUNTIME="${SCRATCH_DIR}/serial-qualify"
mkdir -p "${SERIAL_QUALIFY_RUNTIME}"
chmod 0700 "${SERIAL_QUALIFY_RUNTIME}"

QUALIFY_TART_LINK="${SERIAL_QUALIFY_RUNTIME}/tart-serial"
QUALIFY_OPERATOR_LINK="${SERIAL_QUALIFY_RUNTIME}/operator-serial"
QUALIFY_RELAY_LOG="${SERIAL_QUALIFY_RUNTIME}/relay.log"

"${_socat_resolved_path}" -d -d \
  "pty,raw,echo=0,ignoreeof,link=${QUALIFY_TART_LINK}" \
  "pty,raw,echo=0,ignoreeof,link=${QUALIFY_OPERATOR_LINK}" \
  >>"${QUALIFY_RELAY_LOG}" 2>&1 &
SERIAL_RELAY_PID=$!

# Wait for PTY links
_wait=0
until [[ -L "${QUALIFY_TART_LINK}" && -L "${QUALIFY_OPERATOR_LINK}" ]]; do
  sleep 0.2
  _wait=$(( _wait + 1 ))
  [[ "${_wait}" -lt 50 ]] || fail "qualify serial PTY links did not appear within 10s"
done
chmod 0600 "$(readlink "${QUALIFY_TART_LINK}")"
chmod 0600 "$(readlink "${QUALIFY_OPERATOR_LINK}")"
log "Serial PTY links ready (qualify): tart=${QUALIFY_TART_LINK} operator=${QUALIFY_OPERATOR_LINK}"

# Launch candidate in background; capture PID for binding assertion
env -i \
  HOME="${HOME}" USER="${USER}" LOGNAME="${USER}" \
  PATH="/usr/bin:/bin" \
  TART_HOME="${GATE_TART_HOME}" \
  TART_NO_AUTO_PRUNE=1 \
  LANG=C LC_ALL=C \
  "${_tart_resolved_path}" run \
    --serial-path="${QUALIFY_TART_LINK}" \
    --net-softnet \
    --no-audio \
    --no-clipboard \
    "${CANDIDATE_NAME}" &
TART_PID=$!
log "Tart running in background: PID ${TART_PID}, VM ${CANDIDATE_NAME}"
log "BINDING: serial path ${QUALIFY_TART_LINK} was passed to Tart PID ${TART_PID}"
log "         Operator PTY ${QUALIFY_OPERATOR_LINK} communicates to that exact process."

# Verify candidate is running in GATE_TART_HOME
sleep 3
_run_state="$(gate_tart list --format json | \
  py -c "
import sys,json
for v in json.load(sys.stdin):
    if v.get('Name')=='${CANDIDATE_NAME}':
        print(v.get('State','unknown')); sys.exit(0)
print('not-found')
")"
[[ "${_run_state}" == "running" ]] || \
  fail "candidate '${CANDIDATE_NAME}' expected running after tart run, got '${_run_state}'"
log "Candidate confirmed running in GATE_TART_HOME (TART_PID=${TART_PID})."

# ---------------------------------------------------------------------------
declare -A CHECK_RAW_B64    # base64-encoded raw PTY bytes
declare -A CHECK_NONCE_USED # nonce used (for V2 consumer to re-validate)
declare -A CHECK_PARSED_STATUS  # parsed exit status from frame
declare -A CHECK_RESULT     # derived: PASS or FAIL

generate_nonce() {
  xxd -l 16 -p /dev/urandom | tr -d '\n'
}

# Read from operator PTY until STX+ETX frame found or timeout.
# Writes raw bytes to a scratch file under SCRATCH_DIR.
# Outputs the raw capture file path.
read_to_frame() {
  local check_id="$1"
  local raw_file="${SCRATCH_DIR}/raw-${check_id}.bin"
  local pty_dev
  pty_dev="$(readlink "${QUALIFY_OPERATOR_LINK}")"
  [[ -c "${pty_dev}" ]] || fail "operator PTY not a character device for check '${check_id}'"

  local deadline=$(( $(date +%s) + SERIAL_CMD_TIMEOUT_SECS ))
  local total_bytes=0
  local found_stx=0
  local found_etx=0
  local raw_line

  : > "${raw_file}"
  while [[ $(date +%s) -lt "${deadline}" ]]; do
    if IFS= read -r -t 0.2 raw_line < "${pty_dev}" 2>/dev/null; then
      total_bytes=$(( total_bytes + ${#raw_line} + 1 ))
      [[ "${total_bytes}" -le "${SERIAL_MAX_RAW_BYTES}" ]] || \
        fail "check '${check_id}' raw output exceeded ${SERIAL_MAX_RAW_BYTES}-byte bound"
      printf '%s\n' "${raw_line}" >> "${raw_file}"
      [[ "${found_stx}" -eq 0 ]] && \
        printf '%s' "${raw_line}" | grep -qF "${FRAME_STX}" && found_stx=1
      [[ "${found_stx}" -eq 1 ]] && \
        printf '%s' "${raw_line}" | grep -qF "${FRAME_ETX}" && { found_etx=1; break; }
    fi
  done

  [[ "${found_stx}" -eq 1 ]] || fail "check '${check_id}': no STX in PTY output (timeout)"
  [[ "${found_etx}" -eq 1 ]] || fail "check '${check_id}': STX found but no ETX (timeout/truncated)"
  printf '%s' "${raw_file}"
}

# Run one framed check: send command, read frame, validate, record result
framed_check() {
  local check_id="$1"
  local check_cmd="$2"

  local nonce
  nonce="$(generate_nonce)"
  [[ ${#nonce} -eq 32 ]] || fail "nonce generation failed"
  CHECK_NONCE_USED["${check_id}"]="${nonce}"

  local pty_dev
  pty_dev="$(readlink "${QUALIFY_OPERATOR_LINK}")"

  # Build and send command. The printf format uses \x02 and \x03 which the
  # guest shell interprets as STX/ETX — the host sends the literal escape
  # sequences as printable ASCII so PTY echo cannot form a valid frame.
  local frame_cmd
  frame_cmd="( ${check_cmd} ); _S=\$?; printf '\\x02${FRAME_TAG} ${nonce} ${check_id} %d\\x03\\n' \"\${_S}\""
  printf '%s\n' "${frame_cmd}" > "${pty_dev}"

  log "  Sent framed command for check '${check_id}' (nonce=${nonce})"

  # Read raw PTY output to scratch file
  local raw_file
  raw_file="$(read_to_frame "${check_id}")"

  # Base64-encode raw bytes for evidence record (no /tmp paths in evidence)
  CHECK_RAW_B64["${check_id}"]="$(base64 < "${raw_file}" | tr -d '\n')"

  # Parse frame: extract content between STX and ETX
  local frame_content
  frame_content="$(py -I -c "
import sys, re, base64
raw = open(sys.argv[1], 'rb').read()
matches = re.findall(b'\x02([^\x03]+)\x03', raw)
if len(matches) == 0: print('ERROR:no_frame'); sys.exit(0)
if len(matches) > 1:  print('ERROR:duplicate_frame'); sys.exit(0)
print(matches[0].decode('ascii','replace').strip())
" -- "${raw_file}")"

  case "${frame_content}" in
    ERROR:no_frame)        fail "check '${check_id}': no valid STX/ETX frame in raw output" ;;
    ERROR:duplicate_frame) fail "check '${check_id}': duplicate frames in raw output" ;;
  esac

  # Validate frame fields
  local frame_tag frame_nonce frame_check_id frame_status
  read -r frame_tag frame_nonce frame_check_id frame_status <<< "${frame_content}" || \
    fail "check '${check_id}': malformed frame: '${frame_content}'"

  [[ "${frame_tag}"      == "${FRAME_TAG}"  ]] || fail "check '${check_id}': wrong tag '${frame_tag}'"
  [[ "${frame_nonce}"    == "${nonce}"      ]] || fail "check '${check_id}': nonce mismatch"
  [[ "${frame_check_id}" == "${check_id}"  ]] || fail "check '${check_id}': check_id mismatch (got '${frame_check_id}')"
  [[ "${frame_status}"   =~ ^[0-9]+$       ]] || fail "check '${check_id}': non-numeric status '${frame_status}'"
  [[ "${frame_status}"   -eq 0             ]] || fail "check '${check_id}': nonzero exit status ${frame_status}"

  CHECK_PARSED_STATUS["${check_id}"]="${frame_status}"
  CHECK_RESULT["${check_id}"]="PASS"
  log "  check '${check_id}': PASS (exit_status=0, nonce validated)"
}

# Phase F: Transfer and bind finalize-clone.sh, then operator runs it
# ---------------------------------------------------------------------------
QUALIFY_PHASE="finalize"

log "=== Phase F: operator finalization with machine-verified byte binding ==="

# ITEM 4 (Correction 2): finalize-clone.sh binding via framed serial protocol.
# _finalize_script was verified from the PR #1 checkout in Phase C.
# PROV_FINALIZE_SCRIPT_SHA256 is its verified host-side SHA-256.
# Procedure:
#   (a) operator transfers the verified file to the guest (attended step);
#   (b) this script verifies guest-side SHA-256 via framed serial (machine-derived);
#   (c) this script executes the file at the verified guest path via framed serial;
#   (d) framed exit_status == 0 is required; raw bytes are retained in evidence.
[[ -f "${_finalize_script}" ]] || fail "finalize-clone.sh from Phase C no longer accessible"

FINALIZE_GUEST_PATH="/tmp/boxwarden-gate-finalize-${PROV_FINALIZE_SCRIPT_SHA256:0:12}.sh"

cat <<TRANSFER_INSTRUCTIONS
=============================================================================
OPERATOR: Transfer the verified finalize-clone.sh to the guest.
Transfer from:  ${_finalize_script}
  Host SHA-256: ${PROV_FINALIZE_SCRIPT_SHA256}
To guest path:  ${FINALIZE_GUEST_PATH}

The gate will machine-verify the guest-side SHA-256 via the bound serial
channel. You do NOT need to type or confirm the hash.
After transferring: detach from the serial console (Ctrl-A d) and confirm.
=============================================================================
TRANSFER_INSTRUCTIONS

read -r -p "Has the file been transferred to ${FINALIZE_GUEST_PATH}? [yes/NO]: " _transfer_confirm
[[ "${_transfer_confirm}" == "yes" ]] || \
  fail "operator did not confirm file transfer; stopping before machine hash verification"
log "Operator confirmed file transfer. Proceeding to machine-derived hash verification."

# Framed serial check (a): verify guest-side SHA-256 == host-verified source SHA-256.
# sha256sum output format: "<hash>  <path>" — we grep for the exact expected hash.
FINALIZE_HASH_CHECK_CMD="sha256sum ${FINALIZE_GUEST_PATH} 2>/dev/null | grep -qF '${PROV_FINALIZE_SCRIPT_SHA256}'"
log "Framed serial check: guest-side SHA-256 of ${FINALIZE_GUEST_PATH}"
framed_check "finalize_hash_verify" "${FINALIZE_HASH_CHECK_CMD}"
log "Machine-verified: guest-side finalize-clone.sh SHA-256 == ${PROV_FINALIZE_SCRIPT_SHA256}"

# Framed serial check (b): execute the verified guest path.
# The finalize script exits 0 on success.
log "Framed serial check: executing finalize-clone.sh at verified guest path"
framed_check "finalize_execute" \
  "chmod +x ${FINALIZE_GUEST_PATH} && ${FINALIZE_GUEST_PATH} --acknowledge-clone-finalization --serial-device hvc0"
log "finalize-clone.sh execution: framed exit_status=0 confirmed."

cat <<FINALIZE_DONE
=============================================================================
finalize-clone.sh executed successfully (machine-verified exit_status=0).
Expected terminal output: "clone-ready identity removed; power off without rebooting"
DO NOT power off manually — the gate will send the poweroff command.
=============================================================================
FINALIZE_DONE

# Verify candidate still running under the same TART_PID
kill -0 "${TART_PID}" 2>/dev/null || \
  fail "Tart process PID ${TART_PID} is no longer running after finalization"
log "Confirmed: Tart PID ${TART_PID} still live after finalization."

# Phase Q: Seven framed serial postcondition checks
#
# Protocol: per check, generate a 32-hex nonce, build a guest command that
# runs the check and then emits STX BWQF <NONCE> <CHECK_ID> <STATUS> ETX.
# STX/ETX are nonprinting (0x02/0x03) — the host sends the command as
# printable ASCII, so PTY echo cannot produce a valid STX...ETX frame.
# PASS is derived solely from parsed exit_status == 0, nonce match, and
# check_id match. Prose labels are for readability only.
# ---------------------------------------------------------------------------
QUALIFY_PHASE="serial-checks"

log "=== Phase Q: framed serial postcondition checks ==="

framed_check "c1_active_absent" \
  "test ! -e /etc/ssh/boxwarden/active"
framed_check "c2_ca_absent" \
  "test ! -e /etc/ssh/boxwarden/active/trusted-user-ca.pub"
framed_check "c3_machine_id_empty" \
  "content=\$(cat /etc/machine-id | tr -d '[:space:]'); test -z \"\${content}\""
framed_check "c4_hostkeys_absent" \
  "count=\$(ls /etc/ssh/ssh_host_*_key 2>/dev/null | wc -l | tr -d ' '); test \"\${count}\" -eq 0"
framed_check "c5_clone_ready" \
  "test -f /var/lib/boxwarden-task0/clone-ready"
framed_check "c6_firstboot_service" \
  "systemctl is-enabled boxwarden-task0-firstboot-identity.service"
framed_check "c7_sshd_config" \
  "grep -qF 'TrustedUserCAKeys /etc/ssh/boxwarden/active/trusted-user-ca.pub' /etc/ssh/sshd_config.d/60-boxwarden-task0.conf"

log "All seven framed checks passed (exit_status=0 from nonce-validated frames)."

# ---------------------------------------------------------------------------
# Phase P: sync+poweroff, wait for Tart exit, hash stopped payloads
# ---------------------------------------------------------------------------
QUALIFY_PHASE="poweroff-and-hash"

log "=== Phase P: poweroff and hash ==="

# Binding: TART_PID is still the process we launched in Phase R.
kill -0 "${TART_PID}" 2>/dev/null || \
  fail "Tart PID ${TART_PID} exited unexpectedly before poweroff command"

_pty_dev_poweroff="$(readlink "${QUALIFY_OPERATOR_LINK}")"
printf 'sync && sudo -n poweroff\n' > "${_pty_dev_poweroff}"
log "poweroff command sent to PID ${TART_PID}"

# Wait for the background tart run to exit
wait "${TART_PID}" || true
TART_EXIT_STATUS=$?
TART_PID=""
log "Tart process exited (status ${TART_EXIT_STATUS})."
STOPPED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

# Cleanup qualify relay
kill "${SERIAL_RELAY_PID}" 2>/dev/null || true
wait "${SERIAL_RELAY_PID}" 2>/dev/null || true
SERIAL_RELAY_PID=""
rm -f "${QUALIFY_TART_LINK}" "${QUALIFY_OPERATOR_LINK}"

# Verify candidate is stopped in GATE_TART_HOME
_stopped_state="$(gate_tart list --format json | \
  py -c "
import sys,json
for v in json.load(sys.stdin):
    if v.get('Name')=='${CANDIDATE_NAME}':
        print(v.get('State','unknown')); sys.exit(0)
print('not-found')
")"
[[ "${_stopped_state}" == "stopped" ]] || \
  fail "candidate state after poweroff: '${_stopped_state}', expected 'stopped'"
log "Candidate confirmed stopped after poweroff."

# Get Tart list entry for the stopped artifact
ARTIFACT_TART_ENTRY="$(gate_tart list --format json | \
  py -c "
import sys,json
for v in json.load(sys.stdin):
    if v.get('Name')=='${CANDIDATE_NAME}':
        print(json.dumps(v,sort_keys=True)); sys.exit(0)
")"

# Hash all payload files; fail if any are absent
VM_DIR="${GATE_TART_HOME}/vms/${CANDIDATE_NAME}"
[[ -d "${VM_DIR}" ]] || fail "VM directory not found: ${VM_DIR}"

log "Computing payload SHA-256 hashes..."
for _mandatory in disk.img config.json nvram.bin; do
  [[ -f "${VM_DIR}/${_mandatory}" ]] || \
    fail "mandatory payload absent: ${VM_DIR}/${_mandatory}"
done

DISK_SHA256="$(sha256_file "${VM_DIR}/disk.img")"
CONFIG_SHA256="$(sha256_file "${VM_DIR}/config.json")"
NVRAM_SHA256="$(sha256_file "${VM_DIR}/nvram.bin")"
log "  disk.img:    ${DISK_SHA256}"
log "  config.json: ${CONFIG_SHA256}"
log "  nvram.bin:   ${NVRAM_SHA256}"

# Enumerate all files in VM directory
VM_FILE_JSON="$(py -I -c "
import os, json, subprocess, sys
vm_dir = sys.argv[1]
result = {}
for fname in sorted(os.listdir(vm_dir)):
    fpath = os.path.join(vm_dir, fname)
    if os.path.isfile(fpath):
        h = subprocess.check_output(['shasum','-a','256',fpath]).decode().split()[0]
        result[fname] = h
print(json.dumps(result, sort_keys=True))
" -- "${VM_DIR}")"

# ---------------------------------------------------------------------------
# Phase E: Write structured content-bound machine-generated JSON evidence
# ---------------------------------------------------------------------------
QUALIFY_PHASE="evidence"

log "=== Phase E: writing evidence record ==="

EVIDENCE_FILE="${OUTPUT_DIR}/artifact-qualification-evidence.json"

# Build checks dict for JSON — all string interpolation goes through Python
# json.dumps to prevent injection. Raw bytes are base64; nonces are hex only.
CHECKS_JSON_FRAGMENT="$(py -I -c "
import json, sys

check_keys = ['c1_active_absent','c2_ca_absent','c3_machine_id_empty',
              'c4_hostkeys_absent','c5_clone_ready','c6_firstboot_service','c7_sshd_config']

# Values passed via environment to avoid shell quoting problems
import os
checks = {}
for k in check_keys:
    checks[k] = {
        'raw_pty_bytes_b64':    os.environ.get('CHK_RAW_' + k, ''),
        'nonce_used':           os.environ.get('CHK_NONCE_' + k, ''),
        'parsed_exit_status':   int(os.environ.get('CHK_STATUS_' + k, '-1')),
        'derived_result':       os.environ.get('CHK_RESULT_' + k, 'FAIL'),
    }
    if not checks[k]['raw_pty_bytes_b64']:
        print('ERROR: missing raw bytes for ' + k, file=sys.stderr); sys.exit(1)
    if checks[k]['parsed_exit_status'] != 0:
        print('ERROR: nonzero status for ' + k, file=sys.stderr); sys.exit(1)
print(json.dumps(checks, sort_keys=True))
" \
  CHK_RAW_c1_active_absent="${CHECK_RAW_B64[c1_active_absent]}" \
  CHK_RAW_c2_ca_absent="${CHECK_RAW_B64[c2_ca_absent]}" \
  CHK_RAW_c3_machine_id_empty="${CHECK_RAW_B64[c3_machine_id_empty]}" \
  CHK_RAW_c4_hostkeys_absent="${CHECK_RAW_B64[c4_hostkeys_absent]}" \
  CHK_RAW_c5_clone_ready="${CHECK_RAW_B64[c5_clone_ready]}" \
  CHK_RAW_c6_firstboot_service="${CHECK_RAW_B64[c6_firstboot_service]}" \
  CHK_RAW_c7_sshd_config="${CHECK_RAW_B64[c7_sshd_config]}" \
  CHK_NONCE_c1_active_absent="${CHECK_NONCE_USED[c1_active_absent]}" \
  CHK_NONCE_c2_ca_absent="${CHECK_NONCE_USED[c2_ca_absent]}" \
  CHK_NONCE_c3_machine_id_empty="${CHECK_NONCE_USED[c3_machine_id_empty]}" \
  CHK_NONCE_c4_hostkeys_absent="${CHECK_NONCE_USED[c4_hostkeys_absent]}" \
  CHK_NONCE_c5_clone_ready="${CHECK_NONCE_USED[c5_clone_ready]}" \
  CHK_NONCE_c6_firstboot_service="${CHECK_NONCE_USED[c6_firstboot_service]}" \
  CHK_NONCE_c7_sshd_config="${CHECK_NONCE_USED[c7_sshd_config]}" \
  CHK_STATUS_c1_active_absent="${CHECK_PARSED_STATUS[c1_active_absent]}" \
  CHK_STATUS_c2_ca_absent="${CHECK_PARSED_STATUS[c2_ca_absent]}" \
  CHK_STATUS_c3_machine_id_empty="${CHECK_PARSED_STATUS[c3_machine_id_empty]}" \
  CHK_STATUS_c4_hostkeys_absent="${CHECK_PARSED_STATUS[c4_hostkeys_absent]}" \
  CHK_STATUS_c5_clone_ready="${CHECK_PARSED_STATUS[c5_clone_ready]}" \
  CHK_STATUS_c6_firstboot_service="${CHECK_PARSED_STATUS[c6_firstboot_service]}" \
  CHK_STATUS_c7_sshd_config="${CHECK_PARSED_STATUS[c7_sshd_config]}" \
  CHK_RESULT_c1_active_absent="${CHECK_RESULT[c1_active_absent]}" \
  CHK_RESULT_c2_ca_absent="${CHECK_RESULT[c2_ca_absent]}" \
  CHK_RESULT_c3_machine_id_empty="${CHECK_RESULT[c3_machine_id_empty]}" \
  CHK_RESULT_c4_hostkeys_absent="${CHECK_RESULT[c4_hostkeys_absent]}" \
  CHK_RESULT_c5_clone_ready="${CHECK_RESULT[c5_clone_ready]}" \
  CHK_RESULT_c6_firstboot_service="${CHECK_RESULT[c6_firstboot_service]}" \
  CHK_RESULT_c7_sshd_config="${CHECK_RESULT[c7_sshd_config]}")"

# Build the full evidence record via Python with all fields passed as
# environment variables — no shell string interpolation into JSON values
py -I -c "
import json, os, sys

checks   = json.loads(os.environ['CHECKS_JSON'])
vm_files = json.loads(os.environ['VM_FILE_JSON'])
tart_entry = json.loads(os.environ['TART_ENTRY'])

all_pass = all(c.get('derived_result')=='PASS' and c.get('parsed_exit_status')==0
               for c in checks.values())

record = {
    'version':   os.environ['EVIDENCE_VERSION'],
    'schema':    os.environ['EVIDENCE_SCHEMA'],
    'generated_by': 'artifact-qualify.sh',
    'procedure': 'single-script-owned-framed-serial-qualification',
    'serial_binding_note': (
        'The script that generated this record created the socat PTY pair, '
        'passed the Tart serial endpoint via --serial-path to tart run, '
        'retained TART_PID, and performed all serial observations while '
        'TART_PID was live. The stopped artifact hashed below is the same '
        'VM object that ran under TART_PID.'
    ),
    'framing_protocol': {
        'tag':           'BWQF',
        'stx_hex':       '02',
        'etx_hex':       '03',
        'frame_format':  'STX TAG NONCE CHECK_ID EXIT_STATUS ETX',
        'pass_criterion':'exit_status == 0 AND nonce matches AND check_id matches',
        'pty_echo_note': 'STX/ETX are nonprinting; sent command is printable ASCII only',
    },
    'started_at':  os.environ['STARTED_AT'],
    'stopped_at':  os.environ['STOPPED_AT'],
    'artifact': {
        'name':             os.environ['CANDIDATE_NAME'],
        'tart_list_entry':  tart_entry,
        'vm_payloads':      vm_files,
        'mandatory_present': {'disk_img': True, 'config_json': True, 'nvram_bin': True},
        'vm_payloads_basis': 'Task 0 clone_identity evidence confirms disk.img, config.json, nvram.bin are exhaustive for Tart 2.32.1',
    },
    'build_provenance': {
        'source_commit':                os.environ['PR1_SOURCE_COMMIT'],
        'source_worktree_clean':        True,
        'user_data_template_sha256':    os.environ['PROV_USER_DATA_TEMPLATE_SHA256'],
        'rendered_user_data_sha256':    os.environ['PROV_RENDERED_USER_DATA_SHA256'],
        'remaster_script_sha256':       os.environ['PROV_BOOTSTRAP_SCRIPT_SHA256'],
        'finalize_script_sha256':       os.environ['PROV_FINALIZE_SCRIPT_SHA256'],
        'ubuntu_iso_sha256':            os.environ['PROV_UBUNTU_ISO_SHA256'],
        'remastered_iso_sha256':        os.environ['PROV_REMASTERED_ISO_SHA256'],
        'note': (
            'SHA-256 values bind the exact files consumed from the verified PR #1 source checkout. '
            'rendered_user_data_sha256 is the user-data actually embedded in the installer ISO. '
            'Sensitive attended inputs (password hash) are not recorded in public evidence.'
        ),
    },
    'install': {
        'ubuntu_iso_sha256':    os.environ['PROV_UBUNTU_ISO_SHA256'],
        'remastered_iso_sha256':os.environ['PROV_REMASTERED_ISO_SHA256'],
    },
    'socat': {
        'binary':  os.environ['SOCAT_BIN'],
        'sha256':  os.environ['SOCAT_SHA256'],
        'version': os.environ['SOCAT_VERSION'],
        'note':    'Runtime-resolved; Task 0 qualified version 1.8.1.3. SHA-256 binds executable only.',
    },
    'tart': {
        'binary':  os.environ['TART_BIN'],
        'sha256':  os.environ['TART_SHA256'],
        'version': os.environ['TART_VERSION'],
        'note':    'Prequalified: SHA-256 is the admission identity from V3 attended gate.',
    },
    'python': {
        'binary':   os.environ['PYTHON_BIN'],
        'sha256':   os.environ['PYTHON_SHA256'],
        'note':     'Runtime-resolved trusted-host tool. SHA-256 binds the driver binary only.',
        'version':  sys.version,
        'note':     'SHA-256 binds the Python driver binary only; standard library and extensions are not independently hashed.',
    },
    'bash': {
        'binary':   os.environ['BASH'],
        'sha256':   os.environ['BASH_SHA256'],
        'note':     'SHA-256 binds the Bash driver binary only.',
    },
    'integrity_note': (
        'This record is structured, content-bound, and machine-generated. '
        'It is not cryptographically signed. Integrity depends on: '
        '(1) VM payload SHA-256 hashes; '
        '(2) framed serial protocol recording machine-observed results; '
        '(3) single-script process ownership of the Tart launch and serial topology; '
        '(4) the controlled attended procedure.'
    ),
    'checks': checks,
    'checks_note': 'PASS is derived from parsed_exit_status==0 in nonce-validated frames. Prose labels are for readability only.',
    'overall_result': 'PASS' if all_pass else 'FAIL',
}
print(json.dumps(record, indent=2, sort_keys=True))
" \
  CHECKS_JSON="${CHECKS_JSON_FRAGMENT}" \
  VM_FILE_JSON="${VM_FILE_JSON}" \
  TART_ENTRY="${ARTIFACT_TART_ENTRY}" \
  EVIDENCE_VERSION="${EVIDENCE_VERSION}" \
  EVIDENCE_SCHEMA="${EVIDENCE_SCHEMA}" \
  CANDIDATE_NAME="${CANDIDATE_NAME}" \
  PR1_SOURCE_COMMIT="${PR1_SOURCE_COMMIT}" \
  PROV_USER_DATA_TEMPLATE_SHA256="${PROV_USER_DATA_TEMPLATE_SHA256}" \
  PROV_RENDERED_USER_DATA_SHA256="${PROV_RENDERED_USER_DATA_SHA256}" \
  PROV_BOOTSTRAP_SCRIPT_SHA256="${PROV_BOOTSTRAP_SCRIPT_SHA256}" \
  PROV_FINALIZE_SCRIPT_SHA256="${PROV_FINALIZE_SCRIPT_SHA256}" \
  PROV_UBUNTU_ISO_SHA256="${PROV_UBUNTU_ISO_SHA256}" \
  PROV_REMASTERED_ISO_SHA256="${PROV_REMASTERED_ISO_SHA256}" \
  SOCAT_BIN="${_socat_resolved_path}" \
  SOCAT_SHA256="${_socat_resolved_sha256}" \
  SOCAT_VERSION="${REQUIRED_SOCAT_VERSION}" \
  TART_BIN="${_tart_resolved_path}" \
  TART_SHA256="${_tart_resolved_sha256}" \
  TART_VERSION="${REQUIRED_TART_VERSION}" \
  PYTHON_BIN="${_python_resolved_path}" \
  PYTHON_SHA256="${_python_resolved_sha256}" \
  BASH="${BASH}" \
  BASH_SHA256="${_bash_resolved_sha256}" \
  STARTED_AT="${STARTED_AT}" \
  STOPPED_AT="${STOPPED_AT}" \
  > "${EVIDENCE_FILE}"

chmod 0600 "${EVIDENCE_FILE}"

# Validate JSON
py -I -c "import json,sys; json.load(open(sys.argv[1]))" -- "${EVIDENCE_FILE}" || \
  fail "evidence file is not valid JSON"

_overall="$(py -I -c "import json,sys; print(json.load(open(sys.argv[1]))['overall_result'])" -- "${EVIDENCE_FILE}")"
[[ "${_overall}" == "PASS" ]] || fail "evidence overall_result is '${_overall}', expected PASS"

EVIDENCE_SHA256="$(sha256_file "${EVIDENCE_FILE}")"
log "Evidence record written: ${EVIDENCE_FILE}"
log "Evidence SHA-256: ${EVIDENCE_SHA256}"

log "=== ARTIFACT QUALIFICATION COMPLETE: PASS ==="
# Clean up the build source worktree — all needed hashes are recorded in evidence
git -C "${BOXWARDEN_SRC}" worktree remove --force "${BUILD_WORKTREE}" 2>/dev/null || true
log "Build source worktree removed: ${BUILD_WORKTREE}"
printf '\nSUCCESS: artifact qualification passed.\n'
printf 'Evidence: %s\n' "${EVIDENCE_FILE}"
printf 'SHA-256:  %s\n' "${EVIDENCE_SHA256}"
printf 'Pass to V2 gate: --evidence-file %s --gate-tart-home %s\n' \
  "${EVIDENCE_FILE}" "${GATE_TART_HOME}"
