# Update a user-owned beta package beside the old package

This updates the package and helper asset locators for an existing private
configuration. It does not install Tart/Softnet, change host settings, reformat a
workspace, reimport a project, or silently install guest helpers. Use a new package
directory and retain the old archive, extracted package, formatter and guest
assets. Never prepare an update in the old package directory.

In a fresh terminal, select your **existing synthetic configuration** and the
new package extracted into a new private directory. The old configuration and
state root remain the same; do not run domain init or create another config.

```sh
umask 077
PACKAGE=/absolute/new-boxwarden-package
CONFIG=/absolute/existing-private/config.json
ISO=/absolute/ubuntu-24.04.4-desktop-arm64.iso
CHECKER=/absolute/e2fsck-static_1.47.0-2.4~exp1ubuntu4.1_arm64.deb
GO_BIN=/absolute/actual/go
ZSTD_BIN=/absolute/actual/zstd
cd "$PACKAGE"
shasum -a 256 -c SHA256SUMS
"$PACKAGE/bin/boxwarden" version
bw() { "$PACKAGE/bin/boxwarden" --config "$CONFIG" --domain alpha "$@"; }
bw project stop myproject
bash "$PACKAGE/prepare-projects.sh" --update "$CONFIG" "$ISO" "$CHECKER" "$GO_BIN" "$ZSTD_BIN"
bw project list
bw project status myproject
bw project open myproject
```

Repeat stop for any other project you are upgrading. The preparation runs the
existing managed formatter build in the **new package's** source checkout,
creates its own admitted formatter, and calls `project setup-update`. New source,
formatter, ISO and actual Go inputs must pass the same admission as initial
setup. The active profile remains Version 1. Historical formatter/workspace,
import and export receipts are retained; the update never rewrites them to assert
success. The previous setup document is archived byte-for-byte in the private
state root under `projects/.setup-history-<sha256>.json`, and its path is printed.
Then the active `projects/.setup.json` is atomically replaced. Future creation and
stopped export use the new admitted locators; project identity and data selection
remain unchanged. `project list` still discovers those names read-only after the
update; it validates retained setup history without treating it as a project.

For an already prepared new formatter, the equivalent public command is:

```sh
bw project setup-update --source-root "$PACKAGE/support/source" \
  --formatter-bundle "$PACKAGE/formatter" --iso "$ISO" --go "$GO_BIN"
```

After open, inspect your existing work in the guest. Export to a **new** private
host destination and inspect the returned files as data:

```sh
bw project stop myproject
bw project export --destination /absolute/new-returned-project myproject
```

## Explicit guest helper update

Setup-update does not change a running or stopped guest's installed helpers.
Existing clipboard support continues until you explicitly replace a helper or
replace the system disk. To prepare this package's pinned clipboard payload,
use a new private source directory:

```sh
CLIPBOARD_SOURCE=/absolute/new-private-clipboard-source
bash "$PACKAGE/prepare-guest-clipboard.sh" "$GO_BIN" "$CLIPBOARD_SOURCE"
```

Follow TRY-ME.md's **Controlled clipboard → An older prepared base** section:
open the project, use its printed workspace UUID with public `workspace import`,
check the imported checksums in the guest, and explicitly install the two guest
helpers there. These are changes to your selected guest, not host deployment.
Keep the old source and all import receipts. Ordinary project import is not used
for this helper payload, so the project's original export selection is retained.
Stop and reopen to establish a new exact management generation. Launch the new
menu with the explicit configuration; quit an older copy manually if needed:

```sh
open -n -a "$PACKAGE/Boxwarden Clipboard.app" --args --config "$CONFIG"
bw clipboard targets
```

The automated verification uses synthetic guest text and private named host
pasteboards. General Mac clipboard push/pull is an explicit operator action.
Automatic Tart clipboard/audio sharing stays disabled. Existing vmnet-gateway
exposure remains a documented network limitation.

## Failure and returning to an older package

If formatter preparation or new-asset admission fails, correct the reported
input and rerun the same `prepare-projects.sh --update` command. Existing prepared
new-package assets are retained for admission/retry; no partial formatter is
published as complete. Admission failure leaves the previous active setup in
place. Keep both packages and do not manually rewrite receipts or history.

A filesystem publication/sync error may occur after the new active profile is
visible. The error states this uncertainty. Rerun the **identical** update command
to finish publication/sync; identical inputs are idempotent. A corrupt or unsafe
history file is refused before switching active setup: inspect the reported file
and retain evidence rather than deleting it to bypass the check.

Side-by-side files make the old executable available, but do **not** provide a
transactional rollback of state or guest helpers. This increment preserves the
Version 1 setup/bookmark format and historical receipts, but returning to beta.2
after newer operations is **not guaranteed or qualified**. Use the new executable
to inspect/recover the existing state. Do not point an old executable at changed
state and assume compatibility, manually restore an archived setup, or delete the
new assets while its active profile references them. A later incompatible state
change requires its own explicit compatibility decision; this command is only a
bounded locator update, not a migration framework.
