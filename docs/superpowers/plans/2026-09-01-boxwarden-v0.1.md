# Boxwarden v0.1 V1-V4 Implementation Plan

> **Execution boundary:** V1 and V2 are completed historical slices. Implement
> V3 and then V4 with TDD and their independent review gates. Stop after V4.
> V5 and later are roadmap only and are not authorized by this plan.

**Goal:** Complete the smallest trustworthy path from a registered generic
golden to a READY M1A workstation: explicit host-global and domain-specific
initialization, strict management identity, qualified host-tool privilege,
supervised start, trusted post-clone serial bootstrap, pinned SSH, and time-zone
convergence.

**Architecture:** The common Go control plane owns domain/session identity,
generic-golden admission, intended state, locks, supervisor/readiness policy,
management CA/certificates, host-key pins, strict SSH, serial bootstrap protocol,
time-zone convergence, and failure semantics. The Tart adapter owns exact VM
mechanics and observation. A running backend is not READY.

**Qualified host toolchain:** Go 1.27 standard library; OpenSSH; Tart 2.32.1
executable SHA-256
`05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d`
(archive `8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529`);
Softnet 0.19.0 executable SHA-256
`ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e`
(archive `1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c`).

## Non-negotiable execution rules

- Require `--domain` or `BOXWARDEN_DOMAIN` for commands that operate on
  domain-owned state; never default or search across domains. Host-global
  `boxwarden init` and `boxwarden doctor` operate outside the security-domain
  namespace, require no domain, and reject an explicitly supplied `--domain`.
- Keep common packages independent of `internal/backend/tart`; only command
  composition imports the Tart adapter.
- Use exact argv with bounded contexts and bounded input/output. Never invoke a
  shell for host operations.
- Persist and fsync lifecycle intent under the session lock before backend
  mutation. Treat `(domain, session UUID, backend kind/object, intended state)`
  as durable identity.
- Treat PIDs, process start evidence, PTYs, Screen names, addresses,
  certificates, and a start-generation UUID as correlation/runtime state only.
- Never install start generation or nonce in guest durable state. The immutable
  guest binding is `(domain, session UUID, backend kind/object, CA fingerprint,
  exact derived principal)`; later generations verify the same binding.
- Never adopt an intended-running, observed-running VM whose current supervisor
  ownership/readiness cannot be proven. Report DRIFT/NON-READY and require an
  explicit stop/restart. Cleanup may act only on ownership it proves.
- Never use TOFU, `StrictHostKeyChecking=no`, a network bootstrap path, a host
  SSH agent, a host filesystem share, clipboard/audio sharing, forwarding, or
  provider credentials.
- Host-global `init` is explicit and may request attended administrator
  authorization. Host-global `doctor` is read-only and never repairs, installs,
  rotates, or re-authorizes. Domain `init` creates only domain-owned trust and
  never installs or modifies host-global prerequisites.
- Under ADR 024, any privileged Softnet under mutable Homebrew state is blocking
  `drifted/unsafe`; init/start refuse and Boxwarden never chmods it. Init refuses
  a source with any setuid/setgid bit. Only the exact staged copy is `04550`.
- V4 launches only the default Softnet policy and rejects every allow flag.
  ADR 015 opt-in requires future persisted record/status/CLI semantics.
- Deterministic tests use temporary roots, fake runners/backends/process tables,
  fake clocks, fake serial transports, and fixed fixtures. They never mutate
  `/Library`, directory services, Tart, Softnet, SSH service state, real VMs, or
  the actual host toolchain.
- Real-host install, Tart/Softnet, serial/Screen, SSH, and VM gates are
  user-attended and remain pending until honestly executed.

## State and command contract

V3/V4 introduce session record version 2 while continuing to read V1 records
for V2 stopped sessions. On the first V3/V4 write, preserve all V1 identity and
atomically upgrade to:

```go
type Record struct {
    Version         int                     `json:"version"`
    Domain          domain.ID               `json:"domain"`
    Name            session.Name            `json:"name"`
    ID              string                  `json:"id"`
    Mode            session.Mode            `json:"mode"`
    IntendedState   session.IntendedState   `json:"intended_state"`
    Backend         session.BackendRef       `json:"backend"`
    GoldenRevision  string                  `json:"golden_revision"`
    StartGeneration string                  `json:"start_generation,omitempty"`
    Readiness       session.ReadinessRecord `json:"readiness"`
}

type ReadinessRecord struct {
    Status     ReadinessStatus `json:"status"`     // not_ready, starting, ready, drift
    Diagnostic string          `json:"diagnostic"` // bounded, non-secret last result
}
```

`ReadinessRecord` is an audit/result hint, never proof of current readiness.
Status recomputes readiness from current observation and runtime evidence. A
start-generation is freshly generated and persisted with `starting`; it
correlates one supervisor instance but does not replace session identity.

Configuration version 2 adds one non-secret shared host block while retaining
the existing domain map:

```go
type HostConfig struct {
    TartExecutable string `json:"tart_executable"`
    SoftnetSource  string `json:"softnet_source"`
    TartHome       string `json:"tart_home"`
}
```

Both executable paths must be canonical absolute existing regular files without
symlinked components; `tart_home` is a canonical absolute private directory.
`softnet_source` is input to attended init, never the privileged runtime path.
V1/V2 read-only/status/create commands continue to accept existing
version-1 configuration. Host-global `init`/`doctor` and `session start` require
version 2 and fail with an upgrade diagnostic when the shared host block is
absent. Domain `init` resolves only the explicitly selected domain and its
domain-owned state; it does not require or initialize the shared host block.
Its domain-only configuration view still rejects malformed JSON, duplicate or
unknown host fields when a host object is present, and unsafe domain roots, but
deliberately defers host-path filesystem admission until a host-global command
needs it.
Host-global commands resolve only `HostConfig` and never call domain selection.
The operator group name is the fixed `boxwarden-operators`, not configurable
input.

