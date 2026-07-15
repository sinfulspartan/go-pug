package gopug

import "testing"

// Consecutive piped `|` lines under the same tag are joined with a newline
// in pug.js 3.0.4's own compact-mode output — a semantic newline, not a
// pretty-print artifact. These differential tests pin that behavior against
// values measured directly from pug.js 3.0.4 (perf-compare/node_modules/pug),
// and prove the OLD no-separator output no longer occurs.

// TestPipeTextTwoLinesJoinWithNewline pins pug.js 3.0.4's own output for two
// consecutive piped lines: pug.render("p\n  | First\n  | Second") ===
// "<p>First\nSecond</p>".
func TestPipeTextTwoLinesJoinWithNewline(t *testing.T) {
	out := renderTest(t, "p\n  | First\n  | Second", nil)
	assertEqual(t, out, "<p>First\nSecond</p>")
}

// TestPipeTextThreeLinesJoinWithNewline pins pug.js 3.0.4's own output for
// three consecutive piped lines: each line boundary gets exactly one
// newline, verbatim content otherwise unchanged.
func TestPipeTextThreeLinesJoinWithNewline(t *testing.T) {
	out := renderTest(t, "p\n  | one\n  | two\n  | three", nil)
	assertEqual(t, out, "<p>one\ntwo\nthree</p>")
}

// TestPipeTextTrailingSpacePreserved pins pug.js 3.0.4's own output when a
// piped line has a trailing space before the newline: pug.render("p\n  |
// First \n  | Second") === "<p>First \nSecond</p>" — the source's own
// trailing space survives untouched; the join adds nothing but the newline
// itself.
func TestPipeTextTrailingSpacePreserved(t *testing.T) {
	out := renderTest(t, "p\n  | First \n  | Second", nil)
	assertEqual(t, out, "<p>First \nSecond</p>")
}

// TestPipeTextSingleLineNoNewline is a regression guard: a single piped
// line must never gain a leading or trailing newline just because the join
// logic now exists.
func TestPipeTextSingleLineNoNewline(t *testing.T) {
	out := renderTest(t, "p\n  | Lone line", nil)
	assertEqual(t, out, "<p>Lone line</p>")
}

// TestPipeTextInterpolationMidLineNoNewline is a regression guard: text and
// an interpolation sharing ONE source line (as opposed to two separate
// piped lines) must render back-to-back with no injected newline.
func TestPipeTextInterpolationMidLineNoNewline(t *testing.T) {
	out := renderTest(t, "p\n  | a #{x} b", map[string]interface{}{"x": "X"})
	assertEqual(t, out, "<p>a X b</p>")
}

// TestPipeTextTwoLinesFaultInjection proves
// TestPipeTextTwoLinesJoinWithNewline is actually exercising the newline
// join, not merely checking that the template renders: the OLD (buggy)
// no-separator output must NOT be produced any more.
func TestPipeTextTwoLinesFaultInjection(t *testing.T) {
	out := renderTest(t, "p\n  | First\n  | Second", nil)
	oldBuggyOutput := "<p>FirstSecond</p>"
	if out == oldBuggyOutput {
		t.Fatalf("fault injection did not fault: output %q matches the old no-separator behavior", out)
	}
}

// TestPipeTextTwoLinesPrettyModeIndented pins pug.js 3.0.4's own pretty-mode
// output for the same two-line template: pug.render(src, {pretty:true}) ===
// "\n<p>\n  First\n  Second\n</p>" — the semantic newline combines with
// pretty-mode's own indentation rather than duplicating it.
func TestPipeTextTwoLinesPrettyModeIndented(t *testing.T) {
	out, err := Render("p\n  | First\n  | Second", nil, &Options{Pretty: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertEqual(t, out, "\n<p>\n  First\n  Second\n</p>")
}

// TestPipeTextDoesNotJoinAcrossOtherNodeTypes pins pug.js 3.0.4's own output
// when a non-text sibling (a tag) interrupts a run of piped lines: the join
// only happens between two piped lines that are directly consecutive in the
// source, never across an intervening tag.
func TestPipeTextDoesNotJoinAcrossOtherNodeTypes(t *testing.T) {
	out := renderTest(t, "p\n  | First\n  span Middle\n  | Last", nil)
	assertEqual(t, out, "<p>First<span>Middle</span>Last</p>")
}

// TestPipeTextAfterInlineTagTextNotJoined pins pug.js 3.0.4's own output
// when a tag's own inline text (on the tag's own line) is followed by a
// piped line in its block: the two are NOT joined, because inline-after-tag
// text and a following piped-text run are structurally distinct — the join
// only applies within one contiguous run of piped lines.
func TestPipeTextAfterInlineTagTextNotJoined(t *testing.T) {
	out := renderTest(t, "p Text\n  | more", nil)
	assertEqual(t, out, "<p>Textmore</p>")
}

// TestBlockTextDotStillJoinsWithNewlineUnaffected proves the dot-block text
// path (`p.`, BlockTextNode — a completely different node type from the
// piped-line TextRunNode this fix touches) already joined its lines with a
// newline before this fix and is untouched by it.
func TestBlockTextDotStillJoinsWithNewlineUnaffected(t *testing.T) {
	out := renderTest(t, "p.\n  First\n  Second", nil)
	assertEqual(t, out, "<p>First\nSecond</p>")
}

// TestPipeTextEachLoopBodyJoinsPerIteration proves the join applies inside
// an each-loop body exactly as it does under a tag, and that each iteration
// gets its own independent join (no bleed between iterations).
func TestPipeTextEachLoopBodyJoinsPerIteration(t *testing.T) {
	out := renderTest(t, "each x in items\n  | one\n  | two", map[string]interface{}{"items": []interface{}{1, 2}})
	assertEqual(t, out, "one\ntwoone\ntwo")
}

// TestCodegenPipeTextTwoLinesJoinWithNewline proves codegen's own TextRun
// emission gets the identical line-boundary newline rule as the fixed
// interpreter, so generated code stays byte-identical to it for consecutive
// piped lines — pinned against pug.js 3.0.4's own output.
func TestCodegenPipeTextTwoLinesJoinWithNewline(t *testing.T) {
	t.Parallel()
	src := "p\n  | First\n  | Second\n"
	want := "<p>First\nSecond</p>"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	interpWant, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}
	if interpWant != want {
		t.Fatalf("interpreter Render(%q) = %q, want %q (probe assumption broken)", src, interpWant, want)
	}

	got := runGeneratedGo(t, generated, "opsData{}")
	if got != want {
		t.Errorf("codegen output %q does not match expected %q for %q", got, want, src)
	}
}

// TestCodegenPipeTextTwoLinesFaultInjection proves
// TestCodegenPipeTextTwoLinesJoinWithNewline is actually exercising the
// newline join in the CODEGEN path, not merely checking that the generated
// program built and ran: the old no-separator output must not occur.
func TestCodegenPipeTextTwoLinesFaultInjection(t *testing.T) {
	t.Parallel()
	src := "p\n  | First\n  | Second\n"
	oldBuggyOutput := "<p>FirstSecond</p>"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, "opsData{}")
	if got == oldBuggyOutput {
		t.Fatalf("fault injection did not fault: generated output %q matches the old no-separator behavior", got)
	}
}
