# Generic Golden Qualification Analysis

Status: Research / non-authoritative
Author: Kiro / Claude Sonnet 4.6
Date: 2026-09-02
Reviewed repository base: 7de2306b83136231beb9e03a7a769c52c9691d7f

This document is independent research. It does not modify or supersede accepted
ADRs, canonical architecture, or implementation plans. Maintainer acceptance is
required before any recommendation becomes policy.

---

## Executive conclusion

Generic-golden qualification does **not** require management SSH and does not
require any domain trust to be established before admission. All facts that
characterize the generic artifact — build integrity, guest-boot behavior, serial
console access, workstation usability, no domain state, and clone-readiness —
can be proven entirely through the ADR 017 trusted serial path on a disposable
qualification clone, plus offline static analysis of the artifact and autoinstall
definition. The current V2 attended gate is blocked because the old Task 0
qualification procedure called `issue-cert` to exercise SSH and that procedure
depended on a pre-baked domain CA anchor, which no longer exists in the generic
golden. That is purely a qualification-procedure problem, not a V2 mechanics
problem: V2 create/clone/MAC mechanics are already correct. The non-circular
solution is to define a separate, SSH-free golden-qualification gate that uses
serial-only bounded automation on one disposable clone, destroys that clone
after evidence collection, then admits the untouched generic artifact. V2 gate
proves only V2 create mechanics using an already-qualified artifact. V4 remains
solely responsible for domain trust bootstrap, host-key pinning, SSH, and
time-zone convergence.

No architecture change is required. Only the qualification procedure and its
documented evidence must be updated.

---

## Responsibility matrix

The table below classifies every relevant fact by the lifecycle stage that must
establish and prove it. Abbreviations: BT = build-time (artifact construction),
GQ = golden qualification gate, V2 = V2 create/clone gate, V3 = V3 host/domain
init gate, V4 = V4 session-start gate.

### Artifact and build facts

| Fact | Stage |
|---|---|
| Correct Ubuntu version (24.04) and architecture (arm64) | BT |
| Full `ubuntu-desktop` source selection | BT |
| Canonical ISO identity and verified signature | BT |
| Expected software/tool versions installed and held | BT |
| APT holds prevent automatic updates | BT |
| Vendor tool distributions are first-party official | BT |
| Every installed artifact has verified digest/signature | BT |
| Reproducibility: two clean builds produce same final seed | BT + GQ |
| `__BOXWARDEN_TIMEZONE__` placeholder, not UTC | BT |
| Generic sshd policy present, pointing at future `active` paths | BT |
| `active` directory **absent** from the artifact | BT + GQ |
| No domain CA anchor or public key anywhere in the golden | BT + GQ |
| No domain principal or fixed authorized key | BT + GQ |
| No provider/browser login, session state, or secret | BT + GQ |
| Static `boxwarden-guest-bootstrap` helper installed at fixed path | BT |
| Helper is static ELF arm64, no interpreter, no dynamic deps | BT |
| Helper digest matches `artifacts.lock.json` | BT + GQ |
| Root-owned `/etc/ssh/boxwarden` parent only; no `active` child | BT + GQ |
| Passwordless sudo policy installed and validated | BT |

### Clone-readiness facts

| Fact | Stage |
|---|---|
| Machine ID cleared/reset (`/etc/machine-id` empty or ready to regenerate) | BT (finalize-clone) |
| `boxwarden-task0-firstboot-identity` service present and enabled | BT (finalize-clone) |
| `/var/lib/boxwarden-task0/clone-ready` marker present | BT (finalize-clone) |
| All SSH host keys removed from golden | BT (finalize-clone) |
| DHCP lease state cleared | BT (finalize-clone) |
| `systemd random-seed` removed | BT (finalize-clone) |
| Shell history and caches removed | BT (finalize-clone) |
| Two fresh clones produce distinct machine IDs, SSH host keys, hostnames, MACs, DHCP identities | GQ |
| First-boot identity regeneration service fires and self-disables | GQ |
| MAC randomized by `tart set --random-mac` (V2) | V2 |

