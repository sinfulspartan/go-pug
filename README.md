# Go-Pug

A Pug template engine for Go. Write expressive, indentation-based templates that compile to HTML.

Go-Pug brings the elegant simplicity of Pug (formerly Jade) templates to Go applications. Instead of writing verbose HTML, you can use a clean, whitespace-sensitive syntax inspired by Python and CoffeeScript.

## Features

- **Elegant syntax** — Indentation-based, minimal punctuation
- **Fast compilation** — Templates compile to efficient Go code
- **Safe by default** — Auto-escapes output to prevent XSS
- **Flexible** — Works with any data structure
- **Standard library compatible** — Integrates seamlessly with `net/http`

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

	// Compile and render template
	html, err := gopug.RenderFile("hello.pug", data)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println(html)
}
```

## Syntax Overview

### Tags and Attributes

```pug
div.class-name#id-name
  p(class="dynamic" data-value="123") Content

a(href="/path") Link text
```

### Interpolation

```pug
p= variable
p #[strong= variable]
p!= html_content
```

### Loops and Conditionals

```pug
each item in items
  li= item

if condition
  p True branch
else
  p False branch
```

### Mixins (Reusable Components)

```pug
mixin button(text, class)
  button(class=class)= text

+button("Click me", "btn-primary")
```

## Development

Common commands (see `Makefile` for all options):

```sh
# Run tests
make test

# Build example
make build

# Format code
make fmt

# Run linter
make lint
```

## Examples

See `cmd/example` for a complete working example.

## Contributing

Contributions are welcome! Please:

- Open an issue to discuss major changes
- Write tests for new features
- Follow the existing code style
- Keep commits small and focused

## License

MIT License — see `LICENSE` for details.
