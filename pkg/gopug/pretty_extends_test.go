package gopug

import "testing"

// These tests close the pretty-mode blind spot for composition (extends +
// block override): the family was exercised almost entirely in compact mode
// before this file existed. Every want string below was captured by
// rendering the equivalent template through real pug.js 3.0.4
// (perf-compare/node_modules/pug) with {pretty: true}.

// writePrettyExtendsFixture writes a layout with five named blocks — one
// covering each override mode a child can use in a single template — plus a
// child that exercises all of them at once: block replace (title), block
// append (header), block prepend (content), combined prepend+append on one
// block (footer), and an unoverridden default block nested inside a tag
// (div.wrapper > block nested). It returns the child's absolute path.
func writePrettyExtendsFixture(t *testing.T, dir string) string {
	t.Helper()
	mustWriteFile(t, dir, "layout.pug", "html\n"+
		"  head\n"+
		"    block title\n"+
		"      title Default Title\n"+
		"  body\n"+
		"    header\n"+
		"      block header\n"+
		"        h1 Default Header\n"+
		"    main\n"+
		"      block content\n"+
		"        p Default Content\n"+
		"    div.wrapper\n"+
		"      block nested\n"+
		"        p Default Nested\n"+
		"    footer\n"+
		"      block footer\n"+
		"        p Default Footer\n")
	return mustWriteFile(t, dir, "child.pug", "extends layout.pug\n"+
		"block title\n"+
		"  title Child Title\n"+
		"block append header\n"+
		"  p Appended Header\n"+
		"block prepend content\n"+
		"  p Prepended Content\n"+
		"block prepend footer\n"+
		"  p Prepended Footer\n"+
		"block append footer\n"+
		"  p Appended Footer\n")
}

// TestPrettyExtendsAllOverrideModes pins pug.js 3.0.4's exact pretty-mode
// bytes for a child that replaces one block, appends to another, prepends to
// a third, both prepends and appends the same block, and leaves one block
// (nested inside a tag) at its parent default.
func TestPrettyExtendsAllOverrideModes(t *testing.T) {
	dir := t.TempDir()
	childPath := writePrettyExtendsFixture(t, dir)

	out, err := RenderFile(childPath, nil, &Options{Pretty: true, Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}

	want := "\n<html>\n  <head>\n    <title>Child Title</title>\n  </head>\n  <body>\n    <header>\n      <h1>Default Header</h1>\n      <p>Appended Header</p>\n    </header>\n    <main>\n      <p>Prepended Content</p>\n      <p>Default Content</p>\n    </main>\n    <div class=\"wrapper\">\n      <p>Default Nested</p>\n    </div>\n    <footer>\n      <p>Prepended Footer</p>\n      <p>Default Footer</p>\n      <p>Appended Footer</p>\n    </footer>\n  </body>\n</html>"
	assertEqual(t, out, want)
}

// TestPrettyExtendsThreeLevelChain pins pug.js 3.0.4's exact pretty-mode
// bytes for a three-level extends chain (root -> mid -> leaf) where mid fully
// replaces the root's block and leaf appends to mid's replacement — proving
// the append is resolved against the nearest ancestor's own content, not the
// root's original default.
func TestPrettyExtendsThreeLevelChain(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "root.pug", "html\n  body\n    block content\n      p Root Default\n")
	mustWriteFile(t, dir, "mid.pug", "extends root.pug\nblock content\n  p Mid Content\n")
	leafPath := mustWriteFile(t, dir, "leaf.pug", "extends mid.pug\nblock append content\n  p Leaf Appended\n")

	out, err := RenderFile(leafPath, nil, &Options{Pretty: true, Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}

	want := "\n<html>\n  <body>\n    <p>Mid Content</p>\n    <p>Leaf Appended</p>\n  </body>\n</html>"
	assertEqual(t, out, want)
}
