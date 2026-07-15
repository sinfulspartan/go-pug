package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// acData is the small root type this file's differential cases resolve
// comparison-valued dynamic attributes against: Count and Name give a
// numeric and a string leaf (enough for numeric-coercion, string, and
// ordering comparisons), Flag is a plain bool leaf used only by the
// bare-field regression case.
type acData struct {
	Count int
	Name  string
	Flag  bool
}

var acDataReflectType = reflect.TypeOf(acData{})

// acDataStructSrc is acData's declaration, reused verbatim by the
// differential harness to assemble a standalone, compilable Go source file
// around a GenerateGo result.
const acDataStructSrc = `type acData struct {
	Count int
	Name  string
	Flag  bool
}
`

// acDiffCase is one runAcDifferentialBatch case: src is a full Pug template
// (not wrapped by the harness, since an attribute lives on its own tag
// line), compared through both the interpreter (Compile/Template.Render,
// against data) and the codegen backend (GenerateGo/runDifferentialBatch,
// against dataLiteral — an acData composite literal describing the same
// data).
type acDiffCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

// acDiffResult is one acDiffCase's outcome.
type acDiffResult struct {
	out  string
	want string
	err  string
}

// runAcDifferentialBatch is this file's analogue of
// runCsConditionDifferentialBatch (codegen_condition_struct_test.go): it
// prepares every case's GenerateGo output and interpreter oracle up front,
// then submits them to a single runDifferentialBatch call, returning each
// case's (out, want, err) triple index-matched to cases.
func runAcDifferentialBatch(t *testing.T, cases []acDiffCase) []acDiffResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	type prepared struct {
		c    acDiffCase
		want string
	}

	var diffCases []diffCase
	var prep []prepared

	for _, c := range cases {
		ast, err := Parse(c.src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.src, err)
		}
		generated, err := GenerateGo(ast, Config{
			PackageName:     "main",
			FuncName:        "RenderAc",
			DataType:        "acData",
			DataReflectType: acDataReflectType,
		})
		if err != nil {
			t.Fatalf("GenerateGo(%q): expected no error, got: %v", c.src, err)
		}

		tmpl, err := Compile(c.src, nil)
		if err != nil {
			t.Fatalf("Compile(%q): %v", c.src, err)
		}
		want, err := tmpl.Render(c.data)
		if err != nil {
			t.Fatalf("interpreter Render(%q) with data %v: %v", c.src, c.data, err)
		}

		diffCases = append(diffCases, diffCase{name: c.name, generated: generated, dataLiteral: c.dataLiteral})
		prep = append(prep, prepared{c: c, want: want})
	}

	batchResults := runDifferentialBatch(t, acDataStructSrc, "RenderAc", diffCases)

	results := make([]acDiffResult, len(cases))
	for i, p := range prep {
		r := batchResults[i]
		results[i] = acDiffResult{out: r.Out, want: p.want, err: r.Err}
	}
	return results
}

// assertAcDiffResult asserts an acDiffResult renders without error and
// matches the interpreter oracle exactly.
func assertAcDiffResult(t *testing.T, src, dataLiteral string, r acDiffResult) string {
	t.Helper()
	if r.err != "" {
		t.Fatalf("generated RenderAc: unexpected error %q for template %q (data literal %s)", r.err, src, dataLiteral)
	}
	if r.out != r.want {
		t.Errorf("codegen output %q does not match interpreter output %q for template %q with data literal %s", r.out, r.want, src, dataLiteral)
	}
	return r.out
}

