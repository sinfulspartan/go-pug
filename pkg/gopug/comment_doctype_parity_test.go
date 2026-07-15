package gopug

import "testing"

// The expected strings in this file were measured directly against pug.js
// 3.0.4 (via `pug.render(src)`), so they are the parity oracle for go-pug's
// comment and doctype rendering rather than a hand-derived expectation.

// TestCommentVerbatimNoPadding pins pug.js 3.0.4's buffered-comment
// rendering: the content after "//" is emitted exactly as written, with no
// added leading/trailing padding space and no trimming of the source's own
// whitespace.
func TestCommentVerbatimNoPadding(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "single leading space", src: "// foo", want: "<!-- foo-->"},
		{name: "no space", src: "//foo", want: "<!--foo-->"},
		{name: "trailing spaces preserved", src: "// foo   ", want: "<!-- foo   -->"},
		{name: "sentence with trailing period, no source trailing space", src: "// This is an HTML comment.", want: "<!-- This is an HTML comment.-->"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderTest(t, tc.src, nil)
			assertEqual(t, out, tc.want)
		})
	}
}

// TestCommentBlockJoinedVerbatim pins pug.js 3.0.4's block-comment
// rendering: the dedented child lines are joined by "\n" with no extra
// padding around the block.
func TestCommentBlockJoinedVerbatim(t *testing.T) {
	src := "//\n  line one\n  line two"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<!--line one\nline two-->")
}

// TestCommentSilentStillUnrendered pins that "//-" (unbuffered) comments
// remain entirely absent from the output after the verbatim-content change.
func TestCommentSilentStillUnrendered(t *testing.T) {
	out := renderTest(t, "//- silent", nil)
	assertEqual(t, out, "")
}

// TestCommentOldPaddingFormatFails is a fault injection: the pre-fix go-pug
// behavior padded the content with a leading and trailing space
// ("<!-- "+content+" -->"), which must no longer match pug.js 3.0.4 for
// content with no source whitespace immediately after "//".
func TestCommentOldPaddingFormatFails(t *testing.T) {
	out := renderTest(t, "//foo", nil)
	oldBuggyOutput := "<!-- foo -->"
	if out == oldBuggyOutput {
		t.Errorf("comment still uses the old padded format %q; want pug.js 3.0.4's %q", oldBuggyOutput, "<!--foo-->")
	}
}

// TestDoctypeFiveIsLiteral pins pug.js 3.0.4's "doctype 5" behavior: pug.js
// has no "5" shortcut, so it is emitted as a literal doctype name.
func TestDoctypeFiveIsLiteral(t *testing.T) {
	out := renderTest(t, "doctype 5", nil)
	assertEqual(t, out, "<!DOCTYPE 5>")
}

// TestDoctypeFiveOldAliasFails is a fault injection: the pre-fix go-pug
// behavior aliased "doctype 5" to the HTML5 declaration, which must no
// longer be what go-pug produces.
func TestDoctypeFiveOldAliasFails(t *testing.T) {
	out := renderTest(t, "doctype 5", nil)
	oldBuggyOutput := "<!DOCTYPE html>"
	if out == oldBuggyOutput {
		t.Errorf("doctype 5 still aliases to %q; want pug.js 3.0.4's literal %q", oldBuggyOutput, "<!DOCTYPE 5>")
	}
}

// TestDoctypePlistFullDTD pins pug.js 3.0.4's "doctype plist" behavior: the
// full Apple PLIST public/system DTD, not a bare "<!DOCTYPE plist>".
func TestDoctypePlistFullDTD(t *testing.T) {
	out := renderTest(t, "doctype plist", nil)
	assertEqual(t, out, `<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`)
}

// TestDoctypeAlreadyCorrectShortcuts pins every doctype shortcut that was
// already correct before this fix, verified against pug.js 3.0.4, so a
// future change to formatDoctype cannot regress them.
func TestDoctypeAlreadyCorrectShortcuts(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "bare doctype", src: "doctype", want: "<!DOCTYPE html>"},
		{name: "html", src: "doctype html", want: "<!DOCTYPE html>"},
		{name: "xml", src: "doctype xml", want: `<?xml version="1.0" encoding="utf-8" ?>`},
		{name: "transitional", src: "doctype transitional", want: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`},
		{name: "strict", src: "doctype strict", want: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">`},
		{name: "frameset", src: "doctype frameset", want: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Frameset//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-frameset.dtd">`},
		{name: "1.1", src: "doctype 1.1", want: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">`},
		{name: "basic", src: "doctype basic", want: `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML Basic 1.1//EN" "http://www.w3.org/TR/xhtml-basic/xhtml-basic11.dtd">`},
		{name: "mobile", src: "doctype mobile", want: `<!DOCTYPE html PUBLIC "-//WAPFORUM//DTD XHTML Mobile 1.2//EN" "http://www.openmobilealliance.org/tech/DTD/xhtml-mobile12.dtd">`},
		{name: "unknown literal fallback", src: "doctype foo", want: "<!DOCTYPE foo>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderTest(t, tc.src, nil)
			assertEqual(t, out, tc.want)
		})
	}
}

// TestCodegenCommentVerbatimNoPadding proves codegen's comment emission
// stays byte-identical to the interpreter's for the pug.js 3.0.4 verbatim
// content cases, not just the already-existing loose interleaving tests.
func TestCodegenCommentVerbatimNoPadding(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "single leading space", src: "// foo\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "no space", src: "//foo\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "trailing spaces preserved", src: "// foo   \n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "block comment joined verbatim", src: "//\n  line one\n  line two\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenDoctypeFiveAndPlist proves codegen's doctype emission (which
// calls the shared formatDoctype) stays byte-identical to the interpreter's
// for the two cases this fix changed.
func TestCodegenDoctypeFiveAndPlist(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "doctype 5 literal", src: "doctype 5\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "doctype plist full DTD", src: "doctype plist\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}
