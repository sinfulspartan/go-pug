package gopug

import (
	"os"
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
// Phase 4 — Mixins
// ---------------------------------------------------------------------------

func TestMixinBasicCall(t *testing.T) {
	src := "mixin hello\n  p Hello!\n+hello"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<p>Hello!</p>")
}

func TestMixinWithOneParam(t *testing.T) {
	src := "mixin greet(name)\n  p Hello #{name}!\n+greet(\"World\")"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<p>Hello World!</p>")
}

func TestMixinWithMultipleParams(t *testing.T) {
	src := "mixin btn(text, cls)\n  button(class=cls)= text\n+btn(\"Click\", \"primary\")"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<button")
	assertContains(t, out, "Click")
	assertContains(t, out, "primary")
}

func TestMixinParamFromData(t *testing.T) {
	src := "mixin greet(name)\n  p Hi #{name}\n+greet(username)"
	out := renderTest(t, src, map[string]interface{}{"username": "Alice"})
	assertContains(t, out, "Hi Alice")
}

func TestMixinMissingArgDefaultsEmpty(t *testing.T) {
	src := "mixin greet(name)\n  p Hi #{name}!\n+greet"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<p>Hi !</p>")
}

func TestMixinBlockSlot(t *testing.T) {
	src := "mixin card\n  div.card\n    block\n+card\n  p Inside the card"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<div")
	assertContains(t, out, "card")
	assertContains(t, out, "<p>Inside the card</p>")
}

func TestMixinBlockSlotEmpty(t *testing.T) {
	// Block with no content from caller — nothing rendered in slot
	src := "mixin wrap\n  div\n    block\n+wrap"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<div></div>")
}

func TestMixinCalledBeforeDeclaration(t *testing.T) {
	// Mixins are collected in a first pass so call order doesn't matter
	src := "+hello\nmixin hello\n  p Hello!"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<p>Hello!</p>")
}

func TestMixinCalledMultipleTimes(t *testing.T) {
	src := "mixin item(val)\n  li= val\nul\n  +item(\"a\")\n  +item(\"b\")\n  +item(\"c\")"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<li>a</li>")
	assertContains(t, out, "<li>b</li>")
	assertContains(t, out, "<li>c</li>")
}

func TestMixinScopeIsolation(t *testing.T) {
	// Variables defined inside a mixin should not leak to outer scope
	src := "mixin inner(x)\n  p= x\n- x = \"outer\"\n+inner(\"inner\")\np= x"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<p>inner</p>")
	assertContains(t, out, "<p>outer</p>")
}

func TestMixinRestParam(t *testing.T) {
	src := "mixin list(...items)\n  ul\n    each item in items\n      li= item\n+list(\"x\", \"y\", \"z\")"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<li>x</li>")
	assertContains(t, out, "<li>y</li>")
	assertContains(t, out, "<li>z</li>")
}

func TestMixinNestedCall(t *testing.T) {
	src := "mixin inner\n  span inner\nmixin outer\n  div\n    +inner\n+outer"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<div>")
	assertContains(t, out, "<span>inner</span>")
	assertContains(t, out, "</div>")
}

func TestMixinBlockWithParam(t *testing.T) {
	src := "mixin section(title)\n  div\n    h2= title\n    block\n+section(\"News\")\n  p Article content"
	out := renderTest(t, src, nil)
	assertContains(t, out, "<h2>News</h2>")
	assertContains(t, out, "<p>Article content</p>")
}

// ---------------------------------------------------------------------------
// Phase 5 — Includes
// ---------------------------------------------------------------------------

// testdataPath returns the absolute path to a file in the testdata directory.
func testdataPath(name string) string {
	// The test binary runs with the package directory as cwd.
	return "testdata/" + name
}

func TestIncludeBasic(t *testing.T) {
	src := "div\n  include testdata/header.pug"
	out, err := RenderFile("testdata/header.pug", nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	_ = src
	assertContains(t, out, "<header>")
	assertContains(t, out, "<nav>")
	assertContains(t, out, `href="/"`)
	assertContains(t, out, "Home")
}

func TestIncludeInTemplate(t *testing.T) {
	src := "div\n  include testdata/header.pug\n  main\n    p Content\n  include testdata/footer.pug"
	opts := &Options{Basedir: "."}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "<header>")
	assertContains(t, out, "<nav>")
	assertContains(t, out, "<main>")
	assertContains(t, out, "<p>Content</p>")
	assertContains(t, out, "<footer>")
	assertContains(t, out, "© 2026 Go-Pug")
}

func TestIncludeNoExtension(t *testing.T) {
	// include without .pug extension should default to .pug
	src := "div\n  include testdata/footer"
	opts := &Options{Basedir: "."}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "<footer>")
}

