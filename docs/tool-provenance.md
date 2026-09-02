# Tool provenance and updates

The portable guest-definition lock records guest OS, architecture, and libc where relevant. Every guest package/application records exact version, platform, source URL/repository identity, signing-key fingerprint, artifact digest/checksum, verification method, install method, reason for inclusion, and update owner. A host-specific golden artifact additionally records host/backend type and version, guest-definition digest, build evidence, and final installed package/application BOM. For M1A, Tart and Softnet are one security-critical host toolchain: Task 0 records both exact versions, installation/source identities, relevant artifact/package identities where practical, and the network behaviors empirically qualified for that exact pair. Neither component updates automatically; changing either requires deliberate requalification before its behavior is trusted. Undocumented manual mutation invalidates the artifact: any necessary change must become guest-definition input and survive a clean rebuild.

Softnet 0.19.0's tested execution path requires host root privilege. The
privilege grant is therefore part of the qualified host-toolchain identity: it
must authorize the exact verified Softnet artifact and relevant execution
dependencies without allowing an unprivileged user to replace or mutate what
root will execute. A mutable, user-writable Homebrew path is not an acceptable
standing passwordless-root target merely because Homebrew installed Softnet
there. The eventual implementation may use a root-owned qualified copy, a
sufficiently constrained command/digest authorization, or another narrow
helper, but Task 0 does not select among them. Replacing or upgrading Softnet
invalidates both its behavioral qualification and its privileged binding until
they are deliberately re-established.

The workstation uses first-party official distributions for ChatGPT Desktop, Claude Desktop/Code, Antigravity, Grok Build, Codex, Chrome, Docker, and language toolchains. Prefer official native ARM64 packages/binaries over npm wrappers when functionality is equivalent.

Before installation, each vendor tool passes a qualification gate that proves an immutable or reproducibly identified ARM64 artifact path and verifies its signature/checksum. The build never executes a mutable piped installer. If an optional tool cannot meet the gate, the lock records it as unavailable for that golden revision; the provenance policy is not weakened.

The lock distinguishes two claims:

- a reproducibly identified artifact has an exact version, platform, source identity, and verified digest/signature;
- an indefinitely reproducible repository closure additionally retains or references an immutable snapshot of every dependency and signed repository metadata.

M1A requires the first claim for every input and uses repository snapshots or a verified retained package closure where practical. When a third-party repository cannot guarantee indefinite retention, the lock records that limitation explicitly instead of claiming bit-for-bit rebuildability. Final BOM, repository metadata, package files retained for rebuild, and source snapshots establish the best available closure.

APT packages are installed at exact versions and held, but holds are update controls rather than rebuild provenance. Unattended upgrades, PackageKit/GNOME automatic installation, Snap refresh where applicable, and vendor/app/CLI self-updaters are disabled. New versions require a deliberate lock update, candidate golden rebuild, automated acceptance tests, human GUI acceptance, and promotion. A version-audit command may report staleness but never mutates a golden or session automatically.

Node is pinned because third-party tools/projects may require it; no global npm package belongs in the initial golden unless its locked provenance explicitly justifies the exception.

Golden revision pointers and artifact registries are security-domain scoped even when two domains happen to select artifacts built from the same guest-definition digest. Sharing is explicit, not an implicit namespace fallback.

OCI workloads and any remotely distributed golden image are referenced by immutable digest. GHCR credentials remain on the trusted host and are never passed into a golden or guest merely to manage backend images.
