# Independent workspace disks: alpha contract

Status: design reviewed on 2026-09-23. Volume records, formatter journal, and
an unwired backend attachment lease exist; lifecycle integration and real VM
proof remain pending. The existing v0.1 session record is not a workspace
registry.

## Ownership model

One domain-owned workspace record is the durable attachment authority. It
contains the volume UUID, capacity, whole-device ext4 filesystem UUID, fixed
`raw-ext4` format, exact file identity, state, optional attachment to a stable
session UUID and mount path, optional active use reservation, and optional
incomplete operation. The disk path is derived from its volume UUID inside the
private domain state root. Guest output never supplies a host disk path.

Session JSON does not duplicate the volume attachment list. A bounded registry
scan under the storage lock resolves attachments for a session launch. A
rebuild preserves the session UUID while replacing its system backend object,
so workspace association survives without a transfer between records.

## Locks and admission

Acquire session operation locks first, in stable name order, then the domain
storage operation lock, then the golden lock if needed. Volume-only operations
start with storage. Acquire volume-use locks in UUID order and hold them for the
entire VM/inspector/formatter lifetime through exact stop, wait, reap, and
backend observation. A released lock after a supervisor crash does not prove
the old backend stopped: a durable use reservation and backend observation
must agree before reassignment. Unknown or running state blocks reuse.

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

No public session launch supplies these leases yet. Common lifecycle code must
coordinate session/storage/use locking, compare the exact `Use` reservation,
attachment, disk identity, and verified formatter journal, then transfer the
lease to the backend. Current volume transitions do not acquire volume-use
locks, so this coordination needs an explicit lock-order design before wiring.
The host file descriptor pins identity for admission, but Tart opens the path
itself; trusted-host code must recheck immediately before launch. A malicious
same-UID host process racing after the check is outside the guest-root boundary.

## Lifecycle

| Action | Durable transition and gate |
| --- | --- |
| Create | Reserve name, volume UUID and filesystem UUID, then exclusively create a new sparse raw file. Format only this new file in a fresh trusted formatter guest; verify before marking available. An interrupted format stays failed, never silently reformatted. |
| Import | Explicitly select a host project and ingest it into a new managed volume through a bounded transfer. Record a relative-path size/digest baseline; never reimport during attach or restart. Arbitrary existing disk-image adoption is unsupported initially. |
| Attach/detach | Require both volume and target sandbox observed stopped, no active use, then atomically set/clear the sole attachment. One writable owner is enforced before any launch. |
| Start/stop | Persist exact use reservations before launch; the supervisor independently validates and holds volume locks until the exact Tart handle is stopped and reaped. Mount by unique filesystem UUID at the recipe path; missing/duplicate UUID prevents workspace-ready. Stop clears use but keeps attachment. |
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
