# Agent Notes — go-pug

This file is a **living document**. Each agent session that discovers a bug, fixes an issue, or learns a non-obvious behaviour should append to the relevant section. Do **not** rewrite history — only append.

See `INSTRUCTIONS.md` for the full project reference.

---

## How to Use This File

- **Before starting work:** read every section so you know what has already been learned.
- **After finishing work:** append new entries to the relevant section. Be specific — include file names, function names, and line ranges where relevant.
- Keep entries concise. A two-sentence explanation with a code snippet is better than a paragraph of vague description.

---

## Bug Fixes

_No bugs have been fixed yet — this section will be populated as issues are resolved._

<!--
Template for a new entry:

### YYYY-MM-DD — Short description of the bug

**Symptom:** What the wrong output / error was.
**Root cause:** Which file and function was at fault, and why.
**Fix:** What changed (file, function, ~line range).
**Test added:** `TestFunctionName` in `gopug_test.go`.
-->

---

## Non-Obvious Behaviours & Gotchas

### Expression Evaluator is Hand-Written, Not a Real Parser

`evaluateExpr` in `runtime.go` is a recursive function that scans the expression string character-by-character. It is **not** an AST-based expression parser. Operator precedence is implemented via priority-ordered searches through the token string (e.g., `findTernary`, `findBinaryOp`, `findSubtraction`, `findRightmostOp`). If you need to add a new operator or fix precedence, study those helper functions carefully before touching `evaluateExpr` itself.

### Indentation Tracking in the Lexer

The lexer emits `TokenIndent` and `TokenDedent` tokens to represent block nesting. The `depth` field on each token records the nesting level at the time of emission. The parser uses these to know when a child block starts and ends — it does **not** count spaces itself. If indentation-related bugs appear, check `scanIndentation` in `lexer.go` first.

### Template Inheritance Resolution Happens at Render Time

`extends` is resolved in `renderExtends` / `resolveExtendsAST` inside `runtime.go`, not at compile time. This means each `Render()` call re-reads and re-parses parent `.pug` files from disk unless you use `CompileFile` (which caches the child but not the parent chain). This is intentional — it keeps `Compile()` stateless.

### Block Override Merging Uses a Recursive Walk

`applyBlockOverrides` in `runtime.go` walks the parent AST and replaces `BlockNode` entries in-place using a `map[string][]*BlockNode` index built by `collectBlocks`. Append/prepend modes splice child nodes into the existing block body rather than replacing it. If you see double-content or missing-content bugs with `block append` / `block prepend`, check this function.

### Mixin Scope Is Fully Isolated

When a mixin is called, `renderMixinCall` in `runtime.go` pushes a **fresh scope** containing only the mixin's parameters. It does **not** inherit the caller's variables. Globals (from `opts.Globals`) are still visible because they are merged into `data` before rendering begins. If a mixin needs a caller variable, it must be passed as an explicit argument.

### `&attributes` Merging Is Additive for `class`

When `&attributes(obj)` is used on a tag, the `class` attribute is **merged** (space-joined) with any class shorthand or explicit `class=` already on the tag. All other attributes overwrite. This logic is in `renderTag` inside `runtime.go`.

### Filter Output Is Always Raw HTML

`renderFilter` writes filter output directly to the output buffer without HTML escaping. This is by design — filters are expected to return already-safe content (either raw HTML or pre-escaped text). Filters that return user-controlled plain text are the caller's responsibility to escape.

### `htmlEscapeText` Preserves Existing Entities

The custom `htmlEscapeText` function in `runtime.go` does **not** double-escape valid HTML entities. It uses `entityEnd` to detect sequences like `&amp;`, `&lt;`, `&#123;`, `&#x1F4A9;` and passes them through unchanged. Only bare `&` characters that are not the start of a valid entity are escaped to `&amp;`. Be careful when modifying this function — the entity-detection logic is subtle.

### Void Elements Never Get a Closing Tag

`isVoidElement` in `runtime.go` contains the full list of HTML void elements (`area`, `base`, `br`, `col`, `embed`, `hr`, `img`, `input`, `link`, `meta`, `param`, `source`, `track`, `wbr`). Any tag in this list is rendered as `<tag ...>` even if children were added to it in the Pug source (the children are silently dropped). If a new void element needs to be added, update this function.

### The `each` Loop Variable Is Scoped to the Loop Body

`renderEach` in `runtime.go` pushes a new scope containing the loop variable (and optional key variable) before rendering the body, then pops it. The loop variable is **not** visible after the loop ends. This matches Pug's JavaScript behaviour.

### Chained Filters Use `Subfilter` Field

A chained filter like `:outer:inner` is parsed into a `FilterNode` where `Name = "outer"` and `Subfilter = "inner"`. At render time, `renderFilter` applies the innermost filter first, then the outer. The options parsed from `(key=val)` are forwarded to the **outermost** filter only.

### `CompileFile` Cache Is Keyed by Absolute Path Only

The `compiledCache` (`sync.Map` in `gopug.go`) is keyed by the absolute file path. If a caller passes different `Options` on different calls to `CompileFile` with the same path, the cached `*Template` is returned with the new options shallow-copied in, but the AST is shared. This means you cannot cache two different compiled versions of the same file.

### Pretty-Print Does Not Affect Inline Tags

Tags that contain only inline text content (no child block) are rendered on a single line even in pretty-print mode. `prettyInline` in `runtime.go` detects this case by inspecting the children of a `TagNode`. If a tag has a child `TextNode` or `TextRunNode` (and nothing else), it is considered inline.

---

## Useful Debugging Patterns

### Inspect the Token Stream

Add a temporary print in a test to see what the lexer produces:

```pkg/gopug/gopug_test.go#L1-5
l := NewLexer(src)
tokens, err := l.Lex()
for _, t := range tokens {
    fmt.Println(t)
}
```

### Inspect the AST

After parsing, walk `ast.Children` and call `.String()` on each node to see the parse result:

```pkg/gopug/gopug_test.go#L1-7
l := NewLexer(src)
tokens, _ := l.Lex()
p := NewParser(tokens)
ast, _ := p.Parse()
for _, n := range ast.Children {
    fmt.Println(n.String())
}
```

### Run a Single Test Quickly

```
go test -run TestMyNewTest ./pkg/gopug/
```

### Check Which Tests Cover a Specific Function

Use coverage:

```
go test -coverprofile=coverage.out ./pkg/gopug/
go tool cover -func=coverage.out | grep renderFilter
```

---

## Test Patterns

- **Exact output:** use `assertEqual(t, got, want)` — full string comparison.
- **Partial output:** use `assertContains(t, got, fragment)` — good when surrounding whitespace or doctype is irrelevant to the fix.
- **Error expected:** call `Render(src, data, nil)` directly and assert `err != nil`.
- **With options:** call `Compile(src, opts)` then `tpl.Render(data)` to exercise `Pretty`, `Globals`, or `Filters`.
- **File-based (include/extends):** use `testdataPath(t, "filename.pug")` to get the absolute path to a fixture file, then call `RenderFile`.
