# Boxwarden v0.2 alpha quickstart

For create → graphical desktop → stop → start using an already admitted base,
use the [basic workflow](../operations/basic-workflow.md). This longer walkthrough
includes recipe preparation, rebuild/replacement, and stopped export checks.
Current workspace/start operations require an explicitly
[enrolled storage config](workspace-remount-recovery-r1.md#existing-installation-upgrade-sequence);
legacy handoff configs must be enrolled into a new copy before those operations.

This is the current source-tracked operator path. Two synthetic workspace
histories passed the recorded public workflow, including recovery in the
independent replacement history; see [alpha progress](alpha-progress.md) for
observed results and limits. Use synthetic data and the explicit `alpha` test domain.

## Prerequisites

- A qualified macOS ARM64 host with the admitted Tart/Softnet pair, healthy
  `boxwarden doctor`, an initialized `alpha` domain CA, and adequate disk
  headroom. Host initialization is a separate one-time attended step.
- A clean checkout of this alpha branch, the pinned Ubuntu Desktop ARM64 ISO,
  exact admitted OpenSSL and xorriso executables and digests, and a private
  signed formatter bundle admitted for this source checkout.
- A configured alpha state root separate from other domains. Set `CONFIG`,
  `ISO`, `OPENSSL`, `OPENSSL_SHA256`, `XORRISO`, `XORRISO_SHA256`,
  `FORMATTER_BUNDLE`, and `GO_BIN` to their exact absolute local paths or
  digests. `GO_BIN` must name the actual `go` executable. Set `PRIVATE_BIN`
  to a new private output directory path, `PRIVATE_SOURCE` and `EXPORT_DEST` to new
  private directory paths, and `SESSION` to a fresh session name.

Generate distinct canonical workspace and filesystem UUIDs, for example:

```sh
VOLUME_UUID=$(uuidgen | tr '[:upper:]' '[:lower:]')
FS_UUID=$(uuidgen | tr '[:upper:]' '[:lower:]')
```

Build from a clean checkout and retain its source identity. The macOS host CLI
requires CGO for the Darwin serial PTY implementation; a CGO-disabled binary can
run host diagnostics and export commands but cannot start a workstation. Retain
and inspect build metadata rather than treating successful compilation or doctor
as proof of runtime support:

```sh
test -z "$(git status --porcelain)"
SOURCE_SHA=$(git rev-parse HEAD)
mkdir -m 700 "$PRIVATE_BIN"
CGO_ENABLED=1 GOTOOLCHAIN=local "$GO_BIN" build -o "$PRIVATE_BIN/boxwarden" ./cmd/boxwarden
"$GO_BIN" version -m "$PRIVATE_BIN/boxwarden"
printf 'source SHA: %s\n' "$SOURCE_SHA"
```

Set `BW` to that built binary, `SOURCE_ROOT` to the absolute checkout path,
and `RECIPE` to `examples/v0.2-alpha-chatgpt.json` within it before creating
the session. The full walkthrough requires its declared `edit-project` action. Give each new
session and workspace fresh names and UUIDs. Global flags precede the
command:

The default `examples/v0.2-alpha-chatgpt.json` recipe adds a pinned official
ARM64 ChatGPT package as a `prepare` step and requests a guest-side graphical
launch on `startup`, after the existing `record-first-start` once marker and
`count-starts` startup counter. The installer checks downloaded bytes, Debian package
identity, and installed identity, and disables the package's optional apt
source. The launcher requires the workstation's active graphical user manager
and asks it to start the package executable. Its checked command only proves
that the user manager accepted the process; a usable visible window and
waiting-for-sign-in state require separate VM acceptance. The first fresh
native-store build and visible ChatGPT sign-in window have passed that check;
both synthetic lifecycle histories also passed the recorded engineering
checks. Provider sign-in and authenticated capability remain untested.

[OpenAI's Linux guide](https://learn.chatgpt.com/docs/linux/linux-app) lists
Ubuntu 24.04 ARM64 as supported but says Computer Use is not yet available in
the Linux preview. Its [release notes](https://help.openai.com/en/articles/6825453-chatgpt-release-notes)
clarify the boundary: browser actions in the built-in browser or Chrome are
available, while controlling other desktop apps is not. Installation, visible
launch, and sign-in readiness do not establish either agent capability.

For a credential-free action check, `examples/v0.2-alpha-actions.json` writes
a `once` marker in the guest home, then its `startup` step requires that marker
and increments a guest-local start counter. A fresh run should produce counter
`1`; a later stop/start of the same system should produce `2` while the marker
remains. All three ChatGPT recipes include these same proof actions before GUI
launch. An already-running public start must add no action attempts and leave
the counter unchanged. A rebuilt system runs its own once action and starts
its guest-local counter at `1`; the separate workspace retains project bytes.
The first fresh system verified once/counter ordering, already-running start
idempotence and counter increments through actual restarts. Rebuilt-system
counter reset also passed after the public system rebuild and explicit
start. Replacement retention and the independent repetition also passed;
the latter replacement required the documented same-attempt recovery.

```sh
"$BW" --config "$CONFIG" doctor
"$BW" --config "$CONFIG" --domain alpha alpha recipe check --recipe "$RECIPE" --iso "$ISO"
"$BW" --config "$CONFIG" --domain alpha session create \
  --recipe "$RECIPE" --iso "$ISO" --guest-definition "$SOURCE_ROOT/guest/ubuntu-24.04-arm64" \
  --openssl "$OPENSSL" --openssl-sha256 "$OPENSSL_SHA256" \
  --xorriso "$XORRISO" --xorriso-sha256 "$XORRISO_SHA256" "$SESSION"
"$BW" --config "$CONFIG" --domain alpha workspace create \
  --bundle "$FORMATTER_BUNDLE" --source-root "$SOURCE_ROOT" \
  --filesystem-uuid "$FS_UUID" --size-mib 64 "$VOLUME_UUID"
"$BW" --config "$CONFIG" --domain alpha workspace attach \
  --mount /home/boxwarden/workspaces/project "$VOLUME_UUID" "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session status "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session action list "$SESSION"
```

For a recipe with `once` or `startup` steps, `session start` runs those guest
actions after management READY and reports automatic setup separately.
`session status` reports the live management readiness and action progress.
The action list reads the exact session's durable attempt journal. If an
automatic or explicit `reconfigure` command was interrupted before its UUID
was recorded by the caller, list it here before choosing `session action
retry` in the same READY generation or stopping and using `session action
skip`. An `indeterminate` entry does not prove whether the guest command ran.

Stage a private copy of the tracked synthetic project outside Boxwarden
state. `PRIVATE_SOURCE` must be a new absolute directory owned by the current
operator. The explicit copy preserves the original Git files and gives the
importer its required private modes:

```sh
mkdir -m 700 "$PRIVATE_SOURCE"
install -m 600 "$SOURCE_ROOT/examples/v0.2/synthetic-project/README.md" "$PRIVATE_SOURCE/README.md"
install -m 600 "$SOURCE_ROOT/examples/v0.2/synthetic-project/task.json" "$PRIVATE_SOURCE/task.json"
install -m 600 "$SOURCE_ROOT/examples/v0.2/synthetic-project/app.js" "$PRIVATE_SOURCE/app.js"
"$BW" --config "$CONFIG" --domain alpha workspace import \
  --source "$PRIVATE_SOURCE" "$VOLUME_UUID" "$SESSION"
```

Record the printed import transaction UUID as `IMPORT_UUID`. The command
reports `readback-matched` with journal `transferring`; that proves a pinned
live readback, not retained-disk persistence. Inside the guest, run
`node app.js` from the imported directory to exercise the synthetic workload.

For the independent persistence check, stop the sandbox and select the **whole
import directory** for a controlled stopped-volume export. `EXPORT_DEST` must
be a new empty private directory. The export reserve may refuse before it
creates a transaction when the host lacks headroom; retain the stopped volume
and transaction evidence rather than weakening the guard.

```sh
"$BW" --config "$CONFIG" --domain alpha session stop "$SESSION"
mkdir -m 700 "$EXPORT_DEST"
"$BW" --config "$CONFIG" --domain alpha workspace export \
  --destination "$EXPORT_DEST" --select "boxwarden-import-$IMPORT_UUID" \
  --source-root "$SOURCE_ROOT" --iso "$ISO" --go "$GO_BIN" "$VOLUME_UUID"
```

Set `EXPORT_UUID` from the successful export's printed transaction before
running verification:

```sh
"$BW" --config "$CONFIG" --domain alpha workspace import verify \
  --export "$EXPORT_UUID" "$IMPORT_UUID"
```

This verification compares the pristine import. After guest edits, use the
[project round trip](../operations/project-roundtrip.md) and independently
compare against the intended edited bytes instead.

The verify command reports `import: verified` only after the stopped backend,
qualified disk, published whole-directory export, and captured source all
match. Complete this verification while the original importing session and
backend still own the attachment: a rebuild changes backend identity, and a
replacement changes session identity. After either transition, make a new
stopped export and compare its bytes independently; the original import
verification cannot be repeated against that new owner. Keep the VM stopped
when finished. First-session GUI launch, pristine original-owner verification,
edited-data restart retention and system rebuild/start are verified. The actual
added-software gate passed using `tree`, which was absent in both baselines;
the earlier `jq` claim remains withdrawn because it was already installed.
Replacement reattachment, NEW stopped exports and the independent repetition
also passed, with D's documented recovery limitation. The verified local handoff
and final classification are recorded in [alpha progress](alpha-progress.md).

If export copying is interrupted and reports a transaction UUID, keep its
session stopped and recover that exact transaction:

```sh
"$BW" --config "$CONFIG" --domain alpha workspace export resume \
  --source-root "$SOURCE_ROOT" --iso "$ISO" --go "$GO_BIN" "$EXPORT_UUID"
```

A partial-copy recovery reports `export: aborted`, clears only its exact
reservation after stopped-backend and disk checks, and publishes no files.
Start a new export into a new empty destination afterward. A complete private
snapshot resumes inspection. Unexpected files, changed identities, uncertain
backend state, and ambiguous prior publication still refuse recovery. If the
process died before printing its UUID, retain the exact private export journal
for operator diagnosis; no public transaction listing is implemented yet.

## Explicit synthetic edits and persistence

For the ChatGPT recipes, complete the initial stopped export and original-owner
`workspace import verify` **before** changing the imported project. Then start
sandbox A and run its declared, guest-only action:

```sh
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session action run reconfigure edit-project "$SESSION"
```

The action requires exactly one ordinary `boxwarden-import-<UUID>` directory
at the declared workspace mount. It runs that imported demo with `--edit`,
updates `task.json` and creates `guest-note.txt`. The host source remains
unchanged. This explicit action is not an ordinary startup step. Missing,
ambiguous or linked import candidates stop the demo action before editing.

Stop A and export the whole import directory into a **new** private destination.
Independently require the task title `alpha workspace persistence (guest edited)`,
items `["import", "stop", "export", "verify", "guest-edit"]`, and the note
`Created inside sandbox A; retain through restart, rebuild and replacement.`
followed by one newline. Compare `app.js` and `README.md` to the captured
source and refuse unexpected entries before retaining this export as the
edited-data baseline. Compare these bytes after stop/start,
after rebuild plus explicit public start, and after replacement using another
new stopped export. The original pristine import journal has already been
verified; do not compare the edited tree to it or invoke that journal against a
replacement owner. Repeat on the second fresh workspace independently. These
explicit edits and first-system restart retention are verified with pinned
live readback and independently compared stopped exports. Retained bytes also
matched after rebuild/start and replacement; a NEW replacement export matched
the edited baseline. The second-volume repeat also passed, including the
documented exact receipt recovery for its replacement start.

For the software-changing rebuild trial, the tracked
`examples/v0.2-alpha-chatgpt-tree.json` recipe retains the ChatGPT preparation,
startup action, and workspace declaration while adding the [Ubuntu 24.04 ARM64
`tree` package](https://packages.ubuntu.com/noble/tree). This gives the
replacement base a different preparation key and an
additional package for independent-clone inventory. Confirm it is absent in the
original guest before treating installed presence after rebuild as a software
change. The historical `jq` variant remains available, but its baseline already
contained `jq` and therefore did not prove adding software. After recording the
stopped original system and exact workspace bytes, prepare and rebuild through
the public command:

```sh
"$BW" --config "$CONFIG" --domain alpha session rebuild \
  --recipe "$SOURCE_ROOT/examples/v0.2-alpha-chatgpt-tree.json" --iso "$ISO" \
  --guest-definition "$SOURCE_ROOT/guest/ubuntu-24.04-arm64" \
  --openssl "$OPENSSL" --openssl-sha256 "$OPENSSL_SHA256" \
  --xorriso "$XORRISO" --xorriso-sha256 "$XORRISO_SHA256" "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
```

Rebuild reports management state but does not invoke the recipe's automatic
`once` and `startup` actions. Require the subsequent start to report
`actions: complete`, then check the workspace bytes and actual GUI.
The historical `jq` trial passed system rebuild/start, admission, action counts,
retained edited bytes, ChatGPT sign-in readiness and replacement export. It did
not establish added software. The corrective `tree` rebuild passed for both
workspace histories with separate package/data/GUI evidence. Do not infer
retained data or a usable desktop from rebuild exit alone.


## Replace the disposable system and retain the workspace

Set `REPLACEMENT` to a fresh valid sandbox name (lowercase letters/digits),
`REPLACEMENT_EXPORT_DEST` to a new empty private directory, and `IMPORT_UUID` to
this cycle's original imported directory UUID. Keep the edited export baseline.
The public `session delete` contract retains separate workspace disks; it takes
no workspace-retention flag. Stop and delete only the disposable system:

```sh
"$BW" --config "$CONFIG" --domain alpha session stop "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session delete "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session create \
  --recipe "$SOURCE_ROOT/examples/v0.2-alpha-chatgpt-tree.json" --iso "$ISO" \
  --guest-definition "$SOURCE_ROOT/guest/ubuntu-24.04-arm64" \
  --openssl "$OPENSSL" --openssl-sha256 "$OPENSSL_SHA256" \
  --xorriso "$XORRISO" --xorriso-sha256 "$XORRISO_SHA256" "$REPLACEMENT"
"$BW" --config "$CONFIG" --domain alpha workspace attach \
  --mount /home/boxwarden/workspaces/project "$VOLUME_UUID" "$REPLACEMENT"
"$BW" --config "$CONFIG" --domain alpha session start "$REPLACEMENT"
"$BW" --config "$CONFIG" --domain alpha session stop "$REPLACEMENT"
mkdir -m 700 "$REPLACEMENT_EXPORT_DEST"
"$BW" --config "$CONFIG" --domain alpha workspace export \
  --destination "$REPLACEMENT_EXPORT_DEST" \
  --select "boxwarden-import-$IMPORT_UUID" --source-root "$SOURCE_ROOT" \
  --iso "$ISO" --go "$GO_BIN" "$VOLUME_UUID"
```

Do not format or re-import this existing volume. Independently compare the NEW
returned directory to the known edited bytes and earlier edited-data baseline;
refuse extra files or unsupported types. Keep original pristine import verification
bound to its original owner; do not run that verification against the replacement.
The historical first-workspace delete/create/attach/start/stop/export sequence
passed. Both corrective `tree` histories also completed this sequence with
NEW independently compared exports; D required the documented same-attempt
recovery. An earlier export cannot prove a later software transition.


## Formatter prerequisite and local handoff bindings

The dedicated-Mac handoff supplies private paths and identities for the built
CLI, configuration, verified ISO/tools, signed formatter and retained demo
workspaces. Keep those bindings outside Git. For a new clean source revision,
prepare a source-bound formatter using the verified ARM64 `e2fsck-static` package
required by the script:

```sh
"$SOURCE_ROOT/tools/alpha-formatter/prepare_boot.sh" \
  "$ISO" "$CHECKER_DEB" "$CONFIG" alpha
```

The script checks the fixed ISO/package/extracted-checker digests, builds and
signs the no-NIC formatter, and prints its bundle path. It prepares artifacts;
it does not format a disk. Archive the complete private bundle durably, verify
the archived bytes against its manifest, and set `FORMATTER_BUNDLE` to that
path before `workspace create`. Keep the source checkout clean and at the same
revision. Existing volumes retain their original formatter proof and are not
formatted again when attached, rebuilt or replaced.

## Known limits and remaining human actions

- The admitted Softnet policy permits guest-initiated connections to services
  on the vmnet gateway. Full guest-to-host network isolation is not claimed;
  effectively IPv6-only upstream environments remain unqualified.
- Filesystem UUID identifies the storage volume, but workspace admission also
  pins the raw file's device/inode. A remount can change the device and cause
  refusal; general identity reconciliation is unqualified. Do not rewrite
  records to adopt whatever happens to be mounted.
- Native storage reboot/reconnect and interruption during snapshot copying
  remain unqualified. A bounded real SIGINT during incomplete inspection output
  and same-snapshot resume with a live original workspace lease passed. That
  case does not prove the other interruption or remount scenarios. An export
  interrupted before UUID output still needs exact private-journal diagnosis.
- Provider sign-in and subjective desktop acceptance remain Wes's actions.
  Installation, an unauthenticated window and the synthetic workload do not
  prove an authenticated agent task or desktop computer-use capability.

Keep synthetic data for this acceptance workflow. Both tree rebuild histories
and independent repeats passed recorded engineering checks. Final source/host
handoff checks passed their documented scopes; provider sign-in and subjective
acceptance remain separate human actions.
