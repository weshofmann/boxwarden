# SSH management identity and trust

Boxwarden management SSH is a trusted-host control-plane capability, not a
general remote shell. A domain must be initialized explicitly before a session
can start. Initialization creates exactly one host-only Ed25519 user CA under
that domain's private state root; session start never creates, rotates, repairs,
or selects a CA from another domain.

The CA tree is `identity/ssh-user-ca/` below the domain state root. It contains
`ca` and immutable `metadata.json` as mode-`0600` regular files, and `ca.pub`
as a mode-`0644` regular file, all beneath owner-private mode-`0700` directory
ancestry. The public file is therefore not reachable outside the trusted
operator's state tree. Metadata binds the domain, `ssh-ed25519`, the
canonical public key plus digest and SSH fingerprint, a creation UUID, and the
creating operator UID/name. Load and issuance revalidate the private/public key
relationship and the metadata. Missing components, altered metadata, symlinks,
hardlinks, wrong modes, or a copied tree fail closed. Initializing a domain also
compares only the supplied configured-domain roots for duplicate public-key
fingerprints; it does not discover, select, or fall back to another domain's
identity.

The CA private key never leaves the trusted host and is never passed as command
input, logged, put in a guest, or recorded in a session. The supervisor owns a
generation-private client key. It creates certificates only for:

```text
identity:  boxwarden:<domain>:<session-uuid>
principal: boxwarden-session-<session-uuid>
validity:  -5m:+15m
```

Issuance always uses `ssh-keygen -O clear`; no caller can supply principals or
certificate options. Renewal becomes necessary with five minutes or less
remaining. Certificates are runtime credentials, not durable session identity.

Before issuing a certificate, the V4 trusted serial bootstrap reports the
fresh Ed25519 host public key. Boxwarden admits it into the immutable pin record
for the exact `(domain, session UUID, backend kind, backend object)` binding.
The record never contains an IP address. An exact repeat is idempotent; a
different key or association is drift and requires attended recovery. Each
generation materializes a private `known_hosts` file containing only the
UUID-derived `HostKeyAlias` and that exact public key.

Management connections use `/usr/bin/ssh -F /dev/null` and the generation's
exact key, certificate, alias, and known-hosts file. The client disables global
known hosts, hostname canonicalization, host-key DNS, ambient agents/config,
proxies, multiplexing, TTY allocation, password and keyboard-interactive auth,
agent/X11/TCP/stream-local/tunnel forwarding, local commands, and host-key
updates. It requires strict Ed25519 pin validation, literal resolved address
transport, and bounded connect/keepalive timeouts. It logs in only as
`boxwarden` and always runs:

```text
/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap management
```

No management API accepts a remote command or remote argv. The only typed,
bounded stdin requests are readiness probe, time-zone apply, and time-zone
read. The guest helper validates the durable binding before performing those
operations. SSH success is therefore not a general guest-control channel and
does not establish identity by TOFU or address reachability.

`sshx` deliberately owns a small argv/stdin runner interface. Production
composition supplies a lossless adapter to the shared bounded `execx` runner;
it passes only the fixed `sshx.Command` path, argv, and bounded stdin under a
closed C-locale, UTC environment. The fixed timezone makes `ssh-keygen`
certificate-validity inspection deterministic; parsed times must still match
the fixed validity policy within the documented process-scheduling tolerance.
The adapter never introduces a shell, logs stdin, or adds a generic
remote-command surface.
