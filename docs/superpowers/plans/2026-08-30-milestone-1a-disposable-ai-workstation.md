# Milestone 1A Disposable AI Workstation Implementation Plan

> **Status: superseded; retained as planning history only. Do not execute any
> task below.** The corrected V1-V4 sequence, generic-golden model, serial-first
> trust bootstrap, default-only V4 network policy, and later roadmap are in
> `docs/superpowers/plans/2026-09-01-boxwarden-v0.1.md`. Accepted ADRs and
> current canonical architecture documents control wherever this historical
> plan differs.

> Historical planning note: ADR 023 supersedes this document's treatment of
> Docker as an expected guest workload runtime. Docker remains a possible
> qualified guest-local capability, but Boxwarden does not prescribe a runtime
> for workloads.

**Goal:** Deliver Boxwarden's `boxwarden` executable, a Go control plane that builds qualified Ubuntu 24.04 ARM64 Tart goldens and manages cheap disposable and quarantine AI workstations on macOS without exposing trusted-host control.

**Architecture:** Common packages own security domains, intent/reconciliation, locks, golden selection, profile/encryption policy, project durability, credential/provider policy, validation, and destructive safety. A narrow Tart backend owns only VM mechanics and observations. Portable guest definitions produce generic immutable Tart golden artifacts; domains independently admit and select them through trusted-host metadata. Markdown is canonical memory; M1A profiles use only explicit declarative adapters. Kindex full-state persistence is unsupported.

**M1A platform:** macOS host; Tart backend; Ubuntu 24.04 ARM64 guest; Ubuntu-supported desktop/Wayland with XWayland for ChatGPT Desktop; guest workload-neutral under ADR 023; no Linux-host backend or bootc work.

**Implementation rules:** Go standard library unless a dependency is justified and locked. Never invoke a shell for host control-plane commands. Never accept an unvalidated path as a backend object name or filesystem target. Write intent before mutation, lock conflicting operations, reconcile against observed state, and test failure paths before success paths are considered complete.

## Target repository layout

```text
cmd/
  boxwarden/                 host CLI
  boxwarden-guest/           portable Linux/ARM64 guest helper
internal/
  app/                       command orchestration
  backend/                   minimal backend interface and neutral types
    fake/                    deterministic tests
    tart/                    M1A adapter only
  config/                    strict config and domain resolution
  domain/                    validated security-domain identifiers
  execx/                     argv-only process runner and supervisors
  golden/                    lock, build, evidence, promotion
  lifecycle/                 intent, reconciliation, transitions
  lock/                      filesystem operation locks
  profile/                   manifests, candidates, approval, store
    adapter/                 explicit adapter interface
    gitprefs/                git-preferences-v1
    markdown/                sensitive-markdown-v1
  project/                   durability registration and checks
  secret/                    bounded stdin-only secret delivery
  session/                   modes, naming, registry, destructive policy
  sshx/                      address discovery, certs, known-host pinning
  validate/                  host/golden/session/property checks
config/
  boxwarden.example.json
  golden.lock.json
  profiles/
    development.example.json
guest/
  ubuntu-24.04-arm64/
    autoinstall/
    manifests/
    provision/
    systemd/
    tests/
scripts/
  build-golden.sh
  create-session.sh
  start-session.sh
  ssh-session.sh
  status.sh
  destroy-session.sh
  spike/
docs/
  evidence/
  decisions/
  superpowers/plans/
memory/
  README.md
  knowledge/
  lessons/
```

Runtime state is outside the repository under `$XDG_STATE_HOME/boxwarden/domains/<domain>/` (defaulting to `~/.local/state/boxwarden/domains/<domain>/`). Each configured domain has its own session registry, locks, logs, candidates, golden pointer and artifact registry, SSH CA/known-host references, provider/Git identity references, profile-store URI, memory scope, and age recipients. No global identity, profile, memory, project, session, artifact-registry, or golden fallback exists.

## Task 0: Qualify the physical, network, identity, and graphical lifecycle

**Status:** Complete — **PASS WITH CONDITIONS**. ADR 020 separates the
qualified core platform from explicitly unqualified network environments.

**Files:**

- Create: `scripts/spike/bootstrap-tart.sh`
- Create: `scripts/spike/finalize-clone.sh`
- Create: `guest/ubuntu-24.04-arm64/autoinstall/user-data`
- Create: `guest/ubuntu-24.04-arm64/autoinstall/meta-data`
- Create: `docs/evidence/m1a-bootstrap-spike.md`
- Test: `guest/ubuntu-24.04-arm64/tests/bootstrap.sh`

