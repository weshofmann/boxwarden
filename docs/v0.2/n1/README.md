# N1 staged host-network containment candidate

N1 adds a host-side packet policy immediately before Softnet writes guest frames
into vmnet. It denies ordinary host-service destinations while retaining narrow
DHCP, DHCP-advertised gateway DNS, and responses to host-initiated SSH. Stock
Tart, its viewer, and the controlled clipboard protocol are unchanged.

**This is a reviewed explicit candidate with bounded Mac qualification, not a
change to the working installation.** On 2026-09-28, two disposable guests
completed three positive-controlled live gateway-denial intervals and the tested
DHCP, DNS, HTTPS, pinned-management and restart paths. The temporary candidate
was removed after the trial. The default Boxwarden build still selects stock
Softnet and reports its ADR 015 gateway exposure. A separate `n1candidate`
build admits only the exact candidate artifact and requires its containment
selector in normal and installer launch paths. Neither build accepts the
other's Softnet identity; there is no runtime override.

- [Decision and limits](design.md)
- [Observed acceptance matrix](matrix.md)
- [Review findings and resolutions](review-ledger.md)
- [Attended deployment, positive controls and rollback](attended-qualification.md)
- [Bounded follow-up trial and renewed admission gate](follow-up-trial.md)
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

Any future privileged admission needs a new exact attended approval; the
completed window is closed. The [matrix](matrix.md) separates passed live
gateway denial and compatibility from attempted/unqualified clipboard, an
invalid cross-guest listener interval, and unexercised environments. The
necessary DNS endpoint is still reachable. Scoped/VPN/DNS64 behavior, native
and effectively IPv6-only upstream, address-refresh races and public NAT
hairpin aliases remain outside the demonstrated claim. Merging this branch
would retain the explicit candidate build and stock default; it would not
install or enable N1 on this Mac.
