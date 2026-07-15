package gopug

import (
	"strings"
	"testing"
)

// classTernaryDiffCase is one runClassTernaryDifferentialBatch case: src is a
// full Pug source (a single tag with a fully-parenthesized dynamic class
// attribute, typically), data is the interpreter oracle's render input, and
// dataLiteral is an opsData composite literal describing the same data for
// the generated code's differential run.
type classTernaryDiffCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

// runClassTernaryDifferentialBatch is runClassObjectDifferentialBatch's
// machinery reused for the fully-parenthesized class-value form: every
// case's GenerateGo output and interpreter oracle (Compile/Render) are
// prepared up front and submitted to a single runDifferentialBatch call,
// returning each case's (out, want, err) triple index-matched to cases.
func runClassTernaryDifferentialBatch(t *testing.T, cases []classTernaryDiffCase) []conditionDiffResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	type prepared struct {
		c    classTernaryDiffCase
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
			FuncName:        "RenderOps",
			DataType:        "opsData",
			DataReflectType: opsDataReflectType,
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

	batchResults := runDifferentialBatch(t, opsDataStructSrc, "RenderOps", diffCases)

	results := make([]conditionDiffResult, len(cases))
	for i, p := range prep {
		r := batchResults[i]
		results[i] = conditionDiffResult{out: r.Out, want: p.want, err: r.Err}
	}
	return results
}

// genClassTernaryErr parses and GenerateGoes src against opsData (unless
// noType is true, in which case Config.DataReflectType is left nil), always
// returning GenerateGo's error rather than fatally failing the test — used
// by the deferral cases below.
func genClassTernaryErr(t *testing.T, src string, noType bool) error {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	cfg := Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	}
	if noType {
		cfg.DataReflectType = nil
	}
	_, err = GenerateGo(ast, cfg)
	return err
}

