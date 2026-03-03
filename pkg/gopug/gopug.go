package gopug

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// compiledCache caches the result of CompileFile keyed by absolute file path.
// The cached entries are invalidated by ClearCache().
var compiledCache sync.Map // map[string]*Template

// ClearCache removes all cached compiled templates, forcing the next
// CompileFile call for each path to re-read and re-parse the file.
func ClearCache() {
	compiledCache.Range(func(k, _ interface{}) bool {
		compiledCache.Delete(k)
		return true
	})
}

// Template represents a compiled Pug template ready for rendering.
type Template struct {
	ast  *DocumentNode
	opts *Options
}

// Options configures template compilation and rendering behavior.
type Options struct {
	Basedir string                                  // root directory for absolute paths
	Pretty  bool                                    // pretty-print HTML output
	Globals map[string]interface{}                  // data available to all renders
	Filters map[string]func(string) (string, error) // custom filters
}

// Render compiles a Pug template string and renders it with the given data.
// This is a convenience function that compiles and renders in one step.
func Render(src string, data map[string]interface{}, opts *Options) (string, error) {
	tpl, err := Compile(src, opts)
	if err != nil {
		return "", err
	}
	return tpl.Render(data)
}

// RenderFile reads a .pug file, compiles it, and renders it with the given data.
func RenderFile(path string, data map[string]interface{}, opts *Options) (string, error) {
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

// Compile parses a Pug template string and returns a compiled Template.
// The template can be rendered multiple times with different data.
func Compile(src string, opts *Options) (*Template, error) {
	if opts == nil {
		opts = &Options{}
	}

	// Lex
	lexer := NewLexer(src)
	tokens, err := lexer.Lex()
	if err != nil {
		return nil, fmt.Errorf("lexer error: %w", err)
	}

	// Parse
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

	// Check the cache first.
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

// Render renders the template with the provided data and returns HTML.
func (t *Template) Render(data map[string]interface{}) (string, error) {
	if data == nil {
		data = make(map[string]interface{})
	}

	// Merge globals into data
	if t.opts != nil && t.opts.Globals != nil {
		for k, v := range t.opts.Globals {
			if _, exists := data[k]; !exists {
				data[k] = v
			}
		}
	}

	// Create runtime and render
	rt := NewRuntimeWithOptions(t.ast, data, t.opts)
	return rt.Render()
}

// RenderToWriter renders the template with the provided data and writes to w.
func (t *Template) RenderToWriter(w io.Writer, data map[string]interface{}) error {
	html, err := t.Render(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, html)
	return err
}
