# Changelog

All notable changes to this project are documented here. This project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
