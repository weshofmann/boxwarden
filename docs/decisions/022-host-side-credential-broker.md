# ADR 022: Host-side broker for reusable API credentials

Status: **PROPOSED**

## Context

Boxwarden treats every session as potentially compromised guest root. A secret
delivered through argv, environment, stdin, filesystem, desktop keyring, or a
client process is therefore available to the adversary for the lifetime of the
session. VM isolation can stop that adversary from reading the trusted host, but
it cannot make a bearer credential inside the VM non-exfiltratable.

The useful security principle is narrower:

> A reusable credential the guest never possesses cannot be exfiltrated from
> guest memory or filesystem.

A broker does not remove the authority intentionally granted to a session. A
malicious guest can still issue any request the broker policy permits, send
project data to an allowed provider, consume budget, or exploit a broker/parser
bug. Interactive desktop and browser login state also remains inside the guest.

## Patterns reviewed

| Pattern | Useful mechanism | Boxwarden conclusion |
| --- | --- | --- |
| Docker Sandboxes credential management | Host proxy replaces a sentinel for an approved service/domain/header; host keychain and per-sandbox scope | Strong reference for explicit bindings; do not adopt global secrets, SSH-agent forwarding, or broad custom-domain behavior |
| nilbox zero-token architecture | Placeholder credential is replaced at a trusted boundary; guest lacks the real token | Confirms the core principle; Boxwarden needs stronger domain/session/destination and host attack-surface requirements |
| drydock credential gateway | Per-task gateway token, fixed model-provider gateway, request/budget bounds, and separate egress proxy | Useful separation of credential brokering from general CONNECT egress; its guest-firewall claim cannot be a Boxwarden security boundary |
| AWS STS and GitHub App installation tokens | Dynamically minted, time- and permission-bounded credentials | Prefer when upstream supports adequate scope; a delivered bearer remains stealable until expiry |
| OAuth token exchange and sender constraints | Exchange a durable credential for a narrower token; mTLS/DPoP can bind token use to a key | Useful upstream primitives, but key material inside malicious guest root is also stealable unless the host performs the proof/signing |
| One-shot secret injection | Maximum client compatibility | Comparison baseline only; the guest possesses the reusable secret |

Primary references:

- https://docs.docker.com/ai/sandboxes/configuration/credentials/
- https://docs.docker.com/ai/sandboxes/security/isolation/
- https://docs.nilbox.run/blog/zero-token-architecture/
- https://sricola.github.io/drydock/docs/egress.html
- https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp.html
- https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation
- https://www.rfc-editor.org/rfc/rfc8693.html
- https://www.rfc-editor.org/rfc/rfc8705.html
- https://www.rfc-editor.org/rfc/rfc9449.html
- https://www.rfc-editor.org/rfc/rfc9700.html
- https://www.rfc-editor.org/rfc/rfc9110.html

## Proposed decision

Defer implementation from v0.1. Design the broker as a later M1A capability,
after common security domains, reconciled session identity, Tart lifecycle, and
status exist. It is unrelated to the M1B Kindex persistence gate.

When implemented, prefer two mechanisms in this order:

1. **Upstream-minted short-lived, narrowly scoped credentials** when the
   provider supplies an appropriate primitive.
2. **Named host reverse adapters** for reusable HTTP API credentials when the
   client can target a provider-specific base URL on the Boxwarden broker.

Do not build a generic CONNECT credential proxy or a general TLS interception
system. Do not forward the host SSH agent.

## Credential classes

### Broker on the host

Reusable credentials that can be applied by a fixed provider adapter:

- OpenAI and Anthropic API keys;
- comparable model-provider bearer keys;
- selected registry/API credentials whose request authority and auth placement
  are fixed and testable;
- OAuth refresh tokens only when the entire refresh operation and resulting
  access-token use remain host-side;
- cloud signing credentials only through a provider-specific signing adapter,
  not a generic guest-readable metadata response.

The reusable value is referenced from a domain-owned trusted-host secret store.
No secret value appears in Boxwarden session state, logs, argv, environment,
guest disk, golden, profile, or broker capability.

### Prefer short-lived minting

Examples:

- GitHub App installation tokens limited to selected repositories and
  permissions, normally expiring after one hour;
- AWS STS role credentials with a session policy and minimum practical
  lifetime;
- OAuth token exchange when the authorization server actually supports
  resource/audience and scope reduction.

