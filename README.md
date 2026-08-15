# Axiom Insight

Axiom Insight is building a code-driven observability compiler. Phase 0
establishes the Go compiler foundation and package boundaries.

## Requirements

- Go 1.26.1 or compatible newer Go release
- Protocol Buffers compiler 29.3 or compatible newer release
- `protoc-gen-go` v1.36.6
- `protoc-gen-go-grpc` v1.5.1

## Local Development

```sh
make build
make test
make lint
make generate
```

Install the Go Protobuf generator before `make generate`:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
```

Build the CLI directly:

```sh
go build -o bin/si ./cmd/si-cli
```

The compiler packages currently provide the foundation for the Go analyzer,
semantic model, IR, and plugin layers. Feature work is tracked in
`docs/03-phase-0-jira-stories.md`.

## Endpoint Adapter Scope

The initial endpoint adapters recognize:

- Go `net/http` `Handle`, `HandleFunc`, and `ServeMux` methods. Go 1.22+ method/path patterns are preserved.
- `github.com/gorilla/mux` v1 `Handle`, `HandleFunc`, and chained `Methods` calls.
- Generated-style gRPC `Register*Server` functions when server implementation methods resolve locally.
- `github.com/robfig/cron` v1 and v3 `AddFunc` and `AddJob` calls.

Typed `Handle` or `HandleFunc` calls from unsupported router packages produce
`UNSUPPORTED_HTTP_ROUTER` diagnostics and no guessed endpoint. Custom adapters
can be passed through `goanalyzer.Options.EndpointAdapters`.

## Dependency Adapter Scope

The initial dependency rules recognize:

- `database/sql` `DB`, `Tx`, `Stmt`, and `Conn` query, execution, preparation, and transaction methods.
- `github.com/redis/go-redis/v9` `Client`, `Ring`, and `ClusterClient` cache methods.
- `github.com/IBM/sarama` and `github.com/Shopify/sarama` producer, consumer, and consumer group calls.

Static SQL, Redis keys, Kafka topics, and consumer groups are recorded as values.
Dynamic values are retained as dependency instances with `ValueIsStatic=false`
and an analyzer diagnostic.

## External Client Scope

The initial external client rules recognize:

- Standard library `net/http` package-level `Get`/`Post`, `http.Client` `Do`/`Get`/`Post`, and static request data from `NewRequest` assignments.
- Generated-style external gRPC client methods with a `*Client` or `Client` receiver, `context.Context` input, and `error` result. Static targets from local `grpc.Dial` bindings are stored in `TargetService`.

Dynamic HTTP URLs, HTTP methods, and gRPC targets remain dependencies with empty
unknown fields and analyzer diagnostics. Server registration calls are not
classified as client dependencies.

## Scan Summary

`semantic.Summarize` derives fixed-order counts for HTTP handlers, gRPC
handlers, Cron jobs, Kafka consumers, Kafka producers, SQL, Redis, HTTP
clients, RPC clients, and diagnostics. `goanalyzer.AnalyzeSummary` returns this
summary with the semantic document; CLI layers should consume it instead of
revisiting Go AST data.

## Plugin Protocol

`ir/v1/language_analyzer.proto` defines `LanguageAnalyzer.GetMetadata` and
`LanguageAnalyzer.Analyze`. Metadata reports language, plugin version,
capabilities, and supported IR schema range. Current Go frontend supports
schema `v1` only.

`AnalyzeRequest` carries local `source_root`, package `include`/`exclude`
patterns, inline YAML `config`, schema version, and `include_tests`. Response
contains `ObservabilityDocument`; top-level `diagnostics` mirrors document
diagnostics for transport consumers. Diagnostics remain non-fatal when
analysis can continue.

CLI integration uses `plugins.NewInProcessTransport(nil)` by default; the same
`Analyzer` contract can move behind gRPC or HashiCorp go-plugin transport.
No network or external process is required for in-process scans.

Protocol errors use gRPC status codes: `InvalidArgument` for malformed request
or unusable source root, `FailedPrecondition` for incompatible schema version,
and `Internal` for semantic-to-IR conversion failure. Schema mismatch is
checked before analysis.

## CLI Scan

Build scanner with:

```sh
go build -o bin/si ./cmd/si-cli
```

Run offline scan with current directory or explicit path:

```sh
bin/si scan
bin/si scan ./path/to/service
bin/si scan ./path/to/service --format json
bin/si scan ./path/to/service --format json --output scan.json
bin/si scan --version
```

Default output lists service, schema version, and every fixed summary category.
JSON output contains `schema_version`, ordered `summary`, full `document`, and
top-level `diagnostics`. `--output` is valid only with `--format json`; without
it, scan does not create files.

Optional `si.yaml` is read from scan root. Supported keys include
`service.name` or `service_name`, `language`/`languages`, `include_tests`,
`include`, `exclude`, and `framework_adapters`. Current language is `go`;
supported adapters are `net/http`, `github.com/gorilla/mux`, `grpc`, and
`github.com/robfig/cron`.

Exit codes: `0` successful scan, including non-fatal diagnostics; `1` scan or
output failure; `2` invalid path, arguments, format, or configuration. Module
resolution uses `GOPROXY=off` and `GOSUMDB=off`; scanner makes no network request.

Version output includes CLI and IR schema versions:

```text
si version: v0.1.0
ir_schema_version: v1
```

CLI failures use stable message codes at the start of stderr output:

- `CLI_INVALID_ARGUMENT`: invalid path, flag, format, or configuration; exit `2`.
- `CLI_SCAN_ERROR`: scan or output failure; exit `1`.
- `CLI_INTERNAL_ERROR`: unexpected command failure; exit `1`.

IR diagnostics use stable codes such as `GO_PARSE_ERROR`,
`PACKAGE_LOAD_ERROR`, `INVALID_CONFIG`, and `UNRESOLVED_CALL`. Diagnostics are
preserved in JSON output and do not fail a scan when analysis can continue.

## CLI Generate

Generate versioned observability files from analyzed source. The pipeline is
Analyze -> Plan -> Render -> Validate -> Commit; every stage is offline and
deterministic.

```sh
bin/si generate
bin/si generate ./path/to/service
bin/si generate ./path/to/service --signals metrics,tracing
bin/si generate ./path/to/service --dry-run
bin/si generate ./path/to/service --strict
bin/si generate ./path/to/service --force
bin/si generate ./path/to/service --format json
bin/si generate --version
```

Defaults: path is the current directory, output is `<source-root>/generate`,
and all three signals (`metrics.yaml`, `otel.yaml`, `logging.yaml`) are
generated. `--signals` selects a subset; unselected files are never created,
overwritten or deleted. `--format` controls the command report only; the YAML
files are identical in both formats.

Safety guarantees:

- All selected renderers must succeed and re-validate in memory before any
  file is written; a failure writes nothing.
- Existing targets are refused without `--force` (`GEN_OUTPUT_EXISTS`).
- `--force` replaces only the selected managed targets; symlinked or
  non-regular targets are rejected (`GEN_UNSAFE_TARGET`) and never followed.
- Each file is written to an exclusive temporary file, synced, then renamed
  atomically; a multi-file journal backs up pre-existing targets and rolls
  back on failure.
- `--dry-run` performs the full scan/plan/render/validate pipeline and prints
  planned files, definition counts and SHA-256 hashes without creating the
  output directory, locks, or any file.
- A per-directory lock makes concurrent generations fail fast.

Crash-atomicity limitation: the commit guarantees single-file atomic
replacement and in-process rollback only. If the process is killed by force,
the kernel crashes, or power is lost mid-commit, a mixed set of complete old
and new files may remain — never a truncated renamed target.

Exit codes: `0` success; `1` scan, plan, render, validate, or commit failure
(`CLI_GENERATE_ERROR`); `2` invalid path, arguments, format, or configuration.
With `--format json`, stdout carries exactly one `cli.generate_report/v1`
document (success or failure) and stderr stays empty.

## CLI Dashboard

Generate one offline Grafana dashboard definition from the analyzed source:

```sh
bin/si dashboard
bin/si dashboard ./path/to/service
bin/si dashboard ./path/to/service --output-dir dashboards --dry-run
bin/si dashboard ./path/to/service --strict --format json
bin/si dashboard --version
```

The default target is `<source-root>/dashboards/dashboard.json`. The command
runs the complete Analyze -> Phase 1 Plan -> Dashboard Catalog -> Query/Panel
Plan -> Render/Validate pipeline before committing anything. It does not
connect to Grafana, Prometheus, or any other remote service. Dashboard policy
is read from the optional `dashboard` node in `si.yaml`; CLI `--output-dir`
and `--strict` overrides take precedence.

`--dry-run` reports the deterministic SHA-256 and panel/query/row counts
without creating the output directory, lock, temporary file, or dashboard.
Existing `dashboard.json` is refused unless `--force` is supplied. Symlinks,
non-regular files, and hard-linked targets are always rejected. A write uses
an exclusive per-directory lock, exclusive temporary file, file and directory
sync, and atomic rename; failures clean up temporary and lock files.

`--format json` emits one controlled `cli.dashboard_report/v1` document on
stdout. Text mode prints only a short summary and never prints the full
dashboard JSON. `--strict` promotes dashboard warnings to exit code `1`
before any output write. Exit code `2` means invalid path, flag, format, or
configuration.

Crash limitation: atomic rename protects the single managed file from
truncation, but process termination, kernel failure, or power loss can still
leave either the complete old file or the complete new file. The command
does not import, deploy, or provision the dashboard in Grafana.

## Offline Fixture Tests

Persistent Phase 0 fixtures live under `testdata/fixtures/`. Each fixture is
an isolated Go module with local replacement stubs where third-party types are
needed, plus an `expected.json` semantic snapshot. Run one fixture or all
fixtures with:

```sh
go test ./cmd/si-cli -run 'TestScanFixtures/http$'
go test ./cmd/si-cli -run TestScanFixtures
```

Recognizer changes must include a positive fixture, a negative non-match case,
and snapshot updates. Fixture tests do not access network or external services.

## Phase 0 Quality

Run all local quality gates with:

```sh
make quality
```

This runs tests, vet, fixture E2E tests, race detection, protobuf generation
consistency, and the small-fixture performance budget. Release verification
steps are documented in [the Phase 0 release checklist](docs/phase-0-release-checklist.md).
## Phase 1 Output Contracts

`si generate` produces three versioned, strictly validated files into
`<source-root>/generate` by default:

| File | Contract | Contents |
| --- | --- | --- |
| `metrics.yaml` | `generator.metrics/v1` | Counter/Histogram/Gauge/Summary definitions, record triggers, value bindings, `service`/`operation`/`status` attributes |
| `otel.yaml` | `generator.otel/v1` | Root/Child span definitions, kinds, parent strategies and carriers, semconv `1.37.0` attributes, status mappings, controlled error events |
| `logging.yaml` | `generator.logging/v1` | Structured event families, conditions, severity, correlation fields, immutable redaction rules |

All three are **instrumentation plans**, never runtime configuration:
`otel.yaml` carries `plan_kind: instrumentation` and no Collector
(receivers/processors/exporters/pipelines) fields; no file contains
generated timestamps, fake request/trace/span IDs, or host information.

Configuration lives in the optional `generation` node of `si.yaml`
(see `docs/05-generation-config.md`); merge priority is
CLI flags > si.yaml > built-in defaults. Determinism is a contract:
identical inputs produce byte-identical files on any machine.

Known limitations (Phase 1):

- No source modification, auto-instrumentation or compile-time injection.
- No OpenTelemetry Collector configuration is produced or deployed.
- Kafka consumer handler roots are not inferred.
- Runtime telemetry is not verified; the files are plans for a future
  Runtime/codegen layer.
- File commit guarantees single-file atomic replacement and in-process
  multi-file rollback, not cross-file crash-atomicity.

Exit codes: `si generate` returns `0` success, `1` pipeline failure
(`CLI_GENERATE_ERROR`), `2` invalid path/arguments/format/configuration.

## Phase 1 Quality

```sh
make phase1-quality
```

Runs build, vet, all tests, Phase 0 fixture regression, Generator tests,
race, Proto/Schema regeneration consistency, Golden consistency, security
canary and performance budget checks (see `docs/phase-1-release-checklist.md`).
