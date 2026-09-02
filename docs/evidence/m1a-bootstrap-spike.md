# Boxwarden M1A Task 0 empirical qualification

task0_execution_state: PASS_WITH_CONDITIONS
task0_deferred_evidence:
  - ipv6_only_upstream
  - ipv4_only_destination
  - ipv6_only_destination

This document is the redacted, public-repository evidence record. Raw host names,
addresses, service inventory, machine identifiers, current time-zone identifier,
credentials, and temporary paths were written only to owner-private local state
and are summarized here only to the degree needed to support a security
conclusion. The host-reboot experiment demonstrated that macOS clears the chosen
`/private/tmp` Task-0 root across reboot; earlier temporary raw files were
therefore not retained after their privacy-safe conclusions had been committed.
The separately persisted logout/reboot recovery records remain owner-private and
durable. This is an evidence-retention limitation, not a behavioral test result.

Status vocabulary: `OBSERVED`, `VENDOR-DOCUMENTED`, `INFERRED`, and
`NOT YET PROVEN`.

`PASS_WITH_CONDITIONS` is a terminal Task 0 platform-selection result. Every
required core row is qualified. Only the exact keys under
`task0_deferred_evidence` may remain `NOT YET PROVEN`; those rows describe
environment-specific compatibility that was not available to test, not a
failed property. An unqualified environment is not supported or implied until
its row is promoted by new evidence.

## Starting prerequisites

- Repository baseline: `8f1efd72fde27e9be81060d5d3ffedcf593f19e0`
- Host platform: Apple Silicon (`arm64`), macOS 26.6.2 (build 25G83).
- VM prefix: `boxwarden-m1a-spike-`
- Pre-existing Tart objects: recorded locally before mutation; none are Task-0 cleanup targets.
- Minimum emergency free-space threshold: 12 GiB.

## Evidence matrix

