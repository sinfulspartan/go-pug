package gopug

import (
	"strings"
	"testing"
)

// TestCodegenMixinBlockDynamicParamRefLiteralAndDataArg is this increment's
// headline case: block content referencing the CALLED mixin's own parameter
// name resolves it to THIS call's argument value, over both a literal
// argument and one sourced from a data field (evaluated caller-side, then
// shared with the block-content closure through the same __margN local the
// helper call itself uses).
func TestCodegenMixinBlockDynamicParamRefLiteralAndDataArg(t *testing.T) {
	t.Parallel()
	src := "mixin w(label)\n  .x\n    block\n+w(\"Hello\")\n  p= label\n+w(Title)\n  p= label\n"
	runMixinDifferential(t, src, map[string]any{"Title": "World"}, `mixinDataStruct{Title: "World"}`)
}

// TestCodegenMixinBlockDynamicMissingParamIsEmptyString proves a block-
// content reference to a DECLARED parameter the call simply didn't supply an
// argument for resolves to the empty string, exactly like the missing-arg
// default the mixin body itself already gets.
func TestCodegenMixinBlockDynamicMissingParamIsEmptyString(t *testing.T) {
	t.Parallel()
	src := "mixin g(a, b)\n  block\n+g(\"x\")\n  p= b\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicParamInAttrAndTemplateLiteral proves a param
// reference in block content resolves correctly in every scalar codegen
// context already supported for a mixin's own body: a dynamic attribute
// value, a buffered `= expr`, and a backtick template literal.
func TestCodegenMixinBlockDynamicParamInAttrAndTemplateLiteral(t *testing.T) {
	t.Parallel()
	src := "mixin w(label)\n  block\n+w(\"L\")\n  span(data-x=label)= label\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")

	src2 := "mixin w(label)\n  block\n+w(\"L\")\n  span(data-y=`${label}-x`)\n"
	runMixinDifferential(t, src2, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicParamInIfCondition proves a param reference
// used as an `if` condition in block content is byte-identical over both a
// truthy and a falsy (missing/empty) argument value.
func TestCodegenMixinBlockDynamicParamInIfCondition(t *testing.T) {
	t.Parallel()
	falsySrc := "mixin w(label)\n  block\n+w(\"\")\n  if label\n    p yes\n  else\n    p no\n"
	runMixinDifferential(t, falsySrc, map[string]any{}, "mixinDataStruct{}")

	truthySrc := "mixin w(label)\n  block\n+w(\"L\")\n  if label\n    p yes\n  else\n    p no\n"
	runMixinDifferential(t, truthySrc, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicShadowsCallerLocal is the shadow/boundary case:
// a caller `- var` local shares its name with the CALLED mixin's parameter.
// Runtime.lookup stops descending at the mixin_boundary sentinel before ever
// reaching the caller's frame, so the parameter wins and the caller local is
// completely hidden — this proves codegen's param-scope closure reproduces
// that exactly (using the __margN local, never falling back to any caller
// scope entry for the same name).
func TestCodegenMixinBlockDynamicShadowsCallerLocal(t *testing.T) {
	t.Parallel()
	src := "- var label = \"caller\"\nmixin w(label)\n  block\n+w(\"mix\")\n  p= label\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicTwoCallSitesDifferentArgs proves the SAME
// mixin called twice, each with its own dynamic block content referencing
// the parameter, produces two independently-closed closures — each closing
// over its own call's __margN value, not a single shared one.
func TestCodegenMixinBlockDynamicTwoCallSitesDifferentArgs(t *testing.T) {
	t.Parallel()
	src := "mixin w(label)\n  block\n+w(\"First\")\n  p= label\n+w(\"Second\")\n  p= label\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicMultipleSlotsEachResolveParam proves a mixin
// body with two `block` nodes renders the same dynamic block content twice,
// each occurrence resolving the parameter independently (not, say, caching
// the first occurrence's resolved value for the second).
func TestCodegenMixinBlockDynamicMultipleSlotsEachResolveParam(t *testing.T) {
	t.Parallel()
	src := "mixin twice(label)\n  block\n  block\n+twice(\"X\")\n  p= label\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicStaticContentStillWorks is the slice-2
// regression this increment's param-scope change must not break: block
// content with no identifier reference at all, passed to a mixin that DOES
// declare parameters, still renders as pure static markup.
func TestCodegenMixinBlockDynamicStaticContentStillWorks(t *testing.T) {
	t.Parallel()
	src := "mixin w(label)\n  block\n+w(\"L\")\n  p Hello\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicSlotlessMixinUnaffected proves a mixin with no
// `block` slot at all is entirely unaffected by this increment: its own
// parameter is used only in its own body, never in block content (which is
// silently discarded, as before).
func TestCodegenMixinBlockDynamicSlotlessMixinUnaffected(t *testing.T) {
	t.Parallel()
	src := "mixin simple(label)\n  p= label\n+simple(\"Hi\")\n  p this should not appear\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicDataFieldStillFailsClosed proves a block-
// content reference to a top-level DATA FIELD — not the called mixin's own
// parameter — stays fail-closed exactly as it did before this increment: the
// interpreter's mixin boundary hides caller data just as it hides caller
// locals, rendering "", and codegen refuses to reproduce that silently.
func TestCodegenMixinBlockDynamicDataFieldStillFailsClosed(t *testing.T) {
	src := "mixin w(label)\n  block\n+w(\"L\")\n  p= Title\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Title": "leaked"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p></p>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q (documents the isolation the fail-closed codegen error below is guarding)", src, want, "<p></p>")
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for a data-field reference in dynamic block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "Title") {
		t.Errorf("GenerateGo error %q does not name the offending identifier %q", err.Error(), "Title")
	}
}

// TestCodegenMixinBlockDynamicCallerLocalStillFailsClosed is the caller
// `- var` local sibling of the data-field case above: also hidden by the
// mixin boundary (renders ""), also fail-closed in codegen.
func TestCodegenMixinBlockDynamicCallerLocalStillFailsClosed(t *testing.T) {
	src := "- var callerLocal = \"leaked\"\nmixin w(label)\n  block\n+w(\"L\")\n  p= callerLocal\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p></p>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q (documents the isolation the fail-closed codegen error below is guarding)", src, want, "<p></p>")
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for a caller local reference in dynamic block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "callerLocal") {
		t.Errorf("GenerateGo error %q does not name the offending identifier %q", err.Error(), "callerLocal")
	}
}

// TestCodegenMixinBlockDynamicAttributesStillFailsClosed proves a block-
// content reference to `attributes` — the mixin call's own implicit
// attributes map, set in the SAME scope frame as the parameters but never a
// DECLARED parameter itself — stays fail-closed: the interpreter resolves it
// (it is not hidden by the boundary, unlike a caller local or data field) but
// finds no "class" key in the empty map and renders "", while codegen never
// admits any identifier outside decl.Parameters, so it still defers rather
// than special-case this one further exception.
func TestCodegenMixinBlockDynamicAttributesStillFailsClosed(t *testing.T) {
	src := "mixin w(label)\n  block\n+w(\"L\")\n  p= attributes.class\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p></p>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q (documents the interpreter's own resolution of attributes, which codegen still declines to model)", src, want, "<p></p>")
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for an attributes reference in dynamic block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "attributes") {
		t.Errorf("GenerateGo error %q does not name the offending identifier %q", err.Error(), "attributes")
	}
}

// TestCodegenMixinBlockDynamicIfBlockStillFailsClosed proves `if block` used
// AS PART OF the block content itself (as opposed to the mixin's own body,
// already covered) stays fail-closed too. The interpreter's own behavior
// here is genuinely surprising — `block` evaluates true because the content
// currently rendering IS r.callerBlock, non-empty, while r.inMixin is still
// set — but codegen treats `block` as just another non-parameter identifier
// and defers rather than attempt to model that recursive-feeling special
// case.
func TestCodegenMixinBlockDynamicIfBlockStillFailsClosed(t *testing.T) {
	src := "mixin w(label)\n  block\n+w(\"L\")\n  if block\n    p yes\n  else\n    p no\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p>yes</p>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q (documents the interpreter's own surprising `block`-as-value resolution inside block content)", src, want, "<p>yes</p>")
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for `if block` inside dynamic block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "block") {
		t.Errorf("GenerateGo error %q does not name the offending identifier %q", err.Error(), "block")
	}
}

// TestCodegenMixinBlockDynamicNestedMixinCallStillFailsClosed proves a
// nested mixin call written as part of block content stays deferred even
// though the enclosing call is a dynamic-content one now: the
// g.insideBlockClosure guard is orthogonal to whether the closure's own
// scope is empty (slice 2) or param-bound (this increment).
func TestCodegenMixinBlockDynamicNestedMixinCallStillFailsClosed(t *testing.T) {
	src := "mixin inner(x)\n  p= x\nmixin outer(y)\n  .outer\n    block\n+outer(\"Y\")\n  +inner(\"Z\")\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != `<div class="outer"><p>Z</p></div>` {
		t.Fatalf("interpreter Render(%q) = %q, want %q", src, want, `<div class="outer"><p>Z</p></div>`)
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for a nested mixin call in dynamic block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "nested mixin call") {
		t.Errorf("GenerateGo error %q does not mention %q", err.Error(), "nested mixin call")
	}
}

// TestCodegenMixinBlockDynamicUnbufferedCodeStillFailsClosed proves
// unbuffered code inside block content stays deferred even for a mixin whose
// parameter IS used elsewhere in the same block content: a param-only
// unbuffered local in block content is a distinct, untested claim this
// increment deliberately does not make.
func TestCodegenMixinBlockDynamicUnbufferedCodeStillFailsClosed(t *testing.T) {
	src := "mixin w(label)\n  block\n+w(\"L\")\n  p= label\n  - var x = \"hi\"\n  p= x\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p>L</p><p>hi</p>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q", src, want, "<p>L</p><p>hi</p>")
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for unbuffered code inside dynamic block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "block content") {
		t.Errorf("GenerateGo error %q does not mention %q", err.Error(), "block content")
	}
}

