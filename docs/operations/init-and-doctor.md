# Host initialization and diagnosis

`boxwarden init` is an explicit, attended, host-global prerequisite operation.
It is the only command allowed to establish the ADR 024 Softnet privilege
binding. It neither selects nor initializes a security domain, and rejects an
explicit `--domain` rather than silently ignoring it. It must never be invoked
implicitly by session lifecycle commands.

The qualified Darwin installer accepts a bounded, versioned request over stdin
and uses the exact `/usr/bin/sudo -- <absolute-boxwarden> internal host-install`
root phase. It must install only the exact Softnet 0.19.0 digest tree under
`/Library/Boxwarden/toolchains/softnet/0.19.0/`, publish its root-owned manifest
last, and report when a new group membership needs a login-session refresh.
It does not store administrator credentials, repair unsafe Homebrew state, or
select a mutable `current` link.

The installed host-toolchain `manifest.json` is a regular one-link
`root:wheel 0444` file with no extended ACL. It is intentionally non-secret
local host metadata, and its integrity comes from root ownership, no write bits,
exact metadata/content validation, and protected root-owned non-writable
ancestry. It contains qualified platform release/build, exact Tart and Softnet
paths/versions/digests, root and dedicated operator-group identity, the trusted
operator UID/name/home, canonical `TART_HOME`, installed Softnet mode, and
installation time; it must not contain a security-domain identity, CA material,
credentials, provider data, session state, private keys, tokens, or other
secrets.

`boxwarden doctor` is the host-global, read-only diagnostic. It rejects an
explicit `--domain`, emits a stable status:
`healthy`, `missing/uninitialized`, `drifted/unsafe`, or
`unsupported/unqualified`. A non-healthy report exits nonzero and includes
observed/expected facts plus an attended remedy. Doctor never calls init,
requests interactive authorization, mutates directory-service state, executes
Tart or Softnet, or invokes a session operation. Its mutable-Homebrew safety
check uses only non-interactive `/usr/bin/sudo -n -ll` policy inspection. It
parses the verbose sudoers stanzas to bind `Options: !authenticate` to the
corresponding `Commands` block; inability to parse the list or prove that an
unsafe passwordless rule is absent is a fail-closed diagnostic, never an
authorization attempt or repair.

Doctor must read, hash, and strictly parse the manifest without privilege:
those checks diagnose both the manifest and the current group membership. A
`0440 root:boxwarden-operators` manifest would deny diagnosis before a new group
membership is effective, and a privileged helper would turn a read-only
diagnostic into a privilege boundary. Therefore `0444`, not group readability,
is the exact contract.

The qualified host identity is the exact macOS release `26.6.2` and build
`25G83`, observed with absolute `/usr/bin/sw_vers` probes. Root-published
toolchain manifests use schema version 2 and record `macos_build`; version-1
manifests are unsupported/unqualified and must never be migrated or overwritten
in place.

Normal init and doctor reject an otherwise exact legacy `0400` manifest as
drifted/unsafe and do not chmod, rewrite, adopt, or repair it. The one known
pre-qualification installation may be migrated only while attended and by exact
path. Before old init, build the final-head hostx test binary as the operator,
run `BOXWARDEN_ATTENDED_EXACT_GROUP=1` with only
`TestAttendedExactLocalOperatorGroupState` unprivileged, and capture its
redacted canonical pre-state evidence. The production inspector's
`inspectExactLocalOperatorGroup(..., false)` path proves exact local
group/valid-GID state, exact caller RecordName/UID/GeneratedUID binding, exact
named and GUID membership, no nested groups, exhaustive caller inventory, and
no other user/group sharing the GID. Then run the exact old published `cf2212f`
binary's host-global init against the original configuration and require
`refresh-login-session: false`; rerun the same test and require the evidence
line to match exactly. Old-init success alone is not a non-mutation proof. Stop
before chmod on mismatch, init failure, or refresh request. Then capture exact
manifest metadata and SHA-256 while it remains `0400`; change only that path's
mode to `0444`; synchronize filesystem metadata; then verify unchanged inode,
link count, owner, group, bytes, digest, and absence of ACL, plus exact `0444`
and a successful read by a distinct unprivileged UID. Use only the corrected
binary afterward. Any failed validation or state other than exact legacy `0400`
requires manual investigation rather than repair. Committed migration evidence
redacts local account and path values.

Linux and every unqualified platform return `unsupported/unqualified`; this is
intentional so CI can compile and exercise policy without accidentally treating
its host as qualified.

`boxwarden --domain <domain> domain init` is separate from both host commands.
It creates or validates only the selected domain's management SSH CA, performs
no host initialization or Softnet installation, and does not require the V2
host configuration block or host-path admission. It strictly validates JSON and
domain configuration, and validates the exact shape of an optional host object
when one is present. It is the only command that initializes that CA. A later
`session start` must require the already-initialized CA; it must never create,
repair, or select one lazily. Its authoritative result says either
`management-ca: initialized` or `management-ca: already initialized`.
