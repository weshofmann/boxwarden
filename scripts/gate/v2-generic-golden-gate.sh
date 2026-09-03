#!/opt/homebrew/Cellar/bash/5.3.9/bin/bash
# =============================================================================
# Boxwarden V2 Generic Golden Qualification Gate
#
# STATUS: FOR REVIEW — DO NOT EXECUTE WITHOUT EXPLICIT HUMAN APPROVAL
#
# Requires: Bash 5.3.9 at the absolute path above (associative arrays).
# System bash on the qualified macOS host is 3.2 and is NOT used.
#
# This script is checksum-bound, exact-target, and fail-closed.
# It refuses execution by default until --approve-execution is supplied.
#
# Phase structure:
#   0  Preflight:           validate identities and environment
#   B  Binary build:        reproducible PR #1 build from exact commit
#   1  Artifact admission:  verify pre-poweroff evidence file from artifact build
#   2  Golden registration: boxwarden golden register
#   3  Session create:      boxwarden session create; validate record + Tart state
#   4  Session status:      boxwarden session status
#   5  Idempotence:         normal repeat create; confirm no second clone
#   6  Non-mutation:        source artifact semantically unchanged
#   E  Evidence export:     record all findings before any cleanup
#      (Cleanup is printed as instructions; never executed automatically)
#
# IMPORTANT: artifact construction (render-seed, remaster-iso, run-install,
# finalize-clone, serial verification, poweroff) is a SEPARATE attended
# procedure that must complete FIRST and produce:
#
#   <GATE_ROOT>/artifact-pre-poweroff-evidence.txt
#
# That file must contain all seven PASS lines from the serial verification
# described below, plus the artifact Tart object name and SHA-256 of its
# Tart config.json. This gate requires and checksums that evidence file.
#
# Usage (after explicit human approval):
#   bash v2-generic-golden-gate.sh \
#       --approve-execution \
#       --gate-root        /absolute/path/to/gate-root \
#       --gate-tart-home   /absolute/path/to/gate-tart-home \
#       --boxwarden-src    /absolute/path/to/boxwarden-repo \
#       --artifact-name    <tart-object-name> \
#       --evidence-file    /absolute/path/to/artifact-qualification-evidence.json
#
# The evidence file must be produced by artifact-qualify.sh. It is a
# machine-generated JSON record with raw serial output and derived results
# for all seven postcondition checks, plus content-bound artifact identity
# (config.json and disk.img SHA-256). The V2 gate validates the record
# structure, derives results from raw output, and revalidates the
# content-bound identity against the live artifact before registration.
#
# GATE_ROOT and GATE_TART_HOME must not exist before execution.
# BOXWARDEN_SRC must be a git repository; script checks out a detached
# worktree inside GATE_ROOT for the build.
#
# Source:
#   Analysis worktree:  weshofmann/research/v2-generic-golden-gate
#   PR #1 head:         79aa1230e52486999f79b9193b7f85eade13e75c
#   PR #2 head (basis): 5c09aad9d2c322e42e6e8d7fd38e9584396b3ab5
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Pinned qualified identities
# ---------------------------------------------------------------------------

# Bash: trusted-host tool. Require version 5.x; resolved and recorded at runtime.
readonly REQUIRED_BASH_MAJOR=5

# Tart
# Tart 2.32.1: PREQUALIFIED identity from V3 gate and tool-provenance.md.
# SHA-256 is the admission identity; path is resolved at runtime.
readonly REQUIRED_TART_SHA256="05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d"
readonly REQUIRED_TART_VERSION="2.32.1"

# Canonical Ubuntu 24.04.4 Desktop ARM64 source ISO SHA-256 (Task 0 qualified).
# This is an independently known admission constant in the V2 consumer.
# Pass A requires the evidence record's canonical_ubuntu_iso_sha256 to match exactly.
readonly CANONICAL_UBUNTU_ISO_SHA256="c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"

# Python: trusted-host tool. Require Python 3.x; resolved and recorded at runtime.
readonly REQUIRED_PYTHON_MAJOR=3

# Go: build provenance tool. Require go1.27.x per go.mod; resolved and recorded at runtime.
# SHA-256 binds the driver binary only (not std library or other toolchain components).
readonly REQUIRED_GO_VERSION_PREFIX="go1.27"

# PR #1 exact commit — used for build verification
readonly PR1_COMMIT="79aa1230e52486999f79b9193b7f85eade13e75c"
# Expected build metadata in go version -m output
readonly EXPECTED_VCS_MODIFIED="false"

# Gate domain — intentionally disposable, must not collide with real domains
readonly GATE_DOMAIN="gatev2"
readonly GATE_SESSION_NAME="v2-gate"

# VM name prefix — all gate Tart objects must have this prefix
readonly GATE_VM_PREFIX="boxwarden-m1a-gate-v2-"

# ---------------------------------------------------------------------------
# Approval guard
# ---------------------------------------------------------------------------
APPROVED=0
GATE_ROOT=""
GATE_TART_HOME=""
BOXWARDEN_SRC=""
ARTIFACT_NAME=""
EVIDENCE_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --approve-execution) APPROVED=1; shift ;;
    --gate-root)         GATE_ROOT="${2:?}"; shift 2 ;;
    --gate-tart-home)    GATE_TART_HOME="${2:?}"; shift 2 ;;
    --boxwarden-src)     BOXWARDEN_SRC="${2:?}"; shift 2 ;;
    --artifact-name)     ARTIFACT_NAME="${2:?}"; shift 2 ;;
    --evidence-file)     EVIDENCE_FILE="${2:?}"; shift 2 ;;
    *) printf 'ERROR: unknown argument: %s\n' "$1" >&2; exit 1 ;;
  esac
done

if [[ "${APPROVED}" -ne 1 ]]; then
  printf 'GATE NOT APPROVED FOR EXECUTION\n' >&2
  printf 'This script is for review only.\n' >&2
  printf 'Re-run with --approve-execution after explicit human approval.\n' >&2
  exit 2
fi

# Verify Bash capability: require version 5.x (associative arrays)
_bash_major="${BASH_VERSINFO[0]}"
[[ "${_bash_major}" -ge "${REQUIRED_BASH_MAJOR}" ]] || {
  printf 'ERROR: Bash major version %s < required %s\n' "${_bash_major}" "${REQUIRED_BASH_MAJOR}" >&2
  exit 1
}
_bash_resolved_path="${BASH}"
_bash_resolved_sha256="$(shasum -a 256 "${_bash_resolved_path}" 2>/dev/null | awk '{print $1}')"

# ---------------------------------------------------------------------------
# Utility functions
# ---------------------------------------------------------------------------

EVIDENCE_LOG=""   # set after GATE_ROOT is created

log() {
  local msg
  msg="[$(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"
  printf '%s\n' "${msg}"
  if [[ -n "${EVIDENCE_LOG}" ]]; then
    printf '%s\n' "${msg}" >> "${EVIDENCE_LOG}"
  fi
}

