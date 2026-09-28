# Controlled clipboard progress

Baseline: merged `10ad0bea`; branch `weshofmann/feature/controlled-clipboard`,
Draft PR [#13](https://github.com/weshofmann/boxwarden/pull/13).

Published foundation `40eada6`: bounded shared transfer core, nonblocking locks,
and exact-generation supervisor handshake. Its hosted deterministic CI passed.
Pinned management SSH is the guest transport; clipboard bytes bypass actions and
journals. Busy operations refuse, ambiguous writes do not replay.

This checkpoint adds fixed guest framing and expiring generation-bound requests,
retained owner/SSH capability and session exclusion, desktop selection ownership,
and source installation/artifact pins. The supported adapter is Ubuntu 24.04
GNOME Wayland with Xwayland and logind controller affiliation; ambiguous desktops
and native X11 refuse. Guest-local privileged metadata reads identify executable
links only when ordinary same-user inspection is denied.

Verification: guest helper/source fixtures and focused Go/race checks passed.
The existing Go suite and hosted CI passed at the guest checkpoint. Real synthetic helper
checks passed Unicode/trailing LF, empty text and 1 MiB, later reads after writer
exit, and malformed/NUL/oversize rejection preserving the previous value.
These are helper checks, not production CLI or graphical application acceptance.

The four public CLI commands and private AppKit adapter are implemented. The
host adapter isolates blocking AppKit calls in a bounded child and restores
descriptor flags after owned I/O quiesces. Independent host/CLI and pinned Tart
viewer reviews found no remaining Important/Critical issue. The source patch and
separately signed Tart `2.32.1-boxwarden-clipboard-r3` are staged; the executable
SHA-256 is `1573e4be9a10087f8e5dce5dcc5718cfef4d5d0274c60d5e209ba1f13c49f7a6`
and the reproducible archive SHA-256 is
`b5f487c3b092b48d23819d2828c45b96f7c84816d51b5ca9dd68f3de64fa3a64`.
Full local Go and race suites passed using external scratch space. Source-only
host admission accepts the exact r3 identity. At this source-only checkpoint,
the installed toolchain and root manifest had not changed; the later attended
r3 admission is recorded below. Real VM/window/menu acceptance is pending.

Independent guest review found an Important liveness gap: a blocked native GTK
call could outlive the preclaim Python signal deadline. The source correction
uses a kernel-enforced default SIGALRM until the native clipboard claim succeeds,
then disarms it before retaining ownership. Its regression first reproduced the
gap in a blocking C call and now passes; independent review confirmed native
initialization and claim stalls terminate at the deadline, while committed
owners survive. No new VM/golden qualification is claimed from source checks.

The first disposable guest lacked a running Xwayland process despite having
an active Wayland desktop. The helper now wakes Xwayland before its unchanged
strict desktop process proof. Its 59 guest tests, including native-call deadline
and first-use ordering cases, and fake ISO/finalization fixtures passed;
independent guest review found no remaining Important/Critical issue. After
staging this helper only in the disposable probe, public stop/start produced a
new READY generation. From a cold baseline with no Xwayland process, public
CLI `copy`/`paste` passed exact Unicode/trailing LF, valid empty text, and
1 MiB. Invalid UTF-8, NUL, oversize, and stale-generation requests refused
without changing the destination. Full-host doctor and final session readiness
passed; no Mac pasteboard was accessed. Hosted deterministic CI passed at
`dc3bf28` ([run 36357415006](https://github.com/weshofmann/boxwarden/actions/runs/36357415006))
and at the documentation checkpoint `f232d4b`
([run 36357937418](https://github.com/weshofmann/boxwarden/actions/runs/36357937418)).

A separate synthetic GTK 3 TextView application then pasted exact text after
the public CLI copy had exited. A second TextView copied exact text that the
public CLI paste read later: 25 and 23 bytes respectively, including Unicode
and trailing newlines. The bounded private test used the guest's existing GTK
library and left the exact disposable session READY. Its independent source
review found no Important/Critical issue. This proves application-widget
interoperability, not Firefox behavior or the patched host viewer controls.
The disposable probe was then stopped through the public workflow and verified
stopped; host doctor remained healthy. Independent cumulative review of the
feature diff against merged `main` found no remaining Important/Critical issue.
Hosted deterministic CI passed for the previous documentation checkpoints
`d0dc6cc` (run 36359585931) and `a41b59e` (run 36360506303).

With Wes's attended Terminal action, the exact signed r3 Tart was admitted by
its reviewed helper. The config and root manifest match their reviewed SHA-256
digests; host doctor is healthy and the native APFS mount UUIDs remain exact.
Demo D's backend was already stopped with intended-running drift; the public
stop reconciled it to consistently stopped while preserving its disk and
workspace. The private admission journal is root-readable; the helper's
terminal success output followed its synced admitted phase. No general Mac
clipboard content was read or changed.

The first r3 public viewer start showed the host strip but a black guest display.
It timed out waiting for the guest `hvc0` autologin prompt, so the session never
became READY and no clipboard transfer was attempted. The public stop timed
out while its exact Tart object remained running; a bounded exact-target Tart
stop succeeded, followed by a public stop that reconciled the session record.
All Tart objects are now stopped. The boot failure is unattributed; no retry is
claimed as qualified.

Wes found the strip too tall and specified a single row. The r4 source reduces
three explanatory rows plus buttons to one row with sandbox identity, both
buttons and status; full explanation moves to tooltips. An offscreen rendered
height regression failed before implementation and now passes at <=52 points.
The local viewer suite, exact pinned Tart release build, signed executable
verification, and targeted Tart Swift tests passed. Staged r4 executable SHA-256:
`46e809c95260d6a264b15662bd2117eddd13b0a0ca19dcdc6bae244cc7799fc2`;
archive SHA-256: `4126636c097dffaefff70c0abec116623885554419c87825b4e9e458a9f987ff`.
R4 has **not** been installed or host-admitted. The installed r3 identity remains
active, and its real viewer buttons/menus and host clipboard transfer remain
unverified against a READY guest.

The next source checkpoint recognizes the exact signed r4 version, executable
digest and archive digest as one coherent host identity while retaining the
admitted r3 tuple. A mismatched r3/r4 tuple is rejected, and ordinary host init
still admits only stock Tart. Focused host admission tests, including a simulated
r4 doctor success and wrong-executable refusal, pass. This changes source policy
only; installed config and root manifest still bind r3.

Next: attribute the black guest boot and independently review the compact r4
diff/build; then prepare an exact r4 admission and a bounded disposable GUI
run. Do not use demo D or a provider account for this test. The synthetic Mac
clipboard window still needs explicit agreement before any general pasteboard
read or write. Preserve the failed disposable disk and its observations.
