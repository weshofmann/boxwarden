# ADR 009: Clone identity is unique and lifecycle state is reconciled

Status: Accepted

Goldens end clone-ready with reusable machine identity removed. Every clone receives a random Tart MAC and regenerated machine ID, SSH host keys, DHCP/DUID identity, random seeds, and any other discovered machine-specific state. The control plane persists intent before mutation, serializes conflicting work, compares intended state with backend-observed state, and makes retries idempotent. Two-clone identity tests and crash/partial/orphan recovery tests are promotion gates.
