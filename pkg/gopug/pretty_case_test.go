package gopug

import "testing"

// These tests close the pretty-mode blind spot for `case`/`when`/`default`:
// the family was exercised almost entirely in compact mode before this file
// existed. Every want string below was captured by rendering the equivalent
// template through real pug.js 3.0.4 (perf-compare/node_modules/pug) with
// {pretty: true}.

func prettyCaseSrc() string {
	return "case type\n" +
		"  when 'a'\n" +
		"    div\n" +
		"      p A branch\n" +
		"  when 'b'\n" +
		"    div\n" +
		"      p B branch\n" +
		"  default\n" +
		"    div\n" +
		"      p Default branch\n"
}

// TestPrettyCaseWhenMatchedBranchBlockLevel pins pug.js 3.0.4's exact
// pretty-mode bytes for a `case` whose matched `when` branch emits a
// block-level tag.
func TestPrettyCaseWhenMatchedBranchBlockLevel(t *testing.T) {
	out, err := Render(prettyCaseSrc(), map[string]any{"type": "b"}, &Options{Pretty: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertEqual(t, out, "\n<div>\n  <p>B branch</p>\n</div>")
}

// TestPrettyCaseDefaultBranchBlockLevel pins pug.js 3.0.4's exact
// pretty-mode bytes for a `case` that falls through to its `default` branch,
// which emits a block-level tag.
func TestPrettyCaseDefaultBranchBlockLevel(t *testing.T) {
	out, err := Render(prettyCaseSrc(), map[string]any{"type": "unmatched"}, &Options{Pretty: true})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	assertEqual(t, out, "\n<div>\n  <p>Default branch</p>\n</div>")
}
