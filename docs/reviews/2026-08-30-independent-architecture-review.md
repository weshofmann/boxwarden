# Boxwarden Independent Architecture Review

Date: 2026-08-30
Reviewed commit: `9ba73679de43a3ddfb7ddd095fd764806336c3dd` (`origin/main`)
Reviewer scope: architecture, threat model, security design, state/persistence/lifecycle models, host/backend abstraction, and the Milestone 1A implementation plan. No product implementation was changed.

Reconciliation note: this is preserved as a dated independent review, not as the current architecture authority. The accepted dispositions are incorporated into the canonical documents and later ADRs. Task 0 confirmed this review's `@host` and gateway-DNS predictions, but ADR 015 supersedes its proposed static-resolver/host-block remediation: M1A prioritizes inherited host/VPN/DNS64 connectivity and accepts vmnet-gateway reachability as a documented risk pending a compatible finer isolation mechanism. Checkpoints remain deferred, exact launch behavior remains evidence-gated, and the recommendation to reduce the vendor-tool target was not adopted.

Evidence base for this review: the repository documents listed under "Documents reviewed"; the locally installed Tart 2.32.1 and Softnet CLI surfaces on macOS 26.6.2 / arm64; Cirrus Labs' Softnet documentation; Canonical's `cdimage.ubuntu.com` release listing, subiquity release notes, and archive snapshot service documentation; and vendor download documentation for ChatGPT Desktop for Linux and Google Antigravity. Facts established empirically or from vendor documentation are recorded separately in `memory/knowledge/tart-and-guest-platform-facts.md` with their provenance.

## Executive verdict

**IMPLEMENT WITH CONDITIONS.**

The architecture is sound, unusually disciplined about scope, and materially better than the document set the previous review rejected. All eight of that review's concerns have been addressed in substance, and several of them well beyond the minimum. I recommend proceeding to Task 0 **after** the single blocker below is resolved as a documentation and launch-policy correction, which is cheap and requires no architectural change.

One blocker. Seven major findings. The blocker is not a design flaw; it is a factual error about the M1A backend that the security model currently states as a guarantee.

## Summary

Boxwarden's central thesis — the VM is the trust boundary for autonomous agents, OCI is the boundary for the workloads those agents build, and everything that crosses from a disposable session back into trusted state must be explicitly, narrowly, and reviewably admitted — is correct and consistently applied. The state model's two independent axes (CONFIDENTIALITY × EXECUTION TRUST) are the load-bearing insight of the whole design, and the decision to support only named declarative profile adapters rather than generic archives is the single most valuable scope decision in the repository.

The most important problem I found is not in the design but in an assertion about it. `docs/security-model.md` states as a backend-independent policy property that the guest "cannot initiate connections to trusted-host services or private networks by default." For the M1A Tart/Softnet realization this is **false as written**. Softnet's documented default policy explicitly permits the guest to send traffic to the vmnet bridge gateway — which is the trusted macOS host. The launch line recorded throughout the repository does not close this. Softnet provides the exact mechanism needed (`--block @host`), so the correction is a one-flag policy change plus a guest DNS consequence, but it must land before Task 2 freezes the launch policy into tested argv.

Beyond that, the recurring pattern in the remaining findings is the same: the design correctly identifies where the trust boundary is, but several documents do not say **which side of it** a check runs on. Profile capture validation, the project durability guard, and the human approval rendering step are all described without stating whose bytes are being trusted. Each is cheap to fix in prose now and expensive to fix after code encodes the wrong reading.

A second pattern: the checkpoint feature is under-specified relative to everything around it, and it quietly contradicts an invariant that `AGENTS.md` states absolutely. I recommend deferring checkpoints out of M1A entirely.

I also want to record what the design gets right that a less careful reviewer would mistake for over-engineering — particularly the per-domain SSH user CA, which is not gold-plating but the only mechanism that delivers per-session credentials given the deliberate absence of any host-to-guest injection channel. See "Design choices reviewed and accepted."

## Documents reviewed

`AGENTS.md`; `LICENSE`; `NOTICE`; `memory/README.md`; `docs/architecture.md`; `docs/security-model.md`; `docs/state-model.md`; `docs/persistence-and-encryption.md`; `docs/memory-model.md`; `docs/credentials.md`; `docs/lifecycle-and-recovery.md`; `docs/provider-data-scope.md`; `docs/tool-provenance.md`; `docs/threat-model-node-npm.md`; `docs/kindex-profile-policy.md`; `docs/decisions/001`–`012`; `docs/superpowers/plans/2026-08-30-milestone-1a-disposable-ai-workstation.md`. There is no `README.md` at the repository root (see MINOR-5).

---

## Blockers

### BLOCKER-1 — Softnet permits guest→trusted-host traffic by default; the security model asserts the opposite

**Severity:** BLOCKER

**Relevant files/sections:**
- `docs/security-model.md`, "Backend-independent policy", third bullet
- `docs/security-model.md`, "M1A Tart realization", first two sentences
- `docs/architecture.md`, the `tart run --net-softnet --no-clipboard --no-audio <vm>` launch line
- `docs/decisions/003-softnet-and-minimal-host-integration.md`
- `AGENTS.md`, Softnet bullet
- Plan Task 2, step 2 ("maps that to exactly `tart run --net-softnet --no-clipboard --no-audio <validated-vm>`")
- Plan Task 9, step 5 (where the contradicting test is currently scheduled)

**Failure mode.** Tart 2.32.1's own help text documents the Softnet default policy. By default the VM is able to:

- send traffic from its own MAC address;
- send traffic from the IP address assigned to it by DHCP;
- send traffic to globally routable IPv4 addresses;
- **send traffic to gateway IP of the vmnet bridge** (normally `bridge100`);
- receive any incoming traffic.

