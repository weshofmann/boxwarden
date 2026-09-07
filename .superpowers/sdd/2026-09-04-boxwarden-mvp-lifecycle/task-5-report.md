# Task 5 Report

## Task 5A — Supervisor breaker prerequisites

**Base:** `4b50127a161d362fcb16e11ded312a5d2d97b4ba`.

### Decision and scope

Task 5A changes only `internal/supervisor`. The control client now treats Unix
connection establishment as a separately bounded pre-accept phase. After a
successful connection, it creates a fresh action context: snapshot retains its
two bounded-I/O intervals and stop retains request I/O, lifecycle, and response
I/O. The caller context remains the parent of both phases, so an earlier caller
cancellation/deadline still interrupts dialing and in-flight control I/O.

Both the owner and failed-detached-child reapers now share one completion-first
await helper: check a terminal result before waiting, wait for completion or
deadline, then check completion again before emitting a timeout diagnostic.
This preserves exact retained terminal evidence when completion and deadline
are simultaneously observable. A true late reaper still emits the diagnostic,
retains ownership, waits for its sole reaper, and performs the existing one
cleanup sequence.

No Task 5 runtime composition, serial, SSH, backend, host, VM, Screen, Tart,
Softnet, or CLI behavior was added or executed.

### TDD evidence

Tests were added before the production-path changes.

Initial focused RED (the intended narrow dial seam was absent):

```text
go test ./internal/supervisor -run 'TestStopClientStartsActionWindowAfterDelayedDial|TestOwnerReaperCompletionWinsReadyDeadline|TestDetachedChildReaperCompletionWinsReadyDeadline' -count=1 -v
internal/supervisor/supervisor_test.go:489:18: undefined: controlDialContext
```

Behavioral RED against the former single-budget implementation (with the
test-only dial seam held constant) was then observed:

```text
TestStopClientStartsActionWindowAfterDelayedDial
Stop() spent pre-accept time from its action window: context deadline exceeded
read unix ->.../supervisor.sock: i/o timeout
```

The regression supplies a real owner-private Unix control socket, delays only
pre-accept dialing by 300 ms, then performs a valid 100 ms graceful stop. The
former 360 ms single budget expires; the new fresh post-connect window passes.

Behavioral RED against the former direct `select { done | ctx.Done() }`
reaper await was also observed:

```text
TestOwnerReaperCompletionWinsReadyDeadline
ready owner completion lost to deadline
TestDetachedChildReaperCompletionWinsReadyDeadline
ready child completion lost to deadline
```

GREEN focused set passed after the minimal control/reaper changes, including
the retained-response-window, former-two-second, true-late-reaper, and
one-terminal-result regressions.

The new owner tie test repeats 200 already-ready completion/deadline races and
then exercises real `terminateAndCleanup`: its terminal result appears exactly
once, no synthetic timeout appears, `Stop`, `Wait`, and `Close` occur exactly
once, and the owned namespace is removed. The new detached-child tie test
repeats the same 200 ready races and asserts exactly one retained `Stop`/`Wait`
and no release. `TestDetachedLauncherReturnsTerminalWaitErrorOnce` continues
to compose that detached cleanup path through `Launch`, proving one Stop, one
Wait, terminal error once, and exact request removal. Existing
`TestWaitTimeoutPreservesRuntimeNamespace` and
`TestDetachedLauncherRetainsRequestUntilBlockedReaperCompletes` retain the
true-late ownership/request-retention proof.

### Verification

All commands exited zero in a host environment that permits owner-private Unix
socket binding:

```text
go test ./internal/supervisor -count=1
go test ./internal/supervisor ./cmd/boxwarden -count=20
go test -race ./internal/supervisor -count=1
go test ./...
go test -race ./...
go vet ./...
go build -o /private/tmp/boxwarden-task5a-build.lzg9ja/boxwarden-cgo ./cmd/boxwarden
CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-build.lzg9ja/boxwarden-nocgo ./cmd/boxwarden
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-build.lzg9ja/boxwarden-linux-arm64 ./cmd/boxwarden
```

The 20-iteration package/CLI test gate completed in 71.141 s. The native cgo
and no-cgo artifacts are Mach-O arm64; the cross artifact is ELF arm64.
`gofmt -l` on all changed Go files and `git diff --check` produced no output.

### Files

