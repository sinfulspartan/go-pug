package gopug

import "testing"

// go-pug's inlineTagNames (runtime.go) must contain exactly the tag names
// pug-parser's own inline-tags.js lists, no more and no fewer: a, abbr,
// acronym, b, br, code, em, font, i, img, ins, kbd, map, samp, small, span,
// strong, sub, sup. The tests below pin values measured directly from
// pug.js 3.0.4 (perf-compare/node_modules/pug) against Options{Pretty: true}
// for every name added or removed from the prior (incorrect) set.

// TestPrettyRemovedInlineTagButtonOwnLeadingNewline pins pug.js 3.0.4:
// pug.render("button Click", {pretty:true}) === "\n<button>Click</button>" —
// button is not in pug-parser's inline-tags list, so it starts on its own
// line like any other block-named tag.
func TestPrettyRemovedInlineTagButtonOwnLeadingNewline(t *testing.T) {
	assertEqual(t, prettyRender(t, "button Click"), "\n<button>Click</button>")
}

// TestPrettyRemovedInlineTagButtonForcesParentBlockLayout pins pug.js 3.0.4:
// pug.render("div\n  button Click", {pretty:true}) ===
// "\n<div>\n  <button>Click</button>\n</div>" — button is no longer an
// inline-named child, so the childCanInline ripple forces its parent into
// indented block layout.
func TestPrettyRemovedInlineTagButtonForcesParentBlockLayout(t *testing.T) {
	assertEqual(t, prettyRender(t, "div\n  button Click"),
		"\n<div>\n  <button>Click</button>\n</div>")
}

// TestPrettyRemovedInlineTagLabelOwnLeadingNewline pins pug.js 3.0.4:
// pug.render("label Name", {pretty:true}) === "\n<label>Name</label>".
func TestPrettyRemovedInlineTagLabelOwnLeadingNewline(t *testing.T) {
	assertEqual(t, prettyRender(t, "label Name"), "\n<label>Name</label>")
}

// TestPrettyRemovedInlineTagLabelForcesParentBlockLayout pins pug.js 3.0.4:
// pug.render("p\n  label Name", {pretty:true}) ===
// "\n<p>\n  <label>Name</label>\n</p>".
func TestPrettyRemovedInlineTagLabelForcesParentBlockLayout(t *testing.T) {
	assertEqual(t, prettyRender(t, "p\n  label Name"),
		"\n<p>\n  <label>Name</label>\n</p>")
}

// TestPrettyRemovedInlineTagSelectOwnLeadingNewline pins pug.js 3.0.4:
// pug.render("select Pick", {pretty:true}) === "\n<select>Pick</select>".
func TestPrettyRemovedInlineTagSelectOwnLeadingNewline(t *testing.T) {
	assertEqual(t, prettyRender(t, "select Pick"), "\n<select>Pick</select>")
}

// TestPrettyRemovedInlineTagInputForcesParentBlockLayout pins pug.js 3.0.4's
// block-layout decision for input as a sole child: pug.render("div\n
// input", {pretty:true}) === "\n<div>\n  <input/>\n</div>" — input is not
// in pug-parser's inline-tags list either, so it forces its parent into
// block layout just like button/label/select. go-pug always renders a void
// element without the self-closing slash (a separate, pre-existing,
// deliberate HTML5-terse choice unrelated to inline-tag-set membership),
// so the expected string below uses go-pug's own "<input>" spelling while
// still pinning the same leading-newline/indentation shape pug.js produces.
func TestPrettyRemovedInlineTagInputForcesParentBlockLayout(t *testing.T) {
	assertEqual(t, prettyRender(t, "div\n  input"),
		"\n<div>\n  <input>\n</div>")
}

// TestPrettyAddedInlineTagFontStaysInline pins pug.js 3.0.4:
// pug.render("p\n  font x", {pretty:true}) === "\n<p><font>x</font></p>" —
// font is in pug-parser's inline-tags list, so as a sole child it keeps its
// parent inlined.
func TestPrettyAddedInlineTagFontStaysInline(t *testing.T) {
	assertEqual(t, prettyRender(t, "p\n  font x"), "\n<p><font>x</font></p>")
}

// TestPrettyAddedInlineTagFontOwnLine pins pug.js 3.0.4:
// pug.render("font x", {pretty:true}) === "<font>x</font>" — font never
// starts on its own line.
func TestPrettyAddedInlineTagFontOwnLine(t *testing.T) {
	assertEqual(t, prettyRender(t, "font x"), "<font>x</font>")
}

