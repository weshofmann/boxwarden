# M1A explicit host-directory capability spike

Date: 2026-09-01

Status: Complete experimental evidence; no production capability or promotion
path implemented

## Decision summary

| Question | Classification |
| --- | --- |
| Explicit read-only host tree | **READY FOR M1A IMPLEMENTATION** |
| Writable guest proposal using OverlayFS | **EXPERIMENTAL ONLY** |
| Stable baseline | **PRACTICAL FOR M1A** |
| Promotion/write-back | **READY FOR DESIGN REVIEW** |

The classifications are independent. In particular, the read-only result does
not accept a writable host share, make OverlayFS upperdir formats portable, or
authorize promotion into a durable host tree.

## Scope and safety envelope

The spike used only generated files under a dedicated directory in
/private/tmp and disposable VMs named with the Boxwarden spike prefix. It never
shared a real repository, home directory, credential store, Boxwarden runtime
state, age identity, browser profile, or other private data. Every Tart
directory attachment used the read-only flag. No writable host share was
created.

The host free-space stop threshold was 25 GiB. Free space was 57 GiB after the
work and did not cross the threshold. No kernel panic, launchd crash, system
hang, or filesystem-corruption symptom occurred.

The qualified environment was:

- Apple Silicon macOS 26.6.2;
- Tart 2.32.1;
- Softnet 0.19.0;
- Ubuntu 24.04 ARM64 Task 0 golden;
- APFS host storage;
- Linux 7.0 guest kernel.

The test tree, baseline copies, relay paths, temporary Tart source checkout,
and both spike VMs were removed at the clean stop point. No spike Tart,
Softnet, socat, or Screen process remained.

### Existing-resource audit exception

The pre-run Tart inventory contained three stopped local VMs. A pre-existing
Task 0 clean-run clone was used as the source of the first spike clone. Tart
advanced that source object's host metadata Accessed timestamp while cloning
it. The VM was never booted, configured, or deleted, and no disk or config
change was observed, but this metadata update means the run did not perfectly
satisfy the instruction to avoid affecting pre-existing objects.

Future spikes must first create one spike-owned seed clone, then derive every
experimental VM only from that owned seed. The empirical filesystem results do
not depend on the Accessed timestamp, but the exception is part of the audit
record.

## Tart mechanism and upstream status

Tart 2.32.1 accepts:

    tart run VM --dir=HOST_PATH:ro,tag=TAG

and the guest mounts the tag with:

    mount -t virtiofs TAG GUEST_PATH

Inspection of the 2.32.1 source found that DirectoryShare expands a leading
tilde and constructs VZSharedDirectory with readOnly set from the command-line
option. Tart itself does not canonicalize the source or enforce a Boxwarden
containment policy. Therefore path admission belongs in trusted Boxwarden
control-plane code; the Tart adapter receives an already validated capability.

Relevant upstream material:

- https://github.com/cirruslabs/tart/blob/2.32.1/Sources/tart/Commands/Run.swift
- https://github.com/cirruslabs/tart/blob/main/docs/quick-start.md
- https://github.com/cirruslabs/tart/issues/1308
- https://github.com/cirruslabs/tart/issues/1271

Issue 1308 reports four launchd SIGBUS host panics while copying a roughly
13,000-file DerivedData tree from a read-only VirtioFS share with Tart 2.34 on
macOS 26.3. As of this spike there is no maintainer diagnosis, minimal
reproducer, or recorded fix. It is adjacent rather than an exact reproduction
of Boxwarden's Tart 2.32.1/macOS 26.6.2 pair, but host stability remains a
release property and a version-upgrade requalification condition.

Issue 1271 reports Git corruption on a writable share. Boxwarden did not use or
qualify writable sharing.

## Safe spike launcher

scripts/spike/bootstrap-tart.sh now contains a deliberately spike-only
run-ro-share command. It:

