package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// mixinDataStruct is the declared struct the mixin differential tests
// resolve a data-derived call argument against: Title feeds a `+card(Title)`
// call, and both fields double as the "isolation" tests' invisible-field
// probes (a mixin body accidentally referencing Title or Name instead of its
// own declared parameter).
type mixinDataStruct struct {
	Title string
	Name  string
}

var mixinDataReflectType = reflect.TypeOf(mixinDataStruct{})

// mixinDataStructSrc is mixinDataStruct's field declarations, spliced
// verbatim into the throwaway module runComposedGo assembles around a
// GenerateGo result (see codegen_composition_test.go) — it must match
// mixinDataStruct above field for field.
const mixinDataStructSrc = `type mixinDataStruct struct {
	Title string
	Name  string
}
`

// runMixinDifferential parses src, generates it through GenerateGo against
// mixinDataStruct, builds and runs the result (via runComposedGo), separately
// renders it through the interpreter (Compile/Render) against data, and
// asserts the two outputs are byte-identical.
func runMixinDifferential(t *testing.T, src string, data map[string]any, dataLiteral string) {
	t.Helper()

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

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runComposedGo(t, generated, mixinDataStructSrc, dataLiteral, "RenderMixin")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// genMixinErr parses and GenerateGoes src against mixinDataStruct (unless
// noType is true, in which case Config.DataReflectType is left nil), always
// returning GenerateGo's error rather than fatally failing the test — the
// deferral tests below assert a specific error occurs, not that generation
// succeeds.
func genMixinErr(t *testing.T, src string, noType bool) error {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	cfg := Config{
		PackageName:     "main",
		FuncName:        "RenderMixin",
		DataType:        "mixinDataStruct",
		DataReflectType: mixinDataReflectType,
	}
	if noType {
		cfg.DataReflectType = nil
	}
	_, err = GenerateGo(ast, cfg)
	return err
}

// TestCodegenMixinBasicLiteralAndDataArg is the increment's headline case: a
// single-parameter mixin called once with a string literal and once with a
// data field, proving both the helper-function emission and the caller-side
// argument binding (genValueExpr in the CALLER's own scope) are byte-
// identical to the interpreter.
func TestCodegenMixinBasicLiteralAndDataArg(t *testing.T) {
	src := "mixin card(title)\n  .card\n    h1= title\n+card(\"Hello\")\n+card(Title)\n"
	runMixinDifferential(t, src, map[string]any{"Title": "World"}, `mixinDataStruct{Title: "World"}`)
}

// TestCodegenMixinMultiParamAttrAndCondition exercises a two-parameter mixin
// whose SECOND parameter is used both inside a dynamic attribute value and
// as an `if` condition, over two different argument sets — one where the
// condition parameter is truthy, one where it is the empty string (falsy).
func TestCodegenMixinMultiParamAttrAndCondition(t *testing.T) {
	src := strings.Join([]string{
		"mixin badge(label, kind)",
		"  span.badge(data-kind=kind)",
		"    if kind",
		"      = label",
		"    else",
		"      = \"none\"",
		"+badge(\"Alpha\", \"warn\")",
		"+badge(\"Beta\", \"\")",
		"",
	}, "\n")
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinMissingArgIsEmptyString proves a call with fewer arguments
// than the mixin declares binds the missing trailing parameter to the empty
// string, exactly like Runtime.renderMixinCall's own missing-arg default.
func TestCodegenMixinMissingArgIsEmptyString(t *testing.T) {
	src := "mixin greet(name)\n  p= name\n+greet()\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinExtraArgIsIgnored proves a call with MORE arguments than
// the mixin declares (no rest parameter) simply ignores the extras, exactly
// like Runtime.renderMixinCall's own binding loop (which only iterates up to
// len(decl.Parameters)).
func TestCodegenMixinExtraArgIsIgnored(t *testing.T) {
	src := "mixin greet(name)\n  p= name\n+greet(\"Sam\", \"extra\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinTwoMixinsMultipleCalls proves two independently declared
// mixins each get their own emitted helper function, and that the SAME
// mixin can be called more than once with different arguments — the helper
// function is reused, not re-emitted per call.
func TestCodegenMixinTwoMixinsMultipleCalls(t *testing.T) {
	src := strings.Join([]string{
		"mixin card(title)",
		"  .card",
		"    h1= title",
		"mixin tag(label)",
		"  span.tag= label",
		"+card(\"One\")",
		"+tag(\"X\")",
		"+card(\"Two\")",
		"",
	}, "\n")
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinIsolationDataFieldFailClosed documents the isolation
// boundary this increment enforces: a mixin body that references a
// top-level DATA FIELD instead of its own declared parameter renders ""
// under the interpreter (a plain lookup miss inside the mixin's isolated
// scope — Runtime.renderMixinCall), but GenerateGo refuses to reproduce that
// silently and returns a clean, fail-closed error instead.
func TestCodegenMixinIsolationDataFieldFailClosed(t *testing.T) {
	src := "mixin leak(title)\n  p= Title\n+leak(\"Hi\")\n"

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Title": "Visible"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p></p>" {
		t.Fatalf("interpreter Render(%q) = %q, want %q (documents the isolation the fail-closed codegen error below is guarding)", src, want, "<p></p>")
	}

	err = genMixinErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a fail-closed isolation error, got nil", src)
	}
	if !strings.Contains(err.Error(), "Title") {
		t.Errorf("GenerateGo error %q does not name the offending non-parameter identifier %q", err.Error(), "Title")
	}
}

// TestCodegenMixinIsolationCallerLocalFailClosed is
// TestCodegenMixinIsolationDataFieldFailClosed's sibling for a caller `- var`
// local instead of a top-level data field — also invisible inside the
// mixin's isolated scope under the interpreter, also fail-closed in codegen.
func TestCodegenMixinIsolationCallerLocalFailClosed(t *testing.T) {
	src := "- callerLocal = \"leaked\"\nmixin leak2(title)\n  p= callerLocal\n+leak2(\"Hi\")\n"

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
		t.Fatalf("GenerateGo(%q): expected a fail-closed isolation error, got nil", src)
	}
	if !strings.Contains(err.Error(), "callerLocal") {
		t.Errorf("GenerateGo error %q does not name the offending non-parameter identifier %q", err.Error(), "callerLocal")
	}
}

// TestCodegenMixinDeferrals asserts every construct this increment
// explicitly defers returns its own clean, distinct GenerateGo error rather
// than silently mis-generating.
func TestCodegenMixinDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		noType  bool
		wantSub string
	}{
		{
			name:    "block keyword in a mixin body",
			src:     "mixin wrap(title)\n  .box\n    h1= title\n    block\n+wrap(\"X\")\n",
			wantSub: "BlockNode",
		},
		{
			name:    "block content passed to a call",
			src:     "mixin wrap(title)\n  .box\n    h1= title\n+wrap(\"X\")\n  p child\n",
			wantSub: "block content",
		},
		{
			name:    "attributes forwarded at a call",
			src:     "mixin card(title)\n  .card\n    h1= title\n+card(\"X\", class=\"big\")\n",
			wantSub: "attributes",
		},
		{
			name:    "default parameter value",
			src:     "mixin foo(a = \"x\")\n  p= a\n+foo()\n",
			wantSub: "default parameter",
		},
		{
			name:    "rest parameter",
			src:     "mixin foo(...items)\n  p hi\n+foo(\"a\", \"b\")\n",
			wantSub: "rest parameter",
		},
		{
			name:    "nested mixin call",
			src:     "mixin inner(x)\n  p= x\nmixin outer(y)\n  +inner(y)\n+outer(\"Z\")\n",
			wantSub: "nested mixin call",
		},
		{
			name:    "nil DataReflectType",
			src:     "mixin card(title)\n  .card\n    h1= title\n+card(\"X\")\n",
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
			// Every deferral must be distinguishable from every other: no two
			// cases collapse to the identical error text.
			if other, ok := seen[err.Error()]; ok {
				t.Errorf("deferral %q and %q produced the identical error text %q (expected distinct errors)", tc.name, other, err.Error())
			}
			seen[err.Error()] = tc.name
		})
	}
}

// TestCodegenMixinFaultInjection proves the differential harness itself is
// non-vacuous: a deliberately WRONG expected value must fail the comparison,
// so a passing mixin differential test above is actually exercising the
// generated code's output, not merely checking it built and ran.
func TestCodegenMixinFaultInjection(t *testing.T) {
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

	got := runComposedGo(t, generated, mixinDataStructSrc, "mixinDataStruct{}", "RenderMixin")
	wrongWant := "<div class=\"card\"><h1>Goodbye</h1></div>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenMixinNoMixinRegression proves the helper-function-emission
// machinery this increment adds to GenerateGo does not perturb output for a
// template that declares no mixin at all: exactly one function (the render
// function itself) is emitted, and the differential output is unchanged.
func TestCodegenMixinNoMixinRegression(t *testing.T) {
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
