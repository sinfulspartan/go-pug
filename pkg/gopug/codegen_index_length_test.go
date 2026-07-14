package gopug

import (
	"strings"
	"testing"
)

// This file proves the two TOTAL, type-directed value-context accessors
// that close out structured-data access: index expressions (`arr[i]`) and
// `.length`. Both are built on resolveFieldExpr's reflect.Type, and both
// collapse every absent/out-of-range/wrong-type case to "" the same way
// Runtime.evaluateExpr's own index/`.length` branches do — see
// genIndexValueExpr and genLengthValueExpr in codegen.go. Every differential
// case reuses the codegenArithCase/runCodegenArithDifferential machinery
// codegen_arith_test.go established.

// TestCodegenIndexSlice proves `arr[i]` on a slice field: a present element
// (including a non-scalar struct element, which stringifies via bare
// fmt.Sprintf("%v", …) — the interpreter's own index-path stringify, NOT the
// scalar-restricted genScalarStringify — and a float64 element, which
// therefore differs from strconv.FormatFloat on a value whose shortest %v
// form uses scientific notation), an out-of-range index, a negative index,
// and a non-numeric key all match the interpreter exactly.
func TestCodegenIndexSlice(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "present element via interpolation",
			src:         "p #{Items[0]}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "present element via buffered code",
			src:         "p= Nums[2]\n",
			data:        map[string]any{"Nums": []int{10, 20, 30}},
			dataLiteral: `opsData{Nums: []int{10, 20, 30}}`,
		},
		{
			name:        "out of range",
			src:         "p #{Items[99]}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "negative index",
			src:         "p #{Items[-1]}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "non-numeric key clamps to empty",
			src:         "p #{Items['x']}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "nil slice",
			src:         "p #{Items[0]}\n",
			data:        map[string]any{"Items": []string(nil)},
			dataLiteral: `opsData{Items: nil}`,
		},
		{
			name:        "empty slice",
			src:         "p #{Items[0]}\n",
			data:        map[string]any{"Items": []string{}},
			dataLiteral: `opsData{Items: []string{}}`,
		},
		{
			name:        "struct element renders via %v struct form",
			src:         "p #{Firms[0]}\n",
			data:        map[string]any{"Firms": []opsFirm{{ID: 7}, {ID: 42}}},
			dataLiteral: `opsData{Firms: []opsFirm{{ID: 7}, {ID: 42}}}`,
		},
		{
			name:        "float element uses %v, not FormatFloat",
			src:         "p #{Prices[0]}\n",
			data:        map[string]any{"Prices": []float64{1000000.0, 2.5}},
			dataLiteral: `opsData{Prices: []float64{1000000.0, 2.5}}`,
		},
		{
			name:        "float element in the common (non-scientific) case still agrees",
			src:         "p #{Prices[1]}\n",
			data:        map[string]any{"Prices": []float64{1000000.0, 2.5}},
			dataLiteral: `opsData{Prices: []float64{1000000.0, 2.5}}`,
		},
		{
			name:        "dynamic attribute value",
			src:         "a(data-x=Items[0])\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenIndexSliceFloatElementProvesPercentVFormatting pins down, as an
// explicit assertion (not just a differential match), that the generated
// code's output for a scientific-notation float element is the %v spelling
// ("1e+06"), not FormatFloat's ("1000000") — proving genIndexValueExpr
// really does use fmt.Sprintf("%%v", …) and not genScalarStringify's
// FormatFloat path.
func TestCodegenIndexSliceFloatElementProvesPercentVFormatting(t *testing.T) {
	t.Parallel()
	got := runCodegenMethodOutput(t, "p #{Prices[0]}\n", `opsData{Prices: []float64{1000000.0}}`)
	if !strings.Contains(got, "1e+06") {
		t.Errorf("generated output %q does not contain the %%v scientific-notation form %q", got, "1e+06")
	}
	if strings.Contains(got, "1000000<") {
		t.Errorf("generated output %q looks like FormatFloat's non-scientific form, not %%v", got)
	}
}

// TestCodegenIndexMap proves `arr[key]` on a map[string]V field: a present
// key, an absent key, a nil map, and an int-valued map all match the
// interpreter exactly.
func TestCodegenIndexMap(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "present key",
			src:         `p #{Meta["title"]}` + "\n",
			data:        map[string]any{"Meta": map[string]string{"title": "Hello"}},
			dataLiteral: `opsData{Meta: map[string]string{"title": "Hello"}}`,
		},
		{
			name:        "absent key",
			src:         `p #{Meta["missing"]}` + "\n",
			data:        map[string]any{"Meta": map[string]string{"title": "Hello"}},
			dataLiteral: `opsData{Meta: map[string]string{"title": "Hello"}}`,
		},
		{
			name:        "nil map",
			src:         `p #{Meta["title"]}` + "\n",
			data:        map[string]any{"Meta": map[string]string(nil)},
			dataLiteral: `opsData{Meta: nil}`,
		},
		{
			name:        "int-valued map via buffered code",
			src:         `p= Counts["x"]` + "\n",
			data:        map[string]any{"Counts": map[string]int{"x": 5}},
			dataLiteral: `opsData{Counts: map[string]int{"x": 5}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenIndexKeyFromExpression proves the key may itself be a resolved
// field (`Items[Idx]`) or an arithmetic expression (`Items[Count - 1]`), not
// only a literal.
func TestCodegenIndexKeyFromExpression(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "key from an int field",
			src:         "p #{Items[Idx]}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}, "Idx": 1},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}, Idx: 1}`,
		},
		{
			name:        "key from an arithmetic expression",
			src:         "p #{Items[Count - 1]}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}, "Count": 3},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}, Count: 3}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLengthValueContext proves value-context `.length` for a slice,
// a map, and a string field — the string case using a multibyte value to
// prove a RUNE count (utf8.RuneCountInString), not a byte count.
func TestCodegenLengthValueContext(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "slice length",
			src:         "p #{Items.length}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "map length",
			src:         "p #{Meta.length}\n",
			data:        map[string]any{"Meta": map[string]string{"a": "1", "b": "2"}},
			dataLiteral: `opsData{Meta: map[string]string{"a": "1", "b": "2"}}`,
		},
		{
			name:        "string length ascii",
			src:         "p #{Name.length}\n",
			data:        map[string]any{"Name": "hello"},
			dataLiteral: `opsData{Name: "hello"}`,
		},
		{
			name:        "string length multibyte (rune count, not byte count)",
			src:         "p #{Name.length}\n",
			data:        map[string]any{"Name": "日本語ABC"},
			dataLiteral: `opsData{Name: "日本語ABC"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLengthMultibyteIsSixNotTwelve pins down the multibyte `.length`
// case as an explicit rune-count assertion: "日本語ABC" is 6 runes (3
// three-byte kanji + 3 ASCII letters) but 12 bytes, so a byte-counting bug
// would silently produce "12" instead of "6".
func TestCodegenLengthMultibyteIsSixNotTwelve(t *testing.T) {
	t.Parallel()
	got := runCodegenMethodOutput(t, "p #{Name.length}\n", `opsData{Name: "日本語ABC"}`)
	if !strings.Contains(got, "6") {
		t.Errorf("generated output %q does not contain the rune count 6", got)
	}
	if strings.Contains(got, "12") {
		t.Errorf("generated output %q looks like a byte count (12), not a rune count (6)", got)
	}
}

// TestCodegenLengthParensEqualsBare proves `.length()` (the method-call
// spelling) and `.length` (the bare-property spelling) produce identical
// generated-code output, matching the interpreter's own dispatch which
// treats them the same way.
func TestCodegenLengthParensEqualsBare(t *testing.T) {
	t.Parallel()
	dataLiteral := `opsData{Items: []string{"a", "b", "c"}}`
	bare := runCodegenMethodOutput(t, "p #{Items.length}\n", dataLiteral)
	withParens := runCodegenMethodOutput(t, "p #{Items.length()}\n", dataLiteral)
	if bare != withParens {
		t.Errorf(".length generated %q but .length() generated %q — expected them to match", bare, withParens)
	}

	tmpl, err := Compile("p #{Items.length}\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want, err := tmpl.Render(map[string]any{"Items": []string{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if bare != want {
		t.Errorf("codegen output %q does not match interpreter output %q", bare, want)
	}
}

// TestCodegenIndexAndLengthDeferred proves the constructs still out of
// scope for this increment — index-then-dot (`arr[i].field`), a
// non-string-keyed map index, and a nil-DataReflectType (type-blind) index
// or `.length` — each return a clean "unsupported" error from GenerateGo
// rather than emitting something that might not match the interpreter.
func TestCodegenIndexAndLengthDeferred(t *testing.T) {
	cases := []struct {
		name            string
		src             string
		dataReflectType bool
	}{
		{name: "index-then-dot", src: "p= Firms[0].ID\n", dataReflectType: true},
		{name: "non-string-keyed map index", src: "p= IntKeyMap[0]\n", dataReflectType: true},
		{name: "type-blind index", src: "p= Items[0]\n", dataReflectType: false},
		{name: "type-blind length", src: "p= Items.length\n", dataReflectType: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			cfg := Config{
				PackageName: "gopug",
				FuncName:    "RenderOps",
				DataType:    "opsData",
			}
			if tc.dataReflectType {
				cfg.DataReflectType = opsDataReflectType
			}
			_, err = GenerateGo(ast, cfg)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not contain %q", tc.src, err.Error(), "unsupported")
			}
		})
	}
}