1. Record host architecture, macOS version, ISO URL/digest, firmware/boot method, and every manual prerequisite. Record the exact Tart and Softnet versions, installation/source identities, relevant artifact/package identities where practical, and how automatic upgrade is prevented. Treat that exact pair—not Tart alone—as the security-critical host toolchain being qualified. Because the tested Softnet 0.19.0 path requires host root privilege, also record that trusted-host attack surface and require any eventual privilege mechanism to authorize an exact qualified, non-user-mutable artifact and relevant execution dependencies. Do not authorize a mutable user-writable Homebrew path or let upgrades inherit root authorization without requalification; Task 0 records the requirement without choosing or installing the production mechanism.
2. Produce a genuinely unattended Ubuntu 24.04 ARM64 install under Tart. Preferred path is a supported Desktop ISO autoinstall; approved fallback is live-server autoinstall plus a pinned desktop package set. Record the exact kernel command line, including the required `autoinstall` parameter and seed-discovery mechanism. The fallback must pass the same acceptance properties.
3. Prove install/reboot completion detection, guest desktop login through Tart display/input, Ubuntu's supported Wayland session, XWayland client execution, key-only SSH without X11/agent/TCP/tunnel forwarding, and equality between the host's validated IANA time zone and the installed guest's effective time zone. A fixed UTC seed does not qualify.
4. Qualify the outbound Softnet shared/NAT policy rather than assuming flags imply properties. Record the demonstrated `--net-softnet-block=@host` DNS failure and ADR 015's decision to omit that block in M1A. From the guest, prove public Internet access and host/VPN-provided DNS, attempt meaningful TCP connections—not only ICMP—to the vmnet gateway and representative RFC1918/private-network targets, and classify the gateway reachability as an accepted host attack surface. Under the default policy, the guest must not initiate connections to private/link-local targets. Record the danger that `--net-softnet-allow=0.0.0.0/0` disables bridge isolation and do not use it. Do not use physical bridging, Tart host networking, or a fixed public resolver.
5. Boot at least two Boxwarden-style Softnet VMs concurrently and prove neither can connect to the other's SSH or another known listening service. Exercise same-domain and different-domain labels so the evidence makes clear that session isolation is a backend network property, not a security-domain namespace property.
6. Under the default Softnet shared/NAT candidate, prove DHCP lease acquisition and renewal, DNS resolution, the resolver address received by the guest, and whether the vmnet gateway is also the resolver. Prove outcome-level connectivity with available VPN/custom-or-split-DNS configurations and characterize the actual mobile tether rather than inferring its address-family behavior. Record each tested environment independently. If an effectively IPv6-only upstream is unavailable, keep that upstream plus its dependent IPv4-only and IPv6-only destination cases explicitly `NOT YET PROVEN` under ADR 020; do not block the core platform decision or claim support. A static public resolver is diagnostic evidence only and is prohibited in accepted configuration.
7. Test `tart ip <vm>` and `tart ip --resolver=dhcp <vm>` under Softnet. Observe at least one DHCP lease renewal, whether the address changes, and what refresh/caching interval is safe. Record `arp` incompatibility as a technical constraint and the `agent` resolver as a policy-excluded guest-agent bridge; do not install a guest agent for discovery.
8. Historical investigation item: evaluate serial bootstrap for the regenerated
   SSH host key and management identity. ADR 017 subsequently qualified the
   trusted host-local serial recovery channel, and amended ADR 012 now requires
   post-clone installation of the selected domain's public CA anchor and exact
   principal before host-key pinning and strict SSH. Generic goldens contain no
   domain trust, and TOFU is prohibited.
9. Empirically map graphical Tart process ownership and lifetime. Start from an interactive Terminal; exit the Boxwarden-like launcher; close the invoking shell; start from SSH while the same macOS user has an active Aqua login; disconnect SSH; lock the Mac; log the console user out; and test after host reboot and normal user login. Record whether the UI and VM survive, whether a logged-in Aqua user is required, which process owns the VM, and which process identity/observations can support safe reconciliation without relying on a reusable bare PID. Do not choose launchd agent, launchd daemon, detached child, or helper architecture before this evidence.
10. Finalize the candidate by removing `/etc/machine-id`, SSH host keys, DHCP/DUID/client identifiers, random seeds where safe, installer residue, shell history, logs, credentials, and provider/browser state. Verify the next boot regenerates required state.
11. Clone twice, run `tart set <vm> --random-mac` for each clone, boot both, and prove distinct MAC, machine ID, SSH host keys, DHCP/DUID identity where relevant, hostname, machine/random seed behavior where relevant, and discovered address. Confirm neither clone changes the stopped golden.
12. Measure clone, first-boot, SSH-ready, desktop-ready, stop, and destroy latency and disk use. Record results as the initial routine-destruction UX budget.
13. Preserve reviewable command output, versions, timestamps, and observations. Label each conclusion `observed`, `inferred`, `vendor documented`, or `not yet proven`. Add a scripted evidence check that fails if any required evidence cell or two-clone comparison is absent.
14. Run the spike twice from the recorded starting prerequisites. Commit only portable autoinstall/finalization assets and redacted evidence; never commit a key, token, generated VM, transient host fingerprint, or local path containing private material.

**Gate:** Core platform qualification requires unattended install, graphical/Wayland/XWayland behavior, secure management and serial recovery access, guest/host IANA time-zone equality, unique clone identity, acceptable routine-session latency, public Internet, inherited host/VPN DNS, default guest→private-network denial, session→session denial, host→guest SSH, DHCP behavior, management-address refresh, initial host-key evidence, and graphical process lifetime for the recorded Tart + Softnet pair. Demonstrate and document guest→vmnet-gateway reachability as ADR 015's accepted limitation; it is not a pass criterion and must not be reported as isolation. Environment-specific connectivity is an extensible qualification matrix under ADR 020: untested environments may remain explicitly deferred in `PASS WITH CONDITIONS`, but no support claim may exceed the observed matrix. Future evidence may promote an environment without reopening platform selection. Exact launch argv, resolver behavior, host-key bootstrap, address caching, and supervision must be reviewed before implementation uses them.

