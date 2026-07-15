package gopug

import "testing"

// pug.js's pug-code-gen keeps two independent pretty-print decisions apart:
// tag.isInline (a static, tag-NAME-only classification, driving the tag's
// own leading and closing newline) and tagCanInline (a recursive,
// CONTENT-based classification, driving whether a trailing newline — and
// therefore indented block layout — is needed before the closing tag). The
// tests below pin values measured directly from pug.js 3.0.4
// (perf-compare/node_modules/pug) against Options{Pretty: true}.

func prettyRender(t *testing.T, src string) string {
	t.Helper()
	out, err := Render(src, nil, &Options{Pretty: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

// TestPrettyBlockTagSingleTextChildLeadingNewline pins pug.js 3.0.4:
// pug.render("p Hello", {pretty:true}) === "\n<p>Hello</p>" — a block-named
// tag always starts on its own line, even though its single text child
// keeps the content itself on one line.
func TestPrettyBlockTagSingleTextChildLeadingNewline(t *testing.T) {
	assertEqual(t, prettyRender(t, "p Hello"), "\n<p>Hello</p>")
}

// TestPrettyInlineNamedTagNoLeadingNewline pins pug.js 3.0.4:
// pug.render("span Hello", {pretty:true}) === "<span>Hello</span>" — an
// inline-named tag never starts on its own line.
func TestPrettyInlineNamedTagNoLeadingNewline(t *testing.T) {
	assertEqual(t, prettyRender(t, "span Hello"), "<span>Hello</span>")
}

// TestPrettyNestedBlockChildrenEachOwnLine pins pug.js 3.0.4:
// pug.render("div\n  p Hello\n  p World", {pretty:true}) ===
// "\n<div>\n  <p>Hello</p>\n  <p>World</p>\n</div>" — each block-named child
// gets its own indented line, and the parent's own content is not inline
// (block-named children), so it also gets indented block layout.
func TestPrettyNestedBlockChildrenEachOwnLine(t *testing.T) {
	assertEqual(t, prettyRender(t, "div\n  p Hello\n  p World"),
		"\n<div>\n  <p>Hello</p>\n  <p>World</p>\n</div>")
}

// TestPrettyMixedTextAndInlineChildStaysOneLine pins pug.js 3.0.4:
// pug.render("p Text #[a foo]", {pretty:true}) ===
// "\n<p>Text <a>foo</a></p>" — the tag itself still gets a leading newline
// (p is not inline-named), but an inline-named embedded tag keeps the
// content itself on one line with no trailing newline before </p>.
func TestPrettyMixedTextAndInlineChildStaysOneLine(t *testing.T) {
	assertEqual(t, prettyRender(t, "p Text #[a foo]"), "\n<p>Text <a>foo</a></p>")
}

// TestPrettyBlockChildForcesBlockLayout pins pug.js 3.0.4:
// pug.render("p\n  div Inner", {pretty:true}) ===
// "\n<p>\n  <div>Inner</div>\n</p>" — a block-named (non-inline) child forces
// the parent into indented block layout with a trailing newline.
func TestPrettyBlockChildForcesBlockLayout(t *testing.T) {
	assertEqual(t, prettyRender(t, "p\n  div Inner"), "\n<p>\n  <div>Inner</div>\n</p>")
}

// TestPrettyInlineTagWrappingBlockContent pins pug.js 3.0.4:
// pug.render("a\n  div Inner", {pretty:true}) === "<a>\n  <div>Inner</div></a>"
// — an inline-named wrapping tag never gets its own leading or closing
// newline (isInline is purely name-based), even though its block-named
// child forces indented content and its own content can't inline.
func TestPrettyInlineTagWrappingBlockContent(t *testing.T) {
	assertEqual(t, prettyRender(t, "a\n  div Inner"), "<a>\n  <div>Inner</div></a>")
}

// TestPrettyInlineChildOfBlockCountsAsInlineByNameNotContent pins pug.js
// 3.0.4: pug.render("div\n  a\n    div Inner", {pretty:true}) ===
// "\n<div><a>\n    <div>Inner</div></a></div>" — the outer div's canInline
// check counts its <a> child as inline purely by NAME, not by recursively
// checking whether the <a>'s own content can inline (it can't — it wraps a
// block-named div) — so the outer div's own closing tag gets no trailing
// newline even though the inner div does.
func TestPrettyInlineChildOfBlockCountsAsInlineByNameNotContent(t *testing.T) {
	assertEqual(t, prettyRender(t, "div\n  a\n    div Inner"),
		"\n<div><a>\n    <div>Inner</div></a></div>")
}

// TestPrettyEmptyTagStillGetsLeadingNewline pins pug.js 3.0.4:
// pug.render("div", {pretty:true}) === "\n<div></div>" — an empty tag has no
// children to violate inline content (vacuously true), so it gets no
// trailing newline, but its own leading newline (name-based) still applies.
func TestPrettyEmptyTagStillGetsLeadingNewline(t *testing.T) {
	assertEqual(t, prettyRender(t, "div"), "\n<div></div>")
}

// TestPrettyPipedRunPreservesMultiLineBlockLayout is a regression pin for the
// piped multi-line text run behavior: pug.js 3.0.4:
// pug.render("p\n  | First\n  | Second", {pretty:true}) ===
// "\n<p>\n  First\n  Second\n</p>" — a sole child that spans multiple source
// lines still forces indented block layout even though it produces no
// nested tags of its own.
func TestPrettyPipedRunPreservesMultiLineBlockLayout(t *testing.T) {
	assertEqual(t, prettyRender(t, "p\n  | First\n  | Second"),
		"\n<p>\n  First\n  Second\n</p>")
}

// TestPrettyRealisticNestedDocument mirrors cmd/views/34-pretty-print.pug and
// pins pug.js 3.0.4's own full-document indentation for it.
func TestPrettyRealisticNestedDocument(t *testing.T) {
	src := "html\n  head\n    title Pretty Output\n  body\n    header\n      h1 Hello\n    main\n      p First paragraph.\n      p Second paragraph.\n    footer\n      p Footer text."
	want := "\n<html>\n  <head>\n    <title>Pretty Output</title>\n  </head>\n  <body>\n    <header>\n      <h1>Hello</h1>\n    </header>\n    <main>\n      <p>First paragraph.</p>\n      <p>Second paragraph.</p>\n    </main>\n    <footer>\n      <p>Footer text.</p>\n    </footer>\n  </body>\n</html>"
	assertEqual(t, prettyRender(t, src), want)
}

// TestPrettyListOfInlineWrappedLinks mirrors cmd/views/42-pretty-vs-compact.pug's
// nav list and pins pug.js 3.0.4's own indentation: each li is block-named
// (its own leading newline), but its sole child <a> is inline-named, so the
// li's own content stays on one line.
func TestPrettyListOfInlineWrappedLinks(t *testing.T) {
	src := "ul\n  li\n    a(href=\"/\") Home\n  li\n    a(href=\"/about\") About"
	want := "\n<ul>\n  <li><a href=\"/\">Home</a></li>\n  <li><a href=\"/about\">About</a></li>\n</ul>"
	assertEqual(t, prettyRender(t, src), want)
}

// TestPrettyDoctypeHTMLSkeleton pins pug.js 3.0.4's own line-for-line output
// for a doctype + html/head/body skeleton.
func TestPrettyDoctypeHTMLSkeleton(t *testing.T) {
	src := "doctype html\nhtml\n  body\n    p Hello"
	want := "<!DOCTYPE html>\n<html>\n  <body>\n    <p>Hello</p>\n  </body>\n</html>"
	assertEqual(t, prettyRender(t, src), want)
}

// TestPrettySameLineCodeShorthandStaysInline pins pug.js 3.0.4:
// pug.render("p= 1+1", {pretty:true}) === "\n<p>2</p>" — buffered code
// immediately following the tag on its own source line (tag-shorthand) is
// inline, unlike the same code written as its own indented child statement.
func TestPrettySameLineCodeShorthandStaysInline(t *testing.T) {
	assertEqual(t, prettyRender(t, "p= 1+1"), "\n<p>2</p>")
}

// TestPrettyOwnLineCodeStatementForcesBlockLayout pins pug.js 3.0.4:
// pug.render("p\n  = 1+1", {pretty:true}) === "\n<p>2\n</p>" — the same
// buffered code written as its own indented child (not tag-shorthand) is
// not inline and forces a trailing newline before the closing tag.
func TestPrettyOwnLineCodeStatementForcesBlockLayout(t *testing.T) {
	assertEqual(t, prettyRender(t, "p\n  = 1+1"), "\n<p>2\n</p>")
}

// TestPrettyBlockTextSingleLineStaysInline pins pug.js 3.0.4:
// pug.render("p.\n  Some text here", {pretty:true}) === "\n<p>Some text here</p>"
// — single-line dot-block text is treated exactly like a single-line text
// child: the tag gets its own leading newline, but the content itself has
// none.
func TestPrettyBlockTextSingleLineStaysInline(t *testing.T) {
	assertEqual(t, prettyRender(t, "p.\n  Some text here"), "\n<p>Some text here</p>")
}

// TestPrettyBlockTextMultiLineIndentsEveryLine pins pug.js 3.0.4:
// pug.render("p.\n  Line1\n  Line2", {pretty:true}) ===
// "\n<p>\n  Line1\n  Line2\n</p>" — multi-line dot-block text forces block
// layout, and every line (not just the first) gets its own indentation.
func TestPrettyBlockTextMultiLineIndentsEveryLine(t *testing.T) {
	assertEqual(t, prettyRender(t, "p.\n  Line1\n  Line2"), "\n<p>\n  Line1\n  Line2\n</p>")
}

// TestPrettyBlockTextThreeLinesIndentsEveryLine pins pug.js 3.0.4:
// pug.render("p.\n  Line1\n  Line2\n  Line3", {pretty:true}) ===
// "\n<p>\n  Line1\n  Line2\n  Line3\n</p>" — a regression guard that the
// per-line indentation applies to every embedded line, not just the second.
func TestPrettyBlockTextThreeLinesIndentsEveryLine(t *testing.T) {
	assertEqual(t, prettyRender(t, "p.\n  Line1\n  Line2\n  Line3"),
		"\n<p>\n  Line1\n  Line2\n  Line3\n</p>")
}

// TestCompactBlockTextMultiLineUnchanged pins that compact-mode output for
// multi-line dot-block text is untouched by the pretty-mode fix.
func TestCompactBlockTextMultiLineUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "p.\n  Line1\n  Line2", nil), "<p>Line1\nLine2</p>")
}

