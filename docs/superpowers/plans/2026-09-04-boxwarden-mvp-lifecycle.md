# Boxwarden MVP Lifecycle Implementation Plan

Status: Slices A–B complete; Slices C–H pending. Stop after B for human inspection and the bounded controlled-host check.

## Goal and authority

Deliver one controlled, domain-scoped M1A workflow:
`create → start → READY → status → stop → destroy --allow-data-loss`.

The approved 2026-09-07 threat-model simplification supersedes the old remaining
Task 5–9 plan and same-UID ownership design. The accepted checkpoint
`baeea7ed2991448c483593cf39d3e39d9420de8d` remains permanent history.
Do not rewrite published history. Continue on
`weshofmann/feat/mvp-lifecycle` and the existing Draft PR, with focused commits
and review checkpoints. Smaller slices do not imply stacked branches or PRs.
No merge without human approval.

The source approval is retained in the initiating task attachment; this plan,
ADR 017, and the current architecture/security/state documents record the
repository-facing decisions. Earlier implementation reports and abandoned
Task 5/OFD work are not execution instructions.

## Architecture and invariants

The host and cooperating Boxwarden host processes are trusted. Guest-originated
data remains hostile. Common Go code owns domains, state, locks, readiness,
trust, credentials, and destructive policy; the Tart adapter owns VM mechanics.

- Durable identity is the exact domain/session UUID/backend object binding.
  Persist intent and start generation under the session operation lock before
  runtime mutation. Retrying must reconcile that same durable generation.
- One lightweight detached supervisor holds an ordinary generation ownership
  lock, retains the actual Tart handle in memory, and follows one stop/wait/reap
  path. The private bounded typed Unix socket binds each request and response
  to the exact domain/session/backend/generation. It serves one request at a
  time; client expiry is a validated server-capped liveness bound, not authority.
  Expired queued snapshots receive no fresh observation window, and a live
  request's effective absolute expiry reaches backend observation.
- The minimal persisted request contains only binding, runtime/configuration
  locators, and canonical session-record name. It is an expected binding
  record, not authority. Child composition reloads configured domain and durable
  record and independently checks host/CA prerequisites before runtime creation.
- The supervisor owns the generation namespace; `serialx` alone creates and
  cleans a new `<generation>/serial/` subtree. It owns one PTY pair; Tart gets
  only the mode-`0600` slave. One master pump owns the exclusive bounded
  bootstrap exchange and permanently transitions to bounded discard/drain.
- No Screen, operator endpoint, console lease, generalized console arbitration,
  HMAC/control key, ownership manifest, persisted process authority, libproc
  reconstruction, or retained-descriptor/OFD attestation is part of MVP.
  Ordinary safe path/type/no-follow hygiene and exact retry validation remain.
- Guest bootstrap uses a fixed command and exactly one canonical bounded JSON
  line without waiting for PTY EOF. Validate nonce/generation/domain/session/
  backend correlation, public CA/principal binding, sshd settings, and host key.
  The CA private key remains host-only.
- Guest trust publication is atomic absent-or-exact: publish a complete validated
  tree only when absent, accept exact existing bytes/modes idempotently, and
  reject every conflict without replacing active trust.
- Pin the serial-observed fresh Ed25519 host key with no TOFU before certificate
  SSH. Use one narrow generation client key, short no-extension certificates,
  fixed strict SSH and fixed helper commands; no forwarding, agent, proxy,
  ambient SSH configuration, or generic command execution.
- Preserve the exact admitted Tart/Softnet pair and default containment mapping.
  Use admitted absolute Tart, configured canonical Tart home, generation-private
  TMPDIR, and the closed environment: `PATH=<qualified-softnet-digest-dir>`,
  `HOME=<admitted-home>`, `USER=<admitted-user>`, `LOGNAME=<admitted-user>`,
  `TART_HOME=<configured-home>`, `TMPDIR=<generation-dir>`, `LANG=C`, `LC_ALL=C`.
  No sudo, host mounts, clipboard, audio, credential/runtime sockets, bridges,
  host networking, port publication, or Softnet allow flags.
- READY requires fresh exact-generation live evidence: running backend, healthy
  serial drain, exact pin, current certificate, strict typed SSH probe, and exact
  host/guest IANA-zone agreement. Status observes only.
- Until project durability can be established, destroy requires explicit
  `--allow-data-loss` before any mutation and targets only the exact session
  object and state. Never a golden, prefix match, or other domain.

## Existing foundations

