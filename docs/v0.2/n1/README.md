# N1 staged host-network containment candidate

N1 adds a host-side packet policy immediately before Softnet writes guest frames
into vmnet. It denies ordinary host-service destinations while retaining narrow
DHCP, DHCP-advertised gateway DNS, and responses to host-initiated SSH. Stock
Tart, its viewer, and the controlled clipboard protocol are unchanged.

**This is a reviewed explicit candidate with bounded Mac qualification.** The
2026-09-28 macOS 26 trial and the separately approved 2026-09-29 macOS 27.0.1
(26A434) trial both exercised live gateway TCP/UDP denial with working host and
stock controls, plus the tested DHCP, gateway DNS, HTTPS, pinned-management and
restart paths. Both windows are closed; their temporary candidate installations
and task-owned guests were removed. The working installation uses stock Softnet
and retains ADR 015 gateway exposure.

The macOS 27 trial verified actual installed clipboard helpers and responsive
desktops before synthetic transfers. Stock copy/readback passed. The candidate
acknowledged its copy, but its first readback failed: **UNQUALIFIED**. The fresh
cross-guest TCP interval had both strict host positives and a healthy candidate,
but returned `EHOSTUNREACH`, rather than the required timeout: **UNQUALIFIED**.
Neither transfer nor connection attempt was repeated. Earlier failed/invalid
results remain dated history in the [matrix](matrix.md).

A separate `n1candidate` build admits only the exact candidate artifact and
requires its containment selector in normal and installer launch paths. Default
and candidate builds reject each other's Softnet identities; there is no runtime
override. A possible PR #16 merge preserves this explicit candidate build and
the stock default; it does not enable N1 in the working installation.

- [Current beta packaging and next bounded comparison](current-beta.md)
- [Decision and limits](design.md)
- [Observed acceptance matrix](matrix.md)
- [Review findings and resolutions](review-ledger.md)
- [Attended deployment, positive controls and rollback](attended-qualification.md)
- [Bounded follow-up trial and renewed admission gate](follow-up-trial.md)
- [Dated read-only follow-up preflight](follow-up-preflight.md)
- [Exact macOS 27 source policy and bounded qualification](macos27-compatibility.md)
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

The [macOS 27 record](macos27-compatibility.md) distinguishes the exact source
admission policy, historical installation facts, restored storage prerequisites,
live results and cleanup. Stock doctor remained healthy and the stock manifest
retained its original 26.6.2 installation record. Small-file hashes and disk
metadata for twelve protected Tart objects, plus record hashes and raw metadata
for four existing workspaces, matched pretrial baselines. Large disk contents
were not hashed.

The retained DNS endpoint was reachable in the tested topology. Scoped/split DNS,
VPN transitions, DNS64, native and effectively IPv6-only upstream remain outside
the demonstrated claim. Address-refresh races and public NAT hairpin aliases
remain design limits. Candidate clipboard delivery and positive-controlled
cross-guest TCP are unresolved qualification rows, not supported behavior. Any
further live trial needs a fresh exact admission/test/cleanup package; the closed
windows are not indefinite installation authorization.
