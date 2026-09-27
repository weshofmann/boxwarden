# Controlled clipboard design

Scope: explicit host-authorized UTF-8 plain-text transfers, at most 1 MiB,
through window buttons, active-window menu commands and the four public CLI
commands. No automatic SPICE bridge, paste/keyboard injection, history, payload
logging/hashing, persistent action journal or host credential forwarding.

## Transport and identity

Reuse the existing retained supervisor control socket and pinned management SSH.
Add a narrow typed clipboard capability, not arbitrary execution or recipe actions.
The public service captures domain/session UUID/backend/generation before reading
input. Under existing transition/session locks it requires exact live READY,
then sends only that binding. A ready-to-receive supervisor handshake must
succeed before source capture; nonblocking frontend admission prevents a
connection backlog from retaining clipboard payloads. No queued delivery or generation retry. The runtime
owner rechecks the retained connection and binding before invoking the fixed
clipboard helper. A window gets immutable launch-time target metadata and the
exact Boxwarden command/config locator; focus only selects which window begins a
new menu operation. The viewer never handles clipboard bytes itself.

Control/SSH exchanges use bounded metadata plus a separately length-framed raw
payload. Existing non-clipboard control limits remain unchanged. Clipboard-specific exec
runner stdin/stdout bounds include the 1 MiB payload and small framing overhead;
never raise generic management or state-file limits. Limit bytes,
framing, output and time (30 seconds); reject trailing/invalid framing and sanitize
all errors. UTF-8 must be valid and NUL-free. Empty text is explicitly represented;
missing or unsupported text is an error. Text is never an argv element or hash.

## Desktop clipboard

The first synthetic probe uses the current GNOME workstation's user-manager
DISPLAY/session-bus environment with GTK3 on Xwayland, already present in the
Ubuntu desktop baseline. GTK clipboard operations target CLIPBOARD, not PRIMARY.
The helper admits exactly one active workstation graphical session/display,
captures its identity, and refuses missing/ambiguous or replaced desktop sessions.
Because Xwayland may start only on the first X11 connection, the helper makes
one bounded connection after validating the unique Wayland login and desktop
environment, then closes it before forking or handling text. The unchanged
controller/process/display proof must still pass before any clipboard read or
claim; waking Xwayland itself grants no clipboard authority.
It runs as the workstation user, with closed environment and bounded output. A push validates all input before claiming the selection, then retains
only a guest-local selection-owner process until replacement or desktop logout;
that process closes/detaches inherited SSH stdin/stdout/stderr after a private
readiness acknowledgement of the successful claim; it exits on selection loss
or desktop logout. It serves the one value and cannot read the host clipboard. This is
clipboard ownership lifetime, not a general RPC daemon. A pull requests an exact
UTF-8 text target and validates the complete response before host mutation.

The probe must prove later Firefox/application paste after command exit and
application copy in the other direction. If GTK/Xwayland cannot meet that test,
revise this small adapter before implementing the host UI; do not enable SPICE
or introduce a new transport. Existing installed generic helpers are unchanged;
new helper code is staged on a disposable synthetic guest only for acceptance.

## Destination and cancellation

Host pasteboard access uses a Darwin AppKit adapter, injected for automated tests.
Only push reads it and only pull writes it, after target admission. Copy uses stdin
without host pasteboard access; paste uses stdout without it. Paste refuses a TTY
unless --raw is explicit. Redirected output preserves exact bytes/newlines.

Pre-commit invalid input, unsupported content, cancellation or stale target leaves
the destination unchanged. For pull the commit point is successful AppKit write;
for push/copy it is the guest selection claim. A lost acknowledgement after a
possible guest claim is reported as outcome unknown, not rollback; never retry.
Concurrent operations use nonblocking target admission: busy targets fail before
clipboard capture, with no pending secret queue or focus retargeting. The
clipboard-specific control exchange propagates caller disconnect/cancellation
before dispatch. After a complete write request has reached the guest, failure
can mean outcome unknown; disconnection cannot honestly promise rollback.
The host item is fully prepared before the first pasteboard mutation (the
host commit point). AppKit does not supply a proven rollback transaction: a
write failure after mutation is outcome unknown, not destination-preserved.
No payload preview.

## Tart patch and admission

Patch only pinned Tart 2.32.1 commit 8aa377b71ebfd90b2df9803d3e20033f58d6800c.
Retain its Fair Source 0.9 notices and review distribution constraints. No newer
source is borrowed. Tart exec already transmits EOF but has an upstream EOF
readability-handler issue; it offers no advantage over existing pinned SSH.

The always-visible host strip identifies the sandbox and says:
“Copy text between the host and guest clipboards. Nothing transfers automatically.”
Buttons are HOST -> GUEST and GUEST -> HOST. Application menu items are Copy Host
Clipboard to Guest and Copy Guest Clipboard to Host; unavailable targets disabled.
No key equivalents replace normal guest shortcuts. Both call the same CLI with
expected immutable target metadata and show only success/failure/unknown status.

Track upstream source revision/archive hash, patch hash, reproducible build
instructions, signing details and executable digest as a distinct staged identity.
No installed Tart replacement, root manifest mutation or validation bypass. A
reviewed exact deployment/rollback gate follows safe implementation testing.

## Verification and exclusions

Test exact Unicode/whitespace/newlines/empty text, absent/unsupported representations,
malformed UTF-8/NUL, 1 MiB boundaries, hostile framing/timeouts, cancellation races,
restart/replacement/focus changes, concurrent operations and no subsequent transfer.
Use injected pasteboards; never read the user's unrelated real clipboard. Real host
clipboard tests require a seeded synthetic value in an agreed window. Preserve
prototype/workspaces/history/credentials. No storage/reconnect/network-policy work.