fail() {
  printf 'GATE FAIL [%s]: %s\n' "${GATE_PHASE}" "$*" | tee -a "${EVIDENCE_LOG:-/dev/stderr}" >&2
  exit 1
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

# Python invocation with isolated mode
# Resolve Python at startup
_resolve_python_v2() {
  local _p _ver
  for _p in /opt/homebrew/bin/python3 /usr/local/bin/python3 /usr/bin/python3 python3; do
    command -v "${_p}" >/dev/null 2>&1 || continue
    _p="$(command -v "${_p}")"
    _ver="$(env -i PATH=/usr/bin:/bin "${_p}" -c 'import sys; print(sys.version_info.major)' 2>/dev/null)"
    [[ "${_ver:-0}" -ge "${REQUIRED_PYTHON_MAJOR}" ]] 2>/dev/null && { printf '%s' "${_p}"; return 0; }
  done
  return 1
}
_python_resolved_path="$(_resolve_python_v2)" || {
  printf 'ERROR: no Python %d.x found\n' "${REQUIRED_PYTHON_MAJOR}" >&2; exit 1
}
_python_resolved_sha256="$(shasum -a 256 "${_python_resolved_path}" 2>/dev/null | awk '{print $1}')"
py() { "${_python_resolved_path}" -I "$@"; }

# Canonicalize a path without following symlinks in intermediate components
canon_path() {
  py -c "import os,sys; print(os.path.realpath(sys.argv[1]))" -- "$1"
}

assert_sha256() {
  local path="$1" expected="$2" label="$3"
  local actual
  actual="$(sha256_file "${path}")"
  if [[ "${actual}" != "${expected}" ]]; then
    fail "SHA-256 mismatch for ${label}: expected ${expected}, got ${actual}"
  fi
  log "SHA-256 OK: ${label} = ${actual}"
}

require_absolute() {
  [[ "${1}" == /* ]] || die "${2} must be an absolute path: ${1}"
}

require_does_not_exist() {
  [[ ! -e "${1}" ]] || die "${2} must not already exist: ${1} — remove it first"
}

require_file() {
  [[ -f "${1}" ]] || fail "required file absent: ${2} (${1})"
}

# Capture a labelled command output to both stdout and the evidence log.
capture() {
  local label="$1"; shift
  log "CAPTURE BEGIN: ${label}"
  {
    printf '=== %s ===\n' "${label}"
    "$@" 2>&1
    printf '\n'
  } | tee -a "${EVIDENCE_LOG}"
  log "CAPTURE END: ${label}"
}

# ---------------------------------------------------------------------------
# Resolve Tart at runtime by prequalified SHA-256 (admission identity = digest; path observed)
_resolve_tart_v2() {
  local _candidate _sha
  for _candidate in \
    "/opt/homebrew/Cellar/tart/${REQUIRED_TART_VERSION}/libexec/tart.app/Contents/MacOS/tart" \
    "$(command -v tart 2>/dev/null || true)"; do
    [[ -f "${_candidate}" ]] || continue
    _sha="$(shasum -a 256 "${_candidate}" | awk '{print $1}')"
    [[ "${_sha}" == "${REQUIRED_TART_SHA256}" ]] && { printf '%s' "${_candidate}"; return 0; }
  done
  return 1
}
_tart_resolved_path="$(_resolve_tart_v2)" || {
  printf 'ERROR: no Tart binary with prequalified SHA-256 %s found\n' "${REQUIRED_TART_SHA256}" >&2
  exit 1
}
_tart_resolved_sha256="${REQUIRED_TART_SHA256}"  # by construction: _resolve_tart_v2 verified this

# Closed-environment Tart invocation using the runtime-resolved qualified binary.
gate_tart() {
  env -i \
    HOME="${HOME}" \
    USER="${USER}" \
    LOGNAME="${USER}" \
    PATH="/usr/bin:/bin" \
    TART_HOME="${GATE_TART_HOME}" \
    TART_NO_AUTO_PRUNE=1 \
    LANG=C \
    LC_ALL=C \
    "${_tart_resolved_path}" "$@"
}

# ---------------------------------------------------------------------------
# Closed-environment Boxwarden invocation (corrections #1, #2)
# Uses the gate-built binary with constrained PATH so "tart" resolves to
# exactly the qualified Tart Mach-O binary via a gate-owned symlink.
# BOXWARDEN_BIN is set in Phase B after the build.
# ---------------------------------------------------------------------------
BOXWARDEN_BIN=""   # set after binary build

gate_boxwarden() {
  [[ -n "${BOXWARDEN_BIN}" ]] || fail "gate_boxwarden called before binary build"
  env -i \
    HOME="${HOME}" \
    USER="${USER}" \
    LOGNAME="${USER}" \
    PATH="${GATE_PATH_DIR}:/usr/bin:/bin" \
    TART_HOME="${GATE_TART_HOME}" \
    TART_NO_AUTO_PRUNE=1 \
    LANG=C \
    LC_ALL=C \
    "${BOXWARDEN_BIN}" \
    --config "${GATE_ROOT}/config.json" \
    --domain "${GATE_DOMAIN}" \
    "$@"
}

# ---------------------------------------------------------------------------
# Stop-and-preserve trap (correction #5)
# ---------------------------------------------------------------------------
GATE_PHASE="pre-init"

unexpected_failure() {
  local exit_code=$?
  {
    printf '\n=== UNEXPECTED FAILURE in phase: %s (exit %d) ===\n' \
      "${GATE_PHASE}" "${exit_code}"
    printf 'Do NOT delete any directories or Tart objects until evidence is reviewed.\n'
    printf 'GATE_ROOT preserved at:      %s\n' "${GATE_ROOT}"
    printf 'GATE_TART_HOME preserved at: %s\n' "${GATE_TART_HOME}"
    printf 'Run: TART_HOME=%s %s list --format json\n' \
      "${GATE_TART_HOME}" "${_tart_resolved_path}"
    printf 'to record current Tart state before any investigation.\n'
  } | tee -a "${EVIDENCE_LOG:-/dev/stderr}" >&2
  exit "${exit_code}"
}
trap unexpected_failure ERR

# ---------------------------------------------------------------------------
# Phase 0: Preflight
# ---------------------------------------------------------------------------
GATE_PHASE="preflight"

# Required arguments
[[ -n "${GATE_ROOT}" ]]      || die "--gate-root is required"
[[ -n "${GATE_TART_HOME}" ]] || die "--gate-tart-home is required"
[[ -n "${BOXWARDEN_SRC}" ]]  || die "--boxwarden-src is required"
[[ -n "${ARTIFACT_NAME}" ]]  || die "--artifact-name is required"
[[ -n "${EVIDENCE_FILE}" ]]  || die "--evidence-file is required (path to artifact-qualify.sh JSON output)"

# Absolute paths
require_absolute "${GATE_ROOT}"      "GATE_ROOT"
require_absolute "${GATE_TART_HOME}" "GATE_TART_HOME"
require_absolute "${BOXWARDEN_SRC}"  "BOXWARDEN_SRC"
require_absolute "${EVIDENCE_FILE}"  "EVIDENCE_FILE"

# Artifact name prefix guard
[[ "${ARTIFACT_NAME}" == ${GATE_VM_PREFIX}* ]] || \
  die "artifact name must start with '${GATE_VM_PREFIX}': ${ARTIFACT_NAME}"

# GATE_ROOT must not exist yet; GATE_TART_HOME must already exist
# (it was created by artifact-qualify.sh and contains the qualified stopped artifact)
require_does_not_exist "${GATE_ROOT}" "GATE_ROOT"
[[ -d "${GATE_TART_HOME}" ]] || \
  die "GATE_TART_HOME must already exist (created by artifact-qualify.sh): ${GATE_TART_HOME}"

# Evidence file must exist
[[ -f "${EVIDENCE_FILE}" ]] || die "evidence file not found: ${EVIDENCE_FILE}"

# BOXWARDEN_SRC must be a git repo containing the required commit
[[ -d "${BOXWARDEN_SRC}/.git" ]] || \
  die "BOXWARDEN_SRC is not a git repository: ${BOXWARDEN_SRC}"
git -C "${BOXWARDEN_SRC}" cat-file -t "${PR1_COMMIT}" >/dev/null 2>&1 || \
  die "PR #1 commit ${PR1_COMMIT} not found in BOXWARDEN_SRC"

# Qualified Tart binary
# Tart: _tart_resolved_path was established above by _resolve_tart_v2().
[[ -n "${_tart_resolved_path}" ]] || die "Tart resolution failed (prequalified SHA-256 not found)"
tart_ver="$(env -i PATH=/usr/bin:/bin LANG=C LC_ALL=C "${_tart_resolved_path}" --version 2>/dev/null)"
[[ "${tart_ver}" == "${REQUIRED_TART_VERSION}" ]] || \
  die "Tart version mismatch: expected ${REQUIRED_TART_VERSION}, got ${tart_ver}"

# Python: _python_resolved_path and _python_resolved_sha256 set above by _resolve_python_v2()
[[ -n "${_python_resolved_path}" ]] || die "Python resolution failed"
py -c "import sys; assert sys.version_info >= (3,), 'Python 3 required'" || \
  die "Python sanity check failed"

# Go: resolve at runtime
_go_resolved_path="$(command -v go 2>/dev/null || true)"
[[ -f "${_go_resolved_path}" ]] || die "go not found in PATH"
_go_resolved_sha256="$(shasum -a 256 "${_go_resolved_path}" | awk '{print $1}')"
go_ver="$(env -i PATH="$(dirname "${_go_resolved_path}"):/usr/bin:/bin" "${_go_resolved_path}" version 2>/dev/null | awk '{print $3}')"
[[ "${go_ver}" == ${REQUIRED_GO_VERSION_PREFIX}* ]] || \
  die "Go version mismatch: expected ${REQUIRED_GO_VERSION_PREFIX}.x, got ${go_ver}"

# Non-overlap checks
_normal_tart="$(canon_path "${HOME}/.tart")"
_canon_gate_root="$(canon_path "${GATE_ROOT}")"
_canon_gate_tart="$(canon_path "${GATE_TART_HOME}")"
_canon_src="$(canon_path "${BOXWARDEN_SRC}")"

_ov() {
  local _a="$1" _b="$2"
  if [[ "${_a}" == "${_b}" ]] || [[ "${_a}" == "${_b}"/* ]] || [[ "${_b}" == "${_a}"/* ]]; then
    die "path overlap: '${_a}' and '${_b}'"
  fi
}
_ov "${_canon_gate_root}"  "${_canon_gate_tart}"
_ov "${_canon_gate_root}"  "${_normal_tart}"
_ov "${_canon_gate_tart}"  "${_normal_tart}"
_ov "${_canon_gate_root}"  "${_canon_src}"
_ov "${_canon_gate_tart}"  "${_canon_src}"

printf 'Preflight: all checks passed.\n'
printf 'GATE_ROOT will be created at: %s\n' "${GATE_ROOT}"
printf 'GATE_TART_HOME (existing):    %s\n' "${GATE_TART_HOME}"
printf 'Artifact:                     %s\n' "${ARTIFACT_NAME}"

# ---------------------------------------------------------------------------
# Initialise gate directories and evidence log
# ---------------------------------------------------------------------------
GATE_PHASE="init"

umask 077
mkdir -p "${GATE_ROOT}"
chmod 0700 "${GATE_ROOT}"
# GATE_TART_HOME already exists — created and populated by artifact-qualify.sh

EVIDENCE_LOG="${GATE_ROOT}/gate-evidence.log"
touch "${EVIDENCE_LOG}"
chmod 0600 "${EVIDENCE_LOG}"

log "=== V2 Generic Golden Qualification Gate started ==="
log "GATE_ROOT:      ${GATE_ROOT}"
log "GATE_TART_HOME: ${GATE_TART_HOME}"
log "BOXWARDEN_SRC:  ${BOXWARDEN_SRC}"
log "ARTIFACT_NAME:  ${ARTIFACT_NAME}"
log "PR1_COMMIT:     ${PR1_COMMIT}"

# PATH directory: "tart" symlink -> qualified Mach-O binary
# gate_boxwarden uses this so PR #1 binary resolves "tart" to the exact binary.
GATE_PATH_DIR="${GATE_ROOT}/gate-path"
mkdir -p "${GATE_PATH_DIR}"
ln -s "${_tart_resolved_path}" "${GATE_PATH_DIR}/tart"
actual_tart_via_path="$(readlink -f "${GATE_PATH_DIR}/tart")"
[[ "${actual_tart_via_path}" == "${_tart_resolved_path}" ]] || \
  fail "gate PATH symlink does not resolve to resolved Tart binary: ${actual_tart_via_path}"
log "Gate PATH symlink: ${GATE_PATH_DIR}/tart -> ${_tart_resolved_path}"

# ---------------------------------------------------------------------------
# Phase B: Reproducible PR #1 binary build
# ---------------------------------------------------------------------------
GATE_PHASE="binary-build"

log "=== Phase B: binary build from PR #1 commit ${PR1_COMMIT} ==="

BUILD_WORKTREE="${GATE_ROOT}/build-worktree"
BOXWARDEN_BIN="${GATE_ROOT}/boxwarden-pr1"

# Detached checkout of exact PR #1 commit into a private worktree
git -C "${BOXWARDEN_SRC}" worktree add --detach "${BUILD_WORKTREE}" "${PR1_COMMIT}" 2>&1 | \
  tee -a "${EVIDENCE_LOG}"

# Verify HEAD is the exact commit
actual_head="$(git -C "${BUILD_WORKTREE}" rev-parse HEAD)"
[[ "${actual_head}" == "${PR1_COMMIT}" ]] || \
  fail "build worktree HEAD is ${actual_head}, expected ${PR1_COMMIT}"
log "Build worktree HEAD confirmed: ${actual_head}"

# Verify worktree is clean (no uncommitted changes)
dirty="$(git -C "${BUILD_WORKTREE}" status --porcelain)"
[[ -z "${dirty}" ]] || fail "build worktree is not clean: ${dirty}"
log "Build worktree confirmed clean."

# Build with closed environment and -trimpath for reproducibility
log "Building PR #1 binary..."
(
  cd "${BUILD_WORKTREE}"
  env -i \
    HOME="${HOME}" \
    PATH="$(dirname "${_go_resolved_path}"):/usr/bin:/bin" \
    GOPATH="${GATE_ROOT}/gopath" \
    GOCACHE="${GATE_ROOT}/gocache" \
    GOMODCACHE="${GATE_ROOT}/gomodcache" \
    CGO_ENABLED=0 \
    GOOS=darwin \
    GOARCH=arm64 \
    LANG=C \
    LC_ALL=C \
    go build \
      -trimpath \
      -o "${BOXWARDEN_BIN}" \
      ./cmd/boxwarden
) 2>&1 | tee -a "${EVIDENCE_LOG}"

[[ -f "${BOXWARDEN_BIN}" && -x "${BOXWARDEN_BIN}" ]] || \
  fail "binary build produced no executable at ${BOXWARDEN_BIN}"

# Record and verify build metadata via go version -m
log "Verifying build metadata..."
build_meta="$(go version -m "${BOXWARDEN_BIN}" 2>&1)"
printf '%s\n' "${build_meta}" | tee -a "${EVIDENCE_LOG}"

vcs_revision="$(printf '%s' "${build_meta}" | awk '/vcs.revision/ {print $2}')"
vcs_modified="$(printf '%s' "${build_meta}" | awk '/vcs.modified/ {print $2}')"
trimpath_flag="$(printf '%s' "${build_meta}" | grep -- '-trimpath' | awk '{print $2}' || true)"
cgo_flag="$(printf '%s' "${build_meta}" | awk '/CGO_ENABLED/ {print $2}')"

[[ "${vcs_revision}" == "${PR1_COMMIT}" ]] || \
  fail "vcs.revision is '${vcs_revision}', expected '${PR1_COMMIT}'"
[[ "${vcs_modified}" == "${EXPECTED_VCS_MODIFIED}" ]] || \
  fail "vcs.modified is '${vcs_modified}', expected '${EXPECTED_VCS_MODIFIED}'"
[[ "${trimpath_flag}" == "true" ]] || \
  fail "binary was not built with -trimpath (got: '${trimpath_flag}')"
[[ "${cgo_flag}" == "0" ]] || \
  fail "CGO_ENABLED is '${cgo_flag}', expected '0'"

BIN_SHA256="$(sha256_file "${BOXWARDEN_BIN}")"
log "Binary SHA-256: ${BIN_SHA256}"
log "vcs.revision:   ${vcs_revision} (VERIFIED == PR1_COMMIT)"
log "vcs.modified:   ${vcs_modified} (VERIFIED == false)"
log "CGO_ENABLED:    ${cgo_flag}"
log "trimpath:       ${trimpath_flag}"

# ---------------------------------------------------------------------------
# Phase 1: Validate machine-generated artifact qualification evidence
# ---------------------------------------------------------------------------
GATE_PHASE="artifact-admission"

log "=== Phase 1: artifact admission — validating qualification evidence record ==="

# The evidence record must have been produced by artifact-qualify.sh.
# It is a structured JSON document containing raw serial output and
# derived results for all seven postcondition checks. The gate validates:
#   - the record is valid JSON with the expected schema
#   - overall_result is PASS (derived by artifact-qualify.sh from parsed exit statuses,
#     not from operator-authored text)
#   - every check's derived_result is PASS
#   - every check's parsed_exit_status is 0 (the machine-derived criterion)
#   - every check's raw_pty_bytes_b64 is non-empty (raw capture present)
#   - artifact.name matches --artifact-name
#   - all three mandatory VM payload files present (disk.img, config.json, nvram.bin)
#   - framing_protocol fields match the expected framed nonce protocol
# The consumer independently verifies parsed_exit_status == 0 for each check.
# Human-readable labels in the evidence are for readability only.

[[ -n "${EVIDENCE_FILE}" ]] || fail "evidence file path not set (--evidence-file required)"
require_file "${EVIDENCE_FILE}" "qualification evidence record"

# Verify evidence record is valid JSON
py -I -c "import json,sys; json.load(open(sys.argv[1]))" -- "${EVIDENCE_FILE}" || \
  fail "evidence record is not valid JSON"
log "Evidence record: valid JSON"

EVIDENCE_SHA256="$(sha256_file "${EVIDENCE_FILE}")"
log "Evidence record SHA-256: ${EVIDENCE_SHA256}"
capture "evidence-record" cat "${EVIDENCE_FILE}"

# ---------------------------------------------------------------------------
# Phase 1 evidence validation: Pass A (structural) + Pass B (independent
# raw-frame re-parsing).
#
# Pass B independently base64-decodes raw_pty_bytes_b64, extracts the STX/ETX
# frame, validates TAG/NONCE/CHECK_ID/EXIT_STATUS from the raw bytes, and
# requires exit_status==0. A check is admitted only if this independent parse
# succeeds AND matches the stored nonce_used and check key. The stored
# parsed_exit_status is cross-checked against the independently parsed value.
# ---------------------------------------------------------------------------
log "Pass A: structural validation..."
py -I - <<EVCHECK_A
import json, sys
with open('${EVIDENCE_FILE}') as f:
    rec = json.load(f)
errors = []
if rec.get('schema') != 'boxwarden-artifact-qualification-v1':
    errors.append("schema: got '%s'" % rec.get('schema'))
if rec.get('procedure') != 'single-script-owned-framed-serial-qualification':
    errors.append("procedure: got '%s'" % rec.get('procedure'))
if rec.get('generated_by') != 'artifact-qualify.sh':
    errors.append("generated_by: got '%s'" % rec.get('generated_by'))
if not rec.get('serial_binding_note'):
    errors.append("serial_binding_note absent")
fp = rec.get('framing_protocol', {})
for field, expected in [('tag','BWQF'),('stx_hex','02'),('etx_hex','03')]:
    if fp.get(field) != expected:
        errors.append("framing_protocol.%s: got '%s'" % (field, fp.get(field)))
if rec.get('overall_result') != 'PASS':
    errors.append("overall_result: got '%s'" % rec.get('overall_result'))
art = rec.get('artifact', {})
if art.get('name') != '${ARTIFACT_NAME}':
    errors.append("artifact.name: got '%s'" % art.get('name'))
payloads = art.get('vm_payloads', {})
mandatory = art.get('mandatory_present', {})
for fname, mkey in [('disk.img','disk_img'),('config.json','config_json'),('nvram.bin','nvram_bin')]:
    if not mandatory.get(mkey, False): errors.append("mandatory_present.%s not True" % mkey)
    if fname not in payloads or not payloads[fname]: errors.append("vm_payloads['%s'] missing" % fname)
required = ['c1_active_absent','c2_ca_absent','c3_machine_id_empty',
            'c4_hostkeys_absent','c5_clone_ready','c6_firstboot_service','c7_sshd_config']
checks = rec.get('checks', {})
for k in required:
    if k not in checks: errors.append("check '%s' absent" % k)
    elif not checks[k].get('raw_pty_bytes_b64'): errors.append("check '%s' raw empty" % k)
    elif not checks[k].get('nonce_used'): errors.append("check '%s' nonce_used absent" % k)
if errors:
    for e in errors: print("STRUCTURAL ERROR: " + e, file=sys.stderr)
    sys.exit(1)
# Provenance fields validation (Item 5: independent recomputation from V2 gate's own PR #1 checkout)
# BUILD_WORKTREE was created in Phase B for the binary build — reuse it.
import subprocess, re as _re

def sha256_file(path):
    r = subprocess.run(['shasum','-a','256',path], capture_output=True, text=True)
    if r.returncode != 0:
        raise ValueError("shasum failed for %s: %s" % (path, r.stderr.strip()))
    return r.stdout.split()[0]

def is_valid_sha256(s):
    return bool(_re.fullmatch(r'[0-9a-f]{64}', s or ''))

build_worktree = '${BUILD_WORKTREE}'
expected_commit = '79aa1230e52486999f79b9193b7f85eade13e75c'
prov = rec.get('build_provenance', {})

# Verify source commit and cleanliness
if prov.get('source_commit') != expected_commit:
    errors.append("build_provenance.source_commit: got '%s', expected '%s'" % (prov.get('source_commit'), expected_commit))
if prov.get('source_worktree_clean') is not True:
    errors.append("build_provenance.source_worktree_clean must be true")

# Require all SHA-256 fields are valid 64-char lowercase hex
required_sha_fields = [
    'user_data_template_sha256', 'rendered_user_data_sha256',
    'bootstrap_tart_sha256', 'finalize_clone_sha256',
    'canonical_ubuntu_iso_sha256', 'remastered_iso_sha256',
]
for field in required_sha_fields:
    v = prov.get(field, '')
    if not is_valid_sha256(v):
        errors.append("build_provenance.%s is not a valid 64-char lowercase SHA-256 hex: %r" % (field, v))

# Independently admit the canonical Ubuntu ISO SHA-256.
# This value is known to the V2 consumer independently; it does not come from
# the qualification evidence record. Any mismatch fails admission.
canonical_ubuntu_known = '${CANONICAL_UBUNTU_ISO_SHA256}'
canonical_ubuntu_evidence = prov.get('canonical_ubuntu_iso_sha256', '')
if canonical_ubuntu_evidence != canonical_ubuntu_known:
    errors.append(
        "build_provenance.canonical_ubuntu_iso_sha256 mismatch: "
        "evidence=%r V2-consumer-known=%r" % (canonical_ubuntu_evidence, canonical_ubuntu_known)
    )
else:
    print("  canonical_ubuntu_iso_sha256: independently admitted OK")

# Independently recompute source-controlled file hashes from the V2 gate's own checkout
import os
independent_files = {
    'user_data_template_sha256': os.path.join(build_worktree, 'guest/ubuntu-24.04-arm64/autoinstall/user-data'),
    'bootstrap_tart_sha256':     os.path.join(build_worktree, 'scripts/spike/bootstrap-tart.sh'),
    'finalize_clone_sha256':     os.path.join(build_worktree, 'scripts/spike/finalize-clone.sh'),
}
for field, path in independent_files.items():
    if not os.path.isfile(path):
        errors.append("independent recompute: file not found: %s" % path)
        continue
    try:
        ind = sha256_file(path)
    except ValueError as e:
        errors.append("independent recompute: %s" % e)
        continue
    rec_val = prov.get(field, '')
    if ind != rec_val:
        errors.append("independent recompute MISMATCH for %s: evidence=%r independent=%r" % (field, rec_val, ind))
    else:
        print("  independent recompute OK: %s = %s" % (field, ind))

if errors:
    for e in errors: print("STRUCTURAL ERROR: " + e, file=sys.stderr)
    sys.exit(1)
print("Pass A: structural validation OK (provenance fields independently recomputed)")
EVCHECK_A
log "Pass A: structural validation passed."

log "Pass B: independent raw PTY frame re-parsing..."
py -I - <<EVCHECK_B
import json, re, base64, sys
FRAME_TAG = b"BWQF"
required = ['c1_active_absent','c2_ca_absent','c3_machine_id_empty',
            'c4_hostkeys_absent','c5_clone_ready','c6_firstboot_service','c7_sshd_config']
with open('${EVIDENCE_FILE}') as f:
    rec = json.load(f)
checks = rec['checks']
errors = []
for check_id in required:
    chk = checks[check_id]
    raw_b64       = chk.get('raw_pty_bytes_b64', '')
    stored_nonce  = chk.get('nonce_used', '')
    stored_status = chk.get('parsed_exit_status', -1)
    stored_result = chk.get('derived_result', 'FAIL')
    try:
        raw_bytes = base64.b64decode(raw_b64)
    except Exception as e:
        errors.append("%s: base64 decode: %s" % (check_id, e)); continue
    matches = re.findall(b'\x02([^\x03]+)\x03', raw_bytes)
    if len(matches) == 0:
        errors.append("%s: no STX/ETX frame" % check_id); continue
    if len(matches) > 1:
        errors.append("%s: %d frames (expected 1)" % (check_id, len(matches))); continue
    try:
        frame_str = matches[0].decode('ascii', 'strict').strip()
    except UnicodeDecodeError as e:
        errors.append("%s: frame not ASCII: %s" % (check_id, e)); continue
    parts = frame_str.split()
    if len(parts) != 4:
        errors.append("%s: frame has %d fields: %r" % (check_id, len(parts), frame_str)); continue
    ind_tag, ind_nonce, ind_check_id, ind_status_str = parts
    if ind_tag != 'BWQF':
        errors.append("%s: tag '%s' != BWQF" % (check_id, ind_tag)); continue
    if ind_nonce != stored_nonce:
        errors.append("%s: nonce mismatch" % check_id); continue
    if ind_check_id != check_id:
        errors.append("%s: check_id mismatch (got %s)" % (check_id, ind_check_id)); continue
    if not ind_status_str.isdigit():
        errors.append("%s: non-numeric status '%s'" % (check_id, ind_status_str)); continue
    ind_status = int(ind_status_str)
    if ind_status != 0:
        errors.append("%s: independently parsed exit_status=%d" % (check_id, ind_status)); continue
    if ind_status != stored_status:
        errors.append("%s: stored=%d != independent=%d" % (check_id, stored_status, ind_status)); continue
    if stored_result != 'PASS':
        errors.append("%s: stored derived_result='%s' != PASS" % (check_id, stored_result)); continue
    print("  %s: independent parse OK (exit_status=0, nonce validated)" % check_id)
if errors:
    for e in errors: print("FRAME PARSE ERROR: " + e, file=sys.stderr)
    sys.exit(1)
print("Pass B: all 7 checks independently parsed and validated")
EVCHECK_B
log "Pass B: independent raw-frame re-parsing passed."

evidence_disk_sha256="$(py -I -c "import json,sys; print(json.load(open(sys.argv[1]))['artifact']['vm_payloads']['disk.img'])" -- "${EVIDENCE_FILE}")"
evidence_config_sha256="$(py -I -c "import json,sys; print(json.load(open(sys.argv[1]))['artifact']['vm_payloads']['config.json'])" -- "${EVIDENCE_FILE}")"
evidence_nvram_sha256="$(py -I -c "import json,sys; print(json.load(open(sys.argv[1]))['artifact']['vm_payloads']['nvram.bin'])" -- "${EVIDENCE_FILE}")"

log "Revalidating artifact content-bound identity against GATE_TART_HOME..."

artifact_config_live="${GATE_TART_HOME}/vms/${ARTIFACT_NAME}/config.json"
artifact_disk_live="${GATE_TART_HOME}/vms/${ARTIFACT_NAME}/disk.img"
artifact_nvram_live="${GATE_TART_HOME}/vms/${ARTIFACT_NAME}/nvram.bin"

[[ -f "${artifact_config_live}" ]] || \
  fail "artifact config.json not found at: ${artifact_config_live}"
[[ -f "${artifact_disk_live}" ]] || \
  fail "artifact disk.img not found at: ${artifact_disk_live}"
[[ -f "${artifact_nvram_live}" ]] || \
  fail "artifact nvram.bin not found at: ${artifact_nvram_live}"

live_config_sha256="$(sha256_file "${artifact_config_live}")"
log "Live config.json SHA-256:    ${live_config_sha256}"
log "Evidence config.json SHA-256: ${evidence_config_sha256}"
[[ "${live_config_sha256}" == "${evidence_config_sha256}" ]] || \
  fail "artifact config.json SHA-256 changed since qualification: evidence=${evidence_config_sha256} live=${live_config_sha256}"
log "config.json SHA-256: MATCH"

log "Computing live disk.img SHA-256 (may take time)..."
live_disk_sha256="$(sha256_file "${artifact_disk_live}")"
log "Live disk.img SHA-256:    ${live_disk_sha256}"
log "Evidence disk.img SHA-256: ${evidence_disk_sha256}"
[[ "${live_disk_sha256}" == "${evidence_disk_sha256}" ]] || \
  fail "artifact disk.img SHA-256 changed since qualification: evidence=${evidence_disk_sha256} live=${live_disk_sha256}"
log "disk.img SHA-256: MATCH"

live_nvram_sha256="$(sha256_file "${artifact_nvram_live}")"
log "Live nvram.bin SHA-256:    ${live_nvram_sha256}"
log "Evidence nvram.bin SHA-256: ${evidence_nvram_sha256}"
[[ "${live_nvram_sha256}" == "${evidence_nvram_sha256}" ]] || \
  fail "artifact nvram.bin SHA-256 changed since qualification: evidence=${evidence_nvram_sha256} live=${live_nvram_sha256}"
log "nvram.bin SHA-256: MATCH"

log "Artifact content-bound identity revalidated: all three payload files unchanged since qualification."

# ---------------------------------------------------------------------------
# Phase 1a: Pre-gate Tart inventory
# ---------------------------------------------------------------------------
GATE_PHASE="pre-inventory"

log "=== Phase 1a: pre-gate Tart inventory ==="
capture "pre-gate-tart-list" gate_tart list --format json

# Confirm artifact exists in GATE_TART_HOME and is stopped
artifact_tart_json="$(gate_tart list --format json)"
artifact_state="$(printf '%s' "${artifact_tart_json}" | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${ARTIFACT_NAME}':
        print(vm.get('State', 'unknown'))
        sys.exit(0)
print('not-found')
")"
[[ "${artifact_state}" == "stopped" ]] || \
  fail "artifact '${ARTIFACT_NAME}' not found as stopped in GATE_TART_HOME (got: '${artifact_state}')"
log "Artifact confirmed stopped in GATE_TART_HOME: ${ARTIFACT_NAME}"

# Item 3: After qualification, before V2 registration, GATE_TART_HOME must
# contain EXACTLY ONE Tart object: the qualified candidate.
pre_inventory_count="$(printf '%s' "${artifact_tart_json}" | py -I -c "import sys,json; print(len(json.load(sys.stdin)))")"
[[ "${pre_inventory_count}" -eq 1 ]] || \
  fail "GATE_TART_HOME must contain exactly 1 Tart object before V2 registration (found ${pre_inventory_count})"
log "GATE_TART_HOME object count before registration: ${pre_inventory_count} (required: 1)"

# Record the full Tart entry for the artifact (for Phase 6 non-mutation check)
pre_gate_artifact_entry="$(printf '%s' "${artifact_tart_json}" | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${ARTIFACT_NAME}':
        import json as j; print(j.dumps(vm, sort_keys=True))
        sys.exit(0)
")"
log "Pre-gate artifact entry: ${pre_gate_artifact_entry}"

# ---------------------------------------------------------------------------
# Phase 2: Write gate config.json and perform golden registration
# ---------------------------------------------------------------------------
GATE_PHASE="config-and-register"

log "=== Phase 2: gate config and golden registration ==="

GATE_STATE_ROOT="${GATE_ROOT}/domains/${GATE_DOMAIN}"
mkdir -p "${GATE_STATE_ROOT}"
chmod 0700 "${GATE_STATE_ROOT}"

GATE_CONFIG="${GATE_ROOT}/config.json"
cat > "${GATE_CONFIG}" <<EOF
{
  "version": 1,
  "domains": {
    "${GATE_DOMAIN}": {
      "state_root": "${GATE_STATE_ROOT}"
    }
  }
}
EOF
chmod 0600 "${GATE_CONFIG}"
log "Gate config written: ${GATE_CONFIG}"
capture "gate-config" cat "${GATE_CONFIG}"

log "Executing: boxwarden --config ... --domain ${GATE_DOMAIN} golden register ${ARTIFACT_NAME}"
register_output="$(gate_boxwarden golden register "${ARTIFACT_NAME}" 2>&1)"
printf '%s\n' "${register_output}" | tee -a "${EVIDENCE_LOG}"

# Validate CLI output (domain/golden/state only)
reg_domain="$(printf '%s' "${register_output}" | awk -F': ' '/^domain:/ {print $2}')"
reg_golden="$(printf '%s' "${register_output}" | awk -F': ' '/^golden:/ {print $2}')"
reg_state="$(printf '%s' "${register_output}"  | awk -F': ' '/^state:/  {print $2}')"

[[ "${reg_domain}" == "${GATE_DOMAIN}" ]]   || fail "register domain: '${reg_domain}'"
[[ "${reg_golden}" == "${ARTIFACT_NAME}" ]] || fail "register golden: '${reg_golden}'"
[[ "${reg_state}"  == "registered" ]]       || fail "register state: '${reg_state}'"
log "CLI register output validated."

# Validate durable record on disk
GOLDEN_RECORD="${GATE_STATE_ROOT}/goldens/records/${ARTIFACT_NAME}.json"
GOLDEN_CURRENT="${GATE_STATE_ROOT}/goldens/current.json"
require_file "${GOLDEN_RECORD}"  "golden record"
require_file "${GOLDEN_CURRENT}" "golden current pointer"

rec_mode="$(stat -f '%Mp%Lp' "${GOLDEN_RECORD}" 2>/dev/null || stat -c '%a' "${GOLDEN_RECORD}")"
[[ "${rec_mode}" == "0600" || "${rec_mode}" == "600" ]] || \
  fail "golden record mode: ${rec_mode}, expected 0600"

golden_record_json="$(cat "${GOLDEN_RECORD}")"
capture "golden-record"  cat "${GOLDEN_RECORD}"
capture "golden-current" cat "${GOLDEN_CURRENT}"

rec_version="$(printf '%s' "${golden_record_json}" | py -I -c "import sys,json; print(json.load(sys.stdin)['version'])")"
rec_domain="$(printf '%s' "${golden_record_json}"  | py -I -c "import sys,json; print(json.load(sys.stdin)['domain'])")"
rec_revision="$(printf '%s' "${golden_record_json}" | py -I -c "import sys,json; print(json.load(sys.stdin)['revision'])")"
rec_bkind="$(printf '%s' "${golden_record_json}"    | py -I -c "import sys,json; print(json.load(sys.stdin)['backend']['kind'])")"
rec_bobj="$(printf '%s' "${golden_record_json}"     | py -I -c "import sys,json; print(json.load(sys.stdin)['backend']['object_id'])")"

[[ "${rec_version}"  == "1" ]]                 || fail "golden record version: ${rec_version}"
[[ "${rec_domain}"   == "${GATE_DOMAIN}" ]]    || fail "golden record domain: ${rec_domain}"
[[ "${rec_revision}" == "${ARTIFACT_NAME}" ]]  || fail "golden record revision: ${rec_revision}"
[[ "${rec_bkind}"    == "tart" ]]              || fail "golden record backend.kind: ${rec_bkind}"
[[ "${rec_bobj}"     == "${ARTIFACT_NAME}" ]]  || fail "golden record backend.object_id: ${rec_bobj}"
log "Golden record validated: version=${rec_version} domain=${rec_domain} revision=${rec_revision} backend.kind=${rec_bkind} backend.object_id=${rec_bobj}"

# Confirm source artifact still stopped (registration must not mutate backend)
post_reg_state="$(gate_tart list --format json | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${ARTIFACT_NAME}':
        print(vm.get('State', 'unknown'))
        sys.exit(0)
print('not-found')
")"
[[ "${post_reg_state}" == "stopped" ]] || \
  fail "source artifact not stopped after golden register: '${post_reg_state}'"
log "Source artifact still stopped after golden registration."

# ---------------------------------------------------------------------------
# Phase 3: Session create
# ---------------------------------------------------------------------------
GATE_PHASE="session-create"

log "=== Phase 3: session create ==="
log "Executing: boxwarden --config ... --domain ${GATE_DOMAIN} session create ${GATE_SESSION_NAME}"
create_output="$(gate_boxwarden session create "${GATE_SESSION_NAME}" 2>&1)"
printf '%s\n' "${create_output}" | tee -a "${EVIDENCE_LOG}"

# CLI output is domain/session/mode/state only (correction #3)
cr_domain="$(printf '%s' "${create_output}" | awk -F': ' '/^domain:/  {print $2}')"
cr_session="$(printf '%s' "${create_output}" | awk -F': ' '/^session:/ {print $2}')"
cr_mode="$(printf '%s' "${create_output}"   | awk -F': ' '/^mode:/    {print $2}')"
cr_state="$(printf '%s' "${create_output}"  | awk -F': ' '/^state:/   {print $2}')"

[[ "${cr_domain}"  == "${GATE_DOMAIN}" ]]    || fail "create domain: '${cr_domain}'"
[[ "${cr_session}" == "${GATE_SESSION_NAME}" ]] || fail "create session: '${cr_session}'"
[[ "${cr_mode}"    == "clean" ]]             || fail "create mode: '${cr_mode}'"
[[ "${cr_state}"   == "stopped" ]]           || fail "create state: '${cr_state}'"
log "CLI create output validated: domain=${cr_domain} session=${cr_session} mode=${cr_mode} state=${cr_state}"

# Read UUID, golden revision, and backend object ID from the durable session record
SESSION_RECORD="${GATE_STATE_ROOT}/sessions/${GATE_SESSION_NAME}.json"
require_file "${SESSION_RECORD}" "session record"

sess_mode_r="$(stat -f '%Mp%Lp' "${SESSION_RECORD}" 2>/dev/null || stat -c '%a' "${SESSION_RECORD}")"
[[ "${sess_mode_r}" == "0600" || "${sess_mode_r}" == "600" ]] || \
  fail "session record mode: ${sess_mode_r}, expected 0600"

sess_json="$(cat "${SESSION_RECORD}")"
capture "session-record" cat "${SESSION_RECORD}"

sess_version="$(printf '%s' "${sess_json}" | py -I -c "import sys,json; print(json.load(sys.stdin)['version'])")"
sess_domain="$(printf '%s' "${sess_json}"  | py -I -c "import sys,json; print(json.load(sys.stdin)['domain'])")"
sess_name="$(printf '%s' "${sess_json}"    | py -I -c "import sys,json; print(json.load(sys.stdin)['name'])")"
sess_id="$(printf '%s' "${sess_json}"      | py -I -c "import sys,json; print(json.load(sys.stdin)['id'])")"
sess_mode="$(printf '%s' "${sess_json}"    | py -I -c "import sys,json; print(json.load(sys.stdin)['mode'])")"
sess_state="$(printf '%s' "${sess_json}"   | py -I -c "import sys,json; print(json.load(sys.stdin)['intended_state'])")"
sess_golden="$(printf '%s' "${sess_json}"  | py -I -c "import sys,json; print(json.load(sys.stdin)['golden_revision'])")"
sess_bkind="$(printf '%s' "${sess_json}"   | py -I -c "import sys,json; print(json.load(sys.stdin)['backend']['kind'])")"
sess_bobj="$(printf '%s' "${sess_json}"    | py -I -c "import sys,json; print(json.load(sys.stdin)['backend']['object_id'])")"

[[ "${sess_version}" == "1" ]]                   || fail "session version: ${sess_version}"
[[ "${sess_domain}"  == "${GATE_DOMAIN}" ]]      || fail "session domain: ${sess_domain}"
[[ "${sess_name}"    == "${GATE_SESSION_NAME}" ]] || fail "session name: ${sess_name}"
[[ "${sess_mode}"    == "clean" ]]               || fail "session mode: ${sess_mode}"
[[ "${sess_state}"   == "stopped" ]]             || fail "session intended_state: ${sess_state}"
[[ "${sess_golden}"  == "${ARTIFACT_NAME}" ]]    || fail "session golden_revision: ${sess_golden}"
[[ "${sess_bkind}"   == "tart" ]]                || fail "session backend.kind: ${sess_bkind}"
[[ "${sess_bobj}" == "boxwarden-${GATE_DOMAIN}-"* ]] || \
  fail "session backend.object_id '${sess_bobj}' does not match expected prefix 'boxwarden-${GATE_DOMAIN}-'"
log "Session record validated: version=${sess_version} domain=${sess_domain} name=${sess_name} id=${sess_id} mode=${sess_mode} state=${sess_state} golden=${sess_golden} backend.kind=${sess_bkind} backend.object_id=${sess_bobj}"

# Observe clone in Tart
post_create_json="$(gate_tart list --format json)"
capture "post-create-tart-list" printf '%s' "${post_create_json}"

clone_state="$(printf '%s' "${post_create_json}" | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${sess_bobj}':
        print(vm.get('State', 'unknown'))
        sys.exit(0)
print('not-found')
")"
[[ "${clone_state}" == "stopped" ]] || \
  fail "clone '${sess_bobj}' expected stopped in Tart, got: '${clone_state}'"
log "Clone confirmed stopped in Tart: ${sess_bobj}"

# Item 3: After V2 session create, GATE_TART_HOME must contain EXACTLY TWO
# Tart objects: the qualified candidate and the V2 clone.
post_create_count="$(printf '%s' "${post_create_json}" | py -I -c "import sys,json; print(len(json.load(sys.stdin)))")"
[[ "${post_create_count}" -eq 2 ]] || \
  fail "GATE_TART_HOME must contain exactly 2 Tart objects after session create (found ${post_create_count})"
log "GATE_TART_HOME object count after session create: ${post_create_count} (required: 2)"

# Verify backend object ID matches session record
clone_in_tart="$(printf '%s' "${post_create_json}" | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${sess_bobj}':
        print('found')
        sys.exit(0)
print('not-found')
")"
[[ "${clone_in_tart}" == "found" ]] || fail "session record backend.object_id '${sess_bobj}' not found in Tart"
log "Backend object ID matches session record and Tart state: ${sess_bobj}"

# Verify clone MAC differs from source MAC (log fact, withhold values)
clone_mac="$(printf '%s' "${post_create_json}" | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${sess_bobj}': print(vm.get('MAC','')); sys.exit(0)
")"
source_mac_now="$(printf '%s' "${post_create_json}" | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${ARTIFACT_NAME}': print(vm.get('MAC','')); sys.exit(0)
")"
[[ -n "${clone_mac}" && -n "${source_mac_now}" && "${clone_mac}" != "${source_mac_now}" ]] || \
  fail "MAC check: clone MAC not distinct from source MAC (or one missing)"
log "Clone MAC is distinct from source MAC (values withheld — Tart randomize-MAC effective)."

# ---------------------------------------------------------------------------
# Phase 4: Session status
# ---------------------------------------------------------------------------
GATE_PHASE="session-status"

log "=== Phase 4: session status ==="
status_output="$(gate_boxwarden session status "${GATE_SESSION_NAME}" 2>&1)"
printf '%s\n' "${status_output}" | tee -a "${EVIDENCE_LOG}"
capture "session-status" printf '%s' "${status_output}"

st_intended="$(printf '%s' "${status_output}" | awk -F': ' '/^intended:/    {print $2}')"
st_observed="$(printf '%s' "${status_output}" | awk -F': ' '/^observed:/    {print $2}')"
st_consistency="$(printf '%s' "${status_output}" | awk -F': ' '/^consistency:/ {print $2}')"
st_golden="$(printf '%s' "${status_output}"   | awk -F': ' '/^golden:/      {print $2}')"

[[ "${st_intended}"    == "stopped" ]]         || fail "status intended: '${st_intended}'"
[[ "${st_observed}"    == "stopped" ]]         || fail "status observed: '${st_observed}'"
[[ "${st_consistency}" == "consistent" ]]      || fail "status consistency: '${st_consistency}'"
[[ "${st_golden}"      == "${ARTIFACT_NAME}" ]] || fail "status golden: '${st_golden}'"
log "Session status validated: intended=stopped observed=stopped consistency=consistent golden=${st_golden}"

# ---------------------------------------------------------------------------
# Phase 5: Normal idempotence check
# ---------------------------------------------------------------------------
GATE_PHASE="idempotence"

log "=== Phase 5: normal idempotence check ==="
idem_output="$(gate_boxwarden session create "${GATE_SESSION_NAME}" 2>&1)"
printf '%s\n' "${idem_output}" | tee -a "${EVIDENCE_LOG}"
capture "idempotence-create" printf '%s' "${idem_output}"

idem_domain="$(printf '%s' "${idem_output}" | awk -F': ' '/^domain:/  {print $2}')"
idem_session="$(printf '%s' "${idem_output}" | awk -F': ' '/^session:/ {print $2}')"
idem_state="$(printf '%s' "${idem_output}"  | awk -F': ' '/^state:/   {print $2}')"

[[ "${idem_domain}"  == "${GATE_DOMAIN}" ]]    || fail "idempotence domain: '${idem_domain}'"
[[ "${idem_session}" == "${GATE_SESSION_NAME}" ]] || fail "idempotence session: '${idem_session}'"
[[ "${idem_state}"   == "stopped" ]]           || fail "idempotence state: '${idem_state}'"
log "Idempotence CLI output validated."

# Confirm no second clone created
idem_json="$(gate_tart list --format json)"
capture "post-idempotence-tart-list" printf '%s' "${idem_json}"

clone_count="$(printf '%s' "${idem_json}" | py -I -c "
import sys, json
print(sum(1 for vm in json.load(sys.stdin) if vm.get('Name') == '${sess_bobj}'))
")"
[[ "${clone_count}" -eq 1 ]] || \
  fail "idempotence: expected exactly 1 clone '${sess_bobj}', found ${clone_count}"
log "Idempotence: exactly 1 clone confirmed."

# ---------------------------------------------------------------------------
# Phase 6: Source non-mutation final check (correction #6)
# Tart clone is known to update access/modification timestamps on the source.
# Those exact known metadata changes are accepted. Disk payload, Tart config,
# NVRAM payload, MAC address, and stopped state must remain unchanged.
# ---------------------------------------------------------------------------
GATE_PHASE="non-mutation"

log "=== Phase 6: source non-mutation final check ==="
final_json="$(gate_tart list --format json)"
capture "final-tart-list" printf '%s' "${final_json}"

final_entry="$(printf '%s' "${final_json}" | py -I -c "
import sys, json
for vm in json.load(sys.stdin):
    if vm.get('Name') == '${ARTIFACT_NAME}':
        import json as j; print(j.dumps(vm, sort_keys=True))
        sys.exit(0)
print('not-found')
")"
[[ "${final_entry}" != "not-found" ]] || \
  fail "source artifact '${ARTIFACT_NAME}' not found in final Tart list"

final_state="$(printf '%s' "${final_entry}" | py -I -c "import sys,json; print(json.load(sys.stdin).get('State'))")"
final_mac="$(printf '%s' "${final_entry}"   | py -I -c "import sys,json; print(json.load(sys.stdin).get('MAC',''))")"
pre_mac="$(printf '%s' "${pre_gate_artifact_entry}" | py -I -c "import sys,json; print(json.load(sys.stdin).get('MAC',''))")"

[[ "${final_state}" == "stopped" ]] || \
  fail "source artifact state changed: final='${final_state}'"
[[ "${final_mac}" == "${pre_mac}" ]] || \
  fail "source artifact MAC changed: pre='${pre_mac}' final='${final_mac}'"
log "Source artifact: state=stopped, MAC unchanged. Tart timestamp updates accepted per policy."

# Verify artifact payload SHA-256 hashes unchanged through the gate
# live_{config,disk,nvram}_sha256 were all computed in Phase 1 evidence revalidation
final_config_sha256="$(sha256_file "${GATE_TART_HOME}/vms/${ARTIFACT_NAME}/config.json")"
final_disk_sha256="$(sha256_file "${GATE_TART_HOME}/vms/${ARTIFACT_NAME}/disk.img")"
final_nvram_sha256="$(sha256_file "${GATE_TART_HOME}/vms/${ARTIFACT_NAME}/nvram.bin")"
[[ "${final_config_sha256}" == "${live_config_sha256}" ]] || \
  fail "artifact config.json changed between Phase 1 and Phase 6: was ${live_config_sha256}, now ${final_config_sha256}"
[[ "${final_disk_sha256}" == "${live_disk_sha256}" ]] || \
  fail "artifact disk.img changed between Phase 1 and Phase 6: was ${live_disk_sha256}, now ${final_disk_sha256}"
[[ "${final_nvram_sha256}" == "${live_nvram_sha256}" ]] || \
  fail "artifact nvram.bin changed between Phase 1 and Phase 6: was ${live_nvram_sha256}, now ${final_nvram_sha256}"
log "Artifact payload hashes (config, disk, nvram) unchanged through gate."

# Confirm exactly 2 Tart objects: source + 1 clone
total_objects="$(printf '%s' "${final_json}" | py -I -c "import sys,json; print(len(json.load(sys.stdin)))")"
[[ "${total_objects}" -eq 2 ]] || \
  fail "expected exactly 2 Tart objects (source + 1 clone), found ${total_objects}"
log "Exactly 2 Tart objects confirmed."

# ---------------------------------------------------------------------------
# Phase E: Evidence export summary
# ---------------------------------------------------------------------------
GATE_PHASE="evidence-export"

log "=== Phase E: evidence export ==="
capture "final-session-record"  cat "${SESSION_RECORD}"
capture "final-golden-record"   cat "${GOLDEN_RECORD}"
capture "final-golden-current"  cat "${GOLDEN_CURRENT}"
capture "build-metadata"        go version -m "${BOXWARDEN_BIN}"
capture "qualified-tart-sha256" printf '%s  %s\n' "${REQUIRED_TART_SHA256}" "${_tart_resolved_path}"
capture "binary-sha256"         printf '%s  %s\n' "${BIN_SHA256}" "${BOXWARDEN_BIN}"
capture "evidence-sha256"       printf '%s  %s\n' "${EVIDENCE_SHA256}" "${EVIDENCE_FILE}"

log "=== GATE COMPLETE: ALL PHASES PASSED ==="
log "Artifact:           ${ARTIFACT_NAME}"
log "Session:            ${GATE_SESSION_NAME}"
log "Session UUID:       ${sess_id}"
log "Clone object ID:    ${sess_bobj}"
log "Golden revision:    ${sess_golden}"
log "Binary SHA-256:     ${BIN_SHA256}"
log "Binary commit:      ${vcs_revision}"
log "Evidence SHA-256:   ${EVIDENCE_SHA256}"
log "Evidence log:       ${EVIDENCE_LOG}"

# ---------------------------------------------------------------------------
# Cleanup instructions — printed only, never executed automatically
# ---------------------------------------------------------------------------
printf '\n'
printf '=============================================================================\n'
printf 'V2 GENERIC GOLDEN QUALIFICATION GATE: ALL PHASES PASSED\n'
printf '=============================================================================\n'
printf 'Evidence log:   %s\n' "${EVIDENCE_LOG}"
printf 'Gate root:      %s\n' "${GATE_ROOT}"
printf 'Gate Tart home: %s\n' "${GATE_TART_HOME}"
printf '\n'
printf 'NEXT STEPS:\n'
printf '  1. Review and archive %s\n' "${EVIDENCE_LOG}"
printf '  2. Prepare a redacted public evidence record from the log\n'
printf '  3. After review and explicit approval, run cleanup:\n'
printf '\n'
printf '     # Delete session clone\n'
printf '     TART_HOME=%s \\\n' "${GATE_TART_HOME}"
printf '       TART_NO_AUTO_PRUNE=1 \\\n'
printf '       %s delete %s\n' "${_tart_resolved_path}" "${sess_bobj}"
printf '\n'
printf '     # Delete source artifact (if non-production gate artifact)\n'
printf '     TART_HOME=%s \\\n' "${GATE_TART_HOME}"
printf '       TART_NO_AUTO_PRUNE=1 \\\n'
printf '       %s delete %s\n' "${_tart_resolved_path}" "${ARTIFACT_NAME}"
printf '\n'
printf '     # Remove gate state and Tart home together\n'
printf '     rm -rf %s\n' "${GATE_ROOT}"
printf '     rm -rf %s\n' "${GATE_TART_HOME}"
printf '\n'
printf '  4. Remove the git worktree created for the build:\n'
printf '     git -C %s worktree remove %s\n' "${BOXWARDEN_SRC}" "${BUILD_WORKTREE}"
printf '\n'
printf 'DO NOT run cleanup until evidence has been reviewed and committed.\n'
printf '=============================================================================\n'