**Verification:**

```bash
guest/ubuntu-24.04-arm64/tests/bootstrap.sh
git diff --check
```

## Task 1: Establish the Go control plane, strict domains, and CLI contract

**Files:**

- Create: `go.mod`
- Create: `.gitignore`
- Create: `README.md`
- Create: `cmd/boxwarden/main.go`
- Create: `internal/app/app.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/domain/domain.go`
- Create: `internal/domain/domain_test.go`
- Create: `internal/execx/runner.go`
- Create: `internal/execx/runner_test.go`
- Create: `config/boxwarden.example.json`
- Modify: `memory/README.md`

1. Write failing tests for strict JSON decoding, unknown keys, invalid/duplicate domain IDs, relative or symlinked roots, overlapping domain roots, missing daily/offline age recipients, shared private-identity paths, shared profile stores, and any attempted fallback to a different domain.
2. Define domain IDs as lowercase ASCII labels with a conservative length bound. Resolve all domain-owned paths from the configured record, canonicalize existing ancestors, reject symlinks, and create directories with `0700` and files with `0600`/`O_EXCL` as appropriate.
3. Define `boxwarden --domain <id> <resource> <verb>` in module `github.com/weshofmann/boxwarden`. Require `--domain` or `BOXWARDEN_DOMAIN`; do not invent an implicit first/default domain. Initial resources are `validate`, `golden`, `session`, `profile`, `project`, and `identity`. Reserve repeatable `--allow-private-network <CIDR>` session-creation options under ADR 015; parsing a flag does not itself authorize an exception until the session-policy and backend validations in Tasks 6 and 2 accept it.
4. Implement an argv-only `execx.Runner` around `exec.CommandContext`; capture bounded output, redact declared sensitive stdin, and never invoke `sh -c`.
5. Add an architecture test that parses imports and fails if common packages import `internal/backend/tart` or if the Tart package is imported outside composition/bootstrap code.
6. Document the Markdown memory directories and ignored `.memory/candidates/` convention; do not add an index service.

**Verification:**

```bash
go test ./internal/config ./internal/domain ./internal/execx
go test ./...
go vet ./...
go build -o bin/boxwarden ./cmd/boxwarden
```

## Task 2: Implement the narrow backend contract and Tart adapter

**Files:**

- Create: `internal/backend/backend.go`
- Create: `internal/backend/types.go`
- Create: `internal/backend/fake/fake.go`
- Create: `internal/backend/tart/tart.go`
- Create: `internal/backend/tart/tart_test.go`
- Create: `internal/backend/tart/parse.go`
- Create: `internal/backend/tart/parse_test.go`
- Test: `internal/app/architecture_test.go`

1. Start with consumer-driven tests and define only required operations: create a candidate, clone, configure resources, randomize machine MAC, start under a validated launch policy, stop, delete, inspect actual state, and obtain a management address. Neutral types contain IDs, resources, lifecycle observations, and security properties—not Tart flags or a speculative hypervisor feature matrix. Checkpoint operations are absent from M1A.
2. Have the common layer construct an immutable launch policy requiring no host filesystem/display-server/Docker/clipboard/audio/port/forwarding integration and requiring public Internet, inherited host/VPN DNS, default guest→private-network denial, session→session denial, and honest environment-qualification status under ADR 020. The Tart adapter maps that policy to the exact Tart + Softnet shared/NAT argv qualified and approved by Task 0. Model guest→vmnet-gateway reachability as an explicit M1A limitation rather than a provided isolation property. Support only an explicitly requested, validated list of exact private CIDRs from persisted session policy; map no implicit network, reject vmnet/session overlap, and prove bridge isolation remains effective. The adapter supplies no share, disk, Rosetta, VNC, bridged/host, expose, nested, or `--net-softnet-allow=0.0.0.0/0` flag. Effectively IPv6-only upstream behavior remains unavailable as a support claim until separate evidence promotes it.
3. Validate backend object names before execution. Pass every argument as a separate argv element. Parse `tart list --format json` defensively, reject duplicate/ambiguous objects, and treat schema drift as an actionable error.
4. Model `tart run` as a long-lived graphical process with a domain/session-owned durable log, not as a completed start command. Implement only the supervision/process-identity mechanism justified by Task 0. Stop/reconcile tolerates process exit and manual Tart state changes and never signals or trusts a process solely because a persisted bare PID is live.
5. Test command failure, timeout, malformed JSON, missing object, duplicate object, already-running/stopped/deleted cases, partial configuration, and forbidden launch-policy combinations.
6. Test backend-independent properties separately from exact Tart argv. A passing flag-string test alone is insufficient.

**Verification:**

```bash
go test ./internal/backend/... ./internal/app
go test -race ./internal/backend/...
go vet ./...
```

