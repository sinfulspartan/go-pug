package gopug

import (
	"fmt"
	"strings"
	"testing"
)

// TestCodegenStringCompareFieldVsVarDedup proves the motivating end-to-end
// pattern this generalization unlocks: a `- var` string local tracking the
// previous each-loop row, compared against the current row's dot-path
// string field with `!=`, so only the first row of each run of consecutive
// equal categories renders. Before this generalization genComparison's
// stringish branch only supported a string field compared to a string
// literal; a field compared to a `- var` local fell through to the default
// "not comparable in this increment" deferral.
func TestCodegenStringCompareFieldVsVarDedup(t *testing.T) {
	t.Parallel()
	src := "- var prev = \"\"\n" +
		"each row in Rows\n" +
		"  if row.Cat != prev\n" +
		"    li= row.Cat\n" +
		"  - prev = row.Cat\n"

	cases := []codegenUnbufferedCase{
		{
			name:        "consecutive duplicates collapse to one row each",
			src:         src,
			data:        map[string]any{"Rows": []map[string]any{{"Cat": "a"}, {"Cat": "a"}, {"Cat": "b"}, {"Cat": "b"}, {"Cat": "c"}}},
			dataLiteral: `opsData{Rows: []opsRow{{Cat: "a"}, {Cat: "a"}, {Cat: "b"}, {Cat: "b"}, {Cat: "c"}}}`,
		},
		{
			name:        "no duplicates: every row renders",
			src:         src,
			data:        map[string]any{"Rows": []map[string]any{{"Cat": "a"}, {"Cat": "b"}, {"Cat": "c"}}},
			dataLiteral: `opsData{Rows: []opsRow{{Cat: "a"}, {Cat: "b"}, {Cat: "c"}}}`,
		},
		{
			name:        "all duplicates: only the first row renders",
			src:         src,
			data:        map[string]any{"Rows": []map[string]any{{"Cat": "x"}, {"Cat": "x"}, {"Cat": "x"}}},
			dataLiteral: `opsData{Rows: []opsRow{{Cat: "x"}, {Cat: "x"}, {Cat: "x"}}}`,
		},
		{
			name:        "empty Rows",
			src:         src,
			data:        map[string]any{"Rows": []map[string]any{}},
			dataLiteral: `opsData{Rows: []opsRow{}}`,
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenStringCompareVarVsVar proves two `- var` string locals compared
// against each other — neither side a struct field at all — is routed
// through gopug.CompareValues exactly like a field on either side.
func TestCodegenStringCompareVarVsVar(t *testing.T) {
	t.Parallel()
	mkSrc := func(op string) string {
		return fmt.Sprintf("- var a = Str1\n- var b = Str2\nif a %s b\n  p yes\nelse\n  p no\n", op)
	}

	cases := []codegenUnbufferedCase{
		{name: "== equal", src: mkSrc("=="), data: map[string]any{"Str1": "x", "Str2": "x"}, dataLiteral: `opsData{Str1: "x", Str2: "x"}`},
		{name: "== unequal", src: mkSrc("=="), data: map[string]any{"Str1": "x", "Str2": "y"}, dataLiteral: `opsData{Str1: "x", Str2: "y"}`},
		{name: "!= equal", src: mkSrc("!="), data: map[string]any{"Str1": "x", "Str2": "x"}, dataLiteral: `opsData{Str1: "x", Str2: "x"}`},
		{name: "!= unequal", src: mkSrc("!="), data: map[string]any{"Str1": "x", "Str2": "y"}, dataLiteral: `opsData{Str1: "x", Str2: "y"}`},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenStringCompareFieldVsField proves two string FIELDS (no `- var`
// on either side) compared against each other, another shape the earlier
// field-vs-literal-only increment deferred.
func TestCodegenStringCompareFieldVsField(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: "equal fields", cond: "Str1 == Str2", data: map[string]any{"Str1": "same", "Str2": "same"}, dataLiteral: `opsData{Str1: "same", Str2: "same"}`},
		{name: "unequal fields", cond: "Str1 == Str2", data: map[string]any{"Str1": "a", "Str2": "b"}, dataLiteral: `opsData{Str1: "a", Str2: "b"}`},
		{name: "unequal fields, !=", cond: "Str1 != Str2", data: map[string]any{"Str1": "a", "Str2": "b"}, dataLiteral: `opsData{Str1: "a", Str2: "b"}`},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenStringCompareNumericLookingCoercion is the FIRST discriminating
// case: the interpreter's compareValues compares two numeric-looking string
// values NUMERICALLY, so "3" and "3.0" — and "3" and "03" — compare EQUAL,
// even though they are different byte sequences a raw Go `==` would call
// unequal. This proves gopug.CompareValues, not a raw Go `==`, is what
// genComparison now emits for a stringish comparison whose operand values
// aren't known until render.
func TestCodegenStringCompareNumericLookingCoercion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		cond        string
		data        map[string]any
		dataLiteral string
		want        bool
	}{
		{name: `"3" == "3.0" numeric-equal`, cond: "Str1 == Str2", data: map[string]any{"Str1": "3", "Str2": "3.0"}, dataLiteral: `opsData{Str1: "3", Str2: "3.0"}`, want: true},
		{name: `"3" == "03" numeric-equal`, cond: "Str1 == Str2", data: map[string]any{"Str1": "3", "Str2": "03"}, dataLiteral: `opsData{Str1: "3", Str2: "03"}`, want: true},
		{name: `"abc" == "abc" non-numeric string-equal`, cond: "Str1 == Str2", data: map[string]any{"Str1": "abc", "Str2": "abc"}, dataLiteral: `opsData{Str1: "abc", Str2: "abc"}`, want: true},
		{name: `"3" == "4" numeric-unequal`, cond: "Str1 == Str2", data: map[string]any{"Str1": "3", "Str2": "4"}, dataLiteral: `opsData{Str1: "3", Str2: "4"}`, want: false},
	}

	var diffCases []conditionDiffCase
	for _, tc := range cases {
		diffCases = append(diffCases, conditionDiffCase{name: tc.name, cond: tc.cond, data: tc.data, dataLiteral: tc.dataLiteral})
	}
	results := runConditionDifferentialBatch(t, diffCases)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			if want := boolBranch(tc.want); got != want {
				t.Errorf("condition %q with data %v: got %q, want %q", tc.cond, tc.data, got, want)
			}
		})
	}
}

// TestCodegenStringCompareNumericLookingCoercionFaultInjection asserts the
// discriminator directly rather than only via the interpreter oracle: the
// generated code's own output for "3" == "3.0" must NOT match the
// deliberately wrong raw-Go-`==` expectation ("not equal"), proving the
// generated code actually calls gopug.CompareValues (which numeric-compares)
// rather than emitting a raw Go string `==` (which would say unequal).
func TestCodegenStringCompareNumericLookingCoercionFaultInjection(t *testing.T) {
	t.Parallel()
	src := "if Str1 == Str2\n  p yes\nelse\n  p no\n"

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

	got := runGeneratedGo(t, generated, `opsData{Str1: "3", Str2: "3.0"}`)
	wrongWant := "<p>no</p>" // raw Go `==` behavior: "3" != "3.0" as byte sequences.
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q for Str1=%q Str2=%q matched the deliberately wrong raw-Go-== expectation %q; gopug.CompareValues' numeric coercion is not being exercised", got, "3", "3.0", wrongWant)
	}
	if got != "<p>yes</p>" {
		t.Fatalf(`generated output %q, want "<p>yes</p>" ("3" and "3.0" compare numerically equal)`, got)
	}
}

