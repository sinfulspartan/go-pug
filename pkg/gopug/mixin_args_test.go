package gopug

import "testing"

// lexAttrNames lexes src (expected to be a single tag or mixin-call line)
// and returns the value of every TokenAttrName token it emits, in order.
// It is used to inspect exactly how scanAttributes split an attribute or
// mixin-call argument list, independent of the parser and runtime.
func lexAttrNames(t *testing.T, src string) []string {
	t.Helper()
	toks, err := NewLexer(src).Lex()
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", src, err)
	}
	var names []string
	for _, tok := range toks {
		if tok.Type == TokenAttrName {
			names = append(names, tok.Value)
		}
	}
	return names
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d attr names %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("attr name %d = %q, want %q (full got=%q want=%q)", i, got[i], want[i], got, want)
		}
	}
}

// TestBarePositionalArgOperatorStitching is the repro-then-fix set for the
// lexer bug where a bare, unnamed positional argument containing a top-level
// operator, ternary, or bracket-index mis-tokenized into several separate
// arguments instead of the single JS expression pugjs treats it as.
func TestBarePositionalArgOperatorStitching(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"addition", "+item(a + b)", []string{"a + b"}},
		{"ternary", "+card(c ? x : y)", []string{"c ? x : y"}},
		{"bracket index", "+m(arr[0])", []string{"arr[0]"}},
		{"dot-path addition", "+item(a.b + c.d)", []string{"a.b + c.d"}},
		{"operator arg between simple args", "+item(x, a + b, y)", []string{"x", "a + b", "y"}},
		{"bracket index and operator arg together", "+item(a, arr[0], b + c, x ? y : z)", []string{"a", "arr[0]", "b + c", "x ? y : z"}},
		{"bracket-then-method-call chain", "+item(arr[0].toUpperCase())", []string{"arr[0].toUpperCase()"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStringSlicesEqual(t, lexAttrNames(t, tc.src), tc.want)
		})
	}
}

// TestBarePositionalArgOperatorStitchingRenders confirms the stitched
// single-argument expressions from TestBarePositionalArgOperatorStitching
// don't just lex correctly but also evaluate to the right value end to end.
func TestBarePositionalArgOperatorStitchingRenders(t *testing.T) {
	out := renderTest(t, "mixin sum(v)\n  p= v\n+sum(a + b)", map[string]interface{}{
		"a": 2,
		"b": 3,
	})
	assertContains(t, out, "<p>5</p>")

	out2 := renderTest(t, "mixin pick(v)\n  p= v\n+pick(flag ? yes : no)", map[string]interface{}{
		"flag": true,
		"yes":  "chosen",
		"no":   "other",
	})
	assertContains(t, out2, "<p>chosen</p>")

	out3 := renderTest(t, "mixin first(v)\n  p= v\n+first(items[0])", map[string]interface{}{
		"items": []interface{}{"one", "two"},
	})
	assertContains(t, out3, "<p>one</p>")

	out4 := renderTest(t, "mixin item(label, total)\n  span= label\n  span= total\n+item(x, a + b, y)", map[string]interface{}{
		"x": "left",
		"a": 1,
		"b": 4,
		"y": "right",
	})
	assertContains(t, out4, "<span>left</span>")
	assertContains(t, out4, "<span>5</span>")
}

// TestBarePositionalArgFallsBackToInterpreter confirms a stitched
// operator-expression mixin argument is not one of classifyExpr's whitelisted
// simple shapes, so it keeps rendering through the interpreter fallback
// (evaluateExpr) rather than a compiled closure — the closure-compilation
// work only ever overlays a conservative subset of shapes on top of the one
// interpreter, and a stitched operator expression must not be silently
// treated as if it were.
func TestBarePositionalArgFallsBackToInterpreter(t *testing.T) {
	names := lexAttrNames(t, "+item(a + b)")
	assertStringSlicesEqual(t, names, []string{"a + b"})

	if compiled := classifyExpr(names[0]); compiled != nil {
		t.Fatalf("classifyExpr(%q) = non-nil, want nil so renderMixinCall falls back to evaluateExpr for a stitched operator expression", names[0])
	}

	out := renderTest(t, "mixin item(v)\n  p= v\n+item(a + b)", map[string]interface{}{
		"a": 10,
		"b": 32,
	})
	assertContains(t, out, "<p>42</p>")
}

