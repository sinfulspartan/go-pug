package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// spreadAttrsData is the declared struct the general/dynamic `&attributes`
// differential tests resolve a runtime spread source against. Attrs is the
// map[string]string field this increment supports — its keys are unknown at
// generate time, so the merge, sortAttrNames ordering, and escaping all have
// to run at RUNTIME via gopug.WriteSpreadAttrs, unlike the fully static
// mixin-forwarding case codegen_mixin_attributes_test.go covers. AttrsAny
// and AttrsInt are map fields whose value kind ISN'T string, used to prove
// this increment's "only map[string]string" scope cut defers cleanly rather
// than guessing at output built from a boxed/differently-stringified value.
// Name is a plain string field, used to prove a dynamic (non-static) base
// attribute on a &attributes tag defers.
type spreadAttrsData struct {
	Attrs    map[string]string
	AttrsAny map[string]any
	AttrsInt map[string]int
	Name     string
}

var spreadAttrsReflectType = reflect.TypeOf(spreadAttrsData{})

// spreadAttrsDataStructSrc is spreadAttrsData's field declarations, spliced
// verbatim into the throwaway module runComposedGo assembles around a
// GenerateGo result — it must match spreadAttrsData above field for field.
const spreadAttrsDataStructSrc = `type spreadAttrsData struct {
	Attrs    map[string]string
	AttrsAny map[string]any
	AttrsInt map[string]int
	Name     string
}
`

// runSpreadDifferential parses src, generates it through GenerateGo against
// spreadAttrsData, builds and runs the result (via runComposedGo), separately
// renders it through the interpreter (Compile/Render) against data, and
// asserts the two outputs are byte-identical.
func runSpreadDifferential(t *testing.T, src string, data map[string]any, dataLiteral string) string {
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
	want, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runComposedGo(t, generated, spreadAttrsDataStructSrc, dataLiteral, "RenderSpread")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
	return got
}

// genSpreadErr parses and GenerateGoes src against spreadAttrsData (unless
// noType is true, in which case Config.DataReflectType is left nil), always
// returning GenerateGo's error rather than fatally failing the test.
func genSpreadErr(t *testing.T, src string, noType bool) error {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	cfg := Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsData",
		DataReflectType: spreadAttrsReflectType,
	}
	if noType {
		cfg.DataReflectType = nil
	}
	_, err = GenerateGo(ast, cfg)
	return err
}

