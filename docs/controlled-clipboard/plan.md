# Controlled Clipboard Implementation Plan

**Goal:** Deliver explicit clipboard transfers through one shared implementation.
**Architecture:** Typed exact-generation supervisor/pinned-SSH operations plus a
small desktop selection adapter; Tart invokes the same host CLI.
**Tech stack:** Go, GTK3/Python desktop helper, Darwin AppKit, pinned Tart Swift.
**Spec:** [design.md](design.md). User delegates routine design/plan review decisions.

## Constraints and review focus

1 MiB UTF-8, NUL-free, exact bytes; explicit empty differs from absent. No payload
logs/hashes/argv/journals, no automatic bridge, no host deployment without its gate.
Tests pin stale generation, unsupported text, selection ownership after exit,
ambiguous commit acknowledgement and focus changes before/after click.

## Increments

- [ ] Independently review design; prove synthetic desktop copy and later paste
  over pinned SSH on one disposable non-logged-in guest. Record exact packages,
  clipboard ownership and transport results. No alpha rebuild campaign.
- [x] Add internal/clipboardx bounded text/frame and transfer service tests first;
  implement shared validation, immutable target and commit outcome contracts.
- [ ] Add fixed guest clipboard helper and SSH/supervisor/session runtime capability.
  Test protocol framing, sanitized errors, generation/deadline/lock behavior and
  guest owner lifetime. Keep recipe/action channels untouched.
- [ ] Add clipboard push/pull/copy/paste public parsing and adapters; Darwin AppKit
  isolated behind injected Pasteboard. Tests prove stdin/stdout/TTY --raw, exact
  output and preservation on failure. No real host pasteboard automated tests.
- [ ] Track narrow Tart patch/build/license identity and window/menu invocation;
  test immutable target, availability, concurrent clicks, cancellation/status and
  menu/button equivalence. Add admitted launch metadata without broad backend API.
- [ ] Run focused tests/race and guest/Swift checks; at integration checkpoints
  run existing applicable suite and hosted CI. Independent cumulative security/
  patched executable review; fix confirmed Important/Critical findings.
- [ ] Stage exact CLI/guest/Tart identities; create one deployment/rollback request
  only if needed. Complete safe GUI synthetic acceptance first where possible.
  Finish usage, review outcomes and truthful remaining attended tests.

Every verified meaningful increment is committed/pushed promptly; new Draft PR
against main. Do not alter merged PR12 or merge the feature. Progress stays short.
