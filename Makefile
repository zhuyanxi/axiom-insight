.PHONY: build test lint generate

build:
	go build ./...

test:
	go test ./...

lint:
	go vet ./...

generate:
	@echo "No generated sources yet."