Host-key pins are separate immutable versioned records under
`<state_root>/identity/ssh-host-pins/<session-uuid>.json`:

```go
type HostKeyPin struct {
    Version       int       `json:"version"`
    Domain        domain.ID `json:"domain"`
    SessionID     string    `json:"session_id"`
    BackendKind   string    `json:"backend_kind"`
    BackendObject string    `json:"backend_object"`
    Algorithm     string    `json:"algorithm"`
    PublicKey     string    `json:"public_key"`
    Fingerprint   string    `json:"fingerprint"`
}
```

They contain no IP. An exact repeat is idempotent; any different key or binding
fails closed and is never replaced implicitly.

The guest `active` binding is independent of generation:

```go
type GuestBinding struct {
    Version       int       `json:"version"`
    Domain        domain.ID `json:"domain"`
    SessionID     string    `json:"session_id"`
    BackendKind   string    `json:"backend_kind"`
    BackendObject string    `json:"backend_object"`
    CAFingerprint string    `json:"ca_fingerprint"`
    Principal     string    `json:"principal"`
}
```

CA metadata beside `ca` and `ca.pub` is immutable and versioned and contains
domain ID, Ed25519 algorithm, public key, public-key fingerprint/digest, a
unique creation UUID, and exact creating operator UID/name. Nonce and start generation appear only in current request/
response framing and host runtime metadata.

Commands added by this plan:

```text
boxwarden init
boxwarden doctor
boxwarden --domain DOMAIN domain init
boxwarden --domain DOMAIN session start NAME
boxwarden --domain DOMAIN session status NAME
boxwarden --domain DOMAIN session console NAME
```

`boxwarden init` establishes the shared host toolchain once per trusted Mac.
Re-running against fully matching host state reports healthy; missing partial
state, unsafe state, or conflicting state fails without repair. `boxwarden
doctor` reports `healthy`, `missing/uninitialized`, `drifted/unsafe`, or
`unsupported/unqualified` per host check and exits nonzero unless every
host-global start prerequisite is healthy. It neither selects a domain nor
invents domain-CA health semantics. `boxwarden --domain DOMAIN domain init`
creates or validates exactly one management CA for that domain without touching
the host toolchain. `session start` requires both scopes to exist and prints
success only after READY. `session console` is the supported human attach path
and uses the same exclusive serial lease as automation.

## Target package structure through V4

```text
cmd/boxwarden/                       command composition and hidden supervisor/root installer modes
cmd/boxwarden-guest-bootstrap/       static guest serial bootstrap helper
internal/app/                        public CLI parsing/output and orchestration
internal/backend/                    neutral observe/create/start contracts and deterministic fake
internal/backend/tart/               exact Tart observe/create/address/launch argv
internal/execx/                      bounded argv runner with bounded stdin
internal/golden/                     domain-scoped admission of generic stopped artifacts
internal/hostx/                      exact toolchain manifest, init/install, doctor, uninstall guard
internal/lifecycle/                  start transition and readiness reconciliation
internal/serialx/                    Darwin two-PTY broker, Screen child, framed exchange
internal/session/                    versioned records/store and start-generation state
internal/sshx/                       domain CA, cert issuer, host-key pins, strict client policy
internal/supervisor/                 runtime request/manifest, ownership proof, child protocol
internal/timezonex/                  host detection and guest apply/readback
guest/ubuntu-24.04-arm64/            generic sshd/bootstrap targets and helper installation
docs/operations/                     init/doctor, SSH, start/recovery runbooks
```

The common V3 contracts are deliberately small:

```go
type HostInitializer interface {
    Init(context.Context, config.HostConfig) (hostx.InitResult, error)
}
type HostDoctor interface {
    Check(context.Context, config.HostConfig) (hostx.Report, error)
}
type CAStore interface {
    Init(context.Context, config.Domain, []config.Domain) (sshx.CAIdentity, error)
    Load(context.Context, config.Domain) (sshx.CAIdentity, error)
}
type CertificateIssuer interface {
    Issue(context.Context, config.Domain, session.Record, string) (sshx.Certificate, error)
}
type PinStore interface {
    Admit(context.Context, config.Domain, session.Record, sshx.ObservedHostKey) (sshx.HostKeyPin, error)
    Load(context.Context, config.Domain, session.Record) (sshx.HostKeyPin, error)
}
type ManagementClient interface {
    Probe(context.Context, sshx.Connection, sshx.ProbeRequest) (sshx.ProbeResult, error)
    ApplyZone(context.Context, sshx.Connection, sshx.ApplyZoneRequest) error
    ReadZone(context.Context, sshx.Connection, sshx.ReadZoneRequest) (string, error)
}
```

The common V4/backend contracts pass typed policy, never arbitrary backend
flags:

```go
type Starter interface {
    Start(context.Context, backend.StartRequest) (backend.StartHandle, error)
}
type AddressResolver interface {
    Resolve(context.Context, string) (netip.Addr, error)
}
type RuntimeObserver interface {
    ObserveRuntime(context.Context, supervisor.Ownership) (supervisor.Observation, error)
}
type Bootstrapper interface {
    Apply(context.Context, serialx.Lease, serialx.BootstrapRequest) (serialx.BootstrapResult, error)
}
type GuestZoneManager interface {
    ApplyAndReadBack(context.Context, sshx.Connection, string) (string, error)
}
```

---

## V1 — read-only status (complete)

V1 implemented strict configuration/domain admission, versioned read-only
session records, bounded argv-only execution, defensive Tart observation,
intended/observed reconciliation, and `session status`. It performs no mutation.

