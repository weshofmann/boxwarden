# Pinned Tart controlled clipboard viewer

The patch adds a host strip, per-window immutable target controller and focused
scene menu commands. It invokes the existing Boxwarden CLI once per action.
It does not read either clipboard or implement transfer transport. Automatic SPICE clipboard sharing is disabled for every launch of this
variant, including an unbound launch. Ordinary guest keyboard handling is
unchanged; metadata requires `--no-clipboard`. Auxiliary/non-key windows have no eligible menu target.

The exact strip says:

The compact strip shows one line: `Clipboard copy: DOMAIN/SESSION`, then
`GUEST -> HOST` and `HOST -> GUEST` buttons and a short status. The full target
identity and “Nothing transfers automatically” explanation are available as
tooltips. The strip never expands into explanatory rows. Buttons are per-window. Menu entries are
`Copy Host Clipboard to Guest` and `Copy Guest Clipboard to Host`, without key
equivalents. Each uses the same controller method and exact argument-array
invocation of `clipboard push` or `clipboard pull`. Only a successful child exit
produces success. Failures and cancellation report that the destination may
have changed; the viewer cannot assert remote rollback or safely retry.

## Launch metadata

Pass all six options or none; partial/invalid metadata fails argument validation:

```text
--boxwarden-clipboard-cli /absolute/path/to/boxwarden
--boxwarden-clipboard-config /absolute/path/to/config.json
--boxwarden-clipboard-domain personal
--boxwarden-clipboard-session sandbox-name
--boxwarden-clipboard-session-id UUID
--boxwarden-clipboard-generation UUID
```

The backend object is the actual `tart run` name and backend kind is fixed
`tart`. The child gets `--config`, `--domain`, `clipboard push|pull SESSION`,
`--expected-session-id`, `--expected-backend-kind`, `--expected-backend-object`
and `--expected-generation` as separate argv elements. No shell or payload argv
is used. The absolute executable is supplied by the trusted launching supervisor.
CLI revalidation supplies exact-generation readiness authority; local VM running
state and key-window state only decide whether a user can begin an action.

Only one operation per window can be admitted. Busy clicks are discarded.
Focus changes do not retarget or cancel an admitted transfer. Losing running
state or closing the window cancels its own child. Child stdin/stdout/stderr are
`/dev/null`; no diagnostics/payload are retained. Environment is closed to PATH
and UTF-8 locale. A serial worker owns spawn/signals/reaping, with a 30 second monotonic
time limit, SIGTERM and SIGKILL after one second. It never signals a reaped PID. Normal window/application shutdown, INT/TERM/HUP signals and upstream
stop/suspend/exit paths close admission, cancel all owned children and await
reaping before proceeding. Abrupt SIGKILL of the viewer cannot run this cleanup.

## Verification

Run on macOS with the Swift/Xcode toolchain:

```sh
host/tart-controlled-clipboard/test.sh
```

This typechecks the actual SwiftUI source against compile-only VM stand-ins and
runs the actual controller and subprocess implementation with synthetic targets.
Tests cover focus change during flight, exact binding/argv, busy rejection,
missing/stopped/closed targets, cancellation, metadata validation, dropped ambient
environment, discarded diagnostics and an uncooperative child being reaped.
No VM is instantiated and no host clipboard is read or written. These tests do
not constitute real-window menu focus or guest application acceptance evidence;
those remain an attended integration gate.

## Reproducible build and stage

Start from a fresh disposable checkout of the exact commit listed in NOTICE.md;
retain the exact upstream dependency lock. No installed Tart is modified:

```sh
host/tart-controlled-clipboard/build.sh /absolute/pinned-tart-source /absolute/new-stage
```

The script checks Git revision, license/lock SHA-256, requires a clean checkout,
applies the tracked patch, builds release with `--disable-automatic-resolution`,
and stages an ad-hoc signed executable. Signing identifier is
`org.boxwarden.tart.controlled-clipboard`; version is
`2.32.1-boxwarden-clipboard-r4`. It records compiler, signing, version, patch,
lock, requested/signed entitlement and signed executable identities. Signing
uses only `com.apple.security.virtualization`; the exact signed entitlement
dictionary is extracted and checked. No `get-task-allow` or restricted
`com.apple.vm.networking` entitlement is included. Boxwarden uses the existing
Softnet file-handle network attachment and does not need bridged networking.
The signed synthetic test validates signature metadata, not VM runtime support. Ad-hoc signing is reproducible local
identity, not developer notarization. OS/SDK/compiler differences may yield a
different executable digest; only the actual staged digest can be admitted.

A staged variant is distinct from the admitted upstream Tart executable. It
requires a separately reviewed exact host toolchain deployment/rollback gate.
Do not replace installed Tart, modify root admission state, relax validation or
launch a protected VM to test this patch. See the parent feature progress record
for actual staged build identities and remaining acceptance work.

### Signing provenance

Pinned upstream production entitlements contain `com.apple.security.virtualization`
and restricted `com.apple.vm.networking` (plist SHA-256
`4c822f249f396b4180697085b1247279116160e5fd690b8a768cdf5dcb7c258b`).
Its development plist adds `com.apple.security.get-task-allow` instead of
networking. Our ad-hoc variant uses the minimal virtualization entitlement;
it grants neither debugger attachment nor Apple-restricted vmnet management.
Apple documents the [Virtualization entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.virtualization)
and [restricted vmnet entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.vm.networking).
This change prepares signing; it does not claim a successfully booted guest.

### Staged archive format

`build.sh` writes a sibling `<stage>.tar.gz` and `.sha256` record without
replacing an existing package. The archive is a sorted USTAR stream compressed
with gzip mtime zero and no filename, uid/gid/mtime zero, blank owner/group names,
mode `0755` for the executable and `0644` for metadata. It contains an explicit
whitelist: signed executable, upstream license, requested/signed entitlements,
compiler/version/signing records and source/patch/lock/executable hash record.
No runtime state, logs or clipboard data are packaged. Identical staged bytes
yield identical archives independent of source file timestamps and modes.
Actual build/compiler/signature changes still produce a new identity.

Pinned SwiftPM parsing regression tests are carried in the patch under
`Tests/TartTests/ControlledClipboardRunTests.swift`. They use empty file-name
fixtures in a private temporary TART_HOME, never a bootable image or VM:

```sh
swift test --disable-automatic-resolution --filter ControlledClipboardRunTests
```

A separate signal regression harness invokes the same production signal
installer in a synthetic parent with a TERM-resistant owned child. It sends
actual SIGTERM, SIGINT and SIGHUP, and asserts the parent exits only after the
child is reaped. Abrupt SIGKILL, crashes and fatal signals cannot run asynchronous
cleanup and are not claimed as covered by this contract.
