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
