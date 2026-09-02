# Boxwarden M1A work-VPN / split-DNS validation

Supplemental Task 0 evidence. Status vocabulary matches
`docs/evidence/m1a-bootstrap-spike.md`: `OBSERVED`, `VENDOR-DOCUMENTED`,
`INFERRED`, `NOT YET PROVEN`.

## Scope

This run answers one open Task 0 question: whether the Tart + Softnet
networking model selected by ADR 015 remains usable when the macOS host is
connected to a real corporate VPN that supplies custom and/or split-horizon
DNS. It is supplemental evidence only. It does not begin Task 1, does not
implement Boxwarden, and does not modify the accepted network policy.

It was collected on a separate authorized work Mac because the primary
Boxwarden development Mac cannot reach that VPN. The primary Task 0 agent
owns integration of this result; this document does not modify
`docs/evidence/m1a-bootstrap-spike.md`.

## Privacy boundary

The host used here is a corporate machine on a private network. The following
are intentionally omitted from this document and were never written to the
repository: internal hostnames, internal and resolver IP addresses, DNS search
suffixes and zone names, VPN server/tenant identifiers, VPN configuration,
account names, certificates, and authentication state.

Where an internal resource is referenced it appears only as
"known internal-only hostname A" and "known internal-only hostname B". Where a
resolver is referenced it appears
only as "VPN-provided/scoped resolver" or "vmnet gateway resolver". Raw
capture remained in an owner-private directory outside the repository and
outside Git for the duration of the run.

Address-space claims are stated as classes (RFC1918 / link-local / globally
routable) rather than literals. Two DNS responses are compared by equality of
their answers rather than by publishing them.

## Environment

| Property | Value |
|---|---|
| Architecture | Apple Silicon, `arm64` |
| macOS | 26.6.2 (build 25G83) |
| Tart | 2.32.1 |
| Softnet | 0.19.0 |
| Toolchain provenance | Homebrew formulae from the `cirruslabs/cli` tap |
| Host connected to corporate VPN | Yes |
| Tunnel shape | Full tunnel: a `utun` device carried the default public path via the standard `0.0.0.0/1` + `128.0.0.0/1` pair |
| Custom/split DNS present | Yes; both VPN DNS modes were exercised (see below) |

The Tart and Softnet versions are the **same pair** qualified on the primary
Task 0 Mac (Tart 2.32.1, Softnet 0.19.0), installed from the same tap. No
version divergence had to be reconciled, and no corporate-managed software was
altered, downgraded, or disabled to run this validation.

### Host privilege requirement (new observation)

`OBSERVED`. Softnet cannot run unprivileged. With the Homebrew binary as
installed, `tart run --net-softnet` failed with
`root privileges are required to run and passwordless sudo was not available`.
Softnet requires either a passwordless-sudo grant for its binary or a setuid
root bit on it.

This is a persistent privileged change to the trusted host and is not recorded
in the primary Task 0 evidence. It belongs in the host-integration surface
described by `docs/security-model.md`, because it is a standing root-execution
grant on the machine that holds the age private identities and the domain SSH
user-CA private key.

**A targeted sudoers grant is the preferred mechanism, not setuid.** A
`/etc/sudoers.d` entry naming the invoking user and the exact Softnet binary
path is scoped per user, so only the Boxwarden operator gains the capability.
A setuid root bit is scoped to the binary instead, making it root-escalating
for *every* local user on the machine, which is strictly broader than the
control plane needs. The sudoers grant is also easier to audit and is reverted
by removing a single file. Note additionally that a Homebrew upgrade of Softnet
resets a setuid bit, so that form of the grant is not durable across upgrades.

For this supplemental run the setuid form was used at the operator's election
and was **reverted at the end of the run**; the recommendation above is the
form M1A should adopt.

### Two VPN DNS modes were tested

The corporate VPN supports two configurations, and the operator exercised both:

