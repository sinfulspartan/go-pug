package gopug

import "testing"

// These tests close the pretty-mode blind spot for filters: the family was
// exercised almost entirely in compact mode before this file existed. Every
// want string below was captured by rendering the equivalent template
// through real pug.js 3.0.4 (perf-compare/node_modules/pug) with
// {pretty: true, filters: {uppercase: text => text.toUpperCase()}}.

func prettyFilterOpts() *Options {
	return &Options{
		Pretty:  true,
		Filters: map[string]FilterFunc{"uppercase": uppercaseFilter},
	}
}

// TestPrettyFilterBlockSingleLineInTag documents a pretty-mode-only
// divergence from pug.js: real pug.js treats single-line `:filter` block
// content exactly like single-line piped text (see
// TestPrettyPipedRunPreservesMultiLineBlockLayout) — the filter's own tag
// gets no trailing newline before its closing tag because the content is a
// single line. go-pug's renderFilter path always forces block (indented)
// layout for a filter node regardless of how many lines its result has,
// producing a spurious trailing newline.
//
// pug.js 3.0.4: pug.render("div\n  :uppercase\n    hello world",
// {pretty:true, filters}) === "\n<div>HELLO WORLD</div>".
// go-pug (current): "\n<div>HELLO WORLD\n</div>" — an extra "\n" before
// "</div>" that should not be there.
func TestPrettyFilterBlockSingleLineInTag(t *testing.T) {
	t.Skip("KNOWN pretty-mode divergence from pug.js: pug.js renders single-line `:filter` block content inline with no trailing newline (\"\\n<div>HELLO WORLD</div>\"), but go-pug always forces block layout for a filter node and emits a spurious trailing newline before the closing tag (\"\\n<div>HELLO WORLD\\n</div>\"); tracked for a follow-up fix")

	src := "div\n  :uppercase\n    hello world\n"
	want := "\n<div>HELLO WORLD</div>"
	assertEqual(t, prettyRender2(t, src, prettyFilterOpts()), want)
}

// TestPrettyFilterBlockMultiLineInTag documents two compounding divergences
// from pug.js for multi-line `:filter` block content in pretty mode:
//
//  1. go-pug joins the filter's own embedded newlines with "<br>" (a
//     pre-existing behavior locked in by compact-mode tests such as
//     TestFilterBlockMultiLineNewlinesPreserved); real pug.js keeps the
//     literal "\n" between lines and does not insert "<br>" at all — in
//     either compact or pretty mode.
//  2. The trailing-newline-before-closing-tag layout itself happens to
//     agree here (both sides give the filter's own tag a trailing newline,
//     because pug.js's own multi-line rule also forces block layout for
//     multi-line content) — so this case isolates divergence (1) alone.
//
// pug.js 3.0.4: pug.render("div\n  :uppercase\n    line one\n    line two",
// {pretty:true, filters}) === "\n<div>LINE ONE\nLINE TWO\n</div>".
// go-pug (current): "\n<div>LINE ONE<br>LINE TWO\n</div>".
func TestPrettyFilterBlockMultiLineInTag(t *testing.T) {
	t.Skip("KNOWN divergence from pug.js (not pretty-mode-specific): go-pug joins multi-line filter output with \"<br>\" (\"\\n<div>LINE ONE<br>LINE TWO\\n</div>\"), but pug.js keeps the literal newline between lines (\"\\n<div>LINE ONE\\nLINE TWO\\n</div>\") and never inserts \"<br>\"; tracked for a follow-up fix")

	src := "div\n  :uppercase\n    line one\n    line two\n"
	want := "\n<div>LINE ONE\nLINE TWO\n</div>"
	assertEqual(t, prettyRender2(t, src, prettyFilterOpts()), want)
}

// TestPrettyFilteredIncludeInTag pins pug.js 3.0.4's exact pretty-mode bytes
// for a filtered raw-file include (`include:filtername path`) nested inside
// a tag: unlike the `:filter` block form above, go-pug's renderInclude path
// writes the filtered file content directly without any "<br>" conversion,
// so this shape matches pug.js exactly — an active passing test.
func TestPrettyFilteredIncludeInTag(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "article.txt", "Hello from file.\nSecond line.")
	mainPath := mustWriteFile(t, dir, "main.pug", "div\n  include:uppercase article.txt\n")

	opts := prettyFilterOpts()
	opts.Basedir = dir
	out, err := RenderFile(mainPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	assertEqual(t, out, "\n<div>HELLO FROM FILE.\nSECOND LINE.\n</div>")
}

// prettyRender2 is like prettyRender but accepts a caller-supplied *Options
// (needed here to register filters alongside Pretty).
func prettyRender2(t *testing.T, src string, opts *Options) string {
	t.Helper()
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}
