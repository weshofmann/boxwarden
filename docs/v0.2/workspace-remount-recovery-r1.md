# R1 workspace remount recovery: design and implementation plan

Status: implementation in progress. The first checkpoint implements pinned
APFS identity and duplicate-volume checks, v2 formatter receipts, strict
`workspace_storage` parsing, and external mount admission before public
workspace/start/rebuild/delete dispatch, detached owner launch, and managed
formatter locks. This checkpoint adds explicit `workspace storage enroll`:
it takes an independently supplied APFS UUID, accepts uppercase diskutil
spelling, and writes a new non-overwriting config on a different filesystem.
Legacy reconciliation and final qualification remain pending. A legacy config
can still be used for read-only status and stop/containment, but workspace
operations and session start/rebuild/delete are deliberately refused until an
enrolled config is selected. No installed or user config is changed by this
work.

A host-only probe used a newly created external `0600` synthetic config and
newly owned scratch state root. `hostidentity.CheckStorage` admitted the exact
current DevelData APFS UUID and refused a wrong UUID. No VM or mount changed;
this does not verify behavior across an actual remount.
An additional host-only probe ran the current CLI with a synthetic v1 source
config, an operator-supplied DevelData UUID, a newly owned external state root,
and a private internal output directory. Enrollment wrote and reloaded the new
`0600` config, left the source unchanged, and refused a second attempt to
overwrite the output. The probe used only the current mount and does not prove
remount recovery.
Baseline: `ee3d7a20194d84fa05520f710d2992b6261ac556`.

## Diagnosis and identity model

A verified formatter journal records `(st_dev, st_ino)` when the raw file is
created. The available workspace record copies that pair. `workspaceformat.Admit`
first compares the live pair with the journal; start reservation, supervisor
launch, import verification, export, and deletion then compare the journal pair
with the record. A clean APFS remount can change `st_dev` while the same raw file
retains its file ID and contents. The prior alpha evidence observed exactly that
for two retained workspaces. The focused synthetic regression
`TestFreshVerifiedWorkspaceAdmitsAfterDeviceNumberChanges` changes only the
journal's historical device number. It failed at the expected `Admit`
comparison before the v2 change and now passes with a pinned APFS binding.

`st_dev` is a mount-instance observation. The record's domain, workspace UUID,
filesystem UUID, capacity, and verified formatter journal are durable control
plane authority, but the guest-writable ext4 UUID is not host-file provenance.
The live descriptor/path `SameFile` checks and backend lease remain necessary to
prevent a file switch during one operation.

For supported new APFS workspaces, the durable host binding is the APFS volume
UUID plus its 64-bit permanent object ID, obtained from the **same pinned raw-file
descriptor**. A trusted domain configuration outside the backing filesystem
also names the exact expected mount point and APFS volume UUID. This external
anchor must be checked before any mutating workspace lock or create path: an
identity record stored only on the removable tree disappears with the mount
and cannot authorize creation on its underlying directory. Require the
filesystem to report `apfs`, the volume UUID and
`VOL_CAP_FMT_PERSISTENTOBJECTIDS` / `VOL_CAP_FMT_64BIT_OBJECT_IDS` capabilities.
Apple documents that the persistent-object-ID capability retains IDs across
unmount/remount. Query `ATTR_CMN_OBJPERMANENTID` as a **full uint64** on APFS
and require agreement with `ATTR_CMN_FILEID` and `fstat.st_ino`; never parse
the old 32-bit `fsobj_id_t` member. Fetch volume attributes and common IDs
with separate `fgetattrlist` calls on the same descriptor; validate lengths,
returned fields, nonzero values, and ambiguity. A read-only local APFS probe
confirmed the UUID/capabilities and agreement of all three IDs on this host.
This binding is not a promise that copying, restoring, or physically
replacing an APFS filesystem preserves identity.

