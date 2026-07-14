package gopug

import (
	"fmt"
	"strings"
	"testing"
)

// TestCodegenCaseBasicStringMatchAndDefault proves the ordinary shape — every
// when's expression is a quoted string literal, matched against the case
// expression's own field value, with a default clause — resolves the same
// body the interpreter's renderCase does for a match on the first when, a
// match on the second, and no match at all (default).
func TestCodegenCaseBasicStringMatchAndDefault(t *testing.T) {
	src := "case Name\n  when \"active\"\n    p Active\n  when \"off\"\n    p Off\n  default\n    p ?\n"
	var cases []codegenArithCase
	for _, name := range []string{"active", "off", "other"} {
		cases = append(cases, codegenArithCase{
			name:        name,
			src:         src,
			data:        map[string]any{"Name": name},
			dataLiteral: fmt.Sprintf("opsData{Name: %q}", name),
		})
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenCaseFallThroughEmptyWhen proves an empty-bodied when (`when "a"`
// with no body directly beneath it) falls through to the next clause's body
// — matching the first when renders the SECOND when's body, matching the
// second directly renders it too, and matching neither renders default.
func TestCodegenCaseFallThroughEmptyWhen(t *testing.T) {
	src := "case Name\n  when \"a\"\n  when \"b\"\n    p AB\n  default\n    p D\n"
	var cases []codegenArithCase
	for _, name := range []string{"a", "b", "c"} {
		cases = append(cases, codegenArithCase{
			name:        name,
			src:         src,
			data:        map[string]any{"Name": name},
			dataLiteral: fmt.Sprintf("opsData{Name: %q}", name),
		})
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenCaseStickyFallThroughPastNonMatch is the discriminating case
// that proves the sticky-`matched` model, not a naive "render the body of the
// when whose value equals the case value" model. `when "a"` is empty and
// falls through to `when "zzz"`'s body even though the case value "a" does
// NOT equal "zzz" — Runtime.renderCase's own `matched` flag is only ever set,
// never cleared, so once "a" matches, every later when's own comparison no
// longer matters; only whether ITS body is empty decides whether the walk
// keeps falling through. A naive per-when-match implementation would render
// nothing here (since "a" != "zzz"), which the differential below catches:
// it compares codegen's output directly against the interpreter's own
// Render, so a naive codegen model disagreeing with the interpreter fails
// this test outright, without needing a separately hand-computed
// expectation.
func TestCodegenCaseStickyFallThroughPastNonMatch(t *testing.T) {
	src := "case Name\n  when \"a\"\n  when \"zzz\"\n    p X\n"

	runCodegenArithDifferential(t, codegenArithCase{
		name:        "sticky match falls through an empty when past a non-matching value",
		src:         src,
		data:        map[string]any{"Name": "a"},
		dataLiteral: `opsData{Name: "a"}`,
	})

	// Pin the interpreter's own oracle value explicitly too, so this test's
	// discriminating power does not silently depend on the interpreter
	// itself staying correct.
	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	got, err := tmpl.Render(map[string]any{"Name": "a"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want := "<p>X</p>"; got != want {
		t.Fatalf("interpreter oracle does not match the expected sticky fall-through output: got %q want %q", got, want)
	}

	runCodegenArithDifferential(t, codegenArithCase{
		name:        "no match at all falls off the end with no default",
		src:         src,
		data:        map[string]any{"Name": "q"},
		dataLiteral: `opsData{Name: "q"}`,
	})
}

// TestCodegenCaseNoDefaultNoMatch proves a case with no default clause
// renders nothing when nothing matches.
func TestCodegenCaseNoDefaultNoMatch(t *testing.T) {
	src := "case Name\n  when \"a\"\n    p A\n"
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "no default, no match: empty output",
		src:         src,
		data:        map[string]any{"Name": "zzz"},
		dataLiteral: `opsData{Name: "zzz"}`,
	})
}

// TestCodegenCaseDefaultAfterFallToEnd proves default renders both when every
// matched when's body was empty (the walk fell off the end of c.Cases with
// `matched` true the whole way) and when nothing ever matched at all —
// Runtime.renderCase renders default identically in both post-loop cases, so
// codegen's single `if !doneTmp` gate after the when loop must cover both.
func TestCodegenCaseDefaultAfterFallToEnd(t *testing.T) {
	src := "case Name\n  when \"a\"\n  when \"b\"\n  default\n    p D\n"

	runCodegenArithDifferentialBatch(t, []codegenArithCase{
		{
			name:        "fall to end: every matched when's body empty",
			src:         src,
			data:        map[string]any{"Name": "a"},
			dataLiteral: `opsData{Name: "a"}`,
		},
		{
			name:        "never matched",
			src:         src,
			data:        map[string]any{"Name": "zzz"},
			dataLiteral: `opsData{Name: "zzz"}`,
		},
	})
}

// TestCodegenCaseNumericField proves a case expression over a numeric field
// compared against a bare numeric when literal still matches via STRING
// equality — both the case value and the when value are stringified by
// genValueExpr the same way evaluateExpr stringifies them (Count's int 1
// becomes "1", the when literal 1 also becomes "1"), so the comparison agrees
// with the interpreter without either side ever being treated as a number.
func TestCodegenCaseNumericField(t *testing.T) {
	src := "case Count\n  when 1\n    p one\n  default\n    p other\n"
	cases := []struct {
		name  string
		count int
	}{
		{name: "matches the numeric when", count: 1},
		{name: "does not match: falls to default", count: 2},
	}
	var diffCases []codegenArithCase
	for _, tc := range cases {
		diffCases = append(diffCases, codegenArithCase{
			name:        tc.name,
			src:         src,
			data:        map[string]any{"Count": tc.count},
			dataLiteral: fmt.Sprintf("opsData{Count: %d}", tc.count),
		})
	}
	runCodegenArithDifferentialBatch(t, diffCases)
}

// TestCodegenCaseNested proves a case nested inside another case's when body
// resolves correctly, exercising g.nextTmp()'s fresh numbering for the inner
// case's own caseVal/matched/done locals — if the inner case reused the
// outer's tmp names, the generated Go would either fail to compile (redeclare
// in the same scope) or silently shadow and misbehave; a clean build+run
// proves neither happened.
func TestCodegenCaseNested(t *testing.T) {
	src := "case Name\n  when \"a\"\n    case Str1\n      when \"x\"\n        p AX\n      default\n        p A?\n  default\n    p D\n"
	cases := []struct {
		name string
		nm   string
		str1 string
	}{
		{name: "outer and inner both match", nm: "a", str1: "x"},
		{name: "outer matches, inner falls to its own default", nm: "a", str1: "y"},
		{name: "outer falls to its own default", nm: "zzz", str1: "x"},
	}
	var diffCases []codegenArithCase
	for _, tc := range cases {
		diffCases = append(diffCases, codegenArithCase{
			name:        tc.name,
			src:         src,
			data:        map[string]any{"Name": tc.nm, "Str1": tc.str1},
			dataLiteral: fmt.Sprintf("opsData{Name: %q, Str1: %q}", tc.nm, tc.str1),
		})
	}
	runCodegenArithDifferentialBatch(t, diffCases)
}

// genCaseErr parses and GenerateGoes src against opsData, returning the
// resulting error (expected non-nil in every caller of this helper).
func genCaseErr(t *testing.T, src string) error {
	t.Helper()
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
	return err
}

// TestCodegenCaseFallibleCaseExpressionDeferred proves a fallible case
// expression (Count / Zero, a possible division by zero) is refused outright
// rather than generated — the interpreter could abort partway through the
// when walk with that error, at whatever point in the walk it happened to
// reach, and codegen has no way to reproduce that faithfully.
func TestCodegenCaseFallibleCaseExpressionDeferred(t *testing.T) {
	src := "case Count / Zero\n  when \"1\"\n    p one\n  default\n    p D\n"
	err := genCaseErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}

	// Pin the interpreter's own behavior for this fallible expression: it
	// really does error, confirming codegen's deferral is the correct call,
	// not an overly conservative one.
	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	if _, err := tmpl.Render(map[string]any{"Count": 4, "Zero": 0}); err == nil {
		t.Fatalf("interpreter Render: expected a division-by-zero error, got nil")
	}
}

// TestCodegenCaseFallibleWhenExpressionDeferred proves a fallible when
// expression is refused the same way a fallible case expression is.
func TestCodegenCaseFallibleWhenExpressionDeferred(t *testing.T) {
	src := "case Name\n  when Count / Zero\n    p one\n  default\n    p D\n"
	err := genCaseErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}

// TestCodegenCaseUnsupportedWhenExpressionDeferred proves a when expression
// genValueExpr has no support for at all (an array literal) is refused with
// its own distinct genValueExpr error, separate from the fallible-expression
// deferrals above.
func TestCodegenCaseUnsupportedWhenExpressionDeferred(t *testing.T) {
	src := "case Name\n  when [1, 2]\n    p one\n  default\n    p D\n"
	err := genCaseErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
	if !strings.Contains(err.Error(), "array literal") {
		t.Errorf("GenerateGo(%q): error %q does not describe the array-literal when expression", src, err.Error())
	}
}
