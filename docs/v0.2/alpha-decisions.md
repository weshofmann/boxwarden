# Boxwarden v0.2 alpha decisions

## Durable guest-action attempt foundation, 2026-09-25

An action command must not run before its exact attempt is durable. The first
source increment adds a private owner-only journal bound to the immutable
recipe digest, action ID and phase, domain, session UUID, backend object, and
start generation. Reservation re-admits the stored recipe and current running
record; a `once` action cannot acquire a second attempt on the same system,
even after a new start generation. Other actions cannot acquire a second
attempt in the same generation. An interrupted `reserved` attempt is
indeterminate rather than a reason to replay. A terminal result is one-way;
success requires a digest slot for a separately checked guest receipt and
still requires the exact running generation at the moment it is recorded.

This journal is a storage and replay boundary, not a guest executor or a
trustworthy completion claim. The caller must hold the session operation lock
and establish fresh READY independently of the stored readiness bit. Guest
receipt creation, host verification, explicit retry/skip, and execution are
subsequent increments. The public recipe loader continues rejecting all
session actions until those pieces are connected.

The next source increment defines a separate fixed action request and success
receipt rather than broadening the 30-second management probe. The request
carries the exact association, generation, recipe/action/attempt identities,
and a bounded argv vector with an absolute guest executable. Canonical JSON
bytes have one SHA-256 identity; the receipt repeats the bindings and that
request digest. Strict parsing rejects duplicate or unknown fields, changed
argv, alternate encodings, and mismatched identity. A matching receipt is
guest cooperation only. The helper's durable claim-before-execution marker,
closed environment, bounded execution, and host-side SSH admission remain
separate work; this protocol does not enable the public recipe actions.

The guest now has a root-owned claim store under its private Boxwarden state.
After checking the installed association, the helper must durably publish an
exact request-digest claim before a command can be considered for execution.
The same attempt without a success receipt returns an indeterminate result;
it cannot silently rerun. A success receipt is published separately with
no-replace semantics and is returned on an exact retry. Corrupt, linked,
foreign, or mismatched marker state fails closed. This is guest-local crash
recovery, not host evidence of trustworthy execution. The host journal and a
future fresh pinned SSH check remain independent requirements.

The fixed helper now has a separate ten-minute `action` mode. It validates the
canonical request, claims it durably, then passes argv directly to a bounded
process running as the explicit `boxwarden` workstation user. The root helper
reads the root-owned association and claim state; the child receives a closed
environment and guest home, not the helper's root credentials or ambient host
values. A failed command leaves the claim indeterminate. An exact completed
receipt returns on retry without another process invocation. Desktop `launch`
still refuses before claim because its graphical session environment needs a
separate design. The host has no public action route yet and must validate an
absolute guest executable when it eventually admits recipe actions.

The host action SSH method is separate from the short management probe. It
rechecks the exact private host-key pin and certificate paths, sends only the
canonical bounded request to the fixed `action` helper mode, and accepts only
the matching canonical success receipt. The session action service reloads
the immutable recipe argv, acquires the ordinary transition and session locks,
requires fresh exact-generation READY, and reserves the durable attempt before
asking the retained owner to execute. It requires another fresh READY check
after the receipt before recording the receipt digest as success. Transport
failure, malformed receipt, or lost readiness leaves the attempt
indeterminate and non-replayable. The retained-owner control route must
independently re-admit the exact reserved attempt before exposing its SSH
credentials; it and explicit recovery are still pending. No public recipe
action is enabled by these source increments.

The retained owner's independent admission check now compares the exact
running record, no-rebuild gate, reserved attempt, and immutable recipe argv
against the proposed action request before the future control route can use
the owner's credential. The older architecture guard allowed guest protocol
types only at the bootstrap composition site; its file-specific allowlist was
extended for the session action service and this owner check, while the
backend remains barred from importing action protocol types. The owner control
RPC and live SSH invocation are still separate work.

The retained owner now has the direct action call. It checks fresh READY and
the exact reserved intent before using its pinned SSH connection, validates
the returned success receipt, then rechecks READY and the durable intent. It
does not hold the readiness renewal mutex during a potentially ten-minute
guest action: management certificates last fifteen minutes and renew within
five minutes of expiry, so renewal must remain available while that call is
in flight. The supervisor control RPC still has to constrain and deliver this
operation; no public action route is enabled.

