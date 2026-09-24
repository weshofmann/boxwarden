# Boxwarden v0.2 alpha execution plan

## Objective

Deliver the graphical Ubuntu ARM64 sandbox, independent persistent workspace,
system rebuild, and controlled file return loop described in the supplied
autonomous alpha launch mission. The product vision supplies rationale; the
launch mission sets the alpha scope and acceptance criteria.

## Starting point

- Base: `e16f23962b43337f02de1fbe2779df00a28f3f1a`.
- Integration branch: `weshofmann/feature/v02-alpha`.
- Existing code has host admission, Tart observation/clone/start, locks, domain
  records, a retained supervisor, and serial draining. It has no public v0.2
  recipe, automatic base preparation, workspace volume, or export workflow.
- Read-only host doctor and the baseline Go suite pass outside the restricted
  Codex shell. The protected historical Phase 3 r1 VM remains stopped.

## Sequence

1. **Alpha contract and context.** Record v0.2 command and state semantics,
   establish an alpha-only context using the admitted Tart toolchain and exact
   `TART_HOME`, and inventory any owned runtime before mutation. Preserve
   v0.1 records and historical evidence.
2. **Early export capability probe.** Before fixing storage interfaces, use
   synthetic disks to prove what the pinned Tart version actually permits for
   managed-disk attachment, read-only inspection, offline networking, and a
   bounded return transport. The current launcher has only a Softnet-enabled
   profile and its serial channel is one bootstrap exchange, not a file stream.
   Design any inspector launch and transport as narrow typed operations. Keep
   export disabled if the probe cannot establish a safe route.
3. **First graphical vertical slice.** Add one versioned Ubuntu Desktop ARM64
   recipe and deterministic prepared-image cache key. Reuse only permitted
   tracked installer/provisioning inputs. Version the guest bootstrap contract
   coherently so Boxwarden's pinned channel coexists with intentional additional
   guest authentication. Create or reuse a generic prepared image, clone a
   private system disk, and boot under the fixed Softnet policy. Complete serial
   bootstrap, exact guest binding and host-key pin, management credential,
   strict SSH probe, time-zone convergence, current readiness, stop/restart,
   and credential maintenance. Install and launch the browser, Git, Node.js,
   editor, and selected graphical agent client through the recipe. Prove the
   desktop and application launch. Never treat a cached image as a trust
   certificate or include private credentials in it.
4. **Recipe setup semantics.** Separate reusable preparation, once-per-sandbox
   setup, explicit reconfiguration, and startup actions. Execute custom steps
   only in the guest. A restart preserves system state and reports incomplete
   optional setup without reapplying everything.
5. **Independent workspaces.** Create Linux filesystem disk files with stable
   identities, stop-only attachment, one writable owner, exact locking, and
   explicit import. Persist workspace records separately from system clones.
   Implement stop/start, rebuild, delete-with-retain, and reattach-to-replacement
   with synthetic files before using real user data. Specify durable ownership
   reservations, lock ordering, rebuild phases, and recovery before coding the
   transition. Validate the replacement before removing the old system where
   practical; require an observed stop and exclusive volume hold first.
6. **Controlled return.** Test the transfer protocol with hostile fixtures.
   Implement an isolated Linux inspector for an exclusively stopped
   volume, then a host receiver that validates path, type, length, collisions,
   and complete transfer into a new destination. Keep export disabled if its
   boundary cannot be proved; never mount the guest filesystem on macOS.
7. **Acceptance.** Run source checks, targeted failure tests, separate review,
   real VM synthetic lifecycle tests, and a fresh tracked-source repetition.
   Record actual GUI evidence and host limitations. Leave workspaces durable,
   test VMs stopped, and publish a reviewed Draft PR without merging `main`.

## Cross-cutting gates

- Host doctor must remain healthy before real VM mutations. Only one operator
  mutates alpha VMs or volumes, with at most two running VMs and 8 GiB combined
  assigned RAM initially, never above half of physical RAM. Preserve free disk
  space of at least the greater of 20 GiB or 10% of volume capacity, checked
  before and during disk-expanding operations. Automatic base preparation
  samples both the selected state filesystem and admitted Tart filesystem
  before reserving an attempt and through fresh-clone qualification, using
  an additional 1 GiB cancellation margin. Sampling cannot forecast a whole
  installer peak, so a read-only capacity estimate still precedes each real run.
