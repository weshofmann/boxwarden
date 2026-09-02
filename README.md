# Boxwarden

> Don’t sandbox the agent’s commands. Sandbox its computer.

Boxwarden is a host-neutral control plane for disposable workstations used by
autonomous AI agents. Its security model places the agent in a full graphical
virtual machine, rather than trying to contain each command individually on the
trusted host.

## Status

Boxwarden is in early development and is **not ready for general use**.

Task 0 qualified the M1A platform **PASS WITH CONDITIONS**: an Apple Silicon
macOS 26.6.2 host, Tart 2.32.1, Softnet 0.19.0, and an Ubuntu 24.04 ARM64
desktop guest. The qualification demonstrates unattended desktop installation,
graphical and serial access, guest-root containment, clone identity reset, and
the tested network policy. It does not claim all environments or operational
mechanisms are production-ready; see
[the Task 0 summary](docs/evidence/m1a-task0-final-summary.md).

V1 read-only status is complete. The currently implemented V2 surface lets one
domain explicitly admit an exact existing, stopped Tart object as its selected
generic golden, creates a stopped disposable clone with a new UUID-derived
backend identity and randomized MAC, and reports persisted session intent
beside observed backend state. The artifact itself is not domain-specific: two
domains may independently admit the same exact artifact without placing either
domain's trust material in it. Registration is an operator admission record,
not a Boxwarden claim that it proved provenance, clone-readiness, or the
external qualification evidence. Creation is intent-first and reconciles
partial retries under domain/session locks. V2's attended real-host
golden/clone gate remains pending and requires an artifact built or rebuilt
from the corrected generic guest definition and qualified accordingly. The
unchanged older Task 0 domain-bound artifact is not grandfathered merely because
it previously passed qualification.

The next executable slices are V3 trusted host/domain management foundation and
V4 start/supervision plus serial trust bootstrap and readiness. V3 separates a
host-global foundation (`boxwarden init`, read-only `boxwarden doctor`, and the
qualified Softnet privilege binding) from a domain foundation
(`boxwarden --domain D domain init`, one management CA per domain, certificate
issuance, host-key pins, and strict SSH primitives).
This plan stops after V4. File transfer, stop, destroy, provider authentication,
and other later work remain deferred.

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
as one security-critical toolchain; changing either requires deliberate
requalification.

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
- The production Softnet mechanism is selected but not yet implemented or
  qualified on a real host: `boxwarden init` will install the exact qualified
  Softnet 0.19.0 executable in a root-owned digest-specific path for a dedicated
  trusted operator group. Until V3 and its user-attended gate are complete, the
  project is not production-ready.

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
require a domain. Start from
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