// TestPrettyAddedInlineTagInsStaysInline pins pug.js 3.0.4:
// pug.render("p\n  ins x", {pretty:true}) === "\n<p><ins>x</ins></p>" — ins
// is in pug-parser's inline-tags list, so as a sole child it keeps its
// parent inlined.
func TestPrettyAddedInlineTagInsStaysInline(t *testing.T) {
	assertEqual(t, prettyRender(t, "p\n  ins x"), "\n<p><ins>x</ins></p>")
}

// TestPrettyAddedInlineTagInsOwnLine pins pug.js 3.0.4:
// pug.render("ins x", {pretty:true}) === "<ins>x</ins>" — ins never starts
// on its own line.
func TestPrettyAddedInlineTagInsOwnLine(t *testing.T) {
	assertEqual(t, prettyRender(t, "ins x"), "<ins>x</ins>")
}

// TestPrettyKeptInlineTagAcronymUnaffected pins pug.js 3.0.4:
// pug.render("div\n  acronym Click", {pretty:true}) ===
// "\n<div><acronym>Click</acronym></div>" — acronym IS in pug-parser's
// inline-tags list (unlike a prior mis-transcription that assumed it was
// not), so it must stay inline both standalone and as a child.
func TestPrettyKeptInlineTagAcronymUnaffected(t *testing.T) {
	assertEqual(t, prettyRender(t, "acronym Click"), "<acronym>Click</acronym>")
	assertEqual(t, prettyRender(t, "div\n  acronym Click"),
		"\n<div><acronym>Click</acronym></div>")
}

// TestPrettyRealisticFormSnippet pins pug.js 3.0.4's own line-for-line
// layout shape for a form containing a label, an input, and a button — all
// three now block-named, each starting on its own indented line:
// pug.render("form\n  label Name\n  input(name=\"name\")\n  button
// Submit", {pretty:true}) === "\n<form>\n  <label>Name</label>\n
// <input name=\"name\"/>\n  <button>Submit</button>\n</form>". go-pug
// always renders a void element without the self-closing slash (a
// separate, pre-existing, deliberate HTML5-terse choice unrelated to
// inline-tag-set membership), so the expected string below uses go-pug's
// own "<input name=\"name\">" spelling while still pinning the same
// leading-newline/indentation shape pug.js produces for all three tags.
func TestPrettyRealisticFormSnippet(t *testing.T) {
	src := "form\n  label Name\n  input(name=\"name\")\n  button Submit"
	want := "\n<form>\n  <label>Name</label>\n  <input name=\"name\">\n  <button>Submit</button>\n</form>"
	assertEqual(t, prettyRender(t, src), want)
}

// --- Fault injection: the OLD (incorrect) inline-tag-set output must not occur. ---

// TestPrettyFaultInjectionButtonNoLongerTreatedAsInline proves the old
// (incorrect) classification of button as inline-named is gone: it would
// have produced no leading newline and no childCanInline block-layout
// ripple.
func TestPrettyFaultInjectionButtonNoLongerTreatedAsInline(t *testing.T) {
	if got := prettyRender(t, "button Click"); got == "<button>Click</button>" {
		t.Fatalf("got the old (missing leading newline) output: %q", got)
	}
	if got := prettyRender(t, "div\n  button Click"); got == "\n<div><button>Click</button></div>" {
		t.Fatalf("got the old (parent wrongly stayed inline) output: %q", got)
	}
}

// TestPrettyFaultInjectionSelectNoLongerTreatedAsInline proves the old
// (incorrect) classification of select as inline-named is gone.
func TestPrettyFaultInjectionSelectNoLongerTreatedAsInline(t *testing.T) {
	if got := prettyRender(t, "select Pick"); got == "<select>Pick</select>" {
		t.Fatalf("got the old (missing leading newline) output: %q", got)
	}
}

// --- Compact mode: byte-identical, zero expectation changes. ---

// TestCompactButtonLabelSelectUnchanged pins that compact-mode output for
// the tags whose inline-set membership changed is untouched, since
// inlineTagNames is only consulted on r.pretty()-gated paths.
func TestCompactButtonLabelSelectUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "div\n  button Click\n  label Name\n  select Pick", nil),
		"<div><button>Click</button><label>Name</label><select>Pick</select></div>")
}

// TestCompactFontInsUnchanged pins that compact-mode output for the newly
// added inline tags is untouched.
func TestCompactFontInsUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "p\n  font x\n  ins y", nil), "<p><font>x</font><ins>y</ins></p>")
}
