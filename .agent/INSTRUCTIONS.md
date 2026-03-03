# Agent Instructions — go-pug

Welcome to the **go-pug** project — a runtime Pug → HTML template engine written in pure Go. The main build is complete. All Pug language features are implemented. Future work is **bug fixes, edge cases, and minor improvements only**.

> **Before doing any work, read this file in full, then wait for the user to prompt you.**

---

## Environment & Toolchain

| Item               | Value                                                         |
| ------------------ | ------------------------------------------------------------- |
| Go version         | **1.26** — assume it is available; do not use deprecated APIs |
| OS                 | **Windows** — default shell is `sh` (Git Bash / MSYS2 style)  |
| Module path        | `github.com/sinfulspartan/go-pug`                             |
| `go.mod` directive | `go 1.26` — do not downgrade                                  |

**Preferred APIs (Go 1.16+):**

- `os.ReadFile` / `os.WriteFile` — not `ioutil`
- `os.MkdirTemp` — not `ioutil.TempDir`
- `io.Discard` — not `ioutil.Discard`
- `slices`, `maps`, `cmp` packages (Go 1.21+) are fine to use

**Path conventions:** always use forward slashes when passing paths to tools.

---

## Project Layout

```
go-pug/
├── .agent/
│   ├── INSTRUCTIONS.md        ← this file (do not modify during bug-fix sessions)
│   └── NOTES.md               ← living notes: learned behaviours, gotchas, bug history
├── .github/workflows/ci.yml   ← GitHub Actions: test (ubuntu+windows), race, bench, build
├── cmd/
│   ├── main.go                ← demo HTTP server (port 8080); embeds views/*.pug + *.css
│   └── views/                 ← 34 numbered .pug demo files + demo.css + preview.css
├── pkg/gopug/                 ← ALL engine source code lives here
│   ├── token.go               ← TokenType constants, Token struct, Keywords map
│   ├── lexer.go               ← Lexer struct + Lex(); tokenises raw Pug source
│   ├── ast.go                 ← all AST node types (Node interface + ~25 concrete types)
│   ├── parser.go              ← Parser struct + Parse(); tokens → AST
│   ├── runtime.go             ← Runtime struct + Render(); AST → HTML string
│   ├── gopug.go               ← public API: Compile, CompileFile, Render, RenderFile, Options, FilterFunc
│   ├── gopug_test.go          ← ~360 unit/integration tests (table-driven)
│   ├── benchmark_test.go      ← compile/render/e2e/file benchmarks
│   └── testdata/              ← fixture files for include/extends tests
│       ├── layouts/           ← base.pug, page.pug, chain-*.pug, etc.
│       ├── nested/partial.pug
│       ├── header.pug, footer.pug, mixin-lib.pug
│       ├── article.txt        ← raw text include fixture
│       └── styles.css         ← raw CSS include fixture
├── scripts/bench2md/main.go   ← converts `go test -bench` output → MD/JSON/CSV
├── Makefile                   ← build, test, bench, cover, fmt, vet, lint targets
├── README.md                  ← full user-facing docs (syntax reference + API reference)
└── go.mod                     ← no external dependencies; pure stdlib
```

---

## Pipeline Overview

Understanding the data flow is essential before touching any file:

```
Pug source (string)
      │
      ▼
  Lexer  (lexer.go)
      │  []Token
      ▼
  Parser (parser.go)
      │  *DocumentNode (AST)
      ▼
  Runtime (runtime.go)
      │  string (HTML)
      ▼
  Caller
```

Each stage is **intentionally decoupled** — the lexer only knows tokens, the parser only knows tokens → nodes, the runtime only knows nodes → HTML. Do not introduce cross-stage dependencies.

---

## Key Source Files — What Each One Does

### `token.go`

- Defines `TokenType` (iota enum) and `Token` struct (`Type`, `Value`, `Line`, `Col`, `Depth`).
- `Keywords` map: pug keywords → their `TokenType` (used by the lexer to dispatch keyword tokens).
- `tokenTypeName()`: debug helper used in `Token.String()`.

