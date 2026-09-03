# Softnet Lossless Observer Analysis

Status: Research / non-authoritative
Author: Kiro / Claude Sonnet 4.6
Date: 2026-09-02
Reviewed repository base: 7de2306b83136231beb9e03a7a769c52c9691d7f

This document is independent research. It does not modify or supersede
accepted ADRs, canonical architecture, or implementation plans.
Maintainer acceptance is required before any recommendation becomes policy.

---

## Executive conclusion

The strongest available design is a layered evidence model, not a single lossless
observer. Most security-relevant facts about the Softnet/Tart runtime are already
proven more strongly by static artifact identity and root-controlled filesystem
state than any available runtime mechanism could prove. The residual runtime facts
that genuinely require dynamic observation — principally the setuid/privilege
transition and the closed-environment launch — can be adequately addressed by a
combination of (1) the supervisor-controlled Tart launch environment (proven by
the supervisor itself before exec), (2) a short-duration `proc_pidinfo` poll of
the Softnet process credential state (attended, accepts its sampling limitation
explicitly), and (3) externally observable network behavior proving qualified
Softnet policy. No mechanism on SIP-enabled macOS can claim perfect losslessness
for every candidate fact; the practical question is whether the evidence layer
is strong enough given the specific threat model. Under the ADR 024 threat
model — malicious guest root, not hostile native code at the trusted operator
UID — the combination is defensible.

The specific architecture decision requiring maintainer approval is: accepting
`proc_pidinfo`-based polling (not event-driven; subject to a narrow race window)
as adequate evidence of the Softnet privilege transition, justified by the closed
execution path and the absence of any stronger mechanism on SIP-enabled macOS
without an Apple entitlement the project does not hold.

---

## Background and threat model recap

ADR 024 is explicit: the M1A adversary is a malicious guest root, not arbitrary
hostile native code executing as the trusted macOS operator. The `04550`
setuid mechanism is honest about that boundary:

> "The setuid bit is an intentional, visible host trust grant, not a claim that
> Softnet becomes a sandbox for the trusted operator. [...] It is valid only for
> the documented trusted-operator boundary."

This boundary matters for the observer design. All candidate observations need
only prove that the qualified production path behaves as designed under normal
conditions — not that a hostile operator cannot subvert a runtime observer.
A hostile operator can subvert any observer running at the operator UID. Designing
for that threat would require a separately qualified narrow wrapper (ADR 024
alternative already rejected) or a kernel-level observer with Apple provisioning.
That is a different project.

---

## Qualification requirement matrix

For each candidate runtime fact, the matrix records: security rationale; whether
it is provable statically; whether it requires dynamic observation; required
confidence; and the consequence of failure.

### F1. Exact Softnet executable invoked by Tart

**Security rationale:** If Tart resolves a different binary, the digest/mode
security from `boxwarden init` is bypassed.

**Static proof:** The supervisor constructs Tart's argv with an absolute path and
constructs PATH containing only the qualified digest directory. The supervisor
verifies the digest and tree integrity immediately before launching Tart (re-runs
doctor-equivalent checks). Tart then searches PATH for `softnet` — it finds only
the one root-owned digest-bound executable at the manifest path. The chain of
custody is: supervisor verifies tree → supervisor sets PATH to digest dir → Tart
searches PATH → finds exactly one binary.

**Dynamic observation needed?** No. The supervisor's pre-launch check is the
authoritative proof. Runtime observation of which binary Tart actually invoked
would require attaching to Tart's process at exec time — which requires either
the Tart process group (the supervisor has it) or a kernel-level observer. The
static chain is stronger.

**Required confidence:** High. **Failure consequence:** Wrong binary executes with
root privilege.

**Proposed evidence:** Pre-launch supervisor digest re-verification (already
planned in V4 supervisor design). Attended gate confirms PATH is exactly
the digest directory from the environment printed before exec.

---

### F2. Exact Tart executable

**Security rationale:** A compromised Tart could invoke arbitrary code as root.

**Static proof:** Doctor verifies exact SHA-256 `05b65d5c...` at the configured
absolute path before any start. Supervisor re-runs same check before launch.

**Dynamic observation needed?** No. SHA-256 of a running binary cannot be
observed more strongly than a pre-launch hash of the same file. Once the file
is verified and the launch proceeds with an absolute path, the kernel exec
uses that exact inode.

**Proposed evidence:** Doctor and supervisor pre-launch check. Attended gate
confirms digest match.

---

### F3. Argv passed to Softnet by Tart

**Security rationale:** Tart must pass the correct network configuration flags
(`--net-softnet`, `--no-audio`, `--no-clipboard`) that produce the qualified
Softnet behavior. Wrong argv could enable prohibited policy.

**Static proof:** Partial. The supervisor controls Tart's argv but not what Tart
then passes to Softnet. Tart's source behavior is version-pinned but not
locally auditable without the source.

**Dynamic observation needed?** Partially. The exact argv Tart passes to Softnet
is an internal Tart implementation detail. However: (a) Tart's network policy
flags drive the argv to Softnet, (b) if the network policy is wrong, it will
be externally observable in network behavior (F12), and (c) Tart's behavior
at this version has been empirically qualified for these flags in Task 0.

