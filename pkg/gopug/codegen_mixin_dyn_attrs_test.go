package gopug

import (
	"strings"
	"testing"
)

// TestCodegenMixinDynAttrBasic proves the headline case this suite lifts: a
// mixin call attribute value that is a plain data-field reference — no
// longer required to be a literal — is built into a runtime attribute map at
// the call site and spread onto the forwarding tag through
// gopug.WriteSpreadAttrs, byte-identical to the interpreter.
func TestCodegenMixinDynAttrBasic(t *testing.T) {
	t.Parallel()
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(class=Title)\n"
	runMixinDifferential(t, src, map[string]any{"Title": "on"}, `mixinDataStruct{Title: "on"}`)
}

// TestCodegenMixinDynAttrBare proves a bare call attribute (no "=") still
// becomes "true" in the runtime attribute map, exactly like the all-static
// path, even when another attribute in the SAME call forces the dynamic
// runtime-map branch.
func TestCodegenMixinDynAttrBare(t *testing.T) {
	t.Parallel()
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(disabled, class=Title)\n"
	runMixinDifferential(t, src, map[string]any{"Title": "on"}, `mixinDataStruct{Title: "on"}`)
}

// TestCodegenMixinDynAttrMixedStaticDynamic proves a call mixing a static
// quoted-literal attribute with a dynamic data-field attribute merges both
// into the same runtime map correctly.
func TestCodegenMixinDynAttrMixedStaticDynamic(t *testing.T) {
	t.Parallel()
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(class=\"base\", id=Name)\n"
	runMixinDifferential(t, src, map[string]any{"Name": "s1"}, `mixinDataStruct{Name: "s1"}`)
}

// TestCodegenMixinDynAttrClassMergeWithBase proves a dynamic call attribute
// named "class" is space-appended to a shorthand base class, the same
// mergeSpreadClass rule the all-static path already proves, now reached
// through the runtime gopug.WriteSpreadAttrs path instead of the gen-time
// merge.
func TestCodegenMixinDynAttrClassMergeWithBase(t *testing.T) {
	t.Parallel()
	src := "mixin b\n  button.btn&attributes(attributes)\n+b()(class=Title)\n"
	runMixinDifferential(t, src, map[string]any{"Title": "x"}, `mixinDataStruct{Title: "x"}`)
}

// TestCodegenMixinDynAttrEscapingAndNumeric proves a dynamic call attribute
// value needing HTML-attribute escaping, and a numeric-field call attribute
// value, both render byte-identical to the interpreter: the runtime
// attribute map holds plain strings (genValueExpr's own string-typed
// result), and gopug.WriteSpreadAttrs escapes each spread value exactly
// once, the same as any other spread source this generator supports.
func TestCodegenMixinDynAttrEscapingAndNumeric(t *testing.T) {
	t.Parallel()
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(title=Name, data-n=Count)\n"
	runMixinDifferential(t, src, map[string]any{"Name": "a & b <c>", "Count": 42}, `mixinDataStruct{Name: "a & b <c>", Count: 42}`)
}

// TestCodegenMixinDynAttrBaseParamPlusDynamicSpread proves a mixin body base
// attribute referencing the mixin's OWN parameter (bound to its hoisted
// __marg local) renders correctly alongside a dynamic call attribute spread
// through the runtime map — the two mechanisms (param-scope resolution via
// genSpreadBase, and the "attributes" scope var via genSpreadAttrs) compose
// without interfering with each other.
func TestCodegenMixinDynAttrBaseParamPlusDynamicSpread(t *testing.T) {
	t.Parallel()
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/base\")(class=Title)\n"
	runMixinDifferential(t, src, map[string]any{"Title": "on"}, `mixinDataStruct{Title: "on"}`)
}

// TestCodegenMixinDynAttrMultipleDynamicEntries proves a call supplying
// SEVERAL dynamic attributes together — a "map-derived" runtime attribute
// map with more than one non-static entry — builds and spreads correctly,
// exercising the sorted, comma-joined map-literal construction
// genMixinCallAttrsMap emits for more than a single dynamic key.
func TestCodegenMixinDynAttrMultipleDynamicEntries(t *testing.T) {
	t.Parallel()
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(class=Title, id=Name, data-n=Count)\n"
	runMixinDifferential(t, src, map[string]any{"Title": "on", "Name": "n1", "Count": 7}, `mixinDataStruct{Title: "on", Name: "n1", Count: 7}`)
}

// TestCodegenMixinDynAttrFallibleCallAttrRawStringAsymmetry pins the
// fallible-call-attribute hazard this feature must defer rather than
// silently diverge on: Runtime.renderMixinCall's own attribute loop falls
// back to the RAW, UNEVALUATED expression STRING (not an error) whenever
// r.evaluateExpr fails on a call attribute's value — confirmed here for a
// division-by-zero expression, which renders `data-r="10/Count"` verbatim,
// with NO render error at all. A codegen expression for 10/Count would
// itself have to return a runtime error instead of a string, so this shape
// can never be reproduced byte-identically and must defer at generate time
// instead.
func TestCodegenMixinDynAttrFallibleCallAttrRawStringAsymmetry(t *testing.T) {
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(data-r=10/Count)\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Count": 0})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	const wantRaw = `<a data-r="10/Count" href="/x"></a>`
	if want != wantRaw {
		t.Fatalf("interpreter output %q does not exhibit the expected raw-expression fallback %q — this test's own pinned assumption about Runtime.renderMixinCall's evaluateExpr-error fallback is stale", want, wantRaw)
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error for a fallible call attribute, got nil", src)
	}
	if !strings.Contains(err.Error(), "fallible") {
		t.Errorf("GenerateGo error %q does not mention the call attribute being fallible", err.Error())
	}
}

// TestCodegenMixinDynAttrDeferrals collects every distinct clean error the
// dynamic call-attribute runtime-map path refuses rather than guessing at.
func TestCodegenMixinDynAttrDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		noType  bool
		wantSub string
	}{
		{
			name:    "genValueExpr-unsupported call attribute (array literal)",
			src:     "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(data-x=[1, 2, 3])\n",
			wantSub: "call attribute",
		},
		{
			name:    "rest parameter combined with a dynamic call attribute",
			src:     "mixin box(...items)\n  a&attributes(attributes)\n+box()(class=Title)\n",
			wantSub: "rest parameter",
		},
		{
			name:    "block slot combined with a dynamic call attribute",
			src:     "mixin box()\n  div&attributes(attributes)\n    block\n+box()(class=Title)\n  p child\n",
			wantSub: "block slot",
		},
		{
			name:    "dynamic class= base attribute in the body alongside a dynamic call attribute",
			src:     "mixin item(cls)\n  div(class=cls)&attributes(attributes)\n+item(\"x\")(class=Title)\n",
			wantSub: `base attribute "class" is dynamic`,
		},
		{
			name:    "nil DataReflectType",
			src:     "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(class=Title)\n",
			noType:  true,
			wantSub: "DataReflectType",
		},
	}

	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genMixinErr(t, tc.src, tc.noType)
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

