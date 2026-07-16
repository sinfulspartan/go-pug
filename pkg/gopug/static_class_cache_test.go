package gopug

import (
	"fmt"
	"sync"
	"testing"
)

// forceStaticClassFallback clears hasStaticClass on every TagNode reachable
// from nodes, forcing renderTag to take the unchanged runtime class-value
// dispatch for every tag regardless of what compileTagAttrs computed — i.e.
// the exact behavior renderTag had before this cache existed. It leaves
// noSpread/sortedAttrNames untouched so it only exercises the new cache, not
// the earlier attribute-name-sort cache.
func forceStaticClassFallback(nodes []Node) {
	walkTagNodes(nodes, func(tag *TagNode) {
		tag.hasStaticClass = false
	})
}

// firstTagWithClass returns the first TagNode reachable from nodes that
// carries a non-empty "class" attribute value.
func firstTagWithClass(nodes []Node) *TagNode {
	var found *TagNode
	walkTagNodes(nodes, func(tag *TagNode) {
		if found != nil {
			return
		}
		if attr, ok := tag.Attributes["class"]; ok && attr.Value != "" {
			found = tag
		}
	})
	return found
}

// cacheableStaticClassCases lists every shape that must reach
// resolveClassTokenList's whole-quoted, non-empty branch and therefore must
// be classified static at compile time, along with the exact string
// compileTagAttrs should cache for it.
var cacheableStaticClassCases = []struct {
	name            string
	src             string
	wantStaticClass string
}{
	{"shorthand single class", `div.form-group`, "form-group"},
	{"shorthand multiple classes", `li.item.active`, "item active"},
	{"quoted multi-word", `div(class="a b c")`, "a b c"},
	{"quoted single word", `div(class="single")`, "single"},
	{"quoted multi-space collapses", `div(class="a   b")`, "a b"},
	{"quoted leading and trailing space trims", `div(class="  lead  ")`, "lead"},
	{"shorthand merged with quoted explicit class", `.base(class="foo bar")`, "base foo bar"},
	{
		// The whole value stays whole-quoted even though "badge" also names
		// an in-scope variable; resolveClassTokenList's invariant is that a
		// whole-quoted class value's words are never evaluated, so the
		// collision must not change the cached result.
		"quoted literal colliding with an in-scope variable name",
		`div(class="badge")`,
		"badge",
	},
	{
		// Attribute values are plain JS-style expressions in Pug, not a
		// text-interpolation context: a quoted string's `#{...}` is literal
		// text, never substituted (verified against pug.js 3.0.4 — see this
		// task's report). The value stays whole-quoted, so it is still
		// provably static and correctly cached.
		"quoted literal containing pug-interpolation-shaped text",
		`div(class="a #{b} c")`,
		"a #{b} c",
	},
}

// mustFallThroughClassCases lists every shape that must NOT be classified
// static — each one either evaluates a variable, an operator, an object, or
// a merge expression at render time, so compileTagAttrs must leave
// hasStaticClass false and renderTag must keep taking its existing dispatch.
var mustFallThroughClassCases = []struct {
	name string
	src  string
	data map[string]any
}{
	{"merge-shape shorthand + operator expression", `.btn(class="btn-" + style)`, map[string]any{"style": "primary"}},
	{"object-shape, condition true", `div(class={active: cond})`, map[string]any{"cond": true}},
	{"object-shape, condition false", `div(class={active: cond})`, map[string]any{"cond": false}},
	{"operator ternary", `div(class=(a ? b : c))`, map[string]any{"a": true, "b": "yes", "c": "no"}},
	{"bare variable, set", `div(class=cls)`, map[string]any{"cls": "foo"}},
	{"bare variable, empty", `div(class=cls)`, map[string]any{"cls": ""}},
	{"string concatenation", `div(class="a" + b)`, map[string]any{"b": "dyn"}},
	{"backtick template literal, no interpolation", "div(class=`active`)", nil},
	{"backtick template literal with interpolation", "div(class=`item-${idx}`)", map[string]any{"idx": 3}},
	{"empty quoted class", `div(class="")`, nil},
	{
		"shorthand merged with bare dynamic variable, set",
		`.text-end(class=cls)`,
		map[string]any{"cls": "extra"},
	},
	{
		"shorthand merged with bare dynamic variable, empty (known collision-regression shape)",
		`.text-end(class=cls)`,
		map[string]any{"cls": ""},
	},
}

// TestClassifyStaticClassAttrCacheableCases asserts compileTagAttrs marks
// each cacheable-static shape hasStaticClass = true with the exact resolved
// string.
func TestClassifyStaticClassAttrCacheableCases(t *testing.T) {
	for _, tc := range cacheableStaticClassCases {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			tag := firstTagWithClass(tpl.ast.Children)
			if tag == nil {
				t.Fatalf("no tag with a class attribute found in %q", tc.src)
			}
			if !tag.hasStaticClass {
				t.Fatalf("hasStaticClass = false, want true for %q", tc.src)
			}
			if tag.staticClass != tc.wantStaticClass {
				t.Errorf("staticClass = %q, want %q", tag.staticClass, tc.wantStaticClass)
			}
		})
	}
}

