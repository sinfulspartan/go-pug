package gopug

import (
	"testing"
)

// codegenArithCase is a differential test case for a value-context arithmetic
// expression: src is rendered through both GenerateGo (built and run as a
// standalone Go program via runGeneratedGo) and the interpreter's own
// Compile().Render, against the same data, and the two outputs must match
// exactly — the interpreter's Render output is always the oracle, never a
// hand-computed expectation.
type codegenArithCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

func runCodegenArithDifferential(t *testing.T, tc codegenArithCase) {
	t.Helper()

	ast, err := Parse(tc.src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", tc.src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", tc.src, err)
	}

	tmpl, err := Compile(tc.src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", tc.src, err)
	}
	want, err := tmpl.Render(tc.data)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, tc.dataLiteral)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, tc.src)
	}
}

// runCodegenArithDifferentialBatch is runCodegenArithDifferential generalized
// to a whole slice of cases: every case's GenerateGo output and interpreter
// oracle (Compile().Render) are prepared up front, then submitted to a
// SINGLE runDifferentialBatch call instead of one `go run` per case, cutting
// the dominant per-case cost (a fresh module build) down to one for the
// entire slice. Each case's own pass/fail is still reported through its own
// t.Run(tc.name, ...), matched to its batch result by index (prepared and
// cases share the same order), so a failure stays exactly as attributable as
// it was when run individually.
func runCodegenArithDifferentialBatch(t *testing.T, cases []codegenArithCase) {
	t.Helper()

	if len(cases) == 0 {
		return
	}

	type prepared struct {
		tc   codegenArithCase
		want string
	}

	var diffCases []diffCase
	var prep []prepared

	for _, tc := range cases {
		ast, err := Parse(tc.src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.src, err)
		}
		generated, err := GenerateGo(ast, Config{
			PackageName:     "main",
			FuncName:        "RenderOps",
			DataType:        "opsData",
			DataReflectType: opsDataReflectType,
		})
		if err != nil {
			t.Fatalf("GenerateGo(%q): %v", tc.src, err)
		}

		tmpl, err := Compile(tc.src, nil)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.src, err)
		}
		want, err := tmpl.Render(tc.data)
		if err != nil {
			t.Fatalf("interpreter Render: %v", err)
		}

		diffCases = append(diffCases, diffCase{name: tc.name, generated: generated, dataLiteral: tc.dataLiteral})
		prep = append(prep, prepared{tc: tc, want: want})
	}

	results := runDifferentialBatch(t, opsDataStructSrc, "RenderOps", diffCases)

	for i, p := range prep {
		t.Run(p.tc.name, func(t *testing.T) {
			result := results[i]
			if result.Err != "" {
				t.Fatalf("generated RenderOps(%q): unexpected error %q", p.tc.src, result.Err)
			}
			if result.Out != p.want {
				t.Errorf("codegen output %q does not match interpreter output %q for %q", result.Out, p.want, p.tc.src)
			}
		})
	}
}

// TestCodegenValueExprSubtraction proves gopug.Sub is wired into genValueExpr
// for a numeric field minus a literal, matching evaluateExpr's own `-`
// branch exactly.
func TestCodegenValueExprSubtraction(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "int field minus literal",
		src:         "p= Count - 2\n",
		data:        map[string]any{"Count": 10},
		dataLiteral: "opsData{Count: 10}",
	})
}

