# ADR 011: Host-neutral control plane, narrow backend seam, and security domains

Status: Accepted; the checkpoint-cloning clause is superseded by ADR 014 for M1A

The product and repository are Boxwarden; the executable is `boxwarden`. Common Go packages own policy, security-domain scoping, lifecycle intent/reconciliation, locks, golden selection, profiles/encryption, project durability, credentials/provider scope, validation, and destructive safety. The M1A Tart adapter owns only VM mechanics, actual-state/address observation, checkpoint cloning, and restricted launch arguments. Portable guest definitions are distinct from host/backend-specific golden artifacts. Every session belongs to one explicit local security domain, and golden pointers, profiles, age material, credentials, memory, projects, registry, and runtime paths never fall back across domains. M1A implements macOS/Tart only; it creates no speculative generic hypervisor or Linux backend.
