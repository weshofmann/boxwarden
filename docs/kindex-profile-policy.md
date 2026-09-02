# Kindex profile policy

The locally installed Kindex 0.32.0 is SQLite/WAL-backed at ~/.kindex/kindex.db, not MySQL. It also uses profile data directories and may hold archive databases, topics, skills, inbox, synonyms, budget data, and global/project configuration. The store performs forward schema migrations.

kin export/import is reduced-fidelity interchange, not a profile backup. It omits application state and important graph metadata such as provenance/trust fields, status, timestamps, reminders, candidates, suggestions, activity state, retrieval state, and archive data. Do not call it backup.

Markdown is canonical project memory for M1A. Kindex may remain an optional installed development tool when its artifact passes golden qualification, but its database and companion directories are disposable session state rather than canonical or profile state.

Milestone 1A has no Kindex capture, restore, adapter flag, generic opaque-state mechanism, or fallback. Named adapter source allowlists are the primary boundary that excludes Kindex and every other unregistered path. As defense in depth, profile path validation also rejects `~/.kindex`, project `.kin` private/cache/state paths, configured Kindex profile directories, and canonical aliases of those paths. The denylist is not sufficient by itself and must never replace adapter allowlisting. `boxwarden` reports Kindex persistence unsupported and never substitutes `kin export`.

If Kindex persistence is reconsidered, it remains independently gated. The gate requires a Kindex-native, documented full-fidelity backup capability: transactionally consistent snapshot creation under Kindex control; verify; restore into an empty profile; graph/application-state preservation; schema/Kindex/profile version metadata; same-pinned-version restoration; integrity hashes; and successful round-trip plus forward-migration tests. Do not implement an external SQLite-copy workaround contrary to Kindex policy.
