package gopug

import "testing"

// TestArrayIndexOfElementSemantics proves array `.indexOf` uses JS
// array-element equality, not a substring search over the stringified
// array. A page-nav guard like `["users","firms","firm-applications"].indexOf(cp) !== -1`
// must only match on an exact element, never on a substring of the
// comma-joined array text.
func TestArrayIndexOfElementSemantics(t *testing.T) {
	src := `if pages.indexOf(cp) !== -1
  p yes
else
  p no`

	cases := []struct {
		cp   string
		want string
	}{
		{"users", "<p>yes</p>"},
		{"firms", "<p>yes</p>"},
		{"firm-applications", "<p>yes</p>"},
		{"dashboard", "<p>no</p>"},
		{"firm", "<p>no</p>"},
		{"user", "<p>no</p>"},
		{"app", "<p>no</p>"},
	}

	for _, tc := range cases {
		out := renderTest(t, src, map[string]interface{}{
			"pages": []string{"users", "firms", "firm-applications"},
			"cp":    tc.cp,
		})
		assertEqual(t, out, tc.want)
	}
}

// TestArrayIndexOfRawElementIndex proves the raw numeric result of an array
// `.indexOf` is the 0-based element index, not a byte offset into the
// stringified array.
func TestArrayIndexOfRawElementIndex(t *testing.T) {
	out := renderTest(t, "p= letters.indexOf(\"b\")", map[string]interface{}{
		"letters": []string{"a", "b", "c"},
	})
	assertEqual(t, out, "<p>1</p>")
}

// TestArrayIndexOfPartialSubstringIsAbsent proves that a value which is only
// a substring of one array element (not an element itself) is reported as
// absent, matching pug.js's array semantics.
func TestArrayIndexOfPartialSubstringIsAbsent(t *testing.T) {
	out := renderTest(t, "p= letters.indexOf(\"firm\")", map[string]interface{}{
		"letters": []string{"firms"},
	})
	assertEqual(t, out, "<p>-1</p>")
}

// TestArrayIncludesElementSemantics proves array `.includes` checks element
// membership, not substring containment on the stringified array.
func TestArrayIncludesElementSemantics(t *testing.T) {
	out := renderTest(t, "p= letters.includes(\"b\")", map[string]interface{}{
		"letters": []string{"a", "b"},
	})
	assertEqual(t, out, "<p>true</p>")

	out = renderTest(t, "p= letters.includes(\"x\")", map[string]interface{}{
		"letters": []string{"a", "b"},
	})
	assertEqual(t, out, "<p>false</p>")
}

// TestArrayIncludesPartialSubstringIsFalse proves that a value which is only
// a substring of one array element reports false membership.
func TestArrayIncludesPartialSubstringIsFalse(t *testing.T) {
	out := renderTest(t, "p= letters.includes(\"firm\")", map[string]interface{}{
		"letters": []string{"firms"},
	})
	assertEqual(t, out, "<p>false</p>")
}

// TestStringIndexOfIncludesUnchanged proves the string-receiver forms of
// `.indexOf`/`.includes` remain substring search — this is correct JS
// behavior for strings and must not regress when the array branch is added.
func TestStringIndexOfIncludesUnchanged(t *testing.T) {
	out := renderTest(t, "p= greeting.indexOf(\"ell\")", map[string]interface{}{"greeting": "hello"})
	assertEqual(t, out, "<p>1</p>")

	out = renderTest(t, "p= greeting.includes(\"ell\")", map[string]interface{}{"greeting": "hello"})
	assertEqual(t, out, "<p>true</p>")
}

// TestArrayIndexOfManageNavCorpusShape proves against a realistic
// manage-section nav-list shape (page slugs like the ones a sidebar
// highlights) with a substring-collision value, agreeing with pug.js.
func TestArrayIndexOfManageNavCorpusShape(t *testing.T) {
	src := `if navPages.indexOf(currentPage) !== -1
  p yes
else
  p no`

	navPages := []string{"manage", "manage-users", "manage-firms"}

	out := renderTest(t, src, map[string]interface{}{"navPages": navPages, "currentPage": "manage"})
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, src, map[string]interface{}{"navPages": navPages, "currentPage": "manage-users"})
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, src, map[string]interface{}{"navPages": navPages, "currentPage": "manag"})
	assertEqual(t, out, "<p>no</p>")
}

// TestArrayIndexOfOffersListCorpusShape proves against a realistic
// offers/firm-applications-style list shape with a substring-collision
// value, agreeing with pug.js.
func TestArrayIndexOfOffersListCorpusShape(t *testing.T) {
	src := `if listPages.indexOf(currentPage) !== -1
  p yes
else
  p no`

	listPages := []string{"offers", "offer-applications", "dashboard"}

	out := renderTest(t, src, map[string]interface{}{"listPages": listPages, "currentPage": "offers"})
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, src, map[string]interface{}{"listPages": listPages, "currentPage": "offer"})
	assertEqual(t, out, "<p>no</p>")

	out = renderTest(t, src, map[string]interface{}{"listPages": listPages, "currentPage": "applications"})
	assertEqual(t, out, "<p>no</p>")
}