- [x] Domain/config/session validation and symlink/path rejection.
- [x] Narrow backend observation seam and exact `tart list --format json`.
- [x] Read-only CLI/status and backend import-boundary test.

Do not reimplement V1. Its existing tests remain regression gates.

## V2 — generic golden registration and stopped clone (complete code; attended gate pending)

V2's domain-owned record is admission/selection metadata for a generic artifact.
`golden register` verifies only that the named backend object exists exactly once
and is stopped, then records exact identity and explicit operator admission. It
does not prove or store provenance, clone-readiness, CA state, or qualification
evidence. Multiple domains may independently record the same exact artifact.

`session create` resolves one admitted revision under the domain golden lock,
persists `creating`, clones, randomizes the MAC, re-observes the exact clone, and
persists `stopped`. It never boots the guest, converges time zone, binds a domain
CA, reads a host key, or reports READY.

- [x] `internal/golden`, locking, atomic immutable records/current pointer.
- [x] Intent-first clone/random-MAC reconciliation and collision/fault tests.
- [x] CLI composition for registration, create, and status.
- [ ] User-attended disposable real-host register/clone gate.

The V2 gate must use a stopped, non-production artifact built or rebuilt from
the corrected generic guest definition and externally qualified accordingly.
It contains no Boxwarden domain identity, domain CA anchor, or fixed domain
principal. An unchanged older Task 0 artifact built under the domain-bound
design is not grandfathered merely because it previously passed qualification.
The gate records no private identifiers. Its result may validate V2 mechanics
but must not inflate registration into a provenance claim.

---

## V3 — trusted host/domain management foundation

V3 has two explicit ownership scopes:

```text
trusted host
├── host-global foundation
│   ├── boxwarden init
│   ├── qualified Softnet privilege installation
│   └── boxwarden doctor
├── domain: work
│   └── work management CA
└── domain: personal
    └── personal management CA
```

The host-global foundation is established once for the Mac. Each explicit
`boxwarden --domain D domain init` adds only D's management CA; it never repeats
or re-authorizes the host toolchain.

### V3.1 Exact host-tool manifest and safe deterministic seams

**Files:**

- Create `internal/hostx/identity.go`, `manifest.go`, `manifest_test.go`
- Create `internal/hostx/filesystem.go`, `filesystem_test.go`
- Modify `internal/execx/runner.go`, `runner_test.go` for bounded stdin
- Modify `internal/config/config.go`, `config_test.go`
- Modify `config/boxwarden.example.json`

Define constants for the exact versions and four approved digests above. The
qualified macOS release is the Task 0 M1A `26.6.2` identity. The
root-owned manifest lives at
`/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/manifest.json`
beside `softnet`. It records schema version, platform, qualified macOS identity,
canonical absolute Tart path and its executable/archive identities, canonical
Softnet path and executable/archive identities, root UID, dedicated
`boxwarden-operators` group ID/name/membership, exact single trusted operator
UID/name/home, canonical `tart_home`, expected mode `04550`, and installation
timestamp. No `current` symlink exists.

- [ ] Write failing strict-manifest tests: duplicate/unknown/missing fields,
  wrong platform/version/digest/path/group/mode, oversized JSON, symlinks,
  ancestor replacement, ACL grant, group/other-writable ancestors, non-regular
  executable, link count other than one, wrong owner, malformed root manifest,
  operator/group/name/UID/home/membership mismatch, stale process supplementary
  groups, and path overlap with repository/domain/runtime roots.
- [ ] Add interfaces `FSInspector`, `GroupDB`, `ProcessInventory`, `PrivilegeRunner`,
  and `Clock`; production adapters wrap OS calls, tests use only fakes and temp
  roots. Add `execx.Command.Stdin []byte`, enforce its byte limit, and prove it
  never appears in errors or output.
- [ ] Implement canonical no-symlink traversal and exact manifest parsing. Keep
  policy in `hostx`; do not import Tart.

### V3.2 Host-global `init` and exact ADR 024 Softnet privilege installation

**Files:**

- Create `internal/hostx/init.go`, `init_test.go`
- Create `internal/hostx/install.go`, `install_test.go`
- Create `internal/hostx/uninstall.go`, `uninstall_test.go`
- Modify `internal/app/app.go`, `app_test.go`
- Modify `cmd/boxwarden/main.go`
- Create `docs/operations/init-and-doctor.md`

`boxwarden init` resolves the current executable and qualified source
paths, hashes before mutation, and sends a bounded versioned install request on
stdin to an exact `/usr/bin/sudo -- <absolute-boxwarden> internal host-install`
root phase. The hidden mode requires effective UID 0, ignores ambient PATH and
security-sensitive environment, reopens source files without following
symlinks, rejects every setuid/setgid source, revalidates type/link count/digest
after open, derives and verifies the actual sudo caller UID/name/home rather
than accepting an arbitrary username, and accepts only the
compiled qualified identities. Administrator credentials are handled only by
`sudo`; Boxwarden never stores or reads them.

The root phase creates/validates the dedicated `boxwarden-operators` group and
the single invoking trusted operator membership, creates every ancestor root-owned and
non-writable by group/other, copies Softnet to a sibling staging directory,
fsyncs it, sets root/group and `04550`, verifies digest/type/link/ACL/metadata,
renames the complete digest directory into place, fsyncs its parent, and
publishes the root-owned manifest last by atomic rename and parent fsync. It
never authorizes a Homebrew path. If the exact final tree already exists and is
fully valid, it is idempotent. Partial, unexpected, or mismatched final state
fails closed with manual remediation guidance; it is not overwritten. It removes
only the exact staging tree it created and still owns. New directory-service
membership does not imply membership in the initiating process tree: init
reports that a login-session refresh is required, and doctor/start fail until
the new supplementary group is effective.

