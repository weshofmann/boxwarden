# Boxwarden v0.2 alpha cumulative review ledger

Snapshot frozen for review: base and merge base
`e16f23962b43337f02de1fbe2779df00a28f3f1a`; published head
`8edbd66526a40c56ef3796bbd8c7019643ea37d9`. Reviewers read that
commit, not the moving working tree. Later commits require delta review before
any claim that the newer head has been reviewed. This is a source review;
real-host acceptance remains separately recorded in [alpha progress](alpha-progress.md).

## One-time diff inventory

`git diff --numstat --no-renames` from the frozen base to head, with test paths
classified before implementation paths and binary rows separated:

| Category | Files | Added lines | Removed lines |
| --- | ---: | ---: | ---: |
| Production host code | 147 | 21,972 | 231 |
| Guest/tooling code | 36 | 4,314 | 28 |
| Tests and fixtures | 140 | 20,060 | 102 |
| Documentation and examples | 20 | 2,217 | 14 |
| Generated/binary artifacts | 1 | binary | binary |
| **Total** | **344** | **48,563** | **375** |

The binary row is the digest-locked guest bootstrap artifact. The category
script grouped `docs/` and `examples/` as documentation, `*_test.*`, `tests/`,
and `testdata/` as tests, `guest/`, `tools/`, and `scripts/` as guest/tooling,
and the remaining text paths as production host code. These are review-size
counts, not a quality metric.

## Review scopes and findings

The review assignments requested GPT-6 Astra at extra-high effort. Runtime
model/effort metadata was not exposed to the reviewers, so this ledger records
that routing request rather than claiming independent attestation.

