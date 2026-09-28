# Controlled Clipboard Implementation Plan

**Goal:** Deliver explicit clipboard transfers through one shared implementation.
**Architecture:** Typed exact-generation supervisor/pinned-SSH operations plus a
small desktop selection adapter; a standalone menu-bar app invokes the same host CLI.
**Tech stack:** Go, GTK3/Python desktop helper, Darwin AppKit and Swift.
**Spec:** [design.md](design.md). User delegates routine design/plan review decisions.

## Constraints and review focus

1 MiB UTF-8, NUL-free, exact bytes; explicit empty differs from absent. No payload
logs/hashes/argv/journals, no automatic bridge, no host deployment without its gate.
Tests pin stale generation, unsupported text, selection ownership after exit,
ambiguous commit acknowledgement and focus changes before/after click.

## Increments

- [x] Independently review design; prove synthetic desktop copy and later paste
  over pinned SSH on one disposable non-logged-in guest. Record exact packages,
  clipboard ownership and transport results. No alpha rebuild campaign.
- [x] Add internal/clipboardx bounded text/frame and transfer service tests first;
  implement shared validation, immutable target and commit outcome contracts.
- [x] Add fixed guest clipboard helper and SSH/supervisor/session runtime capability.
  Test protocol framing, sanitized errors, generation/deadline/lock behavior and
  guest owner lifetime. Keep recipe/action channels untouched.
- [x] Add clipboard push/pull/copy/paste public parsing and adapters; Darwin AppKit
  isolated behind injected Pasteboard. Tests prove stdin/stdout/TTY --raw, exact
  output and preservation on failure. No real host pasteboard automated tests.
- [x] Add structured one-domain target discovery with fresh readiness evidence;
  test exact tuple, stale snapshot, and unavailable targets. The menu enumerates
  configured domains and issues one explicit-domain query per domain.
- [x] Build a standalone menu-bar `.app` with the current CLI bundled; test
  immutable click capture, all configured domains, availability, stale refresh,
  subprocess argv/environment, cancellation/status and no payload output.
- [x] Return host admission to the previously qualified stock Tart through the
  reviewed attended rollback; then remove viewer patch sources, patch-only
  launch args and custom toolchain support without changing Softnet policy.
- [ ] Run focused tests/race, guest/Swift checks, applicable full suite and CI;
  independently review cumulative clipboard boundary and fix confirmed findings.
- [ ] Complete attended synthetic GUI acceptance after stock display is usable;
  document launch, review outcomes and truthful remaining limitations.

Every verified meaningful increment is committed/pushed promptly; new Draft PR
against main. Do not alter merged PR12 or merge the feature. Progress stays short.
