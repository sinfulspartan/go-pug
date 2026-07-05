package gopug

import "testing"

// Issue #17: a bare buffered-output line (`= expr`) is silently dropped when it
// immediately follows a void element (br/img/hr/…) at the same indentation.
// Root cause: parseTag adopted same-depth *following-line* content as a child of
// the tag; void elements never render their children, so the node vanished.

func TestIssue17BufferedCodeAfterBr(t *testing.T) {
	src := "div\n  | Hello\n  br\n  = \"world\"\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<div>Hello<br>world</div>")
}

func TestIssue17UnescapedCodeAfterBr(t *testing.T) {
	src := "div\n  br\n  != \"<b>hi</b>\"\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<div><br><b>hi</b></div>")
}

func TestIssue17TextAfterBr(t *testing.T) {
	src := "div\n  br\n  | after\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<div><br>after</div>")
}

func TestIssue17BufferedCodeAfterVoidElements(t *testing.T) {
	for _, void := range []string{"img", "hr", "input"} {
		src := "div\n  " + void + "\n  = \"world\"\n"
		out := renderTest(t, src, nil)
		assertContains(t, out, "world")
	}
}

// A bare `= expr` following a normal (non-void) tag must be a *sibling*, not a
// child of that tag.
func TestIssue17BufferedCodeIsSiblingNotChild(t *testing.T) {
	src := "p\n= \"world\"\np\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<p></p>world<p></p>")
}

// Inline buffered output on the same line as the tag must still be a child.
func TestIssue17InlineBufferedCodeStillChild(t *testing.T) {
	src := "p= \"world\"\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<p>world</p>")
}
