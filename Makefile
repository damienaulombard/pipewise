VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test cover cover-eval lint fmt tidy-check check

build:
	go build -ldflags "$(LDFLAGS)" -o bin/pipewise ./cmd/pipewise

test:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

# internal/eval decides every verdict: it stays at 100 % statement coverage.
cover-eval:
	go test -coverprofile=coverage-eval.out ./internal/eval/
	@go tool cover -func=coverage-eval.out | awk '/^total:/ { print "internal/eval coverage: " $$3; if ($$3 != "100.0%") exit 1 }'

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

# Fails when go.mod or go.sum is not what `go mod tidy` would write.
tidy-check:
	go mod tidy -diff

check: tidy-check lint test cover-eval
