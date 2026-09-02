# ADR 004: Separate golden, profile, identity, project, and session state

Status: Accepted

GOLDEN is reproducible software/configuration. PROFILE is reviewed persistent declarative configuration. SECRETS / IDENTITY is session-scoped authentication. PROJECT is externally durable work. SESSION is disposable mutation. Confidentiality and execution trust are independent classifications applied to persistent artifacts. SECURITY DOMAIN is an orthogonal namespace applied to every layer; there is no implicit cross-domain lookup or sharing. These distinctions prevent durable truth from depending on VM disks and prevent secrets from entering goldens or profiles.
