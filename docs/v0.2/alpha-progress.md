# Boxwarden v0.2 alpha progress

Updated: 2026-09-23 17:45 UTC. Launch-relative target: complete tested alpha
within ten days; stop starting new work after 2026-10-03 14:44 UTC and leave a
resumable handoff if unfinished.

## Current state

| Area | State | Evidence or next gate |
| --- | --- | --- |
| Baseline | verified | Clean `e16f23962b43337f02de1fbe2779df00a28f3f1a`; `weshofmann/feature/v02-alpha` isolated worktree |
| Host admission | verified | Elevated read-only doctor: `status: healthy` on original and alpha-only configs |
| Source baseline | verified | `GOTOOLCHAIN=local go mod verify`; elevated `GOTOOLCHAIN=local go test -count=1 ./...` passed |
| VM inventory | verified | Admitted `TART_HOME` has the protected stopped Phase 3 r1 object and three stopped alpha-owned objects: the prepared candidate and two failed qualification clones |
| Alpha ownership | prepared | Private run ID `v02-alpha-20260923T1444Z`; exact paths and resource identities are in its private ownership manifest |
| Installer input | verified | Ubuntu 24.04.4 ARM64 Desktop ISO: good Canonical detached signature and exact `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe` SHA-256; private cache only |
| Recipe schema | source implemented | Versioned strict JSON loader and exact ISO verification have focused passing tests; no public preparation command yet |
| Serial bootstrap and READY | source implemented | Ported bounded exchange, hvc0 autologin prompt gate, exact host-key pin, certificate, strict SSH probe, time-zone convergence, and live status; integrated Go suite, focused race tests, and vet passed. Real VM bootstrap has not completed |
| Inspector capability | material gap | Pinned Tart supports `--disk <path>:ro`; source review shows no NIC-off mode and residual ARP/DHCP/inbound traffic under Softnet `/0` block. Export acceptance remains closed; receiver/locking work can continue |
| Graphical sandbox | first candidate invalidated, qualification failed | Public fresh clone r1c passed Tart scratch and hvc0 prompt gates, then helper response timed out. Exact stop succeeded. Bounded investigation boots of this failed clone showed the helper rejected its three-field OpenSSH host public key (`root@...` comment); key shape was observed directly over hvc0. Source now canonicalizes that key while preserving strict wire validation, and the locked ARM64 helper was rebuilt. A fresh generic candidate and qualification clone are required; old candidate and clones remain unqualified. GUI and app launch remain unproven |
| Workspace lifecycle | foundation implemented | Volume-owned record, optional attachment, exact use release, locks, sparse raw format journal, ext4 header/UUID verifier, and host ACL admission have focused tests; managed disk admission stays closed until a real formatter, attachment, and recovery are integrated |
| Controlled export | receiver implemented, acceptance closed | Host stream receiver has hostile fixtures and independent review; no inspector transport or offline guarantee under the admitted Tart/Softnet pair |
| Hosted CI | unavailable | Local verification is required; do not report CI passed |

Draft PR #12 tracks the published alpha branch. Full Go tests, vet, host and
guest builds, guest definition scripts, and the real `alpha recipe check` passed
locally at the first checkpoint. After the scratch, READY, stop, storage
foundation, and ambiguous-reap corrections, the full Go suite, vet, and focused
race tests passed at 17:24 UTC. After the host-key correction and locked helper
rebuild, the full Go suite, vet, module verification, diff check, and all
tracked guest script tests passed at 17:45 UTC. The current Boxwarden management guard still
requires password, root-login, and forwarding restrictions even though owner
public keys are now permitted; broader guest SSH policy coexistence is pending.

The restricted Codex shell falsely fails Unix-socket tests and host doctor
inspection with `operation not permitted`; the corresponding elevated checks
pass. The current installed CLI configuration lists GPT-6 Sol at high effort.
An independent review was requested with GPT-6 Astra/high, but the reviewer
could verify only GPT-6 family metadata, not the exact routed variant/effort.

## Live resources

- Alpha VMs: one owned stopped installer candidate with a 40 GiB nominal disk
  and 11 GiB allocated at the first post-finalization inventory; three separate
  qualification clones are stopped and failed. The candidate was built with
  the older locked helper and is invalidated for promotion. No clone has been
  admitted as a reusable base.
- Alpha workspace volumes: none.
- Input cache: verified official Ubuntu 24.04.4 ARM64 Desktop ISO in the private
  alpha state tree; the first private candidate was installed from it.
- Temporary services: the bounded installer and first-clone probe drivers
  completed; both failed public session supervisors, Tart children, and Softnet
  children exited after exact VM stops. Three investigation boots of the
  already failed r1c clone captured private serial evidence; each VM was
  stopped afterward. No alpha VM is running.

## Next executable action

Verify and publish the locked host-key helper correction, then rebuild the
generic candidate from the corrected guest definition. Qualify from a fresh
clone, including guest identity, management, and desktop behavior before cache
admission. Serial text is evidence of the finalizer exchange, not an attestation. In parallel,
implement the public automatic preparation path and the inspector transport.
Before each further VM mutation, reread the private ownership manifest,
inventory the admitted Tart namespace, rerun doctor, and check the disk/RAM floor.

## Human actions

No current action is required. Final real account sign-in and subjective GUI
acceptance remain owner actions after the synthetic path is demonstrated.