- requires a canonical absolute, non-root source;
- rejects a symlink source root;
- requires the source to be strictly below a dedicated synthetic spike root;
- passes exactly one fixed-tag read-only directory attachment;
- retains the Task 0 Softnet, no-audio, no-clipboard, and managed serial policy.

The production abstraction is not this command and not arbitrary Tart flags.
The command exists only to make the experiment repeatable without allowing a
careless invocation to expose a real host tree.

Example setup:

    export BW_HOSTDIR_SPIKE_ROOT=/private/tmp/boxwarden-host-directory-capability-runtime
    mkdir -p "$BW_HOSTDIR_SPIKE_ROOT/source"
    scripts/spike/bootstrap-tart.sh run-ro-share \
      boxwarden-m1a-spike-owned-vm \
      "$BW_HOSTDIR_SPIKE_ROOT/source" \
      "$BW_HOSTDIR_SPIKE_ROOT/serial"

The behavioral tests use a fake Tart executable and assert the exact
read-only argv, the unchanged network/audio/clipboard/serial flags, rejection
of a source-root symlink, and rejection of a source outside the synthetic root.

## Exact experimental command core

The complete raw transcript was intentionally not committed because it contains
host runtime identities. The following is the command core used against the
synthetic paths. Every variable was first checked as an absolute descendant of
the dedicated spike root, and every mutation result was captured with its exit
status instead of allowing an expected denial to abort the sequence.

Inside the guest:

    sudo install -d -m 0755 /mnt/boxwarden-host-tree
    sudo mount -t virtiofs \
      boxwarden-host-tree-v1 \
      /mnt/boxwarden-host-tree
    findmnt -T /mnt/boxwarden-host-tree

The mutation matrix was executed once as boxwarden and once through sudo sh -c
with the same exact target names:

    printf mutation >> /mnt/boxwarden-host-tree/ordinary
    : > /mnt/boxwarden-host-tree/ordinary
    touch /mnt/boxwarden-host-tree/created
    mkdir /mnt/boxwarden-host-tree/created-dir
    mv /mnt/boxwarden-host-tree/ordinary \
       /mnt/boxwarden-host-tree/renamed
    rm /mnt/boxwarden-host-tree/ordinary
    chmod 0777 /mnt/boxwarden-host-tree/ordinary
    chown 0:0 /mnt/boxwarden-host-tree/ordinary
    ln -s /etc/passwd /mnt/boxwarden-host-tree/guest-symlink
    ln /mnt/boxwarden-host-tree/ordinary \
       /mnt/boxwarden-host-tree/guest-hardlink
    setfattr -n user.boxwarden.guest -v mutation \
      /mnt/boxwarden-host-tree/ordinary
    setfattr -x user.boxwarden.synthetic \
      /mnt/boxwarden-host-tree/ordinary

The root remount probe was:

    sudo mount -o remount,rw /mnt/boxwarden-host-tree
    findmnt -T /mnt/boxwarden-host-tree
    sudo sh -c \
      'printf remount-mutation >> /mnt/boxwarden-host-tree/ordinary'

Immediately after each group, the host recomputed file bytes, modes, link
topology, xattrs, and ACLs and compared them with the saved synthetic manifest.

The namespace-replacement probe, with SOURCE already validated under the spike
root, was:

    mv "$SOURCE" "$SOURCE.open-object"
    mkdir "$SOURCE"
    printf replacement > "$SOURCE/replacement-only"
    # Probe the existing guest mount here.
    rmdir "$SOURCE"
    mv "$SOURCE.open-object" "$SOURCE"
    # Probe the same existing guest mount again.

The live-child probe replaced only a synthetic child:

    mv "$SOURCE/nested/child" "$SOURCE/nested/child.original"
    ln -s "$SYNTHETIC_SIBLING" "$SOURCE/nested/child"
    # Probe link visibility and following from the guest here.
    rm "$SOURCE/nested/child"
    mv "$SOURCE/nested/child.original" "$SOURCE/nested/child"

