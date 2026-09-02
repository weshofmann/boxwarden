# Node and npm threat model

Node/npm remains available in the guest because it is part of the work being contained. It is not the default platform for repository-owned software.

Assume npm install, npm ci, package lifecycle hooks, transitive dependencies, repository scripts, and build tooling can execute hostile code as the guest user. The same reasoning applies to Python package installers and other executable dependency systems.

Host isolation reduces host impact but does not protect browser sessions, provider credentials, profile artifacts, or project material intentionally available to that guest user. For unknown or suspicious dependencies, use a separate quarantine Tart session. Do not expose secrets, reusable GitHub credentials, or personal/provider logins there.

Global npm tools increase the golden dependency and update surface. Prefer first-party standalone distributions; otherwise record exact global package provenance, version, dependency mechanism, and rationale in the golden lock.

Absence of global npm packages does not remove Node-derived execution. Electron desktop applications, extensions, MCP servers, skills, hooks, provider plugins, Corepack/package-manager downloads, and project-local dependencies can execute with the guest user's authority. Golden acceptance inventories default extensions and autostart entries; no extension, MCP server, hook, or scheduled agent is enabled by default.
