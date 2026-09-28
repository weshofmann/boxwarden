# Controlled clipboard design

Scope: explicit host-authorized UTF-8 plain-text transfers, at most 1 MiB,
through a standalone macOS menu-bar utility and the four public CLI
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
clipboard helper. The menu captures the selected target's exact tuple at click
time and uses the bundled CLI and fixed configuration locator. A selection
change does not redirect a transfer. The utility never handles clipboard bytes.

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

## Standalone macOS menu

The app is a menu-bar accessory with a sandbox submenu, `HOST -> GUEST` and
`GUEST -> HOST`, the explicit no-automatic-transfer reminder, a short status,
and Quit. It has no VM lifecycle controls, shortcuts, history, or login item.
The build stages a directly launchable `.app` with the current CLI bundled.
Quit does not operate on a VM; the CLI works independently of the app.

The app reads configured domain names from the explicit Boxwarden config JSON.
For each name, it invokes `--domain <name> clipboard targets` using a direct
argument vector and a closed environment. The read-only command returns
validated session metadata and marks a target available only after backend
observation and fresh exact-generation supervisor readiness. No clipboard data
is read during discovery or selection. Metadata is a UI hint, not transfer
authority: the existing service re-admits exact READY under its locks before
source capture. Unsupported, stopped, drifted, stale, and unresolved targets
cannot enable transfer buttons. Discovery and transfers run off the menu thread.

The menu starts with no selected sandbox and requires an explicit choice. It
preserves a choice across refresh only when the domain, session name, session
UUID, backend identity, and generation still match; replacement or restart
clears it. One domain's discovery failure leaves successfully discovered
domains visible and reports a partial-status warning.
The menu always identifies the selected domain/session. A click captures the
session UUID, backend kind/object, and generation and invokes the existing
CLI push or pull with expected-binding flags. A changed selection or
replacement generation cannot retarget that invocation. A failed or cancelled
transfer reports an unknown outcome rather than a rollback guarantee. The app
discards child output and diagnostics, never logs or previews payloads.

## Verification and exclusions

Test exact Unicode/whitespace/newlines/empty text, absent/unsupported representations,
malformed UTF-8/NUL, 1 MiB boundaries, hostile framing/timeouts, cancellation races,
restart/replacement/focus changes, concurrent operations and no subsequent transfer.
Use injected pasteboards; never read the user's unrelated real clipboard. Real host
clipboard tests require a seeded synthetic value in an agreed window. Preserve
prototype/workspaces/history/credentials. No storage/reconnect/network-policy work.
