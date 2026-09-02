# Security model

The primary threat is autonomous or third-party code executing in a guest: malicious repositories, dependencies, lifecycle scripts, build tools, prompt injection, hostile web content, skills or AGENTS instructions, hooks, MCP definitions, shell configuration, and persistence attempts.

## Backend-independent policy

Every supported backend must provide these properties:

- the explicit agent workstation account has full control of its disposable guest, including unrestricted non-interactive root access; containment must remain effective against a malicious guest administrator;
- recovery access may use an explicitly qualified host-local hypervisor console, but it is never exposed as a guest or network service and possession of its handle remains trusted-host authority;
- no trusted-host filesystem, Docker daemon, credential store, display server, clipboard, audio, or equivalent privileged host-control bridge is available to the guest;
- the guest may reach the public Internet and host-provided network infrastructure, but cannot initiate connections to private/link-local networks by default; M1A's accepted vmnet-gateway exposure is documented below;
- concurrently running Boxwarden sessions cannot initiate network connections to one another, whether they belong to the same or different security domains;
- guest GUI display and input use an isolated VM console rather than a client protocol connected to the trusted host display server;
- inbound guest traffic is denied except explicitly required host-to-guest management access;
- host-side state-changing operations are exact-targeted, locked, reconciled, and recoverable after partial failure;
- security domains never share identities, profiles, credentials, memory, project registries, golden pointers, or session registries implicitly.

A future Linux backend would need an isolated VM console, an isolated outbound network, no virtiofs/9p or equivalent share, and equivalent management/firewall controls. Those are examples of required properties, not an M1A implementation promise.

VM isolation does not protect secrets, browser sessions, provider credentials, or sensitive artifacts intentionally placed in the same guest. Nor does it prevent a compromised guest from using permitted outbound network access to exfiltrate exposed data. Security-domain separation prevents accidental cross-context injection by the control plane; it is not a boundary against a malicious trusted-host administrator.

When a guest uses Docker, membership in the Docker group is effectively guest
root. A malicious privileged container or Docker-daemon compromise is therefore
a guest compromise, not an acceptable substitute for a separate quarantine VM
or separate-provider session. Other guest-local runtimes have the same status:
they are workload choices within the guest, not a substitute for VM isolation.

M1A grants the UID-1000 workstation account unrestricted passwordless `sudo`.
This is an explicit product property, not a containment exception. Allowing
arbitrary package installation already lets root-run package maintainer scripts
change networking, services, policy, and executables, so a sudoers command list
cannot reliably permit software administration while denying selected root
effects. Network and host isolation therefore cannot depend on guest firewall,
route, account, or sudo policy. Those properties must be enforced outside the
guest by the qualified VM/backend boundary. Quarantine and narrow credential
scope limit what a compromised root guest receives; they do not make that guest
less privileged.

M1A starts Tart with serial hardware through a host-owned persistent two-PTY
relay and records its detached GNU Screen session as trusted runtime state. The
runtime directory is private, the exact PTY devices are mode `0600`, Screen
keeps the operator slave open and drains output while no human is attached, and
Tart exit removes Screen, relay metadata, and endpoint links. Guest `hvc0`
automatically logs in the UID-1000 workstation account, which can use
passwordless sudo. This intentionally creates a general recovery channel for a
trusted host operator and supersedes ADR 012's earlier bounded-fingerprint-only
serial restriction. The channel does not weaken guest-to-host isolation: it is
created and held by host-side control processes and is not reachable through
guest networking. Attaching to the Screen session is equivalent to access to
Tart's graphical console and must never be published or passed into another
guest.

Anything admitted from a disposable session into trusted persistent configuration is a persistence attempt until reviewed. M1A accepts only declarative adapter outputs whose exact bytes, normalized manifest, confidentiality, execution trust, paths, limits, and semantic diff have been validated authoritatively by trusted-host code. Guest checks may fail early for usability but are never security controls. Human review renders guest-controlled bytes without terminal control: C0/C1 and ANSI/OSC sequences are escaped, bidi and zero-width/format controls are visibly marked, truncation and byte counts are explicit, and the exact relevant digests are adjacent to the reviewed material. Untrusted candidate content is never passed to a rich Markdown renderer. Restore occurs in a fresh staging directory and is applied only after validation; arbitrary archives and opaque state are rejected.

