# Boxwarden Clipboard menu utility

Build the directly launchable macOS app from this checkout:

```sh
host/clipboard-menu/test.sh
host/clipboard-menu/build.sh
open '.build/Boxwarden Clipboard.app'
```

The build stages a signed app containing the current `boxwarden` CLI at
`.build/Boxwarden Clipboard.app`. It uses the normal configuration path,
`~/Library/Application Support/boxwarden/config.json`. For another fixed
configuration file, launch the app executable with `--config /absolute/path`.
Rebuild the app after changing the CLI or menu sources. Building and opening the
utility do not start or stop a sandbox.

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