1. **Full tunnel, all DNS through the tunnel.** The default, and what the
   operator reports almost all users run. Every resolver is VPN-supplied.
2. **Full tunnel for traffic, scoped/split DNS.** The default resolver is the
   local network's DHCP-assigned resolver reached over the physical interface,
   while one internal suffix is scoped to a VPN-provided resolver.

Mode 2 is the harder case for this architecture and is the one that carries
the result; see "Findings".

## Guest under test

The guest was launched under the ADR 015 accepted policy. Observed Tart argv:

```text
tart run --net-softnet --no-audio --no-clipboard --no-graphics <task0-vm>
```

The live Softnet process carried no `--block` argument, matching ADR 015's
decision to omit `--net-softnet-block=@host`. The launch supplied no `--dir`,
`--disk`, `--rosetta`, `--nested`, `--vnc`, `--net-bridged`,
`--net-softnet-expose`, Docker, clipboard, audio, or port-exposure option. No
old host-block policy was reintroduced for this test. `--no-graphics` is a
headless-harness convenience for this supplemental run and is not part of the
M1A session shape; it does not affect networking.

`OBSERVED`. Inside the guest there was no VPN client, no `tun`/`utun`/`wg`
interface, and exactly one `nameserver` line, pointing at the local stub
resolver whose only upstream was the vmnet gateway. No public resolver was
pinned at any point.

The VM name carried the Task-0 spike prefix. Tart inventory was recorded before
any mutation; this host had **zero** pre-existing Tart VMs, so no unrelated VM
was read, modified, or deleted.

**Guest fidelity limitation.** This run used a prebuilt upstream Ubuntu 24.04
ARM64 Tart image, not the Task 0 golden. It was verified to carry no pinned
public resolver, so it is a clean vehicle for a DNS-inheritance test, but its
in-guest stack is netplan/systemd-networkd whereas the golden uses
NetworkManager. Both consume the DHCP-advertised resolver (option 6)
identically, and the property under test is a host-side vmnet behavior that the
guest only consumes. The gap is recorded rather than argued away; see
"Limitations".

## Test matrix

| Property | Status | Result |
|---|---|---|
| host_public_dns | OBSERVED | Passed, in both VPN DNS modes |
| host_public_connectivity | OBSERVED | Passed; HTTPS 200, egress via the tunnel device |
| host_internal_dns | OBSERVED | Known internal-only hostname A resolved to a private (RFC1918) address |
| host_internal_connectivity | OBSERVED | Known permitted service reachable; ordinary HTTPS returned an application redirect |
| guest_public_dns | OBSERVED | Passed in both VPN DNS modes, via the vmnet gateway resolver only |
| guest_public_connectivity | OBSERVED | Passed; bounded HTTPS request returned 200 |
| guest_internal_dns | OBSERVED | Passed in both VPN DNS modes; the guest resolved hostname A to the identical address the host resolved |
| guest_internal_connectivity | OBSERVED | Denied by Softnet private-network policy, as ADR 015 requires. See "Findings". |
| guest_scoped_split_dns_proof | OBSERVED | A second internal-only hostname (B), confirmed never previously queried on this host, resolved from the guest through the vmnet gateway with a full undecremented TTL and an answer byte-identical to the host's, while a public resolver returned NXDOMAIN for it |
| static_dns_required | OBSERVED | No |
| prohibited_integrations_required | OBSERVED | No |
| guest_to_private_still_denied | OBSERVED | Denied while on VPN; a single controlled probe to the host's own private LAN address timed out |
| guest_to_vmnet_gateway | OBSERVED | Reachable, as ADR 015 accepts and reports |
| bridge_isolation | OBSERVED | The vmnet member interface carried the `PRIVATE` flag |
| vpn_transition | OBSERVED | Passed. One VM process spanned full-tunnel, scoped/split-DNS, and VPN-disconnected states. The guest adapted automatically in both directions with no restart and no reconfiguration. |

