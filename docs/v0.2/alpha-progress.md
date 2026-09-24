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
| Workspace volume | One alpha-owned 64 MiB ext4 volume was formatted in a zero-NIC VM and independently inspected. Its synthetic file retained its digest across an earlier stop/restart after manual remount. Public detach and attach moved this exact volume from a stopped original system clone to a separate replacement system clone without copying the disk. The replacement guest file manager opened the mounted 57-byte synthetic file and displayed its expected content; the known bytes match the previously recorded SHA-256. Public stops cleared exact generation Use while preserving the volume identity. Offline export qualification remains pending. |
| Automatic mount and READY | The static generic guest helper resolves an exact FS UUID, mounts ext4 at a validated path, checks existing mounts and read-write state, and probes exact bindings. The host owner derives those bindings from admitted session/Use records, ensures them before READY, and repeats the bound probe for status. Full local Go tests, focused race tests, vet, and artifact checks passed at `5c84520`. The fresh original clone reached mount-bound READY in two generations; the separate replacement clone also reached mount-bound READY with the same reattached volume and later exposed the expected file in its GUI. Both are stopped now. |
| Controlled export | Bounded host stream receiver and zero-NIC synthetic inspector boot/transport have tests. The inspector mounted a private 64 MiB ext4 copy read-only with journal replay disabled and reported the exact digest and size of one fixed file. After a rejected first run exposed TTY CRLF corruption, a fresh run passed strict 573-byte stream parsing, zero-NIC/stopped/helper checks, and unchanged disk identity and SHA-256. The standalone hardlink gate rejects a linked disk and admits a valid one-link control. A source-level stopped-volume snapshot transaction now copies qualified bytes under locks and journals exact identity before clearing Pending; focused tests cover success, running-backend rejection, changed source, and partial-copy cancellation. Real managed-volume and public export remain pending. |
| Example | `examples/v0.2-alpha-base.json` passed public recipe/ISO validation. It requests the Desktop source, a small package set, and one named workspace intent. |
| Recipe-bound create | `session create` accepts exact recipe/ISO/guest definition/tool inputs, prepares or reuses a qualified base, and calls `CreateFromRevision`. A focused fixture and the real public command both selected the newly prepared revision without changing the domain current golden. A later public `alpha prepare` reused the admitted cache after verified redundant installer staging was retired, without creating an attempt journal or new Tart object. |

Hosted CI is unavailable. Local checks above are source or explicitly described
real-host checks; the full graphical, rebuild, and export
acceptance path remains open.

## Current work

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
The current increment connects the strict private transaction journal to
stopped-volume snapshot copying and Pending clearance. It admits a qualified
source, verifies a distinct copy and source digest, and leaves Pending plus a
copying journal on partial cancellation. These are local source tests; the
managed volume has not yet passed public export. Exact crash recovery,
closed-stream inspection, and public export remain to implement. The earlier
failed base attempt was corrected and a fresh base built and qualified; failed
attempts remain outside the prepared cache.

## Next actions

1. Implement the journaled stopped-volume snapshot transaction, exact
   lock-held recovery, and host checks before receiver publication; then
   qualify it with the synthetic volume and hostile exits.
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
