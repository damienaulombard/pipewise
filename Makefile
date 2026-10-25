VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test cover cover-eval lint fmt tidy-check check dist

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

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# One archive per platform, with the license and the README, and their
# checksums in dist/SHA256SUMS. The release workflow runs exactly this.
dist:
	rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		if [ "$$os" = windows ]; then ext=.exe; fi; \
		name=pipewise_$(VERSION)_$${os}_$${arch}; \
		echo "build $$name"; \
		mkdir -p dist/$$name || exit 1; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/$$name/pipewise$$ext ./cmd/pipewise || exit 1; \
		cp LICENSE README.md dist/$$name/ || exit 1; \
		if [ "$$os" = windows ]; then \
			(cd dist && zip -qr $$name.zip $$name) || exit 1; \
		else \
			tar -C dist -czf dist/$$name.tar.gz $$name || exit 1; \
		fi; \
		rm -rf dist/$$name; \
	done
	cd dist && shasum -a 256 *.tar.gz *.zip > SHA256SUMS
