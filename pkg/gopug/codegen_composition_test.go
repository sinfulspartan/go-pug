package gopug

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// compositionData is the declared struct the dynamic-content and
// nested-block composition differential tests resolve field expressions
// against: Flag drives an `if`/`else` inside a block body and a dynamic
// non-boolean attribute value, Title drives a `#{}` interpolation inside a
// block body.
type compositionData struct {
	Flag  bool
	Title string
}

var compositionDataReflectType = reflect.TypeOf(compositionData{})

// compositionDataStructSrc is compositionData's field declarations, spliced
// verbatim into the throwaway module runComposedGo assembles around a
// GenerateGo result — it must match compositionData above field for field.
const compositionDataStructSrc = `type compositionData struct {
	Flag  bool
	Title string
}
`

// runComposedGo writes generated (a GenerateGo result whose Config.FuncName
// is funcName) into a throwaway module alongside structSrc's type
// declaration (empty for a type-blind "map[string]any" test) and a main()
// that builds dataLiteral, calls funcName(os.Stdout, d), builds the module,
// and runs the resulting program, returning its captured stdout. This is
// codegen_ops_test.go's runGeneratedGo generalized to an arbitrary
// struct/data-literal/func-name so the composition tests below aren't tied
// to opsData. repoModuleReplaceDirectives (defined in codegen_ops_test.go,
// same package) supplies the replace directive that lets the throwaway
// module resolve this repository's own gopug package.
func runComposedGo(t *testing.T, generated []byte, structSrc, dataLiteral, funcName string) string {
	t.Helper()

	dir := t.TempDir()
	goMod := "module compbuild\n\ngo 1.26\n" + repoModuleReplaceDirectives(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	genStr := string(generated)
	funcIdx := strings.Index(genStr, "\nfunc ")
	if funcIdx < 0 {
		t.Fatalf("generated source has no \"func \" to splice the struct declaration before:\n%s", genStr)
	}

	var src strings.Builder
	src.WriteString(genStr[:funcIdx])
	src.WriteString("\n\nimport \"os\"\n\n")
	src.WriteString(structSrc)
	src.WriteString(genStr[funcIdx:])
	src.WriteString("\nfunc main() {\n\td := ")
	src.WriteString(dataLiteral)
	src.WriteString("\n\tif err := " + funcName + "(os.Stdout, d); err != nil {\n\t\tpanic(err)\n\t}\n}\n")

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatalf("writing main.go: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run on generated code failed:\n%s\n--- source ---\n%s", out, src.String())
	}
	return string(out)
}

// mustWriteFile writes content to filepath.Join(dir, name), failing the test
// on error.
func mustWriteFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// compareComposedOutput is the differential-test core every case below
// shares: it renders childPath through the interpreter (gopug.RenderFile),
// separately runs it through Parse → ResolveComposition → GenerateGo →
// build+run, and asserts the two outputs are byte-identical. It returns the
// interpreter's own output so a caller can add further assertions on it
// (e.g. pinning down append/prepend order), since the differential
// comparison alone can't distinguish "both sides agree" from "both sides
// agree on the wrong thing."
func compareComposedOutput(t *testing.T, dir, childPath string, interpData map[string]any, dataType, structSrc, dataLiteral string, reflectType reflect.Type) string {
	t.Helper()

	want, err := RenderFile(childPath, interpData, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("interpreter RenderFile: %v", err)
	}

	src, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatalf("reading child template: %v", err)
	}

	opts := &Options{Basedir: dir}
	ast, err := Parse(string(src), opts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	resolved, err := ResolveComposition(ast, opts)
	if err != nil {
		t.Fatalf("ResolveComposition: %v", err)
	}

	generated, err := GenerateGo(resolved, Config{
		PackageName:     "main",
		FuncName:        "Render",
		DataType:        dataType,
		DataReflectType: reflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	got := runComposedGo(t, generated, structSrc, dataLiteral, "Render")

	if got != want {
		t.Errorf("codegen output does not match interpreter output.\ncodegen output:     %q\ninterpreter output: %q", got, want)
	}
	return want
}

// TestResolveCompositionReplaceAndDefault covers the basic extends+block
// case: a layout with two blocks, a child that overrides one (block
// replace, the default `block name` form) and leaves the other at its
// parent default.
func TestResolveCompositionReplaceAndDefault(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", "doctype html\nhtml\n  body\n    block content\n      p Default Content\n    block sidebar\n      p Default Sidebar\n")
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\nblock content\n  p Child Content\n")

	want := compareComposedOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}", nil)

	if !strings.Contains(want, "Child Content") {
		t.Errorf("interpreter output %q does not contain the overriding block's content", want)
	}
	if strings.Contains(want, "Default Content") {
		t.Errorf("interpreter output %q still contains the replaced block's default content", want)
	}
	if !strings.Contains(want, "Default Sidebar") {
		t.Errorf("interpreter output %q does not contain the unoverridden block's default content", want)
	}
}

// TestResolveCompositionAppendPrepend proves the merged order matches the
// interpreter for a child that both prepends and appends to the same
// parent block: [prepend, parent-default, append].
func TestResolveCompositionAppendPrepend(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", "doctype html\nhtml\n  body\n    block content\n      p Default\n")
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\nblock prepend content\n  p Before\nblock append content\n  p After\n")

	want := compareComposedOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}", nil)

	beforeIdx := strings.Index(want, "Before")
	defaultIdx := strings.Index(want, "Default")
	afterIdx := strings.Index(want, "After")
	if beforeIdx < 0 || defaultIdx < 0 || afterIdx < 0 {
		t.Fatalf("interpreter output %q is missing one of the prepend/default/append segments", want)
	}
	if !(beforeIdx < defaultIdx && defaultIdx < afterIdx) {
		t.Errorf("interpreter output %q does not preserve [prepend, default, append] order", want)
	}
}

