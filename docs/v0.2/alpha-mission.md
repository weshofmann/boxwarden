# Boxwarden v0.2 autonomous alpha mission

Wes adopted the supplied *Boxwarden v0.2 — Autonomous Alpha Build Mission* as
the goal on 2026-09-23. The product vision is background context. This file
records the delegated engineering scope for the alpha branch so work can resume
without relying on a chat transcript or treating the vision as implemented.

## Deliverable

A local, runnable personal alpha on the dedicated development Mac supports a
selected Ubuntu Desktop ARM64 installer and versioned recipe, automatically
prepared and reusable base, graphical persistent sandbox with a private CoW
system disk, independent named workspace disk, software installation and
launch, stop/start, system rebuild retaining the workspace, reattachment to a
replacement sandbox, and controlled export into a new host directory. A fresh
synthetic run must repeat the essential path from tracked source. A Draft PR
against `main`, quickstart, evidence, limitations, and concise owner actions
complete the handoff; the branch is not merged or released by the agent.

## Delegated execution

The owner delegated routine alpha design, source correction, verification,
branch integration, owned disposable VM/disk operations, and verified
non-secret commits and pushes. Proceed without routine plan, correction, or
historical phase-gate approvals. Work independently when a sign-in, optional
application, GUI observation, or one workstream is blocked. Separate review is
required for meaningful new guest-to-host and destructive storage interfaces.

Use one integration branch and one operator for real VM mutations. Work from
the pinned baseline; leave the historical Phase 3 r1 candidate and earlier
evidence untouched. Use the admitted Tart 2.32.1/Softnet 0.19.0 pair and exact
`TART_HOME`, with separate alpha-only application state and exact ownership
records. Run read-only host doctor before real mutations; do not silently
repair host admission. Keep VM RAM initially within half physical RAM, at most
two VMs running, and free disk above max(20 GiB, 10% of volume capacity).

## Boundary and exclusions

Guest root is potentially malicious. The host grants no live directory share,
physical disk, credential bridge, Docker socket, display server, clipboard or
audio share, broad network access, or general host-command service. Extra
disks are only Boxwarden-managed files with exact ownership and locking. Guest
filesystems are never mounted in the macOS kernel. Guest output cannot choose
host paths, commands, credentials, or deletion targets. Exports reject unsafe
types, paths, collisions, and incomplete streams and never overwrite a host
destination. Synthetic data is used until final owner acceptance.

No new host privilege, network configuration, billing, subscription, release,
visibility, branch-protection, or `main` merge authority is delegated. Pause
only the dependent operation if such a change becomes necessary. Preserve
accurate reporting of the admitted Softnet vmnet-gateway exposure.

The alpha changes these v0.1 product rules deliberately: prepared bases are
internal and automatic, management authentication need not be exclusive inside
the guest, and independent workspace disks are part of the supported path.
The old implementations and qualification records remain historical evidence;
they are not v0.2 acceptance. Native package distribution, host-admin migration,
other OSes/backends, hot-plug, system overlays, RAM snapshots, collaboration
port exposure, automatic credential migration, and rich merge are deferred.

The active execution sequence and actual status are in
[`alpha-plan.md`](alpha-plan.md) and [`alpha-progress.md`](alpha-progress.md).
The current source-tracked operator commands are in
[`alpha-quickstart.md`](alpha-quickstart.md); that guide does not replace the
fresh acceptance evidence required before calling the alpha ready.

## Guest preparation diagnosis

The fixed guest preparation helper retains its latest command outcome at
`/var/lib/boxwarden/recipe-prepare-result.json`. Version 1 records the exact
preparation key, zero-based command index (including generated apt commands),
step ID, outcome (`started`, `nonzero`, `timeout`, `spawn-error`, or `complete`),
and exit code when available. `complete` has no command index or step ID.
The mode-0600 file is atomically replaced and file/directory synced before
execution or the terminal marker. It contains no child output, argv,
environment, or exception text. It is diagnostic guest state, never a trusted
qualification or cache-admission receipt. A crash can leave `started`; that
means indeterminate execution, not success or permission to rerun a failed
qualification candidate.

The standalone pinned ChatGPT installer similarly retains
`/var/lib/boxwarden/chatgpt-install-result.json`: version, pinned package
digest, last started fixed stage, outcome (`started`, `failed`, `complete`),
failure category, and child exit code when available. Stages cover source
policy, temporary directory, download, package identity, repository policy,
package install, installed identity, final source policy, and cleanup.
It uses the same private atomic file/directory-sync discipline, excludes
exception text and child data, and carries no qualification authority.

## Operator storage direction (2026-09-26)

Wes authorized migration of the existing Tart image store to a native encrypted
APFS volume at the next safe transition, superseding the earlier hold for a
separate disk. Finish or bound the current owned storage operation first;
migration takes priority before another VM boot or expensive fresh baseline.
Preserve the admitted canonical Tart mount, historical disk bytes and journals,
clone relationships, durable storage-identity provenance and noninteractive
unlock handling. Review the concrete capacity and cutover plan independently.
Do not erase existing volumes or containers, delete unrelated data, assume
compaction savings, or relax the free-space floor to make staging fit. A
verified migration is infrastructure preparation, not alpha qualification.
