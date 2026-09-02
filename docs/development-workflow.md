# Development workflow

This document is Boxwarden's canonical Git/GitHub workflow policy. It is the
authoritative source for upstream branch naming and ownership semantics.
`AGENTS.md` carries concise operational invariants, `CLAUDE.md` is subordinate
guidance, and `CONTRIBUTING.md` explains the policy to contributors, especially
for fork-based work.

## Upstream repository work

When a branch is published directly to the upstream Boxwarden repository, use:

```
<owner>/<type>/<topic>
```

`<owner>` is the accountable repository or GitHub identity responsible for the
integration unit; it is not the AI product or model performing an edit.
`<type>` is a short descriptive change class, and `<topic>` names the focused
integration unit. Useful types include `feat`, `fix`, `docs`, `refactor`,
`test`, `research`, `spike`, and `chore`; this vocabulary is guidance rather
than a closed enum.

Examples include `weshofmann/feat/session-create`,
`weshofmann/fix/tart-ip-refresh`, and `weshofmann/docs/public-readiness`.
One focused integration unit gets one branch and one pull request. The branch
name communicates accountable ownership and intent at a glance.

Agents are workers acting under an accountable repository identity. The AI
product or model used to perform an edit is not itself the repository owner.
For example, one `weshofmann/feat/session-create` branch may be edited by
multiple AI tools and a human without changing its identity. If a dedicated bot
account is introduced in the future, that account may own its own upstream
branches; this policy does not create or require one.

Do not use a namespace derived from the AI product or model as upstream
repository policy. Agents never work directly on `main` or push directly to it.

## Fork-based contributions

External contributors normally work on focused branches in their own forks and
open pull requests to upstream `main`. GitHub already identifies the fork
owner, so a fork-local branch only needs a clear, focused descriptive name;
examples include `fix/docs-link`, `feat/linux-backend`, and
`docs/security-model`. Do not require an upstream-style owner prefix inside a
contributor's own fork, and do not require contributors to create upstream
branches.

External contributors receive no direct-main, bypass, or merge authority.
Their pull requests follow the same focused-scope, required-CI, and human
maintainer-review requirements as upstream work.

## Normal development loop

Each increment follows this sequence:

1. use TDD where code behavior changes, and keep the scope focused;
2. inspect the relevant state, then run verification appropriate to the
   increment;
3. make a detailed commit explaining what changed, why, and relevant
   security/architecture implications and verification;
4. push the verified commit immediately — `git push -u origin <branch>` for
   first publication, then `git push` for subsequent verified commits;
5. create a Draft PR after the first meaningful verified push and keep that
   PR's body and CI evidence current;
6. before Ready, review the complete accumulated diff against the PR's actual
   base, update verification evidence, and wait for green CI;
7. a human maintainer reviews and merges. Agents stop before merging.

The PR template records the goal, rationale, security/architecture impact,
verification, limitations, and agent self-review. Agents may create and
update Draft PRs for their own branches, update their bodies, respond to
reviews, fix CI, and mark their completed PRs Ready. They may not merge,
enable auto-merge, direct-push `main`, bypass checks, force-update `main`, or
close a human-created PR.

## Commits and published history

Production commits are coherent reviewed increments, not progress snapshots.
Their messages explain the meaningful scope, observable behavior, and why
non-trivial choices were made. Avoid opaque subjects such as `fix stuff`,
`updates`, `misc`, or `wip`.

Before a branch is first pushed, limited local cleanup is allowed only when it
preserves every original commit under an explicit safety ref. After the first
push, agents do not amend, interactive-rebase, reset, force-push, or otherwise
rewrite that branch without explicit human approval. Make a new corrective
commit, verify it, and push it instead.

## Pull-request self-review and merge

Before marking a PR Ready, inspect the cumulative change, not just the newest
commit. Fetch the actual base and run at least:

```sh
git fetch origin
git diff --check
git diff <actual-base>...HEAD
```

Review for unrelated files, accidental scope expansion, credentials, secrets,
private host details, generated artifacts, stale evidence, debug behavior,
unintended destructive behavior, and architecture or security-boundary changes
that lack their required ADR/documentation. Record the result in the PR body.

Human merge authority is deliberate: no agent merge, auto-merge, or bypass is
allowed. Independent production slices default to squash merge. Do not delete a
parent branch while a dependent Draft PR still needs it; first retarget the
child to its final base, review the updated diff and green `verify` check, and
then let a human decide when the retargeted branches are deleted.

## GitHub Actions CI

The deterministic GitHub-hosted workflow is `CI`. Its GitHub Actions check-run
context is `verify`, which GitHub's UI displays as **`CI / verify`**. The
ruleset must require the context rather than the UI label:

```yaml
workflow: CI
required check context: verify
UI display: CI / verify
```

It runs on all pull requests and pushes to `main`, with `contents: read`
permissions, a 15-minute timeout, PR/ref-scoped concurrency cancellation, and
no repository secrets. It pins official actions to immutable commits:

| Action | Release | Commit |
| --- | --- | --- |
| `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |

CI always runs syntax checks on tracked shell scripts and rejects tracked Go
source without `go.mod`. It runs setup-go with caching disabled, gofmt, test,
race test, vet, and build when `go.mod` exists.

GitHub CI is not a substitute for qualified real-host integration. It never
runs Tart, Softnet, macOS virtualization, graphical VM tests, real providers,
real credentials, or destructive Boxwarden lifecycle operations. The trusted
Mac mini must not become a GitHub self-hosted runner. Release/deployment
automation is also deliberately out of scope.

## Required main protection

Before a public repository accepts contributions, configure protection for
`refs/heads/main` that requires pull requests and the exact `verify` check
context. It must block force pushes and branch deletion, have no bypass actors
(including no administrator exemption), and keep auto-merge disabled. The
repository may use merge, squash, or rebase methods as appropriate; signed
commits and linear history are not required by this policy.

Every production PR must have a successful `verify` check that is fresh for its
current base. Human cumulative-diff review remains required; status freshness
complements it rather than replacing it.
