package gopug

import (
	"strings"
	"testing"
)

// TestCodegenMixinRestEachOverRest is the increment's headline case — the
// primary use of a rest parameter — proving `each x in xs` over the
// collected `[]string` scope var flows through the SAME machinery genEach
// already uses for a slice-typed struct field (Kind() Slice/Array), with no
// new per-op code: a `[]string` scope var and a `[]any`-of-strings runtime
// value range-iterate byte-identically.
func TestCodegenMixinRestEachOverRest(t *testing.T) {
	t.Parallel()
	src := "mixin f(...xs)\n  each x in xs\n    li= x\n+f(\"a\", \"b\", \"c\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestPositionalAndRestSplit proves the call-site split at
// len(decl.Parameters): the first argument binds to the declared positional
// parameter exactly as every prior slice, and every argument after it is
// collected into the rest parameter — both when the call supplies rest
// arguments and when it supplies none (the rest parameter simply gets no
// extra arguments, not an error).
func TestCodegenMixinRestPositionalAndRestSplit(t *testing.T) {
	t.Parallel()
	src := "mixin f(a, ...xs)\n  h1= a\n  each x in xs\n    li= x\n+f(\"A\", \"b\", \"c\")\n+f(\"A\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestEmptyCall proves a call with NO extra arguments at all
// (`+f()`) binds the rest parameter to an empty slice — the each loop body
// never runs, `.length` reads 0 — matching Runtime.renderMixinCall's own
// `rest := make([]any, 0)` when its collection loop never iterates.
func TestCodegenMixinRestEmptyCall(t *testing.T) {
	t.Parallel()
	src := "mixin f(...xs)\n  each x in xs\n    li= x\n  p= xs.length\n+f()\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestLength proves `.length` on a rest parameter is
// SUPPORTED: genLengthOperand switches on typ.Kind() the same way for a
// []string scope var as for a slice struct field (Slice/Array/Map all use
// len(...)), so no new plumbing was needed — it is covered both with and
// without extra arguments.
func TestCodegenMixinRestLength(t *testing.T) {
	t.Parallel()
	src := "mixin f(...xs)\n  p= xs.length\n+f(\"a\", \"b\")\n+f()\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestIndex proves `xs[0]` on a rest parameter is SUPPORTED:
// genIndexValueExpr's Slice/Array case already type-switches the same way
// for any Slice-kind operand.
func TestCodegenMixinRestIndex(t *testing.T) {
	t.Parallel()
	src := "mixin f(...xs)\n  p= xs[0]\n+f(\"a\", \"b\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestIndexOutOfBounds pins the out-of-bounds behavior
// probed against the interpreter: both Runtime.indexValue (a negative or
// too-large numeric index against a slice) and genIndexValueExpr's own
// bounds check collapse to the empty string, so this is byte-identical
// without any special-casing for a rest parameter specifically.
func TestCodegenMixinRestIndexOutOfBounds(t *testing.T) {
	t.Parallel()
	src := "mixin f(...xs)\n  p= xs[5]\n+f(\"a\", \"b\")\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestJoin proves `.join(sep)` on a rest parameter is
// SUPPORTED: genJoinValueExpr's Slice/Array-only receiver check already
// covers any Slice-kind operand, and per-element fmt.Sprintf("%v", …)
// stringification of a string element is a no-op conversion — identical to
// the interpreter's own per-element stringify of a []any-of-strings.
func TestCodegenMixinRestJoin(t *testing.T) {
	t.Parallel()
	src := "mixin f(...xs)\n  p= xs.join(\", \")\n+f(\"a\", \"b\", \"c\")\n+f()\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestComposesWithDefaultParam proves a rest parameter
// composes with a slice-5 default positional parameter in the same
// declaration: the default substitutes for the missing positional argument
// exactly as before, and the split point for the rest collection is still
// len(decl.Parameters) — the default parameter counts toward that split
// even when the call omits it.
func TestCodegenMixinRestComposesWithDefaultParam(t *testing.T) {
	t.Parallel()
	src := "mixin f(a = \"d\", ...xs)\n  h1= a\n  each x in xs\n    li= x\n+f(\"A\", \"b\", \"c\")\n+f()\n"
	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestDeferrals asserts every rest-parameter construct this
// increment cannot prove byte-identical (or that needs new plumbing beyond
// the existing slice machinery) returns its own clean, distinct GenerateGo
// error rather than silently mis-generating.
func TestCodegenMixinRestDeferrals(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			name:    "fallible rest argument (division by zero)",
			src:     "mixin f(...xs)\n  each x in xs\n    li= x\n+f(1/0)\n",
			wantSub: "fallible expression",
		},
		{
			name:    "whole-slice stringify via `=`",
			src:     "mixin wholeA(...xs)\n  p= xs\n+wholeA(\"a\", \"b\")\n",
			wantSub: "non-scalar",
		},
		{
			name:    "whole-slice stringify via `#{}` interpolation",
			src:     "mixin wholeB(...xs)\n  p #{xs}\n+wholeB(\"a\", \"b\")\n",
			wantSub: "non-scalar",
		},
		{
			name:    "rest parameter referenced in dynamic block content",
			src:     "mixin w(...xs)\n  block\n+w(\"a\", \"b\")\n  p= xs\n",
			wantSub: "xs",
		},
		{
			// genMixinCallAttrForward checks decl.RestParamName up front,
			// before it ever looks at whether the mixin has a block slot —
			// so this hits the SAME blanket rest-parameter check a plain
			// &attributes-forwarding rest-param mixin would, not a
			// block-slot-specific one; the block in the source is there only
			// to prove that check fires before block-slot detection, not
			// after it.
			name:    "rest parameter on a mixin that forwards &attributes (block slot present)",
			src:     "mixin w(...xs)\n  a&attributes(attributes)\n    block\n+w(\"a\", \"b\")(class=\"x\")\n  p x\n",
			wantSub: "rest parameter",
		},
		{
			name:    "rest parameter on a mixin that forwards &attributes (no block slot)",
			src:     "mixin box(...xs)\n  a&attributes(attributes)\n+box(\"a\", \"b\")(class=\"x\")\n",
			wantSub: "rest parameter",
		},
		{
			name:    "rest parameter with nil DataReflectType",
			src:     "mixin f(...xs)\n  each x in xs\n    li= x\n+f(\"a\")\n",
			wantSub: "DataReflectType",
		},
	}

	seen := make(map[string]string, len(cases))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			noType := tc.name == "rest parameter with nil DataReflectType"
			err := genMixinErr(t, tc.src, noType)
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

// TestCodegenMixinRestFaultInjection proves the differential harness itself
// is non-vacuous for the rest-parameter shape: a deliberately WRONG expected
// value must fail the comparison, so the passing differentials above are
// actually exercising the generated code's output.
func TestCodegenMixinRestFaultInjection(t *testing.T) {
	t.Parallel()
	src := "mixin f(...xs)\n  each x in xs\n    li= x\n+f(\"a\", \"b\")\n"

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
	wrongWant := "<li>x</li><li>y</li>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenMixinRestNoRestParamIsNoOp proves the rest-parameter plumbing
// this increment adds to genMixinFunc/genMixinCall is a no-op for a mixin
// that declares NO rest parameter at all: the helper's signature gains no
// `[]string` parameter, and the call site passes no extra slice-literal
// argument — exactly the Go source every prior slice already emitted.
func TestCodegenMixinRestNoRestParamIsNoOp(t *testing.T) {
	t.Parallel()
	src := "mixin greet(name)\n  p= name\n+greet(\"Sam\")\n"

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

	if !strings.Contains(genStr, "func pugMixin_greet(w io.Writer, arg1 string) error {") {
		t.Errorf("expected an unchanged, rest-param-less helper signature, got:\n%s", genStr)
	}
	if strings.Contains(genStr, "[]string") {
		t.Errorf("expected no []string rest parameter anywhere in a rest-param-less mixin's generated source, got:\n%s", genStr)
	}
	if !strings.Contains(genStr, `pugMixin_greet(w, "Sam")`) {
		t.Errorf("expected an unchanged call site with no extra slice-literal argument, got:\n%s", genStr)
	}

	runMixinDifferential(t, src, map[string]any{}, "mixinDataStruct{}")
}

// TestCodegenMixinRestNoMixinRegression is the no-mixin-declared regression
// this increment must not perturb: exactly one generated func for a
// template with no mixin at all, and unchanged differential output.
func TestCodegenMixinRestNoMixinRegression(t *testing.T) {
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
