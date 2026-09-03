# V3 Host Manifest Readability Design

## Problem

V3 publishes the host-toolchain `manifest.json` as `root:wheel 0400`, while
host-global `boxwarden doctor` intentionally runs without a privilege runner
and must open, hash, strictly parse, and validate that manifest. A successful
first `boxwarden init` therefore leaves the installed tree healthy to root but
uninspectable to the trusted operator. Doctor reports `drifted/unsafe` even
though the tree is otherwise exact.

This is an implementation contradiction. ADR 024 requires a root-owned,
immutable manifest and an unprivileged read-only doctor; it does not require
the manifest bytes to be confidential.

## Decision

The installed host-toolchain manifest is a regular one-link file owned by
`root:wheel` with exact mode `0444` and no extended ACL. Every installed
ancestor remains a direct, non-symlink, root-owned directory that is not
writable by group or other. Publication still stages, validates, fsyncs, and
atomically renames the manifest last.

The manifest is explicitly non-secret host metadata. Its schema may contain
only:

- qualified platform release and build identity;
- exact Tart and Softnet paths, versions, and artifact digests;
- root UID and the dedicated operator-group ID, name, and membership;
- the trusted operator UID, account name, and home path;
- canonical `TART_HOME`, the installed Softnet mode, and installation time.

It must not contain a security-domain identity, CA material, credentials,
provider data, session state, private keys, tokens, or other secrets. Local
account and path values remain redacted from committed qualification evidence
even though they are not secrets in the on-host authorization record.

Readability is not an integrity mechanism. Integrity continues to depend on
root ownership, zero write bits, exact type/link/mode/owner/group/ACL checks,
strict bounded schema parsing, exact tool digests, caller/group/configuration
binding, and protected ancestry. `0440 root:boxwarden-operators` is rejected
because doctor must be usable before a new group is effective and while group
membership itself is under diagnosis. A privileged doctor helper is rejected
because it would violate doctor's read-only capability boundary.

## Existing `0400` Installation

The corrected binary treats exact `0400` as unexpected drift and does not
repair, adopt, rewrite, or chmod it during normal `init` or `doctor`.

The one known pre-qualification installation is migrated once, attended, and
by exact path:

1. Build the final-head `internal/hostx` test binary as the unprivileged
   operator, then run `BOXWARDEN_ATTENDED_EXACT_GROUP=1` and only
   `TestAttendedExactLocalOperatorGroupState`. Capture its redacted canonical
   pre-state evidence line. This read-only test invokes
   `inspectExactLocalOperatorGroup(..., false)` through the production doctor
   inspector, proving the exact local group and valid GID; exact caller
   RecordName/UID/GeneratedUID binding; exact named and GUID membership; no
   nested groups; caller presence in the exhaustive user inventory; and no
   other user or group sharing the target GID.
2. Run the exact old published `cf2212f` binary's host-global `init` against
   the original exact configuration and require `refresh-login-session: false`.
   It still validates the complete directory tree, absence of extra/current
   entries, manifest type, owner/group/mode/ACL/link count, strict schema-v2
   bytes, exact caller/group/Tart/configuration binding, installed Softnet
   metadata, and both tool digests under the original `0400` contract; it does
   not by itself prove that its group pre-state was non-mutating.
3. Rerun the same unprivileged attended group-state test and require its
   canonical evidence line to match the pre-state exactly. Stop before chmod
   on a mismatch, old-init failure, or `refresh-login-session: true`.
4. Capture the manifest SHA-256 and exact metadata while it is still `0400`.
5. Change only the exact manifest path from `0400` to `0444`. Do not chown,
   rewrite, replace, or rename it. Protected root-owned ancestry prevents an
   unprivileged path substitution between validation and chmod.
6. Synchronize filesystem metadata, verify the same inode/link/owner/group,
   unchanged bytes and digest, no ACL, exact `0444`, and successful read as a
   distinct unprivileged UID.
7. Use only the corrected binary afterward. Its unprivileged `doctor` and
   attended idempotent `init` must both validate the migrated tree.

Any failed validation stops the migration. Any legacy mode other than exact
`0400`, or any unexpected owner, group, link count, ACL, content, ancestry,
tool, group, or configuration state, requires manual investigation rather than
repair.

## Verification

Deterministic tests must prove publication and all consumers require exact
`0444`, legacy `0400` is rejected without mutation, and doctor regards only
the corrected mode as healthy. A root-only Unix integration test must create a
root-owned file using the product `manifestMode`, drop a child process to the
system `nobody` identity, and prove it can read the manifest while an explicit
`0400` negative control remains unreadable. The normal test suite skips that
test when it lacks root privilege; the attended macOS gate builds the test
binary as the operator and runs only that test under `sudo`.