V2 stopped-clone creation and V3 explicit host/domain admission remain. Slice B
now composes the public create/start path through the admitted configuration,
durable session state, detached exact supervisor, one-PTY drain, and retained
Tart handle. Public golden registration, session creation, and status observation
all use the exact configured Tart executable and `TART_HOME`; they do not select
an ambient executable or default Tart namespace. A successful start returns the
durable `starting + generation G` record after proving the exact backend running
and the serial drain healthy. Guest bootstrap, trust publication, host-key pin,
management address, certificate SSH, time-zone convergence, and READY remain
uncomposed foundations for Slices C and D.

The old source refs `0045e205`, `2e711d617`, `75763d58`, and
`6ca97e782` remain historical source material only. The former generation
ownership design is superseded by this plan and amended ADR 017. Do not import
their old console or same-UID fortification machinery.

## Vertical slices and estimates

Estimates are additional production Go physical lines and bounded focused
engineering effort, excluding tests, documentation, review, and external
qualification delays. They are sizing guides, not acceptance gates. Each slice
must preserve the full deterministic suite; G integrates coverage and does not
defer testing from earlier slices.

| Slice | Status | Production estimate | Focused effort |
| --- | --- | --- | --- |
| A — Simplified trusted-host foundation | Complete | Net reduction measured at checkpoint | 3 focused implementation/review units |
| B — Durable create/start/retry and exact VM launch | Complete | 300–550 lines | 1–2 days |
| C — Boot and serial bootstrap composition | Pending | 180–350 lines | 1–2 days |
| D — Strict SSH management and READY | Pending | 350–650 lines | 2–3 days |
| E — Exact stop and explicit-loss destroy | Pending | 250–450 lines | 1–2 days |
| F — CLI, read-only status, and output | Pending | 180–320 lines | 1–2 days |
| G — Deterministic integration validation | Pending | 0–100 corrective lines | 1 day |
| H — Controlled-host qualification and release evidence | Pending | 0–150 corrective lines | 1–2 days plus attended qualification availability |

### A — Simplified trusted-host foundation

- [x] Replace supervisor same-UID fortification with minimal exact binding,
  ordinary generation lock, typed private control, and retained runtime handles.
- [x] Replace two-PTY/Screen arbitration with one exclusive bootstrap/drain PTY;
  serial request decoding consumes one canonical bounded line without EOF.
- [x] Preserve strict guest correlation, atomic absent-or-exact publication,
  host-only CA private key, no-TOFU SSH, and unchanged containment.
- [x] Remove current Screen admission/readiness and stale ownership contracts;
  amend ADR 017 and related documents in place.
- [x] Complete deterministic verification and measure production/branch text LOC.

Acceptance: materially smaller architecture, retained supported behavior,
explicit refusal for unavailable composition, and measured reduction.
This slice performs no Tart/Softnet/VM runtime work. Stop for human inspection
before B; a green foundation alone does not authorize beginning B.

### B — Durable create/start/retry and exact VM launch

- [x] Compose `internal/session → internal/supervisor → internal/sessionruntime
  → internal/backend/tart`, retaining V2's intent-first stopped creation.
- [x] Reload authoritative configuration and durable state in the child, recheck
  current host and complete configured-domain CA admission, and retain the exact
  returned Tart handle rather than persisting or reconstructing process authority.
- [x] Persist `starting + generation G` before runtime mutation and return that
  unchanged non-ready record only after fresh exact backend-running and healthy
  serial-drain proof.
- [x] Reconnect only while the matching generation remains live; if it validly
  transitions into cleanup or absence during the bounded wait, re-enter exact
  admission and relaunch only the same G from an exact structurally resumable
  stopped namespace. Reject missing, ambiguous, foreign, symlinked, or
  unexpectedly populated state without adoption.
- [x] Bind production registration, creation, status, parent start observation,
  and child launch/observation to the admitted absolute Tart executable and exact
  configured Tart home with closed environments.
- [x] Support realistic Darwin runtime paths through a transient owner-private
  short bind/connect alias while keeping the real socket and its inode authority
  in the canonical exact generation.
- [x] Make post-reap outer cleanup crash-recoverable: atomically rename exact G
  to a deterministic same-parent residue while retaining its lock, fsync the
  request removal before publishing the lock marker, fsync the remaining durable
  namespace phases, recover exact admitted stages including request+marker, and
  fail closed on active, coexisting, foreign, malformed, or unexpected state.

Deterministic persistence, retry, process-boundary retention, exact cleanup with
rename/unlink/fsync interruption injection, long-path transport, and
configured-namespace tests are complete. The bounded controlled real-host
exact-start check is the next controller-owned gate and has not yet run; it is
product evidence, not formal qualification. Do not begin C or infer READY from
this completed Slice B implementation boundary.

### C — Boot and serial bootstrap composition

