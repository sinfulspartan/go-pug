package gopug_test

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

// largeCGSrc is largeSrc's page shape (benchmark_test.go, package gopug)
// with the mixin call inlined directly into the each-loop body instead of
// dispatched through a mixin, so the whole page is codegen-able: mixins are
// out of GenerateGo's supported grammar, but an inlined loop body over the
// same product data exercises the same doctype/nav/loop/conditional/footer
// shape and is representative of the hot render path a codegen consumer
// would actually generate for a real product-listing page.
const largeCGSrc = `doctype html
html(lang="en")
  head
    meta(charset="utf-8")
    title= PageTitle
  body
    header
      nav
        a(href="/") Home
        a(href="/about") About
    main
      h1= Heading
      p= Intro
      ul.items
        each product in Products
          li.item(data-id=product.ID)
            span.name= product.Name
            span.price= product.Price
            if product.OnSale
              span.badge Sale
      if ShowFootnote
        p.footnote= Footnote
    footer
      p Go-Pug`

// LargeCGData is the declared struct the large codegen-able benchmark
// template resolves field expressions against.
type LargeCGData struct {
	PageTitle    string
	Heading      string
	Intro        string
	Products     []LargeCGProduct
	ShowFootnote bool
	Footnote     string
}

// LargeCGProduct is one element of LargeCGData.Products.
type LargeCGProduct struct {
	ID     int
	Name   string
	Price  string
	OnSale bool
}

// largeCGDataReflectType is the reflect.Type of LargeCGData, passed as
// Config.DataReflectType so GenerateGo resolves every field expression's Go
// type instead of assuming string.
var largeCGDataReflectType = reflect.TypeOf(LargeCGData{})

// largeCGProductCount is the number of products largeCGData generates —
// enough loop iterations that the per-render cost of the each-loop body
// dominates the fixed per-page overhead, representative of a realistic
// product-listing page.
const largeCGProductCount = 50

// largeCGData builds the LargeCGData fixture both the codegen and
// interpreter benchmarks/differential tests render: 50 products with a mix
// of OnSale true/false (every third product on sale).
func largeCGData() LargeCGData {
	products := make([]LargeCGProduct, largeCGProductCount)
	for i := range products {
		products[i] = LargeCGProduct{
			ID:     i + 1,
			Name:   fmt.Sprintf("Product %d", i+1),
			Price:  fmt.Sprintf("$%d.99", i+1),
			OnSale: i%3 == 0,
		}
	}
	return LargeCGData{
		PageTitle:    "Benchmark Page",
		Heading:      "Welcome",
		Intro:        "This is the intro paragraph.",
		Products:     products,
		ShowFootnote: true,
		Footnote:     "Prices subject to change.",
	}
}

// largeCGDataToMap builds the map[string]any the interpreter renders from,
// with exactly the same shape and Go types as its LargeCGData counterpart —
// so the interpreter and codegen backends are compared on the same logical,
// identically typed data.
func largeCGDataToMap(d LargeCGData) map[string]any {
	products := make([]any, len(d.Products))
	for i, p := range d.Products {
		products[i] = map[string]any{
			"ID":     p.ID,
			"Name":   p.Name,
			"Price":  p.Price,
			"OnSale": p.OnSale,
		}
	}
	return map[string]any{
		"PageTitle":    d.PageTitle,
		"Heading":      d.Heading,
		"Intro":        d.Intro,
		"Products":     products,
		"ShowFootnote": d.ShowFootnote,
		"Footnote":     d.Footnote,
	}
}

// TestCodegenLargeCGGolden guards against generator drift: re-running
// GenerateGo on largeCGSrc must reproduce the exact bytes of the checked-in
// codegen_largecg_gen_test.go, byte for byte.
func TestCodegenLargeCGGolden(t *testing.T) {
	ast, err := gopug.Parse(largeCGSrc, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := gopug.GenerateGo(ast, gopug.Config{
		PackageName:     "gopug_test",
		FuncName:        "RenderLargeCG",
		DataType:        "LargeCGData",
		DataReflectType: largeCGDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	want, err := os.ReadFile("codegen_largecg_gen_test.go")
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("GenerateGo output does not match the checked-in golden file (regenerate codegen_largecg_gen_test.go from GenerateGo's output).\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestLargeCGCodegenMatchesInterpreter is the benchmark's correctness bar: a
// benchmark comparing two divergent renders would be meaningless, so this
// asserts the committed RenderLargeCG produces output byte-identical to the
// interpreter rendering the equivalent, identically typed map, across a mix
// of OnSale true/false products, an empty product list, and ShowFootnote
// both ways.
func TestLargeCGCodegenMatchesInterpreter(t *testing.T) {
	tpl, err := gopug.Compile(largeCGSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	cases := []struct {
		name string
		data LargeCGData
	}{
		{name: "50 products, mixed OnSale, footnote shown", data: largeCGData()},
		{
			name: "empty product list",
			data: LargeCGData{
				PageTitle:    "Benchmark Page",
				Heading:      "Welcome",
				Intro:        "This is the intro paragraph.",
				Products:     nil,
				ShowFootnote: true,
				Footnote:     "Prices subject to change.",
			},
		},
		{
			name: "footnote hidden",
			data: LargeCGData{
				PageTitle:    "Benchmark Page",
				Heading:      "Welcome",
				Intro:        "This is the intro paragraph.",
				Products:     []LargeCGProduct{{ID: 1, Name: "Widget", Price: "$1.00", OnSale: false}},
				ShowFootnote: false,
				Footnote:     "",
			},
		},
		{
			name: "single product on sale",
			data: LargeCGData{
				PageTitle:    "Benchmark Page",
				Heading:      "Welcome",
				Intro:        "This is the intro paragraph.",
				Products:     []LargeCGProduct{{ID: 7, Name: "Gadget", Price: "$7.99", OnSale: true}},
				ShowFootnote: true,
				Footnote:     "Prices subject to change.",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := RenderLargeCG(&buf, tc.data); err != nil {
				t.Fatalf("RenderLargeCG: %v", err)
			}

			want, err := tpl.Render(largeCGDataToMap(tc.data))
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			if buf.String() != want {
				t.Errorf("codegen output does not match interpreter output\ncodegen:     %q\ninterpreter: %q", buf.String(), want)
			}
		})
	}
}

// BenchmarkCodegenLargeCG measures the generated RenderLargeCG function
// directly, rendering into a reused buffer so the measurement isolates the
// generated code's own cost from an allocator warm-up effect.
func BenchmarkCodegenLargeCG(b *testing.B) {
	data := largeCGData()

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := RenderLargeCG(&buf, data); err != nil {
			b.Fatalf("RenderLargeCG: %v", err)
		}
	}
}

// BenchmarkInterpretLargeCG measures the interpreter rendering largeCGSrc
// against the equivalent map data, the baseline BenchmarkCodegenLargeCG is
// compared against.
func BenchmarkInterpretLargeCG(b *testing.B) {
	tpl, err := gopug.Compile(largeCGSrc, nil)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}

	data := largeCGDataToMap(largeCGData())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatalf("Render: %v", err)
		}
	}
}
