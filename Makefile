.PHONY: build test vet lint

build:
	go build -o bin/acp-relay ./cmd/acp-relay/

test:
	go test ./... -v -race

vet:
	go vet ./...

lint:
	golangci-lint run ./...
