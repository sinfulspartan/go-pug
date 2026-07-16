package gopug

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

// TestExtendsUncachedStringRenderBlockOverrideRace renders, from many
// goroutines simultaneously, an extends+block-override template compiled
// from a raw string (Compile, not CompileFile/RenderFile) with only a
// Basedir set and no entry file. That combination bypasses extendsCache on
// every single render (see renderExtends's cacheKey computation), so
// resolveExtendsAST/applyBlockOverrides re-run against the compiled
// Template's shared AST (Template.ast) on every one of those renders — the
// exposure window TestExtendsCacheConcurrentRenderRace (which uses
// RenderFile and is cached after its first render) never opens.
//
// The child template exercises three block-override shapes in one pass:
//   - a "block prepend" whose override body has spare backing-array capacity
//     sized to exactly swallow the parent's default block body (the parser
//     builds a block's Body via make([]Node,0) plus one append per node, so
//     a 3-node override body has cap 4 after normal Go slice growth, and the
//     parent's single-node default body fits exactly in that one spare
//     slot) — the classic in-place-write-into-shared-capacity hazard.
//   - a default-mode "block" (replace) whose replacement content itself
//     contains a nested named block, combined with a separate top-level
//     override for that same nested block name — the replace-then-recurse-
//     into-the-aliased-shared-body hazard, which mutates a shared
//     BlockNode's own field directly regardless of any spare capacity.
//   - a "block append" on that nested block, included for completeness of
//     the shared-content coverage above.
//
// Every render must match a golden computed from a single render up front,
// and the whole thing must be race-detector clean.
func TestExtendsUncachedStringRenderBlockOverrideRace(t *testing.T) {
	dir := t.TempDir()
	basePath := dir + "/base.pug"
	baseSrc := "html\n" +
		"  body\n" +
		"    block content\n" +
		"      p Default\n" +
		"    block outer\n" +
		"      block inner\n" +
		"        p Inner-Default\n"
	if err := os.WriteFile(basePath, []byte(baseSrc), 0644); err != nil {
		t.Fatal(err)
	}

	childSrc := "extends base\n" +
		"block prepend content\n" +
		"  p One\n" +
		"  p Two\n" +
		"  p Three\n" +
		"block outer\n" +
		"  div\n" +
		"    block inner\n" +
		"      p Inner-Replaced\n" +
		"block append inner\n" +
		"  p Inner-Extra\n"

	opts := &Options{Basedir: dir}
	tpl, err := Compile(childSrc, opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if opts.entryFile != "" {
		t.Fatalf("test setup invalid: opts.entryFile must stay empty to exercise the uncached string-render path, got %q", opts.entryFile)
	}

	want, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("Render (golden): %v", err)
	}
	for _, substr := range []string{"<p>One</p>", "<p>Two</p>", "<p>Three</p>", "<p>Default</p>", "<p>Inner-Replaced</p>", "<p>Inner-Extra</p>"} {
		assertContains(t, want, substr)
	}

	const goroutines = 60
	const rendersEach = 60
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rendersEach; j++ {
				got, err := tpl.Render(nil)
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- fmt.Errorf("concurrent uncached extends render mismatch:\ngot:  %q\nwant: %q", got, want)
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
