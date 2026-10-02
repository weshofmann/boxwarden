# Named projects on an initialized Mac

`project` remembers the sandbox, independent workspace and explicit import
selection under an operator-chosen name. It composes the existing admitted
lifecycle and transfer operations. A bookmark is not proof of readiness or
storage health: every operation checks the underlying records and backing
storage. Existing sessions are not adopted by name.

Use a CGO-enabled Darwin ARM64 build, an enrolled private config, a healthy
`doctor`, and a registered prepared Ubuntu Desktop ARM64 base. Keep adequate
free space above the enforced storage reserve. The current interface supports
the explicit `alpha` domain and 16–1024 MiB workspaces, matching the stopped
export size limit. It reuses a prepared base; creating a project does not rebuild
Ubuntu or install host tools.

## One setup

In a new terminal, set `BW` to your staged CLI and `CONFIG` to the Mac's enrolled
config. This shell function only abbreviates public flags:

```sh
BW=/absolute/private/build/boxwarden
CONFIG=/absolute/private/config.json
bw() { "$BW" --config "$CONFIG" --domain alpha "$@"; }
"$BW" --config "$CONFIG" doctor
```

Once per state root, remember the existing admitted assets. Set `SOURCE_ROOT`
to the matching clean committed checkout, `FORMATTER_BUNDLE` to its signed
private formatter bundle, `ISO` to the pinned Ubuntu 24.04.4 Desktop ARM64 ISO,
and `GO_BIN` to the actual absolute Go executable. See
[formatter preparation](../v0.2/alpha-quickstart.md#formatter-prerequisite-and-local-handoff-bindings)
if a matching bundle is not already staged. Preparation creates build artifacts,
not a replacement Ubuntu base or privileged installation.

```sh
bw project setup --source-root "$SOURCE_ROOT" \
  --formatter-bundle "$FORMATTER_BUNDLE" --iso "$ISO" --go "$GO_BIN"
```

Setup validates assets before saving their locators. Repeating identical setup
is safe; different locators are refused. Keep the staged source checkout and
bundle together at their original paths and revision. Routine use below needs
only `BW`, `CONFIG`, a name and explicit transfer paths; no UUIDs or helper flags.

## Create and explicitly import

Choose a fresh name and a new absolute private source path outside domain state:

```sh
SOURCE=/absolute/private/synthetic-source
umask 077
mkdir -m 700 "$SOURCE"
printf 'Original host project.\n' > "$SOURCE/notes.txt"
printf 'unchanged-reference-v1\n' > "$SOURCE/reference.txt"

bw project create --base current --size-mib 64 myproject
bw project status myproject
bw project import --source "$SOURCE" myproject
```

`--base current` uses the selected registered golden. You can instead give an
explicit registered prepared-base name. Create opens the native Tart desktop;
wait for `readiness: ready` before importing. Status reports live readiness,
workspace, guest files path and next actions. Import copies a bounded private
tree (at most 256 files and 16 MiB); later guest edits do not modify the source.
It is one initial selection per project, not synchronization.

The printed guest files directory is below
`/home/boxwarden/workspaces/project`. Open Terminal through Ubuntu's Show Apps.
To enter that directory without copying a transaction ID, type the following
prefix, press Tab to complete the single import directory, then Return:

```text
cd /home/boxwarden/workspaces/project/boxwarden-import-
```

Enter each following line followed by Return. This uses the guest's existing
`ex` editor, replaces an existing file, creates a new one, and leaves the
reference untouched:

```text
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

## Stop, resume and export

```sh
bw project stop myproject
# From a fresh terminal, set BW and CONFIG and define bw as above.
bw project open myproject
bw project status myproject
```

Open resumes the same sandbox/workspace. Repeated open neither clones another
VM nor imports over edits. In the new desktop Terminal, complete the same
directory and repeat only `cat` and `sha256sum`; compare with the pre-stop values.

```sh
bw project stop myproject
bw project export --destination /absolute/private/new-returned-directory myproject
```

The destination must not exist. Boxwarden creates it privately and prints
`project files: /absolute/...`, the actual returned directory. Inspect those
files as data; do not execute guest files or overwrite an existing host project.
Set `RETURNED` to that printed path and compare independently:

```sh
# Restore the original import source path if this is a new terminal.
SOURCE=/absolute/private/synthetic-source
ls -l "$RETURNED"
python3 - "$RETURNED" "$SOURCE" <<'PY'
from pathlib import Path
import stat, sys
def files(root):
    result = {}
    for p in Path(root).iterdir():
        assert stat.S_ISREG(p.lstat().st_mode), f"unexpected entry: {p}"
        result[p.name] = p.read_bytes()
    return result
assert files(sys.argv[1]) == {
    "notes.txt": b"guest-edited-v1\n", "new.txt": b"guest-created-v1\n",
    "reference.txt": b"unchanged-reference-v1\n",
}
assert files(sys.argv[2]) == {
    "notes.txt": b"Original host project.\n",
    "reference.txt": b"unchanged-reference-v1\n",
}
print("Edited project matched; original host source unchanged.")
PY
```

`readback matched` checks the initial import only. Pristine-import verification
is not verification of edited returned files. Create another named project with
the same public commands to obtain a distinct sandbox/workspace; no new setup
or hand-prepared bindings are needed.

## Replace a stopped project's disposable system

Keep work in the independent workspace. To discard guest system-disk changes
and clone a registered prepared base under the same project name:

```sh
bw project list
bw project stop myproject
bw project rebuild --base current myproject
bw project status myproject
bw project open myproject
```

This explicitly creates a replacement even when the selected base is unchanged.
The existing rebuild driver first records a stopped candidate, switches the
session binding, boots that exact candidate to fresh READY with the retained
workspace, then retires only the old system. Rebuild can leave the candidate
running; `project open` reuses that candidate. The name, session ID, workspace
UUID, filesystem identity, attachment and original import selection stay the
same. Subsequent open/export use the new system binding. Workspace data is
neither reformatted nor reimported. Guest packages, system files and settings
created after the prepared base disappear; explicitly staged guest clipboard
helpers may need to be staged again on the replacement.

Rebuild requires a stopped project with no unfinished import. If preparation,
boot, retirement or bookmark publication fails, keep the retained state and use:

```sh
bw project rebuild retry myproject
```

Retry uses the recorded candidate; do not supply another base. The immutable
replacement history allows a retry to settle a final directory-sync failure.
It cannot authorize rebuilding, starting or deleting a different system. List
shows a pending replacement and its retry command. Unrelated or inconsistent
bindings fail closed and require inspection.

Rebuilding the system does **not** sanitize a workspace modified by a malicious
guest. Treat returned files as untrusted data. Verify a system-only marker has
disappeared, inspect the retained project edits in the guest, then stop and
export to a new private destination using the commands above. Compare the
returned bytes against the intended edits, not the pristine import receipt.

## Errors and limits

A name collision directs you to `project open`; a legacy session is not adopted.
Missing setup, unsupported sizes/bases and unavailable backing storage are
refused before allocation. Incomplete workspace initialization retains intent;
`project open NAME` retries the same allocation. An ambiguous interrupted
session allocation requires inspection and is never silently adopted.

If initial import fails, fix the original source path/permissions and use
`bw project import retry NAME`. Retry retains the same transaction and source,
resumes captured bytes when present, and never replaces a completed import.
The existing transfer checks still refuse a different guest generation or
divergent guest contents; keep a pending import in the same running session
while resolving it. Open does not retry imports automatically.

Export requires the exact stopped sandbox. `forced=false` does not prove clean
ext4, and stop may report `workspace_cleanliness=unverified`. The exporter
separately checks clean-superblock/recovery flags, copies a snapshot and mounts
that snapshot read-only without journal replay. A refused dirty disk stays
refused; do not force export or rewrite records. Errors identify retained export
transactions for the existing low-level recovery path.

Ordinary stop/open retains system-disk and independent-workspace bytes;
processes, RAM, windows and the management generation do not persist. This path
does not qualify power loss, disaster recovery, provider login
or complete network isolation. The vmnet-gateway exposure remains a limitation.
Automatic Mac clipboard/audio sharing remains disabled; controlled clipboard
transfers remain separate explicit actions.

## Observed initialized-Mac workflow

The named-project walkthrough was exercised with a CGO Darwin ARM64 CLI built
with Go 1.27.0, admitted Tart 2.32.1 / Softnet 0.19.0, an enrolled APFS backing
volume and the existing prepared Ubuntu 24.04.4 Desktop ARM64 base. Guest GUI
edits and the new file matched independent intended hashes before stop and after
open; repeated open preserved them. Stopped export returned exactly those three
files, and independent host comparison confirmed the original source unchanged.
Both stops reported `tart_fallback`, `forced=false`, and cleanliness unverified;
the actual exporter separately passed its clean/recovery-superblock guard.
A second name allocated a distinct sandbox/workspace with the same saved setup.
An initial missing-source import was repaired at its original path and the
public retry succeeded. Its distinct GUI edits exported correctly, its host
source stayed unchanged, and hashes of the first project's workspace disk,
bookmark, attachment record, source and returned files stayed unchanged.
Both new sandboxes were left stopped with workspaces attached and exports
available. Name collision, missing setup, running export and a synthetic
storage identity mismatch were also refused; no host settings were changed.
