# N1 runnable-but-uninstalled diagnostic package implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development. The owner supplied the merged design, execution scope and delegation policy; execute within that authorization. Do not request renewed design or routine implementation approval.

**Goal:** Prepare exact reproducible unprivileged diagnostic artifacts and a single future attended two-guest request; never execute that window during preparation.

**Architecture:** Add a distinct diagnostic build and clone-only helpers around the canonical clipboard and N1 packet paths. Bounded metadata receipts observe actual operations; they neither authorize operations nor select policy. Admission and cleanup bind the new digest independently of stock and canonical N1.

**Tech Stack:** Go 1.27.0 control plane, pinned Rust 1.98.1 Softnet, Python standard-library guest fixtures and packaging.

**Spec:** `docs/v0.2/n1/two-gap-diagnostic-design.md`, `arp-observer-contract.md`, `clipboard-source-diagnostic-report.md`, and the owner request recorded in the current chat.

## Global Constraints

- Baseline: freshly fetched merged PR17 main `bbcfaac3f3e21b7094b2db4476c54115eb43722e`; branch `weshofmann/feat/n1-diagnostic-package`.
- No privileged installation, /Library mutation, VM or golden operation, guest writes, real Mac clipboard, host route/ARP/firewall/DNS/VPN mutation, live probe, merge, release or default enablement.
- Preserve stock and canonical N1 artifact identities, production clipboard framing and operation semantics, all historical evidence and verdicts.
- No payload/error-string/argv/environment/process dump; fixed codes, finite counters and byte limits. Missing, malformed, dropped, overflowed or incomplete observations cannot establish causal success.
- Trusted-host clipboard receipt: one operation UUID plus exact generation, at most 32 records / 16 KiB; monotonic offsets, one classification per stage; no retry or admission capability.
- Guest overlay: fixed root-owned paths and 64 slots of 256 bytes per read/write trace; original adapter bytes and locked generic bootstrap remain canonical.
- Network: one exact candidate/control pair, finite 1–30 second watch, at most 4096 dispatches per direction, no promiscuous mode or packet payload retention. Observe before filtering/refresh and before write-result collapse.
- VM write full-length API return does not prove enqueue or delivery; preserve ENOBUFS/error behavior. Historical errno113 remains UNQUALIFIED.
- Current owner routing: every routine and implementation task formerly assigned to Luna/Terra uses GPT-6.1 Sol; security-sensitive integration uses Sol/High. Luna is prohibited until owner changes this instruction. Required fresh reviews use Sol/High or specified XHigh. No Astra. Record unresolved question/evidence/bounded outcome before XHigh/Max.
- Controller owns focused verified commits, immediate pushes and Draft PR publication. Never rewrite published history. Keep private evidence and credentials outside Git.

## Review Focus

1. Kernel filter accepts only header bytes for the exact pair and records unsupported/truncated matching traffic as incomplete (Task 1).
2. Observer or metadata query failures do not lend deadlines, retry transfers or conceal unknown writes (Tasks 2–3).
3. Policy reason comes from the actual evaluated branch, and write count/results come from the actual single write (Task 3).
4. Generation, process birth, lease or pair changes invalidate receipts; zero counters without ready/stop/drop coverage cannot attribute absence (Tasks 1–4).
5. Build/source/manifest/cleanup identities cannot silently adopt stock/canonical N1, stale runtime state or a sibling digest (Tasks 4–6).

## Execution dependencies

Task3 is divided into clipboard Go/helper integration (3a), actual Rust hooks/protocol/self-lock (3b), and Go owner/launcher watch and lock lifetime (3c). After3b, perform Task4a: reproduce Rust artifacts, bind the resulting exact digest, and implement tag-selected three-file publication/shared SH admission. Those are prerequisites for3c; no placeholder identity may pass admission. Task4b then completes cleanup and integrated Go/helper reproduction. The final artifact-source commit is separate from later static-lock documentation closeout. This split preserves the original task scope and final independent reviews.

## Task 1: Finite clone-only guest metadata observer

**Files:** Create `tools/n1-qualification/guest_metadata_observer.py` and `guest_metadata_observer_test.py`.

**Interfaces:** Consume one strict stdin JSON watch with version=1, domain=`n1qualification`, role=`candidate` or `control`, interface, distinct candidate/control session/generation UUIDs/backend names, leased private IPv4 addresses, unicast MACs, gateway and duration_ms 1–30000. Export `parse_watch(raw: bytes) -> dict`, `build_filter(watch: dict) -> list[tuple[int,int,int,int]]`, `classify_header(watch: dict, header: bytes, packet_type: int) -> str | None`, and `observe(watch: dict) -> dict`. CLI accepts only `observe`; emits bounded ready and final summary records. No arbitrary file or command argument.

