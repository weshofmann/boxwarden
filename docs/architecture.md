# Architecture

Boxwarden defines a general framework for safe, routinely disposable AI-agent workstations. Milestone 1A has one concrete realization: macOS is the trusted control host, Tart is the VM backend and security boundary, and Ubuntu 24.04 ARM64 is the guest. A later Linux-host backend is an intended evolution, but M1A implements no KVM, libvirt, or other Linux-host support.

    macOS trusted host
          |
          | Tart boundary
          v
    Disposable Ubuntu ARM64 workstation
          +-- ChatGPT Desktop and Codex
          +-- Claude Desktop and Claude Code
          +-- Antigravity desktop, IDE, and CLI
          +-- Grok Build
          +-- optional session-local Kindex development tool
          +-- browser and development tools
          +-- Markdown project memory
          +-- guest applications and workloads
              +-- native processes, language runtimes, or optional guest-local runtimes

The host interacts with GUI applications through Tart display/input, not X11 forwarding. Ordinary administration uses pinned-host-key, short-lived user-certificate SSH without GUI, agent, tunnel, or TCP forwarding. A normal VM uses a Task-0-qualified Tart + Softnet shared/NAT launch policy with clipboard and audio disabled. That policy preserves public Internet, required host-to-guest SSH, host/VPN-provided DNS, work-VPN and scoped/split-DNS behavior, default private/link-local denial, and concurrent-session isolation in the environments Task 0 actually tested. ADR 020 keeps effectively IPv6-only upstream and destination behavior explicitly unqualified rather than blocking implementation or implying support. M1A deliberately retains Softnet's default vmnet-gateway allowance because the gateway is also required network infrastructure and current Softnet cannot isolate gateway services by port. This is a documented host attack surface, not guest-to-host isolation. The VM receives no filesystem share, extra disk, Rosetta share, VNC server, bridged/host network, exposed Tart port, nested virtualization, host Docker, or host service integration beyond the accepted gateway reachability. V4 implements only this default policy and rejects every allow flag. Future ADR 015 private-CIDR support must first add exact persisted and reported session-record/CLI semantics; broad allow-all, implicit LAN access, and any exception that weakens session isolation remain prohibited.

The agent owns its disposable workstation, including unrestricted non-interactive
root access. The explicit UID-1000 workstation account automatically enters the
guest desktop, has full passwordless `sudo`, and is not interrupted by automatic
screen blanking, locking, or suspend. Guest privilege restrictions are not a
security boundary: every backend must contain a malicious guest administrator.
M1A installs Canonical's full `ubuntu-desktop` source so document and
productivity formats can be viewed and verified without per-session downloads or
an independently curated desktop composition.
The guest also uses the trusted host's current IANA time zone. The host detects
and validates the zone; whenever a transition actually boots or resumes a
guest, the common lifecycle applies it through the bounded guest-management
path and verifies the effective zone before reporting readiness. V2 create
leaves a stopped clone and performs no guest convergence. This remains common
workstation policy rather than a Tart backend operation. It controls local
wall-clock presentation and daylight-saving rules; guest clock synchronization
continues independently through the virtual clock and normal
time-synchronization service.
Host-issued management SSH disables password and keyboard-interactive login,
direct root login, agent forwarding, X11 forwarding, stream-local forwarding,
TCP forwarding, tunnels, and local commands; successful management login as
the workstation account may elevate inside the guest without another secret.
Every M1A VM also starts with Tart's host-local serial hardware attached. Its
`hvc0` getty automatically logs in the same workstation account, providing a
recovery shell when guest networking or SSH is broken. Production V4 retains
ADR 017's two-PTY topology but replaces opaque socat forwarding with a bounded
supervisor-owned broker, so the implementation must requalify ADR 017. The
supervisor owns both PTY pairs and masters; Tart opens only the Tart slave.
Exact `/usr/bin/screen -D -m` (qualified system Screen 4.00.03) is a direct
waitable child and sole reader of the operator slave. The broker alone reads
the Tart master, queues output to Screen, and arms a fixed-memory raw frame
parser only during automation. It forwards operator-master input only in
console mode; every other mode, including `idle` and `automation`, discards and
counts it without buffering or replay. Automation never opens
the operator PTY or uses Screen log, hardcopy, paste, `stuff`, or control paths.
One serialized broker state machine owns `idle`, `console`, `automation`, and
`failed`; fixed bounds and deadlines make flood, overflow, Screen/broker loss,
or ambiguous framing poison the generation with no hot repair.

The host-side `boxwarden` program is a small Go control plane split at one narrow backend seam.

The common control plane owns security-domain scoping, session identity and registry, intended lifecycle state, supervisor/readiness reconciliation, locks, generic-golden admission and selection, management CA/certificate/host-key-pin policy, strict SSH policy, host-time-zone synchronization, profile/encryption policy, project durability, quarantine semantics, credential/provider policy, validation, and destructive-operation safeguards. These rules must not import Tart concepts.

The Tart backend owns only Tart mechanics: create or clone a VM, configure CPU/memory/disk, randomize the MAC, start/stop/delete, inspect actual state and address, and construct the restricted Tart launch invocation. It reports observations to the common reconciler rather than deciding policy. M1A keeps this interface deliberately limited to operations the control plane actually needs; it is not a generic hypervisor framework. Checkpoint creation/resume is deferred beyond M1A.

The control plane resolves a domain's admitted golden through trusted-host
metadata to an exact immutable backend object. The metadata is domain-scoped,
but the artifact is generic: the same stopped Tart object may be independently
admitted and selected by more than one domain. Registration records the exact
existing/stopped backend identity and the operator's explicit admission. It does
not claim provenance, clone-readiness, or qualification evidence that the
record does not contain. The control plane records lifecycle intent before
backend mutation and reconciles that record with actual state after crashes or
manual interference.