### Workstation facts

| Fact | Stage |
|---|---|
| Guest boots to graphical desktop without human intervention | GQ |
| Wayland session active and functional | GQ |
| XWayland launches on demand | GQ |
| Serial console `hvc0` autologin as `boxwarden` works | GQ |
| Passwordless `sudo -n` succeeds as `boxwarden` | GQ |
| No first-login wizard, screen lock, blanking, or suspend | GQ |
| GRUB timeout limited to 1 second normal, 1 second recordfail | BT |
| Required tool versions present and runnable | GQ (serial probe) |
| Host-time-zone rendering placeholder confirmed | BT |

### Security-boundary facts

| Fact | Stage |
|---|---|
| No host filesystem share in golden | BT + GQ |
| No host Docker/runtime socket | BT |
| Clipboard sharing disabled in launch (Softnet policy) | V4 |
| Audio sharing disabled in launch (Softnet policy) | V4 |
| No forbidden Tart integrations | V4 (launch policy) |
| Default private/link-local egress denied | V4 (Softnet policy) |
| Session-to-session isolation holds | GQ (network probe) |
| UFW inbound-deny default, port 22 allowed | BT |
| `PermitUserEnvironment no`, `PermitUserRC no`, forwarding disabled in sshd | BT + GQ (serial sshd -T check) |

### Domain trust facts

| Fact | Stage |
|---|---|
| Domain CA created (host-only, never in golden) | V3 |
| Domain CA public anchor installed in clone | V4 (serial bootstrap) |
| Session principal installed in clone | V4 (serial bootstrap) |
| Durable domain/session/backend/CA/principal binding installed | V4 (serial bootstrap) |
| Fresh guest SSH host key obtained and pinned on host | V4 (serial bootstrap) |
| Short-lived certificate issued and strict SSH proven | V4 |
| Host time zone applied and read back | V4 |
| READY asserted | V4 |

---

## Why the current procedure became circular

