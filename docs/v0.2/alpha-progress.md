# Boxwarden v0.2 alpha progress

Updated: 2026-09-25 UTC. Integration branch:
`weshofmann/feature/v02-alpha`; [Draft PR #12](https://github.com/weshofmann/boxwarden/pull/12).
The mission deadline is 2026-10-03 14:44 UTC. This remains a functional
prototype with material acceptance gaps, not an alpha-ready release.

## Verified behavior

| Area | Evidence and limit |
| --- | --- |
| Host and base | Host doctor is healthy. The pinned Ubuntu Desktop ARM64 installer was signature and digest checked. A corrected generic base was built, qualified on a separate clone, admitted, and used to create stopped sessions that reached bound READY. |
| Desktop | A real Ubuntu Desktop clone displayed GNOME and Firefox, restarted to bound READY, and retained a synthetic home file. Agent desktop use and provider sign-in remain unverified. |
| Recipe identity | Preparation has a reusable cache key; the canonical recipe has a separate immutable digest. Public create persists the digest before cloning, and rebuild journals old and candidate intent with system identity. Source tests pass; fresh real-host qualification of software changes remains. |
| Workspaces | Public create formatted independent ext4 volumes. Attachment, mount-bound READY, stop/start, detach/reattach, same-software system rebuild, and retained-volume delete preserved synthetic content in real VM runs. |
| Import persistence | From published cached-disk source, a fresh bound READY session imported a credential-free synthetic tree with matching pinned live readback. Public stop reported `request=guest_accepted forced=false`. The exact stopped 64 MiB raw inode retained its UUID and clean ext4 header without journal recovery or orphan state. A zero-NIC stopped export returned the selected tree; independent byte-for-byte comparison matched all three source files, and public `workspace import verify` advanced the exact import journal to `verified`. This establishes one real stopped-disk import proof, not general reliability. |
| Controlled return | Selected files from clean stopped workspace snapshots were exported through a zero-NIC Linux inspector and bounded host receiver into new destinations. An `inspected` export resumed and published; a dirty ext4 snapshot was refused. Ambiguous post-final-rename recovery remains a manual evidence gate. |
| Stop observability | Public stop and status report whether the pinned guest request was accepted, whether Tart fallback or force stop occurred, and explicitly mark workspace cleanliness unverified. Read-only stopped-filesystem inspection remains the persistence gate. |
| External Tart store | The stopped Tart store moved into an encrypted external APFS image. All 33 files matched byte for byte and by SHA-256 after remount; ownership, modes, extended attributes, and the 11 stopped VMs at cutover matched. Doctor and disposable Tart create/clone/delete passed. A login/mount LaunchAgent remounted it without a prompt in a controlled test; an actual reboot remains untested. |

## Verification and current work

- [Hosted macOS CI for the cached-disk source](https://github.com/weshofmann/boxwarden/actions/runs/36075148788)
  passed gofmt, full Go tests, race tests, vet, and build. Local Tart package
  tests, all-package compilation, and diff checks passed before publication.
  The real-host import proof above used a binary built from that exact
  published integration commit. These are separate source and host checks.
- Earlier automatic-cache trials produced dirty ext4 stops, including one
  public Tart fallback with force and one traced guest request that could not
  resolve the expected filesystem UUID after the raw volume lost its primary
  ext4 header. Failed VMs, volumes, and journals remain private evidence.
  Pinned Tart [uses cached I/O for Linux root disks](https://github.com/openai/tart/blob/2.32.1/Sources/tart/VM.swift)
  while [additional file disks default to automatic caching](https://github.com/openai/tart/blob/2.32.1/Sources/tart/Commands/Run.swift).
  Boxwarden now pins `caching=cached` for managed writable ext4 disks. An
  independent empty-volume trial and the published-source import trial both
  stopped with clean exact filesystem identity. The causal mechanism and
  long-run reliability are still unproven.
- Guest-only recipe `once`, `reconfigure`, `startup`, and `launch` actions are
  rejected until durable attempt, receipt, retry, and skip semantics exist.
  Those records and the bounded guest executor are the current source work.
- Internal free capacity was about 31 GiB after the successful stopped export.
  The export guard previously required about 25.8 GiB. Check capacity before
  another expensive host trial and report any renewed shortfall; no further
  cleanup is planned.

## Remaining acceptance

1. Implement guest-only `once`, explicit `reconfigure`, `startup`, and `launch`
   actions with bounded durable attempts, visible retry/skip, and truthful
   failure or waiting-for-sign-in states.
2. Qualify a software-changing rebuild, replacement-sandbox reattachment,
   desktop application launch, and the complete public synthetic workflow
   from a separate fresh tracked-source sandbox.
3. Run final source and real-host acceptance checks, resolve review findings,
   record limitations, and update the Draft PR. Wes's real provider sign-in
   and subjective GUI acceptance may remain human actions.

## Publication policy

Publish each meaningful independently verified increment promptly on the
alpha branch, with targeted checks for small changes and full integration
checks at mission gates. Keep this record and the Draft PR current. Keep
private host evidence, VM disks, credentials, and vault keys out of Git.
Never push or merge into `main`, rewrite published history, or bypass checks.
