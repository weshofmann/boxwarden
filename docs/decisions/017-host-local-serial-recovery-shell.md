# ADR 017: Host-local serial bootstrap and draining

Status: Accepted; amended for the approved MVP simplification on 2026-09-07.
The earlier retained recovery-console decision is superseded for MVP.

## Supersession and evidence boundary

The approved simplification trusts the host and cooperating Boxwarden host
processes. It removes GNU Screen admission, ownership, health, and READY
requirements; the second/operator PTY; console leases; generalized console
arbitration; and cryptographic same-UID supervisor ownership reconstruction.
This amendment changes the existing decision in place. Operator-console UX is
deferred and is not part of MVP.

Task 0's socat/two-PTY/Screen harness and its recovery-console observations are
historical qualification evidence for that exact harness. They do not qualify
the new one-PTY implementation. Slice A establishes and deterministically tests
the minimal supervisor and serial foundations; it performs no Tart, Softnet,
VM, or controlled-host runtime checks. Controlled product checks begin with
actual launch in Slice B, then bootstrap in C and strict SSH/READY in D.
Formal production qualification remains a separate release gate.

ADR 012's domain user-CA, short certificates, host-key pinning, and strict SSH
decisions remain in force. This channel carries fixed bootstrap commands; it
does not introduce a general guest-to-host command surface.

## Context

Tart attaches virtio serial hardware at boot through `--serial-path`; Ubuntu
24.04 ARM64 exposes it as `hvc0`. Guest networking cannot reach the host PTY.
The guest getty automatically logs in the explicit UID-1000 workstation
account, ordered after clone identity initialization. That account has
unrestricted passwordless sudo under ADR 016.

Task 0 found macOS PTY slave mode `020` insufficient for an unprivileged reader.
Setting exact slaves to `0600` corrected access. Its additional operator PTY
needed a persistent terminal holder to avoid operator-side EOF/EIO failures;
Screen provided that holder and retained interactive output. MVP has no
operator PTY, terminal holder, retained transcript, or attach/detach feature.
A single continuously reading supervisor-owned master serves bootstrap and
then draining.

## Amended decision

The supervisor subsystem owns the exact persisted generation namespace and
holds an ordinary nonblocking generation lock for its lifetime. `serialx`
exclusively creates a new mode-`0700` `<generation>/serial/` subtree and one
PTY pair. It rejects pre-existing serial state rather than adopting it.
The slave is mode `0600`; Tart receives only that exact slave through
`--serial-path`. The serial runtime retains the master and a single read pump.

Bootstrap owns the stream exclusively. It sends exactly the fixed command
`/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap`
and one canonical bounded JSON request line. The guest serial decoder consumes
one line without waiting for PTY EOF. The host accepts exactly one bounded
response with exact nonce, generation, domain, session, backend, CA fingerprint,
and derived-principal correlation. Malformed, oversized, mismatched, ambiguous,
or missing frames fail bootstrap. Successful bootstrap permanently transitions
the pump to continuous bounded discard/drain, preventing serial output from
blocking Tart. There is no second bootstrap or generic command interface.

The exact response supplies the clone's fresh Ed25519 SSH host key. Only the
host pins it, with no network TOFU. The guest receives only the domain CA public
key and durable domain/session/backend/principal binding. Nonce and generation
are exchange correlation and never durable guest identity. The CA private key
remains on the host. Guest trust publication validates a complete tree and
atomically publishes it only when `/etc/ssh/boxwarden/active` is absent.
An exact existing tree is idempotent success; any conflict fails closed and is
never overwritten. Effective sshd settings are verified before success.

The lightweight detached supervisor retains the actual Tart process handle
in memory and follows one stop/wait/reap path. It owns the private bounded typed
Unix control socket with exact domain/session/backend/generation binding.
The minimal persisted launch request contains only binding and non-secret
configuration/session-record locators; it is an expected record, not authority.
Later composition must reload authoritative configuration and durable state
before constructing live runtime capabilities. Retry admits only exact,
structurally valid namespaces and rejects malformed, foreign, symlinked,
unexpected, or ambiguous state.

## Consequences

Removing same-UID host fortification does not weaken the hostile-guest boundary:
fixed commands, bounded exact framing, correlation, host-only CA private key,
atomic absent-or-exact trust publication, no-TOFU pinning, strict SSH, short
certificates, and fixed Tart/Softnet containment remain mandatory. No host
filesystem, clipboard, audio, agent, credential, display, or runtime-socket
integration is added.

A process able to open this private host PTY can control the disposable guest,
just as the trusted operator can control Tart. Ordinary safe path/type/no-follow
checks and owner-only modes remain. The threat model does not defend against
arbitrary malicious code already running as the trusted operator UID.

READY will require fresh exact-generation live backend and serial health plus
pin, current certificate, strict management probe, and time-zone agreement.
Screen or an operator console is never a readiness predicate. Slice A does not
compose or advertise an operational start/READY path; unavailable composition
fails explicitly.
