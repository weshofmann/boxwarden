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

The host interacts with GUI applications through Tart display/input, not X11 forwarding. Ordinary administration uses key-only SSH without GUI, agent, tunnel, or TCP forwarding. A normal VM uses a Task-0-qualified Tart + Softnet shared/NAT launch policy with clipboard and audio disabled. That policy preserves public Internet, required host-to-guest SSH, host/VPN-provided DNS, work-VPN and scoped/split-DNS behavior, default private/link-local denial, and concurrent-session isolation in the environments Task 0 actually tested. ADR 020 keeps effectively IPv6-only upstream and destination behavior explicitly unqualified rather than blocking implementation or implying support. M1A deliberately retains Softnet's default vmnet-gateway allowance because the gateway is also required network infrastructure and current Softnet cannot isolate gateway services by port. This is a documented host attack surface, not guest-to-host isolation. The VM receives no filesystem share, extra disk, Rosetta share, VNC server, bridged/host network, exposed Tart port, nested virtualization, host Docker, or host service integration beyond the accepted gateway reachability. Default private-network denial may be narrowed only by an explicit per-session allowlist of exact private CIDRs under ADR 015; broad allow-all, implicit LAN access, and any exception that weakens session isolation remain prohibited.

The agent owns its disposable workstation, including unrestricted non-interactive
root access. The explicit UID-1000 workstation account automatically enters the
guest desktop, has full passwordless `sudo`, and is not interrupted by automatic
screen blanking, locking, or suspend. Guest privilege restrictions are not a
security boundary: every backend must contain a malicious guest administrator.
M1A installs Canonical's full `ubuntu-desktop` source so document and
productivity formats can be viewed and verified without per-session downloads or
an independently curated desktop composition.
The guest also uses the trusted host's current IANA time zone. The host detects
and validates the zone; the common lifecycle applies it through the bounded
guest-management path and verifies the guest's effective zone before reporting
a create/start transition ready. This remains common workstation policy rather
than a Tart backend operation. It controls local wall-clock presentation and
daylight-saving rules; guest clock synchronization continues independently
through the virtual clock and normal time-synchronization service.
Host-issued management SSH still disables password login, direct root login,
agent forwarding, X11 forwarding, and tunnels; successful management login as
the workstation account may elevate inside the guest without another secret.
Every M1A VM also starts with Tart's host-local serial hardware attached. Its
`hvc0` getty automatically logs in the same workstation account, providing a
recovery shell when guest networking or SSH is broken. Before Tart starts, the
host creates a private persistent PTY relay, restricts both device endpoints to
the owner, passes one to Tart with `--serial-path`, and holds the other open in
a detached GNU Screen session. Operators attach to and detach from that retained
session instead of opening and closing the raw PTY. Screen drains and retains
terminal output while no client is viewing it, preventing an unowned PTY hangup
from terminating the relay. Tart exit cleans up Screen, the relay, its runtime
metadata, and the endpoint links. The console is not exposed over the network
or treated as a guest-to-host bridge.

The host-side `boxwarden` program is a small Go control plane split at one narrow backend seam.

The common control plane owns security-domain scoping, session identity and registry, intended lifecycle state, reconciliation rules, locks, golden-revision selection, host-time-zone synchronization, profile/encryption policy, project durability, quarantine semantics, credential/provider policy, validation, and destructive-operation safeguards. These rules must not import Tart concepts.

The Tart backend owns only Tart mechanics: create or clone a VM, configure CPU/memory/disk, randomize the MAC, start/stop/delete, inspect actual state and address, and construct the restricted Tart launch invocation. It reports observations to the common reconciler rather than deciding policy. M1A keeps this interface deliberately limited to operations the control plane actually needs; it is not a generic hypervisor framework. Checkpoint creation/resume is deferred beyond M1A.

The control plane resolves a domain's promoted golden through host metadata to an immutable revisioned Tart VM name. It records lifecycle intent before backend mutation and reconciles that record with actual state after crashes or manual interference.

Every disposable clone receives unique machine identity, including its MAC address, `/etc/machine-id`, SSH host keys, and DHCP/DUID identity. The promoted golden contains no reusable clone identity, provider/browser/session login, private authentication material, repository, profile, secret, or checkpoint state. Under accepted ADR 012, a domain's non-secret SSH user-CA public trust anchor is system access policy; the CA private key and issued session certificates remain host-only/session-scoped.

M1A profile persistence is deliberately narrow: only explicitly implemented declarative adapters with fixed paths, schemas, limits, semantic review, staged restore, validation, and rollback are supported. Arbitrary archives, browser profiles, opaque application state, and Kindex state are not profile inputs. Application and provider login state remains disposable session state.

Canonical and durable project memory is Markdown. Git versions non-sensitive reviewed memory; age protects sensitive persistent Markdown. Session notes and candidate lessons remain disposable until a human reviews and promotes them. Search, vector, SQLite, or other indexes are derived caches and must be fully rebuildable from Markdown.

Every session belongs to an explicit SECURITY DOMAIN, initially a locally configured name such as `personal` or `work`. Domain-scoped namespaces include golden selection and artifact registries, profiles, age recipients and identity references, provider/Git credentials and identities, memory, projects, session registry, and runtime paths. The control plane never searches another domain as a fallback. This is a local separation primitive, not an enterprise tenancy system; a user with access to the trusted host can still access every locally configured domain.

The architecture distinguishes two build products:

- **GUEST DEFINITION** is portable declarative input: Ubuntu autoinstall, tool manifests and locks, guest provisioning, SSH/firewall and qualified guest-runtime policy, clone-finalize and identity initialization, profile adapters, memory conventions, and guest acceptance tests.
- **HOST-SPECIFIC GOLDEN ARTIFACT** is a backend-produced immutable image built from a guest definition and qualified inputs. In M1A it is a revisioned Tart VM plus its recorded evidence and BOM.

The separation leaves room for a future Linux backend or image-construction path, possibly including bootc after a separate design, without adding either to M1A. The control plane does not make application workloads depend on Tart or require a particular guest runtime; OCI is one optional portability format. Platform identifiers include host/backend, guest OS, architecture, and libc where relevant so incompatible artifacts cannot collide.

See state-model.md, memory-model.md, security-model.md, lifecycle-and-recovery.md, and decisions/.
