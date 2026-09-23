# Boxwarden v0.2 alpha progress

Updated: 2026-09-23 20:45 UTC. Launch-relative target: complete tested alpha
within ten days; stop starting new work after 2026-10-03 14:44 UTC and leave a
resumable handoff if unfinished.

## Current state

| Area | State | Evidence or next gate |
| --- | --- | --- |
| Baseline | verified | Clean `e16f23962b43337f02de1fbe2779df00a28f3f1a`; `weshofmann/feature/v02-alpha` isolated worktree |
| Host admission | verified | Elevated read-only doctor: `status: healthy` on original and alpha-only configs |
| Source verification | verified with limits | Full local Go tests and vet passed at the 20:45 UTC source snapshot; targeted alphaqual race checks passed. Identity-path race checks and guest shell fixtures passed at the prior 20:38 UTC snapshot. Independent review found an effective-hostname acceptance gap, which is closed and regression-tested. Hosted CI is unavailable |
| VM inventory | verified | Admitted `TART_HOME` has eight stopped alpha-owned objects plus one protected stopped historical object; exact names and states are in the private ownership manifest |
| Alpha ownership | prepared | Exact resource paths and identities are retained only in the private ownership manifest |
| Installer input | verified | Ubuntu 24.04.4 ARM64 Desktop ISO: good Canonical detached signature and exact `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe` SHA-256; private cache only |
| Recipe schema | source implemented | Versioned strict JSON loader and exact ISO verification have passing tests. The fixed guest-only preparation helper validates package/argv payloads and executes exact argv with a closed environment in source tests. The builder writes a private payload, binds helper/payload digests into the installer, and sends a fixed prepare command before finalization; failure prevents cache admission. No candidate has run this revision and no public preparation command exists |
| Serial bootstrap and READY | real fresh clone verified | Corrected generic r2 candidate and public qualr2c clone completed serial bootstrap, host-key pin, certificate, strict SSH probe, time-zone convergence, READY, stop, restart to READY, and consistent stop. The management fixes are published |
| Automatic base preparation | source verified, public path missing | Installer launcher, seed/remaster, journal, and admission source pass tests. Cache reuse binds a stopped Tart bundle identity; build stages exact verified inputs; serial admission checks private ACLs. Source tests prove prepare precedes finalization and failure leaves no admitted cache. A typed package query passes through the live exact-generation supervisor with READY checks around its pinned SSH call. The static guest helper has matching lock, installer, and finalizer digests. A recipe-bound qualifier adapter checks baseline and requested packages in 32-name chunks and records versions in a BOM in fake fresh-clone tests. Read-only production preflight checks the fully admitted host runtime and selected CA. Builder composition binds exact admitted Tart/Softnet facts and verifies OpenSSL/xorriso digests before reserving a build attempt; the VM and seed adapters recheck at use. Production qualifier composition now uses exact revision registration, public create-only clone/start/stop, and exact supervisor snapshots, while requiring a trusted guest inspector. These pieces are not yet invoked by a public command. Full guest acceptance and public CLI integration remain |
| Inspector capability | synthetic boot verified, ext4 runtime pending | Pinned Tart has no NIC-off mode. A signed Virtualization.framework guest booted with zero NICs, one read-only synthetic disk, and two serial channels; the guest reported loopback only and returned a typed report. A source-only 64 MiB ext4 fixture contract and guest read-only mount path have targeted tests, but no ext4 image or live mount proof exists. Real workspace export remains closed |
| Graphical sandbox | same-clone GUI verified | Public qualr2c reached READY, GNOME Desktop and Firefox opened through Tart GUI, and Nautilus showed synthetic `bw-alpha-system-persist-r2c` in guest home after stop/restart. This is system-disk persistence only. No guest agent account sign-in or recipe-installed application has been observed |
| Workspace lifecycle | source foundation published, real path missing | Volume-owned record, attachment/use transitions, private format journal, Linux ARM64 ext4 helper, and retained exact Tart disk/lock leases have targeted tests. Guest formatter VM adapter, public attach/rebuild/reattach, and fresh synthetic qualification remain |
| Controlled export | receiver implemented, acceptance closed | Host stream receiver has hostile fixtures and independent review; zero-NIC synthetic boot/transport passed, but ext4 inspection, volume lock admission, hostile exits, and real export remain unproved, so public export remains disabled |
| Hosted CI | unavailable | Local verification is required; do not report CI passed |

The current source checkpoint adds a fixed guest identity inspection through the
retained pinned SSH connection and exact-generation supervisor. It checks a
fresh machine ID, derived persisted and effective hostname, consumed build
markers, and absence of the builder password verifier and backup. It remains
guest-reported qualification evidence; it has not been exercised on a new
candidate, and the public preparation command remains closed.

A host-side prepared-base inspector now composes exact-generation package and
identity queries into one bounded BOM and two explicit checks. Its source tests
show that failed identity inspection stops the fresh clone and cannot issue a
passing receipt. Other guest acceptance gates and a real candidate run remain.

Draft PR #12 tracks the published alpha branch. Commits `06884de` and
`1ef30ab` published the formatter and managed-disk foundations separately.
Focused tests, vet, and a Linux ARM64 formatter build passed for the former;
focused backend and lock tests plus vet passed for the latter. A full local Go
suite and vet passed with uncommitted builder and inspector source present on
2026-09-23; those results are not hosted CI or real storage qualification.
The fresh public clone independently proved the corrected management path.
The current Boxwarden management guard still requires password, root-login, and forwarding
restrictions even though owner public keys are now permitted; broader guest
SSH policy coexistence is pending.

The restricted Codex shell falsely fails Unix-socket tests and host doctor
inspection with `operation not permitted`; the corresponding elevated checks
pass. The current installed CLI configuration lists GPT-6 Sol at high effort.
An independent review was requested with GPT-6 Astra/high, but the reviewer
could verify only GPT-6 family metadata, not the exact routed variant/effort.

## Live resources

- Alpha VMs: two owned stopped generic installer candidates and six owned
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
  The separate synthetic inspector helper also exited after a stopped VM proof;
  both failed and successful private probe bundles are retained as evidence.

## Next executable action

Extend the prepared-base inspector with the remaining guest acceptance gates,
then join preflight, builder, and qualifier
in one production preparation operation, then wire public automatic
preparation. Extend the zero-NIC inspector from
synthetic block/serial proof to read-only ext4 inspection while advancing the
formatter VM adapter and workspace lifecycle. The current r2 engineering candidate predates
the builder's changed tracked definition and cannot qualify its cache key.
Serial text is evidence of the finalizer exchange, not an attestation.
Before each further VM mutation, reread the private ownership manifest,
inventory the admitted Tart namespace, rerun doctor, and check the disk/RAM floor.

## Human actions

No current action is required. Final real account sign-in and subjective GUI
acceptance remain owner actions after the synthetic path is demonstrated.
