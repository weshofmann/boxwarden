# ADR 006: Go-first control plane and quarantine for hostile dependency execution

Status: Accepted

Repository-owned lifecycle/control software defaults to Go with a small dependency surface. Node is retained for third-party tools and projects, but npm/install/build execution is hostile code by default. Separate credential-free quarantine VMs contain untrusted dependency work; worktrees alone do not isolate the guest user secrets. `boxwarden` prevents its own normal profile/secret injection into quarantine but cannot prevent manual GUI login, so quarantine ingress is public or short-lived read-only material only.
