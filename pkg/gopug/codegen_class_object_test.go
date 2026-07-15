package gopug

import (
	"strings"
	"testing"
)

// classObjDiffCase is one runClassObjectDifferentialBatch case: src is a full
// Pug source (a single tag with a dynamic class-object attribute, typically),
// data is the interpreter oracle's render input, and dataLiteral is an
// opsData composite literal describing the same data for the generated
// code's differential run.
type classObjDiffCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

// runClassObjectDifferentialBatch is conditionDiffCase's batch machinery
// (runConditionDifferentialBatch) reused for a class-object src that is not
// wrapped in an `if`/`else` shell: every case's GenerateGo output and
// interpreter oracle (Compile/Render) are prepared up front and submitted to
// a single runDifferentialBatch call, returning each case's (out, want, err)
// triple index-matched to cases.
func runClassObjectDifferentialBatch(t *testing.T, cases []classObjDiffCase) []conditionDiffResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	type prepared struct {
		c    classObjDiffCase
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

// genClassObjectErr parses and GenerateGoes src against opsData (unless
// noType is true, in which case Config.DataReflectType is left nil), always
// returning GenerateGo's error rather than fatally failing the test — used
// by the deferral cases below.
func genClassObjectErr(t *testing.T, src string, noType bool) error {
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

// TestCodegenClassObjectSortReorder proves a class-object's active
// keys are joined in SORTED order, not source order, matching
// Runtime.renderTag's own sort.Strings(activeClasses) — "zebra" is declared
// before "apple" in the object literal but "apple" must appear first in the
// rendered class list. Because genDynamicClassObject bakes its `if` checks
// into the generated Go source in sorted key order at GENERATE time, the
// emitted class string is fully deterministic (no runtime map iteration is
// ever involved), so asserting equality with the sorted expected — and
// inequality with the source-order string a missing sort would produce — is
// not a flaky, iteration-order-dependent check.
func TestCodegenClassObjectSortReorder(t *testing.T) {
	t.Parallel()
	src := "div(class={zebra: Flag, apple: FlagB})\n"
	results := runClassObjectDifferentialBatch(t, []classObjDiffCase{
		{
			name:        "zebra/apple both true",
			src:         src,
			data:        map[string]any{"Flag": true, "FlagB": true},
			dataLiteral: "opsData{Flag: true, FlagB: true}",
		},
	})
	got := assertConditionDiffResult(t, src, "opsData{Flag: true, FlagB: true}", results[0])

	const want = `<div class="apple zebra"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want sorted %q", got, want)
	}
	// Fault injection: the WRONG, unsorted (source-order) expectation must
	// not match — proving the sort actually reorders the keys rather than
	// happening to already be sorted.
	const wrongUnsorted = `<div class="zebra apple"></div>`
	if got == wrongUnsorted {
		t.Fatalf("codegen output %q matches the unsorted source-order string; the key sort is not being applied", got)
	}
}

// TestCodegenClassObjectStaticPrefixTruthyFalsy proves a static
// shorthand prefix before the object literal (`div.card(class={active:
// isActive})`) is space-prepended to the joined active-class list exactly
// like Runtime.renderTag's own `evaluated = prefix + " " + evaluated` —
// including, when the object evaluates to no active classes at all, the
// resulting LONE TRAILING SPACE after the prefix (`class="card "`, not
// `class="card"`).
func TestCodegenClassObjectStaticPrefixTruthyFalsy(t *testing.T) {
	t.Parallel()
	src := "div.card(class={active: Flag})\n"
	results := runClassObjectDifferentialBatch(t, []classObjDiffCase{
		{name: "truthy", src: src, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "falsy", src: src, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
	})

	gotTruthy := assertConditionDiffResult(t, src, "opsData{Flag: true}", results[0])
	if gotTruthy != `<div class="card active"></div>` {
		t.Fatalf("truthy case: codegen output %q, want %q", gotTruthy, `<div class="card active"></div>`)
	}

	gotFalsy := assertConditionDiffResult(t, src, "opsData{Flag: false}", results[1])
	const wantFalsy = `<div class="card "></div>`
	if gotFalsy != wantFalsy {
		t.Fatalf("falsy case: codegen output %q, want %q (with the trailing space after the prefix)", gotFalsy, wantFalsy)
	}
	// Fault injection: the WRONG expectation that drops the trailing space
	// must not match — proving the quirk (prefix+" "+"" when nothing is
	// active) is reproduced byte for byte, not trimmed away.
	const wrongNoTrailingSpace = `<div class="card"></div>`
	if gotFalsy == wrongNoTrailingSpace {
		t.Fatalf("falsy case: codegen output %q matches the no-trailing-space string; the prefix quirk is not being reproduced", gotFalsy)
	}
}

// TestCodegenClassObjectAllFalseEmptyClass proves that with no static
// prefix and every entry falsy, the rendered class attribute is present but
// empty (`class=""`), matching Runtime.renderTag's own empty-join result.
func TestCodegenClassObjectAllFalseEmptyClass(t *testing.T) {
	t.Parallel()
	src := "div(class={a: Flag, b: FlagB})\n"
	results := runClassObjectDifferentialBatch(t, []classObjDiffCase{
		{name: "both false", src: src, data: map[string]any{"Flag": false, "FlagB": false}, dataLiteral: "opsData{}"},
	})
	got := assertConditionDiffResult(t, src, "opsData{}", results[0])
	if got != `<div class=""></div>` {
		t.Fatalf("codegen output %q, want %q", got, `<div class=""></div>`)
	}
}

// TestCodegenClassObjectEscaping proves the class-object's
// rendered result is escaped through EscapeAttr, not written raw — an
// active key containing an HTML-attribute-sensitive character comes out
// entity-escaped exactly like Runtime.renderTag's own htmlEscapeAttr call.
func TestCodegenClassObjectEscaping(t *testing.T) {
	t.Parallel()
	src := `div(class={"a&b": Flag})` + "\n"
	results := runClassObjectDifferentialBatch(t, []classObjDiffCase{
		{name: "ampersand key", src: src, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
	})
	got := assertConditionDiffResult(t, src, "opsData{Flag: true}", results[0])
	const want = `<div class="a&amp;b"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (the \"&\" must be entity-escaped)", got, want)
	}
}

// TestCodegenClassObjectSupportedValueForms proves every value
// shape genCondition's own supported grammar covers — a bare bool field, a
// numeric comparison, a `&&` logical combination, and a bare string field
// (isTruthy of its stringified value) — is compiled into the entry's `if`
// check byte-identically to the interpreter's isTruthy(evaluateExpr(v)).
func TestCodegenClassObjectSupportedValueForms(t *testing.T) {
	t.Parallel()
	cases := []classObjDiffCase{
		{
			name:        "bool field true",
			src:         "div(class={active: Flag})\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "bool field false",
			src:         "div(class={active: Flag})\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
		{
			name:        "comparison truthy",
			src:         "div(class={active: Count > 0})\n",
			data:        map[string]any{"Count": 5},
			dataLiteral: "opsData{Count: 5}",
		},
		{
			name:        "comparison falsy",
			src:         "div(class={active: Count > 0})\n",
			data:        map[string]any{"Count": 0},
			dataLiteral: "opsData{Count: 0}",
		},
		{
			name:        "logical && truthy",
			src:         "div(class={on: Flag && FlagB})\n",
			data:        map[string]any{"Flag": true, "FlagB": true},
			dataLiteral: "opsData{Flag: true, FlagB: true}",
		},
		{
			name:        "logical && falsy",
			src:         "div(class={on: Flag && FlagB})\n",
			data:        map[string]any{"Flag": true, "FlagB": false},
			dataLiteral: "opsData{Flag: true, FlagB: false}",
		},
		{
			name:        "string field truthy",
			src:         "div(class={active: Name})\n",
			data:        map[string]any{"Name": "x"},
			dataLiteral: `opsData{Name: "x"}`,
		},
		{
			name:        "string field falsy empty",
			src:         "div(class={active: Name})\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
	}

	results := runClassObjectDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.src, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenClassObjectDeferrals proves each distinct DEFER path: a
// fallible entry value (`Count/Zero`, mirroring the interpreter's own
// evaluateExpr-errors-so-the-key-is-silently-dropped fallback, which a
// generated function that either compiles or refuses cannot reproduce), an
// entry value genCondition rejects outright (a non-scalar struct field), the
// still-deferred array-literal class form (`class=[...]`, unaffected by this
// increment), and a nil Config.DataReflectType (type-blind mode, checked
// before the object is even inspected) — each returns a distinct descriptive
// "unsupported" error rather than guessing at output.
func TestCodegenClassObjectDeferrals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		src    string
		noType bool
	}{
		{name: "fallible entry value", src: "div(class={active: Count/Zero})\n"},
		{name: "condition-generator-unsupported entry value (non-scalar field)", src: "div(class={active: User})\n"},
		{name: "array-literal class value (unchanged defer)", src: "div(class=[Name, Name])\n"},
		{name: "nil rootType", src: "div(class={active: Flag})\n", noType: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genClassObjectErr(t, tc.src, tc.noType)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenClassObjectDeferralsAreDistinct proves the four deferral cases
// above produce genuinely different error messages, not one shared catch-all
// — each names the actual construct that made codegen refuse (the entry
// value, the field, or the class-value shape), not just a generic
// "unsupported class attribute" string.
func TestCodegenClassObjectDeferralsAreDistinct(t *testing.T) {
	t.Parallel()
	fallible := genClassObjectErr(t, "div(class={active: Count/Zero})\n", false)
	unsupportedField := genClassObjectErr(t, "div(class={active: User})\n", false)
	arrayLit := genClassObjectErr(t, "div(class=[Name, Name])\n", false)
	nilType := genClassObjectErr(t, "div(class={active: Flag})\n", true)

	msgs := map[string]error{
		"fallible":          fallible,
		"unsupported field": unsupportedField,
		"array literal":     arrayLit,
		"nil rootType":      nilType,
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

// TestCodegenClassObjectRegressionFieldTokenPathUnchanged proves the
// pre-existing supported dynamic-class path — a static shorthand prefix
// merged with a bare string-field token, joined through the exported
// JoinClasses (genDynamicClass's Fields-tokenized branch) — is completely
// unaffected by the new object-literal detection genDynamicClass now
// performs before its Fields split.
func TestCodegenClassObjectRegressionFieldTokenPathUnchanged(t *testing.T) {
	t.Parallel()
	src := "div.base(class=Name)\n"
	results := runClassObjectDifferentialBatch(t, []classObjDiffCase{
		{name: "shorthand + bare string field", src: src, data: map[string]any{"Name": "extra"}, dataLiteral: `opsData{Name: "extra"}`},
	})
	got := assertConditionDiffResult(t, src, `opsData{Name: "extra"}`, results[0])
	if got != `<div class="base extra"></div>` {
		t.Fatalf("codegen output %q, want %q", got, `<div class="base extra"></div>`)
	}
}

// TestCodegenClassObjectRegressionSurvivingDefersUnchanged proves the two
// other class-value defers this increment leaves untouched — a
// ternary/operator class expression and an array-literal class value — still
// return their own pre-existing "unsupported" errors, confirming
// genDynamicClass's object-literal branch is reached only for the `{`
// shape and never intercepts these.
func TestCodegenClassObjectRegressionSurvivingDefersUnchanged(t *testing.T) {
	t.Parallel()
	cases := []string{
		`div(class=Count > 0 ? "a" : "b")` + "\n",
		"div(class=[Name, Name])\n",
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			err := genClassObjectErr(t, src, false)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
			}
		})
	}
}

// TestCodegenClassObjectImportGating proves a tag using ONLY a class-object
// attribute compiles with exactly the imports it needs: "gopug" (for
// EscapeAttr) and "strings" (for strings.Join), but not "html"/"strconv"/
// "fmt", which nothing in this template's generated code calls.
func TestCodegenClassObjectImportGating(t *testing.T) {
	t.Parallel()
	src := "div(class={active: Flag})\n"
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
	if !strings.Contains(genStr, "\"strings\"") {
		t.Errorf("GenerateGo(%q) does not import \"strings\" even though it calls strings.Join:\n%s", src, genStr)
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
	want, err := tmpl.Render(map[string]any{"Flag": true})
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, "opsData{Flag: true}")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}
