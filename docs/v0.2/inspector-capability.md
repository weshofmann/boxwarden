# No-NIC inspector capability probe

The controlled-export acceptance gate remains closed. A bounded synthetic
Virtualization.framework guest booted with no NIC, one read-only raw disk, and
separate console/export serial channels. No workspace disk has been attached
to this helper. The later copy probe read ext4 only from an independently
created private synthetic copy.

## Fixed probe surface

The initial `tools/alpha-inspector/main.swift` capability proof used `probe`,
`preflight`, and `boot-probe` commands against a synthetic 8 MiB `synthetic.raw`
in a unique `/private/tmp` probe directory. Only `boot-probe` started a VM.
The helper constructs
a generic ARM64 Linux configuration with two CPUs, 2 GiB RAM, a direct kernel
and initramfs boot loader, exactly one Virtio block disk using
`VZDiskImageStorageDeviceAttachment(url:readOnly: true)`, and two Virtio serial
ports intended for bounded console diagnostics and typed export data. Network,
socket, directory-sharing, graphics, and audio device arrays are explicitly
empty. `probe` and `preflight` validate the configuration, instantiate a VM
object, check the runtime network-device count and stopped state, and exit.
`preflight` also binds a fresh 16-byte transaction into `rdinit=/alpha-probe`.

The reproducible `probe_test.sh` verifies the pinned Ubuntu 24.04.4 ARM64 ISO
SHA-256, extracts only `casper/vmlinuz` and `casper/initrd` into private
temporary storage without mounting the ISO, creates a sparse synthetic raw
disk, compiles the Swift helper, ad-hoc signs it with only
`com.apple.security.virtualization`, verifies the signature, runs `probe`,
checks each JSON evidence field, and removes the temporary tree.

The separate `prepare_boot_probe.sh` verifies the same ISO, builds a static
ARM64 guest PID 1, appends it as a new cpio member to the exact extracted initrd,
and unpacks the exact gzip `casper/vmlinuz` into a bounded, digest-pinned ARM64
Linux `kernel-image`. The unpacker checks the ARM64 Image magic at offset
`0x38` and rejects trailing gzip data. The runner rechecks the pinned source
and Image hashes and proves that the Image is the decoded source. The script
creates an unrelated 8 MiB sparse synthetic disk, compiles/signs the helper,
and runs **only** VZ `preflight`. Its private manifest records exact SHA-256
digests and disk device/inode/size. `run_boot_probe.py --preflight-only` checks
that manifest, regular one-link files and private owner/modes, the signature,
free-space floor, physical memory, and a fresh VZ validation. It prints the
exact launch argv but does not invoke it. The script's explicit `--run` mode
is reserved for the sole VM operator's coordinated synthetic test. The guest
expects `/dev/vda` to be read-only and only `lo` in `/sys/class/net`, reads
4096 zero bytes from the disk, and sends one bounded `report.json` over hvc1
using the BWEX v1 typed stream. hvc0 is drained separately. This proves only
fixed serial transport and read-only synthetic block attachment if it succeeds;
it does not parse ext4 or accept real exports.

