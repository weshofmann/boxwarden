# CLAUDE.md — working on Boxwarden

Guidance for Claude Code sessions in this repository. `docs/development-workflow.md` is the canonical Git/GitHub workflow policy, `AGENTS.md` holds concise operational invariants, and this file is subordinate guidance. Where they disagree, the workflow document and `AGENTS.md` win.

## What Boxwarden is

Boxwarden creates and manages secure, routinely disposable VM workstations for autonomous AI agents.

- **The VM is the trust boundary** between autonomous agents and the trusted host. Boxwarden does not prescribe how workloads execute inside the guest: native processes, language runtimes, Docker, Podman, other guest-local runtimes, and no runtime are all valid. None substitutes for VM isolation.
- **Tart is the first backend, not the product.** M1A is macOS host + Tart + Ubuntu 24.04 ARM64 guest. A Linux/KVM backend is an intended future evolution. Keep the backend seam narrow; do not build a generic hypervisor framework.
- **Go-first.** The `boxwarden` control plane is Go with a deliberately small dependency surface. Module: `github.com/weshofmann/boxwarden`. Shell is for small guest bootstrap/provisioning tasks only. Node exists *inside the guest* for third-party tooling; it is never Boxwarden's implementation platform.
- **Current scope: Milestone 1A.** Determine actual progress from Git history, the worktree, and the M1A plan rather than from a snapshot in this file.

## Read before changing anything architectural

In order:

1. `AGENTS.md` — the canonical invariant list. Short. Read it every time.
2. `docs/architecture.md` — components, backend seam, build products.
3. `docs/security-model.md` — threat model and required backend properties.
4. `docs/state-model.md` — the five layers and the two classification axes.
5. `docs/decisions/` — the accepted and provisional architecture decisions. Read their current status and supersession notes rather than assuming every numbered ADR remains authoritative.
6. `docs/superpowers/plans/2026-09-01-boxwarden-v0.1.md` — the executable
   V1-V4 plan and corrected roadmap. The 2026-08-30 milestone plan is retained
   only as superseded planning history and must not be executed.
7. `docs/reviews/2026-08-30-independent-architecture-review.md` — historical review evidence and rationale. Canonical docs and later ADRs contain the reconciled dispositions. **Read this before starting Task 0.**
8. `memory/knowledge/tart-and-guest-platform-facts.md` — verified external facts (Tart CLI, Softnet policy, Ubuntu imaging). Check here before re-deriving or guessing platform behavior.

Topic-specific: `docs/persistence-and-encryption.md`, `docs/credentials.md`, `docs/lifecycle-and-recovery.md`, `docs/memory-model.md`, `docs/tool-provenance.md`, `docs/provider-data-scope.md`, `docs/threat-model-node-npm.md`, `docs/kindex-profile-policy.md`.

## Non-negotiable security invariants

Never weaken these to make something work. If a task appears to require it, stop and write up the conflict instead.

- **No host filesystem sharing.** No `--dir`, no virtiofs, no 9p, no extra `--disk`, no Rosetta share.
- **No host container-runtime socket or context.** A guest-local runtime may be used when a workload needs it, but it never receives trusted-host runtime control. Docker-group membership is guest-root-equivalent.
- **No host SSH agent forwarding**, no X11 forwarding, no TCP/tunnel forwarding, no VNC, no nested virtualization, no Tart port exposure.
- **No guest-agent bridge is part of M1A** (including `tart-guest-agent`). Introducing one requires explicit architecture review because it changes the host↔guest trust surface.
- **The guest must not initiate connections to private/link-local networks or concurrent sessions by default.** M1A accepts and reports that default Softnet permits access to services on the vmnet gateway because the same gateway provides DNS required for VPN, split-DNS, DNS64, and changing travel networks. V4 implements this default policy only and rejects every allow flag. Future ADR 015 support may add an explicit per-session allowlist of exact validated private CIDRs only after persisted record/status/CLI semantics exist; every other private/link-local destination and every concurrent session must remain denied, and status must disclose the exception. Never claim guest-to-host network isolation, hard-code public DNS, use physical bridging/Tart host networking, add implicit/broad LAN access, or add `--net-softnet-allow=0.0.0.0/0`.
- **Concurrently running sessions must not reach each other**, in the same or different security domains.
- **Generic goldens contain no domain trust.** No provider login, browser profile,
  token, private key, project checkout, cache, session residue, Boxwarden domain
  identity, management-CA anchor, fixed principal, or guest binding belongs in a
  golden. A fresh clone receives only the selected domain CA's public anchor and
  exact session principal through the ADR 017 trusted serial bootstrap before
  pinned strict SSH is attempted; the private CA key never leaves the host. The
  V2 attended gate must use an artifact built or rebuilt from the corrected
  generic guest definition and qualified accordingly; prior qualification does
  not grandfather an unchanged domain-bound Task 0 artifact.
