# Tart and guest platform facts

Verified external facts about the M1A platform, recorded so future work does not re-derive or guess them.

**These are observations, not decisions.** Decisions live in `docs/decisions/`; policy lives in `AGENTS.md` and `docs/`. Where a fact here has a policy consequence, the consequence is named but the authority is the policy document.

Established: 2026-08-30, during the independent architecture review (`docs/reviews/2026-08-30-independent-architecture-review.md`).
Environment: macOS 26.6.2 (build 25G83), arm64, Tart 2.32.1, Softnet 0.19.0 installed via Homebrew. The versions and Homebrew formula identities were re-observed locally during the reconciliation pass; Task 0 must still record qualified artifact/package identity and behavior for the exact pair.

Provenance is marked on every claim:

- **[verified-local]** — observed directly on this host.
- **[verified-separate-host]** — observed on an authorized separate host and
  bounded by that experiment's recorded fidelity limitations.
- **[vendor-source]** — established from the exact pinned upstream source;
  the corresponding filesystem/runtime manifestation may still require
  attended evidence.
- **[vendor-doc]** — stated by Tart, Cirrus Labs, Canonical, or the vendor's own documentation.
- **[unverified]** — expected but not yet proven. Task 0 must confirm.

## Qualification-method rationale

The bounded, front-loaded review method is a factual qualification aid: review
of sibling phases can expose predictable platform failures before an attended
boundary, reducing repeated setup and attended debris. Propagating each newly
learned failure class to unexecuted phases preserves that benefit while keeping
failed runs and evidence-integrity defects explicit. This records the method,
not private run details; its controlling policy remains in `AGENTS.md` and
ADR 024.

The approved ADR024 refinement makes qualification claim-driven. The blocking
subject is the trusted host/operator versus an untrusted guest, including guest
root; every blocking assertion must support a named containment, launch,
privileged-component, lifecycle-safety, or evidence-integrity claim. Immutable
trust/forensic state remains exact. Trusted mutable operational state (source
and qualification Tart homes, temporary namespaces, and disposable VMs) is
admitted by semantic/security-relevant properties, while ordinary atime,
mtime, ctime, and parent-directory timestamp choreography are diagnostic unless
a named claim requires them. This is methodology, not a new product policy.

For qualification records, network observations are separate claims labeled
EXPECTED REACHABILITY, PROHIBITED REACHABILITY, or NOT QUALIFIED. A finite
probe does not establish whole-LAN/private-network or general IPv6 isolation;
the accepted vmnet gateway reachability remains an expected behavior where the
design requires it. Hostile-guest tests must use trusted host-side oracles:
guest-generated PASS/FAIL output is not authoritative security evidence.

---

## Softnet network policy

**[vendor-doc]** Softnet's default policy allows the VM to:

- send traffic from its own MAC address only;
- send traffic from its DHCP-assigned IP only;
- send traffic to globally routable IPv4 addresses;
- **send traffic to the gateway IP of the vmnet bridge** (normally `bridge100`) — i.e. the trusted macOS host;
- receive any incoming traffic.

The fourth point caused BLOCKER-1 in the review: the earlier M1A launch line claimed to realize a backend-independent property that Softnet defaults did not provide. Task 0 then proved the gateway is also required DNS infrastructure. ADR 015 accepts gateway reachability in M1A so host/VPN network inheritance remains functional and defers finer gateway isolation.

**[vendor-doc]** `--allow` / `--block` take comma-separated CIDRs plus an `@host` alias matching the vmnet bridge gateway. Longest prefix match wins; on an identical prefix, block takes precedence. `--allow=0.0.0.0/0` is special-cased: it *additionally disables bridge isolation*.

**[vendor-doc]** vmnet bridge isolation is **on by default**, so VM↔VM traffic is blocked. Softnet's anti-spoofing filtering additionally prevents one VM from claiming another's MAC or source IP — which is what makes MAC→IP address discovery trustworthy with a hostile co-resident VM.

**[vendor-doc]** Softnet lowers the macOS bootpd DHCP lease from 86,400s to 600s so ephemeral VMs do not exhaust the pool. Long-running sessions will renew mid-session.

**[verified-local]** Tart 2.32.1 forwards `--net-softnet-block=@host` to Softnet 0.19.0. The live Softnet process contained `--block @host`, and guest behavior matched the resolved vmnet gateway target.

**[verified-local]** Blocking `@host` preserves DHCP but blocks the DHCP-advertised gateway DNS proxy. Selecting a public resolver restored diagnostic DNS and public TCP reachability, but ADR 015 rejects that as accepted configuration because it can break VPN custom/split DNS and DNS64. Installed Softnet 0.19.0 matches only IPv4 prefixes/`@host`; source through upstream 0.23.0 still has no port/protocol selector.

