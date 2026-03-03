# Go-Pug

A full-featured Pug template engine for Go. Write expressive, indentation-based templates that compile to HTML.

Go-Pug brings the elegant simplicity of [Pug](https://pugjs.org) (formerly Jade) templates to Go applications. Instead of writing verbose HTML, you use a clean, whitespace-sensitive syntax inspired by Python and CoffeeScript.

[![CI](https://github.com/sinfulspartan/go-pug/actions/workflows/ci.yml/badge.svg)](https://github.com/sinfulspartan/go-pug/actions/workflows/ci.yml)

---

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Syntax Overview](#syntax-overview)
   - [Tags and Attributes](#tags-and-attributes)
   - [Interpolation](#interpolation)
   - [Loops and Conditionals](#loops-and-conditionals)
   - [Mixins](#mixins)
   - [Template Inheritance](#template-inheritance)
   - [Includes](#includes)
   - [Filters](#filters)
- [API Reference](#api-reference)
- [Development Guide](#development-guide)
   - [Prerequisites](#prerequisites)
   - [Running Tests](#running-tests)
   - [Benchmarks](#benchmarks)
   - [Windows Notes](#windows-notes)
- [Known Limitations](#known-limitations)
- [Contributing](#contributing)
- [License](#license)

---

## Features

- **Elegant syntax** — Indentation-based, minimal punctuation
- **Full Pug language coverage** — Tags, attributes, mixins, includes, template inheritance, filters, block text, case/when, while, each/for, doctype, comments, and more
- **Safe by default** — Auto-escapes output to prevent XSS (`!=` / `!{}` for raw output)
- **Custom filters** — Register Go functions as named filters; receive parsed option key/value pairs
- **Pretty-print mode** — Optional indented HTML output for readability
- **Template cache** — `CompileFile` caches parsed ASTs; call `ClearCache()` to invalidate
- **Method expressions** — `name.toUpperCase()`, `items.length`, `s.slice(0, 3)`, etc.
- **`&attributes` spread** — Merge a map expression into a tag's attributes at render time
- **Standard-library compatible** — Integrates seamlessly with `net/http`
- **No external dependencies** — Pure Go; only the standard library

---

## Quick Start

### Installation

```sh
go get github.com/sinfulspartan/go-pug
```

### Basic Example

Create a template file (e.g., `hello.pug`):

```pug
doctype html
html
  head
    title= page.Title
  body
    h1= page.Heading
    p Welcome to Go-Pug!
    ul
      each item in items
        li= item
```

Use it in your Go code:

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
            "Heading": "Hello, World!",
        },
        "items": []string{"Item 1", "Item 2", "Item 3"},
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

## Syntax Overview

### Tags and Attributes

```pug
doctype html
html(lang="en")
  head
    meta(charset="UTF-8")
    title My App
  body
    h1.title#main Hello, Go-Pug!
    p.lead(class=extraClass) Welcome.

    // Inline tag nesting with block expansion
    ul
      li: a(href="/home") Home
      li: a(href="/about") About

    // Self-closing tags
    img(src="/logo.png", alt="Logo")
    input(type="text", name="q")/

    // Attribute spreading
    button(type="button")&attributes(btnAttrs) Click me
```

**Class and ID shorthand:**

```pug
.container         // <div class="container">
#hero              // <div id="hero">
span.badge.primary // <span class="badge primary">
```

**Dynamic attributes:**

```pug
- active = true
a(href="/", class=active ? "active" : "") Home
input(checked=isChecked, disabled)
div(style={color: "red", fontSize: "14px"})
```

### Interpolation

```pug
p Hello, #{name}!
p Raw: !{rawHTML}
p Upper: #{name.toUpperCase()}
p #[strong Bold text] inline.
p= variable
p!= htmlContent
```

### Loops and Conditionals

```pug
each item, index in items
  li #{index}: #{item}

each val in []
  li= val
else
  p No items found.

if score >= 90
  p Grade: A
else if score >= 80
  p Grade: B
else
  p Grade: C

unless hidden
  p Visible content.

while count > 0
  p= count
  - count--

case role
  when "admin"
    p Admin panel.
  when "user"
    p User dashboard.
  default
    p Guest view.
```

### Mixins

```pug
mixin button(text, type="button")
  button(type=type)= text

mixin card(title)
  .card
    h2= title
    if block
      block

+button("Save")
+button("Delete", "submit")
+card("Profile")
  p Card body content here.
```

Mixins support:

- Default parameter values: `mixin name(title="Default")`
- Rest parameters: `mixin list(...items)`
- Block content via the `block` keyword
- The implicit `attributes` map for `&attributes(attributes)`

### Template Inheritance

**layout.pug:**

```pug
doctype html
html
  head
    title
      block title
        | Default Title
  body
    block content
    block footer
      footer Default footer.
```

**page.pug:**

```pug
extends layout.pug

block title
  | My Page

block content
  h1 Hello!
  p Page body.
```

You can also use `append` / `prepend` shorthand:

```pug
extends layout.pug

append scripts
  script(src="/app.js")
```

### Includes

```pug
include partials/nav.pug
include:uppercase README.txt
include /absolute/path/to/file.pug
```

Raw (non-Pug) files are included verbatim. If a filter name is supplied after
`:`, the file contents are passed through that filter before insertion.

### Filters

Register Go functions as named filters via `Options.Filters`. Each filter
receives the block content **and** a `map[string]string` of parsed options:

```go
opts := &gopug.Options{
    Filters: map[string]gopug.FilterFunc{
        "markdown": func(text string, opts map[string]string) (string, error) {
            flavor := opts["flavor"]  // e.g. "gfm"
            _ = flavor
            return myMarkdownRenderer(text), nil
        },
    },
}
```

In a template:

```pug
:markdown(flavor="gfm")
  # Hello

  This is **Markdown** content.
```

**Inline filter:**

```pug
p
  :uppercase Hello World
```

**Chained filters** (innermost applied first):

```pug
:wrap:uppercase
  content
```

**`include` with a filter:**

```pug
include :markdown article.md
```

**Adapting old-style filters** (no options):

```go
// SimpleFilter wraps a plain func(string)(string,error) into a FilterFunc.
opts.Filters["plain"] = gopug.SimpleFilter(myOldFilter)
```

---

## API Reference

```go
// Compile a template string into a reusable Template.
tpl, err := gopug.Compile(src, opts)

// Render a template string directly (compile + render in one step).
html, err := gopug.Render(src, data, opts)

// Compile a .pug file; result is cached by absolute path.
tpl, err := gopug.CompileFile("views/index.pug", opts)

// Render a .pug file (reads, compiles, renders).
html, err := gopug.RenderFile("views/index.pug", data, opts)

// Render a compiled template.
html, err := tpl.Render(data)

// Render to an io.Writer.
err := tpl.RenderToWriter(w, data)

// Invalidate the compile cache.
gopug.ClearCache()
```

**Options:**

```go
type Options struct {
    Basedir string                     // root for absolute include/extends paths
    Pretty  bool                       // indent HTML output
    Globals map[string]interface{}     // variables available to every render
    Filters map[string]FilterFunc      // custom filter functions
}
```

**FilterFunc signature:**

```go
type FilterFunc func(text string, options map[string]string) (string, error)
```

---

## Development Guide

### Prerequisites

| Tool     | Minimum version | Notes                                             |
| -------- | --------------- | ------------------------------------------------- |
| Go       | 1.21            | Declared in `go.mod`                              |
| GNU Make | 3.81            | For `make` targets                                |
| Git Bash | any             | **Windows only** — provides `sh.exe` used by Make |

### Running Tests

```sh
# Run the full test suite (currently 291+ tests)
make test

# Verbose output
make test-v

# With race detector
make test-race

# Coverage report (text)
make cover

# Coverage as HTML (opens in browser on macOS/Linux)
make cover-html
```

### Benchmarks

The benchmark suite lives in `pkg/gopug/benchmark_test.go`. Results are
written to `BENCHMARKS.md` (Markdown), `benchmarks.json` (machine-readable),
or `benchmarks.csv` (spreadsheet-friendly) by the `scripts/bench2md` tool.

```sh
# Full benchmark run → BENCHMARKS.md (default 1 s per benchmark)
make bench

# Quick run (100 ms per benchmark) → BENCHMARKS.md
make bench-short

# CPU profiling → cpu.prof + BENCHMARKS.md
make bench-cpu

# Memory profiling → mem.prof + BENCHMARKS.md
make bench-mem

# Benchmark report only (pipe through bench2md)
make bench-report

# Machine-readable outputs
make bench-json     # → benchmarks.json
make bench-csv      # → benchmarks.csv

# Tune parameters on the command line
make bench BENCH=BenchmarkRender BENCHTIME=2s BENCHCOUNT=3
```

Inspect profiles with:

```sh
go tool pprof cpu.prof
go tool pprof mem.prof
```

The `bench2md` tool can also be run directly:

```sh
go test -bench . -benchmem ./pkg/gopug \
  | go run ./scripts/bench2md -format md -o BENCHMARKS.md

go test -bench . -benchmem ./pkg/gopug \
  | go run ./scripts/bench2md -format json -o benchmarks.json

go test -bench . -benchmem ./pkg/gopug \
  | go run ./scripts/bench2md -format csv -o benchmarks.csv
```

Available `bench2md` flags:

| Flag      | Default | Description                              |
| --------- | ------- | ---------------------------------------- |
| `-format` | `md`    | Output format: `md`, `json`, or `csv`    |
| `-o`      | stdout  | Write output to a file instead of stdout |

### Code quality

```sh
make fmt          # gofmt -s
make vet          # go vet ./...
make lint         # golangci-lint run (if installed)
make tidy         # go mod tidy
```

### Windows Notes

The Makefile uses POSIX `sh` for portability. On Windows, GNU Make must be
able to find `sh.exe` from **Git Bash**. The Makefile hard-codes:

```makefile
SHELL := C:/Program Files/Git/usr/bin/sh.exe
```

If your Git installation is in a different location, override it:

```sh
make bench SHELL="D:/Git/usr/bin/sh.exe"
```

Alternatively, run the commands directly without Make:

```sh
go test -count=1 -run='^$' -bench=. -benchmem ./pkg/gopug \
  | go run ./scripts/bench2md -format md -o BENCHMARKS.md
```

`go test -cpuprofile` / `-memprofile` leave a compiled test binary
(`gopug.test.exe`) in the package directory on Windows; `make clean` removes
it automatically.

### CI

GitHub Actions runs three jobs on every push to `main` / pull request:

| Job     | Platforms          | What it does                                                                                                |
| ------- | ------------------ | ----------------------------------------------------------------------------------------------------------- |
| `test`  | ubuntu + windows   | `go vet` + full test suite                                                                                  |
| `race`  | ubuntu             | Test suite with `-race` detector                                                                            |
| `build` | ubuntu             | Build `bin/go-pug` CLI binary                                                                               |
| `bench` | ubuntu (push only) | Benchmark run; uploads `BENCHMARKS.md`, `benchmarks.json`, `benchmarks.csv` as artifacts (retained 90 days) |

---

## Known Limitations

The following Pug features are **not yet implemented**:

| Feature                                                                                                   | Status                                                                                             |
| --------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Filter options forwarding                                                                                 | **Implemented** — `FilterFunc` receives `map[string]string` options parsed from `(key=val)` syntax |
| Attribute values with spaces in unquoted position (e.g. `class!=attributes.class href=href` in one token) | Lexer limitation; use quoted values or `&attributes` as a workaround                               |
| Ternary in `each` collection expression (e.g. `each v in cond ? list : [fallback]`)                       | Only simple variables and inline array literals `[a, b, c]` are supported                          |

---

## Contributing

Contributions are welcome! Please:

1. Open an issue to discuss major changes before starting work.
2. Write tests for new features — see `pkg/gopug/gopug_test.go` for patterns.
3. Run `make test` and `make vet` before opening a PR; all tests must pass.
4. Keep commits focused and include a meaningful commit message.
5. Follow the existing code style (Go standard formatting via `gofmt -s`).

---

## License

MIT License — see [`LICENSE`](LICENSE) for details.
