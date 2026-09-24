# Boxwarden v0.2 alpha decisions

## Immutable recipe intent at session and rebuild boundaries, 2026-09-24

The reusable base preparation key intentionally omits session-only actions and
workspace intent. A recipe session therefore stores a separate SHA-256 digest
of the complete canonical recipe in its creating record before cloning. The
private content-addressed object is published from the same parsed value used
for base preparation. Retry requires the same base and digest; existing
unbound sessions remain unbound rather than acquiring a new recipe implicitly.
Start rechecks a bound object before any guest launch.

A rebuild journal records both the old and candidate intent digests alongside
the exact backend and base revisions. Candidate reservation checks the stored
objects, and cutover changes backend, base, and recipe digest in one session
record write. Journal-only resume takes the candidate digest from that journal;
an explicit retry with a different digest fails. A rebuild selected by base
revision without recipe input leaves the replacement session unbound because
that base has no newly supplied full recipe. A same-base recipe request with
different intent refuses until a distinct reset/reconfigure operation has a
durable completion identity; silently treating it as a no-op would discard
requested session behavior. Guest `once`, `reconfigure`, `startup`, and launch
actions remain rejected until phase execution and receipts are implemented.

Review boundary: the digest is a private-state reference, not a credential or
proof of guest execution. Missing, linked, corrupt, or mismatched objects fail
closed. Existing version 2 records and version 1 rebuild journals without
these optional fields retain their prior unbound meaning. No recipe bytes enter
host shell commands, backend arguments, or the public progress record.

## Tart false-stopped observation and retained ownership, 2026-09-24

A real attached-volume sandbox returned READY, but Tart 2.32.1 subsequently
listed the exact object as `stopped` while its retained Tart child, VM process,
GUI window, guest address, and SSH endpoint remained live. An earlier host
spike saw the same contradiction. Tart's source derives `list` running state
from a config-file PID lock; the specific reason for the lost lock indication
has not been proved. This is a false negative in a backend observation, not
evidence that an orphan can be adopted or a volume released.

For an intended-running or starting *exact existing generation*, admit a
structurally valid exact Tart listing and reconcile its `stopped` state with
the supervisor's retained direct-child lifetime. READY still requires the
same fresh bound serial, host-key pin, current certificate, strict SSH probe
(including configured workspace mounts), and host/guest time-zone evidence.
Recheck retained lifetime after the guest probes and preserve the raw Tart
listing and contradiction in public status. Missing, wrong, stale, exited,
or unprovable exact-owner evidence remains drift/non-ready. A stopped listing
with no exact owner is never itself proof that a launched VM is safely stopped.
No persisted PID or path reconstructs authority.

The read-only retained-child method is a lifetime observation, not an
independent READY bit. Stop still persists stopping intent before asking the
exact owner to stop/wait/reap; managed-volume locks remain held until that
reap. Clone, export, detach, and recovery paths that require a stopped VM must
retain their independent ownership and lock checks. Source review and focused
regressions cover this decision; a fresh real sandbox run remains required.

## Initial scope and environment, 2026-09-23

- Use a private alpha context alongside the admitted host toolchain. The
  existing personal context and protected historical VM stay untouched. This
  gives the alpha exact state ownership without migrating old records.
- Prove the offline-inspector launch and transfer capabilities before fixing
  workspace APIs. Tart 2.32.1 advertises read-only file-backed disk attachment,
  but the current backend launcher always enables Softnet and provides no
  inspector transport. CLI help is a capability clue, not an isolation test.
