// lint runs the project's static-analysis suite using only tools that ship
// with Go or are already on PATH — no shell-specific detection commands
// (where / which) are required.
//
// Steps:
//  1. go vet ./...          — always runs; part of the standard Go toolchain
//  2. golangci-lint run ./... — runs only when the binary is found on PATH
//     via exec.LookPath; silently skipped otherwise
//
// Usage (via Makefile):
//
//	go run ./scripts/lint
//
// Flags:
//
//	-vet-only   skip golangci-lint even if it is installed
//	-strict     exit 1 when golangci-lint is not found (useful in CI)
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	vetOnly := flag.Bool("vet-only", false, "run go vet only; skip golangci-lint")
	strict := flag.Bool("strict", false, "exit 1 if golangci-lint is not installed")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/lint [-vet-only] [-strict]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Runs go vet ./..., then golangci-lint run ./... if available.")
		flag.PrintDefaults()
	}
	flag.Parse()

	// ── Step 1: go vet ────────────────────────────────────────────────────
	fmt.Println("=> go vet ./...")
	if err := run("go", "vet", "./..."); err != nil {
		fmt.Fprintf(os.Stderr, "lint: go vet failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("-> go vet passed")
	fmt.Println()

	if *vetOnly {
		fmt.Println("-> -vet-only set; skipping golangci-lint")
		return
	}

	// ── Step 2: golangci-lint (optional) ──────────────────────────────────
	lintBin, err := exec.LookPath("golangci-lint")
	if err != nil {
		// Not found on PATH.
		if *strict {
			fmt.Fprintln(os.Stderr, "lint: golangci-lint not found on PATH (-strict mode)")
			fmt.Fprintln(os.Stderr, "      Install: https://golangci-lint.run/usage/install/")
			os.Exit(1)
		}
		fmt.Println("=> golangci-lint not found on PATH — skipping")
		fmt.Println("   Install: https://golangci-lint.run/usage/install/")
		return
	}

	fmt.Printf("=> golangci-lint run ./...  (%s)\n", lintBin)
	if err := run(lintBin, "run", "./..."); err != nil {
		fmt.Fprintf(os.Stderr, "lint: golangci-lint failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("-> golangci-lint passed")
}

// run executes name with args, wiring stdin/stdout/stderr to the current
// process so the child's output streams through unchanged.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
