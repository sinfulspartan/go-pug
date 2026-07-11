package gopug

import (
	"sync"
	"testing"
)

// newExprTestRuntime builds a Runtime with data installed in the root scope
// frame, mirroring what Render does before walking the AST. It lets these
// tests call evaluateExpr and compiled closures directly without going
// through the full lex/parse/render pipeline.
func newExprTestRuntime(data map[string]any) *Runtime {
	r := NewRuntime(&DocumentNode{}, data)
	r.scopeStack[0] = data
	return r
}

// exprDataScenarios covers the data shapes called out in the task: a
// present var, a missing var, nested struct/map values, an empty string, a
// number, a bool, and a slice.
func exprDataScenarios() []map[string]any {
	return []map[string]any{
		{
			"name":     "World",
			"count":    42,
			"price":    9.99,
			"flag":     true,
			"emptyStr": "",
			"items":    []any{"a", "b", "c"},
			"person": Person{
				Name:    "Alice",
				Age:     30,
				Address: Address{City: "Wonderland"},
			},
			"nested": map[string]any{
				"inner": map[string]any{"value": "deep"},
			},
		},
		{}, // nothing defined: every lookup misses
		{
			"count":    0,
			"flag":     false,
			"emptyStr": "",
			"items":    []any{},
			"person":   Person{},
		},
	}
}

// exprSupportedShapes lists expressions that must classify to a non-nil
// compiledExpr under the task's whitelist (string literal, bare identifier,
// dot-path).
func exprSupportedShapes() []string {
	return []string{
		`"hello"`,
		`'hello'`,
		`""`,
		`''`,
		`"say \"hi\""`,
		`'it\'s fine'`,
		"name",
		"missingVar",
		"count",
		"flag",
		"emptyStr",
		"items",
		"price",
		"person.Name",
		"person.Address.City",
		"person.Age",
		"nested.inner.value",
		"unknownRoot.field",
		"person.Unknown",
	}
}

// exprUnsupportedShapes lists expressions that must classify to nil so the
// interpreter fallback runs unchanged.
func exprUnsupportedShapes() []string {
	return []string{
		"count + 1",
		`flag ? "yes" : "no"`,
		"items[0]",
		"person.Name.toUpperCase()",
		"!flag",
		"count == 42",
		"count && flag",
		"count || price",
		"(count)",
		"42",
		"3.14",
		"true",
		"false",
		"null",
		"undefined",
		"nil",
		"block",
		"Infinity",
		"NaN",
		"Inf",
		"a.length",
		"a.toFixed",
		"a.toUpperCase",
		"a .b",
		".a",
		"a.",
		"a..b",
		"-name",
		"a.b.c()",
		"`literal ${x}`",
		"",
	}
}

// TestClassifyExprSupportedShapesMatchEvaluateExpr is the differential proof:
// for every supported shape, across every data scenario, the compiled
// closure must return exactly the same string and error-ness as
// evaluateExpr for that expression.
func TestClassifyExprSupportedShapesMatchEvaluateExpr(t *testing.T) {
	for _, expr := range exprSupportedShapes() {
		compiled := classifyExpr(expr)
		if compiled == nil {
			t.Errorf("classifyExpr(%q) = nil, want a compiled closure for a supported shape", expr)
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

// TestClassifyExprUnsupportedShapesReturnNil verifies the conservative
// whitelist: anything with operators, ternary, parens, indexing, method
// calls, interpolation, or reserved words classifies to nil so the
// interpreter fallback is used unchanged.
func TestClassifyExprUnsupportedShapesReturnNil(t *testing.T) {
	for _, expr := range exprUnsupportedShapes() {
		if compiled := classifyExpr(expr); compiled != nil {
			t.Errorf("classifyExpr(%q) = non-nil, want nil (unsupported shape)", expr)
		}
	}
}

// TestClosureCompiledOutputMatchesInterpreterOutput renders templates that
// exercise every supported shape inside loops and mixins once with the
// compiled closures in place, and once with them cleared (forcing the
// string-interpreter fallback for every node), and asserts the HTML is
// byte-identical.
func TestClosureCompiledOutputMatchesInterpreterOutput(t *testing.T) {
	cases := []struct {
		name string
		src  string
		data map[string]any
	}{
		{
			name: "bare identifier in a loop",
			src:  "ul\n  each item in items\n    li= item",
			data: map[string]any{"items": []any{"a", "b", "c"}},
		},
		{
			name: "dot path in a mixin",
			src:  "mixin card(p)\n  div= p.Name\n  div= p.Address.City\n+card(person)",
			data: map[string]any{"person": Person{Name: "Alice", Address: Address{City: "Wonderland"}}},
		},
		{
			name: "string literals buffered and unescaped",
			src:  "p= \"hello\"\np!= 'raw <b>text</b>'",
			data: map[string]any{},
		},
		{
			name: "mixed identifier, dot path, and literal in one loop",
			src:  "ul\n  each product in products\n    li\n      span= product.name\n      span= product.price\n      span= \"unit\"",
			data: map[string]any{
				"products": []any{
					map[string]any{"name": "Widget", "price": "9.99"},
					map[string]any{"name": "Gadget", "price": "19.99"},
				},
			},
		},
		{
			name: "missing variable renders empty",
			src:  "p= missing",
			data: map[string]any{},
		},
	}

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

			clearCompiledExprs(tpl.ast.Children)

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

// TestUnsupportedShapesStillRenderViaFallback confirms that expressions
// classifying to nil still render correctly through the untouched
// interpreter fallback in renderCode.
func TestUnsupportedShapesStillRenderViaFallback(t *testing.T) {
	out := renderTest(t, "p= a + b", map[string]interface{}{"a": 2, "b": 3})
	assertContains(t, out, "5")

	out2 := renderTest(t, `p= cond ? x : y`, map[string]interface{}{"cond": true, "x": "yes", "y": "no"})
	assertContains(t, out2, "yes")

	out3 := renderTest(t, "p= items[0]", map[string]interface{}{"items": []interface{}{"first", "second"}})
	assertContains(t, out3, "first")
}

// TestCompiledExprConcurrentRenderSafety proves the compiled closure field
// on CodeNode can be read concurrently by multiple renders of the same
// compiled Template without a race, since it is populated once at Compile
// time and never mutated afterward.
func TestCompiledExprConcurrentRenderSafety(t *testing.T) {
	src := "ul\n  each item in items\n    li\n      span= item.Name\n      span= item.Address.City\n      span= \"unit\""
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	data := map[string]any{
		"items": []any{
			Person{Name: "Alice", Address: Address{City: "Wonderland"}},
			Person{Name: "Bob", Address: Address{City: "Springfield"}},
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
	assertContains(t, results[0], "Alice")
	assertContains(t, results[0], "Wonderland")
}

// clearCompiledExprs walks an AST and clears every CodeNode's compiled
// closure, forcing renderCode to fall back to the string interpreter. Used
// to capture the "before closures" baseline for the identical-output test.
func clearCompiledExprs(nodes []Node) {
	walkCodeNodes(nodes, func(code *CodeNode) {
		code.compiled = nil
	})
}
