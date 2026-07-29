.PHONY: build test lint generate

build:
	go build ./...

test:
	go test ./...

lint:
	go vet ./...

generate:
	PATH="$(HOME)/go/bin:$(PATH)" protoc \
		--proto_path=. \
		--go_out=. \
		--go_opt=paths=source_relative \
		ir/v1/observability.proto