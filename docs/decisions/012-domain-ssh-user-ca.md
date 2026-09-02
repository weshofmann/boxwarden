# ADR 012: Per-domain SSH user CA for management access

Status: Accepted; amended after ADR 017 qualification

## Decision history

The original accepted decision established one OpenSSH user CA per security
domain, short-lived session/principal-bound user certificates, freshly generated
and host-pinned guest SSH host keys, and strict management SSH without host-agent
or forwarding exposure. It also assumed each domain's public CA anchor would be
baked into that domain's immutable golden and restricted serial use to bounded
host-key fingerprint output.

ADR 017 later accepted and Task 0 qualified a general trusted host-local serial
recovery channel. ADR 017 superseded only the bounded-serial restriction. This
amendment uses that already accepted channel to correct bootstrap ordering while
preserving ADR 012's security goals. Accepted history is retained rather than
recast: the stale pre-baked-anchor mechanism is replaced; the CA, certificate,
host-key pinning, and strict-SSH decisions remain in force.

## Amended decision

Every security domain has exactly one OpenSSH management user CA. The operator
creates it explicitly once with `boxwarden --domain <domain> init`; it is never
created lazily by session start. Its private key remains host-only outside the
repository, profiles, runtime roots, guest disks, command arguments, and logs.
Immutable metadata beside the key binds domain ID, Ed25519 algorithm, public
key/fingerprint/digest, a unique creation UUID, and exact creating operator
UID/name; load and issue revalidate it.
Explicit init receives the complete configured-domain set and compares public
fingerprints across configured roots only to reject accidental reuse. It does
not select or discover credentials across domains. Copying a complete CA tree
fails its bound domain ID. Missing, malformed, unsafe, reused, or conflicting
state fails closed; V0.1 does not silently rotate or replace it.

Golden artifacts are generic. They contain strict sshd configuration and fixed
bootstrap target locations, but no domain CA public anchor, domain identity, or
fixed domain principal. The same exact generic golden may be independently
admitted and selected by multiple domains.

After cloning, V4 starts the VM with ADR 017's retained trusted serial channel.
Automation performs fresh-nonce, bounded, deadline-controlled command/output
exchanges associated with exact durable domain, session UUID, and backend
kind/object plus the current start generation. Through that channel
Boxwarden:

1. atomically and idempotently installs only the selected domain CA's public
   anchor, exact session principal, and durable domain/session/backend/CA/
   principal binding;
2. verifies the effective sshd configuration and installed bytes;
3. obtains the clone's freshly generated SSH host public key; and
4. stores a host-side pin bound to the exact domain, session UUID, and backend
   object, with no IP address as identity.

Nonce and start generation are response/framing correlation only and are never
installed in `/etc/ssh/boxwarden/active`; a later generation accepts only the
same durable binding and current host key. Missing, duplicate, interleaved,
oversized, expired, or association-mismatched serial frames fail closed. An exact retry verifies already-installed bytes;
mismatched anchor, principal, sshd policy, or host key is drift and is never
overwritten automatically. Only after the pin exists may Boxwarden issue a
short-lived certificate for the session principal and attempt normal SSH.

The generic golden contains root:root mode-`0755` `/etc/ssh/boxwarden` but no
`active` child. Bootstrap builds a private sibling staging tree, then publishes
`active` atomically with root:root mode `0755` on it and its
`authorized_principals` directory, mode `0644` on the public CA and exact
`authorized_principals/boxwarden` file, and mode `0600` on the binding manifest;
no path component is group/other-writable. OpenSSH reads the configured CA and
principals files during each certificate authentication, so publication needs
no sshd reload. Traversable final directories are mandatory because OpenSSH
opens the principals file under the target workstation UID and StrictModes
validates the path ancestry. The helper validates exact bytes and modes itself;
`sshd -t`/`-T` alone do not prove them.

Certificates use `ssh-keygen -O clear` and contain no extensions. Management
SSH uses absolute `/usr/bin/ssh`, `-F /dev/null`, exact key/certificate, a
session-UUID `HostKeyAlias` and alias-keyed known-hosts file,
`GlobalKnownHostsFile=/dev/null`, strict checking with `CheckHostIP=no`, and
explicitly disables ambient config, DNS/update/canonicalization, proxies,
multiplexing, TTY, password/keyboard-interactive, agent, X11, TCP, stream-local,
tunnel, and local-command behavior. Its only remote command is `/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap management`; typed fixed
probe/time-zone request shapes follow on stdin, never a general argv API.
First-connection TOFU, silent changed-key acceptance, and
`StrictHostKeyChecking=no` are prohibited. A successful login as
the workstation account may use its accepted unrestricted guest-local `sudo`;
the guest privilege model is not an SSH trust boundary.

## Consequences

One generic qualified golden can serve multiple domains without duplicating the
image or embedding domain trust. Domain binding occurs only on the fresh clone
over a qualified host-local channel. The CA private key never enters the guest,
and network address discovery or reachability never establishes identity.

Readiness now depends on retained serial ownership, the exact pin, and strict
certificate SSH in addition to backend-running. Partial bootstrap remains
reconcilable only when the complete exact association and installed state can be
verified. This amendment creates no new network bootstrap path and makes no
change to ADR 017's trusted-host recovery authority.

## Implementation references

Ubuntu 24.04 packages OpenSSH 9.6p1. In that release, `servconf.c` stores the
`TrustedUserCAKeys` and `AuthorizedPrincipalsFile` pathnames during parse;
`auth2-pubkey.c` opens the CA and principals files during certificate
authentication. `match_principals_file` temporarily uses the target UID, while
`auth2-pubkeyfile.c` applies `safe_path_fd`/StrictModes to the principals path.
See the upstream 9.6p1 sources for
[`servconf.c`](https://github.com/openssh/openssh-portable/blob/V_9_6_P1/servconf.c),
[`auth2-pubkey.c`](https://github.com/openssh/openssh-portable/blob/V_9_6_P1/auth2-pubkey.c),
[`auth2-pubkeyfile.c`](https://github.com/openssh/openssh-portable/blob/V_9_6_P1/auth2-pubkeyfile.c),
and [`misc.c`](https://github.com/openssh/openssh-portable/blob/V_9_6_P1/misc.c).
