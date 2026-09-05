# ADR 024 claim-driven runtime qualification

This repository contains a private, unprivileged process observer at
`internal/qualification/adr024/cmd/observe`. It is one attended qualification
instrument for ADR 024. It is not installed by `boxwarden init`, exposed by the
normal `boxwarden` CLI, imported by production lifecycle code, or retained as a
host service.

The observer accepts exactly one caller-supplied runtime identity: a positive
decimal Tart PID passed as `--tart-pid`. It never searches for Tart or Softnet
by process name and accepts no override for an executable, digest, UID, GID,
operator identity, interval, or evidence threshold. Expected identities come
from the fixed root-owned Boxwarden manifest, parsed by `hostx.ParseManifest`,
and the exact manifested Tart and Softnet files are reopened without following
symlinks and checked against their manifested digests before and after
sampling.

The observer uses read-only Darwin `libproc` calls. It does not invoke sudo,
launch or signal a process, attach or trace, suspend execution, inject an
environment, write a file, or change system configuration. Darwin with cgo is
required; other builds contain a deterministic refusal stub. Qualification
code is isolated below `internal/qualification`, and an architecture test
prevents production packages from importing it.

A fresh, unprivileged `boxwarden doctor` must report healthy immediately before
the attended launch. Doctor owns complete host-state and ACL diagnosis. The
observer deliberately does not rerun doctor because doing so would spawn its
fixed external inspection commands. Instead, it independently reopens the
fixed manifest, Tart, and Softnet files without following symlinks; validates
their required type and security metadata, single-link state, and digests; and
repeats that admission after
sampling. Any pre/post identity difference fails the observation.

Build it from the exact reviewed V3 commit:

```sh
CGO_ENABLED=1 go build -trimpath -buildvcs=true \
  -o /private/tmp/boxwarden-adr024-observe \
  ./internal/qualification/adr024/cmd/observe
```

The attended invocation has only this form:

```sh
/private/tmp/boxwarden-adr024-observe --tart-pid <exact-decimal-tart-pid>
```

It emits exactly one bounded JSON object followed by one newline on stdout and
creates no evidence file. The caller decides whether to retain the private
output and separately produces a redacted public record.

## Reusable attended method

ADR024 qualification is claim-driven. The primary boundary is the trusted
host/operator versus an untrusted guest, including guest root. Every blocking
assertion must name the claim it supports: hostile-guest containment, trusted
launch/configuration correctness, privileged-component security,
lifecycle/unrelated-state safety, or evidence/forensic integrity. An
observable host property is not blocking merely because it can be measured.

Keep immutable trust and forensic state strict: exact reviewed controller and
tool identities, security-relevant manifests, forensic seals, retained failed
runs, and immutable evidence artifacts require exact bytes and applicable
security metadata. Treat trusted mutable operational state semantically:
source and qualification Tart state, Tart temporary namespaces, and
disposable VMs are admitted by identity, state, object shape, ownership/mode
where security-relevant, and isolation/lifecycle properties. Ordinary atime,
mtime, ctime, and parent-directory timestamp choreography are diagnostic
unless a named claim requires them. This timestamp demotion does not apply to
immutable evidence.

Begin with a fresh B0 snapshot of claim-bearing host-toolchain and runtime
objects, plus diagnostic observations useful for investigation. Create a fresh
disposable VM, use the corrected controller built from the reviewed commit,
and record the exact launch inputs. During launch through observer admission,
observe the controller-owned Tart job, direct Tart-to-Softnet ancestry, unique
process identities and start times, executable paths and digests, and
credentials. Exercise the qualified network behavior and host-local serial
relay, then perform a normal stop and identity-bound cleanup. Finish with a
fresh B1 snapshot and compare it with B0 according to each object's immutable
or trusted-mutable state class. The post-init snapshot is the running and
cleanup baseline for that run.

Before crossing an attended, privileged, or evidence-producing gate, perform a
bounded attempt-minimization pass over the planned qualification phases. Apply
known platform facts to every unexecuted sibling phase, in proportion to the
cost and risk of the next boundary, and prioritize false-pass or
evidence-integrity defects. A newly learned failure class must be propagated
before any rerun; this method does not authorize skipping explicit human gates
or turning bounded worker output into evidence without supervisor adjudication.

Before each new attended qualification attempt, fully quit and relaunch
Terminal.app, especially after any supplementary-group change. This is
procedure hygiene only: it refreshes the terminal process environment and
group list, but does not reset or prove the absence of Tart, Softnet, Screen,
socat, VM, filesystem, or network state. Never treat a fresh Terminal.app
process as a workaround for an unexplained gate failure; preserve and diagnose
that failed run independently.

A failed run is immutable evidence, not a checkpoint to resume. Resume only to
investigate, understand, or safely contain the failed runtime. After correction,
preserve the failed evidence and qualify again from a fresh B0 and fresh
disposable runtime; any exception requires explicit qualification design and
architecture review.

