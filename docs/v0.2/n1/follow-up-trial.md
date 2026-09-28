# N1 narrow follow-up trial: clipboard and cross-guest TCP

This is a request design, **not authorization**. The 2026-09-28 attended
candidate installation and two-guest window ended with exact candidate removal.
Keep the PR Draft and the working stock installation unchanged. A new attended
window must bind the exact host artifacts, guest-helper bytes, private config,
task-owned guest names and cleanup procedure before privileged admission. Bind
the generated guest IDs and generations before any clone-only stage or clipboard
write.

## Reusable inputs and preflight

- Reuse the reviewed explicit `n1candidate` source commit `e703453a`, Boxwarden
  executable SHA-256 `da42e38f3c256e4bffb46ec2bb7ffce56d7b1ee77082a3acdcb3c25a00a2e441`,
  and Softnet 0.19.0 N1 executable SHA-256
  `064206d28d82b86093244114f44f726f4f5967575a9b298a7123bf0beb740ef0`
  only after fresh hash, toolchain, platform, stock-doctor and capacity checks.
  The private 2026-09-28 R1 configs and exact cleanup helper are templates, not
  evidence that their retired state root or removed privileged digest still
  exists. Re-establish and verify a fresh isolated state root on the qualified
  APFS volume, config files outside its backing filesystem, R1 enrollment,
  explicit domain CA, and collision-free task-owned names before start.
- Reuse a deliberately selected stopped generic base only after reading its
  build/qualification provenance. The base used in the first N1 trial was built
  from `e10fe501`, before the fixed guest clipboard mode and adapter. It cannot
  establish clipboard capability by its name, stopped state, or registration.
  If no qualified base already contains the capability, prepare and review an
  exact-digest, guest-only installation into **new disposable clones** of the
  committed Linux/arm64 bootstrap artifact locked in
  `guest/ubuntu-24.04-arm64/artifacts.lock.json` (SHA-256 `33b12b9e293bcbdf7d6c91f933b42003ca394e7a678ac83b8d80df714d27c57e`) and the fixed
  `clipboard.py` adapter (SHA-256
  `e38530c45a2aab705be80aad3e9096f2fce9330cb6c0b5b6623a3d76403b4dbb`).
  Record both installed digests and ownership; do not alter the golden, an
  existing VM, or rebuild Ubuntu merely for this N1 check. Without this
  prepared guest-only step, omit the clipboard write and report capability
  unverified. For a clone that needs staging, the exact command must be
  reviewed after its UUID, backend object and generation exist: hold its
  lifecycle locks, use a separately reviewed direct `/usr/bin/ssh` invocation
  with only that generation's private key, short-lived certificate and host-key
  pin. The public Boxwarden management helper cannot run arbitrary commands.
  Stream each verified byte sequence to an unused root-owned staging name,
  verify SHA-256 and `root:root 0755`, then atomically replace only the two
  fixed guest paths. Publicly restart and reverify READY before inspection or
  transfer. The previous private guest-stage code is a reference with old
  hard-coded session IDs, not a command to rerun. This document alone is not
  an executable or approved guest-stage procedure.
- Reuse the first trial's bounded synthetic clipboard probe and exact-generation
  SSH method only after replacing its old UUIDs, backend names, generations and
  output paths. The archived failed cross-guest interval is immutable evidence,
  not a positive control to reuse. No real Mac clipboard, provider credential,
  host filesystem share, guest firewall adjustment or host network change is
  part of this trial.

## One fresh two-guest sequence

Create at most one stock-control and one explicit N1 candidate disposable guest
from the same admitted base, with no workspace or credentials. Require both
READY with exact-generation pinned management SSH. If a guest-only helper stage
was needed, perform it only in these clones, use public stop/start to obtain a
new READY generation, and discard old generation credentials.

Before **any** clipboard write in either guest, use a separate read-only SSH
command with that generation's private key, short-lived certificate, host-key
pin and strict management transport options. Verify the actual installed
`/usr/local/libexec/boxwarden-guest-bootstrap` and
`/usr/local/libexec/boxwarden-guest-clipboard.py` are regular root-owned `0755`
files with the reviewed exact digests; the bootstrap helper must support its
fixed `clipboard` mode. Verify one active local `boxwarden` Wayland login, its
owned runtime directory, GTK3/Xwayland capability and a responsive desktop.
Do not infer these from package presence or a previous guest. If any check is
absent or ambiguous, mark clipboard **NOT EXERCISED**. Otherwise issue one
synthetic CLI copy and exact-byte readback per guest, stock first, bound to each
current generation. Record acknowledgement and output digest. An unknown write
outcome is **UNQUALIFIED** and must not be retried.

For cross-guest TCP, use the stock-control guest's existing management SSH
listener at its observed leased address and port 22 as the owned endpoint.
Prove the same endpoint is reachable from the host with a fresh strict pinned
management probe immediately before and after a single candidate-guest TCP
connection attempt, without sending credentials from the candidate. Confirm
the candidate guest remains READY and its gateway/public controls work. Record
whether TCP connect completes, any banner, a timeout, or another error. A
completed candidate TCP connection fails the isolation check even if no banner
arrives. A timeout with both host positives supports cross-guest denial; another
error is unqualified until attributed. An unreachable host positive makes the
interval **INVALID**. Do not open a guest port or relax a guest firewall to
manufacture reachability. This checks preservation of TCP session isolation in
this topology, not N1-specific gateway denial; UDP cross-guest isolation remains
unqualified without its own working positive control.

Stop and reap both owned guests through public lifecycle commands, preserve
bounded receipts, then use a **newly approved** exact-path attended cleanup to
remove only the temporary candidate digest after consumer/tree checks. Verify
candidate absence, original stock hashes/metadata and stock doctor, the prior
Tart/workspace inventory, and task-only state retirement. If an unexpected
condition appears, stop the affected operation and retain its evidence rather
than restarting the completed 2026-09-28 experiment.
