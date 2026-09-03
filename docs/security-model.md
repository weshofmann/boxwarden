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

The same ADR 017 channel is the only initial management-trust bootstrap path.
Production uses a supervisor-owned two-PTY broker rather than opaque socat
forwarding and therefore requires ADR 017 requalification. Tart opens only its
slave; exact system Screen is a direct waitable child and sole reader of the
operator slave; the bounded broker owns both masters, all forwarding, and the
serialized `idle`/`console`/`automation`/`failed` state. Operator input reaches
Tart only in `console`; every other mode, including `idle` and `automation`,
discards and counts it without buffering or replay. Automation never opens
the operator PTY or uses Screen log/hardcopy/input-control facilities. A fresh
nonce and start generation frame each bounded exchange, but guest `active`
state contains only durable domain/session/backend identity, CA fingerprint,
and derived principal. Later generations verify that same durable binding and
the current host key. Missing, duplicate, interleaved, oversized, overflowed,
or mismatched frames poison the generation. The CA private key never enters the
guest. Network reachability and `tart ip` are not identity evidence; TOFU and
`StrictHostKeyChecking=no` remain prohibited.

The generic golden contains only root:root mode-`0755`
`/etc/ssh/boxwarden`; its `active` child is absent. Serial bootstrap constructs
a private root-only sibling and, only after complete verification, publishes
root:root mode-`0755` `active` and `authorized_principals`, root:root mode-`0644`
public CA and `authorized_principals/boxwarden` files, and a root:root
mode-`0600` binding manifest with no group/other-writable ancestry. The final
directories must be traversable because OpenSSH opens the principals file under
the workstation UID and applies StrictModes ancestry checks. OpenSSH reads both
configured trust files for each certificate authentication, so no sshd reload
is needed; a missing pre-bootstrap target simply fails authentication. Because
the CA path itself does not receive the principals path's StrictModes walk, the
helper independently rejects symlinks, non-regular files, wrong ownership,
writable ancestry, and byte/mode mismatches for the entire tree. The first
strict certificate SSH probe is the end-to-end proof.

Anything admitted from a disposable session into trusted persistent configuration is a persistence attempt until reviewed. M1A accepts only declarative adapter outputs whose exact bytes, normalized manifest, confidentiality, execution trust, paths, limits, and semantic diff have been validated authoritatively by trusted-host code. Guest checks may fail early for usability but are never security controls. Human review renders guest-controlled bytes without terminal control: C0/C1 and ANSI/OSC sequences are escaped, bidi and zero-width/format controls are visibly marked, truncation and byte counts are explicit, and the exact relevant digests are adjacent to the reviewed material. Untrusted candidate content is never passed to a rich Markdown renderer. Restore occurs in a fresh staging directory and is applied only after validation; arbitrary archives and opaque state are rejected.

Use a credential-free quarantine session for hostile builds or dependencies. `boxwarden` can enforce that it does not inject normal profiles or credentials, but it cannot stop a human from logging in manually through the guest GUI. Quarantine ingress is public source or a narrowly scoped, short-lived, read-only credential; no reusable write credential enters it.

Restore only the domain/profile/project material required for the named task. One provider per session is the recommended high-isolation mode. A multi-provider session is supported for convenience, but compromise of its guest user may expose every provider login, browser session, Git credential, and sensitive artifact present. Separate Unix users do not solve this while the operating principal retains Docker/root-equivalent access.

After suspected compromise, do not inject a fresh rescue credential. Revoke affected credentials, destroy with the compromised path, and recreate from a promoted golden. M1A has no checkpoint operation. A separately reviewed export-to-host-quarantine mechanism may be designed later. Sensitive-data disclosure differs from credential disclosure: credentials can normally be revoked, while private knowledge and proprietary content cannot be rotated.

## M1A Tart realization

Tart provides the macOS VM boundary. Normal start uses Tart's display/input console plus the exact Tart + Softnet shared/NAT launch policy qualified by Task 0. M1A omits an explicit Softnet host block because the vmnet gateway supplies DNS that must follow host, VPN, split-DNS, and DNS64 state, while current Softnet cannot distinguish that service from other gateway ports. Task 0 froze the core launch policy after proving private/session isolation, public Internet, DHCP, inherited DNS, work-VPN/scoped-DNS behavior, and host-to-guest SSH for the exact qualified Tart + Softnet pair. ADR 020 keeps untested IPv6-only-upstream behavior outside the support claim without reopening platform selection. `boxwarden` supplies no filesystem share, extra disk, Rosetta share, VNC server, bridged/host network, exposed Tart port, nested virtualization, host Docker socket, X11 forwarding, or host service integration beyond the accepted gateway reachability. `--net-softnet-allow=0.0.0.0/0` is prohibited in M1A because current Softnet semantics also disable bridge isolation.

Private/link-local egress is denied by default. V4 implements only the exact
default policy and rejects every Softnet allow flag. ADR 015 permits a future
operator opt-in to exact private CIDRs, but implementation must first add the
allowlist to session creation/records/status and define its CLI semantics; V4
does not infer or accept it. Such future support may not authorize discovery,
broad LAN access, bridging, host networking, or weaker session isolation.

