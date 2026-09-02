# Boxwarden M1A Task 0 final qualification summary

## Final gate

**PASS WITH CONDITIONS**

Task 0 establishes that the selected Apple Silicon macOS, Tart, Softnet, and
Ubuntu 24.04 ARM64 platform is viable for M1A implementation. ADR 020 separates
this completed core platform decision from environment-specific compatibility.
The evidence matrix remains authoritative: no unperformed result is relabeled
as observed, and unqualified environments are not support claims.

## Qualified host and toolchain

- Apple Silicon (`arm64`) host running macOS 26.6.2 (build 25G83).
- Tart 2.32.1 and Softnet 0.19.0 from the Cirrus Labs Homebrew tap, treated as
  one security-critical host toolchain.
- xorriso 1.5.8.pl02 for installer remastering, socat 1.8.1.3 for the serial
  relay, and macOS GNU Screen 4.00.03 as the retained serial-session holder.
- Canonical Ubuntu 24.04.4 Desktop ARM64 source ISO with verified signature and
  SHA-256 `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe`.

Changing Tart, Softnet, the launch policy, or the privileged Softnet artifact
binding requires deliberate requalification. Softnet 0.19.0 requires host root
privilege. The production mechanism remains intentionally unselected; it must
authorize an exact qualified artifact and relevant execution dependencies that
an unprivileged user cannot replace or mutate. A mutable user-writable Homebrew
path must not inherit standing passwordless-root authorization.

## Qualified guest configuration

- Two clean 30-GiB installations reproduced the same immutable final seed.
- Canonical's full `ubuntu-desktop` source, including LibreOffice and the
  supported Wayland session, with rootless XWayland launched on demand.
- Explicit UID-1000 `boxwarden` workstation account with automatic graphical
  and `hvc0` serial login, unrestricted passwordless sudo, no welcome wizard,
  and machine-wide no-idle/no-lock/no-suspend policy.
- Key-only OpenSSH management with short-lived per-domain user certificates,
  immediate host-key pinning, and password, direct-root, X11, agent, TCP,
  stream-local, gateway, and tunnel forwarding disabled.
- Host-derived IANA time zone in the installed guest, one-second normal and
  record-failure GRUB timeouts, active inbound firewall policy, and no automatic
  golden mutation.
- Clone-ready source with regenerated machine identity, MAC, DHCP identity,
  hostname, random seed, boot ID, and all SSH host-key families.

## Core properties proven

- Genuine unattended Desktop installation, automatic reboot, installed-system
  readiness detection, and two clean-run reproducibility.
- Tart console display/input, unlocked Wayland desktop, visible XWayland client,
  full guest root ownership, and guest-local document/productivity tooling.
- Host-local Screen-held serial recovery across attach, detach, unattended
  output, installer reboot, and installed-system reboot, with owner-only runtime
  permissions and complete cleanup.
- Short-lived certificate SSH, serial-to-scan host-key agreement, distinct clone
  identity, stopped-source immutability, and bidirectional session isolation.
- Default Softnet anti-spoofing, private/link-local denial, public IPv4
  connectivity, DHCP and renewal, inherited gateway DNS, and host-to-guest SSH.
- Clone creation below one second, first graphical target in about 37 seconds,
  warm SSH restart outages of 8–13 seconds, and stop/destroy below two seconds
  in the measured runs.

## VPN and split-DNS qualification

On a separate authorized work Mac using the same Tart 2.32.1 + Softnet 0.19.0
pair, the host-side vmnet path preserved public DNS and HTTPS, full-tunnel VPN
DNS, and scoped/split-horizon DNS. A fresh previously uncached internal-only
name proved that macOS applied per-domain resolver scoping through the vmnet
gateway rather than returning a cached answer. One running VM followed full
tunnel, scoped/split DNS, and VPN-disconnected states without restart or guest
resolver reconfiguration.

