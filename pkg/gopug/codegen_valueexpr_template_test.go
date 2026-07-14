package gopug

import (
	"testing"
)

// TestCodegenTemplateLiteralValueContext proves a backtick template literal
// in value context (`= expr`) interpolates two fields of different types
// (a string and an int) into the surrounding literal text, matching the
// interpreter's own template-literal walk.
func TestCodegenTemplateLiteralValueContext(t *testing.T) {
	t.Parallel()
	src := "p= `Hello ${Name}, item ${Count}`\n"

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
	want, err := tmpl.Render(map[string]any{"Name": "Jane", "Count": 5})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, `opsData{Name: "Jane", Count: 5}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenTemplateLiteralInterpolationPosition proves a backtick template
// literal used inside a `#{...}` interpolation — itself containing a nested
// `${...}` whose own inner expression is a `+` — routes through the same
// genValueExpr recursion, exercising the brace-nesting the lexer's own
// scanBalancedBraces must also get right to hand genValueExpr the raw
// backtick text intact.
func TestCodegenTemplateLiteralInterpolationPosition(t *testing.T) {
	t.Parallel()
	src := "p #{`x=${Count + 1}`}\n"

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
	want, err := tmpl.Render(map[string]any{"Count": 4})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, "opsData{Count: 4}")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenTemplateLiteralBacktickEscape proves that a backslash
// immediately before a backtick, inside a template literal, is treated as an
// escape and passes the backtick through literally rather than closing the
// literal early, matching Runtime.evaluateExpr's own escape handling
// exactly.
func TestCodegenTemplateLiteralBacktickEscape(t *testing.T) {
	t.Parallel()
	src := "p= `It\\`s ${Name}`\n"

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
	want, err := tmpl.Render(map[string]any{"Name": "Jane"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != "<p>It`s Jane</p>" {
		t.Fatalf("interpreter Render sanity check: got %q, want the literal backtick preserved", want)
	}

	got := runGeneratedGo(t, generated, `opsData{Name: "Jane"}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenTemplateLiteralEmpty proves an empty template literal (a
// backtick immediately followed by a closing backtick, with no content)
// evaluates to the empty string in both value and attribute position.
func TestCodegenTemplateLiteralEmpty(t *testing.T) {
	t.Parallel()
	cases := []codegenArithCase{
		{name: "value context", src: "p= ``\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "attribute value", src: "a(href=``) Link\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenAttrValueExprConcat proves the general dynamic-attribute-value
// path now builds its value with genValueExpr rather than a bare
// resolveFieldExpr lookup, so a `+` concatenation of a string literal and a
// string field works as an attribute value.
func TestCodegenAttrValueExprConcat(t *testing.T) {
	t.Parallel()
	src := `a(href="/x/" + Slug) Link` + "\n"

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
	want, err := tmpl.Render(map[string]any{"Slug": "about"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, `opsData{Slug: "about"}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenAttrTemplateLiteral proves an attribute value built from a
// backtick template literal works both for a plain int field and for a
// dot-path rooted at an each-loop variable resolving into a nested struct's
// int field — the diagnostic's headline attr-template-literal gap.
func TestCodegenAttrTemplateLiteral(t *testing.T) {
	t.Parallel()
	sharedDataLiteral := "opsData{Count: 7, Firms: []opsFirm{{ID: 7}, {ID: 42}}}"
	cases := []codegenArithCase{
		{
			name:        "plain int field",
			src:         "div(id=`row-${Count}`) Row\n",
			data:        map[string]any{"Count": 7},
			dataLiteral: sharedDataLiteral,
		},
		{
			name: "dot-path rooted at an each-loop variable into a nested struct field",
			src:  "each firm in Firms\n  a(href=`/admin/firms/${firm.ID}`) Firm\n",
			data: map[string]any{"Firms": []any{
				map[string]any{"ID": 7},
				map[string]any{"ID": 42},
			}},
			dataLiteral: sharedDataLiteral,
		},
	}
	runCodegenArithDifferentialBatch(t, cases)
}

// TestCodegenAttrTemplateLiteralEntitySafety proves the attribute value
// built by a template literal is concatenated first (plain, unescaped) and
// EscapeAttr applied exactly once to the whole result — not per interpolated
// leaf — by interpolating a field whose value contains HTML-special
// characters and comparing against the interpreter, whose renderTag applies
// EscapeAttr the same way (once, to evaluateExpr's already-concatenated
// return value).
func TestCodegenAttrTemplateLiteralEntitySafety(t *testing.T) {
	t.Parallel()
	src := "a(href=`/x/${Name}`) Link\n"

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
	want, err := tmpl.Render(map[string]any{"Name": `a&b"c`})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, `opsData{Name: "a&b\"c"}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenAttrValueExprOperator proves an operator-valued attribute
// (previously rejected outright since the general attribute path only
// resolved a bare field) now renders correctly, since it too is built by
// genValueExpr.
func TestCodegenAttrValueExprOperator(t *testing.T) {
	t.Parallel()
	src := "div(title=Count + 1) Widget\n"

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
	want, err := tmpl.Render(map[string]any{"Count": 4})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, "opsData{Count: 4}")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}
