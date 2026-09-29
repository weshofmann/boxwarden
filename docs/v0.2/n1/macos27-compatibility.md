# macOS 27.0.1 N1 compatibility qualification design

This records the bounded source policy and attended-trial design for the
intentionally upgraded macOS 27.0.1 (build 26A434) host. Source admission on
this branch is **pending final review and live qualification**, not a runtime
result or authorization to install the N1 candidate.
The 2026-09-28 macOS 26.6.2 results remain historical evidence in the
[matrix](matrix.md). The stock toolchain remains the installed default.

## Separate the boundaries

The previously staged executable refuses 27 before a VM is launched: its
`hostx` pins 26.6.2/25G83 in public init, the root install entry, read-only
doctor, and strict manifest parsing. That refusal is policy evidence. It does not prove a
Tart, Softnet, APFS, vmnet, guest desktop, or network API failure on 27. The
unchanged Tart 2.32.1 executable still matches SHA-256
`05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d`;
its app signature verifies and `--version` returns 2.32.1 on 27. This checks
binary identity and launch only, not VM runtime behavior. The installed stock
Softnet SHA-256 remains
`ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e`.
The reviewed N1 Softnet executable/archive remain
`064206d28d82b86093244114f44f726f4f5967575a9b298a7123bf0beb740ef0`
and `e06a722dfc9ab998f99144adc88ff9b03cd3356d1d1b62cb3c692cd4731b458e`.
No new Softnet or Ubuntu build is indicated by this preflight.

The separate storage preflight found native Tart APFS volume
`568EE3B5-885B-4278-BD0E-5FE77C5D01A8` present, encrypted, locked, and
unmounted, with its established bare mountpoint still mode `0000`. The
hash-verified reviewed native-mount helper reports `unlock` as the required
normal action. Qualification volume
`A178510A-D5EC-4495-828B-BD5445E2B66D` is absent from the current APFS
inventory; it cannot safely be mounted by path alone. Neither volume was
unlocked, mounted, recreated, or modified during this design review.

## Exact source policy

Admit only `darwin/arm64` with these current release/build pairs:

| Current host | Manifest installation record | Result |
| --- | --- | --- |
| 26.6.2 / 25G83 | 26.6.2 / 25G83 | Admit existing qualified installation |
| 27.0.1 / 26A434 | 26.6.2 / 25G83 | Admit historical stock installation for attended 27 trial |
| 27.0.1 / 26A434 | 27.0.1 / 26A434 | Admit new exact installation for attended 27 trial |
| 26.6.2 / 25G83 | 27.0.1 / 26A434 | Refuse reverse adoption |

Every mixed release/build pair and other host or manifest platform is refused.
The v2 manifest's `macos` and `macos_build` are immutable **installation
facts**. The existing stock manifest records 26.6.2/25G83 and must stay
byte-identical; its observed SHA-256 is
`cded7e1bad299c0839ab472aa3636d26b4738e4c018e08eeaa6ec15f41af50eb`.
No schema migration or stock manifest edit is needed. A newly published N1
manifest must record the root process's observed 27.0.1/26A434, never a
hard-coded historical value or caller-supplied platform.

Apply the exact-pair predicate at public init, the root installer, doctor,
manifest validation, and runtime admission. Apply the directional
current-host/installation compatibility check in doctor and publisher
preflight before group mutation, as well as publisher state checks. An absent
or unknown root-observed platform refuses publication before mutation. Keep
existing Tart/Softnet digest, signature, ownership, mode, path, ACL, link,
operator and privilege checks unchanged. Doctor's prerequisite result must
not be described as live gateway-denial or desktop qualification.

## Attended gate and evidence

After the source policy, tests and independent delta review, build and hash a
new CLI. The old `e703453a` executable identity and 26-specific cleanup
authorization do not cover it. A single refreshed attended request must name
the new CLI digest, unchanged stock Tart/Softnet identities, N1 Softnet
executable/archive identity, R1-enrolled config bytes outside backing storage,
exact 27 admission, and a new exact candidate cleanup helper that validates
the 27 installation record. Back up any manifest **only if** a separately
reviewed change becomes necessary; this design does not change the stock
manifest. No privileged publication or VM work precedes that approval.

On at most two new disposable guests, separately qualify stock desktop and
start/stop, N1 gateway TCP/UDP denial with live host and stock positives,
DHCP, UDP/TCP DNS and fallback, HTTPS, pinned SSH, restart, preflighted
synthetic CLI clipboard, and positive-controlled cross-guest TCP. Preserve
unknown or failed intervals and 26 evidence. Unavailable VPN, DNS64 and IPv6
environments remain unexercised. Finish with exact candidate cleanup and
current-host stock validation; stock remains the default.
