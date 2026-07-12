package gopug

import "testing"

// TestJoinClasses asserts JoinClasses drops empty class tokens and joins the
// rest with a single space, matching Runtime.renderTag's dynamic class-merge
// rule: a token that resolves to the empty string must not leave a leaked
// identifier or a stray space in the joined result.
func TestJoinClasses(t *testing.T) {
	cases := []struct {
		name    string
		classes []string
		want    string
	}{
		{name: "no args", classes: nil, want: ""},
		{name: "all empty", classes: []string{"", ""}, want: ""},
		{name: "single non-empty", classes: []string{"card"}, want: "card"},
		{
			name:    "empty dynamic token dropped, no trailing space",
			classes: []string{"card", ""},
			want:    "card",
		},
		{
			name:    "empty dynamic token dropped, no leading space",
			classes: []string{"", "card"},
			want:    "card",
		},
		{
			name:    "empty dynamic token dropped in the middle",
			classes: []string{"btn", "", "large"},
			want:    "btn large",
		},
		{
			name:    "multiple non-empty tokens joined with a single space",
			classes: []string{"btn", "large", "primary"},
			want:    "btn large primary",
		},
		{
			name:    "an internal space in a token is kept as-is, not re-split",
			classes: []string{"x y"},
			want:    "x y",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := JoinClasses(tc.classes...)
			if got != tc.want {
				t.Errorf("JoinClasses(%q) = %q, want %q", tc.classes, got, tc.want)
			}
		})
	}
}
