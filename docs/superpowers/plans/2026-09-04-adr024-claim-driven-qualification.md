# ADR024 Claim-Driven Qualification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the over-constrained ADR024 attended Bash harness with a claim-driven qualification system that keeps immutable trust and evidence strict, validates trusted mutable state semantically, and directly exercises the malicious-guest-root boundary with host-authoritative evidence.

**Architecture:** Keep the existing qualification-only Go boundary under `internal/qualification/adr024`. Add small packages for the claim catalog, immutable run specification, semantic Tart state, exact launch recording, host network oracles, a hash-bound guest-root probe, evidence verification, and a thin attended controller. The controller coordinates separately hash-bound components and human gates; it does not embed deterministic fixture suites or infer security from global host cleanliness, trusted mutable timestamps, or guest-generated PASS text.

**Tech Stack:** Go 1.27 standard library, Darwin `libproc` through the existing cgo observer, Darwin filesystem/process adapters behind build constraints, Tart 2.32.1, Softnet 0.19.0, GNU Screen 4.00.03, socat, deterministic JSON fixtures, and repository Markdown.

**Spec:** `docs/operations/adr024-runtime-observer.md`

## Global Constraints

- The accepted threat boundary is trusted macOS host and trusted operator versus an untrusted guest, including guest root.
- This is a qualification-method refinement under ADR024; do not create a new ADR unless observed behavior requires a product policy or trust-boundary change.
- Every blocking assertion must map to one claim ID in the catalog below.
- Immutable trust, forensic state, failed-run evidence, and admitted component artifacts remain byte- and security-metadata exact.
- For trusted mutable source/qualification Tart state, ordinary atime, mtime, ctime, and parent-directory timestamp choreography are diagnostic unless a named claim explicitly requires them.
- Bootpd inode and timestamps are diagnostic. Its canonical path, direct regular-file type, device, root ownership, mode `0644`, one-link state, flags, exact bytes/SHA-256/XML, and sole approved `DHCPLeaseTimeSecs=600` semantics remain blocking.
- Guest-produced PASS/FAIL text is never authoritative for a hostile-guest containment result. The verifier decides from exact launch evidence and host-side oracles; guest output records bounded stimulus and corroborating inventory.
- Network cases are individually classified `expected_reachable`, `prohibited_reachable`, or `not_qualified`; finite probes must not be generalized.
- Vmnet gateway reachability remains explicitly accepted. Changing that policy requires architecture adjudication.
- The observer remains unprivileged, qualification-only, bounded, non-lossless, and outside production imports.
- No provider, browser, Git, keychain, 1Password, SSH-agent, GPG-agent, or other real credential is introduced. Exposure probes use synthetic markers only.
- A failed attended run is immutable evidence, not a checkpoint. Failure containment may stop exact owned processes, but deletion requires its own human token; the next successful attempt starts from a fresh baseline and fresh disposable state.
- Fully quit and relaunch Terminal.app before each new attended attempt as procedure hygiene, never as a workaround for an unexplained failure.
- Do not modify, resume, reuse, or delete the `48bac744` controller, run root, retained VM, earlier failed runs, Resume3/Resume4 evidence, or forensic seal.
- Qualification code must not become part of `boxwarden init`, the normal `boxwarden` CLI, production session lifecycle, or an installed privileged service.

---

## Blocking Claim Inventory

The `claim.ID` constants implemented in Task 1 are the only accepted reasons for an attended blocker. Diagnostic observations may be retained without a claim result.

| ID | Blocking claim |
| --- | --- |
| `A1_CONTROLLER_PROVENANCE` | The executing controller and every helper are the exact reviewed, sealed artifacts built from the admitted V3 source revision. |
| `A2_HOST_TRUST_STATE` | The strict root-owned manifest, qualified Tart, qualified Softnet, protected ancestry, operator/group binding, and absence of an alternate mutable-Homebrew/sudo privilege path match the accepted host state. |
| `A3_PRIVILEGE_BINDING` | The selected Softnet is the exact root-controlled, single-link `04550` artifact reached through the reviewed Tart launch path. |
| `A4_LAUNCH_CONTRACT` | The child-observed executable, ordered argv, working directory, eight-entry closed environment, stdio routing, and omitted integration flags exactly match the run specification. |
| `A5_QUALIFICATION_VM_IDENTITY` | The launch target is the one identity-qualified fresh clone with a distinct randomized MAC in the isolated qualification home. |
| `B1_NO_FILESYSTEM_OR_DISK_EXPOSURE` | No host directory share or unintended additional disk is exposed to the guest. |
| `B2_NO_INTERACTIVE_HOST_INTEGRATION` | Clipboard, audio, VNC/bridged display, system-key capture, and prohibited display integration are not exposed to the guest. |
| `B3_NO_HOST_CONTROL_OR_CREDENTIAL_IPC` | Docker/Podman/containerd sockets, SSH/GPG agents, keychain/1Password IPC, Tart/hypervisor control sockets, and real host credentials are not exposed to the guest. |
| `B4_NO_PROHIBITED_EXECUTION_CAPABILITY` | Nested virtualization, Rosetta-style capability, and other explicitly prohibited opt-in execution capabilities are absent. |
| `B5_INTENDED_CONTROL_CHANNELS_ONLY` | The only admitted recovery/control integrations are the owner-private serial path and separately authenticated host-to-guest management SSH where exercised. |
| `C1_TART_PROCESS_IDENTITY` | The running Tart process is the exact controller-owned process identity created by the admitted launch recorder. |
| `C2_SOFTNET_DIRECT_ANCESTRY` | Exactly one Softnet direct child has stable kernel identity, exact executable identity, and independently observed PGID; PGID equality with Tart is not required. |
| `C3_PRIVILEGED_SETUP_CHAIN` | Exact artifact/source/launch evidence plus successful privileged behavior establishes the intended Softnet privileged setup; a sampled root-effective tuple is optional corroboration. |
| `C4_SOFTNET_STEADY_DROP` | Softnet reaches and retains the accepted trusted-operator UID/GID tuple for the bounded steady-state observation window. |
| `C5_IDENTITY_BOUND_SIGNALING` | No containment or cleanup operation obtains authority from a Softnet PID alone or can signal an unrelated host process. |
| `D1_PRIVATE_SERIAL_RELAY` | Relay directories, endpoint links, and both PTYs are owner-private, identity-bound, and non-networked. |
| `D2_SCREEN_HOLDER_IDENTITY` | The exact collision-free Screen session label resolves to one admitted holder identity and only that session is addressed for cleanup. |
| `D3_UNTRUSTED_SERIAL_CONTAINMENT` | Guest-controlled serial/control bytes remain bounded raw evidence and cannot select another session, invoke a host command, or be rendered automatically on an operator terminal. |
| `E1_EXPECTED_NETWORK_REACHABILITY` | Each named expected-reachable DHCP, DNS, public IPv4, or accepted gateway case succeeds according to its exact finite test and oracle. |
| `E2_PROHIBITED_NETWORK_REACHABILITY` | Each named prohibited host/private/link-local case fails to reach its exact host-controlled oracle during the bounded stimulus window. |
| `E3_NETWORK_POLICY_PERSISTS` | The finite rate-limited malformed TCP/UDP corpus does not make a prohibited oracle reachable, alter admitted Softnet identity/credentials, or destabilize the host/runtime. Raw ICMP/fragment behavior remains not qualified until an independent oracle is approved. |
| `E4_NETWORK_SCOPE_TRUTHFUL` | Every untested or unsupported address/protocol/environment class is reported `not_qualified`; no finite result is broadened into a LAN or general IPv6 claim. |
| `F1_EXACT_RUNTIME_CONTAINMENT` | Normal and failure stop paths reap the exact Tart job, observe the exact Softnet identity disappear without PID-only signaling, and stop only owned relay/oracle/Screen resources. |
| `F2_IDENTITY_BOUND_VM_DELETE` | The irreversible delete command is separately authorized and targets only the admitted stopped qualification VM. |
| `F3_Q1_AND_UNRELATED_STATE` | The qualification namespace returns to Q1 while source semantic state, admitted host trust state, and independently sealed forensic state remain within their named exact/semantic envelopes. |
| `G1_PRIVATE_BOUNDED_EVIDENCE` | Raw evidence is direct, owner-only, ACL-free, size-bounded, schema-valid, and never includes real credentials. |
| `G2_EVIDENCE_IDENTITY_AND_LIMITS` | Every component report is hash-addressed and records its version, run identity, limitations, diagnostics, and exact claim associations. |
| `G3_FAILED_RUN_IMMUTABILITY` | A failed run is preserved as historical evidence and cannot be resumed, rewritten, reused as a qualification runtime, or called a pass. |

## Disposition of the 48 Current Assertion Families

This table is the migration authority from the audited `48bac744` controller. “Keep” means preserve the claim, not copy the old implementation.

