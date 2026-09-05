# Boxwarden assurance

This document records the evidence basis for Boxwarden's security and trust
claims. It uses a consistent evidence vocabulary and tracks the current status
of each material claim.

Claims in this document draw on evidence from several source scopes:
- **Current tree:** code, tests, and documents landed in this integration and
  intended for `main`.
- **Historical qualification evidence on `main`:** Task 0 and independent architecture
  review evidence committed to `main`.
- **Future design:** normative V4 supervisor/broker and lifecycle design that is
  not current operational behavior and still requires implementation and
  qualification.
- **Pending:** the required evidence does not yet exist in any branch.

V3 implementation, deterministic tests, runbooks, and the completed
host/domain attended-evidence record are current-tree evidence. Their presence
does not promote a distinct pending runtime or V4 claim.

Security properties are not uniformly "tested" or "qualified." This document
distinguishes the specific evidence basis for each claim rather than collapsing
evidence into a single label. A claim may simultaneously have an accepted design
basis, deterministic test coverage, independent review, and real-host
qualification. The dimensions are orthogonal; any combination is possible.

"Pending" means the required evidence does not yet exist. This is honest
accounting, not a failure. Do not promote a pending claim to qualified status
merely because implementation looks correct; the distinction only matters when
it is maintained consistently.

A Pending attended qualification limits the assurance claim; it is not, by
itself, an absolute Ready or merge blocker for deterministically verified code
when no known unsafe defect exists. Whether the foundation code may merge and
whether Boxwarden is ready for general use or production are separate decisions.

## Evidence dimensions

### Accepted design basis

The claim is required by an accepted architecture decision record (ADR) or a
canonical design document, and the implementation mechanism that enforces it
is identified. This establishes intent and implementation approach but is not
by itself empirical evidence that the mechanism works as intended.

_Citing requirement:_ name the ADR or document and the implementation mechanism.

### Deterministically verified

The property is enforced and exercised through the project's automated test
suite — unit tests, integration tests, adversarial/fault-injection tests,
architecture boundary tests, build checks, or static analysis — running in CI
without a qualified real host. These tests run deterministically and cannot be
replaced by human observation.

_Citing requirement:_ name the specific test file, function, or category.
Note any same-UID test-model gap where the test proves logic but not
cross-privilege real-host semantics.

### Independently reviewed

A named reviewer with a stated scope examined the implementation, evidence
document, or design and produced a dated written assessment committed to the
repository.

_Citing requirement:_ name the review document with its date and the reviewed
commit.

### Real-host qualified

The behavior was directly observed in an attended procedure on an exact stated
platform and toolchain combination. The evidence is committed to the repository.
A separate earlier gate may be cited as the source for a specific property if
that gate document explicitly records that property and the qualifying
conditions have not changed.

_Citing requirement:_ name the evidence document, the exact host OS/build,
toolchain versions and digests, and which specific observation within that
document supports the claim.

### Evidence provenance — inherited

The claim relies on an earlier qualification for a component or platform pair
that has not been re-exercised in the current gate, but the earlier evidence
explicitly covers the relevant fact and the qualifying conditions are unchanged.
The source and the reason the inheritance is valid must both be named.

### Scope — pending

Designed or implemented, but the required evidence does not yet exist. The
missing evidence is named. Do not use this label as a synonym for "not
important."

### Scope — not claimed / accepted limitation

The property is explicitly outside the current trust model, or the limitation
has been documented as an accepted design trade-off.

_Citing requirement:_ name the ADR or document that records the explicit
acceptance.

---

## Qualification scope and policy

"Qualified" means a property was directly observed in an attended procedure on
an exact stated platform, and the evidence is committed to this repository. It
is not synonymous with "the tests pass" or "the design requires it."

A security-relevant change to a qualified component requires a qualification-
impact assessment before the change is accepted. A change to a component whose
qualification is explicitly pair- or version-bound (such as the Tart/Softnet
pair) requires the corresponding requalification for the bound properties before
those properties can be claimed as qualified for the new version.

### Qualified platform

| Dimension | Value |
|---|---|
| Host OS | Apple Silicon (arm64), macOS 26.6.2 build 25G83 |
| VM backend | Tart 2.32.1; executable SHA-256 `05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d`; archive SHA-256 `8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529` |
| Network shim | Softnet 0.19.0; executable SHA-256 `ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e`; archive SHA-256 `1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c` |
| Serial holder | GNU Screen 4.00.03 (FAU, 23-Oct-06); SHA-256 `07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add` |
| Guest OS | Ubuntu 24.04.4 Desktop ARM64; ISO SHA-256 `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe` |

