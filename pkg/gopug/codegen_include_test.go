package gopug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveCompositionIncludeBasic proves the basic case: a page that
// includes a partial with static text, a `#{}` interpolation, an if/else
// condition, and a dynamic (non-boolean) attribute value — all inlined into
// the page's own data scope, matching RenderFile byte for byte against a
// declared struct.
func TestResolveCompositionIncludeBasic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWriteFile(t, dir, "partial.pug", strings.Join([]string{
		"p Hello, #{Title}!",
		"if Flag",
		"  p.on On",
		"else",
		"  p.off Off",
		"div(data-flag=Flag) Attr",
		"",
	}, "\n"))
	childPath := mustWriteFile(t, dir, "page.pug", "doctype html\nhtml\n  body\n    include partial.pug\n")

	want := compareComposedOutput(
		t, dir, childPath,
		map[string]any{"Title": "World", "Flag": true},
		"compositionData", compositionDataStructSrc, `compositionData{Title: "World", Flag: true}`,
		compositionDataReflectType,
	)

	if !strings.Contains(want, "Hello, World!") {
		t.Errorf("interpreter output %q does not contain the included partial's interpolation", want)
	}
	if !strings.Contains(want, `class="on"`) {
		t.Errorf("interpreter output %q does not contain the included partial's true branch", want)
	}
}

// TestResolveCompositionIncludeNested proves nested includes: partial A
// (in a subdirectory) includes partial B by a bare relative path, which must
// resolve against A's own directory, not the top-level page's directory —
// exactly as renderInclude resolves it via the innermost includeStack entry.
func TestResolveCompositionIncludeNested(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "partials")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	mustWriteFile(t, subDir, "b.pug", "p Partial B\n")
	mustWriteFile(t, subDir, "a.pug", "p Partial A\ninclude b.pug\n")
	childPath := mustWriteFile(t, dir, "page.pug", "doctype html\nhtml\n  body\n    include partials/a.pug\n")

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	if !strings.Contains(want, "Partial A") || !strings.Contains(want, "Partial B") {
		t.Errorf("interpreter output %q is missing content from the nested include chain", want)
	}
}

// TestResolveCompositionIncludeInTagAndConditional proves the deep walk
// reaches an include nested inside a *TagNode and inside a *ConditionalNode,
// and that the conditional branch is only rendered when taken — matching
// the interpreter for both a true and a false condition.
func TestResolveCompositionIncludeInTagAndConditional(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "tagged.pug", "p Tagged Partial\n")
	mustWriteFile(t, dir, "conditional.pug", "p Conditional Partial\n")
	childPath := mustWriteFile(t, dir, "page.pug", strings.Join([]string{
		"doctype html",
		"html",
		"  body",
		"    div.wrapper",
		"      include tagged.pug",
		"    if Flag",
		"      include conditional.pug",
		"",
	}, "\n"))

	wantTrue := compareComposedFileOutput(
		t, dir, childPath,
		map[string]any{"Flag": true},
		"compositionData", compositionDataStructSrc, "compositionData{Flag: true}",
	)
	if !strings.Contains(wantTrue, "Tagged Partial") {
		t.Errorf("interpreter output %q does not contain the tag-nested include", wantTrue)
	}
	if !strings.Contains(wantTrue, "Conditional Partial") {
		t.Errorf("interpreter output %q does not contain the conditional-nested include on the taken branch", wantTrue)
	}

	wantFalse := compareComposedFileOutput(
		t, dir, childPath,
		map[string]any{"Flag": false},
		"compositionData", compositionDataStructSrc, "compositionData{Flag: false}",
	)
	if !strings.Contains(wantFalse, "Tagged Partial") {
		t.Errorf("interpreter output %q does not contain the tag-nested include", wantFalse)
	}
	if strings.Contains(wantFalse, "Conditional Partial") {
		t.Errorf("interpreter output %q contains the conditional-nested include on the untaken branch", wantFalse)
	}
}

