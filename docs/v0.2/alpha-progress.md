# Boxwarden v0.2 alpha progress

Updated: 2026-09-24 UTC. Integration branch:
`weshofmann/feature/v02-alpha`; [Draft PR #12](https://github.com/weshofmann/boxwarden/pull/12).
The mission deadline is 2026-10-03 14:44 UTC. This is a functional prototype
with material acceptance gaps, not an alpha-ready release.

## Verified implementation

| Area | Current evidence and limit |
| --- | --- |
| Host and preparation | Host doctor is healthy. The pinned Canonical Ubuntu 24.04.4 ARM64 Desktop installer was signature and digest checked. The public recipe path builds or reuses a qualified generic base without manual golden registration. A real prepared clone reached bound READY. |
| Graphical sandbox | A real Ubuntu Desktop clone displayed GNOME and Firefox, restarted to bound READY, and retained a synthetic home file. Actual agent desktop use and provider sign-in remain unverified. |
| Recipe identity | Preparation has a reusable cache key; the complete canonical recipe has a separate immutable digest. Public create persists that digest before cloning. Rebuild journals and switches old/candidate recipe intent with the system identity. Legacy unbound sessions remain unbound. These additions have source tests; fresh real-host qualification is pending. |
| Recipe actions | Public preparation rejects `once`, `reconfigure`, `startup`, and `launch` actions until durable attempts and an executor exist. Guest-only phase execution is not claimed. |
| Workspaces | Public create formatted independent ext4 volumes; attachment, mount-bound READY, stop/start, detach/reattach, and a same-software system rebuild preserved synthetic content in real VM runs. Public session delete retains a volume. New-format software upgrade and the complete fresh sequence remain pending. |
| Import | The public command captured a bounded synthetic host tree and transferred it over pinned SFTP to an attached running workspace. Host readback matched. Its journal deliberately remains `transferring`; readback alone does not prove stopped-disk persistence. |
| Controlled return | Selected files from clean stopped workspace snapshots were exported through a zero-NIC Linux inspector and bounded host receiver into new destinations. An `inspected` export resumed and published; a dirty ext4 snapshot was refused. Ambiguous post-final-rename recovery remains a manual evidence gate. |
| Import verification | Source code compares a complete selected stopped export against the captured import and can advance an exact journal to `verified`. Focused tests and a separate source review passed. No real import has yet passed this stopped-disk verification. |
| Shutdown correction | The retained owner now asks the exact bound guest helper to enqueue a fixed systemd poweroff, then retains Tart stop and force-stop fallbacks. Source checks passed. Existing bases contain the older helper, so no real-host effectiveness is claimed yet. |

The latest pushed commit is `b125cfd4428ffd7a8f2a115b150aadd0cc8fa5a5`.
[Hosted macOS CI](https://github.com/weshofmann/boxwarden/actions/runs/36043797725)
passed gofmt, the full Go test suite, race tests, vet, and build at that SHA.
For the shutdown change, local targeted guest, SSH, runtime, supervisor,
helper-artifact, base-build, and architecture checks passed. The full local
SSH package was blocked by the internal disk reserve; hosted CI subsequently
passed the full suite. Source checks do not replace real-host qualification.

## Current work and blockers

- Two fresh import runs reached matching live readback, then controlled stop
  left the exact ext4 workspace with `needs_recovery`. One stopped export
  refused the dirty snapshot; another was not attempted. Their volumes,
  journals, and remaining failed VM evidence are retained privately.
- A new qualified base must be built with the corrected helper before a fresh
  stop can test the shutdown change. Internal build headroom remains tight.
  A cold historical private archive was relocated into the existing encrypted
  external evidence vault. Every file digest and mode matched before and
  after a detach/remount; the internal copy was retired and a private locator
  remains. The vault is unmounted. Current free space clears the stopped
  export reserve but is not yet a safe full-build margin.
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

1. Build and qualify a new generic base carrying the fixed shutdown helper.
   From a fresh VM and clean workspace, repeat public synthetic import,
   controlled stop, selected export, and `workspace import verify`.
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

Implement durable per-action attempt records and tests while planning a safe
internal build-capacity path. Then build the revised generic base and run the
fresh stopped-import qualification. Publish each independently verified
increment promptly on the alpha branch and keep the Draft PR accurate. Keep
private host evidence, VM disks, credentials, and the vault key out of Git.
Never push or merge into `main`, rewrite published history, or bypass checks.