Tart and Softnet are qualified as one pair. A change to either requires
requalification of all properties whose qualification is explicitly attributed
to the pair.

### Qualification gates

**Task 0 (PASS WITH CONDITIONS):** Core network isolation, clone identity,
management SSH path, and serial recovery. Three network environments remain
`NOT YET PROVEN` (`ipv6_only_upstream`, `ipv4_only_destination`,
`ipv6_only_destination`). Task 0 used a foreground harness, not the V4
supervisor; properties it observed must be requalified if the V4 broker changes
the relevant execution path.

Evidence: [`docs/evidence/m1a-task0-final-summary.md`](evidence/m1a-task0-final-summary.md)

**V3 attended host/domain init gate:** Installation of the exact qualified
Softnet artifact in a root-owned digest-specific path, doctor behavior, domain
init separation, unsafe Homebrew detection and init refusal, legacy manifest
migration. Tested at code commit `36839dc7`; binary SHA-256 `242fb3e0`.

Softnet runtime behavior (privilege transition, closed-environment execution,
signal handling, network behavior) is not part of this gate and remains pending.

Evidence: [`docs/evidence/v3-host-domain-attended-gates.md`](evidence/v3-host-domain-attended-gates.md)

**Independent architecture review (2026-08-30):** Reviewed commit `9ba73679`.
Verdict: IMPLEMENT WITH CONDITIONS. Scope: architecture, threat model, security
design, state/persistence/lifecycle models. Eight prior concerns addressed.
BLOCKER-1 (vmnet gateway) resolved by ADR 015 acceptance.

Evidence: [`docs/reviews/2026-08-30-independent-architecture-review.md`](reviews/2026-08-30-independent-architecture-review.md)

### Pending gates

- **Softnet runtime gate (S10–S13):** Softnet privilege transition/drop,
  closed-environment dependency resolution, signal/filesystem/network behavior.
  Partial attended runtime work was performed and produced non-final forensic
  evidence; those runs are sealed as forensic evidence and may not serve as
  qualification continuations. The complete fresh-run qualification remains
  pending; no completed runtime result is claimed. Observer and procedure are
  documented in the current tree (`docs/operations/adr024-runtime-observer.md`).
- **V2 real-host register/clone gate:** Requires an artifact built or rebuilt
  from the corrected generic guest definition (no embedded domain CA). An
  unchanged Task 0 artifact built under the domain-bound design is not
  grandfathered.
- **ADR 017 requalification:** V4 replaces the Task 0 socat harness with a
  supervisor-owned broker. The two-PTY relay, Screen retention/exit/cleanup, and
  serial bootstrap behavior must be re-exercised for the production
  implementation.

---

## Claim inventory and assurance matrix

The table below uses abbreviated dimension codes. A cell may contain multiple
codes when the evidence is multi-dimensional.

| Code | Meaning |
|---|---|
| **D** | Accepted design basis |
| **T** | Deterministically verified |
| **R** | Independently reviewed |
| **Q** | Real-host qualified |
| **I** | Evidence provenance — inherited from earlier gate |
| **P** | Pending |
| **N** | Not claimed / accepted limitation |

### Host isolation

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| H1 | No trusted-host filesystem tree is mounted into a guest | D, T | `docs/architecture.md`; backend seam test rejects all sharing flags; `internal/architecture/backend_seam_test.go` | ADR 021 is **PROPOSED**, not accepted; no host-tree capability exists |
| H2 | No host Docker/Podman/containerd socket or context exposed to guest | D, T | `docs/architecture.md`; backend seam test | No attended runtime proof; V4 launch pending |
| H3 | No host SSH-agent forwarding to guest | D, T | `docs/architecture.md`; `docs/credentials.md`; sshx client test: `IdentityAgent=none`, `ForwardAgent=no` | — |
| H4 | No host display-server (X11/Wayland/VNC) to guest | D, T | `docs/architecture.md`; backend seam test | No attended runtime proof; V4 launch pending |
| H5 | No clipboard or audio sharing by default | D, Q, I | Design basis: `docs/architecture.md`; Task 0 qualified `--no-audio --no-clipboard` in all launches | V4 supervisor must requalify (ADR 017 requalification pending) |
| H6 | No bridged or host networking | D, T | `docs/architecture.md`; ADR 003 (superseded by ADR 015 for network policy); backend seam test | No attended runtime proof; V4 launch pending |
| H7 | No port exposure | D, T | `docs/architecture.md`; backend seam test | — |
| H8 | No nested virtualization | D, T | `docs/architecture.md`; backend seam test | — |
| H9 | No Rosetta share | D, T | `docs/architecture.md`; backend seam test | — |
| H10 | The serial PTY is not reachable through guest networking | D, Q, I | Design basis: `docs/security-model.md`, `docs/decisions/017-host-local-serial-recovery-shell.md`; Task 0 qualified PTY isolation | V4 supervisor uses a different broker; ADR 017 requalification pending |
| H11 | Guest-local runtimes (Docker group, etc.) are not treated as a substitute for VM isolation | D | `docs/security-model.md` | — |
| H12 | `--net-softnet-allow=0.0.0.0/0` is prohibited | D, T | `docs/architecture.md`; V4 launch test: rejects every allow flag | Also disables bridge isolation; prohibition documented and tested |

