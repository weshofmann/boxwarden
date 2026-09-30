# Clipboard source diagnostic preparation

Source baseline: `99b3a8c04c56ddc4292a9dd515c3e2abcb77d5c6`.
This is deterministic source preparation, not live qualification or approval
to operate another guest. The historical candidate copy acknowledgement and
failed first read remain **UNQUALIFIED**. No VM, real clipboard, GTK display,
network policy, credential, golden or canonical guest source was changed here.

## Source findings

The successful copy requires the detached child's native GTK claim to return
true, its immediate postclaim session check to pass, and exact bound receipts
plus host READY rechecks to succeed. It does not establish continued selection
ownership or eventual delivery. The original child disarms its precommit timer
and drops its cancellation lease immediately after claim; it writes its private
acknowledgement before entering `retain`. The overlay adds no native metadata
work between those steps.

Each first read separately discovers its Wayland login and Xwayland binding.
It checks its session before and after exactly one `UTF8_STRING` retrieval.
The callback can report absent data or reject representation; validation can
reject UTF-8/NUL/size. Session admission can fail before that request. A live
writer process with a matching command line does not establish its PID birth
identity, native ownership, or successful callback delivery.

The error collapses through Python's status-only receipt; discarded adapter
stderr; bootstrap executor/decode/deadline/binding handling; fixed helper error;
SSH exit/truncation/framing handling; runtime READY checks; and the supervisor's
generic error/read mapping. Consequently `clipboard text unavailable` does not
identify any of these branches. No source evidence establishes N1 networking
as the clipboard root cause.

The [combined diagnostic design](two-gap-diagnostic-design.md) ranks those
hypotheses and preserves the corresponding network gap independently.

## Delivered source and bounds

- [`clipboard_diagnostic.py`](../../../tools/n1-qualification/clipboard_diagnostic.py)
  is a trial-only overlay at the fixed installed adapter path. It imports only
  the exact preserved production sibling, verified against SHA-256
  `e38530c45a2aab705be80aad3e9096f2fce9330cb6c0b5b6623a3d76403b4dbb`.
- [`clipboard_diagnostic_test.py`](../../../tools/n1-qualification/clipboard_diagnostic_test.py)
  injects the retained module and fake native boundaries. No GTK library or
  real display is opened by these tests. CI runs this test with the controlled
  clipboard helper checks.
- The overlay emits only exact allowlisted event codes, bounded integer fields,
  internally generated guest monotonic `at_ms`, and a digest of bounded desktop
  binding metadata. It retains no text, text digest, exception strings, argv
  dump, environment dump, authority contents, or command stderr.
- One root-created `0600` descriptor is opened with `O_EXCL`/`O_NOFOLLOW` before
  privilege drop for each of the fixed `write.trace` and `read.trace` files.
  The root-owned diagnostic directory must already be `0700`. There is no
  user-supplied diagnostic pathname. Creation refuses reuse. Collection checks
  regular-file ownership/mode/link count, inode identity, byte bound, exact
  fields, types and duplicate keys; it never constructs a clipboard object.
- Each file has at most **64 slots of 256 bytes = 16 KiB**. Parent and detached
  owner have disjoint 32-slot quotas, with the last slot reporting saturation.
  Logging stops after 60 seconds without ending normal ownership. The two files
  together are bounded by **32 KiB**. These bounds are separate from the future
  trusted host stage receipt's 32 records/16 KiB limit.
- Slots are per writer, not a globally chronological log. Compare `at_ms` from
  the same guest clock; equal millisecond timestamps cannot establish order.
  Missing/saturated records are incomplete observations.
- Claim/get/clear/retain and native-read classification wrap original methods.
  Frame status is recorded only after the original frame write and flush return.
  No retry, extra text read, public protocol change or production telemetry was
  introduced. Diagnostic sink failure is not evidence of transfer success.
- Native owner queries run in a disposable exact-path Python worker with closed
  validated desktop environment, no stdin, no text operation, a 250 ms parent
  timeout and a 200 ms default-action kernel alarm. Timeout cleanup kills and
  reaps the worker. Writer observation occurs in `retain`, after private ack.
  The first reader additionally makes one TARGETS metadata request, records only
  UTF8 presence, and bounds its callback wait to 250 ms. Its callback reference
  remains alive if the metadata request is still pending. Original deadline
  exceptions propagate; no diagnostic grants a fresh operation deadline.

