package gopug

import (
	"sync"
	"testing"
)

// mixinArgSupportedShapes lists mixin-call argument expressions that must
// classify to a non-nil compiledExpr under the same whitelist classifyExpr
// already applies to `= expr` output nodes.
func mixinArgSupportedShapes() []string {
	return []string{
		`"hello"`,
		`'hello'`,
		"name",
		"missingVar",
		"count",
		"person.Name",
		"person.Address.City",
		"nested.inner.value",
		"unknownRoot.field",
	}
}

// mixinArgUnsupportedShapes lists mixin-call argument expressions that must
// classify to nil so renderMixinCall keeps falling back to evaluateExpr.
func mixinArgUnsupportedShapes() []string {
	return []string{
		"a + b",
		`cond ? x : y`,
		"arr[0]",
		"person.Name.toUpperCase()",
	}
}

// TestMixinArgEvaluateExprEntryPointProof is the fresh proof the standing
// rule requires before reusing classifyExpr at a new call site: it confirms
// renderMixinCall resolves a positional argument through the very same
// evaluateExpr entry point that CodeNode output nodes used, by rendering a
// mixin call whose argument is a supported dot-path shape and comparing it
// directly against r.evaluateExpr(arg) called on an equivalent runtime. If
// renderMixinCall routed arguments through some other path (a different
// stringify helper, extra formatting, etc.) this would fail even though
// classifyExpr itself is unchanged.
func TestMixinArgEvaluateExprEntryPointProof(t *testing.T) {
	data := map[string]any{
		"person": Person{Name: "Alice", Address: Address{City: "Wonderland"}},
	}

	out := renderTest(t, "mixin card(p)\n  div= p\n+card(person.Address.City)", data)

	r := newExprTestRuntime(data)
	want, err := r.evaluateExpr("person.Address.City")
	if err != nil {
		t.Fatalf("evaluateExpr error: %v", err)
	}

	assertContains(t, out, "<div>"+want+"</div>")
}

// TestClassifyExprSupportedShapesMatchEvaluateExprAsMixinArgs re-runs the
// buffered-output-node differential proof but frames every expression as a
// mixin-call argument would see it, confirming the same classifyExpr
// closures produce output identical to evaluateExpr regardless of call site.
func TestClassifyExprSupportedShapesMatchEvaluateExprAsMixinArgs(t *testing.T) {
	for _, expr := range mixinArgSupportedShapes() {
		compiled := classifyExpr(expr)
		if compiled == nil {
			t.Errorf("classifyExpr(%q) = nil, want a compiled closure for a supported mixin-arg shape", expr)
			continue
		}

		for i, data := range exprDataScenarios() {
			r := newExprTestRuntime(data)
			got, gotErr := compiled(r)

			r2 := newExprTestRuntime(data)
			want, wantErr := r2.evaluateExpr(expr)

			if (gotErr == nil) != (wantErr == nil) {
				t.Errorf("scenario %d: classifyExpr(%q) error-ness mismatch: compiled err=%v, evaluateExpr err=%v", i, expr, gotErr, wantErr)
				continue
			}
			if got != want {
				t.Errorf("scenario %d: classifyExpr(%q) = %q, evaluateExpr(%q) = %q", i, expr, got, expr, want)
			}
		}
	}
}

// TestMixinArgUnsupportedShapesClassifyToNil confirms mixin-call argument
// expressions with operators, ternary, indexing, or method calls still
// classify to nil, so renderMixinCall keeps evaluating them through the
// string interpreter unchanged.
func TestMixinArgUnsupportedShapesClassifyToNil(t *testing.T) {
	for _, expr := range mixinArgUnsupportedShapes() {
		if compiled := classifyExpr(expr); compiled != nil {
			t.Errorf("classifyExpr(%q) = non-nil, want nil (unsupported mixin-arg shape)", expr)
		}
	}
}

// TestCompileMixinArgsReachesNestedCalls asserts compileMixinArgs's walk
// finds mixin calls wherever they appear: inside an each loop, inside an if,
// inside another mixin's body, and inside block content passed to a mixin
// call.
func TestCompileMixinArgsReachesNestedCalls(t *testing.T) {
	src := `mixin inner(x)
  span= x
mixin outer(y)
  div= y
  block
mixin wrapper(z)
  +inner(z)
ul
  each item in items
    li
      +inner(item.name)
if flag
  +inner(flag)
+outer(topLevel)
  +wrapper(nested)
`
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	var calls []*MixinCallNode
	walkMixinCallNodes(tpl.ast.Children, func(call *MixinCallNode) {
		calls = append(calls, call)
	})

	if len(calls) == 0 {
		t.Fatalf("walkMixinCallNodes found no mixin calls, want several")
	}

	for _, call := range calls {
		if len(call.Arguments) == 0 {
			continue
		}
		if call.compiledArgs == nil {
			t.Errorf("mixin call %q: compiledArgs is nil, want a slice populated by compileMixinArgs", call.Name)
			continue
		}
		if len(call.compiledArgs) != len(call.Arguments) {
			t.Errorf("mixin call %q: compiledArgs has %d entries, want %d (matching Arguments)", call.Name, len(call.compiledArgs), len(call.Arguments))
		}
	}
}

