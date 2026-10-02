# Boxwarden 0.2 private beta — Apple Silicon Mac

This archive targets an **already initialized** Mac with admitted Tart 2.32.1 /
Softnet 0.19.0, private enrolled APFS storage and a stopped, generic prepared
Ubuntu 24.04.4 Desktop ARM64 base. It does not install or upgrade host tools.
One-time helper preparation/export also requires actual Go 1.27.0, Xcode
Command Line Tools, system Python 3, an existing `zstd` executable, the pinned Ubuntu Desktop ARM64 ISO and
`e2fsck-static_1.47.0-2.4~exp1ubuntu4.1_arm64.deb`. These are external assets.

Keep the extracted directory at its final absolute location. It contains the
CLI, directly launchable clipboard app and a self-contained source checkout
for existing managed helper admission. No development worktree is required.
`BUILD.json` records revision/version; `SHA256SUMS` covers the shipped files.
Signatures are **ad-hoc, not Developer ID/notarized**. This local beta does not
claim download/Gatekeeper distribution acceptance; it does not bypass OS checks.

## Verify and configure once

Verify the archive beside its checksum file with
`shasum -a 256 -c boxwarden-0.2.0-beta.3-darwin-arm64.tar.gz.sha256`, then extract
in a new private user-owned directory. In a **new terminal**, set these
paths to your actual assets; no profile sourcing or private bindings is needed:

```sh
umask 077
PACKAGE=/absolute/boxwarden-0.2.0-beta.3-darwin-arm64
cd "$PACKAGE"
shasum -a 256 -c SHA256SUMS
"$PACKAGE/bin/boxwarden" version
"$PACKAGE/bin/boxwarden" help

ENROLLED_CONFIG=/absolute/existing/enrolled/config.json
CONFIG="$HOME/Library/Application Support/boxwarden-beta/config.json"
STATE=/absolute/enrolled-apfs-volume/private-beta-state
BASE=existing-stopped-prepared-base-name
ISO=/absolute/ubuntu-24.04.4-desktop-arm64.iso
CHECKER=/absolute/e2fsck-static_1.47.0-2.4~exp1ubuntu4.1_arm64.deb
GO_BIN=/absolute/actual/go
ZSTD_BIN=/absolute/actual/zstd
mkdir -m 700 "$STATE"
mkdir -p -m 700 "$(dirname "$CONFIG")"
python3 - "$ENROLLED_CONFIG" "$CONFIG" "$STATE" <<'PY'
import json, os, sys
with open(sys.argv[1]) as source:
    config = json.load(source)
alpha = dict(config["domains"]["alpha"])
alpha["state_root"] = sys.argv[3]
config["domains"] = {"alpha": alpha}
fd = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, "w") as destination:
    json.dump(config, destination, indent=2)
    destination.write("\n")
PY
bw() { "$PACKAGE/bin/boxwarden" --config "$CONFIG" --domain alpha "$@"; }
"$PACKAGE/bin/boxwarden" --config "$CONFIG" doctor
bw domain init
bw golden register "$BASE"
bash "$PACKAGE/prepare-projects.sh" "$CONFIG" "$ISO" "$CHECKER" "$GO_BIN" "$ZSTD_BIN"
```

The new config preserves the existing toolchain/storage enrollment and creates
an independent state root. Golden registration observes the stopped base; it
does not alter it. Preparation creates a private local formatter bound to the
new state root. Run it once, before creating projects. Identical setup retries
are safe; different saved asset locators are refused. Keep this package and its
formatter in place. Do not reuse an older demo's state root or change its setup.

For an explicit side-by-side update of an existing private configuration, see
[UPGRADING.md](UPGRADING.md). Retain the old package and assets; preparation with
`--update` switches admitted asset locators and preserves previous setup bytes.

## Create, import, edit and resume

```sh
SOURCE=/absolute/new-private-source
mkdir -m 700 "$SOURCE"
printf 'Original host project.\n' > "$SOURCE/notes.txt"
printf 'unchanged-reference-v1\n' > "$SOURCE/reference.txt"
bw project create --size-mib 64 myproject
bw project list
bw project status myproject
bw project import --source "$SOURCE" myproject
```

`bw project list` discovers existing named projects in name order, with their
current state, exact workspace association, and next commands. It is a read-only
snapshot: unavailable backing storage is an error, incomplete projects stay
visible, and a running backend is READY only with a fresh exact supervisor check.
The commands shown recheck their bindings before doing work.

Create opens the native desktop. Wait for `readiness: ready` before importing.
If a fresh observation expires, run `bw project open myproject` to re-establish
readiness, then `bw project import retry myproject` for a recorded import. The
retry keeps its original selection and transaction; do not create another import.
Open Ubuntu Terminal through Show Apps. Enter the printed guest files directory
under `/home/boxwarden/workspaces/project`; type the prefix
`cd /home/boxwarden/workspaces/project/boxwarden-import-` and press Tab to complete
the single import directory. Use an editor to change `notes.txt` to
`guest-edited-v1` plus a newline and create `new.txt` containing
`guest-created-v1` plus a newline. Leave `reference.txt` unchanged.

```sh
bw project stop myproject
bw project open myproject
bw project status myproject
```

