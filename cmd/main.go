package main

import (
	"embed"
	"fmt"
	"html"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

//go:embed views/*.pug views/*.css
var viewsFS embed.FS

func loadPreviewStyles() string {
	data, err := viewsFS.ReadFile("views/preview.css")
	if err != nil {
		return ""
	}
	return "<style>\n" + string(data) + "\n</style>\n"
}

type example struct {
	filename string
	title    string
	pug      string
	data     map[string]interface{}
	opts     *gopug.Options
	basedir  string // non-empty for examples that use include/extends
}

type meta struct {
	title string
	data  map[string]interface{}
	opts  *gopug.Options
}

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
			// Wrap each line individually so multi-line output produces
			// "[LINE1]\n[LINE2]" rather than "[LINE1\nLINE2]" — the latter
			// causes renderFilter to insert <br> inside the brackets.
			lines := strings.Split(strings.TrimSpace(s), "\n")
			for i, l := range lines {
				lines[i] = open + strings.TrimSpace(l) + close
			}
			return strings.Join(lines, "\n"), nil
		},
	},
}

var registry = map[string]meta{
	"01-doctype.pug":    {title: "Doctype"},
	"02-tags.pug":       {title: "Tags & nesting"},
	"03-class-id.pug":   {title: "Class & ID shorthand"},
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
	"21-mixins-basic.pug":         {title: "Mixins — basic"},
	"22-mixins-defaults-rest.pug": {title: "Mixins — default params & rest"},
	"23-mixins-block.pug":         {title: "Mixins — block content"},
	"24-mixins-attributes.pug":    {title: "Mixins — attributes map"},
	"25-filters-block.pug":        {title: "Filters — block", opts: filterOpts},
	"26-filters-inline.pug":       {title: "Filters — inline", opts: filterOpts},
	"27-filters-options.pug":      {title: "Filters — options (key=value)", opts: filterOpts},
	"28-filters-chained.pug":      {title: "Filters — chained (:outer:inner)", opts: filterOpts},
	"29-comments.pug":             {title: "Comments"},
	"30-methods.pug": {
		title: "String method expressions",
		data:  map[string]interface{}{"s": "  Hello, World!  "},
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
	"35-includes.pug":               {title: "Includes"},
	"36-extends.pug":                {title: "Template inheritance — extends & block"},
	"37-expressions.pug":            {title: "Expressions — arithmetic, comparison, logic, ternary"},
	"38-mixins-nested.pug":          {title: "Mixins — nested calls"},
	"39-and-attributes-dynamic.pug": {title: "&attributes — dynamic spread"},
}

// extractViewsToTemp writes all embedded views/*.pug files to a temp
// directory so that include/extends resolution can find them on disk.
// The caller is responsible for removing the directory when done.
func extractViewsToTemp() (string, error) {
	dir, err := os.MkdirTemp("", "gopug-views-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	entries, err := fs.ReadDir(viewsFS, "views")
	if err != nil {
		return "", fmt.Errorf("reading embedded views: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pug") {
			continue
		}
		data, err := viewsFS.ReadFile("views/" + e.Name())
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0644); err != nil {
			return "", fmt.Errorf("writing %s: %w", e.Name(), err)
		}
	}
	return dir, nil
}

func loadExamples(viewsDir string) ([]example, error) {
	entries, err := fs.ReadDir(viewsFS, "views")
	if err != nil {
		return nil, fmt.Errorf("reading views dir: %w", err)
	}

	// Files that need include/extends resolution
	needsBasedir := map[string]bool{
		"35-includes.pug": true,
		"36-extends.pug":  true,
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".pug") && !strings.HasPrefix(e.Name(), "_") {
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

		title := m.title
		if title == "" {
			base := strings.TrimSuffix(name, ".pug")
			if idx := strings.Index(base, "-"); idx >= 0 {
				base = base[idx+1:]
			}
			title = strings.ReplaceAll(base, "-", " ")
		}

		bd := ""
		if needsBasedir[name] {
			bd = viewsDir
		}
		examples = append(examples, example{
			filename: name,
			title:    title,
			pug:      string(raw),
			data:     m.data,
			opts:     m.opts,
			basedir:  bd,
		})
	}

	return examples, nil
}

func writePage(w http.ResponseWriter, exs []example, previewStyles string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html><html lang="en"><head>`)
	sb.WriteString(`<meta charset="UTF-8">`)
	sb.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	sb.WriteString(`<title>Go-Pug Demo</title>`)
	sb.WriteString(`<link rel="stylesheet" href="/demo.css">`)
	sb.WriteString(`</head><body>`)
	sb.WriteString(`<h1 class="site-title">Go-Pug Demo</h1>`)
	sb.WriteString(`<p class="site-sub">Every syntax feature — Pug source on the left, rendered HTML on the right.</p>`)
	sb.WriteString(`<div class="grid">`)

	for i, ex := range exs {
		var rendered string
		var renderErr error
		if ex.basedir != "" {
			opts := ex.opts
			if opts == nil {
				opts = &gopug.Options{}
			}
			merged := *opts
			merged.Basedir = ex.basedir
			rendered, renderErr = gopug.RenderFile(filepath.Join(ex.basedir, ex.filename), ex.data, &merged)
		} else {
			rendered, renderErr = gopug.Render(ex.pug, ex.data, ex.opts)
		}

		sb.WriteString(`<div class="card">`)
		sb.WriteString(`<div class="card-header">`)
		fmt.Fprintf(&sb, `<span class="card-number">%d</span>`, i+1)
		fmt.Fprintf(&sb, `<span class="card-title">%s</span>`, html.EscapeString(ex.title))
		fmt.Fprintf(&sb, `<span class="card-filename">%s</span>`, html.EscapeString(ex.filename))
		sb.WriteString(`</div>`)

		sb.WriteString(`<div class="card-body">`)

		sb.WriteString(`<div class="pane source-pane">`)
		sb.WriteString(`<div class="pane-label">Pug source</div>`)
		sb.WriteString(`<pre>`)
		sb.WriteString(html.EscapeString(strings.TrimSpace(ex.pug)))
		sb.WriteString(`</pre>`)
		sb.WriteString(`</div>`)

		if renderErr != nil {
			sb.WriteString(`<div class="pane error-pane">`)
			sb.WriteString(`<div class="pane-label">Error</div>`)
			sb.WriteString(`<pre>`)
			sb.WriteString(html.EscapeString(renderErr.Error()))
			sb.WriteString(`</pre>`)
			sb.WriteString(`</div>`)
		} else {
			trimmed := strings.TrimSpace(rendered)
			sb.WriteString(`<div class="pane output-pane">`)
			sb.WriteString(`<div class="pane-label">HTML source</div>`)
			sb.WriteString(`<pre>`)
			sb.WriteString(html.EscapeString(trimmed))
			sb.WriteString(`</pre>`)
			sb.WriteString(`<div class="pane-label preview-label">Preview</div>`)
			sb.WriteString(`<iframe class="preview-frame" srcdoc="`)
			sb.WriteString(html.EscapeString(previewStyles + trimmed))
			sb.WriteString(`" loading="lazy" onload="(function(f){var resize=function(){var d=f.contentDocument;var h=Math.max(d.body.scrollHeight,d.documentElement.scrollHeight);f.style.height=h+32+'px';};resize();requestAnimationFrame(resize);})(this)"></iframe>`)
			sb.WriteString(`</div>`)
		}

		sb.WriteString(`</div>`)
		sb.WriteString(`</div>`)
	}

	sb.WriteString(`</div>`)
	sb.WriteString(`<footer>go-pug &mdash; github.com/sinfulspartan/go-pug</footer>`)
	sb.WriteString(`</body></html>`)

	fmt.Fprint(w, sb.String())
}

func main() {
	viewsDir, err := extractViewsToTemp()
	if err != nil {
		log.Fatalf("failed to extract views: %v", err)
	}
	defer os.RemoveAll(viewsDir)

	exs, err := loadExamples(viewsDir)
	if err != nil {
		log.Fatalf("failed to load examples: %v", err)
	}

	log.Printf("Go-Pug demo server — %d examples loaded", len(exs))

	previewStyles := loadPreviewStyles()

	http.HandleFunc("/demo.css", func(w http.ResponseWriter, r *http.Request) {
		data, err := viewsFS.ReadFile("views/demo.css")
		if err != nil {
			http.Error(w, "stylesheet not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(data)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writePage(w, exs, previewStyles)
	})

	addr := ":8080"
	log.Printf("Listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
