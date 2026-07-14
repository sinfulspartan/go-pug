package gopug

import (
	"strings"
	"testing"
)

// TestCodegenUnescapedDiscriminatingHTMLSpecials is the headline proof: a
// string field holding HTML-special characters (`<`, `>`, `&`, `"`) renders
// RAW through both unescaped forms (`!{expr}` and `!= expr`), while the
// SAME field renders HTML-escaped through both escaped forms (`#{expr}` and
// `= expr`). The raw and escaped bytes are asserted explicitly (not merely
// checked for equality with the interpreter), so an implementation that
// escaped by mistake would fail the unescaped assertions even though it
// still matched some other, wrongly-escaped "oracle".
func TestCodegenUnescapedDiscriminatingHTMLSpecials(t *testing.T) {
	t.Parallel()
	data := map[string]any{"Name": `<b>&"x"`}
	dataLiteral := "opsData{Name: `<b>&\"x\"`}"

	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "unescaped interpolation", src: "p !{Name}\n", want: `<p><b>&"x"</p>`},
		{name: "unescaped buffered code", src: "p!= Name\n", want: `<p><b>&"x"</p>`},
		{name: "escaped interpolation (regression)", src: "p #{Name}\n", want: "<p>&lt;b&gt;&amp;&#34;x&#34;</p>"},
		{name: "escaped buffered code (regression)", src: "p= Name\n", want: "<p>&lt;b&gt;&amp;&#34;x&#34;</p>"},
	}

	var diffCases []diffCase
	var interpWants []string
	for _, tc := range cases {
		ast, err := Parse(tc.src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.src, err)
		}
		generated, err := GenerateGo(ast, Config{
			PackageName:     "main",
			FuncName:        "RenderOps",
			DataType:        "opsData",
			DataReflectType: opsDataReflectType,
		})
		if err != nil {
			t.Fatalf("GenerateGo(%q): %v", tc.src, err)
		}

		tmpl, err := Compile(tc.src, nil)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.src, err)
		}
		interpWant, err := tmpl.Render(data)
		if err != nil {
			t.Fatalf("interpreter Render(%q): %v", tc.src, err)
		}

		diffCases = append(diffCases, diffCase{name: tc.name, generated: generated, dataLiteral: dataLiteral})
		interpWants = append(interpWants, interpWant)
	}

	results := runDifferentialBatch(t, opsDataStructSrc, "RenderOps", diffCases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := results[i]
			if result.Err != "" {
				t.Fatalf("generated RenderOps(%q): unexpected error %q", tc.src, result.Err)
			}
			if result.Out != interpWants[i] {
				t.Errorf("codegen output %q does not match interpreter output %q for %q", result.Out, interpWants[i], tc.src)
			}
			if result.Out != tc.want {
				t.Errorf("codegen output %q does not match expected %q for %q", result.Out, tc.want, tc.src)
			}
		})
	}
}

// TestCodegenUnescapedFaultInjection proves TestCodegenUnescapedDiscriminatingHTMLSpecials
// is actually exercising the generated code's output, not merely checking it
// built and ran: comparing the unescaped generated output against the
// deliberately WRONG (HTML-escaped) expectation must fail — an
// implementation that still escaped the value would pass this check only if
// it emitted raw bytes, exactly what this slice is proving.
func TestCodegenUnescapedFaultInjection(t *testing.T) {
	t.Parallel()
	dataLiteral := "opsData{Name: `<b>&\"x\"`}"
	wrongWant := "<p>&lt;b&gt;&amp;&#34;x&#34;</p>"

	for _, src := range []string{"p !{Name}\n", "p!= Name\n"} {
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

		got := runGeneratedGo(t, generated, dataLiteral)
		if got == wrongWant {
			t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong (escaped) expectation %q for %q", got, wrongWant, src)
		}
	}
}