If the minted result is returned to the guest, it is a guest-held bearer secret.
The benefit is bounded lifetime and scope, not non-possession. Where practical,
the host reverse adapter should hold and use even the short-lived token.

Sender-constrained tokens improve replay resistance only when the private proof
key is outside the attacker. Giving both a DPoP/mTLS token and its key to guest
root merely changes the representation of the stealable credential. A
host-signing adapter can retain the proof key.

### Remain guest-local

- ChatGPT Desktop, Claude Desktop, Google, and other browser/app cookies;
- interactive provider OAuth sessions whose client cannot use a broker base URL
  or host-held refresh flow;
- desktop keyring entries and browser profiles;
- any token returned by an unavoidable in-guest interactive login.

These are disposable SESSION state. The broker must not be described as solving
them.

### Git and SSH

Prefer HTTPS plus a repository/permission-scoped GitHub App installation token
for automated Git access. It may be held by a Git HTTP adapter or, if client
compatibility requires delivery, enter the guest as an explicitly short-lived
credential.

Do not expose SSH_AUTH_SOCK or forward a host SSH agent. A malicious guest can
use an agent as a signing oracle for every key and destination the agent will
accept, even without extracting the private key. An SSH use case needs a
separately issued, narrowly scoped, short-lived guest credential or a
provider-specific host operation with an exact repository and verb.

Boxwarden's management SSH user certificate is independent. It authenticates
the trusted host to one guest and grants the guest no host or Git identity.

## Request path

Conceptually:

    guest client
        |
        | TLS to Boxwarden broker endpoint
        | per-session broker capability
        v
    domain/session authenticator
        |
        v
    named provider adapter
        |
        | reconstruct normalized request
        | inject/sign with host-held credential
        v
    one fixed upstream HTTPS authority

The broker binds only to the Boxwarden vmnet-gateway interface or another
equally qualified host-local guest endpoint. It never binds to the LAN, a
public interface, or every host interface. The existing accepted gateway
reachability makes transport possible; it is not itself authentication.

Each running session receives an unguessable, revocable capability, preferably
as a short-lived client certificate plus private key. Guest root can copy and
use that capability, which is expected. It authorizes only the recorded
domain/session/provider adapter, not the reusable upstream secret. The broker
also corroborates the observed session identity/network attachment where the
backend can do so, preventing a token alone from becoming a cross-session or
off-host credential.

This mTLS use authenticates a Boxwarden broker client. It does not imply that an
upstream OAuth token is RFC 8705 certificate-bound.

## Destination binding

Every adapter has compiled or qualified configuration for:

- one service identity and security domain;
- one exact upstream scheme, host, and port;
- allowed method and path prefixes;
- credential placement and format;
- request/response size and time bounds;
- redirect, streaming, protocol, and retry policy;
- rate, request-count, and budget limits where meaningful.

The guest does not select an upstream URL. The host resolves the configured
name, originates TLS with normal certificate validation and the same configured
server name, and reconstructs an outbound request rather than relaying raw
bytes.

Initial adapters:

- reject CONNECT, absolute-form proxy targets, arbitrary authority, IP-direct
  destinations, SNI/Host disagreement, Upgrade, WebSockets, and HTTP/3;
- do not follow redirects;
- reject or strip guest Authorization, Proxy-Authorization, Cookie, and other
  adapter-owned auth fields;
- reject ambiguous framing and hop-by-hop headers;
- normalize the path without allowing scheme/authority replacement;
- use a dedicated upstream transport per authority so HTTP/2 connection
  coalescing cannot cross services;
- define whether streaming is supported rather than silently buffering without
  bounds;
- never perform request-body secret substitution in the first version.

Tools that cannot use a broker base URL are unsupported by the first reverse
adapter. Transparent interception would require the guest to trust a
Boxwarden CA, the host to terminate arbitrary application TLS, and the broker to
defend every authority/protocol edge above. That is a materially larger system
and is not proposed.

## Malicious guest-root analysis

Guest root can:

- read and reuse the session's broker capability;
- call every allowed adapter until it is stopped, revoked, expired, or capped;
- send any permitted request body, including source/data the policy disclosed;
- bypass the broker and contact an allowed provider directly, but only with its
  placeholder or guest-held short-lived credential;
- alter DNS, proxy variables, CA trust, and client binaries inside the guest.

