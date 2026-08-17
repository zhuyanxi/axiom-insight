.PHONY: build test lint generate check-generated fixture-test race perf quality
.PHONY: dashboard-test dashboard-contract-test dashboard-golden-test dashboard-compat-test dashboard-race phase2-quality

build:
	go build ./...

test:
	go test ./...

lint:
	go vet ./...

check-generated:
	$(MAKE) generate
	git diff --exit-code -- ir/v1/*.pb.go

fixture-test:
	go test ./cmd/si-cli -run TestScanFixtures -count=1

race:
	go test -race ./compiler/goanalyzer ./plugins ./cmd/si-cli

perf:
	go test ./cmd/si-cli -run TestScanSmallFixturePerformanceBudget -count=1
	go test ./cmd/si-cli -run '^$$' -bench BenchmarkScanSmallFixture -benchmem -count=1

quality: test lint check-generated fixture-test race perf

generate:
	PATH="$(HOME)/go/bin:$(PATH)" protoc \
		--proto_path=. \
		--go_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		ir/v1/observability.proto \
		ir/v1/generation.proto \
		ir/v1/language_analyzer.proto
# --- Phase 1 quality gates (P1-16) --------------------------------------

.PHONY: generator-test generator-contract-test generator-golden-test generator-race generator-perf phase1-quality phase1-schema-compat

generator-test:
	go test ./generator/... ./ir/... ./cmd/... -count=1

generator-contract-test:
	go test ./generator/ -run 'Contract|Documentation' -count=1

generator-golden-test:
	go test ./generator/ ./generator/e2e/ -run 'Golden' -count=1

generator-race:
	go test -race ./generator/... ./cmd/... -count=1

generator-perf:
	go test ./generator/ ./generator/e2e/ -run '^$$' -bench 'Benchmark(Plan|Render|GenerateFromIR)' -benchmem -count=1
	go test ./cmd/si-cli -run '^$$' -bench BenchmarkScanAndGenerateComposite -benchmem -count=1

# Schema compatibility: every committed Golden must validate against its
# v1 schema, and regeneration must not dirty the tree.
phase1-schema-compat:
	go test ./generator/e2e/ -run 'Schema' -count=1
	$(MAKE) check-generated
	git diff --exit-code -- schemas/ testdata/generator/golden/

# Single quality entry point: any failing step aborts with non-zero.
# Deliberately not chained with && so make reports the failing target.
phase1-quality:
	$(MAKE) build
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) fixture-test
	$(MAKE) generator-test
	$(MAKE) generator-contract-test
	$(MAKE) generator-golden-test
	$(MAKE) generator-race
	$(MAKE) phase1-schema-compat
	$(MAKE) generator-perf
	@echo "phase1-quality: all checks passed"

# --- Phase 2 quality gates (P2-13) --------------------------------------

# Full dashboard suite (unit, golden, contract, canary, P2-13): no -run
# filter, so every dashboard and CLI test runs in the phase2 gate.
dashboard-test:
	go test ./dashboard/... ./cmd/si-cli -count=1

# Report contract and schema-state rules only (cmd/si-cli package).
dashboard-contract-test:
	go test ./cmd/si-cli -run '^TestDashboardReport(Contract|SchemaStateRules)$$' -count=1

# Every committed dashboard/CLI golden snapshot test.
dashboard-golden-test:
	go test ./dashboard/model ./dashboard/pipeline ./cmd/si-cli -run '^Test.*Golden$$' -count=1

# Grafana Schema 41 compatibility corpus (model + pipeline).
dashboard-compat-test:
	go test ./dashboard/model ./dashboard/pipeline -run '^Test(CorpusAC4|P213CompatibilityCorpus)$$' -count=1

dashboard-race:
	go test -race ./dashboard/... ./cmd/si-cli -count=1

phase2-quality:
	$(MAKE) build
	$(MAKE) lint
	$(MAKE) dashboard-test
	$(MAKE) dashboard-contract-test
	$(MAKE) dashboard-golden-test
	$(MAKE) dashboard-compat-test
	$(MAKE) dashboard-race
	@echo "phase2-quality: all checks passed"
