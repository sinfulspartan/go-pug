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

	if opts == nil {
		opts = &Options{}
	}
	if opts.Basedir == "" {
		opts.Basedir = filepath.Dir(path)
	}

	return Render(string(src), data, opts)
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
			merged.opts = opts
			return &merged, nil
		}
		return tpl, nil
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", abs, err)
	}

	if opts == nil {
		opts = &Options{}
	}
	if opts.Basedir == "" {
		opts.Basedir = filepath.Dir(abs)
	}

	tpl, err := Compile(string(src), opts)
	if err != nil {
		return nil, err
	}

	// Store in cache (store the template compiled with default opts; callers
	// with custom opts get a shallow copy above on subsequent calls).
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
