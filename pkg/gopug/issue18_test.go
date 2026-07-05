package gopug

import "testing"

// Issue #18: class= bound to a variable whose value is the empty string leaked
// the variable's *name* as a literal class token. Root cause: the class
// resolution fallback kept an unresolved word literally, unable to tell a
// static class token from a variable that resolved to "".

func TestIssue18EmptyStringVarDoesNotLeakName(t *testing.T) {
	src := "- var cls = \"\"\ndiv(class=cls) hi\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, `<div class="">hi</div>`)
}

func TestIssue18ConditionalVisibilityPattern(t *testing.T) {
	src := "- var c = show ? \"\" : \"d-none\"\ndiv(class=c) x\n"

	shown := renderTest(t, src, map[string]interface{}{"show": true})
	assertEqual(t, shown, `<div class="">x</div>`)

	hidden := renderTest(t, src, map[string]interface{}{"show": false})
	assertEqual(t, hidden, `<div class="d-none">x</div>`)
}

// Non-empty variable values must still resolve normally.
func TestIssue18NonEmptyVarStillWorks(t *testing.T) {
	src := "- var cls = \"active\"\ndiv(class=cls) hi\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, `<div class="active">hi</div>`)
}

// A static quoted class list must keep its literal tokens.
func TestIssue18StaticClassListPreserved(t *testing.T) {
	src := `div(class="foo bar") hi`
	out := renderTest(t, src, nil)
	assertEqual(t, out, `<div class="foo bar">hi</div>`)
}
