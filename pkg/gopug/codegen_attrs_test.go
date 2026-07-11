package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// TestCodegenAttrUnsupported asserts that attribute shapes explicitly out of
// scope for this increment — a dynamic class value, a style object, an
// &attributes spread, an unescaped attribute, and an operator-valued
// attribute — all return a descriptive "unsupported" error rather than
// emitting output that might not match the interpreter's renderTag. Only
// the tractable slice (a dynamic scalar value on a non-class attribute, and
// a boolean attribute driven by a bool field) is supported; everything here
// is a later increment.
func TestCodegenAttrUnsupported(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		wantMessage string
	}{
		{
			name:        "dynamic class attribute",
			src:         "div(class=Name)",
			wantMessage: "class",
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
			name:        "operator-valued attribute",
			src:         "div(title=Count + 1)",
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
// remain supported there.
func TestCodegenAttrNilReflectTypeDynamicUnsupported(t *testing.T) {
	ast, err := Parse("a(href=Link)", nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	_, err = GenerateGo(ast, Config{
		PackageName: "gopug",
		FuncName:    "RenderAttrUnsupported",
		DataType:    "SkelData",
	})
	if err == nil {
		t.Fatalf("GenerateGo: expected an unsupported-construct error for a dynamic attribute with a nil DataReflectType, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo: error %q does not describe an unsupported construct", err.Error())
	}
}