// TestCodegenSpreadAttrsBasic proves the headline case (probe 1): a plain
// tag spreading a runtime map[string]string field renders every spread key,
// in sortAttrNames's id/class/rest-alphabetical order, byte-identically to
// Runtime.renderTag's own spread-merge.
func TestCodegenSpreadAttrsBasic(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"data-x": "1", "role": "btn"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"data-x": "1", "role": "btn"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsClassMerge proves a base "class" (a shorthand class
// token) is space-appended to, never overwritten by, a spread "class" key —
// base first (probe 2).
func TestCodegenSpreadAttrsClassMerge(t *testing.T) {
	src := "div.base&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"class": "extra"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"class": "extra"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsBoolTrue proves a spread value of "true" renders as
// a bare boolean attribute (probe 3, true half).
func TestCodegenSpreadAttrsBoolTrue(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"disabled": "true"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"disabled": "true"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsBoolFalse proves a spread value of "false" DELETES
// the attribute entirely, even when the tag's own base attribute of that
// name was itself a bare boolean (probe 3, false half).
func TestCodegenSpreadAttrsBoolFalse(t *testing.T) {
	src := "div(hidden)&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"hidden": "false"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"hidden": "false"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsOverwriteBase proves a non-"class" spread attribute
// completely overwrites a base attribute of the same name (probe 4).
func TestCodegenSpreadAttrsOverwriteBase(t *testing.T) {
	src := `div(data-x="base")&attributes(Attrs)` + "\n"
	data := map[string]any{"Attrs": map[string]string{"data-x": "new"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"data-x": "new"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsEscaping proves a spread value containing characters
// that must be HTML-escaped inside an attribute (`&`, `<`, `>`) is escaped
// identically to any other attribute value, through EscapeAttr (probe 5).
func TestCodegenSpreadAttrsEscaping(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"title": "a & b <c>"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"title": "a & b <c>"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsEmptyMap proves an empty spread map renders only the
// tag's own base attributes, no error (probe 6).
func TestCodegenSpreadAttrsEmptyMap(t *testing.T) {
	src := `div(id="x")&attributes(Attrs)` + "\n"
	data := map[string]any{"Attrs": map[string]string{}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsSortOrder pins the id/class/rest attribute order
// explicitly, mixing a base "other" attribute with spread "id", "class", and
// two "other" keys, so the resulting merged set spans all three
// sortAttrNames buckets from both sources and proves the ordering is
// resolved at RUNTIME (the spread's keys are not known at generate time —
// probe 7).
func TestCodegenSpreadAttrsSortOrder(t *testing.T) {
	src := `div(z2="base")&attributes(Attrs)` + "\n"
	data := map[string]any{"Attrs": map[string]string{"id": "i", "class": "c", "z": "z", "a": "a"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"id": "i", "class": "c", "z": "z", "a": "a"}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `id="i" class="c" a="a" z="z" z2="base"`) {
		t.Fatalf("output %q does not exhibit the expected id/class/rest order — this test's own pinned assumption about the runtime sort order is stale", got)
	}
}

// TestCodegenSpreadAttrsClassEmptyValue proves an ordinary empty spread
// class value contributes nothing to a merge with an existing base class —
// neither a trailing space nor an empty extra token — matching
// gopug.WriteSpreadAttrs's mergeSpreadClass rule and the (fixed)
// interpreter's own spread-merge.
func TestCodegenSpreadAttrsClassEmptyValue(t *testing.T) {
	src := "div.base&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"class": ""}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"class": ""}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsClassInternalWhitespace proves a spread class
// value's OWN internal whitespace (a double space) is preserved verbatim
// when merged with a base class — the merge treats the spread value as one
// opaque token, never re-tokenized or collapsed.
func TestCodegenSpreadAttrsClassInternalWhitespace(t *testing.T) {
	src := "div.base&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"class": "a  b"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"class": "a  b"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsClassLeadingTrailingWhitespace proves a spread class
// value's own leading/trailing whitespace is preserved verbatim (not
// trimmed) when there is no base class to merge with.
func TestCodegenSpreadAttrsClassLeadingTrailingWhitespace(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"class": " x "}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"class": " x "}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsClassNormal proves an ordinary single class merge
// (no unusual whitespace) is unaffected by the whitespace-preservation fix.
func TestCodegenSpreadAttrsClassNormal(t *testing.T) {
	src := "div.base&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"class": "extra"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"class": "extra"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsBaseClassWhitespaceDefers proves a base "class"
// literal with irregular (leading/trailing or repeated internal) whitespace
// is refused at GENERATE time rather than silently diverging, for a spread
// whose runtime map has no "class" key of its own. Whether the interpreter
// collapses that whitespace depends entirely on whether the runtime spread
// map happens to supply its own "class" key: when it does, both backends go
// through the shared mergeSpreadClass, which leaves whitespace untouched and
// agrees; when it does NOT (this test's shape), the interpreter's base class
// instead falls through to its ordinary (non-spread) render path,
// resolveClassTokenList, which Fields-collapses the whitespace — a
// generate-time-undecidable fork this generator cannot resolve, so it
// defers rather than guessing. The interpreter's own collapsed output is
// pinned here too, documenting the asymmetry a future reader might otherwise
// assume is a codegen bug rather than a deliberate scope cut.
func TestCodegenSpreadAttrsBaseClassWhitespaceDefers(t *testing.T) {
	src := `div(class="a  b")&attributes(Attrs)` + "\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Attrs": map[string]string{"data-x": "1"}})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != `<div class="a b" data-x="1"></div>` {
		t.Fatalf("interpreter output %q does not exhibit the expected Fields-collapsed class — this test's own pinned assumption is stale", want)
	}

	err = genSpreadErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "leading/trailing or repeated internal whitespace") {
		t.Errorf("GenerateGo error %q does not mention the irregular-whitespace base class deferral", err.Error())
	}
}

// TestCodegenSpreadAttrsBaseClassSingleTokenStillWorks proves a CLEAN base
// class (no irregular whitespace) combined with a spread that does not
// touch "class" is unaffected by the new deferral — it must still generate
// and render byte-identically to the interpreter, so the deferral above
// isn't over-broad.
func TestCodegenSpreadAttrsBaseClassSingleTokenStillWorks(t *testing.T) {
	src := "div.base&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"data-x": "1"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"data-x": "1"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsEmbeddedQuote proves a spread value containing a
// literal `"` is preserved and HTML-escaped, not silently lost — pinning
// the interpreter fix this helper was already correct for (WriteSpreadAttrs
// always escaped the raw value directly; the interpreter used to lose data
// by re-quoting the value and re-parsing it as if it were Pug source).
func TestCodegenSpreadAttrsEmbeddedQuote(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"title": `a"b`}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"title": "a\"b"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsBackslashValue proves a spread value containing a
// literal backslash is preserved verbatim (HTML attribute escaping does not
// touch backslashes).
func TestCodegenSpreadAttrsBackslashValue(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"title": `a\b`}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"title": "a\\b"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsAngleBracketValue proves a spread value containing
// `<` is HTML-escaped.
func TestCodegenSpreadAttrsAngleBracketValue(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"title": "a<b"}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"title": "a<b"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsQuoteInjectionStyleValue proves a value shaped like
// an attribute-injection attempt is fully escaped and stays inside its own
// attribute rather than breaking out into what would look like a second
// attribute — the security-relevant shape the interpreter fix specifically
// targets.
func TestCodegenSpreadAttrsQuoteInjectionStyleValue(t *testing.T) {
	src := "div&attributes(Attrs)\n"
	data := map[string]any{"Attrs": map[string]string{"title": `x" onmouseover="y`}}
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"title": "x\" onmouseover=\"y"}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if strings.Contains(got, `onmouseover="y"`) {
		t.Fatalf("spread value broke out of its attribute instead of being escaped, got: %q", got)
	}
}

// TestCodegenSpreadAttrsDeferrals collects every distinct clean error this
// increment's scope cut refuses, rather than guessing at.
func TestCodegenSpreadAttrsDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		noType  bool
		wantSub string
	}{
		{
			name:    "map[string]any source",
			src:     "div&attributes(AttrsAny)\n",
			wantSub: "map[string]string-typed",
		},
		{
			name:    "map[string]int source",
			src:     "div&attributes(AttrsInt)\n",
			wantSub: "map[string]string-typed",
		},
		{
			name:    "inline object &attributes({...})",
			src:     `div&attributes({x: "1"})` + "\n",
			wantSub: "inline object literal",
		},
		{
			name:    "dynamic class= base attribute on a spread tag",
			src:     "div(class=Name)&attributes(Attrs)\n",
			wantSub: "is dynamic",
		},
		{
			name:    "style-object base attribute on a spread tag",
			src:     `div(style={color: "red"})&attributes(Attrs)` + "\n",
			wantSub: "is dynamic",
		},
		{
			name:    "base class literal with irregular whitespace on a spread tag",
			src:     `div(class="a  b")&attributes(Attrs)` + "\n",
			wantSub: "leading/trailing or repeated internal whitespace",
		},
		{
			name:    "nil DataReflectType",
			src:     "div&attributes(Attrs)\n",
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

// TestCodegenSpreadAttrsFaultInjection proves the differential harness
// itself is non-vacuous for this feature: a deliberately WRONG expected
// value must fail the comparison.
func TestCodegenSpreadAttrsFaultInjection(t *testing.T) {
	src := "div.base&attributes(Attrs)\n"
	dataLiteral := `spreadAttrsData{Attrs: map[string]string{"class": "extra"}}`

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

	got := runComposedGo(t, generated, spreadAttrsDataStructSrc, dataLiteral, "RenderSpread")
	wrongWant := `<div class="wrong"></div>`
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenSpreadAttrsNonMapNonMixinStillDefers proves the pre-existing
// GENERAL `&attributes` deferral for a source that isn't resolvable at all
// (not a struct field, not a scope variable) still defers with a distinct,
// clean error, unperturbed by this increment's new map[string]string
// handling.
func TestCodegenSpreadAttrsNonMapNonMixinStillDefers(t *testing.T) {
	src := "div&attributes(NotAField)\n"
	err := genSpreadErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "&attributes") {
		t.Errorf("GenerateGo error %q does not mention &attributes", err.Error())
	}
}

// TestCodegenSpreadAttrsMixinAttributesVarStillRoutesThroughForwarding
// proves a mixin body's own `&attributes(attributes)` — the mixin-forwarding
// special case codegen_mixin_attributes_test.go covers — is unaffected by
// this increment: it is still handled entirely by the generate-time
// mergeForwardedAttributes path, not by the new runtime-merge helper this
// increment adds.
func TestCodegenSpreadAttrsMixinAttributesVarStillRoutesThroughForwarding(t *testing.T) {
	src := "mixin box()\n  div.base&attributes(attributes)\n+box()(class=\"b\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}
