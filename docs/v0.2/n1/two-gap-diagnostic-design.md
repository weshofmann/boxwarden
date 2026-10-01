# N1 two-gap diagnostic design

Status: source-only diagnostic package, based on fetched main
`99b3a8c04c56ddc4292a9dd515c3e2abcb77d5c6`. Neither a live authorization
nor qualification of an instrumented artifact. The completed macOS 27 trial
remains closed. [Historical matrix](matrix.md), [follow-up](follow-up-trial.md),
[evidence inventory](diagnostic-evidence.md), [implementation plan](diagnostic-plan.md).

## Evidence and model

Stock synthetic clipboard passed. Candidate capability preflight passed, copy
acknowledged, and the **first** read failed with text unavailable; no retry.
A later live write worker and responsive desktop do not prove native ownership
or delivery. Candidate TCP to stock SSH22 returned errno 113, with an on-link
route and FAILED neighbor; strict same-generation host positives bracketed it,
and candidate management/DNS/HTTPS stayed healthy. These are **UNQUALIFIED**
results, not hidden PASS results. The macOS 26 unknown/invalid trial is separate.

There are two independent paths. Clipboard runs over host-initiated pinned
management SSH, which remained healthy; no source evidence couples its failure
to peer ARP. Network peer discovery precedes candidate-to-stock TCP establishment;
a failed neighbor can prevent SYN emission, even when host-to-stock SSH works.

## Clipboard path and ranked hypotheses

The path is CLI -> session service -> private supervisor control -> exact READY
runtime -> pinned SSH -> bootstrap clipboard mode -> fixed Python adapter ->
GTK3 X11 backend -> Xwayland beneath the admitted Wayland compositor.

The [canonical adapter](../../../guest/ubuntu-24.04-arm64/clipboard.py) discovers
one local active Wayland user login, derives DISPLAY/XAUTHORITY from the user
manager, checks their metadata, then twice binds compositor/controller and
Xwayland executable/PID/starttime/parent/display/authority. Each read validates
before and after one `UTF8_STRING` request. A write forks a detached child,
claims exactly that target with `gtk_clipboard_set_with_data`, disarms its
precommit timer, validates again, acknowledges, then pumps GTK until selection
loss or session validation fails. The child can exit **after** acknowledgement.

