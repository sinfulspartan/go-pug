# Makefile - common targets for the go-pug module
#
# Usage:
#   make            # runs 'all' (test then build)
#   make build      # build the example binary into bin/
#   make test       # run unit tests
#   make fmt        # run gofmt and go fmt
#   make vet        # run go vet
#   make lint       # run golangci-lint if installed
#   make install    # go install ./...
#   make tidy       # go mod tidy
#   make clean      # remove build artifacts
#   make help       # show this help
#
# Note: This Makefile assumes a POSIX shell (sh). On Windows use an environment
# that provides these utilities (Git Bash, WSL, etc.) or run commands manually.

MODULE := github.com/sinfulspartan/go-pug
GO := go
BIN_DIR := bin
CMD := ./cmd/example

# Detect golangci-lint if available
GOLANGCI_LINT := $(shell command -v golangci-lint 2>/dev/null || true)

.DEFAULT_GOAL := all

.PHONY: all help build test fmt vet lint install tidy mod clean

all: test build

help:
	@echo "Makefile for $(MODULE)"
	@echo
	@echo "Available targets:"
	@echo "  all      - run tests and build"
	@echo "  build    - build example binary to $(BIN_DIR)/"
	@echo "  test     - run go tests"
	@echo "  fmt      - run gofmt and go fmt"
	@echo "  vet      - run go vet"
	@echo "  lint     - run golangci-lint (if installed)"
	@echo "  install  - go install ./..."
	@echo "  tidy     - go mod tidy"
	@echo "  mod      - download modules (go mod download)"
	@echo "  clean    - remove build artifacts"
	@echo "  help     - show this message"

build:
	@echo "=> Building example binary"
	@mkdir -p $(BIN_DIR)
	$(GO) build -v -o $(BIN_DIR)/example $(CMD)
	@echo "-> built $(BIN_DIR)/example"

test:
	@echo "=> Running tests"
	$(GO) test ./...

fmt:
	@echo "=> Formatting code"
	$(GO) fmt ./...
	# gofmt -s writes changes in-place; list files changed with -l
	@gofmt -s -l -w .

vet:
	@echo "=> Running go vet"
	$(GO) vet ./...

lint:
ifneq ($(GOLANGCI_LINT),)
	@echo "=> Running golangci-lint"
	$(GOLANGCI_LINT) run ./...
else
	@echo "=> golangci-lint not found; skipping lint. Install: https://golangci-lint.run/"
endif

install:
	@echo "=> go install ./..."
	$(GO) install ./...

tidy:
	@echo "=> go mod tidy"
	$(GO) mod tidy

mod:
	@echo "=> go mod download"
	$(GO) mod download

clean:
	@echo "=> Cleaning"
	@rm -rf $(BIN_DIR)
	@echo "Removed $(BIN_DIR) directory (if it existed)."
	@echo "Done."