When admission uses exact Tart 2.32.1 `list`, capture the protected Tart config
set immediately before and after the bounded command and retain trusted
timestamps as diagnostic context. Require exact file identity, type, mode,
ownership, links, size, flags, path, protected bytes/SHA-256, and the named
semantic state needed by the launch or lifecycle claim. A ctime change caused
by observation is diagnostic for trusted mutable state; it is not a reason to
claim filesystem neutrality or atomicity. Never apply this relaxation to an
immutable failed-run or forensic artifact.

When the manifest Tart home contains immutable forensic state, the ADR024
runtime gate uses a newly created qualification-only Tart home under that
run's private root. The installed manifest remains unchanged and continues to
supply the admitted Tart, Softnet, platform, operator, group, and canonical
host-state identities. Evidence records both absolute paths and proves they
are canonical, disjoint from each other and from the source, forensic,
installation, and repository roots, and never connected by fallback. No Tart
command may target the manifest/forensic home during this gate.

The qualification home begins with zero VM objects (Q0), contains exactly one
identity-bound disposable VM during the active phase, and returns to zero VM
objects after exact deletion (Q1). Tart list/run/stop/delete observations act
only on that home. Retain list and clone timestamps as diagnostic evidence;
blocking checks cover only exact intended identity, stopped/running shape,
source targeting, fresh identity, and Q0/Q1 lifecycle claims. A stopped clone contains no
`control.sock`; require the socket only in the reviewed running and
stopped-after-run phases. This ADR024 gate qualifies
the exact runtime privilege, network, serial, and lifecycle mechanics with the
explicit qualification-only state-root value; it does not qualify arbitrary
pre-existing VM coexistence or V4 product lifecycle composition against an
occupied production Tart home.

The process evidence must establish that the exact Tart shell-owned job has a
direct Softnet child with a unique start identity and exact executable, while
recording credentials independently. Capture the independently observed PGID,
but do not require PGID equality: Tart group
termination does not prove Softnet disappearance, and no PID-only signal is
permitted. The observer remains bounded and non-lossless; no atomicity claim is
made for filesystem or privilege transitions.

During the launch-to-observer epoch, Softnet may commit/replace the bootpd
preference object. Inode and timestamp replacement information is useful
diagnostic evidence, but does not itself block and does not prove atomicity.
Where the privileged-component claim requires it, device/type, root ownership
and mode, links, size, flags, path, exact SHA-256/XML, and the sole approved
`DHCPLeaseTimeSecs=600` semantics remain blocking. Do not infer a stronger
filesystem-transition claim from these observations.

Network claims are recorded separately as EXPECTED REACHABILITY, PROHIBITED
REACHABILITY, or NOT QUALIFIED. Each finite probe names its destination,
protocol, address family, and expectation; it must not be generalized into
whole-LAN/private-network or general IPv6 isolation. Vmnet gateway reachability
is an accepted expected behavior where required by the design and any policy
change requires separate architecture adjudication.

The guest adversarial suite is a separate, reviewed and hash-bound component.
Guest-generated PASS/FAIL text is not authoritative security evidence: every
blocking guest claim requires a trusted host-side oracle, an expected result,
a failure condition, and a retained evidence artifact. Product claims
requiring V4 lifecycle, cross-session/domain isolation, or destroy/persistence
semantics remain deferred until those lifecycle paths exist.

## Blocking claim families

The redesigned gate blocks only for assertions mapped to one of these named
families:

- **Toolchain and launch authority:** exact reviewed qualification components,
  strict host manifest/tool identities, the root-controlled Softnet privilege
  binding, exact child-observed executable/argv/closed environment, and exact
  qualification VM identity.
- **Guest capability configuration:** no unintended guest-visible host share,
  additional disk, bridged/VNC path, clipboard/audio integration, host
  credential/control IPC, agent forwarding, nested virtualization, Rosetta,
  or other prohibited opt-in capability. Claims concern exposure to the guest,
  not whether a service exists on the trusted host.
- **Privileged process behavior:** exact controller-owned Tart identity, one
  exact direct Softnet child with stable kernel identity and independent PGID,
  the cumulative privileged-setup evidence chain, steady credential drop, and
  containment that never derives signaling authority from a Softnet PID.
- **Host-local serial and relay:** owner-private PTYs and endpoint links, exact
  relay and collision-free Screen-holder identities, bounded untrusted serial
  bytes, and identity-bound cleanup.
- **Network:** individually enumerated expected-reachable,
  prohibited-reachable, and not-qualified cases. Positive and negative
  controls plus host-side oracles support only the exact tested
  destination/protocol/address-family claims.
- **Cleanup and unrelated-state safety:** exact owned runtime targets, Tart
  reap, Softnet disappearance without PID-only signaling, separately
  authorized identity-bound VM deletion, Q1, and preservation of source,
  unrelated, and forensic state under their applicable semantic or exact
  envelopes.
- **Evidence integrity:** owner-private bounded component reports, exact
  identities/hashes and limitations, independent forensic protection, and
  immutable failed-run evidence.

## Component and evidence authority