### Network isolation

The Softnet policy has been exercised on the exact qualified platform. These
claims describe what was directly observed in Task 0 for that pair. They do not
assert general guest-to-host isolation.

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| N1 | Guest cannot initiate connections to private/link-local destinations by default | D, Q | `docs/decisions/015-network-compatibility-before-host-gateway-isolation.md`; Task 0 controlled network probes: `docs/evidence/m1a-task0-final-summary.md` | Vmnet gateway remains reachable (N3) |
| N2 | Concurrent sessions cannot initiate connections to one another | D, Q | Task 0 two-clone TCP/22 test: `docs/evidence/m1a-task0-final-summary.md`; `docs/security-model.md` | Qualified via Softnet bridge isolation default; `--net-softnet-allow=0.0.0.0/0` would defeat it (prohibited) |
| N3 | The vmnet gateway is reachable from a guest | N | ADR 015 explicit acceptance: `docs/decisions/015-network-compatibility-before-host-gateway-isolation.md` | Required for VPN/DNS compatibility; M1A does not claim guest-to-host network isolation |
| N4 | Softnet enforces anti-spoofing: guest may only send from its own leased MAC and DHCP-assigned IP | Q | Task 0 MAC+IP spoofing experiment: `docs/evidence/m1a-task0-final-summary.md` | MAC and IP enforcement confirmed; no port-level enforcement |
| N5 | VPN and scoped/split-DNS behavior is compatible with the qualified Softnet policy | Q | Task 0 supplemental run on a separate authorized work Mac with Tart 2.32.1 + Softnet 0.19.0: `docs/evidence/m1a-task0-final-summary.md`, `docs/evidence/m1a-work-vpn-network-validation.md` | That supplemental guest was not the final Task 0 golden; qualifies the host-side vmnet/DNS path |
| N6 | IPv6-only upstream behavior and dependent destination cases | N, P | ADR 020: `docs/decisions/020-separate-platform-and-environment-qualification.md` | `ipv6_only_upstream`, `ipv4_only_destination`, `ipv6_only_destination` remain `NOT YET PROVEN`; these are unqualified environments, not failures |

### Softnet privilege binding (ADR 024)

