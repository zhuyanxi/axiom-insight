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