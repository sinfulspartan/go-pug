package gopug

import (
	"strings"
	"testing"
)

// TestCodegenUnescapedAttrDiscriminatingStatic is the headline static-value
// proof: the same literal, holding HTML-special characters (`&`, `<`),
// renders RAW through an unescaped attribute (`href!="..."`) and HTML-escaped
// through its escaped counterpart (`href="..."`) — proving the codegen static
// path drops the htmlEscapeAttr wrapper for the unescaped form and nothing
// else, exactly as Runtime.renderTag's own `if !val.Unescaped` branch does.
func TestCodegenUnescapedAttrDiscriminatingStatic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "static unescaped literal with specials", src: `a(href!="/x?a=1&b=2<z")` + "\n", want: `<a href="/x?a=1&b=2<z"></a>`},
		{name: "static escaped literal with specials (regression)", src: `a(href="/x?a=1&b=2<z")` + "\n", want: `<a href="/x?a=1&amp;b=2&lt;z"></a>`},
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
		interpWant, err := tmpl.Render(nil)
		if err != nil {
			t.Fatalf("interpreter Render(%q): %v", tc.src, err)
		}

		diffCases = append(diffCases, diffCase{name: tc.name, generated: generated, dataLiteral: "opsData{}"})
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

// TestCodegenUnescapedAttrDiscriminatingDynamic pairs a dynamic unescaped
// attribute with its escaped counterpart over a field holding HTML-special
// characters INCLUDING a quote: the unescaped form writes the field's raw
// bytes verbatim (even the embedded `"`, exactly as Runtime.renderTag does —
// no attribute-boundary repair), while the escaped form runs it through
// EscapeAttr (`&quot;`, not `&#34;` — attribute escaping, not text escaping).
func TestCodegenUnescapedAttrDiscriminatingDynamic(t *testing.T) {
	t.Parallel()
	data := map[string]any{"Name": `<b>&"z`}
	dataLiteral := "opsData{Name: `<b>&\"z`}"

	cases := []struct {
		name string
		src  string
		want string
	}{
		{name: "dynamic unescaped attr with specials and a quote", src: "div(data-x!= Name)\n", want: `<div data-x="<b>&"z"></div>`},
		{name: "dynamic escaped attr with specials and a quote (regression)", src: "div(data-x= Name)\n", want: `<div data-x="&lt;b&gt;&amp;&quot;z"></div>`},
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

// TestCodegenUnescapedAttrFaultInjection proves
// TestCodegenUnescapedAttrDiscriminatingDynamic is actually exercising the
// generated code's output, not merely checking it built and ran: comparing
// the unescaped generated output against the deliberately WRONG (escaped)
// expectation must fail — an implementation that still escaped the value
// would pass this check only if it emitted the escaped bytes, exactly what
// this slice is proving it does not.
func TestCodegenUnescapedAttrFaultInjection(t *testing.T) {
	t.Parallel()
	src := "div(data-x!= Name)\n"
	dataLiteral := "opsData{Name: `<b>&\"z`}"
	wrongWant := `<div data-x="&lt;b&gt;&amp;&quot;z"></div>`

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

// TestCodegenUnescapedAttrFallibleDeferred pins a fallible-attribute finding:
// unlike genInterpolation/genCode's fallible value (which the interpreter aborts on
// error, matching genFallibleExtraction), Runtime.renderTag's own dynamic
// attribute evaluation FALLS BACK to the raw, un-evaluated expression source
// the moment evaluateExpr errors — confirmed for BOTH the escaped and the
// unescaped form of `data-x= Count / Zero`, since they share the identical
// `evaluated` value. A generated function that aborts the whole render on
// that error cannot reproduce a fallback, so a fallible unescaped attribute
// value is refused at generate time with its own distinct message. This
// deferral applies REGARDLESS of whether the division would actually error at
// runtime — fallibility (a top-level `/` or `%`) is a generate-time property
// genValueExpr reports independent of the operands' runtime values, so an
// unescaped attribute dividing by a literal that would never be zero
// (`Count / 2`) is refused exactly like one dividing by zero: only the
// operator shape is known at generate time, not the outcome.
func TestCodegenUnescapedAttrFallibleDeferred(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{name: "division by a field that is zero at runtime", src: "div(data-x!= Count / Zero)\n"},
		{name: "division by a literal that would succeed at runtime", src: "div(data-x!= Count / 2)\n"},
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
			if !strings.Contains(err.Error(), "unescaped attribute") || !strings.Contains(err.Error(), "fallible") {
				t.Errorf("GenerateGo(%q): error %q does not describe a deferred fallible unescaped attribute", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenUnescapedAttrMixedWithEscaped proves an escaped and an
// unescaped attribute coexist correctly on the SAME tag: each attribute's own
// Unescaped flag drives its own write, with no leakage between them — the
// escaped attribute still comes out through EscapeAttr, the unescaped one
// raw, in the same sortAttrNames order Runtime.renderTag itself uses.
func TestCodegenUnescapedAttrMixedWithEscaped(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "one escaped and one unescaped dynamic attribute on the same tag",
		src:         "div(data-a= Str1 data-b!= Str2)\n",
		data:        map[string]any{"Str1": `<a>`, "Str2": `<b>`},
		dataLiteral: "opsData{Str1: `<a>`, Str2: `<b>`}",
	})
}

// TestCodegenUnescapedAttrStaticAndDynamicMixed pairs a static unescaped
// literal attribute with a dynamic escaped attribute on the same tag,
// exercising the static-buffer/dynamic-write interleaving genAttributes's own
// doc comment describes, now with the Unescaped decision folded into the
// static branch too.
func TestCodegenUnescapedAttrStaticAndDynamicMixed(t *testing.T) {
	t.Parallel()
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "static unescaped literal alongside a dynamic escaped attribute",
		src:         `div(title!="<b>" data-y=Str1)` + "\n",
		data:        map[string]any{"Str1": `<c>`},
		dataLiteral: "opsData{Str1: `<c>`}",
	})
}

// TestCodegenUnescapedAttrClassDeferred and
// TestCodegenUnescapedAttrBooleanDeferred are covered by
// TestCodegenUnescapedAttributeClassAndBooleanStillDeferred in
// codegen_unescaped_test.go, which pins both deferrals alongside the newly
// supported ordinary unescaped attribute case.

// TestCodegenUnescapedAttrNilRootTypeDeferred proves a dynamic unescaped
// attribute value still requires field types: with a nil
// Config.DataReflectType (type-blind mode), it hits the same
// nil-rootType deferral a dynamic escaped attribute would, since neither can
// be classified without reflection — only a STATIC unescaped literal is
// type-blind-safe (see TestCodegenUnescapedAttrStaticNilRootType).
func TestCodegenUnescapedAttrNilRootTypeDeferred(t *testing.T) {
	t.Parallel()
	src := "div(data-x!=Name)\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName: "gopug",
		FuncName:    "RenderAttrNilRootType",
		DataType:    "map[string]any",
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported dynamic-attribute error with a nil DataReflectType, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}

// TestCodegenUnescapedAttrStaticNilRootType proves the flip side: a STATIC
// unescaped literal attribute needs no field types at all, so it compiles
// and matches the interpreter even with a nil Config.DataReflectType.
func TestCodegenUnescapedAttrStaticNilRootType(t *testing.T) {
	t.Parallel()
	src := `a(href!="/x?a=1&b=2")` + "\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName: "main",
		FuncName:    "RenderOps",
		DataType:    "map[string]any",
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, "map[string]any{}")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenUnescapedOnlyAttributeImportGating proves a tag using ONLY an
// unescaped dynamic attribute (no other node needing the "gopug" import)
// still compiles: the unescaped attribute write never calls gopug.EscapeAttr,
// so it alone must not force the import.
func TestCodegenUnescapedOnlyAttributeImportGating(t *testing.T) {
	t.Parallel()
	src := "div(data-x!= Name)\n"
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

	if strings.Contains(string(generated), `"github.com/sinfulspartan/go-pug/pkg/gopug"`) {
		t.Errorf("GenerateGo(%q) imports \"gopug\" even though the template's only attribute is unescaped:\n%s", src, generated)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Name": "hello"})
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, `opsData{Name: "hello"}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}
