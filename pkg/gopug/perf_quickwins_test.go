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

// TestPrettyInlineRefactorPreservesOutput verifies the hoisted-map refactor
// of prettyInline does not change pretty-mode output for templates
// containing inline tags and single-text-child tags.
func TestPrettyInlineRefactorPreservesOutput(t *testing.T) {
	src := "div\n  a(href=\"#\") Link\n  span Inline text\n  p Single text child\n  ul\n    li One\n    li Two"
	opts := &Options{Pretty: true}
	out, err := Render(src, nil, opts)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}

	want := "\n<div><a href=\"#\">Link</a><span>Inline text</span><p>Single text child</p>\n  <ul><li>One</li><li>Two</li>\n  </ul>\n</div>"
	if out != want {
		t.Fatalf("pretty render mismatch:\ngot:  %q\nwant: %q", out, want)
	}
}

// TestCompactInlineGatePreservesOutput verifies that gating the
// prettyInline call behind r.pretty() does not change compact-mode output
// for the same template.
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
