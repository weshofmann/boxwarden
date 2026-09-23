# Boxwarden v0.2 alpha decisions

## Initial scope and environment, 2026-09-23

- Use a private alpha context alongside the admitted host toolchain. The
  existing personal context and protected historical VM stay untouched. This
  gives the alpha exact state ownership without migrating old records.
- Prove the offline-inspector launch and transfer capabilities before fixing
  workspace APIs. Tart 2.32.1 advertises read-only file-backed disk attachment,
  but the current backend launcher always enables Softnet and provides no
  inspector transport. CLI help is a capability clue, not an isolation test.
- The initial graphical agent client candidate is the official ChatGPT Linux
  preview for Ubuntu 24.04 ARM64. [Official OpenAI documentation](https://learn.chatgpt.com/docs/linux/linux-app)
  lists ARM64 packages and says Computer Use is unavailable on Linux preview.
  An official IDE integration is the fallback if guest installation/launch
  fails. An unauthenticated launch test does not prove agent task completion.
- Preserve v0.1's fixed host launch and guest-root threat model until a typed
  v0.2 contract changes them deliberately. The independent review found that
  management READY composition and nonexclusive SSH require coordinated guest
  helper, protocol, host runtime, and test changes.

## Inspector networking, 2026-09-23

Independent source review of the pinned toolchain found that Tart 2.32.1
supports a read-only file-backed disk attachment but always supplies a network
device, using shared networking when none is selected. Softnet 0.19.0 applies
an explicit `0.0.0.0/0` block before its ordinary gateway, DNS, and public
destination allowances, but ARP, broadcast DHCP, and incoming IPv4 still pass.
Guest-controlled link state cannot establish containment against guest root.
This is a source-derived capability finding, not an empirical VM result.

Keep the stopped-workspace export acceptance gate closed until a separately
reviewed offline inspector capability or an explicit acceptance decision covers
that residual surface. Receiver parsing, exclusive workspace locking, and
synthetic failure tests can proceed independently. Tart's read-only attachment
does not by itself establish exclusive ownership; host locks must prevent a
writer from starting until the inspector has stopped and been reaped.

Sources: [Tart 2.32.1 launch](https://github.com/cirruslabs/tart/blob/2.32.1/Sources/tart/Commands/Run.swift#L362-L376),
[Tart read-only disk attachment](https://github.com/cirruslabs/tart/blob/2.32.1/Sources/tart/Commands/Run.swift#L882-L894),
[Softnet 0.19.0 guest filtering](https://github.com/openai/softnet/blob/0.19.0/lib/proxy/vm.rs#L25-L104),
and [Softnet incoming filtering](https://github.com/openai/softnet/blob/0.19.0/lib/proxy/host.rs).

## Prepared-base cache identity, 2026-09-23

The source layer and recipe-specific prepared layer are distinct. A source
candidate built from the signed Canonical ISO and generic guest definition is
not yet an application-prepared base. The recipe preparation key includes the
source identity, exact digest of tracked guest build inputs, machine shape,
apt package intent, and ordered `prepare` steps. Workspace attachments,
`once`/`reconfigure`/`startup` steps, and launch intent are excluded because
they operate on a clone rather than the shared base.

The key selects an existing stopped cache object; it is not evidence that the
build succeeded, that mutable package repositories yielded the same bytes, or
that the image is safe. Cache admission also requires recorded build inputs,
package/application BOM, exact backend object observation, and fresh clone
qualification. A changed package source or version must invalidate or
requalify that cache entry explicitly.

## Tart scratch and exact-generation ownership, 2026-09-23

The first public alpha session start failed before serial bootstrap. Tart
2.32.1 created a `control.sock` in the supervisor's exact generation root;
read-only descriptor inspection proved Tart owned the socket. The launch
policy had set both Tart's working directory and `TMPDIR` to that root, so
this run cannot distinguish which setting selected the path. Boxwarden's
generation validator correctly rejected the extra entry. The foreground
start timed out, and its exact controlled stop could not pass the same
validator. The test VM was stopped through the exact admitted Tart command;
the supervisor, Tart, and Softnet processes then exited.

Give Tart a private, explicitly admitted scratch child within the exact
generation, and point both working directory and `TMPDIR` there. Admit only
that exact subtree under the supervisor root and clean it only after the
retained Tart handle has been reaped. Do not whitelist a foreign `control.sock`
at the generation root or grant it Boxwarden control-socket semantics. Preserve
this failed qualification run and retry on a fresh clone after source and
test correction. Serial autologin timing remains a separate untested risk.

The production launcher now retains its direct child with `os.StartProcess`.
Darwin `Wait4(WNOHANG)` and process-group SIGINT share one mutex; the child PID
cannot be recycled between a successful exact reap and a retry signal. A wait
error or unexpected PID means ownership is unproven: refuse another signal,
preserve scratch and generation, and return a typed cleanup failure. This
retains failure evidence and stops a lost control reply from being mistaken
for a stopped session. A real short Darwin child, ambiguous wait cases, and
the cleanup propagation have focused tests; real Tart qualification is next.

## Generated host-key comment, 2026-09-23

Fresh public clone r1c passed Tart scratch and hvc0 prompt admission, but
serial bootstrap timed out without a framed helper response. An investigation
boot of that failed clone sent a canonical valid request and captured the
helper's exact error: `host public key is not fresh ed25519 public material`.
A separate read-only guest probe counted three fields in the generated
`/etc/ssh/ssh_host_ed25519_key.pub`; the third was an OpenSSH human comment
(`root@boxwarden-...`). The helper required exactly two fields. This was a
guest parser mismatch, not a failed host-key regeneration or a missing hvc0
login. All investigation boots ended stopped; r1c remains failed evidence.

Normalize the guest file to exactly the validated ed25519 type and base64
blob before returning it in the nonce-bound frame. Keep the host wire and CA
validators strict, and reject multiline or malformed trailing material. The
static Linux ARM64 helper digest changes, so the original generic candidate
cannot be promoted: rebuild from corrected autoinstall input and freshly
qualify a new clone. The serial error path still lacks structured guest
diagnostics; failure evidence required a bounded private console capture.

## SSH argv and certificate pairing, 2026-09-23

Fresh r2 clones exposed two distinct OpenSSH invocation defects. `-o
IdentityFile=/.../Application Support/...` passed the path as one argv element,
but OpenSSH still parsed its internal option string on spaces. Quoting the
path inside that option fixed parsing; credential path admission now rejects
characters that could trigger OpenSSH expansion. A separate explicit
`CertificateFile` option caused OpenSSH to treat the public certificate file as
a signing identity. Require the exact `IdentityFile` + `-cert.pub` companion
and let OpenSSH discover it. A bounded manual SSH probe isolated the second
failure; a new fresh public clone, qualr2c, subsequently reached READY through
the regular management path. Keep both failed clones as immutable evidence.

## macOS ACL admission, 2026-09-23

An integration test for prepared-base metadata found that `/bin/ls -lde` can
print `@` in the first mode marker while printing an extended ACL on the next
line. The shared `hostx.OSACLInspector` had checked only for `+` at the end of
the marker and falsely admitted such a path. Treat any nonempty ACL entry line
as extended ACL, in addition to the legacy marker. This shared fix protects
private prepared-base and workspace paths as well as existing host-toolchain
admission. A Darwin `chmod +a` regression test and focused package tests pass.