Baseline candidates were created into unique empty destinations with:

    ditto --clone "$SOURCE" "$DESTINATION"
    cp -ac "$SOURCE/." "$DESTINATION/"
    cp -a "$SOURCE/." "$DESTINATION/"
    ditto --noclone "$SOURCE" "$DESTINATION"
    rsync -aEHS "$SOURCE/" "$DESTINATION/"
    tar -c --format pax --mac-metadata --acls --xattrs --read-sparse \
      -C "$SOURCE" . |
      tar -x -p --mac-metadata --acls --xattrs -S -C "$DESTINATION"

Host allocation was sampled with df and directory physical allocation before
and after each fresh destination. Fidelity was checked independently of command
status; this is how cp -ac, rsync, and ditto --noclone were rejected despite
appearing superficially successful.

The actual VirtioFS lower was mounted with:

    sudo install -d -m 0755 \
      /srv/boxwarden/upper \
      /srv/boxwarden/work \
      /srv/boxwarden/merged
    sudo mount -t overlay overlay \
      -o lowerdir=/mnt/boxwarden-host-tree,upperdir=/srv/boxwarden/upper,workdir=/srv/boxwarden/work \
      /srv/boxwarden/merged
    findmnt -T /srv/boxwarden/merged

The Git exercise, after guest-local ownership normalization and installation of
Ubuntu's signed Git package in the disposable clone, was:

    git -C /srv/boxwarden/merged status --short
    printf proposal >> /srv/boxwarden/merged/tracked.txt
    git -C /srv/boxwarden/merged diff --check
    git -C /srv/boxwarden/merged add tracked.txt
    git -C /srv/boxwarden/merged \
      -c user.name=Boxwarden-Spike \
      -c user.email=spike.invalid \
      commit -m synthetic-proposal

The destructive guest sequence targeted only the merged path after checking
its mount identity:

    findmnt -T /srv/boxwarden/merged
    sudo chmod -R 000 /srv/boxwarden/merged
    sudo chmod -R u+rwX /srv/boxwarden/merged
    sudo find /srv/boxwarden/merged -mindepth 1 -delete

The lower and host manifests were checked again after this sequence and after a
same-VM stop/start. A new disposable VM received a fresh guest-local
upper/work pair against the same B.

## Read-only VirtioFS qualification

### Synthetic source

The source included:

- ordinary mode-0640 data;
- a mode-0755 executable;
- nested, hidden, Unicode, and case-distinct names;
- a hard-linked file pair;
- user and macOS xattrs;
- an internal relative symlink;
- a symlink whose target was outside the authorized root;
- a sibling sentinel outside the shared root.

Host manifests were captured before and after guest-user, guest-root, remount,
namespace-replacement, live-update, stop/start, and destructive tests.

### Results

| Property | Ordinary user | Guest root | Host result |
| --- | --- | --- | --- |
| Read exact tree | Allowed when presented ownership/mode allowed it | Allowed | Expected bytes |
| Append/truncate existing file | Denied | Denied | Unchanged |
| Create file or directory | Denied | Denied | No object created |
| Rename or unlink | Denied | Denied | Unchanged |
| chmod/chown | Denied | Denied | Metadata unchanged |
| Create symlink or hard link | Denied | Denied | No object created |
| Set/remove xattr | Denied | Denied | xattrs unchanged |
| Remount writable | No authority | Command returned success as root, but writes still failed | Still read-only at host boundary |

Linux reported the VirtioFS mount as rw,relatime even though the Tart attachment
was read-only. A root remount command also returned success and still reported
rw. Those mount-table strings are not the security property. All mutating
operations continued to fail with permission or operation-not-permitted errors,
and the host manifest stayed unchanged. Validation must exercise behavior, not
infer it from the guest mount flags.

### Authority bounds and path escapes

- The source parent and synthetic sibling were not visible through the mount.
- Guest-created escape symlinks could not be created because the share was
  read-only.
