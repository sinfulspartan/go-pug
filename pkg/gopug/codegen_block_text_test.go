package gopug

import (
	"strings"
	"testing"
)

// TestCodegenBlockTextBasicAndEntityPreservation proves the two headline
// differential cases for dot-block text (`p.`): plain HTML specials in the
// static text are escaped, and an existing entity reference in the source
// (`&amp;`, `&lt;`) is left alone rather than double-escaped — htmlEscapeText's
// entity-aware behavior, reused unchanged at generate time for the static
// TokenText portions of a block.
func TestCodegenBlockTextBasicAndEntityPreservation(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "basic escaped text",
			src:         "p.\n  Hello <b>x</b> & y\n",
			data:        nil,
			dataLiteral: "opsData{}",
		},
		{
			name:        "existing entities not double-escaped",
			src:         "p.\n  a &amp; b &lt; c\n",
			data:        nil,
			dataLiteral: "opsData{}",
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenBlockTextQuotesNotEscaped is the discriminating case: block text
// uses htmlEscapeText, NOT html.EscapeString, so single and double quotes in
// the static text stay completely literal — an implementation that reused
// html.EscapeString here would turn them into &#34;/&#39; and diverge from
// the interpreter. The expected byte string is asserted explicitly (not
// merely "matches the interpreter"), so a codegen bug that happened to match
// some other, wrongly-escaped output would still fail this check.
func TestCodegenBlockTextQuotesNotEscaped(t *testing.T) {
	t.Parallel()
	src := "p.\n  say \"hi\" it's\n"
	want := `<p>say "hi" it's</p>`

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

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	interpWant, err := tmpl.Render(nil)
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}
	if interpWant != want {
		t.Fatalf("interpreter Render(%q) = %q, want %q (probe assumption broken)", src, interpWant, want)
	}

	got := runGeneratedGo(t, generated, "opsData{}")
	if got != want {
		t.Errorf("codegen output %q does not match expected %q for %q", got, want, src)
	}
}

// TestCodegenBlockTextQuotesFaultInjection proves
// TestCodegenBlockTextQuotesNotEscaped is actually exercising the quote-
// preserving behavior, not merely checking the generated code built and ran:
// a deliberately WRONG expected string using `&#34;` (what an html.EscapeString
// mis-implementation would produce) must NOT match the generated output.
func TestCodegenBlockTextQuotesFaultInjection(t *testing.T) {
	t.Parallel()
	src := "p.\n  say \"hi\" it's\n"
	wrongWant := "<p>say &#34;hi&#34; it&#39;s</p>"

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

	got := runGeneratedGo(t, generated, "opsData{}")
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong (html.EscapeString-style) expectation %q for %q", got, wrongWant, src)
	}
}

// TestCodegenBlockTextEscapedInterpolationWithQuote proves a `#{}`
// interpolation inside block text is escaped through the same htmlEscapeText
// semantics as the surrounding static text — including that a literal quote
// in the interpolated value is left alone, exactly like the static-text case.
func TestCodegenBlockTextEscapedInterpolationWithQuote(t *testing.T) {
	t.Parallel()
	src := "p.\n  Hi #{Name}!\n"
	data := map[string]any{"Name": `<b>&"z"`}
	dataLiteral := "opsData{Name: `<b>&\"z\"`}"
	want := `<p>Hi &lt;b&gt;&amp;"z"!</p>`

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

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	interpWant, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}
	if interpWant != want {
		t.Fatalf("interpreter Render(%q) = %q, want %q (probe assumption broken)", src, interpWant, want)
	}

	got := runGeneratedGo(t, generated, dataLiteral)
	if got != want {
		t.Errorf("codegen output %q does not match expected %q for %q", got, want, src)
	}
}

// TestCodegenBlockTextUnescapedInterpolation proves a `!{}` interpolation
// inside block text is written completely raw, with no HTML-entity escaping
// at all, matching Runtime.renderBlockText's TokenInterpolationUnescape case.
func TestCodegenBlockTextUnescapedInterpolation(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "unescaped interpolation",
			src:         "p.\n  Raw !{Name}\n",
			data:        map[string]any{"Name": "<b>raw</b>"},
			dataLiteral: "opsData{Name: `<b>raw</b>`}",
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenBlockTextRawTextScriptContext proves the raw-text-element
// context (script/style) suppresses escaping entirely for BOTH the static
// text and any `#{}` interpolation in a block text child — mirroring
// Runtime.renderTag's inRawTextElement bracket and renderBlockText's own
// context-specific branches exactly.
func TestCodegenBlockTextRawTextScriptContext(t *testing.T) {
	t.Parallel()
	src := "script.\n  var x = \"#{Name}\"; if (a < b) {}\n"
	data := map[string]any{"Name": `a"b`}
	dataLiteral := "opsData{Name: `a\"b`}"
	want := `<script>var x = "a"b"; if (a < b) {}</script>`

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

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	interpWant, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render(%q): %v", src, err)
	}
	if interpWant != want {
		t.Fatalf("interpreter Render(%q) = %q, want %q (probe assumption broken)", src, interpWant, want)
	}

	got := runGeneratedGo(t, generated, dataLiteral)
	if got != want {
		t.Errorf("codegen output %q does not match expected %q for %q", got, want, src)
	}
}

