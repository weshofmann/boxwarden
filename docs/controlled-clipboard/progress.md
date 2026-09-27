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
The existing Go suite passed at the integration checkpoint. Real synthetic helper
checks passed Unicode/trailing LF, empty text and 1 MiB, later reads after writer
exit, and malformed/NUL/oversize rejection preserving the previous value.
These are helper checks, not production CLI or graphical application acceptance.

In progress: four public CLI commands and private AppKit adapter; review-confirmed
blocked native reads require a bounded child, and descriptor flags require cleanup
after I/O quiescence. Pinned Tart strip/menu build is staged; independent shutdown
review continues. No installed toolchain or root admission has changed.

Next: finish host adapters and patched-viewer review, publish their verified
integration, then demonstrate synthetic Firefox/application copy and later paste.
Valuable demo D is untouched; one disposable probe is used. No real host clipboard
has been accessed. Deployment remains a separate concrete approval gate.