- **Host and domain initialization are separate.** `boxwarden init` and
  `boxwarden doctor` are host-global and operate outside the security-domain
  namespace. `boxwarden --domain D domain init` creates only D's management CA
  and never installs or modifies the host-global Softnet privilege mechanism.
- **No implicit cross-domain fallback.** Commands that operate on domain-owned state require an explicit security domain. Every session belongs to exactly one domain. Golden pointers, profiles, age material, credentials, memory, projects, registry, and runtime paths are domain-scoped and never resolved across domains.
- **No automatic sandbox → trusted-profile synchronization.** Promotion is always explicit, human-approved, and bound to exact manifest and ciphertext digests.
- **Validate guest-originated data on the trusted host.** Guest-side checks are convenience, never the control. A compromised session controls every byte it sends.
- **Quarantine sessions receive no reusable provider or Git credentials.** Public source, or narrowly scoped short-lived read-only ingress only.
- **A compromised session is not trusted during rescue or destruction.** Do not query it, inject credentials into it, or push from it. M1A has no checkpoint operation.
- **age private identities never leave the trusted host.** Not into a guest, future retained-session artifact, golden, repository, profile store, or Tart disk.
- **Markdown is canonical memory.** Any index (SQLite, FTS, vector, graph) is disposable derived state and must be rebuildable from Markdown. No Kindex capture or restore in M1A.
- **No mutable piped installers** (`curl … | sh`) in golden construction. An artifact that cannot be verified is unavailable for that revision.

## Engineering conventions

- Go standard library unless a dependency is justified and locked.
- Never invoke a shell for host control-plane commands — argv-only, via `internal/execx`.
- Never accept an unvalidated path as a backend object name or filesystem target.
- Write intent before mutation; lock conflicting operations; reconcile against observed state; make retries idempotent.
- **Fail closed.** Unknown, unverifiable, or ambiguous state blocks the operation.
- Test failure paths before success paths count as done. Express security requirements as properties, not as flag-string assertions — a test that greps for Tart flags is not evidence of a policy.
- Keep M1A scope narrow. No speculative generalized abstractions. Check the plan's "Explicitly deferred" list before adding anything.

## Workflow expectations

- Read the relevant docs and ADRs **before** changing architecture, not after.
- Changing accepted architecture requires a new ADR in `docs/decisions/`. Do not silently revise an accepted one — supersede it.
- Changes to trust boundaries, credentials, persistence, networking, host integration, or provider data scope require explicit documentation and review. Propose in writing first; do not implement and then document.
- Preserve the lifecycle and recovery properties. If a change touches intent recording, locking, or reconciliation, say explicitly what happens on crash between intent and result.
- Run the verification block for the task you are working on. Report what you actually ran and what it showed.
- Do not modify unrelated files. Do not start a later milestone implicitly — M1B is gated and requires separate approval.
- If a plan task appears wrong or blocked, write up the conflict and stop. Do not route around it.

## Git rules

- **No force pushes** unless explicitly requested.
- **No history rewriting** (rebase onto published commits, amend of pushed commits, filter-branch) without an explicit request.
- **Do not delete branches, tags, or other refs** unless asked.
- Follow `docs/development-workflow.md` for canonical branch naming and ownership. Branches published directly upstream use `<owner>/<type>/<topic>`; the owner is the accountable repository identity, not the tool performing edits. Never work directly on or push directly to `main`.
- After a commit has passed its applicable local verification, push it immediately; create or update the agent-owned Draft PR after the first meaningful verified push.
- Inspect `git status` and `git diff` before committing. Keep commits logically focused, detailed, and free of unrelated staged changes.
- Before marking a PR Ready, review its cumulative diff against the actual base, record verification, and fix branch-caused CI failures. Agents never merge, enable auto-merge, bypass checks, or close a human-created PR.
- GitHub-hosted CI is deterministic only; real-host Tart/Softnet and credential/provider qualification are not CI work. `docs/development-workflow.md` is the canonical workflow authority; `AGENTS.md` carries concise operational invariants.
- Never commit a key, token, identity file, candidate plaintext, runtime state, generated VM, or evidence containing private paths.

## Establish current repository state

Inspect `git status`, recent history, remotes, the plan, and the filesystem before relying on any progress claim. The repository is Apache-2.0 licensed (`LICENSE`, `NOTICE`). A documented directory may be intentionally absent when Git has no reviewed file to track there; do not create placeholders merely to make a prose tree exist.
