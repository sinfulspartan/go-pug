package main

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

//go:embed views/*.pug
var viewsFS embed.FS

// ---------------------------------------------------------------------------
// Example metadata
// ---------------------------------------------------------------------------

// example holds one demo section: the file name, display title, Pug source,
// optional render data, and optional Options (filters, globals, pretty).
type example struct {
	filename string
	title    string
	pug      string
	data     map[string]interface{}
	opts     *gopug.Options
}

// meta carries per-file title, data, and opts. Anything not listed here gets
// sensible defaults (no data, no opts, title derived from the filename).
type meta struct {
	title string
	data  map[string]interface{}
	opts  *gopug.Options
}

// filterOpts is the shared Options used by all filter examples.
var filterOpts = &gopug.Options{
	Filters: map[string]gopug.FilterFunc{
		"uppercase": func(s string, _ map[string]string) (string, error) {
			return strings.ToUpper(strings.TrimSpace(s)), nil
		},
		"shout": func(s string, opts map[string]string) (string, error) {
			suffix := opts["suffix"]
			if suffix == "" {
				suffix = "!"
			}
			lines := strings.Split(strings.TrimSpace(s), "\n")
			for i, l := range lines {
				lines[i] = strings.ToUpper(strings.TrimSpace(l)) + suffix
			}
			return strings.Join(lines, "\n"), nil
		},
		"wrap": func(s string, opts map[string]string) (string, error) {
			open := opts["open"]
			if open == "" {
				open = "["
			}
			close := opts["close"]
			if close == "" {
				close = "]"
			}
			return open + strings.TrimSpace(s) + close, nil
		},
	},
}

// registry maps filename (without path, with extension) to its metadata.
var registry = map[string]meta{
	"01-doctype.pug": {title: "Doctype"},
	"02-tags.pug":    {title: "Tags & nesting"},
	"03-class-id.pug": {title: "Class & ID shorthand"},
	"04-attributes.pug": {title: "Attributes"},
	"05-dynamic-attrs.pug": {
		title: "Dynamic attributes",
		data: map[string]interface{}{
			"url":      "/home",
			"isActive": true,
			"id":       "42",
		},
	},
	"06-style-class-objects.pug": {title: "Style object & class object"},
	"07-and-attributes.pug": {
		title: "&attributes spread",
		data: map[string]interface{}{
			"btnAttrs": map[string]interface{}{
				"class":   "btn btn-primary",
				"data-id": "99",
			},
		},
	},
	"08-block-expansion.pug": {title: "Block expansion"},
	"09-self-closing.pug":    {title: "Self-closing tags"},
	"10-text.pug":            {title: "Text — inline, piped, block"},
	"11-literal-html.pug":    {title: "Literal HTML"},
	"12-code.pug":            {title: "Code — unbuffered, buffered, unescaped"},
	"13-interpolation.pug": {
		title: "Interpolation",
		data: map[string]interface{}{
			"name":    "Alice",
			"snippet": "<strong>bold</strong>",
			"isAdmin": true,
		},
	},
	"14-tag-interpolation.pug": {title: "Tag interpolation #[…]"},
	"15-if-else.pug": {
		title: "if / else if / else",
		data:  map[string]interface{}{"score": 85},
	},
	"16-unless.pug": {
		title: "unless",
		data:  map[string]interface{}{"loggedIn": false},
	},
	"17-each.pug": {
		title: "each / for",
		data: map[string]interface{}{
			"fruits": []string{"Apple", "Banana", "Cherry"},
			"empty":  []string{},
		},
	},
	"18-each-map.pug": {
		title: "each over a map",
		data: map[string]interface{}{
			"config": map[string]interface{}{
				"host": "localhost",
				"port": "8080",
				"env":  "development",
			},
		},
	},
	"19-while.pug": {title: "while loop"},
	"20-case-when.pug": {
		title: "case / when",
		data: map[string]interface{}{
			"role":   "editor",
			"status": "active",
		},
	},
	"21-mixins-basic.pug":      {title: "Mixins — basic"},
	"22-mixins-defaults-rest.pug": {title: "Mixins — default params & rest"},
	"23-mixins-block.pug":      {title: "Mixins — block content"},
	"24-mixins-attributes.pug": {title: "Mixins — attributes map"},
	"25-filters-block.pug":     {title: "Filters — block", opts: filterOpts},
	"26-filters-inline.pug":    {title: "Filters — inline", opts: filterOpts},
	"27-filters-options.pug":   {title: "Filters — options (key=value)", opts: filterOpts},
	"28-filters-chained.pug":   {title: "Filters — chained (:outer:inner)", opts: filterOpts},
	"29-comments.pug":          {title: "Comments"},
	"30-methods.pug": {
		title: "String method expressions",
		data:  map[string]interface{}{"s": "Hello, World!"},
	},
	"31-split-join.pug": {title: "split / join"},
	"32-struct-access.pug": {
		title: "Struct field access",
		data: map[string]interface{}{
			"user": struct {
				Name    string
				Age     int
				Address struct {
					City    string
					Country string
				}
			}{
				Name: "Alice",
				Age:  30,
				Address: struct {
					City    string
					Country string
				}{City: "London", Country: "UK"},
			},
		},
	},
	"33-globals.pug": {
		title: "Globals",
		opts: &gopug.Options{
			Globals: map[string]interface{}{
				"siteName": "Go-Pug Demo",
				"version":  "1.0.0",
				"env":      "development",
			},
		},
	},
	"34-pretty-print.pug": {
		title: "Pretty-print mode",
		opts:  &gopug.Options{Pretty: true},
	},
}