## Task 3: Define provenance locks and qualify immutable inputs

**Files:**

- Create: `internal/golden/lock.go`
- Create: `internal/golden/lock_test.go`
- Create: `config/golden.lock.json`
- Create: `guest/ubuntu-24.04-arm64/manifests/packages.lock.json`
- Create: `guest/ubuntu-24.04-arm64/manifests/tools.lock.json`
- Create: `docs/evidence/tool-qualification.md`

1. Define a lock schema that records guest OS/release/architecture/libc, exact version, immutable source identity, URL/repository snapshot, signing-key fingerprint, digest/signature method, install method, updater-disable method, dependency-closure claim, qualification evidence, and availability status for every artifact. Define the companion host-tool qualification record for the exact Tart + Softnet pair and the Task 0 behaviors proven for it.
2. Write tests rejecting mutable URLs without verified digest/signature, floating package versions/tags, mutable piped installers, missing ARM64 qualification, unrecorded repository keys, duplicate artifacts, and claims of indefinite rebuildability without retained closure evidence.
3. Qualify Ubuntu, ChatGPT Desktop Linux, Claude Desktop/Code, Google Antigravity desktop/CLI, Grok Build, Codex CLI, Chrome, Git/GitHub CLI, Go, Python, Node, Docker Engine/Compose, age, tmux, and guest utilities from current first-party sources. An optional tool that cannot provide a verifiable pinned ARM64 artifact is recorded unavailable for the revision, not installed by weakening policy.
4. Record the official ChatGPT Desktop Linux Ubuntu 24.04 ARM64 artifact and XWayland requirements. General Computer Use remains unavailable on Linux unless current official documentation and acceptance tests change that; browser actions do not imply desktop control.
5. Make availability per golden revision explicit. Core M1A architecture acceptance is separate from the availability of every optional vendor application: a tool whose ARM64 artifact fails qualification is marked unavailable for that revision rather than installed through a weaker path or treated as a failure of the isolation architecture.

**Verification:**

```bash
go test ./internal/golden -race
bin/boxwarden --domain test golden inspect-lock --lock config/golden.lock.json
git diff --check
```

## Task 4: Build and validate the portable guest definition

**Files:**

- Create: `cmd/boxwarden-guest/main.go`
- Create: `internal/guestcmd/guestcmd.go`
- Create: `internal/guestcmd/guestcmd_test.go`
- Create: `guest/ubuntu-24.04-arm64/provision/00-base.sh`
- Create: `guest/ubuntu-24.04-arm64/provision/10-desktop.sh`
- Create: `guest/ubuntu-24.04-arm64/provision/20-docker.sh`
- Create: `guest/ubuntu-24.04-arm64/provision/30-ai-tools.sh`
- Create: `guest/ubuntu-24.04-arm64/provision/40-hardening.sh`
- Create: `guest/ubuntu-24.04-arm64/provision/90-finalize.sh`
- Create: `guest/ubuntu-24.04-arm64/systemd/boxwarden-firstboot.service`
- Create: `guest/ubuntu-24.04-arm64/tests/golden.sh`
- Create: `guest/ubuntu-24.04-arm64/tests/network.sh`
- Create: `guest/ubuntu-24.04-arm64/tests/identity.sh`

1. Write guest-helper tests for strict stdin JSON, bounded inputs, fixed verbs, safe paths, and no arbitrary command execution. Cross-compile the helper as Linux/ARM64 and install it in the guest definition.
2. Make provisioning idempotent and driven only by the lock/manifests. Verify signatures/digests before install. Disable unattended upgrades, PackageKit/GNOME installation, Snap/vendor/CLI self-updaters where applicable, and record the resulting BOM.
3. Configure Ubuntu's supported desktop/Wayland path and XWayland; do not force Xorg. Keep every GUI profile/login inside the guest.
4. Configure Docker guest-local only. Do not install a host context/socket. Treat the Docker group as guest root. Default service examples bind `127.0.0.1`; add Docker-compatible `DOCKER-USER` rules and tests proving no accidental public bind.
5. Configure generic strict `sshd` policy with fixed inactive bootstrap target
   paths, restricted principals, no root/password/X11/agent/TCP/tunnel
   forwarding, and management access only through the intended guest firewall
   rule. Neither a domain public anchor nor a private key enters the image.
6. Configure inbound deny except SSH, the DNS behavior proven necessary by Task 0, no host/private-network routes beyond the approved M1A model, and no autostarted provider agent, MCP server, extension, browser extension, hook, or scheduled agent.
7. Finalize clone identity exactly as proven in Task 0. Fail the build if login/provider/browser/project/history/cache/session residue, cookies, keyring entries, tokens, private keys, or reusable machine identity remains.
8. Test installed versions/BOM, desktop/XWayland, ChatGPT Desktop launch, all available provider tools, Docker isolation, SSH policy, firewall behavior, no listeners beyond the allowlist, no automatic updater, no forbidden integration, and clone-ready identity. Snapshot package/app versions before reboot and GUI/CLI launch, repeat afterward, and fail on unapproved mutation.
9. Apply and test the definition through the Task 0 bootstrap harness or another explicitly recorded direct guest-build path that does not depend on the not-yet-implemented golden builder. Task 5 consumes the resulting complete definition; Task 4 must not call forward into Task 5.