// TestCodegenClassParenTernaryBranches proves both branches of a
// fully-parenthesized whole-value ternary class (`class=(cond ? "a" : "b")`)
// render byte-identically to the interpreter, including the false branch
// resolving to an empty string (`class=""`, not an omitted attribute).
func TestCodegenClassParenTernaryBranches(t *testing.T) {
	t.Parallel()
	trueFalseSrc := `div(class=(Flag ? "active" : "inactive"))` + "\n"
	emptyFalseSrc := `div(class=(Flag ? "active" : ""))` + "\n"
	cases := []classTernaryDiffCase{
		{name: "true branch", src: trueFalseSrc, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "false branch", src: trueFalseSrc, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
		{name: "empty false branch", src: emptyFalseSrc, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
		{name: "empty false branch, true taken", src: emptyFalseSrc, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="active"></div>` {
		t.Fatalf("true branch: codegen output %q, want %q", got, `<div class="active"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class="inactive"></div>` {
		t.Fatalf("false branch: codegen output %q, want %q", got, `<div class="inactive"></div>`)
	}
	got = assertConditionDiffResult(t, cases[2].src, cases[2].dataLiteral, results[2])
	if got != `<div class=""></div>` {
		t.Fatalf("empty false branch: codegen output %q, want %q", got, `<div class=""></div>`)
	}
	got = assertConditionDiffResult(t, cases[3].src, cases[3].dataLiteral, results[3])
	if got != `<div class="active"></div>` {
		t.Fatalf("empty false branch, true taken: codegen output %q, want %q", got, `<div class="active"></div>`)
	}
}

// TestCodegenClassParenOperatorConcat proves a fully-parenthesized `+`
// concatenation class value (`class=(prefix + "-btn")`) renders
// byte-identically to the interpreter's gopug.Add-equivalent concatenation.
func TestCodegenClassParenOperatorConcat(t *testing.T) {
	t.Parallel()
	src := `div(class=(Name + "-btn"))` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{name: "concat", src: src, data: map[string]any{"Name": "x"}, dataLiteral: `opsData{Name: "x"}`},
	})
	got := assertConditionDiffResult(t, src, `opsData{Name: "x"}`, results[0])
	if got != `<div class="x-btn"></div>` {
		t.Fatalf("codegen output %q, want %q", got, `<div class="x-btn"></div>`)
	}
}

// TestCodegenClassParenEscaping proves a fully-parenthesized class value's
// rendered result is escaped through EscapeAttr, not written raw — a bare
// field value containing HTML-attribute-sensitive characters (including a
// literal double quote) comes out entity-escaped exactly like
// Runtime.renderTag's own htmlEscapeAttr call, which escapes `"` as `&quot;`.
func TestCodegenClassParenEscaping(t *testing.T) {
	t.Parallel()
	src := `div(class=(Name))` + "\n"
	raw := `a&b"c`
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{name: "escaped", src: src, data: map[string]any{"Name": raw}, dataLiteral: `opsData{Name: "a&b\"c"}`},
	})
	got := assertConditionDiffResult(t, src, `opsData{Name: "a&b\"c"}`, results[0])
	const want = `<div class="a&amp;b&quot;c"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (the \"&\" and the quote must be entity-escaped)", got, want)
	}
	// Fault injection: the WRONG expectation using the raw, unescaped inner
	// value must NOT match — proving EscapeAttr is actually applied, not
	// dropped, by the parenthesized-class path.
	wrongUnescaped := `<div class="` + raw + `"></div>`
	if got == wrongUnescaped {
		t.Fatalf("codegen output %q matches the raw unescaped string %q; EscapeAttr is not being applied", got, wrongUnescaped)
	}
}

// TestCodegenClassParenLogicalAndComparison proves the value forms
// genValueExpr already supports for a value expression in general —
// a `&&` logical combination and a ternary whose condition is a numeric
// comparison — also work when the whole thing is wrapped in one pair of
// parens as a class value, matching the interpreter byte for byte
// (including the `&&` operator's own quirk of yielding the literal string
// "false", not an omitted/empty attribute, when its left operand is falsy).
func TestCodegenClassParenLogicalAndComparison(t *testing.T) {
	t.Parallel()
	andSrc := `div(class=(Flag && "on"))` + "\n"
	cmpSrc := `div(class=(Count > 0 ? "has" : "none"))` + "\n"
	cases := []classTernaryDiffCase{
		{name: "&& truthy", src: andSrc, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "&& falsy", src: andSrc, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
		{name: "comparison truthy", src: cmpSrc, data: map[string]any{"Count": 5}, dataLiteral: "opsData{Count: 5}"},
		{name: "comparison falsy", src: cmpSrc, data: map[string]any{"Count": 0}, dataLiteral: "opsData{Count: 0}"},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="on"></div>` {
		t.Fatalf("&& truthy: codegen output %q, want %q", got, `<div class="on"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class="false"></div>` {
		t.Fatalf("&& falsy: codegen output %q, want %q", got, `<div class="false"></div>`)
	}
	got = assertConditionDiffResult(t, cases[2].src, cases[2].dataLiteral, results[2])
	if got != `<div class="has"></div>` {
		t.Fatalf("comparison truthy: codegen output %q, want %q", got, `<div class="has"></div>`)
	}
	got = assertConditionDiffResult(t, cases[3].src, cases[3].dataLiteral, results[3])
	if got != `<div class="none"></div>` {
		t.Fatalf("comparison falsy: codegen output %q, want %q", got, `<div class="none"></div>`)
	}
}

// TestCodegenClassParenDeferrals proves each distinct DEFER path this
// increment must NOT try to reproduce: a fully-parenthesized but fallible
// inner value (`(Count/Zero)`, mirroring the interpreter's own
// evaluateExpr-errors-to-"" fallback, which an aborting extraction can't
// reproduce), the two static-prefix-mixed forms that are the central safety
// case (a dot-shorthand class prefix merged with a parenthesized ternary,
// and the same prefix merged with an UNparenthesized ternary — both take the
// interpreter's own heuristic word-splitting fallback, not reproduced here),
// a value that starts with `(` and ends with `)` but is NOT fully
// parenthesized because a top-level operator follows the first paren group
// (`(Name) + FlagB`, still caught by the pre-existing isOperatorExpr defer),
// an unparenthesized bare ternary/operator (the pre-existing defer,
// unaffected by this increment), and a nil Config.DataReflectType.
func TestCodegenClassParenDeferrals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		src    string
		noType bool
	}{
		{name: "fully-parenthesized fallible inner", src: "div(class=(Count/Zero))\n"},
		{name: "static-prefix-mixed, parenthesized ternary (dot-shorthand)", src: `div.card(class=(Flag ? "x":"y"))` + "\n"},
		{name: "static-prefix-mixed, unparenthesized ternary (dot-shorthand)", src: `div.card(class=Flag ? "x":"")` + "\n"},
		{name: "paren-prefix with trailing top-level operator", src: "div(class=(Name) + FlagB)\n"},
		{name: "unparenthesized ternary (unchanged defer)", src: `div(class=Flag ? "a" : "b")` + "\n"},
		{name: "unparenthesized plus (unchanged defer)", src: "div(class=Name + Slug)\n"},
		{name: "nil rootType", src: `div(class=(Flag ? "a" : "b"))` + "\n", noType: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genClassTernaryErr(t, tc.src, tc.noType)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenClassParenDeferralsAreDistinct proves the deferral cases above
// produce genuinely different error messages, not one shared catch-all —
// the fallible-inner, static-prefix-mixed, and nil-rootType deferrals each
// name their own reason for refusing.
func TestCodegenClassParenDeferralsAreDistinct(t *testing.T) {
	t.Parallel()
	fallible := genClassTernaryErr(t, "div(class=(Count/Zero))\n", false)
	mixedParen := genClassTernaryErr(t, `div.card(class=(Flag ? "x":"y"))`+"\n", false)
	unparenthesizedTernary := genClassTernaryErr(t, `div(class=Flag ? "a" : "b")`+"\n", false)
	nilType := genClassTernaryErr(t, `div(class=(Flag ? "a" : "b"))`+"\n", true)

	msgs := map[string]error{
		"fallible inner":          fallible,
		"static-prefix-mixed":     mixedParen,
		"unparenthesized ternary": unparenthesizedTernary,
		"nil rootType":            nilType,
	}
	seen := map[string]string{}
	for name, err := range msgs {
		if err == nil {
			t.Fatalf("%s: expected an error, got nil", name)
		}
		if other, dup := seen[err.Error()]; dup {
			t.Errorf("%s and %s produced the identical error message %q; expected each deferral to describe its own construct", name, other, err.Error())
		}
		seen[err.Error()] = name
	}
}

// TestCodegenClassParenRegressionUnchangedPaths proves the pre-existing
// class-value paths this increment must leave untouched still work exactly
// as before: the class-object literal path, and the Fields-split
// shorthand-prefix + bare-string-field path.
func TestCodegenClassParenRegressionUnchangedPaths(t *testing.T) {
	t.Parallel()
	objSrc := "div(class={active: Flag})\n"
	fieldSrc := "div.base(class=Name)\n"
	cases := []classTernaryDiffCase{
		{name: "class-object literal", src: objSrc, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "Fields-split shorthand + bare string field", src: fieldSrc, data: map[string]any{"Name": "extra"}, dataLiteral: `opsData{Name: "extra"}`},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="active"></div>` {
		t.Fatalf("class-object: codegen output %q, want %q", got, `<div class="active"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class="base extra"></div>` {
		t.Fatalf("Fields-split: codegen output %q, want %q", got, `<div class="base extra"></div>`)
	}
}

// TestCodegenClassParenImportGating proves a tag using ONLY a
// parenthesized-expression class attribute compiles with exactly the
// imports it needs: "gopug" (for EscapeAttr), but not "html"/"strconv"/
// "fmt"/"strings", which nothing in this template's generated code calls.
func TestCodegenClassParenImportGating(t *testing.T) {
	t.Parallel()
	src := `div(class=(Name))` + "\n"
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
	genStr := string(generated)

	if !strings.Contains(genStr, `"github.com/sinfulspartan/go-pug/pkg/gopug"`) {
		t.Errorf("GenerateGo(%q) does not import \"gopug\" even though it calls gopug.EscapeAttr:\n%s", src, genStr)
	}
	if strings.Contains(genStr, "\"html\"") {
		t.Errorf("GenerateGo(%q) imports \"html\" even though nothing in the template calls html.EscapeString:\n%s", src, genStr)
	}
	if strings.Contains(genStr, "\"strconv\"") {
		t.Errorf("GenerateGo(%q) imports \"strconv\" even though nothing in the template needs it:\n%s", src, genStr)
	}
	if strings.Contains(genStr, "\"fmt\"") {
		t.Errorf("GenerateGo(%q) imports \"fmt\" even though nothing in the template needs it:\n%s", src, genStr)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Name": "x"})
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, `opsData{Name: "x"}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}
