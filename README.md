# Boxwarden

> Don't sandbox the agent's commands. Sandbox its computer.

Boxwarden is a host-neutral control plane for disposable workstations used by
autonomous AI agents. Its security model places the agent in a full graphical
virtual machine, rather than trying to contain each command individually on the
trusted host.

## Status

Boxwarden is in early development and is **not ready for general use**.

Task 0 qualified the M1A platform **PASS WITH CONDITIONS**: an Apple Silicon
macOS 26.6.2 host, Tart 2.32.1, Softnet 0.19.0, and an Ubuntu 24.04 ARM64
desktop guest. The qualification demonstrates unattended desktop installation,
graphical and serial access, clone identity reset, and the tested network
policy. It does not claim all environments or operational mechanisms are
production-ready; see [the Task 0 summary](docs/evidence/m1a-task0-final-summary.md).

V1 read-only status is complete. The current V2 surface — generic-golden
registration, stopped clone creation, and session status — lets one domain
explicitly admit an exact existing, stopped Tart object as its selected generic
golden, creates a stopped disposable clone with a new UUID-derived backend
identity and randomized MAC, and reports persisted session intent beside
observed backend state. The artifact itself is not domain-specific: two domains
may independently admit the same exact artifact without placing either domain's
trust material in it. Registration is an operator admission record, not a
Boxwarden claim that it proved provenance, clone-readiness, or the external
qualification evidence. Creation is intent-first and reconciles partial retries
under domain/session locks.

V2's attended real-host golden/clone gate remains **Pending**. It must use an
artifact built or rebuilt from the corrected generic guest definition and
qualified accordingly; the older Task 0 domain-bound artifact is not
grandfathered. This limits V2's assurance claim, but does not alone make the
deterministically verified implementation unsafe or bar its merge absent a
known unsafe defect. It does not change the separate conclusion that Boxwarden
is not ready for general use.

The V3 trusted host/domain-management foundation — `boxwarden init`,
`boxwarden doctor`, the Softnet privilege binding, per-domain management CAs,
certificate issuance, host-key pins, and strict SSH primitives — remains staged
Draft work and is not on `main`. Its complete hostile/runtime Softnet
qualification remains Pending. V3 therefore supports no claim of runtime
qualification or full malicious-guest containment. V4 start/supervision plus
serial trust bootstrap and readiness is also staged Draft work; no V4 start,
readiness, or operational-readiness claim is made. This plan stops after V4.
File transfer, stop, destroy, provider authentication, and other later work
remain deferred.

## Model

Boxwarden gives an agent a disposable Ubuntu desktop workstation: browser, GUI
applications, terminal, development tools, and unrestricted guest `sudo` all
live inside the VM. The VM is the security boundary between the autonomous
agent and the trusted host.

Boxwarden does not prescribe how work executes inside that guest. A workload
may use native processes, Docker, Podman, language runtimes, or no container
runtime. A guest-local runtime is never a substitute for VM isolation, and the
default policy never exposes a trusted-host Docker, Podman, containerd, or
equivalent runtime socket, context, credential store, or control channel.

For the currently qualified M1A backend, macOS is the trusted host, Tart is the
VM backend, and Ubuntu 24.04 ARM64 is the guest. Tart and Softnet are qualified
as one security-critical toolchain; a security-relevant change to either
requires a qualification-impact assessment, and a change to a component whose
qualification is explicitly pair-bound requires the corresponding
requalification.

## Security

**The thesis.** Boxwarden places the agent in a complete disposable virtual
machine. The VM is the security boundary — not the `PATH`, not the syscall
table, not a container namespace — a separate virtual machine whose hardware
boundary is enforced by Tart and the macOS hypervisor. The trusted host and
everything Boxwarden needs to protect stay outside it.

This is motivated by a specific threat: autonomous code that may execute hostile
repositories, package scripts, prompt-injected instructions, or malicious tool
output. An agent that can install packages and run arbitrary commands needs a
boundary that does not depend on cooperation from guest root. A VM boundary
does not depend on guest root cooperation.

**The trusted computing base** for the qualified M1A platform: the Apple Silicon
macOS host and kernel, Tart 2.32.1 (executable SHA-256
`05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d`), Softnet
0.19.0 (executable SHA-256
`ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e`), and the
Boxwarden control plane. Everything inside the VM is outside the TCB.

