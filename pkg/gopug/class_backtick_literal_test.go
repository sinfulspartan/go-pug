package gopug

import "testing"

// A bare (non-quoted) class= value that is a backtick template literal must
// be treated as a single atomic token when the class list is resolved, even
// when the literal's static text contains a space. Splitting the raw class
// value on whitespace before recognizing the backtick delimiters shatters the
// literal and truncates everything after the first space.
//
// Confirmed against pug.js 3.0.4: pug.render('div(class=`a b-${x}`)', {x: 1})
// => '<div class="a b-1"></div>'.

func TestClassBacktickLiteralWithInteriorSpace(t *testing.T) {
	out := renderTest(t, "div(class=`a b-${x}`)", map[string]any{"x": 1})
	assertEqual(t, out, `<div class="a b-1"></div>`)
}

func TestClassBacktickLiteralWithSpaceAroundInterpolation(t *testing.T) {
	out := renderTest(t, "div(class=`a ${x} c`)", map[string]any{"x": 1})
	assertEqual(t, out, `<div class="a 1 c"></div>`)
}

func TestClassBacktickLiteralNoSpaceUnchanged(t *testing.T) {
	out := renderTest(t, "div(class=`ab-${x}`)", map[string]any{"x": 1})
	assertEqual(t, out, `<div class="ab-1"></div>`)
}

func TestClassStaticQuotedWithSpaceUnchanged(t *testing.T) {
	out := renderTest(t, `div(class="a b")`, nil)
	assertEqual(t, out, `<div class="a b"></div>`)
}

func TestClassTernaryBacktickBranchUnchanged(t *testing.T) {
	out := renderTest(t, "div(class=x ? `p q` : `r`)", map[string]any{"x": true})
	assertEqual(t, out, `<div class="p q"></div>`)
}