Before privileged installation, init scans configured mutable Homebrew Softnet
locations. Any setuid/setgid or passwordless-root target is blocking unsafe
state requiring attended manual inspection/remediation. Boxwarden does not
chmod, delete, copy from, or otherwise repair it, even when its bytes match.

The uninstall primitive accepts the full Softnet digest, resolves exactly one
manifested digest directory, checks recorded and live supervisor consumers, and
refuses if any consumer is active or ownership cannot be proven. It never uses a
glob, version-only selector, broad `/Library/Boxwarden` target, or implicit
current pointer. Upgrade is a future explicit `init` of an adjacent qualified
digest and never mutates the existing tree.

- [ ] Write failures first for interrupted copy/fsync/rename/manifest publish,
  source replacement, altered post-copy digest, malicious request paths,
  source privilege bits, inherited environment, spoofed sudo caller,
  wrong caller UID/group/home, new membership without current-process effect,
  blocking Homebrew privilege, already-correct idempotence, unsafe preexisting
  directory, staging ownership/cleanup, active/unverifiable uninstall consumer, and
  exact inactive uninstall.
- [ ] Implement the root phase behind injected filesystem/group/process seams.
  Unit tests execute it only against a temporary synthetic root and fake group
  DB; they never invoke `sudo`, directory services, or `/Library`.
- [ ] Add CLI tests proving host init is explicit, attended, outside the domain
  namespace, rejects a supplied `--domain`, and is never reachable from
  `session start` or `doctor`.

### V3.3 Explicit domain init, one management CA per domain, and certificate issuance

**Files:**

- Create `internal/sshx/ca.go`, `ca_test.go`
- Create `internal/sshx/cert.go`, `cert_test.go`
- Create `internal/sshx/paths.go`, `paths_test.go`
- Modify `internal/app/app.go`, `app_test.go`
- Modify `cmd/boxwarden/main.go`
- Create `docs/operations/domain-init.md`

Store the CA at `<state_root>/identity/ssh-user-ca/ca` with owner-only directory
components and private key mode `0600`; `ca.pub` is a regular non-symlink public
file in the same private tree. Immutable `metadata.json` binds version, domain
ID, Ed25519 algorithm, public key/fingerprint/digest, unique creation UUID, and
exact creating operator UID/name.
Every load and issue validates key/public/metadata agreement. Domain init
receives the complete configured-domain set and compares public fingerprints across
their configured roots only to reject accidental reuse; it never uses that scan
for credential lookup or fallback. A copied CA tree fails its bound domain.
`boxwarden --domain D domain init` uses exact argv to the absolute system
`ssh-keygen` to create one Ed25519 CA only when the complete target is absent.
It validates key/public-key agreement and permissions. A complete matching CA
reports already initialized. Partial, malformed, unsafe, copied-across-domain,
or unexpected state fails; there is no lazy create, silent rotation, or repair.

The persistent supervisor creates and owns an ephemeral Ed25519 client key in
its private runtime generation directory and issues a certificate with identity
`boxwarden:<domain>:<session-uuid>`, sole principal
`boxwarden-session-<session-uuid>`, a five-minute negative validity offset for
clock skew, and fifteen-minute positive validity. Exact `ssh-keygen -O clear`
removes all certificate extensions. The issuer accepts no caller principals or
options and passes no private bytes through argv/logs. The supervisor revalidates
CA metadata before renewal and renews when five minutes remain. Certificates
are runtime state, not session identity.

- [ ] Test absent/correct/partial/unsafe CA state, wrong domain, symlink/hardlink,
  duplicate init, malformed key, key/public/metadata mismatch, copied domain,
  duplicate fingerprint across configured roots, no cross-domain selection,
  no private bytes in diagnostics, exact principal/identity/validity/`-O clear`
  argv, no extensions, renewal threshold, expiry, cancellation, bounded output,
  and cross-domain issuance denial.
- [ ] Implement `CAStore.Init` and `Issuer.Issue` with injected argv runner and
  clock. Fake `ssh-keygen` tests assert argv/files without invoking host SSH.
- [ ] Add CLI tests proving domain init requires an explicit configured domain,
  initializes only that domain's CA, never searches or falls back across
  domains, never invokes host init, and is never reached lazily from session
  start.

### V3.4 Host-key pin store and strict SSH policy

**Files:**

- Create `internal/sshx/pin.go`, `pin_test.go`
- Create `internal/sshx/client.go`, `client_test.go`
- Create `internal/sshx/knownhosts.go`, `knownhosts_test.go`
- Create `docs/operations/ssh-management.md`

`PinStore.Admit` accepts only a parsed Ed25519 public host key obtained by V4's
trusted serial exchange and writes the exact immutable record defined above.
It derives the filename from the validated session UUID, not user input. It
also materializes a generation-private known-hosts file keyed by a
session-UUID-derived `HostKeyAlias`, not the current address.

`Client` uses absolute `/usr/bin/ssh`, `-F /dev/null`, exact generation
`IdentityFile` and `CertificateFile`, derived `HostKeyAlias`, exact alias-keyed
`UserKnownHostsFile`, `GlobalKnownHostsFile=/dev/null`,
`StrictHostKeyChecking=yes`, `CheckHostIP=no`, `BatchMode=yes`,
`IdentitiesOnly=yes`, `IdentityAgent=none`,
`HostKeyAlgorithms=ssh-ed25519`, `UpdateHostKeys=no`,
`VerifyHostKeyDNS=no`, `CanonicalizeHostname=no`, `ProxyCommand=none`,
`ProxyJump=none`, `ControlMaster=no`, `ControlPath=none`, `RequestTTY=no`,
`PasswordAuthentication=no`, `KbdInteractiveAuthentication=no`,
`ForwardAgent=no`, `ForwardX11=no`,
`ClearAllForwardings=yes`, `PermitLocalCommand=no`, `Tunnel=no`, and bounded
connect/server-alive timeouts. It logs in only as `boxwarden` and invokes the
exact fixed remote command `/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap management`. No input-derived bytes
enter that command. Separate bounded typed request structures exist only for
`Probe`, `ApplyZone`, and `ReadZone`; there is no generic operation+argv API.

