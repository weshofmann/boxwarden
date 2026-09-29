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
- [Read-only follow-up preflight and current blocker](follow-up-preflight.md)
- [Exact macOS 27 source policy and pending qualification](macos27-compatibility.md)
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

The original attended window is closed. The separately authorized narrow
follow-up has not reached live admission on this host. The [matrix](matrix.md)
separates passed live gateway denial and compatibility from
attempted/unqualified clipboard, an
invalid cross-guest listener interval, and unexercised environments. The
approved narrow follow-up has not reached admission: a 2026-09-28 read-only
preflight found this Mac on macOS 27.0.1, outside the then-current exact
26.6.2 executable and cleanup binding. The source on this branch now admits
the exact 27.0.1/26A434 pair for a new attended qualification, while retaining
the stock manifest's 26.6.2 installation record. No new CLI has been
published, and the native Tart volume remains locked/unmounted. On 2026-09-29
the retained qualification sparsebundle was normally remounted and its exact
APFS UUID, writable state and ownership were verified. The prepared R1 configs
still await validation after native Tart access is restored. No second
clipboard or cross-guest test has run. The previous trial showed the necessary
DNS endpoint reachable in its tested topology. Scoped/VPN/DNS64 behavior, native
and effectively IPv6-only upstream, address-refresh races and public NAT
hairpin aliases remain outside the demonstrated claim. The merged N1
implementation retains the explicit candidate build and stock default; N1 is
not currently installed or enabled on this Mac.
