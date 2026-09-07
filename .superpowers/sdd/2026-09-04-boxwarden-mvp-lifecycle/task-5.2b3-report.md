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
go test ./internal/supervisor -count=1
PASS
```

Full repository/race/vet/native/no-cgo/Linux verification is recorded with the
commit once completed.
