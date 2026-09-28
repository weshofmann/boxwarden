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
At that source checkpoint, r4 had not yet been installed. Its subsequent
admission and viewer attempt are recorded below; the real viewer buttons/menus
and host clipboard transfer remain unverified against a READY guest.

The published source checkpoint recognizes the exact signed r4 version, executable
digest and archive digest as one coherent host identity while retaining the
admitted r3 tuple. A mismatched r3/r4 tuple is rejected, and ordinary host init
still admits only stock Tart. Focused host admission tests, including a simulated
r4 doctor success and wrong-executable refusal, pass. This changes source policy
only; installed config and root manifest still bind r3.

The r4 executable was staged side by side, and a new exact-source CLI passed
read-only doctor against admitted r3. An attended r3-to-r4 admission/rollback
helper had private exact backups and candidate bytes. Its read-only preflight,
synthetic atomic-write/refusal/rollback fixture, and independent security review
passed with all ten VM objects stopped. A bounded
macOS log read for the failed r3 Tart process showed the raw VM disk opened and
a Virtualization event-tap connection, but did not establish VM start completion
or explain the missing serial prompt.

Wes then ran the reviewed r3-to-r4 admission helper in Terminal. Its reported
synced admitted phase was checked against the exact installed config and root
manifest digests. The installed r4 executable, mount UUIDs and host doctor also
match the reviewed plan; doctor is healthy. This is actual host admission, not
guest or clipboard acceptance. Hosted deterministic CI passed for the compact
viewer documentation checkpoint `7995b72` (run 36368615488).

The domain had no current-golden pointer. We selected the existing qualified,
stopped `boxwarden-alpha-base-af8cf2494da4e5f578f651fbd12bbd87` via public
`golden register`, then used public `session create` for one fresh disposable
`clipboardprobe20260928r2` (session ID
`b637727b-4126-49aa-9a42-fc96afb9f458`), with no workspace attached.
This changed the domain current-golden pointer from absent to that exact base;
it did not rebuild a base or modify demo D.

The public r4 `session start` opened its viewer, but the guest display stayed
black and startup timed out awaiting the `hvc0` autologin prompt. It never
became READY. Public stop initially timed out; bounded exact-target Tart stop
then succeeded, followed by public stop reconciliation. Full-host Tart
inventory shows all objects stopped and doctor is healthy. No clipboard
transfer or general Mac pasteboard access occurred. Preserve both failed
viewer probes and their observations pending attribution.

During the second failure, the exact Tart process sample showed its AppKit
event loop idle, three threads, and no active Softnet child. Its raw disk and
NVRAM timestamps did not advance. These observations point to startup not
progressing, but do not establish whether the VM start task was scheduled,
Softnet exited early, or Virtualization failed before boot. Simple standalone
Swift/AppKit and SwiftUI task-before-event-loop probes did run successfully,
so event-loop scheduling alone is not an established explanation. The next
step is to attribute this startup boundary with bounded diagnostics before
another VM run. Do not use demo D or a provider account for that test. The
synthetic Mac clipboard window still needs explicit agreement before any
general pasteboard read or write.

Startup attribution after that checkpoint found a scheduling race in pinned
Tart: `Run.run()` is main-actor isolated, creates a task to start Softnet and
the VM, then enters the synchronous SwiftUI application loop. An exact-shaped
no-VM `AsyncParsableCommand`/SwiftUI probe reproduced the queued task never
running; an explicit awaited startup handshake made it run before the UI
loop. This explains the idle Tart sample and missing Softnet child more
directly than a guest or serial failure. It remains a source-level diagnosis
until a new viewer starts a VM successfully.

The r5 pinned Tart patch now waits for the VM start task's success signal
before entering the UI loop. It does not weaken `--no-clipboard`, change the
guest definition, or modify installed r4. The patch applied to a clean pinned
source tree, and its generated `Run.swift` matched the compiled tree byte for
byte. Focused viewer checks, the exact Tart release build and two targeted
upstream Tart tests passed locally. A signed r5 candidate was packaged with
exact executable SHA-256
`e0047ddb7ffff0967591a1bd03374980f1775bdb4ab44ffd5b9bfb4700d7b97b`
and final archive SHA-256
`3a58485df6a10958e62da1fd2692c47cf54ddec0448338695b0daf327d7617bc`.
Host admission policy now recognizes only that coherent r5 tuple in addition
to prior pinned variants; r5 host installation, doctor and real GUI acceptance
have not occurred. Next: finish exact candidate review and attended admission,
then use a fresh disposable public session to verify boot and compact controls.

