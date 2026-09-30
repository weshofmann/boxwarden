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