// TestResolveCompositionExtendsIncludeInBlock proves the extends+include
// interaction from the child side: a child that both extends a layout and
// includes a partial from inside its own block override resolves through
// extends first (merging the override into the layout), then inlines the
// include from within the merged block body — matching RenderFile.
func TestResolveCompositionExtendsIncludeInBlock(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "layout.pug", "doctype html\nhtml\n  body\n    block content\n      p Default\n")
	mustWriteFile(t, dir, "partial.pug", "p Partial Content\n")
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\nblock content\n  include partial.pug\n")

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	if !strings.Contains(want, "Partial Content") {
		t.Errorf("interpreter output %q does not contain the included partial's content", want)
	}
	if strings.Contains(want, "Default") {
		t.Errorf("interpreter output %q still contains the replaced block's default content", want)
	}
}

// TestResolveCompositionIncludeInLayoutNotOverridden proves an include
// living directly in the layout (not touched by any child block override)
// is inlined after the extends merge, exactly as renderInclude renders it
// when reached through the merged tree.
func TestResolveCompositionIncludeInLayoutNotOverridden(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "footer.pug", "p Layout Footer\n")
	mustWriteFile(t, dir, "layout.pug", strings.Join([]string{
		"doctype html",
		"html",
		"  body",
		"    block content",
		"      p Default",
		"    include footer.pug",
		"",
	}, "\n"))
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\nblock content\n  p Child Content\n")

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	if !strings.Contains(want, "Child Content") {
		t.Errorf("interpreter output %q does not contain the overriding block's content", want)
	}
	if !strings.Contains(want, "Layout Footer") {
		t.Errorf("interpreter output %q does not contain the layout's own (non-overridden) include", want)
	}
}

// TestResolveCompositionOverriddenBlockDoesNotReachIncludedBlock proves the
// ordering guarantee at the heart of this increment: a `block` that lives
// inside an included partial is invisible to the child's own block override
// of the same name, because applyBlockOverrides runs (as part of extends
// resolution) BEFORE the include is inlined — the included partial's block
// is not yet part of the tree resolveExtendsAST walks. The included block
// must render its own default body, never the child's override.
func TestResolveCompositionOverriddenBlockDoesNotReachIncludedBlock(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "innerpartial.pug", "block greeting\n  p Partial Default Greeting\n")
	mustWriteFile(t, dir, "layout.pug", "doctype html\nhtml\n  body\n    include innerpartial.pug\n")
	childPath := mustWriteFile(t, dir, "child.pug", "extends layout\nblock greeting\n  p Child Override Greeting\n")

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	if !strings.Contains(want, "Partial Default Greeting") {
		t.Errorf("interpreter output %q does not contain the included block's own default body", want)
	}
	if strings.Contains(want, "Child Override Greeting") {
		t.Errorf("interpreter output %q contains the child's override, but a block inside an included partial must not be reachable by it", want)
	}
}

// TestResolveCompositionIncludeRelativeAndSlashForm proves both include path
// forms resolve the same way through codegen as through the interpreter: a
// bare relative path (resolved against the including file's own directory)
// and a leading-slash path (resolved against Basedir).
func TestResolveCompositionIncludeRelativeAndSlashForm(t *testing.T) {
	dir := t.TempDir()
	pagesDir := filepath.Join(dir, "pages")
	partialsDir := filepath.Join(dir, "partials")
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		t.Fatalf("mkdir pages: %v", err)
	}
	if err := os.MkdirAll(partialsDir, 0o755); err != nil {
		t.Fatalf("mkdir partials: %v", err)
	}
	mustWriteFile(t, pagesDir, "sibling.pug", "p Sibling Relative\n")
	mustWriteFile(t, partialsDir, "x.pug", "p Slash Form\n")
	childPath := mustWriteFile(t, pagesDir, "page.pug", strings.Join([]string{
		"doctype html",
		"html",
		"  body",
		"    include sibling.pug",
		"    include /partials/x.pug",
		"",
	}, "\n"))

	want := compareComposedFileOutput(t, dir, childPath, map[string]any{}, "map[string]any", "", "map[string]any{}")

	if !strings.Contains(want, "Sibling Relative") {
		t.Errorf("interpreter output %q does not contain the relative-path include's content", want)
	}
	if !strings.Contains(want, "Slash Form") {
		t.Errorf("interpreter output %q does not contain the leading-slash (Basedir-relative) include's content", want)
	}
}

