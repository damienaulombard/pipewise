VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test cover lint fmt tidy-check check

build:
	go build -ldflags "$(LDFLAGS)" -o bin/pipewise ./cmd/pipewise

test:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

# Fails when go.mod or go.sum is not what `go mod tidy` would write.
tidy-check:
	go mod tidy -diff

check: tidy-check lint test