- Guest root is adversarial. No host tree, credential bridge, display server,
  Docker socket, clipboard/audio share, private network allowance, or guest
  filesystem mount in the macOS kernel is introduced.
- All destructive actions use exact ownership records and locks. Existing
  volumes are never implicitly formatted, imported, or deleted.
- Tests and reviewer findings are recorded separately from real host evidence.
  An unverified GUI or network property remains pending, never inferred from
  process state or guest output.
- Existing Softnet admission grants no new host privilege. Record launch,
  attachment, offline networking, GUI observation, and installer-preparation
  capabilities before relying on them. A new host entitlement or network
  privilege change blocks only its dependent operation.
- Official OpenAI documentation currently offers an ARM64 Ubuntu ChatGPT
  desktop preview, but its Linux preview lacks Computer Use. The alpha can
  validate installation and launch while reporting that functional limitation;
  use the official IDE integration if the desktop package fails in the guest.

## Publication cadence

The integration branch is `weshofmann/feature/v02-alpha` with one Draft PR
against `main`. Publish each meaningful, independently reviewable increment
after targeted verification, aiming for a pushed checkpoint every 30–60
minutes of active work when there is publishable progress. Push every verified
commit promptly. Before a substantial handoff or subsystem switch, update
this plan or `alpha-progress.md` and the Draft PR with the actual state.
Distinguish targeted checks from full-suite and real-host qualification.
Worker-owned incomplete files stay unstaged; a sanitized progress-only commit
is appropriate when code is not yet safe. Never use an empty commit to imply
progress, rewrite published history, or integrate into `main`.

## Rebuild contract and implementation sequence

The public rebuild keeps the sandbox name, session UUID, and workspace
attachment IDs stable. It replaces only the system backend and selected base.
The immutable SSH pin is currently keyed by session UUID and bound to the
backend object, so a fresh clone needs one explicitly journal-authorized pin
transition during serial bootstrap. Ordinary pin admission remains immutable.
This design was challenged in a separate read-only lifecycle/storage review;
its findings are incorporated below. It is a plan, not implemented behavior.

1. Add a separate strict, bounded, session-name-keyed rebuild journal that
   survives ordinary session-record writes: operation UUID, phase, stable
   session UUID, exact old and candidate backend IDs and base revisions, and
   exact old pin presence, binding, and digest. Reserve the candidate ID across
   both active records and all rebuild journals. The journal blocks competing
   start, attach, detach, export, and rebuild mutations, while status remains
   read-only and exact safety stop of the active generation remains available.
2. Require durable stopped old intent from clone creation or exact stop/reap,
   no Use on any attachment, and a fresh exact backend observation before
   recording the candidate identity. Clone only from the recorded admitted
   stopped base,
   then randomize the candidate MAC before any boot. A retry accepts only the
   exact recorded stopped clone and may repeat randomization only while the
   journal proves the candidate has never booted; ambiguous candidate state
   blocks. Neither a false Tart `stopped` listing nor an advisory lock alone
   proves release of a live Use.
3. Persist the active-backend switch and journal phase before starting the
   candidate. Reuse the normal new-generation supervisor, batch Use
   reservation, serial bootstrap, mount-bound READY, and stop paths through
   narrow internal rebuild continuation. During bootstrap, the trusted host
   checks the exact journal witness and serial-observed candidate host key,
   then atomically transitions the pin from the exact old bytes to the exact
   new binding/key. An absent old pin is permitted only if the journal recorded
   absence; a private sibling staging file makes an interrupted replacement
   retryable without exposing a partial pin to normal reads. Retry accepts
   only exact old or exact new state. No old and new VM
   may run concurrently. A failure after candidate launch retains both system
   objects, the workspace record, and the journal; it never silently rolls
   back a candidate that may have written the workspace.
4. Only fresh mount-bound READY permits durable retirement intent for the old
   system. Delete that exact old backend under the journal and reconcile an
   absent old object only in the recorded delete phase. Clear the journal last.
   A failed deletion leaves the new system and old object identified for retry.

Implementation checkpoints are record/journal validation and operation gates;
stopped candidate clone and crash retry; journal-bound pin transition and
internal new-generation cutover; exact old retirement and recovery; then the
public CLI, targeted failure matrix, independent review, and real synthetic
rebuild with a retained workspace. Each checkpoint gets focused verification
and a prompt push. Full integration and fresh real acceptance remain separate.

