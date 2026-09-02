# ADR 023: Guest workload neutrality

Status: Accepted

## Context

Boxwarden's security boundary is the VM between an autonomous agent and the
trusted host. Earlier language described Docker/OCI as the guest workload
boundary, which incorrectly made one implementation approach sound mandatory.
The guest is an agent-controlled workstation, including guest-root authority;
a process or container boundary inside it cannot replace VM containment.

## Decision

Boxwarden does not prescribe how workloads inside a guest are built or
executed. A guest may use native processes, Docker, Podman, another container
runtime, language runtimes, or no container runtime.

OCI remains a useful portability format for workloads that choose it, but it is
not a Boxwarden runtime requirement. A golden may include a specifically
qualified guest-local runtime when a workload or platform needs it; that is a
golden capability, not the product security boundary.

M1A must never expose a trusted-host Docker, Podman, containerd, or equivalent
runtime socket, context, credential store, filesystem control channel, or
control-plane authority to a guest by default. Any guest-local runtime is under
guest-root control and must be analyzed as part of the guest's own attack
surface, not as containment from the trusted host.

## Consequences

- Tart remains the M1A host-security boundary under ADR 001.
- Guest definitions and acceptance tests must describe only the runtime
  capabilities actually included and qualified for a golden revision.
- Workloads remain independent of Tart; using OCI is optional rather than an
  architectural requirement.
- Runtime-specific guest firewall, listener, and privilege behavior remains
  subject to the corresponding golden's security policy and qualification.
