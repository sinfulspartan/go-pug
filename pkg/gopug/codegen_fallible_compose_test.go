package gopug

import "testing"

// This file proves fallibility bubbles correctly through composition: an
// arithmetic combiner or a template literal with a fallible `/`/`%` operand
// (Pattern 1, up-front left-to-right extraction — genArithCombinerIIFE), and
// a ternary with a fallible branch (Pattern 2, short-circuit in-branch
// extraction — genTernaryValueExpr). Every case is a differential test
// against Compile().Render, the oracle for both the rendered output and any
// error message.

// TestCodegenFallibleComposeArithmeticSuccess proves a fallible `/`/`%`
// operand composes into `+`/`-`/`*`, another `/`/`%`, and nests to any
// depth, producing the same successful output as the interpreter when no
// divisor is zero.
func TestCodegenFallibleComposeArithmeticSuccess(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "fallible left operand of +",
			src:         "p= Count / B + 1\n",
			data:        map[string]any{"Count": 10, "B": uint8(2)},
			dataLiteral: "opsData{Count: 10, B: 2}",
		},
		{
			name:        "fallible operand parenthesized then multiplied",
			src:         "p= (Count / B) * Age\n",
			data:        map[string]any{"Count": 10, "B": uint8(2), "Age": int8(3)},
			dataLiteral: "opsData{Count: 10, B: 2, Age: 3}",
		},
		{
			name:        "two fallible operands of +",
			src:         "p= Count / B + BigInt / Age\n",
			data:        map[string]any{"Count": 10, "B": uint8(2), "BigInt": int64(20), "Age": int8(4)},
			dataLiteral: "opsData{Count: 10, B: 2, BigInt: 20, Age: 4}",
		},
		{
			name:        "fallible ${} part inside an interpolation",
			src:         "p #{Count / B + 1}\n",
			data:        map[string]any{"Count": 10, "B": uint8(2)},
			dataLiteral: "opsData{Count: 10, B: 2}",
		},
		{
			name:        "nested fallible operand: outer divisor is itself a division",
			src:         "p= Count / (BigInt / Age)\n",
			data:        map[string]any{"Count": 12, "BigInt": int64(6), "Age": int8(2)},
			dataLiteral: "opsData{Count: 12, BigInt: 6, Age: 2}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenFallibleComposeTemplateLiteralSuccess proves a fallible `/`/`%`
// `${}` part inside a template literal composes and produces the same
// concatenated output as the interpreter when the division succeeds.
func TestCodegenFallibleComposeTemplateLiteralSuccess(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "template literal with a fallible ${} part that succeeds",
		src:         "p= `r=${Count / B}`\n",
		data:        map[string]any{"Count": 10, "B": uint8(2)},
		dataLiteral: "opsData{Count: 10, B: 2}",
	})
}

// TestCodegenFallibleComposeErrorParity proves that a fallible operand's
// division/modulo-by-zero error propagates out through arithmetic
// composition (up-front extraction, Pattern 1) with the identical error
// message the interpreter itself returns.
func TestCodegenFallibleComposeErrorParity(t *testing.T) {
	t.Parallel()
	cases := []codegenFallibleErrorCase{
		{
			name:        "fallible left operand of + errors",
			src:         "p= Count / Zero + 1\n",
			data:        map[string]any{"Count": 10, "Zero": 0},
			dataLiteral: "opsData{Count: 10, Zero: 0}",
		},
		{
			name:        "left operand ok, right fallible operand of + errors",
			src:         "p= Count / B + BigInt / Zero\n",
			data:        map[string]any{"Count": 10, "B": uint8(2), "BigInt": int64(20), "Zero": 0},
			dataLiteral: "opsData{Count: 10, B: 2, BigInt: 20, Zero: 0}",
		},
		{
			name:        "fallible % operand of + errors",
			src:         "p= Count % Zero + 1\n",
			data:        map[string]any{"Count": 10, "Zero": 0},
			dataLiteral: "opsData{Count: 10, Zero: 0}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenFallibleErrorDifferential(t, tc)
		})
	}
}

