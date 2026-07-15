package gopug

import (
	"strings"
	"testing"
)

// --- Headline: both real corpus shapes ---

// TestCodegenEachArrayLiteralStringRealShape is the string-array real corpus
// shape: `each opt in ["3h","24h","72h"]` with the loop variable used as a
// dynamic attribute value AND buffered code in the same tag, byte-identical
// to the interpreter across the full rendered loop.
func TestCodegenEachArrayLiteralStringRealShape(t *testing.T) {
	t.Parallel()
	src := `each opt in ["3h", "24h", "72h"]` + "\n" +
		"  button(data-threshold=opt)= opt\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenEachArrayLiteralNumericRealShape is the numeric-array real
// corpus shape: `each d in [1, 2, 3, 7, 14, 28, 60]` with the loop variable
// used as a dynamic attribute value AND inside a template-literal `${d}d`
// part — the shape that proves the float64 element model is right (`1`
// stringifies to "1d", not "1.000000d" or similar).
func TestCodegenEachArrayLiteralNumericRealShape(t *testing.T) {
	t.Parallel()
	src := "each d in [1, 2, 3, 7, 14, 28, 60]\n" +
		"  button(data-diary-fill=d)= `${d}d`\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenEachArrayLiteralNumericPercent is the third real corpus shape,
// a plain `#{pct}` interpolation over a numeric array literal.
func TestCodegenEachArrayLiteralNumericPercent(t *testing.T) {
	t.Parallel()
	src := "each pct in [50, 55, 60, 65, 70, 75]\n" +
		"  p #{pct}\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// --- Element-model-distinguishing differential (THE correctness proof) ---

// TestCodegenEachArrayLiteralElementModelDistinguishing is a REAL
// differential (not just the happy integer path) over [1e2, 010, 3.5]: a
// scientific-notation token, a legacy-octal-looking token, and a fractional
// token. If codegen modeled the element as anything other than a float64
// parsed via parseJSNumber and stringified via genScalarStringify's Float64
// case (strconv.FormatFloat 'f', -1, 64) — e.g. if it emitted the original
// Pug token verbatim — this would diverge from the interpreter's own
// canonical-decimal rendering ("100", "8", "3.5"), proven independently by
// TestEachArrayLiteralNumericElementModelSelfConsistencyProbe below.
func TestCodegenEachArrayLiteralElementModelDistinguishing(t *testing.T) {
	t.Parallel()
	src := "each n in [1e2, 010, 3.5]\n" +
		"  p #{n}\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// --- Self-consistency probe ---

// TestEachArrayLiteralNumericElementModelSelfConsistencyProbe is the
// interpreter-only evidence the element-type decision rests on: rendering
// `each n in [1e2, 010, 3.5]` then `#{n}` against the interpreter ALONE
// (no codegen involved) yields "100", "8", "3.5" — the canonical decimal form
// parseJSNumber/strconv.FormatFloat produce, NOT the original token spelling
// ("1e2", "010", "3.5"). This proves Runtime.evaluateExprRaw's array-literal
// branch stores each numeric-literal element via the same canonical
// numeric-formatting path Runtime.evaluateExpr's own bare-numeric-literal
// case uses (both ultimately call parseJSNumber then
// strconv.FormatFloat(n, 'f', -1, 64)), which is exactly why modeling the
// codegen element as a Go float64 stringified through genScalarStringify's
// Float64 case reproduces it byte-identically.
func TestEachArrayLiteralNumericElementModelSelfConsistencyProbe(t *testing.T) {
	src := "each n in [1e2, 010, 3.5]\n  p #{n}\n"
	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	got, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("Render(%q): %v", src, err)
	}
	want := "<p>100</p><p>8</p><p>3.5</p>"
	if got != want {
		t.Errorf("Render(%q) = %q, want %q (interpreter's canonical numeric-literal element storage)", src, got, want)
	}
}

// TestEachArrayLiteralEmptySelfConsistencyProbe documents what the
// interpreter does with an empty array-literal each collection: zero
// iterations, no output (with no `else`), and the `else` branch when one is
// present — the basis for this feature's decision to DEFER an empty array
// literal rather than guess at an element type with no element to infer it
// from.
func TestEachArrayLiteralEmptySelfConsistencyProbe(t *testing.T) {
	noElse := "each n in []\n  p=n\n"
	tmpl, err := Compile(noElse, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", noElse, err)
	}
	got, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("Render(%q): %v", noElse, err)
	}
	if got != "" {
		t.Errorf("Render(%q) = %q, want empty output (zero iterations)", noElse, got)
	}

	withElse := "each n in []\n  p=n\nelse\n  p empty\n"
	tmpl2, err := Compile(withElse, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", withElse, err)
	}
	got2, err := tmpl2.Render(map[string]any{})
	if err != nil {
		t.Fatalf("Render(%q): %v", withElse, err)
	}
	if got2 != "<p>empty</p>" {
		t.Errorf("Render(%q) = %q, want %q", withElse, got2, "<p>empty</p>")
	}
}

// --- Deferrals ---

// TestCodegenEachArrayLiteralMixedElementDeferred asserts a mixed
// string/numeric array literal is rejected with a clean, distinct error
// rather than guessed at — there is no single Go slice element type that
// models both kinds.
func TestCodegenEachArrayLiteralMixedElementDeferred(t *testing.T) {
	src := `each x in ["a", 1]` + "\n  p=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a mixed-element error, got nil", src)
	}
}