## Findings

`OBSERVED`. **The current M1A Softnet/vmnet design is compatible with the
tested corporate VPN in both of its DNS modes.** The guest retained ordinary
public DNS and public Internet access throughout, and additionally inherited
the VPN's internal name resolution, using nothing but the DHCP-advertised vmnet
gateway resolver.

The load-bearing evidence is the scoped/split-DNS case. Under mode 2 the
default resolver was the local network's resolver reached over the physical
interface, so a positive result could not be explained by "every resolver
answers everything".

A first-pass positive on hostname A was **not** treated as conclusive: the
gateway's answer arrived with a decremented TTL, showing it had been served
from the host resolver cache populated during mode 1. Caching was therefore
eliminated explicitly, using a second internal-only hostname (B) that the
operator confirmed had never been queried on this host:

- Queried directly from the guest, a public resolver returned `NXDOMAIN` for
  hostname B, establishing that the name is genuinely invisible to public DNS.
- Queried from the guest through the vmnet gateway resolver, hostname B
  returned `NOERROR` with a private (RFC1918) address and a **full,
  undecremented TTL** — the signature of a fresh upstream lookup rather than a
  cache hit.
- That answer was **byte-identical** to the answer the trusted host resolved
  for the same name, compared by digest rather than by publishing the address.

A never-queried name, invisible to public DNS, resolving freshly and
identically through the vmnet gateway while the host's default resolver was the
non-VPN local resolver, proves that macOS applied its per-domain scoping to the
query arriving from the vmnet gateway DNS proxy on the guest's behalf.

A corroborating control pointed the same way: for a randomly generated,
never-cached name under the scoped internal suffix, the vmnet gateway and a
public resolver returned **different SOA authority records**, again indicating
different authoritative sources for the scoped suffix.

`OBSERVED`. **`guest_internal_connectivity` was denied, and that is the correct
M1A outcome, not a VPN incompatibility.** Known internal-only hostname A
resolves to an RFC1918 address, and Softnet's default policy permits guest
egress only to globally routable IPv4 destinations. The guest's ordinary
HTTPS connection to hostname A timed out while the host's succeeded. This is
the same property the primary evidence already records as
`guest_to_private | OBSERVED | denied`, and it continued to hold while the host
was on the VPN. A separate controlled probe from the guest to the host's own
private LAN address also timed out.

The security-relevant summary is therefore: **DNS inherits; private-network
egress stays denied.** A Boxwarden session on a VPN-connected work laptop can
resolve corporate names but cannot reach corporate RFC1918 services. If future
milestones ever require guest access to an internal service, that is a new
architectural decision requiring its own review — it is not available today and
was not enabled here.

`OBSERVED`. No workaround was required or applied. No public DNS server was
hardcoded, no VPN configuration was copied into the guest, no VPN client was
installed in the guest, no bridged or host networking was used, and no
endpoint or network control was weakened. Nothing in the corporate VPN
configuration was modified beyond the operator toggling its own supported DNS
mode.

`OBSERVED`. **The guest adapts to VPN transitions automatically, without a
restart or any reconfiguration.** A single VM process, launched while the host
was in full-tunnel mode, remained running across two subsequent host network
changes:

- full tunnel with all DNS tunneled -> scoped/split DNS: the guest continued to
  resolve internal names, and the fresh-lookup proof above was collected in
  this state;
- scoped/split DNS -> VPN disconnected: the guest immediately lost internal
  resolution (the vmnet gateway returned `NXDOMAIN` for internal-only hostname
  B) while retaining public DNS and public HTTPS, including for a public name
  not previously resolved in that guest.

The guest's kernel boot ID was identical before and after the transition,
proving it never rebooted, and the same Tart and Softnet processes spanned all
three states. No guest-side action was required at any point: no restart, no
DHCP renewal forced by the operator, no resolver edit. Guest DNS caching is the
only lag, and it is ordinary TTL behavior rather than a design problem.

