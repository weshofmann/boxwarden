# Slice B controlled exact-start product check

Date: 2026-09-07 MDT

Result: **PASS for the bounded Slice B product check.** This is current-tree
operational evidence that the public create/start path can retain and recover
one exact Tart runtime. It is not formal ADR 017 or Softnet-runtime
qualification and does not claim READY.

This committed record is deliberately sanitized. Private temporary paths,
local account data, VM/session/generation identifiers, PIDs, the local helper
path, and raw host process output are omitted or replaced by semantic labels.
No provider credential, API key, environment value, private key, or guest-
controlled output is recorded.

## Scope and evidence boundary

The check exercised only:

```text
session create
  -> persist stopped session
session start
  -> persist starting + exact generation G
  -> detached supervisor owns exact G
  -> supervisor retains exact Tart child
  -> one serial PTY is continuously drained
  -> return STARTING / non-ready
```

It then exercised a running retry, exact typed stop/cleanup, and stopped retry
using the same durable generation. No guest bootstrap, host-key pin, client
credential, SSH management, time-zone convergence, READY publication, or
adversarial workload was used. No pre-existing provider or user credential was
supplied; domain initialization created only an isolated test CA under the
private test state root.

## Exact non-private inputs

| Input | Exact value |
|---|---|
| Implementation commit | `73375623ded5fd44788279b7c4b3c38b5f5d28c2` |
| Built `boxwarden` SHA-256 | `4ccf43b6b830d7f1a017086cef47727018c172f3f050b3848c5cac9d45499016` |
| Host platform | Apple Silicon arm64, macOS 26.6.2 build 25G83 |
| Tart | 2.32.1, executable SHA-256 `05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d` |
| Softnet | 0.19.0, installed executable SHA-256 `ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e` |

The private test used an isolated Tart home and domain state root. The
registered test golden was an APFS copy-on-write copy of a stopped known-good
disposable VM. Only `config.json`, `disk.img`, and `nvram.bin` were copied; a
stale source `control.sock` was deliberately excluded. The copied
`config.json` and `nvram.bin` digests matched the source, the disk reported
exactly 30,000,000,000 bytes, and the isolated Tart namespace reported the
golden stopped before the test.

This source artifact does not satisfy the separately pending V2 generic-golden
qualification gate. Its use here establishes only the earliest useful Slice B
launch/ownership behavior.

## Sanitized command transcript

The final-head product binary ran the following command shapes in order. Angle-
bracketed values identify private exact values that were used consistently but
are intentionally absent from the repository:

```sh
boxwarden --config <private-test-config> doctor
boxwarden --config <private-test-config> --domain <test-domain> domain init
boxwarden --config <private-test-config> --domain <test-domain> golden register <test-golden>
boxwarden --config <private-test-config> --domain <test-domain> session create <test-session>
boxwarden --config <private-test-config> --domain <test-domain> session status <test-session>
boxwarden --config <private-test-config> --domain <test-domain> session start <test-session>
```

The first start returned:

```text
state: starting
readiness: starting
```

The identical public start command was then used first while the runtime was
live and again after an exact typed stop. Both retries returned the same
`starting` / `starting` result without claiming READY.

The observation/control command shapes were:

```sh
hostcheck snapshot <private-test-config> <test-domain> <test-session>
TART_HOME=<isolated-tart-home> <qualified-tart> list --format json
lsof +D <exact-generation-directory>
ps -ww -o pid=,ppid=,pgid=,command= -p <supervisor-pid>,<tart-pid>,<softnet-pid>
hostcheck stop <private-test-config> <test-domain> <test-session>
```

The local ignored `hostcheck` helper loaded the public configuration and
durable record, derived the exact binding/runtime directory, and used only
`supervisor.Client.Snapshot` or `supervisor.Client.Stop`. `snapshot` also used a
nonblocking flock attempt against the exact generation lock. `stop` waited for
that exact runtime directory to disappear and reloaded the durable record to
verify that `starting + G` was unchanged. It did not use PIDs or process names
as control authority.