**Verification:**

```bash
go test ./internal/guestcmd
GOOS=linux GOARCH=arm64 go build -o build/boxwarden-guest ./cmd/boxwarden-guest
guest/ubuntu-24.04-arm64/tests/golden.sh
guest/ubuntu-24.04-arm64/tests/network.sh
guest/ubuntu-24.04-arm64/tests/identity.sh
```

## Task 5: Build, validate, and promote immutable generic goldens

**Files:**

- Create: `internal/golden/build.go`
- Create: `internal/golden/build_test.go`
- Create: `internal/golden/promote.go`
- Create: `internal/golden/promote_test.go`

1. Build a generic candidate from the qualified lock and complete portable
   guest-definition digest. Candidate metadata records host/backend type, exact
   qualified Tart + Softnet pair, lock digest, guest-definition digest,
   artifact ID, BOM, tests, and human-acceptance status. Domain-specific trust
   is added only to fresh clones through amended ADR 012's serial bootstrap.
2. Refuse a candidate whose host-tool pair differs from the qualified Task 0 pair or whose guest definition, lock, BOM, identity-finalization evidence, or security-property tests are incomplete.
3. Promote only a stopped, validated, human-accepted candidate. Use a domain golden lock and atomic pointer-file replacement. Never rename or mutate a `current` VM. Session creation holds the same lock while resolving one immutable revision.
4. Test power loss before/after candidate creation, validation recording, pointer fsync/rename, and concurrent create/promote. A failed promotion leaves the previous pointer intact.
5. Keep optional-tool availability in candidate evidence. A missing optional vendor application does not weaken or invalidate core isolation acceptance when the lock correctly records it unavailable.

**Verification:**

```bash
go test ./internal/golden -race
bin/boxwarden --domain test golden build --lock config/golden.lock.json
bin/boxwarden --domain test validate golden <candidate-id>
git diff --check
```

## Task 6: Implement the reconciled session lifecycle and unique identity

**Files:**

- Create: `internal/session/registry.go`
- Create: `internal/session/registry_test.go`
- Create: `internal/session/name.go`
- Create: `internal/session/name_test.go`
- Create: `internal/lifecycle/state.go`
- Create: `internal/lifecycle/reconcile.go`
- Create: `internal/lifecycle/reconcile_test.go`
- Create: `internal/lock/filelock.go`
- Create: `internal/lock/filelock_test.go`
- Create: `internal/session/service.go`
- Create: `internal/session/service_test.go`

1. Define persisted intent states `creating`, `stopped`, `starting`, `running`, `stopping`, `deleting`, and `failed`. Each record contains format version, domain, session UUID/name, mode, backend type/object ID, immutable golden revision, the exact optional private-network CIDR allowlist, registered projects, profile receipts, timestamps, last observation/error, and only the process identity/observations Task 0 proved safe for graphical reconciliation. A reusable bare PID is insufficient. Checkpoint state is not part of M1A.
2. Use one domain/session lock for mutation and a domain golden lock for resolution/promotion. Persist and fsync intent before backend calls; atomically replace state after each confirmed transition. Never infer a destructive target from a display name alone.
3. Implement create as: validate domain/name/mode and each explicitly requested private CIDR; reject broad, non-private, host/vmnet/session-overlapping, or isolation-breaking entries; canonicalize and persist the exact allowlist in intent before launch; lock; resolve golden; reserve UUID/backend ID; record `creating`; clone; configure; `tart set <vm> --random-mac`; boot first-init; prove fresh identity; detect the host's current IANA time zone; apply it through the bounded guest-management path; verify the effective guest zone; stop if requested; record actual state. Retrying after any failure reconciles instead of duplicating. Status must display the exception, and later starts must use only the recorded policy rather than accepting silent drift.
4. Implement start/stop/status/reconcile using backend observations. Every start redetects, reapplies, and verifies the host's current IANA time zone before `running`/ready is reported; detection, application, or verification failure produces an actionable failed/partial state rather than a UTC fallback. Report `partial`, `orphan-record`, `orphan-backend-object`, and `identity-mismatch` explicitly. Do not auto-delete an object absent from the registry.
5. Test malicious path-like, Unicode-confusable, control-character, overlong, and option-shaped session names plus failures before and after every external operation, process crash between intent and result, lock contention, duplicate command retries, manual stop/delete, stale/reused process identity, backend timeout, domain mismatch, golden-promotion race, host time-zone changes between starts, invalid/unresolvable zones, guest apply/readback mismatch, and two concurrently created clones with unique identity.

**Verification:**

```bash
go test ./internal/session ./internal/lifecycle ./internal/lock -race
go test ./...
```

## Task 7: Add convenient SSH discovery without host integration

**Files:**

- Create: `internal/sshx/ca.go`
- Create: `internal/sshx/ca_test.go`
- Create: `internal/sshx/connect.go`
- Create: `internal/sshx/connect_test.go`
- Create: `internal/sshx/knownhosts.go`
- Create: `internal/sshx/knownhosts_test.go`
- Create: `scripts/ssh-session.sh`

