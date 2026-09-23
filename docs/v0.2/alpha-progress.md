# Boxwarden v0.2 alpha progress

Updated: 2026-09-23 14:58 UTC. Launch-relative target: complete tested alpha
within ten days; stop starting new work after 2026-10-03 14:44 UTC and leave a
resumable handoff if unfinished.

## Current state

| Area | State | Evidence or next gate |
| --- | --- | --- |
| Baseline | verified | Clean `e16f23962b43337f02de1fbe2779df00a28f3f1a`; `weshofmann/feature/v02-alpha` isolated worktree |
| Host admission | verified | Elevated read-only doctor: `status: healthy` on original and alpha-only configs |
| Source baseline | verified | `GOTOOLCHAIN=local go mod verify`; elevated `GOTOOLCHAIN=local go test -count=1 ./...` passed |
| VM inventory | verified | Admitted `TART_HOME` has only the protected stopped Phase 3 r1 candidate; alpha run owns no VM yet |
| Alpha ownership | prepared | Private run ID `v02-alpha-20260923T1444Z`; exact paths and resource identities are in its private ownership manifest |
| Installer input | verified | Ubuntu 24.04.4 ARM64 Desktop ISO: good Canonical detached signature and exact `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe` SHA-256; private cache only |
| Recipe schema | source implemented | Versioned strict JSON loader and exact ISO verification have focused passing tests; no public preparation command yet |
| Inspector capability | pending | Pinned Tart CLI has `--disk <path>:ro`; offline network and bounded return transport require a synthetic VM probe |
| Graphical sandbox | pending | No v0.2 recipe, automatic preparation, READY composition, or app launch yet |
| Workspace lifecycle | pending | No independent volume implementation yet |
| Controlled export | pending | No accepted inspector or host receiver yet |
| Hosted CI | unavailable | Local verification is required; do not report CI passed |

The restricted Codex shell falsely fails Unix-socket tests and host doctor
inspection with `operation not permitted`; the corresponding elevated checks
pass. The current installed CLI configuration lists GPT-6 Sol at high effort.
An independent review was requested with GPT-6 Astra/high, but the reviewer
could verify only GPT-6 family metadata, not the exact routed variant/effort.

## Live resources

- Alpha VMs: none.
- Alpha workspace volumes: none.
- Input cache: verified official Ubuntu 24.04.4 ARM64 Desktop ISO in the private
  alpha state tree. No test VM has consumed it yet.
- Temporary services: none beyond the active download process.

## Next executable action

Finish the early inspector capability design/probe and implement the first
graphical recipe and management slice. Before any VM mutation, reread the private ownership
manifest, inventory the admitted Tart namespace, rerun doctor, and check the
disk/RAM floor.

## Human actions

No current action is required. Final real account sign-in and subjective GUI
acceptance remain owner actions after the synthetic path is demonstrated.