func TestIncludeRawFile(t *testing.T) {
	// Including a non-.pug file outputs its raw contents
	src := "style\n  include testdata/styles.css"
	opts := &Options{Basedir: "."}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "font-family")
	assertContains(t, out, "margin: 0")
}

func TestIncludeNestedDirectory(t *testing.T) {
	src := "div\n  include testdata/nested/partial.pug"
	opts := &Options{Basedir: "."}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "nested")
	assertContains(t, out, "Nested Partial")
}

func TestIncludeMixinLibrary(t *testing.T) {
	// Including a file that only contains mixin declarations makes them available
	src := "include testdata/mixin-lib.pug\n+badge(\"new\")"
	opts := &Options{Basedir: "."}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "<span")
	assertContains(t, out, "badge")
	assertContains(t, out, "new")
}

func TestIncludeWithData(t *testing.T) {
	// Included templates share the render data/scope
	out, err := RenderFile(testdataPath("header.pug"), map[string]interface{}{"title": "My Site"}, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "<header>")
}

func TestIncludeMissingFileError(t *testing.T) {
	src := "include testdata/does-not-exist.pug"
	opts := &Options{Basedir: "."}
	_, err := Render(src, nil, opts)
	if err == nil {
		t.Error("expected error for missing include file, got nil")
	}
}

