# Boxwarden MVP Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Execute serially on the single `weshofmann/feat/mvp-lifecycle` worktree; do not create sub-branches, stacked PRs, or modify PR #3/#4.

**Goal:** Deliver one controlled, domain-scoped M1A workflow: `session create` → `session start` → concrete `READY` → reconciled `session status` → exact owned `session stop` → `session destroy --allow-data-loss`.

**Architecture:** Retain V2 creation and landed V3 host/domain foundation. Add the narrow backend launch/address/delete seam; a detached same-user supervisor is the only owner of Tart, the two-PTY serial broker, Screen, and runtime SSH credentials. A session service persists intent before every mutation; serial establishes binding and host-key pin before typed strict-SSH probe and time-zone convergence may produce READY. Status observes/challenges only.

**Tech Stack:** Go (standard library first), Tart/Softnet on macOS, system GNU Screen, strict OpenSSH, `os.Root` state handling, deterministic fake backend, and a committed static Linux/arm64 guest-helper artifact.

**Spec:** `/Users/wes/.codex/attachments/520988fe-e463-46dd-91d8-432c60568f4f/pasted-text.txt`; `docs/architecture.md`, `docs/state-model.md`, `docs/lifecycle-and-recovery.md`, `docs/operations/ssh-management.md`, ADRs 012/017/019, and `AGENTS.md`.

## Global Constraints

- Work from base `7d58540bc7cbda4e48bdaed5dff8309b7cf45b15` in the one branch/worktree: `weshofmann/feat/mvp-lifecycle` at `/private/tmp/boxwarden-mvp-lifecycle`.
- The preserved refs `0045e205`, `2e711d617`, `75763d58`, and `6ca97e782` are read-only source material. Manually port cohesive code only; do not cherry-pick their branch-wide diffs or restore qualification machinery.
- Keep the backend seam small. Common code owns domains, locks, state, readiness, credentials/pins, project-loss gate, and destructive policy; Tart owns VM mechanics only.
- Start uses the admitted absolute Tart path, canonical configured Tart home, generation-private `TMPDIR`, and a closed environment. Its complete environment is `PATH=<qualified-softnet-digest-dir>`, `HOME=<admitted-home>`, `USER=<admitted-user>`, `LOGNAME=<admitted-user>`, `TART_HOME=<configured-home>`, `TMPDIR=<generation-dir>`, `LANG=C`, and `LC_ALL=C`. No shell, sudo, ambient proxy/telemetry/loader variables, clipboard, audio, share, bridge, host networking, port publication, or Softnet allow flag.
- Serial is host-local: a `0700` generation directory; two exact `0600` PTY slaves; Tart sees only its slave; `/usr/bin/screen -D -m -S <session>` is a direct owned child holding the operator slave. Never use a serial network service, socat, or Screen control/data paths for automation.
- Bootstrap is serial-first and canonical. The generic golden receives only selected-domain public CA, durable binding, and derived principal. Generation/nonce are runtime correlation only. Pin fresh guest Ed25519 host key before issuing a certificate; no TOFU and no network bootstrap.
- READY requires current authenticated supervisor ownership, exact running backend, healthy broker/Screen, exact pin, fresh literal address, current no-extension certificate, strict typed probe, and exact host/guest IANA-zone agreement. A record bit or Tart-running alone is non-ready.
- Stop/destroy never trust a PID alone: each mutable action proves control challenge, manifest, process-start evidence, current generation, exact binding, and backend object. Unproven runtime is drift/no mutation.
- Until project registry exists, destroy requires `--allow-data-loss`; without it, fail before state, runtime, or backend mutation. Never delete a golden or cross-domain object.
- Controlled disposable guest development is not V4/hostile-guest qualification. Do not claim qualification.

## Preflight, archaeology, and dependency map

