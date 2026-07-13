package gopug

import (
	"strings"
	"testing"
)

// A whole-quoted class value — whether it comes from a shorthand
// (`.badge`, stored by the parser as the whole-quoted literal `"badge"`) or
// from an explicit `class="..."` — is a fully static token list in Pug. Its
// words must never be re-evaluated as expressions, even when a token happens
// to share a name with an in-scope variable. Confirmed against pug.js 3.0.4:
// `pug.render('span.badge= badge', {badge: 'New'})` => `<span class="badge">New</span>`.

func TestClassShorthandCollisionWithBufferedVar(t *testing.T) {
	src := "span.badge= badge"

	cases := []struct {
		name string
		data map[string]interface{}
	}{
		{"defined", map[string]interface{}{"badge": "New"}},
		{"empty", map[string]interface{}{"badge": ""}},
		{"absent", map[string]interface{}{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderTest(t, src, tc.data)
			if !strings.Contains(out, `class="badge"`) {
				t.Errorf("expected literal class=%q regardless of variable value, got: %q", "badge", out)
			}
		})
	}
}

func TestClassQuotedLiteralCollisionWithVar(t *testing.T) {
	src := `span(class="badge")`
	out := renderTest(t, src, map[string]interface{}{"badge": "New"})
	assertContains(t, out, `class="badge"`)
	if strings.Contains(out, "New") {
		t.Errorf("variable value leaked into a whole-quoted class literal, got: %q", out)
	}
}

func TestClassMultiShorthandCollisionWithVar(t *testing.T) {
	src := "div.foo.bar= foo"
	out := renderTest(t, src, map[string]interface{}{"foo": "X"})
	assertContains(t, out, `class="foo bar"`)
	if !strings.Contains(out, ">X<") {
		t.Errorf("expected the buffered text to still render foo's value in the body, got: %q", out)
	}
}

// The collision is also a security concern: an attacker-controlled variable
// whose name collides with a static class token must never have its value
// land in the class attribute, escaped or otherwise.
func TestClassShorthandCollisionEscapingSafety(t *testing.T) {
	src := "span.badge= badge"
	out := renderTest(t, src, map[string]interface{}{"badge": `<script>"</script>`})

	if !strings.Contains(out, `class="badge"`) {
		t.Errorf("expected literal class=%q, got: %q", "badge", out)
	}
	if strings.Contains(out, "<script>") {
		t.Errorf("dangerous variable value must never reach the class attribute, got: %q", out)
	}
	// The buffered text content still renders the variable's value, escaped.
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected the buffered text content to render the variable's value escaped, got: %q", out)
	}
}

// Regression guards mirroring issue27's merged (non-whole-quoted) class
// paths, which must keep resolving dynamically and must NOT be affected by
// the whole-quoted-literal fix above.
func TestClassMergedFormsStillResolveDynamically(t *testing.T) {
	t.Run("bare merged with non-empty var", func(t *testing.T) {
		out := renderTest(t, "div.text-end(class=cls) hi", map[string]interface{}{"cls": "fw-bold"})
		assertContains(t, out, `class="text-end fw-bold"`)
	})

	t.Run("bare merged with empty var drops token", func(t *testing.T) {
		out := renderTest(t, "div.text-end(class=cls) hi", map[string]interface{}{"cls": ""})
		assertContains(t, out, `class="text-end"`)
		if strings.Contains(out, "cls") {
			t.Errorf("variable name leaked into output, got: %q", out)
		}
	})

	t.Run("quoted new value still merges statically", func(t *testing.T) {
		out := renderTest(t, `div.base(class="foo bar") hi`, nil)
		assertContains(t, out, `class="base foo bar"`)
	})
}
