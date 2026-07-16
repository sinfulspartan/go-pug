package gopug

import "testing"

// These tests close the pretty-mode blind spot for `include`: the family was
// exercised almost entirely in compact mode before this file existed. Every
// want string below was captured by rendering the equivalent template
// through real pug.js 3.0.4 (perf-compare/node_modules/pug) with
// {pretty: true}.

// TestPrettyIncludeTopLevel pins pug.js 3.0.4's exact pretty-mode bytes for a
// partial included directly inside a parent tag: the included subtree is
// indented one level deeper than its including tag.
func TestPrettyIncludeTopLevel(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "_greeting.pug", "p Hello\n")
	mainPath := mustWriteFile(t, dir, "main.pug", "div\n  include _greeting.pug\n")

	out, err := RenderFile(mainPath, nil, &Options{Pretty: true, Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	assertEqual(t, out, "\n<div>\n  <p>Hello</p>\n</div>")
}

// TestPrettyIncludeInsideEachLoop pins pug.js 3.0.4's exact pretty-mode
// bytes for a partial included once per iteration of an each loop: every
// rendered item sits at the same indentation as a sibling written directly
// in the parent.
func TestPrettyIncludeInsideEachLoop(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "_item.pug", "li= item\n")
	mainPath := mustWriteFile(t, dir, "main.pug", "ul\n  each item in items\n    include _item.pug\n")

	out, err := RenderFile(mainPath, map[string]any{"items": []any{"a", "b", "c"}}, &Options{Pretty: true, Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	assertEqual(t, out, "\n<ul>\n  <li>a</li>\n  <li>b</li>\n  <li>c</li>\n</ul>")
}

// TestPrettyIncludeOfNestedTags pins pug.js 3.0.4's exact pretty-mode bytes
// for a partial whose own body has several levels of nested tags: every
// level of the partial's own nesting must be indented on top of the
// including tag's indentation, not reset back to column zero.
func TestPrettyIncludeOfNestedTags(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "_card.pug", "div.card\n  h2 Title\n  p Body\n")
	mainPath := mustWriteFile(t, dir, "main.pug", "section\n  include _card.pug\n")

	out, err := RenderFile(mainPath, nil, &Options{Pretty: true, Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	assertEqual(t, out, "\n<section>\n  <div class=\"card\">\n    <h2>Title</h2>\n    <p>Body</p>\n  </div>\n</section>")
}
