# Makefile for github.com/sinfulspartan/go-pug
#
# Requires GNU Make.  On Windows, Git Bash must be installed so that
# sh.exe is available at C:/Program Files/Git/usr/bin/sh.exe.
# On Linux / macOS the system sh is used automatically.
#
# Usage:
#   make              # default: vet + test + build
#   make help         # list all targets

# ---------------------------------------------------------------------------
# Shell — must be set before any rules so every recipe runs under POSIX sh.
# GNU Make uses SHELL + .SHELLFLAGS to invoke each recipe line.
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
  SHELL     := C:/Program Files/Git/usr/bin/sh.exe
else
  SHELL     := /bin/sh
endif
.SHELLFLAGS := -c

# ---------------------------------------------------------------------------
# Variables
# ---------------------------------------------------------------------------
MODULE   := github.com/sinfulspartan/go-pug
PKG      := ./pkg/gopug
CMD      := ./cmd
GO       := go
BIN_DIR  := bin
BINARY   := $(BIN_DIR)/go-pug

# -- Test binary left behind by -cpuprofile / -memprofile ---------------------
# go test writes a compiled test binary named after the package when profiling.
# On Windows it gets a .exe suffix; on POSIX it has no suffix.
ifeq ($(OS),Windows_NT)
  TEST_BIN := gopug.test.exe
else
  TEST_BIN := gopug.test
endif

# -- Benchmark tunables -------------------------------------------------------
# Override on the command line:  make bench BENCH=BenchmarkRender BENCHTIME=2s
BENCH      ?= .
BENCHTIME  ?= 1s
BENCHCOUNT ?= 1

# -- Coverage tunables --------------------------------------------------------
COVER_OUT  ?= coverage.out
COVER_HTML ?= coverage.html

# -- Benchmark report tunables ------------------------------------------------
BENCH_MD   ?= BENCHMARKS.md
BENCH_JSON ?= benchmarks.json
BENCH_CSV  ?= benchmarks.csv
BENCH2MD   := ./scripts/bench2md

# -- Tooling detection --------------------------------------------------------
# which is available inside Git Bash sh on Windows and on all POSIX systems.
GOLANGCI_LINT := $(shell which golangci-lint 2>/dev/null)

# ---------------------------------------------------------------------------
# Phony targets
# ---------------------------------------------------------------------------
.PHONY: all help build run \
        test test-v test-race \
        bench bench-short bench-cpu bench-mem bench-report bench-json bench-csv \
        cover cover-html \
        fmt vet lint \
        tidy mod \
        clean

.DEFAULT_GOAL := all

# ---------------------------------------------------------------------------
# Default
# ---------------------------------------------------------------------------

all: vet test build

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

help:
	@echo ""
	@echo "  go-pug -- Makefile targets"
	@echo "  -----------------------------------------------------------------"
	@echo "  Build"
	@echo "    build          Build the CLI binary to $(BINARY)"
	@echo "    run            Build and run the demo binary"
	@echo ""
	@echo "  Test"
	@echo "    test           Run the full test suite"
	@echo "    test-v         Run tests in verbose mode"
	@echo "    test-race      Run tests with the race detector"
	@echo ""
	@echo "  Benchmarks"
	@echo "    bench          Run all benchmarks  (BENCH=. BENCHTIME=1s) + $(BENCH_MD)"
	@echo "    bench-short    Run benchmarks with -benchtime=100ms (quick check) + $(BENCH_MD)"
	@echo "    bench-cpu      Run benchmarks with CPU profiling  -> cpu.prof + $(BENCH_MD)"
	@echo "    bench-mem      Run benchmarks with memory profiling -> mem.prof + $(BENCH_MD)"
	@echo "    bench-report   Run benchmarks and write $(BENCH_MD) (requires Go)"
	@echo "    bench-json     Run benchmarks and write benchmarks.json (machine-readable)"
	@echo "    bench-csv      Run benchmarks and write benchmarks.csv (spreadsheet-friendly)"
	@echo ""
	@echo "  Coverage"
	@echo "    cover          Generate coverage report (text + $(COVER_OUT))"
	@echo "    cover-html     Open an HTML coverage report in your browser"
	@echo ""
	@echo "  Code quality"
	@echo "    fmt            Format source files with gofmt -s"
	@echo "    vet            Run go vet"
	@echo "    lint           Run golangci-lint (if installed)"
	@echo ""
	@echo "  Dependencies"
	@echo "    tidy           Run go mod tidy"
	@echo "    mod            Run go mod download"
	@echo ""
	@echo "  Housekeeping"
	@echo "    clean          Remove $(BIN_DIR)/, $(COVER_OUT), $(COVER_HTML), *.prof, $(BENCH_MD)"
	@echo "    help           Show this message"
	@echo ""
	@echo "  Tunable variables (pass on the command line):"
	@echo "    BENCH=<regex>        benchmark filter          (default: .)"
	@echo "    BENCHTIME=<dur>      time per benchmark        (default: 1s)"
	@echo "    BENCHCOUNT=<n>       repetitions per benchmark (default: 1)"
	@echo "    COVER_OUT=<file>     coverage profile output   (default: coverage.out)"
	@echo "    COVER_HTML=<file>    coverage HTML output      (default: coverage.html)"
	@echo "    BENCH_MD=<file>      benchmark report output   (default: BENCHMARKS.md)"
	@echo "    BENCH_JSON=<file>    benchmark JSON output     (default: benchmarks.json)"
	@echo "    BENCH_CSV=<file>     benchmark CSV output      (default: benchmarks.csv)"
	@echo ""

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