**[verified-separate-host]** On an authorized work Mac using the same Tart
2.32.1 + Softnet 0.19.0 pair, public DNS/HTTPS, full-tunnel VPN DNS, and
scoped/split DNS all worked through the DHCP-advertised vmnet gateway. A fresh
uncached internal-only name proved per-domain resolver scoping, and one running
VM adapted from full tunnel to scoped DNS to VPN disconnected without guest
reconfiguration. Softnet continued to deny RFC1918 service egress. The guest
was an upstream Ubuntu 24.04 ARM64 Tart image with a different in-guest network
stack, so this qualifies the tested host-side vmnet/DNS behavior rather than the
final Task 0 golden on that VPN. See
`docs/evidence/m1a-work-vpn-network-validation.md`.

**[verified-separate-host]** The tested Softnet 0.19.0 invocation requires host
root privilege. Softnet is therefore a privileged trusted-host component. Its
eventual authorization must bind to an exact qualified artifact and relevant
execution dependencies that an unprivileged user cannot replace or mutate; a
user-writable mutable Homebrew path must not receive standing passwordless-root
authorization. V3/ADR024 selects the root-owned digest-specific `04550`
installation. Upgrades require requalification and privilege rebinding.

**[verified-local]** With exact Tart 2.32.1 and Softnet 0.19.0, the direct
Softnet child was observed with its own PGID; a common PGID is therefore not a
valid descendant-ownership invariant.

**[verified-local]** Exact Tart 2.32.1 `list --format json` may advance
`config.json` ctime for enumerated local and OCI VMs while preserving observed
device, inode, type, mode, ownership, link count, size, blocks, flags, mtime,
and exact bytes/SHA-256. Source review traces the access through
`VMDirectory.running()` to `PIDLock(config.json)`, which opens the file
`O_RDWR` and issues `F_GETLK`; the evidence does not identify which macOS
subsystem causes the ctime update and does not establish atomicity. Under the
approved ADR024 method this is diagnostic evidence for trusted mutable state,
not a filesystem-neutrality or atomicity claim; immutable forensic state stays
strict.

**[vendor-source]** Tart 2.32.1 clone creates its temporary VM below the
selected `TART_HOME/tmp` and then moves that object into `TART_HOME/vms`.
Qualification should retain parent-container metadata as diagnostic context,
but blocking admission is based on source identity, intended object shape,
targeting, and lifecycle/isolation claims rather than timestamp choreography.

**[verified-local]** During the authorized `48bac744` clone/random-MAC/move
phase, the source Tart home's `tmp` and `vms` parent atime advanced together
with their expected mtime/ctime changes. Source-VM timestamp changes followed
the separately observed clone-access pattern, while protected source and clone
identity/config/member/security fields remained valid. The old validator
failed because it admitted parent mtime/ctime but not atime. This supports the
claim-driven ruling that ordinary timestamps on trusted mutable operational
state are diagnostic; the retained run and clone remain immutable failed-run
evidence and are not a qualification pass.

**[vendor-source]** Tart 2.32.1 clone copies `config.json`, `disk.img`,
`nvram.bin`, and optional saved state, but not `control.sock`. The control
socket is created asynchronously by `tart run`. Admission must consequently
use phase-specific VM shapes: a fresh stopped clone has no socket, while the
running and stopped-after-run shapes may retain the exact socket until VM
deletion.

**[verified-local]** Softnet source commits the complete bootpd dictionary
before privilege drop. Host observation changed bootpd inode/mtime while
preserving bytes, SHA-256, security metadata, path, and the sole
`DHCPLeaseTimeSecs=600` semantics. Inode/timestamp replacement is diagnostic;
the privileged-component claim still requires the approved bytes, ownership,
mode, links, and semantics where applicable, and this is not proof of atomicity.

**[verified-local] / [unverified outcome]** Current Softnet accepts ARP and IPv4 frames and drops native IPv6 frames. Apple vmnet documents IPv4 and IPv6 NAT support, but Task 0 has not yet proven whether an IPv4 guest behind Softnet remains functional over Wes's effectively IPv6-only mobile tether. Test the outcome rather than assuming native guest IPv6 is required or host NAT64/464XLAT is sufficient.

## Host services exposed to the bridge

**[verified-local]** A read-only check of the reference host found several services bound to wildcard addresses, including interactive remote-access services enabled by ordinary macOS settings. Under Softnet's default policy these are reachable from a guest. This matters because the trusted host holds the age private identities and the domain SSH user-CA private key.

A macOS host should not be assumed to have a quiet network profile: system features enabled through normal settings bind to all interfaces, and the vmnet bridge is one of them. Enumerate the actual exposure per host during `validate host` rather than recording one host's inventory here.

Note: Boxwarden's management SSH is **host→guest**, but the guest's DHCP-advertised DNS proxy is guest→gateway. Current Softnet cannot permit that DNS service while denying every other gateway port, so strict gateway denial has a real operational cost.

## Tart CLI surface (2.32.1)

