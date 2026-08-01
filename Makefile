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
		--go-grpc_out=. \
		--go-grpc_opt=paths=source_relative \
		ir/v1/observability.proto \
		ir/v1/language_analyzer.proto