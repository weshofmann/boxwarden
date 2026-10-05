# Softnet N1 candidate source package

This package modifies the exact upstream Softnet 0.19.0 source commit
`df84a30016e3d6acc0d30acc660cf3a726f42a9b`. It is distributed under
upstream GNU AGPL-3.0 (see `LICENSE`). `softnet.patch` is the corresponding
source; `policy.rs` is a duplicate of the pure policy module for direct,
unprivileged `rustc --test` execution. `artifact.json` records the source,
patch, compiler, executable, and deterministic archive identities of the
staged candidate. Tart source is not included or changed.

`build.py` refuses a dirty or wrong upstream source, wrong patch/policy or
compiler binaries, or a pre-existing canonical build stage. It extracts the
pinned source via `git archive`, applies the patch, runs the same policy module
through `rustc --test`, then runs only `cargo build --locked --offline --release`
with a closed environment. After the locked build, it compiles the same policy
module with the exact locked `ip_network` rlib to test the real public/private
fallback classification through the forwarding hook. It never runs Cargo tests because upstream
`.cargo/config.toml` sets a `sudo -E` test runner. The fixed
`/private/tmp/boxwarden-n1-release-stage` build location and path remapping
keep the output path from changing Cargo's local package identity. A new
`--output` directory receives the build tree, test binary, result JSON and
single-file deterministic USTAR archive. The script checks the version and rejects missing, unknown or incompatible
selector/allow/expose/host-mode arguments before comparing resulting artifact
digests with the staged identity. `verify_cli.py <binary>` runs those same
closed-environment argument checks against an already staged executable and
prints exact argv elements, counts, UTF-8 bytes, exits, stdout and stderr as
JSON lines. Both `build.py` and `verify_cli.py` validate the regular,
current-operator-owned, single-link 0755 executable against the pinned digest
before any candidate invocation; setuid/setgid modes are rejected. CLI calls
have a five-second timeout. `test_verify_cli.py` proves a wrong 0755 marker
script never executes and checks privileged-mode rejection using mocked stat
metadata only. Run `test_verify_cli.py verify_cli.py` in deterministic CI without a
candidate binary; pass the staged binary as an optional second argument for
the positive local CLI check. No test changes a file's privileged bits or invokes an accepted
candidate configuration.

Example, using separately verified upstream source, compiler and Cargo cache:

```sh
python3 tools/n1-softnet/build.py \
  --source /path/to/clean/softnet-0.19.0 \
  --rust-bin /path/to/rust-1.98.1/toolchain/bin \
  --cargo-home /path/to/locked/cargo-cache \
  --output /private/tmp/boxwarden-n1-new-build
```

The executable requires exactly `--block=@boxwarden-host-containment` and NAT,
rejects all allow and expose overrides, and reports version
`0.19.0-boxwarden-n1.1`. The packet policy runs before `Host::write`; trusted
DHCP and host-originated management observations run on ingress. It has not
been installed or qualified in a real guest. The prior stock executable
retains its gateway exposure until deliberate operator-controlled admission.

The security claim is limited to stable IPv4 endpoints directly assigned to
host interfaces. Host address observation and vmnet write are not atomic,
and a public NAT hairpin endpoint absent from `getifaddrs` cannot be
classified as host-local. Interface-directed broadcasts are derived only
from valid `IFF_BROADCAST` netmasks. Broadcast topology of arbitrary remote
subnets cannot be inferred. IPv6 guest traffic and IPv6-only upstream remain
unsupported. DHCP, DNS proxy, vmnet and hypervisor parsers remain exposed.

Current beta packaging and the bounded next live comparison are described in
[the current-beta guide](../../docs/v0.2/n1/current-beta.md). That integration
does not turn historical host trials into qualification of a new beta.
