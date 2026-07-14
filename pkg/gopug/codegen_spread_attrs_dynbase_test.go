package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// spreadDynBaseData is the declared struct this file's differential tests
// resolve a &attributes tag's own PLAIN dynamic (non-"class", non-HTML-
// boolean-attribute-name) base attribute against. Href/Cls are string
// fields (Href a plain dynamic base value; Cls used only for the dynamic
// "class" base deferral, which must stay refused), A/B are int fields (a
// numeric base value, and the fallible-division-base scenario), and
// Attrs/AttrsAny are the two runtime spread-source shapes a dynamic base
// must be proven to combine correctly with.
type spreadDynBaseData struct {
	Href     string
	Cls      string
	A        int
	B        int
	Attrs    map[string]string
	AttrsAny map[string]any
}

var spreadDynBaseReflectType = reflect.TypeOf(spreadDynBaseData{})

// spreadDynBaseDataStructSrc is spreadDynBaseData's field declarations,
// spliced verbatim into the throwaway module runComposedGo assembles around
// a GenerateGo result — it must match spreadDynBaseData above field for
// field.
const spreadDynBaseDataStructSrc = `type spreadDynBaseData struct {
	Href     string
	Cls      string
	A        int
	B        int
	Attrs    map[string]string
	AttrsAny map[string]any
}
`

// runDynBaseDifferential parses src, generates it through GenerateGo against
// spreadDynBaseData, builds and runs the result, separately renders it
// through the interpreter (Compile/Render) against data, and asserts the two
// outputs are byte-identical.
func runDynBaseDifferential(t *testing.T, src string, data map[string]any, dataLiteral string) string {
	t.Helper()

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadDynBaseData",
		DataReflectType: spreadDynBaseReflectType,
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

	got := runComposedGo(t, generated, spreadDynBaseDataStructSrc, dataLiteral, "RenderSpread")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
	return got
}

// genDynBaseErr parses and GenerateGoes src against spreadDynBaseData (unless
// noType is true, in which case Config.DataReflectType is left nil), always
// returning GenerateGo's error rather than fatally failing the test.
func genDynBaseErr(t *testing.T, src string, noType bool) error {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	cfg := Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadDynBaseData",
		DataReflectType: spreadDynBaseReflectType,
	}
	if noType {
		cfg.DataReflectType = nil
	}
	_, err = GenerateGo(ast, cfg)
	return err
}

// TestCodegenSpreadAttrsDynBasePlainNotOverwritten proves the headline case
// (probe 1): a PLAIN dynamic base attribute NOT touched by the runtime
// spread renders byte-identically to Runtime.renderTag's own non-spread
// render branch, including HTML-attribute escaping of special characters.
func TestCodegenSpreadAttrsDynBasePlainNotOverwritten(t *testing.T) {
	t.Parallel()
	src := "a(href=Href)&attributes(Attrs)\n"
	data := map[string]any{"Href": `a&b<c>"d`, "Attrs": map[string]string{}}
	dataLiteral := `spreadDynBaseData{Href: "a&b<c>\"d", Attrs: map[string]string{}}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `href="a&amp;b&lt;c&gt;&quot;d"`) {
		t.Fatalf("output %q does not exhibit the escaped dynamic base href value", got)
	}
}

// TestCodegenSpreadAttrsDynBaseOverwritten proves the runtime spread wins
// over a plain dynamic base of the same name (probe 2) — the base value is
// evaluated at generate time only to build the base map, but is discarded at
// render time exactly like a static base literal already is.
func TestCodegenSpreadAttrsDynBaseOverwritten(t *testing.T) {
	t.Parallel()
	src := "a(href=Href)&attributes(Attrs)\n"
	data := map[string]any{"Href": "/orig", "Attrs": map[string]string{"href": "/x"}}
	dataLiteral := `spreadDynBaseData{Href: "/orig", Attrs: map[string]string{"href": "/x"}}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `href="/x"`) || strings.Contains(got, "/orig") {
		t.Fatalf("output %q does not show the spread value overwriting the dynamic base", got)
	}
}

