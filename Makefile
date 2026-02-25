SHELL := /bin/bash

GO_FILES := $(shell find cmd internal -type f -name '*.go')

.PHONY: help fmt fmt-check vet test test-race lint staticcheck architecture-check coverage-check quality

help:
	@echo "Targets:"
	@echo "  fmt              - Format Go files with gofmt"
	@echo "  fmt-check        - Fail if files are not gofmt formatted"
	@echo "  vet              - Run go vet"
	@echo "  test             - Run go test"
	@echo "  test-race        - Run go test -race"
	@echo "  staticcheck      - Run staticcheck (requires binary installed)"
	@echo "  lint             - Run golangci-lint (requires binary installed)"
	@echo "  architecture-check - Validate architecture guardrails"
	@echo "  coverage-check   - Validate feature coverage targets"
	@echo "  quality          - Run fmt-check + vet + test-race + architecture-check + coverage-check"

fmt:
	@gofmt -w $(GO_FILES)

fmt-check:
	@test -z "$$(gofmt -l $(GO_FILES))" || (echo "Run 'make fmt' to format files." && gofmt -l $(GO_FILES) && exit 1)

vet:
	@go vet ./...

test:
	@go test ./...

test-race:
	@go test -race ./...

staticcheck:
	@command -v staticcheck >/dev/null 2>&1 || (echo "staticcheck not found. Install: go install honnef.co/go/tools/cmd/staticcheck@latest" && exit 1)
	@staticcheck ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint not found. Install: https://golangci-lint.run/welcome/install/" && exit 1)
	@golangci-lint run

architecture-check:
	@./scripts/check-architecture.sh

coverage-check:
	@./scripts/check-feature-coverage.sh

quality: fmt-check vet test-race architecture-check coverage-check
