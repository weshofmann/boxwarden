# N1 implementation plan

Goal: deliver a reviewed, tested source/build candidate and an exact attended
deployment package without changing the working privileged installation.
Architecture: a small pinned Softnet Rust patch selects a strict packet policy
through stock Tart's existing block argument. Boxwarden remains Go-first.
Spec: design.md. Execution: driver integration, Sol implementation workers,
fresh Astra boundary review. The owner delegates routine approvals.

## Constraints and review focus

Keep stock viewer, vmnet DNS and host effective routes; no guest-enforced trust.
No new privileged installation, system firewall changes, real clipboard reads,
existing guest disruption or unreviewed admission bypass. Exact artifacts only.
Focus: spoofed/fragmented packets; host-local public addresses and refresh errors;
DHCP poison/renewal/expiry; forged or stale management flows; stock-binary fallback.

## Tasks

- [x] Verify actual 8fd1ded baseline in new feature worktree; Go suite passes.
- [x] Independently review design and resolve concrete boundary findings.
- [ ] Add pinned Softnet patch/source packaging under tools/n1-softnet with
  AGPL notice. Extract pure policy test seam and add failing packet regressions
  before implementation. Show baseline permits ordinary gateway service traffic.
- [ ] Implement packet policy, trusted host-address refresh and integrated
  ingress/egress hook. Run deterministic suite and bounded fuzz; build locked
  unprivileged macOS artifact using exact compiler; record identities.
- [ ] Implement closed n1candidate build-tag admission/launch integration with wrong-artifact,
  missing-policy and restart checks. Ensure default installed behavior remains
  honestly reported. No privileged deployment occurs in this mission authority.
- [ ] Fresh consequential code review; fix Important/Critical findings; run
  appropriate Go/Rust/build checks; publish coherent commits and maintain Draft PR.
- [ ] Produce attended gate with prerequisites, exact scope, positive controls,
  rollback, evidence matrix and resource cleanup; finalize sanitized report.

Private mission record tracks retirement and live resources separately. Failed
real qualification runs are immutable; corrections require a fresh owned trial.
