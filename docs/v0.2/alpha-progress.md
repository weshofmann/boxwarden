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
| Workspace volume | One alpha-owned 64 MiB ext4 volume was formatted in a zero-NIC VM, independently inspected, and attached through the public CLI to a stopped session. Tart exposed it as a distinct ext4 disk on two READY generations. A synthetic file retained its digest across stop/restart after manual guest remount. It is now publicly detached, Available, and retains its exact disk identity. This proves disk persistence, not automatic guest mounting. |
| Automatic mount and READY | The static generic guest helper resolves an exact FS UUID, mounts ext4 at a validated path, checks existing mounts and read-write state, and probes exact bindings. The host owner derives those bindings from admitted session/Use records, ensures them before READY, and repeats the bound probe for status. Full local Go tests, focused race tests, vet, and artifact checks passed at `5c84520`. A new real guest has not exercised these bytes. |
| Controlled export | Bounded host stream receiver and zero-NIC synthetic inspector boot/transport have tests. Read-only ext4 inspection and real stopped-volume export remain pending; public export is disabled. |
| Example | `examples/v0.2-alpha-base.json` passed public recipe/ISO validation. It requests the Desktop source, a small package set, and one named workspace intent. |
| Recipe-bound create | `session create` now accepts the exact recipe/ISO/guest definition/tool inputs, prepares or reuses a qualified base, and calls `CreateFromRevision`. A focused integration fixture proves the new session selects the prepared revision while the domain current golden stays unchanged. Source tests and vet passed; real cache qualification remains pending. |

Hosted CI is unavailable. Local checks above are source or explicitly described
real-host checks; the full graphical, automatic mount, rebuild, and export
acceptance path remains open.

## Current work and blocker

A fresh public base preparation from the checked example began on a new owned
candidate. A preflight-only attempt rejected a Homebrew xorriso symlink;
retrying with the digest-matched canonical executable started the installer.
The disk-reserve guard then canceled and stopped the candidate during
installation. Its failed journal and verified private VM archive are retained.
It is not a qualification checkpoint or resumable build; no cache was admitted.

Exact obsolete alpha-owned r1 VMs and three old-helper r2 clones were retired
after stopped, ownership, and workspace checks. The r2c system VM was archived
and byte-verified privately before retirement; the independent workspace was
detached through the public CLI and remains intact. Failed remastered installer
ISOs are preserved as private exact block-map representations against the
retained, digest-verified Canonical input. Reconstructed streams matched both
original SHA-256 digests after redundant staging copies were removed. Failed
journals and other private diagnostics remain. Free space is about 42.7 GiB,
and read-only host doctor is healthy.

Automatic approval review rejected deletion of the alpha domain's current r2
golden because ordinary legacy session creation still points to it. It remains
stopped and intact. The protected historical VM and archive were untouched.

Recipe-bound `session create` now selects the exact admitted prepared revision
without changing the domain's current golden. The legacy form still uses that
current golden. A real prepared cache entry is required before this new route
can be exercised on the host.

## Next actions

1. Recheck exact ownership, stopped Tart inventory, doctor, RAM, and disk
   headroom; run one fresh preparation from the corrected guest definition.
2. Qualify the fresh prepared base, then use public recipe-bound create/reuse
   and the revised guest helper with the existing workspace volume.
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
