package gopug

import "testing"

// pug.js 3.0.4's pug-code-gen (WHITE_SPACE_SENSITIVE_TAGS + escapePrettyMode,
// index.js) suppresses pretty-print whitespace insertion for the content of a
// `pre` or `textarea` element and its whole subtree, so that significant
// whitespace inside them survives untouched. The values pinned below were
// measured directly against pug.js 3.0.4 (perf-compare/node_modules/pug)
// with Options{Pretty: true}.
//
// The mechanism is narrower than a blanket "no prettyNewline() calls inside
// a pre/textarea subtree": a nested tag's own leading/closing newline is
// still governed entirely by its own isInline/tagCanInline classification
// (unaffected by any ancestor) — only (1) the pre/textarea tag's OWN closing
// tag never gets a trailing newline, regardless of tagCanInline, and (2) the
// newline that separates consecutive text lines (piped `|` runs and
// multi-line block `.` text) loses its added indentation — but keeps the
// bare newline itself — anywhere within the subtree.

// TestPrettyPreSingleLineTextNoExtraWhitespace pins pug.js 3.0.4:
// pug.render("pre Text", {pretty:true}) === "\n<pre>Text</pre>".
func TestPrettyPreSingleLineTextNoExtraWhitespace(t *testing.T) {
	assertEqual(t, prettyRender(t, "pre Text"), "\n<pre>Text</pre>")
}

// TestPrettyPreMultiLinePipedTextNoIndent pins pug.js 3.0.4:
// pug.render("pre\n  | Line1\n  | Line2", {pretty:true}) ===
// "\n<pre>Line1\nLine2</pre>" — the piped lines keep their separating
// newline but gain no leading or interior indentation.
func TestPrettyPreMultiLinePipedTextNoIndent(t *testing.T) {
	assertEqual(t, prettyRender(t, "pre\n  | Line1\n  | Line2"), "\n<pre>Line1\nLine2</pre>")
}

// TestPrettyPreNestedInlineNamedTagChild pins pug.js 3.0.4:
// pug.render("pre\n  code Some code", {pretty:true}) ===
// "\n<pre><code>Some code</code></pre>".
func TestPrettyPreNestedInlineNamedTagChild(t *testing.T) {
	assertEqual(t, prettyRender(t, "pre\n  code Some code"), "\n<pre><code>Some code</code></pre>")
}

// TestPrettyPreNestedBlockPlusInline is the headline residual case, pinning
// pug.js 3.0.4: pug.render("pre\n  div\n    span Hi", {pretty:true}) ===
// "\n<pre>\n  <div><span>Hi</span></div></pre>" — the nested <div> still
// gets its own indented leading newline (its own isInline classification is
// untouched by the pre ancestor), but pre's closing tag gets none, even
// though tagCanInline(pre) would otherwise say it needs one (its content is
// not inline, since div is a block-named child).
func TestPrettyPreNestedBlockPlusInline(t *testing.T) {
	assertEqual(t, prettyRender(t, "pre\n  div\n    span Hi"), "\n<pre>\n  <div><span>Hi</span></div></pre>")
}

// TestPrettyPreNestedBlockWithMultiLineText pins pug.js 3.0.4:
// pug.render("pre\n  div\n    | Line1\n    | Line2", {pretty:true}) ===
// "\n<pre>\n  <div>Line1\nLine2\n  </div></pre>" — the nested div still gets
// its own leading newline and (because its multi-line text content forces
// block layout by pug.js's own isInline test on the synthetic newline text
// node) its own closing newline too; only the interior text-line separator
// loses its indentation, and pre's own closing tag stays suppressed.
func TestPrettyPreNestedBlockWithMultiLineText(t *testing.T) {
	assertEqual(t, prettyRender(t, "pre\n  div\n    | Line1\n    | Line2"),
		"\n<pre>\n  <div>Line1\nLine2\n  </div></pre>")
}

// TestPrettyTextareaSingleLine pins pug.js 3.0.4:
// pug.render("textarea Hello", {pretty:true}) === "\n<textarea>Hello</textarea>".
// textarea is NOT in pug-parser's own inline-tags list, so — unlike
// go-pug's prior (incorrect) classification — it still gets its own leading
// newline like any other block-named tag.
func TestPrettyTextareaSingleLine(t *testing.T) {
	assertEqual(t, prettyRender(t, "textarea Hello"), "\n<textarea>Hello</textarea>")
}

// TestPrettyTextareaMultiLinePipedTextNoIndent pins pug.js 3.0.4:
// pug.render("textarea\n  | Hello\n  | World", {pretty:true}) ===
// "\n<textarea>Hello\nWorld</textarea>".
func TestPrettyTextareaMultiLinePipedTextNoIndent(t *testing.T) {
	assertEqual(t, prettyRender(t, "textarea\n  | Hello\n  | World"), "\n<textarea>Hello\nWorld</textarea>")
}

// TestPrettyTextareaAsSiblingOfP pins pug.js 3.0.4:
// pug.render("div\n  textarea Hello\n  p After", {pretty:true}) ===
// "\n<div>\n  <textarea>Hello</textarea>\n  <p>After</p>\n</div>" —
// textarea behaves exactly like a block-named tag among its siblings.
func TestPrettyTextareaAsSiblingOfP(t *testing.T) {
	assertEqual(t, prettyRender(t, "div\n  textarea Hello\n  p After"),
		"\n<div>\n  <textarea>Hello</textarea>\n  <p>After</p>\n</div>")
}

