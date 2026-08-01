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