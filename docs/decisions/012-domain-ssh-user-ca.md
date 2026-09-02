# ADR 012: Per-domain SSH user CA for management access

Status: Accepted; management path qualified by Task 0

ADR 017 supersedes this ADR's restriction of serial to a bounded fingerprint
channel. The per-domain SSH user-CA decision and host-key-pinning requirement
remain in force.

Each security domain uses a distinct OpenSSH user CA. Its public trust anchor is
non-secret GOLDEN configuration in that domain's golden artifact; its private
key remains host-only outside repository, profile, runtime, and backend-image
roots. `boxwarden` issues short-lived certificates bound to the exact session
UUID/principal and connects without agent, X11, TCP, or tunnel forwarding.
Each clone regenerates its SSH host keys, which are pinned in a
domain/session-specific known-hosts file. Task 0 evaluated Tart's serial console
as the authenticated non-network path for initial host-key observation and
qualified the general recovery channel adopted by ADR 017. First-connection
TOFU and a silent `StrictHostKeyChecking=no` fallback are unnecessary and remain
prohibited.

Task 0 qualified short-lived user-certificate login, immediate host-local
pinning, and exact agreement between serial-observed and host-scanned clone host
keys. ADR 017's general trusted-host recovery shell supersedes this ADR's
bounded-serial restriction; the per-domain CA and host-key-pinning requirements
remain accepted inputs to implementation.
