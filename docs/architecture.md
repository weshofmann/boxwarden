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

The host interacts with GUI applications through Tart display/input, not X11 forwarding. Ordinary administration uses pinned-host-key, short-lived user-certificate SSH without GUI, agent, tunnel, or TCP forwarding. A normal VM uses a Task-0-qualified Tart + Softnet shared/NAT launch policy with clipboard and audio disabled. That policy preserves public Internet, required host-to-guest SSH, host/VPN-provided DNS, work-VPN and scoped/split-DNS behavior, default private/link-local denial, and concurrent-session isolation in the environments Task 0 actually tested. ADR 020 keeps effectively IPv6-only upstream and destination behavior explicitly unqualified rather than blocking implementation or implying support. M1A deliberately retains Softnet's default vmnet-gateway allowance because the gateway is also required network infrastructure and current Softnet cannot isolate gateway services by port. This is a documented host attack surface, not guest-to-host isolation. The VM receives no filesystem share, extra disk, Rosetta share, VNC server, bridged/host network, exposed Tart port, nested virtualization, host Docker, or host service integration beyond the accepted gateway reachability. Slice B's fixed Tart launch implements only this default policy and accepts no allow-flag input. Future ADR 015 private-CIDR support must first add exact persisted and reported session-record/CLI semantics; broad allow-all, implicit LAN access, and any exception that weakens session isolation remain prohibited.

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
guest, Slice D will apply it through the bounded guest-management path and
verify the effective zone before reporting readiness. V2 create
leaves a stopped clone and performs no guest convergence. This remains common
workstation policy rather than a Tart backend operation. It controls local
wall-clock presentation and daylight-saving rules; guest clock synchronization
continues independently through the virtual clock and normal
time-synchronization service.
Host-issued management SSH disables password and keyboard-interactive login,
direct root login, agent forwarding, X11 forwarding, stream-local forwarding,
TCP forwarding, tunnels, and local commands; successful management login as
the workstation account may elevate inside the guest without another secret.
MVP serial hardware uses ADR 017's amended single supervisor-owned PTY.
The guest `hvc0` getty logs in the workstation account so the fixed bootstrap
helper can run with passwordless sudo. `serialx` exclusively creates the private
`<generation>/serial/` subtree and one mode-`0600` PTY slave for Tart.
In Slice B the one master read pump starts immediately in bounded discard/drain
mode. Slice C will give that same exclusive pump one bounded, correlated
bootstrap exchange before its permanent drain. Operator-console UX is deferred.
The earlier two-PTY/Screen Task 0 harness remains historical evidence only.

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
user CA. Slice C will use ADR 017's trusted serial channel to atomically and
idempotently install only a durable binding containing domain, session UUID,
backend kind/object, CA fingerprint, and exact derived principal. Start
generation and exchange nonce will remain host runtime/framing correlation
echoed in the current response; they will never be installed in
`/etc/ssh/boxwarden/active`. Later generations will verify the same durable
binding and current host key. The channel will verify effective sshd
configuration and obtain the clone's fresh SSH host key for an exact host-side
pin. Only after that Slice D sequence may Boxwarden issue a short-lived
no-extension certificate and attempt strict SSH. No TOFU or network bootstrap
path exists.

Backend state and workstation readiness are separate. A running VM can remain
starting or non-ready. The trusted host and cooperating host processes use a
lightweight detached supervisor, ordinary generation lock, and private bounded
typed Unix socket bound to the exact domain/session/backend/generation.
The control listener serves one bounded request at a time. Each typed request
carries its client's absolute expiry as a liveness bound, never authority. The
server validates and caps that expiry by its own action deadline, rejects stale
queued frames, and passes the effective deadline into snapshot observation so
abandoned work cannot accumulate ahead of an exact stop.
The supervisor retains the actual Tart handle in memory with one stop/wait/reap
path; it never reconstructs process authority from persisted PID/inode data.
The minimal launch request holds only binding and configuration/record locators.
Slice B's detached child reloads those authoritative records and rechecks
current host and complete configured-domain CA admission before runtime
construction. It retains the exact Tart handle and one `serialx` runtime, proves
the exact backend running and drain healthy, and leaves the durable record at
`starting + generation G`.

After actual retained-handle reap, serial close, and exact listener-socket
removal, outer cleanup first validates the complete canonical generation and
atomically renames it to the deterministic same-parent `.<G>.cleanup` residue.
The generation lock moves with that rename and remains locked while cleanup is
active; its inode is moved to an exact sibling lock marker for the final
marker-owned empty-directory phase. Cleanup fsyncs the residue immediately after request
removal and before moving the lock to that marker. Directory fsyncs also durably
separate publication, marker, empty-directory, and completed phases; recovery
admits the exact request+marker crash image as well as the ordered stages. A
same-G retry validates and finishes only the exact residue before
republishing G. Canonical/residue coexistence, foreign bindings, malformed
state, an ownerless empty residue, unsafe modes or symlinks, unexpected
entries, and lock contention all fail closed.
Cleanup remains nonrecursive and the deterministic names are correlation, not
generic deletion authority.

Whether a stopped-backend retry initially finds the matching generation live
or its detached launcher has just reported a started snapshot, it reclassifies
that exact namespace between bounded snapshot attempts. A valid transition
from live ownership to cleanup, resumable publication, or absence returns to
the existing exact admission/launch path with the same request and G;
classification errors remain terminal. Detached-launch contention uses the
same live wait, so a finishing concurrent winner cannot hide that transition.

The mode-`0600` control socket and its inode authority remain in the canonical
exact generation. Realistic macOS state-root hierarchies can exceed Darwin's
AF_UNIX address capacity, so bind/connect may use a transient owner-private short
directory with one symlink to the already admitted generation. The alias is
removed immediately on ordinary success/error paths and is never a persisted
identity or cleanup authority; listener cleanup revalidates and removes only the
real canonical socket inode.