// TestResolveCompositionMultiLevelChain proves a three-level extends chain
// (grandchild extends child extends layout) resolves the same way through
// codegen as it does through the interpreter: layout's default body,
// child's prepend, child's append, and grandchild's own further append all
// land in [child-prepend, layout-default, child-append, grandchild-append]
// order.
func TestResolveCompositionMultiLevelChain(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", "doctype html\nhtml\n  body\n    block content\n      p Layout Default\n")
	mustWriteFile(t, dir, "child.pug", "extends layout\nblock prepend content\n  p Child Before\nblock append content\n  p Child After\n")
	grandchildPath := mustWriteFile(t, dir, "grandchild.pug", "extends child\nblock append content\n  p Grandchild After\n")

	want := compareComposedOutput(t, dir, grandchildPath, map[string]any{}, "map[string]any", "", "map[string]any{}", nil)

	idxBefore := strings.Index(want, "Child Before")
	idxDefault := strings.Index(want, "Layout Default")
	idxChildAfter := strings.Index(want, "Child After")
	idxGrandchildAfter := strings.Index(want, "Grandchild After")
	if idxBefore < 0 || idxDefault < 0 || idxChildAfter < 0 || idxGrandchildAfter < 0 {
		t.Fatalf("interpreter output %q is missing a segment from the multi-level chain", want)
	}
	if !(idxBefore < idxDefault && idxDefault < idxChildAfter && idxChildAfter < idxGrandchildAfter) {
		t.Errorf("interpreter output %q does not preserve the expected chained order", want)
	}
}

// TestResolveCompositionNestedBlockDeepWalk proves the deep walk reaches a
// block nested inside a *TagNode (block content, inside div.wrapper) and a
// block nested inside a *ConditionalNode (block sidebar, inside `if Flag`)
// exactly as applyBlockOverrides does at render time: the tag-nested block
// is overridden by the child, the conditional-nested block is left at its
// parent default, and both must still be reachable through the conditional
// branch that actually renders.
func TestResolveCompositionNestedBlockDeepWalk(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", strings.Join([]string{
		"doctype html",
		"html",
		"  body",
		"    div.wrapper",
		"      block content",
		"        p Default Content",
		"    if Flag",
		"      section",
		"        block sidebar",
		"          p Default Sidebar",
		"",
	}, "\n"))
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\nblock content\n  p Nested Override\n")

	want := compareComposedOutput(
		t, dir, childPath,
		map[string]any{"Flag": true},
		"compositionData", compositionDataStructSrc, "compositionData{Flag: true}",
		compositionDataReflectType,
	)

	if !strings.Contains(want, "Nested Override") {
		t.Errorf("interpreter output %q does not contain the tag-nested block's override", want)
	}
	if !strings.Contains(want, "Default Sidebar") {
		t.Errorf("interpreter output %q does not contain the conditional-nested block's default body", want)
	}
}

// TestResolveCompositionDynamicBlockBody proves a block body containing
// `#{}` interpolation, an `if`/`else` condition, and a dynamic (non-boolean)
// attribute value generates through the normal typed codegen pipeline
// against a declared struct, once ResolveComposition has spliced the block
// body into the tree.
func TestResolveCompositionDynamicBlockBody(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", "doctype html\nhtml\n  body\n    block content\n      p Default\n")
	childPath := mustWriteFile(t, dir, "child.pug", strings.Join([]string{
		"extends layout",
		"block content",
		"  p Hello, #{Title}!",
		"  if Flag",
		"    p.on On",
		"  else",
		"    p.off Off",
		"  div(data-flag=Flag) Attr",
		"",
	}, "\n"))

	compareComposedOutput(
		t, dir, childPath,
		map[string]any{"Title": "World", "Flag": true},
		"compositionData", compositionDataStructSrc, `compositionData{Title: "World", Flag: true}`,
		compositionDataReflectType,
	)

	compareComposedOutput(
		t, dir, childPath,
		map[string]any{"Title": "O'Brien & <Sons>", "Flag": false},
		"compositionData", compositionDataStructSrc, `compositionData{Title: "O'Brien & <Sons>", Flag: false}`,
		compositionDataReflectType,
	)
}

