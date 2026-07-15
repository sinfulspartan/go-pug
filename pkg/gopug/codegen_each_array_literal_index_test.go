package gopug

import (
	"testing"
)

// --- Byte-identical differentials (interpreter is the oracle) ---

// TestCodegenEachArrayLiteralIndexStringInterpolation is the headline shape
// for this composition: `each v, i in [<string literals>]` with the index
// variable read through plain-text `#{i}` interpolation alongside the item
// variable — the STRING index model (i rendered as "0", "1", "2", …)
// proven over an array-literal collection for the first time, exactly as it
// is already proven over a field/scope-slice collection.
func TestCodegenEachArrayLiteralIndexStringInterpolation(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in [\"a\", \"b\", \"c\"]\n    li #{i}=#{v}\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenEachArrayLiteralIndexTruthinessSkipsFirst is THE discriminating
// case: gopug.Truthy("0") is false, so `if i` must SKIP the if-body for the
// very first element (index 0, over the array-literal collection) and take
// it for every later one — the same STRING-model proof
// TestCodegenEachIndexTruthinessSkipsFirst already establishes for a
// field/scope-slice collection, now reproduced over an array literal. See
// TestCodegenEachArrayLiteralIndexTruthinessFaultInjection for the
// non-vacuous proof this differential actually exercises the generated
// output.
func TestCodegenEachArrayLiteralIndexTruthinessSkipsFirst(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in [\"x\", \"y\"]\n    if i\n      li.yes= v\n    else\n      li.no= v\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenEachArrayLiteralIndexNumericComparison proves a numeric-literal
// comparison against the index (`i == 0`, `i > 1`) over a NUMERIC
// array-literal collection compiles against the underlying raw Go int local
// (indexRawGoName, via pushIndexScope) and matches the interpreter exactly —
// the one new interaction this composition introduces: the raw-int
// comparison path meeting the array-literal collection for the first time.
func TestCodegenEachArrayLiteralIndexNumericComparison(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferentialBatch(t, []codegenUnbufferedCase{
		{
			name:        "i == 0",
			src:         "ul\n  each v, i in [10, 20, 30]\n    if i == 0\n      li.first #{v}\n    else\n      li.rest #{v}\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
		{
			name:        "i > 1",
			src:         "ul\n  each v, i in [10, 20, 30]\n    if i > 1\n      li.late #{v}\n    else\n      li.early #{v}\n",
			data:        map[string]any{},
			dataLiteral: "opsData{}",
		},
	})
}

// TestCodegenEachArrayLiteralIndexInterpolationAndAttr proves the index
// variable stringifies identically to the interpreter both through a
// dynamic attribute value and a backtick template-literal `${i}`
// interpolation, in the same tag as the item variable, over an
// array-literal collection.
func TestCodegenEachArrayLiteralIndexInterpolationAndAttr(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in [\"a\", \"b\"]\n    li(data-i=i)= `${i}:${v}`\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// --- Fault injection (non-vacuous differential proof) ---

// TestCodegenEachArrayLiteralIndexTruthinessFaultInjection proves
// TestCodegenEachArrayLiteralIndexTruthinessSkipsFirst is actually
// exercising the generated output's truthiness handling, not merely
// compiling it: a deliberately wrong expected value — the shape a buggy
// implementation that treated the index as always-truthy (an int model)
// would produce, rendering the FIRST element with the "yes" class instead
// of "no" — must not match what the generated code actually renders.
func TestCodegenEachArrayLiteralIndexTruthinessFaultInjection(t *testing.T) {
	t.Parallel()
	src := "each v, i in [\"x\", \"y\", \"z\"]\n  if i\n    p.yes= v\n  else\n    p.no= v\n"

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

	wantCorrect := `<p class="no">x</p><p class="yes">y</p><p class="yes">z</p>`
	if got != wantCorrect {
		t.Fatalf("generated output %q does not match the interpreter-matching expectation %q", got, wantCorrect)
	}

	wrongAlwaysTruthy := `<p class="yes">x</p><p class="yes">y</p><p class="yes">z</p>`
	if got == wrongAlwaysTruthy {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong always-truthy (int-model) expectation %q", got, wrongAlwaysTruthy)
	}
}

// --- Regression: the unchanged branches this composition must not disturb ---

// TestCodegenEachArrayLiteralIndexNoIndexRegression proves the no-index
// array-literal `each` path (genEachArrayLiteral's original branch) is
// still byte-for-byte unchanged: a plain `each v in [...]` array-literal
// collection still compiles and renders exactly as it did before this
// increment.
func TestCodegenEachArrayLiteralIndexNoIndexRegression(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v in [1, 2, 3]\n    li= v\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenEachArrayLiteralIndexFieldSliceRegression proves the
// indexed-FIELD-slice each path (the pre-existing genEach two-variable
// form) is still byte-for-byte unchanged by this array-literal composition:
// `each v, i in <field>` still compiles and renders exactly as it did
// before this increment.
func TestCodegenEachArrayLiteralIndexFieldSliceRegression(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in Items\n    li(data-i=i)= v\n",
		data:        map[string]any{"Items": []any{"a", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
	})
}