The old Task 0 qualification procedure was designed when the golden contained a
pre-baked domain CA public anchor (see ADR 012 history: "each domain's public CA
anchor would be baked into that domain's immutable golden"). That design meant:

1. The golden was **domain-specific**: it contained the public CA key for a
   particular domain's management CA.
2. Task 0's `bootstrap-tart.sh issue-cert` issued a management certificate
   against that pre-baked CA.
3. The attended qualification used SSH with that certificate as the final proof.

ADR 012 was amended by ADR 017. The amendment moved CA-anchor installation to
the post-clone serial-bootstrap step (V4) so that the same generic golden can
serve multiple domains. This was the correct design change.

However, the Task 0 spike script still contains `issue-cert`, and the attended
qualification evidence still references the SSH path. No new golden has been
built under the corrected generic guest definition. The V2 attended gate plan
states: "The gate must use a stopped, non-production artifact built or rebuilt
from the corrected generic guest definition." The old Task 0 artifact "is not
grandfathered merely because it previously passed qualification."

The circularity therefore is:

- The old qualification procedure requires SSH.
- SSH requires a pinned host key.
- Pinning requires serial bootstrap (V4).
- Serial bootstrap installs a domain CA anchor.
- A domain CA anchor means the artifact is no longer generic.

So: **the old procedure requires V4 behavior to qualify a V2 artifact**. This
is not inherent to qualification; it is an artifact of the old domain-bound
design. The requirement for SSH in qualification is entirely inherited, not
logically necessary.

The specific assumption that no longer holds: **the golden carries a CA anchor
that enables immediate SSH**. The current generic design intentionally removes
that anchor. There is nothing to SSH into without first running the V4 serial
bootstrap, and that cannot happen during golden qualification without
contaminating the artifact with domain state.

---

## Does generic-golden qualification require SSH?

**No.**

The properties that qualification must prove are:

1. The artifact contains the correct, reviewed generic configuration.
2. The guest boots and reaches a functional workstation state.
3. The serial console is available and autologin works.
4. Clone-readiness: identity regeneration fires and produces distinct clones.
5. No domain state (CA, principal, `active` directory) exists in the golden.
6. The static bootstrap helper is present, has the correct digest, and executes.
7. Workstation usability properties hold (no lock, no wizard, usable desktop).

None of these require SSH. Every one of them can be observed through:

- **Offline static analysis**: inspect the autoinstall `user-data`, check the
  helper digest, verify no `active` directory creation, check sshd config.
- **Serial console automation**: boot a qualification clone, run bounded commands
  via the ADR 017 serial path without installing domain trust.
- **Two-clone identity comparison**: clone twice, boot each through serial, read
  back machine ID, SSH host key fingerprints, and hostname to prove uniqueness.

SSH is the normal production management path. It is not the qualification path.

---

## Candidate qualification designs

### Pattern A — Qualification entirely through serial (recommended)

Boot a disposable qualification clone of the generic candidate. Use the trusted
host serial path (ADR 017, same infrastructure as V4) to run bounded acceptance
checks: probe the bootstrap helper, verify the sshd configuration via
`sshd -t` and `sshd -T`, read back installed file digests and modes, verify no
`active` directory exists, verify the guest is functional (Wayland, desktop,
no-lock policy), and emit clone identity information. Destroy the clone after
evidence collection. The generic source artifact is never modified.

Trust analysis:
- What authenticates the host to the guest? The host owns the serial PTY; the
  guest auto-logs in as the workstation account. The host does not need to
  authenticate itself to the guest for qualification — the serial path is a
  direct hardware channel.
- What authenticates the guest to the host? The host verifies the serial output
  matches bounded expected framing. There is no cryptographic authentication,
  but the channel is host-owned and the clone was just created from the golden
  under test.
- Is the channel bound to the exact disposable clone? Yes — the serial path is
  constructed by the control harness when Tart starts this exact clone.
- Is TOFU introduced? No — no SSH host key is trusted.
- Is `StrictHostKeyChecking=no` required? No SSH is used.
- Is a host filesystem share introduced? No.
- Does the procedure modify the golden itself? No — the qualification clone is
  a separate copy-on-write Tart object; the source is never modified.
- Could qualification accidentally turn generic state domain-specific? No — no
  CA anchor, principal, or `active` directory is installed anywhere.
- Do qualification credentials survive in the promoted artifact? N/A — no
  qualification credentials are created.
- Does the process depend on V3/V4 production management trust? No.
- Is the evidence reproducible? Yes — the qualification clone can be rebuilt and
  the procedure re-run after any golden rebuild.

**Verdict: non-circular, satisfies all security constraints, fully sufficient.**

### Pattern B — Temporary qualification-only CA on a disposable clone

Clone the generic candidate. Install an ephemeral qualification-only CA over the
trusted serial channel into that clone. Establish SSH and run acceptance tests.
Destroy the clone.

Trust analysis:
- Does the procedure modify the golden? No (clone only).
- Does domain trust leak into the artifact? No (clone is destroyed).
- Is TOFU introduced? No — the host-key pin can be established via serial before
  the SSH CA is installed, same as V4.
- Unnecessary complexity? **Yes.** Pattern B adds a full CA generation, certificate
  issuance, pinning, and SSH connection purely to run the same tests Pattern A
  runs over serial. SSH is the V4 runtime management path; adding it to
  qualification introduces a dependency on V4 infrastructure for no additional
  evidence value. Pattern B is not wrong; it is just needlessly complex given
  that Pattern A covers the same acceptance surface.

**Verdict: viable but not recommended. Adds V4 complexity for no additional
qualification coverage.**

### Pattern C — Split qualification with deferred SSH

Prove all build and artifact properties offline/statically. Prove clone-readiness
and boot properties through serial. Defer all SSH proof to the V4 attended gate
on a real session.

This is essentially Pattern A with the explicit acknowledgment that V4 also
provides additional qualification evidence. It is not "deferral" in a problematic
sense — qualification proves the generic artifact is correct and clone-ready,
while V4 proves the production management path works. These are separate
concerns.

**Verdict: this is the pattern this document recommends, identical to Pattern A
in practice.**

### Pattern D — Offline-only qualification

Prove every fact through static analysis: inspect the Tart VM disk, check file
digests, read the autoinstall definition, check the helper binary with
`file`/ELF inspection.

This cannot prove boot behavior, serial console function, identity regeneration
firing on first boot, or workstation usability. Static analysis is necessary but
not sufficient.

**Verdict: necessary component but insufficient alone; combine with Pattern A.**

---

## Recommended qualification sequence

```
guest definition (autoinstall/user-data, autoinstall/meta-data,
                  artifacts.lock.json, tests/bootstrap.sh)
    |
    | static validation (bootstrap.sh passes offline)
    v
candidate backend artifact
(tart clone-ready golden: no active/, no CA anchor,
 helper digest matches lock, machine-id cleared,
 SSH host keys absent, clone-ready marker present)
    |
    | offline artifact inspection
    | - verify no /etc/ssh/boxwarden/active in VM disk or firstboot script
    | - verify helper digest vs. artifacts.lock.json
    | - verify autoinstall tests pass (bootstrap.sh)
    | - verify no SSH CA material anywhere
    v
disposable qualification clone A (tart clone <candidate> <qual-a>)
    |
    | start with serial (run_with_serial, no domain trust installed)
    v
serial acceptance automation (no SSH, no CA)
    | - wait for getty autologin (BOXWARDEN_SSH_HOSTKEYS_V1 framing as proof)
    | - probe: /usr/local/libexec/boxwarden-guest-bootstrap --version or probe
    | - verify helper present, executable, correct static type
    | - verify no /etc/ssh/boxwarden/active exists
    | - run sshd -t (no active CA → expected to pass config syntax, not auth)
    | - run sshd -T -C user=boxwarden,host=localhost,addr=127.0.0.1
    |   verify: TrustedUserCAKeys absent or points at non-existent path,
    |   AuthorizedPrincipalsFile set, PasswordAuthentication no, forwarding no
    | - verify /etc/ssh/boxwarden/ is root:root 0755, no active child
    | - verify Wayland/desktop active (ps aux | grep gnome-session or similar)
    | - verify sudo -n /bin/true succeeds as boxwarden
    | - record machine ID, SSH host key fingerprints (ssh-keygen -lf on all families)
    |   as evidence of this clone's identity
    v
stop and destroy qualification clone A
    |
disposable qualification clone B (tart clone <candidate> <qual-b>)
    |
    | start with serial
    v
identity comparison
    | - record machine ID, SSH host key fingerprints for clone B
    | - compare against clone A: all must differ (machine ID, all host key families,
    |   hostname derived from machine ID)
    v
stop and destroy qualification clone B
    |
    v
evidence record
    | - two-clone identity proof (distinct machine IDs, SSH fingerprints, hostnames)
    | - serial path functional on this exact candidate
    | - no domain state in golden
    | - helper present and has correct digest
    | - sshd config clean
    | - workstation properties verified
    v
immutable generic golden admission
    (tart stop <candidate>; boxwarden --domain D golden register <candidate>)
```

Serial is used throughout. SSH does not appear. Domain trust does not appear.
Clone destruction after evidence collection ensures no qualification state
survives. The generic source artifact is never booted or modified; only
disposable clones are used.

---

## Proposed V2 attended gate

The V2 gate must prove V2 mechanics only: that `session create` correctly:
1. Resolves the admitted generic golden.
2. Clones it to a new stopped backend object.
3. Randomizes the MAC.
4. Reconciles to one stopped clone.

The gate does not need to:
- Boot the clone.
- Install any trust material.
- Prove SSH.
- Prove the guest is functional (that is the golden qualification gate's job).

**Minimal V2 attended gate:**

Precondition: a qualified generic golden is registered in the domain.

1. Run `boxwarden --domain D session create <name>`.
2. Observe the command succeeds and reports a stopped session.
3. Verify via `tart list`: exactly one new Tart object exists with the expected
   derived name, is stopped, and has a MAC distinct from the source golden.
4. Verify via `boxwarden --domain D session status <name>`: reports `stopped`,
   `not_ready`.
5. Run the same create command again for the same name (idempotence): must
   succeed and report the same stopped session without creating a second clone.
6. Optionally: run create with a second name to prove the golden remains
   untouched (Tart's CoW clone should leave the source stopped and unchanged).
7. Verify source golden is still stopped and unchanged (same object ID, still
   reportable).

What the gate does NOT observe:
- Guest boot behavior (this is golden qualification's evidence).
- Serial console (this is golden qualification's evidence).
- Domain trust (this is V4's responsibility).
- SSH reachability (this is V4's responsibility).

The gate's result validates V2 create/clone/MAC/status mechanics. It does not
inflate `golden register` into a provenance claim.

---

## Proposed V4 attended trust-bootstrap gate

The V4 gate proves the production management path for a real session. It
presupposes a qualified generic golden, a working V3 host/domain foundation, and
a V2-created stopped clone.

V4 proves:
1. Serial broker lifecycle (attach/detach, lease exclusion, Screen retention).
2. `boxwarden-guest-bootstrap serial-bootstrap` over the serial channel:
   - CA anchor installed correctly in `active/`.
   - Principal file installed correctly.
   - Durable binding manifest installed.
   - Effective sshd policy verified by the helper.
   - Fresh SSH host public key returned.
3. Host-key pin created and bound to this exact session.
4. Short-lived no-extension certificate issued.
5. Strict SSH readiness probe succeeds.
6. Time zone applied and read back correctly.
7. READY reported.
8. ADR 017 requalification: two-PTY broker identity, Screen child, flood/poison
   paths, lease exclusion.

The V4 gate is necessarily the first place where any SSH connection exists. It
does not leak backward into the golden qualification gate because qualification
is already complete before this gate begins.

---

## Security analysis

### No TOFU

Pattern A serial qualification introduces no TOFU. The serial channel is
host-owned hardware. The qualification clone was just created from the candidate
under test. No SSH host key is accepted or pinned during qualification. SSH never
runs during qualification.

In V4, the fresh host key is obtained via serial (the only trusted pre-SSH
channel) before any SSH connection is attempted. TOFU and
`StrictHostKeyChecking=no` remain prohibited throughout.

### No domain trust in golden

The recommended qualification sequence deliberately never runs V4 serial
bootstrap on the qualification clone. No CA anchor, no principal, no `active`
directory, no durable binding is ever installed into the qualification clone or
the source artifact. The source artifact is never booted at all during
qualification — only CoW clones are booted.

The qualification procedure verifies that no `active` directory exists, that no
CA material is present, and that `TrustedUserCAKeys` points at a non-existent
path. This is positive evidence of absence.

### No persistent qualification credentials

The recommended qualification sequence creates no credentials. No CA is
generated, no certificate is issued, no SSH key pair is created. The serial
channel requires no credential: the host owns it by process ownership and OS
permissions. After the qualification clones are destroyed, no qualification
artifact survives.

### No host filesystem share

The qualification procedure uses only the serial path. No filesystem share,
extra disk, VirtioFS, or Tart `--dir` argument appears. This is consistent with
the production policy. The spike script `run-ro-share` exists for ADR 021
research but is explicitly excluded from V0.1 and from this qualification
sequence.

### Guest-root threat model

The qualification clone boots with unrestricted guest root (passwordless sudo).
The qualification procedure runs through the host-owned serial PTY. A malicious
golden guest cannot escape to the host through the serial path because the
host-side broker discards operator-master input during automation mode (ADR 017).
A malicious guest can only affect the qualification clone, which is destroyed
after evidence collection. It cannot modify the source artifact because Tart's
CoW clone mechanism keeps the source stopped and unchanged.

The qualification procedure does not rely on guest-reported output being
trustworthy beyond the bounded framing check. The purpose is to verify the
golden's generic properties, not to establish an authenticated trust
relationship.

### Exact clone identity

Two-clone identity comparison is part of the recommended qualification sequence
(same as Task 0's requirement). Both clones are created, booted serially, have
their machine IDs and SSH host key fingerprints recorded, compared for
distinctness, then destroyed. This proves the identity regeneration mechanism
works and that the golden is clone-ready.

---

## Required architecture/document changes

No architecture change is needed. The accepted ADRs (004, 009, 010, 012, 017,
019) are fully consistent with the recommended qualification approach. ADR 012
amended after ADR 017 already describes the correct generic-golden model.

The following documentation and procedure updates are required (maintainer
decision needed on each):

1. **New qualification procedure document** (e.g., `docs/evidence/m1a-golden-qualification.md`
   or a formal qualification gate specification): describe the serial-only
   qualification sequence above, the evidence matrix, and the acceptance
   criteria. This replaces the defunct SSH-based Task 0 procedure for this
   specific gate.

2. **Guest definition must be finalized** before a new golden is built: the
   current `autoinstall/user-data` still contains `__BOXWARDEN_RUN_ID__`,
   `__BOXWARDEN_PASSWORD_HASH__`, and the spike `boxwarden-task0-spike` marker.
   These are Task 0 spike artifacts. A production guest definition should
   not carry a task-scoped spike marker into the generic golden. A maintainer
   decision is needed on whether to strip or rename these before the first
   production golden build.

3. **`bootstrap-tart.sh` `issue-cert` subcommand**: this is a spike artifact
   that issued SSH certificates for the domain-bound Task 0 design. It should
   be documented as retired or removed in a future cleanup commit. It is not
   needed for the generic golden qualification procedure.

4. **V2 attended gate plan needs updating**: the current V2 gate plan in the
   v0.1 plan document says "The V2 gate must use a stopped, non-production
   artifact built or rebuilt from the corrected generic guest definition." The
   gate's expected scope needs the clarification this document provides —
   specifically, that the gate proves V2 create mechanics only, not golden
   quality.

5. **New golden build and qualification must be run** on the qualified host
   before PR #2 / V2 gate can complete. The old Task 0 artifact cannot be
   grandfathered.

No new ADR is required. The existing ADRs already describe the correct model.

---

## Open questions

1. **Should the qualification sequence use `boxwarden-guest-bootstrap
   serial-bootstrap` (with a test domain) to also prove the bootstrap helper
   executes correctly end-to-end, or is the static ELF inspection plus a simpler
   serial probe sufficient?** Using the real bootstrap command would install CA
   material into the qualification clone (acceptable since it's destroyed), but
   it also makes the qualification gate depend on the V4 helper being implemented
   before any golden can be qualified. The simpler serial probe avoids this
   dependency and keeps the qualification gate independent of V4 implementation
   status.

2. **What is the correct handling of the `__BOXWARDEN_RUN_ID__` and
   `__BOXWARDEN_PASSWORD_HASH__` placeholders in the production guest
   definition?** The password is needed during installation (the account is
   configured with a password) but is irrelevant for production use since SSH
   is key-only and the guest account has passwordless sudo. A maintainer decision
   is needed on whether to use a fixed hardcoded disposable hash in the
   production definition or retain the placeholder mechanism with a build-time
   input.

3. **When is the `boxwarden-task0-spike` marker file
   (`/etc/boxwarden-task0-spike`) appropriate to keep in the golden?** It exists
   to allow `finalize-clone.sh` to verify it is running inside a Task 0 guest.
   For a production golden, the equivalent guard should be a production marker.
   This is a minor cleanup item but a genuine open decision.

4. **Can the two-PTY serial relay be exercised during golden qualification
   without the full V4 supervisor** (i.e., using the spike `bootstrap-tart.sh
   run_with_serial` directly)? The V3 attended gate evidence notes that serial
   broker behavior "remains unrun until a separately approved lossless-observer
   design exists." The qualification gate does need serial to work, but it could
   use the spike relay or await the production supervisor. This is a sequencing
   question for the maintainer.

---

## Sources and evidence

### Authoritative (observed facts from repository)

- `AGENTS.md`: invariants, serial/security-boundary rules, golden property
  requirements — all cited by content, not paraphrase.
- `docs/architecture.md`: generic golden model, serial/SSH management path, V2
  create contract.
- `docs/security-model.md`: guest-root threat model, serial ownership, TOFU
  prohibition.
- `docs/lifecycle-and-recovery.md`: V2 create stops at stopped clone; V4 owns
  boot/trust/SSH/timezone.
- `docs/state-model.md`: GOLDEN layer definition — never contains PROFILE,
  SECRETS/IDENTITY, domain CA anchor, or fixed domain principal.
- `docs/tool-provenance.md`: qualified toolchain digests.
- `docs/credentials.md`: management CA scope, V4 serial bootstrap sequence.
- `docs/decisions/004-golden-profile-session-separation.md`: GOLDEN is generic;
  registration proves only recorded identity and explicit operator admission.
- `docs/decisions/009-clone-identity-and-reconciled-lifecycle.md`: clone
  identity uniqueness contract.
- `docs/decisions/012-domain-ssh-user-ca.md` (amended): explicitly states
  golden artifacts are generic, no CA anchor, serial path is the only bootstrap
  channel, TOFU prohibited.
- `docs/decisions/017-host-local-serial-recovery-shell.md`: serial path design,
  owned broker, automation state discards operator input, no hot repair.
- `docs/decisions/019-host-time-zone-convergence.md`: V2 leaves stopped clone,
  no convergence; V4 owns convergence.
- `docs/superpowers/plans/2026-09-01-boxwarden-v0.1.md`: V2 attended gate
  language stating old artifact is not grandfathered; V4 gate describes the
  full trust bootstrap.
- `docs/evidence/m1a-task0-final-summary.md`: Task 0 evidence matrix, PASS WITH
  CONDITIONS, unqualified IPv6 environments.
- `docs/evidence/v3-host-domain-attended-gates.md`: V3 gate result, explicit
  statement that serial/Screen behavior "remains unrun."
- `guest/ubuntu-24.04-arm64/autoinstall/user-data`: presence of
  `__BOXWARDEN_TIMEZONE__`, absence of `active` creation, absence of CA
  placeholders, presence of generic sshd policy.
- `guest/ubuntu-24.04-arm64/tests/bootstrap.sh`: test assertions for generic
  golden properties, especially `require_absent '/target/etc/ssh/boxwarden/active'`
  and `require_absent '__BOXWARDEN_SSH_CA_PUBLIC_KEY__'`.
- `scripts/spike/bootstrap-tart.sh`: `issue_cert` function depends on a CA
  private key and issues domain-specific certificates; `run_with_serial` shows
  the serial relay infrastructure.
- `scripts/spike/finalize-clone.sh`: clone finalization removes SSH host keys,
  clears machine-id, installs firstboot identity service; confirms the
  clone-readiness contract.
- `internal/golden/record.go`: `BackendRef` comment explicitly states it "does
  not assert backend immutability, provenance, clone-readiness, or qualification
  evidence."
- `internal/golden/register.go`: `Register` verifies only "one existing stopped
  object" — no qualification assertion.
- `internal/session/create.go`: Create/clone/MAC mechanics stop at stopped clone
  with no boot, no trust, no SSH.

### Inferences (derived from the above, not independently observed)

- The old Task 0 SSH-based qualification path is blocked because the amended
  ADR 012 generic golden no longer contains a domain CA anchor. This is inferred
  from: (1) ADR 012 history describing the pre-amendment design, (2) the amended
  design requiring serial-only bootstrap, and (3) the fact that no new golden
  has been built under the corrected design.
- The V2 attended gate can be completed independently of the golden qualification
  gate, provided a qualified generic golden exists. This follows from the V2
  mechanics being entirely about create/clone/MAC/observe, with no reference to
  golden quality.
- Serial-only qualification is sufficient because every qualification target is
  observable through the host-owned serial channel plus offline static analysis.
  This follows from the list of qualification targets above and the capabilities
  of the ADR 017 serial path.
