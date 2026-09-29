# N1 acceptance matrix

The source and host-only evidence below was recorded before the 2026-09-28
attended trial. The live results from that trial appear separately below. The
candidate was temporarily admitted for two disposable guests and then removed;
the default installation still uses stock Softnet and retains ADR 015 gateway
exposure.

| Case | Deterministic boundary tests | Planned host-only / isolated guest gate |
|---|---|---|
| DHCP discover, ACK, renewal, lease expiry | MAC/source/ports, trusted ACK, unicast renewal | fresh address + renewal after lease interval |
| DNS UDP and TCP53, fallback | gateway advertised, UDP/TCP framing, fragments denied | UDP, TCP, truncated/large answer fallback |
| Resolver changes, scoped DNS, VPN, DNS64 | trusted metadata and DNS payload/path preservation | separate observed environment rows; no VPN changes without approval |
| Public HTTPS | public destination passes stock rule after host ceiling | owned/appropriate public endpoint through selected host route |
| Management + clipboard | host SYN/handshake, exact tuple response, expiry/capacity/address removal and renewal | pinned SSH and controlled synthetic clipboard in owned guest |
| Host service denial | gateway and host-public aliases denied; forged ACK/source22 denied | same owned listener reachable stock control, denied candidate |
| Private/link-local and other sessions | private/link-local, multicast and broadcast packet fixtures; no override | two owned guests, positive control where applicable |
| Root resistance | arbitrary bytes, spoof MAC/IP, unknown protocol/VLAN/IPv6 | guest-root route/firewall changes cannot lift restriction |
| Parser bypasses | short lengths, checksums, options, all fragment forms, bounded fuzz | no hostile real-host probing |
| Initialization/metadata failure | alias rejection, missing/failed address refresh, no forwarding | failed startup remains non-ready; no stock fallback |
| Start/stop/restart/rebuild | exact child argv and admission mismatch tests | repeat lifecycle with policy and listener controls |

The pretrial evidence labels were SOURCE INSPECTION; DETERMINISTIC; HOST-ONLY
INTEGRATION; REAL ISOLATED GUEST; AWAITING ATTENDED DEPLOYMENT. The last label
is historical where the live table below now records a result. Native IPv6 and
effectively IPv6-only upstream remain unsupported/unqualified; no insecure fallback.

## Observed source and host-only results

- **DETERMINISTIC:** 24 standalone Rust policy tests pass; 25 pass when linked
  with the actual locked `ip_network` 0.4.1 fallback dependency. These cover
  DHCP discovery/trusted ACK/unicast renewal/expiry, UDP and TCP gateway DNS
  permission, MAC/IP spoofing, host and multicast/broadcast denial, actual
  private/link-local/loopback/CGNAT fallback denial, public fallback admission,
  exact management tuples, handshake/close/expiry/capacity, address removal,
  checksum-valid fragment variants, padding, unsupported frames, metadata
  failure before write, and bounded valid-seed mutations. Packet admission is
  not proof that a real DNS resolver or public HTTPS connection works.
- **DETERMINISTIC, INDEPENDENT REVIEW:** 29 tests pass in the review harness
  (25 package tests plus four independently authored regressions), including
  recovery after lost final TCP handshake ACK. Earlier false-path fixtures
  were corrected and checked against deliberately removed enforcement checks.
- **HOST-ONLY INTEGRATION:** two listener tests pass using owned loopback TCP
  and UDP endpoints; fixed responses, bounded lifetime, rejection of invalid
  binds/ports/durations, and child cleanup observed. These establish the
  fixture's positive control, not a candidate guest denial.
- **SOURCE INSPECTION:** stock Tart passes the selector as two Softnet argv
  elements; candidate rejects missing/duplicate selectors and incompatible
  network overrides before privilege setup and again at proxy construction.
- **PRETRIAL STATUS (2026-09-28, before admission):** real DHCP and lease timing, vmnet UDP/TCP DNS and fallback,
  scoped/split DNS, VPN/resolver transitions, DNS64, public HTTPS, pinned SSH,
  controlled clipboard, guest-root changes, and real start/stop/restart listener
  comparisons had not been exercised at that checkpoint. The later live results
  below supersede that status where tested. The retained DNS endpoint, vmnet
  and hypervisor remain exposed.