ADR 024 is accepted. The V3 attended gate qualified the installation and
detection properties, with its evidence in the current tree. The runtime
properties (S10–S13) remain pending.

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| S1 | Softnet privilege is bound to an exact root-owned digest-specific artifact, not a mutable Homebrew path | D, Q | ADR 024; V3 attended evidence: exact SHA-256, mode `04550`, path, ancestry |  |
| S2 | Any setuid/setgid or passwordless-root Homebrew Softnet causes doctor to report `drifted/unsafe` and blocks init | D, T, Q | ADR 024; `internal/hostx/doctor_test.go`; V3 attended unsafe-Homebrew refusal | V3 has no `session start`; start-path refusal awaits V4 |
| S3 | `boxwarden init` refuses a source with any setuid/setgid bit | D, T | ADR 024; `internal/hostx/root_install_test.go` | Same-UID test model; real-host cross-UID semantics confirmed at the V3 gate |
| S4 | The installed Softnet executable is SHA-256 `ab333619…`, mode `04550`, one link, root-owned, assigned to `boxwarden-operators` | Q | V3 attended evidence: exact inode, link count, ACL absence table | Specific to macOS 26.6.2 / Tart 2.32.1 / Softnet 0.19.0 |
| S5 | The manifest is `root:wheel 0444`; doctor reads and parses it without privilege | D, T, Q | ADR 024 manifest contract; V3 evidence: `sudo -u nobody` SHA-256 read, healthy doctor post `sudo -k` | The `0444` contract corrects the historical `0400`; the exact attended migration is preserved in V3 evidence |
| S6 | Tart's launch PATH equals only the digest-specific Softnet directory; no ambient proxy, DYLD, telemetry, or loader variables survive | D, T | ADR 024; current-tree launch construction tests | V4 real-host launch remains pending |
| S7 | Doctor diagnoses the full tree: path, ancestors, ACLs, symlinks, digests, modes, group, manifest | D, T, Q | ADR 024; `internal/hostx/doctor_test.go`; V3 evidence: full production inspection post-install | Deterministic tests use same-UID synthetic root; cross-UID semantics confirmed at V3 gate |
| S8 | Doctor is read-only; it never repairs, re-authorizes, or silently mutates | D, T, Q | `docs/operations/init-and-doctor.md`; `internal/hostx/doctor_test.go`: zero-mutation assertions; V3 evidence: post-`sudo -k` doctor | — |
| S9 | Unsafe Homebrew Softnet blocks `boxwarden init` before any trusted-host mutation | D, T, Q | ADR 024; `internal/hostx/init_test.go`; V3 attended unsafe-Homebrew refusal | Start-path blocking awaits V4 |
| S10 | Softnet runtime privilege transition: effective UID 0 after Tart exec's the `04550` binary | P | ADR 024; current-tree observer and procedure | Partial attended runtime work is non-final forensic evidence; complete fresh-run qualification is pending; observer sampling is not lossless |
| S11 | Softnet privilege drop after vmnet setup | P | ADR 024 | Complete fresh-run qualification remains pending |
| S12 | Softnet closed-environment dependency resolution | P | ADR 024 | Complete fresh-run qualification remains pending |
| S13 | Softnet signal behavior, filesystem writes, and qualified launch network behavior | P | ADR 024 | Complete fresh-run qualification remains pending |

### Management SSH and identity

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| M1 | One management CA per domain; `domain init` creates it explicitly and cannot create it lazily from session start | D, T, Q | ADR 012; `docs/credentials.md`; `internal/sshx/ca_test.go`; V3 evidence: two distinct CAs created, no lazy CA path exercised | — |
| M2 | CA private key never enters a guest, golden, repository, argv, or log | D, T | `docs/credentials.md`; `internal/sshx/ca_test.go`: private key bytes never appear in any output | No negative test that actually extracts key bytes from a compromised guest |
| M3 | Generic golden contains no domain CA anchor and no fixed domain principal | D, T | `docs/architecture.md`; `docs/lifecycle-and-recovery.md`; V2 golden test (current tree): no CA state in golden | V2 real-host register/clone gate is pending (requires artifact from corrected generic guest definition) |
| M4 | `domain init` does not install or modify host-global prerequisites; host toolchain unchanged after domain init | D, T, Q | `docs/operations/domain-init.md`; V3 evidence: host snapshot identical before and after domain init | — |
| M5 | `domain init` compares fingerprints across all configured domain roots to reject accidental CA reuse | D, T | `docs/credentials.md`; `internal/sshx/ca_test.go`: duplicate fingerprint rejection | — |
| M6 | Short-lived no-extension certificates: exact validity window (`-5m:+15m`), exact principal format (`boxwarden-session-<uuid>`), no certificate extensions | D, T | `docs/credentials.md`; `internal/sshx/cert_test.go`: exact argv, validity, `-O clear` | Not yet exercised by a real SSH login (V4 pending) |
| M7 | SSH client configuration disables all forwarding: `ForwardAgent=no`, `ForwardX11=no`, `ClearAllForwardings=yes`, `Tunnel=no`, `ControlMaster=no`, `ProxyCommand=none`, `ProxyJump=none`, `IdentityAgent=none` (full 23-option policy) | D, T | `docs/credentials.md`; `internal/sshx/client_test.go`; `internal/sshx/adversarial_test.go` | Not yet exercised against a real sshd (V4 pending) |
| M8 | `StrictHostKeyChecking=yes`; ambient SSH config neutralized (`-F /dev/null`); no TOFU | D, T | `docs/credentials.md`; `internal/sshx/client_test.go`; `internal/sshx/adversarial_test.go` | — |
| M9 | Guest host key observed via serial channel (ADR 017 path); no network TOFU | D, Q, I | ADR 012; ADR 017; Task 0 serial-to-scan host-key agreement, clone fingerprint comparison: `docs/evidence/m1a-task0-final-summary.md` | V4 supervisor uses different broker than Task 0 socat harness; ADR 017 requalification pending |
| M10 | Only typed management operations (probe, timezone-apply, timezone-read); no generic remote-shell API | D, T | `docs/credentials.md`; `docs/operations/ssh-management.md`; `internal/sshx/` typed request tests | Not yet exercised against a real guest (V4 pending) |
| M11 | Cross-domain CA fallback is prohibited; no CA from another domain is ever used | D, T | `docs/credentials.md`; `internal/sshx/ca_test.go`: absent-selected-domain with valid other-domain CA returns `ErrCAMissing` | — |