| Evidence key | Status | Result and evidence reference |
|---|---|---|
| host_toolchain_versions | OBSERVED | Tart 2.32.1, Softnet 0.19.0, xorriso 1.5.8.pl02, socat 1.8.1.3, and the macOS system GNU Screen 4.00.03 were recorded locally. Tart and Softnet are Homebrew formulae from the Cirrus Labs tap; their mutable formula provenance remains a release-pinning concern. Socat and Screen implement the Task 0 PTY bridge and persistent terminal holder and are now part of the exact host-side serial mechanism being qualified. macOS Screen returns status 1 after both a valid `--version` report and a valid detached-session listing; the harness therefore captures and validates their output instead of treating status alone as failure. |
| softnet_host_privilege | OBSERVED | The supplemental work-VPN run found that the tested Softnet 0.19.0 invocation requires host root privilege. This is trusted-host privilege and attack surface, not ordinary unprivileged helper execution. The run's temporary setuid change was reverted, no production mechanism was selected or left installed, and this reconciliation makes no host privilege change. Setuid root is broader than desirable, and a standing passwordless-sudo rule must not authorize a user-writable mutable Homebrew executable. Any eventual mechanism must bind authorization to the exact qualified Softnet artifact and relevant execution dependencies so they cannot be replaced or mutated without privilege; an upgrade requires deliberate requalification and rebinding. See `docs/evidence/m1a-work-vpn-network-validation.md`, `docs/security-model.md`, and `docs/tool-provenance.md`. |
| iso_identity | OBSERVED | Canonical Ubuntu 24.04.4 Desktop ARM64 source ISO SHA-256: `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe`; Canonical signature verification succeeded locally. Initial minimal-DHCP candidate SHA-256: `0ebc198fade8bc85413f9674e5a609fe8ee97c27df431588cf1546cd249d2f9e`. Integrated minimal policy-checkpoint SHA-256: `45b1d2a7e9825f052511a14002e68c70779fdd3173d8aac513e48eb12cf0f0a2`. Pre-time-zone full-desktop integration checkpoint SHA-256: `3ac77a689a30cff60ada923955786d71ea66140df5772727bf2e0d0a34267af7`. Timezone-qualified full-desktop integration checkpoint SHA-256: `0b2dc5ba112f90689c81ff0b3a43b226f84ea096fe804bf2f0aa336753cc1757`. Final-seed formal run-1 candidate SHA-256: `e3a6bccb2f7bc1a446a58b0e42cbe07882b08d48a2fe14f9fdfe5634ef745d58`. Embedded seed and GRUB were extracted and verified after each remaster. |
| unattended_install | OBSERVED | Both minimal candidates, the timezone-qualified full Desktop integration checkpoint, and two immutable final-seed clean runs completed, rebooted, and suppressed Ubuntu's first-login wizard. Both clean runs used no synthetic boot key and reached installed-system acceptance without intervention; run 2 reused the exact run-1 ISO bytes. A run-1 host screenshot still showed GRUB two seconds after the guest's independently reported kernel-boot time, proving Tart can retain a stale EFI framebuffer while the VM advances. |
| install_reboot_detection | OBSERVED | In both clean runs, installer completion caused the requested reboot; Tart and the retained serial relay remained running, DHCP discovery succeeded, and the installed system reached an auto-logged-in desktop and pinned SSH. |
| gui_display_input | OBSERVED | Tart's isolated console rendered the automatically logged-in full Ubuntu desktop without a welcome prompt in both clean runs. Each final-seed run remained active, non-idle, and explicitly unlocked past the default five-minute threshold. The machine-wide no-idle, no-lock, and no-suspend values read back correctly and were non-writable to the user. Earlier console pointer input exposed the expected named-account password prompt when a provisional candidate still locked. |
| wayland | OBSERVED | `loginctl` reported the auto-logged-in graphical session type as `wayland` in the integration checkpoint and both final-seed clean runs, including after measured reboots. |
| xwayland | OBSERVED | In both final-seed clean runs, launching installed `xclock` into the active Wayland session started rootless XWayland on demand; both processes were observed and a targeted capture of only the Tart window proved the clock rendered visibly. |
| ssh | OBSERVED | Both final-seed clean runs reproduced short-lived CA-certificate login with an immediately pinned host key. Untrusted raw-key and direct-root attempts were rejected again in run 2; run 1 also rejected password/keyboard-interactive authentication and administratively prohibited a requested direct-TCP channel. Effective `sshd` configuration in both runs disables password, keyboard-interactive, direct-root, X11, agent, TCP, stream-local, gateway, and tunnel forwarding plus user environment. |
| guest_root_access | OBSERVED | The integrated policy checkpoint, full Desktop integration checkpoint, and both final-seed clean runs auto-logged the `boxwarden` account in as UID 1000; `sudo -n id -u` returned 0 over pinned SSH and the host-local serial console, and the sudoers fragment passed `visudo`. |
| guest_to_internet | OBSERVED | Both final-seed clean runs reproduced DNS resolution, public TCP/80, and HTTP retrieval of the Ubuntu ports Noble InRelease endpoint through default Softnet shared/NAT. On a separate real mobile-tether run with normal Wi-Fi disabled, clean run 2 also reached ordinary and explicitly IPv4-only public HTTPS destinations over the tether's observed IPv4-only path. |
| guest_to_host | OBSERVED | Guest TCP reached a temporary listener on the vmnet gateway. This is ADR 015's accepted Mac attack surface, not an isolation result. |
| guest_to_private | OBSERVED | A controlled temporary listener on the host's active RFC1918 address was host-reachable but guest-denied. |
| guest_to_link_local | OBSERVED | A controlled temporary listener on an existing host IPv4 link-local address was host-reachable but guest-denied. |
| session_to_session | OBSERVED | With two Softnet VMs running, the host reached both SSH services while candidate-to-control and control-to-candidate TCP/22 attempts were denied. |
| softnet_anti_spoofing | OBSERVED | In clean run 2, a normal public TCP/80 control succeeded before and after each bounded probe. With the configured MAC unchanged, traffic bound to an unleased RFC 5737 source IPv4 address timed out. A temporary macvlan then emitted frames using the guest's leased IPv4 address but a different locally administered source MAC; its TCP/80 connection also timed out. The namespace, macvlan, and extra address were removed, pinned SSH remained available, and the post-test control succeeded. |
| dhcp | OBSERVED | The live installer and installed system acquired IPv4 through vmnet DHCP without an activation error; the installed lease subsequently renewed. |
| dns | OBSERVED | Both final-seed clean runs inherited only the DHCP-advertised vmnet gateway resolver; no fixed public resolver remained. Name resolution and Ubuntu repository access succeeded. During the real mobile-tether run, clean run 2 again inherited only that gateway resolver and resolved ordinary A/AAAA plus family-specific test names. |
| vpn_custom_split_dns | OBSERVED | On a separate authorized work Mac, because the primary Boxwarden development Mac could not access the required VPN, the same qualified Tart 2.32.1 + Softnet 0.19.0 host-networking pair preserved public DNS and HTTPS, full-tunnel VPN DNS, and scoped/split DNS through the DHCP-advertised vmnet gateway. A fresh previously uncached internal-only name proved macOS applied per-domain resolver scoping rather than returning cached data. One running VM adapted from full-tunnel VPN to scoped/split DNS to VPN disconnected without restart or guest resolver reconfiguration. No static public resolver, guest VPN client, bridging, or prohibited host integration was required; Softnet continued to deny connections to resolved RFC1918 corporate services. The guest was an upstream Ubuntu 24.04 ARM64 Tart image using a different in-guest networking stack, not the final Task 0 golden, so this qualifies the tested host-side vmnet/DNS path rather than claiming that golden was exercised on the corporate VPN. See `docs/evidence/m1a-work-vpn-network-validation.md`. |
| ipv6_only_upstream | NOT YET PROVEN | The available real mobile tether was characterized rather than assumed: it gave macOS an IPv4 default path, no working native IPv6 path, and no visible DNS64 synthesis. The guest correspondingly received DHCPv4 plus only link-local IPv6. This useful IPv4-only-mobile result does not satisfy the required IPv6-only-upstream prerequisite. |
| ipv4_only_destination | NOT YET PROVEN | The host and clean-run-2 guest both reached an explicit IPv4-only destination on the observed IPv4-only mobile tether. The result does not qualify this row because it was not exercised through an IPv6-only upstream and its translation path. |
| ipv6_only_destination | NOT YET PROVEN | A true IPv6-only destination failed from both the host and clean-run-2 guest on the observed IPv4-only mobile tether. That upstream-consistent failure does not characterize Softnet under a working IPv6-only upstream, so the required row remains pending. |
| lease_renewal | OBSERVED | NetworkManager reported a 600-second lease. Its expiry advanced by exactly 300 seconds at renewal while the address remained stable. Address changes and failure recovery still require lifecycle testing. |
| management_address | OBSERVED | Default and explicit DHCP resolvers returned the running guest's address before and after renewal. The supplemental work-VPN run also reproduced `tart ip --resolver=dhcp` returning a stale prior lease after a stop, relaunch, and network-mode change, while the guest was actually using a different address. ARP unexpectedly returned the correct address in both experiments only after a matching bridge entry existed; despite that incidental result, Tart declares ARP unsupported with Softnet and it remains cache-dependent and unselected. Agent resolution failed because no guest agent is installed. Resolve immediately before management use and refresh on failure or lifecycle change; do not persist an address as identity. See `docs/evidence/m1a-work-vpn-network-validation.md`. |
| host_timezone | OBSERVED | The host detector resolves the current macOS zoneinfo-backed `/etc/localtime` to a valid IANA name recorded in local evidence, rejects out-of-tree links, and seed rendering requires that value. Subiquity staged it as first-boot cloud-init data; cloud-init reported no errors and the newline-normalized installed-guest `timedatectl` digest matched the host in the integration checkpoint and both final-seed clean runs. Future lifecycle start-time convergence is an accepted requirement rather than a Task 0 control-plane implementation. |
| installed_grub_timeout | OBSERVED | Both final-seed clean runs automatically installed the late-sorting fragment with one-second normal and record-failure timeouts, and each generated `grub.cfg` contained one-second branches for both cases. Boot-ID-qualified restarts observed a 13-second SSH outage in run 1 and an eight-second outage in run 2. This setting does not alter the installer ISO's GRUB timeout. |
| ssh_user_ca | OBSERVED | The host-only Ed25519 CA issued a short-lived certificate for the configured principal; the guest trusted only the public anchor. A certificate issued before the long install expired as designed, and a fresh certificate connected successfully. |
| host_key_bootstrap | OBSERVED | First-connection TOFU with immediate host-local pinning worked and the pinned fingerprint was retained locally. In the integration checkpoint and clean run 1, the installed guest's Ed25519 fingerprint read through the serial shell exactly matched the pinned host-side fingerprint. Both finalized-source clones automatically emitted bounded `BOXWARDEN_SSH_HOSTKEYS_V1_BEGIN`/`END` framing for ECDSA, Ed25519, and RSA before automatic serial login; every framed fingerprint exactly matched the trusted host's independently scanned key. The fingerprint comments retain the source's pre-finalization hostname because key generation precedes hostname replacement; this is cosmetic and does not affect the distinct keys, but should be reordered in a future golden revision. |
| serial_recovery_console | OBSERVED | The final Screen-held two-PTY relay uses a `0700` runtime directory, `0600` exact PTY devices, and `0600` session metadata. Real Tart installers and installed guests survived attach/detach, retained command output produced while detached, automatically logged UID-1000 `boxwarden` in on `hvc0`, and retained the console across installer and measured installed-system reboots in both clean runs. Both clone first boots retained complete delimited fingerprint framing in Screen scrollback followed by automatic login under their regenerated hostnames. Stopping the clones removed every harness, Tart, Softnet, socat, Screen, endpoint-link, and metadata runtime object while retaining only owner-private diagnostic logs. |
| graphical_lifetime | OBSERVED | The automatically logged-in Wayland desktop remained unlocked beyond the default idle-lock threshold and returned correctly across measured guest reboots while Tart remained running. Both final-seed clean runs explicitly remained active and unlocked beyond seven minutes before their measured reboots; a run-2 exact-window capture proved the desktop returned afterward. The foreground harness directly owned Tart, Softnet, socat, and Screen; launcher HUP stopped the VM and cleaned live runtime state. Same-user localhost SSH successfully launched Tart into the active Aqua session and produced a visible guest window. Abrupt SSH disconnect hung up the foreground harness, stopped the VM, and cleaned runtime state; Softnet briefly reparented during asynchronous shutdown but exited within five seconds. A direct Terminal.app launch produced the same visible desktop and process ownership; closing its invoking window terminated the foreground harness, stopped Tart, and removed all live relay state. A macOS screen lock/unlock retained the exact harness, Tart, Softnet, socat, and Screen processes plus the same guest boot identity and active graphical session. Console logout and host reboot each stopped the foreground stack; after normal login, a fresh explicit launch in each case restored the same pinned guest identity on a new boot with a visibly ready desktop. No survival or automatic restart across logout/reboot was observed. |
| two_clone_identity | OBSERVED | Two copy-on-write clones of the stopped clone-ready run-1 source regenerated distinct MAC addresses, DHCP client identifiers and leases, machine IDs, derived hostnames, random seeds, boot IDs, and all three SSH host-key families. Both reached graphical target and automatic serial login. The host reached each clone by SSH, while TCP/22 timed out in both clone-to-clone directions under their separate Softnet instances. Rehashing the stopped source's disk, configuration, and NVRAM after both clone boots exactly matched the pre-boot baseline. |
| latency_measurements | OBSERVED | The earlier minimal candidate installed in approximately 15 minutes. The selected full Desktop integration checkpoint took approximately 45 minutes. Final-seed clean runs 1 and 2 took approximately 42 minutes and 40 minutes 31 seconds respectively from Tart launch through the large update transaction and installed-system SSH readiness. Boot-ID-qualified warm restarts had observed SSH outages of 13 seconds in run 1 and eight seconds in run 2. Each copy-on-write clone completed in 0.09 seconds; first boot reached `graphical.target` in 37.066 and 37.091 seconds; clone stops completed in 0.32 and 0.36 seconds; and clone deletion completed in 0.89 and 1.19 seconds. |
| second_run | OBSERVED | A second clean 30-GiB VM reused the exact immutable final-seed ISO and independently reproduced unattended installation, automatic reboot, serial/SSH host-key agreement, certificate-only management, full Desktop/Wayland/XWayland, UID/sudo, cloud-init, time-zone, GRUB, no-idle/no-lock/no-suspend, DNS, and public-network acceptance. |

