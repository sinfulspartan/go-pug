package gopug

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestToFloatMatchesUnguardedParseFloat proves that toFloat's early-exit
// guard (mayBeFloat) never changes the result: for every case in the table,
// toFloat's (float64, bool) must equal what a direct, unguarded
// strconv.ParseFloat(strings.TrimSpace(x), 64) call would have returned.
func TestToFloatMatchesUnguardedParseFloat(t *testing.T) {
	cases := []string{
		"12", "12.5", ".5", "+.5", "-3", "1e5", "1E-5", " 12 ",
		"inf", "+Inf", "-inf", "nan", "NaN", "0x1p4",
		"", "abc", "$9.99", "Product", "12abc",
	}

	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			gotF, gotOK := toFloat(s)

			wantF, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			wantOK := err == nil

			if gotOK != wantOK {
				t.Fatalf("toFloat(%q) ok = %v, want %v", s, gotOK, wantOK)
			}
			if !floatEqualNaNAware(gotF, wantF) {
				t.Fatalf("toFloat(%q) = %v, want %v", s, gotF, wantF)
			}
		})
	}
}

func floatEqualNaNAware(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return a == b
}

// TestPrettyInlineRefactorPreservesOutput pins pug.js 3.0.4's own pretty
// output for a template mixing inline tags, a block tag with a single text
// child, and a nested list: pug.render(src, {pretty:true}) ===
// "\n<div><a href=\"#\">Link</a><span>Inline text</span>\n  <p>Single text
// child</p>\n  <ul>\n    <li>One</li>\n    <li>Two</li>\n  </ul>\n</div>" —
// p and li are block-named (each gets its own leading newline) even though
// each has only a single text child, and the div's own content cannot
// inline because it contains block-named children, so it also gets a
// trailing newline.
func TestPrettyInlineRefactorPreservesOutput(t *testing.T) {
	src := "div\n  a(href=\"#\") Link\n  span Inline text\n  p Single text child\n  ul\n    li One\n    li Two"
	opts := &Options{Pretty: true}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	want := "\n<div><a href=\"#\">Link</a><span>Inline text</span>\n  <p>Single text child</p>\n  <ul>\n    <li>One</li>\n    <li>Two</li>\n  </ul>\n</div>"
	if out != want {
		t.Fatalf("pretty render mismatch:\ngot:  %q\nwant: %q", out, want)
	}
}

// TestCompactInlineGatePreservesOutput verifies that gating the pretty-mode
// tag-inline classification behind r.pretty() does not change compact-mode
// output for the same template.
func TestCompactInlineGatePreservesOutput(t *testing.T) {
	src := "div\n  a(href=\"#\") Link\n  span Inline text\n  p Single text child\n  ul\n    li One\n    li Two"
	out, err := Render(src, nil, nil)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	want := `<div><a href="#">Link</a><span>Inline text</span><p>Single text child</p><ul><li>One</li><li>Two</li></ul></div>`
	if out != want {
		t.Fatalf("compact render mismatch:\ngot:  %q\nwant: %q", out, want)
	}
}
