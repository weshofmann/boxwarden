# Boxwarden v0.2 alpha cumulative review ledger

Snapshot frozen for review: base and merge base
`e16f23962b43337f02de1fbe2779df00a28f3f1a`; published head
`8edbd66526a40c56ef3796bbd8c7019643ea37d9`. Reviewers read that
commit, not the moving working tree. Later commits require delta review before
any claim that the newer head has been reviewed. This is a source review;
real-host acceptance remains separately recorded in [alpha progress](alpha-progress.md).

## One-time diff inventory

`git diff --numstat --no-renames` from the frozen base to head, with test paths
classified before implementation paths and binary rows separated:

| Category | Files | Added lines | Removed lines |
| --- | ---: | ---: | ---: |
| Production host code | 147 | 21,972 | 231 |
| Guest/tooling code | 36 | 4,314 | 28 |
| Tests and fixtures | 140 | 20,060 | 102 |
| Documentation and examples | 20 | 2,217 | 14 |
| Generated/binary artifacts | 1 | binary | binary |
| **Total** | **344** | **48,563** | **375** |

The binary row is the digest-locked guest bootstrap artifact. The category
script grouped `docs/` and `examples/` as documentation, `*_test.*`, `tests/`,
and `testdata/` as tests, `guest/`, `tools/`, and `scripts/` as guest/tooling,
and the remaining text paths as production host code. These are review-size
counts, not a quality metric.

## Review scopes and findings

The review assignments requested GPT-6 Astra at extra-high effort. Runtime
model/effort metadata was not exposed to the reviewers, so this ledger records
that routing request rather than claiming independent attestation.

| ID | Scope and reviewer | Severity and concrete failure | Disposition |
| --- | --- | --- | --- |
| H1 | Swift helper and host boundary; `/root/swift_host_boundary_review` | **Important.** At the snapshot, `tools/alpha-inspector/boot.swift` and `tools/alpha-formatter/boot.swift` accessed `VZVirtualMachine` delegate, state, and devices outside their custom serial queues, contrary to [Apple's queue contract](https://developer.apple.com/documentation/Virtualization/VZVirtualMachine/queue). | **Fixed in `a7de9c7`.** Fresh review found no remaining Important/Critical defect in this correction or the bounded receiver scope. Local queue regression detected 23 baseline accesses and passes corrected source; Swift builds and synthetic preflight passed. [Expanded hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36158247998) passed; fresh helper VM trials remain. |
| S1 | Workspace/import boundary; `/root/storage_snapshot_review` | **Important.** `internal/sshx/import_sftp.go:106-128,184-185` downloads guest-served files into trusted staging before checking the 4 MiB/file and 16 MiB total manifest limits. A hostile guest can grow host state without a transfer-time byte cap. | **Fixed in `c0ba7d8`.** Host-streamed pinned SFTP v3 readback now caps packets and declared per-file/total bytes before host writes; independent snapshot admission remains. `/root/import_readback_review` found no production Important issue; its packet-limit fixture correction was included before publication. Focused tests, local OpenSSH server integration, race check, full Go suite, and [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36159944944) passed. Real guest qualification remains. |
| S2 | Rebuild recovery; `/root/storage_snapshot_review` | **Important.** A public stop after a `Ready` or `Retiring` rebuild journal leaves a stopped candidate that normal start and rebuild retry reject (`internal/session/rebuild.go:447-451,587-606`, `start.go:126-134`). Retained workspace data becomes inaccessible through the supported path. | **Fixed in `227fa7b`.** Exact journal candidate restart now requires the established host-key pin and recipe binding; only Retiring admits an already absent old backend. `/root/rebuild_recovery_review` found no remaining Important/Critical issue. Affected tests, full local Go suite, vet, and [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36159944944) passed. Real stop/retry remains. |
| R1 | Recipe admission; `/root/recipe_snapshot_review` | **Moderate.** `internal/recipe/recipe.go:137-150,233-244` admitted relative/noncanonical session action executables and oversized argv that `internal/guestproto/action.go:60-72,101-107` later rejects, after preparation and VM start. | **Fixed in `518a4be`.** The regression failed on five cases before the fix, then passed. Targeted recipe/app/basebuild/architecture tests and recipe vet passed; hosted deterministic CI passed. `prepare` keeps PATH semantics. |
| R2 | Action recovery; `/root/recipe_snapshot_review` | **Moderate.** Crash-left `.<attempt>.json.tmp-<nonce>` siblings make both action replay scanning and public listing reject an otherwise valid published attempt (`internal/session/action_attempt.go:165-185,226-228,358-383`; `action_list.go:79-87`). | **Fixed in `04a587a`.** Scans retain and ignore only an exact private temporary with a published regular target; the validated published attempt alone controls recovery. Orphan, malformed, and unsafe entries fail closed. `/root/action_journal_review` found no actionable defect; focused tests, full local Go suite, and [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36159944944) passed. |
| R3 | CI coverage; `/root/recipe_snapshot_review` | **Moderate.** Frozen `.github/workflows/ci.yml:27-42,62-76` did not execute recipe preparation Python tests, Swift compilation, alpha tooling Python tests, or Linux ARM64 formatter/inspector entrypoint builds. | **Addressed in `a7de9c7`.** Exact new commands passed locally and in [hosted CI](https://github.com/weshofmann/boxwarden/actions/runs/36158247998). Existing bootstrap artifact test already cross-builds and verifies its bytes. |

## Coverage and next review

The snapshot reviews covered helper VM queue/teardown and bounded export
receiver; workspace authority, lifecycle/rebuild, import/export and partial
failure; recipe identity/action semantics, shortest public path, and CI
coverage. They did not constitute line-by-line approval of all 344 files or
real-VM behavior. Storage durability remains an empirical gate: two clean
cached-disk trials do not prove the earlier ext4 identity-loss root cause or
long-run reliability. Fresh bounded repeated content, filesystem-identity,
and clean-state checks are planned, preserving failed historical volumes.

The three correction deltas have received independent source review. Final
cumulative head review and real-host acceptance remain before the PR can be
marked Ready. No review finding authorizes weakening host guards or using
affected old helper binaries for acceptance.
