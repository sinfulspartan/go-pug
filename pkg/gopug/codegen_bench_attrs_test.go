package gopug_test

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

// benchAttrsSrc declares a tag with a static class shorthand and a dynamic
// base attribute, plus a runtime `&attributes(item.Attrs)` spread from a
// map[string]string field, inside an `each item in Items` loop over a
// data-derived slice. This is the shape genSpreadAttrs/gopug.WriteSpreadAttrs
// were built to support (runtime merge, sortAttrNames ordering, and
// per-value escaping, all resolved at render time since the spread's keys
// are unknown at generate time), looped at scale so that per-iteration
// runtime-merge cost dominates the benchmark.
const benchAttrsSrc = `each item in Items
  div.row(data-id=item.ID)&attributes(item.Attrs)
    span= item.ID`

// BenchAttrsData is the declared struct the &attributes benchmark template
// resolves field expressions against.
type BenchAttrsData struct {
	Items []BenchAttrsItem
}

// BenchAttrsItem is one element of BenchAttrsData.Items. Attrs is the
// runtime spread source — its keys are unknown at generate time, so the
// merge, sort, and escaping all resolve through gopug.WriteSpreadAttrs.
type BenchAttrsItem struct {
	ID    string
	Attrs map[string]string
}

// benchAttrsDataReflectType is the reflect.Type of BenchAttrsData, passed as
// Config.DataReflectType so GenerateGo resolves every field expression's Go
// type instead of assuming string.
var benchAttrsDataReflectType = reflect.TypeOf(BenchAttrsData{})

// benchAttrsItemCount is the number of items benchAttrsData generates —
// enough runtime attribute spreads that the per-iteration merge/sort/escape
// cost dominates the fixed per-render overhead.
const benchAttrsItemCount = 50

// benchAttrsData builds the BenchAttrsData fixture both the codegen and
// interpreter benchmarks/differential tests render: 50 items, each with a
// two-entry spread attribute map.
func benchAttrsData() BenchAttrsData {
	items := make([]BenchAttrsItem, benchAttrsItemCount)
	for i := range items {
		items[i] = BenchAttrsItem{
			ID: fmt.Sprintf("item-%d", i+1),
			Attrs: map[string]string{
				"data-index": strconv.Itoa(i),
				"role":       "listitem",
			},
		}
	}
	return BenchAttrsData{Items: items}
}

// benchAttrsDataToMap builds the map[string]any the interpreter renders
// from, with exactly the same shape and Go types as its BenchAttrsData
// counterpart — the spread source stays a map[string]string, matching its
// struct field type exactly, since Runtime resolves it through reflection
// regardless of the surrounding map's own value kind.
func benchAttrsDataToMap(d BenchAttrsData) map[string]any {
	items := make([]any, len(d.Items))
	for i, it := range d.Items {
		items[i] = map[string]any{
			"ID":    it.ID,
			"Attrs": it.Attrs,
		}
	}
	return map[string]any{"Items": items}
}

// TestCodegenBenchAttrsGolden guards against generator drift: re-running
// GenerateGo on benchAttrsSrc must reproduce the exact bytes of the
// checked-in codegen_bench_attrs_gen_test.go, byte for byte.
func TestCodegenBenchAttrsGolden(t *testing.T) {
	ast, err := gopug.Parse(benchAttrsSrc, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := gopug.GenerateGo(ast, gopug.Config{
		PackageName:     "gopug_test",
		FuncName:        "RenderBenchAttrs",
		DataType:        "BenchAttrsData",
		DataReflectType: benchAttrsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	want, err := os.ReadFile("codegen_bench_attrs_gen_test.go")
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("GenerateGo output does not match the checked-in golden file (regenerate codegen_bench_attrs_gen_test.go from GenerateGo's output).\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBenchAttrsCodegenMatchesInterpreter is the benchmark's correctness
// bar: a benchmark comparing two divergent renders would be meaningless, so
// this asserts the committed RenderBenchAttrs produces output byte-identical
// to the interpreter rendering the equivalent, identically typed map, across
// the full 50-item fixture, an empty item list, and an item whose spread map
// overwrites the dynamic base "data-id" attribute.
func TestBenchAttrsCodegenMatchesInterpreter(t *testing.T) {
	tpl, err := gopug.Compile(benchAttrsSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	cases := []struct {
		name string
		data BenchAttrsData
	}{
		{name: "50 items, two-entry spread each", data: benchAttrsData()},
		{name: "empty item list", data: BenchAttrsData{Items: nil}},
		{
			name: "spread overwrites data-id",
			data: BenchAttrsData{Items: []BenchAttrsItem{
				{ID: "solo", Attrs: map[string]string{"data-id": "overwritten", "class": "extra"}},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := RenderBenchAttrs(&buf, tc.data); err != nil {
				t.Fatalf("RenderBenchAttrs: %v", err)
			}

			want, err := tpl.Render(benchAttrsDataToMap(tc.data))
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			if buf.String() != want {
				t.Errorf("codegen output does not match interpreter output\ncodegen:     %q\ninterpreter: %q", buf.String(), want)
			}
		})
	}
}

// BenchmarkCodegenBenchAttrs measures the generated RenderBenchAttrs
// function directly, rendering into a reused buffer so the measurement
// isolates the generated code's own cost from an allocator warm-up effect.
func BenchmarkCodegenBenchAttrs(b *testing.B) {
	data := benchAttrsData()

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := RenderBenchAttrs(&buf, data); err != nil {
			b.Fatalf("RenderBenchAttrs: %v", err)
		}
	}
}

// BenchmarkInterpretBenchAttrs measures the interpreter rendering
// benchAttrsSrc against the equivalent map data, using an ALREADY-COMPILED
// template (Compile runs once, outside the timed loop) — the cached-template
// path a long-lived server process would actually use — the baseline
// BenchmarkCodegenBenchAttrs is compared against.
func BenchmarkInterpretBenchAttrs(b *testing.B) {
	tpl, err := gopug.Compile(benchAttrsSrc, nil)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}

	data := benchAttrsDataToMap(benchAttrsData())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatalf("Render: %v", err)
		}
	}
}