// TestBooleanAttributesUnaffectedByOperatorStitching guards the boundary
// case the fix must not disturb: two bare tokens separated only by
// whitespace, with no operator between them, are two separate boolean
// attributes/positional arguments, not one stitched expression.
func TestBooleanAttributesUnaffectedByOperatorStitching(t *testing.T) {
	assertStringSlicesEqual(t, lexAttrNames(t, "div(a b)"), []string{"a", "b"})
	assertStringSlicesEqual(t, lexAttrNames(t, "input(checked disabled)"), []string{"checked", "disabled"})

	out := renderTest(t, "input(checked disabled)", nil)
	assertContains(t, out, "checked")
	assertContains(t, out, "disabled")
}

// TestDataAndAriaAndShorthandAttrsUnaffected guards data-*/aria-* attribute
// names and the Alpine/x-bind `:attr` / `@event` shorthands: none of these
// should be mistaken for an operator-stitched expression, since a hyphen or
// colon that is part of an attribute name is consumed by scanAttrName before
// any operator-continuation check runs.
func TestDataAndAriaAndShorthandAttrsUnaffected(t *testing.T) {
	assertStringSlicesEqual(t, lexAttrNames(t, `div(data-foo="x")`), []string{"data-foo"})
	assertStringSlicesEqual(t, lexAttrNames(t, `div(aria-label="y")`), []string{"aria-label"})
	assertStringSlicesEqual(t, lexAttrNames(t, `div(:class="z")`), []string{":class"})
	assertStringSlicesEqual(t, lexAttrNames(t, `button(@click="f")`), []string{"@click"})
	assertStringSlicesEqual(t, lexAttrNames(t, `input(:disabled)`), []string{":disabled"})

	out := renderTest(t, `div(data-foo="x" aria-label="y")`, nil)
	assertContains(t, out, `data-foo="x"`)
	assertContains(t, out, `aria-label="y"`)
}

// TestNamedAttributeOperatorValuesUnaffected re-runs the existing named
// attribute-value ternary case (already supported via scanAttrValueFull)
// to confirm the shared helper factored out for the bare-argument fix did
// not change that behavior.
func TestNamedAttributeOperatorValuesUnaffected(t *testing.T) {
	src := `.card.notif-item(class= !notification.IsRead ? "bg-body-secondary" : "")`
	names := lexAttrNames(t, src)
	assertStringSlicesEqual(t, names, []string{"class"})

	out := renderTest(t, src, map[string]interface{}{
		"notification": map[string]interface{}{"IsRead": false},
	})
	assertContains(t, out, "bg-body-secondary")
}

// TestCommonDotPathMixinArgsUnaffected guards the everyday mixin-call
// pattern the closure-compilation work depends on: comma-separated plain
// dot-path positional arguments, with no operators at all, must still lex
// to exactly one argument per slot.
func TestCommonDotPathMixinArgsUnaffected(t *testing.T) {
	assertStringSlicesEqual(t, lexAttrNames(t, "+item(product.name, product.price)"), []string{"product.name", "product.price"})

	out := renderTest(t, "mixin item(name, price)\n  span= name\n  span= price\nul\n  each product in products\n    li\n      +item(product.name, product.price)", map[string]interface{}{
		"products": []any{
			map[string]any{"name": "Widget", "price": "9.99"},
		},
	})
	assertContains(t, out, "<span>Widget</span>")
	assertContains(t, out, "<span>9.99</span>")
}
