# Dashboard CLI Contract (P2-11/P2-12)

`si dashboard` converts one analyzed source tree into one deterministic,
validated Grafana Dashboard JSON file. Generation is local and offline. The
command never imports, deploys, or provisions a dashboard in Grafana.

## Command

```sh
bin/si dashboard [path]
```

When `path` is omitted, the current directory is used. The default output is
`<source-root>/dashboards/dashboard.json`. Only this managed file is written.
The optional `dashboard` section in `si.yaml` supplies policy values; CLI
`--output-dir` and `--strict` override the corresponding policy fields.

Supported flags:

| Flag | Meaning |
| --- | --- |
| `--config` | Use an explicit `si.yaml` path. |
| `--output-dir` | Override dashboard policy output directory. |
| `--include` / `--exclude` | Select or omit package patterns during analysis. |
| `--include-tests` | Include Go test files during analysis. |
| `--strict` | Fail on any Dashboard warning before writing. |
| `--dry-run` | Run and validate the complete pipeline without filesystem output. |
| `--force` | Replace an existing managed `dashboard.json`. |
| `--format text\|json` | Select short human output or one `cli.dashboard_report/v1` report. |
| `--version` | Print CLI, IR, Generator, Dashboard, and Grafana schema versions. |

Pipeline order is fixed:

```text
Analyze -> Phase 1 Plan -> Dashboard Catalog -> Query/Panel Plan
        -> Render/Validate -> Commit
```

Every fatal failure stops before commit. Dashboard production does not read
AST data, query a telemetry backend, or use environment values to alter the
result.

## Safe Commit

The writer applies these checks and operations:

1. Create or validate the output directory as a real directory.
2. Acquire an exclusive `.si-dashboard.lock` in that directory.
3. Recheck the target under the lock. Refuse existing targets without
   `--force`.
4. Reject symlink, directory, device, FIFO, socket, and hard-linked
   `dashboard.json` targets.
5. Write bytes to an exclusive `.si-dashboard-tmp-dashboard.json` file.
6. Sync the temporary file, close it, atomically rename it to `dashboard.json`,
   and sync the output directory.
7. Remove temporary and lock files on success or failure.

`--dry-run` ends before directory creation, so it creates no output directory,
lock, temporary file, or target. Existing files remain unchanged on refusal or
validation failure.

## Output And Limits

Text output is a short status line containing file name, counts, and SHA-256.
It never prints the complete dashboard JSON. JSON output contains exactly one
`cli.dashboard_report/v1` object after flags are accepted. The report schema is
[`schemas/dashboard/v1/cli-dashboard-report.schema.json`](../schemas/dashboard/v1/cli-dashboard-report.schema.json).

Report fields include:

- schema versions, service, completed stage, and `dry_run`;
- dashboard name, policy digest, SHA-256, panel/query/row counts, and
   `existed_before` when planning reached validation;
- `written` and `diagnostics`, always encoded as arrays, including empty arrays;
- diagnostics sorted by severity, category, target ID, code, field, then
   message;
- a short, stage/code-only error summary on failure. Complete dashboard JSON,
   source paths, raw query data, and rejected values never enter the report.

Report status is `success` for a clean run, `warning` for non-strict output
with dashboard warnings, and `failure` for strict warnings or any pipeline
failure. JSON reports are bounded at 256 KiB; an oversized report becomes a
small `CLI_INTERNAL_ERROR` failure report rather than being emitted without a
contract. JSON encoding failures never fall back to human text.

Exit codes:

- `0`: generated successfully, or completed non-strict with warnings.
- `1`: scan, plan, catalog, render, validation, strict-warning, or commit failure.
- `2`: invalid path, argument, format, or configuration.

Atomic rename protects the managed file from truncated writes. It is not a
cross-process crash transaction: forced termination, kernel failure, or power
loss can leave either the complete old file or the complete new file.
