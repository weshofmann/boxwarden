# State model

The architecture uses five layers. A location or artifact must be classified before it is persisted or restored.

| Layer | Question | Examples |
|---|---|---|
| GOLDEN | What software/system configuration exists? | Ubuntu, desktop configuration, hardening, pinned applications and toolchains |
| PROFILE | What reviewed persistent declarative configuration should a workstation receive? | selected Markdown instructions, narrowly supported tool preferences, encrypted sensitive Markdown |
| SECRETS / IDENTITY | What may this session authenticate as? | provider login, Git credential, API key, SSH private key |
| PROJECT | What durable work/data does this session operate on? | Git remotes, datasets, databases, object storage |
| SESSION | What disposable mutable state has accumulated? | caches, browser profile, app login, worktree, download, temporary build output |

SECURITY DOMAIN is an independent namespace and disclosure boundary applied to all five layers. A session belongs to exactly one configured domain. Golden revision pointers, profiles, age recipients and identity references, credentials, memory, projects, session records, and runtime state are keyed by that domain. Nothing is implicitly shared or resolved across domains; an explicit reviewed transfer is required. M1A provides local domain separation against accidental cross-context use, not protection from an administrator of the trusted host.

Every PROFILE or PROJECT artifact has two independent classifications:

| Dimension | Values | Meaning |
|---|---|---|
| CONFIDENTIALITY | public/plaintext-safe; sensitive; secret | Whether the content may be disclosed and how it must be stored or transported |
| EXECUTION TRUST | inert data; declarative but security-sensitive; executable; opaque/unknown | What the content can cause when restored, read by an agent, or loaded by an application |

These dimensions do not imply one another. Plaintext-safe agent instructions may still be security-sensitive executable input. Sensitive Markdown may be inert prose. Secret values are SECRETS / IDENTITY and are not profile configuration even when embedded in a syntactically declarative file.

M1A PROFILE supports only named declarative adapters. It has no generic stateful or opaque-artifact category. Browser profiles, provider logins, application databases, caches, Kindex state, and unrecognized configuration remain SESSION state and are not capturable. A future milestone must separately approve any opaque state adapter.

GOLDEN never contains PROFILE, SECRETS / IDENTITY, PROJECT data, or SESSION state. PROJECT state must live in a durable external system, not solely in a VM disk. Normal destruction proves registered project durability or requires an explicit data-loss override. A clean session begins without credentials; a selected profile and session credentials are intentionally added later.

Project memory follows the same layers: reviewed non-sensitive Markdown is PROJECT state in Git; encrypted sensitive Markdown may be PROFILE or PROJECT state according to scope; unreviewed notes are SESSION state. Any search or retrieval index is disposable derived SESSION state unless a future architecture explicitly says otherwise.
