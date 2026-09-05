# ADR 004: Separate golden, profile, identity, project, and session state

Status: Accepted

GOLDEN is generic reproducible software/configuration. It never contains a Boxwarden security-domain identity, domain management CA anchor, or fixed domain principal. PROFILE is reviewed persistent declarative configuration. SECRETS / IDENTITY is session-scoped authentication. PROJECT is externally durable work. SESSION is disposable mutation. Confidentiality and execution trust are independent classifications applied to persistent artifacts.

SECURITY DOMAIN is an orthogonal namespace applied to trusted-host admission, selection, identity, and state for every layer; there is no implicit cross-domain lookup or sharing. A domain-scoped registration can explicitly admit one exact generic artifact, and two domains can independently admit the same exact artifact without making it domain-specific. Registration proves only the exact existing/stopped backend identity and explicit operator admission that it records, not unrecorded provenance, clone-readiness, or qualification evidence. These distinctions prevent durable truth from depending on VM disks and prevent secrets or domain binding from entering goldens or profiles.