| Source | Classification | Current/WIP target | Decision |
| --- | --- | --- | --- |
| `0045e205` / `2e711d617`: `internal/backend/start.go`, `internal/backend/tart/{launch,address}.go`, `internal/timezonex/*` | ADAPT | new current-tree backend/tart/timezonex files | Preserve narrow contracts, exact argv/closed env, literal address, and zone convergence; recompose with current V3 admission. |
| Same refs: fake start/address/owned handle | ADAPT | `internal/backend/fake/*` | Required deterministic lifecycle testing; preserve current V2 clone semantics. |
| `75763d58`: guest helper, `internal/guestproto/*`, artifact/autoinstall | ADAPT | same new current-tree paths | Required immutable generic helper and binding protocol; regenerate artifact, never copy preserved binary. |
| `6ca97e782`: `internal/serialx/*` | ADAPT | `internal/serialx/*` + Darwin PTY + supervisor | Retain bounded broker/wire model; checkpoint lacks native PTY and direct-child lifecycle. |
| Branch-wide docs, qualification observer, historical evidence, stale V4 app/session edits | DROP | none | Not MVP product work; would overwrite landed V2/V3 policy. |
| Console attach UI, project registry, profile/credential injection, private CIDR, checkpoints | DEFER | none | Valid later scope; not required for MVP lifecycle. Certificate renewal and the 30-second health probe remain required because READY evidence must not silently expire during an ordinary session. |

| Gate | Command/evidence | Requirement |
| --- | --- | --- |
| Baseline | `git fetch origin`; `git rev-parse origin/main`; `git status --short --branch` | Remote main is exact base; one clean MVP worktree. |
| Source refs | `git show -s --format='%H %s' 0045e205 2e711d617 75763d58 6ca97e782` | Exact archaeology refs above, read only. |
| Baseline deterministic suite | `go test ./...`; `go test -race ./...`; `go vet ./...`; `go build ./cmd/boxwarden`; `bash guest/ubuntu-24.04-arm64/tests/bootstrap.sh` | Green before Task 1. |
| V3 boundary | test `hostx.Report.Status == hostx.Healthy` and selected `sshx.CAStore.Check` | Start does not lazy-run host/domain initialization. |
| Integration order | Tasks 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 | One implementation writer; reviews may overlap only with read-only preparation. The controller pushes each verified commit after its task review. |

## Tasks

### Task 1: Backend start/address/stop/delete contracts

**Files:**
- Create: `internal/backend/{start,delete}.go`, `internal/backend/{start,delete}_test.go`.
- Create: `internal/backend/tart/{launch,address,delete,launch_test,address_test,delete_test,process_group_darwin,process_group_other}.go`.
- Modify: `internal/backend/fake/{fake,fake_test}.go`.

**Interfaces:**

```go
type StartRequest struct { ObjectID, SerialDevice, GenerationDirectory string }
type Handle interface { Stop(context.Context) error; Wait(context.Context) error }
type Starter interface { Start(context.Context, StartRequest) (Handle, error) }
type AddressResolver interface { Resolve(context.Context, objectID string) (string, error) }
type Deleter interface { Delete(context.Context, objectID string) error }
type LaunchConfig struct {
    TartPath, TartHome, SoftnetBinDir, OperatorHome, OperatorName string
    ProcessStarter ProcessStarter
}
```

`ValidateStartRequest` admits a valid object ID plus canonical absolute non-root serial/generation paths. `tart.Launcher.Start` builds exactly:

```go
[]string{"run", "--net-softnet", "--no-audio", "--no-clipboard",
  "--serial-path", request.SerialDevice, request.ObjectID}
```

The direct child has a new process group. `Handle.Stop` sends graceful termination only to that exact group; `Wait` reaps it. `Resolve` uses bounded `tart ip --resolver=dhcp --wait=60 <object>` and returns exactly one literal IP. `Delete` executes only `tart delete <object>`. Launch, address, and delete use the configured absolute Tart path and canonical `TART_HOME`; none may fall back to ambient PATH or environment.