// TestCodegenFallibleComposeLeftBeforeRightErrorOrder proves the interpreter
// and generated code agree on operand evaluation order for a composed
// combiner with a fallible operand on each side: the LEFT operand is
// extracted (and, on error, aborts) before the right one is ever evaluated,
// matching Runtime.evaluateExpr's own eager left-then-right combiner
// branches exactly. The second case makes this structurally provable, not
// just message-equal: the left division and the right modulo would produce
// DIFFERENT error messages, so if generated code evaluated right before
// left, the observed error would say "modulo by zero" instead of "division
// by zero" — asserting the exact interpreter message pins the order.
func TestCodegenFallibleComposeLeftBeforeRightErrorOrder(t *testing.T) {
	t.Parallel()
	cases := []codegenFallibleErrorCase{
		{
			name:        "both operands are zero divisors; left's error wins",
			src:         "p= Count / Zero + Price / 0\n",
			data:        map[string]any{"Count": 10, "Zero": 0, "Price": 5.0},
			dataLiteral: "opsData{Count: 10, Zero: 0, Price: 5}",
		},
		{
			name:        "left division errors, right modulo would error differently if evaluated",
			src:         "p= Count / Zero + Price % 0\n",
			data:        map[string]any{"Count": 10, "Zero": 0, "Price": 5.0},
			dataLiteral: "opsData{Count: 10, Zero: 0, Price: 5}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenFallibleErrorDifferential(t, tc)
		})
	}
}

// TestCodegenFallibleComposeTemplateLiteralSwallowsError proves an
// empirically-verified quirk of Runtime.evaluateExpr's own template-literal
// walk: a `${}` part's division/modulo-by-zero error is DISCARDED, not
// propagated — the part renders as the empty string and Render still
// succeeds. This is a real divergence from the "errors propagate through
// composition" rule the other combiners follow, verified directly against
// the interpreter (not assumed), and codegen's genFallibleTemplatePart
// reproduces it exactly.
func TestCodegenFallibleComposeTemplateLiteralSwallowsError(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "template literal with a fallible ${} part that errors renders that segment empty, with no overall error",
		src:         "p= `r=${Count / Zero}`\n",
		data:        map[string]any{"Count": 10, "Zero": 0},
		dataLiteral: "opsData{Count: 10, Zero: 0}",
	})
}

// TestCodegenFallibleComposeNonNumeric proves the non-numeric composition
// case matches the interpreter's ACTUAL output rather than an assumed one:
// Div of two non-numeric operands yields "" with no error, then Add("", "1")
// concatenates (toFloat("") fails, so Add falls to string concatenation) to
// "1" — verified directly against Compile().Render before being encoded as
// this test's expectation.
func TestCodegenFallibleComposeNonNumeric(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "non-numeric division composed with + still succeeds with no error",
		src:         "p= Str1 / Str2 + 1\n",
		data:        map[string]any{"Str1": "foo", "Str2": "bar"},
		dataLiteral: `opsData{Str1: "foo", Str2: "bar"}`,
	})
}

