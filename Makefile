.PHONY: build test lint generate check-generated fixture-test race perf quality

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