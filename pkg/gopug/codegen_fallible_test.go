package gopug

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGeneratedGoWantErr builds and runs generated (a GenerateGo result whose
// Config.PackageName is "main" and Config.FuncName is "RenderOps") in a
// throwaway module, exactly like runGeneratedGo, except the appended main()
// does not panic when RenderOps returns an error: it prints "ERR:" followed
// by the error's message and exits cleanly, or prints "OK" on success. This
// lets a differential test observe and compare a fallible generated
// function's returned error (division/modulo by zero) instead of only being
// able to assert that it built and ran without crashing.
func runGeneratedGoWantErr(t *testing.T, generated []byte, dataLiteral string) string {
	t.Helper()

	dir := t.TempDir()
	goMod := "module opsbuild\n\ngo 1.26\n" + repoModuleReplaceDirectives(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	genStr := string(generated)
	funcIdx := strings.Index(genStr, "\nfunc ")
	if funcIdx < 0 {
		t.Fatalf("generated source has no \"func \" to splice the struct declaration before:\n%s", genStr)
	}

	// "io" is already imported by generated's own header (GenerateGo always
	// imports it), so main() can use io.Discard without a second "io" import
	// — only "fmt" needs adding here, in its own import declaration spliced
	// in alongside the struct, before "func RenderOps", exactly the way
	// runGeneratedGo splices in its own extra "os" import.
	var src strings.Builder
	src.WriteString(genStr[:funcIdx])
	src.WriteString("\n\nimport \"fmt\"\n\n")
	src.WriteString(opsDataStructSrc)
	src.WriteString(genStr[funcIdx:])
	src.WriteString("\nfunc main() {\n\td := ")
	src.WriteString(dataLiteral)
	src.WriteString("\n\tif err := RenderOps(io.Discard, d); err != nil {\n\t\tfmt.Println(\"ERR:\" + err.Error())\n\t\treturn\n\t}\n\tfmt.Println(\"OK\")\n}\n")

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatalf("writing main.go: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run on generated code failed:\n%s\n--- source ---\n%s", out, src.String())
	}
	return strings.TrimSpace(string(out))
}

// codegenFallibleErrorCase is a differential test case proving error parity:
// both the interpreter (Compile().Render) and the generated code (GenerateGo,
// built and run via runGeneratedGoWantErr) must fail identically when src's
// `/` or `%` expression hits its one runtime-fallible case, a numeric zero
// divisor — the interpreter's own returned error is the oracle its message
// is compared against, never a hand-written expectation.
type codegenFallibleErrorCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

func runCodegenFallibleErrorDifferential(t *testing.T, tc codegenFallibleErrorCase) {
	t.Helper()

	ast, err := Parse(tc.src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", tc.src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", tc.src, err)
	}

	tmpl, err := Compile(tc.src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", tc.src, err)
	}
	_, wantErr := tmpl.Render(tc.data)
	if wantErr == nil {
		t.Fatalf("interpreter Render(%q): expected an error, got nil", tc.src)
	}

	gotOut := runGeneratedGoWantErr(t, generated, tc.dataLiteral)
	if !strings.HasPrefix(gotOut, "ERR:") {
		t.Fatalf("generated RenderOps(%q): expected an error, got success output %q", tc.src, gotOut)
	}
	gotErr := strings.TrimPrefix(gotOut, "ERR:")
	if gotErr != wantErr.Error() {
		t.Errorf("generated RenderOps error %q does not match interpreter error %q for %q", gotErr, wantErr.Error(), tc.src)
	}
}

// TestCodegenFallibleDivisionByZero is the headline error-parity proof: a
// numeric field divided by another numeric field holding zero aborts BOTH
// the interpreter's Render and the generated RenderOps with the identical
// "division by zero" error, matching Runtime.evaluateExpr's own `/` branch
// exactly (via the single-sourced gopug.Div both engines now call).
func TestCodegenFallibleDivisionByZero(t *testing.T) {
	runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
		name:        "field divided by a zero-valued field",
		src:         "p= Count / Zero\n",
		data:        map[string]any{"Count": 10, "Zero": 0},
		dataLiteral: "opsData{Count: 10, Zero: 0}",
	})
}

