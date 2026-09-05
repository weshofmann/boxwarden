# V3 Host Manifest Readability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the root-owned V3 host manifest safely readable by unprivileged doctor, prove the cross-UID behavior, and document the one-time attended migration of the existing installation.

**Architecture:** Publish and validate the non-secret host manifest as exact `root:wheel 0444`; retain every existing integrity check and keep doctor privilege-free. Normal code refuses the legacy `0400` state, while the existing installation is migrated separately only after a final-head read-only exact-group precheck, a non-mutating old-init comparison, and complete original-tree validation.

**Tech Stack:** Go standard library, Unix/macOS file permissions, existing `internal/hostx` publication/doctor code, Markdown architecture and operations documentation.

**Spec:** `docs/superpowers/specs/2026-09-02-v3-host-manifest-readability.md`

## Global Constraints

- The installed manifest is exact `root:wheel 0444`, a regular one-link file with no extended ACL beneath root-owned non-writable direct-directory ancestry.
- The manifest is non-secret host metadata and must contain no domain identity, CA material, credentials, provider data, session state, private keys, or tokens.
- Doctor remains host-global, unprivileged, read-only, and able to hash and strictly parse the manifest without group membership.
- Normal init/doctor must not chmod, rewrite, adopt, or silently repair the existing `0400` manifest.
- The only existing migration is attended, exact-target, mode-only, and occurs after complete validation under the old published contract.
- Do not launch Tart or Softnet while implementing or verifying this correction.
- Do not mutate V2 or any preserved V4 branch/worktree.

---

### Task 1: Correct the product contract and prove OS-level readability

**Files:**
- Modify: `internal/hostx/publisher.go`
- Modify: `internal/hostx/manifest.go`
- Modify: `internal/hostx/publisher_test.go`
- Modify: `internal/hostx/doctor_test.go`
- Create: `internal/hostx/manifest_permissions_darwin_test.go`
- Create: `internal/hostx/operator_group_attended_darwin_test.go`

**Interfaces:**
- Consumes: existing package-private `manifestMode`, `RootedPublisher.State`, `RootedPublisher.Preflight`, and Unix `syscall.SysProcAttr.Credential`.
- Produces: exact `manifestMode = 0o444`; product comments classifying the schema as non-secret; direct-doctor and publisher regressions that reject legacy `0400` without mutation; root-only cross-UID integration test `TestManifestModeAllowsDistinctUnprivilegedUIDToRead`; and attended read-only group-state test `TestAttendedExactLocalOperatorGroupState`.

- [ ] **Step 1: Change test expectations first**

Update publisher and healthy-doctor fixtures to require `0o444`. Add a
regression case that publishes a manifest, changes only its mode to `0o400`,
captures its bytes and metadata, invokes both `Preflight` and `State`, expects
`publicationUnexpected`, and proves the bytes and `0o400` mode are unchanged.
Also change only the healthy doctor fixture's manifest mode to `0o400`, require
`Drifted` and `manifest.mode`, and prove doctor leaves that fake fact's mode
and bytes unchanged with zero mutations.

Create `manifest_permissions_darwin_test.go` with build constraint `darwin`.
The test must skip unless `os.Geteuid() == 0`, create a
`0755` temporary directory directly below `/private/tmp`, resolve `nobody`
with `os/user`, parse signed UID/GID strings with `strconv.ParseInt`, and use:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{
	Credential: &syscall.Credential{
		Uid:         uint32(uid),
		Gid:         uint32(gid),
		NoSetGroups: true,
	},
}
```

Prove `/bin/cat` under that distinct credential cannot read an explicit
root-owned `0400` negative-control file, then prove it reads exact bytes from a
root-owned file created with `manifestMode`.

- [ ] **Step 2: Run the focused tests and record RED**

Run:

```bash
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache \
  go test -count=1 ./internal/hostx \
  -run 'TestRootedPublisherPublishesManifestLastAndValidatesExactTree|TestRootedPublisherRejectsLegacyPrivateManifestWithoutMutation|TestDoctorReportsHealthyOnlyWhenEveryHostPrerequisiteMatches|TestDoctorReportsLegacyPrivateManifestWithoutMutation'
