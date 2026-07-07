package gopug

import "testing"

// Issue #26: an unbuffered code assignment `- var xs = Items` (a bare
// identifier or dot-path right-hand side) coerced the resolved value to a
// string. A field access resolving to a slice, map, or struct lost its type,
// so `xs.length` and `each … in xs` operated on the fmt-stringified form
// instead of the real Go value. In reference Pug a `-` block is raw JS, so a
// plain alias preserves the referenced value's type — the assignment RHS
// now goes through the type-preserving evaluator in every case, and a
// top-level ternary RHS (or `each` collection) preserves the chosen
// branch's type too.

type issue26Item struct {
	Name string
}

func TestIssue26AliasedSlicePreservesTypeForLengthAndEach(t *testing.T) {
	src := "- var xs = Items\np= xs.length\nul\n  each x in xs\n    li= x.Name\n"
	out := renderTest(t, src, map[string]interface{}{
		"Items": []issue26Item{{Name: "a"}, {Name: "b"}, {Name: "c"}},
	})
	assertEqual(t, out, "<p>3</p><ul><li>a</li><li>b</li><li>c</li></ul>")
}

func TestIssue26AliasedMapPreservesType(t *testing.T) {
	src := "- var m = SomeMap\np= m.foo\n"
	out := renderTest(t, src, map[string]interface{}{
		"SomeMap": map[string]interface{}{"foo": "bar"},
	})
	assertEqual(t, out, "<p>bar</p>")
}

func TestIssue26AliasedStructPreservesType(t *testing.T) {
	src := "- var s = SomeStruct\np= s.Name\n"
	out := renderTest(t, src, map[string]interface{}{
		"SomeStruct": issue26Item{Name: "Widget"},
	})
	assertEqual(t, out, "<p>Widget</p>")
}

func TestIssue26TernaryAssignmentPreservesArrayType(t *testing.T) {
	src := "- var xs = cond ? Items : []\np= xs.length\n"

	truthy := renderTest(t, src, map[string]interface{}{
		"cond":  true,
		"Items": []interface{}{"a", "b"},
	})
	assertEqual(t, truthy, "<p>2</p>")

	falsy := renderTest(t, src, map[string]interface{}{
		"cond":  false,
		"Items": []interface{}{"a", "b"},
	})
	assertEqual(t, falsy, "<p>0</p>")
}

func TestIssue26EachOverTernaryPreservesType(t *testing.T) {
	src := "ul\n  each x in cond ? A : B\n    li= x\n"

	fromA := renderTest(t, src, map[string]interface{}{
		"cond": true,
		"A":    []interface{}{"a1", "a2"},
		"B":    []interface{}{"b1"},
	})
	assertEqual(t, fromA, "<ul><li>a1</li><li>a2</li></ul>")

	fromB := renderTest(t, src, map[string]interface{}{
		"cond": false,
		"A":    []interface{}{"a1", "a2"},
		"B":    []interface{}{"b1"},
	})
	assertEqual(t, fromB, "<ul><li>b1</li></ul>")
}

func TestIssue26ScalarAssignmentUnaffected(t *testing.T) {
	src := "- var s = \"hi\"\n- var n = 3\np= s\np= n\n"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<p>hi</p><p>3</p>")
}

func TestIssue26StringConcatAssignmentUnaffected(t *testing.T) {
	src := "- var s = \"hello \" + name\np= s\n"
	out := renderTest(t, src, map[string]interface{}{"name": "world"})
	assertEqual(t, out, "<p>hello world</p>")
}
