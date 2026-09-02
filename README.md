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

The currently implemented V1 surface is deliberately narrow: read-only session
status. It observes a named Tart session and reports persisted intent beside
observed state. It does not create, start, stop, clone, delete, authenticate,
or inject credentials into a workstation.

The broader v0.1 lifecycle and authentication work remains in progress. Its
planned authentication targets are AWS, GCP, GitHub, Bitbucket, Jira /
Atlassian, and Claude Teams.

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
- Task 0 did not select the production mechanism that authorizes privileged
  Softnet execution. The project is therefore not production-ready.

Read [the security model](docs/security-model.md),
[architecture](docs/architecture.md), and the [Task 0 evidence
summary](docs/evidence/m1a-task0-final-summary.md) before evaluating the
project for sensitive work.

## Current command

```sh
boxwarden --domain <id> session status <session>
```

`--domain` is mandatory unless `BOXWARDEN_DOMAIN` is set. Boxwarden has no
implicit domain and never searches across security domains. Start from
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