The vmnet bridge gateway *is* the trusted macOS host. Softnet's own documentation confirms this and provides an `@host` alias for `--allow`/`--block` that exists precisely because the gateway is a distinct, meaningful policy target. So under the launch line recorded throughout this repository, a compromised sandbox can open TCP connections to any service the host has bound to a wildcard address.

This is not hypothetical: a normal macOS host may expose wildcard-bound services through the vmnet gateway. The public repository intentionally does not retain the reference machine's transient service/port inventory; the architectural evidence is that guest→host reachability creates a real attack surface on the machine holding Boxwarden's host-only identities.

**Why it matters.** The trusted host is, by design, the most valuable target in the system: it holds the age private identities (`docs/persistence-and-encryption.md`) and the per-domain SSH user-CA private key (ADR 012). The threat model's entire premise is that a sandbox will sometimes execute hostile code. Handing that hostile code reachable authentication or interactive-control surfaces on the host inverts the primary security objective. Reaching a port is not the same as breaching it, but "the guest cannot initiate connections to trusted-host services" is currently written as a property the architecture *provides*, and it does not.

This is a blocker rather than a major finding for a specific reason: Plan Task 2 step 2 instructs the implementer to construct an **immutable launch policy** and assert it maps to *exactly* that argv, with tests. Implementing Task 2 as written encodes the hole into the tested contract. The test that would have caught this is scheduled in Task 9, after eight intervening tasks of dependent work.

**Minimal recommended correction.**

1. Correct `docs/security-model.md` so the backend-independent property names a mechanism rather than asserting an outcome: the backend must deny guest-initiated traffic to the trusted host and to private address space, and the M1A realization must state how.
2. Add `--net-softnet-block=@host` to the M1A launch policy in `docs/architecture.md`, `docs/security-model.md`, ADR 003, and Plan Task 2. **Verify in Task 0 that `tart run` passes the `@host` alias through to Softnet** — Tart's own help documents only "comma-separated CIDRs" for `--net-softnet-block`, while Softnet's help documents the `@host` alias. If Tart does not forward it, fall back to blocking the discovered bridge gateway as a `/32`, resolved at launch time by the Tart adapter.
3. **Expect this to break guest DNS.** macOS vmnet shared networking normally advertises the gateway as the guest's resolver, and Softnet's CIDR policy has no port granularity — blocking the gateway is all-or-nothing. The guest definition (Task 4) must therefore configure a static external resolver. Confirm in Task 0 that DHCP itself still functions with `@host` blocked (DHCP is broadcast, so it should, but this is exactly the kind of assumption this finding exists to stop).
4. Move the guest→host and guest→private-network negative network tests **from Task 9 into the Task 0 gate**. They are the cheapest tests in the entire plan and they validate the property everything else rests on.
5. Defense in depth, recommended but not required: a host `pf` rule denying `bridge100`→host and disabling unnecessary wildcard-bound host services. Note that this costs nothing operationally — Boxwarden's management SSH is **host→guest**, so a strict "guest never initiates to host" policy does not affect any Boxwarden function.

**When:** Documentation and launch-policy correction **before implementation begins**. Empirical confirmation of `@host` pass-through, DHCP, and DNS **during Task 0**.

---

## Major findings

### MAJOR-1 — Profile capture validation is described without saying which side of the trust boundary performs it

**Severity:** MAJOR

**Relevant files/sections:** `docs/persistence-and-encryption.md`, the "Capture reads only adapter-allowlisted objects…" paragraph; `docs/security-model.md`, "Anything admitted from a disposable session…"; Plan Task 7 steps 2, 5, and 7.

**Failure mode.** Capture necessarily *reads* from inside the guest, and `cmd/boxwarden-guest` is the natural place to walk the filesystem and build the envelope. The documents specify a thorough rejection list (absolute paths, `..`, symlinks, hard links, devices, sockets, setuid/setgid, duplicates, destination escapes, size and count bombs) but never state where those rejections are authoritative. If an implementer reads "capture normalizes them into an adapter-owned candidate" as describing guest-side work, then a compromised session simply emits a hand-crafted envelope that never passed those checks, and the host stores, encrypts, and later restores it. Every downstream control — digest binding, approval receipts, replay rejection — then faithfully protects malicious bytes.

Note that the restore path has the mirror-image ambiguity: `docs/persistence-and-encryption.md` says "The guest validates the staged tree and semantic content before applying it." That is a correctness check, not a security control — validating host-supplied data inside the guest protects nothing that matters, since the guest is the party being protected *from*.

**Why it matters.** Persistence poisoning is the specific attack this entire subsystem exists to prevent. This is the one place where a wrong-but-natural implementation choice silently voids it.

**Minimal recommended correction.** Add one explicit sentence to `docs/persistence-and-encryption.md`: every structural and semantic check is re-performed **on the trusted host**, on the exact received bytes, before encryption and before any candidate record is created; guest-side checks are a convenience that fail fast and are never relied upon. Mirror it in Plan Task 7 step 5, and add a negative test in which a deliberately malformed envelope is injected as if from a hostile guest.

**When:** Documentation correction before implementation begins; enforced during Task 7.

### MAJOR-2 — The human approval step renders attacker-controlled bytes with no output-sanitization requirement

**Severity:** MAJOR

**Relevant files/sections:** `docs/persistence-and-encryption.md`, semantic diff and review receipt; Plan Task 7 step 6 (`profile inspect` "verifies and decrypts the candidate for a transient semantic diff"); `docs/memory-model.md`, "Reviewers inspect the exact Markdown diff".

