# Trial-only peer ARP observer contract

Status: deterministic source preparation only. The observer is not wired into
the installed stock or admitted N1 executable. No VM, policy mutation, listener,
packet capture or qualification interval was executed for this work.

## Source trace and provenance

Repository baseline is `99b3a8c04c56ddc4292a9dd515c3e2abcb77d5c6`.
The canonical [policy](../../../tools/n1-softnet/policy.rs) and
[patch](../../../tools/n1-softnet/softnet.patch) retain SHA-256
`37491b19e58bc0aaed1d3730b40d52e2cad53c74b8337df96fd8c6c145331ddf`
and `f91669592f2231c29d159d753d772dcbe9d08c3446832eadb7c731ba226b80ef`.
The [artifact record](../../../tools/n1-softnet/artifact.json) retains SHA-256
`fe56eac92b0fbb3c491156d08b64a2824beca2f313cace0f26eaf1ed645a14cf`.

Pinned [Tart Softnet.swift](https://github.com/openai/tart/blob/8aa377b71ebfd90b2df9803d3e20033f58d6800c/Sources/tart/Network/Softnet.swift)
uses `socketpair(AF_UNIX, SOCK_DGRAM)` at lines 19-35 and attaches the other
endpoint through `VZFileHandleNetworkDeviceAttachment` at lines 96-99.
Pinned [Softnet VM](https://github.com/openai/softnet/blob/df84a30016e3d6acc0d30acc660cf3a726f42a9b/lib/vm.rs)
uses `UnixDatagram::recv`; one received datagram supplies an Ethernet frame.
`Proxy::read_from_vm` validates the outer Ethernet frame and invokes
`process_frame_from_vm`, then the N1 `forward_with_refresh` hook. The hook
refreshes trusted local IPv4 addresses before deciding and calls `Host::write`
only for allowed traffic. `guest_arp` requires an admitted sender MAC/source
and permits only gateway or current host-local targets. A peer lease outside
that set is denied. A source/lease error is a different denial branch.

Pinned [Softnet Host](https://github.com/openai/softnet/blob/df84a30016e3d6acc0d30acc660cf3a726f42a9b/lib/host.rs)
creates shared/NAT vmnet with isolation enabled, reads the gateway parameter,
and delegates writes to `vmnet::Interface::write`. The actual dependency is
`vmnet 0.5.1`, crate SHA-256
`03f0531c358eba83803f9a46c165b8d1e9747758bf45c41df1499c1017d43f8c`.
Its [write implementation](https://docs.rs/crate/vmnet/0.5.1/source/src/interface.rs)
at lines 207-225 calls `vmnet_write`, checks its status and returns
`pktdesc.vm_pkt_size`; it does not inspect `pktcnt` after the call. Thus a full
successful `Host::write` return proves only the full-length API return, not
enqueue, packet delivery or the framework's internal disposition. On ingress, patched `host.rs` refreshes
metadata, updates policy observations, then writes the frame to the VM.
Its existing ENOBUFS branch reports `Ok(())` without successful delivery.

Reference [Linux 6.8 neighbour.c](https://github.com/torvalds/linux/blob/v6.8/net/core/neighbour.c)
shows ARP solicitation, finite probes, NUD_FAILED and queued-packet error
reporting at lines 1041-1077 and 1142-1157.
[arp.c](https://github.com/torvalds/linux/blob/v6.8/net/ipv4/arp.c) binds this
error callback to link failure at lines 294-297. These explain the mechanism;
the exact guest kernel build must be bound in a future receipt. The historical
errno 113 and FAILED neighbor are compatible with this path, not causal proof.
The existing TCP verdict and historical UNQUALIFIED result stay unchanged.

## Implemented module

[arp_observer.rs](../../../tools/n1-qualification/arp_observer.rs) is a pure
standard-library metadata accumulator. It has no network, file, environment,
privilege or policy-control capability. Its watch binds one explicit
`n1qualification` candidate/peer pair: domain, distinct session and generation
UUIDs, backend objects, private leased IPv4 addresses, unicast MACs, gateway
and a monotonic interval of 1-30 seconds. Construction rejects incomplete or
conflicting bindings. The future driver must authenticate these facts; string
validation does not establish ownership or DHCP provenance.

Only well-formed Ethernet/IPv4 ARP headers for the exact pair are counted.
No frame, padding or payload bytes are retained. Guest requests/replies and
matching peer replies at host ingress have separate counts. There are at most
4096 completed or started dispatches per direction. Overlap, ticket mismatch,
overflow, an unfinished dispatch, an unfinished window, contradictory arithmetic
or a changed binding invalidates a snapshot. Counters are bounded metadata,
not authenticated receipts and not a PASS adjudicator.

`begin_guest` records actual arrival before refresh/decision/write.
`complete_guest` records one actual outcome: peer-target denial, another denial,
refresh error, successful full-length API write, or write error. A started
dispatch without completion remains incomplete. `begin_return` precedes
`VM::write`; `complete_return` separates successful full-length datagram write,
ordinary write error and ENOBUFS. A successful write is still not guest-kernel
receipt. Observer results never select a forwarding branch.

## Required future proxy wiring

This is a concrete source seam design, **not an applied diagnostic proxy patch**.
Before a runnable package exists, prepare a separate patch against the exact
upstream plus admitted N1 source. Preserve the canonical repository policy,
patch and artifact identity. The future variant must:

1. Create exactly one immutable watch from a freshly reviewed pair binding and
   monotonic interval. Use a closed launch binding and existing bounded private
   supervisor sink; no ambient environment, arbitrary trace path, new network
   socket, traffic override or runtime debug bypass. Bind the observer start and
   stop to the single connect interval and current owned Softnet generation.
2. In `process_frame_from_vm`, call `begin_guest` immediately after the actual
   VM datagram read/outer-frame validation and before metadata refresh. Keep the
   existing policy evaluation and fallback call count/order unchanged. Complete
   its ticket on every actual dispatch exit before propagating the same result.
3. Carry a typed reason from the **actual** `guest_arp` branch: malformed header,
   MAC/sender/lease failure, permitted target, or valid sender with denied target.
   Only the last branch maps to `PeerTargetDenied`. The present policy returns
   a bool and cannot supply that reason; its diagnostic-copy refactor and
   equivalence tests remain a preparation requirement. Never label a bool Deny
   as peer-target denial using errno, an offline replay or target alone.
4. Obtain write outcome from the actual `Host::write` result within the existing
   forwarding closure: attempted once, full-length `Ok(n)` versus error or short
   return. Do not call it again. Short success is incomplete write evidence and
   must never be recorded as `Written`; preserve existing forwarding behavior
   and mark the diagnostic invalid. Refresh failure has no decision/write.
5. At entry to `process_frame_from_host`, observe the fixed pair's same-IP ARP
   replies before the allowlist, host refresh, exact peer-MAC filter or VM write.
   The future hook must use finite codes/counts for expected sender MAC,
   unexpected sender MAC, broadcast destination, and unsupported/truncated
   headers; do not dump MACs or header bytes. Bind any same-IP classification to
   validated sender/target IPv4 fields and the immutable pair. Malformed or
   truncated observations that cannot safely establish those fields make
   IP-level coverage incomplete. Apply the same interval, capacity and loss
   bounds as the exact-MAC observer. Then begin exact peer-reply accounting and
   complete from the actual write result, detecting short success and ENOBUFS
   before upstream collapses the latter. A host-refresh failure leaves the
   ticket unfinished and the whole interval incomplete. These broader host
   ingress hooks are unimplemented requirements; the current module counts only
   the exact peer-MAC subset.
6. Emit one strict bounded summary after the closed interval, with pair binding,
   complete/overflow flags and counters. The fixed schema must serialize the
   private Watch fields in the future variant without exposing arbitrary
   strings or host-local address lists. Validate against the driver's original
   binding, source and generation. A crash, missing receipt, failed sink or
   incomplete capture cannot be interpreted as zero traffic.

The standalone tests prove accounting and exercise the real canonical policy.
They do **not** prove these unimplemented proxy hooks or the live observer's
coverage. A future patch must add integration tests that obtain the typed
reason from the real policy branch and compare exact forwarding bytes,
refresh/fallback/write counts, errors and policy state against uninstrumented
source. Then build, review and qualify its new version/source/executable/archive
digests and exact admission path. No same-digest substitution is possible.
Future incorporation into the modified Softnet distribution must preserve its
AGPL-3.0 notices and corresponding-source obligations, including this module
and the integration patch. No Tart source is copied or modified.

## Observations and remaining blind spots

The future guest monitor is another preparation requirement: no monitor is
implemented or run by this module. Bind a finite non-promiscuous AF_PACKET/BPF
monitor to the exact guest interface and pair, readiness before connect,
monotonic interval and drop counters. Retain only counts, direction, ARP opcode,
TCP flags and allowlisted socket stage/errno. Use kernel snap lengths limited
to protocol headers, no pcap files or full packet buffers. Exact peer TCP SYN,
RST and ICMP-unreachable metadata are needed to separate later socket failures;
ARP-only counters do not distinguish them. Never print quoted packet bytes.
Bound guest route/neighbor queries before/after to the same peer and interface.
The monitor's completeness and trustworthy collection must be tested before
live use; AF_PACKET transmit observation is a kernel tap, not NIC/N1 delivery.

| Complete observations | Bounded conclusion |
|---|---|
| Guest emitted matching ARP, actual N1 arrival and peer-target Deny, zero write attempts | Suppression at the N1 boundary is observed; no TCP SYN is required for this ARP-stage denial |
| Actual matching arrival, full successful API write, no same-IP return under complete host ingress coverage of expected/unexpected MAC and broadcast/header classes | No IP-matching reply observed at host ingress in this interval; the API return does not prove enqueue, and vmnet isolation, peer non-response and other loss remain indistinguishable |
| Exact peer-MAC return counter is zero without the additional complete host ingress coverage | Only absence of counted exact-MAC replies; no IP-level localization or downstream-silence conclusion |
| No matching guest ARP or relevant TCP SYN and no N1 ARP arrival, complete loss-free monitor plus route/neighbor and socket metadata | Failure before the observed tap is supported; precise kernel/firewall/socket cause remains unresolved |
| Guest transmit but no N1 arrival | Loss between guest tap and Softnet, not N1 policy denial |
| Peer reply at N1 ingress but VM write error/ENOBUFS or no guest receive | Local delivery failure or later missing delivery; successful API return alone does not locate the latter |
| OtherDenied, refresh failure, observer loss/overflow, changed lease/binding, short write or absent summary | Attribution incomplete; do not promote the qualification verdict |

Host strict SSH positives establish the stock listener and host path, not the
candidate-to-peer ARP path. A pre-existing valid peer neighbor can bypass new
ARP and expose the independent private-IPv4 denial; never flush, add or replace
neighbors to manufacture the desired observation. Route/neighbor/firewall
mutation, ping/arping, new listeners and extra connect attempts are excluded.
The module's return filter requires the peer's bound Ethernet/ARP sender MAC.
Proxy ARP or a mismatched MAC is excluded, so zero `return_reply` means no exact
peer-MAC reply was counted; it does not prove no IP-matching reply appeared.
The future host ingress hook and guest monitor must both distinguish expected
peer replies from proxy or mismatched replies and broadcast/header cases using
bounded codes, with no MAC/frame dump, before claiming complete IP-level return
coverage. Host classification must precede the exact-MAC filter, allowlist,
metadata refresh and any write-result collapse: a proxy reply can reach host
ingress, be omitted by the exact-MAC filter, then encounter VM write error or
ENOBUFS and never appear at the guest. Zeroes at both current exact-MAC and guest
counters cannot exclude that path. Both unimplemented coverage requirements
apply to the downstream-silence row above.

## Deterministic verification record

Tests used the immutable Rust 1.98.1 compiler plus std archives from the retained
component record. Compiler archive SHA-256
`738e3f60114b20550fcf4f8a57c9cea0544239f214709f45594ff3fde327f577`,
std archive `2de831ef563ce772519a4d18ba2659f0f7428d497ef66dad24b5a35e8f8cd177`;
compiler executable `766eda9d8f53afd6fc7f27b3cd2e444dd22afacb5afa710a5625fc8e45b8c941`.
Only these exact verified components were unpacked into disposable
`/private/tmp/n1-peer-arp-observer-rust-1.98.1/compiler`; no installer, global
toolchain or candidate build/install ran. Version output was
`rustc 1.98.1 (48a229cea 2026-09-01)`.

The initial red run compiled successfully: 24 existing policy tests passed and
five new observer tests failed at deliberately unimplemented observer methods.
The first green run passed all 29. Two additional real-policy checks then passed
for admitted lease/peer-target denial and allowed exact bytes/writer-error
preservation (31 tests). An ingress-without-completion regression was observed
red before implementing split begin/complete accounting. Final green: **32
passed, 0 failed**, with no compiler warnings. This includes 24 canonical policy
tests and eight observer/real-policy tests. No live coverage is claimed.

Reproducible command, substituting an already verified Rust 1.98.1 executable:

```sh
rustc --edition=2024 --test tools/n1-qualification/arp_observer.rs -o /path/to/disposable/arp-observer-tests
/path/to/disposable/arp-observer-tests
```

The hosted `n1-packets` job runs this same test target with its existing pinned
Rust selection. The source contract and counts above are the durable record;
temporary compiler and test binaries are reproducible intermediates.
