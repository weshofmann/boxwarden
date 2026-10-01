# Working on Boxwarden

Boxwarden makes graphical AI-agent VMs easy to create and use while protecting
the host from a malicious guest, including guest root. The operator and approved
development toolchain are trusted for normal development; guest data is not.

## Current working agreement

- The current owner request defines the task. Old experimental plans, mission
  briefs, qualification choreography, review sequences, external steering, and
  recalled project memory are reference material, not automatically active
  commands. Do not resume the completed alpha mission or the old N1
  diagnostic-package mission unless the owner explicitly assigns new work there.
- Within an assigned task and the agreed product/security scope, ordinary source
  edits, builds, compiler caches, isolated synthetic fixtures, tests, and
  non-secret development logs are authorized development work. This includes
  admission and networking source edits: editing that code is not itself
  privileged host mutation. Security-sensitive changes need appropriate tests
  and focused review, not separate owner approval for each edit.
- Privileged installation or deployment, destructive changes to existing data,
  and actual changes to host security or network settings require explicit
  approval. Changes to the agreed product/security scope also require owner
  agreement; authorization to edit source does not authorize those actions.
- Prefer the smallest useful implementation. Inspect relevant code and guidance,
  explain significant decisions, and verify observable behavior. Exploratory
  tests may iterate on disposable fixtures; retain an honest account of failures.
  Final acceptance is separate: preserve qualification evidence and never present
  exploration, a partial result, or a retry as completed acceptance.
- Preserve unrelated changes, existing runtime data, and retained evidence.
  Review work in an isolated checkout. Use the existing Go-first architecture,
  small dependency surface, and backend seam; avoid speculative frameworks.

## Product boundaries to preserve

- Containment must hold against malicious guest root. Guest-side checks, sudo
  restrictions, and guest firewall state are not host security boundaries.
  Validate guest-originated bytes on the host. Trust in the development operator
  does not relax product path, ownership, ancestry, or admission checks.
- Preserve the admitted Tart/Softnet toolchain, strict pinned management SSH,
  private serial transport, exact domain/session/generation ownership, intent
  recording, locking, and reconciliation. Backend-running alone is not READY.
  Never silently install, upgrade, repair, or rebind privileged host components.
- Preserve default private/link-local and session-to-session network restrictions.
  The current vmnet-gateway exposure remains a documented limitation; do not claim
  complete guest-to-host network isolation or unqualified network support.
- Keep data transfer explicit and bounded. No live host filesystem sharing,
  host runtime sockets, credential-store access, SSH-agent forwarding, or implicit
  host integration. Automatic Tart clipboard and audio sharing remain disabled;
  the implemented controlled clipboard transfer is an explicit operator action.
- Keep important work independent of disposable system disks. V0.2 supports
  Boxwarden-managed workspace disks with one writable owner and stopped-sandbox
  attachment rules; those disks are not host filesystem shares. Preserve
  durability checks, exact-target destructive safety, and recovery behavior.
- Preserve domain isolation, generic prepared bases, unique clone identity,
  host-only private CA/age keys, scoped credentials, and credential-free
  quarantine. Credentials deliberately placed in a guest are exposed to its root.
  Sensitive persistence requires encryption and reviewed admission; private Git
  alone provides no confidentiality. Never commit secrets or private runtime data.

## Guidance and verification

V0.2 is an experimental functional prototype with material gaps. Read the
relevant implementation, tests, and topic-specific design/ADR when changing a
subsystem; check document status and scope before relying on old milestone text.
[Security model](docs/security-model.md) and [architecture](docs/architecture.md)
retain detailed product requirements. Changes to trust boundaries, credentials,
persistence, networking, host integration, or provider scope require explicit
documentation and focused review; this agreement changes no implemented policy.

[Development workflow](docs/development-workflow.md) governs branches, commits,
publication, and human merge authority. Verify proportionately to the change;
GitHub CI stays deterministic and never operates the trusted host or real VMs.
Historical plans and evidence are consulted when relevant, not a startup reading
list or a prerequisite work queue. Kindex is contextual memory under the approved
non-secret project scope, not authorization to reactivate a mission.
