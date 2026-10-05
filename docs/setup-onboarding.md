# Native setup inspection and preparation

Status: implemented CLI integration seam for the experimental alpha project
manager. This is onboarding information, not host/VM qualification evidence.

`boxwarden [--config /absolute/config.json] setup inspect --json` is read-only and
works before normal configuration loading succeeds. Omit `--config` to inspect
`DefaultConfigPath()`. It does not accept a domain override or use the environment
domain: this seam selects alpha explicitly. A completed inspection exits zero
with one JSON object, including blocked setup states. Invalid argv, cancellation,
and output errors fail the process.

The object has `version: 1`, `scope: "alpha_project_setup"`, `config_path`,
`status`, `config_valid`, `selection_acceptable`, `guidance`, and `next_actions`
(stable action identifiers). `diagnostic` is optional secondary detail and must
never be parsed to classify a result. Domain identity failure diagnostics are
not serialized. `setup_version` is present when an admitted setup is ready;
`recipe_preparation_available` is true only for its version-2 recipe inputs.

| Status | Meaning |
| --- | --- |
| `config_missing` | The selected config file is absent. |
| `config_location_inadmissible` | The locator is noncanonical, has symlink ancestry, is not a regular direct file, cannot be inspected, or failed config storage/ownership admission (including being on the workspace backing filesystem). |
| `config_invalid` | Config admission failed, or alpha is absent. |
| `workspace_storage_unavailable` | Enrolled state root is absent, enrollment is absent, or exact storage admission failed. |
| `host_tools_uninitialized` | Host inputs are absent or the typed host doctor reports missing installation. |
| `host_tools_incompatible` | Host prerequisite admission or the typed host doctor rejected compatibility/safety. |
| `domain_uninitialized` | The existing complete configured-domain CA check returned `ErrCAMissing`. |
| `domain_incompatible` | Complete configured-domain identity admission failed. |
| `project_setup_missing` | No saved project setup exists. |
| `project_setup_invalid` | Saved setup or its remembered assets failed existing admission. |
| `ready` | Config, alpha storage, host doctor, configured-domain CA, and project assets passed their existing checks. |

`config_valid` becomes true only after `config.Load` succeeds. It can remain true
for downstream blockers. `selection_acceptable` becomes true only after the
selected alpha domain and its enrolled storage pass admission; it stays true
when host installation, domain initialization, or asset setup is incomplete.
A config file whose declared filesystem inputs are unavailable is not admitted
by this seam. No failed check silently adopts or repairs paths.

`ready` does not attest any live VM, prepared base, installed guest software,
clipboard feature, or management READY. Empty project lists are supported. A
legacy setup may be admitted and ready while `recipe_preparation_available` is
false: the UI must require an explicit setup-update for recipe workflows.
The ordinary operation repeats all of its normal admission checks.

Explicit preparation uses:

```text
boxwarden --config /absolute/config.json setup prepare --json \
  --package /absolute/extracted-package \
  --iso /absolute/ubuntu.iso --checker /absolute/e2fsck-static.deb \
  --go /absolute/tool/go --zstd /absolute/tool/zstd \
  --openssl /absolute/tool/openssl --xorriso /absolute/tool/xorriso
```

This emits the existing project-event envelope (`version: 1`, `type`,
`operation: "setup.prepare"`, optional `message`, optional `data`). Progress is
actual bounded stdout/stderr from the package helper and has no machine meaning.
A result contains the complete typed inspection. Error data contains `uncertain`
and becomes true once domain initialization or the helper may have changed
state. Retained state must be inspected before retrying a failed preparation.

Preparation admits the direct operator-owned package and clean source tree;
its source HEAD must equal the invoking CLI's compiled revision. The package
CLI SHA-256 must match the invoking executable, and the root helper must match
the clean source's helper bytes. Inputs must be direct absolute regular files;
the four tools must be actual named executable files. A development CLI with
unknown compiled revision cannot authorize a package helper.

Only `domain_uninitialized` and `project_setup_missing` are accepted as
preparation starting states. Existing or invalid saved project setup is refused,
including a saved profile in an uninitialized domain. No implicit setup-update
is attempted. If needed, it explicitly invokes the existing public alpha domain
initializer; the helper then prepares formatter assets and records project
setup using its existing admissions. It does not install/repair host tools,
change config, or start a VM. It derives package source paths internally, so
native onboarding does not require the user to locate a worktree.

The helper owns a process group through the existing mutex/Wait4 runner. Actual
merged progress is bounded to 8 MiB; writer errors cancel the owned group.
Cancellation and the 30-minute helper timeout kill only the owned process group;
a direct-child reap or output-drain uncertainty remains an error. This is a
trusted operator action: package ownership and source binding do not provide
cryptographic provenance against another process with the operator's authority.
No real host installation or VM qualification is implied by synthetic adapter
tests.
