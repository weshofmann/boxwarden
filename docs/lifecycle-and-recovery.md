# Lifecycle and recovery

Before control-plane implementation, Task 0 proved a genuinely unattended Ubuntu 24.04 ARM64 Tart installation, reboot handling, SSH, desktop login, XWayland, host-time-zone inheritance, clone finalization, and per-clone identity regeneration. It qualified the exact Tart + Softnet shared/NAT pair and proved public Internet, inherited host/VPN DNS, work-VPN and scoped/split-DNS behavior, default private-network denial, session-to-session denial, host-to-guest SSH, DHCP behavior, management-address discovery and lease renewal, initial SSH host-key evidence, and graphical process lifetime under interactive shell, SSH disconnect, lock/logout, and reboot/login conditions. Task 0 closed `PASS WITH CONDITIONS` under ADR 020: effectively IPv6-only upstream behavior and its IPv4-only/IPv6-only destination cases remain unqualified environments, not support claims. Task 0 also records ADR 015's accepted guest-to-vmnet-gateway reachability rather than claiming guest-to-host network isolation. The exact ISO, boot/autoinstall mechanism, detected host IANA zone, host-tool provenance, commands, outputs, observations, inferences, and deferred facts are recorded.

Golden lifecycle: qualify and lock Ubuntu/package/artifact inputs, build a portable guest definition into a backend-specific candidate, run automated validation, perform human GUI acceptance, then promote an immutable revisioned artifact. In M1A the artifact is a Tart VM. The artifact is generic: it contains no Boxwarden domain identity or domain management trust. Each domain has its own trusted-host admission record and selected pointer, and two domains may independently admit the same exact stopped backend artifact. V2 registration records the operator's explicit admission and the exact existing/stopped backend identity; it does not claim provenance, clone-readiness, or qualification evidence that the record does not carry. There is no mutable `*-golden-current` Tart VM. Registration/selection and session creation share the domain's golden lock so a session resolves one complete exact revision. Golden images never update in place.

External qualification finalizes the golden clone-ready: reusable machine identity, private authentication material, and session residue are removed, and first boot regenerates `/etc/machine-id`, SSH host keys, DHCP/DUID identity, random-seed material, and any other discovered machine-specific identity. The generic golden contains strict sshd configuration and fixed bootstrap target locations, but no domain CA anchor or fixed domain principal. Session creation runs `tart set <vm> --random-mac`. Acceptance creates two clones and proves their identities differ; V2 registration itself does not assert that this external gate passed.

The attended V2 register/clone gate must use an artifact built or rebuilt from
that corrected generic guest definition and qualified accordingly. An unchanged
older Task 0 artifact that contains a pre-baked domain CA anchor or principal
cannot satisfy the gate merely because the earlier domain-bound design was
previously qualified.

Session lifecycle is reconciled rather than optimistic. Host state records the security domain, immutable session UUID, selected generic-golden revision, backend kind/object identity, intended state `creating`, `stopped`, `starting`, `running`, `stopping`, `deleting`, or `failed`, and a start-generation correlation token before external mutation. Per-session locks serialize conflicting operations. The backend reports actual process state; the M1A Tart adapter uses `tart list --format json` and other documented Tart inspection commands. Runtime metadata records a supervisor instance, PID/process-start evidence, authenticated control socket, broker health, both PTY identities, Screen child/socket evidence, overflow/poison state, and lease mode, but none replace durable identity.

Backend-running and READY are separate. A long-lived same-user supervisor holds
the generation lock and owner-only control socket, survives the initiating CLI,
and keeps Tart, the two-PTY broker, and Screen as direct/owned children. It never
`exec`-replaces itself with Tart. The supervisor owns the generation client key
and certificate, revalidates CA metadata before renewing the no-extension cert,
and refreshes a strict read-only SSH probe on fixed cadence. Status challenges
the supervisor and accepts only a bounded authenticated health snapshot younger
than the maximum evidence age. It never creates credentials, applies a zone, or
repairs state. A stale/expired/authentication failure, poisoned broker, failed
probe, or host/guest-zone mismatch is non-ready.

Start/retry reconciliation is executable and conservative:

- `stopped` + backend stopped + no owned runtime creates and persists a fresh
  generation before launch.
- `starting` + backend running + exact live supervisor generation reconnects and
  resumes that same generation.
- `starting` + backend stopped + exact live supervisor generation in prelaunch
  or launch reconnects to that supervisor and waits for its bounded phase
  transition. It resumes the same generation or completes proven owned cleanup;
  it never clears the generation while that supervisor is live.
- `starting` + backend stopped + no live owned runtime atomically persists
  `stopped`, clears the generation, and only then begins a fresh retry.
- `running` + backend running + exact live supervisor idempotently ensures the
  same generation; it may renew/reprobe through the supervisor and reconverge a
  changed host zone.
