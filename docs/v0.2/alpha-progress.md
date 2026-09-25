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
| Recipe identity | Preparation has a reusable cache key; the canonical recipe has a separate immutable digest. Public create persists the digest before cloning, and rebuild journals old and candidate intent with system identity. Source tests pass; a software-changing rebuild still needs real-host qualification. |
| Guest actions | A fresh base from the corrected finalizer passed independent-clone qualification and cache admission. A new recipe-bound clone reached READY, ran synthetic `reconfigure` create and verify steps with checked receipts, refused same-generation replay, then restarted to READY and verified the marker again in a new generation. Both public stops were guest-accepted without force. This proves one synthetic guest-only action path, not ordered `once`/`startup`, desktop launch, or provider integration. |
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
  locally; [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36084558094)
  passed. The existing qualified generic base predates
  this helper. Explicit `reconfigure` steps are now admitted by the runnable
  recipe loader because they have an explicit public command and durable
  recovery route. Targeted recipe and app tests passed locally, and
  [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36086586857)
  passed. The real-VM action result is recorded below. At that checkpoint,
  public recipes still rejected `once`, `startup`, and `launch`.
  The helper also refuses desktop `launch` until its graphical environment is
  designed.
- A separate encrypted external qualification state passed host doctor,
  domain-CA initialization, exact installer and auxiliary-tool checks. A fresh
  generic base completed finalization, independent-clone qualification, and
  cache admission. One explicit `reconfigure` action then failed before the
  guest could claim or run it: finalization had changed the private guest
  action-state directory from `0700` to `0755`. The failed attempt and VM
  remain private evidence; the VM was stopped through the public path. A
  source fixture reproduced that defect and now verifies the private mode and
  refusal of a public directory. [Hosted CI for the source correction](https://github.com/weshofmann/boxwarden/actions/runs/36088089572)
  passed its full deterministic matrix. The corrected fresh preparation passed
  installer, finalization, independent-clone qualification, and cache
  admission. The published `ccad8c9` CLI created a fresh clone from that
  cache and completed the synthetic action sequence summarized above. A public
  read-only action list reports bounded exact session attempts and recovery
  commands after client output loss; its source tests and one read of the
  retained failed attempt passed locally.
  [Hosted CI for the list increment](https://github.com/weshofmann/boxwarden/actions/runs/36089431070)
  passed its deterministic matrix. Internal free capacity was about 26 GiB
  after the action trial, close to the earlier 25.8 GiB stopped-export guard.
  Recheck before any export; no further cleanup is planned.
- A source-only automatic-action planner now derives ordered `once` then
  `startup` steps from the exact recipe and durable attempt journal. It
  recognizes successful or deliberately skipped steps at the correct system
  and generation, and blocks unresolved attempts or out-of-order completion.
  Session and architecture tests, the session race test, vet, gofmt, and diff
  checks passed locally; [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36091192576)
  passed. At that source-only checkpoint, `session start` did not invoke the
  planner yet.
- The action service now has a bounded automatic runner. It reloads the exact
  record, recipe, and attempts after each checked receipt; the reservation
  path independently enforces the next `once`/`startup` step under locks.
  Tests cover ordering, interruption without replay, and startup on a new
  generation. Public alpha `session start` now calls the runner only after
  exact management READY and reports `management-readiness` separately from
  automatic action completion or a blocked attempt. Read-only `session status` now
  reports `pending`, `blocked`, `complete`, `unknown`, or `unavailable` action
  state separately from live management readiness. Tests cover pending work,
  an unresolved reservation, live-readiness drift, and a corrupt journal.
  Runnable alpha recipes now admit `once` and `startup`; `launch` remains
  closed. Recipe, preparer, app, CLI, session, and architecture package tests,
  targeted vet, gofmt, and diff checks passed locally for this admission
  increment; [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36092521283)
  passed its deterministic matrix. The public ordered flow still needs
  real-VM qualification.
- At the latest capacity check, the internal Data volume had about 14 GiB
  available, below the stopped-export guard. One broad local app test fixture
  hit its free-space reserve on the internal temporary filesystem; the same
  app, CLI, session, and architecture packages passed when temporary test
  state used the external Boxwarden directory. Host export and other large VM
  operations are deferred until headroom is restored; source work continues.
- The official Ubuntu 24.04 ARM64 ChatGPT desktop package is now identified
  by exact byte count, version, and SHA-256 in `alpha-decisions.md`. Its
  installer scripts were inspected as data. Guest installation, dependency
  compatibility, a visible graphical window, and sign-in are unverified.
  The internal Data volume subsequently fell to about 13 GiB free; real-VM
  work remains paused under the host free-space floor.
- The tracked ChatGPT alpha recipe now invokes a guest-only installer during
  reusable-base preparation. The fixed guest definition and remastered ISO
  bind that helper by digest. It checks the official version-specific package
  URL, exact byte count and SHA-256, Debian identity, and installed identity;
  it suppresses the package's optional apt-source registration. Four synthetic
  helper tests, the fake-ISO mapping check, recipe and basebuild package tests,
  shell syntax, and diff checks passed locally. This is source verification:
  no guest installation, dependency resolution, or graphical launch has passed.
  The internal Data volume is now about 12 GiB free. Per the operator's
  direction, no further VM or temporary-file cleanup is planned; large host
  qualification remains deferred until sufficient headroom is available.
- The same recipe now has a `startup` helper that requires an active graphical
  user manager and requests the exact packaged ChatGPT executable as a
  transient user service. It checks the session bus and display environment,
  and avoids submitting a second launch when its unit is already active.
  Synthetic helper and ISO-binding tests are passing locally. A successful
  process request cannot establish a visible window or sign-in readiness;
  those remain real-VM acceptance items.
- The launcher now waits up to 90 seconds when management becomes READY before
  the graphical session, bus, or display is available. Unsafe runtime metadata
  still fails immediately. Seven synthetic launcher tests and the fake-ISO
  source mapping check passed locally; the ordering is not yet VM-qualified.

## Remaining acceptance

1. Qualify the pinned ChatGPT prepare step and ordered `once` and `startup`
   phases in fresh public real-VM flows when host disk headroom is adequate;
   then design graphical-session handling for `launch` and waiting-for-sign-in
   behavior.
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