### Clone identity and lifecycle

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| L1 | Every clone receives a unique MAC address | Q | Task 0 two-clone comparison: `docs/evidence/m1a-task0-final-summary.md` — distinct MACs confirmed | Qualified for the Task 0 golden; updated golden requires new qualification |
| L2 | Every clone receives a unique machine ID (`/etc/machine-id`), SSH host keys, and DHCP/DUID identity | Q | Task 0 two-clone comparison: `docs/evidence/m1a-task0-final-summary.md` — all identity components distinct | Same scope limitation as L1 |
| L3 | Session lifecycle intent is persisted and fsynced before backend mutation | D, T | `docs/lifecycle-and-recovery.md`; `internal/lifecycle/reconcile_test.go`; `internal/session/store_test.go` (current tree) | No real-host power-loss/crash test |
| L4 | Per-session locks serialize conflicting operations | D, T | `docs/lifecycle-and-recovery.md`; `internal/lock/filelock_test.go` (current tree) | — |
| L5 | A running VM without proven supervisor ownership is DRIFT/NON-READY with no mutation or adoption | D, T | `docs/lifecycle-and-recovery.md`; `internal/lifecycle/reconcile_test.go`: unproven ownership → drift | V4 supervisor ownership proof not yet implemented |
| L6 | Retries are idempotent; partial or crashed state does not produce duplicate clones | D, T | ADR 009; `docs/lifecycle-and-recovery.md`; lifecycle reconciliation tests | — |
| L7 | No checkpoint operation exists in M1A | D | `docs/lifecycle-and-recovery.md` (explicit deferral); ADR 014 | — |
| L8 | V2 registration records only operator admission and existing artifact identity; it does not assert provenance, clone-readiness, or qualification evidence | D, T | `docs/lifecycle-and-recovery.md`; README; `internal/golden/register_test.go` (current tree) | V2 real-host gate pending |

### State integrity

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| I1 | Private state paths reject symlinks, hardlinks, unsafe modes, and extended ACLs | D, T | `internal/sshx/paths_acl_test.go`; `internal/hostx/publisher_test.go`; `internal/hostx/filesystem_test.go` | Same-UID test model for root-owned paths; V3 evidence confirms manifest cross-UID semantics |
| I2 | Guest-controlled input cannot automatically become trusted persistent host state | D | `docs/security-model.md`; `docs/state-model.md` | Profile persistence not yet implemented |
| I3 | age private keys are host-only; never enter a guest | D | `docs/credentials.md`; `docs/decisions/005-age-and-explicit-profile-writeback.md` | — |
| I4 | The installed manifest contains only non-secret metadata; CA material, credentials, and session data are prohibited | D, T, Q | ADR 024; `internal/hostx/manifest_test.go`; V3 evidence: manifest SHA-256 and content verified | — |

### Golden provenance

These claims have different evidence levels; they are not grouped under a single
status.

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| G1 | Every golden input is locked by exact version, source URL/repository identity, and digest before use | D | ADR 010; `docs/tool-provenance.md` | Build and acceptance gates are attended manual operations |
| G2 | Mutable piped installers are rejected during golden construction | D | ADR 010; `docs/tool-provenance.md` | Design and policy; no automated test of the rejection |
| G3 | Automatic updates of golden contents are disabled | D | ADR 010; `docs/tool-provenance.md` | Configuration policy; verified at golden build, not continuously |
| G4 | A golden revision is immutable after promotion; it is never updated in place | D, T | ADR 010; golden register test (current tree): immutable record after creation | V2 real-host gate pending |
| G5 | Exact Tart and Softnet executable and archive digests are recorded and verified | D, Q | `docs/tool-provenance.md` (digest table); V3 evidence: exact manifested values confirmed for both tools | Specific to the qualified pair |

