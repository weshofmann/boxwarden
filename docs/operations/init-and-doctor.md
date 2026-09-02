# Host initialization and diagnosis

`boxwarden init` is an explicit, attended, host-global prerequisite operation.
It is the only command allowed to establish the ADR 024 Softnet privilege
binding. It neither selects nor initializes a security domain, and rejects an
explicit `--domain` rather than silently ignoring it. It must never be invoked
implicitly by session lifecycle commands.

The qualified Darwin installer accepts a bounded, versioned request over stdin
and uses the exact `/usr/bin/sudo -- <absolute-boxwarden> internal host-install`
root phase. It must install only the exact Softnet 0.19.0 digest tree under
`/Library/Boxwarden/toolchains/softnet/0.19.0/`, publish its root-owned manifest
last, and report when a new group membership needs a login-session refresh.
It does not store administrator credentials, repair unsafe Homebrew state, or
select a mutable `current` link.

`boxwarden doctor` is the host-global, read-only diagnostic. It rejects an
explicit `--domain`, emits a stable status:
`healthy`, `missing/uninitialized`, `drifted/unsafe`, or
`unsupported/unqualified`. A non-healthy report exits nonzero and includes
observed/expected facts plus an attended remedy. Doctor never calls init,
requests interactive authorization, mutates directory-service state, executes
Tart or Softnet, or invokes a session operation. Its mutable-Homebrew safety
check uses only non-interactive `/usr/bin/sudo -n -l -- <exact-path>` policy
inspection; inability to prove that an unsafe passwordless rule is absent is a
fail-closed diagnostic, never an authorization attempt or repair.

Linux and every unqualified platform return `unsupported/unqualified`; this is
intentional so CI can compile and exercise policy without accidentally treating
its host as qualified.

`boxwarden --domain <domain> domain init` is separate from both host commands.
It creates or validates only the selected domain's management SSH CA, performs
no host initialization or Softnet installation, and does not require the V2
host configuration block or host-path admission. It strictly validates JSON and
domain configuration, and validates the exact shape of an optional host object
when one is present. It is the only command that initializes that CA. A later
`session start` must require the already-initialized CA; it must never create,
repair, or select one lazily. Its authoritative result says either
`management-ca: initialized` or `management-ca: already initialized`.
