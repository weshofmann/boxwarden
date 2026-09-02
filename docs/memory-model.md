# Markdown-first memory model

Markdown is the canonical memory format for M1A. No database-backed memory service is part of the milestone.

## Layer 1: canonical project truth

`AGENTS.md`, `docs/architecture.md`, the security/state/persistence documents, and `docs/decisions/` define reviewed project policy and architecture. They are non-sensitive Git-versioned project state.

## Layer 2: durable project memory

The repository reserves this layout for reviewed non-sensitive memory:

```text
memory/
  README.md
  knowledge/       durable concepts and system facts
  lessons/         reviewed operational lessons
```

Files are curated Markdown and travel through ordinary Git review. Repositories and encrypted memory artifacts belong to an explicit security domain. Sensitive durable Markdown is never stored there in plaintext. It is handled by the domain's age-encrypted `sensitive-markdown-v1` profile adapter or by a separately approved project-specific encrypted artifact.

## Layer 3: episodic/session material

Unreviewed notes, candidate lessons, transcripts, and generated summaries are SESSION state. Projects may keep them under a repository-local ignored `.memory/candidates/` directory inside the guest. They disappear with the session unless reviewed, promoted into `memory/`, committed, and pushed to the durable Git remote.

Promotion from session notes to durable memory is a trusted write. Reviewers inspect the exact Markdown bytes/diff for sensitive data, malicious instructions, hooks, links, embedded content, and claims that should not become policy. The review path uses the same terminal-safe presentation rule as profile inspection: no raw control sequences or rich rendering of untrusted Markdown; C0/C1, ANSI/OSC, bidi, and zero-width/format controls are escaped or visibly marked; byte counts, truncation, and exact digests are explicit. No automatic agent-to-memory synchronization is allowed.

Future full-text, vector, graph, or SQLite indexes are derived state. They must be disposable and deterministically rebuildable from canonical Markdown. Loss of an index must not lose knowledge.

Kindex may remain an optional session-local development tool, but its database is not canonical memory and M1A neither captures nor restores it.
