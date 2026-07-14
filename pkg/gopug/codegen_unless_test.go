package gopug

import (
	"fmt"
	"strings"
	"testing"
)

// TestCodegenUnlessTruthySkipFalsyRender proves genConditional's `unless`
// branch negates genCondition's translated condition — a truthy condition
// skips the body (matching Runtime.renderConditional's boolVal = !boolVal
// negation), a falsy one renders it, exactly the opposite of a plain `if`
// over the same condition.
func TestCodegenUnlessTruthySkipFalsyRender(t *testing.T) {
	src := "unless Flag\n  p yes\n"
	cases := []struct {
		name string
		flag bool
	}{
		{name: "truthy condition: body skipped", flag: true},
		{name: "falsy condition: body rendered", flag: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.flag},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v}", tc.flag),
			})
		})
	}
}

// TestCodegenUnlessElse proves an `unless` with an `else` renders Alternate on
// a truthy condition (the negated body is skipped, so the else fires) and
// Consequent on a falsy one — the parser accepts an else on `unless`
// identically to `if` (parseUnless), and genConditional's Consequent/
// Alternate handling is completely unchanged between the two, so this is the
// same code path as TestCodegenPlainIfRegression's if-else cases with the
// branch selection flipped.
func TestCodegenUnlessElse(t *testing.T) {
	src := "unless Flag\n  p yes\nelse\n  p no\n"
	cases := []struct {
		name string
		flag bool
	}{
		{name: "truthy condition: else branch renders", flag: true},
		{name: "falsy condition: consequent branch renders", flag: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.flag},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v}", tc.flag),
			})
		})
	}
}

// TestCodegenUnlessComparisonCondition proves an `unless` whose condition is
// a comparison (routed through genCondition's operator handling, not bare
// truthiness) negates correctly too — genCondition's translated Go bool is
// negation-agnostic to which of its supported shapes produced it.
func TestCodegenUnlessComparisonCondition(t *testing.T) {
	src := "unless Count == 5\n  p yes\n"
	cases := []struct {
		name  string
		count int
	}{
		{name: "condition true (Count == 5): body skipped", count: 5},
		{name: "condition false (Count != 5): body rendered", count: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Count": tc.count},
				dataLiteral: fmt.Sprintf("opsData{Count: %d}", tc.count),
			})
		})
	}
}

// TestCodegenUnlessUncompilableConditionDeferred proves an `unless` whose
// condition genCondition cannot compile (the same arithmetic-in-comparison
// shape TestCodegenConditionOperatorUnsupported already pins for plain `if`)
// still returns a clean "unsupported" error rather than silently emitting
// something — genConditional propagates genCondition's error unchanged
// regardless of IsUnless.
func TestCodegenUnlessUncompilableConditionDeferred(t *testing.T) {
	src := "unless Count + 1 > 2\n  p yes\n"

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
		t.Fatalf("GenerateGo(%q): expected an unsupported-condition error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}