### Domain isolation

| # | Claim | Dims | Evidence | Limitation |
|---|---|---|---|---|
| D1 | All domain-owned commands require an explicit `--domain`; no implicit default domain | D, T | `docs/architecture.md`; `internal/domain/id_test.go`; `internal/config/config_test.go` | — |
| D2 | No cross-domain fallback for any domain-scoped resource | D, T | ADR 004; ADR 011; `docs/state-model.md`; `internal/config/config_test.go` | — |
| D3 | `boxwarden init` and `boxwarden doctor` operate outside the domain namespace and reject an explicitly supplied `--domain` | D, T, Q | `docs/operations/init-and-doctor.md`; `internal/app/app_test.go`; V3 evidence: both commands ran domain-free | — |
| D4 | Domain CA creation is never triggered lazily by session start | D, T | `docs/credentials.md`; `internal/sshx/ca_test.go`; V3 evidence: no lazy CA path exercised | — |

### Explicitly deferred and not claimed

| # | Item | Status | Source |
|---|---|---|---|
| F1 | Read-only host-tree filesystem capability (ADR 021) | **PROPOSED — not accepted** | ADR 021 is proposed. V14 implementation is blocked until ADR 021 receives formal acceptance. No host-tree capability exists in any current release. |
| F2 | Host-side credential broker (ADR 022) | **PROPOSED — not accepted** | ADR 022 is proposed. No broker is implemented. |
| F3 | Protection against hostile native code already executing as the trusted macOS operator | N | ADR 024 explicitly states this is outside the M1A adversary boundary. A separately reviewed narrow wrapper or service boundary would be required for that threat. |
| F4 | Checkpoint and resume | N, P | `docs/lifecycle-and-recovery.md` explicit deferral; any future design must separately address identity, lineage, taint, and compromise containment. |
| F5 | Provider authentication (AWS, GCP, GitHub, Bitbucket, Jira, Claude Teams) | P | `docs/credentials.md` explicit deferral beyond V1–V4. |
| F6 | Session stop, destroy, file transfer | P | Deferred beyond V4 per v0.1 plan. |
| F7 | ADR 017 requalification for V4 broker | P | V4 replaces the Task 0 socat harness with a supervisor-owned broker. Two-PTY relay, Screen retention/exit/cleanup, and serial bootstrap must be re-exercised. |
| F8 | V2 real-host register/clone gate | P | Requires artifact rebuilt from corrected generic guest definition (no embedded domain CA). |
| F9 | IPv6-only upstream environments | N | ADR 020; `NOT YET PROVEN` in evidence matrix. |

---

## Evidence gaps

These are places where current documentation makes claims more strongly than
available evidence supports, where evidence exists but is not surfaced, or where
important limitations should be more visible. They are documentation improvement
targets, not implementation defects.

**EG-1 — V4 supervisor specification remains normative, not operational**

`docs/security-model.md` now marks its V4 supervisor, two-PTY broker,
generation-locking, and nonce/challenge-response text as normative future
design. `docs/architecture.md` still needs equivalent wording. V4 is not
implemented or qualified; the assurance matrix marks all
V4-supervisor-dependent properties as pending where applicable.

**EG-2 — PROPOSED ADR status requires attention when reading the decisions index**

ADR 021 (`docs/decisions/021-explicit-read-only-host-tree-capabilities.md`)
and ADR 022 (`docs/decisions/022-host-side-credential-broker.md`) both carry
`Status: PROPOSED` in their documents. Neither is accepted architecture. The
assurance matrix above records both as PROPOSED and not accepted. Readers
navigating directly to the `docs/decisions/` directory should read status lines
before treating any ADR as settled.

**EG-3 — Same-UID test model for root-owned path checks**

Several tests in `internal/hostx/publisher_test.go` use `os.Getuid()` as the
expected root UID and a no-op `chown`, proving logic under same-UID semantics
but not under real root-owned file conditions. The V3 attended gate provides
real-host cross-UID evidence for the manifest itself (the `sudo -u nobody` SHA-256 read).
Claims labeled T where this applies note the same-UID limitation.

**EG-4 — ADR 017 requalification scope not yet specified**

The requirement to requalify ADR 017 for the V4 broker is noted in
`docs/architecture.md` and `docs/tool-provenance.md` but the specific
gate procedure is not yet documented. The gate must cover the two-PTY relay,
Screen retention across attach/detach, unattended output, reboot survival,
and cleanup.
