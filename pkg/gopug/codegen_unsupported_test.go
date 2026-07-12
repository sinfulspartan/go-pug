package gopug

import "testing"

// TestCodegenUnsupported asserts that constructs outside this increment's
// grammar subset return a clear "unsupported" error from GenerateGo rather
// than silently emitting output that doesn't match the interpreter — mixins,
// includes, unless, else-if chains, dynamic attributes, and unescaped
// interpolation are all later increments per the codegen design doc.
func TestCodegenUnsupported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "dynamic attribute value",
			src:  "a(href=Link)",
		},
		{
			name: "unescaped interpolation",
			src:  "p !{Raw}",
		},
		{
			name: "unless",
			src:  "unless Flag\n  p Off\n",
		},
		{
			name: "else-if chain",
			src:  "if A\n  p one\nelse if B\n  p two\n",
		},
		{
			name: "each with index variable",
			src:  "each item, i in Items\n  p #{item.Label}\n",
		},
		{
			name: "each with an empty-collection else",
			src:  "each item in Items\n  p #{item.Label}\nelse\n  p none\n",
		},
		{
			name: "mixin declaration",
			src:  "mixin foo()\n  p hi\n",
		},
		{
			name: "include",
			src:  "include other.pug\n",
		},
		{
			name: "subtraction operator in interpolation",
			src:  "p #{A - B}",
		},
		{
			name: "method call in interpolation",
			src:  "p #{Name.toUpperCase()}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName: "gopug",
				FuncName:    "RenderUnsupported",
				DataType:    "SkelData",
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
		})
	}
}

// TestCodegenGenerateGoRequiresConfig asserts GenerateGo rejects a Config
// missing any of its three required fields, rather than emitting a broken
// package/function declaration.
func TestCodegenGenerateGoRequiresConfig(t *testing.T) {
	ast, err := Parse("p hi\n", nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	cases := []Config{
		{FuncName: "Render", DataType: "Data"},
		{PackageName: "pkg", DataType: "Data"},
		{PackageName: "pkg", FuncName: "Render"},
	}

	for _, cfg := range cases {
		if _, err := GenerateGo(ast, cfg); err == nil {
			t.Errorf("GenerateGo(%+v): expected an error for incomplete Config, got nil", cfg)
		}
	}
}
