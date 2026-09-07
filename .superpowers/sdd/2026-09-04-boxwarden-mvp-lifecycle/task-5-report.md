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