- Any running observation with unproven ownership is drift/non-ready with no
  mutation or adoption.
- Owned failure cleanup may persist `stopped` and clear generation only after
  exact supervisor/backend stop and exact runtime cleanup are all observed.
- An ambiguous or poisoned serial transport with still-proven ownership must
  perform exact owned shutdown, observe backend stopped and runtime cleanup, and
  only then create a fresh generation. Without proof it is drift/no mutation.

Cancellation before intent fsync leaves the prior durable record. Cancellation
after `starting` fsync leaves that exact generation for retry. Cancellation
during proven owned cleanup leaves `starting` until stop and cleanup are
observed; only the final stopped-record fsync clears the generation. A final
`running` fsync occurs only after all readiness evidence is current. This makes
V4 recovery complete without waiting for V6.

V2 creates a stopped copy-on-write Tart clone from the selected generic golden and returns only after the randomized-MAC clone is observed stopped. It does not boot the guest, initialize domain trust, obtain a host key, or converge the time zone.

Before V4 start is available, the operator explicitly initializes two separate
scopes. Host-global `boxwarden init` runs once per trusted Mac and installs the
exact qualified Softnet privilege binding; host-global `boxwarden doctor`
diagnoses missing, unsupported, and drifted host prerequisites and gives an
actionable explicit-init or attended-remediation path without repairing them.
For each domain, `boxwarden --domain <domain> domain init` creates only that
domain's sole host-only SSH management user CA. Adding a domain does not repeat
the host installation, and neither scope is created lazily from session start.

V4 verifies the complete V3 prerequisite, establishes the supervisor-owned
broker/Screen topology, and launches only the exact default qualified Tart +
Softnet policy with clipboard/audio disabled and every allow flag rejected.
Future ADR 015 support requires explicit create/record/status/CLI semantics.
Serial automation uses the exact static command `/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap`, followed
separately by canonical bounded JSON. It atomically publishes
`/etc/ssh/boxwarden/active` containing only the durable domain/session/backend,
CA-fingerprint, and derived-principal binding; nonce and generation are echoed
exchange correlation and never installed. Later generations verify that binding
and the current host key. Start then resolves the address, issues a no-extension
certificate, proves strict SSH, applies and reads back the host zone, and only
then persists running/ready. Partial bootstrap verifies exact existing state or
fails closed; it never replaces mismatched trust material.

Repeating time-zone convergence whenever a transition boots or resumes a guest matters because the laptop can move after a golden or stopped session was created. The initial guest definition carries the build host's validated zone only to make first boot correct before management is available. Time-zone convergence is a workstation correctness property, not a security boundary: an agent-owned guest root can later change it. `tart run` is a long-lived graphical process with durable logs; Task 0 established its foreground ownership/lifetime and Aqua-login constraints, and M1A supervision stays within that evidence. Do not retain project truth only in a session.

M1A session classes:

- clean interactive: fresh clone, initially credential-free, optionally receives selected declarative profiles and session credentials;
- quarantine: fresh clone for hostile source with no `boxwarden` profile/normal-secret injection and no reusable provider or Git credential; public or short-lived read-only ingress only.

Checkpoint creation, checkpoint resume, checkpoint lifecycle/retention state,
age warnings, backend operations, and checkpoint tests are unsupported in M1A
and deferred. Any future checkpoint design must treat the artifact as
secret-bearing untrusted session state, never as a backup or golden input, and
must separately define identity, lineage, taint, compromise containment, and
resume semantics.

Future V7 normal destruction will use projects registered within the session's security domain with a guest path and durability policy. Before normal destruction, `boxwarden` will check registered Git worktrees for modified or untracked files, unpushed commits, a configured upstream, and a reachable remote where the policy requires it. This guard is a safety control against accidental work loss, not a security control against a hostile guest. Status and destroy output distinguish guest-reported facts from trusted-host corroboration. The host corroborates the claimed remote commit when the configured policy has a credential-free or already-authorized safe way to do so; Boxwarden does not provision new host Git credentials merely for this check. If the selected durability policy requires corroboration and the host cannot obtain it, destruction fails closed. Non-Git datasets and services are declared either externally durable with evidence or explicitly disposable. Unknown or unverifiable state blocks normal destruction. `--allow-data-loss` is the conspicuous override for intentional loss.

Future V7 `boxwarden --domain <domain> session destroy <name> --compromised` is a distinct containment path. It does not trust guest Git checks, does not inject credentials, and destroys only the exact recorded backend object after host-side domain/identity/name checks. Any recovery export requires a future separately reviewed mechanism that writes only into a domain-scoped host quarantine destination.

Compromise recovery: stop activity, revoke affected credentials, use compromised destruction, and recreate from a promoted golden. If a golden is suspect, stop using it, retire the host pointer, and rebuild from verified inputs. Do not push from a suspected-compromised session using a new credential.