## Two-clone comparison

Only equality/difference conclusions belong here; raw identifiers remain local.

| Evidence key | Status | Comparison |
|---|---|---|
| clone_identity.mac | OBSERVED | Different; Tart assigned a fresh random MAC to each clone. |
| clone_identity.machine_id | OBSERVED | Different; each empty source machine ID was regenerated on first boot. |
| clone_identity.ssh_host_keys | OBSERVED | Different for ECDSA, Ed25519, and RSA; each clone's serial-framed fingerprints matched its trusted-host scan. |
| clone_identity.dhcp_duid | OBSERVED | Different active DHCP identity. The qualified IPv4 path advertised a type-01 client identifier derived from each distinct MAC rather than a DHCPv6 DUID. The portable NetworkManager connection-profile UUID remained intentionally identical and is not used as machine identity. |
| clone_identity.hostname | OBSERVED | Different; each hostname was derived from its regenerated machine ID. |
| clone_identity.random_seed | OBSERVED | Different after first boot. |
| clone_identity.management_address | OBSERVED | Different DHCP leases; host-side `tart ip --resolver=dhcp` resolved and SSH reached each address immediately before use. |
| clone_identity.source_unchanged | OBSERVED | Exact SHA-256 equality for the stopped source `disk.img`, `config.json`, and `nvram.bin` before and after both clone boots. |

## Clean-run reproducibility

