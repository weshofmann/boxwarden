# Independent workspace disks: alpha contract

Status: design reviewed on 2026-09-23. Volume records, a bounded strict
attachment registry reader, a formatter journal-to-record promotion, and
child-side exact-generation lease admission exist. Parent-side batch Use
reservation and stop-side release are wired; real VM proof remains pending. The
existing v0.1 session record is not a workspace registry.

## Ownership model

One domain-owned workspace record is the durable attachment authority. It
contains the volume UUID, capacity, whole-device ext4 filesystem UUID, fixed
`raw-ext4` format, exact file identity, state, optional attachment to a stable
session UUID and mount path, optional active use reservation, and optional
incomplete operation. The disk path is derived from its volume UUID inside the
private domain state root. Guest output never supplies a host disk path.

Session JSON does not duplicate the volume attachment list. A bounded registry
scan under the storage lock resolves attachments for a session launch. It
rejects corrupt records, mismatched session name/UUID, duplicate filesystem
UUIDs, overlapping mount paths, and more than four attachments. A
rebuild preserves the session UUID while replacing its system backend object,
so workspace association survives without a transfer between records.
The Attach transition consults that same registry under its held session and
storage locks, so a fifth or duplicate-filesystem attachment is refused before
the new binding is persisted.

The source-only `PromoteVerified` transition makes a creating record available
only after a fresh `workspaceformat.Admit` proves the exact verified journal,
ext4 header, raw-file inode, and capacity. If the atomic record rename succeeds
but later sync reports an error, an exact retry re-admits that same file and
fsyncs the record directory before returning success. The trusted Linux
formatter VM adapter exists in source; managed-volume health proof is pending. A
signed exploratory formatter runner has completed one fresh synthetic no-NIC
ext4 boot, clean check, VM stop, runner reap, and host header verification.
The production runner's managed mode binds exactly one domain and state root
in its signed binary. The Go adapter independently matches that binding to
the configured root and exact `FormatRequest`, verifies private bundle digests,
signature and sole entitlement, and admits the result only after child reap,
stopped-VM report, fresh journal/raw identity, and ext4 header checks. Its
read-only admission of the real bound bundle passed; no managed raw has booted.
The pinned Ubuntu 24.04.4 ARM64 Desktop initrd has `mkfs.ext4` and `blkid`
but omits `e2fsck`. Formatter boot preparation verifies the exact ISO and
Canonical's matching ARM64 `e2fsck-static` package before appending only the
fixed guest helper and checker to the unmodified initrd. The checker is an
external, digest-pinned private build input, not a repository binary. The
guest's report must be accepted only after a no-NIC, single writable-file VM
stops and its exact host process is reaped; source preparation alone is not
format qualification.

## Locks and admission

Public session start and stop hold an outer per-session transition lock across
their long supervisor operation. Within that gate, operations without a
volume-use lock acquire session operation locks first, in stable name order,
then the domain storage operation lock, then the golden lock if needed.
Volume-only record operations start with storage. Any operation that needs a
volume-use lock acquires those locks first, in volume UUID order, then session
locks in stable name order, then storage. Never wait for a volume-use lock
while holding a session or storage lock: the retained backend lease holds
it while stop and use-release may need session and storage locks. Hold each
volume-use lock for the entire VM/inspector/formatter lifetime through exact
stop, wait, and reap. A released lock after a supervisor crash does not prove
the old backend stopped: the durable use reservation and a fresh exact backend
observation must agree before reassignment. Unknown or running state blocks
reuse.

Before launch, validate the exact configured domain, record and UUID bindings,
private ancestry, opened regular one-link disk file, expected owner/mode,
size and device/inode identity. Reject symlink/hard-link replacement, unresolved
operations, duplicate volume IDs or overlapping guest mount paths. The
backend accepts a bounded typed list of managed disk files; it does not accept
arbitrary `--disk` operands or host devices from a guest or recipe.

The backend transport now accepts at most four one-use managed raw-file leases.
Every lease in one launch must name the same state root and security domain.
Each lease binds a UUID-derived path below a private state root, one opened
regular 0600 file with exact device/inode/length, no extended ACL, the live
`volume-<domain>-<volume UUID>` advisory lock under the same exact state root
(including the root, lock-directory, and lock-file identities, owner-private
modes, and absence of ACLs), and one Tart object/generation.
The path cannot contain `:`, which Tart 2.32.1 parses as an option delimiter.
The launcher passes `--disk` and that path as separate argv elements, with no
sync or read-only option; this is the intended writable workspace attachment.
The retained handle holds the file and lock through exact Tart process reap,
including failed launches that returned a handle. Spawn failure closes both.
Canceled or unverifiable waits retain them. The durable `Use` reservation still
requires a later backend-stopped observation before it can be cleared.

The supervisor child now supplies these leases when exact durable Uses exist.
Common lifecycle now reserves Uses as a batch before public launch.
The child coordinates volume-use/session/storage locking, compares the exact
`Use` reservation, attachment, disk identity, and verified formatter journal,
then transfers the lease to the backend. The session record must already say
`starting` with the same exact generation and backend object as the durable
`Use`; the backend object must be freshly observed stopped before Tart starts.
The formatter journal must be verified for the same domain, volume UUID,
filesystem UUID, size, and raw file device/inode. The host rechecks the private
derived path and open file before handing both it and the live volume-use lock
to the backend. Failure releases the file and lock but leaves the durable `Use`
for exact stopped-state reconciliation.