// TestCodegenValueExprMultiplication proves gopug.Mul is wired into
// genValueExpr for a numeric field times a literal, and for two numeric
// fields of different Go kinds multiplied together, matching evaluateExpr's
// own `*` branch exactly.
func TestCodegenValueExprMultiplication(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "int field times literal",
			src:         "p= Count * 3\n",
			data:        map[string]any{"Count": 10},
			dataLiteral: "opsData{Count: 10}",
		},
		{
			name:        "two numeric fields of different kinds",
			src:         "p= Count * Price\n",
			data:        map[string]any{"Count": 4, "Price": 2.5},
			dataLiteral: "opsData{Count: 4, Price: 2.5}",
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenValueExprArithmeticNonNumeric proves the Sub/Mul contract that
// distinguishes them from Add: a non-numeric operand pair produces the empty
// string rather than falling back to concatenation (the opposite direction
// of Add's "a"+"b" -> "ab" proof), and that this is exactly what codegen's
// gopug.Sub/gopug.Mul calls also produce.
func TestCodegenValueExprArithmeticNonNumeric(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "subtraction of non-numeric strings",
			src:         "p= Str1 - Str2\n",
			data:        map[string]any{"Str1": "a", "Str2": "b"},
			dataLiteral: `opsData{Str1: "a", Str2: "b"}`,
		},
		{
			name:        "multiplication of non-numeric strings",
			src:         "p= Str1 * Str2\n",
			data:        map[string]any{"Str1": "a", "Str2": "b"},
			dataLiteral: `opsData{Str1: "a", Str2: "b"}`,
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenValueExprArithmeticNumericStrings proves the toFloat path: two
// string fields holding numeric-looking text parse and combine numerically
// through both Sub and Mul, exactly like the interpreter's own toFloat
// disambiguation.
func TestCodegenValueExprArithmeticNumericStrings(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "subtraction of numeric-looking strings",
			src:         "p= Str1 - Str2\n",
			data:        map[string]any{"Str1": "5", "Str2": "3"},
			dataLiteral: `opsData{Str1: "5", Str2: "3"}`,
		},
		{
			name:        "multiplication of numeric-looking strings",
			src:         "p= Str1 * Str2\n",
			data:        map[string]any{"Str1": "5", "Str2": "3"},
			dataLiteral: `opsData{Str1: "5", Str2: "3"}`,
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenValueExprArithmeticPrecedence proves genValueExpr splits a
// mixed `-`/`+`/`*` expression on the same top-level operator, in the same
// order (subtraction before addition before multiplication), that
// evaluateExpr does — each case is asserted against the interpreter's own
// Render output, not a hand-computed "Go-native" expectation, since the two
// engines' precedence choice is exactly what's under test.
func TestCodegenValueExprArithmeticPrecedence(t *testing.T) {
	t.Parallel()
	data := map[string]any{"Count": 10, "Age": int8(4), "BigInt": int64(2)}
	dataLiteral := "opsData{Count: 10, Age: 4, BigInt: 2}"

	cases := []codegenArithCase{
		{name: "subtraction before addition", src: "p= Count - Age + BigInt\n", data: data, dataLiteral: dataLiteral},
		{name: "addition before trailing subtraction", src: "p= Count + Age - BigInt\n", data: data, dataLiteral: dataLiteral},
		{name: "subtraction before multiplication", src: "p= Count - Age * BigInt\n", data: data, dataLiteral: dataLiteral},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenValueExprArithmeticFloatFormatting proves a float64 field
// combined with a literal through `-` formats identically to evaluateExpr's
// strconv.FormatFloat(..., 'f', -1, 64) call.
func TestCodegenValueExprArithmeticFloatFormatting(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "float field minus a literal",
		src:         "p= Price - 0.5\n",
		data:        map[string]any{"Price": 9.75},
		dataLiteral: "opsData{Price: 9.75}",
	})
}

// TestCodegenValueExprArithmeticInTemplateLiteral proves a `*` expression
// composes correctly inside a `${...}` template-literal interpolation,
// exercising the same genTemplateLiteral -> genValueExpr recursion the
// earlier template-literal increment relies on.
func TestCodegenValueExprArithmeticInTemplateLiteral(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "multiplication nested in a template literal",
		src:         "p= `total ${Count * Price}`\n",
		data:        map[string]any{"Count": 3, "Price": 2.5},
		dataLiteral: "opsData{Count: 3, Price: 2.5}",
	})
}