// TestCodegenStringCompareOrdering proves ordering compares (`<`/`>`/`<=`/
// `>=`) between two stringish operands — entirely unsupported before this
// generalization — are now routed through gopug.CompareValues, including
// the SECOND discriminating case: "10" > "9" compares numerically TRUE
// (10 > 9), not lexicographically FALSE ("1" < "9" byte-wise).
func TestCodegenStringCompareOrdering(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		cond        string
		data        map[string]any
		dataLiteral string
		want        bool
	}{
		{name: "apple < banana lexicographic", cond: "Str1 < Str2", data: map[string]any{"Str1": "apple", "Str2": "banana"}, dataLiteral: `opsData{Str1: "apple", Str2: "banana"}`, want: true},
		{name: "banana > apple lexicographic", cond: "Str1 > Str2", data: map[string]any{"Str1": "banana", "Str2": "apple"}, dataLiteral: `opsData{Str1: "banana", Str2: "apple"}`, want: true},
		{name: `"10" > "9" numeric ordering`, cond: "Str1 > Str2", data: map[string]any{"Str1": "10", "Str2": "9"}, dataLiteral: `opsData{Str1: "10", Str2: "9"}`, want: true},
		{name: `"9" <= "10" numeric ordering`, cond: "Str1 <= Str2", data: map[string]any{"Str1": "9", "Str2": "10"}, dataLiteral: `opsData{Str1: "9", Str2: "10"}`, want: true},
		{name: `"10" >= "10" numeric ordering, equal`, cond: "Str1 >= Str2", data: map[string]any{"Str1": "10", "Str2": "10"}, dataLiteral: `opsData{Str1: "10", Str2: "10"}`, want: true},
	}

	var diffCases []conditionDiffCase
	for _, tc := range cases {
		diffCases = append(diffCases, conditionDiffCase{name: tc.name, cond: tc.cond, data: tc.data, dataLiteral: tc.dataLiteral})
	}
	results := runConditionDifferentialBatch(t, diffCases)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			if want := boolBranch(tc.want); got != want {
				t.Errorf("condition %q with data %v: got %q, want %q", tc.cond, tc.data, got, want)
			}
		})
	}
}