- [x] Write red tests proving strict schema/duplicate/type/size rejection; exact ARP and TCP22 tuple filter/header-only capture; cached-neighbor SYN; expected/unexpected peer MAC and broadcast replies; ICMP unreachable quoted tuple; unrelated-address exclusion; overflow/drop/truncation/start-stop incompleteness; privacy and fixed read-only route/neighbor argv. Use hand-built headers and a small cBPF interpreter to test the actual filter, not textual source assertions. No AF_PACKET socket is opened by tests; one hosted-Linux-only AF_UNIX fixture checks the real cBPF verifier/execution.
- [x] Observe the intended red failures, then implement a standard-library Linux AF_PACKET/SOCK_RAW observer with an attached exact cBPF filter, no membership/promiscuous option, only protocol-header snap lengths, monotonic finite deadline and bounded counters. Attach filter before binding the selected interface. Read PACKET_STATISTICS drops, detect MSG_TRUNC/unsupported matching headers, and require ready, stop and loss-free accounting before complete=true. Use only `ip -j route get <peer>` and `ip -j neigh show to <peer> dev <interface>` before/after, closed environment, timeout, output bounds and schema sanitization. No ping/arping, connect, listener, route/neighbor write or packet/file capture.
- [x] Run `PYTHONDONTWRITEBYTECODE=1 python3 tools/n1-qualification/guest_metadata_observer_test.py`; Darwin runs pass 30 tests with one Linux-only skip on both Python 3.9.6 and 3.14.7. Fresh scoped Sol/XHigh source review is clean. Published commit1fdacd34 has green CI36663134689; hosted Ubuntu executes all31tests without skips, including actual AF_UNIX cBPF kernel execution. Future guest AF_PACKET/offload capture qualification remains pending.

## Task 2: Bound clipboard overlay and exact owner collector

**Files:** Extend only trial tools `clipboard_diagnostic.py` and its tests; add fixed collector/staging source as required. Canonical adapter and installed generic bootstrap binary stay unchanged.

**Interfaces:** Strict clone-only operation binding supplies operation UUID + exact domain/session/backend/generation externally, verified before input or destination mutation. Trace slots bind that identity with bounded monotonic offsets. Collector reads only exact owner PID/stat/UID/starttime, validates birth identity and fixed trace schemas, never requests text.

- [x] Add red tests for wrong/stale operation bindings, generation mismatch, owner PID reuse/UID/zombie/missing stat, malformed/overflow/failed sink, acknowledgement/retain progression and no payload/error leakage.
- [x] Implement minimal fixed overlay/collector hooks preserving native claim-before-ack-before-retain and first read only. Diagnostic failure yields incomplete/refusal, never transfer success or retry. Do not change production bytes or public text frames.
- [x] Run overlay and canonical tests, including canonical fixtures with the overlay installed; review separately with fresh Sol/High.

Task 2 is closed for the frozen Python source after two Important findings and one scoped fix/re-review round. The controller runs48controls on both Python versions; each includes59canonical fixtures. The actual Go closure producer and complete package reviews remain later gates.

## Task 3: Trusted-host and actual Softnet forwarding integration

**Files:** New bounded Go diagnostic recorder plus diagnostic-only hooks at `guestproto.Clipboard`, `sshx.Clipboard`, `sessionruntime.ReadClipboard`, supervisor clipboard control; distinct `tools/n1-diagnostic-softnet/` policy copy, observer, integration patch and tests. Never alter canonical `tools/n1-softnet/` source or identity.

**Interfaces:** Resolve exact cross-process receipt binding and post-lease Softnet watch activation against the existing closed Tart/private supervisor path before implementation. Record the resolution in the executable design addendum. Carry guest stages via a separately collected metadata channel with unchanged text framing; no stdout/stderr exposure. The diagnostic policy returns typed actual ARP reason and minimal current lease/target-local/freshness metadata; forwarding receipt records actual single write outcome/count/length without changing its return.

