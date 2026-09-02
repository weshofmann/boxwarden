# Contributing to Boxwarden

Boxwarden is early-stage and its security model is intentionally narrow. Small,
well-evidenced changes are easier to review than broad refactors.

## Before opening a pull request

- Read [README.md](README.md), [the architecture](docs/architecture.md), and
  [the security model](docs/security-model.md).
- Discuss changes that affect trust boundaries, credentials, persistence,
  networking, host integration, or provider data scope before implementing
  them. An ADR may be required.
- Keep secrets, private paths, customer data, generated VM state, and runtime
  state out of the repository.
- Keep changes focused and include tests when behavior changes.

## Local verification

Run the deterministic checks:

```sh
test -z "$(gofmt -l $(git ls-files -- '*.go'))"
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/boxwarden
```

Also run syntax checks for any shell scripts you change and `git diff --check`.
CI is deterministic only; it does not exercise trusted-host Tart, Softnet, GUI,
credential, provider, or destructive lifecycle qualification.

## Pull requests

Describe the problem, the security or operational effect, and the verification
you ran. Do not merge your own pull request, bypass required checks, or use a
trusted host as a CI runner.

## Branches and forks

Maintainers and managed agents publishing directly to the upstream repository
use the `<owner>/<type>/<topic>` branch convention in
[the development workflow](docs/development-workflow.md). The owner is the
accountable repository identity, not the editing tool.

External contributors normally work in their own forks and open a pull request
to upstream `main`. Use any clear, focused descriptive branch name in a fork;
the fork owner already supplies the contributor identity, so no username prefix
is required. Fork contributors do not need upstream branch-creation rights.
All contributions still require focused scope, green CI, and human maintainer
review.

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).
