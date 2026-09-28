# N1 review ledger

## Design review — 2026-09-28

Fresh independent Astra/Extra High review of the design and exact upstream
source: proceed with implementation and staged unprivileged artifacts after
these corrections. No Critical finding.

- Important: stock ip_network 0.4.1 global classification includes multicast.
  Accepted: explicitly deny 224/4 and broadcast except validated DHCP, before
  stock fallback; add packet fixtures.
- Important: stale management flows could authorize a formerly local address.
  Accepted: refresh trusted host addresses before exceptions, require current
  locality for every response, evict flows on address removal; add regression.
- Clarification: preserve flow state for a still-live same-address DHCP renewal;
  expiry or address change revokes it. Add lifecycle fixtures.
- Delivery: closed n1candidate Go build identity, mandatory selector in both
  launch paths and candidate executable, default/candidate mutual artifact
  rejection. No installed-toolchain changes.
- Verification: upstream Cargo runner invokes sudo. Never use it; standalone
  deterministic tests and unprivileged builds only.

Explicit claim limits: userspace host-address observation/write race, and public
NAT hairpin endpoints not assigned to the host. These remain outside demonstrated
containment; no qualification claim follows from source review alone.

## Go and host-fixture implementation evidence

- Go launch regressions first failed for omitted candidate selectors in normal
  and installer paths; both now pass. Exact stock/candidate artifact rejection is now bound to the reproduced
  candidate digests; both identity regressions pass.
- Status regression first failed for missing policy/qualification information;
  the fix reports a build policy separately from readiness. Default full Go
  suite passes after these changes.
- Owned fixture regressions failed before implementation. Host-only loopback
  TCP/UDP positive controls, bounded exit and invalid bind/port/lifetime cases
  now pass. No live guest containment is inferred.

## Driver packet inspection — resolved before artifact binding

- Standard minimum-size Ethernet ARP frames include padding; an exact 28-byte
  ARP-payload requirement would reject normal gateway resolution. Require a
  padded positive fixture and correction before artifact binding.
- Foreign-DHCP and fragment fixtures must carry repaired checksums; otherwise
  they only exercise checksum rejection and can falsely claim deeper coverage.
- Reject IPv4's reserved fragment flag and use formatted source for review.
- Exercise the same metadata-refresh/forwarding hook used before vmnet write,
  including a refresh failure proving no packet is forwarded.

## Independent boundary code review — 2026-09-28

Fresh Astra/Extra High review followed the real guest-to-vmnet write path,
trusted ingress, packet parsers, flow authority, metadata refresh, build tooling
and Go integration. No unresolved Critical/Important source finding remains.

- Important compatibility defect: observing the host's final TCP handshake ACK
  advanced state before VM delivery; an ENOBUFS drop made the guest's legitimate
  SYN+ACK retransmission fail. Fixed with an exact sequence/ACK/flags replay
  allowance bounded by the original 30-second handshake window. Bare guest SYN
  stays denied; repeated guest packets cannot keep a half-open flow alive.
- Evidence corrections: a purported fragment mutation changed an Ethernet
  source byte, and another DHCP identity mutation retained a stale checksum.
  Fixed offsets/checksums; mutants that remove the intended enforcement now
  fail the targeted fixtures.
- Coverage: retained independent fragment and renewal regressions, fixed
  handshake expiry, valid-seed mutations, and actual locked-dependency
  public/private fallback through the production forwarding hook. The original
  random-byte test is only a parser panic smoke test.
- Independent replay: 29/29 tests pass against policy SHA-256
  `37491b19e58bc0aaed1d3730b40d52e2cad53c74b8337df96fd8c6c145331ddf`.

This review supports staged candidate delivery. It does not qualify live Mac
network behavior or remove the documented address-race/NAT-hairpin limits.

The final CLI test helper also received narrow independent review. It now
requires an exact current-operator-owned, regular single-link unprivileged
candidate before any execution, with bounded subprocess lifetimes. Wrong-digest
marker tests prove nonexecution; forbidden-mode cases use synthetic metadata.
Full default/candidate Go race suites, vet and builds pass. A historical observer
fixture was made build-aware; no admission rule was relaxed.