build:
	@echo "=> Building $(BINARY)"
	@mkdir -p $(BIN_DIR) && $(GO) build -v -o $(BINARY) $(CMD)
	@echo "-> $(BINARY) ready"
	@echo ""

run: build
	@echo "=> Running demo"
	$(BINARY)

# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------

test:
	@echo "=> Running tests ($(PKG))"
	$(GO) test -count=1 $(PKG)

test-v:
	@echo "=> Running tests -- verbose ($(PKG))"
	$(GO) test -count=1 -v $(PKG)

test-race:
	@echo "=> Running tests with race detector ($(PKG))"
	$(GO) test -count=1 -race $(PKG)

# ---------------------------------------------------------------------------
# Benchmarks
# ---------------------------------------------------------------------------

# _BENCH_TMP is a scratch file used to capture raw benchmark output so it can
# be both displayed to the terminal and fed to bench2md in a single run.
# Using a temp file avoids process-substitution (bash-only) and keeps
# recipes POSIX-sh compatible.
_BENCH_TMP := bench_raw.txt

bench:
	@echo "=> Benchmarks  BENCH=$(BENCH)  BENCHTIME=$(BENCHTIME)  COUNT=$(BENCHCOUNT)"
	$(GO) test -count=$(BENCHCOUNT) \
	           -run "^$$" \
	           -bench "$(BENCH)" \
	           -benchtime $(BENCHTIME) \
	           -benchmem \
	           $(PKG) | tee $(_BENCH_TMP) ; \
	$(GO) run $(BENCH2MD) -o $(BENCH_MD) < $(_BENCH_TMP) ; \
	rm -f $(_BENCH_TMP)
	@echo "-> $(BENCH_MD) written"

bench-short:
	@echo "=> Benchmarks (short -- 100ms each)"
	$(GO) test -count=1 \
	           -run "^$$" \
	           -bench "$(BENCH)" \
	           -benchtime 100ms \
	           -benchmem \
	           $(PKG) | tee $(_BENCH_TMP) ; \
	$(GO) run $(BENCH2MD) -o $(BENCH_MD) < $(_BENCH_TMP) ; \
	rm -f $(_BENCH_TMP)
	@echo "-> $(BENCH_MD) written"

bench-cpu:
	@echo "=> Benchmarks with CPU profiling -> cpu.prof"
	$(GO) test -count=1 \
	           -run "^$$" \
	           -bench "$(BENCH)" \
	           -benchtime $(BENCHTIME) \
	           -benchmem \
	           -cpuprofile cpu.prof \
	           $(PKG) | tee $(_BENCH_TMP) ; \
	$(GO) run $(BENCH2MD) -o $(BENCH_MD) < $(_BENCH_TMP) ; \
	rm -f $(_BENCH_TMP) $(TEST_BIN)
	@echo "-> cpu.prof written  (inspect with: go tool pprof cpu.prof)"
	@echo "-> $(BENCH_MD) written"

