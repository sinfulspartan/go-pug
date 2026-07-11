package gopug

import (
	"testing"
)

// evalWithFastPathOff resets exprFastPathDisabledForTests before and after
// the call so this is the only place in the suite that toggles the global,
// keeping it safe under the package's normal (non-parallel) test execution.
func evalWithFastPathOff(r *Runtime, expr string) (string, error) {
	exprFastPathDisabledForTests = true
	defer func() { exprFastPathDisabledForTests = false }()
	return r.evaluateExpr(expr)
}

// fastPathExprScenarios is the superset of expressions exercised by the
// equivalence test: the three trivial shapes (with tricky literal contents,
// and every documented rejection), plus a set of non-trivial expressions
// that must keep going through the full operator-scan chain.
func fastPathExprScenarios() []string {
	return []string{
		// Quoted literals, including tricky contents.
		`"hello"`,
		`'hello'`,
		`""`,
		`''`,
		`"say \"hi\""`,
		`'it\'s fine'`,
		`"has spaces and + - * / operators inside"`,
		`"1 + 1"`,
		`"a ? b : c"`,
		`'/'`,
		`"en"`,
		`"trailing backslash \\"`,

		// Bare identifiers, accepted.
		"name",
		"missingVar",
		"count",
		"flag",
		"emptyStr",
		"items",
		"price",
		"_underscore",
		"$dollar",

		// Bare identifiers, rejected (must still work via the long path).
		"true",
		"false",
		"null",
		"undefined",
		"nil",
		"block",
		"Infinity",
		"NaN",
		"Inf",
		"42",
		"3.14",

		// Dot-paths, accepted.
		"person.Name",
		"person.Address.City",
		"person.Age",
		"nested.inner.value",
		"unknownRoot.field",
		"person.Unknown",

		// Dot-paths, rejected (builtin method/property segment anywhere).
		"a.length",
		"a.toFixed",
		"a.toUpperCase",
		"person.Name.length",
		"a .b",
		".a",
		"a.",
		"a..b",

		// Non-trivial expressions that must never take the fast path.
		"count + 1",
		`flag ? "yes" : "no"`,
		"items[0]",
		"person.Name.toUpperCase()",
		"!flag",
		"count == 42",
		"count && flag",
		"count || price",
		"(count)",
		`("hello")`,
		"(name)",
		"a + b",
		"c ? x : y",
		"a.b()",
		"arr[0]",
		"`literal ${name}`",
		"",
		"   ",
	}
}

// TestFastPathNeverChangesEvaluateExprResult is the equivalence proof: for
// every expression and every data scenario, evaluateExpr must return the
// exact same (string, error-ness) whether or not the tryEvalSimple
// fast-path runs. The fast-path is a pure short-circuit — it must never
// change a result, only how quickly the trivial shapes are reached.
func TestFastPathNeverChangesEvaluateExprResult(t *testing.T) {
	for _, expr := range fastPathExprScenarios() {
		for i, data := range exprDataScenarios() {
			r1 := newExprTestRuntime(data)
			gotFast, gotFastErr := r1.evaluateExpr(expr)

			r2 := newExprTestRuntime(data)
			gotSlow, gotSlowErr := evalWithFastPathOff(r2, expr)

			if (gotFastErr == nil) != (gotSlowErr == nil) {
				t.Errorf("scenario %d: evaluateExpr(%q) error-ness mismatch: fast-path err=%v, long-path err=%v", i, expr, gotFastErr, gotSlowErr)
				continue
			}
			if gotFast != gotSlow {
				t.Errorf("scenario %d: evaluateExpr(%q) = %q with fast-path, %q with long-path", i, expr, gotFast, gotSlow)
			}
		}
	}
}

// TestTryEvalSimpleAcceptedShapesMatchLongPath is a second, narrower proof
// focused specifically on tryEvalSimple: for every expression it accepts
// (ok == true), the value it returns must equal what the long path (fast
// path forced off) computes for that same expression.
func TestTryEvalSimpleAcceptedShapesMatchLongPath(t *testing.T) {
	accepted := []string{
		`"hello"`,
		`""`,
		`'it\'s fine'`,
		"name",
		"missingVar",
		"count",
		"person.Name",
		"nested.inner.value",
		"unknownRoot.field",
	}

	for _, expr := range accepted {
		for i, data := range exprDataScenarios() {
			r1 := newExprTestRuntime(data)
			got, ok := r1.tryEvalSimple(expr)
			if !ok {
				t.Fatalf("scenario %d: tryEvalSimple(%q) rejected an expression expected to be accepted", i, expr)
			}

			r2 := newExprTestRuntime(data)
			want, err := evalWithFastPathOff(r2, expr)
			if err != nil {
				t.Fatalf("scenario %d: long-path evaluateExpr(%q) returned unexpected error: %v", i, expr, err)
			}

			if got != want {
				t.Errorf("scenario %d: tryEvalSimple(%q) = %q, long-path evaluateExpr(%q) = %q", i, expr, got, expr, want)
			}
		}
	}
}

