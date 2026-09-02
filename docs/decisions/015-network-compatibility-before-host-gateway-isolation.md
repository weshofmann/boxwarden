# ADR 015: Prefer host-network compatibility over vmnet-gateway isolation in M1A

Status: Accepted; Task 0 qualified with conditions

Supersedes ADR 013 for M1A launch-policy requirements.

## Context

Boxwarden must remain usable on a traveling work laptop whose effective network
environment changes. Relevant environments include ordinary Wi-Fi, mobile
tethering that is effectively IPv6-only, NordVPN, corporate VPNs with custom or
split DNS, and combinations of those layers. The guest must follow the host's
effective route and resolver state; binding the VM to a physical interface or
hard-coding a public resolver would bypass or break that behavior.

Task 0 demonstrated that Tart 2.32.1 passes `--net-softnet-block=@host` through
to Softnet 0.19.0 and that `@host` is the vmnet shared/NAT gateway address. The
same gateway also provides the DHCP-advertised DNS proxy. DHCP still succeeds
when `@host` is blocked because its discovery path is handled separately, but
DNS queries to the gateway are denied. Manually selecting a public resolver
restores ordinary public DNS, but that diagnostic workaround is incompatible
with VPN-controlled, split, custom, and DNS64 resolver behavior.

Softnet 0.19.0 rules match IPv4 prefixes or `@host`, not transport protocols or
ports. Source inspection through upstream tag 0.23.0 found directional and
stateful prefix rules but still no destination-port selector. Current flags
therefore cannot permit only gateway DNS while denying other gateway services.
Current Softnet also filters an IPv4 packet path; native guest IPv6 and IPv4
guest operation over an IPv6-only upstream require separate empirical evidence.

Requiring broad gateway denial before that evidence exists would make the
workstation unusable in required environments. A security property that breaks
the laptop's required network control plane is not an acceptable M1A boundary.

## Decision

M1A uses Task-0-qualified Tart plus Softnet in vmnet shared/NAT mode. The
candidate launch includes `--net-softnet` and omits
`--net-softnet-block=@host`. It also disables clipboard and audio and supplies
none of Tart's filesystem, Docker, display-server, port-exposure, bridged,
host-network, or other host-integration options.

The guest accepts DNS supplied by DHCP. M1A does not pin a public resolver.
Physical host bridging and Tart host networking remain prohibited. The special
Softnet `--allow=0.0.0.0/0` policy remains prohibited because it disables
bridge isolation in addition to broadening egress.

The core M1A gate requires public egress, default private/link-local network
denial, session-to-session denial, Softnet anti-spoofing behavior, required
host-to-guest management SSH, DHCP, host-provided DNS, and management-address
discovery. Outcome-level compatibility is recorded separately for each tested
network environment under ADR 020. Work-VPN and scoped/split-DNS behavior is
qualified; IPv6-only-upstream behavior and its two dependent destination cases
remain explicitly unqualified and must not be claimed as supported.

Default private/link-local denial remains the normal session policy. M1A will
also provide repeatable `--allow-private-network <CIDR>` session options to
authorize one or more specific private-network CIDRs when a work session
requires them. The control plane must validate and persist the exact allowlist,
display the resulting
exposure in status, and require it again through recorded session policy on
every start. The backend may map only those exact CIDRs to a qualified Softnet
exception. It must reject broad allow-all, implicit LAN discovery, vmnet/session
network exposure, and any exception that disables session isolation. This
opt-in does not authorize bridged or host networking and does not make private
service access a default or Task 0-proven property.

Guest-to-vmnet-gateway service denial is deferred. A future implementation may
permit only DHCP-learned DNS over UDP and TCP port 53 while denying other
gateway services, but only if the exact mechanism is pinned and proven not to
interfere with VPN, split-DNS, DNS64, IPv6-only-upstream, DHCP, or management
behavior. Feasibility is not assumed.

## Consequences

A compromised M1A guest can probe and attack macOS services reachable through
the vmnet gateway. The VM boundary, lack of privileged host integrations,
private-network denial, and session isolation reduce other paths but do not
remove this host network attack surface. Status, validation, and security
documentation must report the limitation rather than claim guest-to-host
network isolation.

In exchange, the guest can use vmnet's gateway DNS proxy and inherit host/VPN
network policy. This makes M1A usable across required work and travel networks
and avoids a diagnostic public-DNS override becoming production configuration.

Task 0 reviewed the exact launch policy and resolver behavior and, on a
separate authorized work Mac using the same Tart 2.32.1 + Softnet 0.19.0 pair,
qualified VPN-controlled and scoped/split DNS inheritance for the host-side
vmnet path. That supplemental guest was not the final Task 0 golden and private
RFC1918 service egress remained denied under the default policy, as required.
That denial is a separate policy limitation, not a split-DNS failure; sessions
that eventually opt into exact private CIDRs require their own validation. Task
0 closes under ADR 020 while IPv6-only-upstream behavior and its dependent
IPv4-only/IPv6-only destination cases remain unqualified. See
`docs/evidence/m1a-work-vpn-network-validation.md` and ADR 020.