// TestResolveCompositionIncludeCycle proves an include cycle (a.pug includes
// b.pug includes a.pug) is rejected by both the interpreter and codegen —
// resolveIncludeAbs is the exact helper renderInclude itself calls, so the
// cycle is caught on the same hop for both.
func TestResolveCompositionIncludeCycle(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "a.pug", "p A\ninclude b.pug\n")
	mustWriteFile(t, dir, "b.pug", "p B\ninclude a.pug\n")

	_, interpErr := RenderFile(filepath.Join(dir, "a.pug"), map[string]any{}, &Options{Basedir: dir})
	if interpErr == nil {
		t.Fatalf("interpreter RenderFile: expected an include-cycle error, got nil")
	}
	if !strings.Contains(interpErr.Error(), "cycle") {
		t.Errorf("interpreter error %q does not describe a cycle", interpErr.Error())
	}

	_, codegenErr := ResolveCompositionFile(filepath.Join(dir, "a.pug"), &Options{Basedir: dir})
	if codegenErr == nil {
		t.Fatalf("ResolveCompositionFile: expected an include-cycle error, got nil")
	}
	if !strings.Contains(codegenErr.Error(), "cycle") {
		t.Errorf("ResolveCompositionFile error %q does not describe a cycle", codegenErr.Error())
	}
}

// TestResolveCompositionIncludeDeferredFiltered proves a filtered include
// (include:filtername path) is not silently dropped or mis-generated —
// ResolveComposition returns a clear error naming it as unsupported.
func TestResolveCompositionIncludeDeferredFiltered(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "notes.md", "# Notes\n")
	childPath := mustWriteFile(t, dir, "page.pug", "doctype html\nhtml\n  body\n    include:markdown notes.md\n")

	_, err := ResolveCompositionFile(childPath, &Options{Basedir: dir})
	if err == nil {
		t.Fatalf("ResolveCompositionFile: expected an error for a filtered include, got nil")
	}
	if !strings.Contains(err.Error(), "filtered") {
		t.Errorf("ResolveCompositionFile error %q does not describe an unsupported filtered include", err.Error())
	}
}

// TestResolveCompositionIncludeDeferredRaw proves a raw (non-`.pug`) include
// is not silently dropped or mis-generated — ResolveComposition returns a
// clear error naming it as unsupported.
func TestResolveCompositionIncludeDeferredRaw(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "styles.css", "body { color: red; }\n")
	childPath := mustWriteFile(t, dir, "page.pug", "doctype html\nhtml\n  body\n    include styles.css\n")

	_, err := ResolveCompositionFile(childPath, &Options{Basedir: dir})
	if err == nil {
		t.Fatalf("ResolveCompositionFile: expected an error for a raw include, got nil")
	}
	if !strings.Contains(err.Error(), "raw") {
		t.Errorf("ResolveCompositionFile error %q does not describe an unsupported raw include", err.Error())
	}
}

// TestResolveCompositionIncludeDeferredMixin proves a `.pug` include whose
// partial declares/calls a mixin is inlined successfully by ResolveComposition
// (the mixin nodes are spliced into the tree untouched, matching what
// renderInclude itself would render for the include statement), and
// GenerateGo then returns a clean "unsupported" error rather than silently
// mis-generating, since codegen does not support mixins yet.
func TestResolveCompositionIncludeDeferredMixin(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "partial.pug", strings.Join([]string{
		"mixin greet(name)",
		"  p Hello, #{name}",
		"+greet('World')",
		"",
	}, "\n"))
	childPath := mustWriteFile(t, dir, "page.pug", "doctype html\nhtml\n  body\n    include partial.pug\n")

	resolved, err := ResolveCompositionFile(childPath, &Options{Basedir: dir})
	if err != nil {
		t.Fatalf("ResolveCompositionFile: unexpected error for an include with a mixin: %v", err)
	}

	_, err = GenerateGo(resolved, Config{
		PackageName: "main",
		FuncName:    "Render",
		DataType:    "map[string]any",
	})
	if err == nil {
		t.Fatalf("GenerateGo: expected an unsupported-node error for a mixin reached via include, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo error %q does not describe an unsupported construct", err.Error())
	}
}
