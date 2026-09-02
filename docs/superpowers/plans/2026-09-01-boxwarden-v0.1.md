# Boxwarden v0.1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` for inline execution as a single lead. Steps use checkbox (`- [ ]`) syntax for tracking. Do not run parallel state-changing work against Git, Tart, networking, credentials, or the shared repository.

**Goal:** Deliver the reduced, production-quality Boxwarden v0.1 lifecycle manager for one qualified local Tart workstation, including practical per-provider work authentication, while first shipping and reviewing only the non-mutating `session status` vertical slice.

**Architecture:** The Go control plane owns security-domain resolution, trusted-host state, lifecycle intent, reconciliation, locks, destructive safety, and provider policy. The Tart backend owns only exact VM mechanics and observations; it never decides common policy. Provider support is a closed set of named mechanisms selected per provider and domain—never a generic CONNECT proxy, TLS interceptor, or ambient credential bridge.

**Tech Stack:** Go 1.27 standard library; strict JSON; `os/exec` argv execution; Tart 2.32.1 and Softnet 0.19.0 only where Task 0 qualified them; OpenSSH; provider-supported CLIs/SDKs.

**Spec:** `docs/architecture.md`, `docs/security-model.md`, `docs/state-model.md`, `docs/lifecycle-and-recovery.md`, ADRs 001–023, Task 0 evidence, and the 2026-09-01 approved production mandate. `docs/reviews/2026-09-01-v0.1-scope-reduction.md` is superseded where this plan requires practical provider authentication.

## Global constraints

- Task 0 is complete: **PASS_WITH_CONDITIONS**. Do not reopen qualification; report its three unqualified IPv6-related environment rows accurately.
- Each command requires an explicit domain supplied by `--domain` or `BOXWARDEN_DOMAIN`; there is no default domain or cross-domain fallback.
- Common packages must not import `internal/backend/tart`; a composition boundary supplies the Tart observer/lifecycle implementation.
- Use the Go standard library unless a concrete added dependency is documented and reviewed. Host commands use `exec.CommandContext` with exact argv, a bounded context, and bounded output; never use a shell.
- All persistent host paths are canonicalized and checked for symlinks before use. Domain roots must be distinct, private, and non-overlapping. Status creates no directories or files.
- Every lifecycle mutation records and fsyncs intent under a per-session lock before its backend call, then reconciles from backend observation. A raw PID is never a durable identity.
- V1 is read-only: it may read configuration, session records, and `tart list --format json`; it must not modify Tart, Softnet, session state, credentials, or host configuration.
- Normal M1A launches preserve Task 0’s Softnet shared/NAT, no audio, no clipboard, serial Screen-held relay, no host Docker/display/forwarding/port/bridged/nested integration, and ADR 015’s exact-only private CIDR exception policy.
- ADR 021's read-only host-tree capability is a proposed future design, not a V1-accepted exception. V1 neither accepts, implements, nor relies on it, and V14 implementation is blocked until ADR 021 receives formal acceptance through its own decision process. If later accepted, it must never be ambient or writable: by default a session has no host filesystem, a user must explicitly configure each capability for its exact session, and it remains unavailable to quarantine by default. Guest-local OverlayFS proposal/promotion remains outside V0.1 until separately designed.
- Quarantine receives neither normal profile restoration nor reusable provider/Git credentials. A human can still log in through the guest GUI; status and documentation must say so.
- Automated tests use fake backends, fake provider endpoints, and fixtures containing no real credentials. Each provider task separately includes a user-attended real-account gate using test data and revocable/temporary authority.
- Never inspect or copy a host credential store during V1. Provider tasks access a secret only through the later approved, named provider mechanism and never log a secret value.

## Target file structure

```text
cmd/boxwarden/                       command entrypoint and composition
internal/app/                        CLI orchestration and human output
internal/auth/                       closed provider capability registry and revocation inventory
internal/backend/                    neutral lifecycle interfaces and fake backend
internal/backend/tart/               defensive Tart argv/observation adapter
internal/config/                     strict configuration and domain-root admission
internal/domain/                     domain/session identifier validation
internal/execx/                      bounded argv-only command runner
internal/golden/                     registered stopped golden records
internal/lifecycle/                  intended/observed reconciliation and transitions
internal/lock/                       domain/session operation locks
internal/project/                    registered Git durability guard
internal/serialx/                    private PTY relay and Screen supervisor
internal/session/                    versioned session records and registry
internal/sshx/                       per-domain CA, pinning, and bounded SSH launch
internal/validate/                   host and session validation
internal/providers/{aws,gcp,github,bitbucket,jira,claude}/
                                      provider-specific contracts and implementations
config/boxwarden.example.json        non-secret configuration example
docs/operations/                     operator and provider runbooks
```

