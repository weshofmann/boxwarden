# N1 staged deployment and qualification gate

This procedure is **not authorization to install or run a new privileged
binary**. Source tests do not qualify a Mac. The default build retains the stock
ADR 015 limitation. The candidate build is an explicit, separate test artifact.

## Request scope and prerequisites

Request one attended window to admit the exact reviewed candidate Softnet
artifact from its build manifest, under its digest-specific `/Library/Boxwarden`
path, and use the corresponding `n1candidate` Boxwarden executable with an
isolated synthetic configuration. Permit only its exact root-owned binary and
manifest publication, at existing ownership/modes, and bounded tests of at most
two task-owned guests. No firewall/DNS/routes/VPN/extension change is requested.

The existing installed binary, manifest, configurations and working clipboard
sandbox remain protected. Manifests are per digest, not a global mutable
selector. A temporary second exact privileged path for this qualification is a
separate explicit exception to the stock single-toolchain policy, requiring
operator approval; do not interpret the staged candidate as granting it.
If that exception is declined, wait for a separately approved stock-toolchain
transition. Do not stop an existing guest to make the test convenient.

Before requesting execution, the package must contain:

- Exact source, patch, compiler, executable and archive digests, including the
  candidate Boxwarden source commit and `n1candidate` build metadata.
- Green deterministic packet, failure-path, launcher, host-admission and status
  tests for both default and candidate builds, and independent code review.
- An unprivileged candidate source file; no setuid copies outside the proposed
  root-owned digest path. Confirm current admitted stock doctor is healthy and
  record its exact identity without changing it.
- A new private test configuration/state root, synthetic inputs, explicit
  domain initialization, and current R1 workspace enrollment if a workspace is
  needed. No workspace is needed merely to test networking. Reuse a deliberately
  selected existing stopped generic base; do not rebuild Ubuntu for N1.
- Current free-space/memory capacity and resource inventory. Native APFS and
  neighboring APFS volumes share a container budget. Keep existing guards.

The operator-facing request must name actual artifact paths/digests from this
package and the exact `init` command. Do not substitute a PATH lookup or invoke
Softnet via sudo directly. The normal Boxwarden attended installer and doctor
retain their path/ownership/ACL/link/group/digest checks. Do not bypass admission
to get an early candidate VM boot.

## Positive controls and observation

Use `tools/n1-qualification/listener.py` for owned host endpoints. It serves one
fixed synthetic byte string over TCP and UDP, accepts no filenames or commands,
and automatically exits in 1–120 seconds. It rejects wildcard/multicast binds
and privileged ports. Run it in the foreground or retain its exact child handle
and reap it. The local unit tests use loopback only; they are **host-only
integration**, not vmnet evidence.

Bind an explicitly observed vmnet gateway address, then an assigned non-gateway
host address if available. Let the fixture select an unused ephemeral port and
record the returned endpoint. Never scan the host or LAN. In each test interval:

1. Confirm the fixture is running and returns exactly
   `boxwarden-n1-owned-fixture\n` through the owned host-side control.
2. From a task-owned stock-policy guest, request that exact endpoint over TCP
   and UDP and record success. This is the positive gateway exposure control.
   A missing listener or closed port does not qualify a negative case.
3. From a task-owned candidate guest, attempt the same endpoint while the same
   fixture remains live. Record timeout/refusal, fixture response counts and
   bounded policy-boundary observations. A guest-reported PASS is not evidence.
4. Recheck the host-side fixture after the negative attempt. Reap it and record
   its final response counts. If it exited early, the negative result is invalid.

Use a fresh fixture interval when repeating a test. Correlate exact guest, policy
artifact, destination/port and timestamps. Capture policy decisions before
vmnet forwarding where supported; do not treat a flag-string check as isolation.
Use only synthetic bytes, not the general Mac clipboard or provider credentials.

## Compatibility sequence

After fresh DHCP address discovery, exercise UDP DNS and explicit TCP DNS at the
DHCP-advertised vmnet gateway. Exercise truncation/large-answer TCP fallback.
Capture resolver path without publishing private names or answers. Public HTTPS
must follow the host's selected route; no public-resolver override. Test a lease
renewal across the lease interval, including pinned SSH across renewal.

Use public lifecycle commands for start, status, stop and restart of the owned
guest. Repeat the host-listener controls after restart. Exercise controlled
clipboard with synthetic input and explicit output file only, never `pbpaste`
or the real system clipboard. Preserve exact management pin/certificate checks.

Private/link-local and two-guest denial use only explicitly owned fixture
endpoints. Guest-root route/firewall changes in the disposable guest must not
remove the restriction. Do not probe unrelated host services or networks.
Parser, spoofing, fragment, flow-state and metadata-failure adversarial cases
belong in deterministic packet fixtures, not attacks against the host.

Scoped/split DNS, VPN transitions and DNS64 require representative authorized
environments. Where unavailable, record NOT EXERCISED and retain fixture-only
claims. Do not change the operator's VPN or DNS to manufacture a test. Native
IPv6 and effectively IPv6-only upstream remain unqualified. Fragmented guest
IPv4 is denied, with TCP fallback requirements documented. Address refresh/write
races and public NAT hairpin endpoints are outside demonstrated containment.

## Failure, cleanup and rollback

Any ambiguous policy initialization or unavailable trusted metadata must leave
the candidate non-ready; never retry with stock flags as an implicit fallback.
Stop an owned candidate guest if enforcement observations contradict policy.
Preserve failed evidence as immutable; correct source and use a fresh disposable
trial. Keep a full failed disk only for a named question requiring its bytes.

Stop/reap only task-owned guests/listeners and retire them through supported
ownership checks once evidence is captured. Do not touch the working clipboard
guest. For an approved side-by-side qualification install, rollback means stop
candidate consumers, cease use of the candidate configuration, and verify the
prior stock doctor/configuration still work. There is no public uninstall CLI;
the internal exact-digest removal primitive is not an operator command.
Do not remove the shared operator group or stock digest tree, repair permissions
silently, or rewrite a prior manifest. Removing the candidate digest tree is a
separate attended exact-path cleanup after consumer checks; request its concrete
plan and authorization before privileged removal. Merely ceasing use does not
remove the installed privileged artifact.

Final inventory must state task-owned VM IDs, fixture PIDs/reaped state,
candidate privileged-path presence, original toolchain/guest preservation,
private evidence location, actual matrix rows and remaining operator actions.