// TestResolveCompositionNoExtendsStandaloneBlock proves ResolveComposition
// is safe to call on a template with no extends at all: resolveExtendsAST
// returns it unchanged, and the block-flattening pass still reduces the
// standalone block to its own body, matching renderBlockBody rendering it
// directly.
func TestResolveCompositionNoExtendsStandaloneBlock(t *testing.T) {
	src := "doctype html\nhtml\n  body\n    block content\n      p Standalone Default\n"

	want, err := Render(src, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolved, err := ResolveComposition(ast, nil)
	if err != nil {
		t.Fatalf("ResolveComposition: %v", err)
	}
	generated, err := GenerateGo(resolved, Config{
		PackageName: "main",
		FuncName:    "Render",
		DataType:    "map[string]any",
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	got := runComposedGo(t, generated, "", "map[string]any{}", "Render")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q", got, want)
	}
	if !strings.Contains(want, "Standalone Default") {
		t.Errorf("interpreter output %q does not contain the standalone block's own body", want)
	}
}

// TestResolveCompositionDeferredInclude proves that a child using both
// extends+block and include stays deferred: ResolveComposition succeeds
// (extends+block is fully resolved, the IncludeNode is simply left in
// place — include resolution is a later codegen increment), and GenerateGo
// then returns a clean "unsupported" error on the leftover IncludeNode
// rather than silently mis-generating.
func TestResolveCompositionDeferredInclude(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", "doctype html\nhtml\n  body\n    block content\n      p Default\n")
	mustWriteFile(t, dir, "partial.pug", "p Partial Content\n")
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\nblock content\n  include partial.pug\n")

	opts := &Options{Basedir: dir}
	src, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatalf("reading child template: %v", err)
	}
	ast, err := Parse(string(src), opts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	resolved, err := ResolveComposition(ast, opts)
	if err != nil {
		t.Fatalf("ResolveComposition: unexpected error for an extends+block+include template: %v", err)
	}

	_, err = GenerateGo(resolved, Config{
		PackageName: "main",
		FuncName:    "Render",
		DataType:    "map[string]any",
	})
	if err == nil {
		t.Fatalf("GenerateGo: expected an unsupported-node error for a leftover include, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo error %q does not describe an unsupported construct", err.Error())
	}
}

// TestResolveCompositionDeferredMixin proves that a layout declaring and
// calling a mixin inside a block also stays deferred: ResolveComposition
// resolves the extends+block chain and splices the mixin declaration/call
// into the flattened tree exactly as renderExtends would render them, and
// GenerateGo then returns a clean "unsupported" error rather than silently
// mis-generating (mixins are not part of this codegen increment).
func TestResolveCompositionDeferredMixin(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", strings.Join([]string{
		"mixin greet(name)",
		"  p Hello, #{name}",
		"doctype html",
		"html",
		"  body",
		"    block content",
		"      +greet('World')",
		"",
	}, "\n"))
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\n")

	opts := &Options{Basedir: dir}
	src, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatalf("reading child template: %v", err)
	}
	ast, err := Parse(string(src), opts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	resolved, err := ResolveComposition(ast, opts)
	if err != nil {
		t.Fatalf("ResolveComposition: unexpected error for an extends+mixin template: %v", err)
	}

	_, err = GenerateGo(resolved, Config{
		PackageName: "main",
		FuncName:    "Render",
		DataType:    "map[string]any",
	})
	if err == nil {
		t.Fatalf("GenerateGo: expected an unsupported-node error for a mixin declaration/call, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo error %q does not describe an unsupported construct", err.Error())
	}
}

// TestResolveCompositionNoOpOnPlainTemplate proves ResolveComposition is a
// no-op flatten on a template that uses neither extends nor block: passing
// its AST through ResolveComposition before GenerateGo produces byte-
// identical output to calling GenerateGo on the parsed AST directly.
func TestResolveCompositionNoOpOnPlainTemplate(t *testing.T) {
	src := "doctype html\nhtml\n  body\n    p Hello, #{Title}!\n"

	astDirect, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	directGen, err := GenerateGo(astDirect, Config{
		PackageName:     "gopug",
		FuncName:        "Render",
		DataType:        "compositionData",
		DataReflectType: compositionDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo (direct): %v", err)
	}

	astForComposition, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolved, err := ResolveComposition(astForComposition, nil)
	if err != nil {
		t.Fatalf("ResolveComposition: %v", err)
	}
	composedGen, err := GenerateGo(resolved, Config{
		PackageName:     "gopug",
		FuncName:        "Render",
		DataType:        "compositionData",
		DataReflectType: compositionDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo (composed): %v", err)
	}

	if !bytes.Equal(directGen, composedGen) {
		t.Errorf("ResolveComposition changed GenerateGo output for a template with no extends/block; expected a byte-identical no-op flatten.\ndirect:\n%s\ncomposed:\n%s", directGen, composedGen)
	}
}