// TestCodegenStringCompareOrderingFaultInjection asserts the SECOND
// discriminator directly: the generated code's own output for "10" > "9"
// must NOT match the deliberately wrong lexicographic expectation (false),
// proving the comparison is numeric, not a raw Go string `>`.
func TestCodegenStringCompareOrderingFaultInjection(t *testing.T) {
	t.Parallel()
	src := "if Str1 > Str2\n  p yes\nelse\n  p no\n"

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

	got := runGeneratedGo(t, generated, `opsData{Str1: "10", Str2: "9"}`)
	wrongWant := "<p>no</p>" // lexicographic Go string `>` behavior: "10" < "9" byte-wise.
	if got == wrongWant {
		t.Fatalf(`fault injection did not fault: generated output %q for Str1="10" Str2="9" matched the deliberately wrong lexicographic expectation %q; gopug.CompareValues' numeric ordering is not being exercised`, got, wrongWant)
	}
	if got != "<p>yes</p>" {
		t.Fatalf(`generated output %q, want "<p>yes</p>" ("10" > "9" compares numerically true)`, got)
	}
}

// TestCodegenStringCompareFieldVsLiteralRegression proves the field-vs-
// literal shape the earlier increment already supported — including a
// non-numeric-looking literal (the previously-supported fast path) — stays
// byte-identical to the interpreter now that it is routed uniformly through
// gopug.CompareValues, and additionally proves the two cases that increment
// deferred (a numeric-looking string literal, and an ordering compare
// against a literal) now succeed too.
func TestCodegenStringCompareFieldVsLiteralRegression(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		cond        string
		data        map[string]any
		dataLiteral string
	}{
		{name: "non-numeric-looking literal, match", cond: `Name == "invoice"`, data: map[string]any{"Name": "invoice"}, dataLiteral: `opsData{Name: "invoice"}`},
		{name: "non-numeric-looking literal, no match", cond: `Name == "invoice"`, data: map[string]any{"Name": "receipt"}, dataLiteral: `opsData{Name: "receipt"}`},
		{name: "numeric-looking literal, match", cond: `Name == "5"`, data: map[string]any{"Name": "5"}, dataLiteral: `opsData{Name: "5"}`},
		{name: "numeric-looking literal, numeric-equal but different spelling", cond: `Name == "5"`, data: map[string]any{"Name": "5.0"}, dataLiteral: `opsData{Name: "5.0"}`},
		{name: "ordering against a literal", cond: `Name > "m"`, data: map[string]any{"Name": "z"}, dataLiteral: `opsData{Name: "z"}`},
	}

	var diffCases []conditionDiffCase
	for _, tc := range cases {
		diffCases = append(diffCases, conditionDiffCase{name: tc.name, cond: tc.cond, data: tc.data, dataLiteral: tc.dataLiteral})
	}
	results := runConditionDifferentialBatch(t, diffCases)

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenStringCompareStrictEquality proves `===`/`!==` on two stringish
// operands still fold to `==`/`!=` inside gopug.CompareValues, matching
// compareValues' own === /!== handling.
func TestCodegenStringCompareStrictEquality(t *testing.T) {
	t.Parallel()
	cases := []conditionDiffCase{
		{name: "=== equal", cond: "Str1 === Str2", data: map[string]any{"Str1": "x", "Str2": "x"}, dataLiteral: `opsData{Str1: "x", Str2: "x"}`},
		{name: "=== unequal", cond: "Str1 === Str2", data: map[string]any{"Str1": "x", "Str2": "y"}, dataLiteral: `opsData{Str1: "x", Str2: "y"}`},
		{name: "!== unequal", cond: "Str1 !== Str2", data: map[string]any{"Str1": "x", "Str2": "y"}, dataLiteral: `opsData{Str1: "x", Str2: "y"}`},
		{name: "!== equal", cond: "Str1 !== Str2", data: map[string]any{"Str1": "x", "Str2": "x"}, dataLiteral: `opsData{Str1: "x", Str2: "x"}`},
	}
	results := runConditionDifferentialBatch(t, cases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
		})
	}
}

