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
