package gopug

import "testing"

// TestCodegenConditionBareBoolLiteral proves genOperandTruthiness intercepts
// a bare `true`/`false` condition operand before it falls into field
// resolution, matching Runtime.evaluateExpr's own literal cases: `if true`
// renders the body, `if false` takes the else branch — exactly
// isTruthy(evaluateExpr("true")) / isTruthy(evaluateExpr("false")).
func TestCodegenConditionBareBoolLiteral(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "if true: body renders", src: "if true\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "if false: else renders", src: "if false\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenConditionBareNullFamilyLiteral proves the null-family
// literals (`null`, `undefined`, `nil`) are all falsy in condition
// position, matching Runtime.evaluateExpr's case that maps every one of
// them to the empty string and isTruthy's treatment of "" as falsy — a
// wrong oracle that expected the body to render would fail here, which is
// the point: the null-family must NOT be mistaken for truthy.
func TestCodegenConditionBareNullFamilyLiteral(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "if null: else renders (falsy)", src: "if null\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "if undefined: else renders (falsy)", src: "if undefined\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "if nil: else renders (falsy)", src: "if nil\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenConditionBareNullLiteralFaultInjection proves the previous
// test's oracle is not vacuously true: a hand-written expectation that
// treats `if null` as truthy (rendering the body instead of the else
// branch) must NOT match the codegen output, confirming the differential
// harness would actually catch a regression that flipped the null-family's
// polarity.
func TestCodegenConditionBareNullLiteralFaultInjection(t *testing.T) {
	t.Parallel()
	src := "if null\n  p yes\nelse\n  p no\n"

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

	got := runGeneratedGo(t, generated, "opsData{}")
	wrongExpected := "<p>yes</p>"
	if got == wrongExpected {
		t.Fatalf("codegen output %q for %q wrongly matches a truthy-null expectation; null must be falsy", got, src)
	}

	correctExpected := "<p>no</p>"
	if got != correctExpected {
		t.Errorf("codegen output %q does not match expected falsy-null output %q for %q", got, correctExpected, src)
	}
}

// TestCodegenUnlessBareBoolLiteral proves an `unless` over a bare bool
// literal negates it exactly like `unless` does over any other condition
// (genConditional's IsUnless handling is condition-shape-agnostic): `unless
// true` skips the body (condition true, negated to false), `unless false`
// renders it.
func TestCodegenUnlessBareBoolLiteral(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "unless true: else renders", src: "unless true\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "unless false: body renders", src: "unless false\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenConditionBareBoolLiteralNegation proves a leading `!` over a
// bare bool literal composes through genCondition's existing `!` recursion
// unchanged: the recursion resolves the inner `true`/`false` through the
// same literal interception this task adds, then negates it, matching
// Runtime.evaluateExpr's own `!` handling over the same literals.
func TestCodegenConditionBareBoolLiteralNegation(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "if !true: else renders", src: "if !true\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "if !false: body renders", src: "if !false\n  p yes\nelse\n  p no\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenConditionBareBoolLiteralCompound proves a bare bool literal
// composes with `&&`/`||` against a real field, through genCondition's
// existing binary-operator recursion: each operand (the literal and the
// field) is independently resolved to its own Go truthiness, then combined
// with native Go `&&`/`||`, matching isTruthy(a && b) == isTruthy(a) &&
// isTruthy(b) (and the `||` analogue) for a literal left operand.
func TestCodegenConditionBareBoolLiteralCompound(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "true && Flag(true): body renders", src: "if true && Flag\n  p yes\nelse\n  p no\n", data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "true && Flag(false): else renders", src: "if true && Flag\n  p yes\nelse\n  p no\n", data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
		{name: "false || Flag(true): body renders", src: "if false || Flag\n  p yes\nelse\n  p no\n", data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "false || Flag(false): else renders", src: "if false || Flag\n  p yes\nelse\n  p no\n", data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenConditionBoolFieldUnaffectedByLiteralIntercept is the
// regression proof that genOperandTruthiness's new literal switch only
// matches the five exact reserved tokens: a bare bool FIELD condition
// (`if Flag`) still falls through to resolveFieldExpr exactly as before
// this task, unaffected by the literal interception added ahead of it.
func TestCodegenConditionBoolFieldUnaffectedByLiteralIntercept(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "if Flag(true): body renders", src: "if Flag\n  p yes\nelse\n  p no\n", data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "if Flag(false): else renders", src: "if Flag\n  p yes\nelse\n  p no\n", data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}
