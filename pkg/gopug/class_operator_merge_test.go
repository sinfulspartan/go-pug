package gopug

import (
	"strings"
	"testing"
)

// A tag with a shorthand class (`.btn`) AND a class= value that is an
// operator, ternary, logical, or concatenation expression (has a top-level
// operator so it must be evaluated as one unit) must merge the two rather
// than dropping the shorthand and mangling the expression. The expected
// values below were confirmed against pug.js 3.0.4:
//
//	pug.render('button.btn(class="btn-" + style) Click', {style: 'primary'})
//	  => '<button class="btn btn-primary">Click</button>'

func TestClassShorthandPlusConcatMerge(t *testing.T) {
	src := `button.btn(class="btn-" + style) Click`
	out := renderTest(t, src, map[string]any{"style": "primary"})
	if !strings.Contains(out, `class="btn btn-primary"`) {
		t.Errorf("expected class=%q, got: %q", "btn btn-primary", out)
	}
}

// Proves the fix: before the fix this template rendered class="primary",
// silently dropping the shorthand token and losing the "btn-" literal
// prefix from the concatenation.
func TestClassShorthandPlusConcatMergeFaultInjection(t *testing.T) {
	src := `button.btn(class="btn-" + style) Click`
	out := renderTest(t, src, map[string]any{"style": "primary"})
	if strings.Contains(out, `class="primary"`) {
		t.Errorf("regressed to the old broken output class=%q, got: %q", "primary", out)
	}
}

// Confirmed unaffected rows from the same repro table (pug.js 3.0.4) —
// these already rendered correctly before the fix and must stay unchanged.

func TestClassShorthandPlusBareVarStillCorrect(t *testing.T) {
	src := `button.btn(class=style) Click`
	out := renderTest(t, src, map[string]any{"style": "primary"})
	if !strings.Contains(out, `class="btn primary"`) {
		t.Errorf("expected class=%q, got: %q", "btn primary", out)
	}
}

func TestClassStandaloneConcatStillCorrect(t *testing.T) {
	src := `button(class="btn-" + style) Click`
	out := renderTest(t, src, map[string]any{"style": "primary"})
	if !strings.Contains(out, `class="btn-primary"`) {
		t.Errorf("expected class=%q, got: %q", "btn-primary", out)
	}
}

func TestClassMultiShorthandPlusBareVarStillCorrect(t *testing.T) {
	src := `button.btn.lg(class=style) Click`
	out := renderTest(t, src, map[string]any{"style": "primary"})
	if !strings.Contains(out, `class="btn lg primary"`) {
		t.Errorf("expected class=%q, got: %q", "btn lg primary", out)
	}
}

// pug.js semantics validated via a live pug.js 3.0.4 probe (perf-compare's
// node_modules) and reproduced exactly here.

func TestClassShorthandPlusConcatExpressionResultKeepsInternalWhitespace(t *testing.T) {
	// pug.render('.btn(class="a " + s) Test', {s: 'b c'})
	//   => '<div class="btn a b c">Test</div>'
	// The evaluated expression's own whitespace is never re-split or
	// collapsed — it is joined as a single opaque entry, which happens to
	// read as multiple class names only because the expression result
	// itself already contains the separating spaces.
	src := `.btn(class="a " + s) Test`
	out := renderTest(t, src, map[string]any{"s": "b c"})
	if !strings.Contains(out, `class="btn a b c"`) {
		t.Errorf("expected class=%q, got: %q", "btn a b c", out)
	}
}

func TestClassShorthandPlusTernaryTrueBranch(t *testing.T) {
	// pug.render('.card(class=cond ? "x y" : "") Test', {cond: true})
	//   => '<div class="card x y">Test</div>'
	src := `.card(class=cond ? "x y" : "") Test`
	out := renderTest(t, src, map[string]any{"cond": true})
	if !strings.Contains(out, `class="card x y"`) {
		t.Errorf("expected class=%q, got: %q", "card x y", out)
	}
}

func TestClassShorthandPlusTernaryFalseBranch(t *testing.T) {
	// pug.render('.card(class=cond ? "x y" : "") Test', {cond: false})
	//   => '<div class="card">Test</div>'
	src := `.card(class=cond ? "x y" : "") Test`
	out := renderTest(t, src, map[string]any{"cond": false})
	if !strings.Contains(out, `class="card"`) {
		t.Errorf("expected class=%q, got: %q", "card", out)
	}
	if strings.Contains(out, `class="card "`) {
		t.Errorf("expected no trailing space in class list, got: %q", out)
	}
}

func TestClassShorthandPlusLogicalAndTrue(t *testing.T) {
	// pug.render('.x(class=(a && "on")) Test', {a: true})
	//   => '<div class="x on">Test</div>'
	src := `.x(class=a && "on") Test`
	out := renderTest(t, src, map[string]any{"a": true})
	if !strings.Contains(out, `class="x on"`) {
		t.Errorf("expected class=%q, got: %q", "x on", out)
	}
}

func TestClassShorthandPlusLogicalAndFalse(t *testing.T) {
	// pug.render('.x(class=(a && "on")) Test', {a: false})
	//   => '<div class="x">Test</div>'
	src := `.x(class=a && "on") Test`
	out := renderTest(t, src, map[string]any{"a": false})
	if !strings.Contains(out, `class="x"`) {
		t.Errorf("expected class=%q, got: %q", "x", out)
	}
}

func TestClassMultiShorthandPlusConcat(t *testing.T) {
	// pug.render('.a.b(class="c-" + s) Test', {s: 'hello'})
	//   => '<div class="a b c-hello">Test</div>'
	src := `.a.b(class="c-" + s) Test`
	out := renderTest(t, src, map[string]any{"s": "hello"})
	if !strings.Contains(out, `class="a b c-hello"`) {
		t.Errorf("expected class=%q, got: %q", "a b c-hello", out)
	}
}

func TestClassShorthandWithMissingVarStillDrops(t *testing.T) {
	// pug.render('.a(class=missing) Test', {}) => '<div class="a">Test</div>'
	// A bare, non-operator dynamic value must still drop cleanly — this is
	// the pre-existing (unrelated) code path and must not regress.
	src := `.a(class=missing) Test`
	out := renderTest(t, src, map[string]any{})
	if !strings.Contains(out, `class="a"`) {
		t.Errorf("expected class=%q, got: %q", "a", out)
	}
}

// Already-parenthesized ternary/operator class expressions with a shorthand
// prefix must keep working exactly as before (this shape was already
// correct: pug.render('.card(class=(isActive ? "active" : "")) Hello',
// {isActive: true}) => '<div class="card active">Hello</div>').

func TestClassShorthandPlusParenthesizedTernaryTrue(t *testing.T) {
	src := `.card(class=(isActive ? "active" : "")) Hello`
	out := renderTest(t, src, map[string]any{"isActive": true})
	if !strings.Contains(out, `class="card active"`) {
		t.Errorf("expected class=%q, got: %q", "card active", out)
	}
}

func TestClassShorthandPlusParenthesizedTernaryFalse(t *testing.T) {
	src := `.card(class=(isActive ? "active" : "")) Hello`
	out := renderTest(t, src, map[string]any{"isActive": false})
	if !strings.Contains(out, `class="card"`) {
		t.Errorf("expected class=%q, got: %q", "card", out)
	}
}