- [ ] **RED:** Add `TestValidateStartRequestRejectsNonCanonicalPaths`, `TestLauncherUsesClosedQualifiedTartInvocation`, `TestLauncherRejectsAmbientOrUnqualifiedConfiguration`, `TestAddressResolverReturnsOneLiteralIP`, `TestOwnedHandleStopsOnlyItsProcessGroup`, and `TestDeleteUsesOneValidatedObjectID`. Use a recording process starter and assert full argv and full environment equality.
- [ ] **Run RED:** `go test ./internal/backend ./internal/backend/tart -run 'Test(ValidateStartRequest|Launcher|AddressResolver|OwnedHandle|Delete)' -count=1`. Expected: missing start/delete types.
- [ ] **GREEN:** Manually adapt preserved narrow code, deriving launch facts from current V3-admitted host facts, not old host-init code. Extend fake with recorded Start/Resolve/Delete and owned handles.
- [ ] **Verify:** `go test ./internal/backend/... -count=1 && go test ./internal/architecture -count=1 && gofmt -w internal/backend`.
- [ ] **Commit:** `git add docs/superpowers/plans/2026-09-04-boxwarden-mvp-lifecycle.md internal/backend && git commit -m "feat(backend): add exact owned Tart lifecycle seams" -m "Add closed-policy start, fresh address lookup, graceful owned stop, and exact deletion without widening the control-plane backend contract. Record the single-branch MVP execution plan derived from preserved V4 archaeology."`
- [ ] **Controller publish checkpoint after clean task review:** `git push -u origin weshofmann/feat/mvp-lifecycle`, then create the single Draft PR to `main`. Later verified task commits use `git push`; implementer subagents never push.

### Task 2: Generic guest helper and canonical serial protocol

**Files:**
- Create: `internal/guestproto/{protocol,bootstrap,rename_noreplace_linux,rename_noreplace_other}.go` and tests.
- Create: `cmd/boxwarden-guest-bootstrap/{main,artifact_test}.go`.
- Create: `guest/ubuntu-24.04-arm64/artifacts.lock.json`, `guest/ubuntu-24.04-arm64/artifacts/boxwarden-guest-bootstrap`.
- Modify: `guest/ubuntu-24.04-arm64/autoinstall/user-data`, `guest/ubuntu-24.04-arm64/tests/bootstrap.sh`.
- Modify: `scripts/spike/bootstrap-tart.sh` to map only the verified current-tree helper into the remastered ISO staging tree.

**Interfaces:**

```go
type Association struct { Domain, SessionID, BackendKind, BackendObject string }
type SerialRequest struct { Version int; Nonce, StartGeneration string; Association; CAPublicKey, CAFingerprint, Principal string }
type SerialResult struct { Version int; StartGeneration string; Association; CAFingerprint, Principal string; InstalledSHA256, SSHD map[string]string; HostPublicKey string }
func DecodeSerialRequest(io.Reader) (SerialRequest, error)
func DecodeManagementRequest(io.Reader) (ManagementRequest, error)
func EncodeSerialFrame(SerialRequest, SerialResult) (begin, end string, err error)
```

The helper exposes only `serial-bootstrap` and `management`. Canonical bounded JSON is carried in `BOXWARDEN-BEGIN <nonce> <session-id>` / `BOXWARDEN-END <nonce> <session-id> <base64-json>` frames. It atomically publishes only `/etc/ssh/boxwarden/active` with CA public key, derived principal, and durable binding; exact repeat is idempotent, conflict fails. It checks effective sshd and returns fresh Ed25519 public host key. Management accepts exactly existing typed `probe`, `apply_zone`, `read_zone`.

