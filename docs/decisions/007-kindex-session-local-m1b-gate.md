# ADR 007: Session-local Kindex and full-state persistence gate

Status: Accepted

Kindex remains optional disposable session state. A persistent service would widen the sensitive-data blast radius and is not supported by the current local SQLite Kindex model. Milestone 1A has no Kindex adapter, flag, opaque-state fallback, or external SQLite-copy workaround and reports full-state persistence unsupported. Milestone 1B begins only after Kindex exposes a supported, transactionally consistent, full-fidelity backup/verify/restore mechanism and passes same-version round-trip plus forward-migration tests. `kin export` remains separately named interchange, never backup.