GTK claim success means callback-based ownership was accepted; the content is
provided later on demand. The clear callback reports replacement, and a negative
selection length reports retrieval failure, without identifying its cause.
See [GTK claim](https://docs.gtk.org/gtk3/method.Clipboard.set_with_data.html),
[read](https://docs.gtk.org/gtk3/method.Clipboard.request_contents.html) and
[target metadata](https://docs.gtk.org/gtk3/method.Clipboard.request_targets.html).

The Python error frame contains only version/status/length. The executor
suppresses stderr. Bootstrap combines executor, decode, deadline and binding
failures; its command reports target unavailable. SSH, runtime and supervisor
then map multiple transport/protocol/READY/status failures to a generic read
error. Thus the historic message identifies no failing layer.

| Hypothesis | Rank and evidence | Discriminating future observation |
|---|---|---|
| Owner exits after acknowledgement | Plausible: retain can fail independently; later process presence weakens immediate death, but lacks birth/owner correlation | Exact owner PID **and starttime**, claim/ack/retain events and read-time liveness; exit or changed birth identity identifies loss of that process |
| Selection replaced or cleared | Plausible: ownership is transient, and child only tracks its clear callback | Clear callback plus native owner XID at post-acknowledgement retain and immediately before first read on the same Xwayland binding; zero/different owner indicates loss/replacement |
| Different desktop/Xwayland/session | Lower source prior because validation is strict; each operation nevertheless discovers separately | Compare operation binding digests including login, compositor and Xwayland birth identity, DISPLAY and authority metadata; changed binding separates this branch |
| UTF8 target absent or representation rejected | Plausible: owner advertises only UTF8_STRING; read rejects negative length, wrong type/format, oversized or invalid UTF-8 | One bounded target-metadata query (presence of UTF8 only), native callback classification and representation checks, without logging target names or text |
| Session/helper validation rejected | Plausible: repeated validation intentionally fails closed | Allowlisted stage/error codes before/after request and claim; check failure distinct from native retrieval failure |
| Guest error collapsed, or framing/transport failed | Established information loss, unproven original cause | Guest adapter error code vs successful frame emission plus trusted-host stage codes; absence of a trace alone establishes nothing |

Equal binding and owner observations are point-in-time evidence, not an atomic
proof across the interval. Native owner XID can be reused, and the Wayland bridge
can own a proxy selection. A get callback proves service was requested, not that
the host received the bytes. Exact synthetic readback remains the delivery check.

## Minimal diagnostic source changes

See the [clipboard source report](clipboard-source-diagnostic-report.md) for
the implemented overlay, exact bounds and red/green record. Keep the canonical
guest definition, locked bootstrap, public clipboard protocol and ordinary
installation unchanged. A **trial-only adapter overlay** wraps the
exact preserved production adapter in fresh disposable clones. Its fixed trace
is opened before privilege drop; the detached owner retains only the diagnostic
fd plus its normal GTK state. Fixed slots/codes and hard byte/event/time bounds
prevent indefinite owner lifetime from creating unbounded records. Trace failure
must never authorize a write, retry a transfer or yield a PASS.

Record adapter entry, session construction/check result, native claim, native
owner/target metadata, get/clear/retain state and read callback classification;
never exception strings, payload, authority contents, arbitrary argv/env or
process listings. Collect via a separate reviewed pinned SSH metadata read;
collection never invokes another text read. Use original output framing.

If the adapter reports successful read/frame emission but the CLI fails, the
future diagnostic host build must capture **codes only** at these existing seams:

- `guestproto.Bootstrapper.Clipboard`: request/binding admission, executor result,
  desktop decode, deadline, postbinding and response encode;
- `sshx.Client.Clipboard`: admitted, child exit, truncation, deadline,
  response decode and status;
- `sessionruntime.ReadClipboard`: still-ready and response-status classification;
- `supervisor` read control: reply decode, length/EOF and validation result.

One operation ID and generation bind all stages. At most one fixed code per
stage, <= 32 records / 16 KiB, elapsed monotonic offsets only, no stdout/stderr
capture. Emit the receipt from the trusted host after the operation; the detached
supervisor owns its bounded in-memory stages. Unknown exceptions become a fixed
`internal_error`, never their message. These **host hooks are a preparation
requirement**, not implemented production telemetry or admitted executable bytes
in this package. They are needed only to separate downstream sublayers; adapter
traces alone can distinguish a specific guest error from downstream failure.

## Network path and ranked hypotheses

Tart 2.32.1 connects the guest NIC through an `AF_UNIX/SOCK_DGRAM`
socketpair. Softnet `lib/vm.rs` receives one Ethernet frame per datagram; patched
`Proxy::process_frame_from_vm`
calls `Policy::forward_with_refresh`. Fresh host-local/broadcast metadata is
obtained before evaluation. Only Allow or accepted stock-global fallback reaches
`Host::write` -> `vmnet::Interface::write` -> `vmnet_write`. The vmnet shared
interface retains isolation. Host-side receive retains its ARP/IPv4 allowlist before `VM::write` back to
the guest; `Policy::host` observes DHCP/management state, and its returned
boolean is not the sole inbound admission gate. Canonical N1 source is
[policy.rs](../../../tools/n1-softnet/policy.rs) and
[exact patch](../../../tools/n1-softnet/softnet.patch), against upstream
Softnet `df84a30016e3d6acc0d30acc660cf3a726f42a9b`.

`guest_arp` requires Ethernet/ARP sender consistency and the current leased
source (or zero before lease). Its target must be the gateway or a fresh
host-local address. The stock peer lease is neither. Source therefore predicts
peer-ARP suppression, **conditional on the frame actually reaching that policy
with that lease and host metadata**. Stock also uses vmnet isolation and its
private-destination rule; host management positives do not establish peer ARP
reachability. If peer resolution succeeds, TCP may still be denied independently.

| Hypothesis | Rank and evidence | Required passive observation |
|---|---|---|
| N1 suppresses peer ARP | Strongest source hypothesis; live arrival/decision absent | Exact peer ARP emitted by candidate, received by Softnet, actual Deny and no vmnet write; validate source/lease/target and fresh host-local exclusion |
| API write returns but no reply is observed | Plausible alternative, vmnet isolation retained | Full-length API return and no IP-matching reply at host ingress under complete expected/unexpected-MAC, broadcast and header coverage; enqueue and internal cause remain unknown |
| Kernel route/neighbor failure before emission | Plausible, route snapshot alone insufficient | Single-target route and neighbor before/after; complete candidate AF_PACKET transmit coverage with neither matching ARP nor relevant TCP SYN, bounded socket phase/errno; a cached neighbor can send SYN without ARP, and absence without loss-free coverage is inconclusive |
| Reply/guest delivery or other socket failure | Remains possible | Matching reply at Softnet ingress and guest delivery observation; write success/error/ENOBUFS; SYN/connect phase, errno and monotonic timing |

Use one exact candidate/stock tuple, never whole-network capture. In each guest,
a finite AF_PACKET/BPF metadata counter watches only that tuple's ARP and TCP
flags, no promiscuous mode or payload/file capture. Count receive drops/truncation
and observer start/stop completeness. Route query is `ip -j route get <peer>`;
neighbor query is `ip -j neigh show to <peer>` on the selected interface. These
queries neither create static entries nor flush state. Do not run ping/arping,
add routes or neighbors, change firewall state or add a listener.

The trial-only forwarding observer reports actual ingress, decision and write
result at the N1 seam. **It cannot be retrofitted by offline policy replay or
by a guest-only packet counter.** The [pure observer and source integration contract](arp-observer-contract.md)
in this package are preparation for a separately built reviewed
variant. Its future version/source/executable/archive digests, closed launch
binding, fd sink metadata and manifest must be independently admitted. No
runtime debug flag, ambient environment or same-digest substitution is allowed.
Observation must not replace the policy decision or bypass refresh/write errors.
A full-length successful vmnet API return proves only that API outcome. The
locked `vmnet` 0.5.1 wrapper checks status and returned packet size, but not the
returned packet count; it does not prove enqueue, vmnet processing or peer
delivery. Downstream silence cannot name an internal vmnet failure cause.

## Privacy, trust and adjudication

Guest-root trace is cooperative diagnostic evidence, never host authority.
Synthetic text exists only in memory and transfer/readback buffers; publish only
its equality result and length/hash receipt. No real Mac clipboard or credentials
enter these guests. Root-owned fixed guest paths, no symlinks/hardlinks, finite
slot writes and read-time strict schema checks constrain accidental leakage;
they do not make a hostile guest truthful.

Network observers retain only counters/codes for a reviewed fixed pair. Host
local addresses are examined by the policy but not dumped; record target-local
boolean and snapshot freshness. Keep raw private bindings/paths/receipts outside
Git in a durable owner-approved archive, validate readable hashes and provenance.
Retained historical evidence is read-only. Any observer loss/overflow, ambiguous
binding, missing native metadata or unverified source hook makes attribution
incomplete. An incomplete diagnostic never grants PASS.

The historical [TCP verdict tool](../../../tools/n1-qualification/followup_tcp_verdict.py)
is unchanged: connect fails isolation; timeout with working positives/controls
can PASS that bounded check; errno113 remains UNQUALIFIED. A new attributed
ARP-denial receipt may close the **diagnostic question**, with a separately
reviewed future matrix entry; it does not rewrite old receipts or automatically
change acceptance rules. No UDP/VPN/IPv6 or general host-isolation claim.

[Future attended-request template](diagnostic-attended-request.md) lists the
smallest combined window and the remaining exact-byte preparation gates.


### Task3b diagnostic forwarding source gate closed

The distinct diagnostic Rust source calls the tested shared dispatch functions
from the actual VM and host forwarding paths. It preserves the refresh and
timestamp order, policy state transitions, fallback, raw write results and the
single write, including short successful writes, the ignored host policy boolean
and ENOBUFS suppression. Diagnostic lease inspection is read-only. Bounded
metadata identifies only the selected pair; vmnet packet count remains
unobserved, and API success makes no enqueue or delivery claim.

The fixed HELLO/ARM/ARMED/SUMMARY protocol admits one finite interval over an
anonymous nonblocking fd1 stream. Complete and partial ARM arriving after the
original HELLO wait deadline are refused before activation. Mandatory self-tree
admission validates the full manifest, descriptor digest, protected ancestry,
operator/group membership, ACL/link/mode and read-only SH lock; the guard remains
held through actual resource construction and teardown. Timestamp offset fields
require decimal digits before range and zero-instant validation.

The first fresh independent source review found two Important issues and one
Minor issue: late ARM could bypass the HELLO timeout; signed offset components
could pass timestamp admission; and the serialization fixture omitted widest
valid binding spellings. All were corrected and independently closed at their
original severity, with no new findings. The original review and measured
results remain preserved. Spec and quality pass this bounded source-component
gate; it is not a final package review.

Retained targeted RED reproduced four assertion failures with two passing
controls. Targeted GREEN passes six controls; a fresh 14-file patch replay and
actual library/main metadata-only Cargo check pass. The corrected conservative
serialization bound is 3166 bytes for SUMMARY and 4469 framed child bytes,
including the three length prefixes, within 4096/16384 quotas. It overapproximates
jointly reachable counter/flag states; original 3132/4387 remain fixture-specific
measurements. No full 79-test pass is claimed by the targeted run.

Source patch SHA256: 2e8dbbffdbf5869dc5280ef260e9f455cd31af43d8519844be8f95440416f2f6.
Source manifest SHA256: 555d710d92432b7d64621c07719332a37881b5e33efa0776aa3b1683e7050333.
Initial independent review SHA256: 6f7e938f363569cbecb9e0e161e9a8cef6d87521eb1e4ddd2ff22110dd1057d4.
Independent corrective closure SHA256: 325d28c9ef206367f81961fb45bf87071b74ab915396450313c8616ecba2958c.

Native ACL/socket controls are local synthetic evidence. Same-process socket
flags and the earlier small Foundation fd1 round trip do not qualify actual
Go-to-Foundation inheritance. Compiler records identify clang/ar shims, not
historical resolved SDK children or every nested image boundary. Exact artifact
reproduction and admission, Go owner/watch/lock integration, cleanup, finite
driver/static lock, current increment CI and four fresh final reviews remain
gates. No diagnostic executable was built or launched, and nothing was installed
or run against a VM, guest, clipboard or host network state.

A separate deterministic CI matrix now selects only the locked synthetic fixture
on Ubuntu 24.04 and macOS 26. Fresh private neutral working directories, explicit
nonprivileged runner overrides and a closed submitted environment keep upstream
sudo test runners out of that execution. Locked fetch precedes offline tests;
fetch or configuration refusal prevents test execution. Local syntax and
command-spy controls pass, but spies do not establish native compiler identity
or exact native child environment. Hosted execution remains pending publication.


## Executable preparation: separate startup and pre-ARM bounds

The actual diagnostic.1 watch used one 30-second timeout for HELLO and the subsequent pre-ARM wait. The approved sequence performs guest readiness, runtime review and both clipboard attempts before ARM; it can legitimately exceed that timeout. A read-only independent protocol review resolves this integration blocker with a short startup deadline and a separately anchored, finite resource cap. Review SHA256: `95a5e165a60619650ad90894595ca05cb220f4a992386fbe377171abd1050ad7`.

Go must receive HELLO within 30 seconds of the retained pre-spawn launch anchor. The unarmed channel has one immutable maximum 30-minute cap from that anchor; Rust anchors its local maximum at channel admission before host/VM construction. Both use suspend-aware continuous expiry, wall/regression/error refusal and checked arithmetic. Neither inspection, readiness, credential renewal nor partial input can extend the cap. Rust checks before queued input and after full decode. ARM remains single-use with the existing 1000–30000-millisecond duration range, 100-millisecond close grace and passive forwarding/privacy rules.

The channel caps bound resources; their different local origins do not independently enforce the original owner approval expiry at Rust consumption. The trusted coordinator retains the original wall/continuous anchors and cumulative 20-minute active, 10-minute attendance and 30-minute total budget. It checks remaining headroom before ARM, then the returned actual host-send deadline and original budgets before connect and every subsequent dispatch. A late receipt refuses connect and leaves attribution incomplete; no rearm, restart or fresh deadline is admitted. No original-expiry child-enforcement claim is made.

A separate unarmed `ObserveDiagnosticLaunch` returns strictly bounded retained HELLO and exact owner/handle/watch/connection correlation. It neither creates readiness nor changes phase or deadlines. Existing phase-independent network inspection remains available for stock and post-ARM collection rechecks. The ARM control envelope must carry the actual host deadline; callers must never restart the interval from RPC return.

Any changed Rust bytes are a distinct diagnostic.2 source/artifact identity. Diagnostic.1 source, reproduced executable/archive and review evidence remain historical. Rust timing/source controls, fresh independent delta review, publication and fresh twin reproduction precede new Go exact-identity binding. Source-complete Task5 graphs precede final Go twins. This contract is prospective source work, not installed-host or live-window qualification.


## Diagnostic.2 artifact reproduction checkpoint

The corrected native recipe produced two independent successful targets9/10
from Rust source `85bcb9aebffcc4a8c98b1bec111df2db8c18e81f`. Their executable,
USTAR and applied-source record bytes are equal. Executable SHA256 is
`1bb12bec8821835ada8c036426c2c03061be6cebdf4a1b658249d268fc8bde05`; archive
SHA256 is `76704f05eba242cb649ab49db28028556c338a4daeefe9327fdfe37f874808ac`.
`artifact_v2.json` records the exact producing source, source manifest, recipe
and compiler identities separately from historical diagnostic.1 evidence.
The recipe was published at02c4b6 and the artifact record at622bdec.

This closes the Rust reproduction prerequisite only. Go exact admission,
original-budget/ARM-send receipt guards, finite launch/coordinator/closeout
graphs, full static package closure and four final reviews remain required.
The fixed candidate config path is versionless; its existing expected enrolled
bytes remain unchanged while the compiled executable identity changes.
No native installation or live diagnostic result follows from this checkpoint.


## Static admission component closure

The corrected64-file Go/static slice now has independent component acceptance.
The original review remains0Critical/2Important/0Minor; both Important findings
are ADDRESSED at their original severity, with no new finding. Final fixed-file
reads recheck strict leaf ACL and native ctime/flags across descriptor/path
observations; preinstall census refuses exact U/R while cleanup retains them.
Three changed originals plus five additions leave56 original bindings unchanged.

Seven corrective retained runs include two RED attempts and five exit-zero
results: focused GREEN, affected native tests/race/vet and default tests.
140 actual compiler-child argument records and978 corrective evidence files
were independently verified. Closure report SHA256
896c3d1440de68a79a0e224dad985b0a63629e13a14b0590e9aa89d97c7c20a5;
Root readback68b2d085cf47808ffdd270526acc38de4d80c79a66949a5c1e0ff20747a63150.

This is source/fixture acceptance. The finite OS-image table still needs
known GUI-image preparation before the final lock; native census usability
is unqualified. No current process census, installation or actor execution
was performed. Launch/watch, coordinator/closeout, exact Go artifacts and
four fresh final package reviews remain pending. Published622bdec passed
CI440 all4jobs/41steps; the next source publication needs its own CI.


## Trial-only guest transport component closure

Five typed SSH operations now prepare fixed trial artifacts, inspect metadata,
run fixed SSH/DNS/HTTPS controls, retain the exact peer observer child and make
one bounded IPv4 TCP22 connect. Existing connection/pin validation and Runner
remain the transport boundary; canonical clipboard/helper/observer bytes are
unchanged. Configured DNS uses one fixed resolvectl A query with local cache,
synthesis and zone lookup disabled; this does not prove upstream cache freshness.

The initial fresh review found0Critical/1Important/0Minor. Its Important timing
finding is now ADDRESSED at its original severity, with no new finding. Complete
observer records require readiness within the matched budget and elapsed time
within the producer-representable closing envelope before absence eligibility.
This recognizes serialized rounding without extending the actual deadline.
Original-reviewer closure SHA256
26bf3b6f2f0d4cea9c1f9f8b0da30a9ebc84ef63d5dece160c0d32732ec12a2b;
Root independently checked all205 corrective entries and current component bytes.

Eleven retained corrective runs comprise one meaningful RED and ten exit-zero
results: baseline/GREEN, stock/combined tests/race/vet, fourteen canonical producer
controls and compiler argument control. Prior17Go/8Python controls remain retained.
Historical initial RED before-image limitations remain explicit. This closes only
this component; caller locks, current READY/certificate/approval, original budgets,
one-use/two-guest gates and final four-seat review remain package requirements.
No real guest, clipboard, network, census or privileged operation was performed.