// TestCodegenSpreadAttrsDynBaseTemplateLiteral proves a template-literal
// base value — one of genValueExpr's other scalar shapes — renders exactly
// like a normal tag's dynamic attribute (probe 3).
func TestCodegenSpreadAttrsDynBaseTemplateLiteral(t *testing.T) {
	t.Parallel()
	src := "a(href=`/p/${A}`)&attributes(Attrs)\n"
	data := map[string]any{"A": 5, "Attrs": map[string]string{}}
	dataLiteral := `spreadDynBaseData{A: 5, Attrs: map[string]string{}}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `href="/p/5"`) {
		t.Fatalf("output %q does not exhibit the template-literal dynamic base value", got)
	}
}

// TestCodegenSpreadAttrsDynBaseNumericField proves a bare numeric field base
// value renders exactly like a normal tag's dynamic attribute (probe 3).
func TestCodegenSpreadAttrsDynBaseNumericField(t *testing.T) {
	t.Parallel()
	src := "a(data-n=A)&attributes(Attrs)\n"
	data := map[string]any{"A": 5, "Attrs": map[string]string{}}
	dataLiteral := `spreadDynBaseData{A: 5, Attrs: map[string]string{}}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `data-n="5"`) {
		t.Fatalf("output %q does not exhibit the numeric-field dynamic base value", got)
	}
}

// TestCodegenSpreadAttrsDynBaseTernary proves a ternary base value renders
// exactly like a normal tag's dynamic attribute (probe 3).
func TestCodegenSpreadAttrsDynBaseTernary(t *testing.T) {
	t.Parallel()
	src := `a(href=A > 0 ? "pos" : "neg")&attributes(Attrs)` + "\n"
	data := map[string]any{"A": 5, "Attrs": map[string]string{}}
	dataLiteral := `spreadDynBaseData{A: 5, Attrs: map[string]string{}}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `href="pos"`) {
		t.Fatalf("output %q does not exhibit the ternary dynamic base value", got)
	}
}

// TestCodegenSpreadAttrsDynBaseSortInterleave proves a dynamic base attribute
// and the runtime spread's own keys sort together correctly across all three
// sortAttrNames buckets — id, class, and alphabetical rest (probe 6).
func TestCodegenSpreadAttrsDynBaseSortInterleave(t *testing.T) {
	t.Parallel()
	src := `a(z2="static" href=Href)&attributes(Attrs)` + "\n"
	data := map[string]any{
		"Href":  "/h",
		"Attrs": map[string]string{"id": "i", "class": "c", "aaa": "a"},
	}
	dataLiteral := `spreadDynBaseData{Href: "/h", Attrs: map[string]string{"id": "i", "class": "c", "aaa": "a"}}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `id="i" class="c" aaa="a" href="/h" z2="static"`) {
		t.Fatalf("output %q does not exhibit the expected id/class/rest interleaved order — this test's own pinned assumption is stale", got)
	}
}