The overlay SHA-256 observed after implementation was
`6d175eeccea85a531acbfc90ca01c66f5cf1d5b9c9e9eb310dfbc7ee7b3e3ecd`.
This is an identity receipt, not admission or qualification. Recompute it after
any review correction before a future exact-byte request.

## Deterministic verification receipt

Commands were run from the repository with `PYTHONDONTWRITEBYTECODE=1` where
applicable. Disposable test files were deleted by their fixtures; the following
non-private command/result record is the durable evidence.

| Command | Observed result |
|---|---|
| `python3 tools/n1-qualification/clipboard_diagnostic_test.py` before overlay implementation | 8 failures: `trial diagnostic overlay is missing` |
| Same command during strict schema/saturation work | 10 tests, 2 intended failures: missing saturation record and accepted wrong fields |
| Same command during deadline/commit-boundary work | 12 tests, one intended metadata-before-commit failure and one fixture error (`Desktop` lacked `fail`); fixture corrected before final verification |
| Same command before frame status hook | 16 tests, intended failure: successful original frame had no diagnostic status; an earlier fixture index error was replaced by that explicit assertion |
| Same command after implementation and corrections | **16 tests, OK**, latest run 0.062 s |
| `python3 guest/ubuntu-24.04-arm64/tests/clipboard_test.py` | **59 tests, OK**, 0.568 s |
| `python3 tools/n1-qualification/listener_test.py` | **2 tests, OK**, 1.182 s |
| `python3 tools/n1-qualification/followup_tcp_verdict_test.py` | **11 tests, OK**, 0.244 s |
| `git diff --check` | Exit 0, no output |
| `shasum -a 256 guest/ubuntu-24.04-arm64/clipboard.py` | Canonical bytes remain the exact production digest above |

Tests exercise text/frame preservation, no repeated first read, absent versus
invalid text/representation, get/clear hooks, fixed claim-before-metadata order,
deadline propagation, private-error exclusion, native worker argument/env
contract, TARGETS-only metadata and callback retention, fixed fork slot bounds,
saturation, internal timestamps, strict trace schema and unsafe-file refusal.
They do **not** establish live GTK semantics, bridge behavior, timing-neutral
instrumentation, owner XID equivalence, or successful end-to-end delivery.

## Future staging and unresolved discriminators

The future reviewed package needs two installed adapter files per new clone:
the exact overlay at `/usr/local/libexec/boxwarden-guest-clipboard.py`, and the
unchanged exact production bytes at the fixed `.production.py` sibling. Keep
the locked bootstrap unchanged. Verify both as regular one-link root-owned
`0755` files and bind their digests before any write. Preserve the generic base;
stage only in the newly authorized disposable clones. Restart and reverify READY
after staging. Then prepare the fresh root-owned diagnostic directory through
a reviewed exact-generation pinned SSH step, without precreating/adopting either
trace file. These descriptions are staging requirements, not runnable commands
or authorization.

Use stock first, then one candidate copy and first read only. Bind both trace
collections externally to the exact domain/session/backend/generation/host pin
and installed bytes. The overlay's desktop digest is not a security-generation
receipt. Collection itself performs no text read.

The future exact collector must add one bounded `/proc/<exact-owner-pid>/stat`
observation with expected UID and the captured starttime, without inspecting
other processes, environment or command lines. Its source/tests and exact bytes
must be reviewed before owner approval. The trace alone cannot identify a
signal death or prove that a process is still alive at first-read time.

Compare binding digests and native owner snapshots only when their timestamps
place the writer observation before the first read. A zero/different owner and
clear event support a selection-loss branch; a bridge proxy or XID reuse prevents
equating those point observations with causal proof. A get event is request
service evidence, not received-byte evidence. Metadata failure/overflow,
unobserved owner state, or snapshots in the wrong order leave attribution open.

If the adapter reports `frame_ok` but the CLI fails, the future trusted host
hooks in the combined design are still required to distinguish bootstrap decode,
SSH framing/transport, runtime readiness and control framing. Absent traces
alone do not distinguish helper admission from transport refusal. No changed
bootstrap or host diagnostic executable has been prepared or admitted here.