Linux documents concatenated compressed and uncompressed initramfs cpio members
and the `rdinit=` selector: [initramfs buffer format](https://docs.kernel.org/driver-api/early-userspace/buffer-format.html),
[kernel parameters](https://www.kernel.org/doc/html/v5.5/admin-guide/kernel-parameters.html).
Apple's [direct Linux boot guide](https://developer.apple.com/documentation/virtualization/creating-and-running-a-linux-virtual-machine)
describes unpacking compressed kernel images before use with
`VZLinuxBootLoader`.

## Alpha host capability evidence, 2026-09-23

- Sandboxed execution compiled and signed, but `configuration.validate()`
  returned `VZErrorDomain Code=2`, “Virtualization is not available on this
  hardware.” The same probe with host access returned exit 0. The sandboxed
  error therefore does not establish a hardware limitation.
- Host-access result: `validated=true`, `network_devices=0`,
  `runtime_network_devices=0`, `storage_devices=1`,
  `storage_read_only=true`, `serial_ports=2`, `socket_devices=0`,
  `shared_directory_devices=0`, `vm_state=stopped`. `codesign --verify
  --strict` also returned exit 0.
- The first bounded synthetic boot used the ISO's gzip `casper/vmlinuz`
  directly. VZ start failed with an internal error before any guest output;
  the synthetic disk was unchanged and the helper was reaped. Structural
  preflight had accepted the configuration, so validation alone did not prove
  the kernel was bootable. The failed probe bundle remains immutable evidence.
- A fresh bounded probe used the exact uncompressed ARM64 Image from that
  pinned gzip source. The VM booted and stopped. Host runtime reported zero
  NICs, a stopped VM, 4,226 drained console bytes, and a 355-byte typed export
  stream. The guest reported only loopback, `/sys/block/vda/ro=1`, and the
  expected zero-filled disk prefix. The transaction-bound stream parsed with
  its exact digest and terminal record. The helper was reaped; the synthetic
  disk retained its device/inode/size and SHA-256; no inspector process remained.

This is observed synthetic offline, read-only block, and serial transport
behavior, not a qualified ext4 inspector or a real workspace export.
Apple's API documents that a VM's network
device list is empty when none are configured, that the raw disk attachment
can be read-only, and that a process needs the virtualization entitlement:
[configuration network devices](https://developer.apple.com/documentation/virtualization/vzvirtualmachineconfiguration/networkdevices),
[runtime network devices](https://developer.apple.com/documentation/virtualization/vzvirtualmachine/networkdevices),
[read-only raw disk](https://developer.apple.com/documentation/virtualization/vzdiskimagestoragedeviceattachment/init%28url%3Areadonly%3A%29-9qeco),
[entitlement](https://developer.apple.com/documentation/virtualization/adding-the-virtualization-entitlement-to-your-project).

## Synthetic ext4 copy probe

The source now also has `preflight-copy` and `boot-copy` for an exact 64 MiB
private `synthetic.raw`. Preparation accepts a one-link private synthetic copy
with a pinned whole-disk SHA-256 and expected ext4 UUID. Its manifest binds
the expected SHA-256 and length of the fixed
`boxwarden-alpha-synthetic.txt` file. The guest confirms one read-only Virtio
disk, only loopback, a clean whole-device ext4 superblock, and a mount with
`ro,noload,nodev,nosuid,noexec`. It opens that fixed file without following a
final symlink, reads at most 4 KiB, and reports only its digest and size over
the bounded typed serial channel. The host requires an exact matching report,
unchanged disk digest and identity, stopped VM evidence, and a reaped helper.
The standalone `preflight-copy` and `boot-copy` commands also require an exact
private `/private/tmp/boxwarden-inspector-boot.*` directory and an
operator-owned, private, one-link `synthetic.raw`. This rejects a hardlink to
a managed workspace inode. The path check uses the supplied lexical path
because Foundation can alias `/private/tmp` to `/tmp` during normalization.
`run_boot_probe.py` is the sole supported launcher:
it binds the whole-disk digest, ext4 UUID, fixed-file expectation, and post-run
disk identity. The source, focused tests, and a live `boot-copy` run are
verified. This synthetic probe cannot name a managed workspace path or
write a host export directory.

The first live copy boot reached guest shutdown with zero NICs, a stopped
helper, and an unchanged synthetic disk, but the host rejected its binary
report digest. The captured 574-byte stream differed from the expected
573-byte BWEX stream by one CR inserted before an LF inside the digest.
Linux terminal output processing maps LF to CR-LF when `OPOST` and `ONLCR`
are active. The guest now disables `OPOST` on its dedicated hvc1 data port
and verifies the setting before writing binary frames. The failed run is
retained as private evidence. A fresh bundle built from the corrected source
passed live: the host accepted the exact 573-byte typed stream, observed zero
runtime NICs and a stopped VM, and reaped the helper. The guest reported only
loopback, read-only block and ext4 mount state with
`ro,noload,nodev,nosuid,noexec`, the bound filesystem UUID, and the known
57-byte file digest. The private disk SHA-256 and inode stayed unchanged.
The strict host parser remains unchanged.
[Linux termios output flags](https://www.man7.org/linux/man-pages/man3/termios.3type.html).

## Inspector contract before export can open

The controlled export will give the inspector a stable private disk copy,
not the managed volume inode. Alpha admission caps the source volume at 1 GiB,
returned file content at 256 MiB, and the captured typed stream at 320 MiB;
it requires at least 3 GiB free above the normal host reserve before copying
and samples the reserve throughout. The copy has a five-minute deadline. The
inspector retains its 80-second outer deadline. An over-limit request fails
before reserving the volume. These limits keep a full-size copy plus stream
spool and receiver staging within a bounded host footprint; they are alpha
policy, not workspace-format limits. The copy deadline is checked between
bounded local-file I/O chunks and during source/copy hashing. A blocked host
filesystem call itself is not interruptible by Go context cancellation; a
stalled host filesystem can hold the locks past the nominal deadline and must
be diagnosed as host I/O failure, not a completed export.

The host first acquires the exact volume-use lock, then the attached session
and domain storage locks in that order. It requires Stopped intent and a fresh
stopped observation of the exact backend, no Use or other Pending marker, and
the qualified volume identity and ext4 header. It opens the exact source with
`workspaceformat.Admit` and retains that descriptor. Before copying, it
durably creates a private export transaction journal keyed by a fresh UUID and
persists the same ID as an `export-snapshot` Pending marker on the volume.
The journal binds domain, volume, attached session and backend, selected paths,
the intended destination parent identity and deterministic final name, the
private snapshot path `exports/<transaction-id>/snapshot.raw` relative to the
domain state root, and phase. Journal phase changes are serialized by an exact
transaction lock. The host copies through the pinned descriptor while holding
the volume lock, checks deadline and reserve during the copy, then verifies
source identity/content and a distinct one-link snapshot inode, exact length,
and SHA-256. It fsyncs snapshot bytes and its directory before atomically
publishing the snapshot identity/digest and `snapshot-ready` phase in the
journal. Only after that durable journal update may it clear the exact Pending
marker and release the volume lease. The journal remains live after Pending
clears, so the original volume may safely restart while inspection continues.

A crash during copying leaves Pending and an untrusted partial copy. The
snapshot-stage recovery implementation reacquires the volume, session, and
storage locks and rechecks the exact session/volume binding, stopped backend,
source identity, and transaction journal. It rejects unexpected transaction
directory entries, removes only the recognized partial copy, durably records
`aborted`, then clears the exact Pending marker. A retry after cleanup but
before the abort journal update repeats safely; a retry after the journal
update clears a surviving marker. A `snapshot-ready` retry rehashes the exact
private copy before clearing Pending. These transitions have targeted source
tests; no real managed-volume recovery has passed yet. Copying is in-process
under the volume lock; an absent PID or lockfile alone proves nothing.
Ambiguous ownership or backend state keeps Pending and requires explicit
reconciliation. The inspector is never launched before Pending clears. After
that transition, recovery uses the journal: it may remove a
snapshot only after proving any inspector stopped and reaped, and never removes
a possibly published destination. A crash around receiver rename is resolved
by inspecting the exact final name and parent identity; ambiguity is reported,
not overwritten or silently called success.

A separately admitted signed helper and digest-pinned ARM64 kernel and
purpose-built initramfs then read only the private snapshot. The host rechecks
its one-link inode, size, and digest immediately before VM launch and after VM
stop/helper reap; `0400` mode alone does not make same-UID bytes immutable.
The helper cannot name a managed workspace path. The bounded typed serial
stream is captured privately first. The host proves zero runtime NICs, VM
stop, helper reap, and unchanged snapshot identity/content before giving that
closed stream to `exportx.Receive`. The receiver can then validate terminal
and EOF and publish into the pinned empty destination parent. Streaming the
live inspector directly into the receiver would publish before those host
checks and is forbidden. The journal advances through inspection and
publication, remains durable through cleanup, and never treats a staged
receiver directory as a completed export.

The host capture gate is implemented independently of the production helper.
It runs an already-admitted executable with exact argv and a closed environment,
spools at most 320 MiB of stdout into a private one-link file, bounds stderr
at 16 KiB, and retains an 80-second process deadline. It samples the host disk
reserve during capture and exposes the spool only after the helper has exited
and been reaped, its host evidence strictly reports stopped VM, zero runtime
NICs, and the exact captured byte count, and the spool and parent pass owner,
mode, identity, and ACL checks. An inode-bound cleanup method removes the
spool after receiver disposition. Focused tests use subprocess fixtures; this
gate does not yet admit production helper artifacts or recheck the snapshot
after VM stop, so it cannot currently authorize publication.

The generic guest stream writer now walks selected paths beneath an opened
root, rejects symlinks, hardlinked regular files, unsupported object types,
unsafe/colliding names, and excessive directory entries, and enforces the
256 MiB file/content and 4096 file/directory alpha limits. It emits the BWEX
v1 records accepted by `exportx.Receive`; a local round-trip test verifies
the selected subtree and omission of an unselected sibling. This writer is
not yet invoked by the booting guest. Request transport, production artifact
admission, and complete host publication checks remain open.

Inside the isolated Linux guest, validate the expected whole-device ext4 UUID
and mount with `ro,noload,nodev,nosuid,noexec`. Linux documents that plain
`ro` can replay ext4's journal and write to the disk; `noload` prevents that
replay but may expose an inconsistent view after an unclean stop, which must
fail closed if traversal cannot be trusted. Send only the fixed typed export
stream through a dedicated serial channel to the bounded host receiver. The
host does not mount the guest filesystem or expose a host tree to the guest.
[Linux ext4 mount options](https://cdn.kernel.org/doc/html/latest/admin-guide/ext4.html).

The synthetic ext4 copy passed a normal bounded boot and remained
byte-identical. The next qualification must exercise hostile exits, receiver
limits, snapshot publication, managed-volume lock and Pending transitions,
and crash recovery. Only then may the export acceptance gate be reconsidered.

## Synthetic boot operator gate

Before any `--run`, the sole VM operator should run host-global
`boxwarden doctor`, inventory all VMs and VM processes, verify no conflicting
volume work, and preserve combined VM memory below half the physical host RAM.
The probe itself uses two CPUs, 2 GiB RAM,
one immutable 8 MiB zero or 64 MiB ext4 synthetic read-only raw disk, two
bounded output-only serial pipes, and zero NICs, sockets, shares, graphics,
or audio. The runner
requires free space above `max(20 GiB, 10% filesystem capacity) + 64 MiB`.
The host helper waits up to 15 seconds for start, 30 seconds for guest stop,
then performs a bounded request/force-stop sequence; the launcher has an
80-second outer deadline and reaps the helper. Output caps are 256 KiB console
(discarded), 64 KiB export stream, and 16 KiB host log. Its postchecks require
stopped-state evidence, no runtime NICs, exact typed stream transaction and
terminal record, only loopback in the guest report, and unchanged synthetic
disk identity/content. A timeout or missing stop evidence is failure and leaves
VM cleanup **ambiguous** for operator inspection; it cannot open export.

The operator runs `python3 tools/alpha-inspector/run_boot_probe.py
--preflight-only <prepared-directory>` first, then only after coordination
`python3 tools/alpha-inspector/run_boot_probe.py --run
<prepared-directory>`. The private directory holds source extraction,
compiled executables, manifest, and any boot result. It can be removed after
evidence review when the helper is reaped and no VZ instance remains. No
workspace or VM-owned path is in that directory.
