# N1: host-service containment candidate

Status: independently reviewed candidate design. The 2026-09-28 bounded host
qualification is recorded in [the matrix](matrix.md); its temporary candidate
admission was removed. The installed stock policy retains ADR 015's gateway
exposure until a separate deliberate promotion.

## Outcome and scope

A malicious guest, including root, must not initiate connections to ordinary
host application services. Retain shared/NAT public IPv4 egress, the vmnet DNS
proxy and host routing/resolver behavior, DHCP, pinned host-initiated SSH and
controlled clipboard over that management path. Automatic clipboard sharing
remains disabled. No host firewall, route, DNS, VPN, viewer or backend replacement.

DNS and DHCP/ARP remain exposed protocol boundaries. This does not protect from
hypervisor, vmnet, DNS or other parser vulnerabilities. Native guest IPv6 remains
denied. IPv6-only host upstream and its dependent destination cases remain
unqualified under ADR 020. Failed/unknown policy setup never falls back to stock.

## Decision and alternatives

Patch the exact Softnet 0.19.0 source, commit
`df84a30016e3d6acc0d30acc660cf3a726f42a9b`. Its guest packet filter runs before
`Host::write` into vmnet (`lib/proxy/vm.rs`). Add the exact block selector
`@boxwarden-host-containment`, forwarded unchanged by stock Tart 2.32.1 commit
`8aa377b71ebfd90b2df9803d3e20033f58d6800c`. Stock Softnet rejects this unknown
selector. The candidate executable itself requires this selector, rejecting absence
instead of offering a legacy fallback. The selector plus a distinct, digest-bound
admitted artifact will be required; an arbitrary executable or a flag string alone is no evidence.

Stock prefix flags cannot express a DNS-port exception; upgrading to stock
0.23.0 adds flow handling but still cannot express this policy. A host PF anchor
would add privileged shared firewall state and unproven vmnet/VPN interactions.
A replacement network/backend would recreate DHCP, resolver and routing behavior.
The patch changes the smallest relevant enforcement point, retaining vmnet DNS.

Distribute the Softnet modification separately under upstream AGPL-3.0, with
upstream license, exact source commit, patch/source digests, locked dependencies
and compiler identity. Do not relabel it as stock 0.19.0. Boxwarden's Apache-2.0
control plane stays separate. Tart's Fair Source code is inspected, not copied
or modified. No mutable installer or compiler channel is part of the build.

## Packet policy

Use a pure Rust policy module exercised by deterministic Ethernet packet
fixtures, integrated into both directions of the actual Softnet proxy. Keep
vmnet construction and bridge isolation enabled. Reject allow overrides, port
exposure and host-network mode when the N1 selector is active.

Refresh trusted host-local addresses before every guest dispatch and before
any exception decision. Remove management flows whose host endpoint is no longer
local. A refresh error stops forwarding rather than using a stale set.

Guest-side processing, before upstream prefix/global fallback:

1. Validate Ethernet source MAC, ARP/IP formats, lengths and transport headers.
   IPv4 must be version 4, IHL 5, internally consistent and nonfragmented. Reject
   all fragments (including first fragments), IP options, unknown EtherTypes,
   VLAN and IPv6. Invalid/unsupported parsing is denial, never fallback. Validate
   checksums where required by the wire protocol; UDP zero checksum is valid IPv4.
2. Permit only well-formed DHCP client UDP 68 to server 67 at broadcast or the
   vmnet gateway; discovery may use 0.0.0.0, renewal needs the current leased
   source. Bind DHCP client identity to the configured guest MAC. ARP supports
   gateway resolution and host management without arbitrary sender spoofing.
3. Other IPv4 traffic requires a live DHCP lease and its exact source. Gateway
   DNS is UDP or TCP destination 53, only when the host-side DHCP reply advertises
   that gateway. Do not replace it with a public resolver or a copied nameserver.
