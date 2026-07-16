package gopug

import "testing"

// TestRenderTagSpreadFinalGateNoSpreadByteIdentical pins that gating the
// spreadFinal map and the &attributes-scan loop behind !tag.noSpread does not
// change output for a tag with no spread at all: plain attributes, a
// class/id combination, and a tag with no attributes.
func TestRenderTagSpreadFinalGateNoSpreadByteIdentical(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"no attrs", `div`, `<div></div>`},
		{"id and class", `div#main.container(title="hi")`, `<div id="main" class="container" title="hi"></div>`},
		{"many plain attrs", `div(a="1" b="2" c="3")`, `<div a="1" b="2" c="3"></div>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile error: %v", err)
			}
			var tags []*TagNode
			walkTagNodes(tpl.ast.Children, func(tag *TagNode) {
				tags = append(tags, tag)
			})
			for _, tag := range tags {
				if !tag.noSpread {
					t.Fatalf("tag %q: noSpread = false, want true (no &attributes present)", tag.Name)
				}
			}

			got, err := tpl.Render(nil)
			if err != nil {
				t.Fatalf("Render error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRenderTagSpreadFinalGateSpreadTagByteIdentical asserts that a tag WITH
// an &attributes spread — the one case where spreadFinal is still built —
// renders byte-for-byte identically to the pre-gate behavior of always
// building spreadFinal, by comparing the compiled (noSpread=false, gate not
// engaged) render against the same template forced through
// forceAttrSortFallback (which also clears noSpread, i.e. the unconditional
// pre-trim path).
func TestRenderTagSpreadFinalGateSpreadTagByteIdentical(t *testing.T) {
	src := `div.base&attributes(extra)`
	data := map[string]any{
		"extra": map[string]any{"data-foo": "bar", "class": "more"},
	}

	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	var tags []*TagNode
	walkTagNodes(tpl.ast.Children, func(tag *TagNode) {
		tags = append(tags, tag)
	})
	for _, tag := range tags {
		if tag.noSpread {
			t.Fatalf("spread tag %q: noSpread = true, want false (has &attributes)", tag.Name)
		}
	}

	got, err := tpl.Render(data)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	tplForced, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	forceAttrSortFallback(tplForced.ast.Children)
	want, err := tplForced.Render(data)
	if err != nil {
		t.Fatalf("Render (forced) error: %v", err)
	}

	if got != want {
		t.Errorf("spread tag render = %q, want %q", got, want)
	}
}

// TestRenderTagSpreadFinalGateMixedTemplateByteIdentical exercises a single
// template mixing no-spread and spread tags, asserting the gated render
// matches the render obtained by forcing every tag through the unconditional
// pre-trim path (noSpread cleared everywhere).
func TestRenderTagSpreadFinalGateMixedTemplateByteIdentical(t *testing.T) {
	src := "div#a.x(title=\"t\")\n  div.base&attributes(extra)\n  span(data-z=\"1\")\n"
	data := map[string]any{
		"extra": map[string]any{"data-foo": "bar", "class": "more"},
	}

	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	got, err := tpl.Render(data)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	tplForced, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	forceAttrSortFallback(tplForced.ast.Children)
	want, err := tplForced.Render(data)
	if err != nil {
		t.Fatalf("Render (forced) error: %v", err)
	}

	if got != want {
		t.Errorf("mixed template render = %q, want %q", got, want)
	}
}
