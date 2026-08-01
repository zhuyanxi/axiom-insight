# Phase 0 Release Checklist

Run from repository root with Go 1.26.1 or newer, `protoc`, protobuf Go
generators, and `jq` installed. All commands below must pass before release.

## Quality Gates

```sh
go test ./...
go vet ./...
go test ./cmd/si-cli -run TestScanFixtures -count=1
go test -race ./compiler/goanalyzer ./plugins ./cmd/si-cli
make check-generated
make perf
```

Configure the `Phase 0 quality` job from `.github/workflows/quality.yml` as a
required pull-request status check. Any failed check then blocks merging.

## Cross-Platform Builds

Build each release target without running the binaries on the host:

```sh
go build -o /tmp/si-darwin-arm64 ./cmd/si-cli
GOOS=linux GOARCH=amd64 go build -o /tmp/si-linux-amd64 ./cmd/si-cli
GOOS=windows GOARCH=amd64 go build -o /tmp/si-windows-amd64.exe ./cmd/si-cli
```

Record CLI version and IR schema version from the host binary:

```sh
/tmp/si-darwin-arm64 scan --version
```

Expected output contains `si version: v0.1.0` and
`ir_schema_version: v1`, unless release version values were intentionally
updated.

## Offline Scan And JSON

Use a fixture with no network or external service:

```sh
GOPROXY=off GOSUMDB=off /tmp/si-darwin-arm64 scan testdata/fixtures/http --format json >/tmp/si-scan.json
jq -e '.schema_version == "v1" and (.document != null) and (.diagnostics != null)' /tmp/si-scan.json >/dev/null
```

Run all persistent fixture scans:

```sh
make fixture-test
```

Check malformed input keeps a parse diagnostic and succeeds with non-fatal
diagnostics:

```sh
go test ./cmd/si-cli -run 'TestScanFixtures/malformed$' -count=1
```

## Schema Compatibility

Run IR round-trip and plugin handshake tests:

```sh
go test ./ir/v1 ./plugins
```

Verify generated protobuf files are clean after regeneration:

```sh
make check-generated
```

## Performance Baseline

Run benchmark with allocation reporting:

```sh
go test ./cmd/si-cli -run '^$' -bench BenchmarkScanSmallFixture -benchmem -count=1
```

Current regression budget for one HTTP fixture scan is less than `5s` and
`768 MiB` allocated per operation. Treat benchmark values as environment-aware
baseline data; update budget only with an intentional performance review.

## Sign-Off

- [ ] Quality workflow passed on pull request.
- [ ] Cross-platform binaries built.
- [ ] Offline JSON scan parsed and schema version verified.
- [ ] All fixtures passed, including malformed input.
- [ ] Protobuf generation produced no diff.
- [ ] Performance output recorded and reviewed.