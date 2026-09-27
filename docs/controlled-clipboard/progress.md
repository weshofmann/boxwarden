# Controlled clipboard progress

Baseline: merged `10ad0bea`; branch `weshofmann/feature/controlled-clipboard`,
Draft PR [#13](https://github.com/weshofmann/boxwarden/pull/13).

Published checkpoint: bounded shared transfer core, nonblocking operation locks,
and exact-generation supervisor handshake. Source capture follows live admission;
text stays out of action journals. Busy operations refuse, and ambiguous writes
are reported without replay. Pinned management SSH is the chosen guest transport.

Verification: focused core/lock/supervisor tests and race checks passed; independent
checkpoint review found no Important/Critical issue in those published scopes.
Synthetic guest helper roundtrips passed for Unicode, whitespace/trailing LF,
empty text and 1 MiB, including later reads. Invalid UTF-8/NUL/oversize refusal
preserved the prior synthetic value. This is helper evidence, not GUI acceptance.

In progress: four public CLI commands, private AppKit adapter, fixed guest mode
and retained SSH integration are implemented locally. Review found guest owner
preclaim cancellation, exact display/session affiliation and blocked stdout issues;
corrections and integration verification continue. Historical intermittent owner
loss remains unattributed; a reproduced session-metadata enumeration race is fixed.
Pinned Tart strip/menu patch and staged build are being implemented separately.

Next: resolve review findings, publish verified CLI/guest integration, then verify
Tart window/menu behavior and real synthetic application copy/later paste.
Installed Tart and root admission remain unchanged. Valuable demo D is untouched;
one disposable probe is used. No real host clipboard has been accessed.
