# N1 acceptance matrix

Candidate source evidence below was observed on 2026-09-28. Stock evidence is
context, not candidate qualification. **Every real guest row awaits attended
deployment.** No candidate vmnet containment has been exercised on this Mac.

| Case | Deterministic boundary tests | Host-only / isolated guest gate |
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

Evidence labels: SOURCE INSPECTION; DETERMINISTIC; HOST-ONLY INTEGRATION;
REAL ISOLATED GUEST; AWAITING ATTENDED DEPLOYMENT. Native IPv6 and effectively
IPv6-only upstream remain unsupported/unqualified; no insecure fallback.

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
- **NOT EXERCISED:** real DHCP and lease timing, vmnet UDP/TCP DNS and fallback,
  scoped/split DNS, VPN/resolver transitions, DNS64, public HTTPS, pinned SSH,
  controlled clipboard, guest-root changes, and real start/stop/restart listener
  comparisons. The retained DNS endpoint, vmnet and hypervisor remain exposed.

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
