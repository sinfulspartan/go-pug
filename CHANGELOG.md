# Changelog

All notable changes to this project are documented here. This project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
