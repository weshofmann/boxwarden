# Boxwarden 0.2 private beta — Apple Silicon Mac

For an explicitly selected N1 candidate archive, read `N1-CANDIDATE.txt` and
`support/source/docs/v0.2/n1/current-beta.md` first. Its pinned candidate
toolchain needs separate approved deployment; the stock initialization below
does not provide candidate admission. Inspect `bin/boxwarden build-info --json`
and `BUILD.json` for the compiled selection.

This archive targets an **already initialized** Mac with admitted Tart 2.32.1 /
Softnet 0.19.0, private enrolled APFS storage and the pinned Ubuntu 24.04.4
Desktop ARM64 inputs. It does not install or upgrade host tools.
One-time helper preparation/export also requires actual Go 1.27.0, Xcode
Command Line Tools, system Python 3, an existing `zstd` executable, the pinned Ubuntu Desktop ARM64 ISO and
`e2fsck-static_1.47.0-2.4~exp1ubuntu4.1_arm64.deb`, plus an existing OpenSSL
with SHA-512 `passwd -6` support and `xorriso`. These are external assets;
Apple's `/usr/bin/openssl` does not provide the required preparation mode.

Keep the extracted directory at its final absolute location. It contains the
CLI, native project manager and a self-contained source checkout
for existing managed helper admission. No development worktree is required.
`BUILD.json` records revision/version; `SHA256SUMS` covers the shipped files.
Signatures are **ad-hoc, not Developer ID/notarized**. This local beta does not
claim download/Gatekeeper distribution acceptance; it does not bypass OS checks.

## Verify and configure once

Verify the archive beside its checksum file with
`shasum -a 256 -c boxwarden-0.2.0-beta.18-darwin-arm64.tar.gz.sha256`, then extract
in a new private user-owned directory. In a **new terminal**, set these
paths to your actual assets; no profile sourcing or private bindings is needed:

```sh
umask 077
PACKAGE=/absolute/boxwarden-0.2.0-beta.18-darwin-arm64
cd "$PACKAGE"
shasum -a 256 -c SHA256SUMS
"$PACKAGE/bin/boxwarden" version
"$PACKAGE/bin/boxwarden" help

ENROLLED_CONFIG=/absolute/existing/enrolled/config.json
CONFIG=/absolute/private-config-volume/boxwarden-beta/config.json
STATE=/absolute/enrolled-apfs-volume/private-beta-state
ISO=/absolute/ubuntu-24.04.4-desktop-arm64.iso
CHECKER=/absolute/e2fsck-static_1.47.0-2.4~exp1ubuntu4.1_arm64.deb
GO_BIN=/absolute/actual/go
ZSTD_BIN=/absolute/actual/zstd
OPENSSL_BIN=/absolute/actual/openssl
XORRISO_BIN=/absolute/actual/xorriso
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
bash "$PACKAGE/prepare-projects.sh" "$CONFIG" "$ISO" "$CHECKER" "$GO_BIN" "$ZSTD_BIN" "$OPENSSL_BIN" "$XORRISO_BIN"
```

Keep `CONFIG` on a different filesystem from the enrolled workspace backing
storage. The config contains the trusted volume identity used to admit that
storage; placing both on the same filesystem is refused. Use the mounted private
config volume that already holds your enrolled config, with a new file name.
`STATE` remains on the enrolled backing filesystem.

The new config preserves the existing toolchain/storage enrollment and creates
an independent state root. Package preparation creates a private local formatter
bound to the new state root and remembers the exact admitted preparation tools.
Run it once, before creating projects. Identical setup retries
are safe; different saved asset locators are refused. Keep this package and its
formatter in place. Do not reuse an older demo's state root or change its setup.

For an isolated side-by-side test application, build with
`BOXWARDEN_APP_BUNDLE_ID=org.boxwarden.project-manager.your-test` in addition to
`GO_BIN`. `BUILD.json` records the application identifier. That app gets its own
remembered configuration and command-activity directory; choose a fresh initialized
configuration to isolate its guests as well. Omitting the identifier preserves the
normal application identity and preferences. Keep an isolated package under its
original identifier when reopening unfinished work.

For an explicit side-by-side update of an existing private configuration, see
[UPGRADING.md](UPGRADING.md). Retain the old package and assets; preparation with
`--update` switches admitted asset locators and preserves previous setup bytes.

