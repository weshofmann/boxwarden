# N1 in the current private beta

Status: source and packaging integration. Live validation of this beta is pending
separate operator approval. Historical N1 results in [matrix.md](matrix.md) do
not qualify this package. PR #18's diagnostic mission is not a prerequisite.

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