Under ADR 024, the tested Softnet 0.19.0 path requires host root privilege, so Softnet is a
privileged component of Boxwarden's trusted-host attack surface rather than an
ordinary unprivileged helper. For the accepted trusted-macOS-operator / untrusted
guest boundary, host-global `boxwarden init` runs once per trusted Mac and stages
the exact executable digest
`ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e`
into a digest-specific `/Library/Boxwarden` directory. The installed executable
is a regular one-link file owned by root and a dedicated trusted operator group,
mode `04550`; every ancestor is root-owned, non-writable by that group and
others, and not a symlink. A root-owned manifest is published only after the
tree is complete. This deliberately grants the exact single trusted operator
UID/name/home and dedicated group ID/name/membership the exact
qualified Softnet operation without repeated `sudo`; it is not a boundary
against hostile native code already executing as that operator. Such a threat
model would require a separately reviewed narrow wrapper.

That installed host-toolchain manifest is a regular one-link `root:wheel 0444`
file with no ACL, deliberately classified as non-secret local host metadata.
It may record only qualified platform release/build, exact tool
paths/versions/digests, root and dedicated operator-group identity, the trusted
operator UID/name/home, canonical `TART_HOME`, Softnet mode, and installation
time. It must not contain security-domain identity, CA material, credentials,
provider data, session state, private keys, tokens, or other secrets. Root
ownership, no write bits, exact metadata/content validation, and protected
root-owned non-writable ancestry provide integrity; readability provides none.
The unprivileged host-global doctor must read, hash, and strictly parse it so it
can diagnose both the tree and membership without a privileged helper. `0440`
would break that diagnostic boundary before group membership is effective. An
otherwise exact legacy `0400` manifest is drifted/unsafe: normal init and
doctor refuse it without mutation, while the one attended exact-path mode-only
migration validates the old complete tree first and then proves unchanged
inode/link/owner/group/bytes/digest/no-ACL plus `0444` and distinct-UID read.

Init refuses a Softnet source with any setuid/setgid bit. Any setuid or
passwordless-root Softnet under mutable Homebrew state is drifted/unsafe,
causes doctor to exit nonzero, and blocks both init and start until attended
manual inspection/remediation. Boxwarden never chmods, repairs, or adopts it;
only the root-owned digest-specific installed copy may be `04550`.

Normal start uses the absolute qualified Tart 2.32.1 executable digest
`05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d`
with PATH exactly the digest-specific Softnet directory, canonical recorded
`TART_HOME`, generation-private `TMPDIR`, and fixed validated locale/user
values. No ambient proxy, Sentry, Rust, DYLD, or other environment survives; no
sudo is used. It never repairs or re-authorizes privilege. Host-global
`boxwarden doctor` checks
the full canonical path, ancestor permissions and ACLs, symlinks, file type,
link count, executable/archive digests, root ownership, mode, group, manifest,
macOS state, and Tart/Softnet pairing. It reports the current Homebrew setuid
copy as blocking unsafe state rather than trusting or modifying it. Directory
service membership and the current process's supplementary group are checked
separately; init reports the required login-session refresh and doctor/start
fail until the group is effective. Upgrades
install adjacent digest-specific trees and never overwrite a qualified artifact
or retarget a `current` symlink; exact uninstall refuses active consumers.
Real-host install and qualification remain user-attended gates.

Both commands operate outside the security-domain namespace and do not require
or search for a domain. Domain CA creation is a separate explicit
`boxwarden --domain <domain> domain init` operation and is not a doctor health
check. Commands that operate on domain-owned state remain explicitly scoped and
never use cross-domain fallback.

Softnet constrains guest egress but permits incoming guest traffic; its default gateway allowance means M1A does not deny guest-to-host traffic at the vmnet gateway. A compromised guest can probe or attack host services reachable there. This accepted limitation is subordinate to the required ability to inherit the laptop's changing route and resolver environment and must remain visible in status and validation. Ubuntu enforces inbound deny by default except required host-to-guest SSH. A guest runtime can bypass ordinary `ufw` processing; when a golden includes Docker, services bind guest loopback by default and policy is enforced through Docker-compatible iptables/`DOCKER-USER` rules where needed. No workflow habitually publishes `0.0.0.0`.

Acceptance tests assert the security properties above and separately test the Tart argument mapping. A test that only searches for Tart flags is not sufficient evidence of the policy.

An observed running VM is not by itself a ready Boxwarden session. A persistent
same-user supervisor holds the generation lock and authenticated owner-only
socket, owns Tart/broker/Screen and generation key/certificate, renews the
no-extension certificate before a fixed threshold after revalidating immutable
CA metadata, and performs a strict read-only SSH probe on a fixed cadence.
Status requires a fresh authenticated bounded health snapshot within the fixed
maximum age and reads backend/host/guest-zone evidence without mutating it.
Stale/expired/authentication failure or host/guest-zone mismatch is non-ready;
idempotent start on a proven running generation may reconverge the zone. Missing
or unverifiable ownership is drift/non-ready with no adoption or mutation.
