# Credentials and identity

Provider authentication, OAuth sessions, API keys, GitHub credentials, browser cookies, and private SSH material are SECRETS / IDENTITY. They belong to one security domain, are created or injected into a particular guest session, and are never golden or profile configuration. `boxwarden` never discovers or substitutes credentials from another domain. Provider/application authentication is deferred beyond the current V1-V4 executable plan; V3/V4 implement only Boxwarden's management SSH identity.

Use least-privilege, task-scoped credentials when a provider supports them. GitHub access should use a dedicated limited identity or fine-grained credential rather than a broad personal token. Credentials are delivered over SSH or completed through the guest GUI; host browser profiles and host credential stores are not used for application login.

One provider per Tart session is the recommended high-isolation mode. A multi-provider session is supported for convenience, but every provider login, browser session, API key, Git credential, and sensitive artifact available to that guest user is in the same compromise domain. Separate Unix users are not treated as provider isolation while the operating principal has Docker/root-equivalent access.

Quarantine sessions accept no reusable provider or Git credential. `boxwarden` refuses normal secret and profile injection into quarantine, but it cannot prevent a human from manually authenticating in the guest GUI. Supported quarantine ingress is anonymous public source or a narrowly scoped, short-lived, read-only credential. No reusable write credential enters quarantine. M1A has no generic export from quarantine to the host; a future export-to-host-quarantine flow requires separate review.

A clean interactive session may receive credentials only after creation. Secrets are injected for a bounded operation through SSH standard input or are created by completing authentication entirely in the guest GUI. They are not placed in process arguments, host shell history, profile artifacts, or golden files. Where a provider only offers broad credentials, the user explicitly accepts the broader session blast radius.

On suspected compromise, revoke provider/Git credentials before further use, do not inject a fresh rescue credential, destroy the session with `--compromised`, and create a new session.

age decryption identities are host-only recovery material, not session credentials. They are never copied into a guest, future retained-session/checkpoint artifact, golden, repository, profile store, or Tart disk.

For management SSH, accepted ADR 012 uses one distinct user CA per security domain. `boxwarden --domain <domain> domain init` creates it explicitly once with immutable metadata binding domain ID, Ed25519 algorithm, public key/fingerprint/digest, a unique creation UUID, and exact creating operator UID/name. Domain init receives the complete configured-domain set and compares public fingerprints across configured domain roots solely to reject accidental reuse; this is not credential discovery, selection, or cross-domain fallback. Copying a complete CA tree fails its bound domain ID. Missing, malformed, unsafe, reused, or conflicting state fails closed and start never creates or rotates it. The command does not install or modify host-global prerequisites. The private CA remains host-only outside repository, guest, backend-image, and argv/log state. Generic goldens contain strict sshd policy and bootstrap target locations but no domain CA anchor or fixed principal.

On first start, brokered ADR 017 automation atomically/idempotently installs only the selected domain CA's public anchor, exact per-session principal, and durable domain/session/backend/CA/principal binding, then verifies effective sshd policy and returns the clone's fresh SSH host public key. Nonce and start generation frame and correlate the response but are not installed. Boxwarden pins the host key before issuing a short-lived certificate with `ssh-keygen -O clear` and no extensions.

Strict SSH uses absolute `/usr/bin/ssh`, `-F /dev/null`, exact generation
`IdentityFile` and `CertificateFile`, a UUID-derived `HostKeyAlias`, an
alias-keyed exact `UserKnownHostsFile`, `GlobalKnownHostsFile=/dev/null`,
`StrictHostKeyChecking=yes`, `CheckHostIP=no`, `BatchMode=yes`,
`IdentitiesOnly=yes`, `IdentityAgent=none`,
`HostKeyAlgorithms=ssh-ed25519`, `UpdateHostKeys=no`,
`VerifyHostKeyDNS=no`, `CanonicalizeHostname=no`, `ProxyCommand=none`,
`ProxyJump=none`, `ControlMaster=no`, `ControlPath=none`, `RequestTTY=no`,
`PasswordAuthentication=no`, `KbdInteractiveAuthentication=no`,
`ForwardAgent=no`, `ForwardX11=no`, `ClearAllForwardings=yes`, `Tunnel=no`,
and `PermitLocalCommand=no`. The fixed user is `boxwarden`; the fixed remote
command is `/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap management`.
Only typed fixed request shapes for `probe`, `timezone-apply`, and
`timezone-read` travel separately on stdin—there is no generic argv API. Guest
sshd verification checks `PermitUserEnvironment no`, `PermitUserRC no`,
forwarding/tunnel and CA/principal paths, and `AuthorizedKeysFile none`;
`PermitLocalCommand=no` remains a client option, not a server field.
First-connection TOFU, changed-key acceptance, cross-domain fallback, and
`StrictHostKeyChecking=no` are prohibited.