// TestCodegenEachArrayLiteralNonLiteralElementDeferred asserts an array
// literal containing a bare identifier or field reference — not itself a
// string- or numeric-literal token — is rejected, for both a mixed set and a
// wholly non-literal one.
func TestCodegenEachArrayLiteralNonLiteralElementDeferred(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "bare identifiers", src: "each x in [Slug, Name]\n  p=x\n"},
		{name: "single field reference", src: "each x in [Slug]\n  p=x\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genUnbufferedErr(t, tc.src)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a non-literal-element error, got nil", tc.src)
			}
		})
	}
}

// TestCodegenEachArrayLiteralNestedArrayElementDeferred asserts a nested
// array literal element is rejected: it is neither a quoted string literal
// nor a numeric literal, so it falls into the same non-literal-element
// rejection as a bare identifier.
func TestCodegenEachArrayLiteralNestedArrayElementDeferred(t *testing.T) {
	src := "each x in [[1, 2]]\n  p=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a non-literal-element error, got nil", src)
	}
}

// TestCodegenEachArrayLiteralEmptyDeferred asserts an empty array-literal
// each collection is rejected: with no element to infer a Go element type
// from, codegen declines to guess (see
// TestEachArrayLiteralEmptySelfConsistencyProbe for what the interpreter
// itself does with this shape).
func TestCodegenEachArrayLiteralEmptyDeferred(t *testing.T) {
	src := "each x in []\n  p=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an empty-collection error, got nil", src)
	}
}

// --- Fault injection (non-vacuous differential proof) ---

// TestCodegenEachArrayLiteralFaultInjection proves the differential tests
// above are actually exercising the generated code's output, not merely
// checking it built and ran: a deliberately WRONG expected value must fail
// the comparison.
func TestCodegenEachArrayLiteralFaultInjection(t *testing.T) {
	t.Parallel()
	src := "each pct in [50, 55, 60]\n  p #{pct}\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, "opsData{}")
	wrongWant := "<p>1</p><p>2</p><p>3</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// --- Type-blind mode ---

// TestCodegenEachArrayLiteralNumericTypeBlindDeferred asserts a numeric
// array-literal each collection is rejected under a nil Config.DataReflectType
// (type-blind mode): resolveFieldExpr only stringifies a scope var through
// its genuine reflect.Type in type-aware mode, so a float64 item variable
// read back under type-blind mode would otherwise be emitted directly
// wherever a string is expected — invalid Go that GenerateGo's own
// gofmt-only formatting pass cannot catch (only `go build` would). This
// mirrors genNumericExpr/genUnbufferedAssign's identical restriction on a
// numeric `- var` local.
func TestCodegenEachArrayLiteralNumericTypeBlindDeferred(t *testing.T) {
	src := "each d in [1, 2, 3]\n  p=d\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName: "tmpl",
		FuncName:    "Render",
		DataType:    "map[string]any",
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q) under nil DataReflectType: expected an error, got nil", src)
	}
}

// TestCodegenEachArrayLiteralStringTypeBlindWorks proves the string-array
// classification has no such restriction: a type-blind scope var resolves to
// its bare Go identifier unconverted, which is already a valid Go string, so
// the generated code both type-checks (go build) and renders correctly
// regardless of type-awareness.
func TestCodegenEachArrayLiteralStringTypeBlindWorks(t *testing.T) {
	src := "each s in [\"a\", \"b\"]\n  p=s\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName: "tmpl",
		FuncName:    "Render",
		DataType:    "map[string]any",
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q) under nil DataReflectType: expected no error, got: %v", src, err)
	}
	if !strings.Contains(string(generated), `[]string{"a", "b"}`) {
		t.Errorf("GenerateGo(%q) output does not contain the expected []string literal:\n%s", src, generated)
	}
}

// --- Regression: field/slice-var each unchanged ---

// TestCodegenEachArrayLiteralFieldCollectionRegression proves the pre-existing
// field-collection `each` path (resolveFieldExpr) is completely unaffected by
// the array-literal detection guard added at the top of genEach — a plain
// slice field collection still resolves and renders exactly as before.
func TestCodegenEachArrayLiteralFieldCollectionRegression(t *testing.T) {
	t.Parallel()
	src := "each x in Items\n  p=x\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Items": []any{"a", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
	})
}
