package gopug

import (
	"fmt"
	"strconv"
	"testing"
)

// stringifyStringer implements fmt.Stringer. Values of this type must keep
// going through the Sprintf fallback in lookupAndStringify so their String()
// method is honored, never a builtin scalar fast-path.
type stringifyStringer struct {
	label string
}

func (s stringifyStringer) String() string {
	return "Stringer:" + s.label
}

// namedStringType is a defined type whose underlying kind is string. A type
// switch `case string:` does not match values of a named type, only the
// unnamed builtin string type itself, so values of namedStringType must fall
// through to the Sprintf fallback rather than being matched by the fast
// path.
type namedStringType string

// stringifyTestStruct is a plain struct with no String() method, used to
// prove struct values still fall through to the Sprintf path unchanged.
type stringifyTestStruct struct {
	A int
	B string
}

// TestLookupAndStringifyMatchesSprintf proves every fast-path case added to
// lookupAndStringify produces byte-identical output to what
// fmt.Sprintf("%v", val) would produce for that value (with the pre-existing
// float64 carve-out via strconv.FormatFloat), and that types which are not
// exactly one of the fast-pathed builtin kinds continue to flow through the
// Sprintf fallback.
func TestLookupAndStringifyMatchesSprintf(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"empty string", ""},
		{"string", "abc"},
		{"true", true},
		{"false", false},
		{"zero int", int(0)},
		{"negative int", int(-7)},
		{"int8", int8(-8)},
		{"int8 min", int8(-128)},
		{"int16", int16(1234)},
		{"int16 negative", int16(-1234)},
		{"int32", int32(-123456)},
		{"int64 max", int64(9223372036854775807)},
		{"int64 min", int64(-9223372036854775808)},
		{"uint", uint(42)},
		{"uint8", uint8(255)},
		{"uint16", uint16(65535)},
		{"uint32", uint32(4294967295)},
		{"uint64 max", uint64(18446744073709551615)},
		{"float64 simple", float64(3.14)},
		{"float64 exponent", float64(1e20)},
		{"float64 negative", float64(-0.5)},
		{"float64 zero", float64(0)},
		{"map", map[string]int{"a": 1, "b": 2, "c": 3}},
		{"slice", []int{1, 2, 3}},
		{"struct", stringifyTestStruct{A: 1, B: "x"}},
		{"stringer", stringifyStringer{label: "hi"}},
		{"named string type", namedStringType("abc")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newExprTestRuntime(map[string]any{"v": tc.val})
			got := r.lookupAndStringify("v")

			var want string
			if f, ok := tc.val.(float64); ok {
				want = strconv.FormatFloat(f, 'f', -1, 64)
			} else {
				want = fmt.Sprintf("%v", tc.val)
			}

			if got != want {
				t.Errorf("lookupAndStringify(%#v) = %q, want %q (fmt.Sprintf semantics)", tc.val, got, want)
			}
		})
	}
}

// TestLookupAndStringifyStringerNotShortCircuited is a focused regression
// check: a value whose type implements fmt.Stringer must still invoke
// String() through the Sprintf fallback, proving the fast-path switch keys
// off the value's exact static type rather than treating anything
// string-shaped as a builtin string.
func TestLookupAndStringifyStringerNotShortCircuited(t *testing.T) {
	r := newExprTestRuntime(map[string]any{"v": stringifyStringer{label: "custom"}})
	got := r.lookupAndStringify("v")
	want := "Stringer:custom"
	if got != want {
		t.Errorf("lookupAndStringify(Stringer) = %q, want %q (String() must be honored)", got, want)
	}
}

// TestLookupAndStringifyErrorNotShortCircuited proves the same for the error
// interface: an error value must still render via its Error() method through
// the Sprintf fallback.
func TestLookupAndStringifyErrorNotShortCircuited(t *testing.T) {
	r := newExprTestRuntime(map[string]any{"v": fmt.Errorf("boom")})
	got := r.lookupAndStringify("v")
	want := "boom"
	if got != want {
		t.Errorf("lookupAndStringify(error) = %q, want %q (Error() must be honored)", got, want)
	}
}

// TestRenderLargeTemplateByteIdenticalScalars renders the benchmark's large
// template and asserts the output equals the exact byte sequence produced
// before the lookupAndStringify fast-path was added, guarding against any
// accidental output change from the new scalar cases.
func TestRenderLargeTemplateByteIdenticalScalars(t *testing.T) {
	tpl, err := Compile(largeSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	out, err := tpl.Render(largeData())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	const want = `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>Benchmark Page</title></head><body><header><nav><a href="/">Home</a><a href="/about">About</a><a href="/contact">Contact</a></nav></header><main><h1>Welcome</h1><p>This is the intro paragraph.</p><ul class="items"><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li><li class="item"><span class="Product">Product</span><span class="$9.99">$9.99</span></li></ul><p class="Prices subject to change.">Prices subject to change.</p></main><footer><p>&copy; 2025 Go-Pug</p></footer></body></html>`

	if out != want {
		t.Errorf("large template output changed by lookupAndStringify fast-path\ngot:  %s\nwant: %s", out, want)
	}
}

// TestRenderScalarIdentifiersByteIdentical renders string/int/bool/float
// identifiers (the shapes the new fast-path targets) and asserts the output
// matches what the pre-fix Sprintf-only implementation produced.
func TestRenderScalarIdentifiersByteIdentical(t *testing.T) {
	src := "p= name\np= count\np= flag\np= price\np= big\np= tiny"
	data := map[string]any{
		"name":  "World",
		"count": 42,
		"flag":  true,
		"price": 9.99,
		"big":   1e20,
		"tiny":  int8(-8),
	}
	out, err := Render(src, data, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const want = `<p>World</p><p>42</p><p>true</p><p>9.99</p><p>100000000000000000000</p><p>-8</p>`
	if out != want {
		t.Errorf("scalar identifier output changed by lookupAndStringify fast-path\ngot:  %s\nwant: %s", out, want)
	}
}
