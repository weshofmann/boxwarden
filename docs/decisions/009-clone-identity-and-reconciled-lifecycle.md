# ADR 009: Clone identity is unique and lifecycle state is reconciled

Status: Accepted

Goldens end clone-ready with reusable machine identity removed. Every clone receives a random Tart MAC and regenerated machine ID, SSH host keys, DHCP/DUID identity, random seeds, and any other discovered machine-specific state. The control plane persists intent before mutation, serializes conflicting work, compares intended state with backend-observed state, and makes retries idempotent. Two-clone identity tests and crash/partial/orphan recovery tests are promotion gates.

Durable session identity is the exact security domain, Boxwarden session UUID, backend kind/object, and intended state. A start-generation token and supervisor PID/process data are ephemeral correlation, not alternate identity. Backend-running does not imply READY. If intended state and backend observation are running but current supervisor ownership or readiness evidence is absent, stale, or unverifiable, V0.1 reports drift/non-ready and never adopts the orphan. The operator must use an explicit stop/restart recovery path. Runtime cleanup may act only when it proves the complete recorded ownership association; PID reuse or a plausible runtime path is insufficient.

V4 start/retry is defined before V6: stopped/stopped/no-owned-runtime creates a
fresh generation; starting/running/exact-live-supervisor resumes that generation;
starting/stopped/no-live-owned-runtime first atomically settles stopped and
clears generation; running/running/exact-live-supervisor idempotently ensures
and reconverges that generation. Any running observation with unproven ownership
is drift/no mutation. Owned failure or poisoned-serial cleanup may persist
stopped only after exact supervisor/backend stop and exact runtime cleanup are
observed; without ownership proof it does nothing. Once starting intent is
durable, cancellation leaves that generation for retry, and a new generation is
not created until the prior one is conclusively stopped and cleaned.