- `internal/supervisor/control.go`: isolated context-aware Unix dialing from
  post-connect control operation budgeting through a package-private test seam.
- `internal/supervisor/supervisor.go`: one completion-preferred reaper-await
  primitive shared by owner and detached child paths.
- `internal/supervisor/supervisor_test.go`: delayed-dial, completion-tie, and
  exact lifecycle/cleanup regressions.

## Task 5A CI correction round 1

**Base:** `6744cbfa7bf39cd87c51b21d396f7824dc649285`.

GitHub Actions run `34071176020` failed before every supervisor test body
because the test-only `privateRuntime` helper passed the macOS-specific
`/private/tmp` path to `os.MkdirTemp`; that directory does not exist on Ubuntu.
No supervisor production path ran or failed.

### TDD evidence

Added `TestPrivateRuntimeUsesGoTemporaryDirectory` before changing the helper.
The focused RED failed on macOS because the hard-coded directory was outside
Go's configured temporary directory:

```text
private runtime "/private/tmp/bw-sup-..." is not under Go temporary directory
"/var/folders/.../T/": relative="../../../../../private/tmp/bw-sup-..."
```

The minimal test-helper-only correction changes `os.MkdirTemp("/private/tmp",
"bw-sup-")` to `os.MkdirTemp("", "bw-sup-")`; Go therefore selects the
platform's configured temporary directory. The same test verifies that the
result remains a `0700` owner-private directory, preserving the existing
owner-private Unix-socket admission gate.

Focused GREEN:

```text
go test ./internal/supervisor -run '^TestPrivateRuntimeUsesGoTemporaryDirectory$' -count=1 -v
PASS
```

An initial `GOOS=linux GOARCH=amd64 go test ...` attempt correctly produced
`exec format error` because a macOS host cannot execute a Linux test binary.
The portable equivalent compiled the Linux supervisor test binary only:

```text
GOOS=linux GOARCH=amd64 go test -c -o .../supervisor-linux-amd64.test ./internal/supervisor
ELF 64-bit LSB executable, x86-64
```

### Verification

All required native test and verification commands exited zero:

```text
go test ./internal/supervisor -count=1
go test ./internal/supervisor ./cmd/boxwarden -count=20
go test -race ./internal/supervisor -count=1
go test ./...
go test -race ./...
go vet ./...
go build -o /private/tmp/boxwarden-task5a-ci-build.odPoxV/boxwarden-cgo ./cmd/boxwarden
CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-ci-build.odPoxV/boxwarden-nocgo ./cmd/boxwarden
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-ci-build.odPoxV/boxwarden-linux-arm64 ./cmd/boxwarden
```

The 20-iteration supervisor/CLI gate completed in 71.608 s. Native cgo and
no-cgo outputs are Mach-O arm64; the cross-build is ELF arm64. `gofmt -l` on
the changed Go test and `git diff --check` produced no output.

## Task 5A CI correction round 2

**Base:** `9e4c81ea5d0769d108dfe5b6a5707665366345e7`.

GitHub Actions run `34072078362` exposed a real exact-artifact ownership flaw:
the old cleanup remembered only `(path, device, inode)`. On ext4/overlayfs, a
same-UID replacement could immediately reuse an inode after the original
manifest had been removed, allowing cleanup to misclassify and unlink that
replacement. This is not the accepted active same-UID postvalidation race; it
was an avoidable artifact-lifetime gap in supervisor-owned cleanup.

### Decision

Supervisor lifecycle ownership now retains an owner-private regular-file
descriptor for each admitted immutable request and manifest. Admission verifies
the lstat/open/fstat identity before decoding bounded bytes from that retained
descriptor. `Run` supplies the admitted request to `prepare`, rather than
rereading the replaceable request pathname. It admits the generated manifest
again and requires it to equal the generated expected manifest before using it.

The parent detached launcher retains its request descriptor through start,
authenticated handoff, and any failed-launch cleanup. `Run` retains both its
request and manifest descriptors through identity-checked cleanup. The cleanup
first compares/removes only the original path identity while the descriptor is
still open, then closes the descriptor. Descriptors close on every early error,
normal handoff, and final cleanup path. `FileIdentity` remains comparison
evidence only; no PID/path adoption, new signal path, or runtime authority was
introduced.

### TDD evidence

Added the retained-artifact regressions before implementation. Initial focused
RED was the expected absent-admission capability:

```text
internal/supervisor/supervisor_test.go:624:19: undefined: admitPrivateRegular
```

GREEN coverage includes:

- `TestAdmittedPrivateRegularRetainsOriginalInodeUntilClosed`, which removes
  an admitted original, creates a replacement, proves the replacement cannot
  inherit the retained inode, then proves the original descriptor closes.
- `TestCleanupPreservesSameModeManifestReplacement`, strengthened to cover
  both the child `Run` manifest and request cleanup paths; each replacement
  must have a distinct inode and survive cleanup.
- `TestDetachedLauncherRetainsRequestInodeThroughFailedLaunchCleanup`, which
  replaces the parent-owned request during failed child start and proves the
  replacement survives exact cleanup with a different inode.

The focused regressions and their race-enabled form passed. Existing late
reaper and one Stop/Wait/Close/cleanup tests remain green, so the correction
does not add a second lifecycle cleanup sequence.

### Verification

All required native verification commands exited zero:

```text
go test ./internal/supervisor -count=1
go test ./internal/supervisor ./cmd/boxwarden -count=20
go test -race ./internal/supervisor -count=1
go test ./...
go test -race ./...
go vet ./...
go build -o /private/tmp/boxwarden-task5a-ci2-build.Rjvxpx/boxwarden-cgo ./cmd/boxwarden
CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-ci2-build.Rjvxpx/boxwarden-nocgo ./cmd/boxwarden
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-ci2-build.Rjvxpx/boxwarden-linux-arm64 ./cmd/boxwarden
GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/boxwarden-task5a-ci2-build.Rjvxpx/supervisor-linux-amd64.test ./internal/supervisor
```

The 20-iteration supervisor/CLI gate completed in 73.117 s. Native cgo and
no-cgo outputs are Mach-O arm64; the Boxwarden cross-build is ELF arm64 and
the compile-only supervisor test binary is ELF amd64. `gofmt -l` on changed Go
files and `git diff --check` produced no output.

## Task 5A CI correction round 3

**Base:** `e284168b7d0feac6356a36bf80bb6ecc98fc6289`.

Review found that retained-file admission still used an initial `lstat` followed
by ordinary `os.Open`. A same-UID actor could hard-link the original inode,
replace the fixed artifact name with a symlink to that hard link between those
operations, and preserve `(device,inode)`. The descriptor then referred to the
right inode but the fixed layout contained a symlink, which violates artifact
admission rules.

### Decision

`admitPrivateRegular` now uses `os.OpenFile` with `O_NOFOLLOW`, available on
the supported Darwin and Linux targets, after the initial lstat. It validates
the held descriptor's regular type, `0600` mode, owner, and identity; then
performs a second `Lstat` of the pathname and requires non-symlinked regular
type, owner, mode, and exact identity equality with the held descriptor before
any bounded bytes are decoded. Failure closes the descriptor. Existing retained
request/manifest cleanup remains unchanged and continues to close descriptors
on success and every error path.

This is deliberately a bounded admission claim. An active same-UID replacement
after that final pathname validation remains outside the claim; the held
descriptor preserves the admitted bytes, and later cleanup continues to
preserve a changed pathname rather than adopting or removing it.

### TDD evidence

Added deterministic hard-link-to-symlink substitutions before implementation
for both `admitLaunchRequest` and `admitManifest`. The initial RED was the
missing narrow admission seam:

```text
undefined: privateRegularAdmissionHook
```

The behavioral RED against the former ordinary-open path then proved the
defect:

```text
TestAdmitLaunchRequestRejectsHardLinkSymlinkSubstitution
hard-link-backed request symlink was admitted
TestAdmitManifestRejectsHardLinkSymlinkSubstitution
hard-link-backed manifest symlink was admitted
```

With no-follow open and final pathname validation, both regressions pass,
alongside the round-2 request/manifest replacement-retention tests. A first
race run exposed that the asynchronous `runningService` fixture was still
admitting a manifest while the global test seam changed. The test now generates
a valid manifest synchronously with `prepare`; the same focused set passes
under `-race`, with no production synchronization or behavior added.

### Verification

All required native verification commands exited zero:

