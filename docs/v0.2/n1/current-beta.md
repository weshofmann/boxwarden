# N1 in the current private beta

Status: source and packaging integration, with the bounded attended beta.15
observations below. This is not default N1 promotion or complete network
qualification. Historical N1 results in [matrix.md](matrix.md) do not qualify
this package. PR #18's diagnostic mission is not a prerequisite.

The normal launch, detached supervisor, installer and replacement paths already
use the build-selected policy. `n1candidate` binds the exact N1 Softnet version
and digest and adds `@boxwarden-host-containment` before vmnet forwarding. It
cannot admit stock Softnet as a substitute. Guest software preparation is
independent of that host policy; use current source/helper inputs and fresh
clones, with guest-support checks, for the compatibility trial.

## Build and inspect without deployment

On an Apple Silicon Mac, from a clean committed checkout:

```sh
GO_BIN=/absolute/actual/go bash tools/private-beta/build.sh \
  0.2.0-beta.15 /absolute/new-private-output n1candidate
PACKAGE=/absolute/new-private-output/boxwarden-0.2.0-beta.15-darwin-arm64-n1candidate
cd "$PACKAGE"
shasum -a 256 -c SHA256SUMS
"$PACKAGE/bin/boxwarden" build-info --json
cat BUILD.json
```

Omitting the third argument retains stock packaging. The candidate has a distinct
archive name and default application identifier. `BUILD.json` records the policy
reported by the compiled CLI; the builder refuses a selection mismatch. The app
bundles those exact CLI bytes and displays a candidate warning. The UI label is
information, not an admission or enforcement boundary. The package ships no
Softnet executable and installs nothing. A candidate CLI against stock admission
must fail, not fall back. Use a separate initialized candidate configuration
only after the exact privileged deployment is approved.

## Small live comparison after approval

Use the ordinary `init`, `doctor`, project setup/create/open/stop/rebuild and
explicit clipboard copy/paste interfaces. Keep stock and candidate configuration,
state and application identity separate. Use at most two task-owned fresh guests
with current generic guest support. Inspect the root-owned clipboard adapter and
READY generation before a synthetic transfer; do not use the general Mac
clipboard or infer readiness from a backend process.

1. Run an owned fixed-response TCP/UDP listener from
   `tools/n1-qualification/listener.py` at an observed gateway address (and an
   assigned non-gateway IPv4 address if available). Bracket each candidate attempt
   with working host controls and a stock guest positive control. Retain endpoint,
   guest/policy identity, time and response counts. Reap the bounded listener.
2. Verify DHCP and the advertised gateway's UDP/TCP DNS, public HTTPS and pinned
   host-initiated management. Test synthetic clipboard copy and redirected paste
   for exact equality. Treat ambiguous outcomes as failures to establish a row.
3. Repeat the owned service controls after ordinary stop/start and after public
   project replacement. Confirm each new generation retains the candidate policy
   and required compatibility. Keep the independent workspace attached through
   supported lifecycle operations.
4. For peer isolation, use a listener on the owned stock peer. Prove it responds
   locally and from the host; show the candidate can still reach its permitted
   infrastructure. Correlate denial with the existing private-destination fallback
   and peer-ARP restriction. A timeout is not required: `EHOSTUNREACH` can establish
   denial when the controls and enforcement path explain it. An unexplained
   connection failure cannot.

This does not qualify VPN/split DNS/DNS64, topology transitions, IPv6-only upstream,
NAT hairpin addresses, hypervisor/parser attacks or the host-address observation
race. N1 denies native guest IPv6, fragmented IPv4 and unsupported frame forms.
DHCP and gateway DNS remain deliberate infrastructure exceptions. Scope any
successful live claim to the tested supported environment and stable directly
assigned IPv4 host endpoints.

After the trial, stop the new guests and retain their workspace/evidence. Verify
no recorded or live candidate consumer remains, then remove only the approved
exact candidate digest tree with the attended checked removal procedure. Leave
stock installation, operator group, all pre-existing VMs and settings unchanged.

## Attended beta.15 observations (2026-10-04)