// TestCodegenFallibleDivisionByZeroLiteral proves the pure-literal case: `10
// / 0` needs no field resolution at all, yet still errors at RUNTIME (not at
// generate time) with "division by zero" in both engines — proving literal
// operand fallibility flows through genValueExpr's numeric-literal leaf the
// same way a field operand's does.
func TestCodegenFallibleDivisionByZeroLiteral(t *testing.T) {
	runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
		name:        "pure literal division by zero",
		src:         "p= 10 / 0\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenFallibleModuloByZero mirrors TestCodegenFallibleDivisionByZero
// for `%`: both engines abort with the identical "modulo by zero" error.
func TestCodegenFallibleModuloByZero(t *testing.T) {
	runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
		name:        "field modulo a zero-valued field",
		src:         "p= Count % Zero\n",
		data:        map[string]any{"Count": 10, "Zero": 0},
		dataLiteral: "opsData{Count: 10, Zero: 0}",
	})
}

// TestCodegenFallibleDivisionSuccess proves the non-error path: an int field
// divided by a literal renders the quotient identically in both engines, with
// the extraction prelude genInterpolation/genCode emit for a fallible value
// expression never surfacing in the output.
func TestCodegenFallibleDivisionSuccess(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "int field divided by literal",
		src:         "p= Count / 2\n",
		data:        map[string]any{"Count": 10},
		dataLiteral: "opsData{Count: 10}",
	})
}

// TestCodegenFallibleModuloSuccess mirrors TestCodegenFallibleDivisionSuccess
// for `%`.
func TestCodegenFallibleModuloSuccess(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "int field modulo literal",
		src:         "p= Count % 3\n",
		data:        map[string]any{"Count": 10},
		dataLiteral: "opsData{Count: 10}",
	})
}

// TestCodegenFallibleInterpolationNumericStrings proves `/` works as the
// whole value of a `#{}` interpolation (not just a buffered `= expr`), over
// two string fields holding numeric-looking text.
func TestCodegenFallibleInterpolationNumericStrings(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "numeric-looking string fields divided in an interpolation",
		src:         "p #{Str1 / Str2}\n",
		data:        map[string]any{"Str1": "10", "Str2": "4"},
		dataLiteral: `opsData{Str1: "10", Str2: "4"}`,
	})
}

// TestCodegenFallibleAttrValue proves `/` works as a dynamic non-class
// attribute value, exercising genAttributes's extraction-before-the-name-
// write ordering (the __vN, __errN := gopug.Div(...) prelude must land
// before the attribute's ` data-r="` static text is written, not interleaved
// with it).
func TestCodegenFallibleAttrValue(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "int field divided by literal in an attribute value",
		src:         "a(data-r=Count / 2)\n",
		data:        map[string]any{"Count": 10},
		dataLiteral: "opsData{Count: 10}",
	})
}

// TestCodegenFallibleNonNumericIsEmptyNoError proves the OTHER branch of
// gopug.Div/Mod's contract: non-numeric operands produce the empty string
// with NO error (matching evaluateExpr's own "not both numeric -> return "",
// nil" branch) — division by a non-numeric right operand is not a "zero
// divisor" and must not be mistaken for one.
func TestCodegenFallibleNonNumericIsEmptyNoError(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "division of non-numeric strings",
			src:         "p= Str1 / Str2\n",
			data:        map[string]any{"Str1": "x", "Str2": "y"},
			dataLiteral: `opsData{Str1: "x", Str2: "y"}`,
		},
		{
			name:        "modulo of non-numeric strings",
			src:         "p= Str1 % Str2\n",
			data:        map[string]any{"Str1": "x", "Str2": "y"},
			dataLiteral: `opsData{Str1: "x", Str2: "y"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenFallibleFormatting proves gopug.Div/Mod's numeric formatting —
// float quotient, integer-truncating modulo, and modulo of a fractional
// left operand (int64-truncated before the Go `%`) — matches evaluateExpr's
// own strconv.FormatFloat/int64-truncation exactly.
func TestCodegenFallibleFormatting(t *testing.T) {
	cases := []codegenArithCase{
		{name: "division yields a fractional quotient", src: "p= 7 / 2\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "modulo of two integers", src: "p= 7 % 2\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "modulo truncates a fractional left operand", src: "p= 7.9 % 2\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// Composing a fallible `/`/`%` result into an arithmetic combiner, a nested
// `/`/`%` operand, a ternary branch, or a template-literal `${}` part is no
// longer a deferral — see codegen_fallible_compose_test.go for the
// differential build+run proofs.