**What the isolation properties cover.** The Softnet network policy — directly
exercised in Task 0 — denies guest-initiated connections to private and
link-local destinations by default, denies session-to-session traffic, and
enforces source-MAC/IP anti-spoofing. No trusted-host filesystem tree, Docker
socket, SSH-agent, display server, clipboard, or audio is exposed to the guest
by default. These are properties exercised on the qualified platform; they are
not claims about arbitrary host configurations.

**What this does not cover:**
- Hostile native code already executing as the trusted macOS operator. The
  `04550` Softnet privilege mechanism is not a boundary against that threat; see
  ADR 024 (staged Draft branch) for the explicit scope statement.
- A guest using its permitted outbound internet access to exfiltrate data it was
  given.
- The vmnet gateway, which the guest can reach and which may host services on
  the trusted Mac. Boxwarden does not claim guest-to-host network isolation; see
  [ADR 015](docs/decisions/015-network-compatibility-before-host-gateway-isolation.md).
- Credentials, browser sessions, and data intentionally placed in a guest.

These are design decisions with explicit rationale, not implementation gaps.

Read [the security model](docs/security-model.md) and
[architecture](docs/architecture.md) for the full specification.

## Assurance

Boxwarden distinguishes what it claims from what evidence supports each claim.
Security properties are described with an explicit vocabulary rather than a
single "secure" or "tested" label. A claim may simultaneously have an accepted
design basis, deterministic test coverage, independent review, and real-host
qualification; these dimensions are orthogonal.

| Dimension | Meaning |
|---|---|
| **Design basis** | Required by an accepted ADR or canonical document |
| **Deterministically verified** | Covered by the automated test suite in CI |
| **Independently reviewed** | Examined in a named, dated, committed written assessment |
| **Real-host qualified** | Directly observed on a stated exact platform and toolchain |
| **Pending** | Designed but empirical evidence does not yet exist |
| **Not claimed** | Explicitly outside the current trust model |

The full claim inventory, assurance matrix, platform qualification matrix, and
evidence gaps are in [`docs/assurance.md`](docs/assurance.md).

**Selected entries — current status:**

| Property | Evidence basis | Notes |
|---|---|---|
| No host filesystem, Docker socket, SSH agent, display server, clipboard, or audio in guest | Design basis + deterministically verified | No host-tree capability; ADR 021 is PROPOSED, not accepted |
| Private/link-local network denial | Design basis + real-host qualified | Task 0 on macOS 26.6.2 / Tart 2.32.1 / Softnet 0.19.0 |
| Session-to-session network isolation | Design basis + real-host qualified | Task 0 two-clone TCP/22 test |
| **Vmnet gateway reachable from guest** | **Accepted limitation** | Required for VPN/DNS compatibility — see ADR 015 |
| SSH: all forwarding disabled, `StrictHostKeyChecking=yes`, no TOFU | Design basis + deterministically verified | Adversarial and construction tests; real-host exercise pending V4 |
| Softnet artifact bound to exact digest in root-owned path, mode `04550` | Design basis + real-host qualified | V3 attended gate |
| Unsafe Homebrew Softnet detected and blocks `boxwarden init` | Design basis + det. verified + real-host qualified | V3 attended gate |
| **Softnet runtime behavior** (privilege transition/drop, closed env, signal handling) | **Pending** | Partial attended runtime work produced non-final forensic evidence; complete fresh-run qualification not yet performed; no completed runtime result is claimed |
| IPv6-only upstream behavior | Not qualified | `NOT YET PROVEN` per ADR 020 |
| One CA per domain; no CA in generic golden | Design basis + det. verified + real-host qualified | V3 domain init gate |
| Provider authentication | Not claimed | Deferred beyond V1–V4 |

An [independent architecture review](docs/reviews/2026-08-30-independent-architecture-review.md)
was conducted on 2026-08-30 (reviewed commit `9ba73679`). Verdict: **IMPLEMENT
WITH CONDITIONS**. The architecture was found sound. All eight prior concerns
were addressed; the vmnet-gateway limitation was explicitly accepted in ADR 015.

## Qualification

"Qualified" in Boxwarden means a property was directly observed in an attended
procedure on a stated exact platform, and the evidence is committed to the
repository. It is not synonymous with "the tests pass."

