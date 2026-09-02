# ADR 019: Converge every guest to the host time zone

Status: Accepted; qualified by Task 0

## Context

Boxwarden runs on a work laptop that can move between time zones. A fixed UTC
guest makes desktop clocks, calendar data, logs viewed by a human, browser flows,
and other local-time behavior disagree with the host. Copying the host zone only
while building a golden is insufficient: a golden or stopped session can outlive
the laptop's current location.

The guest's time zone and its clock source are separate concerns. The time zone
selects local wall-clock and daylight-saving rules. The virtual clock and normal
guest time-synchronization service keep the underlying instant synchronized.

## Decision

Every M1A guest uses the trusted host's current IANA time-zone name. Task 0
resolves macOS `/etc/localtime` through the host zoneinfo tree, rejects a value
that cannot be resolved and validated, renders that exact name into the
autoinstall seed, and verifies the installed guest reports the same name.

The future common lifecycle redetects the host zone during every create and
start transition, applies it through the bounded guest-management path, reads
the effective value back, and only then reports the session ready. It does not
silently fall back to UTC. This policy stays above the backend seam: Tart owns VM
mechanics, not guest workstation configuration.

M1A has no checkpoint-resume operation. If checkpoint support is designed in a
future milestone, its resume transition must perform the same convergence.

## Consequences

Travel to another time zone is handled when a stopped guest next starts without
rebuilding its golden. A newly booting clone initially receives the build host's
validated zone and is converged again before readiness, closing the interval
before guest management becomes available.

Failure to detect, apply, or verify the zone makes the lifecycle transition
visibly incomplete or failed. This favors an actionable mismatch over a guest
that appears ready with a misleading clock.

Time-zone convergence is a correctness property, not a security boundary. The
agent owns guest root and can change the zone after readiness. No time-zone value
is secret, and this decision does not add host filesystem sharing or broaden the
Tart backend interface.