// TestCodegenSpreadAttrsDynBaseWithMapStringAnySource proves a dynamic base
// attribute combined with a map[string]any runtime spread source (rather
// than map[string]string) is unaffected — the same base map feeds both
// gopug.WriteSpreadAttrs and gopug.WriteSpreadAttrsAny.
func TestCodegenSpreadAttrsDynBaseWithMapStringAnySource(t *testing.T) {
	t.Parallel()
	src := "a(href=Href)&attributes(AttrsAny)\n"
	data := map[string]any{"Href": "/orig", "AttrsAny": map[string]any{"data-x": 1}}
	dataLiteral := `spreadDynBaseData{Href: "/orig", AttrsAny: map[string]any{"data-x": 1}}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `href="/orig"`) || !strings.Contains(got, `data-x="1"`) {
		t.Fatalf("output %q does not exhibit both the untouched dynamic base and the map[string]any spread value", got)
	}
}

// TestCodegenSpreadAttrsDynBaseWithInlineObjectSource proves a dynamic base
// attribute combined with an INLINE object literal &attributes source (the
// generate-time-static spread path, genSpreadAttrsInlineObject) also shares
// genSpreadBase's relaxation correctly, not just the field/variable spread
// path.
func TestCodegenSpreadAttrsDynBaseWithInlineObjectSource(t *testing.T) {
	t.Parallel()
	src := `a(href=Href)&attributes({"data-x": "1"})` + "\n"
	data := map[string]any{"Href": "/orig"}
	dataLiteral := `spreadDynBaseData{Href: "/orig"}`
	got := runDynBaseDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `href="/orig"`) || !strings.Contains(got, `data-x="1"`) {
		t.Fatalf("output %q does not exhibit both the dynamic base and the inline-object spread value", got)
	}
}

// TestCodegenSpreadAttrsDynBaseClassDefers proves a dynamic "class" base
// attribute still defers (unchanged): Runtime.renderTag's spread-merge reads
// a base "class" as the RAW, UNEVALUATED source text whenever the runtime
// spread itself supplies a "class" key, so a dynamic class base can never be
// proven byte-identical either way — pinning both the not-touched and the
// touched-by-spread interpreter output documents exactly why.
func TestCodegenSpreadAttrsDynBaseClassDefers(t *testing.T) {
	src := `a(class=Cls)&attributes(Attrs)` + "\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}

	notTouched, err := tmpl.Render(map[string]any{"Cls": "foo", "Attrs": map[string]string{}})
	if err != nil {
		t.Fatalf("interpreter Render (not touched): %v", err)
	}
	if notTouched != `<a class="foo"></a>` {
		t.Fatalf("interpreter output %q does not exhibit the expected evaluated dynamic class — this test's own pinned assumption is stale", notTouched)
	}

	touched, err := tmpl.Render(map[string]any{"Cls": "foo", "Attrs": map[string]string{"class": "bar"}})
	if err != nil {
		t.Fatalf("interpreter Render (touched by spread): %v", err)
	}
	if touched != `<a class="Cls bar"></a>` {
		t.Fatalf("interpreter output %q does not exhibit the raw-source-text class merge asymmetry — this test's own pinned assumption is stale", touched)
	}

	err = genDynBaseErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "is dynamic") {
		t.Errorf("GenerateGo error %q does not mention the dynamic base deferral", err.Error())
	}
}

// TestCodegenSpreadAttrsDynBaseFallibleOverwrittenDefers proves a FALLIBLE
// base expression (a top-level division) defers unconditionally, even though
// the interpreter — when the runtime spread overwrites that exact key —
// never evaluates the fallible base at all and produces NO error. Codegen
// cannot know at generate time whether a given render call's spread will
// overwrite the key, so it must refuse the fallible base entirely rather
// than risk erroring on a render the interpreter completes successfully.
func TestCodegenSpreadAttrsDynBaseFallibleOverwrittenDefers(t *testing.T) {
	src := "a(href=A / B)&attributes(Attrs)\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"A": 5, "B": 0, "Attrs": map[string]string{"href": "/x"}})
	if err != nil {
		t.Fatalf("interpreter Render (fallible base overwritten by spread): unexpected error %v", err)
	}
	if want != `<a href="/x"></a>` {
		t.Fatalf("interpreter output %q does not exhibit the spread's value with no error raised for the never-evaluated fallible base — this test's own pinned assumption is stale", want)
	}

	err = genDynBaseErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "fallible") {
		t.Errorf("GenerateGo error %q does not mention the fallible-base deferral", err.Error())
	}
}

// TestCodegenSpreadAttrsDynBaseDeferrals collects every distinct clean error
// this relaxation's own scope cut refuses, rather than guessing at.
func TestCodegenSpreadAttrsDynBaseDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		noType  bool
		wantSub string
	}{
		{
			name:    "dynamic HTML boolean attribute name base",
			src:     "input(disabled=Cls)&attributes(Attrs)\n",
			wantSub: "HTML boolean attribute name",
		},
		{
			name:    "style-object base attribute",
			src:     `a(style={color: "red"})&attributes(Attrs)` + "\n",
			wantSub: "is dynamic",
		},
		{
			name:    "nil DataReflectType",
			src:     "a(href=Href)&attributes(Attrs)\n",
			noType:  true,
			wantSub: "DataReflectType",
		},
	}

	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genDynBaseErr(t, tc.src, tc.noType)
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

// TestCodegenSpreadAttrsDynBaseFaultInjection proves the differential harness
// itself is non-vacuous for this relaxation: a deliberately WRONG expected
// value must fail the comparison.
func TestCodegenSpreadAttrsDynBaseFaultInjection(t *testing.T) {
	t.Parallel()
	src := "a(href=Href)&attributes(Attrs)\n"
	dataLiteral := `spreadDynBaseData{Href: "/orig", Attrs: map[string]string{}}`

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadDynBaseData",
		DataReflectType: spreadDynBaseReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runComposedGo(t, generated, spreadDynBaseDataStructSrc, dataLiteral, "RenderSpread")
	wrongWant := `<a href="/wrong"></a>`
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}
