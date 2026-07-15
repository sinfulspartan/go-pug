package gopug

import (
	"strings"
	"testing"
)

// TestCodegenClassMapSortAndFilter proves the headline shape — a single bare
// field on its own resolving to a map, used as the whole class value
// (`div(class=classMap)`) — filters out a falsy-valued key and renders the
// remaining truthy keys in SORTED order, matching Runtime.renderTag's own
// reflect.Map branch (filter-by-value, sort.Strings the kept keys, join). A
// fault-injected unsorted expectation (the fixed alphabetical-reverse order
// a caller might wrongly guess map iteration would produce) must NOT match,
// evidencing the runtime sort.Strings call actually ran rather than the keys
// merely happening to land in source order.
func TestCodegenClassMapSortAndFilter(t *testing.T) {
	t.Parallel()
	src := `div(class=ClassFlags)` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{
			name:        "sort+filter",
			src:         src,
			data:        map[string]any{"ClassFlags": map[string]bool{"active": true, "error": false, "zebra": true, "apple": true}},
			dataLiteral: `opsData{ClassFlags: map[string]bool{"active": true, "error": false, "zebra": true, "apple": true}}`,
		},
	})
	got := assertConditionDiffResult(t, src, `opsData{ClassFlags: map[string]bool{"active": true, "error": false, "zebra": true, "apple": true}}`, results[0])
	const want = `<div class="active apple zebra"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (truthy keys sorted, falsy key dropped)", got, want)
	}

	const wrongUnsorted = `<div class="zebra active apple"></div>`
	if got == wrongUnsorted {
		t.Fatalf("codegen output %q matches an unsorted ordering %q; sort.Strings does not appear to have run", got, wrongUnsorted)
	}
}

// TestCodegenClassMapValueTruthiness proves the per-entry filter tests the
// VALUE's fmt.Sprintf("%v", …) form through gopug.Truthy — the exported
// twin of the interpreter's isTruthy — across an int-valued map (0 is
// falsy), a string-valued map (empty string is falsy), and a bool map where
// every value is false (yielding an empty class list, not an error).
func TestCodegenClassMapValueTruthiness(t *testing.T) {
	t.Parallel()
	intSrc := `div(class=Counts)` + "\n"
	strSrc := `div(class=Meta)` + "\n"
	allFalseSrc := `div(class=ClassFlags)` + "\n"
	cases := []classTernaryDiffCase{
		{name: "int value truthy", src: intSrc, data: map[string]any{"Counts": map[string]int{"a": 0, "b": 1}}, dataLiteral: `opsData{Counts: map[string]int{"a": 0, "b": 1}}`},
		{name: "string value truthy", src: strSrc, data: map[string]any{"Meta": map[string]string{"a": "", "b": "x"}}, dataLiteral: `opsData{Meta: map[string]string{"a": "", "b": "x"}}`},
		{name: "bool all false", src: allFalseSrc, data: map[string]any{"ClassFlags": map[string]bool{"a": false, "b": false}}, dataLiteral: `opsData{ClassFlags: map[string]bool{"a": false, "b": false}}`},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="b"></div>` {
		t.Fatalf("int value truthy: codegen output %q, want %q", got, `<div class="b"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class="b"></div>` {
		t.Fatalf("string value truthy: codegen output %q, want %q", got, `<div class="b"></div>`)
	}
	got = assertConditionDiffResult(t, cases[2].src, cases[2].dataLiteral, results[2])
	if got != `<div class=""></div>` {
		t.Fatalf("bool all false: codegen output %q, want %q", got, `<div class=""></div>`)
	}
}

// TestCodegenClassMapNonStringKeySort proves a non-string key type is
// stringified with fmt.Sprintf("%v", …) exactly like the interpreter's own
// fmt.Sprintf("%v", mk.Interface()), and the resulting keys are sorted as
// STRINGS, not by their underlying numeric order: three all-truthy int keys
// 1, 2, and 10 must render as "1 10 2" (string sort), not "1 2 10" (numeric
// sort) — a fault-injected numeric-order expectation must NOT match.
func TestCodegenClassMapNonStringKeySort(t *testing.T) {
	t.Parallel()
	src := `div(class=IntFlags)` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{
			name:        "all truthy int keys",
			src:         src,
			data:        map[string]any{"IntFlags": map[int]bool{1: true, 2: true, 10: true}},
			dataLiteral: `opsData{IntFlags: map[int]bool{1: true, 2: true, 10: true}}`,
		},
	})
	got := assertConditionDiffResult(t, src, `opsData{IntFlags: map[int]bool{1: true, 2: true, 10: true}}`, results[0])
	const want = `<div class="1 10 2"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (keys sorted as strings)", got, want)
	}
	const wrongNumericOrder = `<div class="1 2 10"></div>`
	if got == wrongNumericOrder {
		t.Fatalf("codegen output %q matches the numeric-order form %q; keys are being sorted numerically instead of as strings", got, wrongNumericOrder)
	}

	filteredResults := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{
			name:        "one falsy int key",
			src:         src,
			data:        map[string]any{"IntFlags": map[int]bool{1: true, 2: false, 10: true}},
			dataLiteral: `opsData{IntFlags: map[int]bool{1: true, 2: false, 10: true}}`,
		},
	})
	got = assertConditionDiffResult(t, src, `opsData{IntFlags: map[int]bool{1: true, 2: false, 10: true}}`, filteredResults[0])
	if got != `<div class="1 10"></div>` {
		t.Fatalf("one falsy int key: codegen output %q, want %q", got, `<div class="1 10"></div>`)
	}
}

// TestCodegenClassMapEmptyAndNil proves an empty (non-nil) map and a nil map
// both collapse to an empty class list, matching the interpreter's own
// rv.MapKeys() returning nothing for either.
func TestCodegenClassMapEmptyAndNil(t *testing.T) {
	t.Parallel()
	src := `div(class=ClassFlags)` + "\n"
	cases := []classTernaryDiffCase{
		{name: "empty map", src: src, data: map[string]any{"ClassFlags": map[string]bool{}}, dataLiteral: `opsData{ClassFlags: map[string]bool{}}`},
		{name: "nil map", src: src, data: map[string]any{"ClassFlags": map[string]bool(nil)}, dataLiteral: `opsData{}`},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class=""></div>` {
		t.Fatalf("empty map: codegen output %q, want %q", got, `<div class=""></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class=""></div>` {
		t.Fatalf("nil map: codegen output %q, want %q", got, `<div class=""></div>`)
	}
}

// TestCodegenClassMapEscaping proves a truthy key containing HTML-special
// characters is entity-escaped through gopug.EscapeAttr, not written raw,
// exactly like Runtime.renderTag's own htmlEscapeAttr call. A fault-injected
// raw (unescaped) expectation must NOT match, proving EscapeAttr is actually
// applied.
func TestCodegenClassMapEscaping(t *testing.T) {
	t.Parallel()
	src := `div(class=ClassFlags)` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{
			name:        "escaped",
			src:         src,
			data:        map[string]any{"ClassFlags": map[string]bool{"a&b": true, `c"d`: true}},
			dataLiteral: `opsData{ClassFlags: map[string]bool{"a&b": true, "c\"d": true}}`,
		},
	})
	got := assertConditionDiffResult(t, src, `opsData{ClassFlags: map[string]bool{"a&b": true, "c\"d": true}}`, results[0])
	const want = `<div class="a&amp;b c&quot;d"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (the \"&\" and the quote must be entity-escaped)", got, want)
	}

	const wrongUnescaped = `<div class="a&b c"d"></div>`
	if got == wrongUnescaped {
		t.Fatalf("codegen output %q matches the raw unescaped string %q; gopug.EscapeAttr is not being applied", got, wrongUnescaped)
	}
}

