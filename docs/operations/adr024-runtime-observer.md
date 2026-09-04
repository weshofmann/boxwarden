# ADR 024 qualification-only runtime observer

This repository contains a private, unprivileged process observer at
`internal/qualification/adr024/cmd/observe`. It is one attended qualification
instrument for ADR 024. It is not installed by `boxwarden init`, exposed by the
normal `boxwarden` CLI, imported by production lifecycle code, or retained as a
host service.

The observer accepts exactly one caller-supplied runtime identity: a positive
decimal Tart PID passed as `--tart-pid`. It never searches for Tart or Softnet
by process name and accepts no override for an executable, digest, UID, GID,
operator identity, interval, or evidence threshold. Expected identities come
from the fixed root-owned Boxwarden manifest, parsed by `hostx.ParseManifest`,
and the exact manifested Tart and Softnet files are reopened without following
symlinks and checked against their manifested digests before and after
sampling.

The observer uses read-only Darwin `libproc` calls. It does not invoke sudo,
launch or signal a process, attach or trace, suspend execution, inject an
environment, write a file, or change system configuration. Darwin with cgo is
required; other builds contain a deterministic refusal stub. Qualification
code is isolated below `internal/qualification`, and an architecture test
prevents production packages from importing it.

A fresh, unprivileged `boxwarden doctor` must report healthy immediately before
the attended launch. Doctor owns complete host-state and ACL diagnosis. The
observer deliberately does not rerun doctor because doing so would spawn its
fixed external inspection commands. Instead, it independently reopens the
fixed manifest, Tart, and Softnet files without following symlinks; validates
their required type and security metadata, single-link state, and digests; and
repeats that admission after
sampling. Any pre/post identity difference fails the observation.

Build it from the exact reviewed V3 commit:

```sh
CGO_ENABLED=1 go build -trimpath -buildvcs=true \
  -o /private/tmp/boxwarden-adr024-observe \
  ./internal/qualification/adr024/cmd/observe
```

The attended invocation has only this form:

```sh
/private/tmp/boxwarden-adr024-observe --tart-pid <exact-decimal-tart-pid>
```

It emits exactly one bounded JSON object followed by one newline on stdout and
creates no evidence file. The caller decides whether to retain the private
output and separately produces a redacted public record.

## Reusable attended method

Begin with a fresh B0 snapshot of all host-toolchain and runtime objects. Create
a fresh disposable VM, use the corrected controller built from the reviewed
commit, and record the exact launch inputs. During launch through observer
admission, observe the Tart shell-owned job, direct Tart-to-Softnet ancestry,
unique process identities and start times, executable paths and digests, and
credentials. Observe the qualified network behavior and host-local serial
relay, then perform a normal stop and identity-bound cleanup. Finish with a
fresh B1 snapshot and compare it with B0. The post-init snapshot is the running
and cleanup baseline for that run.

A failed run is immutable evidence, not a checkpoint to resume. Resume only to
investigate, understand, or safely contain the failed runtime. After correction,
preserve the failed evidence and qualify again from a fresh B0 and fresh
disposable runtime; any exception requires explicit qualification design and
architecture review.

The process evidence must establish that the exact Tart shell-owned job has a
direct Softnet child with a unique start identity and exact executable, while
recording credentials independently. Capture the independently observed PGID,
but do not require PGID equality: Tart group
termination does not prove Softnet disappearance, and no PID-only signal is
permitted. The observer remains bounded and non-lossless; no atomicity claim is
made for filesystem or privilege transitions.

During the launch-to-observer epoch, Softnet may commit/replace the bootpd
preference object, so an inode/mtime change alone is permitted. Device and type,
root ownership and mode, link count, size, flags, path, exact SHA-256 and XML
must remain exact, as must
the sole approved `DHCPLeaseTimeSecs=600` semantics. Outside that phase all
fields are exact. This phase-specific rule permits a replacement observation,
not an atomicity claim.

## Evidence and limits

At five-millisecond configured intervals, the observer makes at most 6,000
sampling attempts and requires 100 consecutive accepted post-drop samples. It
enumerates direct children of only the supplied Tart PID; exactly one child is
permitted. It brackets every process sample with Darwin's kernel process-unique
identity, binds the child to Tart's parent-unique identity, and binds Tart and
Softnet to their exact executable paths. Tart start time is required
immediately; Softnet start time is required once its post-drop identity is
visible and must then remain stable. It records distinct sampled
real/effective/saved UID and GID tuples with bounded occurrence counts.

A root-effective sample is recorded when observed, but is neither required for
success nor sufficient to qualify the complete transition. The report always
states that sampling is not lossless, that activity between samples can be
missed, and that the configured interval excludes query and scheduler latency.
Absence of a sampled root credential says nothing about whether the transient
privileged phase occurred.

The observer fails closed on an unrelated or reused Tart PID, changed process
identity or start time, absent or multiple direct children, wrong ancestry,
wrong executable, unexpected credentials, a child disappearing before enough
evidence, query failure, timeout, fixed-host-state drift, malformed input, or
bounded-output exhaustion.

This report is only one component of sufficient non-perturbing qualification
evidence. The attended gate must also bind the exact artifact/root tree and
reviewed Softnet 0.19.0 implementation; record the controlled executable,
argv, and closed environment; exercise behavior requiring privileged setup;
observe the qualified network policy externally; and confirm the steady-state
drop to the trusted operator. The threat model assumes that operator is
trusted and does not claim resistance to a malicious process already running
as that host operator.