```text
go test ./internal/supervisor -count=1
go test ./internal/supervisor ./cmd/boxwarden -count=20
go test -race ./internal/supervisor -count=1
go test ./...
go test -race ./...
go vet ./...
go build -o /private/tmp/boxwarden-task5a-ci3-build.peDumT/boxwarden-cgo ./cmd/boxwarden
CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-ci3-build.peDumT/boxwarden-nocgo ./cmd/boxwarden
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /private/tmp/boxwarden-task5a-ci3-build.peDumT/boxwarden-linux-arm64 ./cmd/boxwarden
GOOS=linux GOARCH=amd64 go test -c -o /private/tmp/boxwarden-task5a-ci3-build.peDumT/supervisor-linux-amd64.test ./internal/supervisor
```

The 20-iteration supervisor/CLI gate completed in 75.206 s. Native cgo and
no-cgo outputs are Mach-O arm64; the Boxwarden cross-build is ELF arm64 and
the compile-only supervisor test binary is ELF amd64. `gofmt -l` on changed Go
files and `git diff --check` produced no output.

## Task 5 — resumed start-boundary implementation (partial; not a completed READY path)

**Base:** `a8462c564a6d0fcd06a3156daaba089e19247d30`.

### Implemented foundation

- Added a narrow `session.StartDependencies` / `SupervisorControl` boundary.
  `Start` holds the session lock, requires host and complete configured-domain
  CA admission, verifies the exact stopped backend object, persists `starting`
  plus a UUID generation and `readiness=starting`, and only then asks
  `StartExact` to act. A fresh exact all-healthy supervisor snapshot is
  required before it atomically persists `running` / `ready`; a stale or
  incomplete snapshot leaves the exact `starting` generation durable for
  recovery.
- Added `SessionRecordName` to the comparable `supervisor.LaunchRequest`, and
  an `ExactController` which launches only the supplied request and then
  requires an authenticated snapshot with the exact same binding.
- Added a public `session start <name>` command dispatch through one narrow
  application `SessionStarter` interface, plus deterministic output.
- Ported the narrowly typed time-zone detector/convergence helpers from the
  preserved source material. The detector accepts only an IANA path resolved
  under the fixed trusted zoneinfo root; convergence requires apply and exact
  typed readback.

### Honest RED / GREEN evidence

Tests were written before the corresponding production code.

Initial session RED, before the start composition types existed:

```text
undefined: NewStartService
undefined: StartDependencies
service.Start undefined
undefined: RuntimeAdmission
supervisor.LaunchRequest.SessionRecordName undefined
```

The test names the production break: moving persistence below `StartExact`
would expose runtime mutation without durable generation ownership. Its fake
does not assert mock calls: at the runtime boundary it independently reloads
the real stored record and rejects anything other than the hand-specified
`starting` state, generation, and request binding. After the minimal service
implementation:

```text
go test ./internal/session -run 'TestStart' -count=1 -v
PASS
```

Initial time-zone RED, before the package existed:

```text
undefined: Converge
undefined: HostDetector
```

The green detector test resolves literal `/etc/localtime` to a literal
`America/Denver` file under the trusted zoneinfo root; the convergence test
proves an exact guest readback is required after typed application:

```text
go test ./internal/timezonex -run 'Test(DetectHost|HostDetector|Converge)' -count=1 -v
PASS
```

Initial exact-controller RED:

```text
undefined: NewExactController
```

The green behavior rejects a snapshot with a different binding instead of
turning a merely launched child into a ready generation:

```text
go test ./internal/supervisor -run '^TestExactControllerReturnsOnlyAuthenticatedExactSnapshot$' -count=1 -v
PASS
```

The public-command test was likewise RED before `Options.SessionStarter` and
the parser branch existed (`unknown field SessionStarter`), then green with a
selected-domain starter and fixed output.

### Mandatory incompleteness / blocker

This is not the requested complete Task 5 implementation. The inherited Task
4 `supervisor.RunRequest` still deliberately constructs `unavailableOwner`;
there is no Task-5 composition runner to reload authoritative config/record,
revalidate complete host/CA admission, publish the outer generation atomically,
invoke `serialx`, perform serial bootstrap/pin/client-key/certificate/address/
strict-SSH/zone work, or clean proven owned runtime on failure. Consequently
`cmd/boxwarden/main.go` cannot honestly compose a functioning public start and
does not provide `Options.SessionStarter`; invoking the new public command in
the production binary returns `session starter is required` rather than
launching a VM. No real Tart, Softnet, Screen, SSH, or VM was launched.

### Verification actually run