// TestPrettyCommentChildForcesBlockLayout pins pug.js 3.0.4:
// pug.render("p\n  //comment", {pretty:true}) === "\n<p>\n  <!--comment-->\n</p>"
// — a buffered comment child is never counted as inline content.
func TestPrettyCommentChildForcesBlockLayout(t *testing.T) {
	assertEqual(t, prettyRender(t, "p\n  //comment"), "\n<p>\n  <!--comment-->\n</p>")
}

// --- Fault injection: the OLD conflated single-predicate output must not occur. ---

// TestPrettyFaultInjectionBlockTagSingleTextChildIsNotMissingLeadingNewline
// proves the old conflated behavior (a tag with a single text-only child was
// treated as "inline" and never given a leading newline) is gone: it would
// have produced "<p>Hello</p>" without the leading "\n".
func TestPrettyFaultInjectionBlockTagSingleTextChildIsNotMissingLeadingNewline(t *testing.T) {
	got := prettyRender(t, "p Hello")
	if got == "<p>Hello</p>" {
		t.Fatalf("got the old conflated (missing leading newline) output: %q", got)
	}
}

// TestPrettyFaultInjectionMixedInlineChildHasNoSpuriousTrailingNewline proves
// the old conflated behavior (a tag with mixed text + inline-tag children
// was treated as "not inline" and given indented block layout) is gone: it
// would have produced a trailing newline + indentation before </p>.
func TestPrettyFaultInjectionMixedInlineChildHasNoSpuriousTrailingNewline(t *testing.T) {
	got := prettyRender(t, "p Text #[a foo]")
	if got == "\n<p>Text \n  <a>foo</a>\n</p>" {
		t.Fatalf("got the old conflated (spurious trailing newline) output: %q", got)
	}
}

// --- Compact mode: byte-identical, zero expectation changes. ---

// TestCompactBlockTagSingleTextChildUnchanged pins that compact-mode output
// for a block tag with a single text child is untouched by the pretty-mode
// fix.
func TestCompactBlockTagSingleTextChildUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "p Hello", nil), "<p>Hello</p>")
}

// TestCompactMixedTextAndInlineChildUnchanged pins that compact-mode output
// for a tag with mixed text + inline-tag children is untouched by the
// pretty-mode fix.
func TestCompactMixedTextAndInlineChildUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "p Text #[a foo]", nil), "<p>Text <a>foo</a></p>")
}

// TestCompactNestedBlockChildrenUnchanged pins that compact-mode output for
// nested block-level children is untouched by the pretty-mode fix.
func TestCompactNestedBlockChildrenUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "div\n  p Hello\n  p World", nil), "<div><p>Hello</p><p>World</p></div>")
}