// TestCodegenClassMapEmptyStringKeyLeadingSpace proves the strings.Join-not-
// gopug.JoinClasses discriminator: a truthy key that stringifies to the
// empty string is kept as an empty token (not dropped), so joining it ahead
// of another kept key produces a LEADING space, exactly matching the
// interpreter's own strings.Join(activeClasses, " ") call on the same sorted
// key list ("" sorts before "b"). A fault-injected collapsed expectation
// (what a wrongly-substituted gopug.JoinClasses, which drops empty tokens,
// would produce instead) must NOT match.
func TestCodegenClassMapEmptyStringKeyLeadingSpace(t *testing.T) {
	t.Parallel()
	src := `div(class=ClassFlags)` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{
			name:        "empty-string truthy key",
			src:         src,
			data:        map[string]any{"ClassFlags": map[string]bool{"": true, "b": true}},
			dataLiteral: `opsData{ClassFlags: map[string]bool{"": true, "b": true}}`,
		},
	})
	got := assertConditionDiffResult(t, src, `opsData{ClassFlags: map[string]bool{"": true, "b": true}}`, results[0])
	const want = `<div class=" b"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (the empty-string key must produce a leading space)", got, want)
	}

	const collapsed = `<div class="b"></div>`
	if got == collapsed {
		t.Fatalf("codegen output %q matches the collapsed form %q; the empty-string key is being dropped, meaning strings.Join was not actually used", got, collapsed)
	}
}

// genClassMapErr parses and GenerateGoes src against opsData (unless noType
// is true, in which case Config.DataReflectType is left nil), always
// returning GenerateGo's error rather than fatally failing the test — used
// by the deferral cases below.
func genClassMapErr(t *testing.T, src string, noType bool) error {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	cfg := Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	}
	if noType {
		cfg.DataReflectType = nil
	}
	_, err = GenerateGo(ast, cfg)
	return err
}

// TestCodegenClassMapDeferrals proves each distinct DEFER path this
// increment must NOT try to reproduce, even though the interpreter itself
// renders both of them successfully: a multi-token class value formed by
// merging a shorthand class prefix with a dynamic map field
// (`div.card(class=ClassFlags)` -> trimmed `card ClassFlags`, which the
// interpreter resolves through its own resolveClassTokenList flatten path —
// a different code path this increment does not reproduce, and which
// stringifies the whole map with a bare Go %v verb rather than filtering and
// sorting it), and a nil Config.DataReflectType (no type information to
// resolve the bare field against at all).
func TestCodegenClassMapDeferrals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		src    string
		noType bool
	}{
		{name: "multi-token shorthand-prefix + map field", src: `div.card(class=ClassFlags)` + "\n"},
		{name: "nil rootType", src: `div(class=ClassFlags)` + "\n", noType: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genClassMapErr(t, tc.src, tc.noType)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenClassMapDeferralsAreDistinct proves the deferral cases above
// produce genuinely different error messages, not one shared catch-all.
func TestCodegenClassMapDeferralsAreDistinct(t *testing.T) {
	t.Parallel()
	multiToken := genClassMapErr(t, `div.card(class=ClassFlags)`+"\n", false)
	nilType := genClassMapErr(t, `div(class=ClassFlags)`+"\n", true)

	msgs := map[string]error{
		"multi-token shorthand + map": multiToken,
		"nil rootType":                nilType,
	}
	seen := map[string]string{}
	for name, err := range msgs {
		if err == nil {
			t.Fatalf("%s: expected an error, got nil", name)
		}
		if other, dup := seen[err.Error()]; dup {
			t.Errorf("%s and %s produced the identical error message %q; expected each deferral to describe its own construct", name, other, err.Error())
		}
		seen[err.Error()] = name
	}
}

// TestCodegenClassMapRegressionUnchangedPaths proves the pre-existing
// class-value paths this increment must leave untouched still work exactly
// as before: the single-token slice-field path, the single-token
// string-field path, and the multi-token gopug.JoinClasses path.
func TestCodegenClassMapRegressionUnchangedPaths(t *testing.T) {
	t.Parallel()
	sliceFieldSrc := `div(class=Items)` + "\n"
	stringFieldSrc := `div(class=Name)` + "\n"
	multiTokenSrc := `div.base(class=Name)` + "\n"
	cases := []classTernaryDiffCase{
		{name: "single-token slice field", src: sliceFieldSrc, data: map[string]any{"Items": []string{"a", "b"}}, dataLiteral: `opsData{Items: []string{"a", "b"}}`},
		{name: "single-token string field", src: stringFieldSrc, data: map[string]any{"Name": "solo"}, dataLiteral: `opsData{Name: "solo"}`},
		{name: "multi-token JoinClasses", src: multiTokenSrc, data: map[string]any{"Name": "extra"}, dataLiteral: `opsData{Name: "extra"}`},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="a b"></div>` {
		t.Fatalf("single-token slice field: codegen output %q, want %q", got, `<div class="a b"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class="solo"></div>` {
		t.Fatalf("single-token string field: codegen output %q, want %q", got, `<div class="solo"></div>`)
	}
	got = assertConditionDiffResult(t, cases[2].src, cases[2].dataLiteral, results[2])
	if got != `<div class="base extra"></div>` {
		t.Fatalf("multi-token JoinClasses: codegen output %q, want %q", got, `<div class="base extra"></div>`)
	}
}

// TestCodegenClassMapImportGating proves a tag using ONLY a single-token
// map-valued class attribute compiles with exactly the imports it needs —
// "fmt" (for the per-key/value Sprintf), "strings" (for Join), "sort" (for
// the runtime sort.Strings call), and "gopug" (for Truthy/EscapeAttr) — but
// not "html"/"strconv", which nothing in this template's generated code
// calls. A separate, sibling non-map template must NOT import "sort" at
// all, proving needsSort does not leak into templates that never use a
// map-valued class.
func TestCodegenClassMapImportGating(t *testing.T) {
	t.Parallel()
	src := `div(class=ClassFlags)` + "\n"
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
	genStr := string(generated)

	for _, want := range []string{`"fmt"`, `"strings"`, `"sort"`, `"github.com/sinfulspartan/go-pug/pkg/gopug"`} {
		if !strings.Contains(genStr, want) {
			t.Errorf("GenerateGo(%q) does not import %s:\n%s", src, want, genStr)
		}
	}
	if strings.Contains(genStr, "\"html\"") {
		t.Errorf("GenerateGo(%q) imports \"html\" even though nothing in the template calls html.EscapeString:\n%s", src, genStr)
	}
	if strings.Contains(genStr, "\"strconv\"") {
		t.Errorf("GenerateGo(%q) imports \"strconv\" even though nothing in the template needs it:\n%s", src, genStr)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"ClassFlags": map[string]bool{"a": true, "b": true}})
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, `opsData{ClassFlags: map[string]bool{"a": true, "b": true}}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}

	nonMapSrc := `div(class=Items)` + "\n"
	nonMapAst, err := Parse(nonMapSrc, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", nonMapSrc, err)
	}
	nonMapGenerated, err := GenerateGo(nonMapAst, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", nonMapSrc, err)
	}
	nonMapGenStr := string(nonMapGenerated)
	if strings.Contains(nonMapGenStr, "\"sort\"") {
		t.Errorf("GenerateGo(%q) imports \"sort\" even though its class value is a slice field, not a map field; needsSort has leaked:\n%s", nonMapSrc, nonMapGenStr)
	}
}