- The internal host symlink resolved when its target remained inside the
  authorized tree.
- The outside-target host symlink was visible as a link, but following it failed
  with ENOENT; it did not expose the sibling.
- Replacing a live child on the host with a symlink to the sibling made the link
  itself visible immediately, but following it still failed.
- The spike launcher rejected a symlinked source root before invoking Tart.
- A nested source did not expose its parent.

The observed capability was the selected directory tree, not an ambient host
filesystem namespace.

### Namespace replacement after attachment

The trusted host renamed the authorized source root and placed a different
directory at the original pathname. The existing guest mount did not follow
the pathname to the replacement. Directory access failed with EPERM. Restoring
the original directory at the pathname restored access.

This is consistent with a share bound to the originally opened directory
object, not a pathname re-resolved for every operation. It failed closed in the
tested case. Boxwarden should still reject source-root symlinks and should not
treat trusted-host namespace mutation during a session as supported operator
behavior.

### Presentation semantics

- File modes and executable bits were preserved.
- The hard-link pair retained common inode identity and link count.
- Host uid 501/gid 0 appeared as guest uid/gid 1000 in the observed mount.
- com.apple.provenance and user.boxwarden.synthetic xattrs were visible but
  immutable.
- Case lookup remained insensitive, reflecting the APFS source.
- Live host additions and byte changes appeared immediately.
- A stop and relaunch with the same read-only capability succeeded.
- The attachment was a per-run argument and was not persisted in the Tart VM
  configuration.

These presentation details are backend/filesystem facts, not portable Boxwarden
semantics. A session must not infer confidentiality or executable safety from
the presented uid alone.

### Tart observation anomaly

During one launch, tart list --format json reported the VM as stopped while its
disk and NVRAM timestamps advanced and the hvc0 console was interactively
available through the retained Screen session. An immediate detached Screen
hardcopy was empty even though direct attachment showed the console.

Production reconciliation must not treat either Tart's list field or a single
detached Screen scrape as conclusive in isolation. This is consistent with the
existing Task 0 requirement to reconcile from qualified observations rather
than a reusable PID or one optimistic state bit.

## Stable baseline investigation

### Representative tree

The benchmark tree contained 10,046 entries, including:

- 1 GiB of incompressible random content;
- an 8 GiB logical sparse file occupying 64 filesystem blocks;
- an executable;
- a symlink;
- a hard-link pair;
- xattrs and an ACL;
- Unicode names and APFS case behavior;
- a synthetic Git repository.

No hard-link farm was used. A FIFO was tested only as a rejection case and was
removed before accepted measurements.

### Results

| Mechanism | Time | Approximate incremental allocation | Fidelity/result |
| --- | ---: | ---: | --- |
| ditto --clone | 1.58–1.79 s | 0–3 MiB measurement noise | Preserved hard links, sparse allocation, mode, xattr, ACL, symlink, Unicode, and bytes |
| cp -ac | 1.51 s | Cheap | Split source hard links; rejected for correctness |
| cp -a | 4.22 s | 1,092,716 KiB | Preserved sparse/xattr/ACL/bytes; split hard links |
| pax/bsdtar stream | 7.17 s | 1,108,232 KiB | Preserved hard links, sparse allocation, xattr, ACL, and bytes |
| ditto --noclone | 9.14 s | 10,541,784 KiB | Expanded sparse data and failed ACL preservation |
| macOS rsync 2.6.9 -aEHS | 53.61 s | Not accepted | AppleDouble errors/extras despite exit 0 |

The accepted APFS fast path is a tree clone using ditto --clone. The accepted
ordinary-filesystem fallback candidate is a pax-format bsdtar stream:

    tar -c --format pax --mac-metadata --acls --xattrs --read-sparse \
      -C "$SOURCE" . |
      tar -x -p --mac-metadata --acls --xattrs -S -C "$DESTINATION"

This command is evidence, not a production implementation. Production must
check both process statuses rather than relying on the final pipeline status.

