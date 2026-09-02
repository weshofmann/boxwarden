# ADR 001: Tart is the host boundary; guest workloads remain neutral

Status: Accepted

For M1A, Tart isolates autonomous tooling from macOS. Boxwarden does not
prescribe how workloads execute inside the Ubuntu guest: they may use native
processes, a container runtime, language runtimes, or no runtime at all. A
guest-local runtime is never a substitute for VM isolation, and it never
receives a trusted-host runtime socket, context, credential store, or
equivalent host control.

Future host backends must provide the same host-isolation properties but do not
alter the M1A choice. ADR 023 defines the guest workload-neutrality decision
and its consequences for golden construction and documentation.
