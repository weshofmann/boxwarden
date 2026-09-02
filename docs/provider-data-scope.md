# Provider data scope

A provider login authorizes that provider to receive whatever the user or tool sends; it does not make all restored data appropriate for that provider. A session belongs to one security domain, is created for a named purpose, and receives only the corresponding domain/profile/project scope. Cross-domain profile, credential, memory, or project lookup is never implicit.

For example, an example-project session may restore its selected declarative profile and example-project repository/Markdown memory while excluding unrelated personal knowledge and credentials.

Before enabling a provider in a session, classify the project/profile data it may receive. Do not enable remote-control daemons, desktop extensions, browser extensions, MCP servers, skills, hooks, or connectors by default. Each expands the session's executable or data-access surface and must be reviewed as session state.

Use a separate Tart session per provider for high-isolation work. Multi-provider sessions are allowed only with the explicit understanding that one guest-user compromise can expose every login and every sensitive artifact in that session. Provider scope is not implemented with multiple Unix users because Docker/root-equivalent access defeats that separation.

Claude Desktop and Claude Code remain supported M1A targets. Claude Cowork is unsupported because its Linux workflow requires KVM/QEMU/nested virtualization; M1A does not enable Tart nested virtualization for it.
