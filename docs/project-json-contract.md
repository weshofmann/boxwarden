# Project CLI JSONL contract

Status: implemented version 1 presentation contract for the experimental,
alpha-domain project workflow. This interface adds no lifecycle or transfer
engine. Existing storage, bookmark, generation, lock, and receipt admission
remains authoritative.

Use the per-command `--json` flag before positional arguments:

```sh
boxwarden --config /absolute/config.json --domain alpha project list --json
boxwarden --config /absolute/config.json --domain alpha project create --json --recipe desktop --size-mib 512 example
boxwarden --config /absolute/config.json --domain alpha project import preview --json --source /private/source --exclude .git
boxwarden --config /absolute/config.json --domain alpha project import --json --source /private/source --exclude .git --expected-digest SHA256 example
boxwarden --config /absolute/config.json --domain alpha project export --json --destination /private/new-destination example
```

The flag is also available on `setup`, `setup-update`, `open`, `status`, `stop`,
`rebuild`, `rebuild retry`, `import retry`, `export list`, and `export retry`.
Retry flags and selection constraints otherwise remain unchanged. The ordinary
text interface is unchanged when `--json` is absent.

## Stream and completion

Stdout contains one JSON object per line, with no mixed prose:

```json
{"version":1,"type":"progress","operation":"project.create","message":"Formatting the managed workspace"}
{"version":1,"type":"error","operation":"project.create","message":"admission failure","data":{"uncertain":true}}
```

`type` is `progress`, `result`, or `error`. `operation` is the dotted command
path, for example `project.list`, `project.import.preview`, or
`project.export.retry`. `message` and `data` are omitted when unused. Progress
messages describe phases entered by the real implementation; existing human
output may also be wrapped as progress. Progress prose is for display only and
must never be parsed to infer state or success. Progress message chunks are at
most 4096 bytes before JSON encoding. The complete stream is capped at 8 MiB, including a 64 KiB reserve for a
terminal error. Oversized results fail with an error instead of emitting a
partial result. Terminal data also retains the existing bounded registry,
journal, and import-manifest limits.

A normal completed invocation emits one terminal `result` or `error`. A
successful exit **and** a valid version 1 `result` are required for client
success. Missing/truncated/malformed streams, unsupported versions, nonzero
exit, and `error` events are uncertain outcomes. Never retry effects
implicitly. Inspect refreshed state and any retained import/export/rebuild
intent. Parser, configuration, domain, and storage failures use the same error
envelope when JSON mode can be identified. Stderr may still contain CLI
failure diagnostics; it is not part of this contract.

Results come from typed project records or admitted driver receipts. A failed
optional observation after an admitted successful operation is represented by
an unavailable project snapshot; it does not retroactively fail the operation.
As with text commands, successful create/open process completion does not
promise READY: a later fresh readiness observation is authoritative.

## Result data

`project.list` returns `{setup, projects}`. `setup` has `status` (`ready`,
`missing`, `invalid`, or `unverified`) and `guidance`. Even an empty registry
reports missing or invalid setup. Configuration/storage admission failure is
an error, never an empty successful list. Setup commands return `{setup}`.

Create/open/status/stop/import/rebuild, including import/rebuild retries,
return `{project}`. Each project has these fields:

| Field | Meaning |
| --- | --- |
| `name`, `base`, `session_id`, `backend_object` | Remembered exact project identity; allocation-only projects have empty system identity |
| `state`, `diagnostic` | Reconciled snapshot, such as `stopped`, `running`, `READY`, `incomplete`, `unavailable`, or `rebuild pending` |
| `observed_state`, `backend_running` | Exact observer evidence (`unknown` without valid observation); never management readiness |
| `management_ready` | True only after fresh exact supervisor generation checks |
| `workspace` | `{id, filesystem_uuid, size_bytes, mount_path, initialized}` |
| `software` | `{intent_digest, status, actions}` |
| `import` | `{status, id, guest_path}`, with status `not_imported`, `pending`, or `matched` |
| `replacement_pending` | Retained exact system replacement needs explicit recovery |
| `available_actions` | Safe next-action hints, re-admitted when invoked |

Software status is `legacy_unverified`, `unavailable`, `unknown`, `blocked`,
`pending`, or `complete`. Complete requires current management READY and the
exact current automatic action plan. Saved action success does not establish
management readiness. Actions are stored attempt observations with
`{action_id, phase, state, generation}`; history may refer to previous
start generations. Do not describe it as current guest evidence.

Action hints include `open`, `status`, `stop`, `rebuild`, `import`,
`import_retry`, `export`, `rebuild_retry`, and `inspect_session`. They are
navigation hints rather than authority: for example, export still requires an
exact stopped sandbox and a fresh private destination.

`project.import.preview` returns
`{source, entries, file_count, directory_count, total_bytes, digest, exclusions}`.
Entries have `path`, `kind`, and optional `size`/`sha256`. This is the actual
read-only host selection and digest; no project or backend is needed.

`project.export` and `project.export.retry` return
`{transaction, phase, published, project_files}` from the admitted export
receipt. Recovery that safely aborts a partial copy returns phase `aborted`
and empty published/project-files paths, so it must not be shown as a completed
file export.

`project.export.list` returns `{project_name, exports}`. Entries contain
`{transaction, phase, destination_parent, published, project_files,
matches_current_bookmark, retry_available}`. These are retained journal facts,
not proof of current filesystem contents or successful future recovery.
Historical mismatches cannot offer retry. Retry never overwrites published
exports and independently re-admits the exact current stopped binding.

Error `data` currently contains `{uncertain:true}`. The human-readable message
may identify retained intent or a recovery command. It is never a structured
success receipt or permission for automatic retry.