| Evidence key | Status | Result |
|---|---|---|
| clean_run.1 | OBSERVED | The immutable final-seed candidate installed on a clean 30-GiB VM without a synthetic boot key, rebooted, and passed installed-system time-zone, GRUB, UID/sudo, serial, desktop, Wayland/XWayland, SSH, host-key pinning, and public-network acceptance. |
| clean_run.2 | OBSERVED | The exact immutable run-1 ISO installed on a second clean 30-GiB VM without a synthetic boot key, rebooted automatically, and reproduced the installed-system acceptance set. A measured subsequent reboot reached a new boot-ID-qualified SSH session in ten seconds, retained the owner-private serial relay and automatic shell, and returned to an unlocked Wayland desktop. |

## Diagnostic control: unrestricted vmnet gateway

The first completed installation was an intentionally non-qualifying control:
Softnet shared/NAT without `--net-softnet-block=@host`, a temporary fixed public
resolver, and the full `ubuntu-desktop` source. It completed unattended, rebooted,
and reached a running Ubuntu 24.04 ARM64 Wayland desktop with XWayland and
certificate-authenticated SSH. This isolates the earlier package-resolution
failures to the gateway/DNS policy interaction, but it does not qualify inherited
host/VPN DNS because the control ISO still contained the diagnostic resolvers.

The full-desktop control used approximately 11 GiB of its 27 GiB root filesystem
after installation. It contained 1,629 installed Debian packages, 76 package
records matching `libreoffice*`, and nine snaps. The ISO's own
`install-sources.yaml` describes `ubuntu-desktop-minimal` as "A minimal but usable
Ubuntu Desktop," makes it the default source, and describes `ubuntu-desktop` as
the full-featured source. A provisional candidate selected the official minimal
source to measure the difference. ADR 018 subsequently selected the full source;
the final clean runs must re-prove Wayland, XWayland, SSH, and required desktop
behavior under inherited DNS and the complete integrated policy.

Two adjacent `subiquity/Network/_send_update: CHANGE enp0s1` events during the
control were ordinary network-observer notifications. Subiquity emits a `CHANGE`
update when its netlink observer updates an interface after address or link-state
events; installation continued and completed. A continuously repeating stream
without installer progress would be investigated separately as a possible loop.

## Minimal-DHCP candidate checkpoint

The next candidate used the verified remaster digest recorded above and the
following redacted launch shape:

```text
tart run --disk=<verified-candidate.iso>:ro \
  --net-softnet \
  --no-audio \
  --no-clipboard \
  <task0-vm>
```

The launch contained no `block=@host`, allow-all, bridged, host-network,
filesystem, Docker, port-exposure, nested-virtualization, Rosetta, or other host
integration option. After the boot-menu selection, installation completed
without interaction, rebooted, and reached the installed desktop. The first
login displayed Ubuntu's welcome wizard. The session automatically logged in as
the UID-1000 `boxwarden` account, then GNOME's default five-minute idle policy
locked it. Tart console display and pointer input were visually confirmed when a
click exposed that account's password prompt. The candidate seed now suppresses
the welcome wizard, disables and locks out idle blanking, screen locking, and
automatic suspend, and grants the workstation account unrestricted passwordless
sudo. Those amendments must be verified by the next clean run.

The installed minimal candidate used approximately 9.5 GiB of its 27 GiB root
filesystem, with 1,481 Debian packages, no `libreoffice*` package records, and
eight snaps. It retained an active Wayland session, XWayland, SSH, DNS, and public
repository connectivity. Relative to the full-desktop control, it saved about
1.5 GiB, 148 Debian packages, 76 LibreOffice package records, and one snap.

## Integrated minimal policy checkpoint

The next remaster integrated inherited DHCP/VPN DNS, SSH policy, automatic
desktop login, welcome-wizard suppression, locked machine-wide no-idle/no-lock/
no-suspend settings, unrestricted passwordless sudo, and automatic `hvc0`
login. Its digest is recorded as the integrated minimal policy checkpoint above.
Because the ISO was created immediately before ADR 018 selected full Desktop, it
still contains the minimal source and is not either formal clean run.

After the single boot-menu selection, installation continued without interaction.
Its long archive/package phase made forward progress without another network
activation or package-resolution error, completed, rebooted automatically, and
reached an unlocked Wayland desktop without a first-login wizard. The guest
reported UID 1000, non-interactive UID-0 sudo, an active graphical session, all
locked no-idle settings, approximately 9.5 GiB used, 1,481 Debian packages, no
LibreOffice package records, and eight snaps.

The initial install launch used Tart's built-in `--serial` option. Guest-side
`hvc0` login was correct, but the resulting host PTY was not readable by an
unprivileged client. The stopped checkpoint was therefore restarted using a
private two-PTY relay and Tart `--serial-path`. macOS created both slave devices
mode `020`; explicitly changing those exact, user-owned devices to `0600` made
the operator endpoint usable. Plain relay behavior was one-shot because client
EOF stopped socat. Although `ignoreeof` initially survived two immediate client
connections, the later full-desktop checkpoint showed it did not survive
subsequent output after the operator PTY had hung up. The corrected harness owns
relay creation, a persistent detached Screen holder, permissions, Tart lifetime,
and cleanup for both installation and normal starts. Operators attach to Screen
and detach with `Ctrl-A d`; they do not open or terminate the raw operator PTY
directly.

## Desktop source decision

Wes accepted full `ubuntu-desktop` as the final M1A golden source after reviewing
the empirical comparison. Integrated LibreOffice and other productivity tooling
support guest-local viewing and visual verification of agent-generated documents.
The measured approximately 1.5 GiB / 148-package delta is smaller than the cost
of per-session downloads, VPN/network dependency, and maintaining a custom
minimal-plus-office composition. The integrated minimal run remains policy
evidence, but the two final clean reproducibility runs must use the amended full
Desktop seed.

## Pre-time-zone full-desktop integration checkpoint

This candidate was remastered after ADR 018 selected the full source and the
managed serial-launch mechanism was frozen. It reused the verified Canonical
source digest and has its digest recorded in the evidence matrix. Post-remaster
extraction proved that `/autoinstall.yaml` was byte-identical to its rendered
seed, parsed successfully as YAML, selected exact source ID `ubuntu-desktop`,
and contained no unresolved Boxwarden placeholders. The extracted GRUB
configuration placed `autoinstall` before the kernel command-line `---`
separator. Installation then completed without interaction, rebooted without a
network/package-resolution recurrence, suppressed the welcome wizard, and
reached the automatically logged-in full Ubuntu desktop.