## Live workspace identity loss and caching experiment, 2026-09-24

A separate fresh empty-volume run reached exact mount-bound READY, then
developed persistent management-SSH readiness drift before stop. Public stop
reported Tart fallback without force. A bounded private trace identified the
guest rejection: `blkid` no longer resolved the exact filesystem UUID during
shutdown verification, before unmount was attempted. The format journal had
verified whole-device ext4 on the same raw-file inode; read-only inspection
after stop found its primary ext4 magic and UUID zeroed. This finding changes
the diagnostic order: preserving a mounted filesystem's identity while the VM
runs precedes tuning the shutdown grace period.

The pinned Tart source uses [cached I/O for a Linux root disk](https://github.com/openai/tart/blob/2.32.1/Sources/tart/VM.swift)
with an explicit filesystem-corruption rationale, whereas [additional file
disks default to automatic caching](https://github.com/openai/tart/blob/2.32.1/Sources/tart/Commands/Run.swift).
An explicit cached additional-disk trial was selected as a bounded experiment.
The different default is source evidence for a hypothesis, not proof that it
caused this run's lost superblock. Preserve the failed disk unmodified and
check fresh disks before launch and after stop.

The first controlled cached-disk trial used the same pinned Tart toolchain and
an independent 64 MiB ext4 volume. The raw inode had a clean ext4 magic and
UUID before launch and after attachment. The guest remained mount-bound READY
through a roughly one-minute dwell, then public stop reported
`request=guest_accepted forced=false` in 7.89 seconds. After exact stopped
proof, the same inode still had the expected UUID, clean state, and no journal
recovery or orphan flag. This result and Tart's Linux root-disk rationale
justify pinning `caching=cached` for Boxwarden's managed writable ext4 disks.
The backend preserves a single exact `--disk` argv element and Tart's default
full synchronization; no arbitrary disk option is exposed. Repeated fresh
volumes and stopped synthetic import still determine whether the durability
gate is satisfied. One clean run does not establish general reliability.

## Bounded public stop outcome, 2026-09-24

The supervisor now returns a typed, exact-generation stop result. It records
whether the pinned guest request was acknowledged, Tart was requested directly
or as fallback, and whether the retained owner sent force stop after the
bounded wait. The session service persists this result as a non-secret stopped
lifecycle diagnostic, and the CLI renders it on stop and status. An absent
result remains absent after ambiguous recovery; it is never guessed from
elapsed time or a stopped backend. `workspace_cleanliness=unverified` is
explicit because only stopped-volume inspection can establish that property.

The public result contains fixed enums and a Boolean, not guest stderr or
private host traces. This keeps the diagnostic bounded while making the
intermittent shutdown branch observable on the next fresh trial.

That trial reached mount-bound READY on a fresh empty ext4 workspace, then
reported `request=tart_fallback forced=true` after 67.53 seconds. Public
status retained the result and observed the backend stopped. The raw stopped
volume had the expected UUID but `needs_recovery=true`. This identifies a
failed or ambiguous pinned guest request followed by Tart fallback and force
stop as one dirty branch. The request failure reason still needs private
diagnosis; the result alone does not prove whether unmount was attempted.

## Exact workspace quiesce before poweroff, 2026-09-24

A freshly rebuilt and qualified base carrying the fixed systemd shutdown
helper still left ext4 needing recovery after an attached workspace stop. A
control session without a workspace stopped in about six seconds; attaching
an empty workspace to that same software drove stop to the 60-second grace
limit. A separate 120-second grace experiment also reached its limit and left
ext4 dirty. The attachment alone reproduces the failure; the import payload
is not required. These observations supersede the effectiveness expectation
in the previous shutdown decision without proving the exact guest hang cause.

The exact-generation owner now passes its already admitted workspace
UUID/path bindings in the typed `request_shutdown` operation. Before queuing
the same fixed poweroff job, the guest helper verifies every bound mount's
device, filesystem UUID, type, and writable state, unmounts each exact target
with argv-only `umount`, and checks the kernel mount table for absence. It
refuses an acknowledgment if any check or unmount fails. The host retains
Tart's bounded fallback and force-stop path; a guest response still does not
prove clean persistence. Offline ext4 inspection remains the independent
admission gate for stopped export and import verification. This is guest
cooperation for durability, not a new host trust boundary.

The affected Go packages and guest-definition artifact checks passed locally;
at that source checkpoint, a fresh base build was required before any
real-host effectiveness claim.
Previous failed volumes and qualification attempts remain immutable evidence.

The fresh quiesce-helper base subsequently passed separate-clone qualification,
but an empty attached workspace still used the full 60-second stop grace and
left ext4 requiring recovery. The explicit unmount did not establish clean
persistence. The host must surface whether the bound guest request failed or
was accepted before another source correction is selected.

Two additional fresh empty-volume sessions on the same base used an isolated
host-only trace of the existing request result. Both guest requests were
acknowledged within half a second and both stopped ext4 headers were clean.
One VM exited quickly; the other remained until the 60-second grace elapsed,
consistent with a later force stop. The first dirty outcome was untraced, so
its guest request result is unknown. A clean workspace can coexist with a
stalled poweroff; reporting only `stopped` loses that important distinction.

## Bound guest shutdown before Tart force stop, 2026-09-24

A new recipe-bound sandbox with a newly formatted ext4 workspace reached
mount-bound READY and matched a public synthetic import readback, but its
roughly 18-second public stop left ext4 requiring recovery. Tart 2.32.1 maps
the supervisor's `SIGUSR2` to Virtualization.framework `requestStop()`. A
successful signal proves only delivery to Tart; it does not prove that Ubuntu
Desktop started shutdown. The prior 15-second grace then allows a Tart
`SIGINT` force stop while the guest may still own a mounted volume.

The exact-generation supervisor first sends one fixed `request_shutdown`
operation over its already pinned management SSH connection. The bound guest
helper admits no caller-selected command or parameters and, as root, enqueues
`systemctl --no-block --no-wall --ignore-inhibitors poweroff`. The systemd
reply is only enqueue evidence. If the SSH request is unavailable or
ambiguous, the retained Tart handle receives its existing `SIGUSR2` request.
The owner still waits for actual child reap, with a bounded 60-second grace
before exact process-group force stop. A hostile or hung guest can still leave
ext4 dirty; stopped-volume export and import verification continue to require
an independently clean ext4 header and matching retained bytes. No guest
response grants authority to release a live volume lock or report READY.

The pinned guest helper artifact, installer lock, and finalizer pin change
together. Existing clones with the older helper fall back to Tart; a fresh
generic base build and qualification are required before this path is called
real-host verified. The longer grace increases worst-case stop latency but
avoids a predictable 15-second hard stop during an ordinary desktop shutdown.

Sources: [Tart 2.32.1 signal handling](https://github.com/openai/tart/blob/2.32.1/Sources/tart/Commands/Run.swift),
[systemd poweroff semantics](https://manpages.ubuntu.com/manpages/noble/man7/systemd.special.7.html),
and [systemd inhibitor behavior](https://www.freedesktop.org/software/systemd/man/250/systemd-inhibit.html).

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
closed, including a digest-matching file whose JSON is noncanonical or whose
recipe semantics are invalid. Existing version 2 records and version 1 rebuild
journals without these optional fields retain their prior unbound meaning. No recipe bytes enter
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

## Exact supervisor action control, 2026-09-25

The common action service may request only a typed action on an already live
exact generation. The supervisor checks the request's full domain, session,
backend, and generation binding, requires a fresh READY snapshot before and
after the retained owner's pinned SSH call, and validates the exact success
receipt. The separate action controller resolves only the existing generation
socket; it has no launch or stop method. An ambiguous or failed call remains
an indeterminate journal attempt for explicit recovery rather than implicit
replay.

Canonical recipe argv can make a guest action request 64 KiB, so the control
socket frame limit is 80 KiB. The launch request file keeps its independent
16 KiB cap; broadening that publication gate would admit a different class of
startup input. The architecture guard permits only the action protocol types
and encoders in the two reviewed supervisor action files.

## Explicit same-attempt recovery, 2026-09-25

An interrupted host action remains indeterminate. A distinct `retry_action`
control request is available only when a caller names the original attempt
UUID. The host reconstructs the exact stored recipe argv and binding, checks
the current running generation and fresh READY, and marks a crash-left
reservation indeterminate before sending the retry. The retained owner
independently admits only that indeterminate journal and rechecks it after the
guest call. Ordinary `run_action` still accepts only a reserved attempt.

The guest helper's durable claim is the replay boundary: it returns a stored
success receipt when execution already completed, refuses a claim without a
receipt, or executes if the original request never reached the guest. The
host records success only after the exact receipt and fresh READY checks.
A changed generation or recipe cannot turn the old attempt into a new command;
the public retry/skip UX remains separate work.

## Deliberate skip after a proved stop, 2026-09-25

`SkipAction` is a host-only decision for an exact reserved or indeterminate
attempt. It requires the normal stop transition to have published the exact
session as stopped, with no current generation, under the transition and
session locks. This prevents a skip from being followed by another action
while the old VM command is still active. The `skipped` state carries no
success receipt and makes no claim that the command did not run. It remains a
prior attempt for same-system `once` replay prevention. Repeating the exact
skip is idempotent; a successful action cannot later be relabeled skipped.
Public exposure and workflow sequencing remain to be implemented.

## Public alpha action controls, 2026-09-25

The alpha-only CLI exposes an explicitly selected `session action run` step,
an exact attempt UUID `retry`, and a stopped-session `skip`. The common
service, rather than the command parser, re-admits the stored recipe and
durable session binding. An uncertain run prints its durable attempt UUID and
the permitted recovery commands; a successful result prints a receipt digest
only after the service checked it. The CLI rejects a callback's empty or
foreign result instead of reporting success. At this checkpoint, normal
recipe loading still rejected action-bearing recipes while a fresh generic
helper qualification was being prepared.

## Explicit reconfigure recipe admission, 2026-09-25

The runnable recipe loader now admits `reconfigure` steps alongside reusable
`prepare` steps. Reconfiguration runs only when an operator names a specific
step through the public action command; it has no automatic start or replay
semantics. It selects a predeclared step from immutable session intent, and a
second attempt for that step in the same start generation is refused. The
command journals the exact attempt before guest execution and
exposes explicit retry and stopped-session skip for an uncertain outcome.
The reusable base key excludes this per-session step, while the full recipe
intent binds its argv to the session. `once` and `startup` stay rejected until
ordered lifecycle execution and pending-setup reporting exist. `launch` also
stays rejected until the guest graphical-session contract is designed. This
source admission is not a real-VM execution claim.

## Recoverable action attempt discovery, 2026-09-25

The public alpha `session action list <session>` reads the selected session's
bounded durable attempt journal under its ordinary lock. It rechecks each
entry against the exact domain, session UUID, stored recipe, and canonical
attempt before reporting it. Unexpected entries fail closed. This lets an
operator find a reserved or indeterminate attempt after the original client
loses its output without scanning private state or inventing a new attempt.
Listing is observation only: it does not retry, skip, infer guest execution,
or turn stored readiness into current READY.

## Ordered automatic-action plan, 2026-09-25

The first source increment for automatic setup derives a plan from an exact
running session, its immutable recipe, and the validated durable attempt
journal. It orders all `once` steps before `startup`, preserving declaration
order within each phase. A completed or deliberately skipped `once` step is
recognized only on the same system; a `startup` completion applies only to the
current start generation. An unresolved attempt on that system, including an
explicit reconfigure step or a prior-generation startup step, blocks new
automatic execution until explicit recovery. A later completed step cannot
hide a missing predecessor. The planner does not execute guest code or change
public recipe admission; the post-start orchestrator and status reporting are
separate increments.

The service now reads the exact session, recipe, and attempt set under one
session lock. Its bounded runner replans after each checked receipt and stays
bound to the record returned by start. Before reserving a `once` or `startup`
attempt, the action service reloads the journal under the transition and
session locks and requires that action to be the first pending step. An
uncertain result stops the runner and leaves the original attempt available
for explicit retry or deliberate stopped-session skip. Public start routing
and admission remain closed until their output and status semantics land.

Public alpha `session start` now invokes the automatic runner only after the
start service returns exact management READY for a recipe-bound session. The
output labels that evidence `management-readiness` and reports automatic
actions separately as `complete` or `blocked`; a blocked run returns an error
and points to the exact attempt list for recovery. The public recipe gate
still rejects `once` and `startup` until status can report their state.
