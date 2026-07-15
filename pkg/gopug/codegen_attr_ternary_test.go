package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// atData is the small root type this file's differential cases resolve
// ternary-valued dynamic attributes against: Enabled and Flag give bool
// leaves for the ternary condition, Name a string leaf used in a
// branch/escaping case, and Count a numeric leaf used only by a fallible
// (division) branch deferral case.
type atData struct {
	Enabled bool
	Flag    bool
	Name    string
	Count   int
}

var atDataReflectType = reflect.TypeOf(atData{})

// atDataStructSrc is atData's declaration, reused verbatim by the
// differential harness to assemble a standalone, compilable Go source file
// around a GenerateGo result.
const atDataStructSrc = `type atData struct {
	Enabled bool
	Flag    bool
	Name    string
	Count   int
}
`

// atDiffCase is one runAtDifferentialBatch case: src is a full Pug template
// (not wrapped by the harness, since an attribute lives on its own tag
// line), compared through both the interpreter (Compile/Template.Render,
// against data) and the codegen backend (GenerateGo/runDifferentialBatch,
// against dataLiteral — an atData composite literal describing the same
// data).
type atDiffCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

// atDiffResult is one atDiffCase's outcome.
type atDiffResult struct {
	out  string
	want string
	err  string
}

// runAtDifferentialBatch prepares every case's GenerateGo output and
// interpreter oracle up front, then submits them to a single
// runDifferentialBatch call, returning each case's (out, want, err) triple
// index-matched to cases.
func runAtDifferentialBatch(t *testing.T, cases []atDiffCase) []atDiffResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	type prepared struct {
		c    atDiffCase
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
			FuncName:        "RenderAt",
			DataType:        "atData",
			DataReflectType: atDataReflectType,
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

	batchResults := runDifferentialBatch(t, atDataStructSrc, "RenderAt", diffCases)

	results := make([]atDiffResult, len(cases))
	for i, p := range prep {
		r := batchResults[i]
		results[i] = atDiffResult{out: r.Out, want: p.want, err: r.Err}
	}
	return results
}

// assertAtDiffResult asserts an atDiffResult renders without error and
// matches the interpreter oracle exactly.
func assertAtDiffResult(t *testing.T, src, dataLiteral string, r atDiffResult) string {
	t.Helper()
	if r.err != "" {
		t.Fatalf("generated RenderAt: unexpected error %q for template %q (data literal %s)", r.err, src, dataLiteral)
	}
	if r.out != r.want {
		t.Errorf("codegen output %q does not match interpreter output %q for template %q with data literal %s", r.out, r.want, src, dataLiteral)
	}
	return r.out
}

// TestCodegenAttrTernaryBooleanTrueRendersFalseOmits proves the crux of this
// slice: a ternary-valued HTML boolean attribute renders the chosen branch's
// actual string (escaped) when it is not exactly "false", and omits the
// whole attribute only when the chosen branch stringifies to exactly
// "false" — byte-identical to the interpreter, for both a true and a false
// condition.
func TestCodegenAttrTernaryBooleanTrueRendersFalseOmits(t *testing.T) {
	t.Parallel()
	cases := []atDiffCase{
		{
			name:        `checked=(Enabled ? "checked" : false), Enabled true (renders)`,
			src:         `input(checked=(Enabled ? "checked" : false))`,
			data:        map[string]any{"Enabled": true},
			dataLiteral: "atData{Enabled: true}",
		},
		{
			name:        `checked=(Enabled ? "checked" : false), Enabled false (omitted)`,
			src:         `input(checked=(Enabled ? "checked" : false))`,
			data:        map[string]any{"Enabled": false},
			dataLiteral: "atData{Enabled: false}",
		},
	}
	results := runAtDifferentialBatch(t, cases)
	wantRendered := []bool{true, false}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertAtDiffResult(t, tc.src, tc.dataLiteral, results[i])
			if wantRendered[i] {
				if got != `<input checked="checked">` {
					t.Errorf("template %q: got %q, want %q", tc.src, got, `<input checked="checked">`)
				}
			} else if got != `<input>` {
				t.Errorf("template %q: got %q, want %q (the attribute omitted entirely)", tc.src, got, `<input>`)
			}
		})
	}
}

