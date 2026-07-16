package gopug

import (
	"fmt"
	"sync"
	"testing"
)

// TestMergedAttrsNoSpreadAliasByteIdenticalRender proves that aliasing
// renderTag's local merged map directly to tag.Attributes for a no-spread
// tag (skipping the make+copy) produces output identical to forcing every
// tag through the unchanged make+copy path. forceAttrSortFallback (defined
// alongside the attribute-name-sort cache) clears noSpread on every tag,
// which routes renderTag's merged construction into the copy branch too,
// since both are gated on the same flag.
func TestMergedAttrsNoSpreadAliasByteIdenticalRender(t *testing.T) {
	cases := []struct{ name, src string }{
		{"single no-spread tag, many attrs", `div#a.b(title="t" data-x="1" data-y="2")`},
		{"nested no-spread tags", "div.outer\n  span.inner(title=\"hi\")\n  p(data-z=\"3\")\n"},
		{
			"mix of spread and no-spread",
			"div#a.x(title=\"t\")\n  div.base&attributes(extra)\n  span(data-z=\"1\")\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{"extra": map[string]any{"data-foo": "bar", "class": "more"}}

			tplAliased, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			outAliased, err := tplAliased.Render(data)
			if err != nil {
				t.Fatalf("Render (aliased) error: %v", err)
			}

			tplCopy, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			forceAttrSortFallback(tplCopy.ast.Children)
			outCopy, err := tplCopy.Render(data)
			if err != nil {
				t.Fatalf("Render (forced copy) error: %v", err)
			}

			if outAliased != outCopy {
				t.Errorf("aliased-path render != forced-copy-path render\naliased: %q\ncopy:    %q", outAliased, outCopy)
			}
		})
	}
}

// TestMergedAttrsNoSpreadAliasDoesNotMutateSharedAST renders a compiled
// template containing a no-spread tag many times and asserts the tag's
// Attributes map — the shared AST state every render's merged map aliases
// directly — still holds exactly its original keys and values afterward.
// This is the defense-in-depth companion to the concurrent race test below:
// it would catch a silent single-goroutine corruption that a race run might
// not happen to schedule into a detectable interleaving.
func TestMergedAttrsNoSpreadAliasDoesNotMutateSharedAST(t *testing.T) {
	tpl, err := Compile(`div#a.b(title="t" data-x="1")`, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	var tag *TagNode
	walkTagNodes(tpl.ast.Children, func(tg *TagNode) {
		if tag == nil {
			tag = tg
		}
	})
	if tag == nil {
		t.Fatal("no tag found")
	}
	if !tag.noSpread {
		t.Fatal("tag.noSpread = false, want true (test template has no &attributes)")
	}

	before := make(map[string]string, len(tag.Attributes))
	for k, v := range tag.Attributes {
		before[k] = v.Value
	}

	for i := 0; i < 25; i++ {
		if _, err := tpl.Render(nil); err != nil {
			t.Fatalf("Render error: %v", err)
		}
	}

	if len(tag.Attributes) != len(before) {
		t.Fatalf("tag.Attributes key count changed: got %d, want %d", len(tag.Attributes), len(before))
	}
	for k, wantVal := range before {
		got, ok := tag.Attributes[k]
		if !ok {
			t.Errorf("tag.Attributes[%q] missing after render", k)
			continue
		}
		if got.Value != wantVal {
			t.Errorf("tag.Attributes[%q].Value = %q, want %q", k, got.Value, wantVal)
		}
	}
}

// TestMergedAttrsNoSpreadAliasConcurrentRenderRace renders the same compiled
// Template — every tag no-spread, so every render takes the merged-map alias
// path — from many goroutines simultaneously and asserts every render
// produces identical output. tag.Attributes is compile-time-only-written and
// only read at render time, so this must be race-clean under `go test -race`.
func TestMergedAttrsNoSpreadAliasConcurrentRenderRace(t *testing.T) {
	src := `div#page.container(data-role="main")
  ul
    each item in items
      li.item(data-id=item)
  span.footer(data-x="1")
`
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	newData := func() map[string]any {
		return map[string]any{"items": []any{"a", "b", "c"}}
	}

	want, err := tpl.Render(newData())
	if err != nil {
		t.Fatalf("Render error: %v", err)
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
				got, err := tpl.Render(newData())
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- fmt.Errorf("concurrent render mismatch:\ngot:  %q\nwant: %q", got, want)
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
