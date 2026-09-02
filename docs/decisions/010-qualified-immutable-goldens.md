# ADR 010: Golden inputs are qualified and artifacts are immutable

Status: Accepted

Every golden input is locked by exact platform, version, source, verification identity, and digest or signature before execution; mutable piped installers are rejected. The evidence distinguishes a reproducibly identified artifact from an indefinitely reproducible repository closure and records limitations honestly. Automatic updaters are disabled. A new golden revision is built, tested, human-accepted, and atomically promoted; running or promoted images are never updated in place.
