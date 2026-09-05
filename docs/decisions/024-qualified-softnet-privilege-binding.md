# ADR 024: Bind Softnet privilege to an exact root-controlled artifact

Status: Accepted for implementation; real-host qualification pending

## Context

The Tart 2.32.1 + Softnet 0.19.0 path qualified by Task 0 requires Softnet to
run with host root privilege. Normal session start must be unattended after an
explicit operator initialization, but standing privilege over a mutable
Homebrew path would let later user-controlled replacement bytes inherit root.
Tart locates and invokes Softnet as part of its normal launch mechanism, so an
independent passwordless command is not sufficient unless Tart resolves the
same privilege-bearing executable.

The M1A adversary is a malicious guest root, not arbitrary hostile native code
already executing as the single trusted macOS operator. The privilege mechanism
must remain honest about that boundary. If native code at the operator UID
becomes adversarial, giving that UID any direct setuid executable is too broad
and a separately qualified narrow wrapper or service boundary is required.

## Decision

User-attended, host-global `boxwarden init` installs the exact qualified
Softnet 0.19.0 executable SHA-256
`ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e`
at the corresponding digest-specific path below
`/Library/Boxwarden/toolchains/softnet/0.19.0/`. The installed executable is a
regular one-link file owned by root, assigned to the dedicated
`boxwarden-operators` group, and mode `04550`. Every ancestor is a non-symlink
directory owned by root and non-writable by group or other. The installed
versioned manifest is a regular one-link `root:wheel 0444` file with no
extended ACL. It binds the artifact, qualified platform release/build, qualified
Tart and Softnet paths/versions/digests, root and dedicated operator-group
identity, canonical Tart home, exact one trusted operator UID/name/home, the
Softnet mode, and installation time. It is intentionally non-secret local host
metadata: it must not contain a security-domain identity, CA material,
credentials, provider data, session state, private keys, tokens, or other
secrets. Its integrity depends on root ownership, zero write bits, exact
metadata/content validation, and protected root-owned non-writable ancestry;
readability is not an integrity mechanism. It is published last after the tree
is verified and fsynced. There is no mutable `current` link.

The installed artifact and manifest are host state, outside every
security-domain namespace. Initialization occurs once per trusted host; adding
or initializing another domain neither reinstalls nor re-authorizes this
mechanism. Host-global `boxwarden doctor` diagnoses its health without accepting
domain ownership semantics or silently repairing or rebinding it. Doctor must
open, hash, and strictly parse the manifest as the unprivileged trusted
operator; `0440 root:boxwarden-operators` would fail before new membership is
effective and while that membership is itself under diagnosis. A privileged
doctor helper would violate this read-only diagnostic boundary.

Init reopens and validates an unprivileged source without following symlinks,
rejects source setuid/setgid bits, copies through a root-owned sibling staging
directory, verifies the resulting bytes and metadata, and atomically publishes
only a previously absent complete digest tree. It neither overwrites ambiguous
state nor handles administrator credentials itself. A newly added
supplementary group is not effective in the initiating process tree; init
reports the required login-session refresh, while doctor and start fail until
current effective membership agrees with the manifest.

Normal start invokes the absolute qualified Tart binary without `sudo` or a
shell. Its constructed environment contains PATH equal to only the qualified
Softnet digest directory plus the fixed, validated operator, Tart-home,
generation-temporary, and locale values required by the launch. Ambient proxy,
telemetry, language-runtime, and loader variables are absent. Tart therefore
resolves one root-controlled Softnet artifact and no user-writable alternative.

Any setuid/setgid or passwordless-root Softnet target under mutable Homebrew
state is blocking `drifted/unsafe` state, even when the installed digest tree is
otherwise correct. Init, doctor, and start do not chmod, delete, adopt, or
repair it; attended manual inspection and remediation are required. Upgrade
installs an adjacent qualified version-and-digest tree. Exact uninstall names
one manifested digest and refuses while any live or recorded supervisor use is
active or unverifiable.

The corrected binary also treats an otherwise exact legacy `0400` installed
manifest as unexpected drift. Normal init and doctor do not chmod, rewrite,
adopt, or repair it. The one known pre-qualification installation is an
attended, exact-path, mode-only migration. Before old init, build the final-head
hostx test binary as the operator and run
`BOXWARDEN_ATTENDED_EXACT_GROUP=1 TestAttendedExactLocalOperatorGroupState`
unprivileged, retaining its redacted canonical pre-state evidence. Through
`inspectExactLocalOperatorGroup(..., false)`, it proves the exact local group/valid GID, exact caller
RecordName/UID/GeneratedUID binding, exact named and GUID membership, no nested
groups, caller presence in exhaustive user inventory, and no other user/group
sharing the GID. Run the exact old published `cf2212f` init against the original
configuration only if that test passes, require `refresh-login-session: false`,
then rerun the same test and require an identical evidence line. Old-init
success alone does not prove group pre-state or non-mutation. Stop before chmod
on any evidence mismatch, init failure, or refresh request. Only then capture
exact metadata and SHA-256 while still `0400`, change only that manifest path
to `0444`, synchronize filesystem metadata, and revalidate unchanged inode,
link count, owner, group, bytes, digest, and absence of ACL together with exact
`0444` and a read by a distinct unprivileged UID. Use only the corrected binary
afterward. Any failed validation, legacy mode other than exact `0400`, or other
unexpected tree state stops the migration for manual investigation.

## Alternatives considered

- Passwordless sudo over Homebrew Softnet is rejected because pathname
  authorization does not bind later bytes and silently transfers root authority
  across package upgrades.
- Passwordless sudo over a root-owned copy still does not match Tart's own
  helper lookup without interposing another executable, and introduces a second
  privilege path to specify and audit.
- A custom setuid wrapper could constrain arguments and environment for a
  hostile native operator, but safely mirroring and validating Tart's evolving
  Softnet protocol creates additional privileged code and a new compatibility
  boundary. M1A does not claim protection from that operator.
- A root daemon or launch service creates a larger persistent privileged
  protocol, lifecycle, and update surface than the selected mechanism.

## Consequences

The setuid bit is an intentional, visible host trust grant, not a claim that
Softnet becomes a sandbox for the trusted operator. The selected mechanism is
small and matches Tart's qualified execution path while preventing an
unprivileged process from replacing the privileged bytes. It is valid only for
the documented trusted-operator boundary. Per-domain management CA creation is
a separate `boxwarden --domain <domain> domain init` operation and is not part
of this host-global privilege decision.

Deterministic implementation tests use synthetic roots and fake directory,
process, and command adapters. Before V3 can be considered operationally
qualified, an attended disposable-host gate must prove exact installation and
rollback boundaries plus Softnet's setuid argument parsing, closed-environment
and dependency behavior, privilege transition/drop, signals, filesystem
writes, exact qualified network behavior, and absence of an alternate sudo
path. Until that evidence exists, V3's unobserved runtime properties remain
Pending and unclaimed. The missing attended evidence limits assurance claims;
absent a known unsafe defect, it does not by itself bar merge of deterministically
verified foundation code. Boxwarden remains not ready for general use or
production until the applicable operational qualification is complete.
