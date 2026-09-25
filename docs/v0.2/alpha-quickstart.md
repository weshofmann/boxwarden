# Boxwarden v0.2 alpha quickstart

This is the current source-tracked operator path. The fresh full workflow is
still under qualification; see [alpha progress](alpha-progress.md) for observed
results and limits. Use synthetic data and the explicit `alpha` test domain.

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

Build from a clean checkout and retain its source identity:

```sh
test -z "$(git status --porcelain)"
SOURCE_SHA=$(git rev-parse HEAD)
mkdir -m 700 "$PRIVATE_BIN"
GOTOOLCHAIN=local "$GO_BIN" build -o "$PRIVATE_BIN/boxwarden" ./cmd/boxwarden
printf 'source SHA: %s\n' "$SOURCE_SHA"
```

Set `BW` to that built binary, `SOURCE_ROOT` to the absolute checkout path,
and `RECIPE` to `examples/v0.2-alpha-base.json` within it. Give each new
session and workspace fresh names and UUIDs. Global flags precede the
command:

The optional `examples/v0.2-alpha-chatgpt.json` recipe adds a pinned official
ARM64 ChatGPT package as a `prepare` step and requests a guest-side graphical
launch on `startup`. The installer checks downloaded bytes, Debian package
identity, and installed identity, and disables the package's optional apt
source. The launcher requires the workstation's active graphical user manager
and asks it to start the package executable. Its checked command only proves
that the user manager accepted the process; a usable visible window and
waiting-for-sign-in state require separate VM acceptance. This recipe has not
yet passed a real-VM build or graphical acceptance.

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
remains. This example is source checked and awaits fresh-VM qualification.

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
"$BW" --config "$CONFIG" --domain alpha workspace import verify \
  --export "$EXPORT_UUID" "$IMPORT_UUID"
```

Set `EXPORT_UUID` from the successful export's printed transaction. The
verify command reports `import: verified` only after the stopped backend,
qualified disk, published whole-directory export, and captured source all
match. Complete this verification while the original importing session and
backend still own the attachment: a rebuild changes backend identity, and a
replacement changes session identity. After either transition, make a new
stopped export and compare its bytes independently; the original import
verification cannot be repeated against that new owner. Keep the VM stopped
when finished. Rebuild, replacement reattachment, GUI acceptance, and a fresh
independent repetition are tracked in [alpha progress](alpha-progress.md)
until they pass the complete matrix.

For the pending software-changing rebuild trial, the tracked
`examples/v0.2-alpha-chatgpt-jq.json` recipe retains the ChatGPT preparation,
startup action, and workspace declaration while adding the [Ubuntu 24.04 ARM64
`jq` package](https://packages.ubuntu.com/noble/arm64/jq). This gives the
replacement base a different preparation key and an
additional package for independent-clone inventory. After recording the
stopped original system and exact workspace bytes, prepare and rebuild through
the public command:

```sh
"$BW" --config "$CONFIG" --domain alpha session rebuild \
  --recipe "$SOURCE_ROOT/examples/v0.2-alpha-chatgpt-jq.json" --iso "$ISO" \
  --guest-definition "$SOURCE_ROOT/guest/ubuntu-24.04-arm64" \
  --openssl "$OPENSSL" --openssl-sha256 "$OPENSSL_SHA256" \
  --xorriso "$XORRISO" --xorriso-sha256 "$XORRISO_SHA256" "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
```

Rebuild reports management state but does not invoke the recipe's automatic
`once` and `startup` actions. Require the subsequent start to report
`automatic-actions: complete`, then check the workspace bytes and actual GUI.
These commands still require a fresh real-host qualification. Do not infer
retained data or a usable desktop from rebuild's exit status alone.