- [ ] Test exact idempotent pin, changed key, wrong domain/session/backend,
  algorithm rejection, duplicate/oversized key, symlink/hardlink store, IP
  changes, alias-keyed known-host formatting, every exact option above,
  ambient ssh config/proxy/multiplexing rejection, deadline, output truncation,
  fixed remote command, typed stdin framing, and no general argv or
  input-derived shell text.
- [ ] Implement pin/store/client independently of Tart. V4 supplies address and
  serial evidence through interfaces.

### V3.5 Host-global read-only doctor and V3 integration

**Files:**

- Create `internal/hostx/doctor.go`, `doctor_test.go`
- Modify `internal/app/app.go`, `app_test.go`
- Modify `cmd/boxwarden/main.go`

`boxwarden doctor` operates outside the domain namespace and checks: supported
macOS/architecture; exact absolute Tart identity;
Softnet canonical ancestors, ACLs, link count, digest, root owner, group,
`04550`, manifest and paired identities; exact manifested operator UID/name/home
and group ID/name/directory membership; current-process supplementary group;
canonical `tart_home`; ssh/ssh-keygen availability; and exact `/usr/bin/screen` 4.00.03 (FAU,
23-Oct-06), SHA-256
`07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add`,
root:wheel `0755`, one link, on macOS 26.6.2. Any setuid/setgid or
passwordless-root Softnet under mutable Homebrew state is `drifted/unsafe`,
makes doctor nonzero, and blocks init/start even if the staged copy is healthy.
Each finding has stable code, category, observed fact, expected fact, and remedy
requiring attended manual inspection or explicit init; doctor never repairs.
Domain CA state is deliberately absent from this report. Domain init and the
domain-scoped session prerequisite checks own those diagnostics.

- [ ] Test every status category, multiple simultaneous findings, deterministic
  ordering, redaction, unsafe Homebrew blocking, current-process group refresh,
  explicit `--domain` rejection, no domain lookup, nonzero exit, no
  writes/process mutation, and inability to call any initializer/privilege
  runner.
- [ ] Wire host-global `init` only to host-tool installation and host-global
  `doctor` only to read-only host checks. Wire domain `init` independently to
  the CA store. Never couple host installation success or repair to creation of
  a selected domain's CA.
- [ ] Run V3 verification:

```bash
test -z "$(gofmt -l $(git ls-files -- '*.go'))"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/boxwarden
git diff --check
```

**V3 host-global attended gate:** On the exact qualified disposable M1A host,
inspect the install request, authorize `boxwarden init`, verify the complete
installed tree/group/mode/ACL/link/digests/manifest, host-global idempotent
rerun, host-global `boxwarden doctor` output, and refreshed effective group.
Prove the installed exact setuid Softnet's
argument parsing, closed environment and dependency resolution, effective
privilege transition/drop, signals, filesystem writes, exact qualified network
behavior, and absence of any sudo path. Also prove an unsafe Homebrew setuid
copy blocks doctor/init/start without chmod/repair. Record redacted evidence;
deterministic fakes do not satisfy this gate.

**V3 domain-foundation attended gate:** After the host gate, explicitly run
`boxwarden --domain work domain init` and
`boxwarden --domain personal domain init`. Verify exactly one distinct host-only
management CA per domain, idempotent rerun, no cross-domain fallback or reuse,
and no host-toolchain installation or authorization change during either domain
operation. Adding the second domain must not require another `boxwarden init`.

---

## V4 — start, supervise, serial bootstrap, and readiness

### V4.1 Generic guest bootstrap contract

**Files:**

- Create `cmd/boxwarden-guest-bootstrap/main.go` and tests
- Modify `guest/ubuntu-24.04-arm64/autoinstall/user-data`
- Modify `guest/ubuntu-24.04-arm64/tests/bootstrap.sh`
- Create `guest/ubuntu-24.04-arm64/artifacts.lock.json`
- Modify `scripts/spike/bootstrap-tart.sh` remaster mapping and its tests

The golden installs generic sshd policy pointing to
`/etc/ssh/boxwarden/active/trusted-user-ca.pub` and
`/etc/ssh/boxwarden/active/authorized_principals/%u`, but the `active`
directory does not exist in the golden. Install a root-owned static helper at
`/usr/local/libexec/boxwarden-guest-bootstrap`. Guest root remains unrestricted;
Boxwarden invokes the helper only through the fixed sudo commands below.

