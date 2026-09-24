# Boxwarden v0.2 alpha progress

Updated: 2026-09-24 UTC. Integration branch:
`weshofmann/feature/v02-alpha`; [Draft PR #12](https://github.com/weshofmann/boxwarden/pull/12).
The launch target is ten days from 2026-09-23 14:44 UTC. Stop starting new
work after 2026-10-03 14:44 UTC and leave a resumable handoff if unfinished.

## Verified checkpoints

| Area | Evidence and limit |
| --- | --- |
| Host and installer | Read-only host doctor is healthy. The Canonical Ubuntu 24.04.4 ARM64 Desktop ISO has a valid detached signature and exact pinned SHA-256. Pinned OpenSSL 3 and xorriso executables have been checked. |
| Public management | A fresh disposable clone reached exact-generation READY through serial bootstrap, host-key pinning, certificate, strict SSH, and time-zone checks; it stopped and restarted to READY. GNOME, Firefox, and a synthetic home file were observed after restart. |
| Reusable preparation | Strict versioned recipe and ISO checks, candidate build, guest preparation, fresh-clone qualifier, private evidence, and cache admission are implemented. A fresh real build using the corrected finalizer completed installation, guest preparation, clone-ready shutdown, and qualification. Its fresh clone reached READY, passed package-inventory and identity checks, and stopped; the versioned prepared record was admitted. This is base qualification, not the full workspace/export acceptance path. |
| Workspace volume | One alpha-owned 64 MiB ext4 volume was formatted in a zero-NIC VM and independently inspected. Its synthetic file retained its digest across an earlier stop/restart after manual remount. Public detach and attach moved this exact volume from a stopped original system clone to a separate replacement system clone without copying the disk. The replacement guest file manager opened the mounted 57-byte synthetic file and displayed its expected content. The corrected public stop released Use and left ext4 clean; source and export snapshot SHA-256 matched. |
| Automatic mount and READY | The static generic guest helper resolves an exact FS UUID, mounts ext4 at a validated path, checks existing mounts and read-write state, and probes exact bindings. The host owner derives those bindings from admitted session/Use records, ensures them before READY, and repeats the bound probe for status. Full local Go tests, focused race tests, vet, and artifact checks passed at `5c84520`. The fresh original clone reached mount-bound READY in two generations; the separate replacement clone also reached mount-bound READY with the same reattached volume and later exposed the expected file in its GUI. Both are stopped now. |
| Controlled export | Bounded receiver, exact stopped-volume snapshot, private helper capture, and selected publication have targeted tests. After clean guest shutdown, a real public export published one selected 57-byte regular file into a new private directory. Its SHA-256 matched the known synthetic guest file; the durable journal read `published`, and the source and snapshot digests matched. Earlier dirty-volume and low-headroom attempts failed without publishing and remain private evidence. Inspected-phase recovery and hostile-exit qualification remain. |
| Inspector bundle admission | A clean-source production bundle from the pinned ISO passed independent artifact checks and host admission with both synthetic and real journal-derived requests. Admission checks tracked source bytes, kernel/ISO pins, seven artifact digests and private metadata, deterministic appended guest/request initrd, and the signed helper's sole Virtualization entitlement. The first live export-mode VM rejected an unclean volume; a later fresh clean-volume run completed selected publication with zero-NIC inspector evidence. |
| Example | `examples/v0.2-alpha-base.json` passed public recipe/ISO validation. It requests the Desktop source, a small package set, and one named workspace intent. |
| Recipe-bound create | `session create` accepts exact recipe/ISO/guest definition/tool inputs, prepares or reuses a qualified base, and calls `CreateFromRevision`. A focused fixture and the real public command both selected the newly prepared revision without changing the domain current golden. A later public `alpha prepare` reused the admitted cache after verified redundant installer staging was retired, without creating an attempt journal or new Tart object. |

Hosted CI is unavailable. Local checks above are source or explicitly described
real-host checks; the full graphical, rebuild, and failure-recovery
acceptance path remains open.

## Current work

The managed-volume shutdown correction at `7eeba47` now passes one real
attached-volume public stop: the session and backend are consistently stopped,
Use is released, and ext4 `needs_recovery` is clear. A fresh public export
published the selected 57-byte synthetic file; host verification matched its
known SHA-256 and confirmed that the durable journal is `published` and the
source and snapshot digests agree. A preceding attempt stopped at the host
headroom guard after inspector capture; its destination stayed empty and its
snapshot-ready transaction is preserved. Removing only a rebuildable task Go
cache restored headroom for the fresh run.

Tart 2.32.1 still reports `stopped` for this running graphical guest after
READY. Exact supervisor/Tart/Virtualization processes, the attached disk,
guest IP, SSH port, and Tart window proved the guest was live. Status therefore
reports false drift during such runs. Correcting this observed-state contract
without accepting a false READY is the active implementation problem. The
following paragraphs retain the implementation sequence; earlier pending
statements describe their checkpoint at the time.

The admitted prepared base has been reused by public recipe-bound create.
Public start, fresh mount-bound READY, and stop succeeded twice with separate
Use generations. The independent ext4 volume was detached from the stopped
original clone and attached to a separate stopped replacement without copying
or reformatting it. The replacement reached READY and opened the expected
synthetic file in its graphical file manager; it stopped again and cleared Use.
Both system clones are stopped and the volume remains Available and attached.

A fresh isolated inspector probe read a private copy of that synthetic ext4
volume through a restrictive read-only mount. The exact typed report, zero-NIC
and stopped-VM checks, helper reap, and unchanged disk check passed. Earlier
rejected attempts exposed a path-normalization false rejection and binary TTY
output conversion; both were corrected and rechecked on fresh inputs. This
qualifies the private-copy probe, not public stopped-volume export.

An independent export design review found publication, recovery, and resource
bounds that must be enforced. `af857ec` admits a durable `export-snapshot`
Pending marker and verifies that interrupted copying blocks start and detach.
The strict private transaction journal is connected to stopped-volume snapshot
copying and Pending clearance. It admits a qualified source, verifies a
distinct copy and source digest, and leaves Pending plus a copying journal on
partial cancellation. Snapshot-stage recovery requires the same stopped
backend and exact binding, removes only recognized partial state, and records
an aborted phase before clearing Pending. A ready-journal retry rechecks the
snapshot bytes before clearing Pending. Targeted source tests include the
crash windows between cleanup, journal advancement, and marker clearance.
The host capture gate enforces an 80-second helper deadline, a 320 MiB stream
cap, a 16 KiB diagnostic cap, disk-reserve sampling, private ACL admission,
and exact stopped/zero-NIC/byte-count evidence before exposing a closed spool.
Its tests use a synthetic subprocess. It is not yet connected to an admitted
production inspector or the receiver, and no exported tree has been published.
The generic guest-side selected-file writer now serializes regular files and
directories into the receiver's typed format, with path, type, link, count,
and byte limits. A local round-trip test passed through the host receiver.
An exact transaction-bound request parser and optional private initramfs data
member now carry selected paths, expected ext4 UUID, and disk size without a
host share or network. The guest boot code now reads the root-owned request,
checks disk size and ext4 UUID, streams selected files from a restrictive
read-only mount only after rejecting error, orphan, and recovery-required ext4
states, unmounts, and only then emits the terminal record. Targeted
tests and a Linux ARM64 cross-build pass; this branch has not yet had a live
export-mode inspector boot, and the host control plane does not generate the
request bundle yet.
The Swift helper now provides distinct export preflight and boot commands
that check the journal's transaction-bound snapshot path, device, inode, and
size, retain the zero-NIC/read-only VM configuration, and report export-mode
host evidence.
The host capture parser accepts the marker; the production caller must still
require it. Local disk-admission tests reject identity, transaction, hardlink,
and private parent-mode drift. No live export-mode VM has run.
The host can now derive the guest's transaction request only from a
snapshot-ready durable journal after re-admitting and hashing the private
snapshot. Tests reject a copying journal, another domain, a missing or short
snapshot, and changed snapshot bytes. This preparation does not create an
initramfs bundle or authorize publication.
The host receiver now has an optional exact selection gate. When supplied
the journal's selection, it admits only selected paths and necessary parent
directories, requires every selected path to appear, and rejects an extra
sibling before publication. Host orchestration must supply the journal
selection; no public export invokes the receiver yet.
Explicit capture and receive entry points now fail closed if the helper lacks
export-mode stopped-host evidence or the receiver lacks a host selection.
The former removes a rejected synthetic spool. They remain disconnected from
production artifact admission and journal phase transitions.
The source builder for a private per-transaction inspector bundle passed one
test-only full build from the pinned ISO. Independent checks rehashed all
manifested files and parsed the appended guest and request cpio members.
The ordinary builder refused the dirty checkout. A clean-source build then
passed from published `b51d900`; its source and ISO pins, seven file
digests, signed helper, ARM64 guest type, and appended guest/request members
were independently checked. It used a synthetic private request.
Live export boot remains pending.
The builder now also records the exact inventory and digests of 16 tracked
inspector build inputs. A test-only and a clean-source production full build
passed, and independent checks matched every recorded input and artifact.
The host admission gate now enforces this inventory rather than requiring the
same commit SHA, so a docs-only commit does not invalidate executable artifact
identity. A focused fixture rejected a changed request, source input, and
extra initrd bytes. The production bundle with a synthetic request passed
read-only host admission from the clean integration checkout. A real
journal-derived request and managed-volume export remain unqualified.
The journal-bound capture entry point now holds the exact transaction lock
through request derivation, production bundle admission, export-mode helper
reap, and post-run snapshot rehash. A subprocess fixture proved bound launch
arguments and rejected a changed snapshot without exposing a spool. The
publication entry point re-admits the spool, snapshot, and destination parent;
persists `inspected`; gives only the journal selection to the bounded receiver;
and persists `published` after a complete receiver result. A fixture published
the selected file to a new directory; an extra path and a changed snapshot
left no published tree. These tests use a synthetic helper and stream. An
ambiguous post-rename failure returns the exact final path and leaves the
journal `inspected` for explicit reconciliation; that recovery path remains.
The host now builds a private bundle from exact journal-derived request bytes
under the transaction lock, re-admits it, and returns an inode-bound cleanup
receipt. The real controlled builder invocation from clean source passed and
its synthetic-request output passed production admission. A rejected receiver
stream can be recaptured and retried from `inspected` while the exact
destination remains empty. `workspace export` composes snapshot, build,
capture, selected publication, and a final published-journal read for the
explicit `alpha` domain. Its CLI routing tests pass. The first managed-volume
public command preserved its snapshot and reached the inspector, but guest
ext4 admission rejected a recovery-required filesystem. The host capture had
accepted zero export bytes and the receiver rejected EOF before publication;
the new capture regression rejects that false pass and removes its spool.
Pinned Tart 2.32.1 source confirms that the previous `SIGINT` stop invoked an
immediate VM stop. Normal supervised stop now asks the retained Tart child for
guest OS shutdown, waits up to 15 seconds, and then uses the existing exact
force-stop/reap path if necessary. The first correction alone had not reached
attached-volume sessions because an outer handle wrapper hid that method.
The attached-volume launcher wraps that Tart handle once more to retain disk
locks through reap. That outer wrapper omitted the shutdown-request method,
so attached-volume stops still took the immediate force path. A regression
reproduced the missing method before the fix; the wrapper now forwards the
request while retaining its disk locks until exact reap. Focused Tart,
session-runtime, and supervisor tests plus targeted vet pass. A real public
stop then completed in 3.81 seconds and left the stopped volume ext4-clean.
Three subsequent public starts of the attached synthetic-volume sandbox
returned READY, but the next status reported Tart `stopped`. A bounded live
diagnostic reproduced the contradiction: the exact supervisor and Tart child,
the Virtualization process holding the managed disk, the graphical Tart window,
the guest IP, and SSH port 22 were all present while Tart `list` reported
`stopped`. This matches an earlier Tart 2.32.1 observation on this host. Each
generation was reconciled with the public exact stop; the volume Use was
released, but ext4 still reported `needs_recovery` at that point. Neither
durable READY nor clean guest shutdown was qualified by those earlier runs.
Tart's lock-derived state must be reconciled against exact retained live
evidence before status and READY acceptance can complete. Private process IDs,
paths, and disk evidence remain outside this document.
An independent source review scoped the correction to exact retained-child
liveness plus fresh bound guest checks; it warned that a contradictory Tart
listing never proves a VM stopped. The Tart direct-child handle now exposes a
read-only liveness signal through both scratch and managed-disk wrappers,
with focused tests for reaped and unprovable children. READY and status do not
yet consume that signal, so the false-stopped behavior remains open.
Focused export tests and vet pass. Repository-wide Go tests and vet previously
passed with host Unix socket access at `06466ee`; the default sandbox run
could not bind test sockets, and the architecture guard was narrowed for the
v0.2 export calls. The initial lifecycle correction passed the full local Go
suite, focused Tart/runtime/supervisor race checks, and targeted vet. The outer
managed-volume wrapper correction has only the focused checks recorded above.
Broader managed-volume acceptance remains open. The earlier failed base
attempt was corrected and a fresh base built and qualified; failed attempts
remain outside the prepared cache.

## Next actions

1. Correct the observed-state/READY contract for Tart's false `stopped`
   report using exact live supervisor and guest evidence, then verify it on
   the owned disposable sandbox. Add explicit inspected-phase recovery and
   hostile-exit checks to the now-successful public export path.
2. Attach the retained volume to a second fresh sandbox after export, then
   verify its mount and content.
3. Run the full integration checks and real acceptance matrix, and publish the
   runnable build, exact source SHA, quickstart, synthetic demo, and limits.

## Publication and operational policy

Publish each meaningful verified increment on the authorized alpha branch,
targeting a GitHub checkpoint every 30–60 minutes of active work. Push each
verified commit promptly and keep Draft PR #12 accurate at milestones. Targeted
checks support small commits; the full integration and acceptance checks are
separate gates. Preserve unfinished work, coordinate before staging another
worker's files, and keep credentials, raw host inventory, VM disks, and private
evidence out of Git and the PR. Never push or merge into `main`, force-update
published history, or bypass checks.

Before further VM mutation, verify owned resources, host doctor, RAM, and
the free-space floor. No human action is currently required; later account
sign-in and subjective GUI acceptance belong to Wes.