**Required confidence:** Moderate. What is needed is evidence that Tart's argv
construction for the qualified flags produces the qualified behavior — not
byte-level argv interception.

**Proposed evidence:** Externally observable network behavior (F12) is the
operationally meaningful proxy. Task 0 already established the mapping from
Tart launch flags to network behavior. The attended gate re-confirms that
mapping by running the same network policy tests under the production supervisor
environment.

**Losslessness requirement:** Low — network behavior is the meaningful proxy;
exact Softnet argv bytes are Tart internals.

---

### F4. Environment visible to Softnet (closed environment)

**Security rationale:** If ambient DYLD_INSERT_LIBRARIES, LD_PRELOAD, proxy
settings, or Rust/Sentry variables survive into Softnet's environment, they
could affect its behavior (library injection, telemetry exfiltration, proxy
bypass of the qualified network policy).

**Static proof:** The supervisor constructs Tart's environment explicitly with
only the required variables (operator home/user, TART_HOME, TMPDIR, locale,
and PATH). No ambient variables survive (the V4 plan is explicit: "Ambient
proxy, telemetry/Sentry, Rust/language-runtime, DYLD/loader, and unrelated
variables are absent"). The supervisor's construction of this environment is
code under test and is deterministically verifiable.

**Dynamic observation needed?** The supervisor can log its constructed env before
exec. The question is whether Tart further propagates or modifies that env
before passing it to Softnet — again an internal Tart behavior.

**Proposed evidence:** Attended gate: before launching Tart, the supervisor prints
its exact constructed environment. The attended operator verifies the list
contains only the expected variables. For Tart's internal behavior: since Tart
is a pinned binary (digest proven), its env-propagation behavior is fixed for
this exact version. Task 0 qualified the network behavior of this Tart version
with this flag set, which is the meaningful downstream proof that the env
was adequate.

**Losslessness requirement:** Low for exact env bytes in Softnet; high for
proving no prohibited variables survive into Tart's invocation.

---

### F5. Process ancestry

**Security rationale:** If Softnet is invoked through an unexpected parent chain
(e.g., through a sudo wrapper), the privilege escalation path is different from
the qualified path.

**Static proof:** The supervisor is the direct parent of Tart. The supervisor does
not use sudo. Task 0 observed "Tart parented Softnet" — Softnet is a direct child
of Tart in normal operation.

**Dynamic observation needed?** The supervisor knows its own PID and knows Tart is
its direct child. Proving Softnet is a direct child of Tart requires either
observing Tart's fork behavior or polling the Softnet process's ppid after it
appears.

**Proposed evidence:** A `proc_pidinfo` call for the Softnet process immediately
after Tart launches, recording `pbi_ppid`. The observed ppid must equal Tart's
pid. This is subject to a short window between fork and the first poll, but
Softnet is a long-running network daemon (it runs for the duration of the VM),
so the window is not a practical concern for process ancestry verification.

**Losslessness requirement:** Low — ancestry is stable for the life of the process.

---

### F6. Real/effective/saved UID/GID transitions (privilege elevation)

**Security rationale:** The setuid-04550 mechanism is the critical privilege grant.
We need to confirm that when Tart invokes the qualified Softnet executable, the
kernel applies the setuid bit and Softnet executes with effective UID 0. Without
this, either (a) the installation is wrong, or (b) some mechanism suppressed the
setuid bit.

**Static proof:** Doctor verifies mode `04550` on the installed Softnet binary.
The install gate confirmed mode, ownership, and digest. But static proof of
mode does not prove the kernel applied the setuid bit when Tart actually exec'd.

**Dynamic observation needed?** Yes — this is the one credential fact that
genuinely requires runtime observation. The kernel applies the setuid bit at
exec time; static mode verification proves the mode is set, but does not prove
the transition occurred at the specific exec.

**Available mechanisms:**
- `proc_pidinfo(PROC_PIDTASKALLINFO)` on the Softnet PID returns
  `pbi_uid` (effective UID), `pbi_ruid` (real UID), `pbi_svuid` (saved UID).
  Under a correctly executed setuid, effective UID will be 0, real UID will
  be the operator's UID, and saved UID will be 0. This is polling — there is a
  window between fork and the poll. For a long-running daemon like Softnet,
  this is acceptable.
- ES `ES_EVENT_TYPE_NOTIFY_EXEC` provides the post-exec `es_process_t.audit_token`
  which encodes uid/euid. The ES documentation states: "the uid/gid values may be
  different if the new program had setuid/setgid permission bits set" — the
  audit_token after exec reflects post-setuid state. This is event-driven (no
  race), but requires Apple provisioning.
- ES `ES_EVENT_TYPE_NOTIFY_SETUID` is a guaranteed kernel event: "If no
  `es_event_setuid_t` event is emitted then no `setuid` took place. This is a
  security guarantee." But requires Apple provisioning.

**Required confidence:** High. **Failure consequence:** Softnet runs unprivileged
and cannot configure vmnet interfaces; or a path bypass occurred.

**Proposed evidence:** `proc_pidinfo` poll with explicit race-window
acknowledgment. The V4 supervisor already knows the Softnet PID from Tart's
process tree. The supervisor polls credential state after observing that
Softnet is running (verified via proc_pidinfo). The gate records observed
`pbi_uid=0`, `pbi_ruid=<operator_uid>`, `pbi_svuid=0`. The attended gate
accepts this with explicit documentation that the poll is sampling (not an
event), and that the static mode `04550` combined with the running UID=0 poll
constitutes adequate evidence for the ADR 024 threat model.

**Losslessness requirement:** Moderate. The sampling race is acceptable because
(1) Softnet is a long-running daemon, (2) the static proof of mode provides
corroboration, and (3) the ADR 024 adversary cannot alter the setuid bit
without first compromising the root-owned install tree, which would be detected
by doctor.

---

### F7. Privilege drop and when it occurs

**Security rationale:** After Softnet configures vmnet interfaces (which requires
root), it should ideally drop privileges. If it does not, a guest-exploitable
Softnet vulnerability gives an attacker root on the host.

**Analysis:** This is a property of Softnet's implementation, not the installer
or the setuid mechanism. The source code of Softnet 0.19.0 is the authoritative
evidence. This is an upstream code review question, not an observation question.

**Static proof:** Source code review of Softnet 0.19.0 would establish whether
`setuid(getuid())` or equivalent privilege drop occurs after network setup.
This is independent of the observation mechanism.

**Dynamic observation needed?** A `proc_pidinfo` poll after Softnet has been
running for a few seconds (after it would have completed vmnet setup) could
provide a data point — if `pbi_uid` drops from 0 to operator UID, we observe
the drop. But the timing of any drop is unknown without source review.

**Proposed evidence:** Upstream source code review of Softnet 0.19.0 for privilege
drop. This is separate from the observation mechanism question. If Softnet does
not drop privileges, that is an accepted limitation to document (not a blocker
for the gate, since the ADR 024 threat model does not require privilege drop
for correctness — the threat is guest root, not Softnet exploitation).

**Losslessness requirement:** Low — this is a code review question, not a
runtime observation question.

---

### F8. Supplementary groups

**Security rationale:** Unexpected supplementary groups on the Softnet process
could indicate environment contamination.

**Static proof:** The supervisor explicitly constructs the Tart launch environment.
No group injection mechanism exists. The group state of the Tart process (and
thus Softnet as its child) derives from the supervisor's credentials.

**Dynamic observation needed?** proc_pidinfo does not directly return supplementary
groups; that requires a different kernel call. This fact is adequately proven
by the deterministic env construction path.

**Proposed evidence:** Pre-launch: supervisor records its own supplementary groups
(which propagate to Tart and Softnet). Attended gate verifies the supervisor's
group list at launch time. No dynamic runtime check needed for this fact alone.

---

### F9. Inherited/open file descriptors

**Security rationale:** Unexpected open fds in Softnet could be host data
sources or exfiltration paths.

**Analysis:** The V4 plan explicitly constructs a "generation-private TMPDIR" and
uses a closed environment. The supervisor manages the Tart process's standard
I/O and keeps the exact PTY fds under its control.

**Dynamic observation needed?** ES `ES_EVENT_TYPE_NOTIFY_EXEC` provides
`last_fd` (highest open file descriptor after exec completed) and the full
fd list via `es_exec_fd_count`/`es_exec_fd`. This would be definitive — but
requires Apple provisioning. Without ES, the supervisor can verify its own
fd state before forking Tart (it owns that state). Softnet's fds are one level
deeper.

**Proposed evidence:** Adequate: supervisor closes all non-essential fds before
forking Tart (using `CLOEXEC` and explicit close). The supervisor documents its
fd management. No runtime observation of Softnet's exact fd set is required for
the ADR 024 threat model.

---

### F10. Unexpected executable/library resolution (dyld)

**Security rationale:** If DYLD_INSERT_LIBRARIES or other dyld variables inject
unexpected libraries into Softnet, its behavior is unqualified.

**Static proof:** The closed environment prevents DYLD_* variables. Softnet 0.19.0
is a signed Mach-O binary. On macOS with SIP enabled and the binary bearing the
hardened runtime entitlement (if applicable), library injection is blocked.
The exact binary digest proves the binary has not been tampered with.

**Dynamic observation needed?** No — the combination of closed env + digest proof
is adequate. `DYLD_INSERT_LIBRARIES` cannot inject into a hardened-runtime binary
on SIP-enabled macOS. Even if the binary lacks the hardened runtime, the closed
env strips DYLD_* variables.

**Proposed evidence:** Closed environment construction (already verified) + digest
proof (already verified). No runtime observation needed.

---

### F11. Signal behavior

**Security rationale:** Unexpected signals to Softnet could disrupt network
policy (e.g., SIGTERM causing premature teardown without proper cleanup).

**Analysis:** Signal behavior is primarily relevant for the cleanup path (does
Softnet shut down cleanly when Tart exits?). Task 0 observed "Softnet briefly
reparented to PID 1 during async teardown, but exited within five seconds."
This is already observed behavior.

**Dynamic observation needed?** The exit behavior is observable by the supervisor:
it observes Tart's exit, and the Softnet process should subsequently exit.
The supervisor can verify Softnet exits within a bounded time after Tart
exits.

**Proposed evidence:** Attended gate records Softnet process exit within bounded
time after Tart exit. This is verifiable from the supervisor's process
monitoring.

---

### F12. Network interface/routing/filter behavior (qualified Softnet policy)

**Security rationale:** The entire point of the qualified Softnet configuration
is to enforce the network policy: private/link-local denial, public internet
access, session isolation, anti-spoofing. These must hold at runtime.

**Static proof:** None — network policy is a runtime property.

**Dynamic observation needed?** Yes — this is the primary runtime qualification
fact. Task 0 already established the method: controlled network probes from
within a running VM confirm or deny reachability for each policy-relevant class.

**Required confidence:** High. **Failure consequence:** Guest reaches unintended
network destinations.

**Proposed evidence:** Task 0 network policy tests, rerun under the production
V4 supervisor environment. The attended gate must confirm: private/link-local
denial, public internet access, session isolation, and DHCP lease. This is
the most meaningful dynamic observation because it directly tests the
security-relevant policy rather than implementation internals.

**Losslessness requirement:** Not applicable — this is behavioral testing, not
event observation.

---

### F13. Filesystem writes and persistence outside runtime tree

**Security rationale:** Softnet should not write to the trusted host filesystem
outside its expected runtime tree (the generation-private TMPDIR).

**Static proof:** None — this is a runtime property.

**Dynamic observation needed?** Partial. The supervisor controls TMPDIR to the
generation-private directory. Unexpected filesystem writes by Softnet are
detectable by a before/after comparison of filesystem state under key
directories, or by `fs_usage -w` during the Softnet run.

**fs_usage analysis:** `fs_usage` requires root and is sampling-based (not
lossless). Adequate for an attended gate where the operator runs `fs_usage`
for the duration of the test and inspects the output. Not suitable as an
automated production check.

**Proposed evidence:** Pre-launch snapshot of expected write locations; post-run
inspection confirming no unexpected writes. For the attended gate, the operator
may run `fs_usage -w softnet` or equivalent to capture Softnet's filesystem
activity during a controlled run.

---

### F14. Absence of sudo path

**Security rationale:** The V4 supervisor must not use sudo anywhere in the
Softnet invocation chain. If sudo appears, the privilege mechanism has a
second path that may not be subject to the same constraints.

**Static proof:** The supervisor's code is under test and does not invoke sudo.
Doctor verifies no passwordless sudo rule for the Softnet path exists.

**Dynamic observation needed?** No — the supervisor's code is the evidence. The
attended gate verifies by reading the supervisor's launch sequence. Doctor
already validates the sudoers policy.

---

### F15. Tart/Softnet exit and cleanup behavior

**Security rationale:** Stale Softnet processes or leaked vmnet interfaces could
persist after VM shutdown and affect the host network state.

**Static proof:** None — runtime property.

**Dynamic observation needed?** Yes. The supervisor observes Tart exit (direct
child). After Tart exits, the supervisor verifies the process inventory no
longer contains a Softnet process. This is the "Softnet briefly reparented to
PID 1 during async teardown, but exited within five seconds" behavior from Task 0.

**Proposed evidence:** Attended gate: after stopping the VM, the supervisor (or
the attended operator) verifies no `softnet` process remains within a bounded
timeout.

---

### F16. GNU Screen retention/exit/cleanup behavior

**Security rationale:** Screen must retain the serial session during the VM
lifetime and clean up after Tart exits. A stale Screen process holding an
orphaned PTY is a trusted-host attack surface.

**Static proof:** Screen's identity (digest + version) is already proven by
doctor. The V4 broker owns Screen as a direct child.

**Dynamic observation needed?** Yes. The attended gate must confirm:
(a) Screen holds the operator PTY during VM operation (attach/detach works);
(b) Screen exits when Tart exits; (c) PTY devices and session metadata are
removed after cleanup. This was already proven in Task 0 for the socat-based
harness; the V4 supervisor-owned broker requires re-proof.

**Proposed evidence:** Attended gate: attach, verify retained output, detach,
verify harness processes (supervisor, Tart, Screen) still present; then stop
the VM and verify all three exit and runtime directory is clean.

---

## Candidate observer analysis

| Mechanism | Available | Requires Root | Alters System State | Event-Driven vs Polling | Lossless Claim | SIP Impact | Perturbation | Suitable as Qualification Evidence |
|---|---|---|---|---|---|---|---|---|
| **proc_pidinfo / libproc** | Yes | No (for owner's own processes; root for others) | No | Polling | No — races short-lived processes | None | Minimal (read-only) | Yes, with explicit race-window caveat |
| **DTrace (proc:::, syscall::)** | Binary at /usr/sbin/dtrace | Yes (SIP: many probes broken) | Moderate (probe insertion) | Event-driven | Probe drops possible | Severe — most useful probes disabled | Low for remaining probes | No — SIP prevents reliable use on production host |
| **execsnoop / opensnoop** | Yes (DTrace wrappers) | Yes (inherit DTrace limits) | Moderate | Event-driven (via DTrace) | No | Severe | Low for visible events | No — DTrace wrapper, same SIP limitation |
| **Endpoint Security (ES)** | Framework present; API requires entitlement | No (runs at operator UID if entitled) | Low — NOTIFY-only events | Event-driven | Yes for exec+setuid events (kernel guarantee) | None (ES is SIP-compatible) | None for NOTIFY-only | Conditionally yes — strongest mechanism, but requires Apple provisioning |
| **BSM / auditd** | auditd present; not configured | Root to configure | Yes — starts/modifies auditd | Event-driven (with kernel buffering) | No — kernel drops events under load | None | Low (kernel-level) | No — event loss possible; configuration mutates system |
| **fs_usage** | Yes | Yes | No | Sampling | No | None | Low | Partial — attended gate for filesystem writes only |
| **log stream / Unified Logging** | Yes | No | No | Streaming (with latency) | No — process-controlled, arbitrary content | None | None | No — depends on Tart/Softnet log output; not structured guarantee |
| **Parent-side env/argv logging** | Yes (supervisor itself) | No | No | Deterministic (not observation) | Yes for supervisor-controlled state | None | None | Yes — strongest for pre-launch facts |
| **Process tree polling (ps / proc_pidinfo)** | Yes | Moderate (proc_pidinfo for other processes needs entitlement or root) | No | Polling | No — race window | None | Minimal | Yes with caveats for long-running processes |
| **Passive network packet capture** | Yes (pktap / tcpdump) | Yes | Low | Sampling | No | None | None | Partial — confirms network policy in attended gate |
| **Filesystem snapshot/diff** | Yes (APFS snapshots or manual stat) | Elevated | Yes (APFS snapshot) | N/A (post-hoc) | N/A | None | Low | Yes — for filesystem write verification in attended gate |
| **Source code review + digest binding** | Yes (Softnet is open source) | No | No | N/A (static) | N/A (definitional) | None | None | Yes — strongest for behavior provable from source |

---

## Perturbation analysis

### proc_pidinfo

Does not modify the target process in any way. It is a read-only syscall that
returns a snapshot of kernel process state. The snapshot may be stale by the time
it is read (TOCTOU), but does not affect the target process's execution.

No effect on: argv, environment, file descriptors, UID/GID transitions, signals,
scheduling, or process ancestry.

Race window: The process must already exist. For a long-running daemon like
Softnet, a poll immediately after observing the process via Tart's PID tree
is reliable.

### Endpoint Security (NOTIFY-only)

NOTIFY events are delivered after the fact; the kernel does not wait for the
observer before proceeding. The observed process is not blocked, slowed, or
modified. ES framework components are separate processes that receive kernel
event copies.

No effect on: argv, environment, file descriptors, UID/GID transitions, signals,
scheduling for the observed process.

The observer process itself may affect system scheduling under heavy load, but
for an attended gate this is not a meaningful concern.

Key concern: To use ES, a separate observer binary must be built, signed with
the Apple entitlement, approved via TCC, and run before Tart starts. This binary
is itself a production attack surface if it persists. For the attended gate only,
it could be run only during qualification and then removed. But obtaining the
entitlement requires Apple provisioning infrastructure not currently available
to the project.

### BSM/audit

Enabling auditd modifies persistent system configuration (`/etc/security/audit_control`).
This is a system-state change on the qualification host, which the V4 plan
explicitly prohibits ("never mutate the real host" other than the intended
installation). Audit log files persist after the test. Events can be dropped
under kernel buffer pressure. Not suitable.

### DTrace

Most process-level probes are blocked by SIP on production macOS. The `proc:::exec-success`
probe used by execsnoop requires SIP disabled or the `com.apple.private.dtrace`
entitlement (private Apple-internal). The `/usr/sbin/dtrace` binary is present
but effectively neutered for process-level tracing under SIP. Enabling DTrace
probes for this use case requires disabling SIP, which would make the qualified
host an unqualified host.

DTrace also modifies scheduling through probe insertions (the "probe effect"),
which can affect timing-sensitive behavior in ways that are hard to characterize.

### fs_usage

Requires root. Returns a stream of sampled filesystem events, not a complete log.
Can miss events if the sample rate is exceeded. No effect on the target process.
Suitable only for an attended-gate visual check of Softnet's filesystem writes,
not as a lossless automated gate.

### Parent-side env/argv logging

The supervisor logs the exact environment it passes to Tart before calling exec.
This is deterministic and complete — it is not observation of the child but
recording of the supervisor's own state. It is the strongest possible evidence
for facts the supervisor controls. The perturbation is zero: the supervisor logs
its state before launching, so logging cannot affect the launch.

---

## Recommended design

**The smallest defensible design is a layered evidence model.**

The layers, in order of strength:

### Layer 1: Static artifact identity (strongest — already proven)

- Exact Softnet SHA-256 `ab333619...` at root-owned path with mode `04550`
- Exact Tart SHA-256 `05b65d5c...`
- Exact Screen SHA-256 `07b706b7...` with version 4.00.03
- Complete directory tree ownership, modes, and ACL absence
- Exact manifest.json binding all the above
- No mutable Homebrew setuid path (doctor confirmation)

This layer is proven by `boxwarden doctor` + V3 attended init gate. It is
stronger than any runtime observation because it proves the artifacts cannot
have changed without root access to the installation tree.

### Layer 2: Supervisor-controlled launch evidence (strong — part of V4 design)

- Supervisor's exact constructed Tart argv (recorded before exec)
- Supervisor's exact constructed environment for Tart (recorded before exec),
  containing only the required variables and PATH equal to the digest directory
- Pre-launch re-verification of Tart and Softnet digests by the supervisor
- Supervisor's own supplementary groups at launch time

This layer is deterministic and complete because the supervisor owns all of
this state. No observation race or external tool is needed.

### Layer 3: Short-duration attended runtime polling (moderate — adds process fact evidence)

- `proc_pidinfo` on the Softnet PID returning `pbi_uid=0`, `pbi_ruid=<operator_uid>`,
  `pbi_svuid=0`, `pbi_ppid=<tart_pid>` — proves privilege transition and ancestry
- Process exit verification after VM stop (no Softnet process remains)
- Screen retention and cleanup verification (attach/detach + post-stop inventory)

This layer uses non-mutating read-only OS calls. It has a theoretical race window
for process credential polling, but Softnet is a long-running daemon and the
window is not practical. The race is explicitly acknowledged in the gate record.

### Layer 4: Externally observable behavior (direct functional proof)

- Network policy tests (private/link-local denial, public internet, session
  isolation, DHCP) run from within a running VM with the production supervisor
  environment — the same tests as Task 0, re-run under V4 conditions
- Filesystem write check: pre-launch baseline, post-run comparison of
  expected write locations

This layer provides the most operationally meaningful proof: the qualified
network policy holds for the exact running configuration.

### Why this design is sufficient for the ADR 024 threat model

The ADR 024 threat is a malicious guest root. That adversary cannot:
- Alter the root-owned install tree (Layer 1 remains valid)
- Affect the supervisor's pre-launch state (Layer 2 remains valid)
- Change process credentials after the setuid exec is complete (Layer 3 polling
  of the stable credential state remains valid)
- Bypass the Softnet network policy that Tart/Softnet enforce at the host kernel
  level (Layer 4 observable network behavior)

The design does not claim to be lossless against a hostile operator-UID
adversary. It is not required to make that claim under ADR 024.

---

## Exact proposed attended procedure

This is a proposed sequence. It must not be executed until the design is accepted
by the maintainer.

### Prerequisites

1. `boxwarden doctor` reports `status: healthy` (Layer 1 verified).
2. V4 supervisor implementation is complete and passing deterministic tests.
3. A disposable stopped clone exists on the qualified host.
4. No previous Softnet/Tart/Screen processes are running.

### Gate sequence

**Step 1: Pre-launch baseline**

```bash
# Record current Softnet/Tart/Screen process inventory — must be empty
pgrep -x softnet || echo "softnet: not running (expected)"
pgrep -x tart   || echo "tart: not running (expected)"
# Record filesystem baseline for relevant paths
find /Library/Boxwarden /tmp /var/tmp -newer /Library/Boxwarden/toolchains \
  -not -path '*/boxwarden-research*' 2>/dev/null | head -20 || true
# No mutation of host state occurs in this step
```

**Step 2: Supervisor launch with environment logging**

```bash
# The supervisor logs its constructed Tart argv and env before exec.
# Verify that logged env contains exactly:
#   PATH=/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619.../
#   HOME=<operator_home>
#   USER=<operator_name>
#   LOGNAME=<operator_name>
#   TART_HOME=<configured_tart_home>
#   TMPDIR=<private_generation_tmpdir>
#   LANG=en_US.UTF-8 (or exact qualified value)
# Verify logged env does NOT contain any of:
#   DYLD_* LD_* RUST_* SENTRY_* https_proxy http_proxy no_proxy ALL_PROXY
#   or any other variable not in the approved list
```
**Privileges required:** None beyond operator level (supervisor constructs
its own env before the exec).

**Step 3: Process credential observation (no privilege needed if supervisor can observe its own process tree)**

```bash
# After Tart starts, find the Softnet PID (child of Tart)
TART_PID=$(pgrep -x tart | head -1)
# Wait briefly for Softnet to appear as a Tart child
sleep 1
SOFTNET_PID=$(pgrep -P "$TART_PID" softnet | head -1)
# Record credential state — requires proc_pidinfo at privileged level
# This would be implemented in the supervisor which already holds Tart's PID
# Expected output: euid=0, ruid=<operator_uid>, svuid=0, ppid=<tart_pid>
```

**Privileges required:** The supervisor runs as the operator UID. Calling
`proc_pidinfo` on a process owned by the same user is permitted. However,
to read another user's process (root-owned Softnet), the OS may require
elevated privilege or the caller to be root. **This is an open question
requiring testing.** If unprivileged `proc_pidinfo` on a root-owned process
is denied, the supervisor would need to (a) use a root helper, or (b) rely
only on Layers 1+2+4 for F6.

**Step 4: Network policy tests (same as Task 0 tests, run from guest)**

The attended operator runs the Task 0 network policy tests from within the
running VM:
- Private/link-local TCP to a controlled host-side listener → denied
- Session-to-session TCP → denied (if two VMs run)
- Public internet TCP/80 and HTTPS → succeeds
- DNS resolution → succeeds

**Privileges required:** None beyond SSH into the guest (already proven).

**Step 5: Serial channel and Screen verification**

```bash
# Verify Screen holds the operator PTY
screen -ls | grep -i boxwarden  # or supervisor-specific session name
# Attach, verify retained output, detach with Ctrl-A d
# Verify all processes remain after detach
pgrep -x screen && pgrep -x tart
```

**Privileges required:** None.

**Step 6: Filesystem write check**

```bash
# After a short run (e.g., 30 seconds), compare filesystem state
# Look for unexpected writes outside the private generation TMPDIR
find /Library/Boxwarden /etc /var/db \
  -newer /Library/Boxwarden/toolchains/softnet/0.19.0/ab333619.../softnet \
  2>/dev/null | grep -v "^$"
# Expected: nothing except known system files (e.g., syslog, etc.)
```

**Privileges required:** None for the paths listed; root may be needed for
some system paths.

**Step 7: Stop and cleanup verification**

```bash
# Stop the VM through the supervisor
# Wait for Tart exit
# Verify Softnet has exited within bounded time (5–10 seconds, per Task 0)
sleep 6
pgrep -x softnet && echo "FAIL: softnet still running" || echo "PASS: softnet exited"
pgrep -x screen  && echo "FAIL: screen still running"  || echo "PASS: screen exited"
# Verify runtime directory is clean (both PTY links and Screen metadata removed)
```

**Privileges required:** None.

---

## Failure and stop conditions

The attended gate must stop and record a fail if any of the following occur:

1. `boxwarden doctor` reports any non-healthy finding before launch.
2. The supervisor-logged Tart environment contains any prohibited variable
   (DYLD_*, LD_*, RUST_*, proxy variables, or any variable not in the approved list).
3. Tart does not produce a Softnet child process within a bounded timeout (30 seconds).
4. The Softnet process credential poll returns `pbi_uid != 0` or `pbi_ppid != <tart_pid>`.
5. Any private/link-local network destination succeeds from the guest.
6. Any session-to-session network access succeeds.
7. Unexpected filesystem writes are found outside the generation-private TMPDIR.
8. Screen exits before the VM is stopped.
9. Softnet does not exit within 10 seconds after Tart exits.
10. Any runtime directory artifact (PTY links, Screen socket, session metadata)
    remains after the supervisor completes cleanup.

A `proc_pidinfo` permission denial when attempting to read root-owned Softnet
credential state (Step 3) is not automatically a gate failure — it is an open
question requiring separate investigation (see Architecture Impact).

---

## Open questions

1. **proc_pidinfo privilege for root-owned process:** Can the supervisor
   (running at operator UID) call `proc_pidinfo(PROC_PIDTASKALLINFO)` on the
   Softnet process, which runs as root? macOS generally restricts this. If denied,
   the credential poll in Step 3 requires either a root helper or must be omitted,
   relying on static mode proof alone. This needs a direct test on the qualified
   host before committing to the proc_pidinfo approach.

2. **Softnet privilege drop:** Does Softnet 0.19.0 drop privileges after vmnet
   setup? An upstream source code review of
   `https://github.com/cirruslabs/softnet` at the 0.19.0 tag would answer this.
   The answer affects whether the "effective UID stays 0" poll is the expected
   result, or whether a post-setup drop to operator UID is expected.

3. **Tart's Softnet argv construction:** The exact argv Tart passes to Softnet
   is an internal detail of Tart 2.32.1. If the upstream Tart source were
   reviewed at that exact commit, the mapping from `--net-softnet` flag to
   Softnet argv could be proven statically. This would strengthen F3 evidence.

4. **ES entitlement availability:** Does Apple's developer program provide
   the `com.apple.developer.endpoint-security.client` entitlement for
   non-App-Store tools used for security research on a development device?
   If the project or maintainer holds an appropriate membership, obtaining this
   entitlement for an attended-gate observer tool would provide the strongest
   possible privilege-transition evidence (event-driven kernel guarantee, no
   race window). This is worth investigating but should not block the current
   design.

---

## Architecture impact

**The layered evidence design itself requires no architecture change.** The
supervisor already records its pre-launch state as part of its manifest, and
the network policy tests are procedurally the same as Task 0 tests.

**One decision requires maintainer approval:**

> **Decision required:** Accept `proc_pidinfo`-based polling (not event-driven;
> subject to a narrow race window for short-lived processes) as adequate evidence
> of the Softnet privilege transition and process ancestry for the V3/V4 attended
> runtime gate, justified by: (a) Softnet is a long-running daemon, (b) static
> mode `04550` provides corroboration, (c) the ADR 024 threat model does not
> include a hostile operator UID adversary, and (d) no lossless mechanism is
> available on SIP-enabled macOS without Apple provisioning.

If the maintainer instead decides that the ES entitlement should be obtained for
a purpose-built qualification observer tool, that would be a separately approved
addition to the gate procedure, not a change to the accepted ADRs.

The `proc_pidinfo`-for-root-process privilege question (Open Question 1) may
also require an architectural decision: if root privilege is required to read
Softnet's credential state, the supervisor would need either a temporary root
helper (which adds a new privileged code path) or the credential poll would be
omitted from the gate (relying on static evidence for F6). Either outcome is
acceptable under the ADR 024 threat model, but it requires maintainer
acknowledgment.

---

## Sources/evidence

### Repository files (observed facts)

- `docs/architecture.md` — threat model, qualified toolchain, supervisor design
- `docs/security-model.md` — ADR 024 boundary, setuid explanation, M1A threat model
- `docs/decisions/024-qualified-softnet-privilege-binding.md` — exact decision text and boundary statement
- `docs/decisions/017-host-local-serial-recovery-shell.md` — Screen/PTY topology
- `docs/evidence/v3-host-domain-attended-gates.md` — exact unqualified gates list
- `docs/evidence/m1a-task0-final-summary.md` — qualified toolchain versions, network test results, process ownership evidence
- `docs/evidence/m1a-bootstrap-spike.md` — "Tart parented Softnet", process reparenting observation, foreground harness ownership evidence
- `docs/superpowers/plans/2026-09-01-boxwarden-v0.1.md` — V4 supervisor design, closed environment specification
- `docs/operations/init-and-doctor.md` — manifest mode, doctor behavior
- `docs/tool-provenance.md` — exact digests, mode `04550`
- `internal/hostx/identity.go` — compiled-in qualified digests and mode constants
- `internal/hostx/doctor.go` — doctor check implementation
- `internal/backend/tart/observer.go` — Tart observation seam
- `internal/execx/runner.go` — bounded argv runner, no-shell enforcement

### SDK header examination (observed facts about available APIs)

- `/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk/usr/include/EndpointSecurity/ESMessage.h`
  — ES exec event structure, setuid event guarantee, `last_fd` availability,
  entitlement requirement (`com.apple.developer.endpoint-security.client`),
  TCC requirement (Full Disk Access)
- `/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk/usr/include/EndpointSecurity/ESMessageCore.h`
  — `es_process_t` structure with `audit_token`, `ppid`, `original_ppid`,
  `codesigning_flags`
- `/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk/usr/include/sys/proc_info.h`
  — `proc_bsdinfo` structure: `pbi_uid`, `pbi_ruid`, `pbi_svuid`, `pbi_ppid`
- `/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk/usr/include/bsm/audit_kevents.h`
  — BSM event types AUE_EXEC (7), AUE_EXECVE (23), AUE_SETUID (200)
- `/usr/bin/execsnoop` — DTrace wrapper (shell script), confirms execsnoop is a
  DTrace frontend using `proc:::exec-success` probe
- `/usr/sbin/dtrace` — present on the qualified host at `/usr/sbin/dtrace`
- `csrutil status` output — "System Integrity Protection status: enabled"
- Absence of `/etc/security/audit_control` — BSM audit not configured

### External references (inference / documented facts)

- Apple ESMessage.h comment (line 52): "Kernel events are mandatory. There is no
  `setuid` syscall that ES does not interdict." — This is a documented Apple
  security guarantee, not an inference.
- Apple ESMessage.h comment (lines 241–243): "The `audit_token_t` structs
  contained in the two different `es_process_t` structs will not be identical:
  the pidversion field will be updated, and the uid/gid values may be different
  if the new program had setuid/setgid permission bits set." — Documents that
  ES exec events capture post-setuid credential state.
- Apple developer documentation (referenced in ESClient.h line 638):
  `com.apple.developer.endpoint-security.client` entitlement is required;
  TCC Full Disk Access must be approved.

### Inference (not observed facts)

- The claim that `proc_pidinfo` requires root privilege to read another user's
  (root-owned) process credential state is a reasonable inference from macOS
  security model; it should be confirmed by direct test on the qualified host
  before relying on it (see Open Question 1).
- The claim that DTrace `proc:::exec-success` is blocked by SIP is based on
  the macOS SIP documentation and known behavior on Apple Silicon macOS; the
  exact probe availability on macOS 26.6.2 / 25G83 should be confirmed.
- The claim that Softnet is a long-running daemon (enabling the proc_pidinfo
  polling approach) is inferred from its role as a network manager for the
  lifetime of the VM, consistent with Task 0 observations. This should be
  confirmed at runtime.

### Recommendations (not facts)

The layered evidence design and the specific procedure above are recommendations
of this research document. They are not accepted architecture until the
maintainer reviews and accepts them.
