# Axiom Insight

Axiom Insight is building a code-driven observability compiler. Phase 0
establishes the Go compiler foundation and package boundaries.

## Requirements

- Go 1.26.1 or compatible newer Go release
- Protocol Buffers compiler 29.3 or compatible newer release
- `protoc-gen-go` v1.36.6

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