# Boxwarden v0.2 alpha progress

Updated: 2026-09-24 UTC. Integration branch:
`weshofmann/feature/v02-alpha`; [Draft PR #12](https://github.com/weshofmann/boxwarden/pull/12).
The launch target is ten days from 2026-09-23 14:44 UTC. Stop starting new
work after 2026-10-03 14:44 UTC and leave a resumable handoff if unfinished.

## Current snapshot

- Complete: qualified recipe-bound base/create/start, retained ext4 workspace
  across stop, reattach, and same-software rebuild, plus selected stopped
  export and inspected-phase resume on synthetic data. The full local Go
  suite, vet, and CLI build passed at the prior source checkpoint.
- Complete: public stopped-only `session delete` has durable deleting intent,
  exact backend retirement, and volume retention. Tests cover disk bytes,
  post-effect retry, partial two-volume detach, sync failure, missing disk,
  running backend, and active Use. A separate reviewer found three Important
  issues; all were corrected and rereview found no remaining Important issue.
  One exact alpha-owned stopped Tart sandbox was deleted through the public
  CLI. Its session record and VM became absent; its 64 MiB volume became
  Available and detached with no Use or Pending. Raw inode, size, and SHA-256
  stayed unchanged. This is targeted real-host qualification.
- Verification: `GOTOOLCHAIN=local go test -count=1 ./...`, `go vet ./...`,
  CLI build, and `git diff --check` pass for this source checkpoint. The
  separate reviewer rechecked the corrected deletion path.
- In progress: explicit bounded synthetic host-project ingress into an
  independent workspace. A private host-source snapshot primitive accepts
  only an owner-controlled tree with bounded regular files and safe names,
  captures exact digests, and publishes without replacing an existing
  transaction directory. Targeted import tests and vet pass; a separate
  source-only rereview found no remaining Critical/Important defect. No public
  ingress command exists yet.
- Complete source foundation: readmission rehashes every captured
  file and requires exact manifest membership. A durable import journal has
  only `captured` and `transferring` phases, with atomic creation and exact
  retry after uncertain directory sync. `BeginImport` binds the capture to a
  clean running session, exact active workspace Use and mount, and a fresh
  supervisor READY snapshot under transition, session, and storage locks.
  Changed-package tests and vet pass; a separate read-only rereview found no
  remaining Critical/Important defect in this reservation foundation.
  Transfer and a verified phase were not part of that reservation checkpoint.
- Implemented at this checkpoint: a private pinned SFTP transport re-admits
  the snapshot, uploads only bounded captured files with remote file fsync
  requests, reads each file back into a private host tree, and returns a
  receipt only when the canonical manifest and all bytes match. It uses the
  qualified macOS client's batch syntax and shares the strict SSH host-key,
  certificate, and forwarding policy. Focused `internal/sshx` tests and vet
  pass; an actual local OpenSSH SFTP server completed the batch upload and
  readback. A separate reviewer found no remaining Critical/Important defect
  in this private transport. No guest file transfer or durable verified
  journal phase is claimed. Remote file fsync and readback do not alone prove
  directory persistence after a guest crash.
- Implemented for the next source checkpoint: a typed, bounded retained-owner
  import control action keeps the current-generation SSH connection private.
  The owner admits the durable transferring journal, current running session,
  exact workspace Use, launch mount, and fresh READY evidence before and after
  transfer. The host caller persists `transferring` before mutation, holds the
  transition and session locks through the RPC while releasing the domain
  storage lock, then rechecks source, journal, workspace, readiness, and
  measured receipt. Tests cover foreign generation, invalid mount, changed
  Use, false readback, readiness loss, and lock lifetime. Changed-package
  tests, vet, and diff check pass; separate read-only review found no remaining
  Critical/Important defect. The journal remains `transferring` after receipt.
- Transport probe: the exact alpha-owned disposable sandbox reached READY;
  an exact-generation, host-key-pinned SFTP batch read its remote working
  directory successfully. The sandbox was stopped again. No project file was
  transferred; this is a capability probe, not ingress qualification.
- CI repair in progress: the formatting gate passed after `5183b8b`, then
  the Ubuntu job exposed Darwin-only ACL and file-identity checks plus three
  missing retained-owner selector admissions. The deterministic job now
  targets hosted `macos-26`, and the architecture guard admits only those
  selectors. The full local Go test and race suites, vet, and CLI build pass;
  the hosted rerun is pending. This is source verification, not real-host
  qualification.
- Next: add the narrow public capture/begin/transfer command, exercise one
  real synthetic guest transfer and readback, then define the durable verified
  journal gate. Fresh source-tracked acceptance follows.
- Limits: CI rerun, software-changing rebuild, ambiguous export final-rename
  recovery, intermittent dirty stop, GUI agent interface, and final fresh
  acceptance remain open. No human action is currently needed.

## Verified checkpoints

| Area | Evidence and limit |
| --- | --- |
| Host and installer | Read-only host doctor is healthy. The Canonical Ubuntu 24.04.4 ARM64 Desktop ISO has a valid detached signature and exact pinned SHA-256. Pinned OpenSSL 3 and xorriso executables have been checked. |
| Public management | A fresh disposable clone reached exact-generation READY through serial bootstrap, host-key pinning, certificate, strict SSH, and time-zone checks; it stopped and restarted to READY. GNOME, Firefox, and a synthetic home file were observed after restart. |
| Reusable preparation | Strict versioned recipe and ISO checks, candidate build, guest preparation, fresh-clone qualifier, private evidence, and cache admission are implemented. A fresh real build using the corrected finalizer completed installation, guest preparation, clone-ready shutdown, and qualification. Its fresh clone reached READY, passed package-inventory and identity checks, and stopped; the versioned prepared record was admitted. This is base qualification, not the full workspace/export acceptance path. |
| Workspace volume | One alpha-owned 64 MiB ext4 volume was formatted in a zero-NIC VM and independently inspected. Its synthetic file retained its digest across an earlier stop/restart after manual remount. Public detach and attach moved this exact volume from a stopped original system clone to a separate replacement system clone without copying the disk. The replacement guest file manager opened the mounted 57-byte synthetic file and displayed its expected content. The corrected public stop released Use and left ext4 clean; source and export snapshot SHA-256 matched. A later public detach/attach to a fresh recipe-bound sandbox reached mount-bound READY with the same 48 MiB file and digest; controlled stop again left ext4 clean and Use clear. |
| Public volume creation | `workspace create` binds an exact private signed formatter bundle, clean source checkout, new volume/filesystem UUIDs, and capacity to the explicit alpha domain. Focused tests cover creation, idempotent unbound retry, verified-journal recovery after record-publication interruption, preflight refusal before disk reservation, and failed-format refusal without reformatting. A real public command formatted a fresh 64 MiB synthetic volume in a zero-NIC VM. Independent host checks found a verified journal, available unbound record, matching inode/UUID/size, clean ext4 state without `needs_recovery`, and a successful exact public retry without reformatting. A fresh prepared-base clone subsequently reached mount-bound READY and a guest user created a synthetic directory on the mounted volume. A separate fresh volume remained READY for about eleven minutes, retained a GUI-created directory across stop/restart, and stopped cleanly. Intermittent shutdown failure remains unexplained; the retained-volume rebuild is qualified below. |
| Automatic mount and READY | The static generic guest helper resolves an exact FS UUID, mounts ext4 at a validated path, checks existing mounts and read-write state, and probes exact bindings. The host owner derives those bindings from admitted session/Use records, ensures them before READY, and repeats the bound probe for status. Full local Go tests, focused race tests, vet, and artifact checks passed at `5c84520`. The fresh original clone reached mount-bound READY in two generations; the separate replacement clone also reached mount-bound READY with the same reattached volume and later exposed the expected file in its GUI. Both are stopped now. |
| Controlled export | Bounded receiver, exact stopped-volume snapshot, private helper capture, and selected publication have targeted tests. After clean guest shutdown, a real public export published one selected 57-byte regular file into a new private directory. Its SHA-256 matched the known synthetic guest file; the durable journal read `published`, and the source and snapshot digests matched. A later public resume of an earlier clean `snapshot-ready` transaction published the same selected file and digest, with no leftover spool or running inspector. An earlier dirty-volume attempt remains private failed evidence. A fresh synthetic `inspected` hard exit was reviewed privately and resumed through the public command: its 48 MiB selected file matched the guest SHA-256, the journal became `published`, and no inspector or spool remained. A post-rename hard exit with an existing final directory remains an explicit manual reconciliation case. |
| Inspector bundle admission | A clean-source production bundle from the pinned ISO passed independent artifact checks and host admission with both synthetic and real journal-derived requests. Admission checks tracked source bytes, kernel/ISO pins, seven artifact digests and private metadata, deterministic appended guest/request initrd, and the signed helper's sole Virtualization entitlement. The first live export-mode VM rejected an unclean volume; a later fresh clean-volume run completed selected publication with zero-NIC inspector evidence. |
| Example | `examples/v0.2-alpha-base.json` passed public recipe/ISO validation. It requests the Desktop source, a small package set, and one named workspace intent. |
| Recipe-bound create | `session create` accepts exact recipe/ISO/guest definition/tool inputs, prepares or reuses a qualified base, and calls `CreateFromRevision`. A focused fixture and the real public command both selected the newly prepared revision without changing the domain current golden. A later public `alpha prepare` reused the admitted cache after verified redundant installer staging was retired, without creating an attempt journal or new Tart object. |
| System rebuild | The public `session rebuild` command accepts an exact admitted `--base` revision, complete recipe preparation inputs, or no new input to resume a pending journal. A full fake-backend transaction kept the session UUID and workspace attachment identity, reached fresh candidate READY, deleted only the journaled old system, and then no-oped for the selected base. Full local Go tests, vet, CLI build, and diff checks passed. One public no-volume Tart rebuild preserved the session UUID, transitioned the exact pin, reached repeated READY, retired the old object, cleared the journal, and stopped consistently. A later public rebuild with an attached ext4 volume preserved its identity and content, reached mount-bound READY, retired the old system, and stopped cleanly. That target was a distinct qualified Tart object with the same generic software image; a software-changing upgrade and failure recovery remain open. |

Hosted CI is active. The prior Ubuntu job failed on Darwin-specific tests;
the `macos-26` correction is awaiting its hosted rerun. Local checks above
are source or explicitly described real-host checks; the full graphical,
rebuild, and failure-recovery acceptance path remains open.

## Current work

The rebuild foundation now has a separate strict, bounded, durable journal
that binds the stable sandbox identity to exact old/candidate systems and base
revisions. Candidate reservation checks active sessions and other rebuilds;
ordinary create/start and stopped workspace mutation refuse a pending or
corrupt journal. Read-only status reports drift during a pending rebuild and
does not wait on the supervisor. The old pin witness is defined over the full
validated pin record loaded under the exact old binding. Targeted session,
workspace, and app tests plus targeted vet, formatting, and diff checks pass.
At this foundation checkpoint, public routing and real rebuild qualification
were still pending; later paragraphs record the implementation progress.

The stopped rebuild reservation gate now holds all attached volume Use locks,
the exact session lock, and the domain storage lock through the journal writer.
It rechecks the session, attachments, backend stopped observation, and absence
of Use or Pending before invoking that writer. Focused lock and refusal tests,
the affected package tests, targeted vet, and diff checks pass. The producer
acquires the golden lock inside this gate and persists candidate intent before
releasing it for cloning. It accepts only an exact admitted stopped base,
observes the new candidate ID absent, captures the exact old pin witness,
clones the recorded candidate, randomizes its MAC while stopped, and advances
the journal to `cloned`. A clone error after backend mutation was retried
without a second clone. Targeted session/workspace/app tests and vet pass;
this remains a source-only checkpoint without public rebuild or live VM proof.
The next implementation step is internal new-generation cutover, followed by
exact old-system retirement.

The host-key store now has a narrow rebuild-only transition primitive. It
requires the old pin's exact binding and full-record digest, the candidate
binding, operation UUID, and serial-observed Ed25519 key. A private sibling
staging file supports atomic replacement and exact retry after an interrupted
rename; conflicting old, candidate, or staged bytes fail closed. Pin-store,
runtime, and session package tests, targeted vet, and focused pin race tests
pass. The detached owner now loads an exact `cutover` journal before candidate
launch, refuses a running old backend or unavailable transition capability,
and rechecks the unchanged journal after serial bootstrap before rotating the
pin. An owner fixture rotated an old pin to the exact candidate and rejected a
changed journal without changing the old pin. Affected owner, pin, session,
and supervisor tests, targeted vet, and focused owner race tests pass.
These owner checks have source-only fixture evidence; there is no public
rebuild command yet.

The control plane now persists the `cutover` journal phase before switching
the stable session record to the stopped candidate backend and base revision.
An injected interruption between those writes left the old record intact and
retried to the exact candidate; running old or candidate objects were refused.
The workspace attachment retained its stable session UUID through a real
record/gate integration fixture. The internal candidate start now permits only
the exact `cutover` journal and candidate session binding. Its workspace
coordinator rechecks that journal under the normal volume/session/storage
lock order before reserving exact candidate Uses and publishing `Starting`;
the existing supervisor path then requires fresh exact readiness. Ordinary
start remains blocked. Targeted session/workspace/runtime/app/supervisor tests
and vet plus focused start race tests pass. The journal has not yet advanced
to `ready` through the start path alone. A separate confirmation now requires
fresh exact-generation supervisor evidence and durable candidate workspace
Uses before advancing `cutover` to `ready`. A missing zone proof and an
interruption before journal advancement both left the journal at `cutover`;
an exact retry reached `ready`. Targeted session/workspace/runtime/app tests
and vet plus focused race tests pass. No public or live VM rebuild has been
qualified by a real VM yet.

Retirement now refreshes exact candidate owner readiness and workspace Use
evidence, then persists `retiring` before asking the backend to delete only
the journaled old object. A missing old object before delete intent is drift;
an absent object after intent is an exact retry success. The journal is
removed last. A post-effect delete error retained `retiring` and retried
without a second deletion. Targeted session/workspace/runtime/app/fake backend
tests and vet plus focused retirement race tests pass. This is source-only;
public orchestration and real rebuild qualification remain open.

The source-level phase driver now resumes the strict journal from reserved,
cloned, cutover, ready, or retiring state. It invokes the exact candidate
start and fresh ready gate before deletion. A complete fake-backend run kept
the stable session ID, ended with one replacement clone and one old deletion,
and left no journal; repeating the already selected base did not reclone or
delete again. At that checkpoint, the driver was not yet wired into the CLI
or production Tart dependencies; the public routing is described below.

The public `session rebuild` command now routes an explicit `--base` revision,
the existing qualified recipe preparation inputs, or a no-input resume of a
pending journal into the production Tart/Softnet and SSH composition. The
host toolchain is re-admitted before clone or deletion. App routing tests
reject a missing domain, malformed base, conflicting recipe/base inputs, and
an unqualified preparation receipt. The architecture guard permits only the
documented exact old-pin read paths; pin admission and rotation remain with
the detached owner. Full local `go test -count=1 ./...`, `go vet ./...`, a CLI
build, and `git diff --check` passed. A real VM rebuild still requires
qualification; these source checks do not prove live behavior. Invoke with
`--domain <domain> session rebuild --base <revision> <name>` for a new
replacement, or `--domain <domain> session rebuild <name>` to resume its
pending journal. A requested base already selected with no journal is a
no-op. Recipe inputs follow the public `session create` preparation flags;
resume without repeating those inputs after an interrupted preparation.

At the `6dd0f97` public-command checkpoint, host doctor, stopped Tart
inventory, memory, disk reserve, exact old pin, and the absence of attached
volumes or a pending journal passed preflight. A same-base public command
no-oped. One public rebuild of that owned stopped no-volume session onto a
separately admitted base returned READY. Repeated public status was READY,
the stable session UUID and domain remained, the new pin matched the new
backend, the old Tart object was absent, and the journal was gone. Public
stop then left the new backend consistently stopped. This qualifies the
live control path without a retained workspace. The prior failed volume is
still preserved privately and is not a source for the next qualification.

The public `workspace export resume` path takes only a durable transaction
UUID and exact inspector build inputs. It re-admits the journal's private
snapshot and original empty destination before building a fresh inspector
bundle, capturing a new stopped zero-NIC stream, and using the same bounded
selected receiver. Tests reject an occupied final directory and changed
snapshot before capture; an unverified stale private spool remains untouched
and blocks recapture. An inspected retry without that ambiguity published the
selected synthetic file through the receiver. A hard exit with a surviving
spool or final directory still needs attended evidence review; the command
does not adopt either. Invoke as `--domain alpha workspace export resume
--source-root PATH --iso PATH --go PATH <transaction-uuid>`; the journal
supplies the original selection and destination. Full local
`go test -count=1 ./...`, `go vet ./...`, CLI build, and diff checks passed.
At that source-only checkpoint, a real-host retry remained for the
`inspected` phase; the later real-host result is recorded below.

The public resume was also exercised against the retained clean
`snapshot-ready` transaction from an earlier host-headroom refusal. After
healthy doctor, stopped Tart inventory, clean source and snapshot digest,
empty destination, and disk-reserve preflight, a fresh zero-NIC inspector
published exactly the selected 57-byte regular file. The output SHA-256
matched the prior successful export, the journal advanced to `published`,
the snapshot digest still matched, and no spool or running Tart object
remained. The older `inspected` transaction's snapshot has ext4
`needs_recovery`; rerunning it would predictably fail and would not qualify
the retry path. It remains preserved as failed evidence.

The managed-volume shutdown correction at `7eeba47` now passes one real
attached-volume public stop: the session and backend are consistently stopped,
Use is released, and ext4 `needs_recovery` is clear. A fresh public export
published the selected 57-byte synthetic file; host verification matched its
known SHA-256 and confirmed that the durable journal is `published` and the
source and snapshot digests agree. A preceding attempt stopped at the host
headroom guard after inspector capture; its destination stayed empty and its
snapshot-ready transaction is preserved. Removing only a rebuildable task Go
cache restored headroom for the fresh run.

The source correction at `16d6384` now permits READY from an exact retained
supervisor and fresh bound guest checks when Tart lists that same owned object
as `stopped`; status preserves the raw contradiction. A fresh attached-volume
public start and first status reached READY with Tart listing `running`.
A later status drifted because the exact supervisor snapshot timed out while
the supervisor, Tart child, socket, and Tart's `running` listing remained.
The guest SSH port was unreachable. Public exact stop completed in about
15 seconds and left the session consistently stopped, but ext4 now reports
`needs_recovery`, consistent with bounded force-stop fallback. This failed
qualification is preserved privately; it does not establish the new
false-stopped path on a real VM. The following paragraphs retain the
implementation sequence; earlier pending statements describe their
checkpoint at the time.

The snapshot control request previously gave the guest probe and Unix socket
the same two-second absolute deadline. A slow or unreachable SSH endpoint
could consume that entire interval, leaving no time to return a non-ready
snapshot; a focused regression reproduced the missing response. The server
now reserves up to 500 milliseconds within the caller's deadline for its
reply and demotes an observation that finishes after its own deadline; a
regression failed when a canceled observer returned READY, then passed after
the demotion. Focused supervisor/runtime/app tests and targeted vet pass. This
corrects failure reporting; the guest SSH loss itself remains unexplained,
and the real failed run is not requalified.

A storage regression presents an exact but false `stopped` Tart listing while
the session has durable running intent and an exact volume Use. Detach, offline
export snapshot, and direct Use release all refuse, preserving the attachment
and Use without creating an export transaction. This checks the stopped-only
gate under the observed contradiction; it does not qualify Tart's behavior or
explain the guest SSH failure.

The admitted prepared base has been reused by public recipe-bound create.
Public start, fresh mount-bound READY, and stop succeeded twice with separate
Use generations. The independent ext4 volume was detached from the stopped
original clone and attached to a separate stopped replacement without copying
or reformatting it. The replacement reached READY and opened the expected
synthetic file in its graphical file manager; it stopped again and cleared Use.
Both system clones are stopped and the volume remains Available and attached.

A fresh isolated inspector probe read a private copy of that synthetic ext4
volume through a restrictive read-only mount. The exact typed report, zero-NIC
and stopped-VM checks, helper reap, and unchanged disk check passed. Earlier
rejected attempts exposed a path-normalization false rejection and binary TTY
output conversion; both were corrected and rechecked on fresh inputs. This
qualifies the private-copy probe, not public stopped-volume export.

An independent export design review found publication, recovery, and resource
bounds that must be enforced. `af857ec` admits a durable `export-snapshot`
Pending marker and verifies that interrupted copying blocks start and detach.
The strict private transaction journal is connected to stopped-volume snapshot
copying and Pending clearance. It admits a qualified source, verifies a
distinct copy and source digest, and leaves Pending plus a copying journal on
partial cancellation. Snapshot-stage recovery requires the same stopped
backend and exact binding, removes only recognized partial state, and records
an aborted phase before clearing Pending. A ready-journal retry rechecks the
snapshot bytes before clearing Pending. Targeted source tests include the
crash windows between cleanup, journal advancement, and marker clearance.
The host capture gate enforces an 80-second helper deadline, a 320 MiB stream
cap, a 16 KiB diagnostic cap, disk-reserve sampling, private ACL admission,
and exact stopped/zero-NIC/byte-count evidence before exposing a closed spool.
Its tests use a synthetic subprocess. It is not yet connected to an admitted
production inspector or the receiver, and no exported tree has been published.
The generic guest-side selected-file writer now serializes regular files and
directories into the receiver's typed format, with path, type, link, count,
and byte limits. A local round-trip test passed through the host receiver.
An exact transaction-bound request parser and optional private initramfs data
member now carry selected paths, expected ext4 UUID, and disk size without a
host share or network. The guest boot code now reads the root-owned request,
checks disk size and ext4 UUID, streams selected files from a restrictive
read-only mount only after rejecting error, orphan, and recovery-required ext4
states, unmounts, and only then emits the terminal record. Targeted
tests and a Linux ARM64 cross-build pass; this branch has not yet had a live
export-mode inspector boot, and the host control plane does not generate the
request bundle yet.
The Swift helper now provides distinct export preflight and boot commands
that check the journal's transaction-bound snapshot path, device, inode, and
size, retain the zero-NIC/read-only VM configuration, and report export-mode
host evidence.
The host capture parser accepts the marker; the production caller must still
require it. Local disk-admission tests reject identity, transaction, hardlink,
and private parent-mode drift. No live export-mode VM has run.
The host can now derive the guest's transaction request only from a
snapshot-ready durable journal after re-admitting and hashing the private
snapshot. Tests reject a copying journal, another domain, a missing or short
snapshot, and changed snapshot bytes. This preparation does not create an
initramfs bundle or authorize publication.
The host receiver now has an optional exact selection gate. When supplied
the journal's selection, it admits only selected paths and necessary parent
directories, requires every selected path to appear, and rejects an extra
sibling before publication. Host orchestration must supply the journal
selection; no public export invokes the receiver yet.
Explicit capture and receive entry points now fail closed if the helper lacks
export-mode stopped-host evidence or the receiver lacks a host selection.
The former removes a rejected synthetic spool. They remain disconnected from
production artifact admission and journal phase transitions.
The source builder for a private per-transaction inspector bundle passed one
test-only full build from the pinned ISO. Independent checks rehashed all
manifested files and parsed the appended guest and request cpio members.
The ordinary builder refused the dirty checkout. A clean-source build then
passed from published `b51d900`; its source and ISO pins, seven file
digests, signed helper, ARM64 guest type, and appended guest/request members
were independently checked. It used a synthetic private request.
Live export boot remains pending.
The builder now also records the exact inventory and digests of 16 tracked
inspector build inputs. A test-only and a clean-source production full build
passed, and independent checks matched every recorded input and artifact.
The host admission gate now enforces this inventory rather than requiring the
same commit SHA, so a docs-only commit does not invalidate executable artifact
identity. A focused fixture rejected a changed request, source input, and
extra initrd bytes. The production bundle with a synthetic request passed
read-only host admission from the clean integration checkout. A real
journal-derived request and managed-volume export remain unqualified.
The journal-bound capture entry point now holds the exact transaction lock
through request derivation, production bundle admission, export-mode helper
reap, and post-run snapshot rehash. A subprocess fixture proved bound launch
arguments and rejected a changed snapshot without exposing a spool. The
publication entry point re-admits the spool, snapshot, and destination parent;
persists `inspected`; gives only the journal selection to the bounded receiver;
and persists `published` after a complete receiver result. A fixture published
the selected file to a new directory; an extra path and a changed snapshot
left no published tree. These tests use a synthetic helper and stream. An
ambiguous post-rename failure returns the exact final path and leaves the
journal `inspected` for explicit reconciliation; that recovery path remains.
The host now builds a private bundle from exact journal-derived request bytes
under the transaction lock, re-admits it, and returns an inode-bound cleanup
receipt. The real controlled builder invocation from clean source passed and
its synthetic-request output passed production admission. A rejected receiver
stream can be recaptured and retried from `inspected` while the exact
destination remains empty. `workspace export` composes snapshot, build,
capture, selected publication, and a final published-journal read for the
explicit `alpha` domain. Its CLI routing tests pass. The first managed-volume
public command preserved its snapshot and reached the inspector, but guest
ext4 admission rejected a recovery-required filesystem. The host capture had
accepted zero export bytes and the receiver rejected EOF before publication;
the new capture regression rejects that false pass and removes its spool.
Pinned Tart 2.32.1 source confirms that the previous `SIGINT` stop invoked an
immediate VM stop. Normal supervised stop now asks the retained Tart child for
guest OS shutdown, waits up to 15 seconds, and then uses the existing exact
force-stop/reap path if necessary. The first correction alone had not reached
attached-volume sessions because an outer handle wrapper hid that method.
The attached-volume launcher wraps that Tart handle once more to retain disk
locks through reap. That outer wrapper omitted the shutdown-request method,
so attached-volume stops still took the immediate force path. A regression
reproduced the missing method before the fix; the wrapper now forwards the
request while retaining its disk locks until exact reap. Focused Tart,
session-runtime, and supervisor tests plus targeted vet pass. A real public
stop then completed in 3.81 seconds and left the stopped volume ext4-clean.
Three subsequent public starts of the attached synthetic-volume sandbox
returned READY, but the next status reported Tart `stopped`. A bounded live
diagnostic reproduced the contradiction: the exact supervisor and Tart child,
the Virtualization process holding the managed disk, the graphical Tart window,
the guest IP, and SSH port 22 were all present while Tart `list` reported
`stopped`. This matches an earlier Tart 2.32.1 observation on this host. Each
generation was reconciled with the public exact stop; the volume Use was
released, but ext4 still reported `needs_recovery` at that point. Neither
durable READY nor clean guest shutdown was qualified by those earlier runs.
Tart's lock-derived state must be reconciled against exact retained live
evidence before status and READY acceptance can complete. Private process IDs,
paths, and disk evidence remain outside this document.
An independent source review scoped the correction to exact retained-child
liveness plus fresh bound guest checks; it warned that a contradictory Tart
listing never proves a VM stopped. The Tart direct-child handle now exposes a
read-only liveness signal through both scratch and managed-disk wrappers.
The exact owner snapshot uses it with fresh pin, certificate, strict SSH,
workspace, and zone checks and rechecks it after probing. Public status retains
the raw `stopped` listing while reporting READY only with a fresh exact owner;
starting retries check that owner before requesting exact-start admission.
Focused backend, owner, session, and status tests pass. A fresh real sandbox
run is still needed to qualify this behavior and stopped-only recovery paths.
Focused export tests and vet pass. Repository-wide Go tests and vet previously
passed with host Unix socket access at `06466ee`; the default sandbox run
could not bind test sockets, and the architecture guard was narrowed for the
v0.2 export calls. The initial lifecycle correction passed the full local Go
suite, focused Tart/runtime/supervisor race checks, and targeted vet. The outer
managed-volume wrapper correction has only the focused checks recorded above.
Broader managed-volume acceptance remains open. The earlier failed base
attempt was corrected and a fresh base built and qualified; failed attempts
remain outside the prepared cache.

A new session created through the public recipe path from that prepared base
started with the freshly formatted volume attached and initially reported
mount-bound READY. The guest file manager opened the mounted project filesystem
and created the synthetic `alpharetained` directory as the workstation user.
Later, an unsandboxed public status observed the backend still running but
reported exact supervisor readiness drift. A sandboxed Tart read had incorrectly
reported this live VM as stopped, so that read is excluded from qualification.
Public controlled stop reaped the exact VM and cleared volume Use; the stopped
volume's ext4 superblock reported clean state while its recovery-required bit
remained set. The VM and attached volume are preserved as failed qualification
evidence. Neither sustained READY nor clean retained-volume shutdown is claimed
from this attempt. The exact readiness predicate and journal state need
diagnosis before a fresh qualification run.

The status path now reports the exact unproven READY predicates and only
allowlisted internal reasons when a fresh supervisor snapshot fails. A
regression first reproduced the previous generic result, then passed for an
SSH probe failure, an expired observation, and suppression of arbitrary
snapshot text. Targeted app, supervisor, and session-runtime tests and vet
passed. A fresh guest reached READY and remained READY through repeated public
status checks for about eleven minutes, including five minutes idle and a
guest GUI write to its automatically mounted volume. The new failure text
therefore did not trigger during this run. Public stop returned a consistent
stopped state but took 15.47 seconds, matching the bounded graceful window;
offline ext4 retained `needs_recovery`. This does not prove which part of the
shutdown request or guest path failed. The exact stopped VM and volume are
preserved as failed private qualification evidence. Clean stop/restart and
retained-volume rebuild remain unqualified.
Bounded host logs for the failed run showed repetitive Tart AppKit geometry
messages but no explicit VM or disk fault; there was no Tart crash report.
Those observations do not establish the cause of lost readiness or the
recovery-required volume.

A fresh prepared-base clone with a newly public-created ext4 volume provided
a shutdown differential. Its first public stop after initial READY took 3.80
seconds and left ext4 clean. The next generation opened the mounted volume in
Files, created a synthetic directory, and stopped in 3.86 seconds with clean
ext4. A third generation reopened that directory and stayed READY through
repeated public status checks for about eleven minutes. Public stop took 3.92
seconds; status and Tart both reported stopped, and offline ext4 had no
`needs_recovery` flag. A separate no-volume desktop session also stopped in
3.84 seconds. These checks show that a GUI login, a mounted-volume GUI write,
and eleven minutes of uptime are each insufficient to reproduce the earlier
15.47-second dirty stop. The cause remains unproven. An Ubuntu `gsd-rfkill`
crash dialog appeared during the long run while status stayed READY; no
causal link to the stop result was established. All exact VM and volume
ownership records remain private.

The public retained-volume rebuild path then passed a bounded real-host
qualification. A copy-on-write clone of the already qualified generic base
was separately admitted; its fresh qualification clone reached READY with a
different host-key pin and stopped cleanly. The alpha domain's prior default
golden selector was restored afterward. The source session was stopped, its
attachment Available and ext4 clean, and the rebuild journal absent before
the command. The public rebuild completed in 41.49 seconds. It preserved the
session and volume UUIDs, moved the exact volume Use and host-key pin to the
new generation, reported fresh mount-bound READY, retired only the recorded
old VM, and cleared the journal. Guest Files showed the preexisting synthetic
directory and created a second one on the retained volume. Public stop took
3.88 seconds; status and Tart agreed on stopped, Use cleared, and offline ext4
had no `needs_recovery` flag. This qualifies the mechanical cutover and
retention path using the same software image under a distinct base object.
It does not qualify a software-changing upgrade or the intermittent dirty
stop. These were real-host checks; no new source tests or full acceptance
suite were run for this checkpoint.

A fresh public export then qualified the `inspected` hard-exit retry on real
synthetic data. The guest created and SHA-256-hashed a 48 MiB regular file on
the retained volume. Public stop took 3.83 seconds, leaving exact stopped
status and a clean ext4 superblock. The first export invocation refused before
creating a transaction because free space was below its explicit floor plus
3 GiB guard. Clearing only rebuildable Go compilation caches restored
headroom. A bounded second invocation was intentionally terminated only after
its durable journal reached `inspected`, the zero-NIC inspector had reaped,
and the destination was empty. Public resume refused the resulting private
spool. After verifying no running helper, open file handle, or Tart VM, the
spool was moved into private evidence and the orphaned derived bundle was
removed. Public resume of the same transaction published the selected file in
16.09 seconds. Its 50,331,648-byte digest matched the guest; journal phase was
`published`; source and snapshot digests matched; the source inode and FS UUID
were unchanged; ext4 remained clean; and no spool, derived bundle, or running
VM remained. This proves the inspected retry with manual reconciliation of a
surviving spool. It does not qualify an ambiguous post-rename crash. These
were real-host checks; no new source tests or full suite were run for this
checkpoint.

A public stopped-only reattachment now qualifies the exported retained volume
in a fresh recipe-bound sandbox. The source remained stopped; public detach and
attach moved the same 64 MiB disk inode and FS UUID to a new session record.
The replacement started to mount-bound READY. Inside its guest, `/dev/vdb`
was ext4 mounted read-write with `nosuid,nodev`, and the retained
50,331,648-byte file matched the guest and exported SHA-256. Public stop took
3.83 seconds; session status and Tart agreed it was stopped, the attachment
remained bound to the new session with no active Use, and the exact offline
superblock was clean with no `needs_recovery`. No source code or full suite
changed for this real-host qualification. The distinct base object reused an
already qualified software image; software-changing rebuild remains open.

The first full Go-suite run at this checkpoint failed in synthetic export
tests because their production disk-headroom preflight read this host's free
space near the floor plus 3 GiB threshold. The failure occurred before the
small test transactions began; it did not indicate a changed export result.
The public snapshot and publication entry points still bind the real capacity
checks. Their internal test path now supplies a controlled capacity check and
receiver reserve for synthetic state-machine fixtures, and a targeted test
proves capacity refusal leaves no transaction or workspace reservation.
After this correction, `GOTOOLCHAIN=local go test -count=1 ./...`,
`go vet ./...`, a CLI build, and `git diff --check` passed locally. This is a
full source-suite result, separate from the remaining fresh real-host
acceptance run and GUI/provider sign-in checks.

## Next actions

1. Diagnose the intermittent attached-volume readiness loss and 15-second
   dirty stop. The bounded differential and retained-volume rebuild above did
   not reproduce it; preserve failed VM and volume evidence. Qualify a
   software-changing rebuild when disk headroom supports a fresh prepared
   base. The current same-base command intentionally no-ops. A start on the
   older generic r2 clone failed at the guest
   workspace-mount SSH request after the base probe and time-zone steps
   succeeded. The r2 helper digest differs from the prepared
   mount-qualified helper, consistent with the tracked plan's warning that r2
   predates this guest definition. That failed run was controlled-stopped; its
   volume Use was released and the clean volume explicitly detached. Preserve
   the failed r2 VM and the earlier dirty volume as evidence. The separate
   guest SSH loss and false-stopped qualification remain open.
2. Qualify or explicitly defer ambiguous post-final-rename export recovery;
   an occupied final directory still requires manual reconciliation.
3. Implement explicit bounded synthetic host-project ingress; prior
   reattachment and delete qualification used guest-created synthetic files.
4. Run the fresh real acceptance matrix and publish the runnable build, exact
   source SHA, quickstart, synthetic demo, and limits. The full local Go suite,
   vet, and CLI build have passed; the corrected hosted CI run remains pending.

## Publication and operational policy

Publish each meaningful verified increment on the authorized alpha branch,
targeting a GitHub checkpoint every 30–60 minutes of active work. Push each
verified commit promptly and keep Draft PR #12 accurate at milestones. Targeted
checks support small commits; the full integration and acceptance checks are
separate gates. Preserve unfinished work, coordinate before staging another
worker's files, and keep credentials, raw host inventory, VM disks, and private
evidence out of Git and the PR. Never push or merge into `main`, force-update
published history, or bypass checks.

Before further VM mutation, verify owned resources, host doctor, RAM, and
the free-space floor. No human action is currently required; later account
sign-in and subjective GUI acceptance belong to Wes.