The exact r4-to-r5 host admission package is now prepared privately. The
installed r4 config and manifest match their saved bytes; native mount UUIDs,
source-bound CLI doctor, staged r5 signature and digest, and full-host
all-stopped inventory passed a read-only preflight. Synthetic atomic-write,
unknown-input refusal and rollback checks passed; an unprivileged `apply`
refused without creating an intent record. The helper has not been run with
administrator privilege. Hosted deterministic CI for `1b72794` passed the
full test, race, vet and build matrix (run 36371651999). The remaining host
gate is the attended r4-to-r5 admission; real r5 boot remains unverified.

Wes ran the exact attended r5 admission helper, which reported its synced
admitted phase and healthy doctor with all VMs stopped. Independent read-only
host checks found the installed root manifest and domain config byte-identical
to the reviewed r5 candidates, the signed r5 executable at its pinned digest,
both native APFS mount UUIDs unchanged, healthy doctor, and every Tart object
stopped. The root-owned private phase file exists; its contents were not read
under the unprivileged operator account. The installed r5 identity is admitted;
a real r5 guest launch and clipboard operation remain pending. Next: one fresh
workspace-free public disposable viewer probe from the existing qualified base.

The fresh public r5 probe `clipboardprobe20260928r3` was created from that
qualified base with no workspace attached. Public `session start` returned
`running` and `ready`; a subsequent public status read remained consistent and
READY. Full-host Tart observation showed the exact new backend running with
its Softnet child. A targeted capture of that Tart window showed the compact
single-row strip and both clipboard buttons. The capture showed a black guest
area both initially and after a short boot interval; direct on-screen
confirmation is pending because a host window capture may omit the VM display
surface. The session remains running for Wes to inspect. No clipboard transfer,
general Mac pasteboard access, provider sign-in, or workspace attachment was
performed. This confirms the r5 VM startup correction reaches READY, while
graphical display and button behavior still need direct user observation.

Wes's direct screenshot confirms that the r5 guest area itself is black. This
is a real GUI acceptance failure, despite public READY, an active GNOME Wayland
login, and a connected virtual display with an active 1024×768 DRM primary
framebuffer allocated by GNOME Shell. The compact buttons are visible but have
not been exercised. A legacy fbdev buffer read as zero, but it is distinct
from GNOME's current DRM framebuffer, so it does not establish guest pixel
content. The guest screenshot API refused an unattended capture. Focused
no-VM probes show that SwiftUI `onAppear` runs in Tart's command-shaped app
loop while subsequently queued main-actor and main-queue work does not run.
The pinned r5 source attaches its `VZVirtualMachineView` after VM start;
Apple's GUI Linux sample attaches that view before starting. The precise
display failure boundary is still under investigation. Keep the disposable
probe running for inspection; do not present r5 as graphical acceptance or
access the general Mac clipboard.

