package gopug

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

// skelTemplate exercises every shape this codegen increment supports:
// doctype, nested tags, a void element, static attributes plus .class#id
// shorthand, plain text, #{bareField} and #{obj.field} interpolation of
// string/int/float64/bool fields, a single-variable each over a slice
// field, and if/else on both a bool field and a numeric field.
const skelTemplate = `doctype html
html
  head
    title Skeleton
  body
    div.container#main(data-role="app")
      p Hello, #{Name}!
      p Bio: #{Author.Bio}
      p Count: #{Count}
      p Price: #{Price}
      p Flag: #{Flag}
      img(src="/logo.png" alt="logo")
      br
      ul
        each item in Items
          li #{item.Label}
      if Flag
        p.active On
      else
        p.inactive Off
      if Count
        p.has-count Has items
      else
        p.no-count No items
`

// SkelData, SkelAuthor, and SkelItem are the declared struct the codegen
// skeleton test compiles skelTemplate against. RenderSkel (in
// codegen_skel_gen_test.go) is GenerateGo's checked-in output for this
// template and this struct.
type SkelData struct {
	Name   string
	Author SkelAuthor
	Items  []SkelItem
	Flag   bool
	Count  int
	Price  float64
}

type SkelAuthor struct {
	Bio string
}

type SkelItem struct {
	Label string
}

// skelDataReflectType is the reflect.Type of SkelData, passed as
// Config.DataReflectType so GenerateGo resolves every field expression's Go
// type instead of assuming string.
var skelDataReflectType = reflect.TypeOf(SkelData{})

// skelDataToMap builds the map[string]any the interpreter renders from,
// with exactly the same shape and Go types as its SkelData counterpart —
// int for Count, float64 for Price, bool for Flag — so the two backends are
// compared on the same logical, identically typed data.
func skelDataToMap(d SkelData) map[string]any {
	items := make([]any, len(d.Items))
	for i, it := range d.Items {
		items[i] = map[string]any{"Label": it.Label}
	}
	return map[string]any{
		"Name":   d.Name,
		"Author": map[string]any{"Bio": d.Author.Bio},
		"Items":  items,
		"Flag":   d.Flag,
		"Count":  d.Count,
		"Price":  d.Price,
	}
}

// TestCodegenSkelGolden guards against generator drift: re-running
// GenerateGo on skelTemplate must reproduce the exact bytes of the checked-in
// codegen_skel_gen_test.go, byte for byte.
func TestCodegenSkelGolden(t *testing.T) {
	ast, err := Parse(skelTemplate, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderSkel",
		DataType:        "SkelData",
		DataReflectType: skelDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	want, err := os.ReadFile("codegen_skel_gen_test.go")
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("GenerateGo output does not match the checked-in golden file (regenerate codegen_skel_gen_test.go from GenerateGo's output).\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestCodegenSkelByteIdentical is the increment's correctness bar: for
// several data shapes (empty/populated slice, flag true/false, zero/non-zero
// Count to exercise `if Count` both ways, a negative int, a fractional
// float, and strings containing HTML-significant characters), the committed
// generated RenderSkel must produce output byte-identical to the
// interpreter rendering the equivalent, identically typed map.
func TestCodegenSkelByteIdentical(t *testing.T) {
	tpl, err := Compile(skelTemplate, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	cases := []struct {
		name string
		data SkelData
	}{
		{
			name: "populated slice, flag true, non-zero count",
			data: SkelData{
				Name:   "World",
				Author: SkelAuthor{Bio: "A short bio"},
				Items:  []SkelItem{{Label: "one"}, {Label: "two"}},
				Flag:   true,
				Count:  3,
				Price:  9.99,
			},
		},
		{
			name: "empty slice, flag false, zero count",
			data: SkelData{
				Name:   "World",
				Author: SkelAuthor{Bio: "A short bio"},
				Items:  nil,
				Flag:   false,
				Count:  0,
				Price:  0,
			},
		},
		{
			name: "negative count, fractional price",
			data: SkelData{
				Name:   "World",
				Author: SkelAuthor{Bio: "A short bio"},
				Items:  []SkelItem{{Label: "one"}},
				Flag:   true,
				Count:  -5,
				Price:  2.5,
			},
		},
		{
			name: "escaping-sensitive strings",
			data: SkelData{
				Name:   `<b>&"`,
				Author: SkelAuthor{Bio: `it's <i>italic</i> & more`},
				Items:  []SkelItem{{Label: `<script>&"'`}},
				Flag:   true,
				Count:  1,
				Price:  1.5,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf strings.Builder
			if err := RenderSkel(&buf, tc.data); err != nil {
				t.Fatalf("RenderSkel: %v", err)
			}

			want, err := tpl.Render(skelDataToMap(tc.data))
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			if buf.String() != want {
				t.Errorf("codegen output does not match interpreter output\ncodegen:     %q\ninterpreter: %q", buf.String(), want)
			}
		})
	}
}

// BenchmarkCodegenSkel measures the generated RenderSkel function directly.
func BenchmarkCodegenSkel(b *testing.B) {
	data := SkelData{
		Name:   "World",
		Author: SkelAuthor{Bio: "A short bio"},
		Items:  []SkelItem{{Label: "one"}, {Label: "two"}, {Label: "three"}},
		Flag:   true,
		Count:  3,
		Price:  9.99,
	}

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := RenderSkel(&buf, data); err != nil {
			b.Fatalf("RenderSkel: %v", err)
		}
	}
}

// BenchmarkInterpretSkel measures the interpreter rendering the same
// template and equivalent data, as the baseline codegen is compared against.
func BenchmarkInterpretSkel(b *testing.B) {
	tpl, err := Compile(skelTemplate, nil)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}

	data := skelDataToMap(SkelData{
		Name:   "World",
		Author: SkelAuthor{Bio: "A short bio"},
		Items:  []SkelItem{{Label: "one"}, {Label: "two"}, {Label: "three"}},
		Flag:   true,
		Count:  3,
		Price:  9.99,
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatalf("Render: %v", err)
		}
	}
}