// TestCodegenAttrComparisonBooleanOmit proves the boolean-attribute
// omit-on-false / render-on-true rule for a comparison-valued attribute: the
// interpreter's own evaluateExpr stringifies a comparison to "true"/"false",
// and renderTag's boolean-omit rule (runtime.go) drops the whole attribute
// only when that string is "false" AND the raw value is not a quoted
// literal — never true for a comparison expression — so a true comparison
// renders ` name="true"` and a false one is omitted entirely, byte-for-byte
// against the interpreter, for both a numeric and a string operand.
func TestCodegenAttrComparisonBooleanOmit(t *testing.T) {
	t.Parallel()
	cases := []acDiffCase{
		{
			name:        "checked=(Count == 0), Count 0 (true)",
			src:         `input(type="checkbox" checked=(Count == 0))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "acData{Count: 0}",
		},
		{
			name:        "checked=(Count == 0), Count 5 (false)",
			src:         `input(type="checkbox" checked=(Count == 0))`,
			data:        map[string]any{"Count": 5},
			dataLiteral: "acData{Count: 5}",
		},
		{
			name:        "selected=(Name == \"\"), Name empty (true)",
			src:         `option(selected=(Name == ""))`,
			data:        map[string]any{"Name": ""},
			dataLiteral: `acData{Name: ""}`,
		},
		{
			name:        "selected=(Name == \"\"), Name non-empty (false)",
			src:         `option(selected=(Name == ""))`,
			data:        map[string]any{"Name": "x"},
			dataLiteral: `acData{Name: "x"}`,
		},
	}
	results := runAcDifferentialBatch(t, cases)
	wantTrue := []bool{true, false, true, false}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertAcDiffResult(t, tc.src, tc.dataLiteral, results[i])
			if wantTrue[i] {
				if !strings.Contains(got, `="true"`) {
					t.Errorf("template %q: got %q, want the boolean attribute rendered with =\"true\"", tc.src, got)
				}
			} else if strings.Contains(got, "checked") || strings.Contains(got, "selected") {
				t.Errorf("template %q: got %q, want the boolean attribute omitted entirely", tc.src, got)
			}
		})
	}
}

// TestCodegenAttrComparisonNonBooleanRendersBoth proves that a
// comparison-valued attribute on a NON-boolean name never omits: it always
// renders ="true" or ="false", matching renderTag's own omit rule, which is
// gated on isBooleanAttribute and so never fires for a data-* (or any other
// non-HTML-boolean) attribute name.
func TestCodegenAttrComparisonNonBooleanRendersBoth(t *testing.T) {
	t.Parallel()
	cases := []acDiffCase{
		{
			name:        "data-active=(Count == 0), Count 0 (renders true)",
			src:         `div(data-active=(Count == 0))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "acData{Count: 0}",
		},
		{
			name:        "data-active=(Count == 0), Count 5 (renders false)",
			src:         `div(data-active=(Count == 0))`,
			data:        map[string]any{"Count": 5},
			dataLiteral: "acData{Count: 5}",
		},
	}
	results := runAcDifferentialBatch(t, cases)
	wants := []string{`<div data-active="true"></div>`, `<div data-active="false"></div>`}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertAcDiffResult(t, tc.src, tc.dataLiteral, results[i])
			if got != wants[i] {
				t.Errorf("template %q: got %q, want %q", tc.src, got, wants[i])
			}
		})
	}
}

