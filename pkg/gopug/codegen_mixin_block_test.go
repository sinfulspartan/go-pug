package gopug

import (
	"strings"
	"testing"
)

// TestCodegenMixinBlockStaticContentAtSlot is the increment's headline case:
// a mixin with a `block` slot called with static block content, proving the
// helper's block-callback parameter and the call site's closure agree with
// the interpreter byte-for-byte.
func TestCodegenMixinBlockStaticContentAtSlot(t *testing.T) {
	t.Parallel()
	src := "mixin card\n  .card\n    block\n+card\n  p Hello\n  span World\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockSlotNestedInTag proves the deep-scan that detects a
// mixin's block slot reaches a `block` nested inside a wrapping tag, not
// just one written as a direct child of the mixin declaration.
func TestCodegenMixinBlockSlotNestedInTag(t *testing.T) {
	t.Parallel()
	src := "mixin box\n  div.outer\n    block\n+box\n  p x\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockNoSlotContentDiscarded mirrors the interpreter's own
// TestMixinBlockContentSilentlyDiscarded: a mixin body with no `block` node
// called with block content discards that content — no error, no leaked
// output — and the helper gets no block-callback parameter at all.
func TestCodegenMixinBlockNoSlotContentDiscarded(t *testing.T) {
	t.Parallel()
	src := "mixin simple\n  p body\n+simple\n  p this should not appear\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockMultipleSlotsRenderTwice mirrors the probed
// interpreter behavior: a mixin body with two `block` nodes renders the
// caller's block content once per occurrence, not once total.
func TestCodegenMixinBlockMultipleSlotsRenderTwice(t *testing.T) {
	t.Parallel()
	src := "mixin twice\n  block\n  block\n+twice\n  p x\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockSlotNoContentPassesNil proves a call to a slot-
// bearing mixin with no indented block content passes a literal nil for the
// block-callback parameter, and the slot renders nothing — byte-identical
// to the interpreter's own empty-callerBlock behavior.
func TestCodegenMixinBlockSlotNoContentPassesNil(t *testing.T) {
	t.Parallel()
	src := "mixin card\n  .card\n    block\n+card\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockComposesWithOwnParams proves the block-callback
// parameter composes correctly with a mixin's own (slice-1-supported)
// parameters used inside its own body — the block param is simply one more
// argument alongside the positional string params.
func TestCodegenMixinBlockComposesWithOwnParams(t *testing.T) {
	t.Parallel()
	src := "mixin panel(title)\n  .panel\n    h1= title\n    block\n+panel(\"Hi\")\n  p body-content\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockClosureSurroundedByRealSiblingContent stresses
// genMixinBlockClosure's g.body/g.static save-and-restore: unlike
// genMixinFunc, which always runs before the main render function's own body
// walk has written anything, genMixinCall (and therefore the closure it
// builds) is reached FROM WITHIN that walk, with real, unrelated static
// content already sitting in g.body both before and after the call. This
// proves that content survives the swap untouched.
func TestCodegenMixinBlockClosureSurroundedByRealSiblingContent(t *testing.T) {
	t.Parallel()
	src := "p before\nmixin card\n  .card\n    block\np middle\n+card\n  p inside\np after\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicContentReferencingParamNowResolves documents
// the scope crux the block-content mechanism rests on: block content the
// interpreter renders against the CALLEE's own isolated parameter scope
// (Runtime.renderMixinBlockSlot, reached while the mixin's own param frame is
// active) — so a caller's block content referencing the mixin's OWN
// parameter name actually resolves it, a genuinely surprising interpreter
// behavior. Unlike a plain non-parameter reference (still fail-closed — see
// the data-field/caller-local tests below), a reference to a DECLARED
// parameter is exactly the shape genMixinBlockClosure's own param-scope
// mechanism models, so this is byte-identical rather than deferred; see
// codegen_mixin_block_dynamic_test.go for the fuller differential coverage
// of this mechanism.
func TestCodegenMixinBlockDynamicContentReferencingParamNowResolves(t *testing.T) {
	t.Parallel()
	src := "mixin w(label)\n  block\n+w(\"L\")\n  p= label\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinBlockDynamicContentReferencingDataFieldFailsClosed is
// TestCodegenMixinBlockDynamicContentReferencingParamFailsClosed's sibling
// for a caller's block content referencing a top-level DATA field instead
// of the mixin's own parameter — invisible under the interpreter's own
// isolated mixin scope (renders ""), also fail-closed in codegen.
func TestCodegenMixinBlockDynamicContentReferencingDataFieldFailsClosed(t *testing.T) {
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
		t.Fatalf("GenerateGo(%q): expected a fail-closed dynamic-block-content error, got nil", src)
	}
	if !strings.Contains(err.Error(), "Title") {
		t.Errorf("GenerateGo error %q does not name the offending identifier %q", err.Error(), "Title")
	}
}

// TestCodegenMixinBlockIfBlockAsValueFailsClosed asserts that `if block`
// used inside a mixin body — the runtime's own `block`-truthiness special
// case — fails closed in codegen rather than attempting to model it: `block`
// is just another bare identifier to resolveFieldExpr, and it is never
// bound in the mixin's own param-only scope, so it fails the exact same
// guard a non-parameter reference would.
func TestCodegenMixinBlockIfBlockAsValueFailsClosed(t *testing.T) {
	src := "mixin wrap(title)\n  .box\n    h1= title\n    if block\n      p has-block\n+wrap(\"X\")\n  p child\n"
	err := genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for `if block`, got nil", src)
	}
	if !strings.Contains(err.Error(), "block") {
		t.Errorf("GenerateGo error %q does not name the offending identifier %q", err.Error(), "block")
	}
}

// TestCodegenMixinBlockNestedMixinCallInContentFailsClosed asserts that a
// nested mixin call written as part of the block content passed to another
// mixin's call (`+outer("Y")\n  +inner("Z")`) stays deferred, exactly as a
// nested call inside a mixin's own body already is — the block-content
// closure must reach the very same nested-call deferral, not silently
// generate a call with the wrong (empty) scope.
func TestCodegenMixinBlockNestedMixinCallInContentFailsClosed(t *testing.T) {
	src := "mixin inner(x)\n  p= x\nmixin outer(y)\n  .outer\n    block\n+outer(\"Y\")\n  +inner(\"Z\")\n"
	err := genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for a nested mixin call in block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "nested mixin call") {
		t.Errorf("GenerateGo error %q does not mention %q", err.Error(), "nested mixin call")
	}
}

// TestCodegenMixinBlockUnbufferedCodeFailsClosed asserts that block content
// containing ANY unbuffered code statement (`- var x = "literal"`) is
// refused, even one whose right-hand side is a pure literal that would
// resolve purely against its own local scope entry with no external lookup
// at all — this increment's contract for block content is "pure static
// markup", not "anything that happens not to touch the isolation guard".
func TestCodegenMixinBlockUnbufferedCodeFailsClosed(t *testing.T) {
	src := "mixin card\n  .card\n    block\n+card\n  - var x = \"hi\"\n  p= x\n"
	err := genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed error for unbuffered code in block content, got nil", src)
	}
	if !strings.Contains(err.Error(), "block content") {
		t.Errorf("GenerateGo error %q does not mention %q", err.Error(), "block content")
	}
}

// TestCodegenMixinBlockDeferralsAreDistinct proves the two block-related
// deferral errors above (if-block-as-value and nested-mixin-call-in-content)
// produce genuinely different error text, not the same generic message.
func TestCodegenMixinBlockDeferralsAreDistinct(t *testing.T) {
	ifBlockSrc := "mixin wrap(title)\n  .box\n    h1= title\n    if block\n      p has-block\n+wrap(\"X\")\n  p child\n"
	nestedCallSrc := "mixin inner(x)\n  p= x\nmixin outer(y)\n  .outer\n    block\n+outer(\"Y\")\n  +inner(\"Z\")\n"

	err1 := genMixinErr(t, ifBlockSrc, false)
	err2 := genMixinErr(t, nestedCallSrc, false)
	if err1 == nil || err2 == nil {
		t.Fatalf("expected both deferrals to error, got %v and %v", err1, err2)
	}
	if err1.Error() == err2.Error() {
		t.Errorf("if-block-as-value and nested-mixin-call-in-content produced the identical error text %q", err1.Error())
	}
}

// TestCodegenMixinBlockFaultInjection proves the differential harness itself
// is non-vacuous for the block-content shape: a deliberately WRONG expected
// value must fail the comparison.
func TestCodegenMixinBlockFaultInjection(t *testing.T) {
	t.Parallel()
	src := "mixin card\n  .card\n    block\n+card\n  p Hello\n"

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
	wrongWant := "<div class=\"card\"><p>Goodbye</p></div>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenMixinBlockSlotlessSignatureUnperturbed proves adding
// block-callback support does not change the generated helper signature for
// a mixin with no `block` slot at all: the helper still takes exactly the
// same parameters (w plus one string per declared parameter) as it did
// before this increment.
func TestCodegenMixinBlockSlotlessSignatureUnperturbed(t *testing.T) {
	src := "mixin card(title)\n  .card\n    h1= title\n+card(\"Hello\")\n"

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
	if !strings.Contains(genStr, "arg1 string) error {") {
		t.Errorf("expected a slotless mixin helper signature ending in \"arg1 string) error {\" with no block-callback parameter, got:\n%s", genStr)
	}
	if strings.Contains(genStr, "func(io.Writer) error") {
		t.Errorf("expected no block-callback parameter type in a slotless mixin's generated source, got:\n%s", genStr)
	}
}

// TestCodegenMixinBlockNoMixinRegression is the no-mixin-declared regression
// this increment must not perturb: exactly one generated func for a template
// with no mixin at all, and unchanged differential output.
func TestCodegenMixinBlockNoMixinRegression(t *testing.T) {
	t.Parallel()
	src := "p Hello #{Name}\n"

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
	if n := strings.Count(string(generated), "\nfunc "); n != 1 {
		t.Errorf("expected exactly one generated func with no mixin declared, got %d in:\n%s", n, generated)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Name": "Ada"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runComposedGo(t, generated, mixinDataStructSrc, `mixinDataStruct{Name: "Ada"}`, "RenderMixin")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}
