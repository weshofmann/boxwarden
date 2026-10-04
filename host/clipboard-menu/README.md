# Native Boxwarden apps

The project manager reuses the existing CLI process boundary and native clipboard
controls. Build it with:

```sh
bash host/clipboard-menu/test.sh
bash host/clipboard-menu/build.sh --app project-manager
open .build/Boxwarden.app
```

Choose an initialized configuration in the window. See
[the private beta guide](../../tools/private-beta/TRY-ME.md#use-the-native-project-manager)
for create, import preview, stop/resume, export/recovery and system replacement.
The app uses versioned project JSON; the Go backend remains responsible for
admission, locks, lifecycle and recovery. Private command activity records allow
reopening without implicitly canceling or replaying a request. Recovered PIDs
are never signaled. Missing exit receipts are reported as unknown outcomes.
The app manages `alpha`; the CLI and legacy clipboard utility retain their
existing domain support.

The automated private-pasteboard entry point is
`"Boxwarden.app/Contents/MacOS/Boxwarden Projects" --config /absolute/config.json --private-pasteboard org.boxwarden.test.UNIQUE`.
That named board is visibly identified and never falls back to the general board.
Tests use synthetic child commands; actual GUI acceptance is separate.

## Legacy clipboard-only utility

Build the directly launchable macOS app from this checkout:

```sh
host/clipboard-menu/test.sh
host/clipboard-menu/build.sh
open '.build/Boxwarden Clipboard.app'
```

The build stages a signed app containing the current `boxwarden` CLI at
`.build/Boxwarden Clipboard.app`. It uses the normal configuration path,
`~/Library/Application Support/boxwarden/config.json`. For another fixed
configuration file, quit any running Clipboard Utility instance, then launch:

```sh
open -a '.build/Boxwarden Clipboard.app' --args --config '/absolute/path/config.json'
```

The app reads that path at launch; reopening an already running instance does
not change its configuration.
Rebuild the app after changing the CLI or menu sources. Building and opening the
utility do not start or stop a sandbox.
The VM backend remains on admitted stock Tart and disables Tart's automatic
clipboard sharing; the menu app supplies the explicit transfer surface.

The sandbox submenu lists sessions from every configured domain. Select one
explicitly before using a transfer action; a replaced session requires a new
selection. A failed domain lookup leaves other discovered domains available
and reports the partial failure. The utility
asks the bundled CLI for structured target information separately for each
explicit domain. Opening the menu and changing selection refresh readiness
metadata only. They do not read either clipboard. A transfer button captures
the selected domain, session UUID, backend object, and generation, then invokes
the existing `clipboard push` or `clipboard pull` command with an argument
vector. The CLI remains usable with the utility closed.

No transfer happens without a click. A completed command reports success;
failure or cancellation reports an unknown destination outcome because a write
may already have committed. The utility shows no clipboard payload and does
not retain transfer output. Quitting closes only the utility.

The automated tests use a synthetic child command and do not touch the Mac
general pasteboard or a VM. Real menu acceptance requires an agreed synthetic
clipboard test window and a READY guest.