```

Expected: failure because production still publishes and requires `0400`.
The root-only cross-UID test may skip in this unprivileged run.

- [ ] **Step 3: Implement the minimal product change**

Change only the shared product constant and schema comment:

```go
const (
	trustedDirectoryMode = 0o755
	manifestMode         = 0o444
)
```

Document on `Manifest` that its fields are intentionally non-secret local host
metadata and that secrets/domain/session data are prohibited. Keep all parsing,
publication, validation, doctor, and uninstall paths using the one shared
constant; add no migration or repair behavior.

- [ ] **Step 4: Run focused and package tests and record GREEN**

Run:

```bash
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache go test -count=1 ./internal/hostx
```

Expected: PASS; the root-only integration test reports SKIP when unprivileged.

- [ ] **Step 5: Run the genuine cross-UID test in an attended-safe form**

Build the test binary as the operator:

```bash
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache \
  go test -c -o /private/tmp/boxwarden-v3-manifest-permissions.test ./internal/hostx
```

The maintainer then runs this exact test binary under `sudo`:

```bash
sudo /private/tmp/boxwarden-v3-manifest-permissions.test \
  -test.run '^TestManifestModeAllowsDistinctUnprivilegedUIDToRead$' -test.v
```

Expected: PASS, including a successful read after the child credential is
dropped to the distinct `nobody` UID and a failed read of the `0400` control.

- [ ] **Step 5a: Add the attended exact-group migration preflight**

Create `operator_group_attended_darwin_test.go` with build constraint `darwin`.
It must skip unless `BOXWARDEN_ATTENDED_EXACT_GROUP=1`, use `os.Getuid()`,
construct the production OS doctor inspector, call `LookupOperator` and then
`ExactOperatorGroup(operator, OperatorGroupName)`, and require the exact group
name, nonnegative GID, and one numeric member equal to the operator UID. Its
only evidence output is the bounded canonical operator-UID/group-GID/numeric-
member-list line. `ExactOperatorGroup` must retain the full read-only
`inspectExactLocalOperatorGroup(..., false)` checks: exact local group/valid
GID, caller RecordName/UID/GeneratedUID binding, named and GUID membership,
no nested groups, exhaustive caller inventory, and no other user/group sharing
the GID.

Run without opt-in during deterministic verification:

```bash
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache \
  go test -count=1 -run '^TestAttendedExactLocalOperatorGroupState$' -v ./internal/hostx
```

Expected: SKIP. The attended migration gate later builds the final-head test
binary as the operator and runs the exact opt-in test unprivileged before and
after old init; it records redacted output and requires identical evidence.

The attended operator uses these exact commands; it must not use `sudo`:

```bash
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache \
  go test -c -o /private/tmp/boxwarden-exact-group.test ./internal/hostx
BOXWARDEN_ATTENDED_EXACT_GROUP=1 \
  /private/tmp/boxwarden-exact-group.test \
  -test.run '^TestAttendedExactLocalOperatorGroupState$' -test.v
```

- [ ] **Step 6: Verify and commit Task 1**

Run:

```bash
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache go test -count=1 ./...
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache go test -race -count=1 ./internal/hostx
GOCACHE=/private/tmp/boxwarden-v3-manifest-mode-fix-gocache go vet ./...
gofmt -d internal/hostx
git diff --check
```

Expected: all Go commands pass, `gofmt -d` and `git diff --check` are empty.

Commit only the Task 1 files with:

```bash
git add internal/hostx/publisher.go internal/hostx/manifest.go \
  internal/hostx/publisher_test.go internal/hostx/doctor_test.go \
  internal/hostx/manifest_permissions_darwin_test.go \
  internal/hostx/operator_group_attended_darwin_test.go
