package gopug

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// sortedAttrNamesCacheCases exercises the shapes the byte-identity argument
// depends on: id/class/others, id-only, no-id-no-class, an empty-attribute
// tag, and many attributes — all with no `&attributes` spread.
var sortedAttrNamesCacheCases = []struct {
	name string
	src  string
}{
	{"id, class, and others", `div#main.container(data-x="1" title="hi")`},
	{"id only", `div#only`},
	{"no id no class", `div(title="t" data-y="2")`},
	{"empty attrs", `div`},
	{"many attrs", `div(a="1" b="2" c="3" d="4" e="5" f="6")`},
}

// forceAttrSortFallback clears noSpread on every TagNode reachable from
// nodes, forcing renderTag to take the unconditional sortAttrNames(merged)
// path for every tag regardless of what compileTagAttrs computed — i.e. the
// exact behavior renderTag had before this cache existed.
func forceAttrSortFallback(nodes []Node) {
	walkTagNodes(nodes, func(tag *TagNode) {
		tag.noSpread = false
	})
}

// TestCompileTagAttrsCacheMatchesRuntimeSort asserts that, for every
// no-`&attributes` tag compileTagAttrs reaches, the cached sortedAttrNames
// slice is exactly what sortAttrNames(tag.Attributes) computes — the
// key-set equivalence the byte-identity argument for the render-path cache
// rests on.
func TestCompileTagAttrsCacheMatchesRuntimeSort(t *testing.T) {
	for _, tc := range sortedAttrNamesCacheCases {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			var tags []*TagNode
			walkTagNodes(tpl.ast.Children, func(tag *TagNode) {
				tags = append(tags, tag)
			})
			if len(tags) == 0 {
				t.Fatalf("no tags found in %q", tc.src)
			}
			for _, tag := range tags {
				if !tag.noSpread {
					t.Fatalf("tag %q: noSpread = false, want true (no &attributes present)", tag.Name)
				}
				want := sortAttrNames(tag.Attributes)
				if !reflect.DeepEqual(tag.sortedAttrNames, want) {
					t.Errorf("tag %q: sortedAttrNames = %v, want %v", tag.Name, tag.sortedAttrNames, want)
				}
			}
		})
	}
}

// TestCompileTagAttrsSpreadTagLeavesFallbackFlag asserts a tag with an
// `&attributes` spread is marked noSpread = false, so it keeps taking the
// unchanged runtime sortAttrNames(merged) path.
func TestCompileTagAttrsSpreadTagLeavesFallbackFlag(t *testing.T) {
	tpl, err := Compile(`div.base&attributes(extra)`, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	var tags []*TagNode
	walkTagNodes(tpl.ast.Children, func(tag *TagNode) {
		tags = append(tags, tag)
	})
	if len(tags) != 1 {
		t.Fatalf("found %d tags, want 1", len(tags))
	}
	if tags[0].noSpread {
		t.Errorf("spread tag: noSpread = true, want false (has &attributes)")
	}
}

// TestCompileTagAttrsReachesNestedTags asserts the compile-time precompute
// reaches every TagNode the interpreter can render: inside a mixin body,
// inside an if/else, inside an each loop, and inside a case/when/default —
// not just top-level tags.
func TestCompileTagAttrsReachesNestedTags(t *testing.T) {
	src := `mixin card(title)
  div.card
    h2= title
    if title
      span.badge Active
    else
      span.badge Inactive
ul
  each item in items
    li(data-id=item)
case status
  when "ok"
    p.ok OK
  default
    p.bad Bad
+card(name)
`
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	var tags []*TagNode
	walkTagNodes(tpl.ast.Children, func(tag *TagNode) {
		tags = append(tags, tag)
	})
	if len(tags) < 6 {
		t.Fatalf("walkTagNodes found %d tags, want at least 6 (mixin/if/else/each/case/when/default bodies)", len(tags))
	}
	for _, tag := range tags {
		if !tag.noSpread {
			t.Errorf("tag %q: noSpread = false, want true (none of these tags spread)", tag.Name)
			continue
		}
		want := sortAttrNames(tag.Attributes)
		if !reflect.DeepEqual(tag.sortedAttrNames, want) {
			t.Errorf("tag %q: sortedAttrNames = %v, want %v", tag.Name, tag.sortedAttrNames, want)
		}
	}
}

// TestTagNodeZeroValueNoSpreadDefaultsToFallbackSort pins the fail-safe
// default: a TagNode compileTagAttrs never touches keeps noSpread at its
// zero value, false, which routes renderTag to the unconditional
// sortAttrNames(merged) path — still correct, just unoptimized.
func TestTagNodeZeroValueNoSpreadDefaultsToFallbackSort(t *testing.T) {
	tag := &TagNode{
		Name: "div",
		Attributes: map[string]*AttributeValue{
			"id":    {Value: `"x"`},
			"class": {Value: `"y"`},
		},
	}
	if tag.noSpread {
		t.Fatalf("zero-value TagNode.noSpread = true, want false")
	}
	if tag.sortedAttrNames != nil {
		t.Fatalf("zero-value TagNode.sortedAttrNames = %v, want nil", tag.sortedAttrNames)
	}
}

// TestSortedAttrNamesCacheByteIdenticalRender is the render-level
// byte-identity proof: for a variety of no-spread tag shapes, a spread tag,
// and a template mixing both, rendering with the compile-time cache in place
// must produce output identical to forcing every tag through the unchanged
// runtime sortAttrNames(merged) fallback path — the behavior this change
// replaces for the no-spread case.
func TestSortedAttrNamesCacheByteIdenticalRender(t *testing.T) {
	cases := append([]struct{ name, src string }{}, sortedAttrNamesCacheCases...)
	cases = append(cases,
		struct{ name, src string }{"spread tag", `div.base&attributes(extra)`},
		struct{ name, src string }{
			"mix of spread and no-spread in one template",
			"div#a.x(title=\"t\")\n  div.base&attributes(extra)\n  span(data-z=\"1\")\n",
		},
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{
				"extra": map[string]any{"data-foo": "bar", "class": "more"},
			}

			tplCached, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			outCached, err := tplCached.Render(data)
			if err != nil {
				t.Fatalf("Render (cached) error: %v", err)
			}

			tplFallback, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			forceAttrSortFallback(tplFallback.ast.Children)
			outFallback, err := tplFallback.Render(data)
			if err != nil {
				t.Fatalf("Render (fallback) error: %v", err)
			}

			if outCached != outFallback {
				t.Errorf("cached-path render != fallback-path render\ncached:   %q\nfallback: %q", outCached, outFallback)
			}
		})
	}
}

// TestSortedAttrNamesCacheConcurrentRenderRace renders the same compiled
// Template from many goroutines simultaneously and asserts every render
// produces identical output. sortedAttrNames is set once at Compile time and
// only read at render time, so this must be race-clean and byte-identical
// under `go test -race`.
func TestSortedAttrNamesCacheConcurrentRenderRace(t *testing.T) {
	src := `div#page.container(data-role="main")
  ul
    each item in items
      li.item(data-id=item)
  div.footer&attributes(extra)
`
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	newData := func() map[string]any {
		return map[string]any{
			"items": []any{"a", "b", "c"},
			"extra": map[string]any{"data-x": "1", "class": "extra"},
		}
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
