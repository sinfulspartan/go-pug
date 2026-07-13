package gopug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compareComposedFileOutput is compareComposedOutput's file-path-aware
// twin: it renders childPath through the interpreter (gopug.RenderFile) and
// separately through ResolveCompositionFile(childPath) → GenerateGo →
// build+run, asserting the two outputs are byte-identical. Unlike
// compareComposedOutput, which calls Parse+ResolveComposition against an
// AST with no real file path (so a relative extends can only resolve via
// Basedir, never via the child's own directory), this exercises the code
// path a caller that only has a file path — not an AST — actually uses.
func compareComposedFileOutput(t *testing.T, basedir, childPath string, interpData map[string]any, dataType, structSrc, dataLiteral string) string {
	t.Helper()

	want, err := RenderFile(childPath, interpData, &Options{Basedir: basedir})
	if err != nil {
		t.Fatalf("interpreter RenderFile: %v", err)
	}

	resolved, err := ResolveCompositionFile(childPath, &Options{Basedir: basedir})
	if err != nil {
		t.Fatalf("ResolveCompositionFile: %v", err)
	}

	generated, err := GenerateGo(resolved, Config{
		PackageName:     "main",
		FuncName:        "Render",
		DataType:        dataType,
		DataReflectType: nil,
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

// TestResolveCompositionFileSubdirSiblingLayout proves the previously-
// failing shape (a subdirectory child using a relative extends against its
// own sibling layout, unreachable through the string-based
// ResolveComposition since it has no real child file path to resolve
// against): a child at <dir>/layout/page.pug with a relative
// "extends base.pug" resolves
// against its own directory (layout/), not Basedir, when driven through
// ResolveCompositionFile — exactly as RenderFile resolves it for the
// interpreter. It also covers an overridden block and a dynamic block body
// (interpolation + if/else) evaluated against a declared struct.
func TestResolveCompositionFileSubdirSiblingLayout(t *testing.T) {
	dir := t.TempDir()
	layoutDir := filepath.Join(dir, "layout")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	mustWriteFile(t, layoutDir, "base.pug", strings.Join([]string{
		"doctype html",
		"html",
		"  body",
		"    block content",
		"      p Default",
		"",
	}, "\n"))
	childPath := mustWriteFile(t, layoutDir, "page.pug", strings.Join([]string{
		"extends base.pug",
		"block content",
		"  p Hello, #{Title}!",
		"  if Flag",
		"    p.on On",
		"  else",
		"    p.off Off",
		"",
	}, "\n"))

	resolved, err := ResolveCompositionFile(childPath, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("ResolveCompositionFile: unexpected error for a subdir sibling relative extends: %v", err)
	}

	generated, err := GenerateGo(resolved, Config{
		PackageName:     "main",
		FuncName:        "Render",
		DataType:        "compositionData",
		DataReflectType: compositionDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	want, err := RenderFile(childPath, map[string]any{"Title": "World", "Flag": true}, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("interpreter RenderFile: %v", err)
	}

	got := runComposedGo(t, generated, compositionDataStructSrc, `compositionData{Title: "World", Flag: true}`, "Render")
	if got != want {
		t.Errorf("codegen output does not match interpreter output.\ncodegen output:     %q\ninterpreter output: %q", got, want)
	}
	if !strings.Contains(want, "Hello, World!") {
		t.Errorf("interpreter output %q does not contain the interpolated title", want)
	}
	if !strings.Contains(want, `class="on"`) {
		t.Errorf("interpreter output %q does not contain the true branch of the block's if/else", want)
	}
}

// TestResolveCompositionFileParentDirLayout proves a subdir child that
// extends a layout in a different, parent-relative directory (
// <dir>/pages/x.pug with "extends ../layout/base.pug") resolves the same
// way through ResolveCompositionFile as it does through RenderFile.
func TestResolveCompositionFileParentDirLayout(t *testing.T) {
	dir := t.TempDir()
	layoutDir := filepath.Join(dir, "layout")
	pagesDir := filepath.Join(dir, "pages")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}
	mustWriteFile(t, layoutDir, "base.pug", "doctype html\nhtml\n  body\n    block content\n      p Default\n")
	childPath := mustWriteFile(t, pagesDir, "x.pug", "extends ../layout/base.pug\nblock content\n  p Parent-dir Override\n")

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	if !strings.Contains(want, "Parent-dir Override") {
		t.Errorf("interpreter output %q does not contain the overriding block's content", want)
	}
}

// TestResolveCompositionFileParity confirms the same child rendered via
// RenderFile and via ResolveCompositionFile → GenerateGo → run produce
// byte-identical output — exercisable now because ResolveCompositionFile
// gives GenerateGo a real file path to resolve the relative extends
// against.
func TestResolveCompositionFileParity(t *testing.T) {
	dir := t.TempDir()
	layoutDir := filepath.Join(dir, "layout")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	mustWriteFile(t, layoutDir, "base.pug", "doctype html\nhtml\n  body\n    block content\n      p Default\n    block sidebar\n      p Default Sidebar\n")
	childPath := mustWriteFile(t, layoutDir, "page.pug", "extends base.pug\nblock content\n  p Child Content\n")

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	if !strings.Contains(want, "Child Content") {
		t.Errorf("interpreter output %q does not contain the overriding block's content", want)
	}
	if !strings.Contains(want, "Default Sidebar") {
		t.Errorf("interpreter output %q does not contain the unoverridden block's default content", want)
	}
}

// TestResolveCompositionFileSlashFormRegression proves the slash-form
// extends ("extends /layout/base.pug", basedir-relative) that already
// worked through the string-based ResolveComposition still works
// identically through ResolveCompositionFile.
func TestResolveCompositionFileSlashFormRegression(t *testing.T) {
	dir := t.TempDir()
	layoutDir := filepath.Join(dir, "layout")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		t.Fatalf("mkdir layout: %v", err)
	}
	mustWriteFile(t, layoutDir, "base.pug", "doctype html\nhtml\n  body\n    block content\n      p Default\n")
	childPath := mustWriteFile(t, dir, "child.pug", "extends /layout/base.pug\nblock content\n  p Slash Form Override\n")

	src, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatalf("reading child template: %v", err)
	}
	opts := &Options{Basedir: dir}
	ast, err := Parse(string(src), opts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	resolvedViaAST, err := ResolveComposition(ast, opts)
	if err != nil {
		t.Fatalf("ResolveComposition (slash-form via AST entry point): %v", err)
	}
	genViaAST, err := GenerateGo(resolvedViaAST, Config{
		PackageName: "main",
		FuncName:    "Render",
		DataType:    "map[string]any",
	})
	if err != nil {
		t.Fatalf("GenerateGo (AST entry point): %v", err)
	}

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	gotViaAST := runComposedGo(t, genViaAST, "", "map[string]any{}", "Render")
	if gotViaAST != want {
		t.Errorf("AST entry point output %q does not match interpreter output %q for a slash-form extends", gotViaAST, want)
	}
	if !strings.Contains(want, "Slash Form Override") {
		t.Errorf("interpreter output %q does not contain the overriding block's content", want)
	}
}

// TestResolveCompositionFileNoOpOnPlainTemplate proves ResolveCompositionFile
// is a no-op flatten for a template that uses neither extends nor block,
// matching ResolveComposition's own no-op guarantee.
func TestResolveCompositionFileNoOpOnPlainTemplate(t *testing.T) {
	dir := t.TempDir()
	path := mustWriteFile(t, dir, "plain.pug", "doctype html\nhtml\n  body\n    p Hello, #{Title}!\n")

	astDirect, err := Parse("doctype html\nhtml\n  body\n    p Hello, #{Title}!\n", nil)
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

	resolved, err := ResolveCompositionFile(path, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("ResolveCompositionFile: %v", err)
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

	if string(directGen) != string(composedGen) {
		t.Errorf("ResolveCompositionFile changed GenerateGo output for a template with no extends/block; expected a byte-identical no-op flatten.\ndirect:\n%s\ncomposed:\n%s", directGen, composedGen)
	}
}