During that installation Wes added the requirement that every guest use the
trusted host's current time zone, including after the laptop moves between time
zones. This checkpoint's already-frozen seed fixed `Etc/UTC`, so it cannot count
as formal clean run 1 despite validating the full-desktop and autologin path.
ADR 019 defines host-authoritative create/start convergence. The next remaster
must embed the detected host IANA zone and prove equality in the installed guest.

For the network boundary test, the trusted host temporarily listened on one
vmnet-gateway address, one active RFC1918 address, and one existing IPv4
link-local address. Host-local probes proved all listeners were live. The guest
reached only the vmnet-gateway listener. The private and link-local attempts were
denied. The listeners were then removed and verified absent.

For session isolation, two Task-0 Softnet guests ran concurrently. The trusted
host reached both SSH listeners, while TCP/22 was denied in both guest-to-guest
directions. The second guest was stopped after the test.

## Timezone-qualified full-desktop integration input

The candidate was rendered and remastered only after commit
`73d3280` added host-time-zone inheritance and the persistent Screen-owned
serial path. The harness resolved and validated the host's current IANA zone;
the exact value remains in local evidence. Seed rendering required that explicit
value and had no UTC or other fallback.

Post-remaster extraction proved `/autoinstall.yaml` byte-identical to the newly
rendered seed. The embedded document parsed as YAML, selected exact source ID
`ubuntu-desktop`, contained the validated host zone, and had no unresolved
Boxwarden placeholder. The extracted GRUB configuration placed `autoinstall`
before the kernel command-line `---` separator. Its candidate digest is recorded
in the evidence matrix.

The candidate was launched from a clean 30-GiB VM with the qualified host-tool
versions and launch policy. Its background Tart window appeared to remain at the
displayed ten-second GRUB countdown. With the operator away and having explicitly
authorized automation, the harness operator activated only that exact titled
Tart window and sent one Return key. The first refreshed frame only eight seconds
later was already well into the full Desktop copying-files phase. A cold boot of
the Ubuntu live environment could not plausibly reach that point in eight
seconds. The leading inference is therefore that GRUB timed out and installation
began normally while Tart retained a stale background GUI frame; activation
forced a repaint and the Return was unnecessary or harmless. Because activation
and Return were combined, this run cannot prove that inference conclusively and
does not count as a zero-interaction observation.

While this installation was running, the final portable seed gained an explicit
one-second installed-system GRUB policy. A late-sorting configuration fragment
sets both `GRUB_TIMEOUT=1` and `GRUB_RECORDFAIL_TIMEOUT=1`, then `update-grub`
materializes the result. The second value prevents an unclean prior shutdown
from silently restoring Ubuntu's longer record-failure delay. This already
immutable ISO does not contain the amendment, so it is now an integration
checkpoint rather than formal clean run 1. Its runtime evidence remains useful,
but the two reproducibility runs must use a newly rendered candidate.

The harness-owned Screen session presented the live installer's `hvc0` login
prompt. An attach followed by `Ctrl-A d` left the Screen session detached and
left the harness, Tart, socat, and Screen processes alive. This is the first
real-Tart reproduction of the corrected holder path. After logging into the live
installer console, the operator detached and used Screen's control interface to
send a harmless marker command while no client was attached. Reattachment showed
both the command and its returned marker, proving the retained session drained
and preserved real guest output across the detached interval. A second detach
again left all four processes alive.

The same serial shell exposed the installer's actual state behind the stale GUI
slideshow: Curtin had reported successful system installation and Subiquity had
entered final system configuration with state `UU_RUNNING`. The update process
remained CPU-active, advanced into `dpkg`, and completed without a package or
network error. Subiquity then recorded successful postinstall, `LATE_COMMANDS`,
and final state `DONE` before rebooting.

A pre-reboot comparison initially found UTC in `/target/etc/timezone` and
`/target/etc/localtime`. This was not a lost seed value: the installer's
normalized autoinstall data contained the validated host zone and logged a
successful TimeZone-controller application. Subiquity had rendered that zone
into `/target/etc/cloud/cloud.cfg.d/99-installer.cfg` under the `None`
datasource's first-boot `userdata_raw`. Canonical's Subiquity test path checks
the timezone at that same generated-cloud-config boundary. The correct runtime
acceptance point is therefore the installed guest after first-boot cloud-init,
not the still-unbooted extracted target.

After reboot, cloud-init's boot-finished marker and result document reported no
errors, and a newline-normalized digest comparison proved that `timedatectl`
matched the host-derived zone without publishing the zone. The installed serial
getty automatically logged in UID-1000 `boxwarden`; non-interactive sudo reached
UID 0. Tart displayed the unlocked, automatically logged-in Wayland desktop with
no welcome prompt. The full `ubuntu-desktop`, LibreOffice Writer, X11 apps, and
XWayland packages were installed; the system contained 1,629 Debian packages,
nine snaps, and used approximately 11 GiB. Launching `xclock` in the graphical
session started rootless XWayland on demand and rendered the clock visibly.

A fresh short-lived certificate connected over SSH after the serial-reported
Ed25519 host fingerprint exactly matched the host-side pin. Raw-key,
password-only, and direct-root attempts failed; agent forwarding was absent and
a direct-TCP channel was administratively denied. Effective sshd configuration
also disabled password, keyboard-interactive, direct-root, X11, stream-local,
gateway, TCP, agent, and tunnel forwarding. DNS resolution, public TCP/80, and
the Ubuntu ports Noble InRelease HTTP request succeeded through Softnet NAT.

Because this immutable ISO predated the requested GRUB amendment, the exact
two-line final fragment was applied to the installed checkpoint for a mechanism
test. `update-grub` sourced it and emitted one-second normal and record-failure
branches. A restart monitor ignored SSH sessions from the old kernel boot ID;
it observed SSH down six seconds after the scheduled reboot, accepted readiness
only with a new boot ID at 19 seconds, and measured a 13-second outage. The
Screen-held relay and automatic `hvc0` login survived repeated guest reboots.

