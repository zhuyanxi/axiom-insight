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

dashboard-test:
	go test ./dashboard/... ./cmd/si-cli -run 'P213|Dashboard' -count=1

dashboard-contract-test:
	go test ./dashboard/model ./dashboard/pipeline -run 'Contract|Corpus|P213' -count=1

dashboard-golden-test:
	go test ./dashboard/... ./cmd/si-cli -run 'Golden|P213DashboardGoldens' -count=1

dashboard-compat-test:
	go test ./dashboard/model ./dashboard/pipeline -run 'Corpus|Compatibility' -count=1

dashboard-race:
	go test -race ./dashboard/... ./cmd/si-cli -run 'P213|Dashboard' -count=1

phase2-quality:
	$(MAKE) build
	$(MAKE) lint
	$(MAKE) dashboard-test
	$(MAKE) dashboard-contract-test
	$(MAKE) dashboard-golden-test
	$(MAKE) dashboard-compat-test
	$(MAKE) dashboard-race
	@echo "phase2-quality: all checks passed"