## Use the native project manager

Open **Boxwarden.app** in this directory. Click **Choose Configuration…** and
select your private JSON configuration. The app validates the configuration and storage before remembering this choice
without modifying the file. Invalid choices leave a previous valid selection intact. Typed setup messages distinguish unavailable storage, host initialization and missing project assets. **Setup Help…** explains the attended first-host step; **Prepare Project Assets…** selects the existing ISO, checker and build tools and invokes this package's managed preparation. Saved assets are remembered only after successful admission. It currently manages the `alpha` domain.

1. Click **New Project…**; enter a name, choose Ubuntu desktop, desktop with action examples, or desktop with the ChatGPT client,
   and select a workspace size. Keep the window open to watch actual CLI progress.
   The first matching recipe preparation can take tens of minutes. A second
   matching project reuses its qualified base but has an independent workspace.
2. Select a project. **Open Desktop** starts or resumes it; **Stop** stops it.
   Check both management readiness and software state before using the guest.
3. Click **Import Folder…**, choose a private source directory, and enter any
   explicit relative exclusions (comma separated). **Preview Selection** shows
   selected paths, sizes and digest. **Confirm Import** pins that exact selection;
   changing either field requires another preview. The app does not fix permissions,
   add exclusions or alter the source. Finish a pending import before stopping.
4. Edit files inside the imported directory shown in the project details. Keep
   important files on that independent workspace. Stop/open retains their contents.
5. Stop the project, click **Export Files…**, and choose a new folder name.
   **Show Returned Files** opens the returned files after success. Export
   refuses an existing destination and never runs the returned guest files.
6. **Exports & Recovery…** lists retained public records. After an interrupted
   export, keep the project stopped, inspect the listed destination, then use
   **Retry Selected Export…** when available. Retry rechecks the original binding
   and destination; uncertain helper lifetime or populated output remains refused.

**Host → Guest** and **Guest → Host** act only on the displayed exact project
target, after your explicit click. They do not log clipboard contents. No automatic
Tart clipboard or audio sharing is enabled. Automated verification uses a named
private pasteboard, never the general Mac clipboard.

The box icon in the menu bar offers **Show Projects** and both explicit clipboard directions for the selected project. Opening that menu does not inspect clipboard text or initiate a transfer.

Stop the project first. Click **Show Details**, then **Replace System…**. This
secondary action asks for confirmation because it discards system-local files
and applications while retaining the independent workspace. Select the intended
recipe and confirm only after reviewing that loss. **Resume Replacement** continues
an existing recorded replacement through the backend.

Quitting cancels temporary inventory and preview reads and waits briefly for their
children to exit. Guests and intentional project operations continue independently.
Reopen it to rediscover state. A process that finished after the frontend closed
may have no retained exit receipt: the app reports an unknown outcome, rather than
claiming success or replaying it. Refresh and inspect transactions before explicitly
continuing. Retained command activity is private, non-secret metadata under
`~/Library/Application Support/org.boxwarden.project-manager/CommandActivity`.

The CLI remains independently usable; the following commands exercise the same
operations for operators who prefer Terminal.

## Create, import, edit and resume

```sh
SOURCE=/absolute/new-private-source
mkdir -m 700 "$SOURCE"
printf 'Original host project.\n' > "$SOURCE/notes.txt"
printf 'unchanged-reference-v1\n' > "$SOURCE/reference.txt"
bw project create --recipe chatgpt --size-mib 64 myproject
bw project list
bw project status myproject
bw project import --source "$SOURCE" myproject
```

`bw project list` discovers existing named projects in name order, with their
current state, exact workspace association, and next commands. It is a read-only
snapshot: unavailable backing storage is an error, incomplete projects stay
visible, and a running backend is READY only with a fresh exact supervisor check.
The commands shown recheck their bindings before doing work.

Recipes are `desktop` (Ubuntu tools), `actions` (phase demonstration), and
`chatgpt` (the pinned graphical client and phase demonstration). Recipe-enabled
setup defaults a bare create to `desktop`. The first matching preparation installs
Ubuntu, requested software and compatible Boxwarden helpers, then qualifies a
fresh clone. It may take tens of minutes; `preparation:` messages show its stages.
A second `project create --recipe chatgpt anotherproject` reuses the same qualified
preparation (`cache: reused`) and gets its own system/workspace. Software sources
need network access during preparation; a failed attempt is retained and not
admitted as a successful base. No manual golden registration or helper copying
is needed.