That supplemental guest was an upstream Ubuntu 24.04 ARM64 Tart image using a
different in-guest network stack, not the final Task 0 golden. The qualified
property is primarily the tested host-side vmnet/DNS path. Private RFC1918
corporate service connectivity remained denied by the default Softnet policy.
That is a separate network-policy limitation, not a split-DNS failure.

## Accepted limitations and conditions

- The vmnet gateway remains guest-reachable because it supplies required DHCP
  and host/VPN-aware DNS. A compromised guest can probe host services reachable
  there; M1A does not claim guest-to-host network isolation.
- Softnet is a privileged trusted-host component. Task 0 did not choose or
  install the production root-authorization mechanism.
- Private/link-local destinations are denied by default. ADR 015 accepts future
  repeatable `--allow-private-network <CIDR>` session options, but no such
  exception was implemented or qualified in Task 0. Broad allow-all, bridging,
  implicit LAN access, and any exception that weakens session isolation remain
  prohibited.
- `tart ip --resolver=dhcp` can return a stale lease after lifecycle or network
  changes. Resolve immediately before management use, refresh after lifecycle
  changes and connection failure, and never persist an address as identity.
- The current graphical harness is foreground-owned. Screen lock preserves the
  running VM, but launcher/terminal loss, SSH disconnect, macOS console logout,
  and host reboot stop it. Normal login followed by explicit relaunch recovers
  the guest; automatic background survival was not qualified.

## Deferred environment-specific qualification

The following evidence remains `NOT YET PROVEN`:

- `ipv6_only_upstream`;
- `ipv4_only_destination` while the host uses an IPv6-only/NAT64-style
  upstream;
- `ipv6_only_destination` under that representative upstream.

The available real tether was IPv4-only, with no usable native IPv6 path and no
observed DNS64 synthesis. These are unqualified environments, not failures.
Boxwarden must not claim them as supported until empirical validation promotes
their evidence rows.

## Inputs to Task 1

- Preserve the host-neutral boundary: common code models security properties,
  persisted session policy, qualification status, and unsupported environments;
  it does not import Tart flags.
- Keep `PASS_WITH_CONDITIONS` and the environment matrix visible in validation
  and status rather than collapsing them into a generic platform pass.
- Reserve repeatable `--allow-private-network <CIDR>` session-creation options.
  Parsing alone grants nothing; Task 6 owns canonicalization, validation,
  persistence, and status disclosure.
- Preserve explicit domains, argv-only execution, bounded output, and no shell
  invocation. Do not begin from an assumption of IPv6-only compatibility.

## Inputs to Task 2

- Map the immutable common launch policy to the exact qualified Tart 2.32.1 +
  Softnet 0.19.0 shared/NAT invocation with audio and clipboard disabled by
  default and without prohibited host integrations.
- Treat Tart as a long-lived foreground graphical process, use only the process
  and lifecycle observations Task 0 proved, and never trust a reusable bare PID.
- Implement the integrity-bound Softnet root-execution mechanism before calling
  the backend production-ready; upgrades require explicit requalification and
  privilege rebinding.
- Model vmnet-gateway reachability as an accepted limitation, refresh DHCP-based
  management addresses, preserve the Screen-held serial path, and keep
  session-to-session isolation mandatory.
- Accept only exact private CIDRs supplied by validated persisted session policy;
  reject allow-all, implicit networks, vmnet/session overlap, and any mapping
  that disables bridge isolation. Test allowed and neighboring-denied targets.
- Report IPv6-only upstream behavior as unqualified until new empirical evidence
  promotes it; do not invent a resolver, translation mechanism, or support
  claim in the adapter.

## Evidence references

- `docs/evidence/m1a-bootstrap-spike.md`
- `docs/evidence/m1a-work-vpn-network-validation.md`
- `docs/decisions/015-network-compatibility-before-host-gateway-isolation.md`
- `docs/decisions/020-separate-platform-and-environment-qualification.md`
