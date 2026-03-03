package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

func main() {
	fmt.Println("=== Go-Pug Template Engine Demo ===")
	fmt.Println()

	// -----------------------------------------------------------------------
	// 1. Basic tags, attributes, class/ID shorthand
	// -----------------------------------------------------------------------
	fmt.Println("--- 1. Basic tags ---")
	basic := `
doctype html
html
  head
    title My App
  body
    h1.title#main Hello, Go-Pug!
    p.lead Welcome to the template engine.
    a(href="https://pugjs.org") Pug Language Reference
`
	printRender("basic", basic, nil, nil)

	// -----------------------------------------------------------------------
	// 2. Variable interpolation and expressions
	// -----------------------------------------------------------------------
	fmt.Println("--- 2. Interpolation & expressions ---")
	interp := `
p Hello, #{name}!
p Age: #{age}
p Admin: #{isAdmin ? "yes" : "no"}
p Upper: #{name.toUpperCase()}
p Length: #{name.length}
`
	printRender("interp", interp, map[string]interface{}{
		"name":    "Alice",
		"age":     30,
		"isAdmin": true,
	}, nil)

	// -----------------------------------------------------------------------
	// 3. Control flow: if/else if/else, unless, each, while
	// -----------------------------------------------------------------------
	fmt.Println("--- 3. Control flow ---")
	control := `
- score = 85
if score >= 90
  p Grade: A
else if score >= 80
  p Grade: B
else if score >= 70
  p Grade: C
else
  p Grade: F

ul
  each fruit in fruits
    li= fruit

unless hidden
  p This content is visible.
`
	printRender("control", control, map[string]interface{}{
		"fruits": []string{"Apple", "Banana", "Cherry"},
		"hidden": false,
	}, nil)

	// -----------------------------------------------------------------------
	// 4. Mixins
	// -----------------------------------------------------------------------
	fmt.Println("--- 4. Mixins ---")
	mixins := `
mixin card(title, body)
  div.card
    h2= title
    p= body

+card("Welcome", "This is the welcome card.")
+card("About", "Go-Pug is a Pug template engine for Go.")
`
	printRender("mixins", mixins, nil, nil)

	// -----------------------------------------------------------------------
	// 5. Tag interpolation #[tag text]
	// -----------------------------------------------------------------------
	fmt.Println("--- 5. Tag interpolation ---")
	tagInterp := `
p Click #[a(href="/login") here] to log in.
p Use #[strong bold text] for emphasis.
p Visit #[a(href="https://example.com") example.com] for more.
`
	printRender("tagInterp", tagInterp, nil, nil)

	// -----------------------------------------------------------------------
	// 6. Filters
	// -----------------------------------------------------------------------
	fmt.Println("--- 6. Filters ---")
	filters := `
div.uppercase-box
  :uppercase
    hello from a filter block
p Inline:
  :wrap hello
`
	opts := &gopug.Options{
		Filters: map[string]func(string) (string, error){
			"uppercase": func(s string) (string, error) {
				return strings.ToUpper(s), nil
			},
			"wrap": func(s string) (string, error) {
				return "[" + strings.TrimSpace(s) + "]", nil
			},
		},
	}
	printRender("filters", filters, nil, opts)

	// -----------------------------------------------------------------------
	// 7. Pretty-print mode
	// -----------------------------------------------------------------------
	fmt.Println("--- 7. Pretty-print mode ---")
	prettyTpl := `
html
  head
    title Pretty Output
  body
    header
      h1 Hello
    main
      p First paragraph.
      p Second paragraph.
    footer
      p Footer text.
`
	prettyOpts := &gopug.Options{Pretty: true}
	out, err := gopug.Render(prettyTpl, nil, prettyOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	} else {
		fmt.Print(out)
	}
	fmt.Println()

	// -----------------------------------------------------------------------
	// 8. Method expressions
	// -----------------------------------------------------------------------
	fmt.Println("--- 8. Method expressions ---")
	methods := `
p Upper: #{name.toUpperCase()}
p Lower: #{name.toLowerCase()}
p Length: #{name.length}
p Trim: #{padded.trim()}
p Slice: #{name.slice(0, 3)}
p Replace: #{greeting.replace("World", "Go-Pug")}
p Starts: #{name.startsWith("Al")}
p Ends: #{name.endsWith("ce")}
`
	printRender("methods", methods, map[string]interface{}{
		"name":     "Alice",
		"padded":   "  hello  ",
		"greeting": "Hello, World!",
	}, nil)

	// -----------------------------------------------------------------------
	// 9. &attributes spread
	// -----------------------------------------------------------------------
	fmt.Println("--- 9. &attributes spread ---")
	attrs := `
button(type="button")&attributes(btnAttrs) Click me
`
	printRender("attrs", attrs, map[string]interface{}{
		"btnAttrs": map[string]interface{}{
			"class":    "btn btn-primary",
			"data-id":  "42",
			"disabled": "false",
		},
	}, nil)

	// -----------------------------------------------------------------------
	// 10. CompileFile cache demo (renders the same template twice)
	// -----------------------------------------------------------------------
	fmt.Println("--- 10. CompileFile cache ---")
	tmpPath := os.TempDir() + "/go-pug-demo.pug"
	_ = os.WriteFile(tmpPath, []byte("p Cached: #{msg}"), 0644)
	for i, msg := range []string{"first render", "second render (cached)"} {
		tpl, err := gopug.CompileFile(tmpPath, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "compile error: %v\n", err)
			continue
		}
		result, err := tpl.Render(map[string]interface{}{"msg": msg})
		if err != nil {
			fmt.Fprintf(os.Stderr, "render error: %v\n", err)
			continue
		}
		fmt.Printf("  [%d] %s\n", i+1, result)
	}
	gopug.ClearCache()
	fmt.Println("  Cache cleared.")
	fmt.Println()

	fmt.Println("=== Demo complete ===")
}

// printRender is a helper that compiles, renders, and prints a template.
func printRender(label, src string, data map[string]interface{}, opts *gopug.Options) {
	out, err := gopug.Render(src, data, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] render error: %v\n", label, err)
		return
	}
	fmt.Println(out)
}
