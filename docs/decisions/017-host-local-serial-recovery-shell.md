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

**V4-only normative design — not current implementation or operational
behavior.** The following supervisor, broker, managed-launch, readiness, and
cleanup requirements specify the intended V4 lifecycle. They do not claim a
current `session start`, READY, stop, or destroy command. Task 0's separately
qualified foreground socat/Screen harness evidence remains historical evidence
for the facts explicitly recorded below; it is not V4 broker qualification.

Every V4 M1A VM will start with Tart serial hardware as well as the isolated
graphical console. The host will create a private mode-`0700` runtime directory
and a two-PTY relay before starting Tart. Both relay endpoints will ignore client EOF; the
underlying PTY devices are restricted to mode `0600`. Tart receives one endpoint
through `--serial-path`. Before Tart starts in that future lifecycle, a supervisor-owned detached GNU
Screen process will open the other endpoint and remain its lifetime owner. The
runtime will record the Screen session name; operators will use `screen -r` and
detach with `Ctrl-A d` rather than terminating the persistent window. The
control plane will never publish the session, pass its PTY into a guest, or
treat it as a
portable guest identifier.

The Task 0 harness implements the relay using socat 1.8.1.3 and macOS system
GNU Screen 4.00.03. The V4 design preserves the two-PTY topology but replaces
opaque socat forwarding with one supervisor-owned broker; it requires ADR 017
implementation and requalification. The future supervisor will create and
retain both PTY pairs and both masters. Tart will open only the Tart slave.
Exact `/usr/bin/screen -D -m`
4.00.03 (FAU, 23-Oct-06), executable SHA-256
`07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add`,
root:wheel `0755`, one link, will be a direct waitable supervisor child and
remain the operator slave's sole reader on qualified macOS 26.6.2 build 25G83.

The future broker will be the sole forwarding reader. Tart-master output will
enter a bounded Screen-output queue and, only in automation mode, a fixed-memory
raw frame parser. Operator-master input will forward only in console mode; every other mode,
including `idle` and `automation`, will discard and count it without buffering
or replay. Automation will never open the operator PTY or use
Screen log, hardcopy, `stuff`, paste, or control as its data path. A serialized
future broker state machine will own `idle`, `console`, `automation`, and
`failed`. Queues, lines, frames, decoded results, total bytes, and deadlines
will be fixed and bounded. Guest flood, queue overflow, Screen or broker loss,
and ambiguous exchange will poison readiness for the generation; there will be
no hot repair.

Ubuntu enables `serial-getty@hvc0.service` with automatic login as the explicit
UID-1000 `boxwarden` workstation account. The console does not log in directly
as root; `sudo -i` is available without a password under ADR 016. The getty is
ordered after clone first-boot identity initialization so delimited SSH host-key
fingerprints can be emitted before the interactive prompt without interleaving.

The serial device must be present at VM boot. It cannot be added after SSH has
already failed. Future V4 normal and installation launch paths will therefore
both use the managed relay and `--serial-path`. Detaching a terminal client will
not stop the Screen holder, relay, Tart, or VM; a later client will reconnect to
the retained Screen session. Tart exit will stop Screen and the relay and remove
session metadata and both endpoint links so a stale handle cannot silently
address a later VM.

## Consequences

Any process or person able to open the PTY can obtain root-equivalent control of
the disposable guest. That is acceptable only because the PTY exists on the
trusted host and control of the Tart process already confers equivalent power
through the graphical console and VM lifecycle operations. Host filesystem
permissions and Boxwarden runtime-directory ownership protect the handle.

The serial shell is an emergency and bootstrap path, not the routine command
transport. SSH retains structured authentication, bounded certificates, host-key
pinning, command execution, and better automation behavior.

ADR 012's amended post-clone trust establishment specifies this accepted
channel. In the future V4 lifecycle, automation will enter the broker's
exclusive automation state and send the exact
static command `/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap`, followed
separately by canonical bounded JSON. Fresh nonce and start generation will
correlate the current frames and echoed response, but only durable
domain/session/backend, CA fingerprint, and derived principal will be
installed. Later generations will verify the same durable binding and current
host key. Missing, interleaved, malformed, oversized, stale, mismatched, or
ambiguous frames will poison the generation. This contract adds neither a
network bootstrap path nor a private CA key in the guest.

The future long-lived same-user supervisor will own the generation lock,
persistent owner-only authenticated control socket, broker, PTYs, Screen, and
Tart as direct children. It will not `exec`-replace itself with Tart or depend
on the initiating CLI context. Nonce challenge/response plus
manifest/process-start evidence will support reconnect after CLI crash. Its
ownership manifest will include broker generation/health, both PTY device
identities, Screen direct-child/start/socket evidence, overflow/poison state,
and lease mode. Cleanup will follow an explicit child-exit protocol and act only
on fully proven ownership.

Task 0 proved that Tart maps the channel to guest `hvc0`, automatic login reaches
UID 1000 without a password, and `sudo -n` succeeds. It disproved raw
operator-PTY reconnectability when `ignoreeof` failed to prevent a later
operator-side `EIO`. The Screen-held relay then reproduced retained output,
attach/detach, unattended output, reboot survival, owner-only permissions, and
cleanup in both final full runs. Both finalized-source clones retained complete
parseable first-boot fingerprint framing before automatic serial login.
