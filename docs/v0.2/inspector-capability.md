# No-NIC inspector capability probe

The controlled-export acceptance gate remains closed. A bounded synthetic
Virtualization.framework guest booted with no NIC, one read-only raw disk, and
separate console/export serial channels. No workspace disk has been attached
to this helper, and no ext4 data has been read.

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
The source and focused tests are verified; a live `boot-copy` run is pending.
The probe still accepts only the private synthetic path. It cannot open a
managed workspace disk or write a host export directory.

## Inspector contract before export can open

Use a separately admitted signed helper and digest-pinned ARM64 kernel and
purpose-built initramfs. Admit only an exact stopped workspace volume under
an exclusive host volume lock. Persist an export-pending transaction before
launch, retain the admitted disk file descriptor and lock through VM stop and
helper process reap, and leave pending state blocking writers if cleanup is
ambiguous. The current workspace record validator does not yet admit an
`export` pending kind; that needs a reviewed transition and start gate. A
Tart stop observation alone does not prove a separate inspector has exited.

Inside the isolated Linux guest, validate the expected whole-device ext4 UUID
and mount with `ro,noload,nodev,nosuid,noexec`. Linux documents that plain
`ro` can replay ext4's journal and write to the disk; `noload` prevents that
replay but may expose an inconsistent view after an unclean stop, which must
fail closed if traversal cannot be trusted. Send only the fixed typed export
stream through a dedicated serial channel to the bounded host receiver. The
host does not mount the guest filesystem or expose a host tree to the guest.
[Linux ext4 mount options](https://cdn.kernel.org/doc/html/latest/admin-guide/ext4.html).

The next qualification must inspect a synthetic ext4 disk, prove the source
remains byte-identical through normal and hostile exits, exercise receiver
limits, and prove lock retention and crash recovery. Only then may the export
acceptance gate be reconsidered.

## Synthetic boot operator gate

Before any `--run`, the sole VM operator should run host-global
`boxwarden doctor`, inventory all VMs and VM processes, verify no conflicting
volume work, and preserve combined VM memory below half the physical host RAM.
The probe itself uses two CPUs, 2 GiB RAM,
one immutable 8 MiB synthetic read-only raw disk, two bounded output-only
serial pipes, and zero NICs, sockets, shares, graphics, or audio. The runner
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