Every disposable clone receives unique machine identity, including its MAC
address, `/etc/machine-id`, SSH host keys, and DHCP/DUID identity. The promoted
golden contains no reusable clone identity, security-domain identity, domain CA
anchor, fixed domain principal, provider/browser/session login, private
authentication material, repository, profile, secret, or checkpoint state. It
does contain generic strict-sshd configuration and fixed bootstrap target
locations. Each domain has exactly one explicitly initialized, host-only SSH
user CA. On first start, ADR 017's trusted serial channel atomically and
idempotently installs only a durable binding containing domain, session UUID,
backend kind/object, CA fingerprint, and exact derived principal. Start
generation and exchange nonce remain host runtime/framing correlation echoed in
the current response; they are never installed in
`/etc/ssh/boxwarden/active`. Later generations verify the same durable binding
and current host key. The channel verifies effective sshd configuration and
obtains the clone's fresh SSH host key for an exact host-side pin. Only after
that sequence may Boxwarden issue a short-lived no-extension certificate and
attempt strict SSH. No TOFU or network bootstrap path exists.

Backend state and workstation readiness are separate. `tart list` may prove
that the VM process is running while Boxwarden still reports `starting`,
`drift`, or non-ready. A long-lived same-user supervisor holds the generation
lock and authenticated owner-only control socket for its lifetime and keeps
Tart, broker, and Screen as direct/owned children. It never `exec`-replaces
itself with Tart or uses the initiating CLI's cancellation. Later CLIs reconnect
by a nonce challenge/response tied to manifest and process-start evidence. The
supervisor owns the generation client key/certificate, revalidates CA metadata
before fixed-threshold renewal, and refreshes a strict read-only SSH probe on a
fixed cadence. READY requires an authenticated bounded health snapshot within
the maximum evidence age, healthy broker/Screen state, exact pin, current
certificate, recent probe, and guest-zone agreement. Status observes backend
and host zone and challenges the supervisor only; it never mints, applies, or
repairs. A host-zone mismatch is non-ready until idempotent start on the exactly
proven generation reconverges it. Unproven running ownership is drift/non-ready
with no mutation or adoption.

`boxwarden --domain <domain> init` performs the explicit one-time host/domain
bootstrap. It initializes that domain's sole management CA and installs the
exact qualified Softnet executable into a root-owned digest-specific
`/Library/Boxwarden` path with narrowly scoped execute authority for a dedicated
trusted operator group. The manifest binds the exact single operator UID/name/
home and group ID/name/membership. A setuid/setgid source is rejected; any
privileged mutable Homebrew Softnet is unsafe and blocks init/start until
attended manual remediation. Normal start uses the absolute qualified Tart,
canonical configured `tart_home`, generation-private `TMPDIR`, and PATH exactly
equal to the Softnet digest directory, without sudo or ambient proxy,
telemetry, runtime, or loader variables. `boxwarden --domain <domain> doctor` is a fail-closed,
read-only diagnostic for dependency, CA, path, ownership, ACL, link, digest,
mode, group/effective membership, operator identity, manifest, macOS, and
toolchain drift. Repairs require an explicit operator-attended action.

M1A profile persistence is deliberately narrow: only explicitly implemented declarative adapters with fixed paths, schemas, limits, semantic review, staged restore, validation, and rollback are supported. Arbitrary archives, browser profiles, opaque application state, and Kindex state are not profile inputs. Application and provider login state remains disposable session state.

Canonical and durable project memory is Markdown. Git versions non-sensitive reviewed memory; age protects sensitive persistent Markdown. Session notes and candidate lessons remain disposable until a human reviews and promotes them. Search, vector, SQLite, or other indexes are derived caches and must be fully rebuildable from Markdown.

Every session belongs to an explicit SECURITY DOMAIN, initially a locally configured name such as `personal` or `work`. Domain-scoped namespaces include generic-golden admission and selection metadata, management CAs and host-key pins, profiles, age recipients and identity references, provider/Git credentials and identities, memory, projects, session registry, and runtime paths. Domain scoping of an artifact record does not make the referenced artifact domain-specific. The control plane never searches another domain as a fallback. This is a local separation primitive, not an enterprise tenancy system; a user with access to the trusted host can still access every locally configured domain.

The architecture distinguishes two build products:

- **GUEST DEFINITION** is portable declarative input: Ubuntu autoinstall, tool manifests and locks, guest provisioning, SSH/firewall and qualified guest-runtime policy, clone-finalize and identity initialization, profile adapters, memory conventions, and guest acceptance tests.
- **HOST-SPECIFIC GOLDEN ARTIFACT** is a generic backend-produced immutable image built from a guest definition and qualified inputs. In M1A it is a revisioned Tart VM plus separately retained evidence and BOM. Trusted-host domains independently admit and select exact artifacts; no domain trust material is part of the artifact.

The separation leaves room for a future Linux backend or image-construction path, possibly including bootc after a separate design, without adding either to M1A. The control plane does not make application workloads depend on Tart or require a particular guest runtime; OCI is one optional portability format. Platform identifiers include host/backend, guest OS, architecture, and libc where relevant so incompatible artifacts cannot collide. A later exact `session cp` command may transfer explicitly named files over V3/V4 management SSH; it does not authorize host filesystem sharing. Live host-tree attachment remains outside V0.1 and ADR 021 remains proposed.

See state-model.md, memory-model.md, security-model.md, lifecycle-and-recovery.md, and decisions/.