// TestCodegenAttrComparisonNumericCoercion proves a comparison between a
// numeric field and a numeric-looking string literal — reusing
// gopug.CompareValues' numeric coercion — renders identically to the
// interpreter for both a boolean and a non-boolean attribute name.
func TestCodegenAttrComparisonNumericCoercion(t *testing.T) {
	t.Parallel()
	cases := []acDiffCase{
		{
			name:        `selected=(Count == "0"), Count 0 (numeric coercion, true)`,
			src:         `option(selected=(Count == "0"))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "acData{Count: 0}",
		},
		{
			name:        `selected=(Count == "0"), Count 5 (numeric coercion, false)`,
			src:         `option(selected=(Count == "0"))`,
			data:        map[string]any{"Count": 5},
			dataLiteral: "acData{Count: 5}",
		},
		{
			name:        `data-x=(Count == "0"), Count 0 (non-boolean, renders true)`,
			src:         `div(data-x=(Count == "0"))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "acData{Count: 0}",
		},
	}
	results := runAcDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertAcDiffResult(t, tc.src, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenAttrComparisonString proves a plain string-field-vs-literal
// comparison attribute value renders identically to the interpreter.
func TestCodegenAttrComparisonString(t *testing.T) {
	t.Parallel()
	cases := []acDiffCase{
		{
			name:        `data-x=(Name == "admin"), Name admin (true)`,
			src:         `div(data-x=(Name == "admin"))`,
			data:        map[string]any{"Name": "admin"},
			dataLiteral: `acData{Name: "admin"}`,
		},
		{
			name:        `data-x=(Name == "admin"), Name guest (false)`,
			src:         `div(data-x=(Name == "admin"))`,
			data:        map[string]any{"Name": "guest"},
			dataLiteral: `acData{Name: "guest"}`,
		},
	}
	results := runAcDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertAcDiffResult(t, tc.src, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenAttrComparisonOrderingAndNotEqual proves the ordering (`>`) and
// inequality (`!=`) comparison operators render identically to the
// interpreter as dynamic attribute values, on both a non-boolean and a
// boolean attribute name.
func TestCodegenAttrComparisonOrderingAndNotEqual(t *testing.T) {
	t.Parallel()
	cases := []acDiffCase{
		{
			name:        "data-x=(Count > 5), Count 10 (true)",
			src:         `div(data-x=(Count > 5))`,
			data:        map[string]any{"Count": 10},
			dataLiteral: "acData{Count: 10}",
		},
		{
			name:        "data-x=(Count > 5), Count 1 (false)",
			src:         `div(data-x=(Count > 5))`,
			data:        map[string]any{"Count": 1},
			dataLiteral: "acData{Count: 1}",
		},
		{
			name:        "disabled=(Count != 0), Count 1 (true, renders)",
			src:         `input(disabled=(Count != 0))`,
			data:        map[string]any{"Count": 1},
			dataLiteral: "acData{Count: 1}",
		},
		{
			name:        "disabled=(Count != 0), Count 0 (false, omitted)",
			src:         `input(disabled=(Count != 0))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "acData{Count: 0}",
		},
	}
	results := runAcDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertAcDiffResult(t, tc.src, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenAttrComparisonTernaryConditionRendersCorrectly proves the
// ternary/comparison boundary this slice draws: a ternary-valued boolean
// attribute (`checked=(Count == 0 ? "x" : false)`) is NOT reclassified as a
// comparison — the top-level `?` makes it a ternary, a distinct shape that
// this comparison-detection branch itself leaves alone — but a dedicated
// ternary-valued-boolean-attribute code path renders it correctly (byte-for-
// byte against the interpreter, true-branch renders / exact-"false"
// omits), reusing the comparison this ternary's own condition happens to
// contain via the ordinary genCondition path.
func TestCodegenAttrComparisonTernaryConditionRendersCorrectly(t *testing.T) {
	t.Parallel()
	results := runAcDifferentialBatch(t, []acDiffCase{
		{
			name:        `checked=(Count == 0 ? "x" : false), Count 0 (renders)`,
			src:         `input(checked=(Count == 0 ? "x" : false))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "acData{Count: 0}",
		},
		{
			name:        `checked=(Count == 0 ? "x" : false), Count 5 (omitted)`,
			src:         `input(checked=(Count == 0 ? "x" : false))`,
			data:        map[string]any{"Count": 5},
			dataLiteral: "acData{Count: 5}",
		},
	})
	got0 := assertAcDiffResult(t, `input(checked=(Count == 0 ? "x" : false))`, "acData{Count: 0}", results[0])
	if got0 != `<input checked="x">` {
		t.Errorf("got %q, want %q", got0, `<input checked="x">`)
	}
	got1 := assertAcDiffResult(t, `input(checked=(Count == 0 ? "x" : false))`, "acData{Count: 5}", results[1])
	if got1 != `<input>` {
		t.Errorf("got %q, want %q", got1, `<input>`)
	}
}

// TestCodegenAttrComparisonFallibleOperandDefers proves that a comparison
// whose operand genCondition/genComparison cannot resolve (here, an
// arithmetic sub-expression genOperand has no support for at all) propagates
// genCondition's own error rather than being silently accepted or swallowed.
func TestCodegenAttrComparisonFallibleOperandDefers(t *testing.T) {
	cases := []string{
		`div(data-x=(Count / 0 == 1))`,
		`input(checked=(Count / 0 == 1))`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			ast, err := Parse(src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderAc",
				DataType:        "acData",
				DataReflectType: acDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a deferral error for a comparison with an unresolvable operand, got nil", src)
			}
		})
	}
}

// TestCodegenAttrComparisonQuoteEdgeBooleanDefers proves the fix for the
// quote-wrapped literal-comparison omit divergence: the interpreter's
// boolean omit rule (Runtime.renderTag) suppresses omit-on-false only when
// the RAW, un-parenthesized attribute value text both starts and ends with a
// matching quote character — a check that also fires, accidentally, for a
// comparison whose leftmost and rightmost tokens are quoted string literals
// (`checked="a" == "b"`: raw text starts with `"` and ends with the `"`
// that closes `"b"`), even though the raw text is not itself a single
// quoted literal. GenerateGo must defer (return an error) for this shape
// rather than take the comparison path and silently omit — the caller's
// fallback to the interpreter then produces the correct `checked="false"`,
// which this test also confirms directly against the interpreter oracle so
// the deferral is proven to route to a CORRECT fallback, not just to any
// error.
func TestCodegenAttrComparisonQuoteEdgeBooleanDefers(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "double-quoted literals", src: `input(checked="a" == "b")`},
		{name: "single-quoted literals", src: `input(checked='a' == 'b')`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderAc",
				DataType:        "acData",
				DataReflectType: acDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a deferral error for a quote-edge comparison on a boolean attribute, got nil", tc.src)
			}

			tmpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tc.src, err)
			}
			want, err := tmpl.Render(map[string]any{})
			if err != nil {
				t.Fatalf("interpreter Render(%q): %v", tc.src, err)
			}
			if want != `<input checked="false">` {
				t.Fatalf("interpreter oracle for %q = %q, want %q (the omit-suppression this deferral must fall back to)", tc.src, want, `<input checked="false">`)
			}
		})
	}
}

