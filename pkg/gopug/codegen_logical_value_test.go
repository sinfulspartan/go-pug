package gopug

import "testing"

// This file proves value-context `||`/`&&`/`!`/comparison — genOrValueExpr,
// genAndValueExpr, the `!` branch, and the FormatBool(genCondition)
// comparison delegation in genValueExpr. Every case is a differential test
// against Compile().Render (the oracle for both rendered output and any
// error), using the same opsData/runCodegenArithDifferential/
// runCodegenFallibleErrorDifferential machinery codegen_arith_test.go and
// codegen_fallible_test.go already established.

// TestCodegenLogicalValueOrDefaultIdiom proves the classic `name || "anon"`
// default-value idiom: `||` returns the LEFT VALUE (not merely "true") when
// left is truthy, and the right value when left is falsy — in a buffered
// `= expr`, a `#{}` interpolation, and a dynamic attribute value (which
// routes through genValueExpr exactly like interpolation does).
func TestCodegenLogicalValueOrDefaultIdiom(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "buffered code, left falsy: returns the right value",
			src:         "p= Name || \"anon\"\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
		{
			name:        "buffered code, left truthy: returns the left value unchanged",
			src:         "p= Name || \"anon\"\n",
			data:        map[string]any{"Name": "bob"},
			dataLiteral: `opsData{Name: "bob"}`,
		},
		{
			name:        "interpolation, left falsy",
			src:         "p #{Name || \"anon\"}\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
		{
			name:        "interpolation, left truthy",
			src:         "p #{Name || \"anon\"}\n",
			data:        map[string]any{"Name": "bob"},
			dataLiteral: `opsData{Name: "bob"}`,
		},
		{
			name:        "dynamic attribute value, left falsy",
			src:         "a(data-x=Name || \"fallback\")\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
		{
			name:        "dynamic attribute value, left truthy",
			src:         "a(data-x=Name || \"fallback\")\n",
			data:        map[string]any{"Name": "bob"},
			dataLiteral: `opsData{Name: "bob"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLogicalValueAndValue proves `&&`'s value semantics: it returns
// the LITERAL STRING "false" (not "" and not the left operand's own value)
// when left is falsy, and the right operand's value, unchanged, when left is
// truthy.
func TestCodegenLogicalValueAndValue(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "left falsy (empty string): literal \"false\", not \"\"",
			src:         "p= Name && \"yes\"\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
		{
			name:        "left truthy: returns the right value",
			src:         "p= Name && \"yes\"\n",
			data:        map[string]any{"Name": "x"},
			dataLiteral: `opsData{Name: "x"}`,
		},
		{
			name:        "left falsy (the string \"0\"): literal \"false\", not the left value \"0\"",
			src:         "p= Str1 && Str2\n",
			data:        map[string]any{"Str1": "0", "Str2": "y"},
			dataLiteral: `opsData{Str1: "0", Str2: "y"}`,
		},
		{
			name:        "left truthy: returns the right value even when the right value is itself empty",
			src:         "p= Str1 && Str2\n",
			data:        map[string]any{"Str1": "x", "Str2": ""},
			dataLiteral: `opsData{Str1: "x", Str2: ""}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLogicalValueNot proves `!`'s value semantics: it returns the
// strings "true"/"false" from gopug.Not(genValueExpr(inner)) — for both a
// falsy/truthy string field and a bool field's own stringify.
func TestCodegenLogicalValueNot(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "! of a falsy string field",
			src:         "p= !Name\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
		{
			name:        "! of a truthy string field",
			src:         "p= !Name\n",
			data:        map[string]any{"Name": "x"},
			dataLiteral: `opsData{Name: "x"}`,
		},
		{
			name:        "! of a true bool field, in interpolation",
			src:         "p #{!Flag}\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "! of a false bool field, in interpolation",
			src:         "p #{!Flag}\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLogicalValueComparison is the byte-identical proof for the
// strconv.FormatBool(genCondition(expr)) delegation: a numeric comparison and
// a string-equality comparison, both in value context, must produce exactly
// the same "true"/"false" string the interpreter's own value-context
// compareValues-based comparison does.
func TestCodegenLogicalValueComparison(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "numeric comparison, true",
			src:         "p #{Count > 3}\n",
			data:        map[string]any{"Count": 5},
			dataLiteral: "opsData{Count: 5}",
		},
		{
			name:        "numeric comparison, false",
			src:         "p #{Count > 3}\n",
			data:        map[string]any{"Count": 2},
			dataLiteral: "opsData{Count: 2}",
		},
		{
			name:        "string equality, true",
			src:         "p #{Name == \"x\"}\n",
			data:        map[string]any{"Name": "x"},
			dataLiteral: `opsData{Name: "x"}`,
		},
		{
			name:        "string equality, false",
			src:         "p #{Name == \"x\"}\n",
			data:        map[string]any{"Name": "y"},
			dataLiteral: `opsData{Name: "y"}`,
		},
		{
			name:        "two numeric fields of the same type, equal",
			src:         "p= Count == Zero\n",
			data:        map[string]any{"Count": 0, "Zero": 0},
			dataLiteral: "opsData{Count: 0, Zero: 0}",
		},
		{
			name:        "two numeric fields of the same type, not equal",
			src:         "p= Count == Zero\n",
			data:        map[string]any{"Count": 5, "Zero": 0},
			dataLiteral: "opsData{Count: 5, Zero: 0}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLogicalValueOrShortCircuitFallible is the headline correctness
// proof: `||`'s right operand is a fallible `/` expression, and it must NEVER
// be evaluated (and so never error) when the left operand is already truthy
// — matching Runtime.evaluateExpr's own short-circuit `||` branch exactly.
func TestCodegenLogicalValueOrShortCircuitFallible(t *testing.T) {
	t.Run("truthy left: right is never evaluated, no error", func(t *testing.T) {
		runCodegenArithDifferential(t, codegenArithCase{
			name:        "truthy left short-circuits a division by zero on the right",
			src:         "p= Name || Count / Zero\n",
			data:        map[string]any{"Name": "x", "Count": 10, "Zero": 0},
			dataLiteral: `opsData{Name: "x", Count: 10, Zero: 0}`,
		})
	})
	t.Run("falsy left: right is evaluated and its division-by-zero error propagates", func(t *testing.T) {
		runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
			name:        "falsy left evaluates the right, which errors",
			src:         "p= Name || Count / Zero\n",
			data:        map[string]any{"Name": "", "Count": 10, "Zero": 0},
			dataLiteral: `opsData{Name: "", Count: 10, Zero: 0}`,
		})
	})
}

// TestCodegenLogicalValueAndShortCircuitFallible mirrors the `||` headline
// proof for `&&`: the right operand's division by zero must never execute
// (and never error) when the left operand is already falsy, since `&&`
// returns the literal "false" without evaluating right at all in that case.
func TestCodegenLogicalValueAndShortCircuitFallible(t *testing.T) {
	t.Run("falsy left: right is never evaluated, no error, returns literal false", func(t *testing.T) {
		runCodegenArithDifferential(t, codegenArithCase{
			name:        "falsy left short-circuits a division by zero on the right",
			src:         "p= Name && Count / Zero\n",
			data:        map[string]any{"Name": "", "Count": 10, "Zero": 0},
			dataLiteral: `opsData{Name: "", Count: 10, Zero: 0}`,
		})
	})
	t.Run("truthy left: right is evaluated and its division-by-zero error propagates", func(t *testing.T) {
		runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
			name:        "truthy left evaluates the right, which errors",
			src:         "p= Name && Count / Zero\n",
			data:        map[string]any{"Name": "x", "Count": 10, "Zero": 0},
			dataLiteral: `opsData{Name: "x", Count: 10, Zero: 0}`,
		})
	})
}

// TestCodegenLogicalValueOrFallibleLeftErrors proves a fallible LEFT operand
// of `||` is unconditionally evaluated (not short-circuited — only the RIGHT
// operand is conditional), so its error propagates regardless of what the
// right operand is.
func TestCodegenLogicalValueOrFallibleLeftErrors(t *testing.T) {
	runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
		name:        "fallible left operand of || errors before the right is ever considered",
		src:         "p= Count / Zero || \"x\"\n",
		data:        map[string]any{"Count": 10, "Zero": 0},
		dataLiteral: "opsData{Count: 10, Zero: 0}",
	})
}

// TestCodegenLogicalValueNesting proves genValueExpr splits a chained/mixed
// logical expression on the same top-level operator, in the same precedence
// order, that Runtime.evaluateExpr does — every case is asserted against the
// interpreter's own Render output, not a hand-computed "expected" value,
// since the two engines' precedence choice is exactly what's under test.
func TestCodegenLogicalValueNesting(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "left-associative || chain, first operand truthy",
			src:         "p= Str1 || Str2 || Slug\n",
			data:        map[string]any{"Str1": "a", "Str2": "b", "Slug": "c"},
			dataLiteral: `opsData{Str1: "a", Str2: "b", Slug: "c"}`,
		},
		{
			name:        "left-associative || chain, first two falsy",
			src:         "p= Str1 || Str2 || Slug\n",
			data:        map[string]any{"Str1": "", "Str2": "false", "Slug": "c"},
			dataLiteral: `opsData{Str1: "", Str2: "false", Slug: "c"}`,
		},
		{
			name:        "|| before && precedence, left truthy short-circuits the && entirely",
			src:         "p= Str1 || Str2 && Slug\n",
			data:        map[string]any{"Str1": "a", "Str2": "", "Slug": "c"},
			dataLiteral: `opsData{Str1: "a", Str2: "", Slug: "c"}`,
		},
		{
			name:        "|| before && precedence, left falsy falls through to (Str2 && Slug)",
			src:         "p= Str1 || Str2 && Slug\n",
			data:        map[string]any{"Str1": "", "Str2": "b", "Slug": "c"},
			dataLiteral: `opsData{Str1: "", Str2: "b", Slug: "c"}`,
		},
		{
			name:        "! wraps a parenthesized || expression",
			src:         "p= !(Str1 || Str2)\n",
			data:        map[string]any{"Str1": "", "Str2": ""},
			dataLiteral: `opsData{Str1: "", Str2: ""}`,
		},
		{
			name:        "ternary branch is a logical value expression",
			src:         "p= Flag ? (Str1 || \"d\") : \"e\"\n",
			data:        map[string]any{"Flag": true, "Str1": ""},
			dataLiteral: `opsData{Flag: true, Str1: ""}`,
		},
		{
			name:        "logical operand is a parenthesized ternary",
			src:         "p= (Flag ? \"a\" : \"b\") || \"fallback\"\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
		{
			name:        "unparenthesized: ternary's false branch is itself a || expression",
			src:         "p= Flag ? Str1 : Str2 || \"fallback\"\n",
			data:        map[string]any{"Flag": false, "Str2": ""},
			dataLiteral: `opsData{Flag: false, Str2: ""}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLogicalValueInTemplateLiteral proves a logical value expression
// composes inside a `${}` template-literal interpolation, including the
// template's own error-swallowing quirk (verified for arithmetic in
// codegen_fallible_compose_test.go's TestCodegenFallibleComposeTemplateLiteralSwallowsError):
// a fallible logical part's division-by-zero error is discarded, rendering
// that segment empty rather than aborting the whole render.
func TestCodegenLogicalValueInTemplateLiteral(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "logical value expression as a template-literal ${} part",
			src:         "p= `x=${Str1 || \"d\"}`\n",
			data:        map[string]any{"Str1": ""},
			dataLiteral: `opsData{Str1: ""}`,
		},
		{
			name:        "fallible logical ${} part, truthy left short-circuits: no error",
			src:         "p= `x=${Name || Count / Zero}`\n",
			data:        map[string]any{"Name": "x", "Count": 10, "Zero": 0},
			dataLiteral: `opsData{Name: "x", Count: 10, Zero: 0}`,
		},
		{
			name:        "fallible logical ${} part, falsy left evaluates the right, which errors and is swallowed",
			src:         "p= `x=${Name || Count / Zero}`\n",
			data:        map[string]any{"Name": "", "Count": 10, "Zero": 0},
			dataLiteral: `opsData{Name: "", Count: 10, Zero: 0}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenLogicalValueTruthySet proves `||`/`&&`/`!` use gopug.Truthy's
// exact falsy set ("", "false", "0", "null", "undefined", "nil") rather than
// Go's own zero-value notion of falsy: the string "0" is falsy, so
// `"0" || "y"` evaluates the right operand and returns "y".
func TestCodegenLogicalValueTruthySet(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        `"0" is falsy under gopug.Truthy, so "0" || "y" returns "y"`,
		src:         "p= \"0\" || \"y\"\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}