---

### Task V1: Read-only session-status vertical slice

**Files:**
- Create: `go.mod`, `.gitignore`, `README.md`, `config/boxwarden.example.json`
- Create: `cmd/boxwarden/main.go`
- Create: `internal/domain/id.go`, `internal/domain/id_test.go`
- Create: `internal/config/config.go`, `internal/config/config_test.go`
- Create: `internal/session/name.go`, `internal/session/name_test.go`, `internal/session/record.go`, `internal/session/record_test.go`, `internal/session/registry.go`, `internal/session/registry_test.go`
- Create: `internal/backend/observation.go`, `internal/backend/fake/fake.go`, `internal/backend/fake/fake_test.go`
- Create: `internal/backend/tart/observe.go`, `internal/backend/tart/observe_test.go`, `internal/backend/tart/parse.go`, `internal/backend/tart/parse_test.go`
- Create: `internal/execx/runner.go`, `internal/execx/runner_test.go`
- Create: `internal/lifecycle/reconcile.go`, `internal/lifecycle/reconcile_test.go`
- Create: `internal/app/app.go`, `internal/app/app_test.go`, `internal/app/architecture_test.go`

**Interfaces:**

```go
// internal/backend/observation.go
type ObjectState string
const (ObjectRunning ObjectState = "running"; ObjectStopped ObjectState = "stopped"; ObjectUnknown ObjectState = "unknown")
type Observation struct { ObjectID string; Exists bool; State ObjectState; Diagnostic string }
type Observer interface { Observe(context.Context, string) (Observation, error) }

// internal/lifecycle/reconcile.go
type Status string
const (StatusConsistent Status = "consistent"; StatusDrift Status = "drift"; StatusUnknown Status = "unknown")
func Reconcile(intended session.IntendedState, observed backend.Observation) (Status, string)
```

- [ ] **Step 1: Write the domain/config failure tests before implementation.** Cover missing domain, `BOXWARDEN_DOMAIN` fallback only when the flag is absent, invalid or unknown IDs, duplicate IDs, relative/symlinked roots, overlapping roots, and resolution of `work` versus `personal` to distinct roots.

- [ ] **Step 2: Run the focused domain/config tests and verify they fail.**

```bash
go test ./internal/domain ./internal/config
```

- [ ] **Step 3: Implement strict configuration and path admission.** Define version `1` JSON with a `domains` object keyed by lowercase ASCII domain IDs and an absolute `state_root` per domain. Reject unknown fields with `json.Decoder.DisallowUnknownFields`, roots that do not already exist, and any symlink in the root path. The command may default only its non-secret config *location*; it never defaults a domain.

- [ ] **Step 4: Write then run failing session-record tests.** Test valid version-1 records, cross-domain record rejection, unknown JSON fields, missing required backend identity, unsupported intended state, missing record, safe names, traversal, option-shaped names, control bytes, overlength, symlinked `sessions/`, and attempted `work` lookup of a `personal` record.

```go
type Record struct {
    Version        int           `json:"version"`
    Domain         string        `json:"domain"`
    Name           string        `json:"name"`
    ID             string        `json:"id"`
    Mode           Mode          `json:"mode"`
    IntendedState  IntendedState `json:"intended_state"`
    Backend        BackendRef    `json:"backend"`
    GoldenRevision string        `json:"golden_revision,omitempty"`
}
```

- [ ] **Step 5: Implement read-only registry parsing.** Permit only `clean` and `quarantine` modes and `creating`, `stopped`, `starting`, `running`, `stopping`, `deleting`, or `failed` intended states. Resolve `<state_root>/sessions/<validated-name>.json` without creating it; use `Lstat` for every existing component and reject symlinks.

- [ ] **Step 6: Write failing runner/backend/reconciliation tests.** Test fake consistent, missing, drift, and ambiguous observations; Tart valid records, empty listing, missing object, duplicate name, malformed JSON, wrong JSON types, unrecognized `State`, command failure, and deadline. Assert exact argv `tart list --format json`, no `sh`/`sh -c`, and output truncation.

- [ ] **Step 7: Implement the minimal neutral observation seam and Tart parser.** `tart.Observer` invokes only `tart list --format json`; parses the qualified object fields `Name`, `Running`, and `State`; requires `Running` and accepts only a matching `State`/boolean pair. More than one matching name and every unfamiliar schema/state return an actionable error rather than an arbitrary observation.

- [ ] **Step 8: Write failing CLI behavior tests, then implement composition and output.** Accept:

```text
boxwarden [--config PATH] --domain DOMAIN session status NAME
```

Print domain, session, mode, intended state, observed state, golden revision when recorded, consistency (`consistent`, `drift`, or `unknown`), and a bounded diagnostic. Do not print state-root, credential, or runtime paths. Status observes once and never calls a mutation interface.

