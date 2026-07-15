package gopug

import (
	"strings"
	"testing"
)

// TestCodegenClassSliceStringField proves the headline shape — a single bare
// field on its own resolving to a []string, used as the whole class value
// (`div(class=Items)`) — renders byte-identically to the interpreter across a
// populated slice, an empty (non-nil) slice, and a nil slice, all of which
// the interpreter's own reflect.Slice branch (runtime.go) treats as the
// space-joined element list, collapsing to an empty class="" once the
// element count is zero either way.
func TestCodegenClassSliceStringField(t *testing.T) {
	t.Parallel()
	src := `div(class=Items)` + "\n"
	cases := []classTernaryDiffCase{
		{name: "populated", src: src, data: map[string]any{"Items": []string{"a", "b", "c"}}, dataLiteral: `opsData{Items: []string{"a", "b", "c"}}`},
		{name: "empty slice", src: src, data: map[string]any{"Items": []string{}}, dataLiteral: `opsData{Items: []string{}}`},
		{name: "nil slice", src: src, data: map[string]any{"Items": []string(nil)}, dataLiteral: `opsData{}`},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="a b c"></div>` {
		t.Fatalf("populated: codegen output %q, want %q", got, `<div class="a b c"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class=""></div>` {
		t.Fatalf("empty slice: codegen output %q, want %q", got, `<div class=""></div>`)
	}
	got = assertConditionDiffResult(t, cases[2].src, cases[2].dataLiteral, results[2])
	if got != `<div class=""></div>` {
		t.Fatalf("nil slice: codegen output %q, want %q", got, `<div class=""></div>`)
	}
}

// TestCodegenClassSliceElementFormatting proves each supported element type
// stringifies through bare fmt.Sprintf's "%v" verb exactly like the
// interpreter's own fmt.Sprintf("%v", rv.Index(i).Interface()) — an int
// slice renders its plain decimal digits, a float64 slice renders Go's
// default shortest-round-trip float formatting (including a trailing whole
// number like 2 with no ".0"), and a bool slice renders the literal
// "true"/"false" words.
func TestCodegenClassSliceElementFormatting(t *testing.T) {
	t.Parallel()
	intSrc := `div(class=Nums)` + "\n"
	floatSrc := `div(class=Prices)` + "\n"
	boolSrc := `div(class=BoolItems)` + "\n"
	cases := []classTernaryDiffCase{
		{name: "int elements", src: intSrc, data: map[string]any{"Nums": []int{1, 2, 3}}, dataLiteral: `opsData{Nums: []int{1, 2, 3}}`},
		{name: "float elements", src: floatSrc, data: map[string]any{"Prices": []float64{1.5, 2, 3.25}}, dataLiteral: `opsData{Prices: []float64{1.5, 2, 3.25}}`},
		{name: "bool elements", src: boolSrc, data: map[string]any{"BoolItems": []bool{true, false}}, dataLiteral: `opsData{BoolItems: []bool{true, false}}`},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="1 2 3"></div>` {
		t.Fatalf("int elements: codegen output %q, want %q", got, `<div class="1 2 3"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class="1.5 2 3.25"></div>` {
		t.Fatalf("float elements: codegen output %q, want %q", got, `<div class="1.5 2 3.25"></div>`)
	}
	got = assertConditionDiffResult(t, cases[2].src, cases[2].dataLiteral, results[2])
	if got != `<div class="true false"></div>` {
		t.Fatalf("bool elements: codegen output %q, want %q", got, `<div class="true false"></div>`)
	}
}

// TestCodegenClassSliceEmptyElementDoubleSpace proves the strings.Join-not-
// gopug.JoinClasses discriminator: an empty-string element in the middle of
// the slice is kept (not dropped), so the rendered class value carries a
// doubled interior space, exactly matching the interpreter's own
// strings.Join(parts, " ") call on the same slice. A fault-injected
// "collapsed" expectation (what a wrongly-substituted gopug.JoinClasses,
// which drops empty tokens, would produce instead) must NOT match — proving
// the codegen path is really calling strings.Join, not JoinClasses.
func TestCodegenClassSliceEmptyElementDoubleSpace(t *testing.T) {
	t.Parallel()
	src := `div(class=Items)` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{name: "empty element", src: src, data: map[string]any{"Items": []string{"a", "", "b"}}, dataLiteral: `opsData{Items: []string{"a", "", "b"}}`},
	})
	got := assertConditionDiffResult(t, src, `opsData{Items: []string{"a", "", "b"}}`, results[0])
	const want = `<div class="a  b"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (the empty element must produce a doubled interior space)", got, want)
	}

	const collapsed = `<div class="a b"></div>`
	if got == collapsed {
		t.Fatalf("codegen output %q matches the collapsed (single-space) form %q; the empty element is being dropped, meaning strings.Join was not actually used", got, collapsed)
	}
}

// TestCodegenClassSliceEscaping proves the joined slice-class value is
// escaped through gopug.EscapeAttr, not written raw — an element containing
// both "&" and a literal double quote comes out entity-escaped exactly like
// Runtime.renderTag's own htmlEscapeAttr call. A fault-injected raw
// (unescaped) expectation must NOT match, proving EscapeAttr is actually
// applied.
func TestCodegenClassSliceEscaping(t *testing.T) {
	t.Parallel()
	src := `div(class=Items)` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{name: "escaped", src: src, data: map[string]any{"Items": []string{"a&b", `c"d`}}, dataLiteral: `opsData{Items: []string{"a&b", "c\"d"}}`},
	})
	got := assertConditionDiffResult(t, src, `opsData{Items: []string{"a&b", "c\"d"}}`, results[0])
	const want = `<div class="a&amp;b c&quot;d"></div>`
	if got != want {
		t.Fatalf("codegen output %q, want %q (the \"&\" and the quote must be entity-escaped)", got, want)
	}

	const wrongUnescaped = `<div class="a&b c"d"></div>`
	if got == wrongUnescaped {
		t.Fatalf("codegen output %q matches the raw unescaped string %q; gopug.EscapeAttr is not being applied", got, wrongUnescaped)
	}
}

