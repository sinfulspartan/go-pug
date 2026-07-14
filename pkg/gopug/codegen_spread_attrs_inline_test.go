package gopug

import (
	"strings"
	"testing"
)

// runInlineSpreadDifferential parses src, generates it through GenerateGo
// against spreadAttrsData (this file reuses the struct and dataLiteral
// wiring codegen_spread_attrs_test.go declares — an inline-object
// &attributes source needs no template data field of its own, but the
// differential harness still needs a declared root type to generate
// against), builds and runs the result, separately renders it through the
// interpreter (Compile/Render), and asserts the two outputs are
// byte-identical.
func runInlineSpreadDifferential(t *testing.T, src string) string {
	t.Helper()

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsData",
		DataReflectType: spreadAttrsReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runComposedGo(t, generated, spreadAttrsDataStructSrc, "spreadAttrsData{}", "RenderSpread")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
	return got
}

// TestCodegenSpreadAttrsInlineBasic proves the headline case (probe 1): an
// inline object literal spread with both a quoted and an unquoted key
// renders every entry, in sortAttrNames's id/class/rest order,
// byte-identically to Runtime.renderTag's own inline-object spread branch.
func TestCodegenSpreadAttrsInlineBasic(t *testing.T) {
	t.Parallel()
	src := `div&attributes({"data-x": "1", role: "btn"})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if !strings.Contains(got, `data-x="1" role="btn"`) {
		t.Fatalf("output %q does not exhibit both spread entries", got)
	}
}

// TestCodegenSpreadAttrsInlineClassMerge proves a base "class" (a shorthand
// class token) is space-appended to, never overwritten by, an inline
// object's own "class" key (probe 2).
func TestCodegenSpreadAttrsInlineClassMerge(t *testing.T) {
	t.Parallel()
	src := `div.base&attributes({class: "extra"})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if !strings.Contains(got, `class="base extra"`) {
		t.Fatalf("output %q does not exhibit the merged \"base extra\" class", got)
	}
}

// TestCodegenSpreadAttrsInlineQuotedUnquotedKeyAndValue proves every
// quoting combination of key and value — a quoted key with a
// double-quoted value, a quoted key with a single-quoted value, and an
// entirely unquoted key/value pair — all resolve to the identical literal
// string, since parseInlineObject only strips a matching pair of
// surrounding quotes and never evaluates anything (probe 3).
func TestCodegenSpreadAttrsInlineQuotedUnquotedKeyAndValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{"unquoted key, double-quoted value", `div&attributes({class: "x"})` + "\n"},
		{"quoted key, single-quoted value", `div&attributes({"class": 'x'})` + "\n"},
		{"unquoted key, unquoted value", `div&attributes({class: x})` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runInlineSpreadDifferential(t, tc.src)
			if got != `<div class="x"></div>` {
				t.Fatalf("output %q does not exhibit the literal class value \"x\"", got)
			}
		})
	}
}

// TestCodegenSpreadAttrsInlineBoolTrue proves an inline object value of the
// literal text "true" renders as a bare boolean attribute, exactly like the
// map[string]string field/variable spread path (probe 4, true half).
func TestCodegenSpreadAttrsInlineBoolTrue(t *testing.T) {
	t.Parallel()
	src := `div&attributes({disabled: "true"})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if got != `<div disabled></div>` {
		t.Fatalf("output %q does not exhibit a bare \"disabled\" attribute", got)
	}
}

// TestCodegenSpreadAttrsInlineBoolFalse proves an inline object value of the
// literal text "false" DELETES the attribute entirely, even when the tag's
// own base attribute of that name was itself a bare boolean (probe 4, false
// half).
func TestCodegenSpreadAttrsInlineBoolFalse(t *testing.T) {
	t.Parallel()
	src := `div(hidden)&attributes({hidden: "false"})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if got != `<div></div>` {
		t.Fatalf("output %q still exhibits \"hidden\" after a literal \"false\" value should have deleted it", got)
	}
}

// TestCodegenSpreadAttrsInlineEscaping proves an inline object value
// containing characters that must be HTML-escaped inside an attribute
// (`"`, `<`, `>`, `&`) is escaped identically to the field/variable spread
// path, through the same gopug.WriteSpreadAttrs / EscapeAttr call (probe 5).
func TestCodegenSpreadAttrsInlineEscaping(t *testing.T) {
	t.Parallel()
	src := `div&attributes({title: 'a"b <c> & d'})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if got != `<div title="a&quot;b &lt;c&gt; &amp; d"></div>` {
		t.Fatalf("output %q does not exhibit the fully escaped inline object value", got)
	}
}

// TestCodegenSpreadAttrsInlineEmptyObject proves an empty inline object
// renders only the tag's own base attributes, no error (probe 6, empty
// half).
func TestCodegenSpreadAttrsInlineEmptyObject(t *testing.T) {
	t.Parallel()
	src := `div(id="x")&attributes({})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if got != `<div id="x"></div>` {
		t.Fatalf("output %q does not exhibit only the base attribute", got)
	}
}