Build the offline helper reproducibly with
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o guest/ubuntu-24.04-arm64/artifacts/boxwarden-guest-bootstrap ./cmd/boxwarden-guest-bootstrap`.
Use Go `debug/elf` to require `EM_AARCH64`, no `PT_INTERP`, and no `DT_NEEDED`;
build twice from clean inputs and require the same digest before recording that
exact digest and build identity in `artifacts.lock.json` and the
golden BOM, and pass the verified file to the actual remaster command as an
explicit input. `bootstrap-tart.sh remaster-iso` maps that exact source into the
ISO staging tree; autoinstall late-command verifies the locked digest and
installs root:root `0755` at the fixed path. Tests trace clean build → exact ISO
mapping argv → installed source/digest; no network fetch occurs during remaster
or install.

In serial-bootstrap mode, the helper reads one bounded version-1 JSON request
from stdin containing nonce, current start generation, durable domain ID,
session UUID, backend kind/object, public CA line/fingerprint, and exact derived
principal. This command has one fixed serial-bootstrap request shape rather
than an operation selector. Nonce/generation are echoed correlation
only; they are not part of installed binding.
It emits exactly one `BOXWARDEN-BEGIN <nonce> <session-uuid>` line and one
`BOXWARDEN-END <nonce> <session-uuid> <base64-json-result>` line. The result
echoes the full association, installed hashes, validated effective sshd fields,
and `/etc/ssh/ssh_host_ed25519_key.pub`; it contains no private key.

The helper validates all fields and exact principal derivation, writes the public
anchor, `authorized_principals/boxwarden`, and a durable management-binding
manifest containing only domain/session/backend/CA fingerprint/principal into
one fixed-parent sibling staging directory. Staging is root-only until complete;
before publication the helper fixes and verifies root:root `0755` on `active`
and `authorized_principals`, root:root `0644` on the public CA and
`authorized_principals/boxwarden`, root:root `0600` on the binding manifest,
and no group/other-writable path component. It fsyncs every file and directory,
then atomically renames that complete directory to `active` and fsyncs the
parent. OpenSSH resolves and opens the CA/principals files for each certificate
authentication, so the next authentication sees the new tree without an sshd
reload; the traversable final directory modes are required because
`AuthorizedPrincipalsFile` is opened under the target workstation UID and
StrictModes validates its ancestry. If `active` already exists, the helper
succeeds only after verifying the exact association, bytes, ownership, and modes;
mismatched state is never replaced. It then runs absolute
`/usr/sbin/sshd -t` and
`/usr/sbin/sshd -T -C user=boxwarden,host=localhost,addr=127.0.0.1` and verifies
CA/principals paths plus `AuthorizedKeysFile none`, `PermitUserEnvironment no`,
`PermitUserRC no`, password, keyboard-interactive, root, X11, TCP,
stream-local, and tunnel prohibitions before success. `PermitLocalCommand` is
client-only and is not treated as an sshd field. An
exact association/bytes retry is idempotent. Any mismatch fails without replacing
trusted files.

In management mode, the helper reads the bounded versioned request built by
`sshx.Client`, validates the exact durable association and one typed request
shape, and directly executes fixed absolute programs. It does not invoke a
shell. V4 implements only typed readiness-probe, time-zone-apply, and
time-zone-read requests; no generic operation+argv or remote-shell API exists.

- [ ] Write helper tests first for malformed/duplicate/oversized request,
  association/principal mismatch, generation/nonce accidentally persisted,
  later-generation exact binding retry, unsafe target, stale staging directory,
  every interrupted atomic stage, final-tree ownership/modes/traversal, exact
  retry, changed anchor/principal, missing/fresh/malformed host key, sshd
  effective mismatch including client-only/server-field confusion, framing,
  deadline cancellation, and no private output.
- [ ] Update golden fixture tests to prove no domain CA/principal exists and the
  generic policy, locked static helper, and root-owned parent do while `active`
  does not. Test two clean reproducible builds, Go `debug/elf`
  AARCH64/no-interpreter/no-needed inspection, digest lock/BOM, remaster
  mapping argv, and installed source/digest end-to-end with fixtures.

### V4.2 Supervisor-owned two-PTY broker and framed exclusive exchange

**Files:**

- Create `internal/serialx/broker.go`, `broker_test.go`
- Create `internal/serialx/pty_darwin.go`, `pty_darwin_test.go`
- Create `internal/serialx/screen.go`, `screen_test.go`
- Create `internal/serialx/lease.go`, `lease_test.go`
- Create `internal/serialx/exchange.go`, `exchange_test.go`

On Darwin, allocate two native PTY pairs with Go syscalls/ioctls. The supervisor
owns both masters and keeps the exact device identities; Tart opens only the
mode-`0600` Tart slave. Exact `/usr/bin/screen -D -m` is a direct waitable child
on the mode-`0600` operator slave and remains its sole reader. Its required
identity is system Screen 4.00.03 (FAU, 23-Oct-06), SHA-256
`07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add`,
root:wheel `0755`, one link, on macOS 26.6.2. Production has no socat dependency.

The broker is the sole forwarding reader. Tart-master output goes to a fixed
256-KiB Screen-output queue and, only while automation is armed, a separate
fixed-memory raw parser. Operator-master input forwards only in `console`;
every other mode, including `idle` and `automation`, discards and counts it
without buffering or replay. Automation never opens the operator
PTY and never uses Screen log/hardcopy/`stuff`/paste/control as data transport.
One serialized state machine owns `idle`, `console`, `automation`, and `failed`.
The parser permits 8-KiB lines, 64-KiB frames/results, 256 KiB total per exchange,
and a 30-second overall deadline; all bounds are constants. There is no serial
transcript or log. Queue overflow, guest flood, Screen/broker exit, malformed or
ambiguous frame, or bound/deadline violation atomically poisons the generation
and prevents hot repair.

Automation sends exactly `/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap`, newline, then
canonical bounded JSON as a separate write. `Exchange` requires exactly one
matching begin/end pair with fresh nonce, current generation, and durable
domain/session/backend association. It echoes but never installs nonce or
generation. Any poisoned/ambiguous transport with still-proven supervisor
ownership triggers exact owned shutdown and observed cleanup before retry; if
ownership is unproven it reports drift and mutates nothing.

- [ ] Test native PTY allocation/identity/modes, exact Screen argv/identity and
  direct-child lifetime, sole-reader topology, attach/detach, unattended output,
  state transitions, operator-input discard/count/no-replay in every non-console
  mode including idle and automation, lease races and
  cancellation, every fixed queue/line/frame/result/total/deadline boundary,
  flood/overflow/Screen/broker poison, absence of logs and Screen data APIs,
  static command plus separate canonical JSON, later-generation durable-binding
  retry, and owned/unowned poisoned cleanup. CI uses deterministic PTY/process/
  clock/transport fakes; native Darwin tests are non-mutating local probes.

### V4.3 Supervisor ownership and exact Tart/Softnet launch

**Files:**

- Create `internal/supervisor/request.go`, `request_test.go`
- Create `internal/supervisor/runtime.go`, `runtime_test.go`
- Create `internal/supervisor/process.go`, `process_test.go`
- Modify `internal/backend/observation.go`, fake backend and tests
- Create `internal/backend/tart/launch.go`, `launch_test.go`
- Create `internal/backend/tart/address.go`, `address_test.go`
- Modify `cmd/boxwarden/main.go`

Add neutral `Starter.Start(ctx, StartRequest)`, `AddressResolver.Resolve(ctx,
objectID)`, and `RuntimeObserver.ObserveRuntime(ctx, ownership)` interfaces.
`StartRequest` contains exact backend object, generation runtime paths, Tart
path/toolchain identity, and serial Tart endpoint. It
does not contain policy decisions encoded as arbitrary Tart arguments.

The parent persists a versioned supervisor request in the private generation
directory and starts hidden `boxwarden internal supervise` in a private process
group/session with private bounded stdio and a fresh handshake nonce. It does
not use the initiating CLI's `CommandContext`. The supervisor validates the complete request association,
acquires the generation ownership lock, writes a manifest containing domain,
session UUID, backend object, start generation, supervisor instance nonce, PID
and process-start evidence, broker generation/health, both PTY device identities,
Screen direct-child/start/socket evidence, overflow/poison state, lease mode,
and qualified toolchain digests, then signals over an owner-only persistent
control socket. Parent/status/reconnect use nonce challenge/response and accept
only matching socket/manifest/process evidence. PID alone is never sufficient.

The same-user supervisor remains long-lived, holds the generation lock and
socket for its lifetime, and never `exec`-replaces itself with Tart. Tart,
broker, and Screen are direct/owned children with explicit child-exit and cleanup
protocols. A replacement CLI reconnects after parent crash. Only the supervisor
may report or clean its owned generation; external cleanup requires the same
complete proof.

The supervisor re-runs doctor-equivalent immutable prerequisite checks, then
executes the absolute qualified Tart path with a closed environment. PATH is
exactly the digest-specific Softnet directory; `HOME`/`USER`/`LOGNAME` come from
the manifested operator, `TART_HOME` is the canonical configured directory,
`TMPDIR` is the private generation directory, and locale values are fixed.
Ambient proxy, Sentry/telemetry, Rust/language-runtime, DYLD/loader, and other
variables are absent; no `sudo` or shell is inherited. Launch
uses Task 0's exact `--net-softnet`, `--no-audio`, `--no-clipboard`,
and `--serial-path` default policy. Reject every allow flag and
filesystem shares, disks, Rosetta, VNC, bridged/host networking, port exposure,
nested virtualization, `0.0.0.0/0`, and arbitrary arguments.

Address resolution uses exact `tart ip --resolver=dhcp --wait=<bounded>` after
bootstrap is pinned and immediately before SSH. It validates one address,
refreshes after lifecycle/network failure as Task 0 requires, and never persists
the IP as identity.

- [ ] Test exact argv/closed env/PATH equality, digest drift between doctor and
  exec, every allow/prohibited flag, handshake/manifest mismatch, private
  process group/stdio/socket, initiating-context cancellation and reconnect,
  never-exec-replace behavior, lost supervisor,
  fabricated/reused PID, stale socket, duplicate supervisor, parent crash,
  owned child exit cleanup, unproven cleanup refusal, address stale/empty/
  multiple/malformed/timeout, and fake toolchain drift.
- [ ] Ensure only the command composition imports Tart and production process
  cleanup uses the complete proven generation association.

### V4.4 Start transition, bootstrap, SSH, and time-zone readiness

**Files:**

- Create `internal/lifecycle/start.go`, `start_test.go`
- Create `internal/lifecycle/readiness.go`, `readiness_test.go`
- Create `internal/timezonex/host.go`, `host_test.go`
- Create `internal/timezonex/guest.go`, `guest_test.go`
- Modify `internal/session/record.go`, `record_test.go`, `store.go`, `store_test.go`
- Modify `internal/app/app.go`, `app_test.go`
- Create `docs/operations/start-and-recovery.md`

`session start` acquires the session lock, serializes with supervisor renewal,
probe, and status snapshot publication, and applies this exact matrix:

| Durable intent | Backend | Runtime proof | Action/result |
|---|---|---|---|
| stopped | stopped | none owned | persist `starting` + fresh generation + non-ready, fsync, then launch |
| starting | running | exact live same generation | reconnect and resume that generation |
| starting | stopped | no live owned runtime | persist `stopped` + clear generation, fsync, then retry from stopped |
| running | running | exact live same generation | idempotently ensure/reprobe/reconverge that generation |
| any | running | absent, stale, mismatched, or unverifiable | report DRIFT/NON-READY; no durable/backend/runtime mutation or adoption |

All other combinations fail closed with a bounded diagnostic. It does not start
when init/doctor is missing, unsafe, drifted, or unqualified and never repairs
host privilege. Any mutable Homebrew privileged Softnet blocks it.

After observed backend-running and healthy broker ownership, start enters
automation and calls the helper `apply` exchange. It verifies current nonce/
generation echo separately from the durable domain/session/backend/CA/principal
binding, effective sshd settings, and fresh Ed25519 host public key, then calls
`PinStore.Admit`. Only after the
exact pin is durable does it resolve the current address, issue the short-lived
certificate, materialize known-hosts, and run a bounded strict SSH readiness
probe. It detects the host IANA zone with no fallback, applies it over strict
SSH using exact argv, reads back the effective zone, and requires equality.

The persistent supervisor owns the generation client key/certificate. It
revalidates immutable CA metadata before signing, renews the 15-minute
no-extension certificate when five minutes remain, and performs a typed strict
read-only probe every 30 seconds. It publishes only a bounded authenticated
health snapshot over the control socket. Evidence older than 90 seconds,
expired certificate, failed challenge, failed probe, poisoned broker, or missing
child is non-ready. Supervisor renewal/probe are independent lifecycle duties;
`session status` never triggers them.

When every check succeeds, atomically persist intended `running` and last
readiness `ready`, then re-observe backend and runtime once before printing
READY. Read-only status observes backend and current host zone, challenges the
supervisor, and consumes its health snapshot. It never writes durable/backend/
guest state, creates credentials, renews, probes, repairs, or applies a zone.
The supervisor's latest probe reads guest zone without applying it. A host-zone
mismatch is non-ready; idempotent start on the exact proven running generation
may apply/read back and reconverge. Status independently recomputes:

```text
READY = intended running
     && backend running
     && matching live supervisor ownership
     && matching live retained serial/Screen state
     && exact domain/session/backend host-key pin
     && current no-extension certificate and probe evidence are <= 90s old
     && guest zone equals current validated host zone