All Tart/process/filesystem commands were read-only observations. No `tart
stop` or signal was issued; process-observation results and persisted PIDs were
never control authority.

## Observed lifecycle

The exact private generation value is represented below as `G`. The same value
was byte-for-byte unchanged throughout the check.

| Boundary | Durable state | Durable generation | Backend/runtime observation |
|---|---|---|---|
| After create | `stopped / not_ready` | absent | exact backend stopped; no generation |
| First start returned | `starting / starting` | `G` | typed snapshot: backend running, serial healthy, lock held |
| Running retry returned | unchanged | same `G` | identical supervisor/Tart/Softnet processes; no relaunch |
| First typed stop completed | unchanged | same `G` | backend stopped; exact generation absent |
| Stopped retry returned | unchanged | same `G` | new supervisor/Tart/Softnet processes; serial healthy, lock held |
| Final typed stop completed | unchanged | same `G` | backend stopped; exact generation absent; second-launch processes gone |

After each typed stop, public status reported `intended: starting`, `observed:
stopped`, `consistency: indeterminate`, and the diagnostic that transitional
starting intent requires lifecycle reconciliation. It did not allocate a new
generation or manufacture a stopped/ready durable state.

## Runtime ownership and containment observations

The first launch process lineage was:

```text
detached boxwarden supervisor S1
  -> exact Tart child T1
       -> digest-specific installed Softnet child N1
```

The initiating public CLI had already exited successfully. `lsof` showed `S1`
retaining the exact `generation.lock`; the typed snapshot independently
reported `lock_held: true`. The stopped retry established the same ownership
shape under distinct new processes `S2 -> T2 -> N2` while retaining the same
durable generation and backend identity.

The exact observed Tart argument mapping on both launches was:

```text
<qualified-tart> run --net-softnet --no-audio --no-clipboard --serial-path <G>/serial/tart-serial <exact-backend-object>
```

No share, bridged/host-network, port-forward, audio, clipboard, VNC, Rosetta,
extra-disk, checkpoint, or Softnet allow-all flag was present. The actual
Softnet child was the admitted digest-specific installed executable.

The live runtime tree was owner-private: generation and `serial/` directories
were mode `0700`; request, lock, and socket were mode `0600`; and
`serial/tart-serial` named the single PTY slave. Each typed snapshot reported
`SerialHealthy: true`, establishing that the retained master drain was alive.
After each exact stop, the complete generation directory was absent.

The closed Tart environment is enforced by the production launcher and exact-
environment deterministic tests in `internal/backend/tart/launch_test.go` and
`internal/sessionruntime/owner_test.go`. A direct host environment-observation
attempt was excluded because it expanded beyond its intended PID scope and
emitted unrelated process output. No raw output or environment value is
recorded here. Therefore this bounded check directly proves the argv, admitted
Tart/Softnet identities, lineage, and absence of prohibited argv integrations,
but it does not upgrade closed-environment runtime behavior to attended
qualification.

## Result and remaining limits

This check directly observed the requested Slice B outcome: Boxwarden started
the exact disposable VM, returned without claiming READY, retained the runtime
under a detached exact-generation supervisor, reused a live generation without
relaunch, and relaunched the same generation after exact stop/cleanup.

It does not establish:

- power-loss or hostile same-UID recovery;
- ambiguous/foreign runtime recovery beyond deterministic tests;
- formal ADR 017 exclusive-framing, hostile-output, lifetime, or cleanup
  qualification;
- Softnet privilege-transition/drop, filesystem, signal, closed-environment,
  or network qualification (S10-S13);
- the corrected-generic-golden V2 attended gate;
- guest bootstrap, trust publication, host-key pinning, client credentials,
  strict SSH, time-zone convergence, or READY.

The private test state and stopped disposable Tart objects were retained locally
for human inspection. Their paths and identifiers are intentionally omitted.