1. Generate each domain's SSH user CA private key only at explicit domain init,
   with `0600`/`O_EXCL`, outside repo/profile/backend roots. Keep its public
   anchor in host domain state for post-clone serial bootstrap; never put it in
   generic golden inputs. Refuse shared CA-private paths across domains.
2. Issue short-lived, session-UUID/principal-bound OpenSSH user certificates. Do not copy a reusable private key into the guest and do not use the user's SSH agent.
3. Resolve the address through `Backend.ManagementAddress` using the
   Task-0-qualified DHCP resolver behavior, refresh it according to the observed
   lease/cache limits, and use only ADR 017's trusted serial bootstrap to obtain
   and pin the regenerated SSH host key in domain/session-specific host state.
   TOFU and `StrictHostKeyChecking=no` are prohibited.
4. Execute `ssh` with explicit identity/certificate/known-host paths and `-o ForwardAgent=no -o ForwardX11=no -o ClearAllForwardings=yes -o PermitLocalCommand=no`. The convenience command supports a shell, tmux, logs, Git, and Docker inspection but no host forwarding.
5. Test wrong domain, expired certificate, changed host key, address reuse, missing state, multiple addresses, timeout, and forbidden user-supplied SSH options.

**Verification:**

```bash
go test ./internal/sshx -race
bin/boxwarden --domain test session ssh <session> -- uname -m
```

## Task 8: Implement encrypted, reviewed declarative profiles

**Files:**

- Create: `internal/profile/manifest.go`
- Create: `internal/profile/manifest_test.go`
- Create: `internal/profile/candidate.go`
- Create: `internal/profile/candidate_test.go`
- Create: `internal/profile/store.go`
- Create: `internal/profile/store_test.go`
- Create: `internal/profile/adapter/adapter.go`
- Create: `internal/profile/adapter/validate.go`
- Create: `internal/profile/adapter/validate_test.go`
- Create: `internal/profile/gitprefs/gitprefs.go`
- Create: `internal/profile/gitprefs/gitprefs_test.go`
- Create: `internal/profile/markdown/markdown.go`
- Create: `internal/profile/markdown/markdown_test.go`
- Create: `config/profiles/development.example.json`

1. Define independent CONFIDENTIALITY and EXECUTION TRUST enums. Each compiled adapter policy fixes source/destination paths, regular object types, owner/group, modes, file/count/byte limits, classification, application-quiescence protocol, semantic diff, validation, staged apply, and rollback. Reject secrets, executable, opaque/unknown, unregistered adapters, unknown manifest fields, and manifests that differ from compiled policy.
2. Use a canonical versioned JSON envelope, not a general tar/archive extractor. Entries contain only adapter-approved relative paths, regular-file modes, hashes, and bounded content. Trusted-host code parses and validates the exact received bytes and rejects absolute paths, `..`, empty/duplicate/case-colliding paths, symlinks, hard links, devices, sockets, setuid/setgid, non-regular files, destination escapes, excessive count, expanded-size bombs, schema violations, and adapter semantic violations. Guest validation may fail early but is never authoritative. Adapter allowlists are the primary exclusion boundary; explicit rejection of canonical Kindex paths and aliases is defense in depth, not a substitute for allowlisting.
3. Implement `git-preferences-v1` as a fixed schema of non-secret preference keys. Reject aliases, includes, credential helpers, filters, hook paths, SSH commands, signing keys, arbitrary sections, and executable configuration.
4. Implement `sensitive-markdown-v1` as bounded UTF-8 Markdown under its adapter-owned destination. Reject non-Markdown payloads and modes other than the fixed safe file/directory modes.
5. Stream captured adapter output to the trusted host. Before encryption or candidate creation, re-perform every structural and semantic admission check over those exact bytes. Encrypt immediately to the selected domain's daily and offline recipients. Persist only immutable ciphertext, normalized non-secret manifest, and review metadata; use exclusive creation and never persist a plaintext candidate.
6. `profile inspect` verifies and decrypts the candidate for a transient semantic diff without writing plaintext. Its renderer never sends raw candidate bytes or rich Markdown to the terminal: escape C0/C1 and ANSI/OSC sequences; visibly mark bidi and zero-width/format controls; show byte counts, explicit truncation, and the normalized-manifest/ciphertext digests beside the material. Do not add a general Unicode-confusable framework in M1A. `profile approve` binds domain, profile, adapter/version, recipients, destinations, normalized-manifest digest, and ciphertext digest. It atomically promotes those exact bytes; it never re-captures/re-encrypts mutable guest state.
7. Restore verifies receipt/digests/domain and re-validates the exact envelope on the trusted host before decryption, streams to a fresh guest staging directory, validates through `boxwarden-guest` as a guest correctness check, applies atomically or with same-filesystem rollback, validates after apply, and removes staging/rollback data. Never extract directly into a live home.
8. Test a deliberately hostile guest-produced envelope plus tampering, replay across domain/profile/recipient/destination/adapter version, wrong identity, missing recovery recipient, interrupted encryption, partial store write, concurrent approval, required-quiescence failure, ownership/mode mismatch, restore validation failure, apply failure, rollback failure reporting, symlink races, decompression-free size limits, terminal escape/OSC payloads, bidi and zero-width/format controls, truthful truncation/byte counts, and exact round trips for both adapters.
9. Add the explicit status text: `Kindex full state: UNSUPPORTED`. If `kin export` is ever exposed, name it `kindex interchange export`; it is outside profile backup/capture.

