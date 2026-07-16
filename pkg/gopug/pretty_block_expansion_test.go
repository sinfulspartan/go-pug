package gopug

import "testing"

// TestBlockExpansionRepeatedRenderIsIdempotent (gopug_test.go) already guards
// the compact-mode path against a block-expansion node leaking its child
// into its parent's own Children permanently across repeated renders of the
// same compiled *Template. The tests below add the same guard for PRETTY
// mode, which renders each expanded child through renderTagWithChildren's
// closing-tag layout decision — the exact code path a prior pretty-only
// regression hid in, since only a single render (not a repeated one) was
// exercised for pretty mode at the time.

// TestPrettyBlockExpansionRepeatedRenderIsIdempotentInlineChild renders a
// compiled template using inline-named block expansion (`li: a(...) text`)
// three times in pretty mode and asserts every render is byte-identical —
// the inline-named expanded child gets no trailing newline on any render.
func TestPrettyBlockExpansionRepeatedRenderIsIdempotentInlineChild(t *testing.T) {
	tpl, err := Compile(`li: a(href="#") text`, &Options{Pretty: true})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	first, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("first Render: %v", err)
	}
	want := "\n<li><a href=\"#\">text</a></li>"
	assertEqual(t, first, want)

	second, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	if second != first {
		t.Errorf("second render diverged from the first:\nfirst:  %q\nsecond: %q", first, second)
	}

	third, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("third Render: %v", err)
	}
	if third != first {
		t.Errorf("third render diverged from the first:\nfirst: %q\nthird: %q", first, third)
	}
}

// TestPrettyBlockExpansionRepeatedRenderIsIdempotentBlockChild renders a
// compiled template using block-named (non-inline) block expansion
// (`li: div.block Text`) three times in pretty mode and asserts every render
// is byte-identical — this is the sharper case, since a block-named expanded
// child forces the parent's own trailing-newline/indented-block layout
// decision, which is exactly where the prior pretty-only regression hid.
func TestPrettyBlockExpansionRepeatedRenderIsIdempotentBlockChild(t *testing.T) {
	tpl, err := Compile(`li: div.block Text`, &Options{Pretty: true})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	first, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("first Render: %v", err)
	}
	want := "\n<li>\n  <div class=\"block\">Text</div>\n</li>"
	assertEqual(t, first, want)

	second, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	if second != first {
		t.Errorf("second render diverged from the first:\nfirst:  %q\nsecond: %q", first, second)
	}

	third, err := tpl.Render(nil)
	if err != nil {
		t.Fatalf("third Render: %v", err)
	}
	if third != first {
		t.Errorf("third render diverged from the first:\nfirst: %q\nthird: %q", first, third)
	}
}
