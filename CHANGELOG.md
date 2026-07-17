# Changelog

All notable changes to this project are documented here. This project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## v0.4.2

### Fixed

- **Mixin call arguments are now type-preserved.** A slice, map, or struct
  passed as a mixin argument previously reached the mixin body as a
  stringified value, so iterating it with `each` inside the mixin rendered
  one blob of text instead of per-element output — e.g. `+list(items)`
  where `items` is a `[]string` rendered `each x in items` as a single line
  rather than one `<li>` per element. Mixin arguments are now evaluated
  through the same type-preserving path `each`'s own collection expression
  already used, so the mixin body receives the real Go value. String and
  number arguments render exactly as before.

### Performance

Rendering allocates far less and is meaningfully faster, with byte-identical
output throughout — measured with `go test -bench -benchmem -count=3`
against this release and against the previous tagged release on the same
machine:

- A large-template render (`BenchmarkRenderLarge`) now allocates **15
  times/op instead of 133** (~89% fewer) and **3,866 B/op instead of
  24,448** (~84% fewer bytes), rendering in ~19.2µs instead of ~24.5µs
  (~22% faster).
- A small render (`BenchmarkRenderSmall`) allocates **5 times/op instead of
  7** and **292 B/op instead of 1,348** (~78% fewer bytes), in ~379ns
  instead of ~572ns (~34% faster).
- A medium render (`BenchmarkRenderMedium`) allocates **3 times/op instead
  of 5** and **376 B/op instead of 1,432** (~74% fewer bytes), in ~980ns
  instead of ~1,181ns (~17% faster).
- A mixin-heavy render (`BenchmarkInterpretBenchMixin`) allocates **14
  times/op instead of 313** (~96% fewer) and **6,981 B/op instead of
  56,872** (~88% fewer bytes), in ~39.6µs instead of ~81.6µs (~51% faster).
- The full compile+render pipeline for a large template
  (`BenchmarkE2ELarge`) allocates **222 times/op instead of 343** (~35%
  fewer) and **20,598 B/op instead of 41,208** (~50% fewer bytes), in
  ~36.0µs instead of ~52.4µs (~31% faster).

The gains come from three mechanisms, all transparent to rendered output:
the output buffer is now pre-sized from the previous render's byte length
(an adaptive per-`Template` hint) instead of always starting from a small
fixed capacity; output buffers are pooled and reused across `Template.Render`
calls instead of allocating a fresh one every render; and `each`-loop and
mixin-call scope frames (the `map[string]any` holding loop/argument
variables) are recycled from a per-render free-list instead of being
freshly allocated on every iteration/call. None of this changes what gets
rendered: every template in the render-throughput benchmark corpus (see
[`benchmark/`](benchmark/)) is verified byte-identical across pug.js, the
interpreter, and codegen before being timed, on every run of this cycle.

### Added