### `lexer.go`

- `Lexer` struct holds `input string`, `pos`, `line`, `col`, `depth`, and the output `[]Token`.
- Entry point: `NewLexer(src).Lex() ([]Token, error)`.
- Works line-by-line via `scanLine()`. Indentation is tracked with `scanIndentation()` which emits `TokenIndent` / `TokenDedent`.
- Major scan functions and what they handle:
   - `scanTagOrKeyword` — tag names, all control-flow keywords, `extends`, `block`, `mixin`, `include`, `doctype`
   - `scanTagRest` — class/ID shorthand (`.foo`, `#bar`), inline text, block expansion (`:`), self-close (`/`)
   - `scanAttributes` / `scanAttributeValue` — `(key=val, ...)` attribute lists; handles quoted strings, bare booleans, unescaped `!=`
   - `scanComment` — `//` buffered and `//-` unbuffered comments, including multi-line indented bodies
   - `scanUnbufferedCode` — `-` lines (variable assignments, for loops)
   - `scanBufferedCode` — `=` buffered output
   - `scanExclamation` — `!=` unescaped output
   - `scanPipedText` — `|` piped text lines
   - `scanLiteralHTML` — lines starting with `<`
   - `scanMixinCall` — `+name(...)` mixin call lines
   - `scanFilter` — `:filtername` filter blocks and inline filters
   - `scanDotStart` — `.` block-text (indented body) vs `.class` shorthand
   - `scanHashStart` — `#id` shorthand
   - `scanBlockTextBody` — reads the indented body of a dot-block or filter
   - `emitTextWithInterpolations` — splits a text string into `TokenText` / `TokenInterpolation` / `TokenInterpolationUnescape` / `TokenTagInterpolationStart` / `TokenTagInterpolationEnd`

### `ast.go`

- Defines the `Node` interface (`node()`, `String()`) and all concrete node types.
- Node types and their key fields:

| Node                   | Key Fields                                                                                          |
| ---------------------- | --------------------------------------------------------------------------------------------------- |
| `DocumentNode`         | `Children []Node`                                                                                   |
| `TagNode`              | `Name`, `Attributes map[string]*AttributeValue`, `Children []Node`, `SelfClose bool`                |
| `AttributeValue`       | `Value string`, `Unescaped bool`, `Boolean bool`                                                    |
| `TextNode`             | `Content string`                                                                                    |
| `InterpolationNode`    | `Expression string`, `Unescaped bool`                                                               |
| `TagInterpolationNode` | `Tag *TagNode`                                                                                      |
| `TextRunNode`          | `Nodes []Node` — mixed text + interpolation sequence                                                |
| `CommentNode`          | `Content string`, `Buffered bool`                                                                   |
| `CodeNode`             | `Expression string`, `Type CodeType` (unbuffered/buffered/unescaped)                                |
| `ConditionalNode`      | `Condition`, `Consequent []Node`, `Alternate []Node`, `IsElseIf`, `IsUnless`                        |
| `EachNode`             | `Item`, `Key`, `Collection`, `Body []Node`, `ElseBody []Node`                                       |
| `WhileNode`            | `Condition`, `Body []Node`                                                                          |
| `CaseNode`             | `Expression`, `Cases []*WhenNode`, `Default []Node`                                                 |
| `WhenNode`             | `Expression`, `Body []Node`                                                                         |
| `MixinDeclNode`        | `Name`, `Parameters []string`, `DefaultValues map[string]string`, `RestParam string`, `Body []Node` |
| `MixinCallNode`        | `Name`, `Arguments []string`, `Attributes map[string]*AttributeValue`, `Block []Node`               |
| `BlockNode`            | `Name`, `Body []Node`, `Mode BlockMode` (replace/append/prepend)                                    |
| `ExtendsNode`          | `Path string`                                                                                       |
| `IncludeNode`          | `Path string`, `Filter string`                                                                      |
| `FilterNode`           | `Name`, `Content string`, `Options map[string]string`, `Subfilter string`                           |
| `DoctypeNode`          | `Value string`                                                                                      |
| `PipeNode`             | `Content string`                                                                                    |
| `BlockTextNode`        | `Content string`                                                                                    |
| `LiteralHTMLNode`      | `Content string`                                                                                    |
| `BlockExpansionNode`   | `Parent *TagNode`, `Child Node`                                                                     |

