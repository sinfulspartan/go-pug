package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// spreadAttrsScalarData is a declared struct, separate from spreadAttrsData
// (codegen_spread_attrs_test.go), dedicated to the concrete-scalar-value
// `&attributes(<map[string]T field>)` shape: every map field whose element
// kind genSpreadAttrs's scalar-spread path accepts (bool, every
// signed/unsigned integer kind, float32, float64), plus AttrsSlice (a
// map[string][]string — a non-scalar concrete value kind, still deferred)
// and AttrsIntKey (a map[int]int — a non-string key, still deferred,
// re-confirmed here with an int-valued map rather than spreadAttrsIntKeyData's
// map[int]any so the key check is proven independent of the new scalar
// element-kind branch too). Kept apart from spreadAttrsData entirely so this
// file never has to touch that struct, its spliced source string, or any of
// its own differential tests.
type spreadAttrsScalarData struct {
	AttrsInt     map[string]int
	AttrsInt8    map[string]int8
	AttrsInt16   map[string]int16
	AttrsInt32   map[string]int32
	AttrsInt64   map[string]int64
	AttrsUint    map[string]uint
	AttrsUint8   map[string]uint8
	AttrsUint16  map[string]uint16
	AttrsUint32  map[string]uint32
	AttrsUint64  map[string]uint64
	AttrsFloat32 map[string]float32
	AttrsFloat64 map[string]float64
	AttrsBool    map[string]bool
	AttrsSlice   map[string][]string
	AttrsIntKey  map[int]int
}

var spreadAttrsScalarReflectType = reflect.TypeOf(spreadAttrsScalarData{})

// spreadAttrsScalarDataStructSrc is spreadAttrsScalarData's field
// declarations, spliced verbatim into the throwaway module runComposedGo
// assembles around a GenerateGo result — it must match spreadAttrsScalarData
// above field for field.
const spreadAttrsScalarDataStructSrc = `type spreadAttrsScalarData struct {
	AttrsInt     map[string]int
	AttrsInt8    map[string]int8
	AttrsInt16   map[string]int16
	AttrsInt32   map[string]int32
	AttrsInt64   map[string]int64
	AttrsUint    map[string]uint
	AttrsUint8   map[string]uint8
	AttrsUint16  map[string]uint16
	AttrsUint32  map[string]uint32
	AttrsUint64  map[string]uint64
	AttrsFloat32 map[string]float32
	AttrsFloat64 map[string]float64
	AttrsBool    map[string]bool
	AttrsSlice   map[string][]string
	AttrsIntKey  map[int]int
}
`

// runScalarSpreadDifferential is runSpreadDifferential (codegen_spread_attrs_test.go)
// generalized to spreadAttrsScalarData instead of spreadAttrsData, since the
// scalar-value shapes this file covers live on a separate declared struct.
func runScalarSpreadDifferential(t *testing.T, src string, data map[string]any, dataLiteral string) string {
	t.Helper()

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsScalarData",
		DataReflectType: spreadAttrsScalarReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runComposedGo(t, generated, spreadAttrsScalarDataStructSrc, dataLiteral, "RenderSpread")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
	return got
}

// genScalarSpreadErr parses and GenerateGoes src against spreadAttrsScalarData
// (unless noType is true, in which case Config.DataReflectType is left nil),
// always returning GenerateGo's error rather than fatally failing the test.
func genScalarSpreadErr(t *testing.T, src string, noType bool) error {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	cfg := Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsScalarData",
		DataReflectType: spreadAttrsScalarReflectType,
	}
	if noType {
		cfg.DataReflectType = nil
	}
	_, err = GenerateGo(ast, cfg)
	return err
}