bench-mem:
	@echo "=> Benchmarks with memory profiling -> mem.prof"
	$(GO) test -count=1 \
	           -run "^$$" \
	           -bench "$(BENCH)" \
	           -benchtime $(BENCHTIME) \
	           -benchmem \
	           -memprofile mem.prof \
	           $(PKG) | tee $(_BENCH_TMP) ; \
	$(GO) run $(BENCH2MD) -o $(BENCH_MD) < $(_BENCH_TMP) ; \
	rm -f $(_BENCH_TMP) $(TEST_BIN)
	@echo "-> mem.prof written  (inspect with: go tool pprof mem.prof)"
	@echo "-> $(BENCH_MD) written"

bench-report:
	@echo "=> Running benchmarks and writing $(BENCH_MD)"
	$(GO) test -count=$(BENCHCOUNT) \
	           -run "^$$" \
	           -bench "$(BENCH)" \
	           -benchtime $(BENCHTIME) \
	           -benchmem \
	           $(PKG) \
	| $(GO) run $(BENCH2MD) -format md -o $(BENCH_MD)
	@echo "-> $(BENCH_MD) written"

bench-json:
	@echo "=> Benchmarks -> $(BENCH_JSON)  (JSON format)"
	$(GO) test -count=$(BENCHCOUNT) \
	           -run "^$$" \
	           -bench "$(BENCH)" \
	           -benchtime $(BENCHTIME) \
	           -benchmem \
	           $(PKG) \
	| $(GO) run $(BENCH2MD) -format json -o $(BENCH_JSON)
	@echo "-> $(BENCH_JSON) written"

bench-csv:
	@echo "=> Benchmarks -> $(BENCH_CSV)  (CSV format)"
	$(GO) test -count=$(BENCHCOUNT) \
	           -run "^$$" \
	           -bench "$(BENCH)" \
	           -benchtime $(BENCHTIME) \
	           -benchmem \
	           $(PKG) \
	| $(GO) run $(BENCH2MD) -format csv -o $(BENCH_CSV)
	@echo "-> $(BENCH_CSV) written"

# ---------------------------------------------------------------------------
# Coverage
# ---------------------------------------------------------------------------

cover:
	@echo "=> Generating coverage profile ($(COVER_OUT))"
	$(GO) test -count=1 -coverprofile=$(COVER_OUT) -covermode=atomic $(PKG)
	@echo ""
	$(GO) tool cover -func=$(COVER_OUT)

cover-html: cover
	@echo "=> Generating HTML coverage report ($(COVER_HTML))"
	$(GO) tool cover -html=$(COVER_OUT) -o $(COVER_HTML)
	@echo "-> Opening $(COVER_HTML)"
	@open "$(COVER_HTML)" 2>/dev/null \
	  || xdg-open "$(COVER_HTML)" 2>/dev/null \
	  || start "$(COVER_HTML)" 2>/dev/null \
	  || echo "   Open $(COVER_HTML) in your browser"

# ---------------------------------------------------------------------------
# Code quality
# ---------------------------------------------------------------------------

fmt:
	@echo "=> Formatting source files"
	$(GO) fmt ./...
	gofmt -s -l -w .

vet:
	@echo "=> go vet"
	$(GO) vet ./...

lint:
ifneq ($(GOLANGCI_LINT),)
	@echo "=> golangci-lint run"
	$(GOLANGCI_LINT) run ./...
else
	@echo "=> golangci-lint not found -- skipping"
	@echo "   Install: https://golangci-lint.run/usage/install/"
endif

# ---------------------------------------------------------------------------
# Dependencies
# ---------------------------------------------------------------------------

tidy:
	@echo "=> go mod tidy"
	$(GO) mod tidy

mod:
	@echo "=> go mod download"
	$(GO) mod download

# ---------------------------------------------------------------------------
# Housekeeping
# ---------------------------------------------------------------------------

clean:
	@echo "=> Cleaning build artifacts"
	-rm -rf $(BIN_DIR) ; rm -f $(COVER_OUT) $(COVER_HTML) cpu.prof mem.prof $(BENCH_MD) $(BENCH_JSON) $(BENCH_CSV) $(_BENCH_TMP) $(TEST_BIN) bench2md bench2md.exe
	@echo "-> Done"