4. Permit responses for bounded exact TCP tuples created by host-originated
   SYN (without ACK) to the leased guest's TCP 22. Track handshake, expiry and
   closure; a guest source port 22 or unsolicited ACK is insufficient. Flow
   state is cleared on lease expiry/address change and cannot authorize another
   host endpoint. A still-live same-address DHCP renewal preserves established
   flows; an expired lease cannot revive prior authority. Every response requires
   the stored host endpoint to remain in the fresh host-local address set. Capacity failure denies a new flow instead of broadening policy.
5. Deny all other packets to gateway or current host-local IPv4 addresses before
   any upstream global/CIDR allow. Collect addresses from trusted host getifaddrs
   immediately for each guest dispatch; failure terminates the proxy, without
   forwarding using stale metadata. Host-address changes have an unavoidable
   observation-to-write race; test refresh and document this qualification limit.
6. Explicitly deny IPv4 multicast (224.0.0.0/4), limited and current host-interface directed broadcast
   destinations except the exact DHCP exception. Stock ip_network 0.4.1
   is_global() includes multicast, so it is insufficient alone. Remaining public
   egress and private/link-local denial retain stock behavior.
   Host-originated management observation must use validated inbound frames and
   current host-local identities. Do not learn state from guest-provided reports.

On ingress, DHCP snooping must validate gateway origin, IPv4/UDP structure,
fragment rejection and guest client identity before changing the lease. A
malformed/foreign DHCP reply cannot poison the policy's source/DNS binding.
Only host-side packets can create management flows. Ordinary incoming response
traffic remains available; inbound fragments never create management authority.

Dropping guest fragments deliberately narrows compatibility. Large DNS answers
must use TCP fallback; deterministic fixtures can demonstrate policy handling,
but actual resolver fallback remains an attended compatibility result. Do not
claim every UDP workload works. Scoped DNS, VPN changes and DNS64 responses keep
the vmnet path; fixture tests prove decision behavior, not environmental success.

## Admission, lifecycle and deployment boundary

Deliver an unprivileged staged candidate with reproducible source and build
identity and policy tests. A closed `n1candidate` Go build tag selects the exact
new Softnet version/path/executable/archive digests and mandatory selector.
Default builds keep the stock identity; candidate builds reject the stock
manifest and stock builds reject the candidate. No runtime override accepts
arbitrary identities. Apply the selector to both normal lifecycle and
installer/base-build launch paths, with restart/rebuild coverage.
Keep the existing root-owned manifest, setuid binary and working clipboard VM
unchanged. The stock Boxwarden build must continue to reject the new artifact
until deliberately admitted through a reviewed exact identity change. No debug
flag, ambient environment variable or arbitrary candidate path may bypass that
admission. Startup, restart and rebuild must all require the selected policy.

The candidate package must include the tested admission/launch implementation,
fail-closed tests and a rollback plan to the prior exact toolchain and launch
policy. Installing it and exercising vmnet with it require separate operator
authorization. Finish source/build/test/review work while that boundary remains.
Runtime/status documentation must distinguish existing gateway exposure,
staged deterministic results and actual guest qualification.

## Verification and evidence

See matrix.md. Test the integrated decision invoked before vmnet write, with
positive controls; do not infer enforcement from a missing listener, a running
VM, a rendered command or self-reported PASS. No global host networking changes.
Only the driver may operate up to two task-owned guests and short-lived narrowly
bound fixed-response listeners. Preserve all existing resources. Private host
metadata and raw evidence remain outside Git; publish sanitized results only.

Build tests must never inherit upstream `.cargo/config.toml`
`runner = "sudo -E"`. Run pure policy tests explicitly without vmnet/root and
retain the locked upstream source dependency identity.

Claim limits: getifaddrs observation and vmnet write are not atomic during host
address reconfiguration. Public NAT hairpin to a host service through an address
not assigned locally is indistinguishable from permitted public egress. The
candidate does not claim either case is contained; stable directly assigned
IPv4 host endpoints are the bounded target for qualification.

Remote subnet broadcast addresses cannot be inferred without remote topology;
only limited broadcast and trusted current host-interface directed broadcasts
are classified explicitly. No remote network discovery is introduced.
