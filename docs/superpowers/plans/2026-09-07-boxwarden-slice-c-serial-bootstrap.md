# Boxwarden Slice C Serial Bootstrap Implementation Plan

> **For agent execution:** Follow `superpowers:test-driven-development` task by task. Every behavior change begins with a focused failing test, then the smallest implementation, then focused and cumulative verification.

**Goal:** Compose the retained Slice B Tart runtime with the existing bounded one-PTY guest bootstrap and immutable host-key pin store, while leaving the durable session `STARTING / NOT_READY` and stopping before SSH management or READY.

**Spec:** `docs/superpowers/plans/2026-09-04-boxwarden-mvp-lifecycle.md` (Slice C), ADR 017, and the approved 2026-09-07 Slice C checkpoint retained in the initiating task.

**Architecture:** The parent session service persists and reuses one exact start generation. The detached supervisor starts and owns Tart plus `serialx`, then exposes one fixed typed `bootstrap` control action alongside read-only snapshot and exact stop. The owner builds the guest request only from its independently reloaded durable binding and selected-domain public CA. `serialx` emits one canonical request, validates the correlated response, and permanently drains afterward. The owner admits the returned Ed25519 key through the existing domain pin store and reports `PinPresent` only after a fresh exact pin read. A pin-write retry reuses the validated response; a poisoned serial transport remains live until a later explicit same-generation retry stops and relaunches that exact generation.

**Tech Stack:** Go, existing `supervisor`, `sessionruntime`, `serialx`, `guestproto`, and `sshx` packages; Tart/Softnet only for the final controlled host check.

---

## Task 1: Add one bounded typed supervisor bootstrap action

**Files:**
- Modify: `internal/supervisor/request.go`
- Modify: `internal/supervisor/control.go`
- Modify: `internal/supervisor/exact.go`
- Modify: `internal/supervisor/supervisor.go`
- Test: `internal/supervisor/minimal_test.go`
- Test: `internal/supervisor/transport_test.go`

1. Add failing tests proving that `StartExact` first establishes the exact live started generation, then invokes exactly one typed bootstrap action, and returns only an exact fresh snapshot with backend running, serial healthy, and pin present.
2. Add failing wire tests for wrong binding, unknown action, bounded bootstrap expiry, duplicate/concurrent requests, and precise bootstrap errors without generic command execution.
3. Run the focused supervisor tests and record the expected RED failures.
4. Extend `RuntimeOwner` and the private controller surface with `Bootstrap(context.Context) error`; add `Client.Bootstrap` using the existing canonical framed control protocol and a fixed bounded deadline.
5. Keep `detachedLauncher.Launch` waiting only for the Slice B started boundary; make `ExactController.StartExact` invoke bootstrap after it has proved exact live ownership.
6. Run focused tests GREEN, then `go test -count=1 ./internal/supervisor`.

## Task 2: Compose authoritative guest trust and pin admission in the owner

**Files:**
- Modify: `internal/sessionruntime/owner.go`
- Modify: `internal/sessionruntime/owner_test.go`
- Modify: `internal/sessionruntime/process_test.go`
- Modify: `internal/sessionruntime/cleanup_integration_test.go`

1. Extend the owner fixture with narrow serial-bootstrap and pin-store fakes.
2. Add failing tests proving the request is constructed from the reloaded durable domain/session/backend/generation and revalidated public CA, with a fresh bounded nonce and derived principal; no request locator becomes authority.
3. Add failing tests proving one valid response admits the exact Ed25519 pin, `PinPresent` is fresh-read rather than inferred, and the durable record remains `STARTING / NOT_READY`.
4. Add failing tests proving concurrent bootstrap calls emit one guest request; a pin-write interruption retries from the cached validated result without a second serial command; exact pin retry succeeds; conflict and invalid host key fail closed.
5. Implement a serialized owner bootstrap state machine. Store only the validated in-memory public result, never private CA material in guest input or durable runtime request. Use `sshx.NewPinStore` through a narrow admit/load interface.
6. Preserve retained handle and serial lifetime on bootstrap failure. Keep serial poison visible through `SerialHealthy=false`; cleanup still occurs only through exact stop/reap.
7. Run focused owner/process/cleanup tests GREEN, then `go test -count=1 ./internal/sessionruntime`.

