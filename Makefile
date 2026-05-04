.DEFAULT_GOAL := test

GO ?= go
GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo $(HOME)/go/bin/golangci-lint)
INSTALL_DIR ?= $(HOME)/.local/bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)
GOFILES := $(shell find . -type f -name '*.go' -not -path './runtime/*' | sort)

.PHONY: build quickstart install-local uninstall-local dist install-smoke fmt fmt-check vet test test-race test-e2e test-fuzz-smoke bench lint script-check policy-check policy-sign-coverage-check vuln ship-check clean

build:
	mkdir -p bin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate ./cmd/loopgate
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-doctor ./cmd/loopgate-doctor
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-ledger ./cmd/loopgate-ledger
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-policy-admin ./cmd/loopgate-policy-admin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/loopgate-policy-sign ./cmd/loopgate-policy-sign

quickstart: build
	@printf 'Running Loopgate quickstart with the recommended defaults.\n'
	@printf 'This writes a signed starter policy, installs Claude hooks, and on macOS installs and loads a LaunchAgent.\n'
	./bin/loopgate quickstart

install-local: build
	mkdir -p "$(INSTALL_DIR)"
	cp bin/loopgate "$(INSTALL_DIR)/loopgate"
	cp bin/loopgate-doctor "$(INSTALL_DIR)/loopgate-doctor"
	cp bin/loopgate-ledger "$(INSTALL_DIR)/loopgate-ledger"
	cp bin/loopgate-policy-admin "$(INSTALL_DIR)/loopgate-policy-admin"
	cp bin/loopgate-policy-sign "$(INSTALL_DIR)/loopgate-policy-sign"
	chmod 755 "$(INSTALL_DIR)/loopgate" "$(INSTALL_DIR)/loopgate-doctor" "$(INSTALL_DIR)/loopgate-ledger" "$(INSTALL_DIR)/loopgate-policy-admin" "$(INSTALL_DIR)/loopgate-policy-sign"
	@printf 'Installed Loopgate binaries to %s\n' "$(INSTALL_DIR)"
	@printf 'If needed, add %s to PATH.\n' "$(INSTALL_DIR)"

uninstall-local:
	rm -f "$(INSTALL_DIR)/loopgate"
	rm -f "$(INSTALL_DIR)/loopgate-doctor"
	rm -f "$(INSTALL_DIR)/loopgate-ledger"
	rm -f "$(INSTALL_DIR)/loopgate-policy-admin"
	rm -f "$(INSTALL_DIR)/loopgate-policy-sign"

dist:
	./scripts/package_release.sh

install-smoke:
	./scripts/install_smoke_test.sh

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

bench:
	$(GO) test -run=^$$ -bench=. -benchmem ./internal/loopgate ./internal/ledger

lint:
	$(GOLANGCI_LINT) run

script-check:
	bash -n scripts/govulncheck.sh
	bash -n scripts/policy_sign_coverage_check.sh
	bash -n scripts/package_release.sh
	bash -n scripts/install.sh
	bash -n scripts/install_smoke_test.sh

policy-check:
	$(GO) run ./cmd/loopgate-policy-admin validate -repo .

policy-sign-coverage-check:
	./scripts/policy_sign_coverage_check.sh

vuln:
	./scripts/govulncheck.sh

ship-check: fmt-check vet test-race lint script-check policy-check policy-sign-coverage-check vuln

clean:
	rm -rf bin