| Audit family | Disposition in redesign |
| ---: | --- |
| 1 | Keep under `A1` and `G2`; admit exact controller/helper artifacts once, before runtime. |
| 2 | Keep under `A1`; move source/archive cleanliness to deterministic packaging. |
| 3 | Keep private run/evidence/relay roots under `D1` and `G1`; demote generation housekeeping. |
| 4 | Keep strict manifest parsing under `A2`. |
| 5 | Keep exact Tart/Softnet and privilege-path checks under `A2`/`A3`. |
| 6 | Keep exact socat/Screen identities only for `D1`/`D2`; V4 requalifies its native broker. |
| 7 | Move compiler/offline-build checks to deterministic packaging; runtime blocks only on hashes under `A1`. |
| 8 | Remove lsof help/version/output-shape blockers; retain a deterministic adapter test if lsof remains an implementation detail. |
| 9 | Remove Terminal branding, TERM, multiplexer-variable, and presentation blockers; require an interactive owner TTY only where a token is read. |
| 10 | Remove globally empty Screen namespace; block only on collision with the exact run label and bind the created holder under `D2`. |
| 11 | Remove generic process-name emptiness. |
| 12 | Replace global port emptiness with exact bind/collision checks for each oracle under `E1`/`E2`; post-run global absence is diagnostic. |
| 13 | Keep canonical, physical separation of source, qualification, forensic, installation, repository, and evidence roots under `A5`, `F3`, and `G3`. |
| 14 | Keep fresh target absence and Q0 under `A5`/`F3`. |
| 15 | Keep source identity, stopped state, semantic config/MAC, and required members under `A5`; demote lsof opener snapshots. |
| 16 | Remove source-list config ctime grammar; timestamps are diagnostic. |
| 17 | Replace recursive source-home metadata equality with semantic source identity, child-set/orphan checks, and protected bytes under `A5`/`F3`. |
| 18 | Remove source tmp/vms/source-VM timestamp choreography; record diagnostics only. |
| 19 | Keep independent forensic-seal comparison strict under `F3`/`G3`. |
| 20 | Replace whole `/Library/Boxwarden` equality with exact manifest/Tart/Softnet/protected-ancestry checks under `A2`; broader snapshots are diagnostic. |
| 21 | Keep bootpd protected path/type/device/owner/mode/link/flags/bytes/digest/XML/semantics under `C3`/`F3`; inode and timestamps are diagnostic. |
| 22 | Keep the preparation token under `G2`. |
| 23 | Keep exact command/environment routing under `A4`; compare ordered values directly rather than count-only serialization. |
| 24 | Keep clone status, stopped-clone shape, distinct MAC, and fresh identity under `A5`; ignore mutable timestamps. |
| 25 | Keep same-device, exact-object stage-to-final move and substitution prevention under `A5`. |
| 26 | Keep source semantic state, no orphan stage/temp, and exact active-Q object under `A5`/`F3`; ignore timestamps. |
| 27 | Keep owner-private relay/PTYs and exact relay identity under `D1`. |
| 28 | Keep exact Screen label and process identity under `D2`; remove exhaustive listing grammar and global output bounds. |
| 29 | Keep ordered launch argv/environment and launch token under `A4`; remove redundant count-only checks. |
| 30 | Keep scoped pre-exec revalidation of claim-bearing inputs under `A1`–`A5`. |
| 31 | Keep exact Tart owned process group and unique identity under `C1`/`C5`. |
| 32 | Keep observer fixed state, ancestry, unique/start identity, executable, PGID, and credentials under `C2`/`C4`. |
| 33 | Keep the cumulative privileged-setup chain under `C3`; root-effective sampling stays optional corroboration. |
| 34 | Keep first/final bounded identity continuity under `C2`/`F1`; do not claim continuous observation. |
| 35 | Remove human-entered serial PASS as security evidence; replace it with `D3` raw framed evidence and host verification. |
| 36 | Keep exact gateway positive oracle under `E1`, explicitly as accepted exposure. |
| 37 | Retire the one-port result as a broad denial; replace it with matrix cases and host oracles under `E2`. |
| 38 | Keep exact VM/runtime identity before stop under `F1`; opener observations are diagnostic when stronger identity exists. |
| 39 | Keep exact routed stop, Softnet disappearance without PID signaling, and Tart reap under `F1`. |
| 40 | Keep exact owned listener/Screen/relay cleanup under `F1`; never use guest-supplied identifiers. |
| 41 | Keep stopped semantic VM identity before delete under `F2`; lsof is advisory unless it is the only proof of an open object. |
| 42 | Keep exact separately authorized delete and Q1 under `F2`/`F3`. |
| 43 | Keep source semantic state, exact forensic/tool/bootpd protected state under `F3`; broad operational snapshots and doctor are diagnostic unless mapped to a claim. |
| 44 | Remove post-cleanup generic process-name and global-port emptiness. |
| 45 | Remove empty-generation-scratch as a qualification claim; clean/report it operationally. |
| 46 | Keep owner-private bounded schema-valid evidence under `G1`/`G2`; centralize size enforcement. |
| 47 | Keep direct-owned-group containment, no PID-only Softnet signals, and truthful deletion state under `C5`, `F1`, `F2`, and `G3`. |
| 48 | Move every pure model/parser fixture out of attended execution into deterministic tests. |

## Component and File Map

All new Go code remains beneath `internal/qualification/adr024`; the existing architecture test continues to forbid production imports.

| Component | Files | Exists to prove |
| --- | --- | --- |
| Claim catalog and canonical envelope | Create `internal/qualification/adr024/claim/catalog.go`, `result.go`, `json.go`; tests beside them | Every blocker maps to `A1`–`G3`; component output is bounded and canonical. |
| Sealed package and run specification | Create `internal/qualification/adr024/runstate/package.go`, `package_test.go`, `spec.go`, `spec_test.go` | One human-approved package-manifest digest binds the controller, every helper/probe, source archive, immutable run spec, roots, VM names, launch contract, limits, and approval-token format. |
| Semantic Tart/file state | Create `internal/qualification/adr024/runstate/tart.go`, `file_darwin.go`, `file_stub.go`, and tests | `A5`, `F2`, `F3` without trusted mutable timestamp choreography. |
| Child launch recorder | Create `internal/qualification/adr024/launch/record.go`, tests, and `internal/qualification/adr024/cmd/exec-record/main.go` | `A4` and `C1` from the child side before `execve` replaces the recorder with Tart. |
| Existing process observer | Modify only as needed: `internal/qualification/adr024/{observer.go,run.go,state.go}` and tests | `A2`, `C1`–`C4`; preserve its non-lossless limitations and no-signal contract. |
| Privileged-component state | Create `internal/qualification/adr024/hoststate/bootpd.go`, `tree.go`, Darwin adapters, and tests | `A2`, `A3`, `C3`, `F3`; timestamps/inode are diagnostics only for bootpd. |
| Host network oracle | Create `internal/qualification/adr024/oracle/model.go`, `listener.go`, `verify.go`, tests, and `cmd/host-oracle/main.go` | `E1`–`E4` from host-observed exact listeners and bounded events. |
| Guest-root probe | Create `internal/qualification/adr024/probe/model.go`, `execute_linux.go`, `execute_stub.go`, tests, and `cmd/guest-probe/main.go` | Bounded guest stimulus and inventory for `B1`–`B5`, `D3`, `E1`–`E4`; it does not adjudicate security. |
| Evidence verifier | Create `internal/qualification/adr024/evidence/report.go`, `verify.go`, tests, and `cmd/verify/main.go` | `G1`/`G2` and final claim adjudication by correlating host-authoritative artifacts. |
| Attended controller | Create `internal/qualification/adr024/controller/{controller.go,phases.go,resources.go,resources_darwin.go,resources_stub.go,evidence.go}` with tests, and `cmd/controller/main.go` | Human-gated sequencing, `A1`–`A5`, `D1`/`D2`, `F1`–`F3`, and `G3`; business logic stays in focused packages above. |
| Deterministic fixtures | Create `internal/qualification/adr024/testdata/{package-manifest-v1.json,run-spec-v1.json,tart-capabilities-2.32.1.json,tart-list-source.json,tart-list-q0.json,tart-list-active.json,tart-list-q1.json,bootpd.xml,guest-report-v1.json,oracle-report-v1.json,observer-report-v1.json}` | Exact retained real-host representations and the reviewed Tart capability map become regression inputs, not attended-time self-tests. |
| Attended packaging | Create `scripts/qualification/build-adr024-package.sh` and `scripts/qualification/testdata/expected-package-manifest.json`; tests in `scripts/qualification/build-adr024-package_test.sh` | Reproducibly build, hash, inventory, and seal the controller/helpers/probe/spec before human approval. |

The attended controller command remains thin. It accepts only:

```text
adr024-controller --package-manifest <absolute-path> --manifest-sha256 <64-lowercase-hex>
```

The package manifest and every artifact it names are direct regular, one-link,
owner-owned files below one direct owner-private `0700` package directory.
Executable roles are mode `0555`; data roles are mode `0444`. The
controller opens the manifest with no-follow semantics, verifies the one digest
the human approved, strictly decodes it, then reopens and hashes itself, the run
spec, every helper/probe, and the source archive against the manifest before it
can enter preflight. The manifest records exact source commit, build identity,
artifact path/size/mode/owner/group/link/digest, and the run-spec digest. No CLI
option overrides expected paths, UIDs, GIDs, artifact digests, network
expectations, limits, VM names, or approval formats.

## Canonical Evidence Formats

Every component writes one JSON object plus LF, using struct field order, sorted claim/case/artifact arrays, no maps in canonical sections, strict unknown-field rejection, and exact schema version `1`.

```go
type Envelope struct {
	SchemaVersion int             `json:"schema_version"`
	Component     string          `json:"component"`
	RunID         string          `json:"run_id"`
	Status        Status          `json:"status"`
	Claims        []ClaimResult   `json:"claims"`
	Artifacts     []Artifact      `json:"artifacts"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
	Limitations   []string        `json:"limitations"`
}

type ClaimResult struct {
	ID       ID       `json:"id"`
	Outcome  Outcome  `json:"outcome"`
	Evidence []string `json:"evidence"`
	Reason   string   `json:"reason,omitempty"`
}

