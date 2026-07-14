package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// TestCodegenMixinDefaultLiteralUsedAndOverridden is the headline default-
// value case: a string-literal default is used when the caller omits the
// argument, and overridden when the caller supplies one — the same call site
// exercised twice with a different argument count, matching
// Runtime.renderMixinCall's own missing-arg-with-default binding exactly.
func TestCodegenMixinDefaultLiteralUsedAndOverridden(t *testing.T) {
	src := "mixin g(a, b = \"def\")\n  p= a\n  span= b\n+g(\"X\")\n+g(\"X\",\"Y\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinDefaultNumericLiteral proves a bare numeric-literal
// default value (no quotes) resolves the same way a quoted string default
// does.
func TestCodegenMixinDefaultNumericLiteral(t *testing.T) {
	src := "mixin g(n = 5)\n  p= n\n+g()\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinDefaultCallerScopeDataField is the eval-CONTEXT proof: a
// default expression that names a top-level DATA FIELD resolves against the
// CALLER's own data, not some fresh empty scope — because
// Runtime.renderMixinCall evaluates every default with r.evaluateExpr
// BEFORE the mixin's own scope frame is ever pushed onto r.scopeStack, the
// caller's data is exactly what a real, present argument would also see.
// genMixinParamValue reproduces this by calling genValueExpr(defaultExpr) —
// the identical CALLER-scope code path a present argument already used —
// so this is byte-identical by construction, not by coincidence.
func TestCodegenMixinDefaultCallerScopeDataField(t *testing.T) {
	src := "mixin g(a = Title)\n  p= a\n+g()\n"
	runMixinDifferential(t, src, map[string]any{"Title": "fromData"}, `mixinDataStruct{Title: "fromData"}`)
}

// mixinSiblingDefaultStruct is a single-field struct (deliberately unrelated
// to mixinDataStruct's own Title/Name fields) whose sole purpose is
// TestCodegenMixinDefaultSiblingParamNameAliasesCallerField below: it needs
// a Go struct field whose Pug identifier is the exact single letter "a", so
// resolveStructField's case-insensitive tier ("a" -> field "A") lets
// genValueExpr resolve the SAME identifier a sibling mixin parameter
// happens to share, proving that resolution reaches the CALLER's own data
// field rather than the sibling parameter's freshly bound call-time value.
type mixinSiblingDefaultStruct struct {
	A string
}

const mixinSiblingDefaultStructSrc = `type mixinSiblingDefaultStruct struct {
	A string
}
`

// TestCodegenMixinDefaultSiblingParamNameAliasesCallerField is the sibling-
// param eval-context proof made concrete and generatable: `mixin g(a, b = a)`
// called as `+g("A")` binds parameter `a`
// to the call-time argument "A", and — because the interpreter's binding
// loop evaluates b's default BEFORE the mixin's own scope frame (containing
// that very "A" binding for `a`) is ever pushed — the default expression
// `a` resolves against the CALLER's OWN scope instead: here, the top-level
// data field named (case-insensitively) "a", NOT the sibling parameter's
// call-time value. Passing a caller data field whose value ("CallerFieldValue")
// differs from the call argument ("A") makes the two hypotheses
// distinguishable: if codegen (or the interpreter) resolved the default
// against the freshly bound sibling parameter instead, this would render
// "A"; both actually render the caller's own field value, proving
// caller-side (not mixin-param-side) evaluation byte-identically.
func TestCodegenMixinDefaultSiblingParamNameAliasesCallerField(t *testing.T) {
	src := "mixin g(a, b = a)\n  span= b\n+g(\"A\")\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"a": "CallerFieldValue"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<span>CallerFieldValue</span>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q (the sibling parameter's call-time value \"A\" must NOT leak into b's default)", src, want, "<span>CallerFieldValue</span>")
	}

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderMixin",
		DataType:        "mixinSiblingDefaultStruct",
		DataReflectType: reflect.TypeOf(mixinSiblingDefaultStruct{}),
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runComposedGo(t, generated, mixinSiblingDefaultStructSrc, `mixinSiblingDefaultStruct{A: "CallerFieldValue"}`, "RenderMixin")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenMixinDefaultFlowsThroughAttrAndIf proves a defaulted parameter
// flows through every scalar context a normal (non-defaulted) parameter
// already does: a dynamic attribute value and an `if` condition, over two
// call sites — one omitting the argument (default used), one supplying it
// (default overridden).
func TestCodegenMixinDefaultFlowsThroughAttrAndIf(t *testing.T) {
	src := strings.Join([]string{
		"mixin badge(label = \"d\")",
		"  span.badge(data-kind=label)",
		"    if label",
		"      = label",
		"    else",
		"      = \"none\"",
		"+badge()",
		"+badge(\"custom\")",
		"",
	}, "\n")
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinDefaultInDynamicBlockContent is the slice-3 interaction: a
// defaulted parameter referenced from DYNAMIC BLOCK CONTENT a call site
// passes to the mixin's `block` slot must see the SAME value the mixin
// helper call itself receives — the missing-argument default, hoisted into
// the shared `__margN` local both genMixinCall's own call and
// genMixinBlockClosure's closure read from.
func TestCodegenMixinDefaultInDynamicBlockContent(t *testing.T) {
	src := "mixin w(label = \"d\")\n  block\n+w()\n  p= label\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinDefaultInAttrForward is the slice-4 interaction: a
// defaulted parameter used as a BASE attribute value on a mixin's own
// `&attributes`-forwarding tag must resolve its default at the SAME
// `__margN` hoist genMixinCallAttrForward's own attribute-merge machinery
// consumes, alongside a call site's own forwarded (spread) attribute.
func TestCodegenMixinDefaultInAttrForward(t *testing.T) {
	src := "mixin badge(label = \"d\")\n  span(data-label=label)&attributes(attributes)\n+badge()(class=\"x\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinDefaultMixedParams exercises a mixin whose parameters mix
// defaulted and non-defaulted, across call sites that pass none, some, or
// all of them — every combination of "present argument", "missing with
// default", and (for the trailing non-defaulted parameter) "missing with no
// default" a single call site can produce.
func TestCodegenMixinDefaultMixedParams(t *testing.T) {
	src := strings.Join([]string{
		"mixin g(a, b = \"B\", c)",
		"  p= a",
		"  p= b",
		"  p= c",
		"+g(\"A\")",
		"+g(\"A\",\"X\")",
		"+g(\"A\",\"X\",\"Y\")",
		"",
	}, "\n")
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinDefaultDeferrals asserts every default-related construct
// this increment cannot prove byte-identical returns its own clean, distinct
// GenerateGo error rather than silently mis-generating — the DEFER list a
// default parameter value adds on top of every prior increment's own
// deferrals (which stay deferred unchanged: see TestCodegenMixinDeferrals,
// TestCodegenMixinAttrForwardDeferrals, TestCodegenMixinBlockDynamicDeferralsAreDistinct).
func TestCodegenMixinDefaultDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			name:    "rest parameter, even alongside a default parameter",
			src:     "mixin foo(a = \"x\", ...items)\n  p= a\n+foo(\"y\")\n",
			wantSub: "rest parameter",
		},
		{
			name:    "fallible default expression (division by zero)",
			src:     "mixin g(a = 1/0)\n  p= a\n+g()\n",
			wantSub: "fallible expression",
		},
		{
			name:    "genValueExpr-unsupported default shape (array literal)",
			src:     "mixin g(a = [1,2,3])\n  p= a\n+g()\n",
			wantSub: "array literal",
		},
		{
			name:    "sibling-param default referencing a non-field identifier",
			src:     "mixin g(a, b = a)\n  span= b\n+g(\"A\")\n",
			wantSub: "not a field of",
		},
	}

	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genMixinErr(t, tc.src, false)
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

// TestCodegenMixinDefaultFallibleInterpreterFallsBackToRawString pins the
// exact asymmetry that forces the fallible-default deferral above: the
// interpreter does NOT propagate a default expression's evaluation error —
// Runtime.renderMixinCall's own binding loop catches it and falls back to
// the raw, unevaluated default-expression STRING ("1/0") — while codegen has
// no way to reproduce that fallback from a Go expression that would itself
// return a runtime error, so it defers instead of guessing.
func TestCodegenMixinDefaultFallibleInterpreterFallsBackToRawString(t *testing.T) {
	src := "mixin g(a = 1/0)\n  p= a\n+g()\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p>1/0</p>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q (documents the raw-string fallback the fail-closed codegen deferral above is guarding)", src, want, "<p>1/0</p>")
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "fallible expression") {
		t.Errorf("GenerateGo error %q does not mention the fallible-default deferral", err.Error())
	}
}

// TestCodegenMixinDefaultFaultInjection proves the differential harness
// itself is non-vacuous for a defaulted-parameter mixin: a deliberately
// WRONG expected value must fail the comparison.
func TestCodegenMixinDefaultFaultInjection(t *testing.T) {
	src := "mixin g(a = \"def\")\n  p= a\n+g()\n"

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

	got := runComposedGo(t, generated, mixinDataStructSrc, "mixinDataStruct{}", "RenderMixin")
	wrongWant := "<p>WRONG</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenMixinDefaultNoDefaultsIsNoOp proves the per-parameter binding
// change this increment makes (genMixinParamValue) is a no-op for a mixin
// that declares NO default at all: the generated helper call for a present
// argument, and for a missing argument with no default, are exactly the
// same Go source genMixinCall/genMixinCallAttrForward emitted before this
// increment (a plain genValueExpr result, or the literal `""`).
func TestCodegenMixinDefaultNoDefaultsIsNoOp(t *testing.T) {
	src := "mixin greet(name)\n  p= name\n+greet(\"Sam\")\n+greet()\n"

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
	genStr := string(generated)

	if !strings.Contains(genStr, `pugMixin_greet(w, "Sam")`) {
		t.Errorf("generated source does not call the helper with the present argument unchanged:\n%s", genStr)
	}
	if !strings.Contains(genStr, `pugMixin_greet(w, "")`) {
		t.Errorf("generated source does not bind a missing, no-default argument to the literal \"\" unchanged:\n%s", genStr)
	}

	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinDefaultFuncCountRegression proves a mixin with default
// parameters still emits exactly one shared helper function (no extra
// per-call-site function the default-substitution mechanism might have
// needed but doesn't, since defaults are resolved entirely at the CALL
// site, never inside the helper itself).
func TestCodegenMixinDefaultFuncCountRegression(t *testing.T) {
	src := "mixin greet(name = \"World\")\n  p= name\n+greet()\n+greet(\"X\")\n"

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
	if n := strings.Count(string(generated), "\nfunc "); n != 2 {
		t.Errorf("expected exactly 2 generated funcs (render function + one mixin helper) for a mixin with default params, got %d in:\n%s", n, generated)
	}

	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}