The gate is decomposed into a lean human-gated host controller, the existing
unprivileged process observer, small host oracle tools, a separately reviewed
and hash-bound guest-root probe, and a host evidence normalizer/verifier.
Deterministic parser/model fixtures run before attended execution and are not
embedded in the controller.

One reviewed package manifest is the attended trust root. Its human-approved
digest binds the controller, observer, launch recorder, host oracle, verifier,
guest probe, run specification, source revision/archive, and every artifact's
path, size, mode, owner/group, link count, and SHA-256. Supplying hashes beside
unbound artifacts is only self-consistency and is insufficient. The controller
must revalidate the complete package immediately before claim-bearing work.

The exact guest probe is a trusted qualification stimulus generator executed
as root in a fresh controlled clone before an untrusted workload is introduced;
the guest OS and root capabilities are the subject under test. The probe records
bounded stimulus and corroborating inventory but does not decide a containment
result. Exact launch evidence plus the reviewed Tart capability map is
authoritative for omitted/configured integrations; host listeners and other
host observations are authoritative for reachability and host-state effects.
A prohibited network case requires the host-recorded per-case challenge
schedule, exact probe/transport identity, its exact host oracle, and a
successful adjacent positive control; otherwise it is informational or not
qualified. This qualifies the exact controlled finite experiment, not a
tamper-resistant guest attestation protocol. Raw serial bytes stay in
owner-private evidence and are normalized or encoded before any public report;
they are never automatically rendered onto an operator terminal.

The guest suite inventories mounts/filesystems, block and virtio/PCI devices,
interfaces, routes, neighbors, resolver/DHCP state, environment, Unix sockets,
and candidate integration channels. It performs bounded active attempts
against synthetic host-share/credential/control markers, a finite classified
TCP/UDP IPv4/IPv6 network matrix, rate-limited malformed application payloads
over ordinary TCP/UDP sockets where an adequate host oracle exists, and hostile
serial/framing/control sequences. It uses no real credentials and does not
scan destinations outside the reviewed matrix.

The initial oracle design is unprivileged and accepts only exact
preflight-enumerated local interface addresses, fixed bounds, and declared
positive-control replies. Malformed transport headers, raw frames, ICMP, or
IP-fragment behavior that cannot be produced and decided by this design remains
not qualified. A raw generator, privileged capture helper, persistent service,
or installed observer requires separate design and review.

Attended execution has separate approvals for read-only seal/preflight,
clone/MAC/move/relay preparation, Tart/Softnet launch and passive observation,
active adversarial probes, and irreversible exact VM deletion. Runtime failure
containment may stop only revalidated controller-owned processes. It preserves
the VM and evidence; deletion is never generic failure cleanup. Each token is
bound to the SHA-256 of a canonical authority record containing the exact
source, VM, process, evidence, and mutation identities available at that phase.
An owner-private, identity-checked exclusive qualification-home lock is held
from Q0 through Q1 so every cooperating qualification operation shares the
same isolated namespace authority.

## Evidence and limits

At five-millisecond configured intervals, the observer makes at most 6,000
sampling attempts and requires 100 consecutive accepted post-drop samples. It
enumerates direct children of only the supplied Tart PID; exactly one child is
permitted. It brackets every process sample with Darwin's kernel process-unique
identity, binds the child to Tart's parent-unique identity, and binds Tart and
Softnet to their exact executable paths. Tart start time is required
immediately; Softnet start time is required once its post-drop identity is
visible and must then remain stable. It records distinct sampled
real/effective/saved UID and GID tuples with bounded occurrence counts.

A root-effective sample is recorded when observed, but is neither required for
success nor sufficient to qualify the complete transition. The report always
states that sampling is not lossless, that activity between samples can be
missed, and that the configured interval excludes query and scheduler latency.
Absence of a sampled root credential says nothing about whether the transient
privileged phase occurred.

The observer fails closed on an unrelated or reused Tart PID, changed process
identity or start time, absent or multiple direct children, wrong ancestry,
wrong executable, unexpected credentials, a child disappearing before enough
evidence, query failure, timeout, fixed-host-state drift, malformed input, or
bounded-output exhaustion.

This report is only one component of sufficient non-perturbing qualification
evidence. The attended gate must also bind the exact artifact/root tree and
reviewed Softnet 0.19.0 implementation; record the controlled executable,
argv, and closed environment; exercise behavior requiring privileged setup;
observe the qualified network policy externally; and confirm the steady-state
drop to the trusted operator. The threat model assumes that operator is
trusted and does not claim resistance to a malicious process already running
as that host operator.

The redesigned gate intentionally retires claims of exact trusted-mutable
timestamp choreography, whole source/cache metadata neutrality, globally empty
process/Screen/port namespaces, exact Terminal or `lsof` presentation,
empty scratch-directory state, broad network isolation inferred from finite
samples, general IPv6 isolation without direct tests, and absence of
credential channels beyond integrations actually examined. Removing these
unsupported claims is an assurance improvement, not a qualification result.
