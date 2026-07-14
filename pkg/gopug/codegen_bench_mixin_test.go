package gopug_test

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

// benchMixinSrc declares a single mixin with two positional string
// parameters — one used inside a dynamic attribute value, one buffered as a
// heading — and a `block` slot, called once per loop iteration over a
// data-derived slice, each call passing data-derived arguments and static
// block content. This is the shape the mixin codegen machinery
// (genMixinFunc/genMixinCall, the block-callback closure) was built to
// support, looped at scale so its per-call cost (helper-function dispatch,
// parameter binding, block-callback invocation) dominates the benchmark.
const benchMixinSrc = `mixin card(title, kind)
  .card(data-kind=kind)
    h3= title
    block
each item in Items
  +card(item.Title, item.Kind)
    p Standard item footer`

// BenchMixinData is the declared struct the mixin benchmark template
// resolves field expressions against.
type BenchMixinData struct {
	Items []BenchMixinItem
}

// BenchMixinItem is one element of BenchMixinData.Items.
type BenchMixinItem struct {
	Title string
	Kind  string
}

// benchMixinDataReflectType is the reflect.Type of BenchMixinData, passed as
// Config.DataReflectType so GenerateGo resolves every field expression's Go
// type instead of assuming string.
var benchMixinDataReflectType = reflect.TypeOf(BenchMixinData{})

// benchMixinItemCount is the number of items benchMixinData generates —
// enough mixin calls (and block-callback invocations) that the per-call cost
// dominates the fixed per-render overhead.
const benchMixinItemCount = 50

// benchMixinKinds cycles through a few distinct "kind" values so the
// dynamic attribute value isn't a constant across every call.
var benchMixinKinds = []string{"featured", "standard", "clearance"}

// benchMixinData builds the BenchMixinData fixture both the codegen and
// interpreter benchmarks/differential tests render: 50 items with a title
// and a cycling kind.
func benchMixinData() BenchMixinData {
	items := make([]BenchMixinItem, benchMixinItemCount)
	for i := range items {
		items[i] = BenchMixinItem{
			Title: fmt.Sprintf("Card %d", i+1),
			Kind:  benchMixinKinds[i%len(benchMixinKinds)],
		}
	}
	return BenchMixinData{Items: items}
}

// benchMixinDataToMap builds the map[string]any the interpreter renders
// from, with exactly the same shape and Go types as its BenchMixinData
// counterpart.
func benchMixinDataToMap(d BenchMixinData) map[string]any {
	items := make([]any, len(d.Items))
	for i, it := range d.Items {
		items[i] = map[string]any{
			"Title": it.Title,
			"Kind":  it.Kind,
		}
	}
	return map[string]any{"Items": items}
}

// TestCodegenBenchMixinGolden guards against generator drift: re-running
// GenerateGo on benchMixinSrc must reproduce the exact bytes of the
// checked-in codegen_bench_mixin_gen_test.go, byte for byte.
func TestCodegenBenchMixinGolden(t *testing.T) {
	ast, err := gopug.Parse(benchMixinSrc, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := gopug.GenerateGo(ast, gopug.Config{
		PackageName:     "gopug_test",
		FuncName:        "RenderBenchMixin",
		DataType:        "BenchMixinData",
		DataReflectType: benchMixinDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	want, err := os.ReadFile("codegen_bench_mixin_gen_test.go")
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("GenerateGo output does not match the checked-in golden file (regenerate codegen_bench_mixin_gen_test.go from GenerateGo's output).\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBenchMixinCodegenMatchesInterpreter is the benchmark's correctness
// bar: a benchmark comparing two divergent renders would be meaningless, so
// this asserts the committed RenderBenchMixin produces output byte-identical
// to the interpreter rendering the equivalent, identically typed map, across
// the full 50-item fixture, an empty item list, and a single item.
func TestBenchMixinCodegenMatchesInterpreter(t *testing.T) {
	tpl, err := gopug.Compile(benchMixinSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	cases := []struct {
		name string
		data BenchMixinData
	}{
		{name: "50 items, cycling kind", data: benchMixinData()},
		{name: "empty item list", data: BenchMixinData{Items: nil}},
		{name: "single item", data: BenchMixinData{Items: []BenchMixinItem{{Title: "Solo", Kind: "featured"}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := RenderBenchMixin(&buf, tc.data); err != nil {
				t.Fatalf("RenderBenchMixin: %v", err)
			}

			want, err := tpl.Render(benchMixinDataToMap(tc.data))
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			if buf.String() != want {
				t.Errorf("codegen output does not match interpreter output\ncodegen:     %q\ninterpreter: %q", buf.String(), want)
			}
		})
	}
}

// BenchmarkCodegenBenchMixin measures the generated RenderBenchMixin
// function directly, rendering into a reused buffer so the measurement
// isolates the generated code's own cost from an allocator warm-up effect.
func BenchmarkCodegenBenchMixin(b *testing.B) {
	data := benchMixinData()

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := RenderBenchMixin(&buf, data); err != nil {
			b.Fatalf("RenderBenchMixin: %v", err)
		}
	}
}

// BenchmarkInterpretBenchMixin measures the interpreter rendering
// benchMixinSrc against the equivalent map data, using an ALREADY-COMPILED
// template (Compile runs once, outside the timed loop) — the cached-template
// path a long-lived server process would actually use — the baseline
// BenchmarkCodegenBenchMixin is compared against.
func BenchmarkInterpretBenchMixin(b *testing.B) {
	tpl, err := gopug.Compile(benchMixinSrc, nil)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}

	data := benchMixinDataToMap(benchMixinData())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatalf("Render: %v", err)
		}
	}
}