### `parser.go`

- `Parser` struct: `tokens []Token`, `pos int`, `cur Token`.
- Entry point: `NewParser(tokens).Parse() (*DocumentNode, error)`.
- `parseNode()` is the main dispatch switch — it reads the current token and delegates to the appropriate `parse*` function.
- Notable parsing logic:
   - `parseTag` — handles class/ID shorthand tokens, attribute parsing, block expansion (`:`), inline text, self-close, and indented child blocks
   - `parseConditional` / `parseConditionalWithCond` / `parseUnless` — if/else-if/else/unless chains
   - `parseEach` — `each val, key in expr` with optional `else` block
   - `parseCase` / `parseMixinDecl` / `parseMixinCall` — case-when, mixin declarations, mixin calls
   - `parseBlock` / `parseBlockModifier` / `parseExtends` — template inheritance machinery
   - `parseInclude` — handles `include path` and `include:filter path`
   - `parseFilter` — `:filtername(opts)` blocks and chained filters
   - `collectTextRun` — builds a `TextRunNode` from adjacent text/interpolation tokens
   - `splitMixinParams` — splits raw param string into name + optional default value

### `runtime.go`

- `Runtime` struct: `ast`, `data map[string]any`, `globals`, `out *strings.Builder`, `scope []map[string]any`, `mixins map[string]*MixinDeclNode`, `opts *Options`, `basedir string`, `includedPaths map[string]bool`, `prettyDepth int`.
- Entry point: `NewRuntimeWithOptions(ast, data, opts).Render() (string, error)`.
- `renderNode()` is the main dispatch switch over all node types.
- **Expression evaluator** (`evaluateExpr`): a hand-written recursive-descent evaluator. Handles:
   - Literals: strings (`"..."`, `'...'`), numbers, `true`/`false`, `null`/`nil`/`undefined`
   - Variable lookup, dot-notation field/key access, bracket index access
   - Arithmetic: `+`, `-`, `*`, `/`, `%`
   - Comparison: `==`, `!=`, `===`, `!==`, `<`, `>`, `<=`, `>=`
   - Logical: `&&`, `||`, `!`
   - Ternary: `cond ? a : b`
   - Inline arrays: `["a", "b"]`
   - Inline objects: `{key: val}`
   - String methods: `.toUpperCase()`, `.toLowerCase()`, `.trim()`, `.trimStart()`, `.trimEnd()`, `.slice()`, `.indexOf()`, `.includes()`, `.startsWith()`, `.endsWith()`, `.replace()`, `.repeat()`, `.split()`, `.join()`
   - `.length` property
- **Template inheritance** (`renderExtends`, `resolveExtendsAST`, `applyBlockOverrides`): resolves `extends` chains by reading and re-parsing parent files, then merging `block` overrides recursively.
- **Includes** (`renderInclude`): resolves relative and absolute paths, detects cycles, supports raw-file includes and `include:filter` syntax.
- **Mixins** (`renderMixinCall`, `renderMixinBlockSlot`): full scope isolation, default params, rest params (`...args`), block slot (`block` inside mixin body), `&attributes` forwarding.
- **Filters** (`renderFilter`, `lookupFilter`): looks up by name in `opts.Filters` first, then falls back to `opts.Globals`; supports chained filters (`name` → `Subfilter`).
- **Pretty-print** (`pretty`, `prettyNewline`, `prettyInline`): controlled by `opts.Pretty`; uses `prettyDepth` counter.
- **HTML escaping** (`htmlEscapeText`): escapes `<`, `>`, `"`, `&` (but passes through valid named/numeric HTML entities unchanged).

### `gopug.go` — Public API

