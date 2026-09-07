# Supervisor Generation Ownership Design

**Status:** Approved implementation refinement for MVP lifecycle Task 5.

**Scope:** This design resolves the ownership cycle between the detached
supervisor introduced by Task 4 and the `serialx` generation invariant
introduced by Task 3. It refines implementation ownership within the accepted
lifecycle architecture; it does not change a trust boundary or require a new
ADR.

## Decision

The supervisor subsystem owns the outer runtime namespace for one exact
persisted start generation. `serialx` owns one fixed nested `serial/` subtree:

```text
<runtime>/<domain>/<session>/<generation>/   supervisor subsystem
├── supervisor-request.json
├── generation.lock
├── supervisor-manifest.json
├── supervisor.sock
├── client key and public key
├── client certificate
├── known_hosts
└── serial/                                  serialx
    ├── Tart and operator endpoint links
    └── PTY and Screen runtime state
```

The session service persists `starting` plus the exact generation before it
asks the supervisor subsystem to create or admit this namespace. It receives a
narrow exact-generation start/reconcile capability; it does not receive generic
filesystem or supervisor internals and does not write runtime contents.

On first launch, the supervisor subsystem prepares an owner-private staging
directory containing the exact immutable launch request and atomically publishes
that directory at the final generation path with no replacement. This makes the
first visible namespace identity-bound rather than leaving an unbound empty
directory at the create/request crash boundary. A retry may admit an existing
generation path only when its fixed request and current structure prove the
same domain, canonical session name, session UUID, backend kind/object, and
generation recorded durably as `starting` or `running`.

The fixed launch request is an expected-contract record, not authority. It may
contain only non-secret binding and admission expectations plus the canonical
host-config locator. The child obtains authority by reopening trusted state and
constructing live capabilities itself.

## Ownership Invariants

### Outer namespace

The supervisor subsystem exclusively creates, admits, locks, mutates, and
cleans the outer generation directory. Its implementation may be called by the
session service only after durable state records `starting` and the same exact
generation.

An existing outer namespace is reusable only when all of the following hold:

- every path component is canonical, owner-private, and non-symlinked;
- the immutable request exactly binds the configured domain, canonical session
  name, durable session UUID, backend kind/object, and persisted generation;
- the request's runtime path is the expected path derived from those values;
- every present entry belongs to the fixed supervisor layout and has the
  expected type and mode for its lifecycle phase;
- any live manifest/socket authenticates the exact same binding and process
  identity; and
- no unexplained file, directory, endpoint, partial credential, or second
  runtime owner is present.

An empty, request-less, foreign, malformed, symlinked, unexpectedly populated,
or unauthenticated live namespace is ambiguous and is never adopted. A retry
does not allocate a different generation while durable state still names the
original one.

### Serial subtree

The detached child calls `serialx.CreateRuntime` with the admitted outer
generation directory as the root and the fixed child name `serial`. `serialx`
must create that subtree itself with its existing no-adoption semantics. No
caller pre-creates `serial/`, supplies Screen process evidence, or writes its
endpoints.

`serialx` validates and cleans only `serial/`. Supervisor evidence records the
fixed nested endpoint paths. The supervisor subsystem retains outer ownership
while serial cleanup, backend termination, or any retained-child reaping
obligation is unresolved.

### Process and final cleanup

The detached child owns the live backend, broker, PTYs, Screen child, readers,
runtime credentials, and authenticated control service. It can stop and await
the runtime capabilities it holds, but a process cannot reap itself.

Final outer-artifact and directory removal belongs to the supervisor subsystem
as a whole and occurs only after:

1. the exact backend and serial runtime have conclusively terminated;
2. `serialx` has completed its own subtree cleanup;
3. every outer entry still matches the identity originally admitted by that
   supervisor operation.

On the normal detached path, the child may remove the outer artifacts after
those conditions and immediately before it exits. It does not claim to reap
itself; eventual process reaping is separate from namespace cleanup. During a
failed launch before successful detachment, the controller/parent retains the
exact child capability, stops and conclusively reaps that child, and only then
removes the request and outer namespace. If a later controller-owned path is
introduced to combine final reaping with outer removal, both actions remain
supervisor-subsystem responsibilities and their ordering must be explicit.

