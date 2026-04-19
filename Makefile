.DEFAULT_GOAL := test

GO ?= go
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo $(HOME)/go/bin/golangci-lint)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)
GOFILES := $(shell find . -type f -name '*.go' -not -path './runtime/*' | sort)

.PHONY: build fmt fmt-check vet test test-race test-e2e test-fuzz-smoke lint policy-check policy-sign-coverage-check vuln ship-check clean

build:
	mkdir -p bin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate ./cmd/loopgate
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-doctor ./cmd/loopgate-doctor
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-ledger ./cmd/loopgate-ledger
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-policy-admin ./cmd/loopgate-policy-admin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-policy-sign ./cmd/loopgate-policy-sign

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

test-e2e:
	$(GO) test -tags=e2e ./internal/integration -run TestE2EApprovalWriteAuditFlow -count=1

test-fuzz-smoke:
	$(GO) test ./internal/config -run=^$$ -fuzz=FuzzParsePolicyDocument -fuzztime=5s
	$(GO) test ./internal/loopgate -run=^$$ -fuzz=FuzzDecodeJSONBytesCapabilityRequest -fuzztime=5s

lint:
	$(GOLANGCI_LINT) run

policy-check:
	$(GO) run ./cmd/loopgate-policy-admin validate -repo .

policy-sign-coverage-check:
	./scripts/policy_sign_coverage_check.sh

vuln:
	./scripts/govulncheck.sh

ship-check: fmt-check vet test-race lint policy-check policy-sign-coverage-check vuln

clean:
	rm -rf bin
