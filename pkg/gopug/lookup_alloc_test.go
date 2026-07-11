package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// referenceLookup mirrors the pre-optimization lookup implementation exactly
// (strings.Split on ".", TrimSpace on every part) so it can be used as an
// oracle to prove the allocation-free rewrite produces identical results.
func referenceLookup(r *Runtime, key string) (any, bool) {
	parts := strings.Split(key, ".")
	root := strings.TrimSpace(parts[0])

	var rootVal any
	found := false
	for i := len(r.scopeStack) - 1; i >= 0; i-- {
		frame := r.scopeStack[i]
		if frame == nil {
			continue
		}
		if _, isBoundary := frame["\x00mixin_boundary"]; isBoundary {
			break
		}
		if val, ok := frame[root]; ok {
			rootVal = val
			found = true
			break
		}
	}

	if !found {
		if val, ok := r.globals[root]; ok {
			rootVal = val
			found = true
		}
	}

	if !found {
		return nil, false
	}

	current := rootVal
	for j := 1; j < len(parts); j++ {
		part := strings.TrimSpace(parts[j])
		current = r.getField(current, part)
		if current == nil {
			return nil, false
		}
	}

	return current, true
}

// newLookupTestRuntime builds a bare Runtime with a single scope frame set to
// data, plus the given globals, suitable for exercising lookup directly.
func newLookupTestRuntime(data map[string]any, globals map[string]any) *Runtime {
	if globals == nil {
		globals = make(map[string]any)
	}
	return &Runtime{
		data:       data,
		globals:    globals,
		scopeStack: []map[string]any{data},
	}
}

type nestedStructB struct {
	B string
}

type nestedStructA struct {
	A nestedStructB
}

// TestLookupDifferentialAgainstReference exercises the new allocation-free
// lookup against the old strings.Split-based reference implementation across
// bare, dotted, spaced, and degenerate keys, over several data shapes, and
// asserts the results are identical in both value and found-ness.
func TestLookupDifferentialAgainstReference(t *testing.T) {
	keys := []string{
		"name",
		"missing",
		"a.b",
		"a.b.c",
		"a . b",
		" a . b . c ",
		"a..b",
		"a.",
		".a",
		"",
		".",
		"a...",
		"  name  ",
		"a.missing",
		"a.b.missing",
	}

	setups := []struct {
		name string
		data map[string]any
	}{
		{
			name: "flat map",
			data: map[string]any{
				"name": "World",
				"a":    map[string]any{"b": "nested", "": "empty-key-val"},
			},
		},
		{
			name: "nested map chain",
			data: map[string]any{
				"name": "World",
				"a": map[string]any{
					"b": map[string]any{"c": "deep"},
					"":  map[string]any{"b": "via-empty-segment"},
				},
			},
		},
		{
			name: "struct chain",
			data: map[string]any{
				"name": "World",
				"a":    nestedStructA{A: nestedStructB{B: "struct-nested"}},
			},
		},
		{
			name: "empty data",
			data: map[string]any{},
		},
	}

	for _, setup := range setups {
		for _, key := range keys {
			t.Run(setup.name+"/"+key, func(t *testing.T) {
				rt1 := newLookupTestRuntime(setup.data, nil)
				rt2 := newLookupTestRuntime(setup.data, nil)

				gotVal, gotOK := rt1.lookup(key)
				wantVal, wantOK := referenceLookup(rt2, key)

				if gotOK != wantOK {
					t.Fatalf("lookup(%q) ok = %v, want %v", key, gotOK, wantOK)
				}
				if !reflect.DeepEqual(gotVal, wantVal) {
					t.Fatalf("lookup(%q) = %v, want %v", key, gotVal, wantVal)
				}
			})
		}
	}
}

