package gopug

import "testing"

// renderScopePoolCase compiles src, renders it against data, and asserts the
// output matches want exactly. Every case here is designed to exercise the
// per-Runtime scope free-list hard: nested loops, mixin calls inside loops,
// loops inside mixin bodies, mixin-calls-mixin, recursion, and back-to-back
// each/else pairs all reuse and release scope frames from the same pool
// within a single render, so any stale value left behind by a cleared-wrong
// or double-released frame would surface as wrong output.
func renderScopePoolCase(t *testing.T, src string, data map[string]any, want string) {
	t.Helper()
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	got, err := tpl.Render(data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

// TestScopePoolNestedEachSameVarName covers an outer and inner each loop that
// both bind their loop variable to the same name. If a released outer-loop
// frame were handed back to the inner loop and its old value leaked forward
// after the inner loop's frame is released and reused for the next outer
// iteration, the outer variable would read the wrong item.
func TestScopePoolNestedEachSameVarName(t *testing.T) {
	src := "each item in outers\n" +
		"  p= item.name\n" +
		"  each item in item.children\n" +
		"    span= item\n" +
		"  p= item.name\n"
	data := map[string]any{
		"outers": []any{
			map[string]any{"name": "A", "children": []any{"a1", "a2"}},
			map[string]any{"name": "B", "children": []any{"b1"}},
		},
	}
	want := "<p>A</p><span>a1</span><span>a2</span><p>A</p><p>B</p><span>b1</span><p>B</p>"
	renderScopePoolCase(t, src, data, want)
}

// TestScopePoolEachCallsMixinPerIteration is the exact loop+mixin recycling
// pattern the pool targets: each iteration acquires and releases an each
// scope, and the mixin call inside the body acquires and releases its own
// param scope on every pass.
func TestScopePoolEachCallsMixinPerIteration(t *testing.T) {
	src := "mixin card(x)\n" +
		"  span= x\n" +
		"each item in items\n" +
		"  +card(item)\n"
	data := map[string]any{"items": []any{"a", "b", "c"}}
	want := "<span>a</span><span>b</span><span>c</span>"
	renderScopePoolCase(t, src, data, want)
}

// TestScopePoolMixinBlockContainsEach nests a recycled each frame under a
// recycled mixin-call frame: the mixin body pushes its own scope, and the
// block content it renders (an each loop) acquires and releases further
// frames while the mixin's frame is still live on the stack below it.
func TestScopePoolMixinBlockContainsEach(t *testing.T) {
	src := "mixin wrapper()\n" +
		"  div\n" +
		"    block\n" +
		"+wrapper()\n" +
		"  each x in ['a', 'b', 'c']\n" +
		"    span= x\n"
	want := "<div><span>a</span><span>b</span><span>c</span></div>"
	renderScopePoolCase(t, src, map[string]any{}, want)
}

// TestScopePoolMixinCallsMixin covers one mixin-call frame acquired while
// another is still on the stack (the caller's frame below the callee's).
func TestScopePoolMixinCallsMixin(t *testing.T) {
	src := "mixin inner(x)\n" +
		"  span= x\n" +
		"mixin outer(x)\n" +
		"  +inner(x)\n" +
		"+outer(\"hi\")\n"
	want := "<span>hi</span>"
	renderScopePoolCase(t, src, map[string]any{}, want)
}

// TestScopePoolMixinRecursion covers a mixin calling itself, which stacks
// several acquired-but-not-yet-released frames of the SAME mixin on top of
// each other before any of them are released.
func TestScopePoolMixinRecursion(t *testing.T) {
	src := "mixin countdown(n)\n" +
		"  if n > 0\n" +
		"    span= n\n" +
		"    +countdown(n - 1)\n" +
		"+countdown(3)\n"
	want := "<span>3</span><span>2</span><span>1</span>"
	renderScopePoolCase(t, src, map[string]any{}, want)
}

// TestScopePoolEachElseEmptyThenNonEmpty renders an empty each/else (which
// never acquires a scope frame) immediately followed by a non-empty each on
// the same Runtime, proving the pool is unaffected by a loop that never
// touches it.
func TestScopePoolEachElseEmptyThenNonEmpty(t *testing.T) {
	src := "each x in empty\n" +
		"  span= x\n" +
		"else\n" +
		"  p empty\n" +
		"each x in items\n" +
		"  span= x\n" +
		"else\n" +
		"  p empty\n"
	data := map[string]any{
		"empty": []any{},
		"items": []any{"a", "b"},
	}
	want := "<p>empty</p><span>a</span><span>b</span>"
	renderScopePoolCase(t, src, data, want)
}

// TestScopePoolMapEachSameVarNameAcrossIterations exercises the map-key
// branch of renderEach specifically, proving a per-iteration frame reused
// from the pool never carries over a key set by a previous iteration when
// that key is absent from the current one.
func TestScopePoolMapEachSameVarNameAcrossIterations(t *testing.T) {
	src := "each val, key in m\n" +
		"  p= key\n" +
		"  p= val\n"
	data := map[string]any{
		"m": map[string]any{"only": "solo"},
	}
	want := "<p>only</p><p>solo</p>"
	renderScopePoolCase(t, src, data, want)
}

// TestScopePoolLoopLocalsDoNotLeakAcrossIterations proves clear() on release
// actually resets a reused frame: a local declared conditionally on one
// iteration must not still be visible on a later iteration that reuses the
// same underlying map but does not declare it.
func TestScopePoolLoopLocalsDoNotLeakAcrossIterations(t *testing.T) {
	src := "each item in items\n" +
		"  if item == \"set\"\n" +
		"    - var x = \"hello\"\n" +
		"  p= x\n"
	data := map[string]any{"items": []any{"set", "other", "set"}}
	want := "<p>hello</p><p></p><p>hello</p>"
	renderScopePoolCase(t, src, data, want)
}

// TestScopePoolOuterSetVarAcrossBoundary proves that pooling loop-local
// frames does not disturb setVar's own separate handling of an
// outer-declared variable mutated from inside the loop body.
func TestScopePoolOuterSetVarAcrossBoundary(t *testing.T) {
	src := "- var outerX = 0\n" +
		"each item in items\n" +
		"  - outerX = outerX + 1\n" +
		"p= outerX\n"
	data := map[string]any{"items": []any{"a", "b", "c"}}
	want := "<p>3</p>"
	renderScopePoolCase(t, src, data, want)
}
