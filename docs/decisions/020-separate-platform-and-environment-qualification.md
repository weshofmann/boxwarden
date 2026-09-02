# ADR 020: Separate core platform qualification from environment compatibility

Status: Accepted

## Context

The original Task 0 gate intentionally failed closed: M1A implementation could
not begin until every required platform and network property had empirical
evidence. That framing was useful while the basic macOS, Tart, Softnet, and
Ubuntu combination was uncertain.

Task 0 has now reproduced the core workstation on two clean installs and two
independent clones. It has qualified the VM boundary, full Ubuntu desktop,
Wayland/XWayland, management and recovery access, unique clone identity,
default network restrictions, public connectivity, inherited DNS, real work
VPN and scoped/split-DNS behavior, time-zone convergence inputs, and the actual
graphical-process lifetime. This is sufficient evidence that the selected
platform can support M1A implementation.

The available mobile tether was IPv4-only. It could not exercise an effectively
IPv6-only host upstream, IPv4-only destinations through an IPv6-only/NAT64-style
upstream, or IPv6-only destinations under that representative upstream. Those
missing environments do not contradict any observed core result, but treating
them as supported would be equally unjustified. Keeping the entire
platform-selection gate open until every travel network becomes available would
couple implementation progress to environmental access rather than remaining
platform uncertainty.

## Decision

Task 0 closes as **PASS WITH CONDITIONS**. Core M1A platform qualification is a
terminal decision for the recorded host/toolchain pair and guest configuration.
Environment-specific compatibility is a separately extensible evidence matrix.

Every core evidence key must be qualified before the Task 0 validator passes.
The terminal evidence record explicitly enumerates the only environmental keys
that may remain `NOT YET PROVEN`:

- `ipv6_only_upstream`;
- `ipv4_only_destination`;
- `ipv6_only_destination`.

An unqualified environment is not a failure, but it is also not supported.
Documentation, status, validation, and release claims must stay within the
tested matrix. Future empirical runs may promote deferred rows for the same
qualified policy without reopening the basic platform-selection decision.
Changing the host/backend/toolchain pair, launch policy, trust boundary, or
relevant guest network configuration still requires deliberate requalification.

The validator rejects a terminal record if any core row is missing, duplicated,
misclassified, or `NOT YET PROVEN`; if the declared deferred set differs from
the three approved keys; or if any other row remains `NOT YET PROVEN`. A
deferred row must be either `NOT YET PROVEN` or empirically promoted to
`OBSERVED`; `INFERRED` and `VENDOR-DOCUMENTED` do not qualify an environment.
Promotion does not invalidate the terminal gate.

## Consequences

Task 1 and Task 2 may begin when explicitly authorized. Their implementation
must consume only Task-0-qualified properties and must represent unsupported
network environments honestly rather than generalizing from ordinary IPv4 or
the tested VPN. Regression testing remains mandatory for every environment the
project claims as qualified.

ADR 015's shared/NAT policy, accepted vmnet-gateway exposure, default
private/link-local denial, VPN/split-DNS result, session isolation, and broad
allow-all prohibition remain in force. Softnet's host-root requirement and
artifact-integrity constraints also remain unchanged.

This decision separates evidence gates; it does not implement lifecycle code,
weaken a security boundary, or begin Task 1.
