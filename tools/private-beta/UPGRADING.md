# Update a user-owned beta package beside the old package

For beta.4 recipe-enabled setup, keep the new extracted package at its final
path and run its `prepare-projects.sh --update` with the existing five asset
arguments plus exact existing OpenSSL and xorriso executables. This admits and
records the preparation tools; it changes no guest. New projects can then select
`--recipe desktop|actions|chatgpt`. A stopped existing project can explicitly
`project rebuild --recipe chatgpt NAME` to provision software and support on a
new system while retaining its workspace. Later recipe-bound replacements use
captured software intent by default. Ordinary open never updates helpers or
reimports work. See [TRY-ME.md](TRY-ME.md) for the seven-argument setup and route.
After selecting the new package and existing configuration/asset variables
from that guide, use the recipe-enabled update:

```sh
bash "$PACKAGE/prepare-projects.sh" --update "$CONFIG" "$ISO" "$CHECKER" \
  "$GO_BIN" "$ZSTD_BIN" "$OPENSSL_BIN" "$XORRISO_BIN"
```

Recipe-enabled setup/bookmarks use strict version2 records. Older beta packages
may refuse those records; returning to an old binary is not a metadata rollback.
Retain the old package and its independent configuration for the old trial.
The legacy manual helper staging below applies to an older registered-base
workflow, not the recipe-created/rebuilt systems.

## Legacy registered-base update (five inputs)

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

Open the project and use its printed workspace UUID with public `workspace
import`. This keeps the project's original export selection:

```sh
bw project open myproject
bw project status myproject
VOLUME=printed-workspace-uuid
bw workspace import --source "$CLIPBOARD_SOURCE" "$VOLUME" myproject
```

In Ubuntu Terminal, enter the exact **`remote:`** directory printed by import.
Verify the imported files before explicitly installing the two guest helpers:

```sh
cd /home/boxwarden/workspaces/project/boxwarden-import-PRINTED-TRANSACTION-UUID
sha256sum -c SHA256SUMS
umask 077
gzip -dc boxwarden-guest-bootstrap.gz > boxwarden-guest-bootstrap
sha256sum -c BOOTSTRAP.sha256
sudo -n install -o root -g root -m 0755 boxwarden-guest-bootstrap /usr/local/libexec/boxwarden-guest-bootstrap
sudo -n install -o root -g root -m 0755 boxwarden-guest-clipboard.py /usr/local/libexec/boxwarden-guest-clipboard.py
```

Both checksums and installation commands must succeed. These change only the
selected guest's system disk. Keep the old source and all import receipts.
On the Mac, stop and reopen to establish a new exact management generation:

```sh
bw project stop myproject
bw project open myproject
bw project status myproject
```

Wait for management READY before testing clipboard transfer. Launch the new
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
transactional rollback of state or guest helpers. The legacy five-input route preserves the
Version 1 setup/bookmark format and historical receipts, but returning to beta.2
after newer operations is **not guaranteed or qualified**. Use the new executable
to inspect/recover the existing state. Do not point an old executable at changed
state and assume compatibility, manually restore an archived setup, or delete the
new assets while its active profile references them. A later incompatible state
change requires its own explicit compatibility decision; this command is only a
bounded locator update, not a migration framework.