// ---------------------------------------------------------------------------
// Load examples from the embedded filesystem
// ---------------------------------------------------------------------------

func loadExamples() ([]example, error) {
	entries, err := fs.ReadDir(viewsFS, "views")
	if err != nil {
		return nil, fmt.Errorf("reading views dir: %w", err)
	}

	// Collect only .pug files and sort them so numbering is stable.
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".pug") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	examples := make([]example, 0, len(names))
	for _, name := range names {
		raw, err := viewsFS.ReadFile("views/" + name)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}

		m := registry[name]

		// Derive a readable title from the filename if none was registered.
		title := m.title
		if title == "" {
			base := strings.TrimSuffix(name, ".pug")
			// Strip leading "NN-" prefix.
			if idx := strings.Index(base, "-"); idx >= 0 {
				base = base[idx+1:]
			}
			title = strings.ReplaceAll(base, "-", " ")
		}

		examples = append(examples, example{
			filename: name,
			title:    title,
			pug:      string(raw),
			data:     m.data,
			opts:     m.opts,
		})
	}

	return examples, nil
}

// ---------------------------------------------------------------------------
// Page rendering
// ---------------------------------------------------------------------------

const pageCSS = `
* { box-sizing: border-box; margin: 0; padding: 0; }

body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: #f4f5f7;
  color: #1a1a2e;
  padding: 2rem 1.5rem;
}

h1.site-title {
  font-size: 2rem;
  font-weight: 700;
  margin-bottom: 0.25rem;
}

p.site-sub {
  color: #666;
  margin-bottom: 2.5rem;
  font-size: 0.95rem;
}

.grid {
  display: grid;
  gap: 1.5rem;
}

.card {
  background: #fff;
  border: 1px solid #e2e4e9;
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 1px 4px rgba(0,0,0,0.06);
}

.card-header {
  padding: 0.65rem 1.1rem;
  background: #f8f9fb;
  border-bottom: 1px solid #e2e4e9;
  display: flex;
  align-items: center;
  gap: 0.6rem;
}

.card-number {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.6rem;
  height: 1.6rem;
  border-radius: 50%;
  background: #4f46e5;
  color: #fff;
  font-size: 0.7rem;
  font-weight: 700;
  flex-shrink: 0;
}

.card-title {
  font-size: 0.9rem;
  font-weight: 600;
  color: #333;
}

.card-filename {
  margin-left: auto;
  font-size: 0.72rem;
  color: #999;
  font-family: monospace;
}

.card-body {
  display: grid;
  grid-template-columns: 1fr 1fr;
}

@media (max-width: 760px) {
  .card-body { grid-template-columns: 1fr; }
}

.pane {
  padding: 1rem 1.1rem;
  min-width: 0;
}

.pane + .pane {
  border-left: 1px solid #e2e4e9;
}

@media (max-width: 760px) {
  .pane + .pane { border-left: none; border-top: 1px solid #e2e4e9; }
}

.pane-label {
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #999;
  margin-bottom: 0.5rem;
}

pre {
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
  font-size: 0.78rem;
  line-height: 1.65;
  white-space: pre-wrap;
  word-break: break-all;
  border-radius: 6px;
  padding: 0.75rem;
  overflow-x: auto;
  background: #f6f8fa;
  color: #24292f;
}

.output-pane pre {
  background: #fdfaf3;
  color: #2d2d2d;
}

.error-pane pre {
  background: #fff5f5;
  color: #c0392b;
}

footer {
  margin-top: 3rem;
  text-align: center;
  font-size: 0.8rem;
  color: #bbb;
}
`