Before Tart stopped, the serial runtime directory was mode `0700`, both exact
PTY devices were `0600`, and Screen metadata was `0600`. Stopping the VM made
the long-running harness exit successfully and removed the harness, Tart,
socat, Screen, Screen socket, both endpoint links, and session metadata. This
completes the integration checkpoint. It does not count as either clean run
because the final seed must install the GRUB fragment itself; clone fingerprint
framing also remains pending.

## Final-seed formal full-desktop run-1 input

After commit `608425c` added the installed-system GRUB policy and commit
`f9026d0` closed the preceding integration evidence, the seed was rendered again
with the current validated host zone and the same non-secret golden inputs. A
fresh remaster from the verified Canonical source produced the final-seed
formal-run-1 digest recorded in the evidence matrix.

Post-remaster extraction proved `/autoinstall.yaml` byte-identical to the newly
rendered seed and successfully parsed it as YAML. The embedded document selected
exact source ID `ubuntu-desktop`, contained the current host zone, included both
`GRUB_TIMEOUT=1` and `GRUB_RECORDFAIL_TIMEOUT=1` plus the required `update-grub`,
and retained no unresolved Boxwarden placeholder. The extracted installer GRUB
configuration still placed `autoinstall` before the kernel argument separator.

The immutable candidate was then launched on a clean 30-GiB VM without sending a
boot key. Tart's window appeared to remain at the installer GRUB menu beyond its
nominal 30-second timeout. The host-created screenshot timestamp and the guest's
independently reported kernel-boot time proved that the captured GRUB framebuffer
was stale: the guest kernel had booted two seconds before the screenshot. The
serial console subsequently presented the live-environment login, and Subiquity
applied the autoinstall data, configured networking and storage, completed apt
configuration, and entered Curtin system installation. Curtin extracted the
filesystem, installed the current HWE kernel and target EFI stack, and completed
target bootloader configuration. Subiquity's unattended-upgrade worker remained
CPU-active throughout its expensive full-Desktop dependency pass, then applied
the selected security update set successfully and entered late commands. A
non-fatal chroot logging warning did not prevent the package transaction from
recording `All upgrades installed`; the installed system reported no remaining
security-pocket upgrades.

The late commands automatically created the exact one-second normal and
record-failure GRUB fragment, regenerated `grub.cfg`, and completed the remaining
guest policies before the requested reboot. The installed `hvc0` getty
automatically logged in UID-1000 `boxwarden`, noninteractive sudo returned UID 0,
cloud-init reported no errors, and a normalized time-zone digest matched the
trusted host without publishing the zone. The system contained 1,629 installed
Debian packages and nine snaps, used approximately 11 GiB, and included the full
Desktop, LibreOffice Writer, X11 apps, and XWayland.

The auto-logged-in graphical session was active Wayland without a welcome
wizard, remained explicitly unlocked beyond seven minutes, and returned after a
measured reboot. Every configured no-idle/no-lock/no-suspend value was effective
and non-writable by the user. Launching `xclock` started rootless XWayland on
demand, and a targeted capture of only the Tart window proved the clock rendered
in the guest desktop.

The serial-reported Ed25519 host-key fingerprint exactly matched immediate
host-local pinning. A fresh short-lived user certificate connected; untrusted
raw-key, password/keyboard-interactive, direct-root, and direct-TCP-forwarding
attempts failed as required. Effective daemon configuration disabled all
prohibited authentication and forwarding modes. DNS, public TCP/80, and the
Ubuntu ARM64 ports InRelease request succeeded through Softnet shared/NAT.

Generated GRUB contained one-second normal and record-failure branches. A
boot-ID-qualified restart observed SSH down six seconds after scheduling,
accepted readiness only from a new kernel boot ID at 19 seconds, and measured a
13-second outage. The Wayland desktop and passwordless serial shell both returned
after reboot, and the Screen-held serial runtime retained its owner-only
permissions. This completes final-seed clean run 1; clone finalization and clean
run 2 remain separate experiments.

## Finalized-source two-clone qualification

After clean-run-1 acceptance, the explicit destructive finalization command was
run inside the guest and the machine powered off without another boot. The
finalizer cleared the source's machine ID, SSH host keys, DHCP leases, random
seed, cloud instance state, logs, histories, caches, and browser state; installed
the one-shot identity regeneration unit; and marked the source clone-ready. The
source then remained stopped for the complete experiment.

The harness created two Tart copy-on-write clones in 0.09 seconds each and
assigned each a random MAC before either boot. On first boot, both clones emitted
the complete three-key fingerprint block through retained serial Screen
scrollback, selected a different hostname from the newly generated machine ID,
automatically logged UID 1000 into `hvc0`, and reached `graphical.target` in
37.066 and 37.091 seconds. Trusted-host SSH scans exactly matched the serial
fingerprints. Hash comparisons proved that the clones also differed in machine
ID, hostname, MAC, random seed, boot ID, and every ECDSA, Ed25519, and RSA host
public key. Their active DHCPv4 client identifiers inherited their different
MACs and obtained different leases. The shared NetworkManager connection UUID is
portable configuration, not machine identity.

The trusted host connected to both clones with the short-lived certificate and
immediately pinned keys. In contrast, clone A to clone B and clone B to clone A
TCP/22 probes both timed out, empirically confirming session-to-session denial
under two independent Softnet instances. A second full hash of the stopped
source's disk, Tart configuration, and NVRAM exactly matched the baseline taken
before the clone boots, proving COW execution did not mutate the source.

The two VMs stopped in 0.32 and 0.36 seconds. Both long-running launch harnesses
then exited successfully; no Tart, Softnet, socat, Screen, Screen socket, serial
endpoint link, or session metadata remained. Only owner-private diagnostic logs
remained in the private runtime directories. Deleting the two stopped disposable
clones took 0.89 and 1.19 seconds. A final Tart inventory confirmed that both
clones were absent and the finalized run-1 source remained stopped.

