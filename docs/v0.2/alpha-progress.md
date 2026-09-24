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
| Reusable preparation | Strict versioned recipe and ISO checks, candidate build, guest preparation, fresh-clone qualifier, private evidence, and cache admission are implemented. A previous real build reached guest preparation but failed finalization; the finalizer contract is corrected and tested. No prepared base has passed real qualification or entered the cache. |
| Workspace volume | One alpha-owned 64 MiB ext4 volume was formatted in a zero-NIC VM, independently inspected, and attached through the public CLI to a stopped session. Tart exposed it as a distinct ext4 disk on two READY generations. A synthetic file retained its digest across stop/restart after manual guest remount. This proves disk persistence, not automatic guest mounting. |
| Automatic mount and READY | The static generic guest helper resolves an exact FS UUID, mounts ext4 at a validated path, checks existing mounts and read-write state, and probes exact bindings. The host owner derives those bindings from admitted session/Use records, ensures them before READY, and repeats the bound probe for status. Full local Go tests, focused race tests, vet, and artifact checks passed at `5c84520`. A new real guest has not exercised these bytes. |
| Controlled export | Bounded host stream receiver and zero-NIC synthetic inspector boot/transport have tests. Read-only ext4 inspection and real stopped-volume export remain pending; public export is disabled. |
| Example | `examples/v0.2-alpha-base.json` passed public recipe/ISO validation. It requests the Desktop source, a small package set, and one named workspace intent. |

Hosted CI is unavailable. Local checks above are source or explicitly described
real-host checks; the full graphical, automatic mount, rebuild, and export
acceptance path remains open.

## Current work and blocker

A fresh public base preparation from the checked example began on a new owned
candidate. Its first preflight attempt rejected a Homebrew symlink for xorriso
before VM mutation; retrying with the digest-matched canonical executable
started the installer. The disk-reserve guard then canceled and stopped that
candidate while still in the installer phase. The failed journal and stopped
object are private evidence, not a qualification checkpoint or resumable build.
No cache was admitted. Free space is now close to the enforced reserve plus
stopping margin, so another build requires deliberate reclamation of exact
alpha-owned disposable resources and a fresh headroom check. The protected
historical VM and historical archive are outside that scope.

The public preparation path qualifies a candidate without changing the
domain's current golden. Ordinary `session create` still selects that current
golden. The next source increment will bind public recipe-based creation to
the exact admitted prepared revision, then exercise the new helper in a real
clone with the existing workspace volume.

## Next actions

1. Preserve the failed attempt journal and useful diagnostics; inspect exact
   alpha ownership and storage sharing, reclaim only disposable owned state,
   and rerun a fresh build only with enough projected disk headroom.
2. Add public recipe-bound create/reuse through the prepared cache and exact
   `CreateFromRevision` seam, with targeted tests and a published checkpoint.
3. Prove automatic ext4 mounting, stop/restart, system rebuild and workspace
   reattachment, stopped-volume export, then a second fresh sandbox.
4. Run the full integration checks and real acceptance matrix, and publish the
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
