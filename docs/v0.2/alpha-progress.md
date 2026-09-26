# Boxwarden v0.2 alpha progress

Updated: 2026-09-26 UTC. Integration branch:
`weshofmann/feature/v02-alpha`; [Draft PR #12](https://github.com/weshofmann/boxwarden/pull/12).
The mission deadline is 2026-10-03 14:44 UTC. This remains a functional
prototype with material acceptance gaps, not an alpha-ready release.

## Current checkpoint

- Published `c94c915` removes the stale 90-minute serial wait that overrode
  the builder's four-hour installer deadline. The new regression failed
  before the fix; targeted serial/basebuild tests and the full local Go suite
  passed afterward. [Hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36188021072)
  passed its deterministic matrix. Independent read-only delta review found
  no Critical or Important issue. Real installer completion was unproved at
  that source checkpoint.
- Published `5c686d1` updated the sanitized progress, review, and quickstart
  records; its [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36190652530)
  passed. A read-only public-path audit of that head found no new Critical or
  Important source defect. It established that stopped import verification
  must precede rebuild/replacement, and that public `session start` must follow
  rebuild to run pending automatic actions. Both remain untested in this fresh
  ChatGPT workflow.
- Published `e4da704` corrected the public acceptance runbook and review
  ledger for that order. Its [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36191518555)
  passed. These source and documentation checks do not qualify the running
  installer or its future clone.
- A fresh public ChatGPT base preparation from published `c94c915` reached
  the exact installed-guest prompt and entered `preparing` by 2026-09-25
  18:11 MDT, after more than three hours of owned installer runtime. This
  demonstrates that the corrected serial wait did not cut off the installer
  at the old 90-minute cap. The public prepare then exited nonzero during
  guest-only recipe preparation at about 18:20 MDT. Its exact journal records
  `failed`, and the candidate is stopped. The fixed guest failure marker does
  not identify the failed command; investigation is in progress. This attempt
  and the two earlier timed-out candidates remain failed evidence.
  Finalization, independent-clone qualification, and cache admission did not
  run. Neither a prepared ChatGPT base nor its package or window is qualified.
- Read-only inspection of a separate stopped forensic clone found the recipe's
  apt packages installed and no ChatGPT defaults file, placing failure before
  the package install. The pinned package's control fields are correct, but
  [Ubuntu's `dpkg-deb` contract](https://manpages.ubuntu.com/manpages/noble/man1/dpkg-deb.1.html)
  prefixes names when several fields are requested. The guest helper compared
  that output with bare values. Its existing success test reproduced the
  mismatch with the documented output before the source correction and passed
  afterward. The guest source fixtures, relevant Go packages, and full local
  Go test suite passed after the correction. [Hosted CI for `56793ca`](https://github.com/weshofmann/boxwarden/actions/runs/36205538671)
  passed gofmt, full Go tests, race tests, vet, and build. Guest stderr was
  discarded by the preparation runner, so the exact failed guest command is
  not independently recorded. A fresh real-VM preparation and qualification
  are still required.
- Two separate 64 MiB workspace volumes are formatted, identity-checked,
  available, and unattached. The staged credential-free three-file source
  matches the tracked example byte for byte. Guest attach/import and the
  retained-workspace loop remain pending.
- A fresh public ChatGPT base preparation from clean published `52bfd49` is
  failed evidence. It reached the exact installed-guest
  prompt at 2026-09-26 03:40 UTC, after about two hours 51 minutes of installer
  runtime, then failed during recipe preparation at about 04:01 UTC. The
  retained public process exited 1, the exact journal records `failed`, and
  full-visibility Tart observation confirms the candidate stopped. The
  source-bound CLI, pinned ISO and tools,
  full-visibility host doctor, and recipe admission passed preflight. Its
  private exact attempt and process result are retained. The fixed failure
  marker does not identify the guest command. No prepared base or ChatGPT
  installation is claimed. Finalization, independent-clone qualification,
  and cache admission did not run. [Hosted CI for the `36b6551` documentation checkpoint](https://github.com/weshofmann/boxwarden/actions/runs/36216356185)
  passed its deterministic matrix; it does not prove host qualification.
- Separate forensic copies preserved the failed original. Read-only log
  inspection and journal-only replay on a disposable copy recovered no exact
  failure attribution. A bounded diagnostic boot timed out before serial
  readiness; that copy is stopped. These observations do not prove that slow
  storage, apt, or the ChatGPT helper caused the preparation failure.
- The guest preparation helper now atomically retains a private typed result
  before each command and before reporting failure: preparation key, command
  position, step ID, outcome, and exit code. It retains no child text, argv,
  environment, or exception text and is not an admission receipt. The missing
  failure-record regression failed before the change; all eight preparation
  tests, generic seed/finalizer/ISO fixtures, ChatGPT installer tests, and the
  full local Go suite passed afterward. [Hosted CI for `6c9815b`](https://github.com/weshofmann/boxwarden/actions/runs/36219520293)
  passed the deterministic matrix. Real-guest use of this new record remains
  pending.
- The pinned ChatGPT installer also retains a private typed stage record:
  pinned package digest, last started stage, outcome, failure category, and
  exit code. Download and validation failures reproduced missing records
  before the change; seven installer tests now pass, including metadata
  timeout and apt nonzero attribution without private child data. Seed/ISO
  fixtures, all eight preparation tests, and fresh affected base preparation
  and qualification package tests also pass. [Hosted CI for `9a36fd8`](https://github.com/weshofmann/boxwarden/actions/runs/36220077079)
  passed the complete deterministic matrix with both diagnostic changes.
  Real-guest use of the new records remains pending.
- A bounded diagnostic copy verified the original helper digest, then the
  helper exited 1 before download with temporary DNS resolution failure.
  Its boot logs show a D-Bus startup timeout and failed NetworkManager
  dependency. The copy stopped normally and its exact result is retained
  privately. Host resolution works; the original attempt's logs show a
  connected NetworkManager, so this does not attribute that earlier failure.
- A disposable diagnostic copy reached its installed prompt, but its probe
  command was not delivered completely. An isolated host PTY regression
  reproduced a short write that the diagnostic harness ignored; the guest
  received the same prefix without the terminal newline. The regression failed
  on that harness and passed after bounded full-write handling. The corrected
  harness is retained privately. The failed harness exited 1 at its
  result deadline, and full-host observation confirms its copy stopped.
  Its exact result is retained privately. Boxwarden's production
  serial transport already handles short writes. This diagnostic defect does
  not attribute the original preparation failure, and no helper execution or
  qualification is claimed from this probe. No new full preparation is running.
- The corrected diagnostic delivered its complete command and stopped normally.
  NetworkManager started and reported readiness, but the DNS/digest/helper
  chain returned 2 without proving DNS resolution or helper invocation. The
  chain's exit label is not an independently captured helper exit. Read-only
  stopped-copy logs show a DHCP lease and a DNS-plugin readiness
  warning; the original attempt also contains that warning. This suggests a
  common resolver problem but does not establish the original failed command.
- The latest disposable probe completed and stopped normally. DNS already
  worked before resolver restart; the exact original helper digest matched,
  invocation was recorded, and its actual exit was 1 with HTTP 403 from the
  pinned package endpoint. Resolver restart necessity is not established.
  This fresh diagnostic failure does not attribute the historical preparation.
- On the host, the same Python request received HTTP 403 with its default
  User-Agent and HTTP 200 with only an honest `Boxwarden/0.2 package-verifier`
  User-Agent added. The pinned URL and expected content length matched;
  only one response byte was read. The existing owned package matches the
  pinned digest. The installer now identifies its downloader with that fixed
  header while retaining URL, size, digest, and package identity checks.
  The actual-request regression failed before the fix and passed afterward.
  Eight installer tests, eight preparation tests, seven launcher tests,
  generic guest fixtures, and fresh affected Go package tests passed.
  Independent delta review found no Important or Critical finding.
  A full local Go rerun and real-guest use of the new header were not performed
  at this checkpoint. The first guest comparison stopped normally, but DNS
  failed before either HTTP request ran. The next fresh disposable copy
  completed the actual comparison: default Python request HTTP 403, named
  Boxwarden request HTTP 200, exact pinned URL, expected content length, and
  one byte read. DNS worked before the conditional recovery, so no recovery
  was needed. The original process, wrapper and Tart exited zero, and full-host
  observation confirms all diagnostic VMs stopped. Exact results are retained
  privately. This proves the request-header constraint in the guest; it does
  not prove full download, installation, original-failure attribution, or base
  admission. A fresh public preparation using the corrected tracked helper is
  next; failed and forensic images will not be reused.
  [Hosted CI for `85c11d5`](https://github.com/weshofmann/boxwarden/actions/runs/36224786585)
  passed the deterministic matrix. A clean source-bound CLI passed host doctor
  and pinned recipe/ISO checks. A conditional acceptance plan is
  durably retained with all twelve gates pending and commands disabled; the
  next baseline must retain its own exact source, process and admission gates.
  [Hosted CI for `fd41e36`](https://github.com/weshofmann/boxwarden/actions/runs/36225132633)
  also passed its deterministic matrix.
- [Hosted CI for `a66d939`](https://github.com/weshofmann/boxwarden/actions/runs/36220982974)
  passed its deterministic matrix. A CLI built from that clean source passed
  full-host doctor and the pinned recipe/ISO check. A replacement conditional
  acceptance plan is durably retained with all twelve host gates pending and
  no executable commands or candidate binding. The earlier plan depended on
  a failed builder and different guest inputs and must not be used.
- Independent source review at `5af7277` found no Important or Critical issue
  in the typed diagnostic changes; all 15 affected Python tests passed.
  [Hosted CI for that checkpoint](https://github.com/weshofmann/boxwarden/actions/runs/36221684989)
  also passed. These checks do not prove real-guest behavior or admission.
  [Hosted CI for the `be31d38` review record](https://github.com/weshofmann/boxwarden/actions/runs/36221984837)
  passed its deterministic matrix.
  [Hosted CI for `4fea9ef`](https://github.com/weshofmann/boxwarden/actions/runs/36223070383)
  also passed.
- The `11ba8ce` documentation/source-identity checkpoint's
  [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36226186030)
  passed the deterministic matrix. A fresh frozen-source integration audit
  found no new Important or Critical defect in the reviewed paths. It found a
  quickstart output-label mismatch, now corrected to `actions: complete`.
  No tests or real-host acceptance were rerun by that review. Clone qualification
  alone does not independently prove custom preparation results or the GUI.
- The fresh public preparation from clean published `11ba8ce` terminally
  failed during `installer-running` at 2026-09-26 11:18 UTC. The original
  process and its private wrapper result both exited 1; the exact attempt
  journal records `failed`. The installed-guest marker did not arrive before
  the four-hour phase deadline. Full-host observation confirms the candidate
  stopped and no VMs running. Its exact terminal result and disk identity are
  archived privately. This failure precedes recipe preparation, so it does
  not test the corrected downloader or new typed preparation records.
  Full-host doctor, pinned inputs, capacity and source staging passed preflight;
  they do not establish installer completion. Failed images remain immutable.
  No finalization, independent-clone qualification or cache admission is claimed.
- Read-only inspection of a separate stopped derivative recovered installer
  completion followed by an installed-system boot failure. The boot log reports
  a timeout waiting for the EFI filesystem identity, followed by failed
  `boot-efi.mount` and `local-fs.target` dependencies. The actual EFI filesystem
  identity matches `/etc/fstab`, so a simple stale-UUID mismatch is refuted.
  The missing runtime device-link cause remains unknown. No boot, filesystem
  repair or journal replay was performed by this inspection; attachments were
  detached and the original disk identity remained unchanged. Exact logs and
  the comparison are archived privately. These observations are diagnostic,
  not preparation success or base admission.
- A fresh bounded diagnostic copy reproduced the EFI mount dependency failure,
  then reached the exact serial prompt. Its fixed read-only queries found the
  correct EFI filesystem and UUID link present, with udev active. The first
  automatic login timed out; a subsequent login succeeded. The diagnostic
  process, wrapper and Tart exited zero, all VMs are stopped, and original/
  predecessor identities remained unchanged. This refutes a permanently missing
  EFI identity and does not attribute the original prompt timeout. A small idle
  sequential write/fsync sample was fast on physical APFS and slower inside the
  qualification image; it does not measure VM random I/O or prove storage cause.
- Next: use a fresh diagnostic derivative to recover bounded runtime
  original-boot journal evidence for that failure before another expensive
  baseline. Preserve the original and completed forensic derivative. Do not
  increase the deadline or attribute slow storage without evidence. All twelve
  fresh acceptance gates remain pending. After a successful fresh preparation,
  require the exact stopped candidate, cache, passed independent-clone receipt
  and healthy doctor before public session creation and the complete workspace/
  GUI acceptance sequence. Real sign-in remains a human acceptance action.
- Current external image storage remains in use. Relocation is deferred until
  Wes supplies a separate disk and explicit direction. No capacity guard is
  currently blocking investigation.

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
  The internal Data volume was about 12 GiB free at that checkpoint. Per the operator's
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
- A tracked credential-free action recipe now provides observable `once` and
  `startup` evidence: a guest-home marker plus a counter that should advance
  across a later stop/start. Recipe admission and a temporary-directory
  execution of the exact embedded Python steps (counter `1` then `2`) passed
  locally. No real VM has executed this recipe yet.
- At the current source integration checkpoint, `go mod verify`, `go vet
  ./...`, the host and guest-bootstrap command builds, and Linux ARM64 guest
  formatter/inspector cross-builds passed locally with external temporary
  storage. `go test -count=1 ./...` passed every package except one
  `internal/workspacex` inspector-bundle test: its intentional reserve check
  includes `/private/tmp` on the low-space internal Data volume and refused
  before invoking the builder. [Hosted CI for `5e27267`](https://github.com/weshofmann/boxwarden/actions/runs/36094673029)
  passed shell syntax, golden source fixtures, gofmt, full Go tests, full race
  tests, vet, and CLI build. The local `go build ./...` command is inapplicable
  on Darwin to Linux-only guest command packages, which were cross-built
  separately.
- [Hosted CI for the verification-record checkpoint `9336d35`](https://github.com/weshofmann/boxwarden/actions/runs/36095116531)
  also passed its full deterministic matrix. A later read-only capacity check
  found about 11 GiB free on the internal Data volume, while the mounted alpha
  qualification state and Tart store had about 114 GiB and 271 GiB free on
  external volumes. The internal decline keeps new VM and export attempts
  paused under the mission's host-space floor. No further cleanup is planned.
- A static dependency audit compared all 46 direct `Depends` clauses in the
  pinned official ChatGPT ARM64 package with [Canonical's Ubuntu 24.04 ARM64
  `main` package index](https://ports.ubuntu.com/ubuntu-ports/dists/noble/main/binary-arm64/Packages.xz).
  Each clause has a named package or virtual provider;
  the renamed `t64` libraries provide the required older package names.
  This does not prove full apt dependency closure, installation, or GUI launch.
  The internal Data volume remained at about 12 GiB free at this check, so
  real-VM qualification and stopped-volume export remain paused.
- Reusable-base apt installs now use `--no-remove` for declared recipe
  packages and the pinned ChatGPT package, so a dependency conflict cannot
  silently remove installed desktop packages under `-y`. The command-plan
  tests failed before the change and passed afterward; the fake ISO digest
  mapping check also passed. [Hosted CI for `41f6aa6`](https://github.com/weshofmann/boxwarden/actions/runs/36097021662)
  passed shell syntax, golden fixtures, gofmt, full Go tests, race tests,
  vet, and build. A binary built from that clean published source reported
  healthy host doctor, and the public alpha recipe check admitted the tracked
  ChatGPT recipe and verified the pinned installer ISO. The admitted Tart
  inventory showed no running VM. An actual guest apt transaction and GUI
  launch remain untested while internal Data space stays below the host floor.
- [Hosted CI for the published-source preflight checkpoint `f435aab`](https://github.com/weshofmann/boxwarden/actions/runs/36097732422)
  passed shell syntax, golden fixtures, gofmt, full Go tests, race tests,
  vet, and build. The integration branch has no unpublished source work.
  At the latest read-only check, internal Data had about 14 GiB free, below
  the mission floor (about 22.8 GiB on this volume) and the stopped-export
  guard (about 25.8 GiB). The next safe acceptance step is a fresh public
  ChatGPT recipe prepare and ordered-action VM run after that headroom is
  restored. No more cleanup is planned under the operator's direction.

## Stabilization after capacity recovery

- Wes cleared host storage. The latest read-only check found about 81 GiB
  available on internal Data, with 114 GiB in the mounted qualification state,
  271 GiB in the mounted Tart store, and 508 GiB on the external backing
  filesystem. Host doctor was healthy and all 31 admitted Tart VMs were stopped.
  The prior capacity gate is cleared; each operation still has to pass its own
  reserve and expected-allocation check. No further cleanup was performed.
- Cumulative source review is frozen at `8edbd66` against merge base `e16f239`;
  [the review ledger](alpha-review-ledger.md) records scope, reviewers,
  concrete findings, disposition, and the one-time diff inventory. No fresh
  real-VM trial has run since capacity recovery.
- Commit `518a4be` rejects guest session actions that cannot satisfy the fixed
  action protocol before preparation or VM start. The five-case regression
  failed before the change and passed afterward. Focused recipe, app,
  basebuild, architecture tests and recipe vet passed locally; [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36157627198)
  passed its deterministic matrix.
- Commit `a7de9c7` moves inspector/formatter VM access onto each VM's serial
  queue and adds hosted Swift, guest/tooling Python, and Linux ARM64 helper
  build checks. The queue test found 23 baseline off-queue accesses and passes
  corrected source; Swift builds, six Python suites, and synthetic inspector
  disk admission passed locally. Independent review found no remaining
  Important issue in that bounded correction. [Hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36158247998)
  passed its expanded deterministic matrix, including the full Go race suite.
  Replacement helper bundles and fresh synthetic helper VM runs are pending.
- Review also confirmed an unbounded guest-controlled SFTP readback into host
  staging, a stopped-candidate rebuild recovery gap, and crash-left action
  attempt temporaries that block public recovery discovery. Their reviewed
  source corrections and helper trials are recorded below; real guest import,
  rebuild recovery, and controlled export still need acceptance.

## Reviewed corrections and fresh helper qualification

- The three source corrections are now published: `04a587a` preserves exact
  published action-attempt authority across an interrupted temporary write;
  `c0ba7d8` caps pinned SFTP import readback before writing host staging;
  `227fa7b` resumes only the exact stopped rebuild candidate in Ready or
  Retiring, with established pin and recipe binding. Independent delta reviews
  found no remaining Important/Critical production defect in those paths.
  Targeted tests, `go test -count=1 ./...`, `go vet ./...`, and [hosted CI at
  `227fa7b`](https://github.com/weshofmann/boxwarden/actions/runs/36159944944)
  passed. The review ledger records the tests and remaining real-host gates.
- Signed formatter and inspector helpers were rebuilt from clean published
  `227fa7b` with pinned inputs. One fresh no-NIC formatter VM trial produced a
  clean ext4 volume with the requested UUID; the host checked the unchanged raw
  identity, ext4 header, stopped VM, and reaped helper. One fresh no-NIC,
  read-only zero-disk inspector VM trial reported stopped/reaped state, a
  bounded stream, and unchanged disk bytes. The private logs and raw images
  were moved from temporary storage to the approved evidence archive and
  verified byte-for-byte. These synthetic trials verify the corrected helper
  boot paths; controlled export of a stopped workspace remains untested at
  this source head.
- The latest operation check found about 78 GiB free on internal Data; the
  qualification state still had about 114 GiB free. The old capacity blocker
  remains cleared. The next public preparation attempt is recorded below.

## Current qualification result

- From clean published `3bec60d`, the public ChatGPT `alpha prepare` reached
  the 90-minute installed-guest marker deadline. It returned a serial wait
  timeout while Ubuntu Desktop still displayed `Copying files...`. The VM disk
  continued receiving writes near the deadline, so the visible page alone
  does not establish the cause. The attempt journal records failure during
  `installer-running`; the builder stopped the exact candidate and exited.
  Its failed VM, journal, and logs are retained as private evidence. This
  attempt did **not** qualify a reusable base or ChatGPT installation.
- After the failure, internal Data had about 73 GiB free, the qualification
  state 107 GiB, and the Tart store 262 GiB. Disk headroom is not the current
  blocker. Targeted read-only extraction of the stopped target's `dpkg.log`
  found package unpack/configure activity from 16:49 through 18:01 UTC.
  `apt/history.log` shows unattended upgrades beginning at 17:49 with 364
  packages selected and no completed transaction before the stop. Dpkg
  processed 68 upgrade actions after that start, including activity in the
  final minute. The installer was still doing security maintenance at the
  timeout; these logs do not establish why this VM's storage was slow.
- An idle 256 MiB incompressible, uncached benchmark measured about
  466/428 MiB/s write/read on the direct external SSD and 279/258 MiB/s on
  the encrypted qualification volume, with matching readback digests. A
  temporary regular file in the encrypted Tart store read at about 355 MiB/s,
  but two existing VM disk images read populated extents at only 3–4.4 MiB/s.
  The USB SATA bridge negotiated 10 Gb/s. The severe slowdown is specific to
  the VM disk images or their allocated extent layout; fragmentation and
  copy-on-write overhead are hypotheses, not established causes. Temporary
  benchmark files were removed. A bounded 64 MiB synthetic sparse random-I/O
  trial on the same store did not reproduce the existing disk images' 3–4.4
  MiB/s reads, so the current evidence does not isolate a filesystem cause.
  The failed candidate remains immutable evidence.
- The active unattended-upgrades transaction exceeded the old 90-minute
  installed-guest marker window. Source now gives that phase a four-hour
  ceiling while retaining the updates, and gives recipe preparation a
  two-hour ceiling around three separately bounded commands (30 minutes
  each; the pinned ChatGPT installer remains bounded to 20 minutes inside
  its command). Targeted basebuild, app, and recipe Go tests, 15 guest Python
  tests, fake-ISO mapping, gofmt, and diff checks passed locally. [Hosted CI
  for `cc1cdb9`](https://github.com/weshofmann/boxwarden/actions/runs/36176930131)
  passed its deterministic matrix. This is source verification, not a
  successful real installer.
- A private binary built from clean published `cc1cdb9` passed host doctor
  and the public tracked ChatGPT recipe/ISO check. Two fresh invocations were
  rejected before VM creation because their auxiliary executables failed
  admission. The subsequent fresh public attempt again ended at about 90
  minutes in `installer-running`: the builder supplied a four-hour context,
  but the serial marker wait independently imposed its old 90-minute limit.
  The exact candidate is stopped; its failed journal, VM, and logs remain
  private immutable evidence. No prepared ChatGPT base is qualified.
  The serial wait now requires a bounded caller context and uses its deadline
  without a second shorter timer. A regression check failed before the fix;
  targeted serial and basebuild Go tests passed afterward. This is source
  verification; a new attempt and independent-clone qualification remain.
  At the last capacity check, internal Data had about 73 GiB free,
  qualification state 97 GiB, and Tart store 254 GiB.
- A tracked ChatGPT plus `jq` recipe is staged for a later software-changing
  rebuild. It retains the original startup action and workspace declaration;
  only the declared apt package set differs. The structural delta check,
  public recipe/ISO admission, targeted recipe and app tests, and exact key
  calculation passed locally: the current ChatGPT key is `bdc87cda3efe...`,
  while the `jq` variant is `470730d0e0f8...`. This is source verification.
  [Hosted CI for `5fe9283`](https://github.com/weshofmann/boxwarden/actions/runs/36180071270)
  passed its deterministic matrix. A private binary built from that exact
  clean source passed host doctor and public recipe/ISO admission. A private
  synthetic import source now contains all three tracked example files with
  owner-only modes and matching bytes; no guest import is claimed from this
  staging. [Hosted CI for `5dd5cca`](https://github.com/weshofmann/boxwarden/actions/runs/36181523273)
  passed its deterministic matrix. A private signed formatter bundle built
  from clean `5dd5cca` passed artifact-digest, signature, and read-only
  production admission checks. The exact-source CLI passed host doctor and
  public `workspace create` returned `available` for a fresh 64 MiB volume.
  Its verified format journal and available record bind the same raw inode
  and filesystem UUID; an independent read-only ext4 header check confirmed
  clean state, the requested UUID, and no recovery flag. The raw digest and
  resource ownership are retained privately. The installer remained live and
  writing throughout. No guest mount, import, replacement base, rebuild, or
  retained-workspace byte proof is claimed from this trial yet.
- [Hosted CI for `a6ae66b`](https://github.com/weshofmann/boxwarden/actions/runs/36183683850)
  passed its deterministic matrix. An independent source-only review found no
  Important/Critical defect in the ChatGPT guest installer, launcher, and
  automatic startup route; 11 targeted guest Python tests and selected Go
  action, recipe, and app tests passed. This does not prove a visible window
  or waiting-for-sign-in state. From clean published `a6ae66b`, a second
  private signed formatter bundle passed artifact-digest, signature, and
  read-only production admission checks. The exact-source public
  `workspace create` returned `available` for another fresh 64 MiB volume. Its verified
  format journal and available record bind the same raw inode and filesystem
  UUID; an independent read-only ext4 header and raw digest check found clean
  state and no recovery flag. This volume remains unattached for the later
  independent fresh workflow. The live ChatGPT installer was not touched.
  Existing external image storage remains in use; image relocation is deferred
  until the operator provides a separate disk and direction.

## Remaining acceptance

1. Require terminal success from the current fresh ChatGPT base preparation,
   an exact stopped candidate, and a passed independent-clone qualification.
   Then create a public session and verify ordered `once` and `startup`
   actions, actual desktop launch, and waiting-for-sign-in without treating
   management READY as application proof.
2. Import the tracked synthetic project, verify it with an initial stopped
   export while its original session/backend still owns the volume, then
   qualify stop/start retention, software-changing rebuild with explicit
   public start, replacement-sandbox reattachment, and a new independently
   compared stopped export. Repeat the essential workflow from a separate
   fresh tracked-source sandbox and volume.
3. Run final source and real-host acceptance checks, resolve review findings,
   record limitations, and update the Draft PR. Wes's real provider sign-in
   and subjective GUI acceptance may remain human actions.

## Publication policy

Publish each meaningful independently verified increment promptly on the
alpha branch, with targeted checks for small changes and full integration
checks at mission gates. Keep this record and the Draft PR current. Keep
private host evidence, VM disks, credentials, and vault keys out of Git.
Never push or merge into `main`, rewrite published history, or bypass checks.
