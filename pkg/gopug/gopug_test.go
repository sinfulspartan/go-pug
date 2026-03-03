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