**Failure mode.** `profile inspect` decrypts guest-originated content on the trusted host and renders a semantic diff to a human, who then types an approval that binds those bytes into durable trusted state. The content is Markdown and Git configuration produced inside a potentially compromised session. Nothing in the design requires that rendering be sanitized. Terminal control sequences (C0/C1, ANSI cursor movement, OSC), Unicode bidirectional overrides, zero-width characters, and homoglyphs can all make a terminal display benign text while different bytes are what actually get approved and promoted.

This matters more here than in a typical diff-review setting because human approval is not one control among many — it is *the* control. `docs/security-model.md` states that anything admitted from a disposable session is a persistence attempt until reviewed. If the review can be spoofed by the material under review, admission is unauthenticated.

The same exposure applies to Layer-2 Markdown memory promotion, where `docs/memory-model.md` asks reviewers to inspect the exact diff for "malicious instructions" — content that is, by construction, adversarially authored.

**Why it matters.** It converts the design's strongest guarantee (nothing crosses without explicit human approval bound to exact digests) into a guarantee that the human approved *something*, not necessarily what they saw.

**Minimal recommended correction.** Require the diff/inspect renderer to escape or strip C0/C1 and ANSI/OSC sequences; visibly mark bidi controls, zero-width characters, and non-ASCII confusables rather than rendering them transparently; display byte counts and both digests adjacent to the rendered content; and cap rendered size with an explicit truncation notice. Add negative tests using escape-sequence and bidi payloads. Extend the same expectation to memory promotion review in `docs/memory-model.md`.

**When:** Before Task 7.

### MAJOR-3 — Isolation between concurrently running sessions is load-bearing but is stated nowhere

**Severity:** MAJOR

**Relevant files/sections:** `docs/security-model.md`, "Backend-independent policy" bullet list; `docs/state-model.md`, SECURITY DOMAIN paragraph; ADR 003; ADR 011.

**Failure mode.** The design actively encourages running a credential-free quarantine session — explicitly for hostile code — while an interactive session holding every provider login and Git credential is also running. Those VMs share a vmnet bridge. Two sessions in *different* security domains (`personal` and `work`) likewise share it. No document states that concurrently running sessions must be unable to reach one another.

I verified that M1A is safe here **by accident rather than by design**: Softnet enables vmnet bridge isolation by default and additionally filters MAC and source-IP spoofing, so VM↔VM traffic is blocked and one VM cannot impersonate another's address. Two consequences follow. First, an unwritten property cannot be tested, and Plan Task 10 step 1 requires tracing every canonical invariant to a test. Second, Softnet documents that `--allow=0.0.0.0/0` **additionally disables bridge isolation** — so a future "just let this one VM reach the LAN" change silently removes inter-session isolation as a side effect that its author would have no reason to expect.

There is a further consequence worth stating plainly in the docs: security-domain separation is currently described as a control-plane namespace property only. It is *not* a runtime network property, and a reader will assume otherwise unless told.

**Why it matters.** Quarantine's entire value proposition is that hostile code runs somewhere it cannot reach anything valuable. If the credentialed session is one hop away on a shared L2 segment, quarantine is contained only as long as an undocumented default holds.

