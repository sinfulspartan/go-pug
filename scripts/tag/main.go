// tag automates the release-tagging workflow for go-pug.
//
// It replaces the shell boolean chain in the Makefile's tag target with a
// portable Go implementation that works identically on Windows, macOS, and
// Linux without requiring anything beyond the Go toolchain and git.
//
// Steps performed:
//  1. Validate and normalise the version argument (prepend "v" if absent).
//  2. Verify the working tree is clean (no staged or unstaged changes).
//  3. Verify the tag does not already exist locally or on origin.
//  4. Run the full test suite — refuse to tag a broken state.
//  5. Create an annotated git tag.
//  6. Push the tag to origin.
//
// Usage (via Makefile):
//
//	go run ./scripts/tag -version 1.2.3
//	go run ./scripts/tag -version v1.2.3
//
// Or directly:
//
//	go run ./scripts/tag -version v0.2.0 [-remote origin] [-pkg ./pkg/gopug] [-dry-run]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	version := flag.String("version", "", "release version to tag (e.g. 1.2.3 or v1.2.3)")
	remote := flag.String("remote", "origin", "git remote to push the tag to")
	pkg := flag.String("pkg", "./pkg/gopug", "Go package pattern passed to go test")
	dryRun := flag.Bool("dry-run", false, "validate and test but do not create or push the tag")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/tag -version <semver> [-remote <remote>] [-pkg <pattern>] [-dry-run]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  go run ./scripts/tag -version 1.2.3")
		fmt.Fprintln(os.Stderr, "  go run ./scripts/tag -version v1.2.3 -dry-run")
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
	}
	flag.Parse()

	// ── 1. Validate and normalise the version ─────────────────────────────
	if *version == "" {
		fatalf("error: -version is required\n\nUsage: go run ./scripts/tag -version <semver>\n")
	}

	tag := *version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}

	if err := validateSemver(tag); err != nil {
		fatalf("error: %v\n", err)
	}

	fmt.Printf("=> Release tag: %s\n", tag)
	if *dryRun {
		fmt.Println("   (dry-run mode — no tag will be created or pushed)")
	}
	fmt.Println()

	// ── 2. Verify working tree is clean ───────────────────────────────────
	fmt.Println("=> Checking working tree...")
	if err := checkClean(); err != nil {
		fatalf("error: %v\n", err)
	}
	fmt.Println("-> Working tree is clean")
	fmt.Println()

	// ── 3. Verify tag does not already exist ──────────────────────────────
	fmt.Printf("=> Checking tag %s does not already exist...\n", tag)
	if err := checkTagAbsent(tag, *remote); err != nil {
		fatalf("error: %v\n", err)
	}
	fmt.Println("-> Tag is available")
	fmt.Println()

	// ── 4. Run tests ──────────────────────────────────────────────────────
	fmt.Printf("=> Running tests (%s)...\n", *pkg)
	if err := runTests(*pkg); err != nil {
		fatalf("error: tests failed — refusing to tag\n  %v\n", err)
	}
	fmt.Println("-> Tests passed")
	fmt.Println()

	if *dryRun {
		fmt.Printf("=> Dry-run complete — would create and push tag %s\n", tag)
		return
	}

	// ── 5. Create annotated tag ───────────────────────────────────────────
	fmt.Printf("=> Creating annotated tag %s...\n", tag)
	if err := git("tag", "-a", tag, "-m", "Release "+tag); err != nil {
		fatalf("error: failed to create tag: %v\n", err)
	}
	fmt.Printf("-> Tag %s created\n", tag)
	fmt.Println()

	// ── 6. Push tag to remote ─────────────────────────────────────────────
	fmt.Printf("=> Pushing tag %s to %s...\n", tag, *remote)
	if err := git("push", *remote, tag); err != nil {
		// The tag was created locally but the push failed.  Print a clear
		// recovery hint before exiting so the user knows what to do.
		fmt.Fprintf(os.Stderr, "\nerror: push failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nThe tag was created locally but not pushed.\n")
		fmt.Fprintf(os.Stderr, "To retry the push:  git push %s %s\n", *remote, tag)
		fmt.Fprintf(os.Stderr, "To delete the local tag and start over:  git tag -d %s\n", tag)
		os.Exit(1)
	}
	fmt.Printf("-> Tag %s pushed to %s\n", tag, *remote)
	fmt.Println()
	fmt.Printf("=> Done — the Release workflow will create the GitHub Release for %s\n", tag)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// validateSemver checks that tag (already prefixed with "v") looks like a
// valid semantic version: vMAJOR.MINOR.PATCH with optional pre-release and
// build metadata. We validate the required vX.Y.Z prefix only — full semver
// parsing is not needed here.
func validateSemver(tag string) error {
	// Strip the leading "v".
	rest := strings.TrimPrefix(tag, "v")
	if rest == "" {
		return fmt.Errorf("version %q is empty after stripping 'v'", tag)
	}

	// Strip optional pre-release (+build) suffixes to validate the core.
	core := rest
	if idx := strings.IndexAny(core, "-+"); idx >= 0 {
		core = core[:idx]
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return fmt.Errorf("version %q must be in vMAJOR.MINOR.PATCH form", tag)
	}
	for _, p := range parts {
		if p == "" {
			return fmt.Errorf("version %q has an empty numeric component", tag)
		}
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return fmt.Errorf("version %q contains non-numeric component %q", tag, p)
			}
		}
	}
	return nil
}

// checkClean returns an error if the working tree has any staged or unstaged
// changes. It runs `git status --porcelain` and checks for non-empty output.
func checkClean() error {
	out, err := gitOutput("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("uncommitted changes detected — commit or stash them first:\n%s", strings.TrimRight(out, "\n"))
	}
	return nil
}

// checkTagAbsent returns an error if tag already exists locally or on the
// remote. Local check uses `git tag -l`; remote check uses `git ls-remote`.
func checkTagAbsent(tag, remote string) error {
	// Local check.
	out, err := gitOutput("tag", "-l", tag)
	if err != nil {
		return fmt.Errorf("git tag -l failed: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("tag %s already exists locally — delete it first with: git tag -d %s", tag, tag)
	}

	// Remote check.
	out, err = gitOutput("ls-remote", "--tags", remote, "refs/tags/"+tag)
	if err != nil {
		// ls-remote failing is not a hard error (e.g. remote is unreachable in
		// offline environments); warn and continue.
		fmt.Printf("   warning: could not check remote for existing tag: %v\n", err)
		return nil
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("tag %s already exists on remote %s", tag, remote)
	}
	return nil
}

// runTests runs `go test -count=1 -timeout 120s <pkg>` and streams output.
func runTests(pkg string) error {
	return run("go", "test", "-count=1", "-timeout", "120s", pkg)
}

// git runs a git subcommand and streams its output to stdout/stderr.
func git(args ...string) error {
	return run("git", args...)
}

// gitOutput runs a git subcommand and returns its combined stdout as a string.
func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	return string(out), err
}

// run executes name with args, wiring stdout/stderr to the current process.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// fatalf prints a formatted error message to stderr and exits with code 1.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