- A separate, isolated go-pug vs [Joker/jade](https://github.com/Joker/jade)
  render-throughput comparison lives in `benchmark/vs-joker/`, with its own
  chart — a benchmark-only addition in its own Go module, so the root
  go-pug module stays dependency-free.

## v0.4.1

### Fixed

- A **data race in template inheritance**: merging a child template's `block`
  overrides into an extended layout mutated shared, compiled AST nodes in
  place, so rendering the same `extends`/`block` template concurrently from
  multiple goroutines could race under `-race` (output was still correct
  single-threaded, since identical inputs re-derived identical bytes, but the
  memory access itself was undefined behavior). The merge is now a pure
  function that copies every node it touches instead of writing into shared
  state.
- **Block expansion** (`tag: tag` shorthand) mutated the shared compiled tag
  node's children on every render instead of building a fresh local slice,
  which could corrupt output across repeated or concurrent renders of the
  same cached template. Fixed the same way — render from a fresh copy, never
  the shared original.

### Performance

Rendering is substantially faster and allocates far less, with byte-identical
output throughout:

- A large-template render now allocates **133 times/op instead of 701**
  (roughly 81% fewer allocations), from a round of allocation-reduction work:
  a guarded HTML-escape fast path that skips the allocation when nothing
  needs escaping, a `map[string]any` fast path for field lookups that skips
  reflection, a faster attribute-name sort with a compile-time cache for
  templates with no spread attributes, a compile-time cache for fully-static
  `class` attribute values, and skipping the attributes map allocation
  entirely for a mixin call with no attributes.
- Template composition is now dramatically faster in the interpreter: parsed
  `extends` layouts and `include` partials are cached by file path instead of
  being re-read and re-parsed from disk on every `Render()` call — for an
  `include` inside a loop, that was once per iteration. Measured on this
  release's benchmark corpus, `include` renders roughly **12x faster** and
  `extends` renders roughly **3x faster** than before the cache, with file
  I/O and parsing gone from the profile entirely.

### Added

- The render-throughput benchmark corpus gained its first template-inheritance
  and `include` coverage: `page_extends` (an `extends`/`block` layout with
  replace, prepend, and append overrides plus an each-loop item list) and
  `page_include` (`include` from inside an each loop), both verified
  byte-identical across pug.js, the interpreter, and codegen before being
  timed. See the refreshed 10-template table in the README's
  [Benchmarks](README.md#benchmarks) section.
- Pretty-mode golden test coverage for template composition, mixins, filters,
  and `case`/`when` — previously tested deeply only in compact mode, which
  had hidden a pretty-mode-only block-expansion layout bug (now fixed above).

## v0.4.0

### Added

- **Go source-code generation** (`GenerateGo`/`Config`) — compiles a Pug template
  directly into a standalone Go render function for a bounded but growing subset
  of the language: conditions and comparisons (including struct/pointer-path
  truthiness), ternaries, string/numeric/boolean interpolation, `each` over
  slices (and string/numeric array literals), `unless` and `case`/`when`,
  mixins (positional and default parameters, rest parameters, block content
  limited to markup and the callee's own parameter references, `&attributes`
  forwarding), spread attributes (`&attributes(map)`), dynamic and boolean HTML
  attributes, class objects and array/slice/map-valued class attributes,
  nil-safe dot-paths through pointer intermediates, `extends`/`block`/`include`
  resolved at generate time, and unbuffered numeric/string/bool locals with
  reassignment and compound operators. Every template in the differential test
  suite renders byte-identical output between the interpreter and the
  generated code; templates outside the supported subset — including dynamic
  `style=` objects — fall back to the interpreter rather than generating
  incorrect code.
- Six exported runtime helpers used by generated code and usable directly:
  `EscapeAttr`, `EscapeText`, `Truthy`, `CompareValues`, `WriteSpreadAttrs`,
  `WriteSpreadAttrsAny`.
- A public, reproducible three-way render-throughput benchmark suite under
  [`benchmark/`](benchmark/), comparing pug.js, the go-pug interpreter, and
  go-pug codegen across 8 templates, with committed results
  (`benchmark/results.json`) and a chart (`benchmark/chart.svg`). See the new
  results table in the README's [Benchmarks](README.md#benchmarks) section.

### Fixed

- **Pretty-print mode** (`Options.Pretty`) now matches pug.js 3.0.4: the
  indentation algorithm correctly separates a tag's own leading/closing newline
  (name-based "inline" classification) from its children's indentation and
  trailing newline (content-based "can inline" classification); `pre` and
  `textarea` subtrees preserve significant whitespace instead of being
  indented; and the inline-tag set matches pug-parser's list exactly (removing
  several block-level tags that were wrongly treated as inline, such as
  `button`, `label`, `select`, and `input`).
- A **shorthand class combined with an operator/concatenation `class=`
  expression** (`button.btn(class="btn-" + style)`) no longer drops the
  shorthand token — the shorthand and the expression's classes now both merge,
  matching pug.js.
- **HTML comment serialization** (`// text`) no longer pads or trims the
  comment body, and block comments join their lines with a newline, matching
  pug.js verbatim.
- The **doctype table** matches pug.js: `doctype plist` now emits the full
  Apple PLIST DTD instead of a bare tag.
- **Consecutive piped (`|`) text lines** now join with a newline instead of
  being concatenated directly, and a piped line following an inline tag renders
  as that tag's sibling rather than being absorbed into it, matching pug.js.

### Changed

- `doctype 5` **no longer aliases to `<!DOCTYPE html>`**. pug.js has no such
  shortcut and emits the literal `<!DOCTYPE 5>`; a template relying on the old
  alias for HTML5 output should use `doctype html` instead.

Escaping behavior is unchanged throughout this release — all output remains
HTML-escaped by default, in both the interpreter and generated code.

## v0.3.4

### Fixed

- Bare positional mixin-call arguments that contain an operator, ternary, or
  bracket index — `+item(a + b)`, `+card(c ? x : y)`, `+m(arr[0])` — are now passed
  as a **single** argument instead of being mis-split into several. Named attribute
  values already handled this; the bare positional path did not.
- Fully-parenthesized expressions of any nesting depth now resolve correctly:
  `((flag))` renders the value of `flag` (previously empty) and `((a ? b : c))`
  evaluates the ternary. Redundant parentheses around a whole expression are
  transparent, matching standard Pug.

### Changed

- **Rendering is substantially faster and allocates far less, with byte-identical
  output.** A representative full-page template renders ~4.4× faster (≈345µs →
  ≈78µs) with ~62% fewer allocations per render (1,980 → 762). The gains come from
  compiling `= expr` output nodes, mixin-call arguments, and trivial attribute/
  expression shapes into reusable closures at compile time; a scalar value-stringify
  fast path; an allocation-free variable lookup; and elimination of a per-tag map
  allocation. No template behavior changes.

## v0.3.3

### Fixed

- A **class shorthand combined with an empty dynamic `class=` variable** no longer
  leaks the variable's name into the rendered class list. `div.text-end(class=cls)`
  with `cls == ""` produced `<div class="text-end cls">` — the literal identifier
  `cls` leaked as a class token — while the plain `div(class=cls)` form (fixed in
  v0.2.3, [#18]) was already correct. Now the shorthand class survives and the empty
  variable contributes nothing: `<div class="text-end">`. This was a partial
  regression of [#18]; the `- var cls = ""` assignment form is fixed as well. ([#27])

[#27]: https://github.com/sinfulspartan/go-pug/issues/27

## v0.3.2

### Fixed

- Assigning a non-literal value to a variable in an unbuffered code block —
  `- var xs = data.Items` — now **preserves the value's type** (slice, map, or
  struct) instead of coercing it to a string. Previously the alias held the
  `fmt`-stringified form, so `xs.length` and `each … in xs` operated on that
  string rather than the original collection, even though the direct
  `each … in data.Items` worked — a divergence from reference Pug, where a `-`
  block is raw JavaScript that keeps the reference. Ternary right-hand sides
  (`- var xs = cond ? a : b`) and ternary collections (`each x in cond ? a : b`)
  are type-preserved as well. ([#26])

[#26]: https://github.com/sinfulspartan/go-pug/issues/26

## v0.3.1

### Fixed

- A tag whose **attribute list wraps across multiple lines**, followed by inline
  content on the closing `)` line — plain text, a buffered `= expr`, or an
  unescaped `!= expr` — now renders that content as the element's **child**
  instead of as a following sibling. Previously
  `button(⏎ type="button" ⏎) Actions` produced
  `<button ...></button>Actions` (label outside the control), while the
  single-line form rendered correctly, so the two diverged. This was a
  regression introduced by the v0.2.3 void-element fix (`#17`). ([#24])

[#24]: https://github.com/sinfulspartan/go-pug/issues/24

## v0.3.0

### Changed (breaking)

- **`include`/`extends` path resolution is now consistent at every nesting depth**
  and matches standard Pug/Jade semantics. A **relative** path resolves against the
  directory of the file doing the including; a **leading slash** resolves against
  `Basedir`.

  Previously, a relative `include`/`extends` in the **top-level render target**
  resolved against `Basedir`, while the same line in a **nested** included file
  resolved relative to that file's own directory. Top-level relative paths now
  resolve relative to the entry file, removing that asymmetry. ([#21])

  A partial can now use the *same* `include` line whether it is pulled in as a
  nested include or rendered directly as a top-level target.

### Added

- Failed `include`/`extends` resolutions now emit a migration hint when the same
  path *would* resolve against `Basedir` — e.g.
  `did you mean a leading-slash (Basedir-relative) path "/partial/x.pug"?`. The
  hint only appears when such a file actually exists, so genuine typos are not
  given misleading advice.

### Migration

Only affects projects whose **top-level render target does not sit at the
`Basedir` root** and which rely on top-level relative paths resolving against
`Basedir`. If your entry template lives directly in `Basedir`, no change is
needed.

For an affected top-level template, switch its `Basedir`-relative refs to a
leading slash:

```pug
//- before (resolved against Basedir when at the top level)
extends layout/base.pug
include partial/nav.pug

//- after (explicitly Basedir-relative)
extends /layout/base.pug
include /partial/nav.pug
```

Genuinely file-relative refs (same-directory siblings, e.g. `include _header.pug`)
need no change. If a path fails to resolve after upgrading, the error message will
suggest the leading-slash form when a `Basedir` candidate exists.

Templates rendered from a string via `Compile`/`Render` (no entry file) keep the
`Basedir`-relative fallback for top-level relative includes.

[#21]: https://github.com/sinfulspartan/go-pug/issues/21

## v0.2.3

- Fix: render output/text siblings after void elements (`br`/`img`/`hr`/…) instead
  of silently dropping them. ([#17])
- Fix: `class=` bound to a variable whose value is an empty string no longer leaks
  the variable's name as a class token. ([#18])

[#17]: https://github.com/sinfulspartan/go-pug/issues/17
[#18]: https://github.com/sinfulspartan/go-pug/issues/18