// TestCodegenBlockTextRawFlagRestoredAfterScript proves g.inRawTextElement is
// correctly restored once a raw-text element's children finish generating: a
// `script.` block ahead of a plain `p.` block in the SAME template renders
// the script content raw and the paragraph content escaped, in one pass.
func TestCodegenBlockTextRawFlagRestoredAfterScript(t *testing.T) {
	t.Parallel()
	src := "script.\n  var a = \"x\";\np.\n  Hi <b>#{Name}</b>\n"
	data := map[string]any{"Name": "world"}
	dataLiteral := `opsData{Name: "world"}`

	cases := []codegenArithCase{
		{name: "raw script then escaped paragraph", src: src, data: data, dataLiteral: dataLiteral},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenBlockTextMultiLine proves a multi-line dot-block, mixing plain
// text lines and an interpolation across lines, renders identically to the
// interpreter.
func TestCodegenBlockTextMultiLine(t *testing.T) {
	t.Parallel()
	src := "p.\n  Line one #{Name}\n  Line two <b>bold</b> & more\n"
	cases := []codegenArithCase{
		{
			name:        "multi-line block text",
			src:         src,
			data:        map[string]any{"Name": "Alice"},
			dataLiteral: `opsData{Name: "Alice"}`,
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenBlockTextTagInterpolationDeferred proves a `#[...]` tag
// interpolation inside block text is refused with its own distinct error,
// even though the interpreter renders it fine (reproducing its inner
// sub-lex+parse+render at generate time is a separate, larger claim this
// slice does not attempt).
func TestCodegenBlockTextTagInterpolationDeferred(t *testing.T) {
	src := `p.` + "\n  see #[a(href=\"/x\") link] end\n"

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
		t.Fatalf("GenerateGo(%q): expected an unsupported tag-interpolation error, got nil", src)
	}
	if !strings.Contains(err.Error(), "tag interpolation") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported tag interpolation", src, err.Error())
	}

	// Pin that the interpreter really does render this template successfully
	// (it's valid Pug, not a shared parse failure), confirming this is a
	// genuine codegen deferral rather than an invalid-template rejection.
	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	if _, err := tmpl.Render(nil); err != nil {
		t.Fatalf("interpreter Render(%q): unexpected error %v", src, err)
	}
}

// TestCodegenBlockTextFallibleInterpolationDeferred proves a fallible `#{}`
// interpolation (a top-level division/modulo, the same shape genValueExpr
// flags fallible everywhere else) is refused at generate time, because
// Runtime.renderBlockText silently falls back to the RAW, un-evaluated
// expression source the moment evaluateExpr errors instead of aborting the
// whole render — a fallback a generated function that returns an error on
// that same failure cannot reproduce. The interpreter's own raw-then-escape
// fallback output is pinned here too, to prove the deferral is the correct
// call, not an overly conservative one.
func TestCodegenBlockTextFallibleInterpolationDeferred(t *testing.T) {
	src := "p.\n  #{Count/Zero}\n"

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
		t.Fatalf("GenerateGo(%q): expected an unsupported fallible-interpolation error, got nil", src)
	}
	if !strings.Contains(err.Error(), "fallible") {
		t.Errorf("GenerateGo(%q): error %q does not describe a fallible expression", src, err.Error())
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	got, err := tmpl.Render(map[string]any{"Count": 1, "Zero": 0})
	if err != nil {
		t.Fatalf("interpreter Render(%q): unexpected error %v (the interpreter falls back to the raw expression text rather than erroring)", src, err)
	}
	want := "<p>Count/Zero</p>"
	if got != want {
		t.Errorf("interpreter Render(%q) = %q, want %q (raw-then-escape fallback assumption broken)", src, got, want)
	}
}

// TestCodegenBlockTextUnsupportedExpressionDeferred proves a `#{}`
// interpolation whose expression genValueExpr has no support for at all (an
// array literal) is refused with its own distinct error, separate from the
// fallible-expression deferral above.
func TestCodegenBlockTextUnsupportedExpressionDeferred(t *testing.T) {
	src := "p.\n  #{[1, 2, 3]}\n"

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
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}

// TestCodegenBlockTextRawFlagDoesNotPerturbTextNodeChild proves the new
// g.inRawTextElement flag, set/restored around a raw-text element's children,
// does not perturb an ordinary TextNode child (inline tag text, not a dot-
// block) of a script/style tag: genNode's TextNode case still unconditionally
// gen-time-escapes with htmlEscapeText, unchanged and untouched by this
// slice, matching the interpreter's own (pre-existing, out-of-scope-to-fix)
// behavior of also escaping a TextNode child of a raw-text element.
func TestCodegenBlockTextRawFlagDoesNotPerturbTextNodeChild(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{
			name:        "inline TextNode child of script",
			src:         "script Hello <b>x</b> & y\n",
			data:        nil,
			dataLiteral: "opsData{}",
		},
		{
			name:        "nested TagNode child of script",
			src:         "script\n  span foo\n",
			data:        nil,
			dataLiteral: "opsData{}",
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}
