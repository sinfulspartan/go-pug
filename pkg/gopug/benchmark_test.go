package gopug

import (
	"os"
	"testing"
)

// ─── Template sources used across multiple benchmarks ────────────────────────

// smallSrc is a minimal 2-tag template.
const smallSrc = `p Hello, #{name}!`

// mediumSrc is a realistic card component with attributes and conditionals.
const mediumSrc = `div.card(id=cardId)
  h2= title
  p= description
  if badge
    span.badge= badge
  a(href=url) Read more`

// largeSrc is a full-page template: doctype, nav, a loop over 20 items,
// a mixin, and a footer.  It exercises almost every major code-path.
// The mixin declaration is at the top level so collectMixins() picks it up
// before the render walk reaches the +item call site.
const largeSrc = `mixin item(name, price)
  li.item
    span.name= name
    span.price= price
doctype html
html(lang="en")
  head
    meta(charset="utf-8")
    title= pageTitle
  body
    header
      nav
        a(href="/") Home
        a(href="/about") About
        a(href="/contact") Contact
    main
      h1= heading
      p= intro
      ul.items
        each product in products
          +item(product.name, product.price)
      if showFootnote
        p.footnote= footnote
    footer
      p &copy; 2025 Go-Pug`

// largeData returns the data map used with largeSrc.
func largeData() map[string]interface{} {
	products := make([]interface{}, 20)
	for i := 0; i < 20; i++ {
		products[i] = map[string]interface{}{
			"name":  "Product",
			"price": "$9.99",
		}
	}
	return map[string]interface{}{
		"pageTitle":    "Benchmark Page",
		"heading":      "Welcome",
		"intro":        "This is the intro paragraph.",
		"products":     products,
		"showFootnote": true,
		"footnote":     "Prices subject to change.",
	}
}

// ─── Compile benchmarks ───────────────────────────────────────────────────────

// BenchmarkCompileSmall measures Lex + Parse for a tiny template.
func BenchmarkCompileSmall(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(smallSrc, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileMedium measures Lex + Parse for a medium-sized component.
func BenchmarkCompileMedium(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(mediumSrc, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileLarge measures Lex + Parse for a full-page template.
func BenchmarkCompileLarge(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(largeSrc, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Render benchmarks ────────────────────────────────────────────────────────

// BenchmarkRenderSmall measures rendering a pre-compiled tiny template.
func BenchmarkRenderSmall(b *testing.B) {
	tpl, err := Compile(smallSrc, nil)
	if err != nil {
		b.Fatal(err)
	}
	data := map[string]interface{}{"name": "World"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderMedium measures rendering a pre-compiled card component.
func BenchmarkRenderMedium(b *testing.B) {
	tpl, err := Compile(mediumSrc, nil)
	if err != nil {
		b.Fatal(err)
	}
	data := map[string]interface{}{
		"cardId":      "card-1",
		"title":       "Hello World",
		"description": "A short description of the card.",
		"badge":       "New",
		"url":         "/article/1",
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderLarge measures rendering a pre-compiled full-page template
// that exercises loops, mixins, conditionals, and doctype output.
func BenchmarkRenderLarge(b *testing.B) {
	tpl, err := Compile(largeSrc, nil)
	if err != nil {
		b.Fatal(err)
	}
	data := largeData()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRenderLargePretty is the same as BenchmarkRenderLarge but with
// pretty-print indentation enabled, which adds overhead per tag.
func BenchmarkRenderLargePretty(b *testing.B) {
	tpl, err := Compile(largeSrc, &Options{Pretty: true})
	if err != nil {
		b.Fatal(err)
	}
	data := largeData()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := tpl.Render(data); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── End-to-end benchmarks (Compile + Render together) ───────────────────────

// BenchmarkE2ESmall measures the full pipeline (compile + render) for the
// tiny template — representative of one-off usage.
func BenchmarkE2ESmall(b *testing.B) {
	data := map[string]interface{}{"name": "World"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Render(smallSrc, data, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkE2ELarge measures the full pipeline for the large template.
func BenchmarkE2ELarge(b *testing.B) {
	data := largeData()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Render(largeSrc, data, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── CompileFile / cache benchmarks ──────────────────────────────────────────

// BenchmarkCompileFileColdStart measures CompileFile when the file is NOT in
// the cache (cache is cleared before every iteration).
func BenchmarkCompileFileColdStart(b *testing.B) {
	dir := b.TempDir()
	path := dir + "/bench.pug"
	if err := os.WriteFile(path, []byte(largeSrc), 0644); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ClearCache()
		if _, err := CompileFile(path, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompileFileCached measures CompileFile when the template IS already
// in the cache — only a sync.Map lookup + shallow copy.
func BenchmarkCompileFileCached(b *testing.B) {
	dir := b.TempDir()
	path := dir + "/bench_cached.pug"
	if err := os.WriteFile(path, []byte(largeSrc), 0644); err != nil {
		b.Fatal(err)
	}
	// Warm the cache.
	if _, err := CompileFile(path, nil); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := CompileFile(path, nil); err != nil {
			b.Fatal(err)
		}
	}
	b.Cleanup(ClearCache)
}

// BenchmarkRenderFileSmall measures RenderFile (disk read + compile + render)
// for the small template.
func BenchmarkRenderFileSmall(b *testing.B) {
	dir := b.TempDir()
	path := dir + "/small.pug"
	if err := os.WriteFile(path, []byte(smallSrc), 0644); err != nil {
		b.Fatal(err)
	}
	data := map[string]interface{}{"name": "World"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := RenderFile(path, data, nil); err != nil {
			b.Fatal(err)
		}
	}
}