Create opens the native desktop. Wait for **`management-readiness: ready` and
`actions: complete`** before treating software setup as complete; READY alone
checks the management channel. The ChatGPT recipe opens its graphical application
automatically; sign-in is optional and is not part of this walkthrough.
On a fresh desktop, Ubuntu may show optional update/upgrade notices and a
keyring creation dialog. For this credential-free trial, cancel keyring creation
and dismiss the optional updater; no password or upgrade is needed to reach
the application's sign-in screen.
If a fresh observation expires, run `bw project open myproject` to re-establish
readiness, then `bw project import retry myproject` for a recorded import. The
retry keeps its original selection, transaction and running generation. Finish a
pending import before stopping or upgrading that runtime: stop/start changes its
generation, and retry then refuses the old transaction. Do not rewrite the
journal or create another import over uncertain guest contents.
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
Import supports 4096 files / 256 MiB total, with 64 MiB per file and 2048
directories; stopped export supports 8192 files / 512 MiB total on workspace
disks up to 4 GiB. Transfers remain explicit copies. See `support/source/docs/operations/projects.md` in this archive for selection
preview and literal exclusions before importing dependency/build trees. Pin the printed preview digest with
`--expected-digest` when importing the inspected selection.

## Find an interrupted export

After Ctrl-C, wait for the command to return and keep the sandbox stopped:

```sh
bw project export list myproject
bw project export retry --transaction UUID-FROM-LIST myproject
```

Choose the listed transaction with your intended destination. List shows only
recorded journal metadata; it does not retry, repair, inspect the destination or
assert that a recorded phase proves success. A historical binding needs
inspection. Retry rechecks its exact binding and retained resources; partial
copies abort without publishing, while a complete snapshot can resume to its
original private destination. Uncertain helper lifetime or existing output is
still refused. Published entries show the recorded returned path to inspect.

## Controlled clipboard

Recipe-created and recipe-rebuilt systems get their compatible helpers from the
same prepared definition. A startup sanity check verifies installed helper pins,
dependencies and the graphical session without reading clipboard contents.
Actual support is demonstrated by an explicit transfer below. Management READY
alone, or a successful guest check, is not attestation against malicious guest root.

The explicit `--base` legacy route still exists for existing registered bases;
its guest support is unverified and it does not provision a recipe automatically.
Use `--recipe` for the turnkey route. Existing guests and bases are never updated
by ordinary open or package setup.

### Discover and transfer

```sh
open -a "$PACKAGE/Boxwarden.app" --args --config "$CONFIG"
bw project open myproject
bw clipboard targets
```

Select the intended project in the project manager. Its clipboard target must
be ready. **Host → Guest** and **Guest → Host** use the general Mac clipboard only
after your explicit click. Opening the app, refreshing and selecting a project
read readiness metadata only.
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
`GO_BIN=/absolute/actual/go bash tools/private-beta/build.sh 0.2.0-beta.18 /absolute/new-output`.
This builds locally and publishes no release.

## Replace a disposable project system

With a stopped recipe-bound project, `bw project rebuild NAME` uses its captured
software intent and current package support definition. `bw project rebuild
--recipe chatgpt NAME` explicitly selects a new recipe. Both clone a replacement
even if the qualified preparation is reused, retain the
same workspace and import selection, and rebinds later open/export to the new
system. It boots the candidate before retiring the old system. If interrupted,
use `bw project rebuild retry NAME` to continue the same candidate. `bw project
list` shows retained projects and pending replacements. System-only changes
are discarded; workspace contents remain untrusted and are not sanitized.
See `support/source/docs/operations/projects.md` for the runnable sequence and
failure behavior. Requested software and compatible guest support are restored
automatically on the new system; manual helper restaging is unnecessary. Once actions run for the
new system, startup actions run for each new start generation. Repeated open on
the same running generation reruns neither. If software setup fails after cutover,
the replacement remains bound; inspect `session action list NAME` and its explicit
recovery commands. Do not create another replacement merely to replay an uncertain
action.