// TestMixinArgClosureCompiledOutputMatchesInterpreterOutput renders
// templates with mixin calls (including inside an each loop) once with the
// compiled argument closures in place, and once with them cleared, and
// asserts the HTML is byte-identical.
func TestMixinArgClosureCompiledOutputMatchesInterpreterOutput(t *testing.T) {
	cases := []struct {
		name string
		src  string
		data map[string]any
	}{
		{
			name: "identifier, dot-path, and literal args",
			src:  "mixin card(id, city, label)\n  div(data-id=id)= city + \" \" + label\n+card(userId, person.Address.City, \"member\")",
			data: map[string]any{
				"userId": "42",
				"person": Person{Address: Address{City: "Wonderland"}},
			},
		},
		{
			name: "mixin call inside each",
			src:  "ul\n  each product in products\n    li\n      +item(product.name, product.price)",
			data: map[string]any{
				"products": []any{
					map[string]any{"name": "Widget", "price": "9.99"},
					map[string]any{"name": "Gadget", "price": "19.99"},
				},
			},
		},
	}

	// The template above referencing "item" needs its declaration; add it
	// inline per case instead of relying on a shared preamble.
	cases[1].src = "mixin item(name, price)\n  span= name\n  span= price\nul\n  each product in products\n    li\n      +item(product.name, product.price)"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}

			withClosures, err := tpl.Render(tc.data)
			if err != nil {
				t.Fatalf("Render (closures) error: %v", err)
			}

			clearCompiledMixinArgs(tpl.ast.Children)

			withoutClosures, err := tpl.Render(tc.data)
			if err != nil {
				t.Fatalf("Render (interpreter fallback) error: %v", err)
			}

			if withClosures != withoutClosures {
				t.Fatalf("closure-compiled output differs from interpreter output:\nclosures: %q\ninterp:   %q", withClosures, withoutClosures)
			}
		})
	}
}

// TestMixinArgFallbackStillRendersUnsupportedShapes confirms mixin calls
// with argument shapes classifyExpr rejects (a negated identifier, a bare
// numeric literal) still render correctly through the untouched evaluateExpr
// fallback, including inside the rest-parameter loop, and that a rest-arg
// call with plain identifiers still works end to end.
//
// This deliberately avoids a bare positional argument containing a
// space-separated operator or ternary (e.g. `a + b`, `cond ? x : y`):
// probing during this task found that the lexer's unnamed positional
// argument scanner (scanAttributeValue, used only for bare mixin-call
// arguments — named attribute values already stitch operators via
// scanAttrValueFull) splits those into several separate Arguments instead of
// one, which is a pre-existing parser gap unrelated to closure-compiling
// arguments. It reproduces on the unmodified interpreter path with no
// closures involved, so fixing it is out of scope here; it is called out
// separately for the architect.
func TestMixinArgFallbackStillRendersUnsupportedShapes(t *testing.T) {
	out := renderTest(t, "mixin sum(v)\n  p= v\n+sum(!flag)", map[string]interface{}{"flag": false})
	assertContains(t, out, "<p>true</p>")

	out2 := renderTest(t, "mixin sum(v)\n  p= v\n+sum(42)", map[string]interface{}{})
	assertContains(t, out2, "<p>42</p>")

	out3 := renderTest(t, "mixin list(first, ...rest)\n  p= first\n  each r in rest\n    span= r\n+list(a, b, c)", map[string]interface{}{
		"a": "one",
		"b": "two",
		"c": "three",
	})
	assertContains(t, out3, "<p>one</p>")
	assertContains(t, out3, "<span>two</span>")
	assertContains(t, out3, "<span>three</span>")

	out4 := renderTest(t, "mixin list(first, ...rest)\n  p= first\n  each r in rest\n    span= r\n+list(a, !flag, 42)", map[string]interface{}{
		"a":    "one",
		"flag": false,
	})
	assertContains(t, out4, "<p>one</p>")
	assertContains(t, out4, "<span>true</span>")
	assertContains(t, out4, "<span>42</span>")
}

// TestMixinArgCompiledConcurrentRenderSafety proves the compiledArgs field
// on MixinCallNode can be read concurrently by multiple renders of the same
// compiled Template without a race, since it is populated once at Compile
// time and never mutated afterward.
func TestMixinArgCompiledConcurrentRenderSafety(t *testing.T) {
	src := "mixin item(name, price)\n  span= name\n  span= price\nul\n  each product in products\n    li\n      +item(product.Name, product.Price)"
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	data := map[string]any{
		"products": []any{
			struct {
				Name  string
				Price string
			}{"Widget", "9.99"},
			struct {
				Name  string
				Price string
			}{"Gadget", "19.99"},
		},
	}

	const goroutines = 16
	results := make([]string, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = tpl.Render(data)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: Render error: %v", i, err)
		}
	}
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d rendered %q, want %q (same as goroutine 0)", i, results[i], results[0])
		}
	}
	assertContains(t, results[0], "Widget")
	assertContains(t, results[0], "9.99")
}

// clearCompiledMixinArgs walks an AST and clears every MixinCallNode's
// compiledArgs slice, forcing renderMixinCall to fall back to the string
// interpreter for every argument. Used to capture the "before closures"
// baseline for the identical-output test.
func clearCompiledMixinArgs(nodes []Node) {
	walkMixinCallNodes(nodes, func(call *MixinCallNode) {
		call.compiledArgs = nil
	})
}
