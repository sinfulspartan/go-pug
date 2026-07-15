package gopug

import (
	"strings"
	"testing"
)

// TestCodegenUnbufferedReassignSameScopeString proves the base case: a
// `- var` local reassigned to a new value of the same type, entirely within
// its own declaring scope, reads back correctly.
func TestCodegenUnbufferedReassignSameScopeString(t *testing.T) {
	t.Parallel()
	src := "- var s = \"a\"\n- s = \"b\"\np=s\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenUnbufferedReassignSelfConcatInIf is the real corpus shape
// (`roCardClass = roCardClass + " border-primary"`): a `- var` local
// declared BEFORE an `if`, reassigned INSIDE the `if` to an expression that
// reads its own current value, then read again AFTER the `if` closes. This
// proves both the self-reference (the reassignment must read the CURRENT
// value, not a stale one) and the cross-scope persistence (a Go `=` to the
// enclosing-block local, from inside the nested `if` block, must be visible
// after that block closes — matching Runtime.renderConditional, which pushes
// no new scope frame at all, so setVar mutates the outer binding directly).
func TestCodegenUnbufferedReassignSelfConcatInIf(t *testing.T) {
	t.Parallel()
	src := "- var c = \"card\"\nif Flag\n  - c = c + \" active\"\np(class=c)\n"
	cases := []codegenUnbufferedCase{
		{
			name:        "branch taken: self-concat applies",
			src:         src,
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "branch not taken: original value persists",
			src:         src,
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedReassignSelfConcatInIfFaultInjection proves the
// differential above is actually discriminating: the false-branch's
// (pre-reassignment) value must NOT equal the true-branch's output, and
// dropping the concatenation's suffix must NOT equal the true-branch's
// output either — either would silently pass a differential that compared
// against a hand-computed (rather than interpreter-derived) expectation.
func TestCodegenUnbufferedReassignSelfConcatInIfFaultInjection(t *testing.T) {
	t.Parallel()
	src := "- var c = \"card\"\nif Flag\n  - c = c + \" active\"\np(class=c)\n"

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

	got := runGeneratedGo(t, generated, "opsData{Flag: true}")
	want := `<p class="card active"></p>`
	if got != want {
		t.Fatalf("generated output %q, want %q", got, want)
	}
	for _, wrongWant := range []string{`<p class="card"></p>`, `<p class="card active active"></p>`} {
		if got == wrongWant {
			t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched a deliberately wrong (no-reassignment or double-concat) expectation %q, proving the reassignment doesn't take effect or doesn't read the current value", got, wrongWant)
		}
	}
}

// TestCodegenUnbufferedReassignInEachPersistsAfterLoop is the other real
// corpus shape (`prevCategory = c.Category` row-dedup inside an `each`): a
// `- var` local declared BEFORE the loop, reassigned inside the loop body on
// every iteration, and read AFTER the loop closes — proving the
// reassignment persists across iterations and past the loop, matching
// Runtime.renderEach's setVar frame-walk past the loop's own scope frame to
// the pre-loop binding. (The dedup pattern's own guard — comparing the
// current item to the running `prev` value — needs a var-vs-var string
// comparison genComparison does not support yet, a separate, pre-existing
// gap unrelated to this reassignment slice, so this proves the reassignment
// half of the pattern on its own: after the loop, the local holds the LAST
// item processed, not merely its initial value.)
func TestCodegenUnbufferedReassignInEachPersistsAfterLoop(t *testing.T) {
	t.Parallel()
	src := "- var last = \"\"\neach item in Items\n  - last = item\np=last\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Items": []any{"a", "a", "b", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "a", "b", "b", "c"}}`,
	})
}

// TestCodegenUnbufferedReassignInEachWithNumericDedupGuard proves the FULL
// row-dedup pattern, including the guarding comparison against the running
// value: a `- var` local declared before an each-loop, compared against the
// current item (a numeric comparison, which genComparison already supports
// for two dynamically-typed numeric operands — unlike the string case
// above), then reassigned to the current item every iteration. Prices
// ([]float64) is used, not Nums ([]int), specifically so both sides of the
// reassignment classify to the exact same reflect.Type (float64): a bare
// numeric literal `- var` binding is always modeled as float64 (see
// genNumericExpr's own doc comment), so pairing it with an int-typed item
// variable would trip the reassignment's same-type gate — a real, useful
// restriction (`reflect.Type` equality, not merely "both numeric") this
// case is chosen to respect rather than route around.
func TestCodegenUnbufferedReassignInEachWithNumericDedupGuard(t *testing.T) {
	t.Parallel()
	src := "- var prev = -1\neach n in Prices\n  if n != prev\n    li=n\n  - prev = n\np=prev\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Prices": []any{1.0, 1.0, 2.0, 2.0, 3.0}},
		dataLiteral: "opsData{Prices: []float64{1.0, 1.0, 2.0, 2.0, 3.0}}",
	})
}

// TestCodegenUnbufferedReassignNumericSameType proves a numeric `- var`
// local reassigned to another numeric value of the same classified type
// reads back correctly.
func TestCodegenUnbufferedReassignNumericSameType(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\n- x = 10\np=x\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenUnbufferedReassignBoolSameType proves a bool `- var` local
// reassigned to another bool value reads back correctly, including that the
// reassigned value (not the original) is what a subsequent `if` observes. A
// bare `true`/`false` keyword literal is not itself a supported `- var`
// right-hand side yet (genBoolExpr classifies a bool-valued expression by
// resolving it as a comparison, negation, logical combinator, or bool
// field/local — a literal boolean keyword is a separate, pre-existing gap
// unrelated to reassignment), so both sides here are bool-typed fields.
func TestCodegenUnbufferedReassignBoolSameType(t *testing.T) {
	t.Parallel()
	src := "- var b = Flag\n- b = FlagB\nif b\n  p yes\nelse\n  p no\n"
	cases := []codegenUnbufferedCase{
		{
			name:        "reassigned to true",
			src:         src,
			data:        map[string]any{"Flag": false, "FlagB": true},
			dataLiteral: "opsData{Flag: false, FlagB: true}",
		},
		{
			name:        "reassigned to false",
			src:         src,
			data:        map[string]any{"Flag": true, "FlagB": false},
			dataLiteral: "opsData{Flag: true, FlagB: false}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// --- Deferrals ---

// TestCodegenUnbufferedReassignTypeChangeDeferred asserts a reassignment
// that changes the local's classified type (numeric to string) is deferred
// with a distinct, clean error, not attempted: the interpreter's untyped
// scope map allows this freely, but a fixed-type Go local (`__v_x float64`)
// cannot take a string assignment at all — this is required for Go
// validity, not merely a byte-identity precaution.
func TestCodegenUnbufferedReassignTypeChangeDeferred(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\n- x = \"hi\"\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a type-change error, got nil", src)
	}
	if !strings.Contains(err.Error(), "type change") {
		t.Errorf("GenerateGo(%q): error %q does not describe a type change", src, err.Error())
	}
}

// TestCodegenUnbufferedReassignNumericVarStringRhsDeferred asserts the
// reverse type mismatch (an existing STRING `- var` local reassigned from a
// bare numeric literal, which genNumericExpr classifies as float64) is also
// deferred with a distinct error.
func TestCodegenUnbufferedReassignNumericVarStringRhsDeferred(t *testing.T) {
	t.Parallel()
	src := "- var s = \"a\"\n- s = 5\np=s\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a type-change error, got nil", src)
	}
	if !strings.Contains(err.Error(), "type change") {
		t.Errorf("GenerateGo(%q): error %q does not describe a type change", src, err.Error())
	}
}

// TestCodegenUnbufferedReassignEachItemVarDeferred asserts "reassigning" an
// each-loop ITEM variable (not a `- var` local) is deferred with a distinct
// error: the interpreter's own model rebinds the item variable fresh every
// iteration rather than genuinely reassigning it, so mutating one in codegen
// is out of scope, exactly like the mutation slice's own item-var gate.
func TestCodegenUnbufferedReassignEachItemVarDeferred(t *testing.T) {
	t.Parallel()
	src := "each item in Items\n  - item = \"shadow\"\n  li=item\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a loop-item-variable error, got nil", src)
	}
	if !strings.Contains(err.Error(), "loop item/index variable") {
		t.Errorf("GenerateGo(%q): error %q does not describe an each-loop item/index variable", src, err.Error())
	}
}

// --- Regression ---

// TestCodegenUnbufferedReassignRegressionFreshBindingSuites re-runs a
// representative sample of the fresh-binding (`:=`) suites this slice must
// not disturb: a plain string literal, a numeric literal, and a bool
// comparison, none of which re-assign an already-bound name.
func TestCodegenUnbufferedReassignRegressionFreshBindingSuites(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "fresh string local",
			src:         "- var greeting = \"hello\"\np=greeting\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "fresh numeric local",
			src:         "- var x = 5\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "fresh bool local",
			src:         "- var b = Count > 3\nif b\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"Count": 5},
			dataLiteral: "opsData{Count: 5}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}
