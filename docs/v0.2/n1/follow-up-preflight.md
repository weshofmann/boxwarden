# N1 compatibility follow-up: read-only preflight, 2026-09-28

This records the first check against the approved narrow follow-up window.
It is a **blocked preflight**, not a clipboard or cross-guest test result.
The source base is merged main `776ed1d60f8a710c38becddaf941ad7db7d4dc5d`;
the follow-up worktree started clean on
`weshofmann/test/n1-compatibility-followup`. The completed first trial and
cleanup remain recorded in the [matrix](matrix.md).

## Exact inputs and observations

- The archived explicit candidate Boxwarden executable, candidate Softnet
  executable and release archive, guest bootstrap and clipboard adapter, and
  exact cleanup helper still match the SHA-256 values in the
  [follow-up plan](follow-up-trial.md) and private renewed request. The
  candidate Softnet executable/archive also match
  [`artifact.json`](../../../tools/n1-softnet/artifact.json). This establishes
  byte identity, not admissibility on the current host.
- `sw_vers` reported macOS **27.0.1**, build **26A434**. The reviewed
  executable's [`hostx` identity](../../../internal/hostx/identity.go) fixes
  the qualified release/build to **26.6.2**, **25G83**. Public host admission
  refuses a mismatched platform. The installed stock manifest is likewise
  bound to 26.6.2/25G83. The exact cleanup helper validates that same old
  binding, so bypassing admission would also break the reviewed rollback.
- `/Volumes/BoxwardenAlphaQualification` was absent from the mount table and
  `diskutil info` could not resolve it. Both R1 trial configs require its
  enrolled UUID `a178510a-d5ec-4495-828b-bd5445e2b66d`. The dedicated
  Tart-home volume was also unmounted; the mountpoint had mode `0000`, and
  stock doctor could not complete configuration validation. These are current
  host observations, not changes to the earlier qualification receipts.
- The exact temporary candidate digest directory under
  `/Library/Boxwarden/toolchains/softnet/0.19.0-boxwarden-n1.1/` remained
  absent. No state root was recreated and no guest or privileged operation
  was attempted in this follow-up. The original trial's exact cleanup remains
  the last candidate removal; no new cleanup was necessary.

The private operator archive retains the read-only receipt
`followup-prep/preflight-20260928.json` (SHA-256
`c2f7695c5a71028e225b244f7a80f34567f46b4baf4e035be270ef7fe6fefc3b`).

An independent Sol/High review of the install, guest staging, test and cleanup
sequence confirmed the platform mismatch as a stop condition. It also found
that the archived clipboard and cross-guest probe scripts refer to retired
identities or a different endpoint; neither is safe to replay unchanged.
The required guest-only staging command can be made exact only after fresh
UUID/backend/generation identities exist.

## Next gate

The current exact N1 window cannot be executed on macOS 27.0.1. Resume live
work only on a host with the exact previously qualified release/build and
verified mounted storage, or under a separately scoped and reviewed 27.0.1
platform/toolchain qualification with a matching exact cleanup procedure.
Neither route is implied by this preflight. Once a qualified host exists,
rehash the exact inputs, verify stock doctor and R1 enrollment, and review
new-session-bound guest stage/probe commands before any guest write. Keep the
follow-up trial limited to synthetic CLI clipboard and positive-controlled
cross-guest TCP; preserve an invalid or unknown interval as such.
