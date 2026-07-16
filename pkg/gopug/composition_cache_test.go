package gopug

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Include-partial AST cache (parsedIncludeCache)
// ---------------------------------------------------------------------------

// TestIncludeCacheLoopWithBlockExpansionPartialIsCorrect renders an include
// living inside an each loop, where the partial itself uses block expansion
// (tag: child). Once the partial's parsed AST is reused across loop
// iterations (rather than re-parsed fresh every time), a block-expansion
// node that mutated its own parent's Children in place would duplicate its
// child once per extra iteration within a SINGLE render — a much sharper
// trigger than rendering the same *Template twice. This is why the
// block-expansion node's Children must never be mutated for the include
// cache to be safe.
func TestIncludeCacheLoopWithBlockExpansionPartialIsCorrect(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	mustWriteFile(t, dir, "_card.pug", "li: span= item\n")
	mainPath := mustWriteFile(t, dir, "main.pug", strings.Join([]string{
		"ul",
		"  each item in items",
		"    include _card.pug",
		"",
	}, "\n"))

	data := map[string]any{"items": []any{"a", "b", "c", "d", "e"}}

	out, err := RenderFile(mainPath, data, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}

	var b strings.Builder
	b.WriteString("<ul>")
	for _, v := range []string{"a", "b", "c", "d", "e"} {
		b.WriteString("<li><span>")
		b.WriteString(v)
		b.WriteString("</span></li>")
	}
	b.WriteString("</ul>")
	want := b.String()

	if out != want {
		t.Errorf("loop-include with block-expansion partial produced wrong output.\ngot:  %q\nwant: %q", out, want)
	}
}

// TestIncludeCacheReuseIsByteIdenticalAcrossRenders renders the same
// file-based template — an each loop that includes a partial once per
// iteration — several times in a row and asserts every render is identical,
// proving reuse of the cached partial AST changes nothing observable.
func TestIncludeCacheReuseIsByteIdenticalAcrossRenders(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	mustWriteFile(t, dir, "_row.pug", ".row\n  span= item\n")
	mainPath := mustWriteFile(t, dir, "main.pug", strings.Join([]string{
		"table",
		"  each item in items",
		"    include _row.pug",
		"",
	}, "\n"))

	data := map[string]any{"items": []any{1, 2, 3}}

	first, err := RenderFile(mainPath, data, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile (first): %v", err)
	}
	for i := 0; i < 5; i++ {
		got, err := RenderFile(mainPath, data, &Options{Basedir: dir})
		if err != nil {
			t.Fatalf("RenderFile (repeat %d): %v", i, err)
		}
		if got != first {
			t.Errorf("render %d diverged from the first render.\nfirst: %q\ngot:   %q", i, first, got)
		}
	}
}

