# N1 diagnostic review and verification ledger

Scope: source-only design and deterministic diagnostic components, based on
`99b3a8c04c56ddc4292a9dd515c3e2abcb77d5c6`. No live trial, installation or
qualification verdict change. Ordinary adapter, bootstrap, policy, artifact and
TCP classifier are unchanged.

## Independent portion reviews

- Fresh GPT-6.1 Sol/High clipboard reviewer: no actionable finding. Independently
  ran 16 overlay tests, 59 canonical tests, and 59 canonical fixtures with the
  overlay installed. Checked pending TARGETS callback lifetime, claim/ack order,
  output bounds, parser, privacy and future collector/host-hook requirements.
  This does not establish live semantics or timing-neutral instrumentation.
- Fresh GPT-6.1 Sol/XHigh network/security reviewer: no unresolved P1/P2 after
  corrections. Independently verified pinned compiler digest and 32 tests.
  Cached-neighbor/SYN ambiguity and unexpected-MAC proxy replies hidden by guest
  delivery failure were P2 contract findings. Require complete ARP **and SYN**
  coverage for pre-emission inference, plus same-IP reply classification at
  host ingress before filtering/refresh/write, not just at the guest monitor.
  Successful vmnet API return does not establish enqueue (`pktcnt` unchecked).
  Scoped re-review accepted these corrections without current enforcement edits.

The XHigh escalation was bounded: High had traced the source and exercised
fixtures, while causal inference from absence, future actual-policy reason
capture and exact privileged admission needed independent security review.
Implementation and coordination remained Sol/High. GPT-6 Terra was unavailable;
the requested Sol/High fallback was used. Luna performed evidence inventory.
No Astra or older Terra substitute was used.

## Verification before first publication

The driver independently ran:

- `go test ./...`, `go test -race ./...`,
  `go test -race -tags n1candidate ./...`: all passed.
- Both default/candidate `go vet` and CLI builds: passed; outputs were disposable
  test binaries, never installed or run as a candidate.
- Clipboard overlay 16, canonical helper 59, TCP verdict 11 and listener 2
  deterministic tests: passed. Existing fixed CLI verifier and menu tests passed.
- Rust observer plus canonical policy: 32 passed, 0 failed, no warnings, using
  exact compiler identity from the retained build record.
- All 11 archived report/receipt hashes in [evidence](diagnostic-evidence.md)
  match. Reports/receipts were read-only; no VM disk or encrypted state access.
- `git diff --check`, local diagnostic Markdown links and fences: passed.
  Refetched actual main remains the exact baseline above.

Red/green details are in the [clipboard report](clipboard-source-diagnostic-report.md)
and [ARP contract](arp-observer-contract.md). Fixtures, API contracts and
accounting tests do not prove the as-yet-unimplemented future host/proxy hooks,
live monitors, native ownership or end-to-end delivery.

## Whole-package review and hosted CI

Fresh combined GPT-6.1 Sol/High review of the complete 12-file diff from the
actual base to checkpoint `b459537bfa2cd6338e044ecad8c222ec861ac9f9` found no
actionable finding. The reviewer independently reran overlay 16, canonical 59,
TCP verdict 11, listener 2 and Rust 32 tests, verified compiler/component hashes
and whitespace, and confirmed the deliverable's preparation and causal limits.
It did not independently rerun the full Go suite or inspect private receipts;
those checks were performed by the driver as recorded above.