Check the three files again in the guest. Open resumes the same sandbox and
workspace; repeated open does not clone or reimport. From another new terminal,
set `PACKAGE` and `CONFIG` and define `bw` as above; those are the only routine
setup values needed. For a second independent project, use another name and
another private source directory with the same commands.

## Stop and return the edited files

```sh
bw project stop myproject
RETURNED=/absolute/new-returned-directory
bw project export --destination "$RETURNED" myproject
```

Export prints **`project files:`** with the actual returned directory.
Inspect its `notes.txt`, `new.txt` and unchanged `reference.txt`; compare against
the intended edited bytes. The source should still contain its original two
files. Export refuses a running sandbox or an existing destination. Returned
guest files are data: inspect them before deliberately running anything.
Transfers are explicit, bounded to 256 files / 16 MiB, not synchronization.

## Controlled clipboard

### An older prepared base

Older prepared bases may have working management SSH without the current guest
clipboard adapter. On the already initialized Mac, prepare the two generic guest
helpers from this package's source, then explicitly import them into a READY
synthetic project. This changes that project's system disk; it does not change
the registered base or any host component. Preparation requires the actual
Go 1.27.0 executable and creates a new private source directory. The bootstrap
is gzip-compressed to fit the existing 4 MiB per-file import limit; its
uncompressed checksum is retained separately:

```sh
CLIPBOARD_SOURCE=/absolute/new-private-clipboard-source
bash "$PACKAGE/prepare-guest-clipboard.sh" "$GO_BIN" "$CLIPBOARD_SOURCE"
bw project open myproject
bw project status myproject
# Set VOLUME to the workspace UUID printed by project status.
VOLUME=printed-workspace-uuid
bw workspace import --source "$CLIPBOARD_SOURCE" "$VOLUME" myproject
```

Use the exact **`remote:`** directory printed by workspace import. In Ubuntu
Terminal, enter that path and verify the imported files before installing them:

```sh
cd /home/boxwarden/workspaces/project/boxwarden-import-PRINTED-TRANSACTION-UUID
sha256sum -c SHA256SUMS
umask 077
gzip -dc boxwarden-guest-bootstrap.gz > boxwarden-guest-bootstrap
sha256sum -c BOOTSTRAP.sha256
sudo -n install -o root -g root -m 0755 boxwarden-guest-bootstrap /usr/local/libexec/boxwarden-guest-bootstrap
sudo -n install -o root -g root -m 0755 boxwarden-guest-clipboard.py /usr/local/libexec/boxwarden-guest-clipboard.py
```

Both checksums and installation commands must succeed. Then, in the Mac terminal,
run `bw project stop myproject`, `bw project open myproject`, and
`bw project status myproject`. Wait for READY before the clipboard test below.
The restart lets the current helper bind clipboard operations to the new
management generation. The installed helpers persist across ordinary stop/open;
a different clone from the older base needs this preparation separately.

### Discover and transfer

```sh
open -a "$PACKAGE/Boxwarden Clipboard.app" --args --config "$CONFIG"
bw project open myproject
bw clipboard targets
```

Quit an already running Clipboard utility before launching with another config.
Its menu discovers sessions from the config; select the READY sandbox explicitly.
Opening the menu and choosing a target do not read either clipboard. **Push and
Pull use the general Mac clipboard only after an explicit operator click.**
To test synthetic guest clipboard text without accessing the Mac clipboard:

```sh
printf 'synthetic beta clipboard\n' | bw clipboard copy myproject
bw clipboard paste myproject > /absolute/new-private-readback.txt
cat /absolute/new-private-readback.txt
bw project stop myproject
```

## Persistence and limits

Ordinary stop/open retains the same system disk and independent ext4 workspace.
Imported files and guest edits persist; processes, desktop windows and management
generation do not. Keep important work on the workspace. Stop may fall back to
Tart with `forced=false` and **workspace cleanliness unverified**: that is not
proof of a clean filesystem. Export independently checks clean ext4 state and
absence of recovery requirements; it refuses unsafe state without repair.
Leave successful demos stopped for inspection.

This is an experimental functional beta, not new security qualification.
Existing vmnet-gateway exposure, shutdown uncertainty, transfer limits and
untested Mac/guest environments remain. Automatic Tart clipboard/audio sharing
stays disabled. Provider sign-in, cold-machine installation, power-loss
recovery and complete network isolation are outside this walkthrough.

To rebuild from a clean committed checkout on this Mac:
`GO_BIN=/absolute/actual/go bash tools/private-beta/build.sh 0.2.0-beta.3 /absolute/new-output`.
This builds locally and publishes no release.

## Replace a disposable project system

With a stopped named project, `bw project rebuild --base current NAME` clones
the supported prepared base even if the base name is unchanged, retains the
same workspace and import selection, and rebinds later open/export to the new
system. It boots the candidate before retiring the old system. If interrupted,
use `bw project rebuild retry NAME` to continue the same candidate. `bw project
list` shows retained projects and pending replacements. System-only changes
are discarded; workspace contents remain untrusted and are not sanitized.
See `support/source/docs/operations/projects.md` for the runnable sequence and
failure behavior. Explicit guest clipboard helpers may require restaging on
the replacement system.