- [ ] **Step 9: Add the import-boundary test and execute the V1 verification set.** The test walks Go imports and fails when any common package imports `internal/backend/tart`. Test command spies must prove no mutating Tart subcommand is invoked.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/boxwarden
git diff --check
```

- [ ] **Step 10: Commit in focused, reviewable boundaries.**

```bash
git add go.mod .gitignore README.md config cmd/boxwarden internal/domain internal/config internal/session
git commit -m "feat: add strict domain-scoped session state"
git add internal/backend internal/execx
git commit -m "feat: add read-only Tart observation adapter"
git add internal/lifecycle internal/app
git commit -m "feat: add session status reconciliation command"
```

**Gate:** Stop for human review after V1. Do not begin V2, create a VM, or access credentials in the V1 pass.

### Task V2: Register an existing golden and create a stopped session

**Files:**
- Create: `internal/golden/record.go`, `internal/golden/record_test.go`, `internal/golden/register.go`, `internal/golden/register_test.go`
- Create: `internal/lock/filelock.go`, `internal/lock/filelock_test.go`
- Modify: `internal/backend/observation.go`, `internal/backend/fake/fake.go`, `internal/backend/tart/observe.go`
- Create: `internal/session/create.go`, `internal/session/create_test.go`, `internal/session/store.go`, `internal/session/store_test.go`
- Modify: `internal/app/app.go`, `internal/app/app_test.go`

**Interfaces:**

```go
type Creator interface {
    Clone(context.Context, sourceID, targetID string) error
    RandomizeMAC(context.Context, objectID string) error
}
func Register(ctx context.Context, domain config.Domain, tartName string, observer backend.Observer) (golden.Record, error)
func (s *Service) Create(ctx context.Context, name string, mode session.Mode) (session.Record, error)
```

- [ ] **Step 1: Write failing golden and lock tests.** Reject running/missing/ambiguous goldens, invalid object IDs, cross-domain golden record reuse, state/lock symlinks, and lock contention. Require a stopped observed object and a domain-owned record.
- [ ] **Step 2: Implement atomic record storage and locks.** Write only through a same-directory temporary file with `O_EXCL`, `fsync`, `rename`, then directory `fsync`; use owner-only modes. A session lock name derives from its validated domain/name, never a raw path.
- [ ] **Step 3: Write failing create fault tests.** Cover failure before clone, after clone, after MAC randomization, duplicate retry, collision with an unrecorded object, and process interruption after intent is persisted.
- [ ] **Step 4: Implement `session create`.** Resolve one registered stopped golden under its lock; reserve a UUID-derived, validated backend object ID; persist `creating`; invoke exact `tart clone` and `tart set <id> --random-mac`; re-observe; persist `stopped` only on a stopped clone. Failures retain the exact intent for later reconciliation and never create a second clone on retry.
- [ ] **Step 5: Run tests and commit.**

```bash
go test -race ./internal/golden ./internal/lock ./internal/session ./internal/backend/...
git add internal/golden internal/lock internal/session internal/backend internal/app
git commit -m "feat: create stopped sessions from registered goldens"
```

**Gate:** User-attended test only after unit/fault tests pass: register an already-qualified, stopped non-production golden and create one disposable stopped clone. Record no private identifiers in Git.

### Task V3: Start and supervise the qualified running lifecycle

**Files:**
- Create: `internal/serialx/relay.go`, `internal/serialx/relay_test.go`, `internal/serialx/screen.go`, `internal/serialx/screen_test.go`
- Create: `internal/backend/tart/launch.go`, `internal/backend/tart/launch_test.go`, `internal/backend/tart/supervisor.go`, `internal/backend/tart/supervisor_test.go`
- Create: `internal/lifecycle/start.go`, `internal/lifecycle/start_test.go`, `internal/timezonex/host.go`, `internal/timezonex/host_test.go`
- Modify: `internal/session/record.go`, `internal/app/app.go`, `internal/app/app_test.go`

**Interfaces:**

```go
type StartRequest struct { ObjectID, RuntimeDir, SerialPath string; PrivateCIDRs []netip.Prefix }
type Starter interface { Start(context.Context, StartRequest) (backend.Observation, error) }
type Session struct { Name, OperatorPTY, TartPTY, ScreenName string }
func DetectHostZone() (string, error)
type GuestZoneSetter interface { ApplyAndVerify(context.Context, session.Record, string) error }
```

- [ ] **Step 1: Write failing launch-policy and relay-lifetime tests.** Assert the exact qualified launch has Softnet, no audio, no clipboard, a generated `--serial-path`, and no share/disk/Rosetta/VNC/bridge/host/expose/nested/allow-all option. Test runtime `0700`, exact PTYs `0600`, detached Screen ownership, attach/detach retention, cleanup on Tart exit, and rejection of arbitrary CIDRs.
- [ ] **Step 2: Implement `serialx` and Tart launch construction with argv only.** The supervisor stores process identity sufficient to correlate the Tart backend object and its owner-owned runtime metadata; it does not consider a reusable PID proof of a running session.
- [ ] **Step 3: Write start/reconciliation/time-zone failures before code.** Cover start intent crash points, already-running manual state, uncertain process identity, unsupported IPv6 environment disclosure, invalid host zone, guest apply failure, and guest readback mismatch.
- [ ] **Step 4: Implement `session start`.** Persist `starting`, build only the Task-0-qualified policy, launch the retained serial relay and Tart supervisor, observe the exact object, detect the host IANA zone with no fallback, apply/read back the zone through the bounded SSH management path, then persist `running`. Any failure remains actionable/non-ready.
- [ ] **Step 5: Verify and commit.**

```bash
go test -race ./internal/serialx ./internal/backend/tart ./internal/lifecycle ./internal/timezonex
git add internal/serialx internal/backend/tart internal/lifecycle internal/timezonex internal/session internal/app
git commit -m "feat: start sessions with qualified serial supervision"
```

**Gate:** User-attended M1A launch test: assert serial recovery, GUI readiness, Task-0 launch-policy properties, zone convergence, and clean supervisor cleanup. Any host instability is a hard stop.

### Task V4: Domain SSH CA, pinning, and safe management access

**Files:**
- Create: `internal/sshx/ca.go`, `internal/sshx/ca_test.go`, `internal/sshx/pin.go`, `internal/sshx/pin_test.go`, `internal/sshx/connect.go`, `internal/sshx/connect_test.go`
- Modify: `internal/backend/observation.go`, `internal/backend/tart/observe.go`, `internal/session/record.go`, `internal/app/app.go`
- Create: `docs/operations/ssh-management.md`

**Interfaces:**

```go
func InitCA(domain config.Domain) (PublicKey, error)
func PinFromSerial(ctx context.Context, record session.Record, relay serialx.Session) error
func BuildSSHArgs(record session.Record, command []string) ([]string, error)
```

- [ ] **Step 1: Write tests for CA path isolation, expiry, wrong domain, changed key, stale DHCP lease, multiple addresses, and user-provided forwarding/options.**
- [ ] **Step 2: Implement per-domain `0600` CA creation outside every repository/profile/backend root, short-lived session/principal certificates, serial-framed key extraction, and domain/session known-hosts pinning.**
- [ ] **Step 3: Implement `session ssh` with fresh `tart ip --resolver=dhcp` resolution immediately before connection and retry-on-safe-refresh.** It must pass `ForwardAgent=no`, `ForwardX11=no`, `ClearAllForwardings=yes`, `PermitLocalCommand=no`, pinned known-hosts, and no arbitrary SSH options.
- [ ] **Step 4: Run unit tests, then perform a user-attended certificate/pinning test against one disposable session.**

```bash
go test -race ./internal/sshx ./internal/backend/tart
git add internal/sshx internal/backend internal/session internal/app docs/operations/ssh-management.md
git commit -m "feat: add pinned domain-scoped session ssh"
```

### Task V5: Stop with intended-state reconciliation

**Files:**
- Create: `internal/lifecycle/stop.go`, `internal/lifecycle/stop_test.go`
- Modify: `internal/backend/observation.go`, `internal/backend/fake/fake.go`, `internal/backend/tart/launch.go`, `internal/session/store.go`, `internal/app/app.go`

**Interfaces:**

```go
type Stopper interface { Stop(context.Context, string) error }
func (s *Service) Stop(ctx context.Context, name string) (session.Record, error)
```

- [ ] **Step 1: Write tests for already-stopped idempotence, timeout, Tart failure, loss of supervisor, interrupted writes, lock contention, manual stop, and stale runtime cleanup.**
- [ ] **Step 2: Implement `session stop`: persist `stopping`, revoke running management capability only as needed by the session policy, request a bounded Tart stop, observe, clean only recorded serial runtime, and persist `stopped`.** Unknown observation never reports successful stop.
- [ ] **Step 3: Run focused/race tests and commit.**

```bash
go test -race ./internal/lifecycle ./internal/backend/... ./internal/session
git add internal/lifecycle internal/backend internal/session internal/app
git commit -m "feat: reconcile stopped sessions"
```

### Task V6: Safe normal and compromised destruction with a minimal Git guard

**Files:**
- Create: `internal/project/model.go`, `internal/project/model_test.go`, `internal/project/git.go`, `internal/project/git_test.go`
- Create: `internal/lifecycle/destroy.go`, `internal/lifecycle/destroy_test.go`
- Create: `internal/session/policy.go`, `internal/session/policy_test.go`
- Modify: `internal/backend/observation.go`, `internal/backend/fake/fake.go`, `internal/backend/tart/launch.go`, `internal/app/app.go`
- Create: `docs/operations/destroy-and-recovery.md`

**Interfaces:**

```go
type ProjectCheck struct { Dirty, Untracked, Ahead, Upstream, RemoteVerified bool; Diagnostic string }
type Deleter interface { Delete(context.Context, string) error }
func (s *Service) Destroy(ctx context.Context, name string, compromised, allowDataLoss bool) error
```

- [ ] **Step 1: Write normal-destroy blocker tests.** Cover unregistered work, dirty/untracked files, unpushed commits, absent upstream, required but unavailable corroboration, invalid acknowledgement, and backend object mismatch.
- [ ] **Step 2: Write compromised-destroy tests.** Prove it makes no guest SSH/Git/profile/credential call, only targets the exact recorded backend ID, handles a missing/dead guest, and emits credential revocation guidance without secrets.
- [ ] **Step 3: Implement clean/quarantine policies and the minimal Git inspection contract.** Normal destruction labels guest-reported versus host-corroborated facts and fails closed; `--allow-data-loss` requires the exact domain/session acknowledgement. Quarantine rejects normal credential/profile capability requests.
- [ ] **Step 4: Implement destroy.** Persist `deleting`; normal flow establishes the configured loss guard before deletion, while compromised flow skips guest trust entirely; both re-observe then remove only the recorded state/runtime once Tart reports absent.
- [ ] **Step 5: Verify and commit.**

```bash
go test -race ./internal/project ./internal/lifecycle ./internal/session ./internal/backend/...
git add internal/project internal/lifecycle internal/session internal/backend internal/app docs/operations/destroy-and-recovery.md
git commit -m "feat: add guarded and compromised session destruction"
```

**Gate:** User-attended destructive matrix against disposable test data: normal blocker, deliberate acknowledged loss, compromised deletion of an unreachable guest, and recreation from the registered golden.

### Task V7: Work-auth foundation and provider capability inventory

**Files:**
- Create: `internal/auth/provider.go`, `internal/auth/provider_test.go`, `internal/auth/inventory.go`, `internal/auth/inventory_test.go`
- Create: `internal/providers/contracts/contracts.go`, `internal/providers/contracts/contracts_test.go`
- Modify: `internal/config/config.go`, `internal/session/record.go`, `internal/app/app.go`, `internal/app/app_test.go`
- Create: `docs/operations/provider-auth.md`, `docs/operations/provider-test-matrix.md`

**Interfaces:**

```go
type Provider string
const (AWS Provider = "aws"; GCP = "gcp"; GitHub = "github"; Bitbucket = "bitbucket"; Jira = "jira"; Claude = "claude")
type Capability struct { Provider Provider; Mechanism string; Scope string; ExpiresAt time.Time }
type Adapter interface { Validate(context.Context, session.Record) error; Revoke(context.Context, session.Record) error; Status(context.Context, session.Record) (Capability, error) }
```

- [ ] **Step 1: Write tests that reject unknown providers, cross-domain provider references, duplicate conflicting provider grants, provider capability in quarantine, secrets in config/state/output, and generic arbitrary-upstream URLs.**
- [ ] **Step 2: Implement a closed provider registry.** It records only provider identity, mechanism, scope label, expiry, and non-secret domain reference ID. It has no universal credential proxy, no generic request relay, and no secret value type.
- [ ] **Step 3: Add status blast-radius output and revocation inventory.** Report provider identity/mechanism/expiry and warnings, never credential values, paths, authorization headers, cookies, or token hashes.
- [ ] **Step 4: Write the provider matrix with fake-contract and user-attended-real sections for every provider.** The Bitbucket and Jira rows must require a deployment selection before their implementation tasks begin.
- [ ] **Step 5: Run tests and commit.**

```bash
go test -race ./internal/auth ./internal/providers/contracts ./internal/config ./internal/session
git add internal/auth internal/providers/contracts internal/config internal/session internal/app docs/operations/provider-auth.md docs/operations/provider-test-matrix.md
git commit -m "feat: add domain-scoped provider capability contracts"
```

### Task V8: AWS CLI and SDK authentication

**Files:**
- Create: `internal/providers/aws/adapter.go`, `internal/providers/aws/adapter_test.go`, `internal/providers/aws/credential_process.go`, `internal/providers/aws/credential_process_test.go`
- Create: `cmd/boxwarden-aws-credential/main.go`
- Modify: `internal/auth/provider.go`, `docs/operations/provider-auth.md`, `docs/operations/provider-test-matrix.md`

**Interfaces:**

```go
func (a Adapter) WriteCredentialProcessConfig(session.Record) ([]byte, error)
type Credentials struct { AccessKeyID, SecretAccessKey, SessionToken string; ExpiresAt time.Time }
func (a Adapter) MintSTS(context.Context, session.Record) (Credentials, error)
```

- [ ] **Step 1: Write fake AWS contract tests.** Require role/session-policy scope, short expiry, exact domain/session binding, malformed response rejection, timeout, redaction, revocation, and no long-lived access key in guest configuration.
- [ ] **Step 2: Implement a named AWS credential-process path.** It uses host-side IAM Identity Center/workforce authority where available to mint STS/AssumeRole credentials and writes guest-side AWS config that invokes only the bounded per-session helper. It returns temporary AWS credentials only over the authenticated session path; no host SSH agent or generic broker is involved.
- [ ] **Step 3: Run fake tests and commit.**

```bash
go test -race ./internal/providers/aws ./internal/auth
git add internal/providers/aws cmd/boxwarden-aws-credential internal/auth docs/operations
git commit -m "feat: add scoped AWS credential-process support"
```

- [ ] **Step 4: User-attended real gate.** In an isolated work domain with a revocable test role, run `aws sts get-caller-identity` and one SDK call using the default credential chain; verify expiration/scope and revoke or let the test session expire. Store only redacted evidence.

### Task V9: Google Cloud CLI and ADC authentication

**Files:**
- Create: `internal/providers/gcp/adapter.go`, `internal/providers/gcp/adapter_test.go`, `internal/providers/gcp/external_account.go`, `internal/providers/gcp/external_account_test.go`
- Create: `cmd/boxwarden-gcp-token/main.go`
- Modify: `internal/auth/provider.go`, `docs/operations/provider-auth.md`, `docs/operations/provider-test-matrix.md`

**Interfaces:**

```go
func (a Adapter) WriteADC(session.Record) ([]byte, error)
type Token struct { AccessToken string; ExpiresAt time.Time }
func (a Adapter) MintImpersonatedToken(context.Context, session.Record) (Token, error)
```

- [ ] **Step 1: Write fake ADC/external-account contract tests.** Cover allowed audience/service account, short expiration, no JSON private key, domain/session mismatch, executable failure, token redaction, and cancellation.
- [ ] **Step 2: Implement the provider-specific external-account/impersonation path.** Prefer workforce/workload identity federation or host-side service-account impersonation; create guest ADC configuration for a bounded session helper rather than copying persistent service-account JSON keys.
- [ ] **Step 3: Run fake tests and commit.**

```bash
go test -race ./internal/providers/gcp ./internal/auth
git add internal/providers/gcp cmd/boxwarden-gcp-token internal/auth docs/operations
git commit -m "feat: add scoped GCP ADC support"
```

- [ ] **Step 4: User-attended real gate.** In a test project, perform one authenticated `gcloud` read and one client-library operation using ADC; preserve only command success, principal class, and redacted expiry evidence.

### Task V10: GitHub HTTPS, Git, and API access

**Files:**
- Create: `internal/providers/github/adapter.go`, `internal/providers/github/adapter_test.go`, `internal/providers/github/git.go`, `internal/providers/github/git_test.go`
- Create: `cmd/boxwarden-github-credential/main.go`
- Modify: `internal/auth/provider.go`, `docs/operations/provider-auth.md`, `docs/operations/provider-test-matrix.md`

**Interfaces:**

```go
func (a Adapter) InstallationToken(context.Context, session.Record, repository string) (token, error)
func (a Adapter) GitCredentialHelper(session.Record) []string
```

- [ ] **Step 1: Write fake GitHub App/OAuth contract tests.** Cover repository/permission restriction, short expiry, Git credential-helper protocol parsing, no SSH agent, no token logging, revocation, and denied push scope.
- [ ] **Step 2: Implement HTTPS credential helper and `gh` capability setup.** Prefer GitHub App installation tokens; use an official OAuth/device flow only where an App does not fit. A guest-held bearer is explicitly temporary and narrowly scoped, not non-exfiltratable.
- [ ] **Step 3: Run fake tests and commit.**

```bash
go test -race ./internal/providers/github ./internal/auth
git add internal/providers/github cmd/boxwarden-github-credential internal/auth docs/operations
git commit -m "feat: add scoped GitHub HTTPS authentication"
```

- [ ] **Step 4: User-attended real gate.** Against a test private repository, clone, fetch, make and push a test-only commit, and call `gh api user`; remove the test branch and revoke/expire the credential.

### Task V11: Bitbucket deployment-specific work authentication

**Files:**
- Create: `internal/providers/bitbucket/adapter.go`, `internal/providers/bitbucket/adapter_test.go`
- Modify: `internal/config/config.go`, `internal/auth/provider.go`, `docs/operations/provider-auth.md`, `docs/operations/provider-test-matrix.md`

**Interfaces:**

```go
type Deployment string
const (Cloud Deployment = "cloud"; DataCenter Deployment = "data_center")
func (a Adapter) ValidateDeployment(config.ProviderConfig) error
```

- [ ] **Step 1: Obtain the required deployment decision from the operator: Bitbucket Cloud, Bitbucket Data Center, or both.** Record the selected target in non-secret domain configuration and reject omitted/unknown deployment values. Do not choose based on URL shape.
- [ ] **Step 2: Write fake contract tests for the selected deployment.** Test private clone/fetch/push/API authorization, exact base authority, scope/expiry, cross-domain denial, no SSH agent, and no durable credential in session records.
- [ ] **Step 3: Implement only the selected provider-supported HTTPS/OAuth/App-token mechanism.** A second deployment is a separate reviewed implementation path, not a fallback.
- [ ] **Step 4: Run fake tests, commit, then perform the matching user-attended private test-repository clone/fetch/push/API gate using test data.**

```bash
go test -race ./internal/providers/bitbucket ./internal/auth
git add internal/providers/bitbucket internal/config internal/auth docs/operations
git commit -m "feat: add Bitbucket work authentication"
```

### Task V12: Jira/Atlassian deployment-specific work authentication

**Files:**
- Create: `internal/providers/jira/adapter.go`, `internal/providers/jira/adapter_test.go`
- Modify: `internal/config/config.go`, `internal/auth/provider.go`, `docs/operations/provider-auth.md`, `docs/operations/provider-test-matrix.md`

**Interfaces:**

```go
type Deployment string
const (Cloud Deployment = "cloud"; DataCenter Deployment = "data_center")
func (a Adapter) TestOperations(context.Context, session.Record) error
```

- [ ] **Step 1: Obtain the required deployment decision from the operator: Jira Cloud, Jira Data Center, or both.** Persist a non-secret target identifier and reject an absent deployment selection.
- [ ] **Step 2: Write fake contract tests for account discovery, project/issue read, JQL query, controlled create/update, scope rejection, authority pinning, redaction, and quarantine denial.**
- [ ] **Step 3: Implement only official support appropriate to the selected deployment.** The code must never convert a Cloud token/configuration into a Data Center fallback or vice versa.
- [ ] **Step 4: Run fake tests, commit, then execute the user-attended operations on designated test data and delete or restore the test artifact.**

```bash
go test -race ./internal/providers/jira ./internal/auth
git add internal/providers/jira internal/config internal/auth docs/operations
git commit -m "feat: add Jira work authentication"
```

### Task V13: Claude Teams guest-local official authentication

**Files:**
- Create: `internal/providers/claude/adapter.go`, `internal/providers/claude/adapter_test.go`
- Modify: `internal/auth/provider.go`, `internal/session/policy.go`, `docs/operations/provider-auth.md`, `docs/operations/provider-test-matrix.md`

**Interfaces:**

```go
func (a Adapter) Status(context.Context, session.Record) (auth.Capability, error)
func (a Adapter) Revoke(context.Context, session.Record) error
```

- [ ] **Step 1: Write fake policy tests.** Require clean-session-only capability registration, no host credential-store discovery, no claim that a broker protects desktop login state, explicit quarantine rejection, and redacted status.
- [ ] **Step 2: Implement the policy and operator guidance only.** Claude Code/Desktop authenticate through their official interactive guest-local flow. Do not reverse engineer or proxy Anthropic desktop authentication.
- [ ] **Step 3: Run tests and commit.**

```bash
go test -race ./internal/providers/claude ./internal/auth ./internal/session
git add internal/providers/claude internal/auth internal/session docs/operations
git commit -m "feat: document Claude Teams session authentication"
```

- [ ] **Step 4: User-attended real gate.** Complete an official Claude Teams login in a clean disposable guest, verify Claude Code and required Desktop access, then destroy the test session. No cookie/profile/token extraction is permitted.

### Task V14: Proposed explicit read-only host-tree capability

**Formal acceptance gate:** This task is planning only. Do not begin V14
implementation until ADR 021 has been formally accepted through its own
decision process. This V1 PR neither changes ADR 021's status nor authorizes a
host-filesystem exception.

**Files:**
- Create: `internal/hosttree/capability.go`, `internal/hosttree/capability_test.go`, `internal/hosttree/admit.go`, `internal/hosttree/admit_test.go`
- Modify: `internal/session/record.go`, `internal/lifecycle/start.go`, `internal/lifecycle/reconcile.go`, `internal/backend/observation.go`, `internal/backend/tart/launch.go`, `internal/backend/tart/launch_test.go`, `internal/validate/session.go`
- Create: `docs/operations/read-only-host-trees.md`

**Interfaces:**

```go
type Capability struct { ID, Domain, SessionID, HostSource, GuestDestination string; Access string }
func Admit(domain config.Domain, record session.Record, requested Capability) (Capability, error)
```

- [ ] **Step 1: Write the host-root/path matrix before implementation.** Reject the trusted-host root and `<operator-home>` runtime, golden, session, identity, keychain, browser, and credential roots; also reject symlinks, missing/non-directory sources, overlaps, destination collisions, cross-domain ownership, raw Tart `--dir`, and every writable access request.
- [ ] **Step 2: Implement exact canonical admission and session persistence.** The only accepted access value is `read_only_live`; an explicit user session configuration is required and omission means no host tree. Capability IDs and Tart mount tags derive from recorded identities, not input strings. Do not implement B/G/H, OverlayFS upper/work handling, materialization, or promotion.
- [ ] **Step 3: Add qualified Tart mapping and observational status.** Exactly map recorded capability intent to a read-only Tart attachment, retain all Task-0 launch properties, revalidate on start, and report disclosure/status without guessing cleanup targets.
- [ ] **Step 4: Run fake/root/path tests and commit.**

```bash
go test -race ./internal/hosttree ./internal/lifecycle ./internal/backend/tart ./internal/validate
git add internal/hosttree internal/session internal/lifecycle internal/backend internal/validate docs/operations/read-only-host-trees.md
git commit -m "feat: add explicit read-only host-tree capability"
```

- [ ] **Step 5: User-attended empirical gate.** Re-run the full root/path/destructive matrix on the exact qualified Tart/macOS pair: user/root data and metadata writes, remount, sibling/parent, symlink, replacement, nested source, start/restart, status, stop/destroy, and no host instability. Any related host panic or corruption is a hard stop. OverlayFS proposal/promotion remains unimplemented.

### Task V15: Release validation, traceability, and V0.1 handoff

**Files:**
- Create: `internal/validate/host.go`, `internal/validate/host_test.go`, `internal/validate/session.go`, `internal/validate/session_test.go`, `internal/validate/properties.go`, `internal/validate/properties_test.go`
- Modify: `README.md`, `AGENTS.md`, `docs/architecture.md`, `docs/security-model.md`, `docs/lifecycle-and-recovery.md`, `docs/credentials.md`
- Create: `docs/evidence/v0.1-final-report.md`, `docs/operations/v0.1-acceptance.md`

**Interfaces:**

```go
type HostValidator interface { ValidateHost(context.Context, config.Domain) ([]Finding, error) }
type SessionValidator interface { ValidateSession(context.Context, session.Record) ([]Finding, error) }
```

- [ ] **Step 1: Write host/session validation tests for qualified toolchain identity, domain isolation, golden state, serial dependencies, Softnet privilege binding, unsupported IPv6 rows, ADR 015 gateway limitation, provider capability inventory, quarantine, host-tree capability admission, and forbidden integrations.**
- [ ] **Step 2: Implement `validate host` and `validate session` as read-only diagnostics.** They must distinguish observed facts, inferred facts, and unqualified environments; they must not mutate a VM or mint credentials.
- [ ] **Step 3: Run the full automated verification twice.**

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/boxwarden
git diff --check
```

- [ ] **Step 4: Execute the user-attended acceptance matrix.** One clean lifecycle/restart/SSH, one quarantine rejection, one normal destroy blocker, one compromised destroy/recreate, provider gates V8–V13, read-only host-tree gate V14, and report the three intentionally unqualified IPv6 environment rows. Redact identifiers and never commit a real secret or private path.
- [ ] **Step 5: Trace every accepted invariant to code and positive/negative evidence, update final documentation, commit, and stop for release approval.**

```bash
git add README.md AGENTS.md docs internal/validate
git commit -m "docs: record Boxwarden v0.1 acceptance evidence"
git status --short
```

## Explicit deferrals after V0.1

- Automated golden building/promotion and a broad third-party provenance lock.
- Profile capture/restore, age-encrypted candidate admission, and Kindex persistence.
- Generic host filesystem promotion, writable VirtioFS, OverlayFS proposal materialization, and all B/G/H review operations.
- A generalized reusable-secret proxy, transparent TLS interception, CONNECT proxying, host SSH-agent forwarding, and host credential-store discovery.
- Additional backends, checkpoints, cloud hosting, guest-agent bridges, host display/filesystem/Docker/forwarding integrations, and automatic updates.