git commit -m "fix: make host manifest readable to doctor"
```

### Task 2: Make the confidentiality and migration contract durable

**Files:**
- Modify: `AGENTS.md`
- Modify: `docs/decisions/024-qualified-softnet-privilege-binding.md`
- Modify: `docs/operations/init-and-doctor.md`
- Modify: `docs/security-model.md`
- Modify: `docs/tool-provenance.md`
- Modify: `docs/superpowers/plans/2026-09-01-boxwarden-v0.1.md`
- Include: `docs/superpowers/specs/2026-09-02-v3-host-manifest-readability.md`
- Include: `docs/superpowers/plans/2026-09-02-v3-host-manifest-readability.md`

**Interfaces:**
- Consumes: exact product contract and migration design from Task 1 and the specification.
- Produces: consistent architecture, operator, security, provenance, and gate documentation for non-secret `root:wheel 0444` manifests and fail-closed legacy handling.

- [ ] **Step 1: Update the accepted architecture and durable agent invariant**

State in ADR 024 and `AGENTS.md` that the installed manifest is exact
`root:wheel 0444`, is intentionally non-secret local host metadata, and is
protected for integrity by root ownership, no write bits, exact metadata/content
validation, and protected ancestry. Enumerate the prohibited secret/domain/
session data categories from the specification.

- [ ] **Step 2: Update operations, provenance, and security documentation**

Explain why unprivileged doctor must read/hash/parse the manifest and why
`0440` or a privileged helper would break the diagnostic boundary. State that
normal init and doctor reject rather than repair legacy `0400`. Add the exact
one-time migration sequence: final-head test binary built as the operator;
opt-in unprivileged exact-group pre-state evidence using
`inspectExactLocalOperatorGroup(..., false)` semantics; old published init
against original configuration only with `refresh-login-session: false`; the
same post-state evidence line; then exact metadata/digest capture, exact-path
mode-only change, synchronization, unchanged inode/link/owner/group/bytes/
digest/no-ACL plus `0444` and distinct-UID read; use only the corrected binary
thereafter. Stop before chmod on group-evidence mismatch, old-init failure, or
refresh request; old-init success alone is not a group pre-state/non-mutation
proof.

- [ ] **Step 3: Update the V3 plan's deterministic and attended gates**

Require exact `root:wheel 0444`, the root-only genuine cross-UID test, legacy
`0400` refusal without mutation, the direct-doctor `0400` no-mutation
regression, and redacted exact-group evidence before and after old init in the
attended migration. Preserve every existing no-Tart/no-Softnet-runtime
restriction.

- [ ] **Step 4: Check documentation consistency**

Run:

```bash
rg -n 'manifest.*(0400|0444)|root:wheel.*0444|non-secret' \
  AGENTS.md docs internal/hostx
git diff --check
```

Expected: no current contract describes the host-toolchain manifest as `0400`;
`0400` appears only as the explicitly rejected legacy state or migration input;
`git diff --check` is empty.

- [ ] **Step 5: Commit Task 2**

```bash
git add AGENTS.md docs/decisions/024-qualified-softnet-privilege-binding.md \
  docs/operations/init-and-doctor.md docs/security-model.md \
  docs/tool-provenance.md \
  docs/superpowers/plans/2026-09-01-boxwarden-v0.1.md \
  docs/superpowers/specs/2026-09-02-v3-host-manifest-readability.md \
  docs/superpowers/plans/2026-09-02-v3-host-manifest-readability.md
git commit -m "docs: define readable host manifest contract"
```

After both reviewed tasks, the lead integrates the verified commits into the
published V3 branch, reruns cumulative verification, obtains an independent
Sol/high-or-higher security review, pushes normal corrective commits, and waits
for green CI. The lead then presents the exact attended read-only validation
commands before the single exact-target `0400 -> 0444` mutation. Tart and
Softnet runtime execution remain blocked.