**Qualified platform:** Apple Silicon macOS 26.6.2 build 25G83, Tart 2.32.1,
Softnet 0.19.0, Ubuntu 24.04.4 ARM64. A security-relevant change to a qualified
component requires a qualification-impact assessment. A change to a component
whose qualification is explicitly pair-bound (Tart and Softnet are qualified as
one pair) requires the corresponding requalification for the bound properties.

**Task 0 (PASS WITH CONDITIONS):** Core network isolation properties, clone
identity, management SSH path (via serial channel), and serial recovery proved
on the exact platform. Three IPv6-related network environments remain
`NOT YET PROVEN`. See [Task 0 summary](docs/evidence/m1a-task0-final-summary.md)
and [ADR 020](docs/decisions/020-separate-platform-and-environment-qualification.md).

**V3 attended gate (partial):** Installation of the exact Softnet artifact in a
root-owned digest-specific path, doctor read-only behavior, domain init
separation, unsafe Homebrew detection and init refusal, and legacy manifest
migration. Softnet runtime behavior awaits a separate gate whose procedure has
been approved. Attended evidence has been produced and is committed on the
staged V3 Draft branch; it is not yet on `main`.

**Pending gates:**
- Softnet runtime behavior (privilege transition/drop, closed-environment
  execution, signal handling) — partial attended runtime work produced
  non-final forensic evidence and exposed harness assumptions that are being
  corrected; the complete fresh-run runtime qualification remains pending;
  no completed runtime qualification result is claimed
- ADR 017 requalification for the V4 supervisor broker (replaces Task 0 socat
  harness)
- V2 real-host register/clone gate (requires artifact from corrected generic
  guest definition)

See [`docs/assurance.md`](docs/assurance.md) for the full qualification matrix
and evidence gaps.

## Important security limitations

- The guest administrator controls the whole guest. Credentials, browser
  sessions, and data intentionally placed in it are available to a compromised
  guest.
- The qualified Softnet policy denies private and link-local destinations by
  default, but the guest can reach services on the vmnet gateway. Boxwarden does
  not claim guest-to-host network isolation.
- Effectively IPv6-only upstream behavior and its dependent destination cases
  are not qualified support claims.
- Host filesystem sharing, display-server sharing, clipboard and audio sharing,
  host runtime sockets, host credential stores, SSH-agent forwarding, bridged
  networking, port exposure, and nested virtualization are absent by default.
- The V3 Softnet mechanism is staged Draft work, not current `main` behavior.
  Its host/domain-init attended evidence is also staged, while its hostile/runtime
  qualification (privilege transition/drop, closed-environment execution,
  signals, and related behavior) remains Pending and unclaimed. It does not
  establish V3 runtime qualification, V4 readiness, or product operational
  readiness.

Read [the security model](docs/security-model.md),
[architecture](docs/architecture.md), and the [Task 0 evidence
summary](docs/evidence/m1a-task0-final-summary.md) before evaluating the
project for sensitive work.

## Current commands

```sh
boxwarden --domain <id> golden register <qualified-tart-object>
boxwarden --domain <id> session create [--mode clean|quarantine] <session>
boxwarden --domain <id> session status <session>
```

These implemented commands operate on domain-owned state, so `--domain` is
mandatory unless `BOXWARDEN_DOMAIN` is set. Boxwarden has no implicit domain and
never searches across security domains. Future host-global `boxwarden init` and
`boxwarden doctor` operate outside the security-domain namespace and do not
require a domain.

**Staged Draft commands — not on `main`:**

```sh
boxwarden init
boxwarden doctor
boxwarden --domain <id> domain init
```

These commands belong to the staged V3 foundation. `boxwarden init` and
`boxwarden doctor` are host-global: they do not select a domain and reject an
explicitly supplied `--domain`. `domain init` creates only the selected domain's
management CA; it does not install or modify host-global prerequisites. V3 and
V4 remain behind their stated review and qualification gates. Start from
[config/boxwarden.example.json](config/boxwarden.example.json).

## Development

Run the deterministic local checks before opening a change:

```sh
test -z "$(gofmt -l $(git ls-files -- '*.go'))"
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/boxwarden
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution expectations and
[SECURITY.md](SECURITY.md) for vulnerability reporting.
