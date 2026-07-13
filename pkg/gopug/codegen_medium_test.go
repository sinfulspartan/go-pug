package gopug_test

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

// codegenMediumSrc is verbatim mediumSrc (benchmark_test.go, package gopug)
// — the realistic card component with attributes and a conditional — copied
// here under its own name so the codegen benchmark can generate against it
// without reaching into the internal test package. Its interpreter
// (BenchmarkRenderMedium) and pug.js render numbers already exist; this adds
// the third, codegen leg of the same comparison.
const codegenMediumSrc = `div.card(id=cardId)
  h2= title
  p= description
  if badge
    span.badge= badge
  a(href=url) Read more`

// MediumData is the declared struct the medium codegen benchmark template
// resolves field expressions against.
type MediumData struct {
	CardId      string
	Title       string
	Description string
	Badge       string
	Url         string
}

// mediumDataReflectType is the reflect.Type of MediumData, passed as
// Config.DataReflectType so GenerateGo resolves every field expression's Go
// type instead of assuming string.
var mediumDataReflectType = reflect.TypeOf(MediumData{})

// mediumCGData is the fixture both the codegen and interpreter
// benchmarks/differential tests render, matching benchmark_test.go's
// BenchmarkRenderMedium data values field for field.
func mediumCGData() MediumData {
	return MediumData{
		CardId:      "card-1",
		Title:       "Hello World",
		Description: "A short description of the card.",
		Badge:       "New",
		Url:         "/article/1",
	}
}

// mediumDataToMap builds the map[string]any the interpreter renders from,
// with exactly the same values as its MediumData counterpart, keyed by the
// lowercase field names codegenMediumSrc actually references.
func mediumDataToMap(d MediumData) map[string]any {
	return map[string]any{
		"cardId":      d.CardId,
		"title":       d.Title,
		"description": d.Description,
		"badge":       d.Badge,
		"url":         d.Url,
	}
}

// TestCodegenMediumGolden guards against generator drift: re-running
// GenerateGo on codegenMediumSrc must reproduce the exact bytes of the
// checked-in codegen_medium_gen_test.go, byte for byte.
func TestCodegenMediumGolden(t *testing.T) {
	ast, err := gopug.Parse(codegenMediumSrc, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := gopug.GenerateGo(ast, gopug.Config{
		PackageName:     "gopug_test",
		FuncName:        "RenderMedium",
		DataType:        "MediumData",
		DataReflectType: mediumDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	want, err := os.ReadFile("codegen_medium_gen_test.go")
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("GenerateGo output does not match the checked-in golden file (regenerate codegen_medium_gen_test.go from GenerateGo's output).\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestMediumCodegenMatchesInterpreter asserts the committed RenderMedium
// produces output byte-identical to the interpreter rendering the
// equivalent, identically typed map, for both the badge-hidden and
// badge-shown branches of codegenMediumSrc's `if badge` conditional.
func TestMediumCodegenMatchesInterpreter(t *testing.T) {
	tpl, err := gopug.Compile(codegenMediumSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	cases := []struct {
		name string
		data MediumData
	}{
		{
			name: "badge hidden",
			data: MediumData{
				CardId:      "card-2",
				Title:       "Another Card",
				Description: "No badge on this one.",
				Badge:       "",
				Url:         "/article/2",
			},
		},
		{
			name: "badge shown",
			data: mediumCGData(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := RenderMedium(&buf, tc.data); err != nil {
				t.Fatalf("RenderMedium: %v", err)
			}

			want, err := tpl.Render(mediumDataToMap(tc.data))
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			if buf.String() != want {
				t.Errorf("codegen output does not match interpreter output\ncodegen:     %q\ninterpreter: %q", buf.String(), want)
			}
		})
	}
}

// TestMediumCodegenBadgeShownThreeWayAgreement covers the badge-shown branch
// codegenMediumSrc's `span.badge= badge` line exercises, where the shorthand
// class token's text ("badge") collides with the name of the in-scope
// variable it buffers as text content. Both engines must render the static
// shorthand class literally rather than substituting the variable's value:
// confirmed against pug 3.0.4 (`pug.render(codegenMediumSrc, data)` produces
// `class="badge"`). This asserts the interpreter, the codegen output, and the
// pug.js-verified literal all agree.
func TestMediumCodegenBadgeShownThreeWayAgreement(t *testing.T) {
	tpl, err := gopug.Compile(codegenMediumSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	data := mediumCGData()

	var buf bytes.Buffer
	if err := RenderMedium(&buf, data); err != nil {
		t.Fatalf("RenderMedium: %v", err)
	}

	const wantPugJS = `<div id="card-1" class="card"><h2>Hello World</h2><p>A short description of the card.</p><span class="badge">New</span><a href="/article/1">Read more</a></div>`
	if buf.String() != wantPugJS {
		t.Errorf("codegen output %q does not match the pug.js-verified expected output %q", buf.String(), wantPugJS)
	}

	interpreterOut, err := tpl.Render(mediumDataToMap(data))
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if interpreterOut != wantPugJS {
		t.Errorf("interpreter output %q does not match the pug.js-verified expected output %q", interpreterOut, wantPugJS)
	}
}

// BenchmarkCodegenMedium measures the generated RenderMedium function
// directly, the codegen counterpart to benchmark_test.go's
// BenchmarkRenderMedium (interpreter) and bench.js's RenderMedium (pug.js).
func BenchmarkCodegenMedium(b *testing.B) {
	data := mediumCGData()

	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := RenderMedium(&buf, data); err != nil {
			b.Fatalf("RenderMedium: %v", err)
		}
	}
}
