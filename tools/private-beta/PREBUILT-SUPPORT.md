Boxwarden packaged Linux support provenance

Build inputs are the unchanged Ubuntu 24.04.4 Desktop ARM64 ISO
https://cdimage.ubuntu.com/releases/24.04.4/release/ubuntu-24.04.4-desktop-arm64.iso
SHA256 c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe
and e2fsck-static 1.47.0-2.4~exp1ubuntu4.1 arm64:
https://launchpad.net/ubuntu/+source/e2fsprogs/1.47.0-2.4~exp1ubuntu4.1
package SHA256 0a3d402fd7c7c07f63b8104d84a47378f57022e7c816190e6cc99950492d8cae
executable SHA256 e5e8f8b30641fab6e3f402c9746ce0537ad95c528e96cde2e446ca0968e63279

Kernel provenance: the ISO casper/vmlinuz SHA256 is
000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617.
The deterministic decompressed ARM64 Image SHA256 is
a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d.
Linux is GPL-2.0-only with the Linux-syscall-note exception:
https://www.kernel.org/doc/html/latest/process/license-rules.html
Ubuntu Linux source packages and their included license texts:
https://launchpad.net/ubuntu/+source/linux
https://launchpad.net/ubuntu/+source/linux-hwe-6.17

The unchanged casper/initrd is an Ubuntu userspace aggregate. It retains its
original bytes as the prefix of each Boxwarden initramfs. Components retain
their individual upstream licenses and notices. Ubuntu source packages:
https://archive.ubuntu.com/ubuntu/pool/main/
Canonical's intellectual property policy:
https://ubuntu.com/legal/intellectual-property-policy
This software is not endorsed by Canonical. Boxwarden is a separate application.

The static checker is unmodified. Its original package copyright is included
as e2fsprogs-copyright. e2fsprogs combines GPL-2, LGPL-2 and other component
licenses; do not substitute a single license for its package copyright:
https://git.kernel.org/pub/scm/fs/ext2/e2fsprogs.git/tree/debian/copyright
https://launchpad.net/ubuntu/+source/e2fsprogs/1.47.0-2.4~exp1ubuntu4.1

Boxwarden Go guest helpers and source: Apache-2.0 (LICENSE/NOTICE), with Go
runtime notices retained alongside these notes. The Swift runners dynamically
use macOS frameworks; these system frameworks are not redistributed.

The package resource manifest records the exact clean Boxwarden source commit,
complete selected source-input digest inventory, all binary digests, and sole
Virtualization.framework entitlements. Build scripts never boot a VM.

Distribution gate: these provenance links and notices are not a substitute for
corresponding-source obligations. Before external redistribution, retain the
exact kernel/initrd component source/license inventory and satisfy each
applicable source provision. This local prototype packaging change does not
claim that this distribution audit has been completed.
