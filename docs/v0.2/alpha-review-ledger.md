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
