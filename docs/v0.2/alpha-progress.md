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
- A private action-attempt journal now binds each reserved action to the
  immutable recipe, exact session and backend, and start generation. It blocks
  duplicate `once` attempts on a system, blocks same-generation replay of
  other actions, and permits one terminal result with a receipt digest slot.
  The session package tests, all-package compile check, targeted vet, gofmt,
  and diff checks passed locally. [Hosted CI for that journal checkpoint](https://github.com/weshofmann/boxwarden/actions/runs/36077138266)
  passed gofmt, full Go tests, race tests, vet, and build.
- The fixed guest action request and success receipt now have bounded canonical
  JSON, an exact argv digest, and session/recipe/attempt/generation binding.
  The guest helper now checks its installed association, durably claims the
  exact request, runs an argv-only command as the workstation account with a
  closed environment and ten-minute limit, and publishes a success receipt.
  An interrupted claim cannot silently rerun; exact completed retry returns
  the stored receipt. Full local Go tests, affected race tests, vet, CI build,
  static-helper reproducibility, and golden source fixtures passed. An earlier
  hosted run caught a stale static helper artifact after the protocol source
  changed; the [coherent artifact correction passed hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36078574683).
  [Hosted CI for the executor checkpoint](https://github.com/weshofmann/boxwarden/actions/runs/36079269375)
  passed its full deterministic matrix. A separate host SSH method now sends
  only the canonical bounded action request through the fixed `action` helper
  mode with the exact host-key pin, association, and credential path checks;
  it accepts only the matching bounded success receipt. Targeted `sshx`
  tests and vet, all-package compilation, gofmt, and diff checks passed
  locally; [hosted CI for the transport checkpoint](https://github.com/weshofmann/boxwarden/actions/runs/36079723505)
  passed. The session action service now reloads exact stored recipe argv,
  requires fresh generation READY on both sides of execution, reserves before
  the guest call, and journals an exact success receipt digest. An interrupted
  call or changed readiness becomes indeterminate. Full session package tests,
  targeted race tests, vet, all-package compilation, gofmt, and diff checks
  passed locally. [Hosted CI for that source checkpoint](https://github.com/weshofmann/boxwarden/actions/runs/36080269429)
  failed because the architecture guard had not admitted the new action
  protocol import. A narrow guard correction now allows those selectors only
  in the reviewed action service and owner admission files. The owner also
  independently checks the exact running record, reserved attempt, stored
  recipe argv, and no-rebuild gate before any future SSH call. The complete
  local Go suite, affected runtime tests, targeted race test, vet, CI build,
  gofmt, and diff checks passed after the correction. Hosted CI passed for
  [owner admission](https://github.com/weshofmann/boxwarden/actions/runs/36080934075).
  The retained owner now invokes the fixed pinned SSH action only after its
  own fresh READY and reserved-intent checks, then rechecks both after the
  receipt. The full runtime package, targeted race and architecture tests,
  vet, all-package compilation, gofmt, and diff checks passed for that
  increment locally; [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36081366044)
  passed. A typed supervisor RPC now carries the bounded action request and
  receipt to that retained owner for the exact generation. It checks READY
  before and after the call and validates the receipt. The action-only
  controller cannot launch or stop a VM. The control frame can hold the
  canonical 64 KiB request, while launch publication retains its separate
  16 KiB limit. The full supervisor package, focused race and architecture
  tests, vet, CI build, gofmt, and diff checks passed locally;
  [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36082416681)
  passed. An explicit same-attempt retry now reconstructs the immutable argv
  and original attempt ID, marks a crash-left reservation indeterminate before
  retry, and uses a separate supervisor/owner route that admits only that
  exact indeterminate intent. A completed guest claim can return its stored
  receipt; an unresolved claim remains indeterminate. The full local Go suite,
  focused race tests, vet, CLI build, gofmt, and diff checks passed for this
  recovery source increment;
  [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36083318667)
  passed. A deliberate skip now resolves a reserved or indeterminate attempt
  only after the exact session has completed its stopped transition. Skip
  records no success receipt and does not assert that the guest command never
  ran; the prior attempt still blocks same-system `once` replay. The session
  package, focused race and architecture tests, vet, all-package compilation,
  gofmt, and diff checks passed locally for this increment;
  [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36083799557)
  passed. Alpha-only public `session action run|retry|skip` commands now route
  to the exact controller and journal service. An uncertain result reports its
  attempt UUID and recovery commands; successful output requires a receipt
  digest. Full app, CLI, and architecture packages, focused app race tests,
  vet, all-package compilation, CLI build, gofmt, and diff checks passed
  locally; hosted CI is pending. The existing qualified generic base predates
  this helper. Normal recipe admission and real-VM qualification remain.
  Public recipes still reject `once`, `reconfigure`,
  `startup`, and `launch`. The helper also refuses desktop `launch` until its
  graphical environment is designed.
- Internal free capacity was about 30 GiB at the latest check.
  The export guard previously required about 25.8 GiB. Check capacity before
  another expensive host trial and report any renewed shortfall; no further
  cleanup is planned.

## Remaining acceptance

1. Rebuild and qualify a new generic base, then enable action-bearing public
   recipes and guest-only `once`, explicit
   `reconfigure`, `startup`, and `launch` with visible retry/skip and truthful
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
