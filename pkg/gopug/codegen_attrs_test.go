package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// TestCodegenAttrUnsupported asserts that attribute shapes explicitly out of
// scope for this increment — a style object, an &attributes spread, an
// unescaped attribute, a method-call-valued attribute and a ternary-valued
// attribute whose condition is a shape genCondition can't yet compile (still
// outside genValueExpr's ternary-condition grammar), and the dynamic-class
// shapes still deferred past the shorthand+bare-string-field merge (a
// ternary/operator class expression, a class object, a class array, and a
// non-string dynamic class token) — all return a descriptive "unsupported"
// error rather than emitting output that might not match the interpreter's
// renderTag. Only the tractable slice (a dynamic value built by genValueExpr
// on a non-class attribute, including a ternary whose condition and branches
// genValueExpr/genCondition both support — see codegen_ternary_test.go — a
// boolean attribute driven by a bool field, and a dynamic class merging
// shorthand tokens with bare string-field tokens) is supported; everything
// here is a later increment.
func TestCodegenAttrUnsupported(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantMessage string
	}{
		{
			name:        "ternary class expression",
			src:         `div(class=Count > 0 ? "a" : "b")`,
			wantMessage: "unsupported",
		},
		{
			name:        "class object",
			src:         "div(class={active: Flag})",
			wantMessage: "unsupported",
		},
		{
			name:        "class array",
			src:         "div(class=[Name, Name])",
			wantMessage: "unsupported",
		},
		{
			name:        "non-string dynamic class token",
			src:         "div(class=Count)",
			wantMessage: "unsupported",
		},
		{
			name:        "style object attribute",
			src:         "div(style={color: 'red'})",
			wantMessage: "unsupported",
		},
		{
			name:        "&attributes spread",
			src:         "div&attributes(Tags)",
			wantMessage: "&attributes",
		},
		{
			name:        "unescaped attribute",
			src:         "div(title!=Name)",
			wantMessage: "unescaped",
		},
		{
			name:        "method-call-valued attribute",
			src:         "div(title=Name.toUpperCase())",
			wantMessage: "unsupported",
		},
		{
			name:        "ternary-valued attribute on a non-class name with an unsupported condition",
			src:         `div(title=(Count + 1) ? "a" : "b")`,
			wantMessage: "unsupported",
		},
		{
			name:        "non-bool value on a boolean attribute name",
			src:         "input(checked=Name)",
			wantMessage: "checked",
		},
	}

	typ := reflect.TypeOf(typeAwareData{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderAttrUnsupported",
				DataType:        "typeAwareData",
				DataReflectType: typ,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Errorf("GenerateGo(%q): error %q does not mention %q", tc.src, err.Error(), tc.wantMessage)
			}
		})
	}
}

// TestCodegenAttrNilReflectTypeDynamicUnsupported asserts that with a nil
// Config.DataReflectType (type-blind mode), a dynamic attribute value — one
// that would be a supported scalar in type-aware mode — is still rejected,
// exactly as in increment 1: without type information the generator cannot
// tell a scalar field from anything else, so only static/bare attributes
// remain supported there. This includes a dynamic class value: without type
// information the generator can't confirm a bare class token resolves to a
// string field, so it stays unsupported in nil mode too.
func TestCodegenAttrNilReflectTypeDynamicUnsupported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "dynamic scalar attribute", src: "a(href=Link)"},
		{name: "dynamic class attribute, no shorthand", src: "div(class=Extra)"},
		{name: "dynamic class attribute, with shorthand", src: "div.card(class=Extra)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName: "gopug",
				FuncName:    "RenderAttrUnsupported",
				DataType:    "SkelData",
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error for a dynamic attribute with a nil DataReflectType, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}