## Planned guest-only recipe execution contract

The management SSH path now accepts fixed probe, time-zone, package inventory,
and guest-identity operations. Recipe execution remains a separate fixed
guest-only installer operation.
Reusable packages and `prepare` steps run in the disposable installer
candidate's first boot, before generic finalization. The remastered installer
ISO carries bounded recipe preparation JSON and a digest-bound fixed guest
helper; autoinstall installs both into the candidate. The retained private
serial channel sends only a fixed helper command, waits for its bounded
completion marker, then sends the existing fixed finalizer and poweroff
commands. The helper passes validated argv elements directly to guest
processes with a closed environment and deadlines. Recipe bytes never become
a host shell command. A finalized generic candidate cannot be booted again
for preparation without consuming its clone-ready identity, and the
domain-bound management SSH/READY path is unavailable before cloning.

A failed or indeterminate prepare attempt is retained and a fresh candidate
is required. Fresh-clone qualification follows finalization and checks actual
software and generic identity properties before cache admission. `once` steps
run after clone READY over a separate exact-generation pinned SSH guest
operation, record a digest-bound guest receipt after success, and report an
interrupted step without a receipt as indeterminate rather than silently
replaying it. `reconfigure` is explicit, and `startup` runs only after a
successful start. A step's output is diagnostic evidence, not proof that the
guest or its package repository is trustworthy. These contracts and their
recovery tests must be implemented before accepting recipes with packages or
custom steps.

## Workspace mount completion gate

The first real managed volume is now formatted, host-qualified, and attached
through two public start/stop generations. A manually mounted synthetic file
survived restart, but the recorded guest path is not mounted automatically.
The next implementation increment makes that path part of READY:

1. The supervisor retains a sorted copy of the exact volume UUID, filesystem
   UUID, and guest path from the already admitted Starting/Use records while
   holding the volume leases. It never accepts a guest-selected host path or
   disk identity.
2. A typed, bounded pinned-management request asks the fixed guest helper to
   mount only those ext4 UUIDs at validated `/home/boxwarden/workspaces/<name>`
   paths. Repeating the request succeeds only when each exact mount already
   exists; a different device, filesystem, or mountpoint fails without
   substituting one. The helper uses direct argv, not a remote shell or
   arbitrary host command.
3. After mount convergence, READY requires a mount-bound probe. Every fresh
   supervisor status snapshot repeats the same probe, so a later unmount is
   non-ready. Guest mount reports are operational evidence; the host's raw
   inode, journal, Use lock, backend observation, pin, and certificate remain
   separate authority checks. On failure, retain the session's Starting or
   drift state and the volume record; do not format or clear a live Use.
4. Build and digest-lock the revised generic guest helper, prepare and qualify
   a fresh base, then test first mount, same-system restart, system rebuild,
   and reattachment to a replacement sandbox with synthetic data. The older
   engineering candidate lacks this helper and cannot qualify this path.

Source increments are guest protocol/validation, fixed guest mount/probe,
host client/owner readiness, and fresh artifact/base qualification. Test wrong
UUID, wrong path, duplicate input, already mounted correct and incorrect
devices, guest helper failure, stale Use, and status after a later unmount.
Review the new guest-to-host interface separately before real VM use.

## Current next action

The corrected generic r2 candidate and a fresh public clone reached READY,
displayed GNOME and Firefox, and passed same-system stop/restart persistence.
One real managed ext4 volume was qualified and attached to that clone; two
public READY generations retained a manually mounted synthetic file. Automatic
mount and rebuild/reattach remain open.
The source-only installer launcher, prepared-base cache identity, exact input
staging, serial ACL admission, fixed guest preparation, fresh-clone qualifier,
typed package and identity inspection, host/CA preflight, builder composition,
production qualification lifecycle, ordered preparation composition, and the
public alpha preparation command are source-verified. The current r2 candidate
predates the latest tracked guest definition. The typed guest mount operation,
host client, and mount-bound READY/status probes are source-wired and locally
tested; an independent review's read-only and path-validation findings were
corrected. Exercise a fresh candidate through the public
preparation command before treating a prepared cache entry as verified. Prove
live zero-NIC ext4 inspection, qualify automatic mount and rebuild/reattach,
and complete controlled export on fresh synthetic resources before claiming
the alpha ready.