// TestPrettyPreNestedInsideBlockTag pins pug.js 3.0.4:
// pug.render("div\n  pre\n    span Hi", {pretty:true}) ===
// "\n<div>\n  <pre><span>Hi</span></pre>\n</div>" — the outer div still
// indents normally up to <pre>, and everything inside pre is suppressed.
func TestPrettyPreNestedInsideBlockTag(t *testing.T) {
	assertEqual(t, prettyRender(t, "div\n  pre\n    span Hi"),
		"\n<div>\n  <pre><span>Hi</span></pre>\n</div>")
}

// TestPrettyNormalSiblingAfterPreRestoresIndent pins pug.js 3.0.4:
// pug.render("div\n  pre X\n  p Y", {pretty:true}) ===
// "\n<div>\n  <pre>X</pre>\n  <p>Y</p>\n</div>" — the whitespace-sensitive
// subtree flag is correctly restored after leaving pre, so its sibling <p>
// gets normal indented block layout.
func TestPrettyNormalSiblingAfterPreRestoresIndent(t *testing.T) {
	assertEqual(t, prettyRender(t, "div\n  pre X\n  p Y"),
		"\n<div>\n  <pre>X</pre>\n  <p>Y</p>\n</div>")
}

// TestPrettyNestedPreInsidePre pins pug.js 3.0.4:
// pug.render("pre\n  pre\n    span Hi", {pretty:true}) ===
// "\n<pre>\n  <pre><span>Hi</span></pre></pre>" — the inner pre still gets
// its own leading newline (governed by its own tag-name classification, not
// by the outer pre's suppression), but both closing tags stay suppressed.
func TestPrettyNestedPreInsidePre(t *testing.T) {
	assertEqual(t, prettyRender(t, "pre\n  pre\n    span Hi"),
		"\n<pre>\n  <pre><span>Hi</span></pre></pre>")
}

// TestPrettyPreBlockTextMultiLineNoIndent pins pug.js 3.0.4:
// pug.render("pre.\n  Line1\n  Line2", {pretty:true}) ===
// "\n<pre>Line1\nLine2</pre>" — dot-block text behaves identically to
// piped text for whitespace-sensitivity suppression.
func TestPrettyPreBlockTextMultiLineNoIndent(t *testing.T) {
	assertEqual(t, prettyRender(t, "pre.\n  Line1\n  Line2"), "\n<pre>Line1\nLine2</pre>")
}

// --- Fault injection: prove the old (pre-fix) indented output is gone. ---

// TestPrettyFaultInjectionPreNestedBlockPlusInlineHadExtraClosingNewline
// proves the old bug (an extra newline+indent before </pre> even though
// pug.js suppresses it) is gone.
func TestPrettyFaultInjectionPreNestedBlockPlusInlineHadExtraClosingNewline(t *testing.T) {
	got := prettyRender(t, "pre\n  div\n    span Hi")
	if got == "\n<pre>\n  <div><span>Hi</span></div>\n</pre>" {
		t.Fatalf("got the old (extra closing newline before </pre>) output: %q", got)
	}
}

// TestPrettyFaultInjectionPreMultiLineTextHadIndent proves the old bug
// (piped text lines inside pre gained indentation) is gone.
func TestPrettyFaultInjectionPreMultiLineTextHadIndent(t *testing.T) {
	got := prettyRender(t, "pre\n  | Line1\n  | Line2")
	if got == "\n<pre>\n  Line1\n  Line2\n</pre>" {
		t.Fatalf("got the old (indented piped text) output: %q", got)
	}
}

// TestPrettyFaultInjectionTextareaHadNoLeadingNewline proves the old bug
// (textarea misclassified as inline-named, so it lost its own leading
// newline) is gone.
func TestPrettyFaultInjectionTextareaHadNoLeadingNewline(t *testing.T) {
	got := prettyRender(t, "textarea Hello")
	if got == "<textarea>Hello</textarea>" {
		t.Fatalf("got the old (missing leading newline) output: %q", got)
	}
}

// --- Compact mode: byte-identical, zero expectation changes. ---

// TestCompactPreNestedBlockPlusInlineUnchanged pins that compact-mode output
// for a pre with nested block+inline content is untouched by the
// whitespace-sensitivity fix.
func TestCompactPreNestedBlockPlusInlineUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "pre\n  div\n    span Hi", nil), "<pre><div><span>Hi</span></div></pre>")
}

// TestCompactPreMultiLinePipedTextUnchanged pins that compact-mode output
// for piped multi-line text inside pre is untouched by the
// whitespace-sensitivity fix.
func TestCompactPreMultiLinePipedTextUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "pre\n  | Line1\n  | Line2", nil), "<pre>Line1\nLine2</pre>")
}

// TestCompactTextareaNestedUnchanged pins that compact-mode output for
// textarea with piped multi-line content is untouched by the
// whitespace-sensitivity fix (including textarea's inline-membership
// reconciliation, which is pretty-mode-only in its effect).
func TestCompactTextareaNestedUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "textarea\n  | Hello\n  | World", nil), "<textarea>Hello\nWorld</textarea>")
}

// TestCompactPreBlockTextMultiLineUnchanged pins that compact-mode output
// for dot-block multi-line text inside pre is untouched by the
// whitespace-sensitivity fix.
func TestCompactPreBlockTextMultiLineUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "pre.\n  Line1\n  Line2", nil), "<pre>Line1\nLine2</pre>")
}
