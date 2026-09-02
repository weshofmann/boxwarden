# ADR 017: Provide a host-local serial recovery shell

Status: Accepted; qualified by Task 0

Supersedes ADR 012 only where it restricted serial use to a bounded fingerprint
channel. ADR 012's SSH user-CA and host-key-pinning decisions remain in force.

## Context

SSH is the normal automation and administration path, but it depends on several
guest subsystems: network configuration, address discovery, firewall policy,
`sshd`, host keys, and the authorized user-CA configuration. A failure in any of
them can leave a running VM unreachable even though its operating system is
recoverable.

Tart can attach a virtio serial device when the VM starts. Its `--serial` mode
creates a host PTY and reports the path, while `--serial-path` connects an
externally created serial endpoint. Ubuntu 24.04 ARM64 exposes Tart's device as
`hvc0`. Unlike SSH or a published console server, the PTY is owned by the local
Tart process and is not reachable through guest networking.

Task 0 found that Tart's built-in PTY and newly allocated macOS PTY slaves were
mode `020`: the owner could write but an unprivileged console process could not
read them. A plain PTY-to-PTY socat relay became readable after setting the exact
slave devices to owner-only mode `0600`, but it exited when the first terminal
client closed. Adding `ignoreeof` to both relay endpoints appeared to preserve
the relay across two immediate console-client tests, but the longer full-desktop
installation exposed the missing condition: after the last raw-PTY client had
terminated, later guest output made socat fail an operator-side write with
`EIO` and exit while Tart and the VM continued running.

The guest getty was not involved in that failure. `serial-getty@hvc0` owns the
guest end; the failing endpoint was the macOS PTY used by host terminal clients.
A persistent detached GNU Screen process on that operator PTY keeps its slave
open, drains output, retains terminal state, and provides native attach/detach
semantics. A local PTY probe proved that output written before attachment and
while detached was retained across two client attachments without terminating
Screen or socat.

ADR 012 initially limited serial to delimited SSH-host-key output because it
treated a general login channel as unnecessary exposure. That does not match the
project's workstation ownership model: a trusted host operator can already use
Tart's graphical console, and the agent account intentionally owns the guest
with passwordless sudo. Requiring an unknown guest password precisely when SSH
is broken would make the recovery path ineffective.

## Decision

Every M1A VM starts with Tart serial hardware as well as the isolated graphical
console. The host creates a private mode-`0700` runtime directory and a two-PTY
relay before starting Tart. Both relay endpoints ignore client EOF; the
underlying PTY devices are restricted to mode `0600`. Tart receives one endpoint
through `--serial-path`. Before Tart starts, a harness-owned detached GNU Screen
process opens the other endpoint and remains its lifetime owner. The runtime
records the Screen session name; operators use `screen -r` and detach with
`Ctrl-A d` rather than terminating the persistent window. The control plane
never publishes the session, passes its PTY into a guest, or treats it as a
portable guest identifier.

The Task 0 harness implements the relay using socat 1.8.1.3 and the macOS system
GNU Screen 4.00.03. The production backend may own the same semantics directly,
but replacing either process or changing endpoint/lifetime behavior requires
the unattended-output, attach, detach, reconnect, permission, and cleanup tests
to pass for that implementation before promotion.

Ubuntu enables `serial-getty@hvc0.service` with automatic login as the explicit
UID-1000 `boxwarden` workstation account. The console does not log in directly
as root; `sudo -i` is available without a password under ADR 016. The getty is
ordered after clone first-boot identity initialization so delimited SSH host-key
fingerprints can be emitted before the interactive prompt without interleaving.

The serial device must be present at VM boot. It cannot be added after SSH has
already failed. Normal and installation launch paths therefore both use the
managed relay and `--serial-path`. Detaching a terminal client does not stop the
Screen holder, relay, Tart, or VM; a later client reconnects to the retained
Screen session. Tart exit stops Screen and the relay and removes session metadata
and both endpoint links so a stale handle cannot silently address a later VM.

## Consequences

Any process or person able to open the PTY can obtain root-equivalent control of
the disposable guest. That is acceptable only because the PTY exists on the
trusted host and control of the Tart process already confers equivalent power
through the graphical console and VM lifecycle operations. Host filesystem
permissions and Boxwarden runtime-directory ownership protect the handle.

The serial shell is an emergency and bootstrap path, not the routine command
transport. SSH retains structured authentication, bounded certificates, host-key
pinning, command execution, and better automation behavior.

Task 0 proved that Tart maps the channel to guest `hvc0`, automatic login reaches
UID 1000 without a password, and `sudo -n` succeeds. It disproved raw
operator-PTY reconnectability when `ignoreeof` failed to prevent a later
operator-side `EIO`. The Screen-held relay then reproduced retained output,
attach/detach, unattended output, reboot survival, owner-only permissions, and
cleanup in both final full runs. Both finalized-source clones retained complete
parseable first-boot fingerprint framing before automatic serial login.