Run the pure tests with `rustc --edition=2024 --test
 tools/n1-softnet/policy.rs -o /path/to/new/tests`, then execute that test binary.
The pinned build script runs the additional actual-dependency fallback test.
CI runs both suites without vmnet or privilege. Use
`python3 tools/n1-qualification/listener_test.py` for the bounded host fixture.

- **DETERMINISTIC GO INTEGRATION:** full default and `n1candidate` race suites,
  vet and builds pass. Exact artifact mismatch tests, normal/installer child
  argv, status labeling and observer manifest fixtures cover the closed build
  selection. These do not claim an actual VM restart passed.
- **HOST-ONLY CLI INTEGRATION:** exact unprivileged candidate version plus six
  invalid-configuration cases reject at the expected argument boundary. The
  verifier checks ownership, metadata and digest before executing any probe;
  wrong-executable nonexecution and mocked privileged-mode regressions pass.

## Attended live results, 2026-09-28

Two fresh task-owned guests used the same selected generic base: stock control
and explicit N1 candidate. The private qualification report is summarized here;
its raw bounded receipts remain in the operator's N1 archive. The temporary
candidate digest and both guests were removed after the trial, and stock doctor
remained healthy.

| Case | Live result | Evidence and limit |
|---|---|---|
| Ordinary service on vmnet gateway, TCP and UDP | **PASS: N1 denied** | Three owned listener intervals, including after candidate restart and DHCP renewal. Host and stock guest received the exact response before and after candidate timeouts; each reaped listener counted four TCP and four UDP positive responses. |
| DHCP and management | **PASS for tested path** | Distinct leased IPv4 addresses, observed renewal, and exact-generation pinned SSH before and after renewal; candidate public restart reached READY with a new generation. Lease expiry and rebuild were not live-tested. |
| DHCP-advertised gateway DNS and public HTTPS | **PASS for tested path** | Both guests completed explicit UDP and TCP DNS, truncated UDP DNSKEY followed by a complete TCP answer, and direct HTTPS 200. No public DNS resolver was hard-coded. |
| Non-gateway host private address | **PASS for existing private deny** | Host reached the owned listener; both stock and candidate guests timed out. This does not distinguish N1 from the default private-address rule. |
| Synthetic CLI clipboard | **ATTEMPTED / UNQUALIFIED** | Both writes reported destination outcome unknown; read-only pulls were unavailable. The selected base's source provenance predates the fixed guest clipboard mode and adapter. Exact installed bytes were not inspected before deletion, so the guest-side failure path remains an inference. No retry or Mac pasteboard access occurred. |
| Cross-guest listener | **INVALID** | The owned control-guest listener could not be reached even by the host positive control; candidate timeout is not isolation evidence. |
| Guest-root route/firewall bypass and live parser/metadata failures | **NOT EXERCISED live** | Deterministic fixtures cover the packet and failure paths; no hostile real-host probe or guest firewall change was used. |
| Scoped/split DNS, VPN transitions, DNS64, IPv6-only upstream | **NOT EXERCISED** | No representative authorized environment. Native guest IPv6 and effectively IPv6-only upstream remain unsupported/unqualified. |

The bounded [follow-up trial](follow-up-trial.md) addresses only clipboard
compatibility and a positive-controlled cross-guest TCP check. Address-refresh
observation/write races and public NAT hairpin aliases remain design limits, not
results of this live trial.

## Follow-up preflight, 2026-09-28 — blocked before admission

A later read-only preflight on the same Mac found macOS 27.0.1 (build 26A434),
while the exact candidate build, host-toolchain admission and cleanup procedure
remain bound to macOS 26.6.2 (build 25G83). The previously enrolled
`BoxwardenAlphaQualification` APFS volume and dedicated Tart home were not
mounted. The temporary N1 candidate digest was absent. No candidate was
installed, guest created, clipboard write issued, or cross-guest interval run
in this follow-up. These findings do not change the completed 2026-09-28 live
trial results above; clipboard and cross-guest TCP remain unqualified.

The [preflight record](follow-up-preflight.md) names the exact admission and
cleanup blockers. Do not treat a platform-constant edit or a remount alone as
qualification of the new host build.