**Verification:**

```bash
go test ./internal/profile/... -race
bin/boxwarden --domain test profile inspect <candidate>
bin/boxwarden --domain test profile approve <candidate> --manifest-sha256 <digest> --ciphertext-sha256 <digest>
```

## Task 9: Enforce project durability, modes, credentials, and provider scope

**Files:**

- Create: `internal/project/model.go`
- Create: `internal/project/check.go`
- Create: `internal/project/check_test.go`
- Create: `internal/secret/inject.go`
- Create: `internal/secret/inject_test.go`
- Create: `internal/session/policy.go`
- Create: `internal/session/policy_test.go`
- Create: `docs/operations/credential-runbook.md`
- Create: `docs/operations/project-durability.md`

1. Register every important project with domain, session UUID, canonical guest path, type, and durability policy. Guest Git checks cover modified/untracked files, upstream presence, and ahead/unpushed commits. Where the configured policy has a credential-free or already-authorized safe host path, the trusted host corroborates remote reachability and the guest-claimed pushed commit. Do not provision new trusted-host Git credentials merely for this guard. Non-Git state is either externally durable with adapter-specific evidence or explicitly disposable.
2. Normal destroy explicitly labels guest-reported and host-corroborated facts. It blocks on unknown/unregistered durable state, unverifiable guest, dirty/untracked files, unpushed commits, missing upstream, unreachable required remote, failed required host corroboration, or failed non-Git evidence. Document that this is a safety guard against accidental loss, not a security control against a hostile guest. `--allow-data-loss` requires exact domain/session plus a typed acknowledgement and is logged.
3. `--compromised` is a separate host-only containment path: no guest inspection, credential/profile injection, push, or rescue. It revokes no credential automatically, but prints the domain/session credential inventory and ordered revocation actions before exact recorded backend deletion.
4. Enforce clean interactive and quarantine policy centrally. Quarantine rejects normal profile restore, provider/Git secret injection, and write-capable reusable ingress. Document that manual GUI login remains possible and defeats the intended posture. Checkpoint policy is absent because the feature is deferred from M1A.
5. Inject bounded secrets only from explicit domain configuration or stdin, over SSH stdin or guest GUI authentication. Never use argv, environment inherited by unrelated children, shell history, profiles, logs, or state records. Browser/provider profiles remain disposable guest state.
6. Warn when more than one provider is enabled because all provider sessions and restored data share one guest compromise domain. Do not claim Unix users isolate providers while Docker/root-equivalent access exists.
7. Test all destroy blockers and overrides; guest-report versus host-corroboration output; policies that require corroboration when no authorized host path exists; compromised mode with a dead/hostile guest; cross-domain project/credential references; quarantine manual-login warning; secret redaction; no-agent/no-forward SSH; multi-provider warning; and non-Git durable/disposable policies.

**Verification:**

```bash
go test ./internal/project ./internal/secret ./internal/session -race
go test ./...
```

## Task 10: Provide the routine UX, validation matrix, and recovery drills

**Files:**

- Create: `internal/validate/host.go`
- Create: `internal/validate/golden.go`
- Create: `internal/validate/session.go`
- Create: `internal/validate/properties.go`
- Create: `internal/validate/validate_test.go`
- Create: `scripts/build-golden.sh`
- Create: `scripts/create-session.sh`
- Create: `scripts/start-session.sh`
- Create: `scripts/status.sh`
- Create: `scripts/destroy-session.sh`
- Create: `guest/ubuntu-24.04-arm64/tests/acceptance.sh`
- Create: `docs/operations/recovery.md`
- Create: `docs/evidence/m1a-acceptance-template.md`