// TestCodegenAttrTernaryExactFalseOnlyOmit proves the omit rule is gated on
// the chosen branch stringifying to EXACTLY "false" — not general falsiness
// — so a branch yielding "no", the empty string, or "0" always renders,
// never omits, byte-identical to the interpreter.
func TestCodegenAttrTernaryExactFalseOnlyOmit(t *testing.T) {
	t.Parallel()
	cases := []atDiffCase{
		{
			name:        `selected=(Flag ? "yes" : "no"), Flag true`,
			src:         `input(selected=(Flag ? "yes" : "no"))`,
			data:        map[string]any{"Flag": true},
			dataLiteral: "atData{Flag: true}",
		},
		{
			name:        `selected=(Flag ? "yes" : "no"), Flag false ("no" != "false", renders)`,
			src:         `input(selected=(Flag ? "yes" : "no"))`,
			data:        map[string]any{"Flag": false},
			dataLiteral: "atData{Flag: false}",
		},
		{
			name:        `checked=(Enabled ? "" : "x"), Enabled true (empty string, renders)`,
			src:         `input(checked=(Enabled ? "" : "x"))`,
			data:        map[string]any{"Enabled": true},
			dataLiteral: "atData{Enabled: true}",
		},
		{
			name:        `checked=(Enabled ? "0" : "x"), Enabled true ("0" != "false", renders)`,
			src:         `input(checked=(Enabled ? "0" : "x"))`,
			data:        map[string]any{"Enabled": true},
			dataLiteral: "atData{Enabled: true}",
		},
	}
	results := runAtDifferentialBatch(t, cases)
	wants := []string{
		`<input selected="yes">`,
		`<input selected="no">`,
		`<input checked="">`,
		`<input checked="0">`,
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertAtDiffResult(t, tc.src, tc.dataLiteral, results[i])
			if got != wants[i] {
				t.Errorf("template %q: got %q, want %q", tc.src, got, wants[i])
			}
		})
	}
}

// TestCodegenAttrTernaryEscaping proves EscapeAttr is applied to the actual
// evaluated branch value — not a hardcoded literal — for both a non-boolean
// and a boolean attribute name.
func TestCodegenAttrTernaryEscaping(t *testing.T) {
	t.Parallel()
	cases := []atDiffCase{
		{
			name:        `data-x=(Flag ? Name : ""), non-boolean, escaped`,
			src:         `div(data-x=(Flag ? Name : ""))`,
			data:        map[string]any{"Flag": true, "Name": `a&b"c`},
			dataLiteral: `atData{Flag: true, Name: "a&b\"c"}`,
		},
		{
			name:        `checked=(Enabled ? Name : "x"), boolean attr, escaped`,
			src:         `input(checked=(Enabled ? Name : "x"))`,
			data:        map[string]any{"Enabled": true, "Name": `a&b"c`},
			dataLiteral: `atData{Enabled: true, Name: "a&b\"c"}`,
		},
	}
	results := runAtDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertAtDiffResult(t, tc.src, tc.dataLiteral, results[i])
			if !strings.Contains(got, "&amp;") || !strings.Contains(got, "&quot;") {
				t.Errorf("template %q: got %q, want the ampersand and quote in Name HTML-escaped", tc.src, got)
			}
		})
	}
}

// TestCodegenAttrTernaryNonBooleanAlreadyWorks proves a ternary-valued
// NON-boolean attribute — outside this slice's target — already rendered
// byte-identically to the interpreter before this slice's edit, through the
// pre-existing genValueExpr/genTernaryValueExpr path, unaffected by the new
// boolean-attribute-only branch this slice adds.
func TestCodegenAttrTernaryNonBooleanAlreadyWorks(t *testing.T) {
	t.Parallel()
	cases := []atDiffCase{
		{
			name:        `data-x=(Flag ? "a" : "b"), Flag true`,
			src:         `div(data-x=(Flag ? "a" : "b"))`,
			data:        map[string]any{"Flag": true},
			dataLiteral: "atData{Flag: true}",
		},
		{
			name:        `data-x=(Flag ? "a" : "b"), Flag false`,
			src:         `div(data-x=(Flag ? "a" : "b"))`,
			data:        map[string]any{"Flag": false},
			dataLiteral: "atData{Flag: false}",
		},
	}
	results := runAtDifferentialBatch(t, cases)
	wants := []string{`<div data-x="a"></div>`, `<div data-x="b"></div>`}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertAtDiffResult(t, tc.src, tc.dataLiteral, results[i])
			if got != wants[i] {
				t.Errorf("template %q: got %q, want %q", tc.src, got, wants[i])
			}
		})
	}
}

// TestCodegenAttrTernaryQuoteEdgeBooleanDefers proves the isQuoted guard
// reused from the comparison slice: an un-parenthesized ternary whose raw
// value text both starts and ends with a matching quote — here, a
// comparison condition whose left operand is a quoted literal
// (`checked="x" == Name ? "y" : "false"`) — makes the interpreter's crude
// isQuoted check fire and suppress the boolean-omit rule entirely — the
// interpreter then renders `checked="false"` literally rather than omitting
// — so GenerateGo must defer for this shape rather than take the ternary
// path and silently omit; the deferral is proven to route to a CORRECT
// interpreter fallback, not just to any error.
func TestCodegenAttrTernaryQuoteEdgeBooleanDefers(t *testing.T) {
	src := `input(checked="x" == Name ? "y" : "false")`
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderAt",
		DataType:        "atData",
		DataReflectType: atDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error for a quote-edge ternary on a boolean attribute, got nil", src)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Name": "nope"})
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}
	if want != `<input checked="false">` {
		t.Fatalf("interpreter oracle for %q = %q, want %q (the omit-suppression this deferral must fall back to)", src, want, `<input checked="false">`)
	}
}