Wes also resized and restored the r5 window; the guest area stayed black. A
bounded read-only probe of the active GNOME DRM primary framebuffer measured
14,013 distinct colors, so the guest is rendering nonblack pixels; it did not
export a screenshot or read any clipboard data. The black output lies between
that guest buffer and the visible Tart surface. The r6 source binds a single
`VZVirtualMachineView` to the VM before start and mounts that same view in the
compact SwiftUI window. A focused view-identity regression failed before the
change and passes now. The viewer suite, exact pinned Tart release build,
signed executable check, targeted upstream Tart tests, and focused host policy
tests passed locally. Exact staged r6 executable SHA-256:
`e6d6894b793a6e3636e7438756a48fbdba35bf9885c4e08db7ad7bef8c118c99`;
archive SHA-256:
`899773a1dfec8d66c9a42f68ab105c2912b751cd4da3c2883ca2a83db5e355f0`.
Hosted deterministic CI for the r6 source checkpoint `e8dc856` passed
([run 36376589149](https://github.com/weshofmann/boxwarden/actions/runs/36376589149)).
The build is retained privately. Source policy now recognizes that coherent
r6 tuple, while the installed host still admits r5. Real r6 display behavior
and clipboard buttons remain unverified.

The r5 public stop reached its deadline with the exact backend still running.
A bounded Tart stop targeted only that disposable object; the subsequent
public stop reconciled the session to stopped without deleting its disk. The
signed r6 executable is staged side by side. Private exact r5 backups, r6
candidate config/manifest, and a clean source-bound CLI are prepared. The
attended r5-to-r6 helper passed synthetic atomic write/refusal/rollback checks
and a full read-only preflight of exact bytes, native mount UUIDs, signature,
healthy doctor, and all-stopped Tart inventory. An unprivileged apply refused
without writing intent. Installed host admission remains r5; the attended
administrator step and real r6 desktop/button verification are next.

Wes approved a standalone macOS menu-bar clipboard utility and stopped
custom Tart viewer development. The r6 admission request was canceled before
execution; the installed host still binds r5, and no Tart VM is running. The
official stock Tart 2.32.1 executable remains at its previously admitted
digest. An exact private r5-to-stock return package now has current r5
backups, candidate config/manifest changing only Tart identity, a clean
source-bound CLI, and a bounded attended helper. The helper's synthetic
atomic-write/refusal/rollback checks and full read-only host preflight passed;
the administrator step has not run. The actual black-viewer observation is
retained above as diagnosis, not a continuing Tart patch plan.

Current work: implement a directly launchable native menu-bar frontend over
the existing CLI transfer path, with explicit configured sandbox selection,
generation binding, private clipboard tests, and no automatic transfer. After
stock admission, remove patch-only launch arguments and toolchain policy in
one verified source checkpoint, then verify stock desktop and both menu
directions with a synthetic test window. The GUI acceptance and actual stock
host return are pending. No general Mac clipboard content has been read.

The standalone menu source checkpoint adds a signed, directly launchable
`Boxwarden Clipboard.app` build and a read-only, explicit-domain `clipboard
targets` CLI command. Target discovery uses current backend and exact live
supervisor readiness; a click passes the selected domain, session ID, backend
object and generation to the existing public push/pull implementation. No
target is selected on launch, and a replaced session or changed generation
requires a new selection. A failed configured-domain query leaves other
freshly verified domains available and reports partial status. AppKit menu
validation preserves disabled transfer actions. The utility does not read
clipboard contents during launch, discovery, or selection; its CLI children
have bounded deadlines and are reaped on Quit. A failed or cancelled transfer
reports an unknown destination outcome. The build script now fails if it
cannot replace an old app bundle instead of silently nesting a new one.

Verification for this source checkpoint: Swift model, synthetic CLI-child,
and real AppKit menu-validation tests passed; Swift typecheck, app build,
strict ad-hoc signature check, shell syntax, plist lint, and targeted Go race
tests for discovery/session/clipboardx/CLI passed. The broader local Go app
suite encountered the host filesystem's existing free-space reserve check
(about 22.0 GB available versus a 24.5 GB reserve), before any relevant
clipboard failure. Hosted CI and visible-menu/real-guest acceptance remain
pending. The stock-return helper has not run; r5 remains installed and all
VM starts remain on hold until the attended return is verified.

Hosted deterministic CI for the published menu checkpoint `ed830cb` passed
the complete configured matrix ([run 36379454592](https://github.com/weshofmann/boxwarden/actions/runs/36379454592)).
A clean checkout of that commit produced an ad-hoc signed retained app whose
bundled CLI reports the exact revision with `vcs.modified=false`; its copied
bundle passed strict signature verification. A metadata-only app launch with
the explicit alpha config produced a running menu process. Desktop inspection
timed out, so visible menu behavior is still unverified. The default local
Boxwarden config selects a different domain; alpha testing must pass its
explicit config path. No VM or clipboard content was touched during this
smoke test. The attended stock return remains pending.
