package gopug

import (
	"strings"
	"testing"
)

// renderTest is a helper that compiles and renders a pug string with optional data.
func renderTest(t *testing.T, src string, data map[string]interface{}) string {
	t.Helper()
	html, err := Render(src, data, nil)
	if err != nil {
		t.Fatalf("Render error: %v\nsrc:\n%s", err, src)
	}
	return html
}

// assertContains checks that the output contains the expected substring.
func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("expected output to contain %q\ngot: %q", want, got)
	}
}

// assertEqual checks exact equality.
func assertEqual(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("output mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Doctype
// ---------------------------------------------------------------------------

func TestDoctypeHtml(t *testing.T) {
	out := renderTest(t, "doctype html", nil)
	assertEqual(t, out, "<!DOCTYPE html>")
}

func TestDoctypeXml(t *testing.T) {
	out := renderTest(t, "doctype xml", nil)
	assertContains(t, out, "<?xml")
}

// ---------------------------------------------------------------------------
// Basic tags
// ---------------------------------------------------------------------------

func TestSimpleTag(t *testing.T) {
	out := renderTest(t, "p", nil)
	assertEqual(t, out, "<p></p>")
}

func TestTagWithInlineText(t *testing.T) {
	out := renderTest(t, "p Hello", nil)
	assertEqual(t, out, "<p>Hello</p>")
}

func TestNestedTags(t *testing.T) {
	src := "div\n  p Hello"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<div>")
	assertContains(t, out, "<p>Hello</p>")
	assertContains(t, out, "</div>")
}

func TestSelfClosingVoidTag(t *testing.T) {
	out := renderTest(t, "br", nil)
	assertEqual(t, out, "<br>")
}

func TestImgVoidTag(t *testing.T) {
	out := renderTest(t, `img(src="logo.png")`, nil)
	assertContains(t, out, "<img")
	assertContains(t, out, `src="logo.png"`)
}

// ---------------------------------------------------------------------------
// Classes and IDs
// ---------------------------------------------------------------------------

func TestClassShorthand(t *testing.T) {
	out := renderTest(t, "p.intro Hello", nil)
	assertContains(t, out, `class="intro"`)
}

func TestIDShorthand(t *testing.T) {
	out := renderTest(t, "p#main Hello", nil)
	assertContains(t, out, `id="main"`)
}

func TestImplicitDivClass(t *testing.T) {
	out := renderTest(t, ".container", nil)
	assertContains(t, out, "<div")
	assertContains(t, out, `class="container"`)
}

func TestImplicitDivID(t *testing.T) {
	out := renderTest(t, "#header", nil)
	assertContains(t, out, "<div")
	assertContains(t, out, `id="header"`)
}

// ---------------------------------------------------------------------------
// Attributes
// ---------------------------------------------------------------------------

func TestSingleAttribute(t *testing.T) {
	out := renderTest(t, `a(href="/home") Home`, nil)
	assertContains(t, out, `href="/home"`)
	assertContains(t, out, "Home")
}

func TestMultipleAttributes(t *testing.T) {
	out := renderTest(t, `input(type="text" name="q")`, nil)
	assertContains(t, out, `type="text"`)
	assertContains(t, out, `name="q"`)
}

func TestBooleanAttribute(t *testing.T) {
	out := renderTest(t, `input(type="checkbox" checked)`, nil)
	assertContains(t, out, "checked")
}

func TestAttributeHTMLEscaping(t *testing.T) {
	out := renderTest(t, `p(title="<>&\"")`, nil)
	// Attribute value should be HTML-escaped
	assertContains(t, out, "&lt;")
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

func TestBufferedComment(t *testing.T) {
	out := renderTest(t, "// a comment", nil)
	assertContains(t, out, "<!--")
	assertContains(t, out, "a comment")
	assertContains(t, out, "-->")
}

func TestUnbufferedComment(t *testing.T) {
	out := renderTest(t, "//- silent comment\np Hello", nil)
	if strings.Contains(out, "silent") {
		t.Errorf("unbuffered comment should not appear in output, got: %q", out)
	}
	assertContains(t, out, "<p>Hello</p>")
}

// ---------------------------------------------------------------------------
// Pipe text
// ---------------------------------------------------------------------------

func TestPipeText(t *testing.T) {
	src := "p\n  | Hello world"
	out := renderTest(t, src, nil)
	assertContains(t, out, "Hello world")
}

// ---------------------------------------------------------------------------
// Code blocks
// ---------------------------------------------------------------------------

func TestBufferedCode(t *testing.T) {
	out := renderTest(t, `= "hello"`, nil)
	assertContains(t, out, "hello")
}

func TestBufferedCodeEscapes(t *testing.T) {
	out := renderTest(t, `= "<b>bold</b>"`, nil)
	assertContains(t, out, "&lt;b&gt;")
}

func TestUnescapedCode(t *testing.T) {
	out := renderTest(t, `!= "<b>bold</b>"`, nil)
	assertContains(t, out, "<b>bold</b>")
}

// ---------------------------------------------------------------------------
// HTML literal
// ---------------------------------------------------------------------------

func TestLiteralHTML(t *testing.T) {
	out := renderTest(t, "<div>literal</div>", nil)
	assertContains(t, out, "<div>literal</div>")
}

// ---------------------------------------------------------------------------
// Conditional rendering
// ---------------------------------------------------------------------------

func TestIfTrue(t *testing.T) {
	src := "if show\n  p Visible"
	out := renderTest(t, src, map[string]interface{}{"show": true})
	assertContains(t, out, "<p>Visible</p>")
}

func TestIfFalse(t *testing.T) {
	src := "if show\n  p Visible"
	out := renderTest(t, src, map[string]interface{}{"show": false})
	if strings.Contains(out, "Visible") {
		t.Errorf("expected Visible to be hidden, got: %q", out)
	}
}

func TestIfElse(t *testing.T) {
	src := "if show\n  p Yes\nelse\n  p No"
	outTrue := renderTest(t, src, map[string]interface{}{"show": true})
	assertContains(t, outTrue, "<p>Yes</p>")

	outFalse := renderTest(t, src, map[string]interface{}{"show": false})
	assertContains(t, outFalse, "<p>No</p>")
}

func TestUnless(t *testing.T) {
	src := "unless hide\n  p Shown"
	out := renderTest(t, src, map[string]interface{}{"hide": false})
	assertContains(t, out, "<p>Shown</p>")

	outHidden := renderTest(t, src, map[string]interface{}{"hide": true})
	if strings.Contains(outHidden, "Shown") {
		t.Errorf("expected content to be hidden, got: %q", outHidden)
	}
}

// ---------------------------------------------------------------------------
// Each loop
// ---------------------------------------------------------------------------

func TestEachOverSlice(t *testing.T) {
	src := "ul\n  each item in items\n    li= item"
	out := renderTest(t, src, map[string]interface{}{
		"items": []interface{}{"foo", "bar", "baz"},
	})
	assertContains(t, out, "<ul>")
	assertContains(t, out, "<li>foo</li>")
	assertContains(t, out, "<li>bar</li>")
	assertContains(t, out, "<li>baz</li>")
}

func TestEachEmptyElse(t *testing.T) {
	src := "each item in items\n  p= item\nelse\n  p No items"
	out := renderTest(t, src, map[string]interface{}{
		"items": []interface{}{},
	})
	assertContains(t, out, "No items")
}

// ---------------------------------------------------------------------------
// Variable lookup
// ---------------------------------------------------------------------------

func TestVariableLookup(t *testing.T) {
	src := "p= name"
	out := renderTest(t, src, map[string]interface{}{"name": "World"})
	assertContains(t, out, "World")
}

func TestDotNotationLookup(t *testing.T) {
	src := "p= user.name"
	out := renderTest(t, src, map[string]interface{}{
		"user": map[string]interface{}{"name": "Alice"},
	})
	assertContains(t, out, "Alice")
}

// ---------------------------------------------------------------------------
// Doctype variants
// ---------------------------------------------------------------------------

func TestDoctypeHtml5(t *testing.T) {
	out := renderTest(t, "doctype 5", nil)
	assertEqual(t, out, "<!DOCTYPE html>")
}

func TestDoctypeTransitional(t *testing.T) {
	out := renderTest(t, "doctype transitional", nil)
	assertContains(t, out, "Transitional")
}

func TestDoctypeStrict(t *testing.T) {
	out := renderTest(t, "doctype strict", nil)
	assertContains(t, out, "Strict")
}

func TestDoctype11(t *testing.T) {
	out := renderTest(t, "doctype 1.1", nil)
	assertContains(t, out, "XHTML 1.1")
}

// ---------------------------------------------------------------------------
// Block expansion (tag: child)
// ---------------------------------------------------------------------------

func TestBlockExpansion(t *testing.T) {
	out := renderTest(t, "a: img", nil)
	assertContains(t, out, "<a>")
	assertContains(t, out, "<img>")
	assertContains(t, out, "</a>")
}

func TestBlockExpansionWithText(t *testing.T) {
	out := renderTest(t, "ul: li Item", nil)
	assertContains(t, out, "<ul>")
	assertContains(t, out, "<li>Item</li>")
	assertContains(t, out, "</ul>")
}

// ---------------------------------------------------------------------------
// Explicit self-closing tag
// ---------------------------------------------------------------------------

func TestExplicitSelfClose(t *testing.T) {
	out := renderTest(t, "foo/", nil)
	assertContains(t, out, "<foo")
	// Should not have a closing tag
	if strings.Contains(out, "</foo>") {
		t.Errorf("self-closed tag should not have closing tag, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Deeply nested tags
// ---------------------------------------------------------------------------

func TestDeeplyNested(t *testing.T) {
	src := "div\n  section\n    article\n      p Deep"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<div>")
	assertContains(t, out, "<section>")
	assertContains(t, out, "<article>")
	assertContains(t, out, "<p>Deep</p>")
	assertContains(t, out, "</article>")
	assertContains(t, out, "</section>")
	assertContains(t, out, "</div>")
}

// ---------------------------------------------------------------------------
// Multiple classes and combined class+ID
// ---------------------------------------------------------------------------

func TestMultipleClasses(t *testing.T) {
	out := renderTest(t, "p.foo.bar.baz Hello", nil)
	assertContains(t, out, "foo")
	assertContains(t, out, "bar")
	assertContains(t, out, "baz")
}

func TestClassAndIDTogether(t *testing.T) {
	out := renderTest(t, "p.intro#main Hello", nil)
	assertContains(t, out, `class="intro"`)
	assertContains(t, out, `id="main"`)
}

// ---------------------------------------------------------------------------
// Attributes — comma separated and unescaped
// ---------------------------------------------------------------------------

func TestCommaSeperatedAttributes(t *testing.T) {
	out := renderTest(t, `a(href="/", class="btn") Click`, nil)
	assertContains(t, out, `href="/"`)
	assertContains(t, out, `class="btn"`)
}

func TestUnescapedAttribute(t *testing.T) {
	out := renderTest(t, `div(data!="<b>hi</b>")`, nil)
	assertContains(t, out, "<b>hi</b>")
}

// ---------------------------------------------------------------------------
// Void tag variants
// ---------------------------------------------------------------------------

func TestVoidTagHr(t *testing.T) {
	out := renderTest(t, "hr", nil)
	assertEqual(t, out, "<hr>")
}

func TestVoidTagInput(t *testing.T) {
	out := renderTest(t, `input(type="text")`, nil)
	assertContains(t, out, "<input")
	if strings.Contains(out, "</input>") {
		t.Errorf("void tag input should not have closing tag, got: %q", out)
	}
}

func TestVoidTagLink(t *testing.T) {
	out := renderTest(t, `link(rel="stylesheet" href="style.css")`, nil)
	assertContains(t, out, "<link")
	if strings.Contains(out, "</link>") {
		t.Errorf("void tag link should not have closing tag, got: %q", out)
	}
}

func TestVoidTagMeta(t *testing.T) {
	out := renderTest(t, `meta(charset="utf-8")`, nil)
	assertContains(t, out, "<meta")
	if strings.Contains(out, "</meta>") {
		t.Errorf("void tag meta should not have closing tag, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// HTML escaping in text content
// ---------------------------------------------------------------------------

func TestTextContentIsEscaped(t *testing.T) {
	out := renderTest(t, "p <b>bold</b>", nil)
	assertContains(t, out, "&lt;b&gt;")
	if strings.Contains(out, "<b>") {
		t.Errorf("text content should be HTML-escaped, got: %q", out)
	}
}

func TestPipeTextIsEscaped(t *testing.T) {
	src := "p\n  | <script>alert(1)</script>"
	out := renderTest(t, src, nil)
	assertContains(t, out, "&lt;script&gt;")
	if strings.Contains(out, "<script>") {
		t.Errorf("pipe text should be HTML-escaped, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Multiple consecutive pipe lines
// ---------------------------------------------------------------------------

func TestMultiplePipeLines(t *testing.T) {
	src := "p\n  | Hello\n  | World"
	out := renderTest(t, src, nil)
	assertContains(t, out, "Hello")
	assertContains(t, out, "World")
}

// ---------------------------------------------------------------------------
// Conditional — else if chaining
// ---------------------------------------------------------------------------

func TestElseIfChain(t *testing.T) {
	src := "if val == 1\n  p One\nelse if val == 2\n  p Two\nelse\n  p Other"
	outOne := renderTest(t, src, map[string]interface{}{"val": "1"})
	assertContains(t, outOne, "<p>One</p>")

	outTwo := renderTest(t, src, map[string]interface{}{"val": "2"})
	assertContains(t, outTwo, "<p>Two</p>")

	outOther := renderTest(t, src, map[string]interface{}{"val": "3"})
	assertContains(t, outOther, "<p>Other</p>")
}

// ---------------------------------------------------------------------------
// Each — for alias and key variable
// ---------------------------------------------------------------------------

func TestForAsAliasOfEach(t *testing.T) {
	src := "ul\n  for item in items\n    li= item"
	out := renderTest(t, src, map[string]interface{}{
		"items": []interface{}{"a", "b"},
	})
	assertContains(t, out, "<li>a</li>")
	assertContains(t, out, "<li>b</li>")
}

func TestEachWithKeyVariable(t *testing.T) {
	src := "ul\n  each val, idx in items\n    li= idx"
	out := renderTest(t, src, map[string]interface{}{
		"items": []interface{}{"x", "y", "z"},
	})
	assertContains(t, out, "<li>0</li>")
	assertContains(t, out, "<li>1</li>")
	assertContains(t, out, "<li>2</li>")
}

func TestEachOverStringSlice(t *testing.T) {
	src := "ul\n  each item in items\n    li= item"
	out := renderTest(t, src, map[string]interface{}{
		"items": []string{"go", "pug"},
	})
	assertContains(t, out, "<li>go</li>")
	assertContains(t, out, "<li>pug</li>")
}

// ---------------------------------------------------------------------------
// Phase 2 — Interpolation in text nodes
// ---------------------------------------------------------------------------

func TestInterpolationInTagText(t *testing.T) {
	out := renderTest(t, "p Hello #{name}!", map[string]interface{}{"name": "World"})
	assertContains(t, out, "Hello World!")
}

func TestInterpolationEscapesHTML(t *testing.T) {
	out := renderTest(t, "p #{val}", map[string]interface{}{"val": "<b>bold</b>"})
	assertContains(t, out, "&lt;b&gt;")
	if strings.Contains(out, "<b>") {
		t.Errorf("interpolation should HTML-escape by default, got: %q", out)
	}
}

func TestUnescapedInterpolation(t *testing.T) {
	out := renderTest(t, "p !{val}", map[string]interface{}{"val": "<b>bold</b>"})
	assertContains(t, out, "<b>bold</b>")
}

func TestInterpolationInPipeText(t *testing.T) {
	src := "p\n  | Hello #{name}!"
	out := renderTest(t, src, map[string]interface{}{"name": "Pug"})
	assertContains(t, out, "Hello Pug!")
}

func TestMultipleInterpolationsOnOneLine(t *testing.T) {
	out := renderTest(t, "p #{first} #{last}", map[string]interface{}{
		"first": "John",
		"last":  "Doe",
	})
	assertContains(t, out, "John")
	assertContains(t, out, "Doe")
}

func TestInterpolationWithDotNotation(t *testing.T) {
	out := renderTest(t, "p Hello #{user.name}!", map[string]interface{}{
		"user": map[string]interface{}{"name": "Alice"},
	})
	assertContains(t, out, "Hello Alice!")
}

func TestInterpolationStringLiteral(t *testing.T) {
	out := renderTest(t, `p #{"hello world"}`, nil)
	assertContains(t, out, "hello world")
}

func TestInterpolationMixedWithPlainText(t *testing.T) {
	out := renderTest(t, "p before #{val} after", map[string]interface{}{"val": "MID"})
	assertContains(t, out, "before MID after")
}

// ---------------------------------------------------------------------------
// Phase 2 — Ternary expressions
// ---------------------------------------------------------------------------

func TestTernaryTrue(t *testing.T) {
	out := renderTest(t, "p= flag ? \"yes\" : \"no\"", map[string]interface{}{"flag": "true"})
	assertContains(t, out, "yes")
}

func TestTernaryFalse(t *testing.T) {
	out := renderTest(t, "p= flag ? \"yes\" : \"no\"", map[string]interface{}{"flag": "false"})
	assertContains(t, out, "no")
}

func TestTernaryInInterpolation(t *testing.T) {
	out := renderTest(t, "p Result: #{ok ? \"pass\" : \"fail\"}", map[string]interface{}{"ok": "true"})
	assertContains(t, out, "Result: pass")
}

// ---------------------------------------------------------------------------
// Phase 2 — Array index access
// ---------------------------------------------------------------------------

func TestArrayIndexAccess(t *testing.T) {
	out := renderTest(t, "p= items[0]", map[string]interface{}{
		"items": []interface{}{"alpha", "beta", "gamma"},
	})
	assertContains(t, out, "alpha")
}

func TestArrayIndexAccessSecond(t *testing.T) {
	out := renderTest(t, "p= items[1]", map[string]interface{}{
		"items": []interface{}{"alpha", "beta", "gamma"},
	})
	assertContains(t, out, "beta")
}

func TestArrayIndexInInterpolation(t *testing.T) {
	out := renderTest(t, "p First: #{items[0]}", map[string]interface{}{
		"items": []interface{}{"one", "two"},
	})
	assertContains(t, out, "First: one")
}

// ---------------------------------------------------------------------------
// Phase 2 — Comparison and logical operators in expressions
// ---------------------------------------------------------------------------

func TestComparisonEqualTrue(t *testing.T) {
	out := renderTest(t, "if x == 42\n  p yes", map[string]interface{}{"x": "42"})
	assertContains(t, out, "yes")
}

func TestComparisonEqualFalse(t *testing.T) {
	out := renderTest(t, "if x == 42\n  p yes\nelse\n  p no", map[string]interface{}{"x": "99"})
	assertContains(t, out, "no")
}

func TestComparisonNotEqual(t *testing.T) {
	out := renderTest(t, "if x != 0\n  p nonzero", map[string]interface{}{"x": "5"})
	assertContains(t, out, "nonzero")
}

func TestComparisonLessThan(t *testing.T) {
	out := renderTest(t, "if x < 10\n  p small", map[string]interface{}{"x": "3"})
	assertContains(t, out, "small")
}

func TestComparisonGreaterThan(t *testing.T) {
	out := renderTest(t, "if x > 10\n  p big", map[string]interface{}{"x": "99"})
	assertContains(t, out, "big")
}

func TestLogicalAnd(t *testing.T) {
	out := renderTest(t, "if a && b\n  p both", map[string]interface{}{"a": "true", "b": "true"})
	assertContains(t, out, "both")
}

func TestLogicalAndFalse(t *testing.T) {
	out := renderTest(t, "if a && b\n  p both\nelse\n  p nope", map[string]interface{}{"a": "true", "b": "false"})
	assertContains(t, out, "nope")
}

func TestLogicalOr(t *testing.T) {
	out := renderTest(t, "if a || b\n  p either", map[string]interface{}{"a": "false", "b": "true"})
	assertContains(t, out, "either")
}

func TestLogicalNot(t *testing.T) {
	out := renderTest(t, "if !hidden\n  p visible", map[string]interface{}{"hidden": "false"})
	assertContains(t, out, "visible")
}

// ---------------------------------------------------------------------------
// Phase 2 — String concatenation in expressions
// ---------------------------------------------------------------------------

func TestStringConcatInBufferedCode(t *testing.T) {
	out := renderTest(t, `p= "Hello " + name`, map[string]interface{}{"name": "World"})
	assertContains(t, out, "Hello World")
}

func TestStringConcatInInterpolation(t *testing.T) {
	out := renderTest(t, `p #{"Hi " + name}`, map[string]interface{}{"name": "Pug"})
	assertContains(t, out, "Hi Pug")
}

// ---------------------------------------------------------------------------
// Phase 3 — case / when / default
// ---------------------------------------------------------------------------

func TestCaseBasic(t *testing.T) {
	src := "case val\n  when 1\n    p One\n  when 2\n    p Two\n  default\n    p Other"
	out := renderTest(t, src, map[string]interface{}{"val": "1"})
	assertContains(t, out, "<p>One</p>")
	if strings.Contains(out, "Two") || strings.Contains(out, "Other") {
		t.Errorf("only matching when should render, got: %q", out)
	}
}

func TestCaseDefault(t *testing.T) {
	src := "case val\n  when 1\n    p One\n  when 2\n    p Two\n  default\n    p Other"
	out := renderTest(t, src, map[string]interface{}{"val": "99"})
	assertContains(t, out, "<p>Other</p>")
	if strings.Contains(out, "One") || strings.Contains(out, "Two") {
		t.Errorf("only default should render, got: %q", out)
	}
}

func TestCaseSecondWhen(t *testing.T) {
	src := "case val\n  when 1\n    p One\n  when 2\n    p Two\n  default\n    p Other"
	out := renderTest(t, src, map[string]interface{}{"val": "2"})
	assertContains(t, out, "<p>Two</p>")
	if strings.Contains(out, "One") || strings.Contains(out, "Other") {
		t.Errorf("only second when should render, got: %q", out)
	}
}

func TestCaseFallThrough(t *testing.T) {
	// Empty when falls through to next non-empty when
	src := "case val\n  when 1\n  when 2\n    p OneOrTwo\n  default\n    p Other"
	outOne := renderTest(t, src, map[string]interface{}{"val": "1"})
	assertContains(t, outOne, "<p>OneOrTwo</p>")

	outTwo := renderTest(t, src, map[string]interface{}{"val": "2"})
	assertContains(t, outTwo, "<p>OneOrTwo</p>")
}

func TestCaseFallThroughToDefault(t *testing.T) {
	// Empty when falls all the way through to default
	src := "case val\n  when 1\n  default\n    p Caught"
	out := renderTest(t, src, map[string]interface{}{"val": "1"})
	assertContains(t, out, "<p>Caught</p>")
}

func TestCaseNoMatch(t *testing.T) {
	// No default — nothing rendered
	src := "case val\n  when 1\n    p One"
	out := renderTest(t, src, map[string]interface{}{"val": "99"})
	if strings.Contains(out, "One") {
		t.Errorf("no when matched and no default, should be empty, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Phase 3 — while loop
// ---------------------------------------------------------------------------

func TestWhileLoop(t *testing.T) {
	src := "- i = 0\nul\n  while i < 3\n    li= i\n    - i++"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<li>0</li>")
	assertContains(t, out, "<li>1</li>")
	assertContains(t, out, "<li>2</li>")
	if strings.Contains(out, "<li>3</li>") {
		t.Errorf("while loop should stop at i==3, got: %q", out)
	}
}

func TestWhileLoopDecrement(t *testing.T) {
	src := "- n = 3\nul\n  while n > 0\n    li= n\n    - n--"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<li>3</li>")
	assertContains(t, out, "<li>2</li>")
	assertContains(t, out, "<li>1</li>")
}

// ---------------------------------------------------------------------------
// Phase 3 — variable assignment in unbuffered code
// ---------------------------------------------------------------------------

func TestVariableAssignment(t *testing.T) {
	src := "- greeting = \"Hello World\"\np= greeting"
	out := renderTest(t, src, nil)
	assertContains(t, out, "Hello World")
}

func TestVariableReassignment(t *testing.T) {
	src := "- x = \"first\"\n- x = \"second\"\np= x"
	out := renderTest(t, src, nil)
	assertContains(t, out, "second")
	if strings.Contains(out, "first") {
		t.Errorf("x should be reassigned to 'second', got: %q", out)
	}
}

func TestVariableIncrement(t *testing.T) {
	src := "- count = 5\n- count++\np= count"
	out := renderTest(t, src, nil)
	assertContains(t, out, "6")
}

func TestVariableDecrement(t *testing.T) {
	src := "- count = 10\n- count--\np= count"
	out := renderTest(t, src, nil)
	assertContains(t, out, "9")
}

// ---------------------------------------------------------------------------
// Phase 3 — each over map
// ---------------------------------------------------------------------------

func TestEachOverMap(t *testing.T) {
	src := "ul\n  each val, key in data\n    li #{key}: #{val}"
	out := renderTest(t, src, map[string]interface{}{
		"data": map[string]interface{}{"color": "red"},
	})
	assertContains(t, out, "color")
	assertContains(t, out, "red")
}

func TestEachOverMapValuesOnly(t *testing.T) {
	src := "ul\n  each val in data\n    li= val"
	out := renderTest(t, src, map[string]interface{}{
		"data": map[string]interface{}{"a": "apple"},
	})
	assertContains(t, out, "apple")
}

// ---------------------------------------------------------------------------
// Phase 3 — deep if / else if / else chains
// ---------------------------------------------------------------------------

func TestDeepElseIfChain(t *testing.T) {
	src := "if x == 1\n  p one\nelse if x == 2\n  p two\nelse if x == 3\n  p three\nelse\n  p other"

	out1 := renderTest(t, src, map[string]interface{}{"x": "1"})
	assertContains(t, out1, "<p>one</p>")

	out2 := renderTest(t, src, map[string]interface{}{"x": "2"})
	assertContains(t, out2, "<p>two</p>")

	out3 := renderTest(t, src, map[string]interface{}{"x": "3"})
	assertContains(t, out3, "<p>three</p>")

	out4 := renderTest(t, src, map[string]interface{}{"x": "9"})
	assertContains(t, out4, "<p>other</p>")
}

func TestNestedIf(t *testing.T) {
	src := "if outer\n  if inner\n    p Both\n  else\n    p OuterOnly\nelse\n  p Neither"

	outBoth := renderTest(t, src, map[string]interface{}{"outer": "true", "inner": "true"})
	assertContains(t, outBoth, "<p>Both</p>")

	outOuter := renderTest(t, src, map[string]interface{}{"outer": "true", "inner": "false"})
	assertContains(t, outOuter, "<p>OuterOnly</p>")

	outNeither := renderTest(t, src, map[string]interface{}{"outer": "false", "inner": "true"})
	assertContains(t, outNeither, "<p>Neither</p>")
}

// ---------------------------------------------------------------------------
// Phase 3 — unless with else
// ---------------------------------------------------------------------------

func TestUnlessWithElse(t *testing.T) {
	src := "unless hidden\n  p Shown\nelse\n  p Hidden"

	outShown := renderTest(t, src, map[string]interface{}{"hidden": "false"})
	assertContains(t, outShown, "<p>Shown</p>")

	outHidden := renderTest(t, src, map[string]interface{}{"hidden": "true"})
	assertContains(t, outHidden, "<p>Hidden</p>")
}

// ---------------------------------------------------------------------------
// Phase 3 — each with struct slice and dot-notation in body
// ---------------------------------------------------------------------------

func TestEachStructSlice(t *testing.T) {
	type Item struct {
		Name  string
		Price int
	}
	src := "ul\n  each item in items\n    li= item.Name"
	out := renderTest(t, src, map[string]interface{}{
		"items": []interface{}{
			Item{Name: "Apple", Price: 1},
			Item{Name: "Banana", Price: 2},
		},
	})
	assertContains(t, out, "<li>Apple</li>")
	assertContains(t, out, "<li>Banana</li>")
}

// ---------------------------------------------------------------------------
// Struct field lookup via reflect
// ---------------------------------------------------------------------------

func TestStructFieldLookup(t *testing.T) {
	type User struct {
		Name string
		Age  int
	}
	src := "p= user.Name"
	out := renderTest(t, src, map[string]interface{}{
		"user": User{Name: "Bob", Age: 30},
	})
	assertContains(t, out, "Bob")
}

// ---------------------------------------------------------------------------
// Full page test
// ---------------------------------------------------------------------------

func TestFullPage(t *testing.T) {
	src := `doctype html
html
  head
    title My Page
  body
    h1 Hello
    p Welcome`

	out := renderTest(t, src, nil)
	assertContains(t, out, "<!DOCTYPE html>")
	assertContains(t, out, "<html>")
	assertContains(t, out, "<head>")
	assertContains(t, out, "<title>My Page</title>")
	assertContains(t, out, "<body>")
	assertContains(t, out, "<h1>Hello</h1>")
	assertContains(t, out, "<p>Welcome</p>")
	assertContains(t, out, "</html>")
}
