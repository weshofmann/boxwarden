# State model

The architecture uses five layers. A location or artifact must be classified before it is persisted or restored.

| Layer | Question | Examples |
|---|---|---|
| GOLDEN | What software/system configuration exists? | Ubuntu, desktop configuration, hardening, pinned applications and toolchains |
| PROFILE | What reviewed persistent declarative configuration should a workstation receive? | selected Markdown instructions, narrowly supported tool preferences, encrypted sensitive Markdown |
| SECRETS / IDENTITY | What may this session authenticate as? | provider login, Git credential, API key, SSH private key |
| PROJECT | What durable work/data does this session operate on? | Git remotes, datasets, databases, object storage |
| SESSION | What disposable mutable state has accumulated? | caches, browser profile, app login, worktree, download, temporary build output |

SECURITY DOMAIN is an independent namespace and disclosure boundary applied to domain-owned trusted-host policy for all five layers. A session belongs to exactly one configured domain, and commands that operate on domain-owned state require one explicitly. Generic-golden admission/selection records, profiles, age recipients and identity references, management CAs and host-key pins, credentials, memory, projects, session records, and runtime state are keyed by that domain. The immutable golden artifact itself is generic and contains no domain identity or domain trust: multiple domains may independently admit the same exact artifact without an implicit lookup or shared metadata record. Nothing else is implicitly shared or resolved across domains; an explicit reviewed transfer is required. Host-wide toolchain installation, privilege binding, and prerequisite health are not one of the domain-owned layers: `boxwarden init` establishes them once per trusted host and `boxwarden doctor` diagnoses them outside every domain namespace. M1A provides local domain separation against accidental cross-context use, not protection from an administrator of the trusted host.

Every PROFILE or PROJECT artifact has two independent classifications:

| Dimension | Values | Meaning |
|---|---|---|
| CONFIDENTIALITY | public/plaintext-safe; sensitive; secret | Whether the content may be disclosed and how it must be stored or transported |
| EXECUTION TRUST | inert data; declarative but security-sensitive; executable; opaque/unknown | What the content can cause when restored, read by an agent, or loaded by an application |

These dimensions do not imply one another. Plaintext-safe agent instructions may still be security-sensitive executable input. Sensitive Markdown may be inert prose. Secret values are SECRETS / IDENTITY and are not profile configuration even when embedded in a syntactically declarative file.

M1A PROFILE supports only named declarative adapters. It has no generic stateful or opaque-artifact category. Browser profiles, provider logins, application databases, caches, Kindex state, and unrecognized configuration remain SESSION state and are not capturable. A future milestone must separately approve any opaque state adapter.

GOLDEN never contains PROFILE, SECRETS / IDENTITY, PROJECT data, SESSION state, a domain CA anchor, or a fixed domain principal. Generic strict-sshd policy points at `/etc/ssh/boxwarden/active/...`; the golden contains only the root-owned parent and the statically built digest-locked generic helper, not `active`. Trusted serial bootstrap atomically publishes `active` with durable domain/session UUID/backend kind+object, CA fingerprint, and exact derived principal. Exchange nonce and start generation are framing/runtime correlation and are never installed there. PROJECT state must live in a durable external system, not solely in a VM disk. Normal destruction proves registered project durability or requires an explicit data-loss override. A clean session begins without credentials; a selected profile and session credentials are intentionally added later.

Durable lifecycle identity is the tuple `(domain, session UUID, backend kind/object, intended state)`. A start-generation token correlates one persisted transition with one supervisor instance, but the nonce, PID/process-start evidence, broker/PTY/Screen state, address, generation key/certificate, and health snapshot are runtime state, not alternate guest identity. Host-key pin records are durable trusted state bound to the exact domain/session/backend tuple and contain the public host key/fingerprint, never an IP. READY is derived from a fresh authenticated supervisor snapshot, live broker/Screen health, pin, current certificate and strict probe, and time-zone agreement; it is not inferred from backend-running or a stale persisted success bit.

Project memory follows the same layers: reviewed non-sensitive Markdown is PROJECT state in Git; encrypted sensitive Markdown may be PROFILE or PROJECT state according to scope; unreviewed notes are SESSION state. Any search or retrieval index is disposable derived SESSION state unless a future architecture explicitly says otherwise.
