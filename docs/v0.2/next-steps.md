# After the v0.2 prototype

Owner-approved closeout and follow-up scope, updated 2026-09-28.

## Baseline and interpretation

PR #12 is accepted as the experimental development baseline, not a public
release or a certification of complete host containment. The delivered
prototype/handoff checkpoint is `36f4f60542fc155d31c7210a80ae2c7f9ab01d4d`;
its tested local build is recorded at `ca0d69c`. Read the exact build and
artifact bindings in the existing handoff rather than replacing them just
because the repository was merged. The closeout changes documentation only.

[Progress](alpha-progress.md), [reviews](alpha-review-ledger.md), and
[operator instructions](alpha-quickstart.md) remain the evidence sources.
Keep their historical observations intact. The completed autonomous mission
must not be restarted merely because older plans still contain pending gates.

## Follow-ups

| ID | Work | Completion evidence |
| --- | --- | --- |
| C1 | Controlled clipboard transfer | Merged PR #13 provides a standalone menu-bar utility and CLI; Wes observed explicit transfer in both directions and Quit preserving the running sandbox. Provider login remains untested. |
| R1 | Workspace clean-remount identity recovery | A retained workspace survives a clean backing-filesystem remount without reformatting or blind rebinding; missing/wrong storage refuses. Physical reconnect and corruption remain outside this claim. |
| N1 | Guest-to-host vmnet-gateway exposure | A [reviewed candidate](n1/design.md) is in development, with [separate deterministic and attended gates](n1/matrix.md); retain the current explicit limitation until demonstrated. |
| A1 | Remaining interruption and user acceptance | Targeted snapshot-copy and other untested interruption/recovery cases, controlled native reboot/reconnect, authenticated provider use, and subjective GUI usability; evidence must name the tested case. |

C1 is merged at `ee3d7a2`; R1 is the current reliability assignment. N1 and
A1 remain separate work. Reboot, physical reconnect, credential entry, and
host-security changes still require their applicable operator authorization.

## C1: merged controlled clipboard

The approved frontend is a standalone `Boxwarden Clipboard.app` menu-bar
utility over the shared bounded transfer implementation and public
`boxwarden clipboard push|pull|copy|paste` CLI. Stock Tart is restored; its
automatic clipboard sharing remains disabled. The abandoned patched Tart
viewer is retained only in Git history and diagnostic records, not the
current build or host admission. See the
[clipboard design](../controlled-clipboard/design.md),
[progress](../controlled-clipboard/progress.md), and
[launch instructions](../../host/clipboard-menu/README.md).

Wes directly observed the menu, selected the retained synthetic sandbox,
transferred text in both directions, and chose Quit. Full-host checks then
found the menu process absent and the exact sandbox still consistent and
READY; the CLI still discovered its live generation. Firefox and LibreOffice
Writer pasted synthetic text through the guest/CLI path. Cancellation,
restarted-target behavior and size boundaries have automated coverage; those
edges were not all manually exercised in the menu. Provider sign-in and
subjective authenticated-app usability remain untested A1 actions.

## R1: current clean-remount recovery boundary

The alpha demonstrated a retained raw workspace becoming inadmissible after
the qualification filesystem was cleanly remounted with a different transient
device number. R1 must distinguish persistent host-storage and workspace
identity from observations valid only in one mount instance. It must preserve
the existing path, ownership, lease, backend and export safety checks while
rejecting absent/wrong storage and substituted files. New synthetic backing
images may be detached/remounted for testing; the existing user/demo volumes,
installed host toolchain and running clipboard-trial sandbox are excluded.
The focused design, progress and reviewed evidence belong to R1's branch and
Draft PR. A clean-remount result must not be described as proof of sudden disk
removal, filesystem corruption, reboot or physical reconnect recovery.
