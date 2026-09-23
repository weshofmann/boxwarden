# Synthetic ext4 inspector fixture contract

This source increment defines an exact fixture and read-only inspection path.
It has **not** created or booted an ext4 image. The controlled-export gate
remains closed.

`python3 tools/alpha-inspector/fixture_contract.py --request` prints the fixed
formatter request: one new 64 MiB whole-device ext4 image with UUID
`2f1c6b88-9849-4c5d-9d20-f3bc30bd77a1`, a journal and extents, and one
regular root file `proof.txt` containing exactly
`boxwarden synthetic ext4 proof v1\n`. These inputs are deterministic. Image
bytes are not claimed deterministic until a qualified formatter and its exact
toolchain/configuration have been admitted and a repeat-build comparison has
passed.

The pending adapter should use the existing trusted Linux formatter design in
`internal/workspaceformat`: attach only a fresh private synthetic raw file,
prove its exact device/inode/size and marker before formatting, invoke
whole-device `mkfs.ext4` with the requested UUID, write the one proof file in
the isolated guest, cleanly unmount, run independent `e2fsck`, then stop and
reap that formatter VM. Its evidence must bind the exact file identity, UUID,
file digest, and final image SHA-256. No host formatter, host mount, or live
host tree is part of this contract. The adapter is not implemented here.

Once that adapter exists, place the candidate at a private, owner-only
`/private/tmp/boxwarden-inspector-fixture.*/synthetic-ext4.raw` path, mode
`0600`, with one link. Supply that path and its final SHA-256 as
`BOXWARDEN_ALPHA_EXT4_FIXTURE_SOURCE` and
`BOXWARDEN_ALPHA_EXT4_FIXTURE_SHA256` to `prepare_boot_probe.sh`. Preparation
copies only this bounded candidate into a fresh private probe bundle and
performs structural magic/UUID/journal/extents/clean-state checks, followed by
the existing signed VZ dry preflight. Structural checks do not prove that
`proof.txt` exists or that ext4 can mount. The helper uses explicit
`preflight-ext4` and `boot-ext4` commands, separate from the prior zero-disk
probe.

The future sole-operator runtime gate is: host doctor and VM inventory;
formatter evidence and final image digest review; no-NIC VZ dry preflight;
then one bounded read-only ext4 boot. The guest requires a read-only block
device, exact 64 MiB size and ext4 identity, mounts with
`ro,noload,nodev,nosuid,noexec`, checks effective readonly/nodev/nosuid/noexec
flags, rejects symlinks and altered content for `proof.txt`, and emits a typed
transaction-bound report. The host requires stopped/no-NIC/reaped evidence,
exact report content and terminal record, and unchanged image identity and
SHA-256. Any failure leaves controlled export closed. The runtime mount,
fixture producer, and malicious-image tests remain to be qualified.

Linux documents `noload` as the option that prevents ext4 journal replay on a
read-only mount: [ext4 mount options](https://www.kernel.org/doc/html/latest/admin-guide/ext4.html).
