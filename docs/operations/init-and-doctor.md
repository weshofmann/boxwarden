# Host initialization and diagnosis

`boxwarden --domain <domain> init` is an explicit, attended prerequisite
operation. It is the only command allowed to establish the ADR 024 Softnet
privilege binding and initialize the selected domain's management state. It
must never be invoked implicitly by session lifecycle commands.

The qualified Darwin installer accepts a bounded, versioned request over stdin
and uses the exact `/usr/bin/sudo -- <absolute-boxwarden> internal host-install`
root phase. It must install only the exact Softnet 0.19.0 digest tree under
`/Library/Boxwarden/toolchains/softnet/0.19.0/`, publish its root-owned manifest
last, and report when a new group membership needs a login-session refresh.
It does not store administrator credentials, repair unsafe Homebrew state, or
select a mutable `current` link.

`boxwarden --domain <domain> doctor` is read-only. It emits a stable status:
`healthy`, `missing/uninitialized`, `drifted/unsafe`, or
`unsupported/unqualified`. A non-healthy report exits nonzero and includes
observed/expected facts plus an attended remedy. Doctor never calls init,
sudo, directory-service mutation, Tart, Softnet, or a session operation.

Linux and every unqualified platform return `unsupported/unqualified`; this is
intentional so CI can compile and exercise policy without accidentally treating
its host as qualified.
