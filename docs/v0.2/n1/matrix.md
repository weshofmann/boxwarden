# N1 acceptance matrix

Current status: both bounded attended windows are closed. The 2026-09-29
macOS 27.0.1/26A434 trial results and cleanup appear below, separately from the
preserved macOS 26 history. Live gateway TCP/UDP denial and the tested network
compatibility passed on 27; candidate clipboard readback and cross-guest TCP
remain **UNQUALIFIED**. The temporary candidate and fresh guests/state were
removed. The working installation uses stock Softnet and retains ADR 015 gateway
exposure; a possible PR #16 merge does not change that default.

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

## Historical 2026-09-29 macOS 27 source checkpoint — before admission

The follow-up branch now carries an exact 27.0.1/26A434 source admission path
and preserves the installed stock manifest's 26.6.2/25G83 facts. This is code
and deterministic-test evidence only, not a new live result. The changed CLI
has not been published or admitted. The native Tart APFS volume was observed
present but locked/unmounted; the qualification APFS UUID was absent from the
current inventory. The candidate remains removed. Gateway and compatibility
results on 27, synthetic clipboard, and positive-controlled cross-guest TCP
remain **NOT EXERCISED** in the new window. The 26 results above are unchanged.

## Historical 2026-09-29 qualification storage remount — before runtime

The retained qualification sparsebundle was present on mounted DevelData and
unattached. Its normal remount succeeded with the recorded inner APFS UUID
`A178510A-D5EC-4495-828B-BD5445E2B66D`, writable state and ownership enabled.
The reviewed wrapper passed read-only preflight and a regression proving
that a nested filesystem cannot impersonate the recorded backing image.
At that checkpoint native Tart remained locked/unmounted and R1 config
validation and renewed admission were pending. This storage result did not
change any runtime verdict above.

## Historical 2026-09-29 native remount and R1 enrollment — before admission

After explicit owner authorization, the reviewed normal helper unlocked and
mounted native Tart volume `568EE3B5-885B-4278-BD0E-5FE77C5D01A8` at its
canonical Tart home. Exact container/store identity, encryption, writable state,
ownership and directory mode `0700` were verified. The original Tart
sparsebundle remains retired; the separate qualification sparsebundle remains
its own storage input.

The protected twelve-object Tart name set and four qualification workspace
record hashes/raw metadata matched the recorded baselines. VM disk contents
were not hashed, so this is not a full historical byte-equality claim. Only a
fresh private trial state root was created. Both public R1 enrollment commands
passed and produced new configs byte-identical to the reviewed preparations.
The staged stock CLI reported healthy on 27 while retaining the unchanged
26.6.2/25G83 stock manifest. An initial host-only preparation refusal caused by
an incorrect metadata assumption was preserved and corrected before any root
creation; it was not a guest qualification interval.

Candidate publication and every macOS 27 guest/network/clipboard row remain
**NOT EXERCISED** pending the refreshed attended window. No existing VM or
workspace was operated on. These prerequisite results do not change the
2026-09-28 runtime verdicts or enable N1 in the installed default.

## Attended live results, 2026-09-29 — macOS 27.0.1 / 26A434

The renewed approval covered at most two new disposable quarantine guests using
the same retained, stopped generic base. The CLI was rebuilt from `086dd79e`;
Tart, stock Softnet and the N1 Softnet identities were unchanged. No Ubuntu
rebuild, base modification, pre-existing VM/workspace operation, real Mac
clipboard use or host network/security change occurred.

| Case | Live result | Evidence and limit |
|---|---|---|
| Stock desktop/start-stop and candidate restart | **PASS for tested path** | Both first starts reached READY. Both public stop/start cycles completed with guest-accepted, unforced stops and fresh READY generations. Actual bound Wayland/Xwayland/GTK3 desktops were responsive before and after helper staging/restart. No rebuild was tested. |
| DHCP and pinned management | **PASS for tested path** | Distinct dynamic leased IPv4 addresses and DHCP routes after start/restart; exact-generation certificates and host pins used for management. This window did not separately exercise lease expiry or a timed renewal interval. |
| Gateway DNS and public HTTPS | **PASS for tested path** | Both guests completed explicit UDP/TCP gateway DNS and direct HTTPS 200. A truncated UDP `org` DNSKEY response was followed by the probe's successful complete TCP query; this is scripted fallback evidence, not independent proof of automatic OS-resolver fallback. No public DNS resolver was hard-coded. |
| Ordinary service on vmnet gateway, TCP and UDP | **PASS: N1 denied** | One 45-second owned fixture interval after restart. Host and stock positives bracketed one four-second candidate timeout per protocol. Each reaped listener counted four exact positive responses per protocol; these counts are not four candidate attempts. |
| Synthetic CLI clipboard, stock control | **PASS** | Actual installed helper/adapter digests, root ownership/mode and responsive desktop were verified before the first write. One acknowledged copy and exact 31-byte synthetic readback; no Mac pasteboard access. |
| Synthetic CLI clipboard, N1 candidate | **ATTEMPTED / UNQUALIFIED** | The same actual capability preflight passed. Copy was acknowledged with exit 0; the first paste returned exit 1, “clipboard text unavailable,” with no output. No repeated write or read. A later responsive desktop and matching live write worker do not prove native ownership/content/delivery or identify the failed layer. |
| Cross-guest TCP to stock management listener | **UNQUALIFIED** | Both same-generation strict host positives to the existing stock SSH endpoint passed, and candidate READY/DNS/HTTPS/management controls passed. The single candidate attempt did not connect and returned `EHOSTUNREACH` (errno 113), rather than a timeout. The on-link route and FAILED neighbor are consistent with peer ARP denial, but do not prove causation or justify upgrading the original verdict. |
| UDP cross-guest, guest-root route/firewall bypass, live parser/metadata failures | **NOT EXERCISED live** | No isolation weakening, added guest listener or hostile real-host probe. Deterministic evidence remains separate. |
| Scoped/split DNS, VPN transitions, DNS64, IPv6-only upstream | **NOT EXERCISED** | No representative authorized environment. Native guest IPv6 and effectively IPv6-only upstream remain unsupported/unqualified. |

The private qualification report r2 and digest manifest retain exact argv,
generation/pin bindings, bounded observations, unqualified results and independent
review. Report SHA-256:
`ee52e014f6fc4c4748047ccdc6bb4c30447768d60c82d8c3177853f8614b7375`.
The 26 clipboard unknown-write and invalid cross-guest results above were not
overwritten. No failed live interval was rerun to obtain a pass.

Both fresh guests were publicly stopped/deleted, the fixture reaped, and process
and open-file prechecks passed. The owner executed the approved exact candidate
removal; absence was verified. Only the fresh trial state, including its private
CA, was retired without reading or archiving key bytes. Stock artifacts and the
original 26.6.2 manifest were unchanged. Final stock doctor was healthy using a
diagnostic config after the retired trial config correctly refused its missing
state root. That configuration refusal was retained, not relabeled a host failure
or silently repaired by recreating trial state.

Small-file hashes and disk metadata for twelve protected Tart objects, plus
record hashes and raw metadata for four existing workspaces, matched current
pretrial baselines. VM disk
and workspace raw contents were not hashed; this is not a full historical
byte-equality claim. Address-refresh races and public NAT hairpin aliases remain
design limits. Further live work requires a new exact bounded request after a
specific diagnostic design; see [remaining follow-up](follow-up-trial.md).
