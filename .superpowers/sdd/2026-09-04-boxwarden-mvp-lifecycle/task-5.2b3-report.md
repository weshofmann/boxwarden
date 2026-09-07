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
```

## Remaining concerns

The b3 classification and both startup waits are bounded by the private
five-minute/one-second production policy, with short injected policies for
deterministic tests. Runtime composition and authoritative lifecycle creation
remain deliberately deferred to Task 5.3. No real host runtime was exercised.

The focused b3 regressions cover the unheld request-plus-lock foundation and
initial detached authentication polling. Follow-up coverage is still needed
for every later-artifact class, claim-race reconciliation, and deterministic
controller timeout/cancellation sequences before treating the complete b3 test
matrix as independently demonstrated.