- The initial graphical agent client candidate is the official ChatGPT Linux
  preview for Ubuntu 24.04 ARM64. [Official OpenAI documentation](https://learn.chatgpt.com/docs/linux/linux-app)
  lists ARM64 packages and says Computer Use is unavailable on Linux preview.
  An official IDE integration is the fallback if guest installation/launch
  fails. An unauthenticated launch test does not prove agent task completion.
- Preserve v0.1's fixed host launch and guest-root threat model until a typed
  v0.2 contract changes them deliberately. The independent review found that
  management READY composition and nonexclusive SSH require coordinated guest
  helper, protocol, host runtime, and test changes.

## Inspector networking, 2026-09-23

Independent source review of the pinned toolchain found that Tart 2.32.1
supports a read-only file-backed disk attachment but always supplies a network
device, using shared networking when none is selected. Softnet 0.19.0 applies
an explicit `0.0.0.0/0` block before its ordinary gateway, DNS, and public
destination allowances, but ARP, broadcast DHCP, and incoming IPv4 still pass.
Guest-controlled link state cannot establish containment against guest root.
This is a source-derived capability finding, not an empirical VM result.

Keep the stopped-workspace export acceptance gate closed until a separately
reviewed offline inspector capability or an explicit acceptance decision covers
that residual surface. Receiver parsing, exclusive workspace locking, and
synthetic failure tests can proceed independently. Tart's read-only attachment
does not by itself establish exclusive ownership; host locks must prevent a
writer from starting until the inspector has stopped and been reaped.

Sources: [Tart 2.32.1 launch](https://github.com/cirruslabs/tart/blob/2.32.1/Sources/tart/Commands/Run.swift#L362-L376),
[Tart read-only disk attachment](https://github.com/cirruslabs/tart/blob/2.32.1/Sources/tart/Commands/Run.swift#L882-L894),
[Softnet 0.19.0 guest filtering](https://github.com/openai/softnet/blob/0.19.0/lib/proxy/vm.rs#L25-L104),
and [Softnet incoming filtering](https://github.com/openai/softnet/blob/0.19.0/lib/proxy/host.rs).

## Prepared-base cache identity, 2026-09-23

The source layer and recipe-specific prepared layer are distinct. A source
candidate built from the signed Canonical ISO and generic guest definition is
not yet an application-prepared base. The recipe preparation key includes the
source identity, exact digest of tracked guest build inputs, machine shape,
apt package intent, and ordered `prepare` steps. Workspace attachments,
`once`/`reconfigure`/`startup` steps, and launch intent are excluded because
they operate on a clone rather than the shared base.

The key selects an existing stopped cache object; it is not evidence that the
build succeeded, that mutable package repositories yielded the same bytes, or
that the image is safe. Cache admission also requires recorded build inputs,
package/application BOM, exact backend object observation, and fresh clone
qualification. A changed package source or version must invalidate or
requalify that cache entry explicitly.

## Export inspector bundle source identity, 2026-09-24

The private inspector bundle records its clean source commit for provenance
and exact SHA-256 digests for every tracked build input: Go module and guest
sources, Swift helper, initramfs and kernel packers, entitlement, and builder.
Admission must compare the complete tracked input inventory and bytes with
the current clean checkout. A docs-only progress commit does not change the
bundle's executable inputs; requiring its manifest commit to equal HEAD would
invalidate a qualified bundle after every such checkpoint. Changing or adding
an input requires rebuilding and requalifying. The manifest's `test-only-
uncommitted` qualification is never admissible for production launch.

## Tart scratch and exact-generation ownership, 2026-09-23

The first public alpha session start failed before serial bootstrap. Tart
2.32.1 created a `control.sock` in the supervisor's exact generation root;
read-only descriptor inspection proved Tart owned the socket. The launch
policy had set both Tart's working directory and `TMPDIR` to that root, so
this run cannot distinguish which setting selected the path. Boxwarden's
generation validator correctly rejected the extra entry. The foreground
start timed out, and its exact controlled stop could not pass the same
validator. The test VM was stopped through the exact admitted Tart command;
the supervisor, Tart, and Softnet processes then exited.

Give Tart a private, explicitly admitted scratch child within the exact
generation, and point both working directory and `TMPDIR` there. Admit only
that exact subtree under the supervisor root and clean it only after the
retained Tart handle has been reaped. Do not whitelist a foreign `control.sock`
at the generation root or grant it Boxwarden control-socket semantics. Preserve
this failed qualification run and retry on a fresh clone after source and
test correction. Serial autologin timing remains a separate untested risk.

The production launcher now retains its direct child with `os.StartProcess`.
Darwin `Wait4(WNOHANG)` and process-group SIGINT share one mutex; the child PID
cannot be recycled between a successful exact reap and a retry signal. A wait
error or unexpected PID means ownership is unproven: refuse another signal,
preserve scratch and generation, and return a typed cleanup failure. This
retains failure evidence and stops a lost control reply from being mistaken
for a stopped session. A real short Darwin child, ambiguous wait cases, and
the cleanup propagation have focused tests; real Tart qualification is next.

## Generated host-key comment, 2026-09-23

Fresh public clone r1c passed Tart scratch and hvc0 prompt admission, but
serial bootstrap timed out without a framed helper response. An investigation
boot of that failed clone sent a canonical valid request and captured the
helper's exact error: `host public key is not fresh ed25519 public material`.
A separate read-only guest probe counted three fields in the generated
`/etc/ssh/ssh_host_ed25519_key.pub`; the third was an OpenSSH human comment
(`root@boxwarden-...`). The helper required exactly two fields. This was a
guest parser mismatch, not a failed host-key regeneration or a missing hvc0
login. All investigation boots ended stopped; r1c remains failed evidence.

Normalize the guest file to exactly the validated ed25519 type and base64
blob before returning it in the nonce-bound frame. Keep the host wire and CA
validators strict, and reject multiline or malformed trailing material. The
static Linux ARM64 helper digest changes, so the original generic candidate
cannot be promoted: rebuild from corrected autoinstall input and freshly
qualify a new clone. The serial error path still lacks structured guest
diagnostics; failure evidence required a bounded private console capture.

## SSH argv and certificate pairing, 2026-09-23

Fresh r2 clones exposed two distinct OpenSSH invocation defects. `-o
IdentityFile=/.../Application Support/...` passed the path as one argv element,
but OpenSSH still parsed its internal option string on spaces. Quoting the
path inside that option fixed parsing; credential path admission now rejects
characters that could trigger OpenSSH expansion. A separate explicit
`CertificateFile` option caused OpenSSH to treat the public certificate file as
a signing identity. Require the exact `IdentityFile` + `-cert.pub` companion
and let OpenSSH discover it. A bounded manual SSH probe isolated the second
failure; a new fresh public clone, qualr2c, subsequently reached READY through
the regular management path. Keep both failed clones as immutable evidence.

## macOS ACL admission, 2026-09-23

An integration test for prepared-base metadata found that `/bin/ls -lde` can
print `@` in the first mode marker while printing an extended ACL on the next
line. The shared `hostx.OSACLInspector` had checked only for `+` at the end of
the marker and falsely admitted such a path. Treat any nonempty ACL entry line
as extended ACL, in addition to the legacy marker. This shared fix protects
private prepared-base and workspace paths as well as existing host-toolchain
admission. A Darwin `chmod +a` regression test and focused package tests pass.
The SSH credential package had an independent copy of the same parser; its
private path check and real-ACL regression were corrected before credential
admission could rely on this result.

## Recipe-specific session source, 2026-09-23

The v0.1 creator selects the domain-wide current golden while reserving a
session. Alpha recipes can prepare different bases concurrently, so using that
pointer would allow another registration to change the selected source before
the session intent is durable. Admit each qualified prepared candidate as an
exact revision without moving `current.json`, then create through an explicit
revision method. The session lock and golden lock preserve the existing order;
the requested revision is persisted before cloning, and a retry naming another
revision is rejected. Existing `golden register` and `session create` keep
their v0.1 current-pointer behavior. The new source seam has focused tests and
an independent static review; public recipe composition remains pending.

## Rebuild SSH foundation boundary, 2026-09-24

The existing architecture guard admits `sshx.Binding`, `sshx.HostKeyPin`, and
`sshx.NewPinStore` only in the detached runtime owner. Rebuild adds one other
trusted-host use: the common session service reads the exact old pin under its
journaled binding, validates the whole pin record, and stores its digest before
candidate reservation. The journal validates that same binding and digest on
retry. Production composition constructs a domain-scoped pin store and passes
only its `Load` interface to the common service. The detached owner remains
the sole path that admits a new pin or performs the journal-bound transition
during serial bootstrap.

Review of the new paths: permit only `Binding` and `HostKeyPin` in
`internal/session/rebuild.go` and `internal/session/rebuild_journal.go`, and
only `NewPinStore` in `internal/sessionruntime/rebuild.go`. Keep `Admit`,
`TransitionRebuild`, and guest-observed key handling confined to the detached
owner and `sshx` implementation. The guard should continue rejecting these
selectors in other composition files. This narrow exception carries no pin
write capability into the common service; its failure mode is refusal to
reserve or resume a candidate when old pin bytes differ. The architecture
guard's production-tree test must pass before publishing the public command.

## Public workspace creation admission, 2026-09-24

The public alpha creator calls `workspaceformat.Admit` only to recognize a
completed exact formatter journal and disk before record publication, or to
resolve a concurrent create after an exclusive journal reservation lost the
race. This is a read-only check of host-owned formatting evidence; it cannot
format or accept a disk by guest assertion. The existing `PromoteVerified`
path independently repeats admission before making the record available.
The creator accepts only a pre-admitted, exact signed private formatter bundle
for new formatting, and a failed/interrupted journal remains unqualified.

Architecture guard review: add only `internal/workspacex/create_managed.go`
to the formatter `Admit` call-site allowlist. Keep the selector prohibited in
other composition paths; a positive guard fixture covers this exact file.
This exception introduces no new SSH or host integration authority.

## Inspected export retry, 2026-09-24

An `inspected` journal proves a stopped, zero-NIC inspector completed before
the receiver began, but the journal does not persist a digest-bound receipt
for the temporary stream or the receiver's final tree. A retry therefore
re-admits the exact snapshot and original empty private destination, then
captures a fresh stream under the same transaction ID and uses the ordinary
selected-file receiver. The receiver still forbids overwriting its final
transaction directory. If that directory exists, the result may represent a
rename completed before a crash, so the public retry refuses to adopt or
overwrite it and reports an ambiguous prior publication for review.

A hard exit can also leave `stream.bin` in the private snapshot directory.
Without a retained helper handle, that file is not proof that the helper has
stopped or that its bytes passed capture checks. The exclusive-create capture
gate refuses to replace it; automatic cleanup or adoption would make an
unverified stream authoritative. This case requires an attended check of the
helper/process state and exact private file before any manual cleanup. The
public retry does not clear a workspace's snapshot-copy Pending marker.

## Deterministic CI host and import owner boundary, 2026-09-24

The source verification job runs on hosted `macos-26` because the production
host's private ACL and file-identity contracts are Darwin-specific. These
tests are deterministic source checks; actual Tart/Softnet, GUI, credentials,
and destructive lifecycle qualification remain on the attended alpha host.
The architecture guard admits only the retained import owner's pinned SFTP
receipt/client selectors and launch-mount type needed by that narrow control
action. It does not open a general transport or backend seam.

## Public alpha import command and evidence vocabulary, 2026-09-24

The public alpha-only import command captures a new private source snapshot
and exposes the transaction UUID before transferring. An explicit resume uses
that exact UUID, volume, and session; it never selects a different source.
The command's result says `readback-matched` and leaves the journal in
`transferring`, because remote file fsync and host readback establish matching
bytes at that moment but do not establish durable guest directory state after
a crash. A separate verified-phase gate will require its own evidence.

## Import persistence qualification, 2026-09-24

The real public import matched a host readback while the guest was running.
A controlled stop then released the volume Use, but an independent selected
export refused at its host headroom gate before creating a transaction. Keep
the import journal in `transferring` and the disk intact. The verified phase
must compare the captured source to a selected read from the stopped retained
volume, bind the exact disk and export transaction, and reject ambiguous or
incomplete export results. Do not weaken the existing export reserve to make
this qualification pass.

Select the complete `boxwarden-import-<transaction>` directory for the
stopped-volume export. Selecting individual files would fail to prove that
empty directories survived and that no extra guest files appeared. The host
comparison re-admits the original captured manifest and checks the exported
tree exactly; a later transaction gate must also bind the export journal to
the stopped session, volume disk identity, and published destination.

The source gate admits the qualified raw disk again under the volume, session,
and storage locks, requires a fresh stopped backend observation, rehashes the
private export snapshot, checks the published whole-directory selection and
exact destination identity, and compares all exported entries against the
captured source. Only then does it advance `transferring` to `verified` with
the distinct export UUID. A `verified` journal records persistence at that
stop/export point; later guest writes can still change the workspace.
The architecture guard adds only this gate's direct qualified-disk admission
call to its existing per-file allowlist; the trust seam remains narrow.
The public alpha-only verify command requires the exact import and published
export UUIDs. It obtains the qualified backend observer from the selected
domain and prints `verified` only after the stopped-volume gate returns a
matching durable journal. A private readback receipt alone cannot drive it.