// TestCodegenAttrComparisonQuoteEdgeParenthesizedStillOmits proves the
// discriminator the quote-edge fix must preserve: a PARENTHESIZED literal
// comparison (`checked=("a" == "b")`) has raw text starting with `(`, not a
// quote, so the interpreter's crude isQuoted check does NOT fire and the
// omit rule applies normally — the comparison path must still be taken and
// the false comparison must still omit the attribute, byte-identical to the
// interpreter, exactly as every other parenthesized comparison in this file
// already does.
func TestCodegenAttrComparisonQuoteEdgeParenthesizedStillOmits(t *testing.T) {
	t.Parallel()
	results := runAcDifferentialBatch(t, []acDiffCase{
		{
			name:        `checked=("a" == "b") (parenthesized, still omits)`,
			src:         `input(checked=("a" == "b"))`,
			data:        map[string]any{},
			dataLiteral: "acData{}",
		},
	})
	got := assertAcDiffResult(t, `input(checked=("a" == "b"))`, "acData{}", results[0])
	if got != "<input>" {
		t.Errorf(`template %q: got %q, want "<input>" (the parenthesized false comparison must still omit)`, `input(checked=("a" == "b"))`, got)
	}
}

// TestCodegenAttrComparisonQuoteEdgeNonBooleanUnaffected proves the quote-edge
// guard is scoped to the boolean-attribute branch only: a non-boolean
// attribute name never triggers the interpreter's omit rule in the first
// place (it is gated on isBooleanAttribute before the isQuoted check is even
// consulted), so an un-parenthesized quote-edge comparison on a non-boolean
// name is untouched by this fix and renders ="false" in both the
// interpreter and codegen, exactly as it did before the quote-edge guard was
// added.
func TestCodegenAttrComparisonQuoteEdgeNonBooleanUnaffected(t *testing.T) {
	t.Parallel()
	results := runAcDifferentialBatch(t, []acDiffCase{
		{
			name:        `data-x="a" == "b" (non-boolean, unparenthesized, unaffected)`,
			src:         `div(data-x="a" == "b")`,
			data:        map[string]any{},
			dataLiteral: "acData{}",
		},
	})
	got := assertAcDiffResult(t, `div(data-x="a" == "b")`, "acData{}", results[0])
	if got != `<div data-x="false"></div>` {
		t.Errorf("template %q: got %q, want %q", `div(data-x="a" == "b")`, got, `<div data-x="false"></div>`)
	}
}

// TestCodegenAttrComparisonNilReflectTypeDefers proves the pre-existing,
// untouched rootType==nil guard (codegen.go, ahead of both the boolean- and
// non-boolean-attribute branches) still rejects a comparison-valued dynamic
// attribute exactly as it rejects every other dynamic attribute shape in
// type-blind mode.
func TestCodegenAttrComparisonNilReflectTypeDefers(t *testing.T) {
	cases := []string{
		`div(data-x=(Count == 0))`,
		`input(checked=(Count == 0))`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			ast, err := Parse(src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			_, err = GenerateGo(ast, Config{
				PackageName: "gopug",
				FuncName:    "RenderAc",
				DataType:    "SkelData",
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error for a comparison-valued dynamic attribute with a nil DataReflectType, got nil", src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
			}
		})
	}
}