// TestFastPathRejectsParenWrappedTrivialExpr confirms the paren-unwrap
// interaction called out in the task: a paren-wrapped trivial expression is
// rejected by tryEvalSimple's detectors (they don't unwrap parens), so it
// correctly falls through to the long path's own paren-unwrap step and
// renders the same as before.
func TestFastPathRejectsParenWrappedTrivialExpr(t *testing.T) {
	data := map[string]any{"name": "World"}

	for _, expr := range []string{`("hello")`, "(name)", "(count)"} {
		r := newExprTestRuntime(map[string]any{"name": "World", "count": 42})
		if _, ok := r.tryEvalSimple(expr); ok {
			t.Errorf("tryEvalSimple(%q) accepted a paren-wrapped expression, want rejection", expr)
		}
	}

	out := renderTest(t, "p= (name)", data)
	assertContains(t, out, "World")
}

// TestFastPathRenderByteIdentical renders the large benchmark template and a
// template with a variety of dynamic attribute values with the fast-path on
// and off, and asserts the HTML is byte-identical in both.
func TestFastPathRenderByteIdentical(t *testing.T) {
	t.Run("large template", func(t *testing.T) {
		tpl, err := Compile(largeSrc, nil)
		if err != nil {
			t.Fatalf("Compile error: %v", err)
		}
		data := largeData()

		withFastPath, err := tpl.Render(data)
		if err != nil {
			t.Fatalf("Render (fast-path) error: %v", err)
		}

		exprFastPathDisabledForTests = true
		withoutFastPath, err := tpl.Render(data)
		exprFastPathDisabledForTests = false
		if err != nil {
			t.Fatalf("Render (long-path) error: %v", err)
		}

		if withFastPath != withoutFastPath {
			t.Fatalf("fast-path render differs from long-path render:\nfast-path: %q\nlong-path: %q", withFastPath, withoutFastPath)
		}
	})

	t.Run("dynamic attributes", func(t *testing.T) {
		src := `div
  a(href=url)= linkText
  div(class=cls)= body
  input(value=x)
  div(class="static")= body
  a(href="/") Home`
		data := map[string]interface{}{
			"url":      "/products/42",
			"linkText": "View product",
			"cls":      "highlighted item",
			"body":     "Some body text",
			"x":        "prefilled",
		}

		tpl, err := Compile(src, nil)
		if err != nil {
			t.Fatalf("Compile error: %v", err)
		}

		withFastPath, err := tpl.Render(data)
		if err != nil {
			t.Fatalf("Render (fast-path) error: %v", err)
		}

		exprFastPathDisabledForTests = true
		withoutFastPath, err := tpl.Render(data)
		exprFastPathDisabledForTests = false
		if err != nil {
			t.Fatalf("Render (long-path) error: %v", err)
		}

		if withFastPath != withoutFastPath {
			t.Fatalf("fast-path render differs from long-path render:\nfast-path: %q\nlong-path: %q", withFastPath, withoutFastPath)
		}
	})
}

// TestFastPathRejectedShapesStillRenderCorrectly confirms that expressions
// the fast-path rejects (operators, ternary, index, method calls) still
// produce the correct rendered output through the unchanged long path.
func TestFastPathRejectedShapesStillRenderCorrectly(t *testing.T) {
	out := renderTest(t, "p= a + b", map[string]interface{}{"a": 2, "b": 3})
	assertContains(t, out, "5")

	out2 := renderTest(t, `div(class=a ? b : c)`, map[string]interface{}{"a": true, "b": "yes", "c": "no"})
	assertContains(t, out2, `class="yes"`)

	out3 := renderTest(t, "p= items[0]", map[string]interface{}{"items": []interface{}{"first", "second"}})
	assertContains(t, out3, "first")

	out4 := renderTest(t, "p= name.toUpperCase()", map[string]interface{}{"name": "world"})
	assertContains(t, out4, "WORLD")
}