// TestCodegenMixinBlockDynamicDeferralsAreDistinct proves the deferral
// errors above are all genuinely different error text, not a single generic
// message reused everywhere.
func TestCodegenMixinBlockDynamicDeferralsAreDistinct(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"data field", "mixin w(label)\n  block\n+w(\"L\")\n  p= Title\n"},
		{"caller local", "- var callerLocal = \"leaked\"\nmixin w(label)\n  block\n+w(\"L\")\n  p= callerLocal\n"},
		{"attributes", "mixin w(label)\n  block\n+w(\"L\")\n  p= attributes.class\n"},
		{"if block", "mixin w(label)\n  block\n+w(\"L\")\n  if block\n    p yes\n  else\n    p no\n"},
		{"nested mixin call", "mixin inner(x)\n  p= x\nmixin outer(y)\n  .outer\n    block\n+outer(\"Y\")\n  +inner(\"Z\")\n"},
		{"unbuffered code", "mixin w(label)\n  block\n+w(\"L\")\n  p= label\n  - var x = \"hi\"\n  p= x\n"},
	}

	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genMixinErr(t, tc.src, false)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", tc.src)
			}
			if other, ok := seen[err.Error()]; ok {
				t.Errorf("deferral %q and %q produced the identical error text %q (expected distinct errors)", tc.name, other, err.Error())
			}
			seen[err.Error()] = tc.name
		})
	}
}

// TestCodegenMixinBlockDynamicFaultInjection proves the differential harness
// itself is non-vacuous for the dynamic block-content shape: a deliberately
// WRONG expected value must fail the comparison, so a passing differential
// above is actually exercising the generated code's output.
func TestCodegenMixinBlockDynamicFaultInjection(t *testing.T) {
	t.Parallel()
	src := "mixin w(label)\n  block\n+w(\"Hello\")\n  p= label\n"

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
	wrongWant := "<p>Goodbye</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}
