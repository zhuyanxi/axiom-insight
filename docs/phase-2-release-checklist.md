# Phase 2 Release Checklist (v0.3.0)

Run from repository root with Go `1.26.x`, `protoc`, the pinned protobuf Go
generators, and a prepared module cache. Execute sign-off steps on a clean
checkout. Network access is not part of any Phase 2 gate.

## 0. Prerequisites

- `go version` reports the release toolchain.
- `protoc`, `protoc-gen-go`, and `protoc-gen-go-grpc` are installed at the
  versions used by the Phase 1 workflow.
- `git status --short` is empty before and after the gates.
- Go module dependencies have been downloaded before entering offline mode.

## 1. Quality Gates

```sh
make phase2-quality
```

This command runs the Dashboard unit and CLI tests, report contract tests,
Goldens, Grafana Schema 41 compatibility, race, offline tests, and the
opt-in-enforced Dashboard performance gate. Any failure returns non-zero and
stops the release check.

The full repository release gate additionally runs the Phase 1 quality gate
first (including generated-code checks that require a clean tree and the
pinned protoc toolchain):

```sh
make release-quality
```

## 2. Offline Reproducibility

```sh
GOPROXY=off GOSUMDB=off make phase2-quality
```

The command must complete without network access. Dashboard production code
does not read environment variables; these variables only prevent dependency
resolution during the gate.

## 3. Dashboard Contract and Goldens

```sh
make dashboard-contract-test
make dashboard-golden-test
make dashboard-compat-test
go test ./dashboard/pipeline -run 'TestP213CompatibilityCorpus|TestP214CanonicalOutput' -count=1
```

The output contract is `grafana.dashboard/v1` with Grafana Schema `41`.
Committed Dashboard Goldens live under
`testdata/dashboard/v1/golden/`; the local compatibility corpus lives under
`testdata/dashboard/corpus/`. Golden updates require explicit
`SI_UPDATE_GOLDEN=1` and review of the semantic diff.

## 4. Determinism and Security

```sh
go test ./dashboard/pipeline -run 'TestP213DeterminismAndPermutations|TestP213SensitiveCanaryFullChain' -count=1
go test ./dashboard/... ./cmd/si-cli -run 'Canary' -count=1
```

The same IR, policy, CLI version, and template version must produce identical
JSON bytes, SHA-256, UID, panel IDs, query order, LF endings, and diagnostic
order across 10 repeated runs and 25 fixed input permutations. Synthetic
sensitive canaries must not enter Dashboard JSON, reports, stdout, stderr, or
errors.

## 5. Cross-Platform Verification

On macOS, Linux, and Windows, with the same prepared module cache:

```sh
go build ./...
go test ./dashboard/... ./cmd/si-cli -count=1
GOPROXY=off GOSUMDB=off go test ./dashboard/pipeline -run 'TestP214CanonicalOutput' -count=1
```

Compare the SHA-256 of the same committed composite output on all three
platforms. Expected invariants: canonical JSON is UTF-8 with two-space
indentation and one LF terminator, schema validation has the same result, and
the CLI success/failure exit codes are unchanged. Windows runners must use the
repository's `sh`-compatible command environment or equivalent environment
variable syntax.

## 6. Performance Baseline

```sh
make dashboard-perf
```

`BenchmarkP214Dashboard1000` reports catalog construction, query planning and
layout, render/validate, and combined Dashboard work for exactly 1,000 Catalog
Items. The enforced reference budget for combined catalog + plan/layout +
render/validate is under `100ms` and under `64 MiB` added allocation, with an
approved `20%` tolerance. Reference measurement (Apple M4, go1.26.1,
`go test ./dashboard/pipeline -run '^$' -bench BenchmarkP214Dashboard1000/catalog-plan-render -benchmem -count=3`):
≈ `29ms` / `32 MiB` per op. Enforcement is explicit:

```sh
SI_ENFORCE_PERF_BUDGET=1 go test ./dashboard/pipeline -run TestP214DashboardPerformanceBudget1000 -count=1 -v
```

`BenchmarkP214DashboardScanToWrite` separately reports CLI source scan through
Dashboard write time and allocations. It is observational evidence, not part
of the 1,000-item Dashboard budget because it includes Go analyzer and
filesystem costs.

## 7. Release Artifact and Hash

```sh
go build -o bin/si ./cmd/si-cli
bin/si dashboard --version
bin/si dashboard testdata/fixtures/http --dry-run --format json
bin/si dashboard testdata/fixtures/http --output-dir /tmp/phase2-rc --force --format json
shasum -a 256 /tmp/phase2-rc/dashboard.json
```

Record the report fields `sha256`, `panel_count`, `query_count`, `row_count`,
and the output hash below. `dashboard.json` must decode and validate against
`grafana.dashboard/v1` and Schema `41`.

| Artifact | SHA-256 | Executor | Result |
| --- | --- | --- | --- |
| `dashboard.json` | | | |

## 8. Known Limitations

- Dashboard is an offline Grafana definition; it is not deployed or imported.
- No PromQL or TraceQL execution occurs, and runtime data is not validated.
- Phase 2 does not generate SLOs, SLIs, alerts, runbooks, collector config,
  or runtime thresholds.
- Arbitrary Grafana plugins, templates, HTML, JavaScript, URLs, and user
  PromQL are unsupported.
- Kafka consumer handler root spans, lag, partition, and offset panels are
  unsupported.
- Raw SQL, Redis keys/values, URLs, request data, payloads, and remote identity
  values are excluded from Dashboard output.
- File submission guarantees single-file atomic replacement only. It does not
  guarantee a crash-atomic multi-step transaction after process kill, kernel
  failure, or power loss.

## 9. Sign-off

| Check | Executor | Result (pass/fail) | Evidence / notes |
| --- | --- | --- | --- |
| Phase 2 quality gates | | | |
| Offline gates | | | |
| Contract and Schema 41 compatibility | | | |
| Golden and determinism | | | |
| Security canary | | | |
| macOS/Linux/Windows comparison | | | |
| Performance budget | | | |
| Release artifact and hash | | | |
| Known limitations acknowledged | | | |