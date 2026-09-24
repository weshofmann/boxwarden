# Boxwarden v0.2 alpha progress

Updated: 2026-09-24 00:23 UTC. Launch-relative target: complete tested alpha
within ten days; stop starting new work after 2026-10-03 14:44 UTC and leave a
resumable handoff if unfinished.

## Current state

| Area | State | Evidence or next gate |
| --- | --- | --- |
| Baseline | verified | Clean `e16f23962b43337f02de1fbe2779df00a28f3f1a`; `weshofmann/feature/v02-alpha` isolated worktree |
| Host admission | verified | Elevated read-only doctor: `status: healthy` on original and alpha-only configs |
| Source verification | verified with limits | Full local Go suite and targeted session/sessionruntime/workspacex/architecture race tests passed after stop-side wiring; focused vet and diff checks passed. Independent review found no critical or important issue in the stop release, and an interrupted two-volume release/retry fixture passed. These are source and synthetic checks; hosted CI and real workspace VM qualification remain unavailable |
| VM inventory | verified | Admitted `TART_HOME` has eight stopped alpha-owned objects plus one protected stopped historical object. One additional failed alpha VM was archived byte-for-byte and deleted after archive verification; exact identities are in the private ownership manifest |
| Alpha ownership | prepared | Exact resource paths and identities are retained only in the private ownership manifest |
| Installer input | verified | Ubuntu 24.04.4 ARM64 Desktop ISO: good Canonical detached signature and exact `c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe` SHA-256; private cache only |
| Recipe schema | real preparation exchange reached | Versioned strict JSON loader and exact ISO verification have passing tests. The fixed guest-only preparation helper validates package/argv payloads and executes exact argv with a closed environment in source tests. A real candidate reached the guest preparation completion marker before finalization, but no fresh-clone package inventory or cache admission followed |
| Serial bootstrap and READY | real fresh clone verified | Corrected generic r2 candidate and public qualr2c clone completed serial bootstrap, host-key pin, certificate, strict SSH probe, time-zone convergence, READY, stop, restart to READY, and consistent stop. The management fixes are published |
| Automatic base preparation | real guest preparation reached, qualification pending | The first attempt failed before VM creation on an unsupported OpenSSL mode. A fresh attempt with exact OpenSSL 3 passed installer and guest preparation, then reached finalization. A source audit found the finalizer rejected the public random run-ID format; the operator canceled through owned stop/wait cleanup. Its journal is failed, and no cache was admitted. The exact stopped candidate was privately archived and deleted after archive verification. The finalizer contract and regression fixture are published. A source-only reserve monitor now checks the state and Tart filesystems before mutation and through build/qualification |
| Inspector capability | synthetic boot verified, ext4 runtime pending | Pinned Tart has no NIC-off mode. A signed Virtualization.framework guest booted with zero NICs, one read-only synthetic disk, and two serial channels; the guest reported loopback only and returned a typed report. The formatter now produced a synthetic ext4 image, but the inspector has not mounted or validated it. The guest read-only mount path has targeted source tests. Real workspace export remains closed |
| Graphical sandbox | same-clone GUI verified | Public qualr2c reached READY, GNOME Desktop and Firefox opened through Tart GUI, and Nautilus showed synthetic `bw-alpha-system-persist-r2c` in guest home after stop/restart. This is system-disk persistence only. No guest agent account sign-in or recipe-installed application has been observed |
| Workspace lifecycle | isolated synthetic formatter boot verified; production path pending | Volume-owned records, attachment/use transitions, private format journal, Linux ARM64 ext4 helper, and retained exact Tart disk/lock leases have targeted tests. Public start reserves exact Uses as a batch and persists Starting last; retries check them. The child rechecks Starting/Use/attachment/formatter evidence and passes retained leases to Tart. Stop retains Stopping through exact owner stop and batch Use release, then persists Stopped. The pinned Ubuntu initrd lacks e2fsck; preparation verifies Canonical's matching static ARM64 checker and appends it with a fixed formatter guest. A signed no-NIC VZ runner completed one fresh 64 MiB synthetic ext4 format, clean guest check, VM stop, runner reap, and host header/identity verification. Go Formatter integration, public attach/rebuild/reattach, and production volume qualification remain |
| Controlled export | receiver implemented, acceptance closed | Host stream receiver has hostile fixtures and independent review; zero-NIC synthetic boot/transport passed, but ext4 inspection, volume lock admission, hostile exits, and real export remain unproved, so public export remains disabled |
| Hosted CI | unavailable | Local verification is required; do not report CI passed |

