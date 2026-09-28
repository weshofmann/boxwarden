# N1 staged host-network containment candidate

N1 adds a host-side packet policy immediately before Softnet writes guest frames
into vmnet. It denies ordinary host-service destinations while retaining narrow
DHCP, DHCP-advertised gateway DNS, and responses to host-initiated SSH. Stock
Tart, its viewer, and the controlled clipboard protocol are unchanged.

**This is a reviewed source candidate, not Mac qualification.** The default
Boxwarden build still selects stock Softnet and reports its ADR 015 gateway
exposure. A separate `n1candidate` build admits only the exact new artifact and
requires its containment selector in normal and installer launch paths. Neither
build accepts the other's Softnet identity. There is no runtime override.

- [Decision and limits](design.md)
- [Observed acceptance matrix](matrix.md)
- [Review findings and resolutions](review-ledger.md)
- [Attended deployment, positive controls and rollback](attended-qualification.md)
- [Exact source and artifact identity](../../../tools/n1-softnet/artifact.json)
- [Pinned build and licensing instructions](../../../tools/n1-softnet/README.md)

Build and test the candidate without starting a VM:

```sh
go test -race -tags n1candidate ./...
go vet -tags n1candidate ./...
go build -trimpath -tags n1candidate -o /path/to/new/boxwarden-n1 ./cmd/boxwarden
python3 tools/n1-qualification/listener_test.py
```

Run the default Go suite as well. CI covers both variants and the pure packet
policy with its actual pinned public/private classification dependency. Rust
source tests never use upstream's privileged Cargo test runner. `build.py`
requires separately verified source/compiler inputs and a populated locked
Cargo cache; it refuses any output digest different from the staged identity.

Privileged admission remains a separate operator decision. Do not copy the
candidate into a working toolchain or invoke its accepted launch path directly.
Use the attended gate and paired owned-listener controls before making a live
containment claim. Real DNS/VPN, management, clipboard and lifecycle compatibility
remain unexercised. The necessary DNS endpoint is still reachable; address
refresh races, public NAT hairpin aliases and IPv6-only environments are outside
the demonstrated claim.
