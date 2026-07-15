package gopug_test

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

// skelTemplate exercises every shape this codegen increment supports:
// doctype, nested tags, a void element, static attributes plus .class#id
// shorthand, plain text, #{bareField} and #{obj.field} interpolation of
// string/int/float64/bool fields, a single-variable each over a slice
// field, if/else on a bool field and a numeric field (bare truthiness), a
// dynamic string attribute (bare field and dot-path), a dynamic numeric
// attribute, a boolean attribute driven by a bool field, a bool field
// rendered on a non-boolean attribute name, a dynamic class merging a
// shorthand token with a bare string field, a dynamic class with no
// shorthand at all, and — the comparison/`.length` condition slice — a
// `.length` numeric comparison against a slice field, a numeric field
// compared against zero and against an int literal, a string field
// compared for equality against a non-numeric-looking string literal, and a
// `.length` numeric comparison against a string field (proving rune-count,
// not byte-count, semantics on a multibyte value).
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
      a#profile(href=Link) Profile
      img(src=Author.Avatar)
      div(data-count=Count) Count attr
      input(checked=Flag)
      div(data-flag=Flag) Flag attr
      div.card(class=Variant) Card
      span(class=Extra) Extra
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
      if Items.length > 0
        p.has-items Has items (length)
      else
        p.no-items No items (length)
      if Count > 0
        p.positive Positive
      else
        p.not-positive Not positive
      if Count == 3
        p.three Three
      else
        p.not-three Not three
      if Name == "world"
        p.name-world Name is world
      else
        p.name-other Name is other
      if Name.length > 2
        p.long-name Long name
      else
        p.short-name Short name
