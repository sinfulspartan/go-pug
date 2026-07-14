package gopug

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// diffCase is one differential RUN case fed to runDifferentialBatch: generated
// is a GenerateGo result (its own "package X" clause, import block, and
// funcName func), and dataLiteral is a composite literal constructing the
// data value the generated function renders with. name is used both as the
// batch's case<i> sub-package suffix source (indirectly, via its index) and
// for attribution when a batch build fails and per-case fallback kicks in.
type diffCase struct {
	name        string
	generated   []byte
	dataLiteral string
}

// batchResult is one diffCase's outcome: Out is the rendered output (empty on
// error) and Err is the render error's message (empty on success) — the same
// (output, error) shape runGeneratedGo/runGeneratedGoWantErr each returned
// from a separate `go run` before batching folded every case's outcome into
// one process's JSON-encoded stdout.
type batchResult struct {
	Out string
	Err string
}

// runDifferentialBatch compiles and runs every case's generated code in a
// SINGLE throwaway module with a SINGLE `go run .`, instead of the one
// module-per-case `go build`/`go run` cost runGeneratedGo/runGeneratedGoWantErr
// each pay. Each case becomes its own case<i> sub-package (so cases sharing an
// identical funcName, e.g. every ops differential case's "RenderOps", never
// collide) containing a copy of structSrc plus an exported wrapper:
//
//	func Run() (out string, errStr string) {
//		defer recover into errStr
//		var b strings.Builder
//		err := <funcName>(&b, <dataLiteral>)
//		...
//	}
//
// A generated root main.go imports every case<i> sub-package, calls each
// Run() exactly once, and marshals the ordered results as one JSON array to
// stdout. JSON is the isolation protocol: arbitrary rendered output (any
// bytes, including bytes that would collide with a hand-rolled text
// delimiter) round-trips through encoding/json exactly, so one case's output
// can never bleed into another's. `go run .` runs once; its stdout is
// unmarshaled into the ordered []batchResult returned to the caller.
//
// If the batch itself fails to build/run, that failure is NOT surfaced as an
// unattributable blob: runDifferentialBatch falls back to building each
// case's generated code individually (via buildDifferentialCaseSource) so a
// genuine GenerateGo bug is still pinned to the case name that caused it,
// exactly as if that one case had been built alone. A per-case RUNTIME
// panic or error does not reach this path at all — it is caught inside that
// case's own Run() wrapper and surfaces as a non-empty Err in the batch's
// normal JSON result, letting the batch itself always succeed.
func runDifferentialBatch(t *testing.T, structSrc string, funcName string, cases []diffCase) []batchResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	dir := t.TempDir()
	goMod := "module codegenbatch\n\ngo 1.26\n" + repoModuleReplaceDirectives(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	for i, c := range cases {
		caseDir := filepath.Join(dir, fmt.Sprintf("case%d", i))
		if err := os.MkdirAll(caseDir, 0o755); err != nil {
			t.Fatalf("creating case directory for %q: %v", c.name, err)
		}
		src := renderDifferentialCaseSource(t, fmt.Sprintf("case%d", i), structSrc, funcName, c.generated, c.dataLiteral)
		if err := os.WriteFile(filepath.Join(caseDir, "gen.go"), []byte(src), 0o644); err != nil {
			t.Fatalf("writing gen.go for %q: %v", c.name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(renderDifferentialBatchMain(len(cases))), 0o644); err != nil {
		t.Fatalf("writing batch main.go: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		var broken []string
		for i, c := range cases {
			if buildErr := buildDifferentialCaseSource(t, structSrc, funcName, c.generated, c.dataLiteral); buildErr != nil {
				broken = append(broken, fmt.Sprintf("case %d (%s): %v", i, c.name, buildErr))
			}
		}
		if len(broken) > 0 {
			t.Fatalf("batch build/run failed; per-case rebuild isolated the broken case(s):\n%s\n--- batch output ---\n%s", strings.Join(broken, "\n"), out)
		}
		t.Fatalf("batch build/run failed but every case compiled individually (aggregator bug, not a codegen bug):\n%s", out)
	}

	var results []batchResult
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatalf("unmarshaling batch JSON output: %v\nraw output:\n%s", err, out)
	}
	if len(results) != len(cases) {
		t.Fatalf("batch produced %d result(s) for %d case(s) submitted", len(results), len(cases))
	}
	return results
}

// TestRunDifferentialBatchRecoversPanic is the first real exercise of Run()'s
// recover()->Err path: every genuine fallible construct GenerateGo currently
// supports (division/modulo by zero) surfaces as a returned error, not a Go
// panic, so no differential case anywhere in the suite reaches the recover()
// branch by accident. This test submits a hand-built "generated" function
// that panics outright, proving three things end to end before any sweep
// converts a real test onto this mechanism: the panic is actually caught
// (the batch's own `go run .` still exits 0 and produces valid JSON, rather
// than crashing the whole batch), the recovered value round-trips through
// the batch's JSON encoding into a non-empty Err, and Out stays empty for
// that case.
func TestRunDifferentialBatchRecoversPanic(t *testing.T) {
	t.Parallel()
	generated := []byte(`package main

import "io"

func RenderPanic(w io.Writer, d any) error {
	panic("deliberate test panic")
}
`)

	results := runDifferentialBatch(t, "", "RenderPanic", []diffCase{
		{name: "deliberate panic", generated: generated, dataLiteral: "nil"},
	})

	if len(results) != 1 {
		t.Fatalf("runDifferentialBatch: got %d result(s), want 1", len(results))
	}
	got := results[0]
	if got.Err == "" {
		t.Fatalf("panicking case: Err is empty; the panic did not reach Run()'s recover()")
	}
	if !strings.Contains(got.Err, "deliberate test panic") {
		t.Errorf("panicking case: Err %q does not contain the recovered panic value", got.Err)
	}
	if got.Out != "" {
		t.Errorf("panicking case: Out = %q, want empty (Run() must not report partial output for a panicking case)", got.Out)
	}
}

// renderDifferentialCaseSource builds one case sub-package's source, named
// packageName: it rewrites generated's leading "package X" clause to
// "package <packageName>", splices structSrc (and, if generated does not
// already import them, "fmt" and "strings") in right before the first
// "\nfunc " the same way buildGeneratedGo/runGeneratedGo splice
// opsDataStructSrc, and appends an exported Run() wrapper that recovers a
// panic into its error return and otherwise renders funcName's output into a
// strings.Builder. Both runDifferentialBatch (packageName "case<i>") and
// buildDifferentialCaseSource's per-case attribution fallback (packageName
// "diffcase") build the identical wrapper shape from this one function, so a
// case that only breaks because of its own dataLiteral fails the same way in
// both places.
func renderDifferentialCaseSource(t *testing.T, packageName, structSrc, funcName string, generated []byte, dataLiteral string) string {
	t.Helper()

	genStr := string(generated)
	nl := strings.IndexByte(genStr, '\n')
	if nl < 0 || !strings.HasPrefix(genStr, "package ") {
		t.Fatalf("generated source does not start with a \"package \" clause:\n%s", genStr)
	}
	genStr = fmt.Sprintf("package %s\n", packageName) + genStr[nl+1:]

	funcIdx := strings.Index(genStr, "\nfunc ")
	if funcIdx < 0 {
		t.Fatalf("generated source has no \"func \" to splice the struct declaration before:\n%s", genStr)
	}
	header := genStr[:funcIdx]

	var extraImports strings.Builder
	if !strings.Contains(header, `"fmt"`) {
		extraImports.WriteString("\t\"fmt\"\n")
	}
	if !strings.Contains(header, `"strings"`) {
		extraImports.WriteString("\t\"strings\"\n")
	}

	var src strings.Builder
	src.WriteString(header)
	if extraImports.Len() > 0 {
		src.WriteString("\n\nimport (\n")
		src.WriteString(extraImports.String())
		src.WriteString(")\n\n")
	} else {
		src.WriteString("\n\n")
	}
	src.WriteString(structSrc)
	src.WriteString(genStr[funcIdx:])

	src.WriteString("\nfunc Run() (out string, errStr string) {\n")
	src.WriteString("\tdefer func() {\n")
	src.WriteString("\t\tif r := recover(); r != nil {\n")
	src.WriteString("\t\t\terrStr = fmt.Sprint(r)\n")
	src.WriteString("\t\t}\n")
	src.WriteString("\t}()\n")
	src.WriteString("\tvar b strings.Builder\n")
	fmt.Fprintf(&src, "\tif err := %s(&b, %s); err != nil {\n", funcName, dataLiteral)
	src.WriteString("\t\treturn \"\", err.Error()\n")
	src.WriteString("\t}\n")
	src.WriteString("\treturn b.String(), \"\"\n")
	src.WriteString("}\n")

	return src.String()
}

// renderDifferentialBatchMain builds the root aggregator's main.go: it
// imports every codegenbatch/case<i> sub-package, calls each Run() exactly
// once, and JSON-encodes the ordered []batchResult to stdout.
func renderDifferentialBatchMain(n int) string {
	var src strings.Builder
	src.WriteString("package main\n\n")
	src.WriteString("import (\n\t\"encoding/json\"\n\t\"os\"\n\n")
	for i := range n {
		fmt.Fprintf(&src, "\tcase%d \"codegenbatch/case%d\"\n", i, i)
	}
	src.WriteString(")\n\n")
	src.WriteString("type batchResult struct {\n\tOut string\n\tErr string\n}\n\n")
	src.WriteString("func main() {\n")
	fmt.Fprintf(&src, "\tresults := make([]batchResult, 0, %d)\n", n)
	for i := range n {
		fmt.Fprintf(&src, "\t{\n\t\tout, errStr := case%d.Run()\n\t\tresults = append(results, batchResult{Out: out, Err: errStr})\n\t}\n", i)
	}
	src.WriteString("\tb, err := json.Marshal(results)\n")
	src.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	src.WriteString("\tos.Stdout.Write(b)\n")
	src.WriteString("}\n")
	return src.String()
}

// buildDifferentialCaseSource is runDifferentialBatch's per-case attribution
// fallback: it builds ONE case's generated code alone, returning a non-nil
// error describing the `go build` failure instead of failing the test
// itself, so the caller can report which case(s), by name, broke the batch.
//
// It builds the case's FULL sub-package source — structSrc, funcName, and
// dataLiteral all spliced in via the exact same renderDifferentialCaseSource
// call the batch itself used for that case (package "diffcase" instead of
// "case<i>", everything else identical) — rather than only the raw generated
// code. Building the raw generated code alone would miss a case whose
// dataLiteral itself is malformed (e.g. a typo'd field name in the composite
// literal): that broken dataLiteral only appears in the Run() wrapper this
// function now includes, so building it here is what lets the fallback
// pin the fault on the actual offending case instead of leaving it to
// surface only as an unattributable batch failure.
func buildDifferentialCaseSource(t *testing.T, structSrc, funcName string, generated []byte, dataLiteral string) error {
	t.Helper()

	dir := t.TempDir()
	goMod := "module diffcase\n\ngo 1.26\n" + repoModuleReplaceDirectives(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	src := renderDifferentialCaseSource(t, "diffcase", structSrc, funcName, generated, dataLiteral)

	if err := os.WriteFile(filepath.Join(dir, "render.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("writing render.go: %v", err)
	}

	cmd := exec.Command("go", "build", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build failed:\n%s", out)
	}
	return nil
}