Guest root cannot obtain the reusable credential by bypassing the broker,
because direct TLS goes from the guest to the provider and no host substitution
occurs. It cannot change the broker's compiled upstream by changing guest DNS,
Host, SNI, redirects, or proxy environment. It cannot use one domain/session
capability for another if host authentication and state binding work correctly.

These are design claims requiring adversarial tests. A broker vulnerability,
host compromise, trusted provider that echoes credentials, or host log/core
leak can still disclose the reusable credential.

## Host attack surface

The broker moves risk from guest secret storage into a trusted host network
service. New attack surfaces include:

- TLS and HTTP parsers handling malicious guest input;
- request smuggling, ambiguous framing, decompression, streaming, and resource
  exhaustion;
- path/authority normalization, redirect, DNS, SNI, and HTTP/2 routing errors;
- adapter confusion and cross-session/cross-domain authorization bugs;
- secret-store lookup, token refresh, signing, caching, and revocation races;
- response parsing for usage/budget metering;
- credential exposure through logs, traces, crash reports, core dumps, metrics,
  or diagnostics;
- upstream provider, CA/DNS, and dependency compromise.

Mitigations include a small Go standard-library-first daemon, no shell or
dynamic command secret sources initially, no generic proxy mode, least-privilege
host account, domain-separated secret namespaces and policy snapshots, strict
input/output limits, bounded concurrency, no sensitive logging, and
property-oriented red-team tests. Separate per-domain processes are preferable
if the operational cost is acceptable; otherwise the authorization boundary
must be explicit and exhaustively tested.

Best-effort memory zeroization is not a meaningful Go security claim. Prevent
copies, keep secret lifetimes short in process memory, disable core dumps where
appropriate, and treat broker-process compromise as exposure of every secret
loaded into that process.

## Security domains and lifecycle

- No global broker secret or adapter fallback exists.
- A domain references only its own host secret-store entries.
- A session explicitly records provider adapters and the credential reference
  identities, never values.
- Start activates a fresh capability only after backend identity and policy
  reconciliation.
- Stop revokes the live broker capability before or while stopping the VM.
- Destroy revokes it even if the guest is unavailable; compromised destroy
  performs no guest callback.
- Status reports adapters, authority/scope, capability lifetime, request/budget
  state, and revocation result without printing secrets.
- Secret rotation changes the trusted reference and invalidates affected cached
  tokens deliberately.

One provider per high-isolation session remains the recommendation. A provider
adapter is an authority oracle: placing multiple providers in one compromised
guest combines their allowed operations even if no reusable key is extractable.

## Quarantine

Quarantine receives no broker capability, no reusable API/Git credential, and
no normal profile restore. Public source or an explicitly delivered
short-lived, narrowly scoped, read-only ingress credential remains the
recommended input.

A future exceptional quarantine broker grant would need a separate explicit
approval and policy. It must never inherit from the domain or from another
session.

## Audit and observability

Audit records are host-authored and contain:

- domain/session/capability and adapter identity;
- normalized upstream authority and operation class;
- timing, status class, bounded byte counts, request/budget counters;
- policy snapshot and implementation version;
- revocation and error state.

They contain no credential, Authorization/Cookie header, request body, response
body, URL query containing secrets, or unbounded upstream error. Debug mode
must not silently broaden this contract.

## Why not v0.1

The broker is valuable but not foundational to proving Boxwarden's
whole-disposable-computer thesis. It depends on domain/session identity,
reconciliation, backend transport, status, and compromised cleanup that do not
exist yet. It also covers API/CLI authentication but not the browser and desktop
login state that differentiates Boxwarden from coding sandboxes.

The v0.1 comparison baseline remains explicit one-shot/direct guest delivery
with conspicuous blast-radius documentation, one-provider-per-session guidance,
no argv/log/state persistence, and no credential at all in quarantine. The
broker should be the first post-v0.1 M1A security feature if real workflows show
API-key delivery is common.

## Acceptance gate

Before implementation this ADR needs explicit review plus:

- a protocol/adversary state machine;
- concrete first-provider adapter specs;
- transport authentication qualification on Tart/Softnet;
- cross-domain/session and stale-capability tests;
- destination-confusion, redirect, DNS/SNI/Host, smuggling, H2, streaming,
  exhaustion, log, crash, and revocation tests;
- proof that real credentials never enter guest-visible requests, responses,
  state, diagnostics, or captures;
- clear client compatibility and unsupported-tool reporting.

No broker code is part of this investigation.
