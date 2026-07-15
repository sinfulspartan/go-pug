package gopug

import (
	"strings"
	"testing"
)

// --- Headline corpus shapes (differential: codegen build+run vs interpreter) ---

// TestCodegenUnbufferedMutationIncrementDecrement proves `x++`/`x--` on a
// float64 `- var` local reads back correctly, including a repeated
// increment.
func TestCodegenUnbufferedMutationIncrementDecrement(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "increment once",
			src:         "- var x = 5\n- x++\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "increment twice",
			src:         "- var x = 5\n- x++\n- x++\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "decrement once",
			src:         "- var x = 5\n- x--\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedMutationAddSubAssign proves `+=`/`-=` with a numeric
// literal RHS on a float64 `- var` local reads back correctly.
func TestCodegenUnbufferedMutationAddSubAssign(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "add-assign",
			src:         "- var x = 5\n- x += 3\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "sub-assign",
			src:         "- var x = 10\n- x -= 4\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedMutationFractional proves mutation on a fractional
// float64 local — the discriminator that proves the mutated local is a
// genuine Go float64, not an int: an integer model would either fail to
// compile or silently truncate the fractional result.
func TestCodegenUnbufferedMutationFractional(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "fractional add-assign",
			src:         "- var x = 5.5\n- x += 2\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "fractional sub-assign",
			src:         "- var x = 10\n- x -= 2.5\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "fractional rhs on a whole-number local",
			src:         "- var x = 1\n- x += 0.5\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedMutationFractionalFaultInjection proves the
// fractional differential above is actually discriminating float64 from
// int: an integer-truncated expected value must FAIL the comparison.
func TestCodegenUnbufferedMutationFractionalFaultInjection(t *testing.T) {
	t.Parallel()
	src := "- var x = 5.5\n- x += 2\np=x\n"

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
	wrongWant := "<p>7</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong integer-truncated expectation %q (proves float64, not int, is required)", got, wrongWant)
	}
	if got != "<p>7.5</p>" {
		t.Errorf("generated output %q, want %q", got, "<p>7.5</p>")
	}
}

// TestCodegenUnbufferedMutationLoopAccumulation is the money case: a
// `- var` initialized before an each-loop, mutated inside the loop body by
// `+=`/`++` over each iteration's item variable, and read back after the
// loop closes. This proves Go's mutation of the outer local from inside a
// nested range loop matches the interpreter's own setVar-into-the-enclosing-
// frame accumulation, across every iteration, not merely the last one.
func TestCodegenUnbufferedMutationLoopAccumulation(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "sum via += over a float64 slice",
			src:         "- var total = 0\neach item in Prices\n  - total += item\np=total\n",
			data:        map[string]any{"Prices": []any{1.5, 2.5, 3.0}},
			dataLiteral: "opsData{Prices: []float64{1.5, 2.5, 3.0}}",
		},
		{
			name:        "count via ++ over an int slice",
			src:         "- var n = 0\neach item in Nums\n  - n++\np=n\n",
			data:        map[string]any{"Nums": []any{1, 2, 3, 4}},
			dataLiteral: "opsData{Nums: []int{1, 2, 3, 4}}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedMutationLoopAccumulationFaultInjection proves the
// loop-accumulation differential is exercising real per-iteration
// accumulation, not merely the last item or a no-op: both a
// last-item-only and a zero expectation must FAIL.
func TestCodegenUnbufferedMutationLoopAccumulationFaultInjection(t *testing.T) {
	t.Parallel()
	src := "- var total = 0\neach item in Prices\n  - total += item\np=total\n"

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

	got := runGeneratedGo(t, generated, "opsData{Prices: []float64{1.5, 2.5, 3.0}}")
	if got != "<p>7</p>" {
		t.Errorf("generated output %q, want %q", got, "<p>7</p>")
	}
	for _, wrongWant := range []string{"<p>3</p>", "<p>0</p>"} {
		if got == wrongWant {
			t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched a deliberately wrong (last-item-only or zero) expectation %q, proving the accumulation isn't real", got, wrongWant)
		}
	}
}

// TestCodegenUnbufferedMutationPreMutationRead proves a read of the local
// BEFORE the mutation statement is also byte-identical, not merely the
// post-mutation value.
func TestCodegenUnbufferedMutationPreMutationRead(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\np=x\n- x++\np=x\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenUnbufferedMutationComparisonAfterMutation proves a mutated
// float64 local flows correctly into the pre-existing numeric-local
// comparison path.
func TestCodegenUnbufferedMutationComparisonAfterMutation(t *testing.T) {
	t.Parallel()
	src := "- var x = 0\n- x++\nif x == 1\n  p yes\nelse\n  p no\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenUnbufferedMutationNegativeAndZero proves a mutation that
// crosses zero, or lands negative, reads back correctly.
func TestCodegenUnbufferedMutationNegativeAndZero(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "sub-assign crosses zero into negative",
			src:         "- var x = 3\n- x -= 5\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "decrement from zero",
			src:         "- var x = 0\n- x--\np=x\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedMutationCrossScopeEnclosingVar proves a mutation
// inside an if-branch that targets a `- var` declared in an OUTER
// (enclosing) scope mutates that same outer Go local — matching the
// interpreter's setVar frame-walk, which updates the innermost frame
// actually holding the variable rather than creating a fresh shadowed one.
func TestCodegenUnbufferedMutationCrossScopeEnclosingVar(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "mutated inside a true if-branch",
			src:         "- var total = 10\nif Flag\n  - total += 5\np=total\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "not mutated when branch is false",
			src:         "- var total = 10\nif Flag\n  - total += 5\np=total\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// --- Deferrals ---

// TestCodegenUnbufferedMutationStringConcatDeferred asserts a `+=` whose RHS
// is a non-numeric string literal is deferred, not guessed at: the
// interpreter branches into string concatenation for a non-numeric RHS
// (runtime.go), a different value domain from the numeric mutation this
// slice supports.
func TestCodegenUnbufferedMutationStringConcatDeferred(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\n- x += \"z\"\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", src)
	}
	if !strings.Contains(err.Error(), "mutation") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", src, err.Error())
	}
}

// TestCodegenUnbufferedMutationSubAssignNonNumericDeferred asserts a `-=`
// whose RHS is non-numeric is deferred: the interpreter itself ERRORS for
// this shape (a distinct outcome from string concatenation), so codegen
// must not guess at replicating that error path.
func TestCodegenUnbufferedMutationSubAssignNonNumericDeferred(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\n- x -= \"z\"\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", src)
	}
	if !strings.Contains(err.Error(), "mutation") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", src, err.Error())
	}
}