`

// SkelData, SkelAuthor, and SkelItem are the declared struct the codegen
// skeleton test compiles skelTemplate against. RenderSkel (in
// codegen_skel_gen_test.go) is GenerateGo's checked-in output for this
// template and this struct.
type SkelData struct {
	Name    string
	Author  SkelAuthor
	Items   []SkelItem
	Flag    bool
	Count   int
	Price   float64
	Link    string
	Variant string
	Extra   string
}

type SkelAuthor struct {
	Bio    string
	Avatar string
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
		"Name":    d.Name,
		"Author":  map[string]any{"Bio": d.Author.Bio, "Avatar": d.Author.Avatar},
		"Items":   items,
		"Flag":    d.Flag,
		"Count":   d.Count,
		"Price":   d.Price,
		"Link":    d.Link,
		"Variant": d.Variant,
		"Extra":   d.Extra,
	}
}

// TestCodegenSkelGolden guards against generator drift: re-running
// GenerateGo on skelTemplate must reproduce the exact bytes of the checked-in
// codegen_skel_gen_test.go, byte for byte.
func TestCodegenSkelGolden(t *testing.T) {
	ast, err := gopug.Parse(skelTemplate, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := gopug.GenerateGo(ast, gopug.Config{
		PackageName:     "gopug_test",
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
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))

	if !bytes.Equal(got, want) {
		t.Errorf("GenerateGo output does not match the checked-in golden file (regenerate codegen_skel_gen_test.go from GenerateGo's output).\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestCodegenSkelByteIdentical is the increment's correctness bar: for
// several data shapes (empty/populated slice, flag true/false, zero/non-zero
// Count to exercise `if Count`, `if Count > 0`, and `if Count == 3` each
// both ways, a negative int, a fractional float, a string exactly equal to
// the `if Name == "world"` literal and one that isn't, a multibyte Name
// proving `if Name.length > 2` compares a RUNE count (matching the
// interpreter's len([]rune(...))) rather than a byte count, strings
// containing HTML-significant characters, attribute values proving
// EscapeAttr's entity-aware, apostrophe-preserving escaping diverges from
// html.EscapeString, and dynamic class values — a normal value, an empty
// value proving the empty-token drop rule leaves neither a trailing space
// nor a leaked field value, an HTML-special-character value proving
// EscapeAttr runs after JoinClasses, and a value containing an internal
// space proving it is kept as-is rather than re-split), the committed
// generated RenderSkel must produce output byte-identical to the
// interpreter rendering the equivalent, identically typed map.
func TestCodegenSkelByteIdentical(t *testing.T) {
	tpl, err := gopug.Compile(skelTemplate, nil)
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
				Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:  []SkelItem{{Label: "one"}, {Label: "two"}},
				Flag:   true,
				Count:  3,
				Price:  9.99,
				Link:   "/profile",
			},
		},
		{
			name: "empty slice, flag false, zero count",
			data: SkelData{
				Name:   "World",
				Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:  nil,
				Flag:   false,
				Count:  0,
				Price:  0,
				Link:   "/profile",
			},
		},
		{
			name: "negative count, fractional price",
			data: SkelData{
				Name:   "World",
				Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:  []SkelItem{{Label: "one"}},
				Flag:   true,
				Count:  -5,
				Price:  2.5,
				Link:   "/profile",
			},
		},
		{
			name: "Name equals the \"world\" string-equality literal exactly",
			data: SkelData{
				Name:   "world",
				Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:  []SkelItem{{Label: "one"}},
				Flag:   true,
				Count:  3,
				Price:  9.99,
				Link:   "/profile",
			},
		},
		{
			name: "multibyte Name proves .length compares a rune count, not a byte count",
			data: SkelData{
				// "日本" is two runes but six UTF-8 bytes, straddling the
				// `> 2` boundary differently depending on which count is
				// used: a byte count would take the true branch (6 > 2), a
				// rune count — matching the interpreter — takes the false
				// branch (2 > 2 is false).
				Name:   "日本",
				Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:  nil,
				Flag:   false,
				Count:  0,
				Price:  0,
				Link:   "/profile",
			},
		},
		{
			name: "escaping-sensitive strings",
			data: SkelData{
				Name:   `<b>&"`,
				Author: SkelAuthor{Bio: `it's <i>italic</i> & more`, Avatar: `/a?x=1&y=2`},
				Items:  []SkelItem{{Label: `<script>&"'`}},
				Flag:   true,
				Count:  1,
				Price:  1.5,
				Link:   `a"b&c<d`,
			},
		},
		{
			name: "attribute value with an apostrophe (must not be escaped, unlike html.EscapeString)",
			data: SkelData{
				Name:   "World",
				Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:  nil,
				Flag:   true,
				Count:  1,
				Price:  1.5,
				Link:   `it's a link`,
			},
		},
		{
			name: "attribute value with a valid entity (must not be double-escaped)",
			data: SkelData{
				Name:   "World",
				Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:  nil,
				Flag:   false,
				Count:  0,
				Price:  0,
				Link:   `x&amp;y`,
			},
		},
		{
			name: "dynamic class: normal value, with and without a shorthand token",
			data: SkelData{
				Name:    "World",
				Author:  SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:   nil,
				Flag:    true,
				Count:   1,
				Price:   1.5,
				Link:    "/profile",
				Variant: "large",
				Extra:   "note",
			},
		},
		{
			name: "dynamic class: empty value drops the token with no trailing space or leaked field value",
			data: SkelData{
				Name:    "World",
				Author:  SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:   nil,
				Flag:    true,
				Count:   1,
				Price:   1.5,
				Link:    "/profile",
				Variant: "",
				Extra:   "",
			},
		},
		{
			name: "dynamic class: HTML-special character value proves EscapeAttr runs after JoinClasses",
			data: SkelData{
				Name:    "World",
				Author:  SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:   nil,
				Flag:    true,
				Count:   1,
				Price:   1.5,
				Link:    "/profile",
				Variant: `a<b`,
				Extra:   `c&d`,
			},
		},
		{
			name: "dynamic class: value with an internal space is kept as-is, not re-split",
			data: SkelData{
				Name:    "World",
				Author:  SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
				Items:   nil,
				Flag:    true,
				Count:   1,
				Price:   1.5,
				Link:    "/profile",
				Variant: "x y",
				Extra:   "p q",
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
		Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
		Items:  []SkelItem{{Label: "one"}, {Label: "two"}, {Label: "three"}},
		Flag:   true,
		Count:  3,
		Price:  9.99,
		Link:   "/profile",
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
	tpl, err := gopug.Compile(skelTemplate, nil)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}

	data := skelDataToMap(SkelData{
		Name:   "World",
		Author: SkelAuthor{Bio: "A short bio", Avatar: "/avatar.png"},
		Items:  []SkelItem{{Label: "one"}, {Label: "two"}, {Label: "three"}},
		Flag:   true,
		Count:  3,
		Price:  9.99,
		Link:   "/profile",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatalf("Render: %v", err)
		}
	}
}