- [ ] **RED:** `TestSerialRequestRejectsUnknownOrMismatchedFields`, `TestEncodeSerialFrameRoundTripsExactGenerationAndNonce`, `TestSerialFrameIsUnambiguousWithPTYCRLF`, `TestSerialBootstrapPublishesOnlyDurableBinding`, `TestSerialBootstrapRejectsUnexpectedActiveEntries`, `TestSerialBootstrapIsIdempotentAndRejectsConflictingBinding`, `TestManagementRejectsRemoteCommandSurface`, `TestMainPropagatesOutputFailure`, `TestCommittedGuestHelperMatchesLockAndStaticARM64Contract`, and an executable remaster-mapping test proving the locked helper is the installed input.
- [ ] **Run RED:** `go test ./internal/guestproto ./cmd/boxwarden-guest-bootstrap -count=1`. Expected: missing packages.
- [ ] **GREEN:** Adapt `c9a178e` plus required UUID correction `75763d58`, never the integer-generation state or preserved binary. Build twice with exactly `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags=-buildid=`; compare bytes; update committed artifact, SHA-256 lock, remaster mapping, and autoinstall checksum/install to root-owned `0755` `/usr/local/libexec/boxwarden-guest-bootstrap`. Every helper write is checked; exact active-tree validation rejects unexpected entries; PTY CRLF is normalized only at the frame-line boundary and never inside decoded JSON.
- [ ] **Verify:** `go test ./internal/guestproto ./cmd/boxwarden-guest-bootstrap -count=1 && bash guest/ubuntu-24.04-arm64/tests/bootstrap.sh`.
- [ ] **Commit:** `git add docs/superpowers/plans/2026-09-04-boxwarden-mvp-lifecycle.md internal/guestproto cmd/boxwarden-guest-bootstrap guest/ubuntu-24.04-arm64 scripts/spike/bootstrap-tart.sh && git commit -m "feat(guest): add generic serial bootstrap helper" -m "Install a reproducible static arm64 helper that atomically binds one clone to selected public trust without a network bootstrap path."`

### Task 3: Bounded two-PTY, Screen, and serial exchange

**Files:**
- Create: `internal/serialx/{broker,exchange,lease,screen,clock,pty_darwin,pty_other,runtime}.go` and tests.
- Modify: `internal/hostx/{doctor,doctor_test}.go` only to expose/check the already-admitted exact Screen fact.

**Interfaces:**

```go
type Runtime struct { TartSlave, OperatorSlave string; TartMaster, OperatorMaster *os.File; Screen ScreenChild }
func CreateRuntime(context.Context, Root, Generation, ScreenBinary, ScreenStarter) (Runtime, error)
type BrokerConfig struct { Tart, Screen io.Writer; Generation string; Clock Clock }
func NewBroker(BrokerConfig) *Broker
func (b *Broker) Exchange(context.Context, ExchangeRequest) (json.RawMessage, error)
func (b *Broker) AcquireConsole(context.Context) (Lease, error)
```

Create one private generation directory, two Darwin PTY pairs, exact private endpoint links, and direct `/usr/bin/screen -D -m -S <derived-name>` with operator slave stdin. Supervisor-owned readers pass Tart-master output to broker and operator-master input only to `OperatorInput`. The broker has exactly idle/console/automation/failed states; fixed queue, line, frame, aggregate and deadline bounds; operator data outside console is counted/discarded; overflow, interleaving, timeout, or child loss poisons the generation.

- [ ] **RED:** `TestCreateRuntimeRejectsExistingOrUnsafeGenerationPath`, `TestCreateRuntimeUsesTwoOwnerOnlyPTYSlavesAndFixedScreenSpec`, `TestBrokerDiscardsOperatorInputOutsideConsole`, `TestExchangeAcceptsOnlyOneCanonicalAssociatedFrame`, `TestExchangePoisonsOnOverflowInterleavingAndTimeout`, `TestConsoleEOFDoesNotCloseScreenOrTartEndpoint`. Fakes cover platform-independent behavior; Darwin allocation gets a Darwin-tagged integration test.
- [ ] **Run RED:** `go test ./internal/serialx -count=1`. Expected: missing package.
- [ ] **GREEN:** Adapt `6ca97e782`, implement missing Darwin allocator and exact cleanup. No socat, generic broker protocol, or Screen hardcopy/log/stuff/paste/control.
- [ ] **Verify:** `go test ./internal/serialx -count=1 && go test -race ./internal/serialx -count=1 && go test ./internal/hostx -run 'TestDoctor.*Screen' -count=1`.
- [ ] **Commit:** `git add internal/serialx internal/hostx && git commit -m "feat(serial): add bounded owned PTY and Screen transport" -m "Port canonical bootstrap framing into a two-PTY runtime whose fixed bounds and direct-child ownership fail closed on ambiguity."`

