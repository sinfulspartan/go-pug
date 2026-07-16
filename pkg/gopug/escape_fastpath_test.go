package gopug

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// referenceHTMLEscapeAttr is a byte-for-byte copy of htmlEscapeAttr's
// pre-fast-path escaping loop, with no ContainsAny guard in front of it. It
// exists only so the differential tests below can prove the guarded version
// never changes a single output byte: htmlEscapeAttr(s) must equal
// referenceHTMLEscapeAttr(s) for every input, guarded or not.
func referenceHTMLEscapeAttr(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '<':
			b.WriteString("&lt;")
			i++
		case '>':
			b.WriteString("&gt;")
			i++
		case '"':
			b.WriteString("&quot;")
			i++
		case '&':
			if end := entityEnd(s, i); end > i {
				b.WriteString(s[i:end])
				i = end
			} else {
				b.WriteString("&amp;")
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// referenceHTMLEscapeText is a byte-for-byte copy of htmlEscapeText's
// pre-fast-path escaping loop, with no ContainsAny guard in front of it —
// the text-content counterpart to referenceHTMLEscapeAttr above.
func referenceHTMLEscapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '<':
			b.WriteString("&lt;")
			i++
		case '>':
			b.WriteString("&gt;")
			i++
		case '&':
			if end := entityEnd(s, i); end > i {
				b.WriteString(s[i:end])
				i = end
			} else {
				b.WriteString("&amp;")
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// escapeFastPathCases is the shared tricky-input table every differential
// test below runs each escaper against: bare special characters individually
// and mixed, entities that must be passed through unchanged (named, decimal,
// hex), a lookalike that is NOT a valid entity and so must still be escaped,
// an empty string, an all-clean string (the fast path's own target case),
// and strings that mix clean runs with special characters at the start,
// middle, and end (proving the guard's ContainsAny scan can't be fooled by
// where in the string the special character sits).
var escapeFastPathCases = []string{
	"",
	"hello world, nothing special here",
	"<",
	">",
	`"`,
	"'",
	"&",
	"<script>",
	`say "hi"`,
	"it's fine",
	"a<b>c&d\"e'f",
	"&amp;",
	"&lt;",
	"&gt;",
	"&quot;",
	"&#39;",
	"&#169;",
	"&#x1F4A9;",
	"&notanentity",
	"&amp",
	"&;",
	"& ",
	"tail &amp; end",
	"leading&amp;",
	"&amp;trailing",
	"multiple &amp; entities &lt; here &gt; too",
	"bare & then &amp; then bare &",
	"<b>&\"'</b>",
	"onclick=\"alert('x')\"",
	"日本語 with & bare ampersand",
	"emoji 🎉 & more",
	strings.Repeat("clean", 200),
	strings.Repeat("a", 500) + "<" + strings.Repeat("b", 500),
	"\x00\x01 control chars & more",
}

// TestEscapeFastPathHTMLEscapeAttrByteIdentical proves htmlEscapeAttr's
// ContainsAny(s, `<>"&`) fast-path guard never changes a single output byte
// versus the unguarded escaping loop, across every tricky case above: for
// each, the guard fires (returns s unchanged) only when none of <, >, ", or
// & are present, which is exactly when the loop below would also have left
// every byte untouched — so the two functions must always agree.
func TestEscapeFastPathHTMLEscapeAttrByteIdentical(t *testing.T) {
	for _, s := range escapeFastPathCases {
		got := htmlEscapeAttr(s)
		want := referenceHTMLEscapeAttr(s)
		if got != want {
			t.Errorf("htmlEscapeAttr(%q) = %q, reference (unguarded) = %q", s, got, want)
		}
	}
}

// TestEscapeFastPathHTMLEscapeTextByteIdentical is
// TestEscapeFastPathHTMLEscapeAttrByteIdentical's counterpart for
// htmlEscapeText's ContainsAny(s, "<>&") guard.
func TestEscapeFastPathHTMLEscapeTextByteIdentical(t *testing.T) {
	for _, s := range escapeFastPathCases {
		got := htmlEscapeText(s)
		want := referenceHTMLEscapeText(s)
		if got != want {
			t.Errorf("htmlEscapeText(%q) = %q, reference (unguarded) = %q", s, got, want)
		}
	}
}

// TestEscapeFastPathHTMLEscapeStdlibByteIdentical pins htmlEscapeStdlib as a
// pure pass-through to html.EscapeString: unlike htmlEscapeAttr/
// htmlEscapeText, it carries no ContainsAny pre-scan of its own (html.
// EscapeString already has its own allocation-free no-op fast path, so an
// extra scan here would only add overhead without saving an allocation).
// This test proves the two never diverge across every tricky case above.
func TestEscapeFastPathHTMLEscapeStdlibByteIdentical(t *testing.T) {
	for _, s := range escapeFastPathCases {
		got := htmlEscapeStdlib(s)
		want := html.EscapeString(s)
		if got != want {
			t.Errorf("htmlEscapeStdlib(%q) = %q, want %q (html.EscapeString directly)", s, got, want)
		}
	}
}

// TestEscapeFastPathExportedWrappersMatchUnexported proves EscapeAttr,
// EscapeText, and EscapeHTML — the exported wrappers codegen-generated code
// calls — are genuinely single-sourced to the interpreter's own
// htmlEscapeAttr/htmlEscapeText/htmlEscapeStdlib rather than a separate
// reimplementation that could silently drift (e.g. gain the guard without
// the interpreter, or vice versa).
func TestEscapeFastPathExportedWrappersMatchUnexported(t *testing.T) {
	for _, s := range escapeFastPathCases {
		if got, want := EscapeAttr(s), htmlEscapeAttr(s); got != want {
			t.Errorf("EscapeAttr(%q) = %q, htmlEscapeAttr = %q", s, got, want)
		}
		if got, want := EscapeText(s), htmlEscapeText(s); got != want {
			t.Errorf("EscapeText(%q) = %q, htmlEscapeText = %q", s, got, want)
		}
		if got, want := EscapeHTML(s), htmlEscapeStdlib(s); got != want {
			t.Errorf("EscapeHTML(%q) = %q, htmlEscapeStdlib = %q", s, got, want)
		}
	}
}

// TestEscapeFastPathNoOpProvenByGuardCondition asserts the core safety
// argument directly, rather than just its consequence: whenever each
// character set's ContainsAny check is false, the corresponding unguarded
// reference escaper must ALREADY be the identity function on that input.
// For htmlEscapeAttr/htmlEscapeText this is exactly why their ContainsAny
// guards are safe to keep; for html.EscapeString (what htmlEscapeStdlib
// wraps, unguarded) it is exactly why no extra guard is needed there in the
// first place — it already has this no-op fast path internally. This is
// checked over both the fixed case table and a wide swept range of
// single/double/triple-byte strings, so the character set can't be missing
// a transformed character without this test catching it.
func TestEscapeFastPathNoOpProvenByGuardCondition(t *testing.T) {
	checkAttr := func(s string) {
		if !strings.ContainsAny(s, `<>"&`) {
			if got := referenceHTMLEscapeAttr(s); got != s {
				t.Errorf("htmlEscapeAttr guard says %q is a no-op, but the unguarded escaper produced %q", s, got)
			}
		}
	}
	checkText := func(s string) {
		if !strings.ContainsAny(s, "<>&") {
			if got := referenceHTMLEscapeText(s); got != s {
				t.Errorf("htmlEscapeText guard says %q is a no-op, but the unguarded escaper produced %q", s, got)
			}
		}
	}
	checkStdlib := func(s string) {
		if !strings.ContainsAny(s, `'"&<>`) {
			if got := html.EscapeString(s); got != s {
				t.Errorf("html.EscapeString(%q) should be a no-op when it contains none of '\"&<>, but produced %q", s, got)
			}
		}
	}

	for _, s := range escapeFastPathCases {
		checkAttr(s)
		checkText(s)
		checkStdlib(s)
	}

	// Sweep every single byte and every ordered pair of bytes in the
	// printable ASCII range: this exhaustively covers the boundary between
	// "guard says no-op" and "escaper actually changes something" for every
	// character each escaper could possibly special-case, not just the ones
	// enumerated by hand above.
	for b := byte(0x20); b < 0x7f; b++ {
		s := string([]byte{b})
		checkAttr(s)
		checkText(s)
		checkStdlib(s)
		for b2 := byte(0x20); b2 < 0x7f; b2++ {
			pair := string([]byte{b, b2})
			checkAttr(pair)
			checkText(pair)
			checkStdlib(pair)
		}
	}
}

// TestEscapeFastPathFaultInjection proves the differential tests above would
// actually catch a real under-scan: an intentionally-wrong guard that omits
// one transformed character (here, "<") is shown to diverge from the real
// escaper on an input containing only that character, confirming the test
// methodology has teeth rather than vacuously passing.
func TestEscapeFastPathFaultInjection(t *testing.T) {
	brokenGuard := func(s string) string {
		if !strings.ContainsAny(s, `>"&`) { // "<" wrongly left out
			return s
		}
		return referenceHTMLEscapeAttr(s)
	}
	s := "<"
	broken := brokenGuard(s)
	correct := htmlEscapeAttr(s)
	if broken == correct {
		t.Fatalf("fault injection did not diverge: an under-scanning guard omitting '<' should skip escaping %q, but got %q same as the correct %q", s, broken, correct)
	}
	if broken != s {
		t.Fatalf("fault injection setup error: expected the broken guard to pass %q through unescaped, got %q", s, broken)
	}
	if correct == s {
		t.Fatalf("fault injection setup error: expected the correct escaper to actually escape %q, got %q unchanged", s, correct)
	}
}

// escapeFastPathCardListData/escapeFastPathProduct mirror
// benchmark/templates/card_list.pug's data shape exactly (see
// benchmark/data.go's product/cardListData): a prior profiling pass found
// that this template's render allocations are ~100% escaping on ALREADY-safe
// product names/prices, making it the headline case the fast path targets.
type escapeFastPathCardListData struct {
	PageTitle string
	Products  []escapeFastPathProduct
}

type escapeFastPathProduct struct {
	Name        string
	Description string
	Price       string
	InStock     bool
	Featured    bool
}

const escapeFastPathCardListTemplate = `doctype html
html
  head
    title= PageTitle
  body
    h1= PageTitle
    div.product-grid
      each product in Products
        div(class=(product.Featured ? "product-card product-card--featured" : "product-card"))
          h2.product-card__name= product.Name
          p.product-card__description= product.Description
          p.product-card__price #{product.Price}
          if product.InStock
            span.badge.badge--in-stock In stock
          else
            span.badge.badge--out-of-stock Out of stock
`

// escapeFastPathCardListStructSrc is escapeFastPathCardListData's (and
// escapeFastPathProduct's) field declarations, verbatim, spliced into the
// throwaway module TestEscapeFastPathCardListCodegenByteIdentical builds and
// runs the generated Go source in.
const escapeFastPathCardListStructSrc = `type escapeFastPathProduct struct {
	Name        string
	Description string
	Price       string
	InStock     bool
	Featured    bool
}

type escapeFastPathCardListData struct {
	PageTitle string
	Products  []escapeFastPathProduct
}
`

func escapeFastPathCardListDataToMap(d escapeFastPathCardListData) map[string]any {
	products := make([]any, len(d.Products))
	for i, p := range d.Products {
		products[i] = map[string]any{
			"Name":        p.Name,
			"Description": p.Description,
			"Price":       p.Price,
			"InStock":     p.InStock,
			"Featured":    p.Featured,
		}
	}
	return map[string]any{
		"PageTitle": d.PageTitle,
		"Products":  products,
	}
}

// buildAndRunEscapeFastPathGo writes generated (a GenerateGo result whose
// Config.PackageName is "main"), a copy of structSrc, and a main() that
// builds dataLiteral and writes funcName's output to stdout, into a
// throwaway module, then `go run`s it and returns the captured stdout. This
// is the build+run half of the codegen byte-identity proof required
// alongside the unit-level guard differentials above: it exercises the
// ACTUAL generated Go source (gopug.EscapeHTML/gopug.EscapeAttr calls and
// all), compiled and executed as a real binary, not merely evaluated
// in-process.
func buildAndRunEscapeFastPathGo(t *testing.T, generated []byte, structSrc, dataType, funcName, dataLiteral string) string {
	t.Helper()

	dir := t.TempDir()
	goMod := "module escapefastpathbuild\n\ngo 1.26\n" + repoModuleReplaceDirectives(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	genStr := string(generated)
	funcIdx := strings.Index(genStr, "\nfunc ")
	if funcIdx < 0 {
		t.Fatalf("generated source has no \"func \" to splice the struct declaration before:\n%s", genStr)
	}

	var src strings.Builder
	src.WriteString(genStr[:funcIdx])
	src.WriteString("\n\nimport \"os\"\n\n")
	src.WriteString(structSrc)
	src.WriteString(genStr[funcIdx:])
	src.WriteString("\nfunc main() {\n\td := ")
	src.WriteString(dataLiteral)
	fmt.Fprintf(&src, "\n\tif err := %s(os.Stdout, d); err != nil {\n\t\tpanic(err)\n\t}\n}\n", funcName)

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatalf("writing main.go: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run on generated code failed:\n%s\n--- source ---\n%s", out, src.String())
	}
	return string(out)
}

// TestEscapeFastPathCardListCodegenByteIdentical is the codegen half of the
// byte-identity proof: card_list.pug — the template previously identified as
// ~100% escaping allocations on already-safe product data — is generated,
// built as a real binary, run, and its output compared byte-for-byte to the
// interpreter's Render output for the exact same data, first with clean
// strings (the fast path's own target: every
// escape call in this render should now be a no-alloc passthrough) and then
// with escaping-sensitive strings mixed in (proving the fast path's guard
// still correctly falls through to real escaping when needed).
func TestEscapeFastPathCardListCodegenByteIdentical(t *testing.T) {
	ast, err := Parse(escapeFastPathCardListTemplate, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderCardListFastPath",
		DataType:        "escapeFastPathCardListData",
		DataReflectType: reflect.TypeOf(escapeFastPathCardListData{}),
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	tpl, err := Compile(escapeFastPathCardListTemplate, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	cases := []struct {
		name string
		data escapeFastPathCardListData
	}{
		{
			name: "all-clean strings (the fast path's own target case)",
			data: escapeFastPathCardListData{
				PageTitle: "Featured Products",
				Products: []escapeFastPathProduct{
					{Name: "Wireless Mouse", Description: "A comfortable ergonomic mouse", Price: "29.99", InStock: true, Featured: true},
					{Name: "Mechanical Keyboard", Description: "Clicky and satisfying", Price: "89.99", InStock: false, Featured: false},
					{Name: "USB-C Hub", Description: "Seven ports of convenience", Price: "19.99", InStock: true, Featured: false},
				},
			},
		},
		{
			name: "escaping-sensitive strings mixed in (fast path must still fall through correctly)",
			data: escapeFastPathCardListData{
				PageTitle: `Deals & <Steals>`,
				Products: []escapeFastPathProduct{
					{Name: `<script>alert("x")</script>`, Description: `it's "on sale" & marked down`, Price: `$19.99 & up`, InStock: true, Featured: true},
					{Name: "Clean Product", Description: "Nothing special here", Price: "9.99", InStock: false, Featured: false},
					{Name: `Entity passthrough &amp; test`, Description: `already &lt;escaped&gt;`, Price: "5.00", InStock: true, Featured: false},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := tpl.Render(escapeFastPathCardListDataToMap(tc.data))
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			var literal strings.Builder
			literal.WriteString("escapeFastPathCardListData{\n\tPageTitle: ")
			fmt.Fprintf(&literal, "%q,\n\tProducts: []escapeFastPathProduct{\n", tc.data.PageTitle)
			for _, p := range tc.data.Products {
				fmt.Fprintf(&literal, "\t\t{Name: %q, Description: %q, Price: %q, InStock: %t, Featured: %t},\n",
					p.Name, p.Description, p.Price, p.InStock, p.Featured)
			}
			literal.WriteString("\t},\n}")

			got := buildAndRunEscapeFastPathGo(t, generated, escapeFastPathCardListStructSrc, "escapeFastPathCardListData", "RenderCardListFastPath", literal.String())

			if got != want {
				t.Errorf("codegen output != interpreter output\n--- codegen ---\n%s\n--- interpreter ---\n%s", got, want)
			}
		})
	}
}

// escapeFastPathBenchClean is a representative already-safe interpolation
// value (a product name, the case the stdlib escape path is on the hot path
// for in interpolation-heavy templates): no escaping is actually needed, so
// this benchmark measures pure call/scan overhead rather than the cost of
// building escaped output.
const escapeFastPathBenchClean = "Wireless Mouse, ergonomic and quiet"

// BenchmarkHTMLEscapeStdlibClean pins htmlEscapeStdlib's cost on a clean,
// no-escaping-needed input to html.EscapeString's own cost on the same
// input: since htmlEscapeStdlib is a direct pass-through with no pre-scan of
// its own, the two must track closely (a regression here — htmlEscapeStdlib
// meaningfully slower than html.EscapeString alone — would mean a guard or
// other overhead crept back in front of the stdlib call).
func BenchmarkHTMLEscapeStdlibClean(b *testing.B) {
	s := escapeFastPathBenchClean
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = htmlEscapeStdlib(s)
	}
}

// BenchmarkHTMLEscapeStringClean is BenchmarkHTMLEscapeStdlibClean's
// baseline: html.EscapeString called directly, with no go-pug wrapper at
// all, on the same clean input.
func BenchmarkHTMLEscapeStringClean(b *testing.B) {
	s := escapeFastPathBenchClean
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = html.EscapeString(s)
	}
}