This ledger closeout changes only review documentation. Exact final-head hosted
CI results are maintained in [Draft PR #17](https://github.com/weshofmann/boxwarden/pull/17)
after they complete, rather than asserting a future run passed. Keep the PR
Draft; no merge or live deployment belongs to this task.

## Executable package preparation after merged PR17

The historical reviews above cover PR17 only. Executable preparation starts
from merged `bbcfaac3f3e21b7094b2db4476c54115eb43722e` on
`weshofmann/feat/n1-diagnostic-package`. Its scope is runnable unprivileged
source/artifacts and an uninvoked future attended package. Installation, guest
operations, actual clipboard/network capture and qualification remain excluded.
The [implementation plan](../../superpowers/plans/2026-09-29-n1-diagnostic-package.md)
and [interface addendum](executable-diagnostic-design.md) distinguish proposed
later interfaces from verified coverage.

Task 1's first independent GPT-6.1 Sol/XHigh review found five Important issues:
packet tap direction was ignored; identifiable TCP22 truncation could disappear
at the kernel filter; late readiness/packets could yield a complete interval;
conditional candidate pre-emission inference required by the merged design was
missing; and Python 3.11+ integer-limit ValueError could escape the fixed parser
error schema. No finding was downgraded. Two scoped fix/review rounds followed;
their results are recorded below.
The earlier 20-test green is not evidence that those five issues were closed.

The controller's initial unconditional restriction on zero-count inference was
too broad. The corrected contract permits only a conditional absence claim for
supported well-formed outgoing candidate ARP/SYN at the guest tap after all
capture and driver controls pass. It leaves precise kernel cause, global absence,
downstream absence, enqueue and delivery unproved.

An earlier inherited fixture interpreter incorrectly modeled cBPF indirect byte
load opcode 0x50 as a word load. Independent opcode fixtures exposed the false
green; the interpreter and actual emitted program were corrected before review.
Actual kernel execution is assigned to the hosted Linux AF_UNIX fixture; Darwin
synthetic execution cannot certify that check or future guest-kernel coverage.

Until changed by the owner, Luna is prohibited: all formerly Luna/Terra tasks
route to GPT-6.1 Sol. Required independent reviews retain High/XHigh as specified.
Requested spawn routing is inspectable; actual backend routing metadata is not
exposed by the delegation tool. No Astra is used.

The first scoped re-review closed packet direction, truncated-header accounting,
conditional inference and modern-Python parsing. It kept the timing finding
Important: final statistics and socket close were still outside the measured
closing phase. It also recorded a Minor extreme-clock conversion overflow.
The second fix measures through actual capture close before separately bounded
post-observation queries and validates derived deltas before integer conversion.
Scoped GPT-6.1 Sol/XHigh re-review closed both remaining findings with zero
Critical, Important or Minor findings for this component. No severity was lowered.

The controller independently ran the final owned suite on Python 3.9.6 and
Python 3.14.7: **31 tests, 30 passed, 1 Linux-only skip**, exit 0 on each. Python AST,
local documentation links/fences and whitespace checks passed. Canonical adapter
and generic helper bytes still match their historical locked hashes. The exact
observer source is SHA256
`1a52c58c2143f3f2876bdde829fba6dca35a99f23843a8bb0af61aa4a018c455`;
its tests are
`c788ba33a54f0ef3e3f6a8c4e85af674e8e67013109e1638d391310d42164bb8`.
Complete synthetic red/green and scoped-review records are retained outside Git.

Published observer commit `1fdacd34b928ec9bd0156a0010c3f9db10313970` is
covered by green [CI run 36663134689](https://github.com/weshofmann/boxwarden/actions/runs/36663134689)
on Draft PR18. Hosted Ubuntu executes all 31 observer tests without skips,
including the actual cBPF filter on unnamed AF_UNIX datagrams and truncation
boundaries. Default/candidate tests, race, vet and build also pass for this head.
These results cover this published component, not later uncommitted integration.
Guest AF_PACKET/offload qualification, driver controls, actual Softnet hooks,
clipboard integration, artifact/admission/cleanup closure and cumulative reviews
remain later tasks. This component's source review does not certify those tasks
or claim live attribution/N1 qualification.


Task 2's fresh GPT-6.1 Sol/High source/security review returned **two Important
findings, changes required**. Actual descriptor closure was outside the measured
collection/finalization bounds and close-then-error could leave a valid durable
end. Presence-only progression also admitted wrong order, missing claim stages,
conflicting outcomes, duplicate acknowledgements and misattributed owner
checkpoints. Eight focused real-slot synthetic reproductions confirmed these
on Python 3.9.6 and 3.14.7. Earlier 33-test green does not close the findings.
The first corrective freeze and scoped GPT-6.1 Sol/High re-review closed both
original Important findings, with no new Critical, Important or Minor finding
within Task 2. No severity was lowered. Real-slot positive baselines precede
malformed-progression rejection, and byte-bound replay preserves the original
32 failing subcases and close-error escape. Actual trace/collector closes,
post-close clock checks, fixed parent-only fd4 proof, derived physical lanes,
exact stage multiplicity and native checkpoint position are covered.

The controller independently ran **48 controls on Python 3.9.6 and 3.14.7**,
exit 0; each also runs all 59 canonical fixtures with the overlay installed.
The reviewer personally ran all 15 focused repair controls on both versions
and inspected the complete retained red/green evidence. All 43 frozen worker
artifacts and four scoped-review artifacts were hash/readback verified. Final
overlay SHA256 is
`57a04ad9d3155e0e9b5d4e4fd718fd7cba5a467f87d7cac60b593e78df546f6f`;
tests are
`fc7c982af2afcd48e16fc648d7320704efcff6b43df26be12cddb6033a3c4a7c`.
Canonical adapter and locked generic helper remain byte-identical.

The post-close private pipe interface is documented in the addendum. Its actual
root Go helper producer, EOF/reader-close adjudication and fixed no-overwrite
publication remain explicit Task 3a integration gates; without the fixed closure
record the collector is incomplete. These tests and this scoped source review
do not certify those Go hooks, native GTK timing, final artifacts, installation,
the complete package or live qualification. No historical verdict changes.

Published clipboard commit `10f8a76ab477e9c0fc398326066d65ce0afbd326` has
green [CI run 36673956382](https://github.com/weshofmann/boxwarden/actions/runs/36673956382).
Decoded Mac job logs show all 48 clipboard controls pass; hosted Ubuntu runs
all 31 guest-observer tests without skips. Default/candidate Go tests, race,
vet and builds pass for that published head. This does not cover subsequent
uncommitted Go integration.


Root semantics ruling (2026-09-30, Kindex87c3ac896e80): diagnostic collection
completeness is independent of invocation outcome. Unknown final ACK may coexist
with complete metadata; missing terminal/fragment, loss, invalid prefix or closure
cannot. Driver uses original outcome plus fixed synthetic equality/consumed length,
never metadata completeness as success. Worker requested a focused regression;
source/report freeze and fresh review remain pending.


Nested Go test boundary learned in Task3a: Go1.27 cmd/go/internal/test/test.go92
and1676 deliberately prepend GOROOT/bin to actual test process PATH. An outer
PATH go-wrapper therefore does not prove nested artifact compiler checks ran.
Use a fixed unprivileged -exec test launcher to restore exact closed PATH before
exec of the unchanged actual test binary, preserve actual argv and retain two
nested compiler checks/records. No source/test/tool mutation or runtime override.
Earlier unchanged named artifact pass proves locked bytes but cannot substitute
for missing nested validation. Propagate to every remaining Go test with nested
compiler boundaries (Kindex46bf54f42581).


Root verified corrected nested compiler provenance before Task3a review:
unchanged named generic-helper artifact test passed with fixed -exec launcher,
two real compiler argv/count8 records plus launcher read in full, exact Go SHA
and encrypted mount association recorded per boundary, all three archive/original
pairs equal and SHA verified. Root durable verification SHA
81e0d3ad5f74f91f781b6485a8d5495badf7ef694f24382fa4dbd72c9db1f741.
Earlier pass and setup typo retained. Task3a worker reports final affected stock
and combined race/vet/host build/static Linuxarm64 helper and canonical rejection
green, CI shell syntax and43 Go files formatted; full freeze/report and fresh
scoped review remain the publication gate. This is deterministic source evidence,
not live clipboard/native timing or whole-package qualification.


Task3a fresh independent component review completed on frozen46 changed/new files
(diff d4de2465416f30623213b5bdb44458ac6075dd1646bbc51c3901c34122d06f5d).
Final report SHAa5cb9e8195d77830bdd6907b8c0e06e80875461ba61920245abf26c70b88353a,
provenance22fea730a6930f1c03444acd490198708698f20067da33a9e7afb06d9a2a1a6e:
0Critical,4Important,0Minor; needs fixes. Initial3finding report retained and
explicitly superseded after focused real collection-runner quota check.
I1 producer/stage crossproduct missing; I2 incomplete or conflicting final frames
admitted; I3 bootstrap and actual directory-close failure can remain complete;
I4 actual collection buffers ordinary1,052,673bytes per stream before rejecting
metadata limits32768stdout/4096stderr. Ordinary intake is finite and cannot promote
oversize; it still violates the diagnostic drain quota.

Root read full report/provenance and independently checked mechanisms before
returning all4 to original requestedSolHigh implementer with boundedR1brief.
No severity lowered or issue parked. Statusok for terminal frame means actual
successful frame-write/finalization, including an error payload/Unknown invocation;
it does not imply transfer success. Earlier Root acceptance of regardlessStatus
was too broad and is corrected. Initial source/evidence/review remain immutable.
R1 fixes/scoped same-reviewer closure, publication and hostedCI remain pending.
This is the component gate; final4fresh package review seats remain required.

Task3a R1 publication ruling (source preparation, review pending): the existing fixed pending name is a provisional same-inode hardlink guard. Data write/sync/actual close, exclusive seal, directory sync and actual directory close must succeed while pending exists and the sealed artifact has two links. Collection refuses either condition. After exact pending/final inode validation, one actual local atomic unlink of the pending link commits the checked artifact; admission requires pending absent and the final artifact one-link. No fallible postcommit rewrite/sync/close, retry, new marker, foreign cleanup or general removal authority is introduced. Precommit or actual unlink errors remain incomplete and preserve one-use consumption and the original ACK/outcome; never infer commit from absence following a reported failure. Crash retention/reappearance of the guard is conservatively incomplete. Actual syscall error/effect ambiguity must be documented; a synthetic after-effect error does not prove known commit. Required controls cover close-then-error before commit, failed unlink retaining the guard, provisional concurrent-collector refusal and success only after all finalization. This publication guard is not the later privileged cleanup authority.

Task3a R1 corrective review disposition: I1 producer attribution, I2 terminal finalization and I4 actual finite drain are closed. I3 publication remains Important; both spec and quality need fixes (0 Critical,1 Important,0 Minor). The conditional hardlink/unlink ruling above is insufficient as a completion contract: pending absence plus final nlink1 records namespace effect, not acknowledgement of a successful unlink return. Linux v6.8 ext4 __ext4_unlink removes the direntry/drops nlink before a possible later inode-dirty error (https://raw.githubusercontent.com/torvalds/linux/v6.8/fs/ext4/namei.c); unlink(2) documents EIO without rollback guarantee (https://man7.org/linux/man-pages/man2/unlink.2.html). This is an authoritative contract counterexample, not a reproduced or pinned future guest fault. Precommit data/directory checks and provisional refusal remain required, but successful-return evidence must reach exact-operation private collection admission; limitation comments or best-effort rollback cannot close I3. Original source/evidence/review records remain immutable. Final R1 review report SHAedb644088c52478d4390cd02951c64ecfb421a152532c7858d5734c09f6ac7c5, provenance813b379c1c5fb30911e06cee7ed523ae3c7d94aa6a899ce0098bb8f814d6fb5a. No severity lowering, publication, live trial or installation. A bounded R2 acknowledgement design is pending; canonical ACK/outcome/one-use and Unknown-with-complete must remain intact.

R2 source-only architecture decision: reserve trial-helper stderr for one fixed publication-return witness (maximum512bytes including LF), parsed privately and never exposed as raw stderr/stdout or logged. Its exact fields are v,phase,header,generation,created,bootstrap,closure,collect; hashes cover only fixed typed metadata and the original complete operation header, never payload/credentials. Emit only after required publication/finalization calls actually return nil. Namespace guards remain supplemental; their visible state cannot reconstruct acknowledgement. The existing live host owner retains exact-operation/generation witnesses and requires matching collected metadata for complete admission; a reused generation needs a previously observed successful creation witness in that scope. No new guest filename, host journal, listener, share, caller selector or rollback/retry. A fixed tagged invoke runner retains stderr512 and the existing payload stdout quota; collect retains stdout32768/stderr4096. A narrow opt-in strict-stderr receiver records actual EOF and checked actual read-close within the original deadline, without changing ordinary runner behavior or the original clipboard ACK/outcome. Capture the witness before ACK parsing; a malformed ACK with valid evidence remains Unknown and may have complete metadata. Missing/truncated/mismatched/no-EOF/close-error proof cannot become complete. All eight-field canonical schema, maximum serialization, actual descriptor ownership/drain ordering and after-effect-error controls remain implementation/independent-review gates. No live or installation authorization is granted by this source decision.


### Task3a corrective source gate closed

The independent R2 component review closes I3 without lowering severity; I1,
I2 and I4 retain their R1 closure. Spec compliance and quality are approved
for the correction, with zero open or new Critical/Important/Minor findings.
The earlier pending dispositions above are preserved review history.

The host now retains a bounded canonical publication-return witness before
ACK parsing and requires matching operation, original expiry, generation,
bootstrap and closure metadata for complete collection. Generation reuse
requires prior acknowledged creation in the live owner scope. Actual unlink
then reported-error controls demonstrate that complete-looking files alone
are insufficient. The opt-in receiver requires actual EOF and checked actual
read-close before the original deadline, joins cancellation, and preserves
the original process result and clipboard ACK/outcome. Helper stderr is
reserved for one eight-field metadata frame; child stderr uses /dev/null.
Measured invoke/collect maxima are 368/307 bytes including LF, below 512.

Root read the full 1055-word worker report and 997-word independent review,
verified 63 source/archive identities, 140 retained evidence files and all
28 command records, and reconciled 105 unique named passing controls. The
19 final gates cover affected default/canonical and stock/combined race/vet,
actual host and fixed trial-helper builds, tag rejection, schema and the
unchanged generic-artifact test. Two fresh actual nested compiler records
preserve all eight argument elements; the fixed launcher retains six.
The generic artifact remains 33b12b9e293bcbdf7d6c91f933b42003ca394e7a678ac83b8d80df714d27c57e.
The canonical prefix remains 377 lines/13866 bytes and the generic method
27 lines. Supplementary generic-command builds are distinguished from the
actual separately named trial command.

Frozen R1-to-R2 diff SHA256: f8fa752648b4d90b66de9821f42233d4653da42226f6321f8d37e230f85f314e.
Worker report SHA256: 6f95e5c47129b5f818676ce3ca91639d6c69486b36a1810f6e42e6ae0d9e62fe.
Independent R2 report SHA256: 5000104dcd118ec829f82eb22dfecfaca7c8817099e65b433eab23956ffc1212.
Independent provenance SHA256: 945e5a5a93e999bb7298c9bdb63f01b79bd38851e5f562c4c03d2e43ceb1b121.

This is deterministic component evidence. It does not qualify installed
root producers, a guest kernel/filesystem, real SSH or native clipboard
timing. Rust hooks, exact artifact reproduction/admission/launch/cleanup,
the finite driver/static package and four fresh final reviews remain gates.
Nothing has been installed or run against a VM, guest, the real clipboard or
host network state. Private evidence remains outside Git.


### Task3b diagnostic forwarding source gate closed

The distinct diagnostic Rust source calls the tested shared dispatch functions
from the actual VM and host forwarding paths. It preserves the refresh and
timestamp order, policy state transitions, fallback, raw write results and the
single write, including short successful writes, the ignored host policy boolean
and ENOBUFS suppression. Diagnostic lease inspection is read-only. Bounded
metadata identifies only the selected pair; vmnet packet count remains
unobserved, and API success makes no enqueue or delivery claim.

The fixed HELLO/ARM/ARMED/SUMMARY protocol admits one finite interval over an
anonymous nonblocking fd1 stream. Complete and partial ARM arriving after the
original HELLO wait deadline are refused before activation. Mandatory self-tree
admission validates the full manifest, descriptor digest, protected ancestry,
operator/group membership, ACL/link/mode and read-only SH lock; the guard remains
held through actual resource construction and teardown. Timestamp offset fields
require decimal digits before range and zero-instant validation.

The first fresh independent source review found two Important issues and one
Minor issue: late ARM could bypass the HELLO timeout; signed offset components
could pass timestamp admission; and the serialization fixture omitted widest
valid binding spellings. All were corrected and independently closed at their
original severity, with no new findings. The original review and measured
results remain preserved. Spec and quality pass this bounded source-component
gate; it is not a final package review.

Retained targeted RED reproduced four assertion failures with two passing
controls. Targeted GREEN passes six controls; a fresh 14-file patch replay and
actual library/main metadata-only Cargo check pass. The corrected conservative
serialization bound is 3166 bytes for SUMMARY and 4469 framed child bytes,
including the three length prefixes, within 4096/16384 quotas. It overapproximates
jointly reachable counter/flag states; original 3132/4387 remain fixture-specific
measurements. No full 79-test pass is claimed by the targeted run.

Source patch SHA256: 2e8dbbffdbf5869dc5280ef260e9f455cd31af43d8519844be8f95440416f2f6.
Source manifest SHA256: 555d710d92432b7d64621c07719332a37881b5e33efa0776aa3b1683e7050333.
Initial independent review SHA256: 6f7e938f363569cbecb9e0e161e9a8cef6d87521eb1e4ddd2ff22110dd1057d4.
Independent corrective closure SHA256: 325d28c9ef206367f81961fb45bf87071b74ab915396450313c8616ecba2958c.

Native ACL/socket controls are local synthetic evidence. Same-process socket
flags and the earlier small Foundation fd1 round trip do not qualify actual
Go-to-Foundation inheritance. Compiler records identify clang/ar shims, not
historical resolved SDK children or every nested image boundary. Exact artifact
reproduction and admission, Go owner/watch/lock integration, cleanup, finite
driver/static lock, current increment CI and four fresh final reviews remain
gates. No diagnostic executable was built or launched, and nothing was installed
or run against a VM, guest, clipboard or host network state.

A separate deterministic CI matrix now selects only the locked synthetic fixture
on Ubuntu 24.04 and macOS 26. Fresh private neutral working directories, explicit
nonprivileged runner overrides and a closed submitted environment keep upstream
sudo test runners out of that execution. Locked fetch precedes offline tests;
fetch or configuration refusal prevents test execution. Local syntax and
command-spy controls pass, but spies do not establish native compiler identity
or exact native child environment. Hosted execution remains pending publication.


### Task4a exact artifact and admission prerequisite closed

The diagnostic Rust source remains published commit
`ecae4b745ef52cdd2b3c5ede6a76db732a38c57e`, upstream
`df84a30016e3d6acc0d30acc660cf3a726f42a9b`. Two fresh, independent offline
release builds produce version `0.19.0-boxwarden-n1-diagnostic.1`, executable
SHA256 `e567d610fb2a854755bd7786031ed00c28fc3a970213606f21350c6e1f754f61`
and deterministic single-file USTAR SHA256
`9f696a25549b324138f17242b5e4169a64664de3638f3e9417af24b534fa4aac`.
The recipe/result records are separate later source, avoiding a self-hash cycle.
No privileged artifact was published or installed.

The first build compiled but failed cross-volume stage retention with EXDEV.
Two later fully retained builds differed in target-path OSO debug strings and
archive-index timestamps. These failed/nonmatching runs and recipes remain
immutable investigation evidence. A corrected same-device retention preflight,
actual ld `-oso_prefix` argv pair and ZERO_AR_DATE=1 on every ar invocation were
applied before two new empty-target builds. No failed output was adopted,
stripped, normalized or substituted. Cross-volume archival uses separate exact
copy/fsync/hash/readback. Both successful legs match all 25 native objects,
three native archives and 101 OSO records. All 449 actual child invocations per
leg retain element counts/bytes, environments and direct selected tool hashes;
all 53 ld calls and six ar calls per leg enforce the corrected contracts.

Rust/Cargo 1.98.1 and selected direct Xcode clang/ar/ld identities are recorded in
`tools/n1-diagnostic-softnet/artifact.json` with the source and recipe hashes.
Selected SDK metadata pins are not a whole-SDK attestation. Both release logs
retain unused canonical-library method warnings and the pinned block 0.1.6
future-Rust-incompatibility warning. Current pinned builds succeeded; this does
not claim compatibility with future compiler/dependency upgrades. Actual child
environments are retained; no equality with the submitted top-level environment
or historical whole-SDK attestation is asserted.

The tagged Go identity admits only these exact bytes/version/path and rejects
stock/canonical/relocated substitutions; dual candidate+diagnostic build tags
fail. Clipboard-only diagnostic keeps stock. Diagnostic publication stages a
zero-byte root/operator 0440 one-link/noACL launch.lock before tree visibility,
then publishes the root/wheel 0444 manifest last. Doctor/init require exactly
softnet,manifest.json,launch.lock while ordinary/canonical two-entry admission,
artifact identities and manifest history remain unchanged.

`SystemDoctor.AcquireDiagnosticLaunch` takes fixed-tree read-only, close-on-exec
independent descriptors and nonblocking SH, repeats full current host and
held inode/ancestor admission, and exposes only Revalidate/Release. Cancellation
never releases SH; Release closes each descriptor once, lock last, and retains
actual close errors. Cancellation inside full CheckRuntime currently reports
ErrDiagnosticLaunchDrift; direct/tree cancellation preserves context errors.
Callers must refuse either and must not use error classification as release or
readiness authority. This documented minor remains visible to final review.
Ordinary diagnostic RootedUninstaller refuses before inventory/mutation;
qualification-only exact EX/census/unlink cleanup remains a separate gate.

Fresh independent Sol/High component review is spec compliant and quality
Approved, with 0 Critical / 0 Important / 2 Minor findings. Both minor handoff qualifications are
recorded above. The independent review SHA256 is
`0b1c479b9d3d40b8d43de07a7482449d70970aad0e6b70294e5ed07351c7efdc`.
Root independently checked all 738 source entries, 19 changed/new snapshots,
18 unchanged published Rust files, 321 sealed evidence files, both actual
binary/archive pairs and all 449 child record per leg. Retained synthetic
RED/GREEN proves archive/device/argv/environment controls, exact admission,
SH/EX exclusion and inode drift, descriptor flags, staging failure before
visibility, retained close failures and legacy cleanup refusal. Diagnostic,
stock, canonical, stock clipboard and combined affected Go tests pass locally;
CGO_ENABLED=0 local checks make no race claim. Private evidence stays outside Git.

Task3b current-source CI run 36737771516 is green: hosted 79 Darwin / 78 Linux
actual synthetic Rust tests plus the existing Go/default/race/candidate/tagged
clipboard, guest observer and packet policy checks. Historical pending CI
statements above remain preserved chronology. Additional Task4a tagged guard
race/vet/build and pure recipe-contract coverage receives a separate workflow
review/current-head CI gate before integration.

This closes only the artifact/admission prerequisite. Task3c actual owner/watch
launch and independent lock lifetimes, Task4b exact cleanup and final Go/helper
reproduction, finite driver/static package, full current CI and four fresh final
reviews remain. No VM, guest, real clipboard, host network mutation or live
qualification occurred.

The separate CI workflow continuation is independently spec/quality approved
with zero new findings. Review SHA256:
`8b89ee043b5a075e962d583a44dcc210576fe5b868248aa11b31b91c7ecbfa91`.
It adds unfiltered diagnostic/combined guard races, vet and unexecuted CLI
builds, eight pure recipe controls, and intentional dual-tag rejection. Existing
workflow blocks remain unchanged. Actual current-head hosted execution is
pending publication; the original two minor limitations remain documented.


## Task3c owner/watch integration and R1 closure (2026-09-30)

Task4a published-head CI run 36751386544 passed all four jobs and 41 steps,
including actual 79 Darwin / 78 Linux Rust controls and the added tagged
admission race/vet/build and eight recipe-contract controls. Task3c now adds
independent parent-before-intent and detached Owner launch guards, the bounded
private watch, fixed enrollment and live-generation inspection. Its 49-file
source delta has been read against the retained source and evidence inventory.

The initial independent review found two Important issues: complete summaries
could contain refresh/mismatch counters contradicting completeness, and a
READY inspection could return an earlier connection after actual readiness
replaced it. The six-file R1 correction checks the actual producer's failure
columns when Complete is true and compares the full captured connection and
authority after Snapshot. Meaningful original RED/GREEN, expanded transport/
owner controls and scoped diagnostic, stock-inspection, combined and vet
checks pass; 74 focused PASS lines contain no SKIP. Fresh independent R1
review approves spec and quality, closing both issues at their original
Important severity without downgrade. Review SHA256:
`c000b918c636d7ad83dc5d2bba79160948451f74535a2cdc69b076abb35af46d`.
Open findings are 0 Critical / 0 Important / 2 carried Minor / 0 new Minor.

Local wider checks preserve the explicit pre-existing CGO-disabled APFS test
exclusion. The ordinary detachment fixture skips under diagnostic enrollment;
a separate supported diagnostic detached-handoff fixture proves independent
SH ownership. Local checks make no race claim. Fresh current-head hosted CI
is the publication gate and is pending for this source checkpoint. The generic
helper still hashes to 33b12b9e293bcbdf7d6c91f933b42003ca394e7a678ac83b8d80df714d27c57e.
A native synthetic Go/Foundation/Swift chain proves exact argv, unnamed
full-duplex descriptors and actual reap within its recorded bounds; it does
not qualify real Tart/Softnet, a VM, kernel delivery or native clipboard.
Failed evidence remains immutable, including the superseded incorrect
ordinary CloseUnclaimed result fixture; final source preserves errors.Join.

Shared private control retains its existing 80 KiB outer frame bound; strict
diagnostic payloads and client reads have a 4096-byte bound. Peer inspection
checks bracket the watch but do not claim an atomic continuous peer lease.
Task4b qualification-only exact cleanup, Task5 finite/static closure, complete
Go artifact graphs, full verification and four fresh final reviews remain.
The two carried Minor limitations above remain explicit. Private evidence is
outside Git. Nothing has been installed or exercised against a VM, guest,
real clipboard or host network state.