### Task 4: Detached authenticated supervisor

**Files:**
- Create: `internal/supervisor/{supervisor,control,manifest,runtime,process_darwin,process_other}.go` and tests.
- Modify: `cmd/boxwarden/main.go`, `internal/backend/fake/{fake,fake_test}.go`.

**Interfaces:**

```go
type Binding struct { Domain, SessionID, BackendKind, BackendObject, Generation string }
type Snapshot struct { Binding Binding; BackendRunning, BrokerHealthy, ScreenHealthy, PinPresent, CertificateCurrent, ProbeOK, ZoneMatches bool; ObservedAt time.Time; Diagnostic string }
type Controller interface { Snapshot(context.Context, Binding) (Snapshot, error); Stop(context.Context, Binding) error }
type LaunchRequest struct { Binding Binding; RuntimeDirectory, HostConfigPath string }
type Launcher interface { Launch(context.Context, LaunchRequest) error }
```

Start writes an owner-only immutable supervisor request in its fresh runtime directory, starts fixed `boxwarden internal session-supervisor <request-path>`, and waits only for authenticated starting/ready response. Supervisor does not exec-replace: it owns Tart handle, PTYs, broker, Screen, readers, client key/cert, and a `0600` Unix socket. Its manifest records binding, an owner-private random control key, supervisor PID plus Darwin process-start evidence, endpoint file identities, direct child evidence, and poison state. Each request uses fresh challenge + HMAC over canonical bytes; client validates socket/manifest ownership, process-start evidence, and binding. PID reuse, stale socket, wrong key, missing child, stale snapshot = no ownership.

- [ ] **RED:** `TestSupervisorLaunchPersistsNoBarePIDOwnership`, `TestControlRejectsWrongBindingChallengeOrMAC`, `TestControllerRejectsPIDReuseAndStaleManifest`, `TestSupervisorReapsDirectChildrenOnBackendExit`, `TestSnapshotIsBoundedAndCannotReportReadyAfterBrokerPoison`.
- [ ] **Run RED:** `go test ./internal/supervisor -count=1`. Expected: missing package.
- [ ] **GREEN:** Implement fixed internal parser plus injected process/clock/socket seams. On exit/poison, close socket/readers, stop/reap only exact handle, remove only proven runtime tree; never scan or signal by process name/PID.
- [ ] **Verify:** `go test ./internal/supervisor -count=1 && go test -race ./internal/supervisor -count=1 && go test ./cmd/boxwarden -count=1`.
- [ ] **Commit:** `git add internal/supervisor internal/backend/fake cmd/boxwarden && git commit -m "feat(supervisor): own lifecycle runtime through authenticated control" -m "Make mutable running-session actions prove recorded generation and direct-child ownership rather than trusting a bare PID."`

### Task 5: Start through serial trust, pin, strict SSH, and zone convergence

**Files:**
- Create: `internal/timezonex/{host,guest}.go` and tests; `internal/session/{start,runtime}.go` and tests; `internal/hostx/runtime_admission.go` and tests.
- Modify: `internal/session/{record,record_test,store}.go`; `internal/sshx/{ca,cert,pin,client}.go` only to expose existing typed operations to supervisor; `internal/app/{app,app_test}.go`; `cmd/boxwarden/{main,main_test}.go`.

**Interfaces:**

```go
type RuntimeAdmission struct { Manifest hostx.Manifest; ScreenPath, ScreenSHA256, ScreenVersion, SoftnetBinDir string }
type RuntimeChecker interface { CheckRuntime(context.Context, hostx.Request) (RuntimeAdmission, error) }
type StartDependencies struct { Observer backend.Observer; Host RuntimeChecker; CA CAValidator; Supervisor supervisor.Launcher; RuntimeRoot string; NewGeneration func() (string, error) }
func (s *Service) Start(context.Context, string) (Record, error)
func DetectHost() (string, error)
func Converge(context.Context, ZoneClient, sshx.Connection, string) error
```

