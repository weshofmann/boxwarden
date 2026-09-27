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
host admission accepts the exact r3 identity; the installed toolchain and root
manifest have not changed. Real VM/window/menu acceptance is pending.

Independent guest review found an Important liveness gap: a blocked native GTK
call could outlive the preclaim Python signal deadline. The source correction
uses a kernel-enforced default SIGALRM until the native clipboard claim succeeds,
then disarms it before retaining ownership. Its regression first reproduced the
gap in a blocking C call and now passes; independent review confirmed native
initialization and claim stalls terminate at the deadline, while committed
owners survive. No new VM/golden qualification is claimed from source checks.

The first production `clipboard copy` on the restarted disposable probe
refused with `clipboard text unavailable`; an independent direct invocation
of the fixed guest helper returned a pre-claim error. Read-only exact-session
diagnostics found one GNOME Shell process and no Xwayland process. The active
Wayland login and desktop environment checks passed; the strict process
binding therefore refused before GTK initialized. No production CLI transfer
has passed. A bounded X11 connection on that disposable guest started one
Xwayland process without accessing clipboard contents. The guest source now
makes that connection before its unchanged strict process proof; 59 guest tests,
including native-call deadline and first-use ordering cases, and the fake ISO
and finalization fixtures passed. Independent guest review found no remaining
Important/Critical issue in the correction. After staging only this helper in
the disposable probe, public stop/start produced a new READY generation and a
read-only inventory again found one GNOME Shell and zero Xwayland. From that
cold baseline, public CLI `copy`/`paste` passed exact Unicode/trailing LF,
valid empty text, and 1 MiB. Invalid UTF-8, NUL, oversize, and stale-generation
requests refused without changing the destination. Full-host doctor and final
session readiness passed; no Mac pasteboard was accessed. Hosted deterministic
CI passed at `dc3bf28` ([run 36357415006](https://github.com/weshofmann/boxwarden/actions/runs/36357415006)).

Next: demonstrate synthetic guest Firefox/application copy and later paste,
then finish the attended deployment and host GUI acceptance path. Host
deployment and real host-clipboard GUI testing remain attended gates.

Valuable demo D is untouched; one disposable probe is used. No real host clipboard
has been accessed. Deployment remains a separate concrete approval gate.
