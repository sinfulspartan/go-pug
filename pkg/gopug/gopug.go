package gopug

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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

func ClearCache() {
	compiledCache.Range(func(k, _ any) bool {
		compiledCache.Delete(k)
		return true
	})
}

type Template struct {
	ast  *DocumentNode
	opts *Options
}

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

func Compile(src string, opts *Options) (*Template, error) {
	if opts == nil {
		opts = &Options{}
	}

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

	// Compile buffered/unescaped expressions and mixin-call arguments into
	// closures once, here, so renderCode/renderMixinCall never re-parse
	// their strings on every render. These are one-time passes over the AST
	// at compile time, not per render.
	compileExprs(ast.Children)
	compileMixinArgs(ast.Children)

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
		// If caller supplied opts, return a shallow copy that merges them in
		// so the cached AST is re-used but per-call options are honoured.
		if opts != nil {
			merged := *tpl
			// Preserve the entry-file path for top-level relative resolution;
			// the caller's Options never carries it.
			mergedOpts := *opts
			mergedOpts.entryFile = abs
			merged.opts = &mergedOpts
			return &merged, nil
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

	// Store with default opts so the cache-hit branch can return a safe shallow copy for callers with custom opts.
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

	rt := NewRuntimeWithOptions(t.ast, data, t.opts)
	return rt.Render()
}

func (t *Template) RenderToWriter(w io.Writer, data map[string]any) error {
	html, err := t.Render(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, html)
	return err
}