1. Make each wrapper a minimal `exec` of `bin/boxwarden`; require `BOXWARDEN_DOMAIN` or an explicit domain argument. The binary remains the authoritative interface.
2. Implement property-oriented host validation: exact supported macOS/ARM64 and qualified Tart + Softnet pair, recorded graphical/Aqua prerequisite, safe roots/modes, distinct domain material, lock validity, age identities/recipients, SSH CA, backend availability, and no symlink/root overlap.
3. Implement golden validation: immutable/stopped candidate, guest-definition/lock/BOM digests, no identity/session residue, updater disablement, tool versions, desktop/XWayland, SSH/firewall/Docker properties, forbidden integrations absent, GUI applications launch, and two-clone identity uniqueness.
4. Implement session validation/status: intent versus backend and Task-0-qualified process observations, domain/golden/mode, identity, refreshed address/SSH pin, providers, profiles, registered projects/durability with trust basis, listeners/firewall, and actionable partial/orphan state. Treat ambiguous or reused process identity as non-running/failed reconciliation evidence rather than signaling it.
5. Repeat the Task 0 core network properties as regression tests: public Internet, inherited host/VPN DNS, qualified work-VPN/scoped-DNS behavior, default guest→private-network denial, session→session denial, required host→guest SSH, DHCP, and address refresh. Report the ADR 020 environment matrix and refuse to claim effectively IPv6-only support while its rows remain unqualified. Test an explicitly allowlisted private CIDR positively and prove a neighboring unlisted private CIDR plus every concurrent Boxwarden session remains denied. Assert and report ADR 015's accepted vmnet-gateway reachability rather than claiming guest→host denial. Also run inbound scans from reachable host context, Docker-publish tests, forbidden SSH forwarding tests, and assertions that no host share/socket/display/clipboard/audio path exists. Keep exact qualified Tart-argv tests as adapter tests, not the sole evidence.
6. Add fault-injection tests around state writes and every backend operation. Kill the process during create/start/stop/destroy/promote/profile approve/restore, rerun, and prove reconciliation is safe and idempotent.
7. Exercise compromise recovery: revoke test credentials, destroy without trusting guest, prove no export/profile writeback occurred, and recreate from promoted golden. Exercise suspect-golden retirement and rollback to the previous domain pointer.
8. Re-measure routine UX. Target clone/create under 60 seconds, SSH-ready under 90 seconds, destroy under 30 seconds on the reference Mac unless Task 0 establishes a justified budget. Report actual p50/max; do not fake pass/fail around machine variance.
9. Perform human GUI acceptance for every vendor application marked available in the candidate revision, including ChatGPT Desktop login/launch and browser actions, plus XWayland behavior, Tart console input, and guest-contained profiles. Preserve ChatGPT Desktop, Claude Desktop/Code, Antigravity desktop/CLI, Grok Build, and Codex CLI as intended qualification targets, but record a tool that fails immutable ARM64 qualification as unavailable for that revision rather than weakening installation policy or treating core isolation acceptance as failed. Record General Computer Use and Cowork as unsupported M1A capabilities, not failed hidden tests.

**Expected user workflow:**

```bash
export BOXWARDEN_DOMAIN=personal
./scripts/build-golden.sh
./scripts/create-session.sh example-project
./scripts/start-session.sh example-project
./scripts/ssh-session.sh example-project
./scripts/status.sh example-project
./scripts/destroy-session.sh example-project
```

**Verification:**

```bash
go test -race ./...
go vet ./...
go build -o bin/boxwarden ./cmd/boxwarden
GOOS=linux GOARCH=arm64 go build -o build/boxwarden-guest ./cmd/boxwarden-guest
bin/boxwarden --domain test validate host
bin/boxwarden --domain test validate golden <golden-revision>
bin/boxwarden --domain test validate session <session>
git diff --check
```

## Task 11: Final audit and milestone handoff

**Files:**

- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/*.md`
- Modify: `docs/decisions/*.md`
- Create: `docs/evidence/m1a-final-report.md`

1. Trace every canonical invariant to implementation and at least one positive or negative test. Fail the audit for untested destructive, domain, profile, network, identity, or reconciliation rules.
2. Search for pre-Boxwarden product/executable/runtime/module/repository identifiers, generic opaque/stateful profile support, Kindex backup claims, implicit default domains, mutable current-golden VM names, host shares, forbidden forwarding, automatic updates, and Tart policy leaking into common packages.
3. Confirm every optional tool is either installed from a qualified lock entry and accepted or reported unavailable. Do not silently omit or substitute a community wrapper.
4. Confirm `git status` contains no key, identity, token, browser profile, candidate plaintext, runtime state, generated Tart object, or local evidence secret.
5. Run full tests twice, then execute one complete interactive lifecycle, one quarantine rejection path, one normal destroy blocker, one compromised destroy, one profile round trip, two-clone uniqueness, one session-isolation test, and one golden rollback.
6. Record actual versions, evidence, latency, limitations, and recovery results. Stop for approval before calling M1A operational.

## Milestone 1B gate: Kindex full-fidelity persistence

M1B is independent and requires separate approval. `boxwarden` must continue to report `Kindex full state: UNSUPPORTED` until Kindex itself provides a documented application-controlled operation equivalent in capability to backup, verify, and restore.

The supported Kindex facility must quiesce writers and create a transactionally consistent full-fidelity snapshot under Kindex control; preserve graph and companion application state; include integrity hashes plus schema/Kindex/profile versions; restore into an empty profile at the same pinned Kindex version; validate a full round trip; and demonstrate forward migration on a scratch restored copy before promotion of a new Kindex version. External SQLite/WAL copying is prohibited. `kin export` remains intentional interchange and is never described or tested as backup.

## Explicitly deferred

- Linux-host, KVM, libvirt, cloud, and bootc backends;
- a generic hypervisor feature framework;
- generic opaque/profile filesystem artifacts;
- browser/provider session persistence;
- Kindex capture/restore or persistent Kindex service;
- checkpoint creation, resume, lifecycle state, retention, age warnings, backend operations, and tests;
- host filesystem/clipboard/audio/display/Docker integration;
- Tart port exposure, broad or implicit private-network exceptions, nested virtualization, and Claude Cowork;
- quarantine export/rescue;
- remote profile store and enterprise domain/identity administration;
- automatic profile writeback, golden mutation, or application updates.
