package gopug

import (
	"testing"
)

// codegenFallibleErrorCase is a differential test case proving error parity:
// both the interpreter (Compile().Render) and the generated code (GenerateGo,
// built and run via runDifferentialBatch) must fail identically when src's
// `/` or `%` expression hits its one runtime-fallible case, a numeric zero
// divisor — the interpreter's own returned error is the oracle its message
// is compared against, never a hand-written expectation.
type codegenFallibleErrorCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

// runCodegenFallibleErrorDifferential is the single-case form of
// runCodegenFallibleErrorDifferentialBatch, kept for every call site (in this
// file and several others) that has only one case to check and gains nothing
// from batching. It routes through the same runDifferentialBatch mechanism
// as every other differential RUN test — the (string, error) RenderOps
// returns on a division/modulo-by-zero surfaces as the batch case's Err via
// the shared Run() wrapper's ordinary `if err := %s(...); err != nil` path,
// not its recover() branch (see TestRunDifferentialBatchRecoversPanic in
// codegen_batch_test.go for that path's own, separate proof).
func runCodegenFallibleErrorDifferential(t *testing.T, tc codegenFallibleErrorCase) {
	t.Helper()
	runCodegenFallibleErrorDifferentialBatch(t, []codegenFallibleErrorCase{tc})
}

// runCodegenFallibleErrorDifferentialBatch batches multiple
// codegenFallibleErrorCase checks into a single runDifferentialBatch call:
// every case's GenerateGo output and interpreter oracle error
// (Compile().Render, expected non-nil) are prepared up front, then submitted
// together, cutting the dominant per-case cost (a fresh module build) down
// to one for the whole slice. Each case's own pass/fail is still reported
// through its own t.Run(tc.name, ...), matched to its batch result by index.
func runCodegenFallibleErrorDifferentialBatch(t *testing.T, cases []codegenFallibleErrorCase) {
	t.Helper()

	if len(cases) == 0 {
		return
	}

	type prepared struct {
		tc      codegenFallibleErrorCase
		wantErr string
	}

	var diffCases []diffCase
	var prep []prepared

	for _, tc := range cases {
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

		diffCases = append(diffCases, diffCase{name: tc.name, generated: generated, dataLiteral: tc.dataLiteral})
		prep = append(prep, prepared{tc: tc, wantErr: wantErr.Error()})
	}

	results := runDifferentialBatch(t, opsDataStructSrc, "RenderOps", diffCases)

	for i, p := range prep {
		t.Run(p.tc.name, func(t *testing.T) {
			result := results[i]
			if result.Err == "" {
				t.Fatalf("generated RenderOps(%q): expected an error, got success output %q", p.tc.src, result.Out)
			}
			if result.Err != p.wantErr {
				t.Errorf("generated RenderOps error %q does not match interpreter error %q for %q", result.Err, p.wantErr, p.tc.src)
			}
		})
	}
}

// TestCodegenFallibleDivisionByZero is the headline error-parity proof: a
// numeric field divided by another numeric field holding zero aborts BOTH
// the interpreter's Render and the generated RenderOps with the identical
// "division by zero" error, matching Runtime.evaluateExpr's own `/` branch
// exactly (via the single-sourced gopug.Div both engines now call).
func TestCodegenFallibleDivisionByZero(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "int field divided by literal in an attribute value",
		src:         "a(data-r=Count / 2)\n",
		data:        map[string]any{"Count": 10},
		dataLiteral: "opsData{Count: 10}",
	})
}

// TestCodegenFallibleAttrValueZeroDivisorFallback proves the ESCAPED dynamic
// attribute path's error case matches the interpreter's own behavior: on a
// runtime-erroring fallible value (a zero divisor), Runtime.renderTag does
// NOT abort the render — it falls back to the attribute's raw, un-evaluated
// source text (escaped) and keeps going. The generated code must reproduce
// that fallback byte-for-byte rather than aborting.
func TestCodegenFallibleAttrValueZeroDivisorFallback(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "zero-divisor attribute value falls back to raw source instead of aborting",
		src:         "a(data-r=Count / Zero)\n",
		data:        map[string]any{"Count": 10, "Zero": 0},
		dataLiteral: "opsData{Count: 10, Zero: 0}",
	})
}

// TestCodegenFallibleAttrValueZeroDivisorFallbackWithSpecials mirrors
// TestCodegenFallibleAttrValueZeroDivisorFallback with a raw source
// containing HTML-special characters (via a quoted string operand),
// proving the fallback's raw source text is run back through EscapeAttr
// exactly like the interpreter's own fallback is, not emitted unescaped.
func TestCodegenFallibleAttrValueZeroDivisorFallbackWithSpecials(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "zero-divisor attribute value with specials in its raw source falls back escaped",
		src:         "div(data-x= '<a>&\"' + Count / Zero)\n",
		data:        map[string]any{"Count": 10, "Zero": 0},
		dataLiteral: "opsData{Count: 10, Zero: 0}",
	})
}

// TestCodegenFallibleNonNumericIsEmptyNoError proves the OTHER branch of
// gopug.Div/Mod's contract: non-numeric operands produce the empty string
// with NO error (matching evaluateExpr's own "not both numeric -> return "",
// nil" branch) — division by a non-numeric right operand is not a "zero
// divisor" and must not be mistaken for one.
func TestCodegenFallibleNonNumericIsEmptyNoError(t *testing.T) {
	t.Parallel()
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
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenFallibleFormatting proves gopug.Div/Mod's numeric formatting —
// float quotient, integer-truncating modulo, and modulo of a fractional
// left operand (int64-truncated before the Go `%`) — matches evaluateExpr's
// own strconv.FormatFloat/int64-truncation exactly.
func TestCodegenFallibleFormatting(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "division yields a fractional quotient", src: "p= 7 / 2\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "modulo of two integers", src: "p= 7 % 2\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "modulo truncates a fractional left operand", src: "p= 7.9 % 2\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// Composing a fallible `/`/`%` result into an arithmetic combiner, a nested
// `/`/`%` operand, a ternary branch, or a template-literal `${}` part is no
// longer a deferral — see codegen_fallible_compose_test.go for the
// differential build+run proofs.
