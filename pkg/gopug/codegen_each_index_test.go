package gopug

import (
	"strings"
	"testing"
)

// --- Byte-identical differentials (interpreter is the oracle) ---

// TestCodegenEachIndexBasic is the headline shape: `each v, i in Items`
// with the index variable read both from a dynamic attribute and a
// buffered `= v` value, byte-identical to the interpreter — the STRING
// model (i rendered as "0", "1", "2", …) proven by the attribute output
// alone.
func TestCodegenEachIndexBasic(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in Items\n    li(data-i=i)= v\n",
		data:        map[string]any{"Items": []any{"a", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
	})
}

// TestCodegenEachIndexInterpolation proves the index variable stringifies
// identically to the interpreter both through plain-text `#{i}`
// interpolation and through a backtick template-literal `${i}`.
func TestCodegenEachIndexInterpolation(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferentialBatch(t, []codegenUnbufferedCase{
		{
			name:        "plain-text interpolation",
			src:         "ul\n  each v, i in Items\n    li #{i}: #{v}\n",
			data:        map[string]any{"Items": []any{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
		{
			name:        "backtick template literal",
			src:         "ul\n  each v, i in Items\n    li= `${i}: ${v}`\n",
			data:        map[string]any{"Items": []any{"a", "b", "c"}},
			dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
		},
	})
}

// TestCodegenEachIndexTruthinessSkipsFirst is THE case that proves the
// index is modeled as a STRING, not a raw Go int: gopug.Truthy("0") is
// false, so `if i` must SKIP the if-body for the very first element (index
// 0) and take it for every later one. An int model happening to also skip
// index 0 via `!= 0` would not, on its own, distinguish the two models —
// see TestCodegenEachIndexTruthinessFaultInjection for the non-vacuous
// proof that this differential is actually exercising the generated
// output, not merely compiling it.
func TestCodegenEachIndexTruthinessSkipsFirst(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in Items\n    if i\n      li.yes= v\n    else\n      li.no= v\n",
		data:        map[string]any{"Items": []any{"a", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
	})
}

// TestCodegenEachIndexComparison proves a numeric-literal comparison
// against the index (`i == 0`, `i > 1`) is byte-identical to the
// interpreter's compareValues numeric-string coercion, across every
// branch of an if/else-if/else chain.
func TestCodegenEachIndexComparison(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src: "ul\n  each v, i in Items\n" +
			"    if i == 0\n      li.first= v\n" +
			"    else if i > 1\n      li.late= v\n" +
			"    else\n      li.mid= v\n",
		data:        map[string]any{"Items": []any{"a", "b", "c", "d"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c", "d"}}`,
	})
}

// TestCodegenEachIndexOnlyBody covers the unused-item-variable Go
// mechanics: the body reads the index but never the item, which — absent
// the each-index path's own unconditional blank-use of both range
// variables — would fail to compile ("v declared and not used").
func TestCodegenEachIndexOnlyBody(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in Items\n    li= i\n",
		data:        map[string]any{"Items": []any{"a", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
	})
}

// TestCodegenEachIndexItemOnlyBody is TestCodegenEachIndexOnlyBody's
// mirror image: the body reads the item but never the index, which would
// fail to compile ("i declared and not used") without the index
// variable's own unconditional blank-use.
func TestCodegenEachIndexItemOnlyBody(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in Items\n    li= v\n",
		data:        map[string]any{"Items": []any{"a", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
	})
}

// TestCodegenEachIndexEmptyCollection proves an empty collection still
// iterates zero times (no error, no output) with an index variable present,
// exactly as it does without one.
func TestCodegenEachIndexEmptyCollection(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v, i in Items\n    li= i\n",
		data:        map[string]any{"Items": []any{}},
		dataLiteral: `opsData{Items: []string{}}`,
	})
}

// --- Fault injection (non-vacuous differential proof) ---

// TestCodegenEachIndexTruthinessFaultInjection proves
// TestCodegenEachIndexTruthinessSkipsFirst is actually exercising the
// generated output's truthiness handling, not merely compiling it: a
// deliberately wrong expected value — the shape a buggy implementation
// that treated the index as always-truthy (or 1-based) would produce,
// rendering the FIRST element with the "yes" class instead of "no" — must
// not match what the generated code actually renders.
func TestCodegenEachIndexTruthinessFaultInjection(t *testing.T) {
	t.Parallel()
	src := "each v, i in Items\n  if i\n    p.yes= v\n  else\n    p.no= v\n"

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

	got := runGeneratedGo(t, generated, `opsData{Items: []string{"a", "b", "c"}}`)

	wantCorrect := `<p class="no">a</p><p class="yes">b</p><p class="yes">c</p>`
	if got != wantCorrect {
		t.Fatalf("generated output %q does not match the interpreter-matching expectation %q", got, wantCorrect)
	}

	wrongAlwaysTruthy := `<p class="yes">a</p><p class="yes">b</p><p class="yes">c</p>`
	if got == wrongAlwaysTruthy {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong always-truthy expectation %q", got, wrongAlwaysTruthy)
	}
}

// --- Deferrals (each a clean, distinct error) ---

// TestCodegenEachIndexArrayLiteralDeferred asserts an each-index over an
// array-literal collection is rejected with a clean, distinct error: only
// the field/slice-var collection path is supported for an index variable
// in this increment.
func TestCodegenEachIndexArrayLiteralDeferred(t *testing.T) {
	src := "each v, i in [1, 2, 3]\n  p=v\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an array-literal-index error, got nil", src)
	}
	if !strings.Contains(err.Error(), "array-literal") {
		t.Errorf("GenerateGo(%q): error %q does not describe the array-literal-index restriction", src, err.Error())
	}
}

// TestCodegenEachIndexElseDeferred asserts each/else with an index
// variable is still rejected exactly like each/else without one — the
// pre-existing EmptyBody guard fires unconditionally, before the index
// variable is ever inspected.
func TestCodegenEachIndexElseDeferred(t *testing.T) {
	src := "each v, i in Items\n  li= v\nelse\n  li.empty none\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an each/else error, got nil", src)
	}
	if !strings.Contains(err.Error(), "each/else") {
		t.Errorf("GenerateGo(%q): error %q does not describe the each/else restriction", src, err.Error())
	}
}

// --- Regression: no-index each and unrelated suites unchanged ---

// TestCodegenEachIndexNoIndexRegression proves the no-index `each` path
// (the ONLY path this increment's IndexVar-guard removal could plausibly
// disturb) is still byte-for-byte unchanged: a plain `each v in Items`
// still compiles and renders exactly as it did before this increment.
func TestCodegenEachIndexNoIndexRegression(t *testing.T) {
	t.Parallel()
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         "ul\n  each v in Items\n    li= v\n",
		data:        map[string]any{"Items": []any{"a", "b", "c"}},
		dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`,
	})
}
