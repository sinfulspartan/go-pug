# Go-Pug

A full-featured [Pug](https://pugjs.org) template engine for Go. Write clean, indentation-based templates that compile to HTML.

[![CI](https://github.com/sinfulspartan/go-pug/actions/workflows/ci.yml/badge.svg)](https://github.com/sinfulspartan/go-pug/actions/workflows/ci.yml)

---

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Demo Server](#demo-server)
- [Syntax Reference](#syntax-reference)
   - [Doctype](#doctype)
   - [Tags](#tags)
   - [Attributes](#attributes)
   - [Text Content](#text-content)
   - [Code](#code)
   - [Interpolation](#interpolation)
   - [Conditionals](#conditionals)
   - [Loops](#loops)
   - [Case / When](#case--when)
   - [Mixins](#mixins)
   - [Template Inheritance](#template-inheritance)
   - [Includes](#includes)
   - [Filters](#filters)
   - [Comments](#comments)
- [API Reference](#api-reference)
- [Development](#development)
- [Known Limitations](#known-limitations)
- [Contributing](#contributing)
- [License](#license)

---

## Features

- **Full Pug language coverage** — doctype, tags, attributes, text, code, interpolation, conditionals, loops, case/when, mixins, template inheritance, includes, filters, comments
- **Safe by default** — all output is HTML-escaped; opt out per-expression with `!=` or `!{}`
- **Custom filters** — register Go functions as named filters; receive both body text and parsed `(key=value)` options
- **Template cache** — `CompileFile` caches parsed ASTs by absolute path; call `ClearCache()` to invalidate
- **Pretty-print mode** — optional indented HTML output
- **Method expressions** — `s.toUpperCase()`, `s.trim()`, `s.slice(0,3)`, `items.length`, and more
- **`&attributes` spread** — merge a map into a tag's attribute list at render time
- **No external dependencies** — pure Go, standard library only
- **Interactive demo server** — `make run` launches a local web server showing all 34 syntax examples side-by-side (Pug source, HTML output, live preview)

---

## Installation

```sh
go get github.com/sinfulspartan/go-pug
```

Import path: `github.com/sinfulspartan/go-pug/pkg/gopug`

---

## Demo Server

The `cmd/` directory contains an HTTP demo server that renders every supported syntax feature as a card showing the Pug source, the generated HTML, and a live preview iframe.

```sh
make run           # build + start on http://localhost:8080
# or
go run ./cmd
```

The server embeds all template files and stylesheets at compile time (`//go:embed`), so no extra assets need to be on disk at runtime. Three built-in filters are registered for the filter examples:

| Filter      | Behaviour                                                                                         |
| ----------- | ------------------------------------------------------------------------------------------------- |
| `uppercase` | Uppercases the body text                                                                          |
| `shout`     | Uppercases each line and appends a configurable suffix (`!` by default); accepts `suffix=` option |
| `wrap`      | Wraps each line in configurable brackets (`[` `]` by default); accepts `open=` / `close=` options |

---

## Quick Start

**hello.pug**

```pug
doctype html
html(lang="en")
  head
    title= page.Title
  body
    h1= page.Heading
    ul
      each item in items
        li= item
```

**main.go**

```go
package main

import (
    "fmt"
    "github.com/sinfulspartan/go-pug/pkg/gopug"
)

func main() {
    data := map[string]interface{}{
        "page": map[string]string{
            "Title":   "My Page",
            "Heading": "Hello, Go-Pug!",
        },
        "items": []string{"Apples", "Bananas", "Cherries"},
    }

    html, err := gopug.RenderFile("hello.pug", data, nil)
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Println(html)
}
```

---

## Syntax Reference

### Doctype

```pug
doctype html
doctype xml
doctype transitional
doctype strict
doctype frameset
doctype 1.1
doctype basic
doctype mobile
doctype plist
doctype html PUBLIC "-//W3C//DTD XHTML Basic 1.1//EN"
```

### Tags

Tags are written by name, indentation defines nesting. Void elements (`br`, `hr`, `img`, `input`, `link`, `meta`, etc.) are self-closed automatically.

```pug
div
  p Hello
  span World
```

**Block expansion** — inline nesting with `:`

```pug
ul
  li: a(href="/") Home
  li: a(href="/about") About
```

**Explicit self-close** — append `/` for non-void elements

```pug
foo/
```

### Attributes

```pug
a(href="/path", class="nav-link") Link
input(type="checkbox", checked)
input(type="checkbox", checked=false)
```

**Class and ID shorthand**

```pug
.container          // <div class="container">
#hero               // <div id="hero">
p.lead.text-muted
a.btn#submit
```

Shorthands and attribute lists can be mixed freely:

```pug
div.card(id="main", data-x="1")
```

**Dynamic and unescaped attributes**

```pug
a(href=url) Click
a(href=url, class=isActive ? "active" : "")
a(href!=rawUrl)
```

**Style object**

```pug
div(style={color: "red", fontSize: "14px"})
```

**Class array / object**

```pug
div(class=["foo", "bar"])
div(class={active: true, disabled: false})
```

**`&attributes` spread** — merge a map expression at render time

```pug
button(type="button")&attributes(btnAttrs)
```

### Text Content

**Inline text** — space after tag name

```pug
p Hello, world!
```

**Piped text** — `|` prefix

```pug
p
  | First line.
  | Second line.
```

**Block text** — `.` suffix on the tag opens a verbatim text block

```pug
p.
  This entire indented block is plain text.
  No child tags are parsed inside here.
```

**Literal HTML** — lines starting with `<` are passed through verbatim

```pug
<div class="raw">inserted as-is</div>
```

### Code

**Unbuffered** — executed, output not written

```pug
- count = 0
- items = ["a", "b"]
```

Multi-line unbuffered block:

```pug
-
  x = 1
  y = 2
```

**Buffered (escaped)** — value is HTML-escaped and written

```pug
p= title
= "Hello " + name
```

**Buffered (unescaped)** — raw HTML, use with care

```pug
p!= htmlContent
!= rawFragment
```

### Interpolation

**Inside text**

```pug
p Hello, #{name}!
p Raw: !{htmlSnippet}
```

**Escaped interpolation** — literal `#{`

```pug
p \#{not interpolated}
```

**Tag interpolation** — inline tags within text

```pug
p Click #[a(href="/login") here] to sign in.
p Use #[strong bold] for emphasis.
```

**Inline code on a tag** — `=` and `!=` suffixes

```pug
h1= pageTitle
div!= rawHtml
```

### Conditionals

```pug
if score >= 90
  p Grade: A
else if score >= 80
  p Grade: B
else
  p Grade: C
```

**`unless`** — negated `if`

```pug
unless isLoggedIn
  a(href="/login") Sign in
```

### Loops

**`each` / `for`**

```pug
each item in items
  li= item

each item, index in items
  li #{index}: #{item}
```

Iterating over a map yields values; use the key variable to capture keys:

```pug
each val, key in config
  dt= key
  dd= val
```

**`else` branch** — rendered when the collection is empty

```pug
each item in items
  li= item
else
  li No items found.
```

**Inline array literals**

```pug
each color in ["red", "green", "blue"]
  span= color
```

**`while`**

```pug
- n = 3
while n > 0
  p= n
  - n--
```

### Case / When

```pug
case role
  when "admin"
    p Admin view.
  when "editor"
    p Editor view.
  default
    p Guest view.
```

Fall-through — an empty `when` body falls through to the next clause:

```pug
case status
  when "active"
  when "enabled"
    p On.
  default
    p Off.
```

### Mixins

**Declaration and call**

```pug
mixin greeting(name)
  p Hello, #{name}!

+greeting("Alice")
```

**Default parameter values**

```pug
mixin button(text, type="button")
  button(type=type)= text

+button("Save")
+button("Delete", "submit")
```

**Rest parameters**

```pug
mixin list(...items)
  ul
    each item in items
      li= item

+list("a", "b", "c")
```

**Block content** — the caller passes a block; use `block` to render it and `if block` to test for its presence

```pug
mixin card(title)
  .card
    h2= title
    if block
      block

+card("Profile")
  p Body content here.
```

**The `attributes` map** — callers can pass extra attributes via `&attributes`

```pug
mixin tag(name)
  div&attributes(attributes)= name

+tag("Hello")(class="highlight")
```

### Template Inheritance

**layout.pug**

```pug
doctype html
html
  head
    title
      block title
        | My Site
  body
    block content
    block footer
      footer Default footer.
```

**page.pug**

```pug
extends layout.pug

block title
  | Home Page

block content
  h1 Welcome
  p Page body.
```

**`append` and `prepend`** — add content around a parent block's default

```pug
extends layout.pug

append footer
  p Extra footer line.

prepend footer
  p Notice above the footer.
```

Shorthand (standalone, without `block` keyword):

```pug
append scripts
  script(src="/app.js")
```

Inheritance chains are supported — a child can itself be extended.

### Includes

```pug
include partials/nav.pug
include /absolute/from/basedir.pug
include styles.css
include data.txt
```

Files without a `.pug` extension are included verbatim. An included Pug file shares the current scope and any mixins declared in it become available to the including template.

**Include with a filter** — apply a registered filter to a raw file's content before inserting it

```pug
include :uppercase README.txt
```

### Filters

Register Go functions as named filters via `Options.Filters`. Each `FilterFunc` receives the body text and a `map[string]string` of any parsed options. Filter output is written **raw** — the filter function is responsible for any HTML escaping it needs (this allows filters such as Markdown renderers to return real HTML tags).

```go
opts := &gopug.Options{
    Filters: map[string]gopug.FilterFunc{
        "markdown": func(text string, opts map[string]string) (string, error) {
            flavor := opts["flavor"] // "" if not specified
            return renderMarkdown(text, flavor), nil
        },
    },
}
```

**Block filter**

```pug
:markdown(flavor="gfm")
  # Hello

  Paragraph text.
```

**Inline filter** — pipe text followed by a filter as sibling children of a tag; use a trailing space in the pipe text to separate the label from the filter output

```pug
p
  | Result:
  :uppercase hello world
```

**Chained filters** — innermost applied first; options may appear before or after a subfilter colon

```pug
:wrap:uppercase
  content

:outer(suffix="!!"):inner
  body text
```

Multi-line filter output has its `\n` characters replaced with `<br>` tags so visual line breaks are preserved in the browser without forcing monospace `<pre>` formatting. Single-line output is emitted as-is.

**Options syntax** — key=value pairs in parentheses, quoted or bare

```pug
:my-filter(start="BEGIN", end="FINISH", pretty)
  body text
```

The options map keys and values are always strings. A bare flag like `pretty` is stored as `"true"`.

**`SimpleFilter` adapter** — wrap a plain `func(string)(string,error)` for use with the new API

```go
opts.Filters["plain"] = gopug.SimpleFilter(myOldFilter)
```

### Comments

**HTML comment** — rendered into output

```pug
// This becomes <!-- This becomes -->
```

**Unbuffered comment** — never appears in output

```pug
//- This is invisible
```

Multi-line comments indent their body:

```pug
//
  First line.
  Second line.
```

---

## API Reference

### Functions

```go
// Compile a template string into a reusable Template.
tpl, err := gopug.Compile(src string, opts *gopug.Options) (*gopug.Template, error)

// Render a template string in one step (compile + render).
html, err := gopug.Render(src string, data map[string]interface{}, opts *gopug.Options) (string, error)

// Compile a .pug file; result is cached by absolute path.
tpl, err := gopug.CompileFile(path string, opts *gopug.Options) (*gopug.Template, error)

// Render a .pug file in one step (read + compile + render).
html, err := gopug.RenderFile(path string, data map[string]interface{}, opts *gopug.Options) (string, error)

// Invalidate the entire compile cache.
gopug.ClearCache()
```

### Template methods

```go
// Render with a data map; returns the HTML string.
html, err := tpl.Render(data map[string]interface{}) (string, error)

// Render directly into an io.Writer.
err := tpl.RenderToWriter(w io.Writer, data map[string]interface{}) error
```

### Options

```go
type Options struct {
    Basedir string                // root directory for absolute include / extends paths
    Pretty  bool                  // emit indented HTML
    Globals map[string]interface{} // variables visible to every render of this template
    Filters map[string]FilterFunc  // named filter functions
}
```

`Basedir` defaults to the directory of the template file when using `CompileFile` or `RenderFile`. When using `Compile` or `Render` with relative includes, set `Basedir` explicitly.

`Globals` are merged into `data` before rendering; a key present in `data` takes precedence over the same key in `Globals`.

### FilterFunc

```go
type FilterFunc func(text string, options map[string]string) (string, error)
```

The `options` map is never `nil` — it is an empty map when no options were supplied in the template.

```go
// SimpleFilter wraps a plain func(string)(string,error) into a FilterFunc.
gopug.SimpleFilter(fn func(string) (string, error)) FilterFunc
```

### Expressions

The expression evaluator supports:

| Feature             | Example                                        |
| ------------------- | ---------------------------------------------- |
| Variable lookup     | `name`, `user.address.city`                    |
| Struct field access | `user.Name` (exported fields)                  |
| Map key access      | `config.debug`                                 |
| Array / slice index | `items[0]`                                     |
| Boolean literals    | `true`, `false`                                |
| Numeric literals    | `42`, `3.14`                                   |
| String literals     | `"hello"`, `'world'`                           |
| Arithmetic          | `a + b` (numeric add or string concat)         |
| Comparison          | `==`, `!=`, `===`, `!==`, `<`, `>`, `<=`, `>=` |
| Logical             | `&&`, `\|\|`, `!`                              |
| Ternary             | `cond ? a : b`                                 |
| Inline array        | `["a", "b", "c"]`                              |
| Inline style object | `{color: "red", fontSize: "14px"}`             |
| Inline class object | `{active: isActive, disabled: false}`          |

**String methods**

| Method                         | Description                                                                                                              |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `.length`                      | Character count (or element count for slices/maps)                                                                       |
| `.toUpperCase()`               | Upper-case                                                                                                               |
| `.toLowerCase()`               | Lower-case                                                                                                               |
| `.trim()`                      | Strip leading/trailing whitespace                                                                                        |
| `.trimStart()` / `.trimLeft()` | Strip leading whitespace                                                                                                 |
| `.trimEnd()` / `.trimRight()`  | Strip trailing whitespace                                                                                                |
| `.slice(start[, end])`         | Substring by rune index; negative indices count from end                                                                 |
| `.indexOf(needle)`             | First index of needle, or `-1`                                                                                           |
| `.includes(needle)`            | `true` / `false`                                                                                                         |
| `.startsWith(prefix)`          | `true` / `false`                                                                                                         |
| `.endsWith(suffix)`            | `true` / `false`                                                                                                         |
| `.replace(old, new)`           | Replace first occurrence                                                                                                 |
| `.repeat(n)`                   | Repeat string n times                                                                                                    |
| `.split(sep)`                  | Split into a slice (usable as an `each` collection or chained into `.join`)                                              |
| `.join(sep)`                   | Join a slice into a string; works on Go slice variables and on chained expressions such as `csv.split(",").join(" \| ")` |

---

## Development

### Requirements

| Tool     | Notes                                              |
| -------- | -------------------------------------------------- |
| Go 1.26+ | Declared in `go.mod`                               |
| GNU Make | Optional — all targets have plain `go` equivalents |
| Git Bash | **Windows only** — Make recipes require `sh.exe`   |

### Common commands

```sh
make test          # run the full test suite
make test-v        # verbose output
make test-race     # race detector
make cover         # coverage profile + text summary
make cover-html    # coverage as HTML (opens in browser on macOS / Linux)

make build         # build bin/go-pug demo server binary
make run           # build + run the demo server on http://localhost:8080
make fmt           # gofmt -s
make vet           # go vet ./...
make lint          # golangci-lint (if installed)
make tidy          # go mod tidy
make clean         # remove all generated artifacts
```

### Benchmarks

```sh
make bench          # full run → BENCHMARKS.md
make bench-short    # 100 ms per benchmark → BENCHMARKS.md
make bench-cpu      # CPU profile → cpu.prof + BENCHMARKS.md
make bench-mem      # memory profile → mem.prof + BENCHMARKS.md
make bench-json     # machine-readable → benchmarks.json
make bench-csv      # spreadsheet-friendly → benchmarks.csv
```

Tunable variables:

```sh
make bench BENCH=BenchmarkRenderLarge BENCHTIME=2s BENCHCOUNT=5
```

Profiles can be inspected with `go tool pprof cpu.prof` / `go tool pprof mem.prof`.

The `scripts/bench2md` tool can also be called directly:

```sh
go test -bench . -benchmem ./pkg/gopug \
  | go run ./scripts/bench2md -format md  -o BENCHMARKS.md

go test -bench . -benchmem ./pkg/gopug \
  | go run ./scripts/bench2md -format json -o benchmarks.json

go test -bench . -benchmem ./pkg/gopug \
  | go run ./scripts/bench2md -format csv  -o benchmarks.csv
```

### Windows notes

The Makefile sets `SHELL` to the Git Bash `sh.exe`. The default path is:

```
C:/Program Files/Git/usr/bin/sh.exe
```

If your Git installation is elsewhere, override it on the command line:

```sh
make test SHELL="D:/Git/usr/bin/sh.exe"
```

Or run the `go` commands directly — no shell is required for that.

`go test -cpuprofile` / `-memprofile` leave a compiled test binary (`gopug.test.exe`) in the package directory on Windows. `make clean` removes it.

### CI

GitHub Actions runs on every push to `main` and on pull requests:

| Job     | Platforms          | Description                                                                                                 |
| ------- | ------------------ | ----------------------------------------------------------------------------------------------------------- |
| `test`  | ubuntu, windows    | `go vet` + full test suite                                                                                  |
| `race`  | ubuntu             | test suite with `-race`                                                                                     |
| `build` | ubuntu             | build the demo server binary (`bin/go-pug`)                                                                 |
| `bench` | ubuntu (push only) | benchmark run; uploads `BENCHMARKS.md`, `benchmarks.json`, `benchmarks.csv` as artifacts (retained 90 days) |

---

## Known Limitations

| Area                                        | Detail                                                                                                                                                                  |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Unquoted attribute values containing spaces | e.g. `class!=attributes.class href=href` is mis-lexed as a single token. Workaround: quote the value or use `&attributes`.                                              |
| Ternary in `each` collection                | `each v in cond ? list : [fallback]` is not supported. Only a plain variable or an inline array literal `[a, b, c]` works as the collection expression.                 |
| Filter output escaping                      | Filter output is always written raw. Filters that return plain text must escape it themselves (e.g. with `html.EscapeString`) if the text may contain `<`, `>`, or `&`. |

---

## Contributing

1. Open an issue before starting significant work.
2. Add tests — see `pkg/gopug/gopug_test.go` for patterns.
3. Run `make test` and `make vet` before opening a PR; all tests must pass.
4. Keep commits small and focused with a clear message.
5. Follow existing code style (`gofmt -s`).

---

## License

MIT — see [`LICENSE`](LICENSE) for details.
