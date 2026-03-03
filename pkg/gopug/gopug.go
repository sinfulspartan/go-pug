package gopug

import (
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
)

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
	src, err := ioutil.ReadFile(path)
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
func CompileFile(path string, opts *Options) (*Template, error) {
	src, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
	}

	if opts == nil {
		opts = &Options{}
	}
	if opts.Basedir == "" {
		opts.Basedir = filepath.Dir(path)
	}

	return Compile(string(src), opts)
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
	rt := NewRuntime(t.ast, data)
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
