# V3 attended host and domain initialization evidence

## Result

**HOST INIT, DOCTOR, DOMAIN INIT, AND UNSAFE-HOMEBREW INIT REFUSAL PASS;
COMPLETE V3 GATE INCOMPLETE**

The host-global `init`/`doctor` and domain-scoped `domain init` behaviors were
exercised on the qualified macOS host with the maintainer present. The exact
published V3 code commit under test was
`36839dc7fc987fa0b56f29339331804b091726f2`; the clean binary SHA-256 was
`242fb3e015f91361f079af9209ff590f3fcefac120216f4e55c0eb4589f23c77`.
The unsafe-Homebrew refusal gate was completed with the same binary while the
published branch was at `7de2306b83136231beb9e03a7a769c52c9691d7f`; the
intervening commit changed documentation only.

Qualification did not complete, but Resume3 launched once for forensic/development
evidence. Softnet privilege transition/drop, the native serial relay, and GNU
Screen exit/cleanup behavior remain unqualified.
The maintainer has since approved a cumulative, sufficient non-perturbing
qualification-evidence model that includes bounded `proc_pidinfo` sampling;
approval of that model is not execution evidence. This evidence does not make
PR #2 Ready and does not qualify those runtime properties.

All domain roots, operator account fields, creation UUIDs, timestamps, and raw
sudoers output are omitted or redacted. No provider credentials were used.

## ADR024 Resume3 and Resume4 forensic evidence

Resume3 used exact V3 head `5c09aad9d2c322e42e6e8d7fd38e9584396b3ab5` and
launched the already retained disposable clone for its first runtime start. The observer recorded direct Tart→Softnet
ancestry, independent PGIDs, 192 sampled root-effective tuples (corroborating
only), 100 consecutive steady operator tuples, and stable identity/path. The
harness then failed its invalid shared-PGID assertion. Tart's group, Screen,
and relay were stopped; observed Softnet briefly remained, so the controller
correctly reported containment incomplete and sent no PID-only signal. A later
read-only inventory found Tart/Softnet/Screen/socat absent. The VM remains
evidence-only; this is not a completed qualification result. PR #2 remains
Draft.

### Accepted deterministic preparation

Private deterministic preparation was accepted without publishing raw
fixtures: process-topology script
`/private/tmp/boxwarden-v3-adr024-fresh-process-regression-test.sh` (SHA-256
`b7179de2f78e34fdb2546fed30053b2f617fcb979573954e5eecf80b496bd2d1`, 60/60)
and bootpd script
`/private/tmp/boxwarden-v3-adr024-fresh-bootpd-regression-test.sh` (SHA-256
`689a09753f5953e9623018ae6c902d7ee6d6834d4b0ffbf4f1f2a5ed0d024342`, 40/40).
These validate the corrected models but are not runtime qualification
evidence.

Resume4 launched nothing and stopped before token issuance because historical
bootpd inode/mtime differed while all protected fields, bytes, SHA-256
`d304019edf49f565d1950da56cbe687c732d57332cb702dc21f8f18e009af6c9`, XML, and
the sole `DHCPLeaseTimeSecs=600` semantics matched. No atomicity claim is made.

The authoritative private seal is
`/private/tmp/boxwarden-v3-adr024-forensic-seal`; its external seal digest is
`a8b546db960e19413ecc2b2bb657d87bebadea35445b4e1d61a6498be16d0f0f`. The seal
prohibits continuation from its evidence or dependency on a fresh controller;
future qualification must use a fresh baseline and fresh disposable runtime.

## Host-global initialization and diagnosis

The initial attended `boxwarden init` installed the exact qualified Softnet
artifact and reported `refresh-login-session: true`. After a complete
logout/login, the operator process had the dedicated group as an effective
supplementary group. A corrected-head repeat reported:

```text
host-installed: true
refresh-login-session: false
```

The final interactive-shell `boxwarden doctor` result, both before and after
domain initialization, was:

```text
status: healthy
```