// TestCodegenSpreadAttrsScalarInt proves the headline concrete-scalar probe
// (probe 1): a map[string]int value boxes into `any` and %v-stringifies to
// the exact same text Runtime.renderTag's own reflect-box-then-%v produces —
// data-n="5".
func TestCodegenSpreadAttrsScalarInt(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsInt)\n"
	data := map[string]any{"AttrsInt": map[string]int{"data-n": 5}}
	dataLiteral := `spreadAttrsScalarData{AttrsInt: map[string]int{"data-n": 5}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `data-n="5"`) {
		t.Fatalf("output %q does not exhibit int value %%v-stringified to \"5\"", got)
	}
}

// TestCodegenSpreadAttrsScalarFloat64Fractional proves a fractional float64
// value boxes and %v-stringifies identically to the map[string]any path
// (probe 2, fractional half): %v(1.5) == "1.5".
func TestCodegenSpreadAttrsScalarFloat64Fractional(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsFloat64)\n"
	data := map[string]any{"AttrsFloat64": map[string]float64{"x": 1.5}}
	dataLiteral := `spreadAttrsScalarData{AttrsFloat64: map[string]float64{"x": 1.5}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `x="1.5"`) {
		t.Fatalf("output %q does not exhibit float64 value %%v-stringified to \"1.5\"", got)
	}
}

// TestCodegenSpreadAttrsScalarFloat64Whole proves a whole-number float64
// value drops its trailing ".0" identically on both backends (probe 2, whole
// half): %v(1.0) == "1", not "1.0".
func TestCodegenSpreadAttrsScalarFloat64Whole(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsFloat64)\n"
	data := map[string]any{"AttrsFloat64": map[string]float64{"x": 1.0}}
	dataLiteral := `spreadAttrsScalarData{AttrsFloat64: map[string]float64{"x": 1.0}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `x="1"`) {
		t.Fatalf("output %q does not exhibit whole float64 value %%v-stringified to \"1\"", got)
	}
}

// TestCodegenSpreadAttrsScalarFloat32ShortestRepr proves the trickiest
// per-kind probe (probe 2, float32 half): a float32 value's %v text uses
// Go's shortest round-tripping decimal representation for THAT narrower
// type, which can differ from the same bit pattern's float64 %v text — but
// because both the interpreter's reflect-boxed float32 and codegen's
// directly-boxed float32 box the identical concrete float32 value before the
// identical %v call runs, the two sides can never disagree regardless of
// which representation Go's formatter picks: %v(float32(0.1)) == "0.1".
func TestCodegenSpreadAttrsScalarFloat32ShortestRepr(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsFloat32)\n"
	data := map[string]any{"AttrsFloat32": map[string]float32{"x": 0.1}}
	dataLiteral := `spreadAttrsScalarData{AttrsFloat32: map[string]float32{"x": 0.1}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `x="0.1"`) {
		t.Fatalf("output %q does not exhibit float32 value %%v-stringified to \"0.1\"", got)
	}
}

// TestCodegenSpreadAttrsScalarBoolTrue proves a REAL bool true value in a
// map[string]bool spread renders as a bare boolean attribute, exactly like
// the map[string]any path's own real-bool-true probe (probe 3, true half).
func TestCodegenSpreadAttrsScalarBoolTrue(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsBool)\n"
	data := map[string]any{"AttrsBool": map[string]bool{"disabled": true}}
	dataLiteral := `spreadAttrsScalarData{AttrsBool: map[string]bool{"disabled": true}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, " disabled") || strings.Contains(got, `disabled="`) {
		t.Fatalf("output %q does not exhibit a bare \"disabled\" attribute for a real bool true value", got)
	}
}

// TestCodegenSpreadAttrsScalarBoolFalse proves a REAL bool false value
// DELETES the attribute entirely, even when the tag's own base attribute of
// that name was itself a bare boolean (probe 3, false half).
func TestCodegenSpreadAttrsScalarBoolFalse(t *testing.T) {
	t.Parallel()
	src := "div(hidden)&attributes(AttrsBool)\n"
	data := map[string]any{"AttrsBool": map[string]bool{"hidden": false}}
	dataLiteral := `spreadAttrsScalarData{AttrsBool: map[string]bool{"hidden": false}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if strings.Contains(got, "hidden") {
		t.Fatalf("output %q still exhibits \"hidden\" after a real bool false value should have deleted it", got)
	}
}

// TestCodegenSpreadAttrsScalarIntAsClass proves a non-string, non-bool
// scalar (an int) used as the "class" key is still %v-stringified before the
// class merge, exactly like the map[string]any path's own non-string-class
// probe (probe 4).
func TestCodegenSpreadAttrsScalarIntAsClass(t *testing.T) {
	t.Parallel()
	src := "div.base&attributes(AttrsInt)\n"
	data := map[string]any{"AttrsInt": map[string]int{"class": 5}}
	dataLiteral := `spreadAttrsScalarData{AttrsInt: map[string]int{"class": 5}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `class="base 5"`) {
		t.Fatalf("output %q does not exhibit the base class merged with the %%v-stringified int class value \"5\"", got)
	}
}

