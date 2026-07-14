package gopug

import "testing"

// TestCodegenUnsupported asserts that constructs outside this increment's
// grammar subset return a clear "unsupported" error from GenerateGo rather
// than silently emitting output that doesn't match the interpreter — mixins,
// includes, unless, and dynamic attributes are all later increments per the
// codegen design doc; a plain else-if chain is now supported (see
// codegen_comment_elseif_test.go), so the case below instead covers an
// else-if chain whose OWN condition is still outside genCondition's grammar,
// proving that error still propagates (fail-closed) through the else-if
// recursion. An each-loop index variable over a slice/array FIELD collection
// is likewise no longer in this list — it is now supported (see
// codegen_each_index_test.go) — so the "each with index variable" case below
// was changed to an each-index over an ARRAY-LITERAL collection, which
// remains unsupported (see TestCodegenEachIndexArrayLiteralDeferred).
// Unescaped interpolation (`!{expr}`) is no longer in this list either — it
// is now supported, over the same expression surface as escaped
// interpolation, including in type-blind mode (a nil Config.DataReflectType)
// for a bare field reference — see codegen_unescaped_test.go.
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
			name: "unless",
			src:  "unless Flag\n  p Off\n",
		},
		{
			name: "else-if chain whose own condition is unsupported",
			src:  "if A\n  p one\nelse if B + 1\n  p two\n",
		},
		{
			name: "each with index variable over an array-literal collection",
			src:  "each item, i in [1, 2, 3]\n  p=item\n",
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
			name: "index expression in interpolation",
			src:  "p #{Items[0]}",
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