// TestCodegenUnescapedNoSpecials is the sanity companion to the
// discriminating case: a string field with no HTML-special characters
// renders identically whether escaped or unescaped, for both interpolation
// and buffered code.
func TestCodegenUnescapedNoSpecials(t *testing.T) {
	t.Parallel()
	data := map[string]any{"Name": "hello"}
	dataLiteral := `opsData{Name: "hello"}`

	var cases []codegenArithCase
	for _, src := range []string{"p !{Name}\n", "p!= Name\n", "p #{Name}\n", "p= Name\n"} {
		cases = append(cases, codegenArithCase{name: src, src: src, data: data, dataLiteral: dataLiteral})
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenUnescapedNumericAndBool proves a numeric and a bool field
// stringify identically through the unescaped forms as the escaped ones —
// neither can produce an HTML-special character, so escaping was always a
// no-op for these, but the codegen path must still build and match.
func TestCodegenUnescapedNumericAndBool(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "numeric interpolation", src: "p !{Count}\n", data: map[string]any{"Count": 42}, dataLiteral: "opsData{Count: 42}"},
		{name: "numeric buffered code", src: "p!= Count\n", data: map[string]any{"Count": 42}, dataLiteral: "opsData{Count: 42}"},
		{name: "bool interpolation", src: "p !{Flag}\n", data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "bool buffered code", src: "p!= Flag\n", data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenUnescapedExpression proves a `+`-concatenation expression
// (genValueExpr's supported operator surface) renders unescaped RAW, over
// two string fields each holding an HTML-special character — proving the
// unescaped path composes with genValueExpr's expression support, not just
// its bare-field leaf.
func TestCodegenUnescapedExpression(t *testing.T) {
	t.Parallel()
	data := map[string]any{"Str1": "<a>", "Str2": "<b>"}
	dataLiteral := `opsData{Str1: "<a>", Str2: "<b>"}`

	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "unescaped interpolation expression", src: "p !{Str1 + Str2}\n", want: "<p><a><b></p>"},
		{name: "unescaped buffered code expression", src: "p!= Str1 + Str2\n", want: "<p><a><b></p>"},
	}

	var diffCases []diffCase
	var interpWants []string
	for _, tc := range cases {
		ast, err := Parse(tc.src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.src, err)
		}
		generated, err := GenerateGo(ast, Config{
			PackageName:     "main",
			FuncName:        "RenderOps",
			DataType:        "opsData",
			DataReflectType: opsDataReflectType,
		})
		if err != nil {
			t.Fatalf("GenerateGo(%q): %v", tc.src, err)
		}

		tmpl, err := Compile(tc.src, nil)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.src, err)
		}
		interpWant, err := tmpl.Render(data)
		if err != nil {
			t.Fatalf("interpreter Render(%q): %v", tc.src, err)
		}

		diffCases = append(diffCases, diffCase{name: tc.name, generated: generated, dataLiteral: dataLiteral})
		interpWants = append(interpWants, interpWant)
	}

	results := runDifferentialBatch(t, opsDataStructSrc, "RenderOps", diffCases)
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := results[i]
			if result.Err != "" {
				t.Fatalf("generated RenderOps(%q): unexpected error %q", tc.src, result.Err)
			}
			if result.Out != interpWants[i] {
				t.Errorf("codegen output %q does not match interpreter output %q for %q", result.Out, interpWants[i], tc.src)
			}
			if result.Out != tc.want {
				t.Errorf("codegen output %q does not match expected %q for %q", result.Out, tc.want, tc.src)
			}
		})
	}
}

