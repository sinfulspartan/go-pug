package gopug

import (
	"strings"
	"testing"
)

// TestCodegenMixinAttrForwardBasic is the headline forwarding case: a mixin
// body tag spreads the call site's own attributes onto itself, proving both
// the forwarding mechanism itself and the id/class/rest attribute sort order
// (a base dynamic "href", plus two spread attributes, land in the same order
// Runtime.renderTag's own sortAttrNames produces: "class" before any other
// name, then alphabetically).
func TestCodegenMixinAttrForwardBasic(t *testing.T) {
	src := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/x\")(class=\"active\", target=\"_blank\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardClassMerge proves a base "class" (here, a
// shorthand class token) is space-appended to, never overwritten by, a
// spread "class" — matching Runtime.renderTag's class-specific merge branch.
func TestCodegenMixinAttrForwardClassMerge(t *testing.T) {
	src := "mixin box()\n  button.base&attributes(attributes)\n+box()(class=\"b\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardBoolTrue proves a bare call attribute
// (`+foo()(disabled)`) becomes a bare boolean attribute on the forwarding
// tag, exactly like a bare attribute written directly on a tag.
func TestCodegenMixinAttrForwardBoolTrue(t *testing.T) {
	src := "mixin box()\n  input&attributes(attributes)\n+box()(disabled)\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardBoolTrueQuoted proves the SAME bare-boolean
// outcome for a call attribute whose value is the literal string "true"
// (quoted OR the bare `true` keyword, not the no-value shorthand) — the
// interpreter's own attribute map only ever holds strings, so a quoted
// "true" is indistinguishable from a bare call attribute once it reaches
// Runtime.renderTag's spread-merge switch, and this increment's static
// call-attribute classification (staticCallAttrValue) reproduces that
// collapse exactly.
func TestCodegenMixinAttrForwardBoolTrueQuoted(t *testing.T) {
	srcQuoted := "mixin box()\n  input&attributes(attributes)\n+box()(disabled=\"true\")\n"
	runMixinDifferential(t, srcQuoted, map[string]any{}, "mixinDataStruct{}")

	srcKeyword := "mixin box()\n  input&attributes(attributes)\n+box()(disabled=true)\n"
	runMixinDifferential(t, srcKeyword, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardBoolFalse proves a spread attribute whose value
// is the string "false" (or the bare `false` keyword) is DELETED from the
// tag entirely, even when the tag's own base attribute of that name was a
// bare boolean.
func TestCodegenMixinAttrForwardBoolFalse(t *testing.T) {
	srcQuoted := "mixin box()\n  input(hidden)&attributes(attributes)\n+box()(hidden=\"false\")\n"
	runMixinDifferential(t, srcQuoted, map[string]any{}, "mixinDataStruct{}")

	srcKeyword := "mixin box()\n  input(hidden)&attributes(attributes)\n+box()(hidden=false)\n"
	runMixinDifferential(t, srcKeyword, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardOverwriteBase proves a non-"class" spread
// attribute completely overwrites a base attribute of the same name, whether
// that base attribute was itself static or a dynamic reference to the
// mixin's own parameter.
func TestCodegenMixinAttrForwardOverwriteBase(t *testing.T) {
	srcStaticBase := "mixin box()\n  a(href=\"/base\")&attributes(attributes)\n+box()(href=\"/new\")\n"
	runMixinDifferential(t, srcStaticBase, map[string]any{}, "mixinDataStruct{}")

	srcDynamicBase := "mixin link(href)\n  a(href=href)&attributes(attributes)\n+link(\"/base\")(href=\"/new\")\n"
	runMixinDifferential(t, srcDynamicBase, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardEscaping proves a call attribute value
// containing characters that must be HTML-escaped inside an attribute
// (`&`, `<`, `>`) is escaped identically to any other static attribute
// value, through EscapeAttr/htmlEscapeAttr.
func TestCodegenMixinAttrForwardEscaping(t *testing.T) {
	src := "mixin box()\n  a&attributes(attributes)\n+box()(title=\"a & b <c>\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardEmpty proves a call with no attributes group at
// all renders the tag with only its own base attributes, no error.
func TestCodegenMixinAttrForwardEmpty(t *testing.T) {
	src := "mixin box()\n  a(href=\"/x\")&attributes(attributes)\n+box()\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardTwoCallSites proves each call site's own
// attributes are forwarded independently — the FIRST call's attributes must
// never leak into the SECOND's rendering (critical for this increment's
// per-call-site generation: no shared runtime state, and no accidental
// generate-time state reuse either).
func TestCodegenMixinAttrForwardTwoCallSites(t *testing.T) {
	src := "mixin box()\n  div.base&attributes(attributes)\n+box()(class=\"a\", id=\"one\")\n+box()(class=\"b\", target=\"_blank\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinAttrForwardSortOrder pins the id/class/rest attribute
// order explicitly, mixing a base id and a base "other" attribute with a
// spread class and a spread "other" attribute, so the resulting merged
// attribute set spans all three sortAttrNames buckets from both sources.
func TestCodegenMixinAttrForwardSortOrder(t *testing.T) {
	src := "mixin box()\n  div(id=\"base\", z=\"1\")&attributes(attributes)\n+box()(class=\"c\", a=\"z\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if !strings.Contains(want, `id="base" class="c" a="z" z="1"`) {
		t.Fatalf("interpreter output %q does not exhibit the expected id/class/rest order — this test's own pinned assumption about Runtime.renderTag's sort order is stale", want)
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
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q", got, want)
	}
}

// TestCodegenMixinAttrForwardDeferrals collects every distinct clean error
// this increment's scope cut refuses, rather than guessing at. A dynamic
// (non-literal, non-fallible) call attribute — the one case this suite used
// to defer here — is now a supported shape (built as a runtime attribute
// map and spread via gopug.WriteSpreadAttrs); its own differential and
// deferral coverage lives in the dynamic-call-attribute test suite instead.
func TestCodegenMixinAttrForwardDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		noType  bool
		wantSub string
	}{
		{
			name:    "non-attributes forwarding arg (data-map field)",
			src:     "mixin box()\n  a&attributes(extra)\n+box()\n",
			wantSub: "special \"attributes\" variable",
		},
		{
			name:    "inline object &attributes({...})",
			src:     "mixin box()\n  a&attributes({x: \"1\"})\n+box()\n",
			wantSub: "special \"attributes\" variable",
		},
		{
			name:    "style-object base attribute on a &attributes tag",
			src:     "mixin box()\n  div(style={color: \"red\"})&attributes(attributes)\n+box()(class=\"x\")\n",
			wantSub: "attribute",
		},
		{
			name:    "dynamic class= base attribute on a &attributes tag",
			src:     "mixin item(cls)\n  div(class=cls)&attributes(attributes)\n+item(\"x\")(class=\"y\")\n",
			wantSub: "dynamic class value",
		},
		{
			name:    "block slot combined with &attributes",
			src:     "mixin box()\n  div&attributes(attributes)\n    block\n+box()(class=\"x\")\n  p child\n",
			wantSub: "block slot",
		},
		{
			name:    "nil DataReflectType",
			src:     "mixin box()\n  a&attributes(attributes)\n+box()(class=\"x\")\n",
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

// TestCodegenMixinAttrForwardNonMixinAttributesStillDefers proves the
// GENERAL `&attributes` spread on a tag OUTSIDE any mixin body — the
// pre-existing codegen.go deferral this increment must not perturb — still
// defers exactly as before.
func TestCodegenMixinAttrForwardNonMixinAttributesStillDefers(t *testing.T) {
	src := "div&attributes(extra)\n"
	err := genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "&attributes") {
		t.Errorf("GenerateGo error %q does not mention &attributes", err.Error())
	}
}

// TestCodegenMixinAttrForwardFaultInjection proves the differential harness
// itself is non-vacuous for this feature: a deliberately WRONG expected
// value must fail the comparison.
func TestCodegenMixinAttrForwardFaultInjection(t *testing.T) {
	src := "mixin box()\n  button.base&attributes(attributes)\n+box()(class=\"b\")\n"

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
	wrongWant := `<button class="wrong"></button>`
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenMixinAttrForwardFuncCountRegression proves the mechanism choice
// this increment made (inlining a &attributes-forwarding mixin's body at
// each call site, rather than emitting it as a shared helper function like
// every other mixin — see mixinAttrForward's own doc comment): a template
// declaring a &attributes-using mixin called TWICE emits exactly ONE
// function (the render function itself — no shared helper at all, since
// none is ever generated for this mixin), unlike an ordinary mixin (no
// `&attributes` anywhere in its body), which still emits its usual TWO
// (the render function plus its one shared helper) regardless of how many
// times it is called.
func TestCodegenMixinAttrForwardFuncCountRegression(t *testing.T) {
	attrForwardSrc := "mixin box()\n  div.base&attributes(attributes)\n+box()(class=\"a\")\n+box()(class=\"b\")\n"
	ast, err := Parse(attrForwardSrc, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", attrForwardSrc, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderMixin",
		DataType:        "mixinDataStruct",
		DataReflectType: mixinDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", attrForwardSrc, err)
	}
	if n := strings.Count(string(generated), "\nfunc "); n != 1 {
		t.Errorf("expected exactly one generated func for a &attributes-forwarding mixin (inlined per call site, no shared helper), got %d in:\n%s", n, generated)
	}

	ordinarySrc := "mixin box()\n  div.base\n    p hi\n+box()\n+box()\n"
	ast2, err := Parse(ordinarySrc, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", ordinarySrc, err)
	}
	generated2, err := GenerateGo(ast2, Config{
		PackageName:     "main",
		FuncName:        "RenderMixin",
		DataType:        "mixinDataStruct",
		DataReflectType: mixinDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", ordinarySrc, err)
	}
	if n := strings.Count(string(generated2), "\nfunc "); n != 2 {
		t.Errorf("expected exactly two generated funcs for an ordinary mixin (render function + one shared helper), got %d in:\n%s", n, generated2)
	}
}