```

Any failed step with proven ownership may persist a bounded non-secret
`starting` diagnostic only when the current durable state and lock allow it and
reports NON-READY. An unproven running observation reports drift without any
durable write. With
proven ownership, failure or poisoned/ambiguous serial transport requires the
exact supervisor to stop all owned children, observe backend stopped, and
complete exact runtime cleanup; only then may start persist `stopped`, clear the
generation, and permit fresh retry. If ownership proof is lost, it leaves the
VM/runtime untouched and reports drift. This V4 recovery path exists before V6.

Cancellation before the first intent fsync leaves the prior stopped record.
Cancellation after `starting` fsync leaves `starting` with that exact generation
for reconnect/retry. Cancellation during proven shutdown leaves it `starting`
until observed backend stop and runtime cleanup allow the final atomic
`stopped`/clear-generation fsync. Failure of final running fsync leaves
`starting` with the live exact supervisor generation; retry reconnects and
revalidates rather than launching another. No boundary creates two generations.

- [ ] Write table-driven failure tests for every boundary: intent fsync,
  prerequisite check, supervisor handshake, backend launch/observation, relay/
  Screen loss, serial lease/frame/apply/sshd verification, changed pin, address,
  certificate, strict SSH, host-zone detect, guest apply/readback, final record
  fsync, final re-observation, cancellation, and retry after each partial state.
- [ ] Test intended-running + backend-running with absent/stale/wrong supervisor
  as DRIFT/NON-READY; prove no adoption and no unproven signal/unlink. Test
  backend-running before trust as STARTING/NON-READY and stale persisted `ready`
  as non-authoritative.
- [ ] Test V2 stopped-record upgrade, exact bootstrap retry idempotence, host-key
  mismatch refusal, address change without identity change, certificate renewal,
  and host-zone change across starts. Create never calls time-zone code.
- [ ] Exercise every matrix row and every cancellation/fsync boundary, including
  final-running-fsync reconnect, exact owned shutdown before stopped persistence,
  poisoned serial with/without proof, supervisor renewal/start/status
  serialization, 30-second probe cadence, five-minute renewal threshold,
  90-second evidence age, and read-only status call-spies proving no mutation.
- [ ] Add CLI output tests that distinguish intended, backend, supervisor,
  bootstrap, SSH, time-zone, and aggregate readiness without printing secret or
  private runtime paths.
- [ ] Run V4 verification:

```bash
test -z "$(gofmt -l $(git ls-files -- '*.go'))"
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./cmd/boxwarden
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o guest/ubuntu-24.04-arm64/artifacts/boxwarden-guest-bootstrap ./cmd/boxwarden-guest-bootstrap
bash -n guest/ubuntu-24.04-arm64/tests/bootstrap.sh
bash -n scripts/spike/bootstrap-tart.sh
git diff --check
```

**V4 attended gate:** On one disposable stopped clone and the exact V3-qualified
host, prove retained serial attach/detach and lease exclusion, generic-to-domain
anchor/principal binding, effective sshd policy, serial-observed/pinned fresh
host-key equality, strict short-lived certificate SSH, address refresh, host
time-zone convergence, READY gating, 30-second probe/90-second evidence/renewal
behavior, and owned cleanup. Requalify ADR 017's native two-PTY broker with
exact Screen identity, every bound/flood/poison path, and no transcript. Separately induce lost
supervisor evidence and prove DRIFT/NON-READY with no adoption or unproven
cleanup. Record only redacted evidence.

---

## Roadmap after this plan (not executable here)

- **V5 — exact `session cp`:** explicit host↔guest file transfer over V3/V4
  pinned SSH. No live host filesystem share, mount, sync, promotion, or ADR 021
  implementation.
- **V6 — stop:** intended-state reconciliation and exact owned supervisor/backend
  shutdown.
- **V7 — destroy:** exact guarded normal and compromised destruction.
- **Later:** project durability, profiles, provider/application authentication,
  additional backends, checkpoints, and release validation only after separate
  decisions and plans.

This run must not implement or authorize V5+, provider authentication, host
filesystem sharing, ADR 021, or ADR 022. After V4 verification and its honestly
reported attended-gate status, stop.