Start acquires the per-session lock, validates only selected-domain V3 host/CA prerequisites, loads exact record/object, persists `StateStarting` + fresh UUID generation + `ReadinessStarting` before launch. `CheckRuntime` reuses doctor-equivalent inspection and returns the exact validated manifest/Screen/Softnet facts rather than duplicating path trust in session code. Those facts are serialized into the owner-private supervisor request and revalidated by the child before Tart execution. Supervisor exchanges exact serial request/result, validates nonce/generation/association/CA/principal/sshd/host key, admits pin, resolves fresh literal address, issues runtime cert, probes strict SSH, applies and reads back `DetectHost()`. Only a fresh healthy snapshot fsyncs `StateRunning` / `ReadinessReady`; failures remain non-ready or settle stopped only after proven owned shutdown and observation.

The supervisor revalidates immutable CA metadata, renews the 15-minute no-extension certificate when five minutes remain, and runs a typed read-only probe every 30 seconds. It publishes only a bounded authenticated health snapshot. Evidence older than 90 seconds, an expired certificate, failed probe/challenge, poisoned broker, missing child, or zone mismatch is non-ready. Status consumes this evidence but never renews, probes, or repairs.

- [ ] **RED:** `TestStartPersistsGenerationBeforeBackendMutation`, `TestStartRejectsUnhealthyHostOrMissingSelectedDomainCA`, `TestStartDoesNotAdoptAlreadyRunningObject`, `TestStartPinsSerialHostKeyBeforeCertificateAndSSH`, `TestStartRequiresExactZoneReadbackBeforeReady`, `TestStartRetryResumesOnlyExactLiveGeneration`, `TestStartFailureCleansOnlyProvenOwnedRuntime`.
- [ ] **Run RED:** `go test ./internal/session ./internal/timezonex -run 'Test(Start|DetectHost|Converge)' -count=1`.
- [ ] **GREEN:** Adapt preserved backend/timezone/guestproto code. Preserve record compatibility: starting/running require generation; stopped clears it. No IP in record/pin; no generic SSH command, lazy init, credential/profile injection, or repair.
- [ ] **Verify:** `go test ./internal/session ./internal/timezonex ./internal/sshx -count=1 && go test -race ./internal/session ./internal/timezonex -count=1`.
- [ ] **Commit:** `git add internal/session internal/timezonex internal/sshx internal/hostx internal/app cmd/boxwarden && git commit -m "feat(session): start owned sessions through serial readiness" -m "Persist a start generation before launch and require admitted host facts, serial trust, exact host-key pinning, strict SSH, and time-zone convergence before READY."`

### Task 6: Read-only reconciled status

**Files:**
- Create: `internal/session/{status,status_test}.go`.
- Modify: `internal/lifecycle/{reconcile,reconcile_test}.go`, `internal/app/{app,app_test}.go`, `cmd/boxwarden/main_test.go`.

**Interfaces:**

```go
type Status struct { Record Record; Backend backend.Observation; Consistency lifecycle.Reconciliation; Snapshot supervisor.Snapshot; Ready bool; Diagnostic string }
func (s *Service) Status(context.Context, string) (Status, error)
```

Stopped is true only with observed stopped. Running requires backend observation, host-zone detection, and fresh authenticated supervisor snapshot with all READY predicates. Otherwise print STARTING/STOPPING/DELETING/DRIFT/NON_READY. No status operation may issue certs, apply zone, bootstrap, stop, or mutate persisted state.

- [ ] **RED:** `TestStatusReportsReadyOnlyForFreshAuthenticatedSnapshot`, `TestStatusDoesNotInferReadyFromRunningBackendOrRecord`, `TestStatusReportsDriftForUnprovenRuntime`, `TestStatusReportsZoneMismatchWithoutRepair`, `TestStatusIsReadOnly`.
- [ ] **Run RED:** `go test ./internal/session ./internal/lifecycle ./internal/app -run 'TestStatus' -count=1`.
- [ ] **GREEN:** Compose `session status <name>`; retain existing output fields and append `lifecycle: READY|STOPPED|STARTING|STOPPING|DELETING|DRIFT|NON_READY` plus bounded diagnostic.
- [ ] **Verify/commit:** `go test ./internal/session ./internal/lifecycle ./internal/app ./cmd/boxwarden -count=1 && git add internal/session internal/lifecycle internal/app cmd/boxwarden && git commit -m "feat(session): report reconciled lifecycle readiness" -m "Expose READY only from fresh authenticated supervisor evidence while preserving read-only status and visible drift."`.