// TestCodegenStringCompareNonScalarOperandStillDeferred asserts a
// non-scalar operand (a slice field) compared against anything is NOT
// routed through gopug.CompareValues: genOperand itself rejects a
// slice/map/struct/pointer field before genComparison's stringify-both
// fallback (see codegen_mixed_compare_test.go) is ever reached, since the
// interpreter would fmt-stringify such a value instead of stringifying it
// the way genScalarStringify's per-Kind switch does — a footgun this
// package does not attempt to reproduce. A string operand compared against
// a numeric operand, previously deferred here too, is now supported; see
// TestCodegenMixedCompareStringFieldVsNumericLiteral and its siblings.
func TestCodegenStringCompareNonScalarOperandStillDeferred(t *testing.T) {
	t.Parallel()
	src := "if Items == Name\n  p yes\n"
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
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}

// TestCodegenStringCompareImportGating proves the gopug import is added
// (and, since the batch's `go build` succeeds, correctly USED — an unused
// import is itself a compile error) for a template whose only need for the
// gopug package is the CompareValues call this generalization introduces.
func TestCodegenStringCompareImportGating(t *testing.T) {
	t.Parallel()
	src := "if Str1 == Str2\n  p yes\nelse\n  p no\n"

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

	genSrc := string(generated)
	if !strings.Contains(genSrc, `"github.com/sinfulspartan/go-pug/pkg/gopug"`) {
		t.Errorf("generated source does not import the gopug package:\n%s", genSrc)
	}
	if !strings.Contains(genSrc, "gopug.CompareValues(") {
		t.Errorf("generated source does not call gopug.CompareValues:\n%s", genSrc)
	}

	// buildGeneratedGo fails the compile if the import is missing (an
	// undefined gopug.CompareValues reference) or unused (Go itself rejects
	// an unused import), so a successful build is itself proof the import
	// gating is correct in both directions.
	buildGeneratedGo(t, generated)
}