- `Options` struct: `Basedir`, `Pretty`, `Globals map[string]any`, `Filters map[string]FilterFunc`
- `FilterFunc`: `func(text string, options map[string]string) (string, error)`
- `SimpleFilter(fn)`: adapter for old-style `func(string)(string,error)` filters
- `Compile(src, opts)` → `*Template`
- `CompileFile(path, opts)` → `*Template` (cached via `sync.Map`; invalidate with `ClearCache()`)
- `Render(src, data, opts)` → `string`
- `RenderFile(path, data, opts)` → `string`
- `(*Template).Render(data)` → `string`
- `(*Template).RenderToWriter(w, data)` → `error`

---

## Test Suite

All tests live in `pkg/gopug/gopug_test.go` (~3850 lines, ~360 tests). There are no golden files — assertions are inline `assertEqual` / `assertContains` calls.

**Helper functions:**

- `renderTest(t, src, data)` — calls `Render` with nil opts, fails on error
- `assertEqual(t, got, want)` — exact string match
- `assertContains(t, got, want)` — substring check

**Test fixture files** (used by include/extends tests):

- `testdata/header.pug`, `testdata/footer.pug` — simple partials
- `testdata/mixin-lib.pug` — mixin library for include tests
- `testdata/article.txt`, `testdata/styles.css` — raw file include fixtures
- `testdata/nested/partial.pug` — for relative-path resolution tests
- `testdata/layouts/` — base/page/chain/full-override layouts for extends tests

**Running tests:**

```
go test ./pkg/gopug/              # standard run
go test -v ./pkg/gopug/           # verbose
go test -run TestFoo ./pkg/gopug/ # single test
go test -race ./pkg/gopug/        # race detector
```

Or via Make: `make test`, `make test-v`, `make test-race`

---

## Known Limitations (from README)

| Area                                  | Detail                                                                                                                                      |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Unquoted attribute values with spaces | `class!=attributes.class href=href` is mis-lexed as a single token. Workaround: quote the value or use `&attributes`.                       |
| Ternary in `each` collection          | `each v in cond ? list : [fallback]` is not supported. Only a plain variable or an inline array literal works as the collection expression. |
| Filter output escaping                | Filter output is written raw. Filters returning plain text must escape it themselves if it may contain `<`, `>`, or `&`.                    |

---

## How to Find a Bug

1. **Reproduce it** — write the smallest possible failing test case in `gopug_test.go` using `renderTest` + `assertEqual`.
2. **Identify the stage** — is the output wrong HTML (runtime), a parse error (parser), or a lex error (lexer)?
   - Add a `fmt.Println` of the token stream or AST node to narrow it down.
   - Lexer tokens: `lexer.Lex()` returns `[]Token` directly — inspect them.
   - Parser AST: `parser.Parse()` returns `*DocumentNode` — call `.String()` on nodes.
3. **Check `NOTES.md`** — past agents document learned behaviours and recurring patterns there.
4. **Consult the Pug reference** — https://pugjs.org/api/getting-started.html for authoritative behaviour.

---

## Code Standards

- All public API types and functions must have Go doc comments.
- Lexer, parser, and runtime must remain decoupled — no cross-stage imports of internals.
- Prefer `fmt.Errorf("context: %w", err)` with file/line info where available.
- HTML escaping is **on by default**. Unescaped output (`!=`, `!{}`) must be explicitly opted into.
- Expression data is `map[string]any`; struct field access is via `reflect`.
- No external dependencies — keep `go.mod` dependency-free.
- Run `make fmt` (`gofmt -s`) before committing.
- Run `make test` before committing — all ~360 tests must pass.

---

## Git Workflow

- Commit after each meaningful fix with a short, descriptive message.
- Example: `"fix: lexer mis-tokenises unquoted attribute values containing spaces"`.
- Run `make fmt` then `make test` before every commit.

---

## Session End

Before finishing any session, update `.agent/NOTES.md`:

1. Add any bugs found and how they were fixed (file, function, what changed).
2. Add any non-obvious behaviours or edge cases discovered.
3. Note any test patterns that proved useful.

Do **not** modify this file (`INSTRUCTIONS.md`) — it is the stable reference. `NOTES.md` is where living knowledge accumulates.
