# ADR 021: Explicit read-only host-tree capabilities

Status: **PROPOSED**

## Context

Accepted Boxwarden architecture currently gives a guest no trusted-host
filesystem share. That is a sound default: the workstation account has
unrestricted sudo, so any writable share or ambient host path would grant a
potentially malicious guest root durable authority over trusted state.

Some projects are nevertheless useful to an agent as large, durable read-mostly
trees. Copying all content into every disposable session creates avoidable time
and storage cost. More importantly, a direct writable share would collapse
reading durable state and authorizing durable mutation into the same capability.

The 2026-09-01 spike qualified Tart 2.32.1 read-only VirtioFS against both uid
1000 and guest root. Neither could persist bytes, metadata, names, links, or
xattrs to the host, remounting did not broaden authority, inside/outside symlink
tests did not escape the selected tree, and replacing the host pathname after
attachment failed closed. APFS tree cloning also provided a cheap stable
baseline, and Linux OverlayFS accepted that real VirtioFS lower with guest-local
upper/work directories.

The spike does not make writable VirtioFS safe, accept promotion/write-back, or
remove the need to treat disclosure of the selected tree as irrevocable for the
session.

## Proposed decision

Retain the absolute default: no ambient, implicit, or writable trusted-host
filesystem exposure exists.

Permit a separately reviewed session policy to declare one or more **HOST TREE
CAPABILITIES**. Each capability identifies:

- one exact canonical absolute host source;
- one exact absolute guest destination;
- an access class;
- one owning security domain;
- one owning session and, when applicable, registered project;
- a visible lifecycle/status identity.

The initial access class is read-only live tree. A later proposal class may
refer to a sealed stable baseline B and a guest-local writable view, but that
class is not accepted by this ADR until the ownership and lifecycle design has
passed a separate implementation review.

Tart VirtioFS and its --dir argument are backend mechanics. Common policy
models the capability and required properties. A backend may implement the
property with another mechanism, but must independently prove that malicious
guest root can read only the admitted tree, cannot persist host changes, and
cannot widen authority.

No promotion or write-back follows from capability possession. A future trusted
host operation may inspect a materialized proposal G, compare B/G/H, and ask for
explicit approval. Until that separate design is accepted, all guest writes
remain disposable session state.

## Admission requirements

Trusted-host code performs admission over the exact source and destination
before persisting intent or invoking a backend.

### Source

- It is absolute, existing, canonical, and a directory.
- The source root itself is not a symlink.
- Canonicalization errors, nonexistent sources, and namespace ambiguity fail
  closed.
- The path is owned by the declared security domain and does not rely on
  cross-domain fallback or global search.
- The source is not the filesystem root, an entire user home, Boxwarden
  repository/runtime/state, a golden/session/backend root, an age private
  identity, SSH credential tree, browser profile, keychain/password-manager
  state, provider credential store, or another configured forbidden root.
- Overlap with another capability, runtime root, baseline destination, or
  destructive-control path is rejected unless a later reviewed rule proves the
  overlap unambiguous.
- A live capability warns that every readable byte is disclosed to a malicious
  guest and may be retained or transmitted within allowed network scope.

This is a structural deny boundary, not an assertion that paths outside the
denylist are safe. The operator must explicitly select the exact source and
security domain.

### Destination

- It is an exact normalized absolute guest path.
- It cannot be /, a system configuration root, a credential path, the
  guest-local upper/work directory, or a destination owned by another
  capability/profile/project.
- Parent/child and case-collision checks are performed across every declared
  destination.
- Backend-generated mount tags and identifiers derive from persisted capability
  identity, not from raw user-controlled flags.

The guest destination is configuration, not authority. A malicious guest root
can hide or overmount it inside its VM, but cannot thereby widen the host source.

### Access and lifecycle

- Access is explicit at session create/configure time; there is no inheritance
  from a domain, profile, current directory, environment variable, or prior
  session.
- Start revalidates that the source still satisfies the structural policy
  before attaching it. Failure yields an actionable non-ready state.
- Status reports capability identity, access class, canonical source,
  destination, domain, project/session owner, live versus stable-baseline
  semantics, and disclosure warning.
- Stop removes the live attachment with the VM process.
- Destroy removes guest-local writable state and releases baseline references;
  cleanup never guesses a host target from a display path.
- Removing or replacing a source while running is unsupported. The backend must
  fail closed, and reconciliation must report loss rather than silently attach a
  replacement.

## Stable proposal semantics reserved for review

The empirical result supports, but this ADR does not yet accept, the following
model:

- B: sealed stable session baseline created by an APFS CoW tree clone, with a
  correctness-preserving full-copy fallback;
- G: independently materialized guest proposal tree;
- H: current durable host tree at review time.

OverlayFS upperdir internals and whiteouts are never the portable contract.
Promotion eligibility begins only when trusted-host semantic comparison finds
H equal to B. H not equal to B fails closed and requires explicit
reconciliation. Git-backed projects continue to use Git-native semantics.

## Backend qualification

The M1A Tart implementation must:

- pass exactly one admitted canonical source per capability with read-only
  semantics and a Boxwarden-generated tag;
- preserve the Task 0 Softnet, serial, no-audio, and no-clipboard launch policy;
- reject arbitrary extra share or mount flags;
- test user and root writes, metadata changes, remounts, source parent/sibling
  access, inside/outside symlinks, nested sources, and post-attachment namespace
  replacement;
- repeat host-stability qualification for any Tart, macOS, or directory-sharing
  implementation change.

Open Tart issue 1308 remains an adjacent host-stability risk. A related host
panic or corruption symptom is a hard stop, not a test to repeat unattended.

## Security consequences

The capability deliberately weakens confidentiality for one selected tree. It
does not weaken host integrity in the qualified read-only mode. A compromised
guest may read, cache, transform, or exfiltrate all disclosed bytes and may use
them to attack parsers and tools inside the guest.

The trusted host gains additional attack surface in Virtualization.framework,
Tart argument construction, path admission, baseline creation, semantic
materialization, and cleanup. No guest-authored path, manifest, or OverlayFS
metadata is trusted as an authority-bearing instruction.

Quarantine receives no host-tree capability by default. A future quarantine
exception would have to be exact, explicitly approved, read-only, and
independently justified; ordinary domain inheritance is prohibited.

## Portability consequences

The policy is portable because it names required properties and lifecycle, not
APFS, VirtioFS, or Tart flags. APFS cloning is an M1A optimization behind the
stable-baseline abstraction. A future backend may use a different read-only
filesystem transport and baseline mechanism only after equivalent empirical
qualification.

## Alternatives

### Keep all host sharing prohibited

This preserves the smallest attack surface but forces full copies or
network/remote workflows even for large read-only sources. It remains the
effective decision unless this ADR is accepted.

### Writable host VirtioFS

Rejected. It directly gives malicious guest root durable host mutation
authority, and upstream has reported writable-share Git corruption.

### Hard-link baseline

Rejected. B and H would share file-content identity, so mutation could violate
the stable-baseline invariant.

### APFS snapshot as the product contract

Rejected. It is host/filesystem-specific and does not match the desired
per-tree abstraction. APFS CoW directory cloning is a replaceable optimization.

### Copy the project into every VM

Correct and portable, but potentially expensive. It remains the fallback and a
useful simplification for the first production milestone.

## Acceptance gate

This proposal becomes Accepted only after explicit architecture review. An M1A
implementation pass must be test-first, update the affected canonical documents
and plan, and re-run the full destructive/root/path matrix. Production
promotion/write-back requires a separate proposed ADR.

Evidence: docs/evidence/m1a-host-directory-capability-spike.md