## Task 3: Preserve the exact generation across bootstrap retry

**Files:**
- Modify: `internal/session/start.go`
- Modify: `internal/session/start_test.go`

1. Add failing tests for initial bootstrap success returning unchanged `starting + G`, live healthy incomplete retry invoking bootstrap on the same G, and exact already-pinned retry succeeding idempotently.
2. Add failing tests for poisoned serial retry: stop only the exact live G, prove the exact backend stopped, relaunch the same G, and never call `NewGeneration`.
3. Add failing tests for pin conflict, runtime/binding drift, and interruption preserving `starting + G` with no READY publication.
4. Implement the narrow retry branch. For `starting + G` and a running backend, inspect the exact supervisor first; reuse a healthy exact runtime, or explicitly stop a poisoned exact runtime before relaunching the same persisted G.
5. Require `PinPresent` at the Slice C acceptance boundary while leaving certificate/probe/zone predicates false and never persisting `StateRunning` or `ReadinessReady`.
6. Run focused session tests GREEN, then `go test -count=1 ./internal/session`.

## Task 4: Update architecture guards and repository-facing Slice C documentation

**Files:**
- Modify/rename as appropriate: `internal/architecture/slice_b_runtime_guard_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/lifecycle-and-recovery.md`
- Modify: `docs/superpowers/plans/2026-09-04-boxwarden-mvp-lifecycle.md`
- Test: `cmd/boxwarden-guest-bootstrap/artifact_test.go`

1. Add failing architecture-guard cases that allow exactly the intended `serialx.Bootstrap` and `sshx` pin selectors only in `sessionruntime`, while continuing to reject Screen, operator/second PTY, generic serial command, later SSH/time-zone/READY calls, HMAC, libproc, OFD, and persisted process authority.
2. Update the production-tree guard to assert exactly one PTY allocator/runtime construction and one bounded bootstrap composition route.
3. Update architecture/lifecycle text to describe the completed Slice C boundary and explicitly defer SSH client credentials, address resolution, time-zone convergence, probes, and READY to Slice D.
4. Run architecture tests, tracked shell syntax checks, guest helper reproducible artifact verification, `gofmt`, and `git diff --check`.

## Task 5: Verify, publish, and perform one controlled real-host check

**Files:**
- Add: `docs/evidence/slice-c-controlled-serial-bootstrap.md`
- Update: Draft PR body

1. Review the cumulative diff against exact base `e3dbcb08f863a447af43f6ab56c5ed6c7d07818e`; measure production/test LOC and scan for private paths, usernames, keys/tokens, private-key markers, process-environment output, PIDs, unnecessary real UUIDs/object names, and accidental safety refs.
2. Run `gofmt`, tracked shell syntax, helper artifact verification, architecture guards, `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `go build ./cmd/boxwarden`, and `git diff --check`.
3. Commit each verified implementation unit with a detailed engineering record, push it immediately, and create/maintain one Draft PR titled `Serial bootstrap guest trust and host-key pin` against `main`.
4. On the existing admitted personal-Mac environment, use one exact known-good disposable Tart object and narrowly scoped Boxwarden state/runtime paths. Demonstrate create/start, one real serial bootstrap, exact guest active trust association, one serial-observed Ed25519 host key, exact host pin persistence, retained supervisor ownership, and `STARTING / NOT_READY`. If practical, repeat exact same-G start once. Do not inspect broad processes/environments or run hostile workloads.
5. Sanitize the evidence to public bindings/digests/fingerprints and generic object labels; rerun the cumulative secret/privacy scan before committing and pushing it.
6. Re-run the complete verification at the exact published head, inspect Draft PR CI, recommend Ready only if deterministic CI and the controlled host criterion are green, then stop without beginning Slice D.
