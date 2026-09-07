# Task 5.2b3 Report — Exact-generation retry classification

**Base:** `e024d5b`.

## Ownership result

`ExactController` now distinguishes absent/request-only launch, exact
request-plus-unheld-bound-lock resumable launch, and a held exact lock with
valid lifecycle entries. An unheld lock plus any later-phase artifact is
drift/no mutation. The classification probe is retained-descriptor based and
unlocks/closes a successful probe before launch; b2 re-admits and claims the
foundation independently. A b2 `errGenerationAlreadyOwned` claim race moves to
authenticated reconciliation rather than spawning or cleaning a competitor.

Reconciliation has a narrow private injected startup policy: production uses a
five-minute timeout and one-second interval; tests may use short deterministic
values. It polls snapshots immediately and thereafter until exact fresh READY,
cancellation, or timeout; non-ready authenticated snapshots remain pending and
wrong bindings fail closed.

## Scope retained

No Task 5.3 runtime construction, serial, backend, SSH, credentials, zone,
session settlement, or CLI work was added. No real host/VM tools ran.

## Verification

```text
RED: go test ./internal/supervisor -run TestAwaitAuthenticatedPollsUntilExactReady -count=1
FAIL: undefined: awaitAuthenticatedWithPolicy

GREEN: go test ./internal/supervisor -run 'TestAwaitAuthenticatedPollsUntilExactReady|TestExactController' -count=1
PASS

go test ./internal/supervisor -count=1
PASS

go test ./internal/supervisor ./internal/session ./cmd/boxwarden -count=20
PASS

go test -race ./internal/supervisor ./internal/session ./cmd/boxwarden -count=1
PASS

go test ./...
PASS

go vet ./...
PASS

CGO_ENABLED=1 go build ./cmd/boxwarden
CGO_ENABLED=0 go build ./cmd/boxwarden
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/boxwarden
GOOS=linux GOARCH=amd64 go test -c ./internal/supervisor
PASS

gofmt -w internal/supervisor/exact.go internal/supervisor/supervisor.go internal/supervisor/exact_test.go
git diff --check
PASS

RED: go test ./internal/supervisor -run 'TestExactController(PollsHeldCrashWindowUntilReady|CancellationAndTimeoutArePromptAndDiagnostic|WrongAuthenticatedBindingFailsWithoutExtraPoll|RejectsUnheldLaterArtifactsWithoutMutation|ReconcilesClaimContentionWithoutCleanup)' -count=1
FAIL: pre-cancelled reconciliation sampled before context cancellation; timeout discarded Snapshot.Diagnostic

RED: go test ./internal/supervisor -run 'Test(ExactController|DetachedLauncherCleansExactUnheldFoundationAfterStartFailure)' -count=1
FAIL: claimed exact request-plus-lock foundation remained as an empty directory after pre-detachment start failure

GREEN: go test ./internal/supervisor -run 'Test(ExactController|AwaitAuthenticated|DetachedLauncherCleansExactUnheldFoundationAfterStartFailure)' -count=1
PASS

FINAL CLOSEOUT: go test ./internal/supervisor -count=1; go test ./internal/supervisor ./internal/session ./cmd/boxwarden -count=20; go test -race ./internal/supervisor ./internal/session ./cmd/boxwarden -count=1; go test ./...; go vet ./...; CGO_ENABLED=1 go build ./cmd/boxwarden; CGO_ENABLED=0 go build ./cmd/boxwarden; GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/boxwarden; GOOS=linux GOARCH=amd64 go test -c ./internal/supervisor; git diff --check
PASS
```

## Closeout behavior

The held crash-window test has no manifest or socket and sequences controller
unavailable, authenticated non-READY, then exact READY; it makes zero launch
calls. Timeout reports a non-READY snapshot diagnostic, pre-cancel avoids the
first snapshot in both wait paths, and mid-loop cancellation returns promptly.
Wrong authenticated bindings fail on their first sample. The later-artifact
table covers `supervisor-manifest.json`, a real Unix socket, `serial/`, and all
four credential shapes; it proves no launch and preservation of every entry.
An `errors.Join(errGenerationAlreadyOwned, ...)` launch result reconciles the
same namespace to READY without cleanup. A failed real detached launch from a
claimed request-plus-lock-only foundation removes its retained request, lock,
and proven empty directory.

## Remaining concerns

The b3 classification and both startup waits are bounded by the private
five-minute/one-second production policy, with short injected policies for
deterministic tests. Runtime composition and authoritative lifecycle creation
remain deliberately deferred to Task 5.3. No real host runtime was exercised.

The deterministic b3 matrix is complete for exact-generation retries. Runtime
composition and any real Tart/serial/SSH qualification remain deliberately
outside this slice.
