package gopug

import (
	"strings"
	"testing"
)

// This file proves the type-directed value-context methods that close the
// value-context expression compiler: `.join` (a Slice/Array receiver,
// resolved via resolveFieldExpr's RAW typed receiver rather than a
// stringified one, see genJoinValueExpr) and `.toFixed`/`.toPrecision` (a
// numeric receiver, TOTAL; a string receiver, FALLIBLE via the single-sourced
// gopug.ToFixedStr/gopug.ToPrecisionStr, see genToFixedOrPrecisionValueExpr).
// Every differential case is checked against Compile().Render, reusing the
// codegenArithCase/runCodegenArithDifferential and
// codegenFallibleErrorCase/runCodegenFallibleErrorDifferential machinery
// codegen_arith_test.go and codegen_fallible_test.go already established.

// TestCodegenJoinSliceReceivers proves `.join` on a Slice/Array receiver of
// several element kinds: a string slice, an int slice (each element's own
// fmt.Sprintf("%v", …) form), and a struct slice (proving the unrestricted
// per-element %v form the interpreter's own "join" case uses, not a
// scalar-only stringify).
func TestCodegenJoinSliceReceivers(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "string slice",
			src:         "p #{Items.join(', ')}\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "int slice",
			src:         "p= Nums.join('-')\n",
			data:        map[string]any{"Nums": []int{1, 2, 3}},
			dataLiteral: `opsData{Nums: []int{1, 2, 3}}`,
		},
		{
			name:        "struct slice (proves unrestricted per-element %v form)",
			src:         "p #{Firms.join(',')}\n",
			data:        map[string]any{"Firms": []opsFirm{{ID: 1}, {ID: 2}}},
			dataLiteral: `opsData{Firms: []opsFirm{{ID: 1}, {ID: 2}}}`,
		},
		{
			name:        "0-arg join defaults sep to empty string",
			src:         "p= Items.join()\n",
			data:        map[string]any{"Items": []string{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "nil-valued (but present) slice joins to empty string",
			src:         "p= Items.join(', ')\n",
			data:        map[string]any{"Items": []string(nil)},
			dataLiteral: `opsData{Items: []string(nil)}`,
		},
		{
			name:        "empty (non-nil) slice joins to empty string",
			src:         "p= Items.join(', ')\n",
			data:        map[string]any{"Items": []string{}},
			dataLiteral: `opsData{Items: []string{}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenJoinSepQuoteStripBothQuoteStylesAgree proves join(',') and
// join(",") produce IDENTICAL generated-code output to each other, exactly
// like the existing split(',')/split(",") proof, since UnquoteArg strips
// either quote style off the separator the same way.
func TestCodegenJoinSepQuoteStripBothQuoteStylesAgree(t *testing.T) {
	data := map[string]any{"Items": []string{"a", "b", "c"}}
	dataLiteral := `opsData{Items: []string{"a", "b", "c"}}`

	singleQuoted := runCodegenMethodOutput(t, "p= Items.join(',')\n", dataLiteral)
	doubleQuoted := runCodegenMethodOutput(t, `p= Items.join(",")`+"\n", dataLiteral)
	if singleQuoted != doubleQuoted {
		t.Errorf("join(',') generated %q but join(\",\") generated %q — expected the quote style to make no difference", singleQuoted, doubleQuoted)
	}

	tmpl, err := Compile("p= Items.join(',')\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if singleQuoted != want {
		t.Errorf("codegen output %q does not match interpreter output %q", singleQuoted, want)
	}
}

// TestCodegenToFixedNumericReceiverTotal proves `.toFixed` on a numeric
// receiver (float64, int) is TOTAL: it never returns an error, formats via
// the single-sourced gopug.ToFixed, and matches the interpreter's own
// formatting exactly, including the negative-precision clamp to 0 decimals
// and the 0-arg default of 0 decimals.
func TestCodegenToFixedNumericReceiverTotal(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "float64 field, two decimal places",
			src:         "p #{Price.toFixed(2)}\n",
			data:        map[string]any{"Price": 9.995},
			dataLiteral: "opsData{Price: 9.995}",
		},
		{
			name:        "int field, zero decimal places",
			src:         "p= Count.toFixed(0)\n",
			data:        map[string]any{"Count": 42},
			dataLiteral: "opsData{Count: 42}",
		},
		{
			name:        "negative precision clamps to 0 decimals",
			src:         "p= Price.toFixed(-1)\n",
			data:        map[string]any{"Price": 3.7},
			dataLiteral: "opsData{Price: 3.7}",
		},
		{
			name:        "0-arg toFixed defaults to 0 decimals",
			src:         "p= Price.toFixed()\n",
			data:        map[string]any{"Price": 7.9},
			dataLiteral: "opsData{Price: 7.9}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenToFixedStringReceiverFallibleSuccess proves `.toFixed` on a
// string receiver holding a numeric-looking value is FALLIBLE but succeeds:
// gopug.ToFixedStr parses it with strconv.ParseFloat and formats via
// gopug.ToFixed, matching the interpreter's own default-branch
// ParseFloat(objVal) fallback exactly.
func TestCodegenToFixedStringReceiverFallibleSuccess(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "numeric-looking string field",
		src:         "p #{Name.toFixed(2)}\n",
		data:        map[string]any{"Name": "3.14159"},
		dataLiteral: `opsData{Name: "3.14159"}`,
	})
}

// TestCodegenToFixedStringReceiverErrorParity is the headline error-parity
// proof: `.toFixed` on a string receiver holding a NON-numeric value aborts
// BOTH the interpreter's Render and the generated RenderOps with the
// identical "toFixed: value ... is not a number" error, matching
// Runtime.evaluateExpr's own default-branch ParseFloat failure exactly (via
// the single-sourced gopug.ToFixedStr both engines now call).
func TestCodegenToFixedStringReceiverErrorParity(t *testing.T) {
	runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
		name:        "non-numeric string field",
		src:         "p #{Name.toFixed(2)}\n",
		data:        map[string]any{"Name": "abc"},
		dataLiteral: `opsData{Name: "abc"}`,
	})
}

// TestCodegenToPrecisionNumericReceiverTotal proves `.toPrecision` on a
// numeric receiver is TOTAL, matching gopug.ToPrecision's formatting
// (including the 0-arg default of 6 significant figures) exactly.
func TestCodegenToPrecisionNumericReceiverTotal(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "float64 field, three significant figures",
			src:         "p #{Price.toPrecision(3)}\n",
			data:        map[string]any{"Price": 123.456},
			dataLiteral: "opsData{Price: 123.456}",
		},
		{
			name:        "0-arg toPrecision defaults to 6 significant figures",
			src:         "p= Price.toPrecision()\n",
			data:        map[string]any{"Price": 0.1234567},
			dataLiteral: "opsData{Price: 0.1234567}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenToPrecisionStringReceiverFallible proves `.toPrecision` on a
// string receiver is FALLIBLE in both directions: a numeric-looking string
// succeeds (matching gopug.ToPrecisionStr's ParseFloat-then-format path),
// and a non-numeric string produces the identical
// "toPrecision: value ... is not a number" error in both engines.
func TestCodegenToPrecisionStringReceiverFallible(t *testing.T) {
	t.Run("numeric-looking string succeeds", func(t *testing.T) {
		runCodegenArithDifferential(t, codegenArithCase{
			name:        "numeric-looking string field",
			src:         "p #{Name.toPrecision(3)}\n",
			data:        map[string]any{"Name": "123.456"},
			dataLiteral: `opsData{Name: "123.456"}`,
		})
	})
	t.Run("non-numeric string errors identically in both engines", func(t *testing.T) {
		runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
			name:        "non-numeric string field",
			src:         "p #{Name.toPrecision(3)}\n",
			data:        map[string]any{"Name": "xyz"},
			dataLiteral: `opsData{Name: "xyz"}`,
		})
	})
}

// TestCodegenJoinToFixedToPrecisionDeferredReceivers proves join/toFixed/
// toPrecision each still return a clean "unsupported" error from GenerateGo
// when their receiver's Kind isn't one their type-directed dispatch
// supports (a bool/struct receiver for toFixed/toPrecision, a non-slice
// receiver for join), and that a nil Config.DataReflectType — which leaves
// the receiver's Kind unknowable — defers all three the same way.
func TestCodegenJoinToFixedToPrecisionDeferredReceivers(t *testing.T) {
	cases := []struct {
		name            string
		src             string
		dataReflectType bool
	}{
		{name: "toFixed on a bool receiver", src: "p= Flag.toFixed(2)\n", dataReflectType: true},
		{name: "toFixed on a struct receiver", src: "p= User.toFixed(2)\n", dataReflectType: true},
		{name: "toPrecision on a bool receiver", src: "p= Flag.toPrecision(3)\n", dataReflectType: true},
		{name: "join on a non-slice (string) receiver", src: "p= Name.join(',')\n", dataReflectType: true},
		{name: "join on a non-slice (struct) receiver", src: "p= User.join(',')\n", dataReflectType: true},
		{name: "index-then-dot receiver stays deferred", src: "p= Firms[0].ID.toFixed(2)\n", dataReflectType: true},
		{name: "toFixed with a nil DataReflectType", src: "p= Name.toFixed(2)\n", dataReflectType: false},
		{name: "join with a nil DataReflectType", src: "p= Items.join(',')\n", dataReflectType: false},
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
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}