// TestClassifyStaticClassAttrFallthroughCases asserts compileTagAttrs leaves
// hasStaticClass false for every shape that must be evaluated at render
// time.
func TestClassifyStaticClassAttrFallthroughCases(t *testing.T) {
	for _, tc := range mustFallThroughClassCases {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			tag := firstTagWithClass(tpl.ast.Children)
			if tag == nil {
				t.Fatalf("no tag with a class attribute found in %q", tc.src)
			}
			if tag.hasStaticClass {
				t.Errorf("hasStaticClass = true, want false for %q (cached value %q)", tc.src, tag.staticClass)
			}
		})
	}
}

// TestStaticClassCacheSpreadTagNeverCached asserts a tag with a provably
// static class value that ALSO carries an "&attributes" spread is never
// cached: a spread can inject or append to the class value at render time
// (see mergeSpreadClass), so caching would go stale the instant a spread
// touched that tag.
func TestStaticClassCacheSpreadTagNeverCached(t *testing.T) {
	tpl, err := Compile(`div.form-group&attributes(extra)`, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	tag := firstTagWithClass(tpl.ast.Children)
	if tag == nil {
		t.Fatalf("no tag with a class attribute found")
	}
	if tag.noSpread {
		t.Fatalf("noSpread = true, want false (tag has &attributes)")
	}
	if tag.hasStaticClass {
		t.Errorf("hasStaticClass = true, want false for a tag with an &attributes spread")
	}
}

// TestStaticClassCacheByteIdenticalRender is the render-level byte-identity
// proof — THE safety net for this change. For every cacheable-static shape,
// every must-fall-through shape, a spread tag, and a mixed template, it
// renders once with the compile-time cache in place and once with the cache
// forced off (forceStaticClassFallback), and asserts the two outputs are
// byte-for-byte identical.
func TestStaticClassCacheByteIdenticalRender(t *testing.T) {
	type tc struct {
		name string
		src  string
		data map[string]any
	}

	var cases []tc
	for _, c := range cacheableStaticClassCases {
		cases = append(cases, tc{c.name, c.src, nil})
	}
	for _, c := range mustFallThroughClassCases {
		cases = append(cases, tc{c.name, c.src, c.data})
	}
	cases = append(cases,
		tc{"spread tag with static-looking class", `div.form-group&attributes(extra)`, map[string]any{"extra": map[string]any{"data-x": "1"}}},
		tc{"spread tag whose spread carries its own class", `div.form-group&attributes(extra)`, map[string]any{"extra": map[string]any{"class": "more"}}},
		tc{
			"mixed template with many class shapes",
			"div.container\n" +
				"  span.badge Static\n" +
				"  p(class=\"a b\") Also static\n" +
				"  a.btn(class=\"btn-\" + variant) Merge\n" +
				"  div(class={active: isActive}) Object\n" +
				"  i(class=(flag ? \"on\" : \"off\")) Ternary\n" +
				"  b(class=freeform) Bare\n" +
				"  em.text-end(class=trailing) Collision-merge\n" +
				"  u&attributes(spreadAttrs) Spread\n",
			map[string]any{
				"variant":     "primary",
				"isActive":    true,
				"flag":        false,
				"freeform":    "runtime-class",
				"trailing":    "",
				"spreadAttrs": map[string]any{"class": "extra-spread"},
			},
		},
	)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tplCached, err := Compile(c.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			outCached, err := tplCached.Render(c.data)
			if err != nil {
				t.Fatalf("Render (cached) error: %v", err)
			}

			tplForced, err := Compile(c.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			forceStaticClassFallback(tplForced.ast.Children)
			outForced, err := tplForced.Render(c.data)
			if err != nil {
				t.Fatalf("Render (forced fallback) error: %v", err)
			}

			if outCached != outForced {
				t.Errorf("cached-path render != forced-fallback render\ncached: %q\nforced: %q", outCached, outForced)
			}
		})
	}
}

// TestTagNodeZeroValueHasStaticClassDefaultsToFallback pins the fail-safe
// default: a TagNode compileTagAttrs never reaches keeps hasStaticClass at
// its zero value, false, which routes renderTag to its unchanged runtime
// class-value dispatch — still correct, just unoptimized.
func TestTagNodeZeroValueHasStaticClassDefaultsToFallback(t *testing.T) {
	tag := &TagNode{
		Name: "div",
		Attributes: map[string]*AttributeValue{
			"class": {Value: `"static-looking"`},
		},
	}
	if tag.hasStaticClass {
		t.Fatalf("zero-value TagNode.hasStaticClass = true, want false")
	}
	if tag.staticClass != "" {
		t.Fatalf("zero-value TagNode.staticClass = %q, want empty", tag.staticClass)
	}
}

// TestStaticClassCacheConcurrentRenderRace renders the same compiled
// Template, containing both cacheable-static and must-fall-through class
// shapes, from many goroutines simultaneously and asserts every render
// produces identical output. staticClass/hasStaticClass are set once at
// Compile time and only read at render time, so this must be race-clean
// under `go test -race`.
func TestStaticClassCacheConcurrentRenderRace(t *testing.T) {
	src := "div.page\n" +
		"  ul\n" +
		"    each item in items\n" +
		"      li.item(data-id=item)\n" +
		"  span(class=\"a b c\") Static\n" +
		"  a.btn(class=\"btn-\" + variant) Merge\n" +
		"  div(class={active: isActive}) Object\n" +
		"  div.footer&attributes(extra)\n"

	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	newData := func() map[string]any {
		return map[string]any{
			"items":    []any{"a", "b", "c"},
			"variant":  "primary",
			"isActive": true,
			"extra":    map[string]any{"data-x": "1", "class": "extra"},
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
