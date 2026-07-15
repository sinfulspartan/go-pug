package gopug

import (
	"testing"
)

// TestCodegenMixedCompareStringFieldVsNumericLiteral proves genComparison's
// new default-branch stringify-both fallback handles a string field
// compared against a numeric literal, including the coercion case: a
// string field holding "5" compared against the numeric literal 5 —
// Runtime.evaluateExpr stringifies the literal to "5" too, so
// compareValues numeric-coerces both sides and calls them equal, even
// though the literal itself was never a string.
func TestCodegenMixedCompareStringFieldVsNumericLiteral(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: `Name == "0" literal, Name field "0"`, cond: `Name == "0"`, data: map[string]any{"Name": "0"}, dataLiteral: `opsData{Name: "0"}`},
		{name: "Name == 5, Name field \"5\" (numeric coercion)", cond: `Name == 5`, data: map[string]any{"Name": "5"}, dataLiteral: `opsData{Name: "5"}`},
		{name: "Name == 5, Name field \"6\" (numeric coercion, unequal)", cond: `Name == 5`, data: map[string]any{"Name": "6"}, dataLiteral: `opsData{Name: "6"}`},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenMixedCompareNumericFieldVsStringLiteral proves the reverse
// shape — a numeric field compared against a string literal — including
// both the numeric-coercion-equal case (count 3 against the literal "3")
// and the non-numeric-looking-string case (count 3 against "abc", which
// forces compareValues' string-compare branch since "abc" never parses as
// a number).
func TestCodegenMixedCompareNumericFieldVsStringLiteral(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: `Count == "3", Count field 3 (numeric-equal)`, cond: `Count == "3"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
		{name: `Count == "abc", Count field 3 (string-compare, unequal)`, cond: `Count == "abc"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
		{name: `Count != "abc", Count field 3`, cond: `Count != "abc"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenMixedCompareNumericFieldVsStringLiteralFaultInjection asserts
// the numeric-coercion discriminator directly: `Count == "3"` with Count=3
// must render the TRUE branch, not the FALSE branch a raw Go
// int-vs-string-typed comparison (a type mismatch that wouldn't even
// compile, or a naive fmt-stringify-then-== that would still agree here)
// might otherwise suggest. A deliberately WRONG expected treating this
// case as NOT equal must fail against the generated code's actual output,
// proving genComparison's fallback really does stringify Count to "3" and
// route it through gopug.CompareValues' numeric coercion rather than
// deferring or string-comparing byte-for-byte against some other
// representation.
func TestCodegenMixedCompareNumericFieldVsStringLiteralFaultInjection(t *testing.T) {
	t.Parallel()
	src := "if Count == \"3\"\n  p yes\nelse\n  p no\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, "opsData{Count: 3}")
	wrongWant := "<p>no</p>" // a raw-type-mismatch treatment of int 3 vs string "3" as unequal.
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q for Count=3 matched the deliberately wrong not-equal expectation %q; gopug.CompareValues' numeric coercion of the stringified operand is not being exercised", got, wrongWant)
	}
	if got != "<p>yes</p>" {
		t.Fatalf(`generated output %q, want "<p>yes</p>" (Count=3 stringifies to "3", which numeric-coerces equal to the literal "3")`, got)
	}
}

// TestCodegenMixedCompareNumericFieldVsStringField proves a numeric field
// compared against a STRING field (neither side a literal) also takes the
// stringify-both fallback.
func TestCodegenMixedCompareNumericFieldVsStringField(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: "Count == Str1, numeric-equal", cond: "Count == Str1", data: map[string]any{"Count": 3, "Str1": "3"}, dataLiteral: `opsData{Count: 3, Str1: "3"}`},
		{name: "Count == Str1, unequal (non-numeric string)", cond: "Count == Str1", data: map[string]any{"Count": 3, "Str1": "abc"}, dataLiteral: `opsData{Count: 3, Str1: "abc"}`},
		{name: "Count != Str1, unequal", cond: "Count != Str1", data: map[string]any{"Count": 3, "Str1": "abc"}, dataLiteral: `opsData{Count: 3, Str1: "abc"}`},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenMixedCompareBoolVsBool proves two independent bool fields
// compared with `==`/`!=` — a shape the numeric and stringish fast paths
// both reject (a Go bool doesn't satisfy either isNumeric() or
// isStringish()) — is handled by the stringify-both fallback: each side
// stringifies through strconv.FormatBool, exactly matching
// Runtime.evaluateExpr's own "true"/"false" for a bool field, then
// compareValues string-compares them ("true"/"false" never parse as
// numbers).
func TestCodegenMixedCompareBoolVsBool(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: "Flag == FlagB, both true", cond: "Flag == FlagB", data: map[string]any{"Flag": true, "FlagB": true}, dataLiteral: "opsData{Flag: true, FlagB: true}"},
		{name: "Flag == FlagB, true vs false", cond: "Flag == FlagB", data: map[string]any{"Flag": true, "FlagB": false}, dataLiteral: "opsData{Flag: true, FlagB: false}"},
		{name: "Flag != FlagB, true vs false", cond: "Flag != FlagB", data: map[string]any{"Flag": true, "FlagB": false}, dataLiteral: "opsData{Flag: true, FlagB: false}"},
		{name: "Flag != FlagB, both false", cond: "Flag != FlagB", data: map[string]any{"Flag": false, "FlagB": false}, dataLiteral: "opsData{Flag: false, FlagB: false}"},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenMixedCompareBoolVsString proves a bool field compared against
// a string literal — Runtime.evaluateExpr(flag) is "true"/"false", so
// `Flag == "true"` is TRUE exactly when Flag is true.
func TestCodegenMixedCompareBoolVsString(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: `Flag == "true", Flag true`, cond: `Flag == "true"`, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: `Flag == "true", Flag false`, cond: `Flag == "true"`, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
		{name: `Flag == "false", Flag true`, cond: `Flag == "false"`, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: `Flag == "false", Flag false`, cond: `Flag == "false"`, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenMixedCompareOrdering proves an ordering compare (`<`/`>`)
// between a numeric field and a string literal takes the numeric branch of
// compareValues (the string literal "2" parses as a number), matching the
// interpreter's own numeric ordering rather than a lexicographic string
// compare.
func TestCodegenMixedCompareOrdering(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: `Count > "2", Count field 3`, cond: `Count > "2"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
		{name: `Count < "2", Count field 3`, cond: `Count < "2"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
		{name: `Count >= "3", Count field 3`, cond: `Count >= "3"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
		{name: `Count <= "2", Count field 3`, cond: `Count <= "2"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenMixedCompareNonScalarOperandStillDeferred proves a
// slice-typed field compared against a numeric field still defers with a
// distinct error rather than being silently routed through the new
// stringify-both fallback: genOperand itself rejects a non-scalar field
// before genComparison's default branch is ever reached, since the
// interpreter would fmt-stringify a slice value (e.g. "[]") rather than
// stringifying it the way genScalarStringify's per-Kind switch does.
func TestCodegenMixedCompareNonScalarOperandStillDeferred(t *testing.T) {
	t.Parallel()
	src := "if Firms == Count\n  p yes\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-comparison error, got nil", src)
	}
}

// TestCodegenMixedCompareStrictEquality proves `===`/`!==` between a
// numeric field and a string literal still fold to `==`/`!=` inside
// gopug.CompareValues, matching compareValues' own ===/!== handling —
// the same folding genComparison's stringish fast path already applies,
// now exercised through the default-branch fallback instead.
func TestCodegenMixedCompareStrictEquality(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: `Count === "3", numeric-equal`, cond: `Count === "3"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
		{name: `Count !== "3", numeric-equal`, cond: `Count !== "3"`, data: map[string]any{"Count": 3}, dataLiteral: `opsData{Count: 3}`},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenMixedCompareImportGating proves the gopug import is added and
// used for a template whose only need for the gopug package is a
// stringify-both fallback comparison (as opposed to the stringish fast
// path's own CompareValues calls, already covered by
// codegen_string_compare_test.go).
func TestCodegenMixedCompareImportGating(t *testing.T) {
	t.Parallel()
	src := "if Count == \"3\"\n  p yes\nelse\n  p no\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "opsbuild",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	buildGeneratedGo(t, generated)
}