// TestCodegenSpreadAttrsScalarEmptyMap proves an empty concrete-scalar spread
// map renders only the tag's own base attributes, no error (probe 5, empty
// half).
func TestCodegenSpreadAttrsScalarEmptyMap(t *testing.T) {
	t.Parallel()
	src := `div(id="x")&attributes(AttrsInt)` + "\n"
	data := map[string]any{"AttrsInt": map[string]int{}}
	dataLiteral := `spreadAttrsScalarData{AttrsInt: map[string]int{}}`
	runScalarSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsScalarSortOrder pins the id/class/rest attribute
// order for a concrete-scalar spread mixing a base "other" attribute with
// spread "id" (int), "class" (int), and two "other" int keys, proving
// sortAttrNames orders the merged, boxed-then-%v-stringified result exactly
// like the map[string]any path (probe 5, ordering half).
func TestCodegenSpreadAttrsScalarSortOrder(t *testing.T) {
	t.Parallel()
	src := `div(z2="base")&attributes(AttrsInt)` + "\n"
	data := map[string]any{"AttrsInt": map[string]int{"id": 1, "class": 2, "z": 9, "a": 3}}
	dataLiteral := `spreadAttrsScalarData{AttrsInt: map[string]int{"id": 1, "class": 2, "z": 9, "a": 3}}`
	got := runScalarSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `id="1" class="2" a="3" z="9" z2="base"`) {
		t.Fatalf("output %q does not exhibit the expected id/class/rest order — this test's own pinned assumption about the runtime sort order is stale", got)
	}
}

