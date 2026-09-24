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
| Controlled export | Bounded host stream receiver and zero-NIC synthetic inspector boot/transport have tests. A source-only extension can mount a private 64 MiB ext4 copy read-only with journal replay disabled and report the digest of one fixed bounded file. Review found a standalone hardlink admission gap; the corrected gate rejects a hardlink and admits a private one-link control. Focused Go/Python tests, Swift typecheck, and shell syntax checks passed; a live copy boot, real stopped-volume export, and public export remain pending. |
| Example | `examples/v0.2-alpha-base.json` passed public recipe/ISO validation. It requests the Desktop source, a small package set, and one named workspace intent. |
| Recipe-bound create | `session create` accepts exact recipe/ISO/guest definition/tool inputs, prepares or reuses a qualified base, and calls `CreateFromRevision`. A focused fixture and the real public command both selected the newly prepared revision without changing the domain current golden. A later public `alpha prepare` reused the admitted cache after verified redundant installer staging was retired, without creating an attempt journal or new Tart object. |

Hosted CI is unavailable. Local checks above are source or explicitly described
real-host checks; the full graphical, rebuild, and export
acceptance path remains open.

## Current work

The corrected public recipe-bound create admitted one fresh prepared base and
created a session from its exact revision. After verified redundant installer
staging was removed, a repeated public prepare reused that cache. The public
workspace attach bound the existing volume to the stopped session. Start,
fresh READY status, stop, second start, fresh READY status, and second stop all
succeeded. The two Use generation IDs differed and both stops cleared Use.
Public recipe-bound create then made a separate stopped system clone from the
admitted base. Public detach and attach moved the Available volume to it
without copying the disk. The replacement reached fresh mount-bound READY;
its public stop cleared Use. The volume remains attached and Available, and
both system clones are stopped. READY includes the exact writable ext4 mount
probe. A subsequent replacement boot opened the mounted synthetic file in
the guest file manager; its displayed text and 57-byte size match the known
payload whose SHA-256 was recorded before transfer. The session was stopped
again. This checks content in the replacement guest; it is not an offline
inspector or export qualification.

The first replacement create input had an incorrect supplied OpenSSL digest;
strict preflight rejected it before any attempt journal, session, or VM was
created. The subsequent run used freshly observed digests for both tools and
succeeded. This was an input error, not a qualified-build failure.

The first synthetic-copy inspector preparation stopped at no-start preflight:
Foundation aliased `/private/tmp` to `/tmp` in the new standalone disk gate.
The gate now checks the exact supplied lexical path; a focused test verifies
both hardlink rejection and valid one-link admission. No inspector VM started
in that failed preparation.

The previous public recipe-bound attempt reached the generic base installer
and guest preparation. Finalization then timed out after ten minutes waiting
for the clone-ready marker. Its attempt journal records failure, and no session
or prepared-cache record was
admitted. This failed run is investigation evidence, not a qualification
checkpoint or resumable build. Two bounded read-only, zero-network forensic
boots confirmed that the fixed finalizer command was invoked before any
cleanup, and that the installed helper digest differed from the finalizer's
embedded pin. The finalizer pin now matches the installed, artifact-locked
helper. Its existing real-artifact fixture failed before the correction and
passed afterward; remaster, render, bootstrap, and recipe helper checks also
passed. These are source checks, not real build qualification. The stopped
failed VM was byte-verified into the private archive before its live object
was retired; its journal and forensic reports remain.

An earlier fresh attempt was canceled by the disk-reserve guard during
installation. Its failed journal and verified private VM archive are retained.

Exact obsolete alpha-owned r1 VMs and three old-helper r2 clones were retired
after stopped, ownership, and workspace checks. The r2c system VM was archived
and byte-verified privately before retirement; the independent workspace was
detached through the public CLI and remains intact. Failed remastered installer
ISOs are preserved as private exact block-map representations against the
retained, digest-verified Canonical input. Each reconstructed stream matched
its original SHA-256 digest after redundant staging copies were removed. Failed
journals and other private diagnostics remain. The two older alpha builder
installer ISOs are also retained as private, SHA-256-verified exact block maps
against the pinned Canonical source. The admitted successful attempt's staged
source and installer ISOs were likewise verified as an exact block map before
and after their redundant copies were retired. Journals and qualification
evidence remain. The current r2 golden VM and selection record are intact.
Free space was about 41.5 GiB before the successful build and is about 29 GiB
after staging retirement; read-only host doctor remains healthy.

Automatic approval review rejected deletion of the alpha domain's current r2
golden because ordinary legacy session creation still points to it. It remains
stopped and intact. The protected historical VM and archive were untouched.

Recipe-bound `session create` selected the exact admitted prepared revision
without changing the domain's current golden. The legacy form still uses that
current golden.

## Next actions

1. Qualify the new bounded zero-NIC ext4 inspection on a synthetic copy, then complete
   controlled stopped-volume export with exact ownership, locking, output
   limits, and recovery checks.
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

The private ownership manifest records exact live resources and evidence
locations. Before any further VM mutation, verify that manifest against Tart,
check host doctor, RAM, and the free-space floor. No human action is currently
required; later account sign-in and subjective GUI acceptance belong to Wes.