## Clean formal run 2 and anti-spoofing qualification

Clean run 2 reused the exact immutable final-seed ISO from run 1 rather than a
newly rendered seed. The 30-GiB VM launched with Softnet shared/NAT, a read-only
installer image, audio disabled, and clipboard disabled. No synthetic boot key
or graphical input was sent. Subiquity completed its full-Desktop package and
update transaction, performed the requested automatic reboot, and reached the
installed SSH service in approximately 40 minutes 31 seconds.

The installed guest's Ed25519 host-key fingerprint reported through the retained
serial console exactly matched an immediate trusted-host scan. A newly issued
short-lived user certificate then passed strict host-key-pinned SSH. The guest
again reported UID 1000, noninteractive UID-0 sudo, a valid sudoers fragment,
cloud-init disabled by its expected post-first-boot marker with `DataSourceNone`
and no errors, 1,629 Debian packages, nine snaps, and approximately 11.1 GB in
use. The full Desktop, LibreOffice Writer, X11 apps, and XWayland were present.
The generated GRUB configuration contained one-second normal and record-failure
branches, and the normalized host and guest time-zone digests matched exactly.

The automatically logged-in graphical session was active, unlocked Wayland past
the default idle threshold. All configured no-idle, no-lock, and no-suspend
values were effective and non-writable. Starting `xclock` with no pre-existing
XWayland process caused rootless XWayland to start on demand; an exact-window
capture containing only Tart visibly showed the application. Effective SSH
policy again disabled every prohibited authentication and forwarding mode, raw
key and direct-root attempts failed, UFW was active with SSH allowed, the guest
inherited a single vmnet resolver, DNS succeeded, public TCP/80 succeeded, and
the Ubuntu Ports Noble InRelease request succeeded.

A boot-ID-qualified installed-system restart observed SSH go down two seconds
after scheduling and accept a new boot ID at ten seconds, for an eight-second
outage. A brief status probe raced graphical and serial unit activation; the
settled units were active, the Wayland session was unlocked, and a command sent
through the same retained Screen session returned the new boot ID from the
automatically logged-in `hvc0` shell. The serial runtime remained owner-private.

Finally, a bounded Softnet anti-spoofing experiment kept a successful public
TCP/80 control on each side of its mutations. On the configured interface and
MAC, a connection bound to a temporary unleased documentation-range source IPv4
address timed out. A temporary network namespace and macvlan then reused the
guest's leased address while emitting frames under a different locally
administered source MAC; that connection also timed out. Cleanup removed the
namespace, macvlan, and extra address, pinned SSH remained functional, and the
same public control succeeded afterward. This empirically supports enforcement
of both the configured source MAC and DHCP-learned source IP for the qualified
Tart/Softnet pair; it does not substitute for the still-pending VPN and
IPv6-only-upstream experiments.

## Real mobile-tether characterization

The available phone tether was tested with normal host Wi-Fi disabled. It
presented an IPv4 default path to macOS, no working native IPv6 path, and no
visible DNS64 synthesis. Ordinary A and AAAA queries still returned the public
records published by their respective names; an AAAA answer therefore did not
by itself prove IPv6 transport. Ordinary and explicitly IPv4-only HTTPS
destinations succeeded from the host, while a true IPv6-only destination and a
direct native-IPv6 control failed. An apparent `curl --ipv6` success to a
dual-stack name was rejected as evidence after its connection endpoints proved
to be IPv4-mapped IPv6 socket addresses carrying IPv4 traffic.

Clean run 2 then launched through the unchanged qualified Tart 2.32.1 and
Softnet 0.19.0 harness. The guest acquired DHCPv4, had only link-local IPv6 and
no IPv6 default route, and inherited only the vmnet gateway resolver. That
resolver successfully returned ordinary A and AAAA records plus IPv4-only and
IPv6-only test records, without synthesizing a DNS64 AAAA record for the
standard IPv4-only test name. The guest reached ordinary and explicitly
IPv4-only public HTTPS destinations over IPv4. Forced IPv6 and the true
IPv6-only destination failed immediately, consistent with the independently
observed host upstream rather than demonstrating a Softnet-specific failure.

This experiment adds real mobile-network evidence for DHCP, inherited DNS, and
IPv4 public reachability. It intentionally leaves `ipv6_only_upstream`,
`ipv4_only_destination`, and `ipv6_only_destination` as `NOT YET PROVEN`: the
first prerequisite was absent, and the two destination rows must be qualified
through that actual upstream condition rather than inferred from this one.

## Graphical launcher and SSH lifetime observations

During clean run 2, the foreground harness directly parented Tart, socat, and
the retained Screen process in one process group; Tart parented Softnet. The
Tart process, rather than an independent macOS service, owned the visible VM
window. Sending a hangup to the exact launcher harness simulated loss of its
invoking shell. The harness's exit cleanup stopped Tart and the VM, removed the
serial endpoint links and Screen metadata, and left only owner-private logs. No
Tart, Softnet, socat, or Screen process remained.

The stopped installed VM was then launched through pinned same-user localhost
SSH while that user had an active Aqua login. The SSH command supplied the
qualified Homebrew tool path but otherwise invoked the same foreground harness
and normal no-audio/no-clipboard Softnet launch. The observed process chain was
`sshd-session` to harness to Tart, Softnet, socat, and Screen. Tart successfully
created a visible Aqua window, and an exact-window capture showed the installed
Wayland desktop.

An abrupt OpenSSH client escape disconnected without issuing a Tart or guest
stop command. Loss of the remote pseudo-terminal hung up the foreground harness,
which again stopped the VM and removed live serial state instead of orphaning an
unmanaged running VM. Softnet was briefly visible reparented to PID 1 during
asynchronous teardown, but exited within five seconds; the final process and
Screen inventories were empty. Thus an active Aqua login permits same-user SSH
to create Tart's GUI, but the current foreground harness deliberately couples VM
lifetime to that SSH connection.

