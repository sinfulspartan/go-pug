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
// equivalent, identically typed map, for the badge-hidden case (the
// badge-shown case is covered separately by
// TestMediumCodegenBadgeShownAgreesWithPugSemantics — see that test's doc
// comment for why it does not compare against the interpreter here).
func TestMediumCodegenMatchesInterpreter(t *testing.T) {
	tpl, err := gopug.Compile(codegenMediumSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	data := MediumData{
		CardId:      "card-2",
		Title:       "Another Card",
		Description: "No badge on this one.",
		Badge:       "",
		Url:         "/article/2",
	}

	var buf bytes.Buffer
	if err := RenderMedium(&buf, data); err != nil {
		t.Fatalf("RenderMedium: %v", err)
	}

	want, err := tpl.Render(mediumDataToMap(data))
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	if buf.String() != want {
		t.Errorf("codegen output does not match interpreter output\ncodegen:     %q\ninterpreter: %q", buf.String(), want)
	}
}

// TestMediumCodegenBadgeShownAgreesWithPugSemantics covers the badge-shown
// case codegenMediumSrc's `span.badge= badge` line exercises — and
// deliberately checks RenderMedium's output against a literal, pug.js-
// verified expected string instead of the interpreter's own Render, because
// the interpreter has a pre-existing, unrelated bug on exactly this shape: a
// tag whose ONLY class comes from shorthand (`.badge`, no `class=` attribute)
// and whose shorthand token's text happens to match the name of an in-scope
// variable renders that variable's VALUE as the class instead of the literal
// shorthand text (runtime.go's class-attribute branch re-evaluates each
// whitespace-split "word" of the shorthand's quoted value as an expression,
// which incorrectly resolves a bare word that shadows a variable name). Since
// codegenMediumSrc's own variable for the badge text is itself named
// "badge" — the same as the shorthand class token — this collision is
// unavoidable for any template exercising the shown-badge branch, so the
// interpreter renders `class="New"` here instead of the correct
// `class="badge"` (confirmed against pug 3.0.4: `pug.render(codegenMediumSrc,
// data)` produces `class="badge"`, matching codegen, not the interpreter).
// This is an interpreter defect independent of codegen — codegen resolves
// the shorthand class token at generate time as a plain static string, so it
// is not exposed to it — and is out of scope for the codegen benchmark task;
// it is called out here so the divergence is documented rather than silently
// worked around.
func TestMediumCodegenBadgeShownAgreesWithPugSemantics(t *testing.T) {
	tpl, err := gopug.Compile(codegenMediumSrc, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	data := mediumCGData()

	var buf bytes.Buffer
	if err := RenderMedium(&buf, data); err != nil {
		t.Fatalf("RenderMedium: %v", err)
	}

	const wantCodegen = `<div id="card-1" class="card"><h2>Hello World</h2><p>A short description of the card.</p><span class="badge">New</span><a href="/article/1">Read more</a></div>`
	if buf.String() != wantCodegen {
		t.Errorf("codegen output %q does not match the pug.js-verified expected output %q", buf.String(), wantCodegen)
	}

	interpreterOut, err := tpl.Render(mediumDataToMap(data))
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if interpreterOut == wantCodegen {
		t.Errorf("interpreter output now matches codegen/pug.js for the badge-shown case; the known class-shorthand/variable-name collision bug this test documents appears to be fixed — replace this test with a plain byte-identical comparison against the interpreter")
	}
}

// BenchmarkCodegenMedium measures the generated RenderMedium function
// directly, the codegen counterpart to benchmark_test.go's
// BenchmarkRenderMedium (interpreter) and bench.js's RenderMedium (pug.js).
//
// Note for a future reader comparing the two: on badge-shown data this
// benchmark and BenchmarkRenderMedium render DIFFERENT bytes, not just
// different speeds — the interpreter has a pre-existing, unrelated
// shorthand-class bug where `span.badge= badge` emits class="New" instead of
// the correct class="badge" that this benchmark's codegen (and pug.js) both
// produce. See TestMediumCodegenBadgeShownAgreesWithPugSemantics for the
// documented divergence and root cause.
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