// TestCodegenUnbufferedMutationStringLocalDeferred asserts mutating a
// STRING-typed `- var` local (`x++`/`x--`/`x +=`/`x -=`) is still rejected:
// a Go string can't `++`, and this slice only lifts the deferral for a
// float64 var-local. This is TestCodegenUnbufferedMutationDeferred's exact
// corpus (codegen_unbuffered_test.go), which must keep passing unchanged.
func TestCodegenUnbufferedMutationStringLocalDeferred(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{name: "increment", src: "- var x = \"a\"\n- x++\np=x\n"},
		{name: "decrement", src: "- var x = \"a\"\n- x--\np=x\n"},
		{name: "add-assign", src: "- var x = \"a\"\n- x += \"b\"\np=x\n"},
		{name: "sub-assign", src: "- var x = \"a\"\n- x -= \"b\"\np=x\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genUnbufferedErr(t, tc.src)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "mutation") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenUnbufferedMutationBoolLocalDeferred asserts mutating a
// bool-typed `- var` local is rejected — a distinct typ mismatch from the
// string case above, but the same gate.
func TestCodegenUnbufferedMutationBoolLocalDeferred(t *testing.T) {
	t.Parallel()
	src := "- var x = Flag\n- x++\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", src)
	}
	if !strings.Contains(err.Error(), "mutation") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", src, err.Error())
	}
}

// TestCodegenUnbufferedMutationUndefinedVarDeferred asserts mutating a
// variable never bound by a `- var` at all is rejected. This is the case
// where the interpreter itself does something surprising (setVar silently
// CREATES the variable at 0 in the top scope frame rather than erroring —
// see the empirical evidence this doc comment cites), which is exactly why
// codegen must not guess: lookupScope misses, so it defers rather than
// emitting a Go compile error (an undeclared identifier) or a mis-render.
func TestCodegenUnbufferedMutationUndefinedVarDeferred(t *testing.T) {
	t.Parallel()
	src := "- y++\np=y\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", src)
	}
	if !strings.Contains(err.Error(), "mutation") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", src, err.Error())
	}
}

// TestCodegenUnbufferedMutationStructFieldTargetDeferred asserts mutating a
// bare struct field name (never a `- var` local at all) is rejected —
// lookupScope never finds a struct field, only a scope-bound local or
// each-loop item variable, so this is the same lookupScope-miss gate as the
// undefined-var case.
func TestCodegenUnbufferedMutationStructFieldTargetDeferred(t *testing.T) {
	t.Parallel()
	src := "- Count++\np=Count\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", src)
	}
	if !strings.Contains(err.Error(), "mutation") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", src, err.Error())
	}
}

// TestCodegenUnbufferedMutationComplexRHSDeferred asserts an arithmetic
// (non-bare) `+=` RHS is rejected: genNumericExpr only accepts a bare
// numeric literal or a bare field/local, mirroring its existing restriction
// for a `- var` assignment RHS — no new grammar is added for mutation.
func TestCodegenUnbufferedMutationComplexRHSDeferred(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\n- var y = 2\n- x += y * 2\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", src)
	}
	if !strings.Contains(err.Error(), "mutation") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", src, err.Error())
	}
}

// TestCodegenUnbufferedMutationNilReflectTypeDeferred asserts mutation under
// a nil Config.DataReflectType (type-blind mode) is rejected: with no type
// information, genUnbufferedAssign itself already refuses every `- var`
// declaration, so no float64 var-local can ever exist in scope to mutate —
// lookupScope always misses in type-blind mode.
func TestCodegenUnbufferedMutationNilReflectTypeDeferred(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\n- x++\np=x\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName: "gopug",
		FuncName:    "RenderOps",
		DataType:    "opsData",
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q) with nil DataReflectType: expected an error, got nil", src)
	}
}

// --- Regression ---

// TestCodegenUnbufferedMutationAssignPathUnaffected proves genUnbufferedAssign
// (the `- var x = <rhs>` binding/reassignment path) is completely unaffected
// by this numeric MUTATION slice: this is a same-name, same-type
// reassignment (`- var x = 6` re-declaring x, not a `++`/`--`/`+=`/`-=`
// mutation at all), handled by the reassignment path as a plain Go `=` to
// the existing local — a separate mechanism the mutation slice above never
// touches.
func TestCodegenUnbufferedMutationAssignPathUnaffected(t *testing.T) {
	t.Parallel()
	src := "- var x = 5\n- var x = 6\np=x\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}
