# ADR 008: Markdown is canonical memory; indexes are derived

Status: Accepted

Reviewed non-sensitive project memory is Git-versioned Markdown. Sensitive durable Markdown is age-encrypted through an explicit adapter. Unreviewed notes remain disposable session candidates until an exact diff is reviewed and promoted. Search, vector, graph, SQLite, and Kindex databases are derived or session state, never the sole copy of knowledge. This makes memory reviewable, portable, and recoverable without requiring a stateful service.
