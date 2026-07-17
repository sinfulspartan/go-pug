package gopug

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// FilterFunc is the signature for a filter function that also receives the
// key=value options parsed from the filter header, e.g.:
//
//	:markdown(flavor="gfm" pretty=true)
//
// The options map is never nil (it is an empty map when no options were given).
// Use SimpleFilter to wrap a plain func(string)(string,error) so it satisfies
// this interface automatically.
type FilterFunc func(text string, options map[string]string) (string, error)

// SimpleFilter adapts a plain func(string)(string,error) into a FilterFunc so
// that existing filter implementations continue to work with the new API.
//
//	opts.Filters["upper"] = gopug.SimpleFilter(func(s string, _ map[string]string) (string, error) {
//	    return strings.ToUpper(s), nil
//	})
//
// Or more concisely, wrapping an old-style func:
//
//	opts.Filters["upper"] = gopug.SimpleFilter(myOldFilter)
func SimpleFilter(fn func(string) (string, error)) FilterFunc {
	return func(text string, _ map[string]string) (string, error) {
		return fn(text)
	}
}

var compiledCache sync.Map

// ClearCache drops every entry from the compiled-template cache and the
// render-time composition caches (parsed include partials, resolved extends
// chains), so a hot-reloaded file on disk is picked up on the next
// Compile/Render instead of returning a stale cached AST.
func ClearCache() {
	compiledCache.Range(func(k, _ any) bool {
		compiledCache.Delete(k)
		return true
	})
	parsedIncludeCache.Range(func(k, _ any) bool {
		parsedIncludeCache.Delete(k)
		return true
	})
	extendsCache.Range(func(k, _ any) bool {
		extendsCache.Delete(k)
		return true
	})
}

type Template struct {
	ast  *DocumentNode
	opts *Options

	// renderSizeHint records the byte length of the most recent successful
	// render of this Template, used to pre-size the next render's output
	// buffer (see Render). A *Template is shared and rendered concurrently
	// (that's the whole point of compiledCache), so this MUST be accessed
	// atomically. Zero means "no hint yet" — the first render falls back to
	// defaultRenderBufCap. An approximate or momentarily stale value is
	// harmless: it only ever affects reserved buffer capacity, never a single
	// emitted byte.
	renderSizeHint atomic.Uint64
}

// maxRenderBufCap bounds the buffer pre-size derived from renderSizeHint so
// a pathological hint (or a hint corrupted by some future bug) can never
// force an absurdly large upfront allocation. Real templates never approach
// this; it exists purely as a defensive ceiling.
const maxRenderBufCap = 1 << 26 // 64 MiB

// renderBufPool recycles output buffers across Template.Render calls. A
// bytes.Buffer holds no semantic state of its own — just an underlying byte
// slice and a length — and Reset only sets the length back to zero without
// touching the bytes beyond it. Since nothing ever reads past a buffer's
// current length, a Reset buffer handed to a later, unrelated render can
// never surface a previous render's bytes: there is no read path that could
// observe them. This lets us reuse the buffer's backing array across renders
// instead of allocating a fresh one every time.
var renderBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// maxPooledBufCap bounds what Template.Render returns to renderBufPool, so
// one unusually large render doesn't leave a big backing array pinned in the
// pool indefinitely for every subsequent, typically much smaller render.
const maxPooledBufCap = 1 << 20 // 1 MiB

type Options struct {
	Basedir string
	Pretty  bool
	Globals map[string]any
	Filters map[string]FilterFunc

	// entryFile is the path of the top-level template being rendered, if it
	// came from a file (RenderFile/CompileFile). It lets top-level relative
	// includes/extends resolve against the entry file's own directory —
	// matching standard Pug semantics — instead of falling back to Basedir.
	// Unexported: set internally, never by callers.
	entryFile string
}

func Render(src string, data map[string]any, opts *Options) (string, error) {
	tpl, err := Compile(src, opts)
	if err != nil {
		return "", err
	}
	return tpl.Render(data)
}

func RenderFile(path string, data map[string]any, opts *Options) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %q: %w", path, err)
	}

	// Copy so we never mutate the caller's Options struct.
	copied := Options{}
	if opts != nil {
		copied = *opts
	}
	if copied.Basedir == "" {
		copied.Basedir = filepath.Dir(path)
	}
	copied.entryFile = path

	return Render(string(src), data, &copied)
}