### Task 7: Exact graceful stop

**Files:**
- Create: `internal/session/{stop,stop_test}.go`.
- Modify: `internal/session/{record,record_test}.go`, `internal/app/{app,app_test}.go`, `cmd/boxwarden/main_test.go`.

**Interface:**

```go
func (s *Service) Stop(context.Context, string) (Record, error)
```

Stop locks session, proves record/backend/supervisor binding, fsyncs `StateStopping` plus non-ready before authenticated control. Supervisor sends SIGINT only through the exact owned Tart handle and waits a fixed bound for that handle to reap. A wait timeout returns without SIGKILL, PID fallback, or other escalation; the record remains `StateStopping` and non-ready for later reconciliation. Service observes the exact object stopped before fsyncing `StateStopped` and clearing generation. Unproven runtime is drift/no signal.

- [ ] **RED:** `TestStopPersistsStoppingBeforeControlRequest`, `TestStopRefusesBarePIDOrMismatchedGeneration`, `TestStopSignalsOnlyOwnedBackendGroup`, `TestStopWaitsForStoppedObservationBeforeClearingGeneration`, `TestStopTimeoutLeavesStoppingNonReadyWithoutEscalation`, `TestStopIsIdempotentForExactStoppedRecord`, `TestStopLeavesDriftUntouched`.
- [ ] **Run RED:** `go test ./internal/session ./internal/app -run 'TestStop' -count=1`.
- [ ] **GREEN:** Add fixed `session stop <name>` parser and reuse Task 4 controller/Task 1 handle; SIGINT plus bounded handle `Wait` is the only shutdown action. No escalation, process search, orphan adoption, signal flag, or PID fallback.
- [ ] **Verify/commit:** `go test ./internal/session ./internal/app ./cmd/boxwarden -count=1 && go test -race ./internal/session ./internal/supervisor -count=1 && git add internal/session internal/app cmd/boxwarden && git commit -m "feat(session): stop only exact owned runtimes" -m "Require authenticated generation ownership before graceful bounded shutdown and settle stopped only after backend and runtime cleanup are observed."`.

### Task 8: Explicit-loss-gated exact destroy

**Files:**
- Create: `internal/session/{destroy,destroy_test}.go`.
- Modify: `internal/app/{app,app_test}.go`, `cmd/boxwarden/main_test.go`, `internal/session/{store,store_test}.go`, `internal/sshx/{pin,pin_test}.go`.

**Interfaces:**

```go
type DestroyOptions struct { AllowDataLoss bool }
func (s *Service) Destroy(context.Context, string, DestroyOptions) error
```

CLI is exactly `boxwarden --domain <D> session destroy --allow-data-loss <name>`. Absent/duplicate/unknown flags are parser errors. Without allow flag: no record write, supervisor control, backend mutation, or cleanup. With flag: lock, persist deleting, exact owned stop if needed, observe stopped, `Deleter.Delete(record.Backend.ObjectID)`, verify exact object absent, then remove only session runtime, the exact binding-bound pin through a new `PinStore.Remove`, and the exact record through a rooted `RemoveRecord` operation. Never resolve/delete golden or another session/domain.

