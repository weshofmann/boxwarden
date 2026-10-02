# Bring a guest-edited project back to the host

Start with the [basic graphical workflow](basic-workflow.md): an initialized
Mac, a CGO-enabled CLI, explicit enrolled `CONFIG`, and a READY sandbox with an
attached independent workspace at `/home/boxwarden/workspaces/project`.
Keep its `BW`, `SESSION`, and `VOLUME_UUID` bindings. This walkthrough uses only
synthetic text and the public lifecycle/import/export commands.

If you finished the basic guide, its sandbox is stopped. Start that same
session and wait for `readiness: ready` before importing:

```sh
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session status "$SESSION"
```

For export, also set `SOURCE_ROOT` to a clean, committed checkout used to build
the CLI, `ISO` to the admitted Ubuntu 24.04.4 Desktop ARM64 ISO, and `GO_BIN` to
an absolute Go executable path. The command builds its managed inspector from
those assets; no separate helper installation is needed. Keep sufficient disk
headroom above the enforced storage reserve.

## Import a private copy

Set `PRIVATE_SOURCE` to a new absolute host directory outside Boxwarden state.
The importer requires private directory/file modes. It copies bytes into the
workspace; subsequent guest edits do not update the source.

```sh
mkdir -m 700 "$PRIVATE_SOURCE"
printf 'Original host project.\n' > "$PRIVATE_SOURCE/notes.txt"
printf 'unchanged-reference-v1\n' > "$PRIVATE_SOURCE/reference.txt"
chmod 600 "$PRIVATE_SOURCE/notes.txt" "$PRIVATE_SOURCE/reference.txt"
"$BW" --config "$CONFIG" --domain alpha workspace import \
  --source "$PRIVATE_SOURCE" "$VOLUME_UUID" "$SESSION"
```

Save the printed `transaction` as `IMPORT_UUID` and the printed `remote` path
as `GUEST_PROJECT`. `readback-matched` checks the initial live copy, not edits
made later or stop/start persistence.

## Edit inside the desktop, then check persistence

Open Terminal through Ubuntu's Show Apps menu. Change to the printed remote
directory. The following lines replace the existing file and create another
using the guest's existing `ex` editor. Enter each line, including the single
period, followed by Return. Replace the path on the first line with your actual
`GUEST_PROJECT`; host shell variables are not available in the guest.

```text
cd /home/boxwarden/workspaces/project/boxwarden-import-<IMPORT_UUID>
ex -s notes.txt
1c
guest-edited-v1
.
wq
ex -s new.txt
a
guest-created-v1
.
wq
cat notes.txt new.txt reference.txt
sha256sum notes.txt new.txt reference.txt
```

Expect `guest-edited-v1`, `guest-created-v1`, and `unchanged-reference-v1`, each
with a trailing newline. Leave the reference file untouched. On the host:

```sh
"$BW" --config "$CONFIG" --domain alpha session stop "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session start "$SESSION"
"$BW" --config "$CONFIG" --domain alpha session status "$SESSION"
```

After READY, open a new guest Terminal, change to the same directory, and repeat
only `cat` and `sha256sum`. Compare all three contents/hashes with the pre-stop
values; do not re-import or repeat the edits. Then stop on the host:

```sh
"$BW" --config "$CONFIG" --domain alpha session stop "$SESSION"
```

## Export and inspect on the host

Set `EXPORT_DEST` to a new absolute, empty private host directory outside
Boxwarden state. Export the whole imported directory while the sandbox remains
stopped:

```sh
mkdir -m 700 "$EXPORT_DEST"
"$BW" --config "$CONFIG" --domain alpha workspace export \
  --destination "$EXPORT_DEST" --select "boxwarden-import-$IMPORT_UUID" \
  --source-root "$SOURCE_ROOT" --iso "$ISO" --go "$GO_BIN" "$VOLUME_UUID"
```

Set `EXPORT_PATH` to the successful command's printed `export` path. Your project
is directly below it:

```sh
PROJECT_DIR="$EXPORT_PATH/boxwarden-import-$IMPORT_UUID"
ls -l "$PROJECT_DIR"
cat "$PROJECT_DIR/notes.txt" "$PROJECT_DIR/new.txt" "$PROJECT_DIR/reference.txt"
```

Independently compare against the intended edits and original source. This
reads returned files as data; it does not execute them or overwrite a host
project:

```sh
python3 - "$PROJECT_DIR" "$PRIVATE_SOURCE" <<'PY'
from pathlib import Path
import stat
import sys

def files(directory):
    result = {}
    for p in Path(directory).iterdir():
        assert stat.S_ISREG(p.lstat().st_mode), f"unexpected entry: {p}"
        result[p.name] = p.read_bytes()
    return result

assert files(sys.argv[1]) == {
    "notes.txt": b"guest-edited-v1\n",
    "new.txt": b"guest-created-v1\n",
    "reference.txt": b"unchanged-reference-v1\n",
}, "returned project differs from intended guest edits"
assert files(sys.argv[2]) == {
    "notes.txt": b"Original host project.\n",
    "reference.txt": b"unchanged-reference-v1\n",
}, "original host source changed"
print("Edited project matched; original host source unchanged.")
PY
```

`workspace import verify` compares an export with the captured **pristine import**.
It is not the check for this edited project. Use the independent comparison
above, including the new file and unchanged reference.

## Stop cleanliness and limits

`forced=false` does not mean a clean filesystem. A stop may report
`workspace_cleanliness=unverified`. Export separately checks the qualified
stopped disk's ext4 clean state and recovery flags before copying a snapshot.
Its managed inspector mounts that snapshot read-only without journal replay.
If export refuses a dirty/recovery-needed filesystem, preserve the stopped
workspace and error; do not force export, repair the disk offline, or rewrite
records to claim success. Interrupted exports have an
[exact transaction recovery path](../v0.2/alpha-quickstart.md).

The exercised Ubuntu desktop round trip retained edits through ordinary
stop/start and exported them after an unverified fallback stop that passed the
clean-superblock guard. This does not prove arbitrary shutdowns are clean,
power-loss durability, system rebuild/recovery, or complete network isolation.
The vmnet-gateway exposure remains a limitation. Leave the sandbox stopped,
keep the workspace attached, and retain the returned directory for inspection.