**Minimal recommended correction.** Add to the backend-independent property list: concurrently running sessions cannot reach one another over the network, whether in the same or different security domains. Record in ADR 003 and `AGENTS.md` that `--net-softnet-allow=0.0.0.0/0` disables bridge isolation and is therefore **forbidden outright**, not merely subject to review. Add a two-VM reachability test (one attempts to reach the other's SSH port) to Task 0 and to the Task 9 property matrix.

**When:** Documentation before implementation; test in Task 0.

### MAJOR-4 — Checkpoints have no restore path, contradict the clone-identity invariant, and are invisible to compromise recovery

**Severity:** MAJOR

**Relevant files/sections:** `AGENTS.md`, clone-identity bullet; `docs/lifecycle-and-recovery.md`, "Session classes" and "Compromise recovery"; `docs/state-model.md`; ADR 009; Plan Task 5 step 5.

Three coupled defects in one feature.

**(a) No restore path exists.** No document and no plan task defines an operation that creates a runnable session *from* a checkpoint. Plan Task 5 step 5 defines only creation ("Clone to a distinct recorded checkpoint object"). As specified, an M1A checkpoint is write-only: it can be created, listed, aged, warned about, and destroyed, but never used. A convenience artifact that cannot be resumed is not a convenience.

**(b) Resuming one contradicts an absolute invariant.** `AGENTS.md` states without qualification: "Every clone receives a unique MAC address, machine ID, SSH host keys, DHCP identity, and other machine-specific seed material." That invariant is achievable because the *golden* is finalized clone-ready and regenerates identity on first boot. A checkpoint is a clone of a live, un-finalized session, so it carries its parent's `/etc/machine-id` and SSH host keys. Cloning one checkpoint twice therefore produces two VMs with identical SSH host keys — which defeats per-session known-hosts pinning (ADR 012) as a means of distinguishing one session from another. This is a genuine contradiction within the canonical invariant set, and Plan Task 10 step 1 will trip over it when it tries to trace the invariant to a test.

**(c) Compromise recovery does not see them.** `docs/lifecycle-and-recovery.md` prescribes: revoke credentials, destroy with `--compromised`, recreate from a promoted golden. It never mentions checkpoints. But a checkpoint is a durable, secret-bearing, restorable copy of a session's disk, and compromise is frequently discovered *after* the fact. A checkpoint taken before the compromise was noticed is precisely a persistence mechanism: restore it later and the attacker's state — shell startup files, hooks, MCP definitions, autostart entries, browser extensions, all the things `docs/security-model.md` enumerates — comes back with it. Nothing records checkpoint→parent lineage, so `--compromised` cannot enumerate what it needs to contain.

**Why it matters.** (c) is a straightforward hole in the system's central claim that a compromised sandbox must not poison future sessions. (b) is a contradiction that will force an arbitrary implementation-time decision if not resolved in the documents first.

**Minimal recommended correction.** Cheapest and my recommendation: **defer checkpoints out of M1A** and add them to "Explicitly deferred". They prove nothing about the disposable-workstation architecture, and the milestone's thesis is that destruction is cheap and routine — a retention feature is in mild tension with that thesis anyway.

If checkpoints are retained: define the resume operation explicitly; carve a documented, narrow exception to the identity invariant in `AGENTS.md` and ADR 009 for checkpoint-derived sessions (regenerate SSH host keys and MAC on resume, accept the inherited machine-id, re-pin known-hosts); record parent session UUID and golden revision as checkpoint lineage in the registry; and make `session destroy --compromised` enumerate every checkpoint derived from the compromised session and either destroy them or refuse to report containment complete while they exist.

**When:** Before Task 5.

### MAJOR-5 — The normal-destroy durability guard trusts a report from the guest it is about to destroy

**Severity:** MAJOR

**Relevant files/sections:** `docs/lifecycle-and-recovery.md`, "Projects used by normal sessions are registered…"; Plan Task 8 steps 1–2; `AGENTS.md`, routine-destruction bullet.

**Failure mode.** The guard verifies "registered Git worktrees have no modified or untracked files, no unpushed commits, a configured upstream, and a reachable remote where the policy requires it." Those facts exist only inside the guest, so the check runs there and the trusted host consumes its output to decide whether to destroy a disk irreversibly. A compromised session can report "clean and pushed" and induce the host to destroy the evidence; a merely broken one (corrupt index, wrong working directory, a `git` shim on `PATH`) can do the same by accident.

The documents are correct that `--compromised` does not trust guest Git checks. What is missing is any statement of the *normal* path's trust basis. As written it reads as a strong guarantee, and a reader will reasonably conclude their work is safe because Boxwarden checked.

**Why it matters.** This guard exists because routine destruction is the design's core hygiene behavior. Users will rely on it heavily and precisely to the extent they trust it. It fails in the direction of data loss, and it fails silently.

**Minimal recommended correction.** Two parts, both cheap.

1. State the trust basis explicitly in `docs/lifecycle-and-recovery.md`: the normal durability check is a **safety** control against accidental loss, not a **security** control against a hostile guest, and its guarantees are void once compromise is suspected — which is why `--compromised` exists as a separate path.
2. Add the one host-side corroboration available at no cost: have the trusted host run `git ls-remote` against the registered remote and confirm that the head commit the guest claims to have pushed actually exists there. This turns an unverifiable guest assertion into a host-verified fact for the case that matters most. Fail closed when the host cannot corroborate and the project's policy requires a reachable remote.

**When:** Documentation correction before implementation; `ls-remote` corroboration during Task 8.

### MAJOR-6 — `tart run` opens a UI window and requires a logged-in GUI session; the reference host is a Mac mini

**Severity:** MAJOR

**Relevant files/sections:** `docs/architecture.md`, launch line; `docs/lifecycle-and-recovery.md`, "`tart run` is a supervised long-lived process with durable logs"; Plan Task 2 step 4; Plan Task 5 step 3; Plan Task 9 step 2 (host validation).

**Failure mode.** The M1A launch line deliberately omits `--no-graphics`, because GUI access is supposed to happen through the Tart console. That means `tart run` must execute inside a logged-in macOS Aqua session. Two operational consequences follow, neither of which is addressed:

1. `boxwarden session start` invoked over SSH into the Mac mini, from a launchd *daemon* context, or after the console user logs out, will fail or misbehave — leaving a recorded `running` intent that reconciliation cannot satisfy. A headless-administered Mac mini is the stated reference host, and administering it over SSH is the obvious usage pattern.
2. A `tart run` spawned as an ordinary child of the CLI process dies when the invoking shell or SSH session ends. The plan says "supervised long-lived process with a process handle" but never specifies the detachment and supervision mechanism, which is the part that determines whether sessions survive the CLI exiting.

A related trap in the same area: recording a bare PID in session state is unsafe across control-plane restarts, because PIDs are recycled. Task 5 step 6 tests "stale PID" but the state schema in step 1 lists no field that would make staleness detectable.

**Why it matters.** This determines whether the product works at all in its intended deployment, and it shapes the lifecycle state schema — which is expensive to change once written.

**Minimal recommended correction.** Decide and document the supervision model before Task 5. A per-user launchd **agent** running in the Aqua session is the natural fit: the CLI records intent and asks the supervisor to start the VM, rather than parenting the VM to itself. Add "a logged-in GUI session is available" to `validate host`. Record **PID plus process start time** (and ideally the Tart object ID) in session state so reconciliation can distinguish a live supervised process from a recycled PID, and never signal a PID whose start time does not match.

**When:** Observe in Task 0; design before Task 5.

### MAJOR-7 — Softnet constrains address discovery to one resolver, and the constraint is unwritten

**Severity:** MAJOR

**Relevant files/sections:** Plan Task 6 step 3 (`Backend.ManagementAddress`); `docs/lifecycle-and-recovery.md`, "the M1A Tart adapter uses `tart list --format json` and other documented Tart inspection commands"; ADR 003 and `AGENTS.md` (guest-agent prohibition).

**Failure mode.** Tart offers three IP resolution strategies, and Boxwarden's own constraints eliminate two of them:

- `arp` — Tart's help states plainly that it "won't work for VMs using the Softnet networking."
- `agent` — requires the Cirrus Labs `tart-guest-agent` inside the VM. ADR 003 and `AGENTS.md` forbid guest-agent bridges without separate architecture review, and it is also the component that provides clipboard sharing, which is deliberately disabled.
- `dhcp` (the default) — parses the host DHCP lease file for the VM's MAC. This is the only permissible option.

Nothing records this. An implementer choosing a resolver, or debugging discovery failures under load, will re-derive it — or worse, reach for `tart-guest-agent` because it is documented as "works in all cases reliably," quietly installing a host↔guest bridge that the architecture forbids.

There is a second-order behavior worth proving in Task 0: Softnet shortens the DHCP lease from 86,400 to 600 seconds specifically so ephemeral VMs do not exhaust the pool. Address discovery and any cached address must therefore tolerate lease renewal and, in principle, address change during a long-running session.

**Why it matters.** Task 6 depends entirely on address discovery working, Task 0 does not currently test it under Softnet, and the "obvious" fix for a discovery problem is the one option that breaches an architectural boundary.

**Minimal recommended correction.** Pin `--resolver=dhcp` in the Tart adapter and record in ADR 003 or `docs/lifecycle-and-recovery.md` why the other two are unavailable — one for a technical reason, one for a policy reason, and the distinction matters. Add to Task 0: prove address discovery under Softnet, and observe behavior across at least one lease renewal.

**Positive note in the same area:** Softnet's anti-spoofing (a VM may send only from its own MAC and its DHCP-assigned IP) is what makes MAC→IP lease-file discovery trustworthy in the presence of a hostile co-resident VM. That is a real security dependency of the management path and deserves to be written down rather than relied on tacitly.

---

## Minor findings

### MINOR-1 — ADR 012's open host-key question has a concrete candidate the documents do not consider

**Files:** `docs/decisions/012-domain-ssh-user-ca.md`; `docs/credentials.md` final paragraph; Plan Task 0 step 4.

The documents correctly refuse to assume an answer and pre-authorize explicit TOFU as a fallback. But Tart 2.32.1 offers `tart run --serial-path <path>` ("attach an externally created serial console… for programmatic integrations"), which is a hypervisor-mediated, non-network channel from the guest to the host. A first-boot unit that emits the freshly generated SSH host key fingerprint on that console, read by the control plane from a host-created PTY, would let Boxwarden pin the correct key on the very first connection and **avoid TOFU entirely**.

Task 0 should evaluate this before accepting TOFU. Caveats to carry into that evaluation: ensure no getty runs on the serial console, or it becomes a login surface; the channel is bidirectional, so treat everything read from it as untrusted, bounded input (parse only a delimited fingerprint line, cap bytes and wall time); and confirm which guest device the console appears as on Ubuntu ARM64 under Virtualization.framework. This is a host↔guest channel and so deserves an explicit note against ADR 003's guest-agent prohibition — a one-way console read at boot is materially narrower than a persistent bidirectional agent, but the distinction should be argued in the ADR rather than assumed.

**When:** Task 0.

### MINOR-2 — Task 3 and Task 4 are circularly dependent

**Files:** Plan Task 3 step 5 and its verification block; Plan Task 4 verification block.

Task 3 step 5 builds a golden candidate "from the portable guest-definition digest," but the guest definition (autoinstall, provisioning, manifests, tests) is created in Task 4. Task 4's verification then invokes `boxwarden golden build`, from Task 3. Neither task can complete as written.

**Correction:** split Task 3 into (3a) lock schema, qualification gates, and evidence — which stands alone and should precede Task 4 — and (3b) candidate build and promotion, which follows Task 4.

**When:** Before implementation (plan edit).

### MINOR-3 — Tart automatic pruning and TART_HOME capacity are unmanaged

**Files:** Plan Task 9 step 2 (host validation); `docs/tool-provenance.md`, final paragraph.

`tart clone` performs automatic pruning when TART_HOME lacks space, with a default `--prune-limit` of 100 GB. I verified this **does not** delete local VMs — it evicts least-recently-used entries from the OCI image cache and IPSW cache only — so goldens and sessions built locally are safe today, and this is not the alarm it first appears to be. Two residual points remain worth acting on:

1. It is still capacity-triggered deletion inside TART_HOME, triggered implicitly by an operation the control plane initiates. Set `TART_NO_AUTO_PRUNE` in the `execx` environment for deterministic behavior.
2. `docs/tool-provenance.md` contemplates remotely distributed golden images referenced by digest. Those would live in the prunable OCI cache, at which point automatic pruning *can* evict a golden. Record the caveat now.

Separately, `validate host` should check free space in TART_HOME, and `status` should report committed disk, given 120 GB session disks against a Mac mini's internal SSD.

**When:** Task 9 (host validation); the `TART_NO_AUTO_PRUNE` decision can land whenever `execx` is written in Task 1.

### MINOR-4 — `memory/knowledge/` and `memory/lessons/` are documented but absent

**Files:** `docs/memory-model.md`, Layer 2 layout block; `memory/README.md`.

Both documents describe a directory layout the repository does not contain — Git does not track empty directories. This review creates `memory/knowledge/` with real content; `memory/lessons/` remains absent until there is a lesson to record, which is the correct outcome but should not surprise a future reader.

### MINOR-5 — No `README.md` at the repository root

The repository is public and Apache-2.0 licensed with no front door. Plan Task 1 creates one; until then a visitor's first impression of Boxwarden is a bare file listing.

### MINOR-6 — `docs/operations/credentials.md` will collide with the existing `docs/credentials.md`

**Files:** Plan Task 8, Files list.

Two documents named `credentials.md` in adjacent directories with overlapping subject matter will drift, and future readers will not know which is authoritative. Rename the new one to something that states its role (`docs/operations/credential-runbook.md`) or fold the operational content into the existing document.

### MINOR-7 — Kindex path rejection is a denylist operating inside an allowlist system

**Files:** `docs/kindex-profile-policy.md`, "Profile path validation rejects `~/.kindex`…"; Plan Task 7 step 2.

The adapter model is an allowlist: only adapter-declared source paths are readable at all, which is what actually excludes Kindex state. The explicit Kindex denylist is sound belt-and-braces and should stay, but the documents should say which of the two is the control. Otherwise a later implementer may treat the denylist as sufficient, and a relocated or renamed Kindex profile directory would then pass.

### MINOR-8 — No resource admission policy for concurrent sessions

**Files:** `docs/lifecycle-and-recovery.md`; Plan Task 9 step 2.

The reference host has 16 GB of RAM; the intended profiles are 8–10 GB; and the design explicitly encourages running a quarantine session alongside an interactive one. Two concurrent sessions will not fit. Nothing checks this, and nothing accounts for cumulative disk growth across sparse 120 GB session disks plus goldens.

**Correction:** have `session create`/`session start` check committed memory and free disk against configured limits and refuse or warn explicitly. Include ENOSPC during a state write in the Task 9 fault-injection matrix — the lifecycle model depends on being able to fsync intent before mutating, and that is exactly what fails when the disk fills.

---

## Optional improvements

### OPTIONAL-1 — Name `snapshot.ubuntu.com` in the provenance document

`docs/tool-provenance.md` carefully distinguishes a "reproducibly identified artifact" from an "indefinitely reproducible repository closure," and honestly concedes that M1A requires only the former. For the Ubuntu layer specifically, the stronger claim is available essentially free: Canonical's archive snapshot service exposes the Ubuntu archive as of any date and time since 2023-03-01, addressed by a UTC snapshot ID, and is supported by the apt version shipped in 24.04. Pinning golden builds to a snapshot ID converts the OS package closure from "best available" into an actual reproducible closure. Worth naming explicitly so the option is not rediscovered later.

### OPTIONAL-2 — Trim the M1A golden to a minimum viable set

Plan Task 3 step 3 qualifies roughly fifteen artifacts, and Task 9 step 9 gates the milestone on human GUI acceptance of four separate third-party AI desktop applications. None of that proves the disposable-workstation architecture; it is the highest-variance, least-architectural work in the plan, and it is gated on third parties' release engineering.

Consider gating M1A on Ubuntu + desktop + XWayland + SSH + Docker + **one** AI application, with the remainder recorded as lock entries pending qualification. Every one of them can be added later through the normal lock-update → rebuild → accept → promote path that M1A exists to build.

To be clear, this is a scope recommendation, not a feasibility concern. I verified that the artifacts exist: `ubuntu-24.04.4-desktop-arm64.iso` is published; Desktop autoinstall has been fully supported since 24.04.1; ChatGPT Desktop for Linux shipped 2026-08-11 with an arm64 `.deb` tested on Ubuntu 24.04; and Antigravity ships Linux arm64 with an apt repository exposing an arm64 index.

### OPTIONAL-3 — Pre-record ChatGPT Desktop's provenance limitations

The Linux app is a **public preview**, distributed as a `.deb` from a download endpoint rather than (as far as I could determine) a signed apt repository with retained history. Its digest will change with every vendor release and superseded versions are unlikely to remain retrievable. The provenance policy already handles this correctly — record the limitation rather than weaken the claim — but recording it in the lock up front avoids a mid-Task-3 debate. Prefer Antigravity's apt repository model where a choice exists, since a signed repository with a key fingerprint is a materially stronger provenance claim than a bare download URL.

### OPTIONAL-4 — Record `tart suspend` / `--suspendable` as deliberately unused

Tart supports suspending a VM. Doing so writes guest RAM — including every credential and decrypted secret live in that session — to host disk. That is a secrets-at-rest change requiring review. It is correctly absent from M1A; noting it as a deliberate exclusion prevents a future contributor from adding it as an obvious ergonomic win.

---

## Design choices reviewed and accepted

These are decisions I examined closely and concluded are correct, several of which look like over-engineering until the constraint that forces them is visible.

1. **Per-domain SSH user CA (ADR 012).** This is the strongest single design decision in the repository and the easiest to mistake for gold-plating. With no filesystem share, no cloud-init, no guest agent, and no port exposure, there is **no channel** by which the host can inject a per-session public key into a fresh clone. Baking the CA's *public* trust anchor into the golden is the only mechanism that yields short-lived, session-bound credentials without such a channel. The design is a consequence of the isolation constraints, not decoration on top of them.

2. **Guest definition vs host-specific golden artifact (`docs/architecture.md`).** The right portability seam. It keeps a future Linux/libvirt backend cheap without building a hypervisor abstraction now, and it correctly identifies that the *definition* is the portable asset while the image is not.

3. **Rejecting generic archive and opaque-state profile adapters (ADR 005).** The most valuable scope decision in the design. It converts an unbounded validation problem into a small number of bounded ones, and it is what makes the persistence-poisoning defenses tractable at all.

4. **The two independent classification axes (`docs/state-model.md`).** Separating CONFIDENTIALITY from EXECUTION TRUST is the insight that makes "safe to disclose is not safe to restore" operational rather than aspirational. It is what correctly catches AGENTS files, hooks, MCP definitions, and shell startup files as dangerous despite being plaintext-safe.

5. **Kindex exclusion and the `kin export` ≠ backup framing (ADR 007).** Technically accurate about SQLite/WAL semantics, honest about what interchange export omits, and correct to refuse the external-SQLite-copy workaround. The M1B gate is specific enough to be falsifiable.

6. **Docker-group membership treated as guest root.** Correct, and correctly used to reject "separate Unix users" as a provider isolation boundary. Many designs get this wrong in the flattering direction.

7. **Refusing to inject rescue credentials into a compromised session.** The right call, and notable because the temptation to save work is exactly when the mistake gets made.

8. **The "reproducibly identified" vs "indefinitely reproducible" distinction (`docs/tool-provenance.md`).** Rare intellectual honesty. Most projects claim reproducibility on the strength of pinned versions; this one names precisely what pinning does and does not buy.

9. **Softnet's anti-spoofing as a dependency of address discovery.** Not currently written down (see MAJOR-7), but the design is sound: MAC→IP lease discovery is only trustworthy because Softnet prevents a co-resident hostile VM from claiming another VM's MAC or source IP.

10. **Modeling `tart run` as a supervised long-lived process rather than a command.** Correct in principle; see MAJOR-6 for the part that still needs specifying.

11. **`--domain` required with no implicit default (Plan Task 1 step 3).** Refusing to invent a default domain eliminates an entire class of cross-context accidents at the cost of a little typing. Right trade.

---

## Explicit non-findings

Things I specifically checked and found to be fine, recorded so they are not re-litigated:

- **Ubuntu 24.04 ARM64 Desktop imaging.** I expected the preferred Desktop-ISO path to be impossible on ARM64. It is not: `ubuntu-24.04.4-desktop-arm64.iso` is published on `cdimage.ubuntu.com`, and Desktop autoinstall has had full parity with Server autoinstall since 24.04.1 via subiquity. The plan's caution and its live-server fallback remain appropriate, but the preferred path is real and should be attempted first.
- **Third-party AI application availability on Linux ARM64.** ChatGPT Desktop for Linux (arm64 `.deb`, tested on Ubuntu 24.04) and Google Antigravity (Linux arm64, apt repository with arm64 index) both exist. See OPTIONAL-3 for the provenance nuance.
- **Tart automatic pruning.** It evicts OCI and IPSW *cache* entries only, never local VMs in `~/.tart/vms`. Locally built goldens and sessions are not at risk. See MINOR-3 for the residual points.
- **Tart's forbidden capabilities are opt-in.** `--dir`, `--disk`, `--rosetta`, `--nested`, `--vnc`, `--net-bridged`, and `--net-softnet-expose` all require explicit flags. "Supply no flags" is therefore a safe default, and Plan Task 2's exact-argv assertion is the right shape of test — with the correction required by BLOCKER-1.
- **VM↔VM isolation under Softnet.** Enabled by default. See MAJOR-3 — the finding is that it is undocumented and silently defeasible, not that it is absent.
- **Repository hygiene.** No pre-Boxwarden product, CLI, module, or repository identifiers remain in any tracked file. Git author and committer metadata is consistent across all four commits. `LICENSE` (Apache-2.0) and `NOTICE` are present and correct. The worktree is clean and `main` matches `origin/main`. No Git history work is needed and none was performed.
- **The previous review's eight concerns.** All eight are substantively addressed; see the assessment below.

## Assessment of the previous review's eight concerns

| # | Prior concern | Status | Residual |
|---|---|---|---|
| 1 | Profile admission / trusted write-back | **Addressed well** — declarative adapters, digest-bound approval, staged restore, replay rejection | MAJOR-1 (validation side), MAJOR-2 (approval rendering) |
| 2 | Kindex persistence bypass | **Fully addressed** — ADR 007, dedicated policy doc, path rejection, explicit UNSUPPORTED status, falsifiable M1B gate | MINOR-7 (denylist vs allowlist framing) |
| 3 | Clone machine identity | **Addressed** — ADR 009, finalization, first-boot regeneration, two-clone test as a promotion gate | MAJOR-4(b) (checkpoints contradict the invariant) |
| 4 | Tart lifecycle / reconciliation | **Addressed** — intent-before-mutation, locks, observed-state reconciliation, orphan reporting, idempotent retry | MAJOR-6 (supervision/GUI session), MAJOR-7 (resolver) |
| 5 | Unattended Ubuntu bootstrap | **Addressed** — Task 0 gate with a documented fallback that must pass the same GUI tests | None; the preferred path is more viable than the plan assumes |
| 6 | Networking / Docker firewall | **Partially addressed** — inbound deny, loopback binding, `DOCKER-USER` rules are all correct | **BLOCKER-1** (guest→host egress), MAJOR-3 (session↔session) |
| 7 | Golden reproducibility / provenance | **Addressed well** — qualification gate, honest closure claims, updater disablement, unavailable-rather-than-weaken rule | OPTIONAL-1, OPTIONAL-3 |
| 8 | Accidental project loss | **Addressed** — durability registration, blocking checks, conspicuous override, separate compromised path | MAJOR-5 (trust basis of the guard) |

The revisions succeeded. Concern 6 is the one where the document now states a stronger property than the backend actually delivers, which is how BLOCKER-1 arises — the intent was right and the mechanism was not verified.

## M1A scope assessment

The scope is well controlled, and the "Explicitly deferred" list is one of the better artifacts in the repository. I found no scope creep of the usual kind — no speculative hypervisor abstraction, no premature Linux backend, no database-backed memory, no enterprise policy framework.

Two items I would move out:

- **Checkpoints** (MAJOR-4). Under-specified, in tension with the disposability thesis, and they prove nothing about the architecture. Deferring them removes a whole class of identity and containment questions from M1A.
- **Most of the vendor tool qualification** (OPTIONAL-2). Fifteen artifacts and four GUI acceptance gates are the highest-variance work in the plan and the least architectural. One AI application proves the guest supports the workload; the rest is throughput.

Two items I checked and would **keep** despite their apparent cost:

- **The per-domain SSH user CA.** Not optional — see item 1 in "Design choices reviewed and accepted."
- **Both profile adapters.** A single adapter would leave the adapter *interface* shaped around one case. `git-preferences-v1` and `sensitive-markdown-v1` differ in confidentiality, content type, and validation shape, which is exactly what is needed to prove the abstraction is real. Cheap, and it earns its place.

## Task 0 assessment

Task 0 is the right shape: an empirical spike gated before abstraction, with a stop-for-review gate and a requirement to run twice from recorded prerequisites. Step 8's "scripted evidence check that fails if any required evidence field or two-clone comparison is absent" is a genuinely good idea — evidence that can be silently skipped is not evidence.

Four additions are required before it can discharge its purpose:

1. **Guest→host and guest→private-network negative network tests** (BLOCKER-1). Currently in Task 9. They belong in the Task 0 gate; they are among the cheapest tests in the plan and they validate the property the architecture rests on.
2. **`--net-softnet-block=@host` behavior** (BLOCKER-1): does Tart forward the `@host` alias to Softnet, does DHCP still function with the gateway blocked, and does guest DNS require static configuration as a result?
3. **Session↔session reachability** (MAJOR-3): boot two clones, attempt to reach one from the other.
4. **Address discovery under Softnet** (MAJOR-7): prove `--resolver=dhcp` works and observe behavior across a lease renewal.

One addition I would recommend but not require: **evaluate `tart run --serial-path` as an authenticated host-key discovery channel** (MINOR-1) before accepting TOFU. If it works, ADR 012's one open question closes with a stronger answer than the fallback the plan pre-authorizes.

Task 0's own gate should also record the **supervision and GUI-session facts** from MAJOR-6, since those are observations about the host rather than design decisions and Task 0 is where host observations are made.

## Suggested implementation ordering

1. **Documentation corrections first, before any code.** BLOCKER-1 (security model + launch policy + ADR 003), MAJOR-1 (validation side), MAJOR-3 (session isolation property), MAJOR-5 (durability guard trust basis), MAJOR-4 (resolve checkpoints — I recommend deferring them). These are prose changes and they determine what the code is supposed to do.
2. **Task 0, with the four additions above.** Stop at the gate as planned. Record host-supervision facts for MAJOR-6.
3. **Resolve MAJOR-6 and MAJOR-7** using Task 0's observations, before the lifecycle state schema is written.
4. **Task 1** (control plane, domains, `execx`) — unchanged. Set `TART_NO_AUTO_PRUNE` here (MINOR-3).
5. **Task 2** (backend seam, Tart adapter) — with the corrected launch policy. The architecture test forbidding Tart imports in common packages is well placed; keep it.
6. **Task 3a** (lock schema, qualification gates, evidence) — split per MINOR-2.
7. **Task 4** (guest definition and provisioning) — including static DNS from BLOCKER-1.
8. **Task 3b** (candidate build and promotion).
9. **Task 5** (lifecycle) — with the PID-plus-start-time state field and the checkpoint decision already made.
10. **Task 6** (SSH) — with `--resolver=dhcp` pinned and the host-key channel decided.
11. **Task 7** (profiles) — with MAJOR-1 and MAJOR-2 enforced.
12. **Task 8** (durability, credentials, modes) — with host-side `git ls-remote` corroboration.
13. **Task 9, Task 10** — unchanged, minus the tests promoted into Task 0.

## Questions requiring empirical validation

Task 0 must answer these. None should be assumed.

1. Does `tart run --net-softnet-block=@host` forward the `@host` alias to Softnet, or must the bridge gateway be blocked as an explicit `/32` resolved at launch time?
2. With the gateway blocked, does the guest still obtain a DHCP lease? (Expected yes — DHCP is broadcast — but this is load-bearing.)
3. With the gateway blocked, does guest DNS break, and does the guest definition therefore require a static external resolver?
4. From a running guest, which deliberately selected trusted-host TCP services are reachable before and after the block? Use known listening surfaces rather than relying only on ICMP, but do not publish the reference host's transient service inventory.
5. Can one running session reach another running session's SSH port, in the same and in different security domains?
6. Does `tart ip --resolver=dhcp` work reliably under Softnet, and what happens across a 600-second lease renewal during a long session?
7. Does `tart run --serial-path` provide a usable one-way channel for reading a freshly generated SSH host key fingerprint at first boot? Which guest device does it appear as on Ubuntu 24.04 ARM64 under Virtualization.framework? Can a getty be reliably kept off it?
8. If not (7), is explicit TOFU with immediate local pinning accepted as a recorded assumption?
9. Does `tart run` succeed when invoked over SSH into the Mac mini with no console user logged in? What is the observed failure mode? Does a launchd user agent in the Aqua session behave correctly?
10. Does Ubuntu 24.04.4 Desktop ARM64 autoinstall complete genuinely unattended under Tart, including the exact kernel command line and seed-discovery mechanism? If not, does the live-server plus pinned desktop package set pass the same GUI acceptance tests?
11. Does golden finalization plus first boot regenerate *every* identity component the invariant claims — machine-id, SSH host keys, DHCP/DUID, random seed — verified by direct comparison across two clones?
12. What are the measured clone, first-boot, SSH-ready, desktop-ready, stop, and destroy latencies, and the actual disk growth per session? Is routine destruction cheap enough to actually be routine?

## Final recommendation

**IMPLEMENT WITH CONDITIONS.**

Resolve BLOCKER-1 as a documentation and launch-policy correction before implementation begins. Make the five documentation corrections in ordering step 1 — they cost hours and they determine what the code must do. Then run Task 0 with the four required additions and stop at its gate as planned.

The architecture does not need to change. The findings in this review are, almost without exception, cases where the design is right and a document either states a property it does not yet deliver, or omits which side of the trust boundary a check runs on. That is a good position to be in before writing code, and a bad one to be in after.

The one judgment call I would press: **defer checkpoints**. They are the only part of M1A that is simultaneously under-specified, in tension with the milestone's own thesis, and a live persistence vector. Everything else in the plan earns its place.
