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
  before and during disk-expanding operations.
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

## Current next action

Finish the independent plan review, resolve concrete findings, and implement
the alpha context and first public recipe/prepare path in small verified commits.
