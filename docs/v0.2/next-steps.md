# After the v0.2 prototype

Owner-approved closeout and next-feature scope, 2026-09-27.

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
| C1 | Controlled clipboard transfer and login usability | Explicit transfers through window buttons, application menu, and CLI; no unsolicited synchronization; actual guest-application copy/paste and an attended login trial. |
| R1 | Workspace remount/reconnect identity reconciliation | Retained workspace remains usable after an authorized remount/reconnect without reformatting or blindly rebinding to a different disk; wrong-volume cases refuse. |
| N1 | Guest-to-host vmnet-gateway exposure | Review and test a host-enforced approach while preserving required DNS/network compatibility; retain the current explicit limitation until demonstrated. |
| A1 | Remaining interruption and user acceptance | Targeted snapshot-copy and other untested interruption/recovery cases, controlled native reboot/reconnect, authenticated provider use, and subjective GUI usability; evidence must name the tested case. |

C1 is the next feature. R1 and N1 remain substantive reliability/containment
work, not cosmetic polish. None is completed by merging the prototype.
Reboot, physical reconnect, credential entry, and host-security changes still
require their applicable operator authorization. Do not bundle them into the
clipboard feature merely to clear this table.

## C1: controlled clipboard feature handoff

### Approved user-facing direction

Add a narrow, versioned Boxwarden integration patch to the pinned Tart viewer.
The host-owned strip stays visible alongside the guest display and identifies
its sandbox. Include explanatory text and these two buttons:

- `HOST -> GUEST`: copy the host clipboard into that guest's clipboard once.
- `GUEST -> HOST`: copy that guest's clipboard into the host clipboard once.

Add application-menu items for the same operations:

- `Copy Host Clipboard to Guest`
- `Copy Guest Clipboard to Host`

Menu actions target the active VM window; per-window buttons retain their own
VM target. Disable unavailable targets and do not deliver an old request to a
replacement/restarted sandbox. Preserve ordinary guest copy/paste shortcuts.
No operation automatically pastes, presses Enter, or submits a form.

Provide CLI equivalents using the same transfer implementation. Proposed
command names are `boxwarden clipboard push|pull|copy|paste <session>` with
the existing explicit domain/config selection. `push`/`pull` match the buttons;
`copy` consumes stdin and `paste` produces stdout, analogous to pbcopy/pbpaste.
Optional pbcopy/pbpaste aliases and exact syntax belong in the feature design.
These are proposed commands, not implemented features in the prototype.

### Implementation constraints and remaining choices

Keep Tart's automatic SPICE clipboard sharing disabled. Do not implement a
one-shot action by briefly enabling automatic sharing. Explicit host actions
are the authority to transfer; the guest gets no general host-clipboard API.

Evaluate the existing Tart exec/RPC facilities against Boxwarden's existing
management connection before introducing new transport machinery. Check the
exact pinned versions, relevant upstream fixes, graphical-session access,
and applicable source licenses. Do not assume a guest system service can
already read the active desktop clipboard.

Keep the viewer patch thin. Share behavior across UI, menu, and CLI rather
than maintaining separate clipboard implementations. Give a patched Tart
binary its own source/patch/build/signing identity and validation; it is not
the previously admitted upstream executable under the old digest. Do not
silently replace the installed toolchain or reinterpret its manifest.

A text-first implementation with bounded UTF-8 payloads is the recommended
initial scope. Finalize supported types, limits, empty-clipboard behavior,
CLI terminal-output behavior, and transport in the written feature design.
Do not silently claim image/rich-text support or alter text/newlines.
Clipboard payloads must not enter command arguments, recipes, logs, content
hash records, clipboard history, or persistent action journals. Failed reads,
unsupported/oversized data, stale targets, and cancelled transfers must not
clear or overwrite the destination clipboard. Secrets sent intentionally to
the guest become available to that guest; receiving text is not approval to
execute it on the host.

Verify both directions in the actual guest desktop with synthetic data,
including guest Firefox/application paste, Unicode and multiline text,
multiple VM targets, stopped/replaced targets, and malformed/oversized guest
responses. Verify that unrelated subsequent clipboard changes do not cross
without a new host action. A helper's zero exit alone is not GUI acceptance.

### Start the next session

Use a fresh Codex conversation and a focused branch from merged `main`, such
as `weshofmann/feature/controlled-clipboard`. Inspect the actual merged SHA and
current runtime state; do not reset the old worktree or recreate the full
prototype. Preserve the tested build, handoff, workspace data, and rollback.

The requested driver remains GPT-6 Sol / High, with a fresh independent
review for the transfer boundary and patched host executable. Record actual
model availability rather than claiming an unverified switch. Complete a short
written design and implementation plan for this feature before changing host
integration. Publish small coherent commits and a new Draft PR. Do not add more
features to PR #12, publish a release, recreate the cancelled continuation
automation, or infer permission to merge future PRs from the one-time approval
of #12.