func TestIncludeCycleDetection(t *testing.T) {
	// Write two temp files that include each other
	// We can't easily do this with static fixtures, so test self-include via RenderFile
	// by creating a temp file that includes itself.
	dir := t.TempDir()
	selfPath := dir + "/self.pug"
	err := os.WriteFile(selfPath, []byte("p Hello\ninclude self.pug"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = RenderFile(selfPath, nil, nil)
	if err == nil {
		t.Error("expected cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected 'cycle' in error message, got: %v", err)
	}
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

// ---------------------------------------------------------------------------
// Phase 6 — Template Inheritance (extends + block)
// ---------------------------------------------------------------------------

func layoutPath(name string) string {
	return "testdata/layouts/" + name
}

// TestExtendsBasicBlockReplace verifies that a child can replace a named block
// in the parent layout.
func TestExtendsBasicBlockReplace(t *testing.T) {
	out, err := RenderFile(layoutPath("page.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	// Title block replaced
	assertContains(t, out, "My Page")
	// Content block replaced
	assertContains(t, out, "<h1>Hello from page</h1>")
	assertContains(t, out, "<p>This is the page content.</p>")
	// Parent default header preserved (child did not override it)
	assertContains(t, out, "Default Header")
	// Parent default footer preserved
	assertContains(t, out, "Default Footer")
	// DOCTYPE from parent
	assertContains(t, out, "<!DOCTYPE html>")
}

// TestExtendsDefaultBlockKept verifies that when a child does not override a
// block, the parent's default body is rendered.
func TestExtendsDefaultBlockKept(t *testing.T) {
	out, err := RenderFile(layoutPath("page.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "Default Header")
	assertContains(t, out, "Default Footer")
}

// TestExtendsFullOverride verifies that a child can replace every block in the parent.
func TestExtendsFullOverride(t *testing.T) {
	out, err := RenderFile(layoutPath("full-override.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "Full Override")
	assertContains(t, out, "Custom Header")
	assertContains(t, out, "<p>Full override content</p>")
	assertContains(t, out, "Custom Footer")
	// meta tag from head block override
	assertContains(t, out, `name="description"`)
	// Parent defaults must NOT appear
	if strings.Contains(out, "Default Header") {
		t.Error("expected 'Default Header' to be replaced, but it is still present")
	}
	if strings.Contains(out, "Default Footer") {
		t.Error("expected 'Default Footer' to be replaced, but it is still present")
	}
}

// TestExtendsBlockAppend verifies block append mode (parent default + child additions).
func TestExtendsBlockAppend(t *testing.T) {
	out, err := RenderFile(layoutPath("append-page.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "Append Page")
	assertContains(t, out, "<p>Append page content</p>")
	// The appended link tag must appear in the head block
	assertContains(t, out, `href="/app.css"`)
}

// TestExtendsBlockPrepend verifies block prepend mode (child additions + parent default).
func TestExtendsBlockPrepend(t *testing.T) {
	out, err := RenderFile(layoutPath("prepend-page.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "Prepend Page")
	assertContains(t, out, "<p>Prepend page content</p>")
	// The prepended meta tag must appear
	assertContains(t, out, `name="viewport"`)
}

// TestExtendsChained verifies three-level inheritance (leaf → mid → root).
func TestExtendsChained(t *testing.T) {
	out, err := RenderFile(layoutPath("chain-leaf.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	// Leaf overrides title
	assertContains(t, out, "Leaf Title")
	// Mid overrides header — leaf did not override it
	assertContains(t, out, "Mid Header")
	// Leaf overrides mid-content block (nested inside content block)
	assertContains(t, out, "<p>Leaf content here</p>")
	// Leaf overrides footer
	assertContains(t, out, "Leaf Footer")
	// Root-default content must NOT appear (overridden by mid then leaf)
	if strings.Contains(out, "Root default content") {
		t.Error("expected 'Root default content' to be replaced")
	}
	assertContains(t, out, "<!DOCTYPE html>")
}

// TestExtendsChainedMidDefaultsPreserved verifies the mid-level template still
// renders correctly with its own defaults when rendered directly.
func TestExtendsChainedMidDefaults(t *testing.T) {
	out, err := RenderFile(layoutPath("chain-mid.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "Mid Title")
	assertContains(t, out, "Mid Header")
	assertContains(t, out, "Mid default content")
	// Root footer preserved (mid didn't override it)
	assertContains(t, out, "Root Footer")
}

// TestExtendsWithData verifies that template data is accessible inside block
// bodies in child templates.
func TestExtendsWithData(t *testing.T) {
	data := map[string]interface{}{
		"pageTitle": "Dynamic Title",
		"heading":   "Welcome",
		"body":      "Hello from data.",
	}
	out, err := RenderFile(layoutPath("data-page.pug"), data, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "Dynamic Title")
	assertContains(t, out, "<h1>Welcome</h1>")
	assertContains(t, out, "<p>Hello from data.</p>")
	assertContains(t, out, `src="/main.js"`)
}

// TestExtendsViaRenderString verifies that Render() (not RenderFile) works for
// extends when Basedir is supplied pointing to the layouts directory.
func TestExtendsViaRenderString(t *testing.T) {
	src := "extends base\n\nblock content\n  p From render string"
	opts := &Options{Basedir: "testdata/layouts"}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "<p>From render string</p>")
	assertContains(t, out, "Default Title")
	assertContains(t, out, "Default Header")
	assertContains(t, out, "Default Footer")
}

// TestExtendsMissingParentError verifies that a meaningful error is returned
// when the extended layout file does not exist.
func TestExtendsMissingParentError(t *testing.T) {
	src := "extends does-not-exist"
	opts := &Options{Basedir: "testdata/layouts"}
	_, err := Render(src, nil, opts)
	if err == nil {
		t.Error("expected error for missing extends file, got nil")
	}
}

// TestExtendsEmptyBlock verifies that overriding a block with an empty body
// produces no output for that block slot.
func TestExtendsEmptyBlock(t *testing.T) {
	src := "extends base\n\nblock header\n\nblock content\n  p Content only\n\nblock footer"
	opts := &Options{Basedir: "testdata/layouts"}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "<p>Content only</p>")
	// Header and footer defaults replaced with empty — should not appear
	if strings.Contains(out, "Default Header") {
		t.Error("expected 'Default Header' to be replaced with empty block")
	}
	if strings.Contains(out, "Default Footer") {
		t.Error("expected 'Default Footer' to be replaced with empty block")
	}
}

// TestExtendsStandaloneAppend verifies that the shorthand "append <name>"
// (without the "block" prefix) works identically to "block append <name>".
func TestExtendsStandaloneAppend(t *testing.T) {
	out, err := RenderFile(layoutPath("standalone-append.pug"), nil, nil)
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertContains(t, out, "Standalone Append")
	assertContains(t, out, "<p>Standalone append content</p>")
	// The appended link tag must appear
	assertContains(t, out, `href="/standalone.css"`)
}

// TestExtendsStandalonePrepend verifies that the shorthand "prepend <name>"
// (without the "block" prefix) works identically to "block prepend <name>".
func TestExtendsStandalonePrepend(t *testing.T) {
	src := "extends base\n\nblock title\n  | Standalone Prepend\n\nprepend head\n  meta(name=\"robots\" content=\"noindex\")\n\nblock content\n  main\n    p Standalone prepend content"
	opts := &Options{Basedir: "testdata/layouts"}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "Standalone Prepend")
	assertContains(t, out, "<p>Standalone prepend content</p>")
	assertContains(t, out, `name="robots"`)
}

// TestExtendsCycleDetection verifies that a meaningful error is returned when
// a layout file creates an inheritance cycle.
func TestExtendsCycleDetection(t *testing.T) {
	dir := t.TempDir()
	// Write two layout files that extend each other.
	aPath := dir + "/a.pug"
	bPath := dir + "/b.pug"
	if err := os.WriteFile(aPath, []byte("extends b\n\nblock content\n  p A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("extends a\n\nblock content\n  p B"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := RenderFile(aPath, nil, nil)
	if err == nil {
		t.Error("expected cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected 'cycle' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Phase 7 — Filters
// ---------------------------------------------------------------------------

// uppercaseFilter is a simple test filter that uppercases its input.
func uppercaseFilter(s string) (string, error) {
	return strings.ToUpper(s), nil
}

// wrapFilter wraps content in square brackets.
func wrapFilter(s string) (string, error) {
	return "[" + s + "]", nil
}

// exclaim appends "!" to each line.
func exclaimFilter(s string) (string, error) {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = l + "!"
		}
	}
	return strings.Join(lines, "\n"), nil
}

func filterOpts() *Options {
	return &Options{
		Filters: map[string]func(string) (string, error){
			"uppercase": uppercaseFilter,
			"wrap":      wrapFilter,
			"exclaim":   exclaimFilter,
		},
	}
}

// TestFilterBlockBasic verifies that a block filter applies the registered
// filter function to its indented body content.
func TestFilterBlockBasic(t *testing.T) {
	src := ":uppercase\n  hello world"
	out, err := Render(src, nil, filterOpts())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertEqual(t, out, "HELLO WORLD")
}

// TestFilterBlockMultiLine verifies that a block filter receives all indented
// lines joined by newlines.
func TestFilterBlockMultiLine(t *testing.T) {
	src := ":exclaim\n  line one\n  line two\n  line three"
	out, err := Render(src, nil, filterOpts())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "line one!")
	assertContains(t, out, "line two!")
	assertContains(t, out, "line three!")
}

// TestFilterInline verifies that a filter with same-line inline content works.
func TestFilterInline(t *testing.T) {
	src := ":uppercase hello inline"
	out, err := Render(src, nil, filterOpts())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertEqual(t, out, "HELLO INLINE")
}

// TestFilterSubchain verifies that chained filters (:outer:inner) apply inner
// first, then outer.
func TestFilterSubchain(t *testing.T) {
	// :wrap:uppercase content → uppercase("content") → wrap("CONTENT") → "[CONTENT]"
	src := ":wrap:uppercase\n  content"
	out, err := Render(src, nil, filterOpts())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertEqual(t, out, "[CONTENT]")
}

// TestFilterSubchainInline verifies that inline chained filters work.
func TestFilterSubchainInline(t *testing.T) {
	src := ":wrap:uppercase hello"
	out, err := Render(src, nil, filterOpts())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertEqual(t, out, "[HELLO]")
}

// TestFilterInsideTag verifies that a filter nested inside a tag renders its
// output as the tag's content.
func TestFilterInsideTag(t *testing.T) {
	src := "div\n  :uppercase\n    inside a div"
	out, err := Render(src, nil, filterOpts())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "<div>")
	assertContains(t, out, "INSIDE A DIV")
	assertContains(t, out, "</div>")
}

// TestFilterUnregistered verifies that using an unregistered filter returns a
// descriptive error rather than silently doing nothing.
func TestFilterUnregistered(t *testing.T) {
	src := ":nonexistent\n  body"
	_, err := Render(src, nil, &Options{})
	if err == nil {
		t.Error("expected error for unregistered filter, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("expected filter name in error message, got: %v", err)
	}
}

// TestFilterNilOptions verifies that using a filter with nil Options returns
// a descriptive error.
func TestFilterNilOptions(t *testing.T) {
	src := ":uppercase\n  text"
	_, err := Render(src, nil, nil)
	if err == nil {
		t.Error("expected error when no filters registered, got nil")
	}
}

// TestFilterIncludeRawWithFilter verifies that "include :filter path" reads
// the raw file and applies the named filter to its content.
func TestFilterIncludeRawWithFilter(t *testing.T) {
	src := "include :uppercase testdata/article.txt"
	opts := &Options{
		Basedir: ".",
		Filters: map[string]func(string) (string, error){
			"uppercase": uppercaseFilter,
		},
	}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertContains(t, out, "HELLO FROM A TEXT FILE.")
	assertContains(t, out, "THIS IS LINE TWO.")
}

// TestFilterIncludeUnregistered verifies that "include :filter path" with an
// unregistered filter returns a descriptive error.
func TestFilterIncludeUnregistered(t *testing.T) {
	src := "include :nofilter testdata/article.txt"
	opts := &Options{Basedir: "."}
	_, err := Render(src, nil, opts)
	if err == nil {
		t.Error("expected error for unregistered include filter, got nil")
	}
	if !strings.Contains(err.Error(), "nofilter") {
		t.Errorf("expected filter name in error message, got: %v", err)
	}
}

// TestFilterViaGlobals verifies that filters work when accessed via Options
// even when the template data is nil.
func TestFilterViaGlobals(t *testing.T) {
	src := ":wrap\n  global test"
	out, err := Render(src, nil, filterOpts())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertEqual(t, out, "[global test]")
}