Connect the retained Tart launch to the `serialx` pump and
`guestproto` helper. Compose exclusive bootstrap, exact host-key/public trust
validation, atomic absent-or-exact guest publication, and permanent drain.
The one-line decoder and pump exist from A; C connects them to a real VM.
Bootstrap errors keep the generation non-ready and preserve an exact cleanup
or retry path without adopting stale serial state.

Acceptance: deterministic composition/failure cases plus one controlled
real-VM bootstrap check using a corrected generic guest image. Record public,
redacted binding outcomes only. No generic console or host integration.

### D — Strict SSH management and READY

Pin the serial-observed key, create the narrow generation Ed25519 client key,
issue the short no-extension certificate, resolve a fresh literal guest address,
and invoke the fixed management helper over strict SSH. Apply/read back the
trusted host's validated current IANA zone. Publish READY only after all live
predicates hold. The supervisor owns CA revalidation, certificate renewal
(15-minute lifetime, renew at five minutes remaining), and the read-only probe
every 30 seconds. Evidence older than 90 seconds is non-ready.

Acceptance: deterministic no-TOFU/ordering/expiry/zone/error tests and a
controlled strict SSH management probe through READY. An IP address is a
locator, never identity. Status must not mint, probe, renew, apply, or repair.

### E — Exact stop and explicit-loss destroy

Persist stopping/deleting under the operation lock. Stop only the exact current
generation through typed control and the supervisor's retained Tart handle;
wait/reap once, retain ownership until termination, and observe backend stopped
before clearing generation. Timeout leaves non-ready durable intent; no PID
fallback, signal escalation, process search, or ambiguous cleanup.

Destroy refuses without `--allow-data-loss` before mutation. With it, perform
exact owned stop if needed, delete only the record's backend object, observe
absence, then remove only exact bound runtime/pin/session state. Retry never
broadens the target.

Acceptance: deterministic cancellation/idempotence/wrong-generation/loss-gate
tests and bounded controlled stop/destroy checks on the exact disposable object.

### F — CLI, read-only status, and output

Complete `internal/app` and `cmd/boxwarden` wiring across all lifecycle actions.
Status combines record, backend, exact supervisor snapshot, and current host
zone, preserving read-only behavior. Report READY only from live complete
evidence; distinguish STOPPED, STARTING, STOPPING, DELETING, DRIFT, and NON_READY.
Bound diagnostics and avoid guest-controlled terminal sequences, private
material, or misleading successful output on partial failure.

Acceptance: CLI parsing/output/exit-code tests and controlled status observation
at the available lifecycle boundaries. Earlier B–E slices wire their own
minimum action surface; F completes consistent output and read-only reporting.

### G — Deterministic integration validation

Exercise create → start → READY → status → stop → destroy with real local
components and fake external VM/SSH boundaries. Cover interruption, exact retry,
stale health, corrupted/foreign namespace, partial bootstrap/trust conflict,
stop timeout, and destroy refusal without mutation. Keep tests behavioral.

Run the CI `verify` equivalent: tracked shell syntax, gofmt, `go test ./...`,
`go test -race ./...`, `go vet ./...`, `go build ./cmd/boxwarden`, and
`git diff --check`. Use targeted cross-builds where platform code changes.
No live Tart/Softnet/VM/provider/host qualification runs in hosted CI.

### H — Controlled-host qualification and release evidence

After green deterministic verification, repeat the full controlled lifecycle
against one exact disposable VM and corrected generic guest artifact. Reuse
the early B–F product checks, record redacted outcomes and cleanup, and resolve
composition failures with focused verified corrections.

Product development checks are not formal qualification. Track separately the
attended V2 artifact/clone gate, ADR 024 Softnet runtime gate, and amended
ADR 017 one-PTY qualification. Do not transfer old Screen recovery-console
evidence to the new implementation or claim unsupported IPv6 environments.
No hostile workload, intentional compromise, escape testing, credential-store
probing, real secrets, or hostile/raw network fuzzing is authorized.

Update the single Draft PR with cumulative scope, exact verification/evidence,
remaining gates, and loss-override limitations. Review the cumulative diff
against its actual base and require fresh green CI before Ready. Formal
release/qualification claims require their own completed evidence; no merge or
auto-merge by an agent.

## Checkpoint measurement

At the Slice A stop point report exact HEAD, production Go physical LOC before
and after simplification (excluding tests and isolated qualification code),
deleted/materially simplified files, and exact text additions/deletions versus
`7d58540bc7cbda4e48bdaed5dff8309b7cf45b15`. Record deterministic test/race/vet/
build results, guest-security preservation, remaining estimates, and any
architecture blocker. Do not start B until the human inspects this checkpoint.
