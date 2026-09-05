# Domain management CA initialization

`boxwarden --domain <domain> domain init` is the explicit, one-time operation
that creates a security domain's management SSH user CA. It initializes only
domain-owned state. It does not inspect, install, repair, or otherwise modify
the host-global Tart/Softnet toolchain.

Run host initialization and domain initialization as separate operations:

```sh
boxwarden init
boxwarden doctor
boxwarden --domain work domain init
```

The selected domain must exist in the configuration and have a private state
root. `--domain` may be replaced by `BOXWARDEN_DOMAIN` for domain-owned
commands, but an explicit flag is preferable for attended initialization. The
command validates every configured domain root before creating anything so it
can reject accidental management-CA fingerprint reuse. This validation is not
cross-domain credential discovery or fallback.

## Persisted identity

The command creates one Ed25519 CA below the selected domain state root:

```text
identity/                         0700
identity/ssh-user-ca/             0700
identity/ssh-user-ca/ca           0600
identity/ssh-user-ca/ca.pub       0644
identity/ssh-user-ca/metadata.json 0600
```

All components are direct regular files or directories owned by the creating
operator; symlinks, hard-linked files, unsafe modes, unexpected entries, and
unsafe ancestry are rejected. The metadata binds the domain ID, algorithm,
canonical public key, public-key digest, SSH fingerprint, creation UUID, and
exact creating operator identity. The private key remains on the trusted host
and is never written to the repository, guest, command line, or logs.

## Idempotence and failure behavior

A successful first run reports:

```text
domain: work
management-ca: initialized
```

A repeat run strictly validates the existing identity and reports:

```text
domain: work
management-ca: already initialized
```

The repeat does not rotate or rewrite the CA. Missing state is the only state
that permits creation. Partial, malformed, copied-across-domain, reused,
unexpected, or permission-unsafe state fails closed and requires attended
manual investigation; `domain init` does not silently repair it.

Session lifecycle code must load and validate this already-initialized CA.
It must never create, rotate, repair, or select a CA lazily. V3 has no
`session start` command; the later start implementation remains bound to this
contract.

See [SSH management identity and trust](ssh-management.md) for certificate
issuance and strict client policy, and [host initialization and diagnosis](init-and-doctor.md)
for the separate host-global privilege mechanism.
