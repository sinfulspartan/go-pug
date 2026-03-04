// clean removes build artefacts listed as command-line arguments.
// Directories are removed recursively; files are removed individually.
// Missing paths are silently skipped.
//
// Usage (via Makefile):
//
//	go run ./scripts/clean -- bin coverage.out coverage.html cpu.prof ...
package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		return
	}

	var failed bool
	for _, path := range args {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "clean: stat %s: %v\n", path, err)
			failed = true
			continue
		}

		if info.IsDir() {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "clean: remove %s: %v\n", path, err)
			failed = true
		}
	}

	if failed {
		os.Exit(1)
	}
}
