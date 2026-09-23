# Boxwarden v0.2 alpha progress

Updated: 2026-09-23 18:30 UTC. Launch-relative target: complete tested alpha
within ten days; stop starting new work after 2026-10-03 14:44 UTC and leave a
resumable handoff if unfinished.

## Current state

| Area | State | Evidence or next gate |
| --- | --- | --- |
| Baseline | verified | Clean `e16f23962b43337f02de1fbe2779df00a28f3f1a`; `weshofmann/feature/v02-alpha` isolated worktree |
| Host admission | verified | Elevated read-only doctor: `status: healthy` on original and alpha-only configs |
| Source baseline | verified | `GOTOOLCHAIN=local go mod verify`; elevated `GOTOOLCHAIN=local go test -count=1 ./...` passed |
| VM inventory | verified | Admitted `TART_HOME` has the protected stopped Phase 3 r1 object and nine stopped alpha-owned objects; exact names and states are in the private ownership manifest |
| Alpha ownership | prepared | Private run ID `v02-alpha-20260923T1444Z`; exact paths and resource identities are in its private ownership manifest |
| Installer input | verified | Ubuntu 24.04.4 ARM64 Desktop ISO: good Canonical detached signature and exact `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe` SHA-256; private cache only |
| Recipe schema | source implemented | Versioned strict JSON loader and exact ISO verification have focused passing tests; no public preparation command yet |
| Serial bootstrap and READY | real fresh clone verified | Corrected generic r2 candidate and public qualr2c clone completed serial bootstrap, host-key pin, certificate, strict SSH probe, time-zone convergence, READY, stop, restart to READY, and consistent stop. SSH argv quoting and certificate pairing fixes have focused tests and a real VM pass; full integration verification pending before publication |
| Automatic base preparation | source foundation, public path missing | Strict recipe/key, host ISO seed/remaster, attempt journal, cache admission/recovery, and Tart object mechanics have focused tests. A bounded installer launcher, real qualifier, guest recipe execution, and public CLI integration remain |
| Inspector capability | material gap | Pinned Tart has no NIC-off mode. An ad-hoc signed Virtualization.framework structural probe validated a stopped zero-NIC VM configuration, but it has not booted, read ext4, or transferred output. Export acceptance remains closed |
| Graphical sandbox | same-clone GUI verified | Public qualr2c reached READY, GNOME Desktop and Firefox opened through Tart GUI, and Nautilus showed synthetic `bw-alpha-system-persist-r2c` in guest home after stop/restart. This is system-disk persistence only. No guest agent account sign-in or recipe-installed application has been observed |
| Workspace lifecycle | source foundation, real path missing | Volume-owned record, optional attachment, exact use release, locks, sparse raw format journal, ext4 header/UUID verifier, host ACL admission, and retained Tart disk handle have focused tests. A real formatter, public attach/rebuild/reattach, and fresh synthetic qualification remain |
| Controlled export | receiver implemented, acceptance closed | Host stream receiver has hostile fixtures and independent review; zero-NIC inspector structural probe has no boot/transport/ext4 proof, so public export remains disabled |
| Hosted CI | unavailable | Local verification is required; do not report CI passed |

Draft PR #12 tracks the published alpha branch. Full Go tests, vet, host and
guest builds, guest definition scripts, and the real `alpha recipe check` passed
at the 17:45 UTC published checkpoint. Since then, a focused Darwin ACL test,
`hostx`, `privateacl`, `basebuild`, and `sshx` tests passed; the public qualr2c
run independently proved the corrected management path. The current Boxwarden
management guard still requires password, root-login, and forwarding
restrictions even though owner public keys are now permitted; broader guest
SSH policy coexistence is pending.

The restricted Codex shell falsely fails Unix-socket tests and host doctor
inspection with `operation not permitted`; the corresponding elevated checks
pass. The current installed CLI configuration lists GPT-6 Sol at high effort.
An independent review was requested with GPT-6 Astra/high, but the reviewer
could verify only GPT-6 family metadata, not the exact routed variant/effort.

## Live resources

- Alpha VMs: two owned stopped generic installer candidates and seven owned
  stopped qualification clones. The r1 candidate and its clones are invalidated
  or failed evidence; r2 has the corrected locked helper. qualr2c passed public
  READY and GUI persistence on its system disk. The r2 candidate was manually
  registered in the private alpha domain for engineering qualification only;
  no automatic prepared base is admitted.
- Alpha workspace volumes: none.
- Input cache: verified official Ubuntu 24.04.4 ARM64 Desktop ISO in the private
  alpha state tree; the first private candidate was installed from it.
- Temporary services: installer and probe drivers completed; public qualr2c
  was stopped after its second READY/GUI check. Public status reports intended
  and observed stopped with consistent state; Tart lists every alpha VM stopped.

## Next executable action

Review and publish the SSH/ACL fixes with the real clone evidence. Integrate
the source-only automatic base and managed-disk foundations after full
verification. Then implement the installer launcher and fresh qualifier,
public recipe and workspace lifecycle, and zero-NIC inspector transport.
Serial text is evidence of the finalizer exchange, not an attestation.
Before each further VM mutation, reread the private ownership manifest,
inventory the admitted Tart namespace, rerun doctor, and check the disk/RAM floor.

## Human actions

No current action is required. Final real account sign-in and subjective GUI
acceptance remain owner actions after the synthetic path is demonstrated.
