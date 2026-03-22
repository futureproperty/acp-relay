.PHONY: build test vet lint

build:
	go build -o bin/acp-remote ./cmd/acp-remote/

test:
	go test ./... -v -race

vet:
	go vet ./...

lint:
	golangci-lint run ./...