Source and clone writes were independent, satisfying the requirement that the
baseline and live source not share logical file-content identity. ditto's clone
did not preserve symlink mtime; directory and symlink timestamps should not be
part of the initial semantic equality contract.

### Special files and concurrent mutation

A FIFO in the source caused ditto --clone to block for more than 42 seconds
until the exact process group was interrupted. Baseline admission must reject
FIFOs, sockets, devices, and other non-regular special objects before copying.

Three clones made while a two-file generation pair was being mutated all
returned status 0 yet captured different generations for the pair. A recursive
directory clone is not an atomic tree snapshot. A candidate baseline must be
semantically compared with the intended host source after copying. On mismatch,
discard and retry or require an explicitly quiesced source. Do not advertise an
APFS directory clone as a transactional snapshot.

Running git status against a stable clone modified its .git/index. Baseline B
must remain a sealed comparison object; tools that may refresh metadata operate
on another materialization or on G, not on B.

## Guest-local OverlayFS feasibility

The actual VirtioFS mount was used as lowerdir. Guest-local ext4 directories
were used for upperdir and workdir, with a normal merged project path.

Linux 7.0 OverlayFS accepted this combination. The following operations worked
in the merged tree while leaving the shared baseline and live host source
unchanged:

- read, modify, add, delete, and rename;
- mkdir and rmdir;
- chmod and executable-bit changes;
- symlinks;
- file and directory renames;
- file-to-directory and directory-to-file replacement;
- timestamp changes;
- a 32 MiB file;
- 2,000 small files;
- a 64-level deep tree;
- Unicode names;
- a root-created FIFO and device node.

The final item is a negative admission result: the guest user could not create
a device, but guest root could. Materialization and any future promotion must
reject devices, FIFOs, sockets, setuid/setgid files, and other special objects.

### Ownership caveat

Initial lower files appeared as uid/gid 1000 in stat output, but OverlayFS
permission checks still prevented the workstation user from reading some
mode-0640 files and using the Git repository. Copy-up performed by root could
also leave root-owned upper objects inaccessible to the user.

A guest-local recursive normalization:

    sudo chown -hR boxwarden:boxwarden "$MERGED_PROJECT"

made the synthetic project usable. On the small Git fixture it created 33 upper
entries in 0.196 seconds. On a large tree this may create extensive upper
metadata or copy-up work. A production design must choose and qualify an
ownership strategy; it must not rely on the uid presented by VirtioFS.

The Task 0 golden also lacked Git. Installing Ubuntu's signed git 2.43.0 into
the disposable spike VM allowed git status, diff, add, and commit to succeed
after ownership normalization. This is a guest-definition/tool-availability
gap, not an OverlayFS failure.

These caveats are why the writable proposal is **EXPERIMENTAL ONLY**, despite
functional correctness in the bounded spike.

## Destructive and lifecycle results

Inside only the merged synthetic tree, the spike exercised recursive chmod,
exact recursive deletion, directory replacement, symlink chains, deep and
Unicode paths, and root-created special files. The merged tree became empty as
requested. The stable lower baseline and live host source retained their
original bytes and metadata.

The guest-local upper survived stop/start of the same VM, including 2,087
entries, the 32 MiB file, whiteouts, and special objects. A fresh VM from the
same baseline had no prior upper/work/merged state and saw the original source.
Destroying the first VM removed its overlay with the disposable disk. A stable
baseline therefore has to outlive every session using it, while upper/work
directories have exactly the owning VM's lifetime.

Abnormal host/control-process failure was not injected because the share is
read-only and the safe question is primarily cleanup/reconciliation. Production
must keep the baseline immutable, record the session-to-baseline reference
before VM mutation, and use reference-aware cleanup after backend observation.

## Trusted B/G comparison

Definitions:

- B is the sealed stable session baseline.
- G is a materialized guest proposal tree, not raw OverlayFS upperdir internals.
- H is the current durable host tree at review time.