// TestCodegenAttrTernaryQuoteEdgeParenthesizedStillOmits proves the
// discriminator the quote-edge fix must preserve: a PARENTHESIZED ternary
// (`checked=("x" == Name ? "y" : "false")`) has raw text starting with `(`,
// not a quote, so the interpreter's isQuoted check does NOT fire and the
// omit rule applies normally — the ternary path must still be taken, and the
// "false" branch must still omit the attribute, byte-identical to the
// interpreter.
func TestCodegenAttrTernaryQuoteEdgeParenthesizedStillOmits(t *testing.T) {
	t.Parallel()
	results := runAtDifferentialBatch(t, []atDiffCase{
		{
			name:        `checked=("x" == Name ? "y" : "false") (parenthesized, still omits)`,
			src:         `input(checked=("x" == Name ? "y" : "false"))`,
			data:        map[string]any{"Name": "nope"},
			dataLiteral: `atData{Name: "nope"}`,
		},
	})
	got := assertAtDiffResult(t, `input(checked=("x" == Name ? "y" : "false"))`, `atData{Name: "nope"}`, results[0])
	if got != "<input>" {
		t.Errorf(`template %q: got %q, want "<input>" (the parenthesized "false" branch must still omit)`, `input(checked=("x" == Name ? "y" : "false"))`, got)
	}
}

// TestCodegenAttrTernaryFallibleBranchDefers proves a ternary whose chosen
// branch expression is fallible (a division or modulo) on a boolean
// attribute defers rather than silently mis-rendering: the interpreter's own
// raw-source fallback inside the boolean-omit test is a subtle case this
// slice deliberately does not attempt to reproduce.
func TestCodegenAttrTernaryFallibleBranchDefers(t *testing.T) {
	src := `input(checked=(Flag ? Count/0 : "x"))`
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderAt",
		DataType:        "atData",
		DataReflectType: atDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error for a fallible ternary branch on a boolean attribute, got nil", src)
	}
}

// TestCodegenAttrTernaryLogicalBooleanDefers proves `&&`/`||`-valued
// (non-ternary) boolean attributes stay out of this slice's scope and still
// defer, exactly as before this slice's edit.
func TestCodegenAttrTernaryLogicalBooleanDefers(t *testing.T) {
	cases := []string{
		`input(checked=(Enabled && Flag))`,
		`input(checked=(Enabled || Flag))`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			ast, err := Parse(src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderAt",
				DataType:        "atData",
				DataReflectType: atDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a deferral error for a logical-valued boolean attribute, got nil", src)
			}
		})
	}
}

// TestCodegenAttrTernaryNilReflectTypeDefers proves the pre-existing,
// untouched rootType==nil guard still rejects a ternary-valued boolean
// attribute exactly as it rejects every other dynamic attribute shape in
// type-blind mode.
func TestCodegenAttrTernaryNilReflectTypeDefers(t *testing.T) {
	src := `input(checked=(Enabled ? "checked" : false))`
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName: "gopug",
		FuncName:    "RenderAt",
		DataType:    "SkelData",
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-construct error with a nil DataReflectType, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}

