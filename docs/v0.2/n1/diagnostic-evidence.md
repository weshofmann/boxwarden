# N1 follow-up diagnostic evidence inventory

This note records a source-only review of the retained macOS 27 follow-up evidence. The source baseline reviewed was `99b3a8c04c56ddc4292a9dd515c3e2abcb77d5c6`. PR #16 is merged at final head `ec6f5e1bc0fee4fdff7d5c3de3b57daf3522fcf9`. This inventory does not change any qualification verdict.

The live observations below are from the bounded trial on 2026-09-29 (macOS 27.0.1, build 26A434). The macOS 26 observations remain as recorded in the [acceptance matrix](matrix.md); the macOS 27 follow-up did not replace or reinterpret them.

## Clipboard observations

The stock-control receipt records one acknowledged synthetic copy and one exact 31-byte readback. The readback SHA-256 is `ba898e1ade98d69956328d6c2c76a8fcf168471701707a6c619c0e7ba0469980`. Its verdict is **PASS** for that tested path.

The candidate receipt records an acknowledged copy (`rc=0`) followed by one paste attempt (`rc=1`) with the diagnostic `clipboard text unavailable` and no output. The receipt verdict is **UNQUALIFIED**. There was no repeated write or read. A read-only preflight and post-stage inspection found the reviewed helper files with their expected digests and root ownership/mode, and a responsive desktop. A later read-only diagnostic also observed a live clipboard worker. Those observations establish capability and process presence at inspection time; they do not establish native clipboard ownership, content, or delivery, and they do not identify which layer caused the read failure. The generic CLI error does not establish N1 causation.

## Cross-guest TCP observations

One candidate-to-stock management TCP attempt was bracketed by successful, same-generation strict host positives to the stock SSH endpoint. Candidate readiness and management, gateway DNS, and public HTTPS controls were healthy. The candidate attempt returned `OSError: [Errno 113] No route to host`; it did not connect and did not time out.

Passive route and neighbor metadata showed an on-link route toward the peer and a `FAILED` neighbor entry. This is consistent with peer ARP denial, but it does not prove that ARP policy caused the socket error. The retained classifier reports that the candidate error was not a timeout and leaves the row **UNQUALIFIED**. No retry, route, neighbor, guest listener, or firewall change was used to seek a different result. This evidence does not justify treating the attempt as a timeout or upgrading it to a denial **PASS**.

## Report revision and retained evidence

The private report r1 remains preserved with SHA-256 `7b26411614ba59f794547230f85a68ed7788a22291b0c6b65dc685f91f3650d0`. Report r2, SHA-256 `ee52e014f6fc4c4748047ccdc6bb4c30447768d60c82d8c3177853f8614b7375`, supersedes r1 as the final runtime checkpoint. R1 was written before the post-retirement doctor invocation was inspected. R2 adds that the invocation refused its retired trial configuration before host checks; a separate diagnostic-only configuration then reported stock doctor healthy. The refusal is preserved as a configuration refusal after deliberate state retirement, not relabeled as a host failure or a new qualification result. Neither report overwrites the other.

The following basenames and SHA-256 values identify the reviewed archived reports and receipts. They are listed for evidence integrity and retrieval; this document intentionally omits their private storage locations and runtime-specific identifiers.

| Archived report or receipt | SHA-256 |
|---|---|
| `qualification-report-r1.md` | `7b26411614ba59f794547230f85a68ed7788a22291b0c6b65dc685f91f3650d0` |
| `qualification-report-r2.md` | `ee52e014f6fc4c4748047ccdc6bb4c30447768d60c82d8c3177853f8614b7375` |
| `candidate-clipboard-r1.json` | `8dfd4a00d98992e829fdab76b9f0ea3a320825fc8f74ddfb461fd2ed6cc3e6d0` |
| `control-clipboard-r1.json` | `b4229b40380fef0dae18efe3503ab9a01c05fff015fbb0b5d7f342ff536154b7` |
| `candidate-readonly-inspection-preclipboard-r1.json` | `bd2cff27f28ee631ac6374f3c17d3579f56f0e4fd6db7425236fbd5ebb4a61ea` |
| `candidate-readonly-inspection-poststage-r1.json` | `a42faf297718be113ced08c9a479bdfa5239e412a1ec5cc3a7f7aaac7a6853be` |
| `candidate-readonly-diagnostic-r1.json` | `41276d97c6fc322dfc470baa2d45896314e5940a1145a62da2400302b67d9978` |
| `cross-guest-interval-r1.json` | `4bab4927d15e5c38d06da518f71619565449549245af3fb0e276d7a880f0a4c5` |
| `cross-guest-verdict-r1.json` | `63e5ba1f18f4b7e7f7c1c20f7a71b1ffc795005048d434d7fcba1a2ec363659c` |
| `cross-guest-classifier-input-r1.json` | `a7ff681ef0158d78fde1a387222de7dd5e3864c201c445c3d9bf8cf5c5ca7227` |
| `runtime-evidence-manifest-r2.json` | `68405b3633238dafc60b89a512c8492e641fb1ddcb92818f2b9618a0a8b1ef25` |

## Scope and limits

This was a read-only source and retained-report/receipt inventory. It did not access VM disk state or the encrypted qualification storage, and it did not repeat a live operation. The two candidate observations remain **UNQUALIFIED**; the stock synthetic clipboard control remains **PASS**. The reviewed evidence does not determine the clipboard failure layer or prove the cause of errno 113. No other row or historical verdict is changed.
