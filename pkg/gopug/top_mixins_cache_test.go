package gopug

import (
	"fmt"
	"sync"
	"testing"
)

// TestTopMixinsCacheConcurrentRenderRace renders a single compiled Template
// declaring a top-level mixin from many goroutines simultaneously and
// asserts every render matches a known single-threaded baseline, under
// `go test -race`. Template.topMixins is written once at Compile time and
// only ever read afterward, so this must be race-clean.
func TestTopMixinsCacheConcurrentRenderRace(t *testing.T) {
	src := "mixin item(name, price)\n" +
		"  li\n" +
		"    span.name= name\n" +
		"    span.price= price\n" +
		"ul\n" +
		"  each p in products\n" +
		"    +item(p.Name, p.Price)\n"

	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	newData := func() map[string]any {
		return map[string]any{
			"products": []any{
				map[string]any{"Name": "Widget", "Price": "9.99"},
				map[string]any{"Name": "Gadget", "Price": "19.99"},
				map[string]any{"Name": "Gizmo", "Price": "29.99"},
			},
		}
	}

	want, err := tpl.Render(newData())
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	const goroutines = 50
	const rendersEach = 50
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rendersEach; j++ {
				got, err := tpl.Render(newData())
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- fmt.Errorf("concurrent top-mixin render mismatch:\ngot:  %q\nwant: %q", got, want)
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

// TestTopMixinsCacheIncludeSameNameMixinWins pins the current resolution
// order for a name collision between a top-level mixin declaration and a
// same-named mixin declared in an included file: the include is rendered
// after both mixin declarations are visible, so its declaration overwrites
// the top-level one, and any subsequent call resolves to the include's
// version. The two-level base/overlay lookup must reproduce this exactly.
func TestTopMixinsCacheIncludeSameNameMixinWins(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "_greet.pug", "mixin greet(name)\n  p Include: #{name}\n")
	mainPath := mustWriteFile(t, dir, "main.pug",
		"mixin greet(name)\n  p Top: #{name}\ninclude _greet.pug\n+greet(\"World\")\n")

	out, err := RenderFile(mainPath, nil, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	want := "<p>Include: World</p>"
	if out != want {
		t.Errorf("include-vs-top-level mixin conflict resolved to the wrong winner.\ngot:  %q\nwant: %q", out, want)
	}
}

// TestTopMixinsCacheExtendsSameNameMixinPreservesCurrentWinner pins the
// current resolution order for a name collision between a mixin declared in
// an extends child and one declared, under the same name, in its parent
// layout: renderExtends's final collectMixins pass walks the resolved root
// AST's own top-level nodes — which, for a two-level chain, are the parent's
// own nodes — so the parent's declaration is the last one written and wins.
// This is a pre-existing quirk this refactor deliberately preserves
// byte-for-byte rather than "fixing".
func TestTopMixinsCacheExtendsSameNameMixinPreservesCurrentWinner(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug",
		"mixin greet(name)\n  p Parent: #{name}\nblock content\n  +greet(\"Default\")\n")
	childPath := mustWriteFile(t, dir, "child.pug",
		"extends layout.pug\nmixin greet(name)\n  p Child: #{name}\nblock content\n  +greet(\"Override\")\n")

	out, err := RenderFile(childPath, nil, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	want := "<p>Parent: Override</p>"
	if out != want {
		t.Errorf("extends child/parent mixin conflict resolved to the wrong winner.\ngot:  %q\nwant: %q", out, want)
	}
}

// TestTopMixinsCacheDirectNewRuntimeResolvesTopLevelMixin asserts that a
// direct NewRuntimeWithOptions caller — one with no Template to precompute
// topMixins from, so topMixinsSeeded stays false — still resolves a
// top-level mixin, via Render's own collectMixins fallback into the
// mixinDecls overlay.
func TestTopMixinsCacheDirectNewRuntimeResolvesTopLevelMixin(t *testing.T) {
	src := "mixin greet(name)\n  p Hello, #{name}!\n+greet(\"Direct\")\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	rt := NewRuntimeWithOptions(ast, map[string]any{}, nil)
	out, err := rt.Render()
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	want := "<p>Hello, Direct!</p>"
	if out != want {
		t.Errorf("direct NewRuntimeWithOptions render = %q, want %q", out, want)
	}
}