// Parse lexes and parses src into a *DocumentNode without any of the
// render-time preparation Compile does on top (closure-compiling expressions
// and mixin arguments). It exists so callers other than the interpreter —
// most notably the typed-codegen backend — can obtain the same AST the
// runtime walks without going through Compile/Template at all. opts is
// accepted for symmetry with Compile and future use; the lex/parse stages
// do not currently consult it.
func Parse(src string, opts *Options) (*DocumentNode, error) {
	lexer := NewLexer(src)
	tokens, err := lexer.Lex()
	if err != nil {
		return nil, fmt.Errorf("lexer error: %w", err)
	}

	parser := NewParser(tokens)
	ast, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("parser error: %w", err)
	}

	return ast, nil
}

func Compile(src string, opts *Options) (*Template, error) {
	if opts == nil {
		opts = &Options{}
	}

	ast, err := Parse(src, opts)
	if err != nil {
		return nil, err
	}

	// Compile buffered/unescaped expressions into closures once, here, so
	// renderCode never re-parses their strings on every render. Mixin-call
	// arguments are deliberately NOT compiled into string closures here —
	// renderMixinCall evaluates them through the type-preserving
	// evaluateExprRaw path instead, so a slice/map/struct argument reaches
	// the mixin body with its real Go type intact rather than being
	// stringified up front. These are one-time passes over the AST at
	// compile time, not per render.
	compileExprs(ast.Children)
	compileTagAttrs(ast.Children)

	return &Template{
		ast:  ast,
		opts: opts,
	}, nil
}

// CompileFile reads a .pug file and compiles it into a Template.
// Results are cached by absolute file path; call ClearCache() to invalidate.
// The cache key includes only the path — if you need per-call option
// variations, use Compile() directly.
func CompileFile(path string, opts *Options) (*Template, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path %q: %w", path, err)
	}

	if cached, ok := compiledCache.Load(abs); ok {
		tpl := cached.(*Template)
		// If caller supplied opts, return a variant that reuses the cached
		// AST but honours the per-call options. Built field-by-field (not a
		// whole-struct copy) because Template embeds an atomic.Uint64, which
		// must never be copied. The new Template starts with its own fresh
		// renderSizeHint (zero) rather than inheriting the cached one, since
		// different options can legitimately produce different-sized output.
		if opts != nil {
			// Preserve the entry-file path for top-level relative resolution;
			// the caller's Options never carries it.
			mergedOpts := *opts
			mergedOpts.entryFile = abs
			return &Template{ast: tpl.ast, opts: &mergedOpts}, nil
		}
		return tpl, nil
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", abs, err)
	}

	// Copy the Options so we don't mutate the caller's struct.
	copied := Options{}
	if opts != nil {
		copied = *opts
	}
	if copied.Basedir == "" {
		copied.Basedir = filepath.Dir(abs)
	}
	copied.entryFile = abs
	opts = &copied

	tpl, err := Compile(string(src), opts)
	if err != nil {
		return nil, err
	}

	// Store with default opts so the cache-hit branch can return a Template
	// with custom opts for callers that need them.
	compiledCache.Store(abs, tpl)
	return tpl, nil
}

func (t *Template) Render(data map[string]any) (string, error) {
	if data == nil {
		data = make(map[string]any)
	}

	if t.opts != nil && t.opts.Globals != nil {
		for k, v := range t.opts.Globals {
			if _, exists := data[k]; !exists {
				data[k] = v
			}
		}
	}

	// Pre-size the output buffer from the previous successful render's
	// length: successive renders of the same compiled *Template with
	// different data tend to produce similarly-sized output, so this
	// collapses bytes.Buffer's growth-doubling reallocations to near zero
	// after the first render. It never changes what gets written — Grow only
	// reserves capacity — so output is byte-identical regardless of the hint.
	presize := defaultRenderBufCap
	if h := t.renderSizeHint.Load(); h > 0 {
		hp := h + h/8
		if hp > maxRenderBufCap || hp < h { // hp < h catches uint64 overflow
			hp = maxRenderBufCap
		}
		presize = int(hp)
	}

	buf := renderBufPool.Get().(*bytes.Buffer)
	defer func() {
		// Reset before the cap check: len must go back to zero regardless of
		// whether the backing array is small enough to keep. html was already
		// produced by rt.Render() via htmlBuf.String() — an independent copy —
		// so clearing/returning buf here can never affect the returned string.
		buf.Reset()
		if buf.Cap() <= maxPooledBufCap {
			renderBufPool.Put(buf)
		}
	}()

	rt := newRuntimeWithBufCap(t.ast, data, t.opts, buf, presize)
	html, err := rt.Render()
	if err == nil {
		t.renderSizeHint.Store(uint64(len(html)))
	}
	return html, err
}

func (t *Template) RenderToWriter(w io.Writer, data map[string]any) error {
	html, err := t.Render(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, html)
	return err
}
