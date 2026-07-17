package gopug

import (
	"sync"
	"testing"
)

// mixinArgTypeContainer is a struct with a slice field, used to exercise
// passing a struct field (rather than a top-level variable) as a mixin
// argument that the mixin body then iterates with each.
type mixinArgTypeContainer struct {
	Items []string
}

// A mixin argument that resolves to a slice, array, map, or struct must
// reach the mixin body with its real Go type intact, exactly as reference
// Pug's JS semantics behave: passing an array/object into a function and
// iterating it inside works per-element, not as one stringified blob. Before
// the fix, mixin arguments were eagerly stringified at the call, so `each`
// inside the mixin body iterated over a single fmt-formatted string instead
// of the original collection.

func TestMixinArgArrayLiteralIteratesPerElement(t *testing.T) {
	src := "mixin list(items)\n  ul\n    each item in items\n      li= item\n+list(['a', 'b', 'c'])"
	out := renderTest(t, src, nil)
	assertEqual(t, out, "<ul><li>a</li><li>b</li><li>c</li></ul>")
}

func TestMixinArgVariableSlicePreservesTypeForEach(t *testing.T) {
	src := "mixin list(items)\n  ul\n    each item in items\n      li= item\n- var xs = Items\n+list(xs)"
	out := renderTest(t, src, map[string]interface{}{
		"Items": []string{"a", "b", "c"},
	})
	assertEqual(t, out, "<ul><li>a</li><li>b</li><li>c</li></ul>")
}

func TestMixinArgStructFieldSlicePreservesTypeForEach(t *testing.T) {
	src := "mixin list(items)\n  ul\n    each item in items\n      li= item\n+list(Data.Items)"
	out := renderTest(t, src, map[string]interface{}{
		"Data": mixinArgTypeContainer{Items: []string{"a", "b", "c"}},
	})
	assertEqual(t, out, "<ul><li>a</li><li>b</li><li>c</li></ul>")
}

func TestMixinArgMapPreservesTypeForEachKeyValue(t *testing.T) {
	src := "mixin summary(dict)\n  ul\n    each val, key in dict\n      li= key + \": \" + val\n+summary(Info)"
	out := renderTest(t, src, map[string]interface{}{
		"Info": map[string]interface{}{"foo": "bar"},
	})
	assertEqual(t, out, "<ul><li>foo: bar</li></ul>")
}

// TestMixinArgStringAndNumberRegression pins the pre-existing, correct
// behavior for scalar arguments so the type-preservation fix does not
// disturb it: a string argument must render unchanged, and a numeric
// argument must stringify identically to how it always has (no trailing
// zero, no scientific notation for an ordinary value).
func TestMixinArgStringAndNumberRegression(t *testing.T) {
	src := "mixin card(label, price)\n  span= label\n  span= price\n+card(\"Widget\", Product.Price)"
	out := renderTest(t, src, map[string]interface{}{
		"Product": struct{ Price float64 }{Price: 9.99},
	})
	assertContains(t, out, "<span>Widget</span>")
	assertContains(t, out, "<span>9.99</span>")

	intSrc := "mixin card(price)\n  span= price\n+card(Product.Price)"
	intOut := renderTest(t, intSrc, map[string]interface{}{
		"Product": struct{ Price int }{Price: 42},
	})
	assertContains(t, intOut, "<span>42</span>")

	litSrc := "mixin card(price)\n  span= price\n+card(9.99)"
	litOut := renderTest(t, litSrc, nil)
	assertContains(t, litOut, "<span>9.99</span>")
}

