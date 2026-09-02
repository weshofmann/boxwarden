# Tool provenance and updates

The portable guest-definition lock records guest OS, architecture, and libc where relevant. Every guest package/application records exact version, platform, source URL/repository identity, signing-key fingerprint, artifact digest/checksum, verification method, install method, reason for inclusion, and update owner. A host-specific golden artifact additionally records host/backend type and version, guest-definition digest, build evidence, and final installed package/application BOM. For M1A, Tart and Softnet are one security-critical host toolchain: Task 0 records both exact versions, installation/source identities, relevant artifact/package identities where practical, and the network behaviors empirically qualified for that exact pair. Neither component updates automatically; changing either requires deliberate requalification before its behavior is trusted. Undocumented manual mutation invalidates the artifact: any necessary change must become guest-definition input and survive a clean rebuild.

The M1A host-toolchain identity is exact:

| Tool | Version | Executable SHA-256 | Archive SHA-256 |
|---|---:|---|---|
| Tart | 2.32.1 | `05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d` | `8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529` |
| Softnet | 0.19.0 | `ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e` | `1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c` |

Under ADR 024, Softnet 0.19.0's tested execution path requires host root privilege. The
selected mechanism for the trusted macOS operator / untrusted guest boundary is
an exact qualified copy installed by user-attended `boxwarden init` at
`/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet`.
The executable is a regular one-link file, root-owned, assigned to the dedicated
trusted Boxwarden operator group, and mode `04550`. Every ancestor is a
non-symlink directory owned by root and non-writable by group/other. The
digest-root `manifest.json` is root-owned and published by atomic rename only
after the executable and directory metadata have been verified and synchronized.
A mutable user-writable Homebrew path never inherits authorization. Any
setuid/setgid/passwordless-root Softnet in mutable Homebrew state is blocking
drifted/unsafe state: doctor is nonzero and init/start refuse until attended
manual inspection/remediation. Boxwarden never chmods or repairs it. Init also
refuses any source file with a setuid/setgid bit; only the exact root-owned
digest-specific installed copy may be `04550`.

Normal start uses the absolute qualified Tart executable and a closed
environment whose PATH is exactly the installed digest-specific Softnet
directory. Other variables are explicitly constructed from validated state:
the manifested operator user/home, canonical configured `TART_HOME`, private
generation `TMPDIR`, and fixed locale/user values required by Tart. Ambient
proxy, telemetry/Sentry, Rust/language-runtime, DYLD/loader, and unrelated
variables are absent. It invokes no shell or `sudo`; the `04550` binding is intentionally acceptable only because
native code at the trusted operator UID is outside the M1A adversary boundary.
If that UID becomes hostile, a narrow separately qualified wrapper is required.

`boxwarden doctor` validates the complete canonical path, every ancestor's
owner/mode/ACL/symlink status, Softnet file type/link count/owner/group/mode and
digest, manifest bytes and publish state, absolute Tart digest, macOS version,
and the exact paired-toolchain identity. The manifest binds the single trusted
operator UID/name/home and exact dedicated group ID/name/membership. Directory
service membership and effective supplementary membership of the current
process are distinct checks; init reports a required login-session refresh and
doctor/start fail until membership is effective. It is diagnostic and never changes
privilege. Upgrades install adjacent version-and-digest roots, never overwrite a
qualified tree, and never switch a `current` symlink. Exact uninstall names one
manifested digest root and refuses while any recorded or live supervisor uses
it. Replacing either tool requires deliberate requalification and explicit
re-initialization. Installation, upgrade, uninstall, and real-host qualification
remain user-attended operations.

Production V4 also binds `/usr/bin/screen` 4.00.03 (FAU, 23-Oct-06), executable
SHA-256 `07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add`,
root:wheel mode `0755`, one link, on qualified macOS 26.6.2. Doctor/runtime
verify that exact identity. Its PTY/broker behavior and the direct `04550`
Softnet execution—including argument/environment/dependency use, privilege drop,
signals, file writes, and absence of sudo—remain explicit attended gates.

The workstation uses first-party official distributions for ChatGPT Desktop, Claude Desktop/Code, Antigravity, Grok Build, Codex, Chrome, Docker, and language toolchains. Prefer official native ARM64 packages/binaries over npm wrappers when functionality is equivalent.

Before installation, each vendor tool passes a qualification gate that proves an immutable or reproducibly identified ARM64 artifact path and verifies its signature/checksum. The build never executes a mutable piped installer. If an optional tool cannot meet the gate, the lock records it as unavailable for that golden revision; the provenance policy is not weakened.

The lock distinguishes two claims:

- a reproducibly identified artifact has an exact version, platform, source identity, and verified digest/signature;
- an indefinitely reproducible repository closure additionally retains or references an immutable snapshot of every dependency and signed repository metadata.

M1A requires the first claim for every input and uses repository snapshots or a verified retained package closure where practical. When a third-party repository cannot guarantee indefinite retention, the lock records that limitation explicitly instead of claiming bit-for-bit rebuildability. Final BOM, repository metadata, package files retained for rebuild, and source snapshots establish the best available closure.

APT packages are installed at exact versions and held, but holds are update controls rather than rebuild provenance. Unattended upgrades, PackageKit/GNOME automatic installation, Snap refresh where applicable, and vendor/app/CLI self-updaters are disabled. New versions require a deliberate lock update, candidate golden rebuild, automated acceptance tests, human GUI acceptance, and promotion. A version-audit command may report staleness but never mutates a golden or session automatically.

Node is pinned because third-party tools/projects may require it; no global npm package belongs in the initial golden unless its locked provenance explicitly justifies the exception.

Golden admission/selection records are security-domain scoped even when two domains select the same exact generic backend artifact. The artifact is not duplicated or made domain-specific; each operator admission is explicit and no domain searches another domain's metadata as a fallback. Registration records exact existing/stopped identity, not unrecorded provenance or qualification evidence.

OCI workloads and any remotely distributed golden image are referenced by immutable digest. GHCR credentials remain on the trusted host and are never passed into a golden or guest merely to manage backend images.