// TestCodegenClassSliceDotPath proves a dot-path bare field
// (`div(class=User.Tags)`) resolves through resolveFieldExpr to the nested
// slice field exactly like a top-level slice field does, reusing the same
// genDynamicClassSlice machinery.
func TestCodegenClassSliceDotPath(t *testing.T) {
	t.Parallel()
	src := `div(class=User.Tags)` + "\n"
	results := runClassTernaryDifferentialBatch(t, []classTernaryDiffCase{
		{name: "dot-path slice", src: src, data: map[string]any{"User": map[string]any{"Tags": []string{"a", "b"}}}, dataLiteral: `opsData{User: opsUser{Tags: []string{"a", "b"}}}`},
	})
	got := assertConditionDiffResult(t, src, `opsData{User: opsUser{Tags: []string{"a", "b"}}}`, results[0])
	if got != `<div class="a b"></div>` {
		t.Fatalf("codegen output %q, want %q", got, `<div class="a b"></div>`)
	}
}

// genClassSliceErr parses and GenerateGoes src against opsData (unless
// noType is true, in which case Config.DataReflectType is left nil), always
// returning GenerateGo's error rather than fatally failing the test — used
// by the deferral cases below.
func genClassSliceErr(t *testing.T, src string, noType bool) error {
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

// TestCodegenClassSliceDeferrals proves each distinct DEFER path this
// increment must NOT try to reproduce, even though the interpreter itself
// renders every one of them successfully: a multi-token class value formed
// by merging a shorthand class prefix with a dynamic slice field
// (`div.card(class=Items)` -> trimmed `card Items`, which the interpreter
// resolves through its own resolveClassTokenList flatten path — a different
// code path this increment does not reproduce), a map-valued field
// (deferred to a later increment), and a nil Config.DataReflectType (no type
// information to resolve the bare field against at all).
func TestCodegenClassSliceDeferrals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		src    string
		noType bool
	}{
		{name: "multi-token shorthand-prefix + slice field", src: `div.card(class=Items)` + "\n"},
		{name: "map-valued field", src: `div(class=Meta)` + "\n"},
		{name: "nil rootType", src: `div(class=Items)` + "\n", noType: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genClassSliceErr(t, tc.src, tc.noType)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenClassSliceDeferralsAreDistinct proves the deferral cases above
// produce genuinely different error messages, not one shared catch-all.
func TestCodegenClassSliceDeferralsAreDistinct(t *testing.T) {
	t.Parallel()
	multiToken := genClassSliceErr(t, `div.card(class=Items)`+"\n", false)
	mapField := genClassSliceErr(t, `div(class=Meta)`+"\n", false)
	nilType := genClassSliceErr(t, `div(class=Items)`+"\n", true)

	msgs := map[string]error{
		"multi-token shorthand + slice": multiToken,
		"map-valued field":              mapField,
		"nil rootType":                  nilType,
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

// TestCodegenClassSliceRegressionUnchangedPaths proves the pre-existing
// class-value paths this increment must leave untouched still work exactly
// as before: the single-token string-field path (a bare field resolving to
// a plain string, not a slice) and the multi-token gopug.JoinClasses path
// (a shorthand class prefix merged with a bare string field).
func TestCodegenClassSliceRegressionUnchangedPaths(t *testing.T) {
	t.Parallel()
	stringFieldSrc := `div(class=Name)` + "\n"
	multiTokenSrc := `div.base(class=Name)` + "\n"
	cases := []classTernaryDiffCase{
		{name: "single-token string field", src: stringFieldSrc, data: map[string]any{"Name": "solo"}, dataLiteral: `opsData{Name: "solo"}`},
		{name: "multi-token JoinClasses", src: multiTokenSrc, data: map[string]any{"Name": "extra"}, dataLiteral: `opsData{Name: "extra"}`},
	}
	results := runClassTernaryDifferentialBatch(t, cases)

	got := assertConditionDiffResult(t, cases[0].src, cases[0].dataLiteral, results[0])
	if got != `<div class="solo"></div>` {
		t.Fatalf("single-token string field: codegen output %q, want %q", got, `<div class="solo"></div>`)
	}
	got = assertConditionDiffResult(t, cases[1].src, cases[1].dataLiteral, results[1])
	if got != `<div class="base extra"></div>` {
		t.Fatalf("multi-token JoinClasses: codegen output %q, want %q", got, `<div class="base extra"></div>`)
	}
}

// TestCodegenClassSliceImportGating proves a tag using ONLY a single-token
// slice-valued class attribute compiles with exactly the imports it needs —
// "fmt" (for the per-element Sprintf), "strings" (for Join), and "gopug"
// (for EscapeAttr) — but not "html"/"strconv", which nothing in this
// template's generated code calls.
func TestCodegenClassSliceImportGating(t *testing.T) {
	t.Parallel()
	src := `div(class=Items)` + "\n"
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

	for _, want := range []string{`"fmt"`, `"strings"`, `"github.com/sinfulspartan/go-pug/pkg/gopug"`} {
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
	want, err := tmpl.Render(map[string]any{"Items": []string{"a", "b"}})
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, `opsData{Items: []string{"a", "b"}}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}
