# Boxwarden v0.2 alpha progress

Updated: 2026-09-24 UTC. Integration branch:
`weshofmann/feature/v02-alpha`; [Draft PR #12](https://github.com/weshofmann/boxwarden/pull/12).
The mission deadline is 2026-10-03 14:44 UTC. This is a functional prototype
with material acceptance gaps, not an alpha-ready release.

## Verified implementation

| Area | Current evidence and limit |
| --- | --- |
| Host and preparation | Host doctor is healthy. The pinned Canonical Ubuntu 24.04.4 ARM64 Desktop installer was signature and digest checked. The public recipe path built a fresh generic base carrying the corrected shutdown helper, qualified a separate clone, admitted the base to its cache, and created a stopped session from it. The session reached bound READY. |
| Graphical sandbox | A real Ubuntu Desktop clone displayed GNOME and Firefox, restarted to bound READY, and retained a synthetic home file. Actual agent desktop use and provider sign-in remain unverified. |
| Recipe identity | Preparation has a reusable cache key; the complete canonical recipe has a separate immutable digest. Public create persists that digest before cloning. Rebuild journals and switches old/candidate recipe intent with the system identity. Legacy unbound sessions remain unbound. These additions have source tests; fresh real-host qualification is pending. |
| Recipe actions | Public preparation rejects `once`, `reconfigure`, `startup`, and `launch` actions until durable attempts and an executor exist. Guest-only phase execution is not claimed. |
| Workspaces | Public create formatted independent ext4 volumes; attachment, mount-bound READY, stop/start, detach/reattach, and a same-software system rebuild preserved synthetic content in real VM runs. Public session delete retains a volume. New-format software upgrade and the complete fresh sequence remain pending. |
| Import | The public command captured a bounded synthetic host tree and transferred it over pinned SFTP to an attached running workspace. Host readback matched. Its journal deliberately remains `transferring`; readback alone does not prove stopped-disk persistence. |
| Controlled return | Selected files from clean stopped workspace snapshots were exported through a zero-NIC Linux inspector and bounded host receiver into new destinations. An `inspected` export resumed and published; a dirty ext4 snapshot was refused. Ambiguous post-final-rename recovery remains a manual evidence gate. |
| Import verification | Source code compares a complete selected stopped export against the captured import and can advance an exact journal to `verified`. Focused tests and a separate source review passed. No real import has yet passed this stopped-disk verification. |
| Shutdown correction | A fresh base carrying fixed guest poweroff qualified, but an attached empty workspace reproduced a dirty stop while the no-workspace control stopped promptly. A 120-second grace experiment also failed. The owner now passes exact bound workspace mounts to the helper, which verifies and unmounts them before queuing poweroff. The quiesce source and pinned helper passed targeted tests and hosted CI. On its freshly qualified base, one empty-volume stop was dirty; two more fresh empty-volume stops had clean ext4, one fast and one after the full grace. Public stop now records whether the guest request was acknowledged, Tart was used, and force stop was sent; it explicitly leaves workspace cleanliness unverified. This reporting has targeted source tests but no real-host trial yet. Clean persistence is intermittent, not accepted. |
| Host capacity | The stopped Tart store moved into an encrypted external APFS image. All 33 files matched byte for byte and by SHA-256 after remount; ownership, modes, extended attributes, and the 11 stopped VMs present at cutover matched. Doctor and disposable Tart create/clone/delete passed. A login/mount LaunchAgent remounted it without a prompt in a controlled test. The internal copy was retired, recovering 31.99 GiB immediately; an actual host reboot remains untested. |

The quiesce source checkpoint `31399e5f05c089e87da82cf2bdc340ea7f059220`
passed [hosted macOS CI](https://github.com/weshofmann/boxwarden/actions/runs/36065549820)
(gofmt, full Go tests, race tests, vet, and build). Local affected-package
tests (guest protocol, SSH, runtime, supervisor), all-package Go compilation,
guest installer/finalizer/remaster fixtures, artifact digest, and diff checks
also passed. These source checks do not establish a clean real attached-volume
stop.

The newer stop-outcome source checkpoint passed full tests in the four affected
Go packages (session runtime, supervisor, session, and app), focused outcome
regressions, all-package Go compilation, and diff checks. Hosted CI and a
real-host trial of this reporting remain pending.

## Current work and blockers

- Two fresh import runs reached matching live readback, then controlled stop
  left the exact ext4 workspace with `needs_recovery`. One stopped export
  refused the dirty snapshot; another was not attempted. Their volumes,
  journals, and remaining failed VM evidence are retained privately.
- A third fresh public session used the newly qualified base and an independent
  formatted workspace. Synthetic import matched a pinned live readback and
  the session reached READY; after public controlled stop, stopped export
  refused `ext4 filesystem requires recovery`. An empty attached workspace
  reproduced the dirty stop without import; the same software stopped promptly
  without a workspace. Doubling the graceful window to 120 seconds did not
  change the result. These failed runs and their exact volumes remain private
  evidence. A single source-digest preflight anomaly did not recur in two
  read-only checks or a bounded public create retry; its cause remains unproven.
- The quiesce-helper source built and qualified another fresh generic base. A
  new public session with an empty attached ext4 workspace reached bound
  READY. Controlled stop took 60.96 seconds and the stopped ext4 header still
  had `needs_recovery`; no import was involved. Its VM and volume are retained
  privately without restart or repair. That run predates public outcome
  reporting, so its guest-request result is unknown.
- Two further fresh empty-volume trials from that same base used an isolated
  host-only private trace, with no guest or stop-decision change. Both pinned
  guest shutdown requests were acknowledged in under half a second, and both
  stopped ext4 headers were clean. One VM exited in about six seconds; the
  other took roughly 63 seconds, consistent with the force-stop path after
  the grace window. The first dirty stop had no trace, so its request outcome
  remains unknown. The trace patch is retained only in private evidence and
  was removed from the worker checkout.
- The external Tart migration recovered 31.99 GiB of unique internal space.
  If the encrypted image is unavailable, the unmounted
  Tart path is mode `000` and operations fail closed. Login and filesystem
  mount triggers are installed, but an actual reboot has not been exercised.
  Separately, a cold historical private archive was verified after relocation
  into the encrypted evidence vault; its internal copy was retired and that
  vault is unmounted.
- One stopped synthetic system clone was retired through public `session
  delete`; its independent workspace kept the same inode, size, and SHA-256
  and is now available and detached. A later private ledger check showed that
  this volume also had a dirty-stop diagnostic. Its system disk is no longer
  available for further diagnosis; the retained volume, host records, and
  other failed runs remain. No further diagnostic VM retirement is planned.
- Durable recipe action attempts and receipts are the next source increment.
  Until their retry and unknown-outcome semantics exist, action execution
  stays disabled.

## Remaining acceptance

1. Exercise public bound guest-request and forced-stop reporting on a fresh
   disposable trial. Capture the intermittent dirty branch, correct it,
   qualify a new baseline, prove clean stopped ext4, then repeat public
   synthetic import, stopped export, and `workspace import verify`.
2. Implement guest-only `once`, explicit `reconfigure`, `startup`, and `launch`
   actions with bounded durable attempts, visible retry/skip, and truthful
   failure or waiting-for-sign-in states.
3. Qualify a software-changing rebuild, replacement-sandbox reattachment,
   desktop application launch, and the complete public synthetic workflow
   from a separate fresh tracked-source sandbox.
4. Run final source and real-host acceptance checks, resolve review findings,
   record limitations, and update the Draft PR. Wes's real provider sign-in
   and subjective GUI acceptance may remain human actions.

## Next step and publication policy

Preserve the failed diagnostic runs. Build the new host-only source and run a
fresh disposable attached-volume stop through the public lifecycle. Use its
bound request and force-stop result alongside authoritative offline ext4
inspection to isolate the dirty branch, then correct and retest from a new
baseline.
Continue durable per-action attempt records and tests as the next source
increment. Publish each independently verified increment promptly on the alpha
branch and keep the
Draft PR accurate. Keep private host evidence, VM disks, credentials, and the
vault key out of Git.
Never push or merge into `main`, rewrite published history, or bypass checks.
