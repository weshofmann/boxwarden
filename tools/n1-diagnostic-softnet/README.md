# N1 diagnostic Softnet source

This distinct AGPL-3.0 source package applies to Softnet commit
`df84a30016e3d6acc0d30acc660cf3a726f42a9b` after the unchanged canonical
`tools/n1-softnet/softnet.patch`. Apply `diagnostic.patch` second with
`git apply --check`, then `git apply`, in a disposable source tree.
`source-manifest.json` binds the patch, shared source, frozen wire catalogue,
build definitions and pinned compiler identities; it has no executable hash.

Actual Proxy VM/host writes invoke the tested shared dispatch functions.
Choosing policy metadata is observational; refresh/fallback/write order,
raw results, byte lengths and ENOBUFS suppression retain canonical behavior.
Only identified pair headers are counted. No enqueue or delivery claim is
made; the pinned vmnet wrapper does not expose its output packet count.

The exact diagnostic selector accepts one anonymous inherited nonblocking
AF_UNIX stream on fd1. HELLO follows privilege drop; one strict ARM selects
one 1000–30000ms interval, followed by ARMED and one bounded SUMMARY.
Metadata failure stays passive and makes coverage incomplete. No arbitrary
packet contents, address lists or error text are published. Watch service
runs at every drain/batch boundary and the 100ms idle timeout.

Mandatory admission resolves the actual image path, hashes its open
self-descriptor, validates the protected fixed digest tree and complete
host manifest, then retains a read-only shared launch.lock through actual
resource teardown. Tart remains exact stock 2.32.1. The child's archive
field checks nonzero lowercase SHA-256 syntax only to avoid a self-hash
cycle; the later publisher must bind the actual reproduced archive exactly.
No relocated/unprivileged valid candidate run is permitted.

Deterministic CI uses only `synthetic/Cargo.toml`, its separate Cargo.lock,
and `--lib`. From a neutral working directory outside both repositories,
with absolute manifest path and a private target/temp/cache, run:

```sh
cargo test --locked --offline --manifest-path /absolute/repo/tools/n1-diagnostic-softnet/synthetic/Cargo.toml --lib
```

This fixture compiles the actual shared modules plus unchanged canonical
policy as oracle. It never imports Proxy, vmnet, Host, bootpd or upstream
Cargo test targets. On Darwin, ordinary private ACL/lock descriptors and
anonymous socketpairs provide native controls; Linux has a synthetic-only
anonymous sockaddr shape. Production self-admission refuses off Darwin.
Do not run upstream Cargo tests: its configured runner uses sudo. Actual
source verification uses only `cargo check --locked --offline` from a
neutral directory with an explicit inert runner override, never a valid
candidate invocation. Pinned Rust/cargo 1.98.1 are required; final artifact
reproduction is a separate reviewed phase. Existing locked telemetry
transitives remain to preserve the lock graph, while executable telemetry
and logging initialization/calls have been removed.
