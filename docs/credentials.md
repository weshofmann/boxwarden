# Credentials and identity

Provider authentication, OAuth sessions, API keys, GitHub credentials, browser cookies, and private SSH material are SECRETS / IDENTITY. They belong to one security domain, are created or injected into a particular guest session, and are never golden or profile configuration. `boxwarden` never discovers or substitutes credentials from another domain.

Use least-privilege, task-scoped credentials when a provider supports them. GitHub access should use a dedicated limited identity or fine-grained credential rather than a broad personal token. Credentials are delivered over SSH or completed through the guest GUI; host browser profiles and host credential stores are not used for application login.

One provider per Tart session is the recommended high-isolation mode. A multi-provider session is supported for convenience, but every provider login, browser session, API key, Git credential, and sensitive artifact available to that guest user is in the same compromise domain. Separate Unix users are not treated as provider isolation while the operating principal has Docker/root-equivalent access.

Quarantine sessions accept no reusable provider or Git credential. `boxwarden` refuses normal secret and profile injection into quarantine, but it cannot prevent a human from manually authenticating in the guest GUI. Supported quarantine ingress is anonymous public source or a narrowly scoped, short-lived, read-only credential. No reusable write credential enters quarantine. M1A has no generic export from quarantine to the host; a future export-to-host-quarantine flow requires separate review.

A clean interactive session may receive credentials only after creation. Secrets are injected for a bounded operation through SSH standard input or are created by completing authentication entirely in the guest GUI. They are not placed in process arguments, host shell history, profile artifacts, or golden files. Where a provider only offers broad credentials, the user explicitly accepts the broader session blast radius.

On suspected compromise, revoke provider/Git credentials before further use, do not inject a fresh rescue credential, destroy the session with `--compromised`, and create a new session.

age decryption identities are host-only recovery material, not session credentials. They are never copied into a guest, future retained-session/checkpoint artifact, golden, repository, profile store, or Tart disk.

For management SSH, accepted ADR 012 uses a distinct user CA per security domain. Only the public CA trust anchor appears in that domain's golden; the private CA remains host-only, and `boxwarden` issues a short-lived certificate bound to one session UUID and principal. Each clone's regenerated SSH host key is pinned in a domain/session-specific known-hosts file. Task 0 qualified ADR 017's authenticated host-local serial recovery channel as the initial host-key observation path and proved exact agreement with host-side scans. First-connection TOFU and a silent `StrictHostKeyChecking=no` fallback are therefore unnecessary and prohibited.