- [ ] Add red fixtures for every stage failure and recorder rejection, byte-identical ordinary text frames and no retry/authority. Add actual diagnostic dispatch equivalence tests against canonical policy for permitted/denied/refresh/fallback/write/error/short-write cases.
- [ ] Apply a separate patch against exact upstream plus canonical N1 patch: actual VM ingress before refresh, actual decision with typed ARP reason, actual Host::write result, same-IP host ingress coverage before filters and actual VM::write before ENOBUFS collapse. Record proxy/broadcast/unsupported header classes, finite accounting and missing summary as incomplete. No offline replay as attribution evidence.
- [ ] Verify exact dependency vmnet 0.5.1 status/size/packet-count contract; observation must not claim enqueue/delivery. Resolve private watch/sink binding without Tart change, ambient override, arbitrary path, new network socket or policy control.
- [ ] Run focused deterministic Go/race and Rust tests; fresh Sol/High clipboard and Sol/XHigh network reviews must inspect actual wiring and all required negative-evidence branches.

## Task 4: Distinct artifact, launch admission and exact cleanup

**Files:** Diagnostic Softnet reproducible build/verify definitions and artifact record; diagnostic build-tag host identities, launch/control binding and tests; diagnostic-only exact cleanup source/tests.

**Interfaces:** Diagnostic CLI selects one exact distinct version/digest/path/selector at compile time. Stock, canonical N1 and diagnostic builds mutually reject inappropriate artifacts/manifests/selectors. Cleanup names only the new exact manifested tree and refuses live, recorded, ambiguous or unverifiable consumers.

- [ ] Add red mutual-rejection, child-observed argv-element, closed environment, generation/sink mismatch and cleanup preservation tests, including sibling stock/canonical tree and changed identity/ACL/link/mode cases.
- [ ] Implement the bounded diagnostic-only binding and cleanup source using existing validated admission mechanisms. No installation or cleanup execution on /Library; use synthetic roots only.
- [ ] Build diagnostic Softnet twice using exact source/patch/compiler/Cargo/locked cache and canonical disposable stage/remapped paths; independently compare executable and deterministic USTAR hashes. Build default/candidate/diagnostic Boxwarden and trial guest helper twice with exact Go/linker flags, record hashes and toolchain identities. Archive readable artifacts and provenance to durable authorized local storage, no private state in Git.
- [ ] Fresh Sol/High admission/artifact/cleanup review; record unresolved complexities before any XHigh escalation.

## Task 5: Single-window procedure, adjudication and static closure

**Files:** Runnable uninvoked bounded driver, collector/adjudicator/config templates and deterministic tests; executable attended request and identity lock; addenda to design/ledger without rewriting historical conclusions.

**Interfaces:** Static package lock binds exact source commit, host 27.0.1/26A434 compatibility, CLIs, Tart/stock/N1/diagnostic Softnet, helpers/configs/procedures/adjudicator/cleanup, destination and removal target, evidence bounds and archive. Runtime binding schema admits only UUID/session/backend/generation, leased addresses, short-lived public credential fingerprints/paths and process/runtime facts generated during the approved window.

- [ ] Implement strict static lock/runtime binding validation and state-machine fixtures for at most two fresh quarantine guests, no workspace/credentials/shares, one copy/read per guest, one connect interval, bracketing host positives, observation completion and exact cleanup. Record actual child argv bytes/counts and bound all worker output. Missing/unknown outcomes stop the affected phase without retry.
- [ ] Instantiate every obtainable static value, including base/provenance, configs/storage policy and durable retention. Read existing retained records only; do not mount/unlock or change failed evidence. Independently review runtime facts inside one future approved window, without a second authorization merely for generated IDs. Owner still performs attended installation and exact cleanup authentication.
- [ ] Adjudicator preserves historical TCP rules and separates new diagnostic attribution from qualification PASS; tests reject absent/overflowed observation, unsupported coverage and vmnet enqueue inference.

## Task 6: Final verification, reviews and publication gate

- [ ] Run full default/candidate/diagnostic Go tests and race suites, vet/build, all relevant Python/shell and Rust packet/observer tests, artifact reproduction, privacy/boundary and cleanup tests. Resolve failures with evidence, preserve failed-run records.
- [ ] Request fresh independent clipboard Sol/High, network/vmnet/ARP Sol/XHigh, admission/cleanup/artifact Sol/High or justified XHigh, and cumulative-delta Sol/XHigh reviews. Preserve disagreements; resolve all Critical/Important findings.
- [ ] Inspect cumulative diff against freshly fetched actual base, private-data scope and whitespace. Commit detailed coherent verified increments and push immediately; create/attach/maintain Draft PR after first meaningful push. Wait for green current-head CI and fix branch-caused failures.
- [ ] Record exact final artifact-source commit separately from later hash/documentation closeout commits to avoid a self-referential source-commit/hash cycle. Stop uninstalled with one concise fully concrete owner request; make no live qualification claim.