```text
go test ./internal/app ./internal/session ./internal/timezonex ./internal/supervisor -run 'Test(SessionStart|Start|HostDetector|Converge|ExactController)' -count=1 -v
PASS
go test ./...
PASS
go test -race ./...
PASS
go vet ./...
PASS
go build ./cmd/boxwarden (native cgo)
PASS (Mach-O arm64)
CGO_ENABLED=0 go build ./cmd/boxwarden
PASS (Mach-O arm64)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/boxwarden
PASS (ELF arm64)
GOOS=linux GOARCH=amd64 go test -c ./internal/session
PASS (ELF amd64 compile-only test binary)
gofmt -l [changed Go files]
(no output)
git diff --check
(no output)
```

## Task 5.2 — supervisor publication/admission correction

**Base:** `a6811888a776faf842229f0a86f91be936b1663a`.

This bounded slice replaces direct request creation in an existing generation
directory with supervisor-owned staged publication. It creates/adopts only
owner-private parent directories, writes and syncs a `0600` immutable request
in a sibling staging directory, uses Darwin `renameatx_np(..., RENAME_EXCL)`
for no-replace publication (and fails closed in Darwin no-cgo builds), then
syncs the parent. Existing retry state must contain precisely the exact
immutable request; empty, foreign, and extra-entry state is rejected without
mutation. The launcher removes a first-publication namespace only after its
existing exact child cleanup path completes.

The request now carries the complete public host manifest plus comparable
Screen/Softnet facts, and a public-only CA projection. Session has no fallback
for an absent configured-domain collection, passes those expectations to the
supervisor, and uses its injected trusted clock to reject both stale and
future READY evidence.

### TDD evidence

New publication tests were RED before `publishOrAdmitRequest` existed:

```text
undefined: publishOrAdmitRequest
```

They then proved staged first publication has only `supervisor-request.json`,
request-only exact retry is admitted, and a pre-created empty generation is
rejected. The future-snapshot session regression was RED before the injected
clock field existed (`unknown field Now`), and is green once the trusted clock
is used for both lower and upper freshness bounds.

Focused GREEN:

```text
go test ./internal/session ./internal/supervisor -run 'Test(Start|Publish)' -count=1
go test ./internal/supervisor -count=1
```

Both passed. No real host runtime was launched.

### Explicitly still open

5.3 still must implement authoritative detached-child reload and comparison of
these request expectations, authenticated live-artifact reconciliation before
any spawn decision, and serial/bootstrap/SSH composition. 5.4 still owns
failure convergence and production main wiring. `RunRequest` remains the
intentional unavailable owner; this slice does not claim a working VM start.

### 5.2 corrective review follow-up

The final 5.2 correction changes the publication primitive to the repository's
pure-Go Darwin `renameatx_np(RENAME_EXCL)` syscall pattern, so `CGO_ENABLED=0`
Darwin builds retain no-replace publication. Linux has only the same
conservative, explicitly unqualified CI fallback used by `hostx`: it rejects
an already visible destination and must not be read as a production atomicity
claim. The regression test creates a competing empty generation and proves
the staged directory remains and the competitor is not replaced.

`ExactController` now classifies an existing exact generation before calling
the launcher. A request-only matching directory can launch; empty, missing,
foreign, symlinked, malformed, or unexpected outer state fails closed. The
only accepted live outer names are the request, lock, manifest, socket, and
owner-private `serial/` directory (whose nested contents remain serialx-owned
and opaque here). Live state is returned only from the authenticated exact
controller; it gets a bounded reconciliation interval rather than spawning a
second child. This does not make `RunRequest` available—the runtime owner and
its authoritative revalidation remain 5.3 work.

Request JSON field names are now stable and explicit. Request validation
requires canonical record identity, a complete qualified public host manifest
with Screen and Softnet expectations, and all public CA identity fields; no
private CA paths, state roots, process handles, or opaque Screen capability is
serializable. The controller now applies both stale and future bounds against
an injectable trusted clock.

The staged-directory cleanup no longer uses `RemoveAll`: it captures the
private staging identity, removes only the retained request identity, then
removes the captured directory. Parent admission explicitly verifies the
runtime/domain/session components as private non-symlink directories.

Additional RED/GREEN evidence:

```text
go test ./internal/supervisor -run 'Test(RenameWithoutReplacePreservesConcurrentGenerationNamespace|ExactControllerReconcilesAuthenticatedLiveGenerationWithoutLaunch)' -count=1
RED: StartExact() launched a second child for authenticated live generation
GREEN: PASS

go test ./internal/supervisor -run TestClientRejectsFutureAuthenticatedSnapshot -count=1
RED: undefined: validateSnapshotFreshness
GREEN: PASS

go test ./internal/supervisor ./internal/session -count=1
PASS
go test ./...
PASS
```

The stale, unheld `generation.lock` normalization requested in review remains
open: the available identity/advisory-lock evidence is not safe deletion
authority, and the safety guard correctly rejected an implementation that
would remove it based only on a nonblocking flock. It therefore remains drift
without mutation in this correction rather than risking a concurrent-owner
deletion. 5.3/5.4 must supply an owner-authenticated convergence protocol for
that case. Production main wiring and owned failure convergence remain open.

## Task 5.2 review-round correction (partial safety fixes)

This corrective commit rejects duplicate JSON fields recursively before strict
decode, binds `CA.Domain` to `Binding.Domain`, moves the runtime admission
projection to `hostx`, and nests serial endpoint evidence below `serial/`.
`hostx.SystemDoctor.CheckRuntime` refuses every non-Healthy Doctor result and
returns the qualified manifest plus public Screen/Softnet projection and the
opaque Screen admission. Session consumes the projection type only.

RED/GREEN evidence:

```text
go test ./internal/supervisor -run TestDecodeExactRejectsDuplicateNestedFields -count=1
RED: decodeExact() accepted duplicate nested binding field
GREEN: PASS

go test ./internal/supervisor -run TestPublishOrAdmitRequestRejectsCADomainDifferentFromBinding -count=1
RED: publishOrAdmitRequest() accepted CA identity for another domain
GREEN: PASS

go test ./internal/hostx ./internal/session ./internal/supervisor -count=1
PASS
go test ./...
PASS
```

The descriptor-transfer lock protocol and injected realistic startup policy
remain unresolved in this commit. They cannot safely be simulated by deleting
or briefly releasing the present lock; the current detached launcher API lacks
an `ExtraFiles`/child-handoff capability. No completion claim is made for that
Critical review finding.

### Correction before commit

The initial `CheckRuntime` draft called `Doctor` and then re-read manifest and
Screen state. That is a TOCTOU and does not satisfy the same-inspection
contract. It is intentionally left uncommitted pending a refactor that has
Doctor return one unexported inspection result (report, parsed manifest, and
screen inspection result) for both public projections. Likewise, the lock
descriptor-transfer Critical finding remains open. No review-round commit was
made from this incomplete state.

## Task 5.2a — same-inspection admission correction

`SystemDoctor` now has one unexported inspection result that carries the
normalized Doctor report, the manifest parsed during that inspection, and the
same `screenInspectionResult` added to the report. `Doctor` projects only the
report; `CheckRuntime` returns only comparable public expectation facts;
`AdmitRuntime` is the later child-facing capability that additionally returns
the opaque Screen admission. Neither method re-reads manifest or re-probes
Screen after Doctor.

Outer live-state validation accepts serial only as one private directory and
rejects root-level relay endpoints. Its finite credential allowlist is exactly
`client`/`known_hosts` (0600) and `client.pub`/`client-cert.pub` (0644); no
prefix or glob is admitted. Transient credential staging remains a 5.3 design
item. The generation-lock handoff and injected startup reconciliation policy
remain explicitly open for 5.2b.

## Task 5.2a follow-up — Screen minting surface and finite outer artifacts

`SystemDoctor.CurrentScreen` was removed from the exported API: a screen-only
inspection cannot mint the opaque capability for production callers. The sole
child-facing production mint is `AdmitRuntime`, after its complete shared
Doctor inspection; parent `CheckRuntime` remains expectation-only. In-package
tests retain the unexported helper for narrow Screen inspection coverage.

The supervisor outer artifact tests now independently enumerate every accepted
credential name/mode/type and rejected near-miss, arbitrary, and root serial
endpoint. Recursive duplicate rejection is tested through request binding,
host manifest, CA, and manifest evidence nesting.

Focused GREEN:

```text
go test ./internal/hostx ./internal/supervisor -run 'Test(ScreenAdmissionPublicSurface|ValidateLiveOuterEntry|DecodeExact)' -count=1
PASS
```

No generation-lock or startup-policy work was added; those remain the bounded
5.2b follow-up.