- [ ] **RED:** `TestDestroyRequiresExplicitAllowDataLossBeforeMutation`, `TestDestroyStopsExactOwnedRunningSessionThenDeletesItsObject`, `TestDestroyRefusesUnprovenRunningOwnership`, `TestDestroyNeverDeletesGoldenOrOtherDomainObject`, `TestDestroyRetryDoesNotBroadenTarget`, `TestDestroyRemovesOnlyExactSessionStateAfterBackendAbsence`.
- [ ] **Run RED:** `go test ./internal/session ./internal/app -run 'TestDestroy' -count=1`.
- [ ] **GREEN:** Reuse exact Stop; add no speculative project registry. Retried deletion re-reads exact record/object association; no name/prefix resolution.
- [ ] **Verify/commit:** `go test ./internal/session ./internal/app ./cmd/boxwarden -count=1 && go test -race ./internal/session -count=1 && git add internal/session internal/sshx internal/app cmd/boxwarden && git commit -m "feat(session): destroy exact sessions with explicit loss override" -m "Keep deletion MVP-sized and fail closed on project durability until a registry can establish safer normal destroy."`.

### Task 9: End-to-end verification, controlled attempt, and Draft PR update

**Files:**
- Modify: `internal/app/app_test.go`.
- Modify only if implementation changes documented interface: `README.md`, `docs/lifecycle-and-recovery.md`.

- [ ] **RED/GREEN CLI test:** Add `TestMVPLifecycleCreateStartReadyStatusStopDestroy` with deterministic fake backend/supervisor/SSH facts. Assert create stopped; start ready; status emits `lifecycle: READY`; stop stopped; destroy without override errors without mutation; destroy with override removes only selected record/object.
- [ ] **Run deterministic release gate:**

```sh
gofmt -w cmd internal
git diff --check
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/boxwarden
bash guest/ubuntu-24.04-arm64/tests/bootstrap.sh
```

Expected: all green and no generated runtime/credential/private-host data in `git status`.

- [ ] **One controlled real-host lifecycle attempt only after green:** Preflight exact resolved argv/env, Tart home, domain/name/object, TMPDIR, serial modes/links, forbidden integrations, state transition, and cleanup association. With a known-good disposable golden and initialized selected domain:

```sh
boxwarden --config "$BW_CONFIG" doctor
boxwarden --config "$BW_CONFIG" --domain "$BW_DOMAIN" domain init
boxwarden --config "$BW_CONFIG" --domain "$BW_DOMAIN" session create mvpcheck
boxwarden --config "$BW_CONFIG" --domain "$BW_DOMAIN" session start mvpcheck
boxwarden --config "$BW_CONFIG" --domain "$BW_DOMAIN" session status mvpcheck
boxwarden --config "$BW_CONFIG" --domain "$BW_DOMAIN" session stop mvpcheck
boxwarden --config "$BW_CONFIG" --domain "$BW_DOMAIN" session destroy --allow-data-loss mvpcheck
```

Record only redacted product observations: exit status, lifecycle state, domain/session/backend object, and cleanup result. Stop if a hostile workload/adversarial probe, real credential, host share, or unproven destructive target would be required.

- [ ] **Commit:** `git add README.md docs/lifecycle-and-recovery.md internal/app/app_test.go && git commit -m "test(session): verify MVP lifecycle composition" -m "Demonstrate the controlled create-to-destroy path with deterministic fakes and document implemented product behavior, not qualification claims."`. Omit paths with no actual change.
- [ ] **Controller publish/update:** push the verified commit and update the single Draft PR with cumulative scope, deterministic results, redacted controlled result if run, `--allow-data-loss` limitation, no qualification claim, and deferred project registry/console-attach work. Before Ready: fetch actual base, `git diff --check`, review `git diff <actual-base>...HEAD`, wait for fresh green `verify`; never merge/auto-merge.

## Acceptance Checklist

- Deterministic test proves create → start → READY → status → stop → destroy and proves no mutation without `--allow-data-loss`.
- V2 create retains durable intent, exact reserved clone identity, randomized MAC, stopped result, and retry safety.
- Start enforces healthy host/selected-domain CA, admitted Tart/closed environment, serial-first binding/pin, strict SSH, and zone convergence.
- Status is read-only and refuses READY from Tart/record alone.
- Stop/delete require complete identity-bound ownership proof; no bare PID, process scan, or broad delete.
- Any controlled disposable run is product evidence only; V4 qualification remains explicitly unclaimed.