The session-lock handoff releases the session lock during
`Supervisor.StartExact`, allowing child admission to acquire volume-use, then
session, then storage. The lifecycle transaction has these phases:

1. Discover candidate attachments under storage, release it, acquire their
   volume-use locks (UUID order), then the session lock, then storage. Re-scan
   and reject any changed binding. Persist and sync every exact `Use`
   reservation before the session `starting` generation, which is the batch
   launch commit marker. No launch may precede that marker. A partial set of
   Uses with a still-stopped session remains fail-closed until exact stopped
   observation permits reconciliation. Release storage, session, and
   volume-use locks before waiting for the child. The durable records, not the
   interval between lock owners, carry authority across this handoff.
2. The exact-generation supervisor child reacquires volume-use locks, then
   session and storage. It reloads both records, checks their exact session
   UUID/backend object/generation binding and the verified formatter journal,
   and freshly observes the backend stopped. Any intervening stop, rebind,
   failed journal, or changed file aborts launch. It releases session and
   storage locks after validation, retaining volume-use/file leases through
   backend start and exact process reap.
3. A concurrent stop first persists stopping intent without waiting for a
   volume-use lock while holding session/storage. It asks the exact supervisor
   owner to stop and waits for exact reap, which releases the leases. Only then
   may batch use-release acquire volume-use, session, and storage locks in
   order, freshly observe the exact backend stopped, and clear the matching
   durable Uses. Persist session `stopped` last. A crashed owner leaves `Use`
   set until that same observation and reap proof succeeds.

Session start/stop now release their session lock before waiting for the
supervisor, then reacquire it and recheck the exact durable generation. Their
outer transition lock serializes public starts and stops, including a
poisoned-serial recovery stop, through the handoff. Parent batch reservation
and child-side lease admission are wired. Stop keeps `Stopping` while the
supervisor stops and reaps, then clears matching Uses under volume-first locks
before persisting `Stopped`. An interrupted release retains `Stopping` and
retries only the remaining exact Uses. `PrepareSessionStart` reserves `Use`
while the session is still durably stopped, before persisting `Starting`.
A failed intermediate write retains any partial Uses; a later stopped-state
start or stop clears them only after a fresh exact backend-stopped observation.
`ReserveUse` deliberately rejects an already starting session.
The host file descriptor pins identity for admission, but Tart opens the path
itself; trusted-host code must recheck immediately before launch. A malicious
same-UID host process racing after the check is outside the guest-root boundary.

## Lifecycle

| Action | Durable transition and gate |
| --- | --- |
| Create | Reserve name, volume UUID and filesystem UUID, then exclusively create a new sparse raw file. Format only this new file in a fresh trusted formatter guest; verify before marking available. An interrupted format stays failed, never silently reformatted. |
| Import | Explicitly select a host project and ingest it into a new managed volume through a bounded transfer. Record a relative-path size/digest baseline; never reimport during attach or restart. Arbitrary existing disk-image adoption is unsupported initially. |
| Attach/detach | Require both volume and target sandbox observed stopped, no active use, then atomically set/clear the sole attachment. One writable owner is enforced before any launch. |
| Start/stop | Persist exact use reservations before launch; the supervisor independently validates and holds volume locks until the exact Tart handle is stopped and reaped. Mount by unique filesystem UUID at the recipe path; missing/duplicate UUID prevents workspace-ready. Normal stop asks the retained Tart child to request guest OS shutdown and waits a bounded 15 seconds before exact process-group SIGINT fallback. The fallback contains an unresponsive guest but may leave ext4 requiring recovery; export still requires a clean filesystem and never replays its journal. Stop clears use but keeps attachment. |
| Rebuild | Persist old and replacement system object IDs in a session rebuild journal. Clone and validate the replacement without workspaces where possible, stop it, then switch the session backend while retaining the session UUID and volume records. Delete the old system only after successful validation and an explicit persisted phase. Workspace data itself is never rolled back. |
| Sandbox delete | Stop/prove stopped, clear attachments, delete only the exact system object, and retain all workspace files, records and import baselines. |
| Replacement attach | Explicitly detach from stopped A, then attach to stopped B. Failure between the two leaves an unattached surviving volume. |
| Volume delete | Separate explicit command requiring no attachment/use and exact file admission; never a side effect of sandbox or cache cleanup. |

Whole-device ext4 avoids partition-table probing. The filesystem root is given
the standard workstation UID/GID only when first created. Reattachment never
recursively changes file ownership. Guest root may alter filesystem metadata,
so UUID and mount checks establish operational identity, not guest honesty.

Tests must cover competing attach/start, supervisor death with a still-running
backend, interrupted durable phases, file replacement, missing/duplicate
filesystem UUID, unchanged volume bytes after sandbox deletion, and exact
child-observed Tart argv boundaries for paths containing spaces. Real synthetic
acceptance follows the sequence in `alpha-plan.md`.
