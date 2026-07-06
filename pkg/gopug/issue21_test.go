package gopug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #21: include/extends path resolution was inconsistent between the
// top-level render target (Basedir-relative) and nested includes (file-relative).
// The fix threads the entry file's path into the runtime so *every* relative
// include/extends resolves against the directory of the file doing the
// including — standard Pug semantics — while a leading slash stays
// Basedir-relative.

// writeTree writes {relpath: contents} under root, creating parent dirs.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The core fix: a top-level render target that lives in a SUBDIRECTORY of
// Basedir now resolves a bare relative include against its own directory, not
// Basedir. Before the fix this resolved against Basedir and failed.
func TestIssue21TopLevelInSubdirIsFileRelative(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"layout/page.pug":    "div\n  include _header.pug\n",
		"layout/_header.pug": "h1 Header\n",
	})

	out, err := RenderFile(filepath.Join(root, "layout/page.pug"), nil, &Options{Basedir: root})
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertEqual(t, out, "<div><h1>Header</h1></div>")
}

// Nested relative includes were already file-relative — must stay that way.
func TestIssue21NestedStaysFileRelative(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"top.pug":        "include sub/nested.pug\n",
		"sub/nested.pug": "div\n  include leaf.pug\n",
		"sub/leaf.pug":   "span leaf\n",
	})

	out, err := RenderFile(filepath.Join(root, "top.pug"), nil, &Options{Basedir: root})
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertEqual(t, out, "<div><span>leaf</span></div>")
}

// A leading slash remains Basedir-relative at every depth (the escape hatch).
func TestIssue21LeadingSlashIsBasedirRelative(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"layout/page.pug":  "div\n  include /shared/_foot.pug\n",
		"shared/_foot.pug": "footer Foot\n",
	})

	out, err := RenderFile(filepath.Join(root, "layout/page.pug"), nil, &Options{Basedir: root})
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertEqual(t, out, "<div><footer>Foot</footer></div>")
}

// The payoff from the original issue: a partial used BOTH as a nested include
// AND rendered directly as a top-level target can now use the *same* bare
// include line in both roles.
func TestIssue21DualRolePartialSameIncludeLine(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"page.pug":          "include sub/_fragment.pug\n",
		"sub/_fragment.pug": "section\n  include _content.pug\n",
		"sub/_content.pug":  "p Content\n",
	})

	// Role 1: reached via a nested include from the top-level page.
	nested, err := RenderFile(filepath.Join(root, "page.pug"), nil, &Options{Basedir: root})
	if err != nil {
		t.Fatalf("nested RenderFile error: %v", err)
	}
	assertEqual(t, nested, "<section><p>Content</p></section>")

	// Role 2: the same fragment rendered directly as the top-level target —
	// its bare `include _content.pug` must resolve identically.
	direct, err := RenderFile(filepath.Join(root, "sub/_fragment.pug"), nil, &Options{Basedir: root})
	if err != nil {
		t.Fatalf("direct RenderFile error: %v", err)
	}
	assertEqual(t, direct, "<section><p>Content</p></section>")
}

// extends from a top-level file in a subdirectory resolves file-relative too,
// in lockstep with includes.
func TestIssue21ExtendsFileRelativeFromSubdir(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pages/child.pug": "extends _base.pug\nblock body\n  p Child\n",
		"pages/_base.pug": "html\n  body\n    block body\n",
	})

	out, err := RenderFile(filepath.Join(root, "pages/child.pug"), nil, &Options{Basedir: root})
	if err != nil {
		t.Fatalf("RenderFile error: %v", err)
	}
	assertEqual(t, out, "<html><body><p>Child</p></body></html>")
}

// A relative include that fails but WOULD resolve against Basedir gets a
// leading-slash migration hint. A genuine typo does not.
func TestIssue21BasedirResolveHint(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"layout/page.pug": "include partial/x.pug\n",      // resolves file-relative → layout/partial/x.pug (missing)
		"partial/x.pug":   "p X\n",                        // ...but exists at Basedir/partial/x.pug
		"layout/typo.pug": "include does/not/exist.pug\n", // no Basedir candidate either
	})

	_, err := RenderFile(filepath.Join(root, "layout/page.pug"), nil, &Options{Basedir: root})
	if err == nil {
		t.Fatal("expected include-resolution error, got nil")
	}
	if !strings.Contains(err.Error(), "leading-slash") {
		t.Errorf("expected a leading-slash hint, got: %v", err)
	}

	_, err = RenderFile(filepath.Join(root, "layout/typo.pug"), nil, &Options{Basedir: root})
	if err == nil {
		t.Fatal("expected include-resolution error for typo, got nil")
	}
	if strings.Contains(err.Error(), "leading-slash") {
		t.Errorf("hint should not fire for a genuine typo, got: %v", err)
	}
}

// String render (no entry file) keeps the documented Basedir-relative fallback
// for relative includes.
func TestIssue21StringRenderFallsBackToBasedir(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"widget.pug": "b widget\n",
	})

	out, err := Render("div\n  include widget.pug\n", nil, &Options{Basedir: root})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	assertEqual(t, out, "<div><b>widget</b></div>")
}