// TestCodegenSpreadAttrsScalarEveryAcceptedKind exercises every OTHER
// concrete scalar element kind genSpreadAttrs's isScalarMapElemKind accepts
// besides int, float32, float64, and bool (already covered individually
// above) — int8/16/32/64 and uint/8/16/32/64 — proving the acceptance ladder
// is not accidentally narrower than its own documented kind set.
func TestCodegenSpreadAttrsScalarEveryAcceptedKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		src         string
		data        map[string]any
		dataLiteral string
	}{
		{"int8", "div&attributes(AttrsInt8)\n",
			map[string]any{"AttrsInt8": map[string]int8{"n": int8(7)}},
			`spreadAttrsScalarData{AttrsInt8: map[string]int8{"n": 7}}`},
		{"int16", "div&attributes(AttrsInt16)\n",
			map[string]any{"AttrsInt16": map[string]int16{"n": int16(7)}},
			`spreadAttrsScalarData{AttrsInt16: map[string]int16{"n": 7}}`},
		{"int32", "div&attributes(AttrsInt32)\n",
			map[string]any{"AttrsInt32": map[string]int32{"n": int32(7)}},
			`spreadAttrsScalarData{AttrsInt32: map[string]int32{"n": 7}}`},
		{"int64", "div&attributes(AttrsInt64)\n",
			map[string]any{"AttrsInt64": map[string]int64{"n": int64(7)}},
			`spreadAttrsScalarData{AttrsInt64: map[string]int64{"n": 7}}`},
		{"uint", "div&attributes(AttrsUint)\n",
			map[string]any{"AttrsUint": map[string]uint{"n": uint(7)}},
			`spreadAttrsScalarData{AttrsUint: map[string]uint{"n": 7}}`},
		{"uint8", "div&attributes(AttrsUint8)\n",
			map[string]any{"AttrsUint8": map[string]uint8{"n": uint8(7)}},
			`spreadAttrsScalarData{AttrsUint8: map[string]uint8{"n": 7}}`},
		{"uint16", "div&attributes(AttrsUint16)\n",
			map[string]any{"AttrsUint16": map[string]uint16{"n": uint16(7)}},
			`spreadAttrsScalarData{AttrsUint16: map[string]uint16{"n": 7}}`},
		{"uint32", "div&attributes(AttrsUint32)\n",
			map[string]any{"AttrsUint32": map[string]uint32{"n": uint32(7)}},
			`spreadAttrsScalarData{AttrsUint32: map[string]uint32{"n": 7}}`},
		{"uint64", "div&attributes(AttrsUint64)\n",
			map[string]any{"AttrsUint64": map[string]uint64{"n": uint64(7)}},
			`spreadAttrsScalarData{AttrsUint64: map[string]uint64{"n": 7}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runScalarSpreadDifferential(t, tc.src, tc.data, tc.dataLiteral)
			if !strings.Contains(got, `n="7"`) {
				t.Fatalf("output %q does not exhibit n=\"7\" for scalar kind %s", got, tc.name)
			}
		})
	}
}

// TestCodegenSpreadAttrsScalarBaseClassWhitespaceStillDefers proves the
// pre-existing irregular-base-class-whitespace deferral still fires for a
// concrete-scalar spread source too — genSpreadBase runs after the spread
// source's element kind has already been inspected, but its whitespace
// check never looks at that element kind, so it is source-type-agnostic
// by construction, and this test exercises that directly.
func TestCodegenSpreadAttrsScalarBaseClassWhitespaceStillDefers(t *testing.T) {
	src := `div(class="a  b")&attributes(AttrsInt)` + "\n"
	err := genScalarSpreadErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "leading/trailing or repeated internal whitespace") {
		t.Errorf("GenerateGo error %q does not mention the irregular-whitespace base class deferral", err.Error())
	}
}

// TestCodegenSpreadAttrsScalarDeferrals collects every distinct clean error
// this increment's own scope cut refuses, rather than guessing at output for
// a shape it cannot prove byte-identical to the interpreter.
func TestCodegenSpreadAttrsScalarDeferrals(t *testing.T) {
	t.Run("map[string][]string source (non-scalar concrete value)", func(t *testing.T) {
		src := "div&attributes(AttrsSlice)\n"
		err := genScalarSpreadErr(t, src, false)
		if err == nil {
			t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
		}
		if !strings.Contains(err.Error(), "map[string]string-typed") {
			t.Errorf("GenerateGo error %q does not mention the type-ladder deferral", err.Error())
		}
	})

	t.Run("map[int]int source (non-string key, scalar value)", func(t *testing.T) {
		src := "div&attributes(AttrsIntKey)\n"
		err := genScalarSpreadErr(t, src, false)
		if err == nil {
			t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
		}
		if !strings.Contains(err.Error(), "string-keyed") {
			t.Errorf("GenerateGo error %q does not mention the non-string-key deferral", err.Error())
		}
	})

	t.Run("nil DataReflectType", func(t *testing.T) {
		src := "div&attributes(AttrsInt)\n"
		err := genScalarSpreadErr(t, src, true)
		if err == nil {
			t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
		}
		if !strings.Contains(err.Error(), "DataReflectType") {
			t.Errorf("GenerateGo error %q does not mention the nil-DataReflectType deferral", err.Error())
		}
	})
}

// TestCodegenSpreadAttrsScalarFaultInjection proves the differential harness
// itself is non-vacuous for the concrete-scalar entry point: a deliberately
// WRONG expected value must fail the comparison.
func TestCodegenSpreadAttrsScalarFaultInjection(t *testing.T) {
	t.Parallel()
	src := "div.base&attributes(AttrsInt)\n"
	dataLiteral := `spreadAttrsScalarData{AttrsInt: map[string]int{"class": 5}}`

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsScalarData",
		DataReflectType: spreadAttrsScalarReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runComposedGo(t, generated, spreadAttrsScalarDataStructSrc, dataLiteral, "RenderSpread")
	wrongWant := `<div class="wrong"></div>`
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}