Slice B also binds public golden registration, session creation, and status to
the exact configured absolute Tart executable and `TART_HOME`. Start's parent
and child observations and the fixed child launch use the same admitted Tart
namespace; ambient PATH/default Tart state cannot select another object. The
fixed launch is `run --net-softnet --no-audio --no-clipboard --serial-path
<owned-endpoint> <exact-object>` under the admitted closed environment.

Slices A and B are deterministically implemented. The bounded Slice B
controlled-host check observed the exact configured Tart object running under a
retained detached supervisor after the public CLI exited, continuous serial
drain health, same-generation live retry, exact cleanup, and same-generation
relaunch. It is current product evidence, not formal ADR 017 or Softnet-runtime
qualification; see `docs/evidence/slice-b-controlled-exact-start.md`. Slice C
bootstrap and trust publication and Slice D host-key pin, client
key/certificate, management address, strict SSH, time-zone convergence, and
READY publication remain pending. The A–H plan in
`docs/superpowers/plans/2026-09-04-boxwarden-mvp-lifecycle.md` defines the
remaining slices and evidence gates.

The eventual supervisor owns generation SSH credentials, renews short-lived
no-extension certificates after CA revalidation, and refreshes strict read-only
SSH probes. READY requires a fresh bounded exact-generation health snapshot,
running backend, healthy serial drain, exact pin, current certificate, recent
probe, and host/guest-zone agreement. Status reads backend/host-zone and
supervisor observations only; it never mints, applies, or repairs. Ambiguous
ownership remains drift/non-ready with no adoption or mutation.

Host-wide prerequisites and domain-owned trust have separate lifetimes.
`boxwarden init` runs once per trusted host, outside the security-domain
namespace. It installs the exact qualified Softnet executable into a root-owned
digest-specific `/Library/Boxwarden` path with narrowly scoped execute authority
for a dedicated trusted operator group. The manifest binds the exact single
operator UID/name/home and group ID/name/membership. A setuid/setgid source is
rejected; any privileged mutable Homebrew Softnet is unsafe and blocks
init/start until attended manual remediation. Normal start uses the absolute
qualified Tart, canonical configured `tart_home`, generation-private `TMPDIR`,
and PATH exactly equal to the Softnet digest directory, without sudo or ambient
proxy, telemetry, runtime, or loader variables. `boxwarden doctor` is the
host-global, fail-closed, read-only diagnostic for dependency, path, ownership,
ACL, link, digest, mode, group/effective membership, operator identity,
manifest, macOS, and toolchain drift. It does not inspect domain CA health or
silently repair or rebind security-sensitive state. Both host-global command
implementations reject an explicitly supplied `--domain` rather than ignoring
it.

`boxwarden --domain <domain> domain init` runs once for each explicitly selected
domain and initializes only that domain's sole host-only management CA. It does
not install or modify the host-global Softnet privilege mechanism. Adding a
second domain therefore adds a second CA without repeating trusted-host
initialization, and session start never creates either host or domain
prerequisites lazily.

M1A profile persistence is deliberately narrow: only explicitly implemented declarative adapters with fixed paths, schemas, limits, semantic review, staged restore, validation, and rollback are supported. Arbitrary archives, browser profiles, opaque application state, and Kindex state are not profile inputs. Application and provider login state remains disposable session state.

Canonical and durable project memory is Markdown. Git versions non-sensitive reviewed memory; age protects sensitive persistent Markdown. Session notes and candidate lessons remain disposable until a human reviews and promotes them. Search, vector, SQLite, or other indexes are derived caches and must be fully rebuildable from Markdown.

Every session belongs to an explicit SECURITY DOMAIN, initially a locally configured name such as `personal` or `work`. Commands that operate on domain-owned state require an explicit domain. Domain-scoped namespaces include generic-golden admission and selection metadata, management CAs and host-key pins, profiles, age recipients and identity references, provider/Git credentials and identities, memory, projects, session registry, and runtime paths. Domain scoping of an artifact record does not make the referenced artifact domain-specific. The control plane never searches another domain as a fallback. Host-global prerequisites established by `boxwarden init` and diagnosed by `boxwarden doctor` are outside these namespaces and are not repeated per domain. This is a local separation primitive, not an enterprise tenancy system; a user with access to the trusted host can still access every locally configured domain.

The architecture distinguishes two build products:

- **GUEST DEFINITION** is portable declarative input: Ubuntu autoinstall, tool manifests and locks, guest provisioning, SSH/firewall and qualified guest-runtime policy, clone-finalize and identity initialization, profile adapters, memory conventions, and guest acceptance tests.
- **HOST-SPECIFIC GOLDEN ARTIFACT** is a generic backend-produced immutable image built from a guest definition and qualified inputs. In M1A it is a revisioned Tart VM plus separately retained evidence and BOM. Trusted-host domains independently admit and select exact artifacts; no domain trust material is part of the artifact.

The separation leaves room for a future Linux backend or image-construction path, possibly including bootc after a separate design, without adding either to M1A. The control plane does not make application workloads depend on Tart or require a particular guest runtime; OCI is one optional portability format. Platform identifiers include host/backend, guest OS, architecture, and libc where relevant so incompatible artifacts cannot collide. A later exact `session cp` command may transfer explicitly named files over V3/V4 management SSH; it does not authorize host filesystem sharing. Live host-tree attachment remains outside V0.1 and ADR 021 remains proposed.

See state-model.md, memory-model.md, security-model.md, lifecycle-and-recovery.md, and decisions/.