Persisted PIDs, process-start facts, manifests, and file identities are
comparison evidence. They never become a signal, wait, delete, or adoption
capability.

## Launch Contract and Authoritative Revalidation

The launch request includes the existing runtime binding plus the canonical
session-record name and the exact non-secret admission expectations needed to
detect drift. It contains no CA private key, client private key, control key,
pre-opened descriptor, process handle, or other ambient capability.

After claiming the exact generation lock and before creating `serial/` or
starting Tart, the detached child must:

1. reload the host configuration from the fixed canonical config path;
2. select the exact requested domain without cross-domain fallback;
3. load the durable session record by canonical name;
4. require durable state `starting` or `running` with the exact requested
   generation;
5. compare domain, session UUID, backend kind/object, and generation with the
   request;
6. rerun host runtime admission and require the result to equal the request's
   expected non-secret host facts;
7. rerun configured-domain CA admission, preserving its cross-configured-domain
   duplicate/partial-state validation, and require the selected CA identity to
   equal the request's expected public facts; and
8. only then construct the backend, serial, pin, client-key, certificate,
   strict-SSH, time-zone, and health-loop capabilities it owns.

The canonical session name is a record locator, while the session UUID remains
the durable identity. Matching only the runtime path or only request bytes is
insufficient.

## Narrow Interfaces

The session service receives a focused supervisor capability for exact-binding
start/reconciliation and authenticated inspection or stop needed by start
failure handling. It does not depend on a concrete controller with generic
runtime internals. The existing `supervisor.Launcher` and
`supervisor.Controller` may be composed behind that interface when doing so is
natural; no abstraction is required beyond the authority the service actually
needs.

SSH client-key creation is similarly typed. The supervisor asks `sshx` to
create exactly one generation-private Ed25519 management client key at an
admitted fixed path. The `ssh-keygen` path, argv, environment, output bounds,
file modes, atomic publication, and existing-file refusal/revalidation remain
inside `sshx`. The supervisor is not given a generic command runner.

## Crash and Retry State Machine

The ordered happy path is:

```text
persist starting + exact generation
  -> atomically publish or admit exact bound supervisor namespace/request
  -> spawn detached supervisor
  -> child claims exact generation lock
  -> child reloads and revalidates authoritative config, record, host, and CA
  -> child creates serial/ through serialx
  -> serial bootstrap and durable host-key pin
  -> create client key, issue certificate, resolve fresh address
  -> strict management probe and exact time-zone convergence
  -> publish fresh authenticated READY snapshot
  -> persist running + ready
```

Recovery uses the persisted generation, never a speculative replacement:

- `stopped` plus backend stopped and no namespace allocates one new generation,
  persists it, and starts it.
- `starting` plus a structurally valid exact namespace but no child resumes
  launch for that same generation.
- `starting` or `running` plus an authenticated exact live supervisor
  reconnects and reconverges that same generation.
- `starting` plus backend stopped and conclusively terminated exact owned
  runtime may clean that generation, persist `stopped` with no generation, and
  only then begin a later fresh start.
- Any running backend or populated runtime namespace whose exact ownership
  cannot be proven is drift/no mutation.

Cancellation or a crash after the `starting` fsync leaves the exact generation
for retry. The service clears it only after exact owned shutdown, conclusive
backend-stopped observation, serial cleanup, supervisor outer cleanup, and any
parent-retained child reap required by that path have all completed.

## Required Tests

Task 5 must add behavioral tests proving:

- durable `starting` plus generation precedes namespace or backend mutation;
- first publication is bound and rejects a pre-created empty or foreign path;
- exact-generation retry reuses only the same validated request/namespace;
- the child refuses stale request admission after config, record, host, or CA
  state changes;
- `serialx` creates `serial/` itself and never adopts it;
- serial endpoints are validated at their fixed nested paths;
- client-key creation is exact and exposes no generic execution surface;
- pin admission precedes certificate issuance and strict SSH;
- cancellation/failure retains the generation until owned termination and
  cleanup are proved; and
- failed pre-detach launch cleanup reaps the exact retained child before the
  parent/controller removes the outer namespace, while normal child cleanup
  never claims self-reaping.

No test or implementation step in this refinement launches a real Tart VM,
Softnet, Screen, or SSH process. Those remain controlled verification work only
after deterministic composition is green.