Doctor ran without a domain and performed read-only production inspection. The
maintainer first invalidated the Terminal's sudo timestamp with `/usr/bin/sudo
-k`; doctor then remained noninteractive, returned `status: healthy`, and
exited zero. Its healthy result therefore includes successful parsing of actual
macOS `/usr/bin/sudo -n -ll` output for both mutable Homebrew Softnet candidates
without depending on cached authentication, and no privileged candidate
finding. The raw policy listing was not retained because the stable, redacted
repository evidence is the normalized production result.

A run from the Codex sandbox was deliberately excluded: that sandbox denied
`/usr/bin/sudo` execution and returned Directory Services errors, producing the
expected fail-closed `homebrew.scan` and `group.identity` findings rather than
evidence of host drift.

The manifest and direct host probes bound the qualified identities:

| Component | Qualified identity |
| --- | --- |
| Host | Apple Silicon macOS 26.6.2 build 25G83 |
| Tart | 2.32.1; executable SHA-256 `05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d`; archive SHA-256 `8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529` |
| Softnet | 0.19.0; executable SHA-256 `ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e`; archive SHA-256 `1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c` |
| GNU Screen | 4.00.03 (FAU), 23-Oct-06; executable SHA-256 `07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add` |

The final installed tree remained:

| Object | Owner/group | Mode | Links | Inode | SHA-256 |
| --- | --- | ---: | ---: | ---: | --- |
| Softnet digest directory | `root:wheel` | `0755` | 4 | `37686074` | n/a |
| `softnet` | `root:boxwarden-operators` | `04550` | 1 | `37686075` | `ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e` |
| `manifest.json` | `root:wheel` | `0444` | 1 | `37686076` | `1289fbb409bc075dd188bb4d9574db41136b0ee58b308e91fec950172d7dca68` |

Every ancestor from `/Library/Boxwarden` through the digest directory was a
direct `root:wheel 0755` directory. No mutable `current` pointer existed. Every
listed object had an observed `com.apple.provenance` extended attribute and no
extended ACL entries; the `ls -ldeO@` mode marker was `@`, not `+`.

Before host installation, the mutable Homebrew Softnet was directly observed
as `root:admin 04555`, one link, with the qualified executable digest. The
unsandboxed production doctor reported that privileged mutable artifact as
blocking `drifted/unsafe` state and also reported the then-absent host tree. It
did not repair either condition. The attended remediation changed only that
exact Homebrew file's mode to `0555`; inode, ownership, link count, and digest
were unchanged. Doctor then reported only the missing host tree.

## Unsafe mutable Homebrew refusal

The remaining non-runtime negative gate began with no running `softnet` or
`tart` process. A complete pre-state snapshot recorded the canonical mutable
Homebrew target as a one-link `root:admin 0555` regular file at device
`16777232`, inode `35254215`, size `14999920`, with the qualified Softnet
SHA-256. Its extended ACL was empty. The same snapshot recorded every object
in the installed `/Library/Boxwarden` tree, including device, inode,
owner/group, mode, link count, size, flags, SHA-256 where applicable, extended
attributes, and ACL absence.

With the maintainer present, the target file alone was changed to mode `04555`
and synchronized. Its other recorded metadata and bytes were unchanged. After
invalidating the sudo timestamp, `boxwarden init` did not prompt for a password,
exited one, and returned the expected refusal before invoking the root install
phase:

```text
boxwarden: initialize host prerequisites: refuse privileged mutable Homebrew Softnet at [canonical Homebrew path]; inspect and remediate manually
```

An immediate probe while the file was unsafe found the same device, inode,
owner/group, link count, size, and SHA-256. The attended cleanup changed only
that exact file back to mode `0555` and synchronized it. Fresh complete
snapshots then matched both recorded pre-states exactly: the Homebrew target,
its canonical symlink resolution, extended attributes, and ACL absence were
restored, and every object in `/Library/Boxwarden` was unchanged. A final
fresh-authentication `doctor` remained noninteractive, reported
`status: healthy`, and exited zero.

This gate used no sudoers change and did not execute Softnet, Tart, GNU Screen,
or a VM. It proves the V3 `init` path rejects a privileged mutable Homebrew
Softnet before trusted-host mutation and does not attempt to repair the unsafe
artifact. V3 has no `session start` command, so the future start-path refusal
remains outside this evidence.

## Legacy manifest exact-target migration

The one pre-qualification installation had an otherwise-valid
`manifest.json` at mode `0400`. Before mutation, the final-head attended group
probe established one exact local operator group, one exact caller binding,
matching named and GUID membership, no nested groups, exhaustive caller
membership, and no shared GID. The canonical values were retained only in
redacted form:

```text
operator_uid=[redacted] group_gid=[redacted] numeric_members=[[redacted]]
```

The exact old published binary then returned `host-installed: true` and
`refresh-login-session: false`; the group probe was byte-identical afterward.
The migration changed only the manifest mode from `0400` to `0444` at its exact
digest-bound path. Device, inode, owner, group, link count, size, flags, bytes,
SHA-256, extended attribute, ACL absence, and every other installed-tree object
remained unchanged. Both a synthetic cross-UID Darwin permission test and an
actual `sudo -u nobody` SHA-256 read of the installed manifest succeeded with
the exact digest above.

Normal corrected-head `init` and `doctor` performed no migration or repair.

## Domain initialization

Two disposable configured domains, `gatealpha` and `gatebeta`, began with
empty owner-private state roots. Sequential first runs returned
`management-ca: initialized`. The resulting CA trees contained only
`identity/ssh-user-ca/{ca,ca.pub,metadata.json}` with directory mode `0700`,
private key and metadata mode `0600`, public key mode `0644`, one-link regular
files, and exact creating-operator ownership.

The public identities were distinct:

| Domain | SSH fingerprint | Public-key SHA-256 | Metadata SHA-256 |
| --- | --- | --- | --- |
| `gatealpha` | `SHA256:T/AqXZFEYVe3n4P3Shj6PTuqYZJ8v2BO/KPvqEAAE2Q` | `cdb3c965ac2f026256ca82475c7e6b51ea391dddab16495fae67070cb0c2e204` | `7cdd9de1147c0ad467bd6a4111893663ab0eebcac63bc054fa582a2991bcb540` |
| `gatebeta` | `SHA256:Y6EpzO2+32SxU3N5qVxlK0jEaJLrbuneFjWxBvJrbbY` | `e007c544986837e75da8383529b306fd417c423f423cfd7a701194e7ba685239` | `1e3f5e066c92d18afda78fc4ee7f5053a4e4b3af4db0188ea193614ca286c3a0` |

Sequential repeat runs returned `management-ca: already initialized`. Every CA
file inode, mode, owner/group, link count, size, public fingerprint, public-key
digest, and metadata digest was unchanged. The post-domain-init host snapshot
matched the pre-domain-init snapshot exactly, including the two host-toolchain
digests and absence of a `current` pointer. This proves domain initialization
did not reinstall or mutate the host-global toolchain.

V3 exposes no `session start` command. Deterministic `sshx` and application
tests additionally prove that issuance loads existing CA state, missing or
partial CA state fails closed, and `domain init` is the only application path
that invokes CA creation. Targeted verification passed:

```text
go test ./internal/sshx ./internal/app -count=1
ok github.com/weshofmann/boxwarden/internal/sshx
ok github.com/weshofmann/boxwarden/internal/app
```

## Qualification boundary

The following V3 architecture questions are resolved by this attended evidence:

- exact qualified Tart/Softnet host identity and digest-bound installation;
- ownership, group, modes, ACL absence, link counts, and file digests;
- real directory-service membership plus required logout/login behavior;
- host-init and domain-init idempotence;
- read-only doctor behavior, actual macOS sudo-policy parsing, and useful
  fail-closed drift reporting;
- readable non-secret manifest handling across a distinct unprivileged UID;
- positive doctor detection of a privileged mutable Homebrew Softnet, followed
  by exact-target attended deprivileging; absence of a matching
  passwordless-root policy for either canonical mutable Homebrew Softnet path;
- real-host rejection of unsafe mutable Homebrew privilege by `init` before
  root installation or trusted-host mutation, followed by exact restoration
  and a healthy fresh-authentication doctor;
- distinct domain-bound CAs, no host mutation from domain init, and no lazy CA
  creation.

The following remain explicitly unqualified and block a complete V3 attended
gate:

- real-host proof that unsafe mutable Homebrew privilege blocks the future
  `start` path without repair;
- installed Softnet argument parsing, dependency resolution, effective setuid
  transition and privilege drop, signal behavior, and filesystem writes;
- launch under the closed runtime environment and exact network argv, including
  the qualified network behavior;
- bounded process ancestry and credential sampling, with its explicit race
  window, plus the rest of the approved cumulative evidence chain;
- two-PTY relay and GNU Screen retention/exit/cleanup behavior.

The runtime and Screen properties must be exercised only through a separately
reviewed attended procedure using disposable VM/session state. `proc_pidinfo`
is non-mutating sampling evidence, not lossless tracing and not independent
proof of every instant of the transient setuid-root phase. No result in this
document should be read as evidence for any unrun property.