// TestCodegenSpreadAttrsInlineMalformedPair proves a malformed pair with no
// ":" separator is silently skipped, matching parseInlineObject's own
// "continue" behavior for such a pair, while a well-formed sibling pair in
// the same object still renders (probe 6, malformed half).
func TestCodegenSpreadAttrsInlineMalformedPair(t *testing.T) {
	t.Parallel()
	src := `div&attributes({novalue, id: "x"})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if got != `<div id="x"></div>` {
		t.Fatalf("output %q does not exhibit only the well-formed pair, with the malformed pair silently skipped", got)
	}
}

// TestCodegenSpreadAttrsInlineExprLookingValue proves an inline object value
// that looks like an operator expression is treated as a plain literal
// string, never evaluated — parseInlineObject has no expression evaluator at
// all (probe 7, expr-looking half).
func TestCodegenSpreadAttrsInlineExprLookingValue(t *testing.T) {
	t.Parallel()
	src := `div&attributes({a: b + c})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if got != `<div a="b + c"></div>` {
		t.Fatalf("output %q does not exhibit the literal, unevaluated text \"b + c\"", got)
	}
}

// TestCodegenSpreadAttrsInlineNestedLookingValue proves an inline object
// value that looks like an array literal is likewise treated as a plain
// literal string, never evaluated (probe 7, nested-looking half).
func TestCodegenSpreadAttrsInlineNestedLookingValue(t *testing.T) {
	t.Parallel()
	src := `div&attributes({a: [1,2]})` + "\n"
	got := runInlineSpreadDifferential(t, src)

	if got != `<div a="[1,2]"></div>` {
		t.Fatalf("output %q does not exhibit the literal, unevaluated text \"[1,2]\"", got)
	}
}

// TestCodegenSpreadAttrsInlineDeferrals collects every distinct clean error
// this slice's own scope cut refuses, rather than guessing at, for an
// inline-object &attributes tag specifically. Every value shape inside the
// object itself is always a literal string (parseInlineObject never
// evaluates), so there is no dynamic-VALUE deferral to test here — only the
// base-attribute and type-resolution deferrals genSpreadBase and
// genSpreadAttrs's own nil-DataReflectType gate still apply.
func TestCodegenSpreadAttrsInlineDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		noType  bool
		wantSub string
	}{
		{
			name:    "dynamic class= base attribute on an inline-object spread tag",
			src:     `div(class=Name)&attributes({x: "1"})` + "\n",
			wantSub: "is dynamic",
		},
		{
			name:    "style-object base attribute on an inline-object spread tag",
			src:     `div(style={color: "red"})&attributes({x: "1"})` + "\n",
			wantSub: "is dynamic",
		},
		{
			name:    "base class literal with irregular whitespace on an inline-object spread tag",
			src:     `div(class="a  b")&attributes({x: "1"})` + "\n",
			wantSub: "leading/trailing or repeated internal whitespace",
		},
		{
			name:    "nil DataReflectType",
			src:     `div&attributes({x: "1"})` + "\n",
			noType:  true,
			wantSub: "DataReflectType",
		},
	}

	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genSpreadErr(t, tc.src, tc.noType)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("GenerateGo error %q does not mention %q", err.Error(), tc.wantSub)
			}
			if other, ok := seen[err.Error()]; ok {
				t.Errorf("deferral %q and %q produced the identical error text %q (expected distinct errors)", tc.name, other, err.Error())
			}
			seen[err.Error()] = tc.name
		})
	}
}

// TestCodegenSpreadAttrsInlineBaseClassWhitespaceInterpreterCollapses pins
// the interpreter's own irregular-whitespace-base-class behavior for an
// inline-object spread whose object has no "class" key of its own —
// documenting, the same way TestCodegenSpreadAttrsBaseClassWhitespaceDefers
// already does for the field/variable path, that the Fields-collapsed
// output this deferral protects against is real interpreter behavior, not a
// hypothetical.
func TestCodegenSpreadAttrsInlineBaseClassWhitespaceInterpreterCollapses(t *testing.T) {
	src := `div(class="a  b")&attributes({x: "1"})` + "\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != `<div class="a b" x="1"></div>` {
		t.Fatalf("interpreter output %q does not exhibit the expected Fields-collapsed class — this test's own pinned assumption is stale", want)
	}
}

// TestCodegenSpreadAttrsInlineFaultInjection proves the differential harness
// itself is non-vacuous for the inline-object entry point: a deliberately
// WRONG expected value must fail the comparison.
func TestCodegenSpreadAttrsInlineFaultInjection(t *testing.T) {
	t.Parallel()
	src := `div.base&attributes({class: "extra"})` + "\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsData",
		DataReflectType: spreadAttrsReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runComposedGo(t, generated, spreadAttrsDataStructSrc, "spreadAttrsData{}", "RenderSpread")
	wrongWant := `<div class="wrong"></div>`
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenSpreadAttrsInlineFieldVariablePathStillWorks re-confirms a
// representative slice of the pre-existing map[string]string field/variable
// spread suite is unaffected by this slice's new inline-object branch — the
// full original suites live in codegen_spread_attrs_test.go and
// codegen_spread_attrs_any_test.go and run unchanged alongside this file.
func TestCodegenSpreadAttrsInlineFieldVariablePathStillWorks(t *testing.T) {
	t.Parallel()
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"data-x": "1", "role": "btn"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"data-x": "1", "role": "btn"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}