| ID | Scope and reviewer | Severity and concrete failure | Disposition |
| --- | --- | --- | --- |
| H1 | Swift helper and host boundary; `/root/swift_host_boundary_review` | **Important.** At the snapshot, `tools/alpha-inspector/boot.swift` and `tools/alpha-formatter/boot.swift` accessed `VZVirtualMachine` delegate, state, and devices outside their custom serial queues, contrary to [Apple's queue contract](https://developer.apple.com/documentation/Virtualization/VZVirtualMachine/queue). | **Fixed in `a7de9c7`.** Fresh review found no remaining Important/Critical defect in this correction or the bounded receiver scope. Local queue regression detected 23 baseline accesses and passes corrected source; Swift builds and synthetic preflight passed. [Expanded hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36158247998) passed; fresh helper VM trials remain. |
| S1 | Workspace/import boundary; `/root/storage_snapshot_review` | **Important.** `internal/sshx/import_sftp.go:106-128,184-185` downloads guest-served files into trusted staging before checking the 4 MiB/file and 16 MiB total manifest limits. A hostile guest can grow host state without a transfer-time byte cap. | **Fixed in `c0ba7d8`.** Host-streamed pinned SFTP v3 readback now caps packets and declared per-file/total bytes before host writes; independent snapshot admission remains. `/root/import_readback_review` found no production Important issue; its packet-limit fixture correction was included before publication. Focused tests, local OpenSSH server integration, race check, full Go suite, and [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36159944944) passed. Real guest qualification remains. |
| S2 | Rebuild recovery; `/root/storage_snapshot_review` | **Important.** A public stop after a `Ready` or `Retiring` rebuild journal leaves a stopped candidate that normal start and rebuild retry reject (`internal/session/rebuild.go:447-451,587-606`, `start.go:126-134`). Retained workspace data becomes inaccessible through the supported path. | **Fixed in `227fa7b`.** Exact journal candidate restart now requires the established host-key pin and recipe binding; only Retiring admits an already absent old backend. `/root/rebuild_recovery_review` found no remaining Important/Critical issue. Affected tests, full local Go suite, vet, and [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36159944944) passed. Real stop/retry remains. |
| R1 | Recipe admission; `/root/recipe_snapshot_review` | **Moderate.** `internal/recipe/recipe.go:137-150,233-244` admitted relative/noncanonical session action executables and oversized argv that `internal/guestproto/action.go:60-72,101-107` later rejects, after preparation and VM start. | **Fixed in `518a4be`.** The regression failed on five cases before the fix, then passed. Targeted recipe/app/basebuild/architecture tests and recipe vet passed; hosted deterministic CI passed. `prepare` keeps PATH semantics. |
| R2 | Action recovery; `/root/recipe_snapshot_review` | **Moderate.** Crash-left `.<attempt>.json.tmp-<nonce>` siblings make both action replay scanning and public listing reject an otherwise valid published attempt (`internal/session/action_attempt.go:165-185,226-228,358-383`; `action_list.go:79-87`). | **Fixed in `04a587a`.** Scans retain and ignore only an exact private temporary with a published regular target; the validated published attempt alone controls recovery. Orphan, malformed, and unsafe entries fail closed. `/root/action_journal_review` found no actionable defect; focused tests, full local Go suite, and [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36159944944) passed. |
| R3 | CI coverage; `/root/recipe_snapshot_review` | **Moderate.** Frozen `.github/workflows/ci.yml:27-42,62-76` did not execute recipe preparation Python tests, Swift compilation, alpha tooling Python tests, or Linux ARM64 formatter/inspector entrypoint builds. | **Addressed in `a7de9c7`.** Exact new commands passed locally and in [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36158247998). Existing bootstrap artifact test already cross-builds and verifies its bytes. |

## Coverage and next review

The snapshot reviews covered helper VM queue/teardown and bounded export
receiver; workspace authority, lifecycle/rebuild, import/export and partial
failure; recipe identity/action semantics, shortest public path, and CI
coverage. They did not constitute line-by-line approval of all 344 files or
real-VM behavior. Storage durability remains an empirical gate: two clean
cached-disk trials do not prove the earlier ext4 identity-loss root cause or
long-run reliability. Fresh bounded repeated content, filesystem-identity,
and clean-state checks are planned, preserving failed historical volumes.

The three correction deltas have received independent source review. Final
cumulative head review and real-host acceptance remain before the PR can be
marked Ready. No review finding authorizes weakening host guards or using
affected old helper binaries for acceptance.

The `f282f28..c94c915` serial-deadline correction also received independent
read-only review. No Critical or Important issue was found. The reviewer
confirmed that the builder's four-hour, two-hour, and ten-minute phase
contexts reach the serial wait unchanged and that marker binding, ordering,
and terminal poisoning remain intact. A Minor test-coverage note remains:
the new test proves rejection of an unbounded caller but cannot simulate the
old 90-minute cap without a virtual clock. Targeted serial/basebuild tests,
the full local Go suite, and [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36188021072)
passed. A fresh real installer attempt is still running; this review does
not qualify it.

At published `5c686d1`, a separate read-only audit followed the public
ChatGPT recipe, start, action, workspace, rebuild, replacement, and export
routes. It found no new confirmed Critical or Important source defect. The
import verifier binds the original session and backend, so the initial
stopped export and verification must precede rebuild or replacement. A later
owner can make a new stopped export for independent byte comparison. Rebuild
reports management state but does not invoke automatic actions; public
`session start` must follow it before checking action completion and the GUI.
These are acceptance-order findings, not real-host qualification.

At published `5af7277`, `/root/typed_diagnostic_review` independently reviewed
the `52bfd49..5af7277` guest preparation and pinned-installer diagnostic delta.
The requested routing was GPT-6 Astra at high effort. No Important or Critical
defect was found. The review confirmed private mode-0600 atomic replacement,
file and directory sync, completion recording before the success marker,
failure suppression of that marker, unchanged subprocess argument boundaries,
and exclusion of child output, argv, environment, and exception text. The
installer creates the private parent before execution, and host admission
does not consume these diagnostic files. All 15 affected Python tests passed
with bytecode writes disabled; the checkout remained clean. Real guest use,
GUI behavior, power-loss durability, and filesystem fault injection were not
tested. This is delta review, not final cumulative approval or admission.

## Pinned downloader identity follow-up

Independent read-only review of the two-file downloader delta from `4101382`
found no Important or Critical finding. The named User-Agent contains no
private identifier; URL, redirect refusal, byte bounds, digest and package
identity checks remain intact. The reviewer reran all eight installer and
eight preparation tests successfully. The new regression exercises the
actual Request and pinned-byte validation; its header assertion covers
explicitly configured headers, not every wire header. Host HTTP success
does not qualify guest download, installation, or the historical failure.

## Integration audit at `54c185f`

A fresh read-only reviewer audited the delta from the reviewed `8edbd665`
snapshot, checked the earlier finding dispositions, and traced preparation
and cache admission, management readiness and actions, original-owner import
verification, stopped export, stop/start, rebuild recovery, explicit start
after rebuild, and replacement attachment/export. The actual PR merge base
remains `e16f239`. No new Important or Critical defect was confirmed in these
bounded paths. The requested routing was GPT-6 Astra at extra-high effort.

One Minor documentation mismatch was confirmed: the quickstart required
`automatic-actions: complete`, while the public start formatter emits
`actions: complete`. This checkpoint corrects the quickstart; acceptance
checks must use the actual emitted field. Management readiness and GUI
observation remain separate gates.

The reviewer used exact-ref Git reads and diff checks. No tests were rerun,
private evidence accessed, files changed, or host VM operations performed.
The live builder's `11ba8ce` differs from the frozen review head only in the
progress document. This is bounded integration review, not exhaustive
line-by-line approval or real-host acceptance. Independent-clone qualification
checks management readiness, declared apt packages and clone identity;
arbitrary preparation-step results and ChatGPT GUI behavior need their own
acceptance evidence. Full download/install, graphical launch, retained bytes
through rebuild/replacement, exports and a fresh repeat remain pending.

## Required EFI device wait correction

An independent read-only reviewer checked the installer-input and CI delta
from `2bdeacb`, including the new fixture that executes the exact inline
installer Python against disposable filesystem entries. Required EFI uses
a finite ten-minute device wait, retaining its UUID/type/dump/pass and required
semantics. Other bytes and ownership/mode are preserved by a synced atomic
replacement; unexpected structure or file types fail before mutation.

The reviewer found an Important blocking FIFO open before the regular-file
check. A bounded regression reproduced that hang. Nonblocking open fixed it;
re-review found no remaining Important/Critical issue. All nine filesystem
fixtures pass, including hard links, symlinks, missing files, directories,
oversized/malformed entries, idempotence and private permission preservation.
The extracted shell/heredoc passes syntax checking. User-data already belongs
to the guest-definition digest and staging list; the new fixture is in CI.

The coordinator ran fresh affected recipe/basebuild/golden/app Go tests,
generic seed/fake-ISO/finalizer fixtures, eight preparation and eight installer
tests, and diff checks. This targeted verification is not a fresh full local
suite. Separate forensic boot evidence supports finite EFI wait and individual
normal-target completion, but still records failed snapd seeding and udisks2.
No successful fresh installation, visible GUI, cache admission or independent
clone qualification is claimed by this delta review.

## Supervisor invalid-generation fixture budget

Hosted CI at `62f43a3` failed the coexisting invalid-generation case when its
200 ms test context expired during a 370 ms fixture operation. The fixture
publishes and syncs files/directories before classification. This delta raises
only that test budget to five seconds; production deadlines are unchanged.
Both malformed and coexisting cases still require their specific classification
error, reject deadline expiration, and assert one launch with zero snapshots.

An independent read-only reviewer found no Important/Critical issue and no
weakened behavior assertion. The coordinator ran all targeted `TestExactStart`
tests ten times and three times with the race detector; both passed. The first
race invocation could not access the default build cache; the executed repeat
used a writable disposable cache. Formatting and diff checks passed. This is
targeted verification, not a fresh full local suite or host qualification.

## Scoped desktop daemon startup limits

A bounded independent read-only review checked the installer-input, filesystem
fixture and CI delta from `65db6b3`. No Important/Critical finding remained.
The existing late-command path writes fixed `TimeoutStartSec=10min` drop-ins
only for snapd and udisks2 with directory/file modes 0755/0644. Global deadlines,
configure-hook limits and waiter ordering are unchanged. The target is the
existing trusted fresh Canonical installer tree; this does not add a generalized
hostile-filesystem writer or a host integration surface.

Three behavioral fixtures execute the actual shell block against disposable
targets. All failed before the block existed, then passed: exact two finite
service limits/modes, repeated bytes, and preserved global/unrelated settings.
Nine existing EFI fixtures, generic seed/fake-ISO/finalizer, eight preparation
and eight installer tests passed. Fresh affected recipe/basebuild/golden/app
Go tests passed using a writable disposable cache; the initial app invocation
failed because the default cache was inaccessible. Diff checks passed. This
is targeted local verification, not a fresh full local suite. New hosted CI
and real installer execution remain pending. Repeated diagnostic daemon-active
results support the limits, while seeding/ModemManager gaps remain unqualified.

The later ordering-only experiment is not promoted: Firefox configure exceeded
a persisted five-minute task limit and seeding rolled back before owned cleanup.
Exact upstream 2.76 source exposes a configure timeout when new tasks are made,
but the immutable parent already contains five-minute tasks. Simply changing
the daemon environment cannot extend those existing tasks. Packaging equivalence
and supported recovery are not yet verified; failed evidence remains immutable.


## Private diagnostic observer review (2026-09-26)

The next bounded host observer received read-only review while the same-store
copy remained the sole live operation. The reviewer reproduced two Important
false passes: accepting a completion marker from accumulated boot bytes, then
accepting queued boot output in a later PTY read. Both regressions failed before
correction. The replacement generates a fresh 128-bit token first exposed in
the submitted query, excludes received boot bytes, requires original-child exit
zero, and preserves synced phase/result evidence with bounded own-child cleanup.

Six fake-PTY behavioral cases passed locally and independently: zero-exit
success, nonzero child rejection, missing-marker query timeout, shutdown timeout,
same-chunk pre-query marker rejection and queued pre-query marker rejection.
No tests were skipped in the final run. Re-review found no remaining
Important/Critical issue in this bounded observer/test delta. Failed versions
remain immutable private evidence. The observer has not been launched against
Tart; this is diagnostic-tool verification, not repository-wide approval or
real-host qualification. Historical exact child-exit receipts remain separately
required; a harness capability defect alone does not change a recorded exit.


## Conditional diagnostic preparation review (2026-09-26)

The next stopped-copy setup preserves the earlier EFI and two scoped daemon
changes, making file layout the intended trial variable. Its conditional driver
also requires the original preparation's numeric zero exit, exact final disk
identity and detach receipt before any boot. Current-state execution rejected
the absent terminal copy exit before any native tool or VM call.

Read-only review found an Important cleanup gap: native attach and plist parsing
preceded cleanup protection, and detach-phase logging could suppress detach.
An exact old attachment-code span with a fake malformed result reproduced the
missing cleanup. The replacement protects the complete attempt, refuses to
adopt pre-existing attachments, reconciles only the exact new owned image, and
performs bounded cleanup before propagating logging errors. Unresolved cleanup
prevents a successful preparation receipt. The preparation body has a finite
thirty-minute bound; cleanup has separate finite native-command bounds.

Eight fake-tool checks passed locally and independently: successful cleanup;
attach timeout, nonzero exit, malformed result and missing GUID; detach-log
failure; refusal to adopt an existing attachment; and finite repeated detach
failure. Helper/preparation compile and driver syntax/placeholder checks passed.
Both wrappers passed shell syntax, and isolated exit recorders preserved a
numeric failure while rejecting overwrite and invalid input. Re-review found
no remaining Important/Critical issue in the bounded delta. Private artifacts
are durably archived; none of these tests operated a real VM or native disk
tool. The copy remains the sole live operation, and fresh clone preparation,
boot, guest behavior and all alpha acceptance gates remain unexecuted.

## Native migration capacity review (2026-09-26)

Independent read-only review checked installed ASR, diskutil, clonefile and
hdiutil contracts plus Apple's primary APFS replication explanation. At the pre-cleanup inventory, a full
replica could not fit alongside the source while retaining the mission
free-space floor. Internal staging does not close that gap; source snapshots
and caches offer no measured reclamation. Cross-filesystem clone calls fail,
and snapshot deltas require an existing baseline on the target. No documented
compaction saving justifies proceeding with an undersized full restore.

The recommended plan stages and verifies the intact sparsebundle on temporary
storage, then restores into the exact new encrypted native volume, preserving
canonical mounting and historical evidence provenance. Incremental source
deletions and bespoke clone reconstruction are not the selected approach.
This is a capacity/method review, not approval of unprepared device-specific
commands or executed migration evidence. The post-cleanup reassessment below
supersedes the temporary-staging capacity dependency.


## Selective retirement and capacity reassessment (2026-09-26)

A fresh bounded private-manifest review checked ownership, protected history,
dependencies, live-use exclusions and exact disk/configuration identities.
It excluded one uncertain-provenance derivative. Follow-up review required
cooperating transition, volume-use, session and storage locks for older attached
sessions whose filesystem-device records differed after remount; it approved
exact-target operator retirement preserving raw disks and proofs. This is an
operator cleanup procedure, not public lifecycle admission or a source fix.

All 46 approved bundles are absent; nine alpha bundles and protected history
remain stopped. Exact before/after checks preserved all 17 workspace identities,
sizes/mtimes, formatter proofs and unrelated snapshotted records, with only
expected attachment clearances and retired session/cache/registration records.
Full-host doctor is healthy. The read-only verification harness initially
failed on a null session field; its corrected execution passed.

Effective inner availability increased 197 GiB; actual inner APFS free is
543 GiB but constrained by outer free capacity of 405 GiB. The sparsebundle's
432 GiB allocation barely changed; no outer reclamation is claimed. Source
container usage is now 97 GiB, so the retained-source native replication plan
fits a 117 GiB budget and 93 GiB outer floor with 195 GiB spare. Staging and
compaction are unnecessary on these observations. Source and outer filesystem
checks exited zero and reported healthy after the disconnect. Concrete migration
driver verification remains pending; no native volume or cutover exists.


A bounded independent native-plan review found no Critical issue and required
an exact newly created leaf-volume ASR target, with fresh UUID/container binding,
and a privileged signal/reap path for the bounded root restore. Container/physical
disk erase targets remain prohibited. The measured capacity receipt matched.

The review recommends native restore verification, filesystem consistency,
complete file/metadata inventory and small configuration digests, encryption
lock/unlock, and a tiny clone isolation check while keeping the source intact.
These checks must not be reported as independent equality of every VM disk byte.
[Apple's APFS replication explanation](https://developer.apple.com/videos/play/wwdc2019/710/)
supports the encrypted destination and sibling-volume preservation mechanism.
This is method review; concrete driver preflight and actual migration remain.


## Concrete native helper review (2026-09-26)

The first read-only preflight refused an unreadable root-owned filesystem event
log; payload live-use checks now cover VM/cache/tmp trees while clean detach
remains mandatory. Independent payload comparison excludes only the mutable
root-owned event-log subtree; native restore verification still covers the volume.
The installed Python lacked its xattr API, so metadata comparison uses the native
xattr tool. Corrected read-only preflight passed on the installed interpreter.

Review found an Important failed-cutover recovery gap and required explicit
new-volume ownership. The correction attempts exact-source restoration while
locks remain held, refuses unknown canonical mounts, records recovery outcome,
and enables/checks ownership only on the new leaf. The root wrapper pins the
helper digest and retains the actual original driver process exit separately.
Bounded correction review found no new Important/Critical issue.

Target/capacity/privilege guards, stdin framing, exact-target rollback, unknown-
mount refusal and owned-child stop/reap checks passed. A logging-error regression
first exposed inherited blocked child signals; restoring the child mask before
exec made the actual child promptly terminate/reap. Final read-only preflight
passed. No root helper, native copy or cutover has executed; attended administrator
authentication is the remaining platform prerequisite, not a new migration scope.


## Native source remount failure and recovery (2026-09-26)

The attended first migration exited 1 after clean source detach and failed
read-only attach authentication, before target creation or ASR. The helper
stripped the existing credential file's line ending and added a NUL; the
established mount path passes unchanged file bytes through EOF. Rollback
then failed by querying the unmounted canonical directory as a disk.

One bounded exact-source recovery with raw-file stdin exited zero, leaving
the source read-only at the canonical mount with ownership enabled. All ten
retained VMs are stopped, no VM files are open, payload metadata and small
digests match, and full-host doctor passed. No VM disk was fully hashed;
workspace data and original failed-attempt receipts remain unchanged.

The retry preserves exact credential bytes, handles the unmounted directory
without that disk query, and avoids detaching an already read-only source.
Three regressions failed on the original and passed on the correction. A
missing inventory mock initially failed the corrected rollback fixture; the
corrected red/green repeat passed. Actual full-host read-only preflight passed
after sandbox diskutil access was refused. Independent bounded correction
review found no Important/Critical issue, verified both artifact pins, reran all
three regressions and checked wrapper syntax. The attended retry is approved;
at that checkpoint no second root helper, new destination or native copy had
executed. The actual second attempt is recorded below.

## Native locked-volume correction (2026-09-26)

The second attended driver and original parent exited 1 after encrypted volume
creation succeeded. `-nomount` left the new destination locked; the helper's
unlocked-state assertion was wrong. No ASR started. Rollback confirmed the
source already canonical and read-only. Stateless key verification first
required an explicit crypto-user; the corrected command exited zero and left
the exact destination locked and unmounted. Failed receipts remain unchanged.

The next correction reuses only the original empty leaf, checking creation,
terminal failure, no restore, successful rollback, key verification, current
UUID/container/store and unchanged sibling volume set. It separates identity
from locked-state expectations, unlocks explicitly and inspects emptiness
read-only before erase. Bounded review found two Important sibling gaps:
unmount could re-lock the target before source detach, and the old mount
LaunchAgent could race cutover. Both are corrected: observe/re-unlock first,
and require the idle old job disabled and unloaded. Original helper/plist files
were archived unchanged. An initial quiescence verifier expected `true`, while
the installed launchctl prints `disabled`; the read-only corrected recheck passed.

Seventeen targeted synthetic cases passed, including prior recovery,
provenance/refusal, empty inspection, encryption transitions and the automatic
mount guard. The learned locked/cutover gaps failed on the prior helper and
passed on the correction. An initial sibling fixture combined locked state
with a mountpoint; the corrected consistent fixture passed. Full-host read-only
preflight passed with source inventory/small digests unchanged, all retained
VMs stopped and doctor healthy. Final independent rereview found no remaining
Important/Critical issue, verified durable helper/wrapper pins, and reran all
seventeen fixtures plus syntax checks. The concrete attended retry is approved.
Native replication, cutover and the fresh alpha workflow remain unverified.

## Explicit synthetic edit increment (2026-09-26)

The read-only demo could not prove the mission's required guest creation/edit
retention. The demo now exposes explicit `--edit`; both ChatGPT recipes offer
`edit-project` as a reconfigure action. Ordinary execution and startup do not
change the imported files. The action selects exactly one ordinary import
directory at its declared guest mount, with refusal for missing, ambiguous or
linked candidates. Foreign note/task content is not silently overwritten.

Five new filesystem cases failed before demo implementation; eight recipe
action cases and both runnable-recipe subtests failed before the steps were
added. Fourteen Node filesystem fixtures now pass. Fresh targeted recipe,
session and app Go checks, targeted vet, gofmt and diff checks passed. The
preparation-key comparison confirms the non-prepare action leaves reusable
base identity unchanged. Independent bounded review reran the fixtures, Go
checks and formatting; no Important/Critical issue remains. CI pins the
official Node setup action and exact tested Node distribution, with package
caching disabled and no npm dependency installation.

These are source/fixture checks, not real guest or lifecycle acceptance. Keep
the initial pristine stopped export/public import verification before any
edit. Then compare independently expected modified task/new note and unchanged
other source files before adopting a new edited-data baseline. Later stop/start,
rebuild and replacement require new independent comparisons. Private staged
source and acceptance input hashes from the prior revision require refresh.

## Focused post-migration integration review (2026-09-27)

Frozen revision: `6588c88e136c1d38316f245cf522f8dc044f1d05`.
Fresh operational, lifecycle/data and recipe/usability reviewers were requested
on GPT-6 Astra at extra-high effort; runtime model metadata was not independently
exposed. Existing corrected findings were respected; this is a bounded review
of subsequent changes and interactions, not exhaustive whole-PR approval.

The operational reviewer found no new blocking native mount defect. Exact
native volume/container/store, completed encryption, ownership, canonical
writable mounting, healthy doctor and ten stopped VMs passed full-host checks.
The old Tart image remains detached; the active LaunchAgent invokes only the
native helper. Eighteen safe helper policy fixtures and wrapper/plist syntax
passed. Reboot/reconnect remain untested, and no restore, benchmark or full VM
disk hashing was repeated. Native Tart and DevelData share one outer capacity
budget (about 266 GiB available); inner image availability is not additional
physical capacity.

**Important operational limitation:** qualification-image remount changed
filesystem device numbers. The two old reserved workspace files retain their
inode, size, mtime and record digests, but formatter/record admission rejects
the new device. Metadata-only attach can still succeed; subsequent start/use
fails. No supported reconciliation was found. Preserve all seventeen historical
workspace volumes and proofs without identity edits; exclude the two old
reserved volumes and use new independently named/formatted volumes for this
acceptance run. General remount recovery is unqualified. This restriction does
not block fresh public base preparation, which does not consume these volumes.

The recipe/usability pass found no new Critical/Important source defect.
Fifteen installer/launcher, eight preparation, fourteen Node filesystem/action
fixtures and fresh targeted recipe/session/app Go checks passed. Once/startup
ordering, generation-bound retry, explicit edit behavior and public start after
rebuild remain distinct from actual GUI/durability proof. One Minor quickstart
ordering defect is corrected here: the operator must set the printed export
UUID before invoking import verification. No real VM behavior was exercised.

**Important source finding:** interrupted export snapshot copy leaves a durable
Copying journal and volume Pending, but returns no transaction ID and public
resume refuses the Copying phase. Start, detach, rebuild and delete remain
blocked. The existing safe internal snapshot recovery has no production caller.
A frozen-source synthetic regression reproduced all four blocked lifecycle
paths and confirmed internal recovery releases the exact volume. Existing
interrupted-copy, recovery and resume tests also passed. Fix this public-path
composition before exercising stopped exports; base preparation is unaffected.

### Interrupted-export correction delta

The driver reproduced RED failures for the lost durable copy transaction,
public Copying-phase resume, and CLI error recovery ID. This correction returns
the exact transaction on post-reservation failure, reports its validated UUID
in the public error, and composes the existing exact stopped-backend snapshot
recovery into public resume. A partial copy aborts and releases its reservation;
`export: aborted` never claims published files. Ready snapshots continue through
the existing inspector path after any matching crash-left marker is cleared.

Bounded delta review reproduced an Important availability regression in the
first draft: ready-snapshot resume acquired the original volume's live lease
even after Pending cleared. The held-lease regression failed, then passed after
limiting recovery to this transaction's matching Pending. Completed snapshots
remain independent of the source's later runtime and attachment.

The final affected export/recovery and app routing tests, full local Go suite,
targeted vet, formatting and diff checks passed. Tests verify nil/running/foreign
backend refusal preserves evidence, exact stopped recovery clears Pending,
normal start becomes available, admitted observer routing and distinct aborted
output. Fresh delta source review found no remaining Important/Critical defect.
Its small held-lease check passed without a helper; a broader independent repeat
hit actual internal free-space reserve during fixture setup and was not green.
The guard was retained. Hard termination before UUID output still requires exact
private-journal inspection; public transaction listing remains unimplemented.
No real export recovery or GUI/durability proof is claimed.

### Ordered ChatGPT action proof delta

The recipes now reuse the existing credential-free once marker and startup
counter ahead of graphical launch, including the software-changing `jq`
variant. New regressions failed against the three-step recipes, then passed
with the five-step ordering. Sixteen Node fixtures execute the tracked Python
commands with only the fixed guest home replaced by isolated temporary paths;
they verify missing/foreign proof refusal and exact `1`/`2` counter bytes.
The full recipe package also verifies unchanged preparation keys and changed
session intent. Existing guest preparation inputs are unchanged, so the
already-running base preparation remains usable only if its own admission
succeeds. Actual session ordering, repeat-start idempotence and rebuild counter
reset remain acceptance gates. No VM behavior is claimed by these fixtures.

Fresh bounded recipe delta review found no Important/Critical issue in the
four code/example files atop `523f719`, confirming planner phase ordering and
stop-on-failure behavior. Targeted session/app action and recipe tests passed.
The reviewer inspected source and did not rerun tests or touch live storage.

### Fresh native prepared-base checkpoint

Public preparation from clean `523f719` reached terminal zero and exact cache
admission. Full-host independent verification matched journal/cache candidate,
preparation key and identity, hashed both qualification receipt files, checked
passed inventory/identity and stop proof, observed candidate/independent clone
stopped with all twelve backend objects, and confirmed native encryption and
healthy doctor. No diagnostic clone or timeout extension was introduced.
The updated `a3a7d6c` recipes retain the prepare-only projection and all nineteen
guest files; their different complete intents apply to new sessions.

Fresh operational delta review found no Important omission in the disabled
acceptance plan. Its minor version/equivalence metadata and fixed counter `2`
expectation were corrected in a new immutable plan. Every real restart,
including export-related restarts, now checks a counter increment and exact
startup attempt delta while preserving once attempts. Original exit-zero
admission remains required before enabling session commands. Two new volumes
were actually formatted and independently checked; old remount-drift records
were preserved. Public session A reused the exact admitted base and first public start exited
zero with READY and three successful actions in once/counter/GUI order. Pinned
SFTP readback independently matched once marker and startup counter `1`. Actual
Ubuntu desktop and ChatGPT waiting-for-sign-in were separately observed in the
exact session window; first-run keyring creation was cancelled without a
password. Private screenshots were archived with matching digests. Provider
sign-in, already-running idempotence, import and durability remain unqualified.


## Configured-state export staging delta (parent `15b22f9`)

A fresh Astra/extra-high reviewer was requested for the three-file export builder
and script delta, with relevant unchanged capture/resume/private-path callers.
Routing was requested through the agent tool; independent runtime model metadata
is not exposed. The initial pass found one Important issue: caches moved to
external state while compiler scratch still defaulted to the unguarded internal
disk. It is corrected with private bundle scratch, explicit Go `TMPDIR` and
`GOTMPDIR`, Swift `TMPDIR`, and wrapper child `TMPDIR`. Follow-up found no remaining
Important/Critical issue and verified Swift planned output paths with spaces,
Go scratch configuration, trap ordering, syntax and scope checks.

Root regression first reproduced fixed-path rejection and the actual internal
reserve failure. A child-observed byte regression then failed without scratch
binding and passed with it; standalone shell assertions were insufficient on the
installed Bash and are not used as the proof. Exact request/transaction lock,
changed-snapshot rejection, replaced-parent cleanup refusal, unexpected path
refusal and argument boundaries remain tested. Affected workspacex/exportx/
diskreserve packages, ShellCheck and syntax passed. Final full-suite and real-host
export results are tracked in the progress record; no host success is inferred
from this review. No trust-boundary, guard-floor or arbitrary-path CLI expansion.


### Argument-count proof follow-up

A two-line test-only delta against `e1fbb9c` also records child-observed argv count
in the Go assertion. Injecting a fourth argument made the regression fail; the
unchanged production three-argument call was restored before the passing repeat.
The fresh bounded reviewer found no issue in the test delta; root independently
verified production restoration. This closes reliance on the installed Bash's
standalone conditional/errexit behavior for the count proof. Runtime export code
and inspector build inputs are unchanged.


## Bounded cumulative source gate at `551b297`

A fresh reviewer was requested with Astra/extra-high routing, frozen at
`551b297` against base `e16f239`, building on the completed `6588c88` ledger and
bounded `523f719`, `a3a7d6c` and `e1fbb9c` corrections. Requested model routing
is not independently attested. The pass traced original-owner import verification,
edited baselines, public recovery/inspection, action ordering and the rebuild →
explicit start → replacement boundaries. No new Important/Critical source issue
was found; this is bounded composition review, not exhaustive whole-PR approval.

Clean/frozen checkout, diff and shell syntax checks passed. No tests were rerun;
private evidence and live host state were untouched. Minor stale quickstart and
remaining-acceptance descriptions are corrected to distinguish the verified first
system from pending rebuild, replacement, independent repetition and final gates.
The current `jq` installer is a single retained public operation, not admission.


### Exact public replacement deletion review

A fresh bounded read-only reviewer approved the one stopped synthetic system
manifest at source `c38b3de`; later documentation-only revisions preserve the
reviewed production contract. Exact session/backend and owned bundle identity,
all-stopped inventory, no open target/workspace files, no rebuild or volume-use
reservation, and the preservation set matched. Root repeated those guards just
before public deletion and verified target absence, retained raw identity/size,
cleared attachment and every other VM preserved afterward.

The review traced public delete locking/finalization and found no Important or
Critical blocker. No disk hashes or tests were rerun for that review; filesystem
cleanliness was not inferred from stop. Fresh replacement readback and a NEW
controlled export separately proved the edited bytes. No broader cleanup or
whole-PR approval is implied. Requested Astra/extra-high routing is not independently
attested. Logs, import/export baselines, protected history and old workspaces remain.


## Bounded software-change correction at `f5113f0`

Fresh requested Astra/extra-high review found no Important/Critical design defect
in adding a separate package-only `tree` recipe and correcting the acceptance
sequence. Routing is requested, not independently attested. Both preserved B/C
pinned inventories contain installed `jq` and no `tree` stanza. The previous jq
trial proves system replacement and data retention, not added software; its
software-change/full-cycle claim is withdrawn.

Retain A's valid pristine/edit/restart history, then rebuild B with tree, explicitly
start and verify packages/actions/GUI/edited bytes, and make a new replacement and
NEW stopped export. Independently finish C's pristine verify/edit/restart, tree
rebuild/start and D replacement/export. One freshly qualified tree base may be
reused; lifecycle and workspace observations must remain independent. Original
recipes, guest inputs and all historical evidence are preserved.

The review required runnable admission, distinct preparation key/intent, unchanged
actions/workspace declarations and tree absence/presence evidence. Added regression
failed before the variant existed, then Go recipe checks and real Node edit/once/
startup fixtures passed. These are targeted source checks, not tree VM acceptance.
No whole-PR rereview, tests or VM operations were performed by this reviewer.

The same bounded reviewer inspected the actual six-file correction over
`f5113f0` and found no Important/Critical issue. Recipe-only delta, runnable
admission/key/intent regression, both Go/Node fixture loops and corrected active
commands/claims matched. Read-only diff check passed; suites and VMs were not
rerun by the reviewer. Two minor wording fixes (all three variants; explicitly
historical next-step paragraph) were applied before publication.


### Tree launch runner delta and second workspace checkpoint

Fresh bounded review caught an Important evidence-retention failure in the
private launch runner: a receipt exception after spawning could bypass waiting
on the original builder. Corrected post-launch bookkeeping retains that same
child and records its exit/reaping separately; bookkeeping errors fail the
runner. The original failure reproduced with an isolated fake child, and five
corrected fault cases passed. Fresh delta review found no remaining
Important/Critical blocker for the exact tree preparation. Model routing was
requested as Astra/extra-high and is not independently attested. The reviewer
performed static checks only; no tests or VM operations.

The updated private plan labels original builder and jq replacement evidence
historical, with fresh tree attempt/candidate admission pending. Before later
replacement gating, active descriptions use B → E and independently C → D.
C's own public pristine import verification, explicit edit, NEW edited export,
restart data comparison and once/edit/startup counts passed. Those observations
do not complete the tree rebuild or fresh-repeat gate. One fresh public tree
preparation is running from published `e10fe50`; its terminal result remains
required. Current deterministic CI passed, and a fresh bootstrap build exactly
reproduced the tracked artifact lock.


### Pre-handoff documentation and coverage audit at `c9097ff`

A bounded read-only audit of the mission criteria, current docs and selected
acceptance metadata found no new production/security issue. It identified a
runnable quickstart mismatch: the minimal recipe has no edit action but the
walkthrough later invokes `edit-project`. The full demo now selects the
ChatGPT recipe before immutable session creation. Formatter construction and
private handoff bindings are explicit prerequisites; final delivered bindings
remain to be recorded at the final source checkpoint.

Stale C edit/restart-pending wording is corrected. The quickstart and current
progress now expose vmnet-gateway access, remount identity refusal, unqualified
native reboot/reconnect and real-host interrupted-export recovery separately
from human sign-in/GUI acceptance. Earlier checkpoint text is retained in the
existing folded history. No tests or live host operations were performed by
the reviewer, and this does not approve the still-running tree preparation.


## Export cancellation ownership review at `3d98aec`

A fresh bounded read-only review found an Important gap in builder cancellation:
`exec.CommandContext` kills the Bash parent by default, but does not own and
terminate its descendants. The builder's EXIT cleanup cannot run after SIGKILL,
and bundle identity is received only on success. The proposed extra real-host
cancellation test is held pending a production correction and deterministic
descendant/cleanup regression. The reviewer executed no tests or host mutations.

The same review confirmed that durable snapshot-ready recovery with cleared
Pending bypasses the source runtime lease, but snapshot-ready spans several
stages and cannot identify where cancellation occurred. Inspector teardown must
have its own evidence if the helper launched. Inspector memory is 2 GiB, so a
4 GiB source session plus inspector uses 6 GiB. This planned case would cover
ready-snapshot resume; interrupted copying remains a separate unqualified case.

The current tree builder and B software-addition/start/data/GUI observations are
actual host evidence, separate from this open cancellation finding. The earlier
jq software-addition claim remains withdrawn.

## Cancellation correction deltas and retained-workspace repeat

Fresh bounded six-file reviews approved builder correction `1d079be` and
inspector correction `6643d02`, integrated as `aecd7f7` and `a0fbda8`.
The additional two-file SIGINT delta `7b8a31f` received its own bounded review
and was integrated as `f1f82e2`. No Important/Critical defect remained in those
frozen deltas. Requested Astra/xhigh runtime routing was not independently
attested. Reviews cover source and synthetic checks, not actual VZ shutdown.
Builder descendant and bounded-log failures reproduced before correction;
source cancellation/EOF and shared-signal fixtures also have retained RED/GREEN
evidence. Fresh integration targeted race checks passed using external scratch.
The earlier internal-scratch run failed the real disk reserve; it is not a pass.

Reviewed exact B and C deletion manifests preserved their independent workspace
volumes and excluded every other backend object. E replacement and its NEW
stopped export passed independent data/package comparisons. C separately
completed tree rebuild, explicit start, actions/data/package checks and GUI
observation before D replacement. D returned a valid GUI action receipt but
failed the caller's post-action READY gate; one reviewed exact retry failed the
supervisor precheck before any guest action request. Both original failures are
retained. Later public READY, pinned edited data/package readback and actual GUI
observation do not convert them into successful starts. Five bounded read-only
typed snapshots subsequently proved all six predicates, with no recurrence;
the earlier discarded failing snapshot cannot be reconstructed. Recovery and
D's NEW stopped export remain open gates.

The bounded observation batch saw no recurrence. A reviewed final exact retry
recovered the original GUI attempt's receipt; no new attempt was allocated and
the earlier once/counter records were unchanged. D then stopped through the
public command. Its NEW export from clean `2699a46` passed independent comparison
against its own C history and live/predetermined files, unchanged host source
and exact raw/snapshot digests; all eighteen objects were stopped. This records
supported recovery and preserves both original failures, rather than claiming
an uninterrupted start.

Fresh six-file diagnostic delta review approved `a0fbda8..77f9cebe`, integrated
as `8d0956b`, with no Important/Critical finding. The existing safe formatter
and whitelist were relocated unchanged. Run/retry precheck errors retain the
original failing observation's fixed predicates even when a later snapshot is
READY; service errors add fixed binding/freshness details. Unknown secret-like
diagnostics remain suppressed, and every readiness guard is unchanged.
Eighteen RED cases, full worker Go checks, affected race/vet and fresh targeted
integration race checks passed. The original host cause remains unknown.