// TestMixinArgDotPathMatchesEvaluateExprRaw proves renderMixinCall resolves
// a positional argument through the same type-preserving entry point,
// evaluateExprRaw, that an each loop's own collection expression uses — by
// rendering a mixin call whose argument is a dot-path to a struct field and
// comparing the mixin body's rendered value against r.evaluateExprRaw(arg)
// evaluated directly against an equivalent runtime. If renderMixinCall
// routed arguments through some other path this would drift even though
// evaluateExprRaw itself is unchanged.
func TestMixinArgDotPathMatchesEvaluateExprRaw(t *testing.T) {
	data := map[string]interface{}{
		"person": Person{Name: "Alice", Address: Address{City: "Wonderland"}},
	}

	out := renderTest(t, "mixin card(p)\n  div= p\n+card(person.Address.City)", data)

	r := newExprTestRuntime(data)
	want := r.evaluateExprRaw("person.Address.City")

	assertContains(t, out, "<div>"+want.(string)+"</div>")
}

// TestMixinArgUnsupportedShapesStillRenderCorrectly confirms mixin-call
// argument expressions with operators or bare numeric/negated literals —
// shapes evaluateExprRaw does not special-case, so it falls back to the
// string interpreter internally — still render correctly, including inside
// the rest-parameter loop.
func TestMixinArgUnsupportedShapesStillRenderCorrectly(t *testing.T) {
	out := renderTest(t, "mixin sum(v)\n  p= v\n+sum(!flag)", map[string]interface{}{"flag": false})
	assertContains(t, out, "<p>true</p>")

	out2 := renderTest(t, "mixin sum(v)\n  p= v\n+sum(42)", map[string]interface{}{})
	assertContains(t, out2, "<p>42</p>")

	out3 := renderTest(t, "mixin list(first, ...rest)\n  p= first\n  each r in rest\n    span= r\n+list(a, b, c)", map[string]interface{}{
		"a": "one",
		"b": "two",
		"c": "three",
	})
	assertContains(t, out3, "<p>one</p>")
	assertContains(t, out3, "<span>two</span>")
	assertContains(t, out3, "<span>three</span>")

	out4 := renderTest(t, "mixin list(first, ...rest)\n  p= first\n  each r in rest\n    span= r\n+list(a, !flag, 42)", map[string]interface{}{
		"a":    "one",
		"flag": false,
	})
	assertContains(t, out4, "<p>one</p>")
	assertContains(t, out4, "<span>true</span>")
	assertContains(t, out4, "<span>42</span>")
}

// TestMixinArgRestParamPreservesTypeForSlice confirms a rest parameter
// (`...rest`) also carries a typed value through unchanged, not just
// positional parameters: a rest argument that is itself a slice must reach
// the mixin body as that real slice — proven here via `.length`, which only
// reports the element count for an actual slice, not the byte length of a
// stringified `"[a b]"` blob.
func TestMixinArgRestParamPreservesTypeForSlice(t *testing.T) {
	src := "mixin wrap(label, ...rest)\n  p= label\n  each r in rest\n    p= r.length\n+wrap(\"L\", Items)"
	out := renderTest(t, src, map[string]interface{}{
		"Items": []string{"a", "b"},
	})
	assertEqual(t, out, "<p>L</p><p>2</p>")
}

// TestMixinArgConcurrentRenderSafety proves that concurrent renders of the
// same compiled Template, each passing a slice-valued mixin argument, don't
// race — every render evaluates its own argument freshly against its own
// Runtime/scope, so there is no shared mutable state between renders (unlike
// the earlier compiledArgs mechanism, there is now no compile-time-populated
// field on MixinCallNode to reason about at all).
func TestMixinArgConcurrentRenderSafety(t *testing.T) {
	src := "mixin list(items)\n  ul\n    each item in items\n      li= item\n+list(Items)"
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	data := map[string]any{
		"Items": []string{"a", "b", "c"},
	}

	const goroutines = 16
	results := make([]string, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = tpl.Render(data)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: Render error: %v", i, err)
		}
	}
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d rendered %q, want %q (same as goroutine 0)", i, results[i], results[0])
		}
	}
	assertEqual(t, results[0], "<ul><li>a</li><li>b</li><li>c</li></ul>")
}