Primary references: [Apple persistent volume UUID](https://developer.apple.com/documentation/corefoundation/kcfurlvolumeuuidstringkey),
[Apple `getattrlist` manual](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/getattrlist.2.html),
and [Apple XNU `attr.h` capability and 64-bit ID definitions](https://raw.githubusercontent.com/apple-oss-distributions/xnu/main/bsd/sys/attr.h).

## Proposed transition

1. `workspace_storage` in the selected domain config supplies the exact mount
   point and expected APFS volume UUID. The selected config file must reside on
   a different filesystem from the backing APFS volume. Enrollment is explicit:
   a new CLI operation requires an expected UUID from the operator's prior
   trusted storage inventory, not merely the UUID read from the current mount.
   It checks the supplied value against the live mount
   and writes a new config at an operator-selected, non-overwriting path on a
   different filesystem. It does not adopt whichever volume is currently at
   the path. Legacy configs still parse and support read-only status and
   stop/containment; workspace mutation/start needs an enrolled config. The
   app dispatch boundary checks the anchor before a lock that could create
   under a missing mount. The detached owner repeats the check after loading
   config and before launch admission; `CreateManaged`/formatter and the
   reconciliation operation independently check before their first storage
   lock. Recheck the mount and pinned raw file after the required locks before
   any publication.
2. New formatter journals use version 2. Their verified state atomically adds
   `host_identity` (APFS volume UUID and 64-bit file ID) while retaining the
   original device/inode observation and Linux formatter evidence. Incomplete
   journals never authorize use or reformat. Resolve errors after the verified
   journal rename by exact reread and directory sync; do not let `Create`'s
   deferred failure handler overwrite a verified receipt with `failed` after
   an ambiguous post-rename/sync error. Existing version-1 receipts are
   read unchanged. `Admit` checks the live file against the durable host
   binding for v2; it still checks current descriptor/path equality, owner,
   mode, ACL, one link, size, exact domain/volume/UUID and ext4 header. It
   returns the journal's original device/inode so existing workspace records
   and export transaction source fields remain bound to the original receipt.
3. Version-1 records with changed `st_dev` have no recorded APFS volume UUID,
   so they cannot be automatically rebound safely. Add an explicit
   `workspace reconcile <volume>` operation under the enrolled config.
   It requires the exact verified v1 journal and matching available record,
   the same historical inode, a pinned live file with all existing checks,
   the config's expected APFS UUID and persistent-ID capability, no `Use` or
   `Pending`, the volume-use lock, and a fresh exact stopped-backend check for
   any attachment. It publishes a write-once, private, synced v1 binding
   record that names the original journal identity and its SHA-256, the exact
   workspace fields, and the newly observed APFS UUID/file ID. It never edits
   the historical formatter receipt, workspace data, or ext4 UUID. A missing
   or mismatched mount, duplicate/ambiguous identity, changed inode, unsafe
   path, or unresolved backend remains blocked with an actionable diagnostic.
   Repeating an identical reconciliation returns success after full recheck;
   a conflicting existing binding refuses. A failed write before rename
   leaves no authority; failure after rename is settled by exact reread and
   directory sync.
4. Version-1 journals without a v1 binding keep their existing strict
   device/inode admission when unchanged. Drifted legacy records require the
   explicit operation above; they are never automatically promoted merely
   because path, name, ext4 UUID, or inode matches. A version-1 binding, once
   present, is mandatory and must validate; corrupt or conflicting binding
   cannot fall back to the old rule. Ordinary status stays read-only.
5. Keep the `Use` reservation, lock ordering, fresh backend observation,
   launch lease, snapshot/export recovery guards, and deletion checks. A
   backend `ManagedDisk` captures a **fresh** `fstat` device/inode for the
   lifetime of its lease. Export copy compares its new snapshot to the pinned
   source's current `fstat` identity, not the historical journal identity. A
   remount during an unfinished export transaction retains its existing
   fail-closed snapshot rules; R1 does not silently rewrite snapshot identity.
   A missing backing volume must fail before reconciliation creates any file.

The explicit enrollment transfers the operator's knowledge of the expected
APFS volume UUID into configuration outside the backing storage. The new
configuration is published without overwrite, then reread and compared byte
for byte with the proposed copy before the CLI reports success. If an error
occurs after publication, the new path may remain durable; inspect it and use
a different path for a retry. The legacy receipt cannot supply that UUID. A
legacy inode change, duplicated mounted volume UUID, or absent
trusted expected UUID is an unsupported ambiguous case requiring separate
manual data recovery; it is not eligible for R1 automatic reconciliation.

## Implementation sequence and checks

1. Add capability-gated Darwin host-identity reading from a pinned descriptor,
   with a fail-closed non-Darwin implementation and injected deterministic
   identity source for component tests. Check APFS type and duplicate mounted
   UUIDs before binding/admission; deduplicate aliases of the same mounted
   filesystem. Add config enrollment and prelock guards at the public command,
   detached owner, formatter creation, and legacy reconcile boundaries. Test
   missing/wrong mount causes **no new lock, journal, or raw file**. Test stable identity across simulated device
   drift, wrong volume, substituted file, duplicate ID, unavailable volume,
   malformed capability responses, and unsafe path metadata.
2. Extend the formatter journal decoder/validation and verified transition for
   version 2. Keep v1 bytes and semantics intact. Make the focused regression
   green only after a v2 journal proves the durable host identity. Test a
   replacement file with the same ext4 header and repeated remount drift.
3. Add the narrow v1 reconciliation binding and its workspacex operation and
   public CLI parsing. Use the existing volume-use → session → storage lock
   order and exact stopped observation. Inject failures before/after atomic
   rename and sync; test idempotent retry, active `Use`, unknown/running backend,
   `Pending`, wrong UUID, mismatched original receipt, and no write on missing
   storage. Reconciliation never treats released locks or a missing supervisor
   as stopped-backend evidence. Preserve active stop/containment even when
   storage enrollment is missing.
4. Run package tests, full Go tests/race, vet/build, helper checks and hosted
   CI as applicable; keep public synthetic remount/reattach/export and content
   comparison distinct from ext4-header checks. Only use a newly created
   disposable APFS image if native testing is coordinated and capacity allows.

The correction is intentionally scoped to a clean remount of the same APFS
volume and file. Sudden removal, corruption, power loss, and physical-reconnect
qualification remain separate.