// TestCodegenFallibleComposeTernaryShortCircuit is the headline correctness
// proof for Pattern 2: only the TAKEN ternary branch's fallible extraction
// ever executes. An untaken branch containing a division by zero must NOT
// error — the interpreter's own ternary (runtime.go:2128) only calls
// evaluateExpr on the taken branch, and genTernaryValueExpr's IIFE puts each
// branch's extraction inside that branch's own if/else arm to match.
func TestCodegenFallibleComposeTernaryShortCircuit(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "false condition: untaken true branch divides by zero, never evaluated",
			src:         "p= Flag ? Count / Zero : \"safe\"\n",
			data:        map[string]any{"Flag": false, "Count": 10, "Zero": 0},
			dataLiteral: "opsData{Flag: false, Count: 10, Zero: 0}",
		},
		{
			name:        "true condition: untaken false branch divides by zero, never evaluated",
			src:         "p= Flag ? \"safe\" : Count / Zero\n",
			data:        map[string]any{"Flag": true, "Count": 10, "Zero": 0},
			dataLiteral: "opsData{Flag: true, Count: 10, Zero: 0}",
		},
		{
			name:        "true condition: taken branch is fallible but the divisor is non-zero, succeeds",
			src:         "p= Flag ? Count / B : \"safe\"\n",
			data:        map[string]any{"Flag": true, "Count": 10, "B": uint8(2)},
			dataLiteral: "opsData{Flag: true, Count: 10, B: 2}",
		},
		{
			name:        "nested ternary: outer selects an inner ternary whose untaken branch would divide by zero",
			src:         "p= Flag ? (FlagB ? Price : Count / Zero) : BigInt\n",
			data:        map[string]any{"Flag": true, "FlagB": true, "Price": 3.5, "Count": 10, "Zero": 0, "BigInt": int64(99)},
			dataLiteral: "opsData{Flag: true, FlagB: true, Price: 3.5, Count: 10, Zero: 0, BigInt: 99}",
		},
		{
			name:        "ternary as a fallible operand of a combiner, taken branch total",
			src:         "p= (Flag ? Price : Count / Zero) + 1\n",
			data:        map[string]any{"Flag": true, "Price": 4.0, "Count": 10, "Zero": 0},
			dataLiteral: "opsData{Flag: true, Price: 4, Count: 10, Zero: 0}",
		},
		{
			name:        "fallible ternary inside a template literal ${} part, untaken branch",
			src:         "p= `x=${Flag ? \"safe\" : Count / Zero}`\n",
			data:        map[string]any{"Flag": true, "Count": 10, "Zero": 0},
			dataLiteral: "opsData{Flag: true, Count: 10, Zero: 0}",
		},
		{
			name:        "fallible ternary inside a template literal ${} part, taken branch succeeds",
			src:         "p= `x=${Flag ? Count / B : Price}`\n",
			data:        map[string]any{"Flag": true, "Count": 10, "B": uint8(2), "Price": 9.0},
			dataLiteral: "opsData{Flag: true, Count: 10, B: 2, Price: 9}",
		},
		{
			name:        "fallible ternary inside a template literal ${} part, taken branch itself errors: swallowed by the template, no overall error",
			src:         "p= `x=${Flag ? Count / Zero : Price}`\n",
			data:        map[string]any{"Flag": true, "Count": 10, "Zero": 0, "Price": 3.5},
			dataLiteral: "opsData{Flag: true, Count: 10, Zero: 0, Price: 3.5}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenFallibleComposeTernaryEagerTakenBranchErrors is the flip side
// of the short-circuit proof: when the TAKEN branch is fallible and its
// divisor is zero, both engines error identically — the taken branch is
// still evaluated eagerly, only the untaken one is skipped.
func TestCodegenFallibleComposeTernaryEagerTakenBranchErrors(t *testing.T) {
	t.Parallel()
	cases := []codegenFallibleErrorCase{
		{
			name:        "true condition selects the fallible branch, which errors",
			src:         "p= Flag ? Count / Zero : \"x\"\n",
			data:        map[string]any{"Flag": true, "Count": 10, "Zero": 0},
			dataLiteral: "opsData{Flag: true, Count: 10, Zero: 0}",
		},
		{
			name:        "false condition selects the fallible branch, which errors",
			src:         "p= Flag ? \"x\" : Count / Zero\n",
			data:        map[string]any{"Flag": false, "Count": 10, "Zero": 0},
			dataLiteral: "opsData{Flag: false, Count: 10, Zero: 0}",
		},
		{
			name:        "nested ternary: taken inner branch is fallible and errors",
			src:         "p= Flag ? (FlagB ? Count / Zero : Price) : BigInt\n",
			data:        map[string]any{"Flag": true, "FlagB": true, "Count": 10, "Zero": 0, "Price": 3.5, "BigInt": int64(99)},
			dataLiteral: "opsData{Flag: true, FlagB: true, Count: 10, Zero: 0, Price: 3.5, BigInt: 99}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenFallibleErrorDifferential(t, tc)
		})
	}
}