func writePage(w http.ResponseWriter, exs []example) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html><html lang="en"><head>`)
	sb.WriteString(`<meta charset="UTF-8">`)
	sb.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	sb.WriteString(`<title>Go-Pug Demo</title>`)
	fmt.Fprintf(&sb, "<style>%s</style>", pageCSS)
	sb.WriteString(`</head><body>`)
	sb.WriteString(`<h1 class="site-title">Go-Pug Demo</h1>`)
	sb.WriteString(`<p class="site-sub">Every syntax feature — Pug source on the left, rendered HTML on the right.</p>`)
	sb.WriteString(`<div class="grid">`)

	for i, ex := range exs {
		rendered, renderErr := gopug.Render(ex.pug, ex.data, ex.opts)

		sb.WriteString(`<div class="card">`)

		// Header
		sb.WriteString(`<div class="card-header">`)
		fmt.Fprintf(&sb, `<span class="card-number">%d</span>`, i+1)
		fmt.Fprintf(&sb, `<span class="card-title">%s</span>`, html.EscapeString(ex.title))
		fmt.Fprintf(&sb, `<span class="card-filename">%s</span>`, html.EscapeString(ex.filename))
		sb.WriteString(`</div>`)

		// Body — two panes
		sb.WriteString(`<div class="card-body">`)

		// Left: Pug source
		sb.WriteString(`<div class="pane source-pane">`)
		sb.WriteString(`<div class="pane-label">Pug source</div>`)
		sb.WriteString(`<pre>`)
		sb.WriteString(html.EscapeString(strings.TrimSpace(ex.pug)))
		sb.WriteString(`</pre>`)
		sb.WriteString(`</div>`)

		// Right: rendered output or error
		if renderErr != nil {
			sb.WriteString(`<div class="pane error-pane">`)
			sb.WriteString(`<div class="pane-label">Error</div>`)
			sb.WriteString(`<pre>`)
			sb.WriteString(html.EscapeString(renderErr.Error()))
			sb.WriteString(`</pre>`)
			sb.WriteString(`</div>`)
		} else {
			sb.WriteString(`<div class="pane output-pane">`)
			sb.WriteString(`<div class="pane-label">Rendered HTML</div>`)
			sb.WriteString(`<pre>`)
			sb.WriteString(html.EscapeString(strings.TrimSpace(rendered)))
			sb.WriteString(`</pre>`)
			sb.WriteString(`</div>`)
		}

		sb.WriteString(`</div>`) // card-body
		sb.WriteString(`</div>`) // card
	}

	sb.WriteString(`</div>`) // grid
	sb.WriteString(`<footer>go-pug &mdash; github.com/sinfulspartan/go-pug</footer>`)
	sb.WriteString(`</body></html>`)

	fmt.Fprint(w, sb.String())
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	exs, err := loadExamples()
	if err != nil {
		log.Fatalf("failed to load examples: %v", err)
	}

	log.Printf("Go-Pug demo server — %d examples loaded", len(exs))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writePage(w, exs)
	})

	addr := ":8080"
	log.Printf("Listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
