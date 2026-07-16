package gopug

import "testing"

// These tests close the pretty-mode blind spot for mixins: the family was
// exercised almost entirely in compact mode before this file existed. Every
// want string below was captured by rendering the equivalent template
// through real pug.js 3.0.4 (perf-compare/node_modules/pug) with
// {pretty: true}.

// TestPrettyMixinNestedBlockLevelBody pins pug.js 3.0.4's exact pretty-mode
// bytes for a mixin whose own body is several nested block-level tags: the
// expanded call site reproduces the mixin body's own indentation exactly as
// if it had been written inline at the call site.
func TestPrettyMixinNestedBlockLevelBody(t *testing.T) {
	src := "mixin card(title)\n  div.card\n    h2= title\n    p Body text\n+card('Hello')\n"
	want := "\n<div class=\"card\">\n  <h2>Hello</h2>\n  <p>Body text</p>\n</div>"
	assertEqual(t, prettyRender(t, src), want)
}

// TestPrettyMixinBlockDefaultContentSlot pins pug.js 3.0.4's exact
// pretty-mode bytes for the documented "if block / block / else default"
// pattern: one call site supplies its own block content, the other falls
// back to the mixin's own default — both indented correctly under the
// mixin's own wrapping tag.
func TestPrettyMixinBlockDefaultContentSlot(t *testing.T) {
	src := "mixin wrap(title)\n  section.wrap\n    h2= title\n    if block\n      block\n    else\n      p Default slot content\n+wrap('First')\n+wrap('Second')\n  p Custom slot content\n"
	want := "\n<section class=\"wrap\">\n  <h2>First</h2>\n  <p>Default slot content</p>\n</section>\n<section class=\"wrap\">\n  <h2>Second</h2>\n  <p>Custom slot content</p>\n</section>"
	assertEqual(t, prettyRender(t, src), want)
}

// TestPrettyMixinRestArgs pins pug.js 3.0.4's exact pretty-mode bytes for a
// mixin with a `...items` rest parameter iterated with `each` inside the
// mixin body.
func TestPrettyMixinRestArgs(t *testing.T) {
	src := "mixin list(...items)\n  ul\n    each item in items\n      li= item\n+list('x', 'y', 'z')\n"
	want := "\n<ul>\n  <li>x</li>\n  <li>y</li>\n  <li>z</li>\n</ul>"
	assertEqual(t, prettyRender(t, src), want)
}

// TestPrettyMixinAndAttributesSpread pins pug.js 3.0.4's exact pretty-mode
// bytes for a mixin call that spreads call-site attributes via
// `&attributes(attributes)`: the mixin's own tag (`a`) is inline-named, so
// pretty mode adds no leading or trailing newline around it.
func TestPrettyMixinAndAttributesSpread(t *testing.T) {
	src := "mixin link(href, name)\n  a(href=href)&attributes(attributes)= name\n+link('/foo', 'foo')(class='btn')\n"
	want := "<a class=\"btn\" href=\"/foo\">foo</a>"
	assertEqual(t, prettyRender(t, src), want)
}