// TestCodegenUnescapedFallibleSuccess proves genFallibleExtraction's
// prelude — the same extraction the escaped path uses — composes correctly
// with the unescaped write: a successful division renders the quotient with
// no extraction machinery leaking into the output, for both `!{}` and `!=`.
func TestCodegenUnescapedFallibleSuccess(t *testing.T) {
	t.Parallel()
	data := map[string]any{"Count": 10}
	dataLiteral := "opsData{Count: 10}"

	var cases []codegenArithCase
	for _, src := range []string{"p !{Count / 2}\n", "p!= Count / 2\n"} {
		cases = append(cases, codegenArithCase{name: src, src: src, data: data, dataLiteral: dataLiteral})
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenUnescapedFallibleError proves error parity for the unescaped
// forms: both the interpreter and the generated code abort with the
// identical "division by zero" error for a fallible unescaped expression —
// exactly the same error-propagation shape the escaped path already proves
// in codegen_fallible_test.go.
func TestCodegenUnescapedFallibleError(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"p !{Count / Zero}\n", "p!= Count / Zero\n"} {
		t.Run(src, func(t *testing.T) {
			runCodegenFallibleErrorDifferential(t, codegenFallibleErrorCase{
				name:        src,
				src:         src,
				data:        map[string]any{"Count": 10, "Zero": 0},
				dataLiteral: "opsData{Count: 10, Zero: 0}",
			})
		})
	}
}

// TestCodegenUnescapedUnsupportedExpressionSameSurface proves the unescaped
// supported expression set is co-extensive with the escaped one: an array
// literal, a construct genValueExpr already defers for the escaped forms,
// is rejected identically — with the SAME error message — for the unescaped
// forms, proving no new deferral (and no new acceptance) was introduced.
func TestCodegenUnescapedUnsupportedExpressionSameSurface(t *testing.T) {
	genErr := func(t *testing.T, src string) string {
		t.Helper()
		ast, err := Parse(src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		_, err = GenerateGo(ast, Config{
			PackageName:     "gopug",
			FuncName:        "RenderOps",
			DataType:        "opsData",
			DataReflectType: opsDataReflectType,
		})
		if err == nil {
			t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", src)
		}
		return err.Error()
	}

	escapedInterp := genErr(t, "p #{[1, 2, 3]}\n")
	unescapedInterp := genErr(t, "p !{[1, 2, 3]}\n")
	if unescapedInterp != escapedInterp {
		t.Errorf("unescaped interpolation error %q does not match escaped interpolation error %q for the same unsupported expression", unescapedInterp, escapedInterp)
	}
	if !strings.Contains(unescapedInterp, "unsupported") {
		t.Errorf("unescaped interpolation error %q does not describe an unsupported construct", unescapedInterp)
	}

	escapedCode := genErr(t, "p= [1, 2, 3]\n")
	unescapedCode := genErr(t, "p!= [1, 2, 3]\n")
	if unescapedCode != escapedCode {
		t.Errorf("unescaped buffered code error %q does not match escaped buffered code error %q for the same unsupported expression", unescapedCode, escapedCode)
	}
	if !strings.Contains(unescapedCode, "unsupported") {
		t.Errorf("unescaped buffered code error %q does not describe an unsupported construct", unescapedCode)
	}
}

// TestCodegenUnescapedAttributeClassAndBooleanStillDeferred pins that an
// ordinary unescaped attribute (`div(x!= f)`) is supported by this slice —
// see codegen_unescaped_attr_test.go for its differential proofs — while an
// unescaped dynamic class attribute and an unescaped HTML boolean attribute
// each still hard-error with their own distinct messages, unchanged.
func TestCodegenUnescapedAttributeClassAndBooleanStillDeferred(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "unescaped dynamic class attribute", src: "div(class!=Name)\n"},
		{name: "unescaped boolean attribute", src: "input(checked!=Flag)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported unescaped-attribute error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unescaped attribute") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported unescaped attribute", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenUnescapedOnlyTemplateImportGating proves a template using ONLY
// unescaped output (no escaped `#{}`/`=` node anywhere) still compiles and
// runs without an unused "html" import: genInterpolation/genCode's
// unescaped arm never sets g.needsHTML, since it never emits an
// html.EscapeString call.
func TestCodegenUnescapedOnlyTemplateImportGating(t *testing.T) {
	t.Parallel()
	src := "p !{Name}\np!= Count\n"
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

	if strings.Contains(string(generated), `"html"`) {
		t.Errorf("GenerateGo(%q) imports \"html\" even though the template contains only unescaped output:\n%s", src, generated)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Name": "hello", "Count": 42})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, `opsData{Name: "hello", Count: 42}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenUnescapedAlongsideEscapedImportGating proves the "html" import
// is still emitted when a template mixes unescaped and escaped output — the
// unescaped arm's needsHTML skip must not suppress the import an escaped
// sibling node in the SAME template still needs.
func TestCodegenUnescapedAlongsideEscapedImportGating(t *testing.T) {
	t.Parallel()
	src := "p !{Name}\np= Name\n"
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

	if !strings.Contains(string(generated), `"html"`) {
		t.Errorf("GenerateGo(%q) does not import \"html\" even though the template contains an escaped node:\n%s", src, generated)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Name": `<b>&"x"`})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, "opsData{Name: `<b>&\"x\"`}")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}
