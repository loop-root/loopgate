.DEFAULT_GOAL := test

GO ?= go
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo $(HOME)/go/bin/golangci-lint)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)
GOFILES := $(shell find . -type f -name '*.go' -not -path './runtime/*' | sort)

.PHONY: build fmt fmt-check vet test test-race lint policy-check vuln ship-check clean

build:
	mkdir -p bin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate ./cmd/loopgate

fmt:
	gofmt -w $(GOFILES)

fmt-check:
	@unformatted="$$(gofmt -l $(GOFILES))"; \
	test -z "$$unformatted" || (printf '%s\n' "$$unformatted"; false)

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race -count=1 ./...

lint:
	$(GOLANGCI_LINT) run

policy-check:
	$(GO) run ./cmd/loopgate-policy-admin validate -repo .

vuln:
	./scripts/govulncheck.sh

ship-check: fmt-check vet test-race lint policy-check vuln

clean:
	rm -rf bin