**[verified-local]** All isolation-breaking capabilities are **opt-in flags**: `--dir`, `--disk`, `--rosetta`, `--nested`, `--vnc`, `--vnc-experimental`, `--net-bridged`, `--net-softnet-expose`, `--capture-system-keys`. Supplying none of them is therefore a safe default.

**[verified-local]** `tart run` opens a UI window unless `--no-graphics` is passed. Normal M1A sessions require the Tart GUI rather than `--no-graphics`; Task 0 must determine the actual Aqua-login and process-lifetime constraints before a supervision model is chosen. See MAJOR-6.

**[verified-local]** `tart run --serial` opens a serial console; `tart run --serial-path <path>` attaches an externally created one "for programmatic integrations." This is a candidate authenticated, non-network channel for reading a fresh clone's SSH host key fingerprint at first boot, which would avoid TOFU (MINOR-1). **[unverified]**: which guest device it maps to on Ubuntu 24.04 ARM64 under Virtualization.framework, and whether a getty can be reliably kept off it.

**[verified-local]** `tart ip` resolvers: `dhcp` (default, parses the host lease file by MAC), `arp` (Tart's help: "won't work for VMs using the Softnet networking"), `agent` (requires `tart-guest-agent` in the VM). In M1A, **`dhcp` is the only permissible candidate resolver**: `arp` is excluded technically, while `agent` is excluded by the current architecture because no guest-agent bridge is approved. A future explicit architecture review could revisit the latter, and the distinction matters.

**[verified-separate-host]** After a VM stop, relaunch, and network-mode change,
`tart ip --resolver=dhcp` returned a stale previous lease that did not answer;
the running guest had a different address. Resolve immediately before each
management use, refresh after lifecycle changes and on connection failure, and
never persist an address as session identity. This reproduces and strengthens
the primary Task 0 guidance without selecting ARP or installing the guest agent.

**[verified-local]** `tart set` supports `--random-mac`, `--random-serial`, `--cpu`, `--memory`, `--display`, `--disk-size`. `--disk-size` can only grow.

**[verified-local] / [vendor-doc]** `tart clone` performs automatic pruning when TART_HOME lacks capacity, with `--prune-limit` defaulting to 100 GB, disabled by `TART_NO_AUTO_PRUNE`. **It evicts OCI-cache and IPSW-cache entries only — never local VMs in `~/.tart/vms`.** Locally built goldens and sessions are not at risk. It would matter if goldens were ever distributed as OCI images, since those live in the prunable cache.

**[vendor-doc]** `TART_HOME` defaults to `~/.tart/`; local VMs in `~/.tart/vms/`, pulled images in `~/.tart/cache/OCIs/`. The VM namespace is flat and global — it is not domain-scoped.

**[verified-local]** `tart list --format json` exists, as the lifecycle document assumes.

**[verified-local]** `tart suspend` / `--suspendable` exist and are deliberately unused: suspending writes guest RAM, including live secrets, to host disk.

## Guest imaging and vendor artifacts

**[vendor-doc]** `ubuntu-24.04.4-desktop-arm64.iso` **is published** on `cdimage.ubuntu.com/releases/noble/release/` alongside `ubuntu-24.04.4-live-server-arm64.iso`. A generic ARM64 Desktop ISO exists; the Raspberry Pi preinstalled images are a separate thing.

**[vendor-doc]** Ubuntu 24.04 is the first LTS with autoinstall for Ubuntu Desktop Bootstrap, and from **24.04.1** the Desktop installer supports the same autoinstall functionality as Server (subiquity 24.04.1). The plan's preferred Desktop-ISO path is viable; the live-server fallback remains a reasonable hedge.

**[vendor-doc]** Canonical's archive snapshot service (`snapshot.ubuntu.com`) exposes the Ubuntu archive as of any date/time since **2023-03-01**, addressed by a UTC snapshot ID (`YYYYMMDDTHHMMSSZ`), supported by the apt in 24.04. This is how the OS layer can reach `docs/tool-provenance.md`'s stronger "indefinitely reproducible repository closure" claim rather than only "reproducibly identified artifact."

**[vendor-doc]** ChatGPT Desktop for Linux launched **2026-08-11 in public preview**, distributed as `chatgpt_arm64.deb` from `chatgpt.com/download`, tested by OpenAI on Ubuntu 24.04 LTS. Preview status and a bare download endpoint (rather than a signed apt repository with retained history) mean its digest changes per release and superseded versions are unlikely to remain retrievable — record the limitation in the lock rather than weakening the provenance claim.

**[vendor-doc]** Google Antigravity ships Linux **arm64**, with an apt repository exposing an arm64 index as well as `.tar.gz` archives. Stated requirements include glibc ≥ 2.28. A signed repository with a key fingerprint is a stronger provenance claim than a bare download URL; prefer it where a choice exists.

**[unverified]** Grok Build on Linux ARM64 was not investigated.
