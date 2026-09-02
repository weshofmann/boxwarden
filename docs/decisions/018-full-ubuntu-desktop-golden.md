# ADR 018: Use the full Ubuntu Desktop source

Status: Accepted; qualified by Task 0

Extends ADR 002 and supersedes the provisional Task 0 selection of
`ubuntu-desktop-minimal`.

## Context

The initial Task 0 candidate selected Canonical's official
`ubuntu-desktop-minimal` source to reduce golden-image size and unnecessary
packages. It successfully provided Wayland, XWayland, Firefox, SSH, and the core
desktop. It did not install LibreOffice.

The empirical comparison was smaller than expected. The minimal candidate used
approximately 9.5 GiB of its root filesystem and contained 1,481 Debian packages
and eight snaps. The full-desktop control used approximately 11 GiB, 1,629
Debian packages, and nine snaps. Its 148 additional Debian packages included 76
LibreOffice package records.

Boxwarden agents will create and inspect more than source code. DOCX, XLSX, PPTX,
PDF, Markdown, HTML, and other deliverables need guest-local visual verification.
Installing productivity tools on demand makes every disposable session depend on
its current network/VPN state, increases cold-start time, and lets nominally
identical sessions drift. Selecting a custom minimal-plus-office package set
would recover little of the measured footprint while making Boxwarden maintain
its own desktop composition instead of Canonical's coherent source.

## Decision

The M1A Ubuntu golden selects Canonical's full `ubuntu-desktop` installation
source. Integrated document and productivity applications are expected golden
content, not optional per-session state.

The golden remains immutable and deliberately rebuilt. Full Desktop does not
authorize automatic package updates or mutable package acquisition during image
promotion. Task 0's two final clean reproducibility runs used the same immutable
full-desktop seed; the minimal integrated policy run remains a useful checkpoint
but is not either final run.

## Consequences

The golden consumes approximately 1.5 GiB more installed space in the measured
Ubuntu 24.04.4 ARM64 comparison and carries 148 more Debian packages plus one
additional snap. That modest increase adds package and application surface
inside a guest that is already disposable and root-owned by the agent.

In exchange, sessions have predictable document-viewing and productivity tools
without network access, package installation latency, or session-specific drift.
The project avoids maintaining and qualifying a bespoke desktop package set.

Future footprint optimization may remove a specific application only after its
workstation use cases and transitive effects are understood and the resulting
guest definition is requalified.