The first public attempt failed before VM creation because macOS LibreSSL
lacked SHA-512 crypt. Published tool-capability preflight now catches that
before reserving an attempt. A second fresh attempt with verified OpenSSL 3
reached the guest preparation marker and finalization. Source audit then found
the fixed guest finalizer accepted only historical `run-1`/`run-2`, whereas the
public command generates `run-` plus 12 lowercase hex digits. The exact
candidate was canceled through the owned cleanup path and observed stopped;
its journal is failed and no cache receipt was issued. Its exact bundle was
archived privately and verified before the live VM was deleted. The failed
journal and staged installer remain private evidence. A fixture reproduced the
mismatch before the one-line finalizer correction and passed afterward. A new
candidate is required for qualification. The current source change uses APFS
copy-on-write for the staged ISO on macOS, with digest re-verification and an
ordinary-copy fallback on unsupported filesystems, disabled cgo, and sources
with inherited file flags. A regression test reproduced the immutable-source
failure found during review before the fallback was added. The clone path can
avoid a full physical ISO duplicate, but the next real installation still
requires a fresh disk-reserve check. No failed evidence has been discarded.

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

The formatter preparation check verified the pinned ISO, Canonical's
`e2fsck-static` package SHA-256, the extracted static ARM64 checker, and the
ARM64 guest build. The appended initrd preserves the source bytes. This is
source preparation with one subsequent synthetic VM proof, not a managed volume.
The VZ runner's signed no-start preflight left the synthetic disk unchanged.
An independent pre-boot review caught a broad caller-selected target and an
unbound input bundle. The runner now requires an exact private `formatting`
journal and matching nonzero disk marker; the probe requires a private
digest-bound, signed preparation bundle from committed source. A further
review restricted this exploratory runner to a fresh private synthetic probe
root and closed extended ACL admission on every private target path; the
negative no-start probes reject a caller-selected root and ACL-bearing
journal. The retained private synthetic boot from published source returned a
transaction-bound clean ext4 result. The host observed VM stop, runner reap,
unchanged raw identity and size, changed disk digest, and a matching ext4
superblock. Doctor, stopped Tart inventory, RAM, and disk headroom passed the
alpha preflight. This does not qualify managed workspace creation or Tart
attachment.

The restricted Codex shell falsely fails Unix-socket tests and host doctor
inspection with `operation not permitted`; the corresponding elevated checks
pass. The current installed CLI configuration lists GPT-6 Sol at high effort.
An independent review was requested with GPT-6 Astra/high, but the reviewer
could verify only GPT-6 family metadata, not the exact routed variant/effort.

## Live resources

- Alpha VMs: two owned stopped generic installer candidates and six owned
  stopped qualification clones. The failed public candidate is retained as a
  verified private exact-bundle archive, not a live Tart object. The r1
  candidate and its clones are invalidated
  or failed evidence; r2 has the corrected locked helper. qualr2c passed public
  READY and GUI persistence on its system disk. The r2 candidate was manually
  registered in the private alpha domain for engineering qualification only;
  no automatic prepared base is admitted.
- Alpha workspace volumes: none.
- Synthetic formatter proof: one retained private 64 MiB raw disk and bounded
  boot evidence; it is not registered as a workspace volume.
- Input cache: verified official Ubuntu 24.04.4 ARM64 Desktop ISO in the private
  alpha state tree; the first private candidate was installed from it.
- Temporary services: installer and probe drivers completed; public qualr2c
  was stopped after its second READY/GUI check. Public status reports intended
  and observed stopped with consistent state; Tart lists every alpha VM stopped.
  The separate synthetic inspector helper also exited after a stopped VM proof;
  both failed and successful private probe bundles are retained as evidence.

## Next executable action

Wire the stopped/reaped VZ result into `workspaceformat.Formatter` with exact
trusted state-root and artifact admission, then qualify one managed ext4 raw volume,
then expose bounded public attachment and verify the source-wired start/stop
loop against a disposable Tart clone. The parent writes Uses before Starting,
the child checks exact formatter and disk evidence, and Stop clears Uses only
after exact owner reap and fresh backend-stopped observation.
The next full prepared-base run needs more safe disk headroom. After that, record actual
qualification run. Record actual
package, identity, READY, and stopped-object evidence. Extend the zero-NIC inspector from
synthetic block/serial proof to read-only ext4 inspection while advancing the
formatter VM adapter and workspace lifecycle. The current r2 engineering candidate predates
the builder's changed tracked definition and cannot qualify its cache key.
Serial text is evidence of the finalizer exchange, not an attestation.
Before each further VM mutation, reread the private ownership manifest,
inventory the admitted Tart namespace, rerun doctor, and check the disk/RAM floor.

## Human actions

No current action is required. Final real account sign-in and subjective GUI
acceptance remain owner actions after the synthetic path is demonstrated.