// TestLookupDifferentialWithGlobals confirms the globals fallback path is
// also byte-identical between the two implementations.
func TestLookupDifferentialWithGlobals(t *testing.T) {
	data := map[string]any{"local": "local-val"}
	globals := map[string]any{
		"g":     "global-val",
		"nest":  map[string]any{"x": "global-nested"},
		"local": "shadowed-by-scope",
	}

	keys := []string{"g", "nest.x", "local", "missing", "g.", ".g", "g . x"}

	for _, key := range keys {
		rt1 := newLookupTestRuntime(data, globals)
		rt2 := newLookupTestRuntime(data, globals)

		gotVal, gotOK := rt1.lookup(key)
		wantVal, wantOK := referenceLookup(rt2, key)

		if gotOK != wantOK || !reflect.DeepEqual(gotVal, wantVal) {
			t.Errorf("lookup(%q) = (%v, %v), want (%v, %v)", key, gotVal, gotOK, wantVal, wantOK)
		}
	}
}

// TestLookupMixinBoundaryUnaffected proves the mixin scope boundary
// short-circuit still stops descent at the sentinel frame after the rewrite.
func TestLookupMixinBoundaryUnaffected(t *testing.T) {
	callerFrame := map[string]any{"secret": "leaked"}
	mixinFrame := map[string]any{"x": "mixin-val"}

	r := &Runtime{
		globals:    map[string]any{},
		scopeStack: []map[string]any{callerFrame, mixinScopeBoundary, mixinFrame},
	}

	if _, ok := r.lookup("secret"); ok {
		t.Errorf("lookup(%q) should not see past the mixin boundary", "secret")
	}
	if val, ok := r.lookup("x"); !ok || val != "mixin-val" {
		t.Errorf("lookup(%q) = (%v, %v), want (%v, %v)", "x", val, ok, "mixin-val", true)
	}
}

// TestLookupBareKeyDoesNotAllocate proves the no-dot fast path resolves a
// bare identifier without allocating a slice.
func TestLookupBareKeyDoesNotAllocate(t *testing.T) {
	r := newLookupTestRuntime(map[string]any{"name": "World"}, nil)

	allocs := testing.AllocsPerRun(100, func() {
		if _, ok := r.lookup("name"); !ok {
			t.Fatal("lookup(name) failed")
		}
	})
	if allocs != 0 {
		t.Errorf("lookup(bare key) allocs/run = %v, want 0", allocs)
	}
}

// isPlainIdentifierCases is shared between the identifier classification test
// and documents the inf/nan spellings that must still be rejected.
var isPlainIdentifierCases = []struct {
	expr string
	want bool
}{
	{"name", true},
	{"price", true},
	{"_private", true},
	{"$scope", true},
	{"Inf", false},
	{"inf", false},
	{"Infinity", false},
	{"infinity", false},
	{"INFINITY", false},
	{"NaN", false},
	{"nan", false},
	{"NAN", false},
	{"nax", true},
	{"info", true},
	{"nano", true},
	{"", false},
	{"123abc", false},
	{"a.b", false},
}

// TestIsPlainIdentifier verifies the mayBeFloat-guarded ParseFloat call keeps
// rejecting the inf/nan spellings while accepting ordinary identifiers.
func TestIsPlainIdentifier(t *testing.T) {
	for _, tc := range isPlainIdentifierCases {
		if got := isPlainIdentifier(tc.expr); got != tc.want {
			t.Errorf("isPlainIdentifier(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

// TestRenderByteIdenticalAfterLookupOptimization renders the large benchmark
// template and a nested-dot-path template, and checks the output is exactly
// what it was before the lookup/isPlainIdentifier allocation changes.
func TestRenderByteIdenticalAfterLookupOptimization(t *testing.T) {
	tpl, err := Compile(largeSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	out, err := tpl.Render(largeData())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "Welcome") || !strings.Contains(out, "Product") {
		t.Fatalf("large template render looks wrong: %s", out)
	}

	nestedSrc := "each x in items\n  p= x.a.b"
	nestedTpl, err := Compile(nestedSrc, nil)
	if err != nil {
		t.Fatalf("Compile nested: %v", err)
	}
	data := map[string]any{
		"items": []any{
			map[string]any{"a": map[string]any{"b": "first"}},
			map[string]any{"a": map[string]any{"b": "second"}},
		},
	}
	nestedOut, err := nestedTpl.Render(data)
	if err != nil {
		t.Fatalf("Render nested: %v", err)
	}
	want := "<p>first</p><p>second</p>"
	if nestedOut != want {
		t.Fatalf("nested dot-path render = %q, want %q", nestedOut, want)
	}
}