// TestIncludeCacheSurvivesPartialDeletionAfterFirstRender proves the
// included partial's parsed AST is actually reused, not merely harmless to
// reuse: after a first successful render populates parsedIncludeCache, the
// partial file is deleted from disk, and a second render must still succeed
// with identical output — it never touches the filesystem again for that
// partial.
func TestIncludeCacheSurvivesPartialDeletionAfterFirstRender(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	partialPath := mustWriteFile(t, dir, "_footer.pug", "footer\n  p Footer Text\n")
	mainPath := mustWriteFile(t, dir, "main.pug", "div\n  include _footer.pug\n")

	first, err := RenderFile(mainPath, nil, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile (first): %v", err)
	}
	if !strings.Contains(first, "Footer Text") {
		t.Fatalf("first render %q missing expected footer content", first)
	}

	if err := os.Remove(partialPath); err != nil {
		t.Fatalf("removing partial: %v", err)
	}

	second, err := RenderFile(mainPath, nil, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile (second, after partial deleted): %v", err)
	}
	if second != first {
		t.Errorf("render after partial deletion diverged (cache not reused).\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestIncludeCacheConcurrentRenderRace renders a loop-nested-include
// template from many goroutines simultaneously and asserts every render
// matches a known-good golden, under `go test -race`.
func TestIncludeCacheConcurrentRenderRace(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	mustWriteFile(t, dir, "_item.pug", "li: strong= item\n")
	mainPath := mustWriteFile(t, dir, "main.pug", strings.Join([]string{
		"ul",
		"  each item in items",
		"    include _item.pug",
		"",
	}, "\n"))

	newData := func() map[string]any {
		return map[string]any{"items": []any{"x", "y", "z", "w"}}
	}
	opts := &Options{Basedir: dir}

	want, err := RenderFile(mainPath, newData(), opts)
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}

	const goroutines = 50
	const rendersEach = 20
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rendersEach; j++ {
				got, err := RenderFile(mainPath, newData(), opts)
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- fmt.Errorf("concurrent include-cache render mismatch:\ngot:  %q\nwant: %q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// Resolved-extends-tree cache (extendsCache)
// ---------------------------------------------------------------------------

// writeExtendsFixture writes a layout.pug (three blocks: header, content,
// footer, each with default content) and a child.pug that extends it with an
// override (header), an append (content), and a prepend (footer) — covering
// every block-override mode in one fixture — returning the child's path.
func writeExtendsFixture(t *testing.T, dir string) string {
	t.Helper()
	mustWriteFile(t, dir, "layout.pug", strings.Join([]string{
		"doctype html",
		"html",
		"  body",
		"    block header",
		"      p Default Header",
		"    block content",
		"      p Default Content",
		"    block footer",
		"      p Default Footer",
		"",
	}, "\n"))
	return mustWriteFile(t, dir, "child.pug", strings.Join([]string{
		"extends layout.pug",
		"block header",
		"  p Overridden Header",
		"block append content",
		"  p Appended Content",
		"block prepend footer",
		"  p Prepended Footer",
		"",
	}, "\n"))
}

// TestExtendsCacheReuseIsByteIdenticalAcrossRenders renders an
// extends+block-override/append/prepend chain several times in a row and
// asserts every render is identical, proving reuse of the cached resolved
// root AST changes nothing observable.
func TestExtendsCacheReuseIsByteIdenticalAcrossRenders(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	childPath := writeExtendsFixture(t, dir)
	opts := &Options{Basedir: dir}

	first, err := RenderFile(childPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile (first): %v", err)
	}
	if !strings.Contains(first, "Overridden Header") ||
		!strings.Contains(first, "Default Content") || !strings.Contains(first, "Appended Content") ||
		!strings.Contains(first, "Prepended Footer") || !strings.Contains(first, "Default Footer") {
		t.Fatalf("first render %q missing an expected override/append/prepend fragment", first)
	}

	for i := 0; i < 5; i++ {
		got, err := RenderFile(childPath, nil, opts)
		if err != nil {
			t.Fatalf("RenderFile (repeat %d): %v", i, err)
		}
		if got != first {
			t.Errorf("render %d diverged from the first render.\nfirst: %q\ngot:   %q", i, first, got)
		}
	}
}

// TestExtendsCacheSurvivesParentDeletionAfterFirstRender proves the resolved
// extends chain is actually reused, not merely harmless to reuse: after a
// first successful render populates extendsCache, the parent layout file is
// deleted from disk, and a second render must still succeed with identical
// output — it never re-reads or re-resolves the parent chain.
func TestExtendsCacheSurvivesParentDeletionAfterFirstRender(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	childPath := writeExtendsFixture(t, dir)
	opts := &Options{Basedir: dir}

	first, err := RenderFile(childPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile (first): %v", err)
	}

	if err := os.Remove(filepath.Join(dir, "layout.pug")); err != nil {
		t.Fatalf("removing layout: %v", err)
	}

	second, err := RenderFile(childPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile (second, after layout deleted): %v", err)
	}
	if second != first {
		t.Errorf("render after layout deletion diverged (cache not reused).\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestExtendsCacheDoesNotCollideOnDifferentBasedirs proves the cache key
// folds in Basedir, not just the entry file's absolute path: two children at
// the identically-named relative path under different temp directories, one
// using a leading-slash (Basedir-relative) extends, must each resolve
// against their OWN layout, never the other's.
func TestExtendsCacheDoesNotCollideOnDifferentBasedirs(t *testing.T) {
	ClearCache()
	dirA := t.TempDir()
	dirB := t.TempDir()

	mustWriteFile(t, dirA, "layout.pug", "html\n  block content\n    p A Default\n")
	childA := mustWriteFile(t, dirA, "page.pug", "extends /layout.pug\nblock content\n  p A Override\n")

	mustWriteFile(t, dirB, "layout.pug", "html\n  block content\n    p B Default\n")
	childB := mustWriteFile(t, dirB, "page.pug", "extends /layout.pug\nblock content\n  p B Override\n")

	outA, err := RenderFile(childA, nil, &Options{Basedir: dirA})
	if err != nil {
		t.Fatalf("RenderFile A: %v", err)
	}
	outB, err := RenderFile(childB, nil, &Options{Basedir: dirB})
	if err != nil {
		t.Fatalf("RenderFile B: %v", err)
	}

	if !strings.Contains(outA, "A Override") || strings.Contains(outA, "B Override") {
		t.Errorf("child A output resolved against the wrong layout: %q", outA)
	}
	if !strings.Contains(outB, "B Override") || strings.Contains(outB, "A Override") {
		t.Errorf("child B output resolved against the wrong layout: %q", outB)
	}
}

// TestClearCacheInvalidatesIncludeAndExtendsCaches proves ClearCache's
// documented contract holds for both new composition caches, not just
// compiledCache: after a render populates parsedIncludeCache and
// extendsCache, editing the partial and the parent layout on disk changes
// nothing until ClearCache is called, and does change after it.
func TestClearCacheInvalidatesIncludeAndExtendsCaches(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	mustWriteFile(t, dir, "_greeting.pug", "p Hello v1\n")
	mustWriteFile(t, dir, "layout.pug", strings.Join([]string{
		"html",
		"  body",
		"    include _greeting.pug",
		"    block content",
		"      p Default v1",
		"",
	}, "\n"))
	childPath := mustWriteFile(t, dir, "child.pug", strings.Join([]string{
		"extends layout.pug",
		"block content",
		"  p Child v1",
		"",
	}, "\n"))
	opts := &Options{Basedir: dir}

	first, err := RenderFile(childPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile (first): %v", err)
	}
	if !strings.Contains(first, "Hello v1") || !strings.Contains(first, "Child v1") {
		t.Fatalf("first render %q missing expected v1 content", first)
	}

	mustWriteFile(t, dir, "_greeting.pug", "p Hello v2\n")
	mustWriteFile(t, dir, "layout.pug", strings.Join([]string{
		"html",
		"  body",
		"    include _greeting.pug",
		"    block content",
		"      p Default v2",
		"",
	}, "\n"))

	stale, err := RenderFile(childPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile (stale, before ClearCache): %v", err)
	}
	if stale != first {
		t.Errorf("render before ClearCache diverged from the first render even though it should still be cached.\nfirst: %q\nstale: %q", first, stale)
	}

	ClearCache()

	fresh, err := RenderFile(childPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile (fresh, after ClearCache): %v", err)
	}
	if !strings.Contains(fresh, "Hello v2") {
		t.Errorf("render after ClearCache %q does not reflect the edited partial", fresh)
	}
	if !strings.Contains(fresh, "Child v1") {
		t.Errorf("render after ClearCache %q lost the child's own block override", fresh)
	}
}

// TestExtendsCacheConcurrentRenderRace renders an
// extends+block-override/append/prepend template from many goroutines
// simultaneously and asserts every render matches a known-good golden, under
// `go test -race`.
func TestExtendsCacheConcurrentRenderRace(t *testing.T) {
	ClearCache()
	dir := t.TempDir()
	childPath := writeExtendsFixture(t, dir)
	opts := &Options{Basedir: dir}

	want, err := RenderFile(childPath, nil, opts)
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}

	const goroutines = 50
	const rendersEach = 20
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rendersEach; j++ {
				got, err := RenderFile(childPath, nil, opts)
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- fmt.Errorf("concurrent extends-cache render mismatch:\ngot:  %q\nwant: %q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