type Artifact struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
```

Allowed `Status` values are `pass`, `fail`, and `incomplete`; allowed claim outcomes are `pass`, `fail`, `not_qualified`, and `diagnostic`. A final `pass` requires exactly one result for every blocking claim applicable to that run; omitted claims fail verification. `not_qualified` is permitted only for cases explicitly classified that way in the immutable spec.

Size limits in `RunSpec.Limits` are fixed at:

- component JSON report: 256 KiB;
- guest raw serial transcript: 8 MiB;
- decoded guest report: 256 KiB;
- oracle event stream: 1 MiB;
- final evidence manifest: 1 MiB;
- network cases: at most 64;
- attempts across network cases: at most 256;
- payload per network attempt: at most 512 bytes;
- network rate: at most 20 attempts per second;
- active network phase: at most 90 seconds;
- hostile serial corpus: at most 64 frames, 4 KiB per frame, 64 KiB total, and 30 seconds.

The guest transport is an ASCII frame written through the detached Screen session. It contains a controller-generated 128-bit nonce, decoded length, SHA-256, base64 payload, and an exact end marker. Raw serial is retained privately; the controller extracts only the one matching bounded frame and never prints guest-controlled bytes to the operator terminal.

## Hostile-Guest Claim/Oracle Matrix

| Claim | Guest stimulus | Host-side oracle | Expected result | Blocking failure | Evidence artifact |
| --- | --- | --- | --- | --- | --- |
| `B1` | Inventory `/proc/self/mountinfo`, block devices, and virtio/PCI devices; attempt read-only opens of synthetic share candidates. | Exact child-observed Tart argv plus the reviewed Tart 2.32.1 capability map proves no `--dir` or extra `--disk`; guest inventory is corroboration. | Only the VM's expected disks/filesystems; no configured host share. | Prohibited flag/capability-map mismatch; guest-only observations cannot independently pass or fail absence. | `tart-capabilities.json`, `launch.json`, `guest-report.json`, `claims.json` |
| `B2` | Enumerate display/audio/clipboard/VNC devices/sockets and attempt bounded synthetic markers. | Exact `--no-audio`, `--no-clipboard`, absent VNC/bridged/system-key flags, and the reviewed capability map are authoritative. | The expected virtual guest display may exist, but no configured host display, VNC, clipboard, or audio integration is exposed. | Launch/capability-map mismatch; successful synthetic access is a failure, while non-receipt alone is diagnostic. | `tart-capabilities.json`, `launch.json`, `guest-report.json`, `oracle-report.json` |
| `B3` | Enumerate Unix sockets/environment and attempt synthetic Docker/agent/keychain/control marker protocols. | Exact launch argv, closed environment, no host shares/forwarding mechanism, and the reviewed capability map prove no configured transport; marker receipt can independently disprove the claim. | No configured host-control or credential IPC path. | Any enabling launch/env mapping or received marker nonce. Guest inventory non-observation is corroborative only. | `tart-capabilities.json`, `launch.json`, `guest-report.json`, `oracle-report.json`, `claims.json` |
| `B4` | Inventory CPU flags/devices and attempt bounded nested/Rosetta capability probes. | Exact absence of `--nested`/`--rosetta` and their reviewed Tart 2.32.1 capability mappings is authoritative; inventory is corroboration. | Those opt-in capabilities are not configured. | Enabling flag or capability-map mismatch; guest text cannot establish absence. | `tart-capabilities.json`, `launch.json`, `guest-report.json` |
| `B5` | Exercise serial framing and report management interface inventory without real credentials. | Private PTY/Screen/process identity, exact serial launch argument, closed integration map, and separately qualified production SSH policy. | Only declared serial and separately authenticated SSH surfaces are configured. | Another configured host-control transport, host marker receipt, or serial escape from its identity-bound relay. | `tart-capabilities.json`, `relay.json`, `launch.json`, `guest-report.json`, `claims.json` |
| `D3` | Emit bounded ANSI, OSC, DCS, Screen-looking escape, malformed UTF-8, oversized, interleaved, and frame-confusion corpus. | Detached holder identities, host process/marker state, bounded raw capture; no auto-render. | Relay/holder remain owned and stable; no host command/marker effect. | Identity change, host marker mutation, another Screen session addressed, output bound, or automatic rendering. | `serial.raw`, `serial-summary.json`, `process.json` |
| `E1` DHCP/DNS | Renew DHCP and perform exact DNS cases from the spec. | Host lease state, expected bootpd semantics, and case-specific response evidence; guest text is functional corroboration only. | Exact declared cases succeed. | No matching lease/response or policy drift. | `network-plan.json`, `oracle-report.json`, `guest-report.json` |
| `E1` public/gateway | Send unique nonces over exact TCP/UDP cases. | Identity-bound host gateway listeners; authenticated public response fixture only where the spec names one. | Each expected case produces its exact response/nonce. | Missing or mismatched response. | `oracle-report.json`, `guest-report.json` |
| `E2` host/private/link-local | The exact reviewed probe executes a controller-issued, per-case nonce schedule against exact host-controlled prohibited listeners/addresses, with no untrusted workload running concurrently. | The host records the issued schedule, exact probe artifact/transport frame, listener identities, monotonic events, and adjacent positive controls. The guest does not adjudicate the result. | No prohibited listener receives its nonce and every adjacent positive control succeeds. This qualifies only the exact controlled finite experiment, not a probe-resistant attestation protocol or broad network class. | Any prohibited nonce received, missing positive control, wrong probe/frame identity, ambiguous listener lifecycle, incomplete schedule, or timeout. | `stimulus-schedule.json`, `probe-transfer.json`, `oracle-report.json`, `guest-report.json`, `claims.json` |
| `E3` malformed TCP/UDP payloads | Send the fixed malformed application-payload corpus over ordinary TCP/UDP sockets within limits to exact cases backed by host listeners. | Exact prohibited/positive listeners, process observer, bootpd protected-state snapshot, and host stability checks. | No prohibited delivery or component/host drift. | Prohibited delivery, observer identity/credential failure, protected-state drift, output exhaustion, or host instability. | `corpus.json`, `oracle-report.json`, `observer-final.json`, `hoststate-final.json` |
| `E4` unqualified | Attempt only cases present in the spec; report absent IPv6/environment cases explicitly. | Verifier compares executed case IDs to the immutable matrix. | Untested cases remain `not_qualified`. | Missing classification or broad claim derived from a subset. | `run-spec.json`, `claims.json` |

The exact reviewed guest probe is a trusted qualification stimulus generator
executed as root in a fresh controlled clone before any untrusted workload is
introduced; the guest OS and root capabilities are the subject under test. Its
PASS/FAIL interpretation is never outcome authority. The host records the
exact challenge schedule it issues and the admitted probe/frame identity;
host-side launch/configuration maps and oracles decide results. This method
does not claim to resist a guest that tampers with the qualification tool or
fakes the test protocol itself. A case without authoritative launch mapping or
an adequate host outcome oracle remains informational or `not_qualified`.

## Attended Phase and Human-Gate Sequence

1. **Deterministic package review (no attended runtime):** build all binaries, cross-build the Linux/arm64 probe, generate the immutable spec, run all tests/static scans, record sizes and SHA-256 values, complete attempt minimization, and obtain independent review.
2. **Seal and preflight (explicit execution approval):** admit the one human-approved package-manifest digest, controller/spec/helpers/source/host trust state, verify a fresh run root and qualification home, acquire the direct owner-only qualification-home lock, capture Q0/source/bootpd/forensic baselines, write a canonical prepare-authority record, and stop at `APPROVE-ADR024-PREPARE-<RUNID>-<AUTH12>`.
3. **Prepare token:** clone exact source to an identity-bound stage, randomize MAC, same-device move to the qualification home, validate source semantics/no orphan and active-Q state, start private socat relay and exact Screen holder, write an authority record containing the exact fresh VM config/NVRAM/MAC/device/inode identity, then stop at `APPROVE-ADR024-LAUNCH-<RUNID>-<AUTH12>`.
4. **Launch token:** use the child recorder to direct-exec the exact Tart contract, start the existing unprivileged observer, establish serial readiness, capture passive process/privilege/bootpd evidence, transfer and hash-admit the guest probe through the detached bounded serial protocol, write an authority record containing exact process identities and matrix digest, then stop at `APPROVE-ADR024-ADVERSARIAL-<RUNID>-<AUTH12>`.
5. **Adversarial token:** start exact host oracles, execute the guest inventory/integration/network/serial corpus, collect bounded reports, complete the final observer window, and perform normal identity-bound runtime stop. On any failure after launch, containment may stop exact owned groups but never deletes the VM. Re-admit the stopped VM, write the exact delete-authority record and evidence-manifest digest, and stop with the VM preserved at `APPROVE-ADR024-DELETE-<RUNID>-<AUTH12>`.
6. **Delete token:** while retaining the exclusive qualification-home lock, immediately revalidate the exact stopped VM against the authority record, mark the deletion commit point, invoke the exact routed delete, record status/result, prove Q1, compare final bootpd/tool/source/forensic state under the correct exact/semantic classes, finalize hashes, release the lock, and exit.

Tokens are read from the controller's admitted `/dev/tty`; they are never accepted on argv, environment, a pipe, or a file. `AUTH12` is the first 12 lowercase hex characters of the SHA-256 of the canonical authority record for that exact phase. Each prompt displays and durably records the full authority-record digest, exact source/VM/process/evidence identities, authorized mutation, and automatic failure-containment scope. A token is phase-specific, identity-bound, one-use, and authorizes no later phase.

## Failure Preservation and Migration

- Before the prepare token, failure preserves the private preflight evidence and performs no VM mutation.
- After prepare but before launch, failure stops only exact owned relay/Screen jobs and preserves the staged/final clone and evidence.
- After launch, failure stops only revalidated controller-owned Tart/relay/oracle/Screen identities, observes Softnet disappearance without PID-only signaling, and preserves the VM/evidence.
- The delete operation is never part of generic failure cleanup. Only the exact `APPROVE-ADR024-DELETE-<RUNID>-<AUTH12>` derived from the stopped-VM authority record authorizes it.
- A direct, no-follow, owner-only `0600`, single-link lock file below the fresh qualification root is exclusively `fcntl`-locked before the first Tart state observation and held through Q1 or failed-run preservation. All qualification components honor it; the trusted-operator threat model and operator procedure prohibit unrelated direct Tart use of that isolated home during the gate.
- No failed run is resumed for a pass. A bounded resume may only investigate, understand, or safely contain the same failed state under a separately reviewed procedure.

Existing evidence that remains admissible as an input or independently established fact:

- attended host-global init, fresh-auth doctor, distinct domain CA, idempotence, manifest migration, and unsafe mutable-Homebrew refusal evidence;
- exact installed Tart/Softnet/manifest identities and protected ancestry;
- reviewed Softnet 0.19.0 privilege-drop and bootpd commit source behavior;
- Resume3 direct Tart-to-Softnet topology, independently observed PGIDs, and steady credential samples, subject to their recorded sampling limits;
- Resume4 exact bootpd bytes/security metadata/semantics and replacement observation, without atomicity;
- deterministic observer, process-topology, bootpd, stat representation, Screen representation, job-reap, and argv-forwarding regressions;
- the independent forensic seal and every failed-run artifact as historical evidence.

Evidence that becomes historical/diagnostic only and cannot satisfy the new final pass:

- every prior failed controller/run, including `48bac744`, and its retained VM;
- source/qualification atime, mtime, ctime, inode transition narratives for trusted mutable state;
- global process/Screen/port emptiness;
- Terminal.app and lsof presentation fingerprints;
- scratch-directory emptiness and whole mutable-source metadata neutrality;
- manual guest serial PASS strings;
- the single private-port non-delivery sample and any broad network conclusion inferred from it.

Claims formally retired by this redesign are exact trusted-mutable timestamp choreography, whole source/cache metadata neutrality, global process/Screen/port cleanliness, exact Terminal/lsof presentation, empty scratch state, broad LAN/private-network denial from finite samples, general IPv6 isolation without direct cases, credential-channel absence beyond examined integrations, cross-session/domain isolation before V4, and destroy persistence before the corresponding lifecycle exists.

V4 owns simultaneous-session isolation, security-domain cross-session isolation, production native serial-broker qualification, destroy/persistence semantics, repeated lifecycle durability, production occupied-home reconciliation, and lifecycle readiness/status/CLI composition. ADR020 continues to classify effectively IPv6-only upstream/destination behavior as unqualified until its matrix passes.

## Parallelization Map

After Task 1 fixes the shared claim/evidence vocabulary, these disjoint workstreams may run in isolated worktrees:

- Tasks 2–3: run specification and semantic Tart/host state;
- Tasks 4–5: launch recorder/process observer and privileged-component state;
- Tasks 6–7: host oracle and guest probe.

Task 8 begins only after Tasks 2–7 interfaces are integrated. Task 9 begins after Task 8. Task 10 is serial integration, complete verification, review, and packaging. One lead owns every integration into the published V3 branch and all Git/PR mutations. No concurrent worker may mutate the same worktree, branch, VM, session, port/oracle, or other external state.

### Task 1: Claim Catalog and Canonical JSON

**Files:**
- Create: `internal/qualification/adr024/claim/catalog.go`
- Create: `internal/qualification/adr024/claim/result.go`
- Create: `internal/qualification/adr024/claim/json.go`
- Test: `internal/qualification/adr024/claim/catalog_test.go`
- Test: `internal/qualification/adr024/claim/json_test.go`

**Interfaces:**
- Produces: `type ID string`, the exact `A1`–`G3` constants above, `type Status string`, `type Outcome string`, `type NetworkExpectation string` with `expected_reachable`, `prohibited_reachable`, and `not_qualified`, `type ClaimResult`, `type Artifact`, `type Diagnostic`, `type Envelope`, `func ValidateEnvelope(Envelope, int64) error`, and `func MarshalCanonical(Envelope, int64) ([]byte, error)`.
- Invariant: results and artifacts are strictly sorted, claim IDs are unique and known, paths are absolute and clean, SHA-256 is lowercase hex, output is exactly one object plus LF, and oversized/unknown/duplicate input fails closed.

- [ ] **Step 1: Write failing catalog tests**

```go
func TestCatalogContainsEveryApprovedBlockingClaimExactlyOnce(t *testing.T) {
	want := []claim.ID{
		claim.A1ControllerProvenance, claim.A2HostTrustState,
		claim.A3PrivilegeBinding, claim.A4LaunchContract,
		claim.A5QualificationVMIdentity, claim.B1NoFilesystemOrDiskExposure,
		claim.B2NoInteractiveHostIntegration, claim.B3NoHostControlOrCredentialIPC,
		claim.B4NoProhibitedExecutionCapability, claim.B5IntendedControlChannelsOnly,
		claim.C1TartProcessIdentity, claim.C2SoftnetDirectAncestry,
		claim.C3PrivilegedSetupChain, claim.C4SoftnetSteadyDrop,
		claim.C5IdentityBoundSignaling, claim.D1PrivateSerialRelay,
		claim.D2ScreenHolderIdentity, claim.D3UntrustedSerialContainment,
		claim.E1ExpectedNetworkReachability, claim.E2ProhibitedNetworkReachability,
		claim.E3NetworkPolicyPersists, claim.E4NetworkScopeTruthful,
		claim.F1ExactRuntimeContainment, claim.F2IdentityBoundVMDelete,
		claim.F3Q1AndUnrelatedState, claim.G1PrivateBoundedEvidence,
		claim.G2EvidenceIdentityAndLimits, claim.G3FailedRunImmutability,
	}
	if got := claim.BlockingIDs(); !slices.Equal(got, want) {
		t.Fatalf("BlockingIDs() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the focused tests and require the expected compile failure**

Run: `go test ./internal/qualification/adr024/claim -run 'TestCatalog|TestMarshal|TestValidate' -count=1`

Expected: FAIL because the package and types do not exist.

- [ ] **Step 3: Implement the exact constants, structs, strict validation, and bounded encoder**

Use only `encoding/json`, `errors`, `fmt`, `path/filepath`, `regexp`, `slices`, and `strings`. Do not use maps in canonical output.

- [ ] **Step 4: Add table tests for unknown IDs, duplicate IDs, unsorted arrays, malformed paths/digests, trailing JSON, missing LF, and every size boundary**

- [ ] **Step 5: Run and pass the package plus architecture boundary**

Run: `go test ./internal/qualification/adr024/claim ./internal/architecture -count=1`

- [ ] **Step 6: Commit**

```sh
git add internal/qualification/adr024/claim
git commit -m "test: define ADR024 blocking claim evidence schema"
```

### Task 2: Sealed Package Manifest and Immutable Run Specification

**Files:**
- Create: `internal/qualification/adr024/runstate/package.go`
- Test: `internal/qualification/adr024/runstate/package_test.go`
- Create: `internal/qualification/adr024/runstate/spec.go`
- Test: `internal/qualification/adr024/runstate/spec_test.go`
- Create: `internal/qualification/adr024/testdata/package-manifest-v1.json`
- Create: `internal/qualification/adr024/testdata/run-spec-v1.json`

**Interfaces:**
- Consumes: `claim.ID` and network expectation strings from Task 1.
- Produces: `type PackageManifest`, `type PackageArtifact`, `func ParsePackage(raw []byte) (PackageManifest, error)`, `func (PackageManifest) Admit(manifestPath, approvedDigest, selfPath string) (RunSpec, []claim.Artifact, error)`, `type RunSpec`, `type ArtifactSpec`, `type RootSpec`, `type LaunchSpec`, `type NetworkCase`, `type Limits`, `type ApprovalFormat`, `func ParseSpec(raw []byte) (RunSpec, error)`, and `func (RunSpec) Validate() error`.
- `LaunchSpec` contains exact executable, ordered argv, eight ordered `KEY=value` entries, working directory, stdin/stdout/stderr policy, and prohibited Tart flag list.
- `NetworkCase` contains stable ID, address family, protocol, destination class, address, port, expectation, positive-control ID, attempts, per-attempt timeout, payload size, and oracle kind.

```go
type PackageManifest struct {
	SchemaVersion int
	SourceCommit  string
	Build         BuildIdentity
	Artifacts     []PackageArtifact
	RunSpecRole   string
}

type PackageArtifact struct {
	Role   string
	Path   string
	SHA256 string
	Size   int64
	Mode   uint32
	UID    int
	GID    int
	Links  uint64
}

type ApprovalFormat struct {
	PrepareVerb     string
	LaunchVerb      string
	AdversarialVerb string
	DeleteVerb      string
	DigestHexChars  int
}

type RunSpec struct {
	SchemaVersion int
	RunID         string
	Roots         RootSpec
	Source        SourceSpec
	StageVM       string
	VM            string
	Screen        string
	Artifacts     []ArtifactSpec
	Launch        LaunchSpec
	Network       []NetworkCase
	Limits        Limits
	Approval      ApprovalFormat
}

type RootSpec struct {
	Run, SourceTartHome, QualificationTartHome string
	Forensic, Evidence, Relay, Repository       string
	Installation, Package                      string
}

type SourceSpec struct {
	Name, MAC, ConfigSHA256 string
	RequiredMembers         []string
	Stopped                 bool
}

type ArtifactSpec struct {
	Role, Path, SHA256 string
	Size               int64
}

type LaunchSpec struct {
	Executable  string
	Argv        []string
	Environment []string
	Directory   string
	Stdin       string
	Stdout      string
	Stderr      string
}

type NetworkCase struct {
	ID, AddressFamily, Protocol, DestinationClass string
	Address, PositiveControlID, OracleKind         string
	Port, Attempts, PayloadBytes                   int
	PerAttemptTimeout                              time.Duration
	Expectation                                    claim.NetworkExpectation
}

type Limits struct {
	ComponentReportBytes, RawSerialBytes, GuestReportBytes int64
	OracleEventBytes, FinalManifestBytes                    int64
	NetworkCases, NetworkAttempts, NetworkPayloadBytes      int
	NetworkAttemptsPerSecond                                int
	NetworkPhase                                            time.Duration
	SerialFrames, SerialFrameBytes, SerialTotalBytes         int
	SerialPhase                                             time.Duration
}
```

- [ ] **Step 1: Add complete valid package/spec fixtures and failing strict-parse tests**

The fixtures use `/private/tmp/adr024-test/run-0123abcd` as the synthetic package/run root, distinct `source`, `qualification`, `forensic`, `evidence`, and `relay` descendants, synthetic lowercase 64-hex hashes, the four exact approval verbs with 12 digest characters, 64-case/256-attempt bounds, and one case of each expectation. The package fixture binds roles `controller`, `exec_record`, `observer`, `host_oracle`, `verifier`, `guest_probe`, `run_spec`, and `source_archive` exactly once.

- [ ] **Step 2: Run and observe failure**

Run: `go test ./internal/qualification/adr024/runstate -run 'TestParsePackage|TestPackageAdmit|TestParseSpec|TestSpecRejects' -count=1`

- [ ] **Step 3: Implement strict package admission, decoding, and cross-field validation**

`PackageManifest.Admit` no-follow opens and hashes the approved manifest, rejects a digest mismatch before reading artifact paths, reopens every direct one-link artifact below the admitted package root, requires exact `0555` for executable roles and `0444` for data roles, verifies metadata/size/digest, requires `selfPath` to equal the controller artifact, and parses the exact run-spec artifact. `RunSpec.Validate` requires canonical/physically disjoint roots, unique VM/session/case names, a lowercase eight-hex run ID, exact approval verbs and 12-character authority-digest suffixes, no forensic-root target, exact limits, positive controls for every prohibited network case, and absence of forbidden launch flags.

- [ ] **Step 4: Add mutation-table tests covering every field and limit**

- [ ] **Step 5: Run and pass focused tests**

Run: `go test ./internal/qualification/adr024/runstate -count=1`

- [ ] **Step 6: Commit**

```sh
git add internal/qualification/adr024/runstate/package.go internal/qualification/adr024/runstate/package_test.go internal/qualification/adr024/runstate/spec.go internal/qualification/adr024/runstate/spec_test.go internal/qualification/adr024/testdata/package-manifest-v1.json internal/qualification/adr024/testdata/run-spec-v1.json
git commit -m "feat: bind ADR024 qualification to a sealed package manifest"
```

### Task 3: Semantic Tart and Filesystem State

**Files:**
- Create: `internal/qualification/adr024/runstate/tart.go`
- Create: `internal/qualification/adr024/runstate/file.go`
- Create: `internal/qualification/adr024/runstate/file_darwin.go`
- Create: `internal/qualification/adr024/runstate/file_stub.go`
- Test: `internal/qualification/adr024/runstate/tart_test.go`
- Test: `internal/qualification/adr024/runstate/file_test.go`
- Create: `internal/qualification/adr024/testdata/tart-list-source.json`
- Create: `internal/qualification/adr024/testdata/tart-list-q0.json`
- Create: `internal/qualification/adr024/testdata/tart-list-active.json`
- Create: `internal/qualification/adr024/testdata/tart-list-q1.json`

**Interfaces:**
- Produces: `type VMState`, `type NamespaceState`, `type DiagnosticTimes`, `func ParseTartList([]byte) (NamespaceState, error)`, `func AdmitSource(before, after NamespaceState, expected VMState) error`, `func AdmitQualification(phase Phase, state NamespaceState, expected VMState) error`, and `func InspectDirectTree(root string, policy TreePolicy) (TreeEvidence, error)`.
- Timestamps and inode values are emitted only in `DiagnosticTimes`; admission functions cannot read them.

```go
type Phase string

const (
	PhaseSourceBefore Phase = "source_before"
	PhaseSourceAfter  Phase = "source_after"
	PhaseQ0           Phase = "q0"
	PhasePrepared     Phase = "prepared"
	PhaseRunning      Phase = "running"
	PhaseStopped      Phase = "stopped"
	PhaseQ1           Phase = "q1"
)

type VMState struct {
	Name, Kind, MAC, ConfigSHA256 string
	Running                       bool
	Root                          FileIdentity
	Members                       []FileIdentity
	Config                        []ConfigField
	ControlSocket                 *FileIdentity
	Diagnostics                   []DiagnosticTimes
}

type ConfigField struct {
	Name, Value string
}

type NamespaceState struct {
	TartHome    string
	VMs         []VMState
	TmpChildren []FileIdentity
	Diagnostics []DiagnosticTimes
}

type TreePolicy struct {
	Root             string
	ExpectedUID      int
	ExpectedGID      int
	ExpectedRootMode uint32
	Members          []MemberPolicy
}

type MemberPolicy struct {
	RelativePath string
	Kind         string
	Mode         uint32
	UID, GID     int
	Links        uint64
	SHA256       string
}

type FileIdentity struct {
	Path, Kind, SHA256 string
	Device, Inode      uint64
	UID, GID           int
	Mode                uint32
	Links               uint64
	Size                int64
	Flags               uint32
}

type DiagnosticTimes struct {
	Path                            string
	Accessed, Modified, ChangedNanos int64
}

type TreeEvidence struct {
	Root        FileIdentity
	Members     []FileIdentity
	Diagnostics []DiagnosticTimes
}
```

- [ ] **Step 1: Copy redacted exact real-host Tart JSON shapes into fixtures and write failing semantic tests**

Tests must accept arbitrary atime/mtime/ctime changes for trusted mutable state while rejecting source name/config/MAC/member/running-state changes, unexpected stages, target substitution, wrong control-socket phase, and extra Q objects.

- [ ] **Step 2: Add an explicit regression for `48bac744`**

Construct SOURCE_P0/SOURCE_P1 where only source `tmp`, `vms`, and VM atime/mtime/ctime differ; require admission. Change config bytes, MAC, a member, ownership, mode, or link count one at a time; require rejection.

- [ ] **Step 3: Run and observe failure**

Run: `go test ./internal/qualification/adr024/runstate -run 'TestAdmitSource|TestAdmitQualification|TestInspectDirectTree' -count=1`

- [ ] **Step 4: Implement strict JSON parsing, phase-specific object shapes, no-follow tree inspection, and semantic comparison**

`AdmitSource` compares a `SemanticVM` projection containing name, local/running state, MAC, config digest/fields, member names/digests, owner, mode, and links; it separately requires no stage/temp child. `AdmitQualification` switches on the constants above: Q0/Q1 require zero VMs, prepared requires the exact three-file stopped VM and no `control.sock`, running requires the same VM plus its exact socket, and stopped requires the same identity with the reviewed stopped-after-run socket shape. `DiagnosticTimes` is appended to evidence but has no conversion into `SemanticVM`.

- [ ] **Step 5: Run all runstate tests and a 100-iteration deterministic repeat**

Run: `go test ./internal/qualification/adr024/runstate -count=1 && go test ./internal/qualification/adr024/runstate -run 'TestAdmitSource|TestAdmitQualification' -count=100`

- [ ] **Step 6: Commit**

```sh
git add internal/qualification/adr024/runstate internal/qualification/adr024/testdata/tart-list-*.json
git commit -m "feat: validate ADR024 Tart state by semantic identity"
```

### Task 4: Child-Observed Launch and Owned Process Control

**Files:**
- Create: `internal/qualification/adr024/launch/record.go`
- Create: `internal/qualification/adr024/launch/process.go`
- Create: `internal/qualification/adr024/launch/process_darwin.go`
- Create: `internal/qualification/adr024/launch/process_stub.go`
- Test: `internal/qualification/adr024/launch/record_test.go`
- Test: `internal/qualification/adr024/launch/process_test.go`
- Create: `internal/qualification/adr024/cmd/exec-record/main.go`
- Test: `internal/qualification/adr024/cmd/exec-record/main_test.go`

**Interfaces:**
- Produces: `type Record`, `func RecordAndExec(evidenceFD uintptr, executable string, argv, environment []string) error`, `type Identity`, `type OwnedProcess`, `func StartOwned(Command) (*OwnedProcess, error)`, `func (*OwnedProcess) Revalidate() error`, `func (*OwnedProcess) SignalGroup(os.Signal) error`, and `func (*OwnedProcess) Wait(context.Context) error`.
- The recorder writes its own observed argv/environment/working directory and target identity to an inherited owner-private FD, fsyncs, then uses `syscall.Exec`; PID/PGID continuity is recorded.

```go
type Command struct {
	Name        string
	Executable  string
	Argv        []string
	Environment []string
	Directory   string
	Stdin       *os.File
	Stdout      *os.File
	Stderr      *os.File
	EvidenceFD  *os.File
}

type Identity struct {
	PID, PPID, PGID int
	UniqueID        uint64
	StartUnixMicros int64
	Executable      string
}
```

- [ ] **Step 1: Write failing tests for ordered argv/environment, FD-only evidence, and exec indirection**

Use a helper-test subprocess that records the post-exec PID/PGID. Reject duplicate environment keys, ambient variables, relative executables, shell execution, non-regular targets, and any output path supplied by the child.

- [ ] **Step 2: Write failing fake-kernel tests for identity-bound group signaling**

Cover stable identity, PID reuse, PGID mismatch, leader disappearance, unrelated group member, timeout, successful reap, and the prohibition on Softnet PID signaling.

- [ ] **Step 3: Run and observe failure**

Run: `go test ./internal/qualification/adr024/launch ./internal/qualification/adr024/cmd/exec-record -count=1`

- [ ] **Step 4: Implement the recorder and Darwin process adapter with a deterministic stub**

`StartOwned` validates the executable/argv/environment, opens the evidence file before spawning, sets a new child process group, starts only `cmd/exec-record`, obtains its kernel unique/start identity, and returns that closed authority object. `RecordAndExec` captures `os.Getpid`, `os.Getppid`, `syscall.Getpgrp`, `os.Getwd`, `os.Args`, and `os.Environ`, writes one bounded canonical record to the inherited FD, fsyncs/closes it, and calls `syscall.Exec(executable, argv, environment)`. `SignalGroup` first revalidates PID, unique ID, start time, executable, and leader PGID, then signals `-PGID`; no API accepts a Softnet PID as signal authority.

- [ ] **Step 5: Run focused tests and race detection**

Run: `go test -race ./internal/qualification/adr024/launch ./internal/qualification/adr024/cmd/exec-record -count=1`

- [ ] **Step 6: Commit**

```sh
git add internal/qualification/adr024/launch internal/qualification/adr024/cmd/exec-record
git commit -m "feat: bind ADR024 launch evidence to the child process"
```

### Task 5: Observer and Privileged Host-State Integration

**Files:**
- Modify: `internal/qualification/adr024/observer.go`
- Modify: `internal/qualification/adr024/observer_test.go`
- Modify: `internal/qualification/adr024/run.go`
- Modify: `internal/qualification/adr024/state.go`
- Create: `internal/qualification/adr024/hoststate/bootpd.go`
- Create: `internal/qualification/adr024/hoststate/capabilities.go`
- Create: `internal/qualification/adr024/hoststate/tree.go`
- Create: `internal/qualification/adr024/hoststate/file_darwin.go`
- Create: `internal/qualification/adr024/hoststate/file_stub.go`
- Test: `internal/qualification/adr024/hoststate/bootpd_test.go`
- Test: `internal/qualification/adr024/hoststate/capabilities_test.go`
- Test: `internal/qualification/adr024/hoststate/tree_test.go`
- Create: `internal/qualification/adr024/testdata/bootpd.xml`
- Create: `internal/qualification/adr024/testdata/tart-capabilities-2.32.1.json`

**Interfaces:**
- Produces: `hoststate.LoadFixed()`, `hoststate.CaptureBootpd()`, `hoststate.CompareBootpd(before, after)`, and observer-to-claim evidence conversion without changing `ObserveFixed(ctx, tartPID)` caller identity.
- `BootpdEvidence` separates `Protected` fields from `Diagnostic` inode/timestamps; `CompareBootpd` reads only `Protected`.

```go
type FixedEvidence struct {
	Manifest adr024.FileEvidence
	Tart     adr024.FileEvidence
	Softnet  adr024.FileEvidence
	Ancestry []FileIdentity
}

type BootpdEvidence struct {
	Protected  BootpdProtected
	Diagnostic DiagnosticIdentity
}

type BootpdProtected struct {
	Path, Kind, SHA256, XML string
	Device, Links           uint64
	UID, GID                int
	Mode, Flags             uint32
	DHCPLeaseTimeSecs       int
}

type DiagnosticIdentity struct {
	Inode                           uint64
	Accessed, Modified, ChangedNanos int64
}

type CapabilityMap struct {
	SchemaVersion int
	TartVersion   string
	TartSHA256    string
	Capabilities []CapabilityRule
}

type CapabilityRule struct {
	ClaimID         claim.ID
	EnableFlags     []string
	RequiredFlags   []string
	ForbiddenEnvKey []string
}

func LoadFixed() (FixedEvidence, error)
func CaptureBootpd(path string) (BootpdEvidence, error)
func CompareBootpd(before, after BootpdEvidence) error
func ParseCapabilityMap(raw []byte) (CapabilityMap, error)
func (CapabilityMap) AdmitLaunch(runstate.LaunchSpec) ([]claim.ClaimResult, error)
func ClaimsFromObserver(report adr024.Report) ([]claim.ClaimResult, error)
```

- [ ] **Step 1: Add failing bootpd replacement tests**

The accepted fixture changes inode/atime/mtime/ctime but preserves canonical path, direct type, device, UID `0`, GID `0`, mode `0644`, links `1`, flags, exact XML/SHA-256, and sole `DHCPLeaseTimeSecs=600`. Mutation tests change each protected field and reject duplicate/missing/non-integer/other lease semantics.

- [ ] **Step 2: Add observer claim-conversion tests**

Require direct ancestry and stable unique IDs, accept independent PGIDs, require 100 steady operator samples, make root sample optional, and preserve every sampling limitation.

- [ ] **Step 3: Add a failing exact Tart 2.32.1 capability-map test**

Map `--dir`/additional `--disk` to host filesystem/disk exposure;
`--net-bridged`, `--vnc`, `--vnc-experimental`, and
`--capture-system-keys` to prohibited host interaction; `--rosetta` and
`--nested` to prohibited execution capability; `--net-softnet-expose` to
prohibited inbound exposure; and `--no-audio`/`--no-clipboard` to required
negative controls. Also map closed-environment SSH/GPG-agent and display socket
variables plus host-share absence to the credential/control IPC claims. Require
an unknown Tart version, unknown enabling flag, missing required negative flag,
or ambient integration variable to fail closed. Guest inventory non-observation
must not appear in `AdmitLaunch` inputs.

- [ ] **Step 4: Run and observe failure**

Run: `go test ./internal/qualification/adr024 ./internal/qualification/adr024/hoststate -count=1`

- [ ] **Step 5: Implement protected/diagnostic state separation, capability mapping, and claim conversion**

`CaptureBootpd` uses no-follow open/revalidation, hashes the exact XML bytes, and strictly parses one integer `DHCPLeaseTimeSecs` value. `CompareBootpd` uses `before.Protected == after.Protected` and never compares `DiagnosticIdentity{Inode, Accessed, Modified, Changed}`. `ClaimsFromObserver` emits `C2` and `C4` only for an exact passing report with stable fixed state and all limitation flags preserved; it records root observation as diagnostic and cannot emit `C3` without the verifier's separate artifact/launch/behavior inputs.

`ParseCapabilityMap` admits only schema version 1 and the exact qualified Tart
2.32.1 executable/source identity. `AdmitLaunch` compares the ordered argv and
closed environment to the complete map and emits `B1`–`B4` from host-side
configuration evidence; the guest report can only contradict/corroborate those
results in the final verifier, never create them.

- [ ] **Step 6: Run observer tests 100 times plus the architecture boundary**

Run: `go test ./internal/qualification/adr024/... ./internal/architecture -count=1 && go test ./internal/qualification/adr024 -run 'TestObserver' -count=100`

- [ ] **Step 7: Commit**

```sh
git add internal/qualification/adr024/observer.go internal/qualification/adr024/observer_test.go internal/qualification/adr024/run.go internal/qualification/adr024/state.go internal/qualification/adr024/hoststate internal/qualification/adr024/testdata/bootpd.xml internal/qualification/adr024/testdata/tart-capabilities-2.32.1.json
git commit -m "feat: map privileged ADR024 evidence to explicit claims"
```

### Task 6: Host Network Oracle

**Files:**
- Create: `internal/qualification/adr024/oracle/model.go`
- Create: `internal/qualification/adr024/oracle/listener.go`
- Create: `internal/qualification/adr024/oracle/verify.go`
- Test: `internal/qualification/adr024/oracle/listener_test.go`
- Test: `internal/qualification/adr024/oracle/verify_test.go`
- Create: `internal/qualification/adr024/cmd/host-oracle/main.go`
- Test: `internal/qualification/adr024/cmd/host-oracle/main_test.go`

**Interfaces:**
- Consumes: validated `[]runstate.NetworkCase` and one 128-bit run nonce through an inherited FD.
- Produces: `type Event`, `type Report`, `func Start(plan Plan) (*Oracle, error)`, `func (*Oracle) Stop(context.Context) (Report, error)`, and `func Verify(plan Plan, report Report) []claim.ClaimResult`.
- Every listener binds one exact address/port with exclusive collision checks and records only case ID, monotonic offset, protocol, byte count, peer class, and nonce match; it never records arbitrary payload or unrelated traffic.

```go
type Plan struct {
	RunID  string
	Nonce  [16]byte
	Cases  []runstate.NetworkCase
	Limits runstate.Limits
}

type Event struct {
	CaseID         string
	MonotonicNanos int64
	Protocol       string
	Bytes          int
	PeerClass      string
	NonceMatch     bool
}
```

- [ ] **Step 1: Write failing TCP/UDP positive, prohibited, collision, timeout, duplicate, wrong-nonce, and output-bound tests**

- [ ] **Step 2: Add matrix-verdict tests**

Expected cases require exact matching events. Prohibited cases require zero matching events and a passing adjacent positive control. `not_qualified` cases cannot affect pass/fail and must remain explicit.

- [ ] **Step 3: Run and observe failure**

Run: `go test ./internal/qualification/adr024/oracle ./internal/qualification/adr024/cmd/host-oracle -count=1`

- [ ] **Step 4: Implement bounded listener lifecycle and canonical report output**

`Start` sorts and validates cases, permits only exact preflight-enumerated local
interface addresses, rejects wildcard/multicast/public/external binds unless
that exact class has separate approval in the run spec, binds every
`host_listener` TCP/UDP endpoint before returning, fails on any collision, and
starts one bounded goroutine per socket under a shared context. Each listener
accepts at most four connections/datagrams and 512 bytes per event, enforces
deadlines before allocation, never replies except with the exact fixed positive-
control nonce, and stops at 256 aggregate events. `Stop` cancels, closes, joins
every goroutine, sorts events, records its process/socket identity and close
confirmation, and returns exactly one report. `Verify` requires a matching
event for expected cases, zero matching events plus the named passing positive
control and complete controller-issued stimulus schedule for prohibited cases,
and a literal `not_qualified` result for all other cases.

Use only unprivileged TCP/UDP sockets. Do not add sudo, BPF, libpcap, auditd, Endpoint Security, or a persistent service. ICMP, IP-fragment, and raw-frame policy cases remain `not_qualified` in schema version 1 because this oracle cannot independently observe their disposition; adding such an oracle requires separate review.

- [ ] **Step 5: Run race and repetition tests**

Run: `go test -race ./internal/qualification/adr024/oracle/... -count=1 && go test ./internal/qualification/adr024/oracle -count=50`

- [ ] **Step 6: Commit**

```sh
git add internal/qualification/adr024/oracle internal/qualification/adr024/cmd/host-oracle
git commit -m "feat: add bounded host-authoritative ADR024 network oracles"
```

### Task 7: Hash-Bound Guest-Root Probe

**Files:**
- Create: `internal/qualification/adr024/probe/model.go`
- Create: `internal/qualification/adr024/probe/execute_linux.go`
- Create: `internal/qualification/adr024/probe/execute_stub.go`
- Create: `internal/qualification/adr024/probe/inventory.go`
- Create: `internal/qualification/adr024/probe/network.go`
- Create: `internal/qualification/adr024/probe/serial.go`
- Test: `internal/qualification/adr024/probe/model_test.go`
- Test: `internal/qualification/adr024/probe/inventory_test.go`
- Test: `internal/qualification/adr024/probe/network_test.go`
- Test: `internal/qualification/adr024/probe/serial_test.go`
- Create: `internal/qualification/adr024/cmd/guest-probe/main.go`
- Test: `internal/qualification/adr024/cmd/guest-probe/main_test.go`

**Interfaces:**
- Consumes: one strict canonical `probe.Plan` on stdin containing the run nonce, bounded inventory roots, network cases, serial corpus IDs, and exact limits.
- Produces: one canonical `probe.Report` on stdout with attempted stimuli and observations; status values are descriptive and never final ADR024 verdicts.
- Linux/arm64 build is static with `CGO_ENABLED=0`; non-Linux execution emits one deterministic unsupported report and exits nonzero.

```go
type Plan struct {
	SchemaVersion int
	RunID         string
	Nonce         [16]byte
	Inventory     []InventoryRequest
	Network       []runstate.NetworkCase
	Serial        []SerialCase
	Limits        runstate.Limits
}

type Report struct {
	SchemaVersion int
	RunID         string
	Nonce         [16]byte
	Inventory     []InventoryObservation
	Stimuli       []StimulusRecord
	Diagnostics   []claim.Diagnostic
}

type InventoryRequest struct {
	ID, Kind, Path string
	MaxEntries, MaxBytes, MaxDepth int
}

type SerialCase struct {
	ID      string
	Payload []byte
}

type InventoryObservation struct {
	ID, Kind string
	Values   []string
}

type StimulusRecord struct {
	CaseID, Kind string
	Attempts, Bytes int
}

type System interface {
	ReadFile(path string, maxBytes int64) ([]byte, error)
	ReadDir(path string, maxEntries int) ([]string, error)
	Dial(ctx context.Context, network, address string, payload []byte) (int, error)
	RenewDHCP(ctx context.Context) error
	WriteSerial(payload []byte) error
}

func ParsePlan(raw []byte) (Plan, error)
func Execute(ctx context.Context, plan Plan, system System) (Report, error)
```

- [ ] **Step 1: Write failing plan/report and non-Linux refusal tests**

- [ ] **Step 2: Write fixture-driven inventory tests**

Cover mounts, block devices, PCI/virtio devices, interfaces, routes, neighbors, resolver/DHCP state, environment, Unix sockets, and candidate integration paths. Bound every file count, line length, recursion depth, total bytes, and command duration.

- [ ] **Step 3: Write fake-socket network and serial corpus tests**

Require at most 64 cases, 256 attempts, 20 attempts/second, 512-byte payloads, 90 seconds, plus 64 serial frames/4 KiB each/64 KiB total/30 seconds. Include TCP, UDP, IPv4, IPv6, DNS, DHCP-renewal record, gateway, host, private/VPN/link-local, multicast/broadcast/mDNS, and the fixed malformed TCP/UDP application-payload corpus. Require malformed transport-header, ICMP, IP-fragment, and raw-frame entries to remain `not_qualified` and unexecuted in schema version 1.

- [ ] **Step 4: Run and observe failure**

Run: `go test ./internal/qualification/adr024/probe ./internal/qualification/adr024/cmd/guest-probe -count=1`

- [ ] **Step 5: Implement standard-library-only collection and stimulus generation**

`Execute` iterates only the sorted plan entries. Inventory readers use no-follow bounded reads of the exact Linux `/proc`, `/sys`, `/run`, and `/etc` paths named in the plan. Network stimulus uses `net.Dialer` for TCP, `net.DialUDP` for UDP, and fixed byte slices keyed by case ID; the rate limiter and overall context enforce the spec's counts and deadlines. Serial cases emit only the fixed encoded corpus and record case IDs/byte counts. The program never scans beyond the plan, reads real credentials, changes persistent guest configuration, installs software, or declares a security PASS.

- [ ] **Step 6: Cross-build and verify deterministic behavior**

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o /private/tmp/boxwarden-adr024-guest-probe ./internal/qualification/adr024/cmd/guest-probe && go test -race ./internal/qualification/adr024/probe/... -count=1`

- [ ] **Step 7: Commit**

```sh
git add internal/qualification/adr024/probe internal/qualification/adr024/cmd/guest-probe
git commit -m "feat: add bounded ADR024 guest-root adversarial probe"
```

### Task 8: Host-Authoritative Evidence Verifier

**Files:**
- Create: `internal/qualification/adr024/evidence/report.go`
- Create: `internal/qualification/adr024/evidence/verify.go`
- Test: `internal/qualification/adr024/evidence/report_test.go`
- Test: `internal/qualification/adr024/evidence/verify_test.go`
- Create: `internal/qualification/adr024/cmd/verify/main.go`
- Test: `internal/qualification/adr024/cmd/verify/main_test.go`
- Create: `internal/qualification/adr024/testdata/guest-report-v1.json`
- Create: `internal/qualification/adr024/testdata/oracle-report-v1.json`
- Create: `internal/qualification/adr024/testdata/observer-report-v1.json`

**Interfaces:**
- Consumes: exact `RunSpec`, component artifact manifest, launch record, source/Q/host/relay/process reports, existing observer report, guest report, oracle report, and raw serial digest/size.
- Produces: `func Verify(Input) (claim.Envelope, error)` and one final `claims.json` plus an artifact hash manifest.
- Authority order: immutable spec and host observations decide claims; guest report may prove only that the reviewed stimulus generator reported an attempt or a functional response, never that a prohibited host path was absent.

```go
type Input struct {
	Spec             runstate.RunSpec
	Artifacts        []claim.Artifact
	Launch           launch.Record
	SourceBefore     runstate.NamespaceState
	SourceAfter      runstate.NamespaceState
	Qualification    []runstate.NamespaceState
	HostBefore       hoststate.FixedEvidence
	HostAfter        hoststate.FixedEvidence
	BootpdBefore     hoststate.BootpdEvidence
	BootpdAfter      hoststate.BootpdEvidence
	Relay            RelayReport
	Processes        ProcessReport
	ObserverInitial  adr024.Report
	ObserverFinal    adr024.Report
	Guest            probe.Report
	Oracle           oracle.Report
	RawSerial        claim.Artifact
	DeletionCommitted bool
}

type RelayReport struct {
	RunID       string
	Relay       launch.Identity
	Screen      launch.Identity
	TartPTY     runstate.FileIdentity
	OperatorPTY runstate.FileIdentity
	RawSerial   claim.Artifact
}

type ProcessReport struct {
	RunID   string
	Tart    launch.Identity
	Softnet adr024.ProcessEvidence
	Owned   []launch.Identity
	Reaped  []launch.Identity
}

func Verify(input Input) (claim.Envelope, error)
```

- [ ] **Step 1: Write a complete passing fixture and remove one required claim at a time**

Every omitted, duplicate, contradicted, or evidence-free blocking claim must fail. Every explicitly unqualified network case must remain visible without failing unrelated claims.

- [ ] **Step 2: Add adversarial guest-report tests**

Feed fabricated PASS values, duplicate case IDs, wrong nonce, oversized arrays, prohibited-listener receipt hidden by the guest, missing positive controls, and mismatched artifact hashes. Require host evidence to win every conflict.

- [ ] **Step 3: Run and observe failure**

Run: `go test ./internal/qualification/adr024/evidence ./internal/qualification/adr024/cmd/verify -count=1`

- [ ] **Step 4: Implement strict correlation, authority precedence, and final manifest hashing**

`Verify` first validates every schema/run ID/nonce/hash, then derives results in catalog order. It calls `runstate`, `hoststate`, observer, launch, relay/process, and oracle validators; it accepts guest observations only as stimulus/corroboration. Conflicts resolve in favor of host evidence, any missing applicable claim is failure, and `not_qualified` is copied only from the immutable spec. It hashes the exact component bytes before creating the final canonical envelope.

- [ ] **Step 5: Run tests with race detection and a 100-run fixture repeat**

Run: `go test -race ./internal/qualification/adr024/evidence/... -count=1 && go test ./internal/qualification/adr024/evidence -run TestVerifyCompleteFixture -count=100`

- [ ] **Step 6: Commit**

```sh
git add internal/qualification/adr024/evidence internal/qualification/adr024/cmd/verify internal/qualification/adr024/testdata/*-report-v1.json
git commit -m "feat: verify ADR024 claims from host-authoritative evidence"
```

### Task 9: Lean Human-Gated Controller

**Files:**
- Create: `internal/qualification/adr024/controller/controller.go`
- Create: `internal/qualification/adr024/controller/phases.go`
- Create: `internal/qualification/adr024/controller/resources.go`
- Create: `internal/qualification/adr024/controller/resources_darwin.go`
- Create: `internal/qualification/adr024/controller/resources_stub.go`
- Create: `internal/qualification/adr024/controller/evidence.go`
- Test: `internal/qualification/adr024/controller/controller_test.go`
- Test: `internal/qualification/adr024/controller/phases_test.go`
- Test: `internal/qualification/adr024/controller/failure_test.go`
- Create: `internal/qualification/adr024/cmd/controller/main.go`
- Test: `internal/qualification/adr024/cmd/controller/main_test.go`

**Interfaces:**
- Consumes: Tasks 1–8 packages through interfaces injected into `Controller`.
- Produces: `type Phase`, `type Controller`, `func New(Dependencies, runstate.RunSpec) (*Controller, error)`, `func (*Controller) Run(context.Context, TokenReader) error`, and `func Execute(ctx context.Context, args []string, tty io.ReadWriter, stdout, stderr io.Writer) int`.
- Phases are `preflight`, `prepared`, `launched`, `adversarial_complete`, `stopped`, `delete_committed`, `complete`, and `failed_preserved`.

```go
type TokenReader interface {
	ReadToken(prompt, expected string) error
}

type TartRunner interface {
	List(context.Context) ([]byte, error)
	Clone(context.Context, source, stage string) error
	RandomizeMAC(context.Context, stage string) error
	MoveStage(context.Context, stage, target string) error
	Start(context.Context, runstate.LaunchSpec, *os.File) (*launch.OwnedProcess, error)
	Stop(context.Context, string) error
	Delete(context.Context, string) error
}

type Dependencies struct {
	Tart       TartRunner
	State      StateInspector
	Relay      RelayController
	Screen     ScreenController
	Observer   ObserverController
	Oracle     OracleController
	Probe      ProbeTransport
	Verifier   EvidenceVerifier
	Evidence   EvidenceStore
	Clock      Clock
}

type Phase string

type StateInspector interface {
	Source(context.Context) (runstate.NamespaceState, error)
	Qualification(context.Context) (runstate.NamespaceState, error)
	FixedHost(context.Context) (hoststate.FixedEvidence, error)
	Bootpd(context.Context) (hoststate.BootpdEvidence, error)
}

type RelayController interface {
	Start(context.Context, runstate.RunSpec) (RelayHandle, error)
	Stop(context.Context, RelayHandle) error
}

type RelayHandle struct {
	Process     *launch.OwnedProcess
	TartPTY     runstate.FileIdentity
	OperatorPTY runstate.FileIdentity
}

type ScreenHandle struct {
	Identity launch.Identity
	Label    string
	Socket   runstate.FileIdentity
}

type TransferBatch struct {
	Path, SHA256 string
	Size         int64
}

type ScreenController interface {
	Start(context.Context, runstate.RunSpec, RelayHandle) (ScreenHandle, error)
	Transfer(context.Context, ScreenHandle, TransferBatch) error
	Stop(context.Context, ScreenHandle) error
}

type ObserverController interface {
	Observe(context.Context, int) (adr024.Report, error)
}

type OracleController interface {
	Start(context.Context, oracle.Plan) (*oracle.Oracle, error)
	Stop(context.Context, *oracle.Oracle) (oracle.Report, error)
}

type ProbeTransport interface {
	BuildBatch(runstate.RunSpec, probe.Plan) (TransferBatch, error)
	Collect(context.Context, ScreenHandle) (probe.Report, claim.Artifact, error)
}

type EvidenceVerifier interface {
	Verify(evidence.Input) (claim.Envelope, error)
}

type EvidenceStore interface {
	WriteCanonical(role string, value any, maxBytes int64) (claim.Artifact, error)
	Fsync() error
}

type Clock interface {
	Now() time.Time
}

type AuthorityRecord struct {
	SchemaVersion int
	RunID         string
	Phase         Phase
	Source        runstate.VMState
	VM            *runstate.VMState
	Processes     []launch.Identity
	EvidenceSHA256 string
	Mutation      []string
}
```

- [ ] **Step 1: Write failing phase/token tests**

Prove no mutation before the prepare token, no launch before the launch token, no active probes before the adversarial token, and no deletion before the delete token. Each expected token includes the digest-derived `AUTH12`; reject a changed authority record, wrong/replayed/prefetched token, or EOF.

- [ ] **Step 2: Write failing complete success-state tests with fakes**

Assert the exact sequence: preflight → clone → random MAC → move → relay → Screen → prelaunch revalidation → exec-record/Tart → observer → guest transfer → oracles/probe → final observer → stop/reap → preserve-for-review → delete token → delete → Q1 → verify.

Acquire a fake exclusive qualification-home lock before Q0 and assert it remains
held across every Tart call and the delete commit point. Add failure cases for a
pre-existing lock, symlink, second controller, lock identity change, and lost
lock; none may invoke clone, launch, or delete.

- [ ] **Step 3: Write failing fault-injection tests at every external operation**

For each phase, assert exact owned containment, no Softnet PID signal, no unrelated process/VM target, no automatic delete, truthful commit-point status, immutable evidence preservation, and a fresh-run requirement.

- [ ] **Step 4: Write framed serial transport tests**

Cover split markers, duplicated/wrong nonces, hostile ANSI/OSC/DCS bytes, malformed base64, declared/actual length mismatch, wrong digest, output exhaustion, and guest termination. Raw bytes must never appear on controller stdout/stderr.

- [ ] **Step 5: Run and observe failure**

Run: `go test ./internal/qualification/adr024/controller ./internal/qualification/adr024/cmd/controller -count=1`

- [ ] **Step 6: Implement only orchestration and phase evidence**

Call exact Tart 2.32.1 forms `clone <source> <stage>`, `set <stage> --random-mac`, `list --format json`, `run --net-softnet --no-audio --no-clipboard --serial-path <exact-relay-endpoint> <exact-vm>`, `stop <exact-vm>`, and `delete <exact-vm>` through the qualification-home environment while the lock is held. The angle-bracketed values are fields from the already validated immutable `RunSpec`, never free-form runtime input. Immediately before delete, compare the current device/inode/config/NVRAM/MAC/name state with the delete authority record, then invoke Tart by exact name under the locked exact `TART_HOME`. Do not embed pure fixture tests, shell parsers, global process searches, or generic cleanup.

Implement `Run` as an explicit switch over the phase constants. Each transition writes and fsyncs its phase report before the next prompt. A deferred failure handler receives only already-admitted `OwnedProcess`, relay, oracle, and Screen authority objects; it contains those identities, sets `failed_preserved`, and never calls `Delete`. `delete_committed` is persisted immediately before the exact Tart delete call so post-failure reporting cannot misstate whether irreversible authority was consumed.

For the qualification-only guest transport, create an owner-only ASCII batch containing the base64-encoded static probe and plan, exact decoded lengths and SHA-256 values, and commands using `/usr/bin/base64`, `/usr/bin/sha256sum`, and `/usr/bin/sudo -n`. Feed that private batch only to the exact detached run-owned Screen session with direct argument-vector invocations of `screen -S <label> -X readbuf <batch>` and `screen -S <label> -X paste .`. This Screen control path is qualification-only, is never production lifecycle behavior, and does not qualify V4's native broker. Keep Screen detached, retain bounded raw serial evidence, parse only the nonce/length/digest frame, and never copy raw guest bytes to stdout/stderr.

- [ ] **Step 7: Run race, repetition, and non-Darwin refusal tests**

Run: `go test -race ./internal/qualification/adr024/controller/... -count=1 && go test ./internal/qualification/adr024/controller -run 'TestRun|TestFailure' -count=100`

- [ ] **Step 8: Commit**

```sh
git add internal/qualification/adr024/controller internal/qualification/adr024/cmd/controller
git commit -m "feat: orchestrate claim-driven ADR024 attended qualification"
```

### Task 10: Deterministic Package, Documentation, and Review Gate

**Files:**
- Create: `scripts/qualification/build-adr024-package.sh`
- Create: `scripts/qualification/build-adr024-package_test.sh`
- Create: `scripts/qualification/testdata/expected-package-manifest.json`
- Modify: `docs/operations/adr024-runtime-observer.md`
- Modify: `docs/evidence/v3-host-domain-attended-gates.md`
- Modify: `memory/knowledge/tart-and-guest-platform-facts.md`
- Modify: `internal/architecture/backend_seam_test.go`

**Interfaces:**
- Produces one owner-private package directory containing exact controller, exec recorder, observer, host oracle, verifier, guest probe, run spec, source archive, and `package-manifest.json`; every member has path, SHA-256, size, mode, owner/group, and link count. The manifest is the sole human-approved digest root for attended admission.
- The build script does not seal or execute the package and refuses a dirty tracked/index state. The architecture test explicitly permits qualification imports only from paths below `internal/qualification` and rejects imports from `cmd/boxwarden` and every other production package.

- [ ] **Step 1: Write the failing shell package test**

Test source-head binding, offline/trimpath builds, Linux/arm64 guest probe, deterministic sorted manifest, one-link regular files, no unexpected member, and refusal on dirty or mismatched source.

- [ ] **Step 2: Run and observe failure**

Run: `/bin/bash scripts/qualification/build-adr024-package_test.sh`

- [ ] **Step 3: Implement the packaging script using explicit arrays and exact paths**

The script sets `umask 077`, accepts an explicit output directory and run-spec source, builds the controller, exec recorder, observer, host oracle, and verifier with fixed `go build -trimpath -buildvcs=true` package paths, cross-builds only `cmd/guest-probe` with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64`, hashes each direct regular file with `/usr/bin/shasum -a 256`, and emits one sorted manifest through the repository's Go canonical encoder. It rejects symlinks, non-one-link files, unexpected output members, dirty tracked/index state, source-head mismatch, or an existing output directory. After review, a separate attended sealing command changes each exact executable from `0755` to `0555` and each exact data artifact from `0644` to `0444`, revalidates every hash/metadata field, and supplies only the package-manifest digest to the controller. Do not use `eval`, sourced untrusted files, wildcard deletion, network access, sudo, or runtime tools. The generated package remains writable until separately reviewed and explicitly sealed.

- [ ] **Step 4: Run the complete deterministic verification**

Run:

```sh
go fmt ./internal/qualification/adr024/...
go vet ./...
go test ./... -count=1
go test -race ./internal/qualification/adr024/... -count=1
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o /private/tmp/boxwarden-adr024-guest-probe ./internal/qualification/adr024/cmd/guest-probe
/bin/bash -n scripts/qualification/build-adr024-package.sh scripts/qualification/build-adr024-package_test.sh
/bin/bash scripts/qualification/build-adr024-package_test.sh
```

- [ ] **Step 5: Run fresh-only/static safety scans**

Search for production imports of qualification code, `sudo`, PID-only `kill`, `StrictHostKeyChecking=no`, `--net-softnet-allow=0.0.0.0/0`, forensic-home Tart targets, wildcard deletion, guest PASS adjudication, timestamp fields in semantic blockers, and unclassified network cases. Every match must be explained or removed.

- [ ] **Step 6: Perform the attempt-minimization pass**

Trace every remaining external command and producer/consumer contract once against retained real-host fixtures. Stop when known platform behaviors have tested contracts, every unexecuted phase has one mechanical/source review, and no Critical/Important issue remains.

- [ ] **Step 7: Obtain independent reviews**

Use parallel Terra/high reviews for controller/process/cleanup and guest/oracle/evidence boundaries, a Luna/medium mechanical audit for claim coverage/file paths/commands, then one Sol High whole-system adjudication. Close every Critical/Important finding with tests and normal corrective commits.

- [ ] **Step 8: Record final package identities and stop before runtime**

Report exact paths, hashes, sizes, ownership, modes, links, source head, run ID, component-to-claim map, deterministic results, review verdicts, and the exact first attended seal/preflight command. Do not seal, execute, launch, clone, or delete anything without new human authorization.

- [ ] **Step 9: Commit and publish the verified implementation series**

```sh
git add scripts/qualification internal/architecture/backend_seam_test.go docs/operations/adr024-runtime-observer.md docs/evidence/v3-host-domain-attended-gates.md memory/knowledge/tart-and-guest-platform-facts.md
git commit -m "docs: prepare claim-driven ADR024 qualification gate"
git push
```

This is a subsequent push to the already published
`weshofmann/feat/host-domain-foundation` branch. If an isolated implementation
branch is published instead, its first publication uses
`git push -u origin <exact-branch-name>`.

## Acceptance Criteria

- Every runtime blocker emits exactly one approved claim ID and cites one or more hash-addressed artifacts.
- The one human-approved package-manifest digest binds the controller, run spec, source archive, and every helper/probe; runtime-supplied sibling hashes cannot substitute for that trust root.
- No semantic source/Q/qualification admission reads trusted mutable atime, mtime, ctime, or parent-directory timestamp values.
- Immutable controller/helper/spec/tool/manifest/forensic/evidence objects retain exact byte and security-metadata admission.
- The exact `48bac744` atime-only SOURCE_P0→SOURCE_P1 fixture passes; every claim-bearing identity/config/member/ownership/mode/link mutation fails.
- Bootpd inode/timestamp replacement with exact protected state passes; any protected path/type/device/owner/mode/link/flags/bytes/XML/semantic change fails.
- Exact child-observed Tart executable/argv/environment/working-directory evidence is present before Tart runs.
- Observer evidence accepts independent Tart/Softnet PGIDs, requires exact direct ancestry and stable unique identities, and proves the bounded steady operator-credential sequence without claiming losslessness.
- `B1`–`B4` are derived from exact child-observed launch evidence and the reviewed Tart 2.32.1 capability map; guest inventory/probe output cannot create or override those results.
- Guest inventory/probe output cannot override launch evidence or a host oracle and cannot independently satisfy a prohibited-reachability claim.
- Every network case has an exact expectation, finite limits, and an evidence path; absence for a prohibited case is admitted only with the complete host-issued per-case schedule, exact probe/transport identity, exact host listener report, and its passing adjacent positive control. The result is scoped to that controlled finite experiment.
- Guest-controlled serial bytes remain private bounded raw evidence and never reach controller/operator output unescaped.
- Fault injection at every external operation stops only revalidated owned processes, never PID-signals Softnet, never targets forensic/unrelated state, never auto-deletes, and records truthful failure state.
- An exact owner-private qualification-home lock is held from Q0 through delete/Q1; target identity is revalidated against the digest-bound delete authority record immediately before the exact routed delete.
- A deterministic full suite, race suite, 100-run state/verifier/controller repetitions, cross-build, shell syntax/package tests, and safety scans pass.
- Independent Terra/high and Luna/medium reviews plus final Sol High adjudication contain no open Critical/Important findings.
- The host controller is materially simpler than the 2,014-line/136-KB monolith. The audit estimate is approximately 900–1,100 lines / 60–80 KB for controller-specific code, but LOC is not a pass criterion and helper/probe code may increase total assurance code.
- One wholly fresh attended run eventually completes preflight, prepare, launch, adversarial tests, exact normal stop, separately authorized delete, Q1, and final verification without relying on any failed runtime state.
- PR #2 remains Draft until that fresh run passes and its redacted evidence is reviewed; nothing is merged by this plan.
