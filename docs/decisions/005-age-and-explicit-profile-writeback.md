# ADR 005: age encryption and explicit profile writeback

Status: Accepted

Sensitive persistent artifacts use age with per-domain daily and offline-recovery recipients and host-only private identities. Git is not treated as confidential storage. M1A profile adapters are named and declarative with fixed paths, types, limits, validation, semantic diff, staged apply, and rollback; arbitrary archives and opaque state are rejected. Capture immediately encrypts a candidate, and approval binds its exact normalized-manifest and ciphertext digests. There is no automatic session-to-profile synchronization or cross-domain recipient fallback.