Use a credential-free quarantine session for hostile builds or dependencies. `boxwarden` can enforce that it does not inject normal profiles or credentials, but it cannot stop a human from logging in manually through the guest GUI. Quarantine ingress is public source or a narrowly scoped, short-lived, read-only credential; no reusable write credential enters it.

Restore only the domain/profile/project material required for the named task. One provider per session is the recommended high-isolation mode. A multi-provider session is supported for convenience, but compromise of its guest user may expose every provider login, browser session, Git credential, and sensitive artifact present. Separate Unix users do not solve this while the operating principal retains Docker/root-equivalent access.

After suspected compromise, do not inject a fresh rescue credential. Revoke affected credentials, destroy with the compromised path, and recreate from a promoted golden. M1A has no checkpoint operation. A separately reviewed export-to-host-quarantine mechanism may be designed later. Sensitive-data disclosure differs from credential disclosure: credentials can normally be revoked, while private knowledge and proprietary content cannot be rotated.

## M1A Tart realization

Tart provides the macOS VM boundary. Normal start uses Tart's display/input console plus the exact Tart + Softnet shared/NAT launch policy qualified by Task 0. M1A omits an explicit Softnet host block because the vmnet gateway supplies DNS that must follow host, VPN, split-DNS, and DNS64 state, while current Softnet cannot distinguish that service from other gateway ports. Task 0 froze the core launch policy after proving private/session isolation, public Internet, DHCP, inherited DNS, work-VPN/scoped-DNS behavior, and host-to-guest SSH for the exact qualified Tart + Softnet pair. ADR 020 keeps untested IPv6-only-upstream behavior outside the support claim without reopening platform selection. `boxwarden` supplies no filesystem share, extra disk, Rosetta share, VNC server, bridged/host network, exposed Tart port, nested virtualization, host Docker socket, X11 forwarding, or host service integration beyond the accepted gateway reachability. `--net-softnet-allow=0.0.0.0/0` is prohibited in M1A because current Softnet semantics also disable bridge isolation.

Private/link-local egress is denied by default. An operator may explicitly opt a
session into one or more exact private-network CIDRs under ADR 015. That
exception is part of the session's persisted security policy and must be
visible in status; it does not authorize discovery, broad LAN access, bridging,
host networking, or any change that weakens session-to-session isolation. A
session granted such access places every credential and artifact in that guest
within reach of those destinations and must not be described as satisfying the
default private-network-denial property.

The tested Softnet 0.19.0 path requires host root privilege, so Softnet is a
privileged component of Boxwarden's trusted-host attack surface rather than an
ordinary unprivileged helper. M1A must not grant standing passwordless root
execution to a user-writable mutable Homebrew executable. Setuid root is broader
than desirable. The production mechanism remains intentionally undecided, but
it must narrowly bind authorization to the exact qualified Softnet artifact and
relevant execution dependencies in a form an unprivileged user cannot replace
or mutate. A Softnet upgrade must require deliberate requalification and
privilege rebinding rather than inherit authorization automatically.

Softnet constrains guest egress but permits incoming guest traffic; its default gateway allowance means M1A does not deny guest-to-host traffic at the vmnet gateway. A compromised guest can probe or attack host services reachable there. This accepted limitation is subordinate to the required ability to inherit the laptop's changing route and resolver environment and must remain visible in status and validation. Ubuntu enforces inbound deny by default except required host-to-guest SSH. A guest runtime can bypass ordinary `ufw` processing; when a golden includes Docker, services bind guest loopback by default and policy is enforced through Docker-compatible iptables/`DOCKER-USER` rules where needed. No workflow habitually publishes `0.0.0.0`.

Acceptance tests assert the security properties above and separately test the Tart argument mapping. A test that only searches for Tart flags is not sufficient evidence of the policy.
