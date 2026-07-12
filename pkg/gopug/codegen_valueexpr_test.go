package gopug

import (
	"fmt"
	"strings"
	"testing"
)

// TestCodegenValueExprBufferedCodeStringConcat is the increment's headline
// case: a buffered code node (`= expr`) mixing a string literal with an int
// field through `+`. The literal ("Total: ") never parses as a number, so
// gopug.Add falls to string concatenation regardless of what the field
// holds — this proves the previously-unsupported buffered CodeNode is now
// wired through genValueExpr.
func TestCodegenValueExprBufferedCodeStringConcat(t *testing.T) {
	src := "p= \"Total: \" + Count\n"

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
	want, err := tmpl.Render(map[string]any{"Count": 5})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, "opsData{Count: 5}")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenValueExprInterpolationNestedPlus proves #{a + b} routes through
// genValueExpr and reproduces evaluateExpr's left-to-right `+` splitting for
// a nested expression combining two string fields with a literal separator.
func TestCodegenValueExprInterpolationNestedPlus(t *testing.T) {
	src := "p #{Str1 + \" \" + Str2}\n"

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
	want, err := tmpl.Render(map[string]any{"Str1": "Jane", "Str2": "Doe"})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, `opsData{Str1: "Jane", Str2: "Doe"}`)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenValueExprAddRuntimeDisambiguation is the proof that gopug.Add
// reproduces the interpreter's `+` operator exactly where it matters most:
// two string fields holding numeric-looking text add numerically ("5"+"3"
// is the number 8), but the same expression over non-numeric text
// concatenates ("a"+"b" is the string "ab") — a distinction that cannot be
// resolved from the fields' static Go type (both are plain strings), only
// from their runtime values, which is exactly what gopug.Add checks.
func TestCodegenValueExprAddRuntimeDisambiguation(t *testing.T) {
	src := "p= Str1 + Str2\n"

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

	cases := []struct {
		name       string
		str1, str2 string
		wantHTML   string
	}{
		{name: "both numeric-looking sums numerically", str1: "5", str2: "3", wantHTML: "<p>8</p>"},
		{name: "non-numeric concatenates", str1: "a", str2: "b", wantHTML: "<p>ab</p>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := tmpl.Render(map[string]any{"Str1": tc.str1, "Str2": tc.str2})
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}
			if want != tc.wantHTML {
				t.Fatalf("interpreter Render sanity check: got %q, want %q", want, tc.wantHTML)
			}

			dataLiteral := fmt.Sprintf("opsData{Str1: %q, Str2: %q}", tc.str1, tc.str2)
			got := runGeneratedGo(t, generated, dataLiteral)
			if got != want {
				t.Errorf("codegen output %q does not match interpreter output %q (Str1=%q, Str2=%q)", got, want, tc.str1, tc.str2)
			}
		})
	}
}

// TestCodegenValueExprLeaves proves every leaf shape genValueExpr supports —
// a bare field of each scalar kind, a quoted string literal, a numeric
// literal (including a leading-zero token whose Go and JS readings
// disagree), and the true/false/null keywords — renders through `= expr`
// identically to the interpreter.
func TestCodegenValueExprLeaves(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		data        map[string]any
		dataLiteral string
	}{
		{name: "int field", src: "p= Count\n", data: map[string]any{"Count": 42}, dataLiteral: "opsData{Count: 42}"},
		{name: "float64 field", src: "p= Price\n", data: map[string]any{"Price": 9.5}, dataLiteral: "opsData{Price: 9.5}"},
		{name: "bool field", src: "p= Flag\n", data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "string field", src: "p= Name\n", data: map[string]any{"Name": "World"}, dataLiteral: `opsData{Name: "World"}`},
		{name: "string literal", src: "p= \"hello\"\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "numeric literal", src: "p= 42\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "leading-zero numeric literal (JS octal, not Go octal)", src: "p= 0100\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "true keyword", src: "p= true\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "false keyword", src: "p= false\n", data: map[string]any{}, dataLiteral: "opsData{}"},
		{name: "null keyword", src: "p= null\n", data: map[string]any{}, dataLiteral: "opsData{}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			want, err := tmpl.Render(tc.data)
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			got := runGeneratedGo(t, generated, tc.dataLiteral)
			if got != want {
				t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, tc.src)
			}
		})
	}
}

// TestCodegenValueExprUnsupported asserts that every construct outside this
// increment's value-context grammar — every operator besides `-`, `+`, `*`,
// `/`, `%`, a top-level ternary, and `||`/`&&`/`!`/comparison, an
// array/object literal, a still-deferred method call, an unbuffered code
// statement, and unescaped buffered output — is rejected with a clear
// "unsupported" error rather than silently emitting something that might not
// match the interpreter. A template literal itself is no longer in this list
// (genTemplateLiteral now supports it, see codegen_valueexpr_template_test.go),
// but one whose `${...}` interpolation contains a construct genValueExpr
// still can't build (a deferred method call, here) still propagates that
// "unsupported" error. Subtraction and multiplication are also no longer in
// this list — see TestCodegenValueExprArithmetic. Division and modulo are no
// longer in this list either — a standalone `/`/`%` is now supported
// (fallible) and proven by differential build+run in codegen_fallible_test.go,
// and so is composing a fallible `/`/`%` result into an arithmetic combiner, a
// nested `/`/`%` operand, a ternary branch, or a template-literal `${}` part —
// see codegen_fallible_compose_test.go. A top-level ternary is no longer in
// this list either — see codegen_ternary_test.go — but a ternary whose
// CONDITION is a shape genCondition can't compile (here, arithmetic) still
// propagates an error, since genValueExpr's ternary support reuses
// genCondition unchanged for the condition. `||`, `&&`, `!`, and comparison
// are no longer in this list either — see codegen_logical_value_test.go for
// the differential default-value-idiom, short-circuit, and
// FormatBool(genCondition) proofs. A string-method call (`.toUpperCase()`,
// `.trim()`, `.split(',')`, …) is also no longer in this list — see
// codegen_methods_test.go — but `.join`/`.toFixed`/`.toPrecision` and an
// unrecognized method name stay deferred/unsupported, exercised here. An
// index expression (`arr[i]`) and value-context `.length` are also no longer
// in this list — see codegen_index_length_test.go — though a
// non-string-keyed map index and an index-then-dot receiver stay deferred
// there.
func TestCodegenValueExprUnsupported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "ternary with an unsupported (arithmetic) condition", src: "p= (Count + 1) ? \"yes\" : \"no\"\n"},
		{name: "deferred method call", src: "p= Name.toFixed(2)\n"},
		{name: "unknown method call", src: "p= Name.frobnicate()\n"},
		{name: "template literal with a deferred ${} method call", src: "p= `hello ${Name.toFixed(2)}`\n"},
		{name: "array literal", src: "p= [1, 2, 3]\n"},
		{name: "object literal", src: "p= {a: 1}\n"},
		{name: "unbuffered code statement", src: "- var x = 1\n"},
		{name: "unescaped buffered output", src: "p!= Count\n"},
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
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// Composing a fallible `/`/`%` result into an arithmetic combiner, a nested
// `/`/`%` operand, a ternary branch, or a template-literal `${}` part is no
// longer a deferral either — see codegen_fallible_compose_test.go for the
// differential build+run proofs (including the ternary short-circuit and
// left-to-right error-order proofs).
