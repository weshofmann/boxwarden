# Project memory

Markdown is the canonical durable memory format for this repository.

- `knowledge/` holds reviewed system facts and durable concepts.
- `lessons/` holds reviewed operational lessons.
- guest-local `.memory/candidates/` holds unreviewed episodic notes and is disposable.

Only non-sensitive reviewed Markdown belongs here. Sensitive memory uses the domain-scoped age-encrypted profile path described in `docs/memory-model.md`; database and search indexes are derived state and must be rebuildable.