// TestCodegenAttrComparisonRegressionExistingShapes proves that every
// pre-existing dynamic/static attribute shape this slice must leave
// byte-for-byte untouched — a bare bool field on a boolean attribute name, a
// bare field on a non-boolean attribute name, and a static quoted value —
// still renders identically to the interpreter, unaffected by the new
// comparison-detection branch added ahead of each existing code path.
func TestCodegenAttrComparisonRegressionExistingShapes(t *testing.T) {
	t.Parallel()
	cases := []acDiffCase{
		{
			name:        "bare bool field, boolean attr, true",
			src:         `input(checked=Flag)`,
			data:        map[string]any{"Flag": true},
			dataLiteral: "acData{Flag: true}",
		},
		{
			name:        "bare bool field, boolean attr, false",
			src:         `input(checked=Flag)`,
			data:        map[string]any{"Flag": false},
			dataLiteral: "acData{Flag: false}",
		},
		{
			name:        "bare field, non-boolean attr",
			src:         `div(data-x=Name)`,
			data:        map[string]any{"Name": "admin"},
			dataLiteral: `acData{Name: "admin"}`,
		},
		{
			name:        "static quoted attr",
			src:         `div(data-x="literal")`,
			data:        map[string]any{},
			dataLiteral: "acData{}",
		},
	}
	results := runAcDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertAcDiffResult(t, tc.src, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenAttrComparisonFaultInjectionBooleanFalseNotOmitted asserts the
// first required fault injection: a WRONG expectation that renders
// checked="true" when the comparison is FALSE (i.e. treats false as
// non-omitting) must fail against the real codegen/interpreter agreement,
// proving TestCodegenAttrComparisonBooleanOmit actually pins the omit rule
// rather than vacuously agreeing regardless of the comparison's truth value.
func TestCodegenAttrComparisonFaultInjectionBooleanFalseNotOmitted(t *testing.T) {
	results := runAcDifferentialBatch(t, []acDiffCase{
		{
			name:        "checked=(Count == 0), Count 5 (false)",
			src:         `input(type="checkbox" checked=(Count == 0))`,
			data:        map[string]any{"Count": 5},
			dataLiteral: "acData{Count: 5}",
		},
	})
	got := assertAcDiffResult(t, `input(type="checkbox" checked=(Count == 0))`, "acData{Count: 5}", results[0])
	wrongExpected := `<input checked="true" type="checkbox">`
	if got == wrongExpected {
		t.Fatalf("fault injection failed to catch a wrong expectation: a FALSE comparison rendered %q, matching the WRONG (treat-false-as-non-omitting) expectation instead of omitting the attribute", got)
	}
}

// TestCodegenAttrComparisonFaultInjectionBooleanTrueNotDropped asserts the
// second required fault injection: a WRONG expectation that OMITS the
// attribute when the comparison is TRUE must fail, proving
// TestCodegenAttrComparisonBooleanOmit also pins render-on-true, not just
// omit-on-false.
func TestCodegenAttrComparisonFaultInjectionBooleanTrueNotDropped(t *testing.T) {
	results := runAcDifferentialBatch(t, []acDiffCase{
		{
			name:        "checked=(Count == 0), Count 0 (true)",
			src:         `input(type="checkbox" checked=(Count == 0))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "acData{Count: 0}",
		},
	})
	got := assertAcDiffResult(t, `input(type="checkbox" checked=(Count == 0))`, "acData{Count: 0}", results[0])
	wrongExpected := `<input type="checkbox">`
	if got == wrongExpected {
		t.Fatalf("fault injection failed to catch a wrong expectation: a TRUE comparison rendered %q, matching the WRONG (treat-true-as-omitting) expectation instead of rendering the attribute", got)
	}
}

// TestCodegenAttrComparisonFaultInjectionNonBooleanNeverOmits asserts the
// third required fault injection: a WRONG expectation that OMITS a
// non-boolean attribute when the comparison is false must fail, proving
// TestCodegenAttrComparisonNonBooleanRendersBoth actually pins that a
// non-boolean attribute name never triggers the omit rule (it always
// renders ="false" rather than disappearing).
func TestCodegenAttrComparisonFaultInjectionNonBooleanNeverOmits(t *testing.T) {
	results := runAcDifferentialBatch(t, []acDiffCase{
		{
			name:        "data-active=(Count == 0), Count 5 (false)",
			src:         `div(data-active=(Count == 0))`,
			data:        map[string]any{"Count": 5},
			dataLiteral: "acData{Count: 5}",
		},
	})
	got := assertAcDiffResult(t, `div(data-active=(Count == 0))`, "acData{Count: 5}", results[0])
	wrongExpected := `<div></div>`
	if got == wrongExpected {
		t.Fatalf("fault injection failed to catch a wrong expectation: a FALSE comparison on a non-boolean attribute rendered %q, matching the WRONG (non-boolean-attrs-also-omit) expectation instead of rendering data-active=\"false\"", got)
	}
}