Tested source: `d935f1bf7a1f5dd1764c458e88efb4d27f358820` (PR #36),
stacked above PR #35. This results update does not rebuild or substitute the
approved executables. Both exact-head CI checks passed before installation.
The attended installation and independent inspection matched the approved
artifact, manifest platform, ownership and modes; both doctors were healthy.

- Host: Apple Silicon, macOS 27.0.1 / 26A434; Tart 2.32.1.
- Candidate CLI SHA-256:
  `ff4e97f071d40375e44396663b91339619f7336037d4a7eebf2c250636435fd3`.
- Candidate Softnet: 0.19.0-boxwarden-n1.1, SHA-256
  `064206d28d82b86093244114f44f726f4f5967575a9b298a7123bf0beb740ef0`.
- Separate stock/candidate alpha configurations, fresh prepared-base clones,
  independent 64 MiB workspaces and no provider credentials. Actual clone
  bootstrap/clipboard/support-check digests and root-owned modes matched the
  prepared inputs. Legacy-base software warnings were retained.

| Observation | Actual result |
| --- | --- |
| Owned gateway service, TCP and UDP | Candidate root timed out in initial, ordinary restart and rebuilt-system intervals. Host and stock positives bracketed every attempt; each bounded, reaped listener counted four TCP and four UDP positive responses. |
| Directly assigned private host address | Host controls passed; both guests denied it. This does not distinguish N1 from the existing private-address restriction. |
| Required connectivity and management | Both guests passed DHCP, advertised gateway UDP/TCP DNS, truncated UDP DNSKEY with TCP fallback, public HTTPS and strict pinned management in all three phases. |
| Explicit clipboard | Exact synthetic stdin/copy/redirected-paste equality in both guests in all three phases. Automatic Tart clipboard/audio remained disabled; no general Mac clipboard was accessed. |
| Workspace lifecycle | An imported existing file was edited and a new file created; a reference file stayed unchanged. All three intended contents survived ordinary stop/start and public candidate system replacement. Workspace, filesystem, session and import bindings persisted; only that project's old system was retired. Fresh live candidate generations retained the N1 selector. |
| Stopped export | Public project export passed its independent disk checks after a stop reporting cleanliness unverified. Host comparison matched the intended edited three-file project and confirmed the original two-file source unchanged. Pristine-import verification was not used as an edited-project check. |
| Owned peer high-port TCP/UDP listener | **Unqualified.** Local peer controls passed, but direct host controls failed. Candidate timeouts cannot establish this row. The bounded fixture exited with two local responses per protocol. |
| Candidate to stock SSH port 22 | Candidate root received `EHOSTUNREACH` (113), with an on-link route and FAILED neighbor, bracketed by successful same-generation strict host management. This is consistent with the inspected peer-ARP restriction and private-destination fallback before vmnet forwarding; it does not qualify arbitrary peer UDP services. |

Two candidate import attempts were refused by the unchanged Data reserve plus
stopping margin (25,584,461,414 bytes). Headroom recovered and the supported
retained-selection retry succeeded; no capacity/admission check or host setting
was changed. One stock status query returned unavailable supervisor-snapshot
drift before a peer fixture started. Both READY snapshots returned on recheck,
and the retried fixture ran. The original failure remains recorded; its cause
is unexplained and it is not containment evidence.

Both new projects and workspaces are stopped and retained. All 30 pre-existing
Tart objects kept their stopped/data inventory; reading the approved base can
advance its access metadata. Exact hash-checked cleanup `--check` passed using
`/usr/bin/python3`, followed by the owner’s attended removal with that same
interpreter. Independent post-removal verification confirmed the exact candidate
digest tree absent; stock doctor healthy; stock bytes, parent identities/modes,
operator group, protected inventory and both stopped workspace images preserved.
The earlier beta.14 handoff and returned project also passed their checks again.
No merge, release or promotion occurred. The candidate cannot start without its
exact admitted installation; no stock fallback or automatic reinstall is implied.

For the public project workflow, use the [named-project guide](../../operations/projects.md)
and [edited-project round trip](../../operations/project-roundtrip.md). Export
uses a new private destination, prints `project files:` for immediate inspection,
and `project export list NAME` locates retained exports. Treat returned files as
data; never execute them automatically or overwrite an existing host project.
