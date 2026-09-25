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
ARM64 ChatGPT package as a `prepare` step. Its helper checks the downloaded
bytes, Debian package identity, and installed identity, and disables the
package's optional apt source. This source path has targeted checks, but the
ChatGPT recipe has not yet passed real-VM build or graphical acceptance.

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
match. Keep the VM stopped when finished. Rebuild, replacement reattachment,
GUI acceptance, and a fresh independent repetition are tracked in
[alpha progress](alpha-progress.md) until they pass the complete matrix.