A direct Terminal.app launch next reproduced the ownership chain from the GUI
terminal itself: Terminal.app parented its login process and interactive shell;
the shell ran the foreground harness; the harness directly parented Tart, socat,
and the retained Screen holder; and Tart parented Softnet. The installed Ubuntu
desktop was visibly rendered and the relay remained owner-only. Closing that
specific Terminal window terminated its foreground job. The Tart window closed
at the same time, the VM became stopped, and the final inventory contained no
Tart, Softnet, socat, Screen process, or Screen socket. The runtime directory
retained only owner-private diagnostic logs, with both PTY links and Screen
session metadata removed. Direct Terminal launch is therefore supported, while
its VM lifetime is deliberately coupled to the invoking terminal just as it is
to an invoking SSH session.

The macOS screen-lock experiment deliberately left the host's existing
sleep-prevention utility active so that sleep could not be confused with screen
lock. Immediately before the operator locked the screen, the host recorded the
exact process identities for the harness, Tart, Softnet, socat, and Screen plus
a digest of the guest boot ID. The guest reported an active graphical target,
an active UID-1000 user session, and an unlocked graphical session. The operator
then locked macOS for approximately 20–30 seconds and unlocked normally.

After unlock, all five original host process IDs remained alive in the same
ownership chain, Tart still reported the VM running, and the same detached
serial Screen session remained available. The guest boot-ID digest was
unchanged, ruling out a guest restart, and its graphical target and user session
were still active and unlocked. A separate sandboxed `kill -0` sampling aid was
excluded from evidence because macOS process-signal permission denial made it
report every live process as absent; escalated `ps`, Tart, Screen, and guest
checks supplied the valid result. The harness subsequently stopped the VM and
again removed all live relay state. This qualifies macOS screen lock/unlock;
host reboot/login remains pending.

The console-logout experiment first recorded owner-private recovery state, the
clean pushed branch position, the exact five-process host chain, a digest of the
guest boot ID, the pinned SSH host key, and the active graphical-session state.
The operator then logged out of the active macOS Aqua session and logged in
normally. Logout was expected to terminate Codex as well as Tart, so no
continuation mechanism inside either process was treated as evidence.

Before any cleanup or relaunch after login, Tart reported the VM stopped. All
five exact pre-logout process IDs were absent, no replacement Tart, Softnet,
socat, or Screen process existed, and Screen had no retained socket. Both PTY
links and Screen session metadata were absent from the private serial runtime;
only owner-private diagnostic logs remained. Thus console logout terminated the
foreground VM stack and allowed the harness to complete its normal cleanup. It
did not leave an orphaned VM or stale relay, but the VM did not survive logout.

A fresh explicit launch after login reused the stopped guest and a new private
serial runtime. Its SSH host key exactly matched the existing trusted pin, while
its boot-ID digest differed from the pre-logout value as required for a new
boot. SSH and the automatic user session became reachable briefly before
`graphical.target`; a bounded readiness check subsequently observed the target
active, the UID-1000 graphical session active and unlocked, and passwordless
sudo still returning UID 0. The operator visually confirmed that the Tart
window rendered a ready Ubuntu desktop. The recovery run then stopped cleanly
and again left only owner-private logs. This qualifies manual post-login
relaunch, not background survival or automatic restart.

The host-reboot experiment separately armed a running guest with the clean
pushed branch position, a digest of the host boot time, the guest boot-ID digest,
the pinned host key, the active graphical-session state, and the five-process
host ownership chain in durable owner-private recovery state. The operator then
restarted macOS while the foreground stack remained running and logged in
normally. No process-local continuation was treated as evidence.

The first untouched post-login inspection observed a different host boot-time
digest and the clean pushed branch position. Tart reported the guest stopped,
and no Tart, Softnet, socat, or Screen process or Screen socket existed. Several
numeric pre-reboot PIDs had already been reused by unrelated applications;
their new command identities and the changed host boot identity demonstrate why
a bare PID is not a durable cross-reboot process identity. The complete
`/private/tmp` serial runtime was absent. Because macOS had cleared the whole
Task-0 temporary root, that absence cannot be attributed specifically to the
harness cleanup path.

A fresh explicit launch after host login used a new private serial runtime. The
guest SSH host key exactly matched the durable pre-reboot pin, its boot-ID digest
differed from the pre-reboot value, and bounded readiness checks observed the
UID-1000 graphical session active and unlocked with passwordless sudo returning
UID 0. The operator visually confirmed the ready Ubuntu desktop. Stopping this
recovery run removed every live Tart, Softnet, socat, and Screen process and
left only owner-private serial logs, which were copied to durable private state.
This completes the local graphical-lifetime cases: lock/unlock preserves the
running VM, while terminal/SSH launcher loss, console logout, and host reboot
stop it. Normal login plus explicit relaunch recovers; automatic restart and
supervision remain outside Task 0.

## Deferred environmental qualification

The only unqualified environments at Task 0 closure are the exact three keys
listed in `task0_deferred_evidence`: an effectively IPv6-only host upstream,
IPv4-only destination behavior while the host is using an IPv6-only/NAT64-style
upstream, and IPv6-only destination behavior under that representative
upstream. The real mobile tether available for Task 0 was IPv4-only, so it could
not exercise those cases. They are conditions on the scope of support, not
failures of the qualified core platform.

Future validation may promote any of these rows to `OBSERVED` for this exact
host-toolchain policy without reopening the basic M1A platform-selection
decision. Until then, Boxwarden must report the environments as unqualified and
must not imply native IPv6, NAT64, DNS64, or IPv4-over-IPv6 behavior.

## Gate classification

**PASS WITH CONDITIONS.**

The selected macOS, Tart 2.32.1, Softnet 0.19.0, and Ubuntu 24.04 ARM64 platform
is sufficiently qualified to begin M1A implementation. The conditions are the
three deferred environmental rows above, the accepted guest-to-vmnet-gateway
host exposure, Softnet's trusted-host root requirement and unresolved
integrity-bound privilege mechanism, default denial of private-network service
egress, and the foreground graphical-lifecycle constraints recorded in this
document. The first three conditions limit support claims; none is relabeled as
observed or treated as a hidden pass.
