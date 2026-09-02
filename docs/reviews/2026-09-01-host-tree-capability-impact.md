# Host-tree capability architecture impact review

Date: 2026-09-01

Status: Working impact analysis for proposed ADR 021; no Accepted architecture
changed

## Conclusion

Explicit read-only host trees are a bounded refinement of Boxwarden's
no-host-integration default, not a Tart convenience flag. Accepting the
capability would require coordinated changes to common policy, state,
lifecycle, validation, backend qualification, and public threat claims.

The smallest safe first implementation is read-only live trees only. The stable
baseline and OverlayFS results should remain a separate experimental feature
until ownership normalization and baseline lifecycle are designed. Promotion
must remain a third, later gate.

## Canonical invariant affected

Current invariant:

> The guest receives no trusted-host filesystem share.

Proposed refinement:

> No ambient or writable trusted-host filesystem exposure exists by default.
> One exact canonical host tree may be disclosed read-only only through an
> explicit domain/session-owned capability. A normal writable project view
> remains guest-local. Durable mutation requires a separate trusted-host
> admission operation.

This changes the confidentiality surface but is intended to preserve host
integrity. It does not change the rule that guest root is malicious, nor the
rule that a disclosed secret is compromised for the session.

## Document impact inventory

| Canonical source | Current assumption | Required change if accepted |
| --- | --- | --- |
| docs/architecture.md | No filesystem share or extra disk | Describe the common HOST TREE CAPABILITY, default absence, read-only property, and backend-neutral contract |
| docs/security-model.md | No trusted-host filesystem available | Replace the absolute claim with exact opt-in disclosure; add root/path/symlink/namespace and Virtualization.framework attack surfaces |
| docs/state-model.md | PROJECT is durable external truth; SESSION holds mutable worktree | Record a domain/session/project capability reference and distinguish live H, sealed B, disposable overlay, and materialized G without making upperdir portable |
| docs/persistence-and-encryption.md | Admission focuses on named profile adapters | Keep profile admission separate; future proposal promotion needs its own exact-byte/tree admission and review binding |
| docs/lifecycle-and-recovery.md | Start has no share; destroy checks project durability | Add source revalidation, attachment observation, baseline reference lifetime, guest-local upper cleanup, and fail-closed source-loss behavior |
| docs/credentials.md | Secrets are injected or guest login state | Add forbidden source roots and state clearly that host-tree capability is never a credential delivery mechanism |
| docs/tool-provenance.md | Tart + Softnet pair is qualified | Treat directory sharing and APFS/copy tooling as security/correctness-critical inputs requiring versioned evidence |
| AGENTS.md / CLAUDE.md | Host integration is prohibited without architecture review | If accepted, replace only the exact filesystem clause with the bounded capability; retain every other prohibition |

No existing canonical document should be edited while ADR 021 remains Proposed.

## ADR impact inventory

| ADR | Relationship |
| --- | --- |
| ADR 001 | Tart remains the host security boundary; guest Docker/OverlayFS remains inside it |
| ADR 003 | Its minimal-host-integration rule explicitly requires separate review; ADR 021 would be that filesystem-specific review, not a network change |
| ADR 004 | Stable B, proposal G, durable H, and guest upper state must retain GOLDEN/PROJECT/SESSION separation |
| ADR 005 | Existing profile writeback is not reusable as a generic tree promotion protocol; exact review binding is analogous but independently designed |
| ADR 009 | Capability and baseline intent must be persisted before backend mutation and reconciled from observation |
| ADR 010 | Goldens contain no host source or session baseline; the capability is attached only at session run time |
| ADR 011 | Common code owns admission/domain/state; Tart owns only attachment mechanics and observation |
| ADR 013 | The Task 0 launch policy changes only by an explicitly qualified read-only attachment; Softnet properties remain unchanged |
| ADR 015 | Network policy is independent; allowed egress still determines how disclosed bytes may be exfiltrated |
| ADR 016 | Unrestricted guest sudo is the reason read-only enforcement must hold against root and ownership normalization cannot be a security control |
| ADR 017 | The existing private serial relay must remain present and must not collide with share runtime paths |
| ADR 020 | Core platform qualification remains complete, but enabling the new attachment requires an additive capability-specific qualification matrix |

No ADR is silently superseded. ADR 021 itself remains Proposed.

## Backend contract impact

The current Task 2 contract prohibits every share. If ADR 021 is accepted, the
common immutable launch policy would carry a validated list of neutral
host-tree capabilities and required properties. It would not carry Tart tags or
--dir strings.

The Tart adapter would:

- map each capability to one generated read-only VirtioFS argument;
- reject writable or raw user-supplied directory options;
- preserve the exact Task 0 network/serial/clipboard/audio policy;
- report enough attachment observation for common reconciliation;
- fail unsupported combinations rather than weakening isolation.

Backend tests need both exact Tart argv and property-oriented attack tests.
Future backends must requalify the property; successful Tart tests do not make
the abstraction universally safe.

## State and lifecycle impact

A session record would need, conceptually:

- immutable capability identity;
- canonical source and guest destination;
- access class;
- domain/session/project ownership;
- source admission result/digest of policy inputs;
- live or baseline identity;
- last backend observation/error;
- baseline reference and cleanup state when proposal mode eventually exists.

Create/configure persists this intent before clone/start. Start revalidates
source structure and collision policy. Status makes the disclosure and access
class visible. Stop removes the per-run attachment. Destroy removes only
recorded guest/session objects and reference-counted owned baselines; it never
deletes H.

Source removal or replacement while stopped prevents readiness. During a run it
must surface as attachment loss/failure, not be silently rebound. A baseline
cannot be collected while any session or candidate comparison still references
it.

## Validation impact

Task 10 currently asserts that no host share exists. If ADR 021 is accepted,
that assertion becomes:

- no unrecorded, writable, overlapping, cross-domain, or forbidden-root share;
- every recorded share is exact, canonical, read-only, and visible in status;
- root cannot mutate H or broaden the tree;
- network and serial properties remain qualified;
- the running backend arguments equal persisted capability intent.

Acceptance adds negative tests for root remount, parent/sibling reads,
source-root and child symlinks, source namespace replacement, collisions,
nonexistent sources, source loss, special files, host instability, and cleanup.

## Plan impact

The current M1A plan would require deliberate edits in:

- Task 1: neutral capability types, validation, and CLI/status contract;
- Task 2: backend property and Tart read-only mapping;
- Task 6: persisted intent, revalidation, lifecycle, and reconciliation;
- Task 9: project association and durability semantics;
- Task 10: root/path/destructive acceptance and revised no-ambient-share check;
- Task 11: traceability and final host-integration audit.

Stable-baseline creation, OverlayFS setup, G materialization, comparison, and
promotion should not be squeezed into those tasks without a separate plan. The
v0.1 scope review recommends deferring the entire capability so the first
control-plane release does not absorb a second persistence system.

## Security review questions still open

- Which exact host roots and overlaps are structurally rejected in common code?
- Is read-only live-tree disclosure sufficient, or is every project use case
  actually better served by sealed B?
- What guest ownership normalization avoids expensive recursive metadata
  copy-up?
- How is a stable baseline quiesced or verified against concurrent source
  mutation?
- What host-side materialization channel obtains G without introducing a
  writable host share?
- What canonical semantic manifest and terminal-safe diff bind future review?
- How are baseline references recovered after host/control-process failure?

Only the first question is needed for a read-only implementation. The rest
belong to the later proposal/promotion design review.
