# ADR 014: Defer checkpoints from Milestone 1A

Status: Accepted

Checkpoint creation, resume, lifecycle state, retention, age warnings, backend operations, and tests are not part of M1A. This supersedes only the checkpoint-cloning clause of ADR 011; the rest of ADR 011 remains accepted. M1A proves routinely disposable clean and quarantine sessions without introducing a durable, secret-bearing copy whose restore identity, lineage, taint propagation, compromise containment, and resume semantics are undefined. A future separately reviewed checkpoint design must treat checkpoints as untrusted secret-bearing session state, never as backups or golden inputs, and must define those semantics before implementation.