The representative comparison used mtree semantic manifests containing object
type, mode, uid/gid, link count, size, symlink target, flags, file digest, xattr
digest, and ACL digest. Each 10,046-entry/1-GiB tree produced a 40,224-line,
roughly 2.4-MiB manifest.

| Operation | Time |
| --- | ---: |
| Build one semantic manifest | 6.45–7.63 s |
| Compare already materialized manifests | approximately 0.01 s |
| Uncached B and G comparison | approximately 14 s |
| G comparison with cached sealed-B manifest | approximately 7 s |

One synthetic proposal produced a 26-line, 1,734-byte semantic delta. Option A,
a trusted host-derived B-to-G comparison, is practical and much simpler than
making OverlayFS whiteouts or OCI layer internals part of the portable contract.

The spike used SHA-1 as mtree's internal test digest and SHA-256 over the
resulting spec. Production must instead define one canonical, versioned,
strong-hash manifest (at least SHA-256) and terminal-safe rendering.

## Proposed concurrency semantics

The first design review should evaluate:

    if semantic(H) == semantic(B):
        proposal may be eligible for explicit review and later promotion
    else:
        fail closed; require explicit reconciliation

The guest's manifest is evidence at most. The trusted host derives B, G, H, and
the semantic delta over the exact bytes it receives or reads. No generic
filesystem merge engine is proposed. Git-backed projects continue to use
Git-native commits, diffs, branches, and remotes.

Promotion remains unimplemented. A later design must define admission against
absolute/outside symlinks, case collisions, hard-link boundary changes, special
files, file-count and byte limits, terminal control characters, concurrent
host mutation, atomic replacement, rollback, and exact review binding.

## Reproduction procedure

The high-level sequence was:

1. Inventory tart list --format json and the Tart object directory without
   changing any object.
2. Create a unique synthetic root and capture a semantic manifest.
3. Create a spike-owned VM, launch it with run-ro-share, and mount the fixed
   VirtioFS tag.
4. Run the read and mutation matrix as uid 1000 and with passwordless sudo.
5. Recompute the host manifest after each destructive group.
6. Exercise inside/outside symlinks, nested paths, live child replacement, and
   whole-source pathname replacement.
7. Stop/relaunch and repeat a read/write-negative smoke test.
8. Generate the representative host tree and benchmark each baseline into a
   unique empty destination while measuring wall time and host allocation.
9. Build lower/upper/work/merged in the guest, run the functional/destructive
   matrix, stop/start, and compare all three host-side trees.
10. Materialize G independently of OverlayFS internals and benchmark trusted
    semantic manifests for B, G, and H.
11. Stop and delete only spike-owned VMs, remove only the synthetic root and
    relays, then repeat the Tart/process inventory.

Every destructive command used a previously validated exact path below the
synthetic root or the disposable VM. No wildcard or unresolved environment
variable selected a deletion target.

## Acceptance boundaries

### What the spike proves

- Tart 2.32.1 can expose one exact canonicalized synthetic tree read-only such
  that guest root can read it but could not persist host writes or broaden
  readable authority in the tested environment.
- A normal guest-local writable tree can be constructed with OverlayFS over the
  actual VirtioFS lower and can contain destructive root behavior.
- APFS CoW tree clones are fast and cheap for a meaningful tree, with a
  correctness-preserving ordinary-copy fallback.
- Trusted semantic B/G comparison is operationally reasonable.

### What it does not prove

- arbitrary or sensitive host paths are safe to disclose;
- Tart versions other than 2.32.1 have the same behavior or stability;
- writable VirtioFS is safe;
- current OverlayFS ownership handling is production-ready;
- directory cloning is an atomic snapshot;
- promotion/write-back is safe or accepted;
- Linux, cloud, or future backends use the same mechanism.

The result is ready for a separate, test-first M1A implementation pass only
after the proposed ADR is reviewed. It does not modify accepted architecture
by itself.