This is the behavior ADR 015 intends: the guest follows the host's effective
resolver state rather than pinning its own.

### Incidental observation: `tart ip` returned a stale lease

`OBSERVED`. After the VM was stopped and relaunched with a different networking
mode, `tart ip --resolver=dhcp` reported an address from the previous run's
lease while the guest was actually reachable at a different address on the
active vmnet bridge. The stale value was confirmed wrong: the reported address
did not answer, and the bridge's own ARP table held the correct one.

This is direct support for the existing `management_address` guidance in the
primary evidence — resolve immediately before management use, refresh on
failure or lifecycle change, and never persist an address as identity. It is
recorded here because this run reproduced the failure the guidance anticipates.

## Limitations

One corporate VPN environment does not prove compatibility with every VPN
product or DNS topology. Specifically:

- A single vendor and configuration was tested. Clients that install their own
  resolver daemon, use DNS-over-HTTPS/TLS to a private endpoint, enforce
  posture checks per-connection, or refuse to serve queries proxied from a
  local virtual interface could behave differently.
- The guest was a prebuilt upstream Ubuntu 24.04 ARM64 image rather than the
  Task 0 golden. See "Guest fidelity limitation".
- IPv6 behavior was not the subject of this run and remains covered by the open
  `ipv6_only_upstream` row. Native guest IPv6 remains unproven; Softnet 0.19.0
  filters an IPv4 packet path.
- Only one internal destination was used, on one port, with one ordinary
  application connection. No scanning, enumeration, service discovery, or
  probing of any other internal host was performed at any point, by design.
- The corporate VPN's own split-DNS mode is, per the operator, rarely used in
  practice; the commonly used mode is full tunnel with all DNS tunneled. Both
  were tested, so this limitation affects only how representative each mode is,
  not coverage.

## Integration recommendation

**PASS**

The `vpn_custom_split_dns` row in `docs/evidence/m1a-bootstrap-spike.md`
should become:

> | vpn_custom_split_dns | OBSERVED | On a separate authorized work Mac with the same qualified Tart 2.32.1 + Softnet 0.19.0 pair, a guest launched under the ADR 015 accepted policy retained public DNS and public HTTPS and additionally inherited VPN-controlled internal name resolution through the DHCP-advertised vmnet gateway resolver alone. Both corporate VPN DNS modes were exercised: full tunnel with all DNS tunneled, and scoped/split DNS. In the scoped mode the default resolver was the local non-VPN resolver, and a random never-cached name under the scoped internal suffix returned a different SOA authority via the vmnet gateway than via a public resolver, proving fresh per-domain scoping rather than a cached answer. A single VM process spanned full-tunnel, split-DNS, and VPN-disconnected states, adapting automatically in both directions with no restart or reconfiguration. No static public resolver, guest VPN client, bridging, or other prohibited host integration was required. Guest connections to the resolved internal RFC1918 address remained denied by Softnet private-network policy, consistent with `guest_to_private`. See `docs/evidence/m1a-work-vpn-network-validation.md`, including its guest-fidelity limitation and the newly recorded Softnet host root-privilege requirement. |

Two items are referred to the primary Task 0 agent rather than decided here:

1. **Softnet's host root requirement** is unrecorded in the primary evidence and
   is a standing privileged grant on the trusted host. It should be documented
   in the security model's host-integration surface, specifying a targeted
   per-user `sudoers` grant for the exact Softnet binary as the accepted
   mechanism, and recording that setuid root is broader than required because
   it escalates for every local user and is reset by Homebrew upgrades.
2. **Guest fidelity.** The operator elected not to build the Task 0 golden for
   this supplemental run, on the basis that the proven property is host-side
   vmnet behavior which the guest only consumes through the DHCP-advertised
   resolver. If the reviewer judges the netplan/NetworkManager difference
   material, this run should be repeated against the golden before the row is
   frozen.
