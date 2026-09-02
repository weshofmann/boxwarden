# ADR 016: The agent owns the disposable guest

Status: Accepted; qualified by Task 0

## Context

Boxwarden exists to give an AI agent a complete workstation it can modify and
discard. Normal work includes installing packages, changing development tools,
running Docker workloads, managing services, and diagnosing the operating
system. Requiring a human to type a sudo password would break unattended work.

Restricting passwordless sudo to a command list does not preserve a meaningful
boundary if the account may install arbitrary software. Package managers run
package-provided maintainer scripts as root. An allowed package installation can
therefore change NetworkManager, routes, firewall rules, systemd units, sudoers,
executables, or any other guest state. Similar privilege escapes follow from
allowing general-purpose editors, interpreters, service managers, or writable
root-owned configuration.

The project already treats Docker-group membership and privileged containers as
guest-root-equivalent. Making the workstation account nominally less privileged
would add workflow friction without changing the boundary that protects the
trusted host.

## Decision

Every Boxwarden session has one explicit agent workstation account. In the
Ubuntu M1A guest this is the first regular account, UID 1000. It has unrestricted
non-interactive root access through:

```text
boxwarden ALL=(ALL:ALL) NOPASSWD: ALL
```

The guest desktop automatically logs in that account. Automatic screen
blanking, screen locking, and idle suspend are disabled so unattended agent work
continues without human console intervention.

The guest is nevertheless not a management authority over the host. Direct SSH
root login and password login remain disabled. The trusted host authenticates
management access with short-lived user certificates, and login reaches the
workstation account before any guest-local elevation. No trusted-host Docker
socket, filesystem, display server, credential store, network mode, or control
socket is exposed to the guest.

Every backend security property is evaluated against a malicious guest root.
For M1A, Tart plus Softnet must continue enforcing default private/link-local
denial, every explicit per-session CIDR boundary accepted under ADR 015,
session isolation, and the documented host-integration boundary after the guest
changes its own routes, addresses, firewall, services, or network configuration.

## Consequences

An agent or hostile workload can completely corrupt, disable, or persist within
its current VM. That is expected session compromise. Recovery is containment,
credential revocation where necessary, destruction, and recreation from a
qualified golden—not repair of the compromised guest.

Sudoers policy is intentionally simple and auditable. Boxwarden does not claim
that command filtering can safely distinguish package installation from other
root effects. Quarantine sessions and credential/provider scoping still reduce
the value exposed to a compromised guest, but do not reduce its operating-system
privilege.

Task 0 verified that the installed account is UID 1000, automatic desktop and
serial login succeed, `sudo -n` reaches UID 0, idle desktop policy remains
inactive, and the backend-enforced network properties survive the qualified
guest configuration.
