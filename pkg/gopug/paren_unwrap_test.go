package gopug

import "testing"

// evaluateExpr's paren-unwrap previously stripped only one level of a
// fully-parenthesized whole expression, so an over-parenthesized expression
// like `((flag))` fell through to a raw lookup on the literal string
// "(flag)" and rendered empty instead of the inner value. In reference Pug
// (plain JS expression semantics), redundant parens around a whole
// expression are transparent, so `((flag))`, `(((x)))`, and
// `((a ? b : c))` must evaluate exactly like their unwrapped form. The fix
// loops the existing balanced-depth "is the whole string wrapped in one
// paren pair" check until no more whole-expression parens remain.

func TestParenUnwrapDoubleWrappedIdentifier(t *testing.T) {
	out := renderTest(t, "p= ((flag))\n", map[string]interface{}{"flag": "yes"})
	assertEqual(t, out, "<p>yes</p>")
}

func TestParenUnwrapTripleWrappedIdentifier(t *testing.T) {
	out := renderTest(t, "p= (((flag)))\n", map[string]interface{}{"flag": "yes"})
	assertEqual(t, out, "<p>yes</p>")
}

func TestParenUnwrapDoubleWrappedTernary(t *testing.T) {
	out := renderTest(t, "p= ((a ? b : c))\n", map[string]interface{}{
		"a": true, "b": "B", "c": "C",
	})
	assertEqual(t, out, "<p>B</p>")

	out = renderTest(t, "p= ((a ? b : c))\n", map[string]interface{}{
		"a": false, "b": "B", "c": "C",
	})
	assertEqual(t, out, "<p>C</p>")
}

func TestParenUnwrapDoubleWrappedComparison(t *testing.T) {
	out := renderTest(t, "p= ((x > 0))\n", map[string]interface{}{"x": 5})
	assertEqual(t, out, "<p>true</p>")

	out = renderTest(t, "p= ((x > 0))\n", map[string]interface{}{"x": -1})
	assertEqual(t, out, "<p>false</p>")
}

func TestParenUnwrapDoubleWrappedLogicalAnd(t *testing.T) {
	out := renderTest(t, "p= ((a && b))\n", map[string]interface{}{"a": true, "b": "yes"})
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, "p= ((a && b))\n", map[string]interface{}{"a": false, "b": "yes"})
	assertEqual(t, out, "<p>false</p>")
}

func TestParenUnwrapSingleWrappedIdentifierUnaffected(t *testing.T) {
	out := renderTest(t, "p= (flag)\n", map[string]interface{}{"flag": "yes"})
	assertEqual(t, out, "<p>yes</p>")
}

func TestParenUnwrapSingleWrappedTernaryUnaffected(t *testing.T) {
	out := renderTest(t, "p= (a ? b : c)\n", map[string]interface{}{
		"a": true, "b": "B", "c": "C",
	})
	assertEqual(t, out, "<p>B</p>")
}

func TestParenUnwrapPartiallyWrappedLogicalAndUnaffected(t *testing.T) {
	out := renderTest(t, "p= (a) && (b)\n", map[string]interface{}{"a": true, "b": "yes"})
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, "p= (a) && (b)\n", map[string]interface{}{"a": false, "b": "yes"})
	assertEqual(t, out, "<p>false</p>")
}

func TestParenUnwrapWholeWrappedPartialInnerUnaffected(t *testing.T) {
	out := renderTest(t, "p= ((a) && (b))\n", map[string]interface{}{"a": true, "b": "yes"})
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, "p= ((a) && (b))\n", map[string]interface{}{"a": false, "b": "yes"})
	assertEqual(t, out, "<p>false</p>")
}

func TestParenUnwrapMixedGroupedOrAndUnaffected(t *testing.T) {
	out := renderTest(t, "p= (a || b) && c\n", map[string]interface{}{
		"a": false, "b": true, "c": "yes",
	})
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, "p= (a || b) && c\n", map[string]interface{}{
		"a": false, "b": false, "c": "yes",
	})
	assertEqual(t, out, "<p>false</p>")
}

func TestParenUnwrapPlainIdentifierUnaffected(t *testing.T) {
	out := renderTest(t, "p= flag\n", map[string]interface{}{"flag": "yes"})
	assertEqual(t, out, "<p>yes</p>")
}

func TestParenUnwrapDegenerateEmptyParens(t *testing.T) {
	out := renderTest(t, "p= ()\n", nil)
	assertEqual(t, out, "<p></p>")
}

func TestParenUnwrapDegenerateNestedEmptyParens(t *testing.T) {
	out := renderTest(t, "p= (())\n", nil)
	assertEqual(t, out, "<p></p>")
}