// TestCodegenMixinDynAttrFaultInjection proves the differential harness
// itself is non-vacuous for the dynamic call-attribute path: a deliberately
// WRONG expected value must fail the comparison.
func TestCodegenMixinDynAttrFaultInjection(t *testing.T) {
	t.Parallel()
	src := "mixin b\n  button.btn&attributes(attributes)\n+b()(class=Title)\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderMixin",
		DataType:        "mixinDataStruct",
		DataReflectType: mixinDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runComposedGo(t, generated, mixinDataStructSrc, `mixinDataStruct{Title: "x"}`, "RenderMixin")
	wrongWant := `<button class="wrong"></button>`
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenMixinDynAttrRoutesThroughWriteSpreadAttrs proves, at the
// generated-Go-SOURCE level rather than only through rendered output, that a
// dynamic call attribute's `&attributes(attributes)` body tag is generated
// through the runtime gopug.WriteSpreadAttrs path (genSpreadAttrs), NOT
// through the gen-time mergeForwardedAttributes/genAttributes path the
// all-static case uses — the two mechanisms produce visibly different
// generated source, so this is a direct, non-behavioral check that binding
// "attributes" as a scope var genuinely falls through to the pre-existing,
// already-proven spread machinery instead of accidentally reusing the
// static merge.
func TestCodegenMixinDynAttrRoutesThroughWriteSpreadAttrs(t *testing.T) {
	dynamicSrc := "mixin b\n  button.btn&attributes(attributes)\n+b()(class=Title)\n"
	ast, err := Parse(dynamicSrc, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", dynamicSrc, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderMixin",
		DataType:        "mixinDataStruct",
		DataReflectType: mixinDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", dynamicSrc, err)
	}
	genStr := string(generated)
	if !strings.Contains(genStr, "gopug.WriteSpreadAttrs(") {
		t.Errorf("dynamic call-attribute generated source does not call gopug.WriteSpreadAttrs at all:\n%s", genStr)
	}
	if !strings.Contains(genStr, `map[string]string{"class":`) {
		t.Errorf("dynamic call-attribute generated source does not build a runtime __mixinAttrs map literal:\n%s", genStr)
	}

	staticSrc := "mixin b\n  button.btn&attributes(attributes)\n+b()(class=\"x\")\n"
	astStatic, err := Parse(staticSrc, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", staticSrc, err)
	}
	generatedStatic, err := GenerateGo(astStatic, Config{
		PackageName:     "main",
		FuncName:        "RenderMixin",
		DataType:        "mixinDataStruct",
		DataReflectType: mixinDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", staticSrc, err)
	}
	genStaticStr := string(generatedStatic)
	if strings.Contains(genStaticStr, "gopug.WriteSpreadAttrs(") {
		t.Errorf("all-static call-attribute generated source unexpectedly calls gopug.WriteSpreadAttrs (should stay on the gen-time merge path):\n%s", genStaticStr)
	}
}