// TestCodegenAttrTernaryRegressionExistingShapes proves every pre-existing
// dynamic/static boolean and non-boolean attribute shape — a comparison
// value, a bare bool field, a static value, and class — this slice must
// leave byte-for-byte untouched still renders identically to the
// interpreter, unaffected by the new ternary branch added ahead of the
// existing comparison/resolveFieldExpr code path.
func TestCodegenAttrTernaryRegressionExistingShapes(t *testing.T) {
	t.Parallel()
	cases := []atDiffCase{
		{
			name:        "comparison-valued boolean attr, true",
			src:         `input(checked=(Count == 0))`,
			data:        map[string]any{"Count": 0},
			dataLiteral: "atData{Count: 0}",
		},
		{
			name:        "comparison-valued boolean attr, false",
			src:         `input(checked=(Count == 0))`,
			data:        map[string]any{"Count": 5},
			dataLiteral: "atData{Count: 5}",
		},
		{
			name:        "bare bool field, boolean attr, true",
			src:         `input(checked=Enabled)`,
			data:        map[string]any{"Enabled": true},
			dataLiteral: "atData{Enabled: true}",
		},
		{
			name:        "bare bool field, boolean attr, false",
			src:         `input(checked=Enabled)`,
			data:        map[string]any{"Enabled": false},
			dataLiteral: "atData{Enabled: false}",
		},
		{
			name:        "bare field, non-boolean attr",
			src:         `div(data-x=Name)`,
			data:        map[string]any{"Name": "admin"},
			dataLiteral: `atData{Name: "admin"}`,
		},
		{
			name:        "static quoted attr",
			src:         `div(data-x="literal")`,
			data:        map[string]any{},
			dataLiteral: "atData{}",
		},
		{
			name:        "class attribute unaffected",
			src:         `div(class=(Flag ? "on" : "off"))`,
			data:        map[string]any{"Flag": true},
			dataLiteral: "atData{Flag: true}",
		},
	}
	results := runAtDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertAtDiffResult(t, tc.src, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenAttrTernaryFaultInjectionTrueNotOmitted asserts the first
// required fault injection: a WRONG expectation that OMITS the attribute
// when the ternary picks the non-"false" true branch must fail, proving
// TestCodegenAttrTernaryBooleanTrueRendersFalseOmits actually pins
// render-on-true, not just omit-on-false.
func TestCodegenAttrTernaryFaultInjectionTrueNotOmitted(t *testing.T) {
	results := runAtDifferentialBatch(t, []atDiffCase{
		{
			name:        `checked=(Enabled ? "checked" : false), Enabled true`,
			src:         `input(checked=(Enabled ? "checked" : false))`,
			data:        map[string]any{"Enabled": true},
			dataLiteral: "atData{Enabled: true}",
		},
	})
	got := assertAtDiffResult(t, `input(checked=(Enabled ? "checked" : false))`, "atData{Enabled: true}", results[0])
	wrongExpected := `<input>`
	if got == wrongExpected {
		t.Fatalf("fault injection failed to catch a wrong expectation: a TRUE ternary rendered %q, matching the WRONG (treat-true-branch-as-omitting) expectation instead of rendering the attribute", got)
	}
}

// TestCodegenAttrTernaryFaultInjectionFalseNotRendered asserts the second
// required fault injection: a WRONG expectation that RENDERS the attribute
// when the ternary picks the exact "false" branch must fail, proving the
// omit rule actually fires.
func TestCodegenAttrTernaryFaultInjectionFalseNotRendered(t *testing.T) {
	results := runAtDifferentialBatch(t, []atDiffCase{
		{
			name:        `checked=(Enabled ? "checked" : false), Enabled false`,
			src:         `input(checked=(Enabled ? "checked" : false))`,
			data:        map[string]any{"Enabled": false},
			dataLiteral: "atData{Enabled: false}",
		},
	})
	got := assertAtDiffResult(t, `input(checked=(Enabled ? "checked" : false))`, "atData{Enabled: false}", results[0])
	wrongExpected := `<input checked="false">`
	if got == wrongExpected {
		t.Fatalf("fault injection failed to catch a wrong expectation: the exact-\"false\" branch rendered %q, matching the WRONG (never-omit) expectation instead of omitting the attribute", got)
	}
}

// TestCodegenAttrTernaryFaultInjectionEmptyAndZeroNotOmitted asserts the
// third required fault injection: a WRONG expectation that OMITS the
// attribute when the chosen branch is the empty string or "0" must fail,
// proving the omit rule is gated on EXACT string equality with "false" and
// not on general falsiness.
func TestCodegenAttrTernaryFaultInjectionEmptyAndZeroNotOmitted(t *testing.T) {
	results := runAtDifferentialBatch(t, []atDiffCase{
		{
			name:        `checked=(Enabled ? "" : "x"), Enabled true (empty branch)`,
			src:         `input(checked=(Enabled ? "" : "x"))`,
			data:        map[string]any{"Enabled": true},
			dataLiteral: "atData{Enabled: true}",
		},
		{
			name:        `checked=(Enabled ? "0" : "x"), Enabled true ("0" branch)`,
			src:         `input(checked=(Enabled ? "0" : "x"))`,
			data:        map[string]any{"Enabled": true},
			dataLiteral: "atData{Enabled: true}",
		},
	})
	got0 := assertAtDiffResult(t, `input(checked=(Enabled ? "" : "x"))`, "atData{Enabled: true}", results[0])
	if got0 == `<input>` {
		t.Fatalf("fault injection failed to catch a wrong expectation: an empty-string branch rendered %q, matching the WRONG (general-falsiness-omits) expectation instead of rendering checked=\"\"", got0)
	}
	got1 := assertAtDiffResult(t, `input(checked=(Enabled ? "0" : "x"))`, "atData{Enabled: true}", results[1])
	if got1 == `<input>` {
		t.Fatalf("fault injection failed to catch a wrong expectation: a \"0\" branch rendered %q, matching the WRONG (general-falsiness-omits) expectation instead of rendering checked=\"0\"", got1)
	}
}
