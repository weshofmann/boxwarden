# Basic graphical sandbox workflow

Use this path on an already initialized Mac with a healthy `doctor`, an
initialized domain, and a selected, admitted, stopped generic golden. It clones
that existing golden; it does not rebuild a base, install host tools, or sign in
to a provider. The recipe preparation path in the
[alpha quickstart](../v0.2/alpha-quickstart.md) is for building or selecting a
recipe-specific base, not a prerequisite for using an existing one.

## Local prerequisites

Use a CGO-enabled Darwin ARM64 CLI and an explicit config. Set `PRIVATE_BIN` to
a new absolute private build-directory path outside the checkout and `CONFIG`
to the private config supplied for this Mac. Global
flags go before the command. This prototype's workspace interface uses the
explicit `alpha` domain.

```sh
mkdir -m 700 "$PRIVATE_BIN"
CGO_ENABLED=1 GOTOOLCHAIN=local go build -o "$PRIVATE_BIN/boxwarden" ./cmd/boxwarden
BW=$PRIVATE_BIN/boxwarden
go version -m "$BW"
"$BW" --config "$CONFIG" doctor
```

Current workspace operations require enrolled backing storage. For a legacy
config, take the expected APFS UUID from the trusted storage inventory and write
an enrolled **copy**, at a new private path on a different filesystem from the
workspace state. Keep the original config unchanged:

```sh
"$BW" --config "$CONFIG" --domain alpha workspace storage enroll \
  --expected-apfs-volume-uuid "$EXPECTED_APFS_UUID" \
  --mount-point "$MOUNT_POINT" --output-config "$NEW_CONFIG"
CONFIG=$NEW_CONFIG
```

See [storage enrollment](../v0.2/workspace-remount-recovery-r1.md) for its
identity checks. Do not infer authority from whatever happens to be mounted.
Fresh workspace creation also needs a signed formatter bundle bound to the
clean source revision and selected state root; use the
[formatter preparation instructions](../v0.2/alpha-quickstart.md#formatter-prerequisite-and-local-handoff-bindings).
An existing workspace keeps its original formatter proof.

## Create once, then use start and stop

Choose a fresh lowercase session name and distinct canonical volume/filesystem
UUIDs. Set `SOURCE_ROOT` to the clean checkout and `FORMATTER_BUNDLE` to its
verified private bundle. Leave adequate free space above the enforced reserve.

```sh
SESSION=basicdemo
VOLUME_UUID=$(uuidgen | tr '[:upper:]' '[:lower:]')
FS_UUID=$(uuidgen | tr '[:upper:]' '[:lower:]')

"$BW" --config "$CONFIG" --domain alpha session create "$SESSION"
"$BW" --config "$CONFIG" --domain alpha workspace create \
  --bundle "$FORMATTER_BUNDLE" --source-root "$SOURCE_ROOT" \
  --filesystem-uuid "$FS_UUID" --size-mib 64 "$VOLUME_UUID"
"$BW" --config "$CONFIG" --domain alpha workspace attach \
  --mount /home/boxwarden/workspaces/project "$VOLUME_UUID" "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session status "$SESSION"
```

Start opens a native Tart window showing Ubuntu Desktop. Bring that window
forward if necessary. Wait for `readiness: ready`; a visible desktop alone is
not management READY. Open Terminal through Ubuntu's Show Apps menu. Automatic
host clipboard and audio sharing are disabled. There is no separate public
desktop-open command; minimizing the Tart window preserves the running guest.
Use public stop when finished instead of closing the VM window.

For an explicit synthetic file transfer, create a new private host directory
outside domain state, then import it after READY:

```sh
mkdir -m 700 "$PRIVATE_SOURCE"
printf 'Hello from the independent workspace.\n' > "$PRIVATE_SOURCE/hello.txt"
chmod 600 "$PRIVATE_SOURCE/hello.txt"
"$BW" --config "$CONFIG" --domain alpha workspace import \
  --source "$PRIVATE_SOURCE" "$VOLUME_UUID" "$SESSION"
```

The printed `remote` directory is `boxwarden-import-<transaction UUID>` below
the mount. In the guest terminal, inspect `findmnt` for the mount and read/hash
`hello.txt`. `import: readback-matched` proves live transfer, not persistence.
Then exercise the ordinary lifecycle:

```sh
"$BW" --config "$CONFIG" --domain alpha session stop "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session status "$SESSION"
# In the new desktop, read/hash the same imported file again; do not re-import.
"$BW" --config "$CONFIG" --domain alpha session stop "$SESSION"
```

## Persistence and limits

Stop/start retains the same writable system disk: installed guest software,
home files, and desktop settings persist. Processes, RAM, open application
windows, and the running management generation do not persist. The workspace
is a separate Boxwarden-managed ext4 disk with one writable owner; its attachment
and project bytes persist across stop/start. It is not a host filesystem share.
Keep important work there rather than relying on the disposable system disk.

This basic check does not establish system rebuild, disaster recovery, power-loss
durability, physical disconnect behavior, provider authentication, or complete
network qualification. Guest connections to vmnet-gateway services remain an
accepted limitation. Preserve an error and inspect the exact session status if
start/stop fails; do not rewrite lifecycle or storage records to assert success.

The local handoff for an exercised demo should identify its CLI build, enrolled
config, session/backend, workspace/filesystem UUIDs, imported path, evidence,
and final stopped state. Those machine-specific bindings belong outside Git.
