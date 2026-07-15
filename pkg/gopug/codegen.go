package gopug

import (
	"fmt"
	"go/format"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// gopugImportPath is the module path generated code imports (aliased to its
// own package name, "gopug") when it needs a support function such as
// EscapeAttr. Generated files live in the caller's package, never in this
// one, so they always reach EscapeAttr through this qualified import.
const gopugImportPath = "github.com/sinfulspartan/go-pug/pkg/gopug"

// Config configures GenerateGo's output: the package the generated file
// belongs to, the name of the render function it defines, and the Go type
// of the data that function accepts.
type Config struct {
	// PackageName is the package clause of the generated file.
	PackageName string
	// FuncName is the generated function's name, e.g. "RenderHome".
	FuncName string
	// DataType is the Go type of the data parameter, e.g. "HomeData" or
	// "*HomeData". It is emitted verbatim into the function signature.
	DataType string
	// DataReflectType is the reflect.Type of the data struct DataType names
	// (e.g. reflect.TypeOf(HomeData{})). When non-nil the generator is
	// type-aware: it walks this type through field dot-paths and each-loop
	// element types to learn each expression's Go type, and emits per-type
	// stringify and truthiness that matches the interpreter's semantics
	// (Runtime.lookupAndStringify, Runtime.isTruthy) exactly. When nil, the
	// generator falls back to the string-assuming behavior of the untyped
	// codegen skeleton, unchanged.
	DataReflectType reflect.Type
}

// GenerateGo translates ast into a gofmt-ed Go source file defining
//
//	func <FuncName>(w io.Writer, d <DataType>) error
//
// which writes the same HTML byte sequence the interpreter (Template.Render)
// would produce for equivalent data, for the grammar subset this increment
// supports: doctype, nested tags including void elements, static attributes
// and class/id shorthand, plain text, #{field-or-dot-path} interpolation,
// one level of `each item in <slice field>`, and `if <field>` with an
// optional `else`.
//
// When cfg.DataReflectType is set, GenerateGo is type-aware: it resolves
// every field expression's Go type and emits per-type stringify for
// interpolation (string, bool, every sized int/uint kind, and float64 — see
// genInterpolation) and per-type truthiness for conditions (bool and numeric
// kinds native — bare, and compared against zero, respectively — a string
// kind routed through the exported gopug.Truthy — see genConditional),
// matching the interpreter's Runtime.lookupAndStringify/isTruthy exactly. A
// condition may also use a numeric comparison, a string-equality comparison,
// a `.length` operand, or the `&&`/`||`/`!` combinators over any of those
// (see genCondition, which emits native Go `&&`/`||`/`!` — provably
// equivalent to the interpreter's own value-returning combinators once
// isTruthy is applied) — but only for the bounded-agreement subset provably
// byte-identical to the interpreter's compareValues/isTruthy; a top-level
// ternary, arithmetic, and every other operand combination still return an
// error instead. It also emits a runtime write for a
// dynamic attribute value on a non-"class" attribute — built the same way
// interpolation is, by genValueExpr (a bare field/dot-path, a `+` concat, or
// a backtick template literal), then always escaped through the exported
// EscapeAttr (see genAttributes) — and a conditional bare write for a
// bool-typed field on an HTML boolean-attribute name (present iff true,
// matching pug's omit-on-false; that path stays bare-field-only, not routed
// through genValueExpr). genValueExpr also supports a top-level ternary
// (`cond ? a : b`, emitted as an immediately-invoked function literal — see
// genTernaryValueExpr — reusing genCondition for the condition and
// recursing for each branch) and, in the interpreter's own precedence order,
// value-context `||`/`&&`/`!`/comparison: `||` and `&&` return the operand
// VALUE (the classic `name || "anon"` default-value idiom), short-circuited
// exactly like the interpreter — a fallible right operand (e.g. a division by
// zero) is never evaluated, and never errors, when it isn't needed (see
// genOrValueExpr/genAndValueExpr); a leading `!` returns "true"/"false" via
// the exported gopug.Not; a comparison delegates to genCondition and wraps
// its bool result in strconv.FormatBool. genValueExpr also supports a
// top-level index expression (`arr[i]`, a total IIFE over native Go
// slice/map indexing that collapses out-of-range/absent to "" exactly like
// Runtime's indexValue, and stringifies a present result with
// fmt.Sprintf("%v", …) — the interpreter's own index-path stringify, which
// is intentionally NOT the scalar-restricted genScalarStringify, so an
// indexed element's type is otherwise unrestricted — see genIndexValueExpr)
// and value-context `.length`/`.length()` (strconv.Itoa of the same
// type-directed len()/RuneCountInString logic genCondition's `.length`
// truthiness already uses — see genLengthValueExpr). Any value genValueExpr
// still can't build (a non-string-keyed map index, an index-then-dot
// receiver, most other method calls, an array/object literal, …), or a
// non-scalar field type (struct, slice, map, pointer, interface, float32,
// …) reached outside of index/`.length`, is out of scope and returns an
// error rather than guessing. An unbuffered `- var x = <rhs>` assignment
// (typed mode only) is also supported for a narrow, separately-proven RHS
// grammar — a string literal, a template literal, string concatenation, a
// ternary/`||`/`&&` over string operands, or a string-typed field/dot-path —
// emitted as a Go string local later references resolve to (see
// genUnbufferedAssign/genAssignRHS for the bounded-agreement proof this rests
// on, which is narrower than genValueExpr's own grammar on purpose). When
// DataReflectType is nil, every field is assumed to be a string (the
// original untyped skeleton behavior), and only static/bare attributes and
// bare-field conditions are supported, unchanged.
//
// GenerateGo assumes complete, well-typed data and does no nil-guarding —
// like Pug itself, and unlike the interpreter's lenient missing-value
// handling. Any node or expression shape outside the supported subset
// (includes/extends, dynamic class/style attributes, &attributes spreads,
// operators, method calls, unless/case, unescaped output, comments, …)
// returns a descriptive error instead of silently emitting something
// incorrect; those shapes are later increments.
//
// A mixin is supported as of the first mixin increment, for the narrow
// positional-string-parameter subset described here — matching the
// interpreter's own mixin semantics exactly (see Runtime.renderMixinCall),
// which fully ISOLATES a mixin's body: it sees only its own parameters, never
// the caller's data or locals. Each top-level MixinDeclNode is emitted as its
// own top-level Go helper function — `func <sanitized name>(w io.Writer, <one
// string param per Pug parameter>) error` — generated in a PARAM-ONLY scope
// (see genMixinFunc): a reference to one of the mixin's own parameters
// resolves to its Go argument, exactly like a bare string field elsewhere in
// this file, but a reference to anything else — a top-level data field, a
// caller's `- var` local, the special `attributes`/`block` identifiers — is
// FAIL-CLOSED: GenerateGo returns an error rather than emit "" the way the
// interpreter's own isolation would (a lookup miss). This keeps the whole
// feature within the bounded-agreement contract (byte-identical output, or a
// clean compile-time error — never a silent divergence) while still covering
// the common case, a mixin parameterized entirely by its own arguments. Each
// MixinCallNode is emitted as a call to that helper: its arguments are built
// by genValueExpr in the CALLER's own (data-visible) scope, exactly
// len(decl.Parameters) of them are passed — a missing trailing argument
// becomes the literal "" (matching the interpreter's own missing-arg
// default), and an extra call argument beyond the parameter count is simply
// ignored (no rest-parameter support yet) — mirroring
// Runtime.renderMixinCall's own arg-binding loop exactly. A default
// parameter value, a rest parameter, `attributes`/`&attributes` (in a body or
// forwarded at a call site), a nested mixin call (one mixin's body calling
// another), and a nil Config.DataReflectType (mixins need type information to
// classify call arguments) are all later increments and return a distinct
// error instead of guessing.
//
// A mixin's `block` slot — the caller's own indented content passed to a
// call — is supported for the STATIC subset only (see genMixinFunc and
// genMixinCall): when a decl's body contains a `block` node anywhere,
// reachable through the same nesting genNode itself walks, the generated
// helper gains one extra nilable `func(io.Writer) error` parameter, and a
// call passing block content generates that content as a closure IN A FAIL-
// CLOSED, EMPTY SCOPE — matching Runtime.renderMixinBlockSlot's own behavior
// (the block content renders while the CALLEE's own param scope is active,
// never the caller's) without needing to model that scope: pure static
// markup generates fine, since it performs no identifier lookup either way,
// while ANY identifier reference (a data field, a caller local, or the
// callee's own parameter — codegen cannot yet tell which), a nested mixin
// call, and any unbuffered code statement (even a literal-only `- var`
// local, self-contained and provably safe, but a distinct, untested claim
// this increment deliberately does not make) are all refused with a clean
// error instead of guessed at. A call passing block content to a decl
// with NO `block` slot silently discards that content, matching the
// interpreter's own silent-discard behavior exactly (Runtime.renderMixinCall
// sets callerBlock regardless; it is simply never read if the body has no
// slot to read it from).
func GenerateGo(ast *DocumentNode, cfg Config) ([]byte, error) {
	if cfg.PackageName == "" {
		return nil, fmt.Errorf("codegen: Config.PackageName is required")
	}
	if cfg.FuncName == "" {
		return nil, fmt.Errorf("codegen: Config.FuncName is required")
	}
	if cfg.DataType == "" {
		return nil, fmt.Errorf("codegen: Config.DataType is required")
	}

	g := &generator{rootType: cfg.DataReflectType}

	mixinDecls := map[string]*MixinDeclNode{}
	for _, child := range ast.Children {
		if m, ok := child.(*MixinDeclNode); ok {
			mixinDecls[m.Name] = m
		}
	}

	var mixinFuncsSrc []string
	if len(mixinDecls) > 0 {
		if cfg.DataReflectType == nil {
			return nil, fmt.Errorf("codegen: unsupported mixin in codegen (Config.DataReflectType is required to classify a mixin call's arguments; type-blind mode is not supported for mixins in this increment)")
		}

		g.mixinDecls = mixinDecls
		g.mixinFuncNames = make(map[string]string, len(mixinDecls))
		g.mixinHasSlot = make(map[string]bool, len(mixinDecls))
		g.mixinAttrForward = make(map[string]bool, len(mixinDecls))
		for name, decl := range mixinDecls {
			g.mixinHasSlot[name] = mixinBodyHasBlockSlot(decl.Body)
			g.mixinAttrForward[name] = mixinBodyUsesAttributesForward(decl.Body)
		}

		names := make([]string, 0, len(mixinDecls))
		for name := range mixinDecls {
			names = append(names, name)
		}
		slices.Sort(names)

		used := map[string]bool{cfg.FuncName: true}
		for _, name := range names {
			g.mixinFuncNames[name] = uniqueGoName("pugMixin_"+sanitizeGoIdent(name), used)
		}

		for _, name := range names {
			if g.mixinAttrForward[name] {
				// Generated per call site instead (see genMixinCallAttrForward
				// and mixinAttrForward's own doc comment) — no shared helper
				// function exists for this mixin at all.
				continue
			}
			src, err := g.genMixinFunc(g.mixinFuncNames[name], mixinDecls[name])
			if err != nil {
				return nil, err
			}
			mixinFuncsSrc = append(mixinFuncsSrc, src)
		}
	}

	for _, child := range ast.Children {
		if err := g.genNode(child); err != nil {
			return nil, err
		}
	}
	g.flushStatic()

	var src strings.Builder
	fmt.Fprintf(&src, "package %s\n\n", cfg.PackageName)
	src.WriteString("import (\n")
	if g.needsGopug {
		fmt.Fprintf(&src, "\t%q\n", gopugImportPath)
	}
	if g.needsFmt {
		src.WriteString("\t\"fmt\"\n")
	}
	if g.needsHTML {
		src.WriteString("\t\"html\"\n")
	}
	src.WriteString("\t\"io\"\n")
	if g.needsStrconv {
		src.WriteString("\t\"strconv\"\n")
	}
	if g.needsSort {
		src.WriteString("\t\"sort\"\n")
	}
	if g.needsStrings {
		src.WriteString("\t\"strings\"\n")
	}
	if g.needsUtf8 {
		src.WriteString("\t\"unicode/utf8\"\n")
	}
	src.WriteString(")\n\n")
	fmt.Fprintf(&src, "func %s(w io.Writer, d %s) error {\n", cfg.FuncName, cfg.DataType)
	src.WriteString(g.body.String())
	src.WriteString("\treturn nil\n}\n")

	for _, fnSrc := range mixinFuncsSrc {
		src.WriteString("\n")
		src.WriteString(fnSrc)
	}

	formatted, err := format.Source([]byte(src.String()))
	if err != nil {
		return nil, fmt.Errorf("codegen: generated source failed to gofmt (this is a generator bug, not a template error): %w", err)
	}
	return formatted, nil
}

// generator walks a *DocumentNode's children and accumulates the Go source
// of a render function's body. Static output (literal HTML the template
// contributes regardless of data) is buffered in static and flushed into a
// single io.WriteString call whenever a dynamic construct (an interpolation,
// each, or if) needs to emit code in between, so adjacent literal chunks
// don't turn into a write call per AST node.
type generator struct {
	body   strings.Builder
	static strings.Builder
	// scope holds each-loop item variables AND `- var` locals currently in
	// scope, innermost last, so a bare identifier or dot-path whose first
	// segment matches one of them resolves to the Go loop variable/local
	// directly instead of being treated as a field of d.
	scope []scopeVar
	// leakedVarNames records the Pug name of every `- var` local a
	// scopeRestore call has ever popped out of scope (see scopeRestore's own
	// doc comment for why this is necessary: Runtime's real scopeStack does
	// not frame a scope at several of the boundaries codegen's g.scope does,
	// so a popped `- var` name can still be live in the interpreter).
	// resolveFieldExpr consults this, after a scope-lookup miss and before
	// ever falling through to struct-field resolution, to refuse a reference
	// that could silently disagree with the interpreter instead of guessing.
	// Once a name is recorded it stays recorded for the rest of code
	// generation (deliberately global, not un-recorded when a same-named
	// local later goes out of scope again elsewhere) — the safe, simple
	// choice, since generation is a single one-shot pass with no use for
	// "forgetting" a name was ever unsafe.
	leakedVarNames map[string]bool
	// rootType is the reflect.Type of the data struct (Config.DataReflectType).
	// When nil, the generator is in type-blind, string-assuming mode; when
	// non-nil, resolveFieldExpr walks it (and each scope entry's typ) to
	// resolve every field expression's Go type.
	rootType reflect.Type
	// needsHTML/needsStrconv/needsGopug track whether the generated body
	// actually calls html.EscapeString/strconv.*/gopug.EscapeAttr anywhere,
	// so GenerateGo only imports those packages when they are used (an
	// unused import fails to compile).
	needsHTML    bool
	needsStrconv bool
	needsGopug   bool
	needsUtf8    bool
	// needsFmt tracks whether the generated body calls fmt.Sprintf directly
	// (the value-context index accessor stringifies its result with
	// fmt.Sprintf("%v", …), matching Runtime's own index path exactly — the
	// scalar-restricted genScalarStringify path never needs it), so
	// GenerateGo only imports "fmt" when it is actually used.
	needsFmt bool
	// needsStrings tracks whether the generated body calls a strings.*
	// function directly (the trivial string methods — toUpperCase et al. —
	// emit the stdlib call inline rather than through a gopug helper), so
	// GenerateGo only imports "strings" when it is actually used.
	needsStrings bool
	// needsSort tracks whether the generated body calls sort.Strings directly
	// (only a map-valued class attribute does this, since the active class
	// keys are unknown until runtime and must be sorted the same way
	// Runtime.renderTag's own reflect.Map branch does), so GenerateGo only
	// imports "sort" when it is actually used.
	needsSort bool
	// tmpCounter is the next unused index nextTmp will hand out, so every
	// fallible value-expression extracted at a write site (genInterpolation,
	// genCode, genAttributes) gets its own uniquely named __vN/__errN locals
	// within the generated function body, even when several such extractions
	// appear in the same function.
	tmpCounter int
	// mixinDecls holds every top-level mixin declaration GenerateGo collected
	// before generating any node, keyed by its Pug name, so a MixinCallNode
	// encountered anywhere during generation (in the main render function or,
	// once nested calls are supported, inside another mixin's own body) can
	// look up its parameter count. nil when the template declares no mixin.
	mixinDecls map[string]*MixinDeclNode
	// mixinFuncNames maps a mixin's Pug name to the sanitized, collision-free
	// Go identifier genMixinFunc emitted its helper function under (see
	// GenerateGo's name-assignment pass), so genMixinCall knows which
	// top-level function to call. nil when the template declares no mixin.
	mixinFuncNames map[string]string
	// mixinHasSlot maps a mixin's Pug name to whether its decl body contains a
	// `block` node anywhere (see mixinBodyHasBlockSlot), computed once by
	// GenerateGo's collection pass so both genMixinFunc (whether to add the
	// helper's block-callback parameter) and genMixinCall (whether to build a
	// closure or pass nil for that parameter, or silently discard block
	// content passed to a slotless mixin) agree on the same answer for a
	// given mixin. nil when the template declares no mixin.
	mixinHasSlot map[string]bool
	// mixinAttrForward maps a mixin's Pug name to whether its decl body
	// contains a tag with `&attributes` anywhere (see
	// mixinBodyUsesAttributesForward), computed once by GenerateGo's
	// collection pass. A mixin flagged here is NOT generated as a shared
	// top-level helper function the way every other mixin is (see
	// genMixinFunc) — GenerateGo's collection pass skips it entirely —
	// because the tag's actual rendered attributes depend on the CALL
	// site's own attributes (`+foo(class="x")`), which differ from one call
	// to the next, so no single shared function body could represent every
	// call. Instead genMixinCall detects this flag and generates the whole
	// mixin body freshly, INLINE, at each call site (genMixinCallAttrForward),
	// with that call's own (required-static) attributes merged into the
	// `&attributes` tag at GENERATE TIME — the same generate-time merge this
	// slice's call-attr staticness requirement makes possible for every other
	// part of the body too, so nothing here needs a runtime merge/sort/render
	// helper at all. nil when the template declares no mixin.
	mixinAttrForward map[string]bool
	// inAttrForwardBody is true only while genMixinCallAttrForward is
	// generating a &attributes-forwarding mixin's body, INLINE, for one
	// specific call site — so genTag's own `&attributes` handling knows it is
	// safe to look at attrForwardCallAttrs (that call's own static
	// attributes) rather than defer, the same way every other `&attributes`
	// tag (reached outside this mechanism) still does via genAttributes's own
	// pre-existing, unperturbed check.
	inAttrForwardBody bool
	// attrForwardCallAttrs holds the CURRENT call site's own static
	// call-attributes (`+foo(class="x", target="_blank")`), each already
	// reduced to the exact string Runtime.renderMixinCall's own attribute map
	// would hold (a bare attribute becomes the literal string "true"; a
	// quoted-string or unquoted true/false-keyword attribute value becomes
	// its dequoted content) — valid only while inAttrForwardBody is true, and
	// consulted by genTag's `&attributes` handling to reproduce
	// Runtime.renderTag's own spread-merge switch (class → append;
	// "true" → bare; "false" → delete; anything else → quoted) at GENERATE
	// TIME instead of at render time.
	attrForwardCallAttrs map[string]string
	// paramOnlyScope is true while genMixinFunc is generating a mixin body's
	// statements, AND while genMixinBlockClosure is generating a call site's
	// block-content closure. It makes resolveFieldExpr FAIL-CLOSED the moment
	// an identifier misses the scope stack (empty, in the closure case; the
	// mixin's own parameters, in the mixin-body case) instead of falling
	// through to struct-field resolution against d — reproducing
	// Runtime.renderMixinCall's own scope isolation (a mixin body sees only
	// its parameters; block content is rendered against that SAME isolated
	// scope, never the caller's — see genMixinBlockClosure's own doc comment)
	// as a clean GenerateGo error instead of a silent, potentially wrong "".
	paramOnlyScope bool
	// insideMixinBody is true only while genMixinFunc is generating a mixin
	// body's statements, so genMixinCall can reject a nested mixin call (one
	// mixin's body calling another) with a distinct, clean error rather than
	// attempt to generate it — a later increment's job.
	insideMixinBody bool
	// insideBlockClosure is true only while genMixinBlockClosure is
	// generating a call site's block-content closure, so genMixinCall can
	// reject a nested mixin call found there (`+other(...)` written as part
	// of the content passed to `+outer`'s block slot) with its own distinct
	// error, separate from insideMixinBody's — a later increment's job.
	insideBlockClosure bool
	// blockParamName is the Go parameter name genMixinFunc chose for the
	// current mixin's block-callback parameter ("pugBlock") while it is
	// generating that mixin's body — empty when the current decl has no
	// `block` slot, or whenever no mixin body is being generated at all
	// (including while genMixinBlockClosure builds a call site's own block
	// closure, which is never itself inside a slot-bearing mixin body). A
	// `*BlockNode` reached by genNode invokes this parameter when non-empty
	// and refuses (as an unsupported node) otherwise.
	blockParamName string
	// inRawTextElement mirrors Runtime.renderTag's own bracketed flag
	// (runtime.go, set/restored around a raw-text element's children —
	// script/style, see isRawTextElement): true only while genNode is
	// generating the children of such a tag, so genBlockText knows whether a
	// dot-block text's own text and #{} interpolations must be written raw
	// (no HTML-entity escaping) instead of through the normal escaped path.
	// Consumed only by genBlockText this slice.
	inRawTextElement bool
}

// nextTmp returns a fresh, monotonically increasing index for naming a
// fallible value-expression's extracted result locals (see
// genFallibleExtraction), unique within the current generated function body.
func (g *generator) nextTmp() int {
	n := g.tmpCounter
	g.tmpCounter++
	return n
}

// genFallibleExtraction flushes any pending static text and emits the
// statement pair that extracts a fallible value expression's (string, error)
// result — goExpr, a Go expression genValueExpr has already built (e.g.
// "gopug.Div(a, b)") — into two freshly named locals, returning early from
// the generated function with the interpreter's own error the moment it is
// non-nil (matching Runtime.evaluateExpr's own error propagation exactly:
// the interpreter's Render aborts and returns that error the instant a
// fallible sub-expression produces one). It returns the value local's name,
// for the caller to substitute wherever it would otherwise have used goExpr
// directly. This is the one place a fallible genValueExpr result is consumed
// by a write (genInterpolation, genCode, genAttributes), so all three sites
// emit the identical `__vN, __errN := <goExpr>; if __errN != nil { return
// __errN }` shape through this single helper rather than each reproducing it.
func (g *generator) genFallibleExtraction(goExpr string) string {
	n := g.nextTmp()
	valVar := fmt.Sprintf("__v%d", n)
	errVar := fmt.Sprintf("__err%d", n)
	g.writeRaw(fmt.Sprintf("%s, %s := %s\n", valVar, errVar, goExpr))
	g.body.WriteString(fmt.Sprintf("if %s != nil {\n\treturn %s\n}\n", errVar, errVar))
	return valVar
}

// genFallibleExtractionWithFallback is genFallibleExtraction's
// attribute-only analogue: instead of aborting the render when the fallible
// value expression's evaluation fails, it substitutes rawSourceLiteral — a
// strconv.Quote'd Go string literal of the attribute's raw, un-evaluated
// source text (val.Value) — into the value local, mirroring
// Runtime.renderTag's own dynamic-attribute fallback exactly: `evaluated,
// err := r.evaluateExpr(val.Value); if err != nil { evaluated = val.Value }`
// — the interpreter does NOT abort the render on a fallible attribute
// value's evaluation error, it falls back to the attribute's raw source text
// instead, which the caller then still runs through EscapeAttr like any
// other value. This is used ONLY at the escaped dynamic-attribute write site
// in genAttributes; genFallibleExtraction itself, used by genInterpolation
// and genCode (the interpreter DOES abort on a fallible `#{}`/`=` text
// expression), keeps its abort unchanged, and the unescaped attribute path
// has no equivalent since a fallible unescaped attribute value is refused
// outright before reaching either helper.
func (g *generator) genFallibleExtractionWithFallback(goExpr, rawSourceLiteral string) string {
	n := g.nextTmp()
	valVar := fmt.Sprintf("__v%d", n)
	errVar := fmt.Sprintf("__err%d", n)
	g.writeRaw(fmt.Sprintf("%s, %s := %s\n", valVar, errVar, goExpr))
	g.body.WriteString(fmt.Sprintf("if %s != nil {\n\t%s = %s\n}\n", errVar, valVar, rawSourceLiteral))
	return valVar
}

// genFallibleExtractionInline is genFallibleExtraction's composition-site
// analogue: instead of the enclosing generated function's own bare error
// (used at the three top-level write sites — genInterpolation, genCode,
// genAttributes), it appends the extraction pair into b, a (string,
// error)-returning IIFE body under construction, returning "" plus the
// error the instant it is non-nil. Used inside the Pattern-1 arithmetic-
// combiner IIFE (genArithCombinerIIFE) and inside each short-circuited arm
// of the Pattern-2 ternary IIFE (genTernaryValueExpr) — the two places a
// fallible sub-expression's result is consumed by something other than a
// top-level write.
func (g *generator) genFallibleExtractionInline(b *strings.Builder, goExpr string) string {
	n := g.nextTmp()
	valVar := fmt.Sprintf("__v%d", n)
	errVar := fmt.Sprintf("__err%d", n)
	fmt.Fprintf(b, "%s, %s := %s\n", valVar, errVar, goExpr)
	fmt.Fprintf(b, "if %s != nil {\n\treturn \"\", %s\n}\n", errVar, errVar)
	return valVar
}

// genArithCombinerIIFE builds Pattern 1 — the non-short-circuit composition
// shape used when an arithmetic combiner (`+`/`-`/`*`, or `/`/`%` when
// either operand is itself fallible) has at least one fallible operand.
// Runtime.evaluateExpr's own combiner branches evaluate left, then right,
// eagerly and in that order, returning left's error immediately without
// evaluating right at all when left errors — so this mirrors that exactly:
// each fallible operand is extracted UP FRONT, in left-to-right source
// order, with an early `return "", err` the instant that operand's own
// extraction fails; a total operand needs no extraction and is used inline.
// combineFmt is a fmt template with two %s placeholders for the resolved
// left/right operand expressions. combinerFallible distinguishes the two
// shapes the final combine call can take: a total combiner (Add/Sub/Mul)
// returns a plain string, so the IIFE wraps it "return <combine>, nil"; a
// fallible combiner (Div/Mod, reached when the outer operator is itself `/`
// or `%` but at least one operand is fallible) already returns its own
// (string, error), so the IIFE returns that directly, propagating its own
// possible division/modulo-by-zero error rather than pretending it can't
// fail.
func (g *generator) genArithCombinerIIFE(leftExpr string, leftFallible bool, rightExpr string, rightFallible bool, combineFmt string, combinerFallible bool) string {
	var b strings.Builder
	b.WriteString("func() (string, error) {\n")

	resolvedLeft := leftExpr
	if leftFallible {
		resolvedLeft = g.genFallibleExtractionInline(&b, leftExpr)
	}
	resolvedRight := rightExpr
	if rightFallible {
		resolvedRight = g.genFallibleExtractionInline(&b, rightExpr)
	}

	combine := fmt.Sprintf(combineFmt, resolvedLeft, resolvedRight)
	if combinerFallible {
		fmt.Fprintf(&b, "return %s\n", combine)
	} else {
		fmt.Fprintf(&b, "return %s, nil\n", combine)
	}
	b.WriteString("}()")
	return b.String()
}

// genLogicalLeftVar emits a plain (non-fallible) `:= leftExpr` assignment
// into a value-context `||`/`&&` IIFE body under construction and returns the
// freshly named local it assigned, or, when leftExpr is itself fallible,
// delegates to genFallibleExtractionInline so the local is only bound once
// its (string, error) result is known to be nil-error. Factoring this choice
// out keeps genOrValueExpr and genAndValueExpr's fallible branch identical
// apart from the truthiness test and the falsy-side return value.
func (g *generator) genLogicalLeftVar(b *strings.Builder, leftExpr string, leftFallible bool) string {
	if leftFallible {
		return g.genFallibleExtractionInline(b, leftExpr)
	}
	n := g.nextTmp()
	lv := fmt.Sprintf("__l%d", n)
	fmt.Fprintf(b, "%s := %s\n", lv, leftExpr)
	return lv
}

// genOrValueExpr builds the short-circuit IIFE for a value-context `||`
// (leftExpr, rightExpr are genValueExpr's own results for each operand; *Fallible
// reports whether that operand's goExpr is (string, error)-typed rather than a
// plain string). It mirrors Runtime.evaluateExpr's own `||` branch
// (runtime.go: eval left, propagate its error; if isTruthy(left), return left
// UNCHANGED — the value, not a boolean; otherwise evaluate and return right)
// exactly, including the short-circuit: right is only ever evaluated when
// left is falsy.
//
// When both operands are total, the result is a plain string IIFE:
//
//	func() string {
//		__lN := <leftExpr>
//		if gopug.Truthy(__lN) {
//			return __lN
//		}
//		return <rightExpr>
//	}()
//
// When either operand is fallible, the result is a (string, error) IIFE.
// Left's extraction (if leftExpr is itself fallible) happens up front, since
// left is unconditionally evaluated either way; right's extraction (if
// rightExpr is fallible) is written only in the code that runs after the
// truthy-left early return — i.e., inside the falsy arm — so a fallible right
// operand (e.g. a division by zero) is never reached, and never errors, when
// left is truthy. This is the same in-arm short-circuit placement
// genTernaryValueExpr uses for its own branches.
func (g *generator) genOrValueExpr(leftExpr string, leftFallible bool, rightExpr string, rightFallible bool) (goExpr string, fallible bool) {
	if !leftFallible && !rightFallible {
		n := g.nextTmp()
		lv := fmt.Sprintf("__l%d", n)
		return fmt.Sprintf("func() string {\n%s := %s\nif gopug.Truthy(%s) {\nreturn %s\n}\nreturn %s\n}()", lv, leftExpr, lv, lv, rightExpr), false
	}

	var b strings.Builder
	b.WriteString("func() (string, error) {\n")
	lv := g.genLogicalLeftVar(&b, leftExpr, leftFallible)
	fmt.Fprintf(&b, "if gopug.Truthy(%s) {\nreturn %s, nil\n}\n", lv, lv)
	resolvedRight := rightExpr
	if rightFallible {
		resolvedRight = g.genFallibleExtractionInline(&b, rightExpr)
	}
	fmt.Fprintf(&b, "return %s, nil\n", resolvedRight)
	b.WriteString("}()")
	return b.String(), true
}

// genAndValueExpr is genOrValueExpr's `&&` counterpart, mirroring
// Runtime.evaluateExpr's own `&&` branch exactly: eval left, propagate its
// error; if left is NOT truthy, return the literal string "false" (not "" and
// not left's own value) without evaluating right at all; otherwise evaluate
// and return right. The short-circuit placement is the same as
// genOrValueExpr's — a fallible right operand's extraction sits in the code
// that runs after the falsy-left early return, i.e. inside the truthy arm, so
// it is only ever reached when left is truthy.
func (g *generator) genAndValueExpr(leftExpr string, leftFallible bool, rightExpr string, rightFallible bool) (goExpr string, fallible bool) {
	if !leftFallible && !rightFallible {
		n := g.nextTmp()
		lv := fmt.Sprintf("__l%d", n)
		return fmt.Sprintf("func() string {\n%s := %s\nif !gopug.Truthy(%s) {\nreturn \"false\"\n}\nreturn %s\n}()", lv, leftExpr, lv, rightExpr), false
	}

	var b strings.Builder
	b.WriteString("func() (string, error) {\n")
	lv := g.genLogicalLeftVar(&b, leftExpr, leftFallible)
	fmt.Fprintf(&b, "if !gopug.Truthy(%s) {\nreturn \"false\", nil\n}\n", lv)
	resolvedRight := rightExpr
	if rightFallible {
		resolvedRight = g.genFallibleExtractionInline(&b, rightExpr)
	}
	fmt.Fprintf(&b, "return %s, nil\n", resolvedRight)
	b.WriteString("}()")
	return b.String(), true
}

// genFallibleTemplatePart wraps a fallible ${} interpolation part's goExpr
// (a (string, error)-typed expression, e.g. "gopug.Div(a, b)") for use
// inside a template literal's `+`-joined segment list. Runtime.evaluateExpr's
// own template-literal walk evaluates each ${...} part with `val, _ :=
// r.evaluateExpr(interp)` — discarding any error rather than aborting the
// literal, so a division/modulo-by-zero inside a `${}` part renders that
// segment as the empty string and the surrounding Render call still
// succeeds (verified empirically: a template literal is the one place in
// the grammar where a fallible sub-expression's error is silently dropped,
// not propagated). This mirrors that by extracting the part's result and
// discarding its error the same way, so the template literal as a whole
// stays a plain, never-fallible string expression.
func (g *generator) genFallibleTemplatePart(goExpr string) string {
	n := g.nextTmp()
	valVar := fmt.Sprintf("__v%d", n)
	return fmt.Sprintf("func() string {\n\t%s, _ := %s\n\treturn %s\n}()", valVar, goExpr, valVar)
}

// scopeVar is one entry in the generator's local-variable scope stack: a Pug
// identifier (name) paired with the Go expression/variable name that
// generated code substitutes for it (goName), its reflect.Type (nil in
// type-blind mode), and whether it is a `- var` local (isVarLocal) as
// opposed to an each-loop item variable. An each-loop item variable pushes
// goName == name (the Go range variable is named after the Pug one,
// unchanged from before this field existed) and isVarLocal == false; a
// `- var` local pushes a prefixed goName (see goLocalNameForVar) and
// isVarLocal == true, so a Pug var can never collide with the generated
// function's own receiver/writer/tmp names, AND so scopeRestore can tell the
// two apart when a scope pop needs to record which names go out of scope
// (see scopeRestore's own doc comment for why that distinction matters).
type scopeVar struct {
	name       string
	goName     string
	typ        reflect.Type
	isVarLocal bool

	// indexRawGoName is set only for an each-loop INDEX variable: the Go
	// name of the underlying raw `int` range-index local this string
	// variable was derived from (goName itself holds
	// `strconv.Itoa(indexRawGoName)`, matching the interpreter's own
	// `scope[IndexVar] = strconv.Itoa(i)`). It lets a numeric-literal
	// comparison against the index (`if i == 0`, `if i > 1`) compile
	// against the exact underlying int rather than attempting a string
	// comparison, which is unset for every other scope var.
	indexRawGoName string
}

// lookupScope searches the scope stack innermost-first for name, returning
// its full entry and whether it was found. A found entry's typ is nil only
// when the generator itself is in type-blind mode (rootType == nil).
func (g *generator) lookupScope(name string) (scopeVar, bool) {
	for i := len(g.scope) - 1; i >= 0; i-- {
		if g.scope[i].name == name {
			return g.scope[i], true
		}
	}
	return scopeVar{}, false
}

func (g *generator) isBound(name string) bool {
	_, ok := g.lookupScope(name)
	return ok
}

func (g *generator) pushScope(name, goName string, typ reflect.Type, isVarLocal bool) {
	g.scope = append(g.scope, scopeVar{name: name, goName: goName, typ: typ, isVarLocal: isVarLocal})
}

// pushIndexScope binds an each-loop INDEX variable: a STRING scope var
// (goName holds `strconv.Itoa(rawIntGoName)`, mirroring the interpreter's
// own `scope[IndexVar] = strconv.Itoa(i)` exactly — see genEach's doc
// comment), with rawIntGoName recorded so a numeric-literal comparison
// against it (genComparison) can compile against the underlying int instead
// of the string.
func (g *generator) pushIndexScope(name, goName, rawIntGoName string) {
	g.scope = append(g.scope, scopeVar{name: name, goName: goName, typ: reflectTypeString, indexRawGoName: rawIntGoName})
}

// scopeMark returns the current scope depth, for a caller that is about to
// process a list of sibling nodes (a tag's children, an if/else branch's
// body, an each-loop's body) to save and later restore with scopeRestore —
// so any `- var`/each-loop binding introduced while processing that list
// goes out of scope again once the list finishes, exactly mirroring the Go
// lexical block the generated code for that list lives in.
func (g *generator) scopeMark() int {
	return len(g.scope)
}

// scopeRestore truncates the scope stack back to mark, discarding every
// binding pushed since the matching scopeMark call — and, for every
// `- var` local among those discarded bindings (isVarLocal — never an
// each-loop item variable), records its Pug name into leakedVarNames FIRST,
// so a LATER out-of-scope reference to that same name is rejected by
// resolveFieldExpr instead of silently falling through to struct-field
// resolution.
//
// This exists because Runtime's OWN scopeStack does not frame a scope at
// every one of these boundaries the way codegen's g.scope does: renderEach
// (and a mixin call) push/pop a real scopeStack frame, matching codegen's
// each-loop-body pop exactly, but renderConditional and a tag's children
// loop do NOT push a frame at all — a `- var` assigned inside an `if`
// branch or a tag's children is stored via Runtime.setVar into whichever
// frame is already innermost at that point (typically the enclosing
// each-loop iteration's frame, or the function-wide root frame), so it
// stays live and readable by a LATER SIBLING after that `if`/tag block
// closes, for as long as that enclosing frame itself survives. If the
// referenced name also happens to satisfy resolveStructField's exact/tag/
// case-insensitive matching against a real struct field — proven
// empirically to fire for both an EXACT-name collision (Runtime.setVar's
// walk-and-overwrite finds and mutates an existing same-named top-level
// key, so a later read returns whatever was last assigned, not any
// "original" field value) and a case-differing collision (resolveStructField's
// third, case-insensitive tier) — a naive codegen that just pops the
// binding and falls through to `d.Field` would silently read a value the
// interpreter never produces. Since this is exactly as true for an
// each-loop-body `- var` (Runtime's frame pop there is real, but nothing
// stops a DIFFERENT already-existing outer-frame same-named key from being
// mutated during the loop, or a differently-cased struct field from
// matching afterward) as it is for an if/tag-children one, EVERY
// scopeRestore call in the generator applies this same guard uniformly —
// deliberately more conservative than only guarding the exact shape the bug
// was first found in. Only a `- var` binding is ever recorded (an
// each-loop item variable is a pre-existing, differently-typed mechanism
// this feature does not touch); the top-level statement list is never
// scope-restored at all (no call site does it), so a `- var` declared at
// the top level — the real-world headline shape this feature targets —
// never enters leakedVarNames and stays resolvable for the rest of the
// generated function, exactly like Runtime's own root-frame `- var`.
func (g *generator) scopeRestore(mark int) {
	for i := mark; i < len(g.scope); i++ {
		if g.scope[i].isVarLocal {
			if g.leakedVarNames == nil {
				g.leakedVarNames = make(map[string]bool)
			}
			g.leakedVarNames[g.scope[i].name] = true
		}
	}
	g.scope = g.scope[:mark]
}

// derefType unwraps t through any number of pointer indirections, returning
// the first non-pointer type reached (or nil if t is nil).
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// resolveFieldPath walks a dot-path of already-split Pug identifier segments
// starting from typ, dereferencing pointers at each step and mapping each
// segment to its Go struct field via resolveStructField (the same exact
// name → `pug` tag → case-insensitive rule the interpreter's getField uses),
// so a lowercase or tagged Pug identifier resolves to its exported Go field.
// Returns the resolved leaf reflect.Type and the dot-joined RESOLVED Go
// field names (e.g. "TaskPreview.AdjusterName"), not the verbatim Pug
// segments. path is the original dotted expression, used only for the error
// message. An empty segments slice returns typ unchanged and an empty
// goPath (the path is exactly the starting type itself, e.g. a bound
// each-loop scalar used bare).
func resolveFieldPath(typ reflect.Type, path string, segments []string) (reflect.Type, string, error) {
	cur := typ
	var goSegs []string
	for _, seg := range segments {
		cur = derefType(cur)
		if cur == nil || cur.Kind() != reflect.Struct {
			return nil, "", fmt.Errorf("unsupported field path %q: %s is not a field of a struct", path, seg)
		}
		f, ok := resolveStructField(cur, seg)
		if !ok {
			return nil, "", fmt.Errorf("unsupported field path %q: %q is not a field of %s", path, seg, cur)
		}
		goSegs = append(goSegs, f.Name)
		cur = f.Type
	}
	return cur, strings.Join(goSegs, "."), nil
}

// Basic (unnamed) reflect.Types for the scalar kinds resolveFieldExpr's
// callers need to distinguish from a named type of the same kind (e.g. a
// bare bool field vs a `type Flag bool` field): the stringify calls the
// latter needs still type-check, but only once wrapped in an explicit
// conversion, whereas the exact builtin type doesn't need one.
var (
	reflectTypeString  = reflect.TypeOf("")
	reflectTypeBool    = reflect.TypeOf(false)
	reflectTypeInt     = reflect.TypeOf(int(0))
	reflectTypeFloat64 = reflect.TypeOf(float64(0))
)

// reflectTypeStringSlice is the reflect.Type a mixin rest parameter
// (`...items`) is pushed onto the generator's scope with — a Go `[]string`,
// the element type Runtime.renderMixinCall's own rest-argument collection
// always produces (each element is a string, via evaluateMixinArg), so the
// existing slice-typed body machinery (genEach, `.length`, index, `.join`)
// consumes it unchanged.
var reflectTypeStringSlice = reflect.TypeOf([]string(nil))

// reflectTypeStringMap is the reflect.Type a &attributes-forwarding mixin
// call's runtime call-attribute map (see genMixinCallAttrsMap) is pushed
// onto the generator's scope with, under the mixin's own special
// "attributes" name — a Go map[string]string, matching the value type
// Runtime.renderMixinCall's own attrMap always holds (every entry is a
// string, via evaluateExpr or the bare "true" literal). Pushing it with
// this type lets the mixin body's own `&attributes(attributes)` tag resolve
// "attributes" through resolveFieldExpr into genSpreadAttrs's pre-existing
// map[string]string spread-source path, exactly like any other
// map[string]string field or scope variable.
var reflectTypeStringMap = reflect.TypeOf(map[string]string(nil))

// convertExpr returns goExpr unchanged when typ is exactly builtin (no
// conversion needed to satisfy a function parameter of that builtin type),
// or wraps it in an explicit conversion (goTypeName + "(" + goExpr + ")")
// when typ is merely a named type with the same underlying kind.
func convertExpr(goExpr string, typ, builtin reflect.Type, goTypeName string) string {
	if typ == builtin {
		return goExpr
	}
	return goTypeName + "(" + goExpr + ")"
}

// writeStatic appends literal text to the pending static chunk.
func (g *generator) writeStatic(s string) {
	g.static.WriteString(s)
}

// flushStatic emits the pending static chunk (if any) as a single
// io.WriteString call and resets the buffer.
func (g *generator) flushStatic() {
	if g.static.Len() == 0 {
		return
	}
	g.body.WriteString("io.WriteString(w, ")
	g.body.WriteString(strconv.Quote(g.static.String()))
	g.body.WriteString(")\n")
	g.static.Reset()
}

// writeExprWrite flushes any pending static text, then emits a write of a
// dynamic Go expression (already valid Go source, e.g. "html.EscapeString(d.Name)").
func (g *generator) writeExprWrite(goExpr string) {
	g.flushStatic()
	g.body.WriteString("io.WriteString(w, ")
	g.body.WriteString(goExpr)
	g.body.WriteString(")\n")
}

// writeRaw flushes any pending static text, then appends a raw line of Go
// source (a control-flow header or closer) to the function body.
func (g *generator) writeRaw(line string) {
	g.flushStatic()
	g.body.WriteString(line)
}

// genNode dispatches a single AST node to its codegen handler, or returns an
// "unsupported" error for any node type outside this increment's grammar.
func (g *generator) genNode(n Node) error {
	switch node := n.(type) {
	case *DoctypeNode:
		g.writeStatic((*Runtime)(nil).formatDoctype(node.Value))
		return nil
	case *TagNode:
		return g.genTag(node)
	case *TextNode:
		g.writeStatic(htmlEscapeText(node.Content))
		return nil
	case *PipeNode:
		g.writeStatic(htmlEscapeText(node.Content))
		return nil
	case *BlockTextNode:
		return g.genBlockText(node)
	case *TextRunNode:
		for _, child := range node.Nodes {
			if err := g.genNode(child); err != nil {
				return err
			}
		}
		return nil
	case *InterpolationNode:
		return g.genInterpolation(node)
	case *CodeNode:
		return g.genCode(node)
	case *EachNode:
		return g.genEach(node)
	case *ConditionalNode:
		return g.genConditional(node)
	case *CaseNode:
		return g.genCase(node)
	case *CommentNode:
		return g.genComment(node)
	case *MixinDeclNode:
		// Already collected and emitted as its own top-level helper function
		// by GenerateGo before the main render function is generated (see the
		// mixin-decl pass there); a declaration contributes no output of its
		// own wherever it's encountered, matching
		// Runtime.renderNode's own `case *MixinDeclNode: return nil` exactly.
		return nil
	case *MixinCallNode:
		return g.genMixinCall(node)
	case *BlockNode:
		if g.blockParamName != "" {
			g.writeRaw(fmt.Sprintf("if %s != nil {\nif err := %s(w); err != nil {\nreturn err\n}\n}\n", g.blockParamName, g.blockParamName))
			return nil
		}
		return fmt.Errorf("unsupported node/expr in codegen: %T", n)
	default:
		return fmt.Errorf("unsupported node/expr in codegen: %T", n)
	}
}

// genTag emits a tag's open tag (name + static attributes), its children
// (unless it is self-closing or a void element), and its close tag.
//
// A tag carrying `&attributes(...)`, reached WHILE genMixinCallAttrForward is
// generating a &attributes-forwarding mixin's body inline for one specific
// call site (g.inAttrForwardBody), is special-cased: mergeForwardedAttributes
// computes, at GENERATE TIME, the exact same merged attribute map
// Runtime.renderTag's own spread-merge would build for that call — the
// result is an ordinary map[string]*AttributeValue containing only bare and
// quoted-literal-string entries, so it is handed to genAttributes completely
// unchanged, through the exact same static rendering path (sortAttrNames +
// htmlEscapeAttr) every other static tag in this file already uses.
//
// A `&attributes` tag reached OUTSIDE that inlined-forwarding body — a plain
// document tag, or a mixin body that isn't itself flagged as attribute-
// forwarding — is handed to genSpreadAttrs instead, which supports the
// narrower case of a spread source resolving to a RUNTIME map[string]string
// value (its keys aren't known at generate time, so genAttributes's static
// path can't render it): if genSpreadAttrs can't prove the spread source and
// the tag's own base attributes fit that shape, it returns its own clean
// deferral error rather than falling through to genAttributes's blanket
// "&attributes" defer, so each unsupported shape gets a distinct message.
func (g *generator) genTag(tag *TagNode) error {
	g.writeStatic("<" + tag.Name)

	spread, hasSpread := tag.Attributes["&attributes"]
	switch {
	case hasSpread && g.inAttrForwardBody:
		merged, err := g.mergeForwardedAttributes(tag.Attributes, spread)
		if err != nil {
			return fmt.Errorf("tag %q: %w", tag.Name, err)
		}
		if err := g.genAttributes(merged); err != nil {
			return fmt.Errorf("tag %q: %w", tag.Name, err)
		}
	case hasSpread:
		if err := g.genSpreadAttrs(tag, spread); err != nil {
			return fmt.Errorf("tag %q: %w", tag.Name, err)
		}
	default:
		if err := g.genAttributes(tag.Attributes); err != nil {
			return fmt.Errorf("tag %q: %w", tag.Name, err)
		}
	}

	if tag.SelfClose {
		g.writeStatic(" />")
		return nil
	}
	if isVoidElement(tag.Name) {
		g.writeStatic(">")
		return nil
	}
	g.writeStatic(">")

	if isRawTextElement(tag.Name) {
		g.inRawTextElement = true
	}

	mark := g.scopeMark()
	for _, child := range tag.Children {
		if err := g.genNode(child); err != nil {
			g.scopeRestore(mark)
			if isRawTextElement(tag.Name) {
				g.inRawTextElement = false
			}
			return err
		}
	}
	g.scopeRestore(mark)

	if isRawTextElement(tag.Name) {
		g.inRawTextElement = false
	}

	g.writeStatic("</" + tag.Name + ">")
	return nil
}

// genBlockText emits a dot-block text node (`p.` / `script.` etc.) by
// tokenizing block.Content, at GENERATE time, through the exact same
// Lexer.emitTextWithInterpolations helper Runtime.renderBlockText calls at
// RENDER time. Since block.Content is a static string, this produces
// byte-identical tokens to whatever the interpreter would split at render
// time — there is no render-state input to that split beyond the static
// content itself — so each token can be mapped straight onto already-proven
// codegen machinery instead of re-deriving the split.
//
// The interpreter's own per-token escaping (Runtime.renderBlockText) is
// CONTEXT-SPECIFIC: a TokenText and a TokenInterpolation (`#{}`) are each
// escaped through htmlEscapeText — NOT html.EscapeString, which also escapes
// quotes and would diverge — in the normal case, but written completely raw
// (no escaping at all) while g.inRawTextElement mirrors the interpreter
// being inside a raw-text element (script/style). A TokenInterpolationUnescape
// (`!{}`) is always written raw, in both contexts. A TokenText's static
// portion is escaped (or not) at GENERATE time, exactly like genNode's own
// TextNode/PipeNode cases already do; a TokenInterpolation/
// TokenInterpolationUnescape's dynamic portion is escaped (or not) at RUNTIME
// via the exported gopug.EscapeText, generated code's single-sourced twin of
// htmlEscapeText.
//
// A fallible interpolation (genValueExpr's own fallible flag — a top-level
// `/` or `%`) is refused rather than extracted: Runtime.renderBlockText
// silently falls back to the RAW, un-evaluated expression source the moment
// evaluateExpr errors, a fallback a generated function that aborts the whole
// render on that same error cannot reproduce. Tag interpolation (`#[...]`) is
// refused too — reproducing its inner sub-lex+parse+render at generate time
// is a separate, larger claim this slice does not attempt.
func (g *generator) genBlockText(block *BlockTextNode) error {
	lx := NewLexer("")
	lx.emitTextWithInterpolations(block.Content, 0)

	for _, tok := range lx.tokens {
		switch tok.Type {
		case TokenText:
			if g.inRawTextElement {
				g.writeStatic(tok.Value)
			} else {
				g.writeStatic(htmlEscapeText(tok.Value))
			}

		case TokenInterpolation:
			valExpr, fallible, err := g.genValueExpr(tok.Value)
			if err != nil {
				return fmt.Errorf("unsupported block text interpolation %q in codegen: %w", tok.Value, err)
			}
			if fallible {
				return fmt.Errorf("unsupported block text interpolation %q in codegen: a fallible expression (a division or modulo operator) cannot be reproduced, since the interpreter falls back to the raw, un-evaluated expression source on an evaluation error rather than aborting the render", tok.Value)
			}
			if g.inRawTextElement {
				g.writeExprWrite(valExpr)
			} else {
				g.needsGopug = true
				g.writeExprWrite("gopug.EscapeText(" + valExpr + ")")
			}

		case TokenInterpolationUnescape:
			valExpr, fallible, err := g.genValueExpr(tok.Value)
			if err != nil {
				return fmt.Errorf("unsupported block text interpolation %q in codegen: %w", tok.Value, err)
			}
			if fallible {
				return fmt.Errorf("unsupported block text interpolation %q in codegen: a fallible expression (a division or modulo operator) cannot be reproduced, since the interpreter falls back to the raw, un-evaluated expression source on an evaluation error rather than aborting the render", tok.Value)
			}
			g.writeExprWrite(valExpr)

		case TokenTagInterpolationStart, TokenTagInterpolationEnd:
			return fmt.Errorf("unsupported block text tag interpolation (#[...]) in codegen")

		default:
			return fmt.Errorf("unsupported block text token type %v in codegen", tok.Type)
		}
	}

	return nil
}

// genAttributes streams a tag's attribute list — in the same id/class/
// alphabetical order sortAttrNames and Runtime.renderTag use — interleaving
// static baked chunks (bare attributes, quoted-literal values) with dynamic
// writes so a tag mixing static and dynamic attributes still shares the
// generator's single static-buffer-then-flush machinery. Per attribute:
//
//   - a bare attribute (no value) emits its static " name" form, unchanged
//     from increment 1;
//   - a plain quoted string literal is baked into the static buffer at
//     generate time, escaped (htmlEscapeAttr) unless the attribute is
//     unescaped (`!=`/an unescaped shorthand), in which case the literal is
//     baked in RAW — Runtime.renderTag computes the identical `evaluated`
//     value for the escaped and unescaped forms of the same attribute and
//     differs only by whether it wraps it in htmlEscapeAttr, so the
//     unescaped form is simply that wrapper dropped;
//   - a dynamic value whose name IS an HTML boolean attribute
//     (isBooleanAttribute) requires a bool-typed field: it emits a
//     conditional bare write — ` name="true"` iff the field is true, nothing
//     at all iff false — matching the interpreter's omit-on-false behaviour
//     for these names. This path stays resolveFieldExpr-only (a bare field,
//     never a general value expression): a non-bool-typed value on such a
//     name is deferred (error) rather than risking a byte-identical breach —
//     the interpreter also omits a boolean-attribute-named value that merely
//     stringifies to "false" (e.g. a string field literally holding
//     "false"), a general rule this increment doesn't reproduce. An
//     unescaped boolean attribute is deferred outright (a distinct error) —
//     the value can only ever be "true" or omitted, so escaping is moot, but
//     combining the omit-on-false rule with an unescaped write is a separate
//     claim this increment doesn't make;
//   - a dynamic value on any other, non-"class" name is built by genValueExpr
//     (genValueExpr's whole supported grammar) and, for the ordinary escaped
//     case, always escaped through the exported EscapeAttr (never
//     html.EscapeString — attribute escaping has different rules from text
//     escaping), applied once to the built value as a whole rather than
//     per-leaf, exactly as genInterpolation and genCode do for the same
//     value-context grammar. An unescaped value on such a name writes the
//     SAME genValueExpr result RAW instead — no EscapeAttr wrapper, and this
//     write alone never sets g.needsGopug (a sibling escaped write or any
//     other gopug-needing construct in the template still will). When
//     genValueExpr reports the value fallible (a top-level `/` or `%`), the
//     escaped case's genFallibleExtraction prelude is emitted BEFORE the
//     attribute's own static ` name="` text — the extraction is a statement,
//     so it must land as its own line ahead of the write sequence it feeds,
//     never interleaved inside it. A fallible unescaped value is deferred
//     instead of extracted: Runtime.renderTag's own dynamic-attribute
//     evaluation (the branch this whole case mirrors) falls back to the RAW,
//     un-evaluated expression source the moment evaluateExpr errors, rather
//     than aborting the render — a fallback a generated function that aborts
//     the whole render on that same error cannot reproduce, for either the
//     escaped or the unescaped form. (The escaped form still uses
//     genFallibleExtraction regardless, matching its own prior increment
//     unchanged; this increment does not touch that.)
//   - a dynamic "class" value merging shorthand class tokens with one or
//     more bare string-field tokens (see genDynamicClass) emits a runtime
//     write joining the tokens with the exported JoinClasses (which drops an
//     empty token, matching the interpreter's empty-token rule) and escapes
//     the joined result with EscapeAttr. An unescaped dynamic class value is
//     deferred outright (a distinct error) — merging class-normalization
//     with no-escape is a separate claim this increment doesn't make;
//
// A style object, `&attributes`, an unescaped dynamic class or boolean
// attribute, a fallible unescaped value, a class-object/array value, or any
// value genValueExpr still can't build (a non-string-keyed map index, most
// method calls, an array/object literal, …) is out of scope for this
// increment and returns an error rather than guessing at output that might
// not match the interpreter. With a nil Config.DataReflectType (type-blind
// mode), a dynamic value can't be classified as scalar or bool at all (nor a
// class token confirmed a string field), so only static/bare attributes
// (including a static unescaped literal, which needs no type information)
// are supported there, matching increment 1 unchanged.
func (g *generator) genAttributes(attrs map[string]*AttributeValue) error {
	if _, ok := attrs["&attributes"]; ok {
		return fmt.Errorf("unsupported dynamic &attributes in codegen")
	}

	for _, name := range sortAttrNames(attrs) {
		val := attrs[name]

		if val.IsBare {
			g.writeStatic(" " + name)
			continue
		}

		trimmed := strings.TrimSpace(val.Value)
		if lit, ok := unwrapQuotedLiteral(trimmed); ok {
			if val.Unescaped {
				g.writeStatic(" " + name + `="` + lit + `"`)
			} else {
				g.writeStatic(" " + name + `="` + htmlEscapeAttr(lit) + `"`)
			}
			continue
		}

		if name == "class" {
			if val.Unescaped {
				return fmt.Errorf("unsupported unescaped attribute %q in codegen (a dynamic unescaped class value is not supported in this increment)", name)
			}
			if err := g.genDynamicClass(trimmed); err != nil {
				return fmt.Errorf("attribute %q: %w", name, err)
			}
			continue
		}

		if g.rootType == nil {
			return fmt.Errorf("unsupported dynamic attribute %q in codegen (only static quoted values are supported in this increment)", name)
		}

		if isBooleanAttribute(name) {
			if val.Unescaped {
				return fmt.Errorf("unsupported unescaped attribute %q in codegen (an unescaped HTML boolean attribute is not supported in this increment)", name)
			}
			goExpr, typ, err := g.resolveFieldExpr(trimmed)
			if err != nil {
				return fmt.Errorf("attribute %q: %w", name, err)
			}
			if typ.Kind() != reflect.Bool {
				return fmt.Errorf("unsupported dynamic attribute %q in codegen (only a bool-typed value is supported for an HTML boolean attribute name in this increment)", name)
			}
			boolExpr := convertExpr(goExpr, typ, reflectTypeBool, "bool")
			g.writeRaw(fmt.Sprintf("if %s {\n", boolExpr))
			g.body.WriteString("io.WriteString(w, " + strconv.Quote(" "+name+`="true"`) + ")\n")
			g.body.WriteString("}\n")
			continue
		}

		valExpr, fallible, err := g.genValueExpr(trimmed)
		if err != nil {
			return fmt.Errorf("attribute %q: %w", name, err)
		}

		if val.Unescaped {
			if fallible {
				return fmt.Errorf("unsupported unescaped attribute %q in codegen (a fallible expression — a division or modulo operator — cannot be reproduced, since the interpreter falls back to the raw, un-evaluated expression source on an evaluation error rather than aborting the render)", name)
			}
			g.writeStatic(" " + name + `="`)
			g.writeExprWrite(valExpr)
			g.writeStatic(`"`)
			continue
		}

		if fallible {
			valExpr = g.genFallibleExtractionWithFallback(valExpr, strconv.Quote(val.Value))
		}
		g.needsGopug = true
		g.writeStatic(" " + name + `="`)
		g.writeExprWrite("gopug.EscapeAttr(" + valExpr + ")")
		g.writeStatic(`"`)
	}
	return nil
}

// mergeForwardedAttributes computes, at GENERATE TIME, the exact merged
// attribute map Runtime.renderTag's own `&attributes` spread-merge would
// build for the CURRENT call site (g.attrForwardCallAttrs), for a tag whose
// `&attributes` entry is spread — the only call site this is ever invoked
// from, genTag, already guarantees g.inAttrForwardBody is true.
//
// Only `&attributes(attributes)` — the mixin's own special "attributes"
// variable, spelled exactly that way — is supported; any other expression
// (a data-map field, a `- var` object, an inline `&attributes({...})`) is
// the GENERAL `&attributes` spread, a separate, not-yet-supported feature
// (genAttributes's own pre-existing check still defers it everywhere else),
// so it is refused here too with its own distinct error.
//
// Every OTHER attribute already on the tag is copied into the merged map
// unchanged — INCLUDING a dynamic one (e.g. `href=href`, a reference to the
// mixin's own parameter): genAttributes already knows how to render a
// dynamic, non-"class" attribute exactly like it would on any other tag, and
// nothing here needs to inspect its value, since the call's spread either
// leaves it alone entirely or completely overwrites it (matching
// Runtime.renderTag's own default-case plain map assignment) — EXCEPT
// "class", which is APPENDED to rather than overwritten, so the tag's own
// base "class" (if any) must be a simple bare/quoted-literal value for this
// tag to be supported at all, regardless of whether the call actually
// spreads a "class" key (a class object, an operator expression, or any
// other dynamic class value is out of scope for a &attributes tag this
// slice — Runtime.renderTag's own dynamic-class evaluation branches stay
// untouched and untested here).
//
// Reproduces Runtime.renderTag's spread-merge switch exactly: a spread
// "class" key is space-appended to the existing "class" value (or set
// outright if the tag has none); for every other name, the call attribute's
// value string decides the outcome — "true" makes it bare, "false" deletes
// it entirely, and anything else becomes a quoted string literal — matching
// renderTag's `case "true": IsBare = true` / `case "false": delete` /
// `default: quoted` exactly. Because both the call attributes and every
// value produced here are static, the resulting map contains only bare and
// quoted-literal-string entries — genAttributes's own already-tested static
// path renders it byte-identically to Runtime.renderTag's own render loop
// (both call sortAttrNames for order and htmlEscapeAttr/EscapeAttr for
// escaping), without any further changes to genAttributes itself.
func (g *generator) mergeForwardedAttributes(attrs map[string]*AttributeValue, spread *AttributeValue) (map[string]*AttributeValue, error) {
	expr := strings.TrimSpace(spread.Value)
	if expr != "attributes" {
		return nil, fmt.Errorf("&attributes(%s): forwarding a value other than the mixin's own special \"attributes\" variable is not supported in codegen yet", expr)
	}

	merged := make(map[string]*AttributeValue, len(attrs))
	for name, val := range attrs {
		if name == "&attributes" {
			continue
		}
		merged[name] = val
	}

	if base, ok := merged["class"]; ok {
		if base.IsBare || base.Unescaped {
			return nil, fmt.Errorf("base attribute \"class\" on a &attributes tag: only a static quoted-string base class is supported in codegen")
		}
		if _, ok := unwrapQuotedLiteral(strings.TrimSpace(base.Value)); !ok {
			return nil, fmt.Errorf("base attribute \"class\" on a &attributes tag: a dynamic class value is not supported in codegen")
		}
	}

	for name, valStr := range g.attrForwardCallAttrs {
		if name == "class" {
			if existing, ok := merged["class"]; ok {
				existingLit, _ := unwrapQuotedLiteral(strings.TrimSpace(existing.Value))
				merged["class"] = &AttributeValue{Value: `"` + existingLit + " " + valStr + `"`}
			} else {
				merged["class"] = &AttributeValue{Value: `"` + valStr + `"`}
			}
			continue
		}

		switch valStr {
		case "true":
			merged[name] = &AttributeValue{IsBare: true}
		case "false":
			delete(merged, name)
		default:
			merged[name] = &AttributeValue{Value: `"` + valStr + `"`}
		}
	}

	return merged, nil
}

// genSpreadAttrs handles a `&attributes(<expr>)` tag reached OUTSIDE an
// inlined attribute-forwarding mixin body (genTag's other &attributes
// branch, mergeForwardedAttributes, covers that case): here <expr> can be
// ANY expression, most commonly a struct field, whose keys are not known
// until the template actually renders — unlike the forwarding case, there is
// no way to compute the merged attribute map at generate time, so the merge
// itself (and its sortAttrNames ordering and escaping) has to happen at
// RUNTIME, via the exported gopug.WriteSpreadAttrs / gopug.WriteSpreadAttrsAny
// helpers.
//
// This increment supports three narrow shapes it can prove byte-identical to
// Runtime.renderTag: <expr> must resolve, through resolveFieldExpr, to a
// map[string]string-typed value, a map[string]any (map[string]interface{})-
// typed value, or a map[string]<T>-typed value whose element kind T is a
// concrete SCALAR (bool, any signed/unsigned integer kind, float32, or
// float64) — a struct field or a mixin-scope variable — reflect.Map with a
// string key and a string, empty-interface, or scalar element kind — and
// every OTHER attribute already on the tag (the merge's "base", built by
// genSpreadBase) must be simple: a bare boolean, a static quoted-string
// literal (the same base-attribute shape mergeForwardedAttributes itself
// requires), or a dynamic PLAIN-SCALAR value-expr genSpreadBase can prove
// reduces to the same evaluate-then-escape-once render Runtime.renderTag's
// own non-spread branch already produces for a base attribute the runtime
// spread does not touch (see genSpreadBase's own doc comment for exactly
// which dynamic base shapes qualify, and which stay deferred). For a
// map[string]any source, each value is stringified with fmt.Sprintf("%v", v)
// — the EXACT call Runtime.renderTag's own spread path uses (valStr :=
// fmt.Sprintf("%v", attrVal)) — before the merge, via gopug.WriteSpreadAttrsAny.
// A concrete-scalar source (map[string]int, map[string]bool,
// map[string]float64, ...) is first converted, AT THE CALL SITE, into a
// map[string]any by boxing each typed value (see genScalarSpreadBoxLiteral),
// then handed to that SAME gopug.WriteSpreadAttrsAny entry point — boxing a
// concrete scalar into `any` and then `%v`-ing it produces the identical text
// Runtime.renderTag's own reflect.Value.MapIndex(...).Interface() boxing
// followed by the identical fmt.Sprintf("%v", ...) call produces, because
// both sides box the exact same concrete type before the exact same %v call
// runs on it; no new runtime helper or render logic is needed for this shape.
// A map[string]string source needs no stringification and goes through
// gopug.WriteSpreadAttrs unchanged, exactly as before this increment.
// Anything else — an inline object literal (`&attributes({...})`), a
// non-map, a map with a non-string key, or a map whose element kind is
// neither string, the empty interface, nor a concrete scalar
// (map[string][]string, map[string]map[...]..., map[string]struct{...},
// map[string]*T, map[int]string, ...), a base attribute genSpreadBase can't
// prove reduces to that same evaluate-then-escape-once render (a dynamic
// "class" or HTML-boolean-attribute-named base, a fallible base expression,
// a style/class object, or any shape genValueExpr itself doesn't support), an
// unescaped base attribute, a base "class" literal with leading/trailing or
// repeated internal whitespace (whether the interpreter collapses it depends
// on whether the RUNTIME spread map happens to supply its own "class" key —
// not knowable at generate time, so it is refused rather than guessed), or a
// nil Config.DataReflectType (there is no type to resolve <expr> or classify
// a base attribute's value against at all) — is refused with its own
// distinct, clean error rather than guessing at output that might not match
// the interpreter.
//
// base's AttributeValue.Value entries hold the RAW, already-unescaped
// attribute text with no surrounding quotes (e.g. Value: "red", not
// Value: `"red"`) — WriteSpreadAttrs's/WriteSpreadAttrsAny's own contract,
// distinct from the quoted-source-string convention AttributeValue.Value
// otherwise carries elsewhere in this file (parseAttributes's parsed AST,
// and mergeForwardedAttributes's static merge result, both feed
// genAttributes's unwrapQuotedLiteral step instead) — because these are
// runtime code paths with no static unwrap/escape step of their own to
// reuse; each escapes every non-bare value exactly once, itself, via
// EscapeAttr.
//
// An inline object literal source (`&attributes({key: "val", ...})`) is
// handled separately by genSpreadAttrsInlineObject: unlike a field/variable
// spread source, an inline object's keys AND values are fully determined by
// the template source text alone (parseInlineObject never evaluates a
// value — it only strips surrounding quotes), so the whole spread map is
// knowable at generate time and can be emitted as a static Go map literal
// instead of a runtime expression. The nil-Config.DataReflectType (type-
// blind) gate below still applies to that path too, purely for consistency
// with every other &attributes shape this function supports — an inline
// object needs no type resolution at all, so it could in principle be
// supported type-blind as a small follow-up, but this increment keeps the
// scope of "type-blind mode" simple (either everything dynamic defers, or
// nothing does) rather than carving out a single exception.
func (g *generator) genSpreadAttrs(tag *TagNode, spread *AttributeValue) error {
	expr := strings.TrimSpace(spread.Value)

	if g.rootType == nil {
		return fmt.Errorf("unsupported &attributes(%s) in codegen: a nil Config.DataReflectType (type-blind mode) cannot resolve a runtime spread source's type", expr)
	}

	if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
		return g.genSpreadAttrsInlineObject(tag, expr)
	}

	goExpr, typ, err := g.resolveFieldExpr(expr)
	if err != nil {
		return fmt.Errorf("unsupported &attributes(%s) in codegen: %w", expr, err)
	}
	if typ == nil || typ.Kind() != reflect.Map {
		gotKind := "untyped"
		if typ != nil {
			gotKind = typ.String()
		}
		return fmt.Errorf("unsupported &attributes(%s) in codegen: only a map[string]string-typed or map[string]any-typed spread source is supported this increment (got %s)", expr, gotKind)
	}
	if typ.Key().Kind() != reflect.String {
		return fmt.Errorf("unsupported &attributes(%s) in codegen: only a string-keyed map spread source is supported this increment (got %s, whose key type is %s)", expr, typ.String(), typ.Key().String())
	}
	isAnySpread := typ.Elem().Kind() == reflect.Interface && typ.Elem().NumMethod() == 0
	isScalarSpread := isScalarMapElemKind(typ.Elem().Kind())
	if typ.Elem().Kind() != reflect.String && !isAnySpread && !isScalarSpread {
		return fmt.Errorf("unsupported &attributes(%s) in codegen: only a map[string]string-typed, map[string]any-typed, or scalar-valued (map[string]<bool/int/uint/float kind>) spread source is supported this increment (got %s)", expr, typ.String())
	}

	base, err := g.genSpreadBase(tag, expr)
	if err != nil {
		return err
	}

	helperFunc := "gopug.WriteSpreadAttrs"
	spreadExpr := goExpr
	if isAnySpread {
		helperFunc = "gopug.WriteSpreadAttrsAny"
	} else if isScalarSpread {
		helperFunc = "gopug.WriteSpreadAttrsAny"
		spreadExpr = genScalarSpreadBoxLiteral(goExpr)
	}

	g.needsGopug = true
	g.writeRaw("if err := " + helperFunc + "(w, " + genSpreadBaseLiteral(base) + ", " + spreadExpr + "); err != nil {\nreturn err\n}\n")
	return nil
}

// isScalarMapElemKind reports whether k is a concrete scalar reflect.Kind —
// bool, any signed/unsigned integer kind, or either float kind — the set of
// map[string]<T> element kinds genSpreadAttrs's scalar-spread path accepts
// in addition to its pre-existing string and empty-interface element kinds.
// Every other kind (slice, array, map, struct, pointer, complex, chan, func,
// string, interface) is deliberately excluded: a non-scalar concrete value
// is out of this increment's scope even though its fmt.Sprintf("%v", ...)
// text would likely also match the interpreter, and string/interface are
// each already handled by genSpreadAttrs's own pre-existing branches.
func isScalarMapElemKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// genScalarSpreadBoxLiteral wraps srcExpr — a Go expression resolving to a
// map[string]<T> with a concrete scalar element type T (see
// isScalarMapElemKind) — in an immediately-invoked function literal that
// copies it into a plain map[string]any, boxing each typed value with a bare
// `m[k] = v` assignment. Boxing a concrete scalar into `any` this way and
// then letting gopug.WriteSpreadAttrsAny's own fmt.Sprintf("%v", v) stringify
// it produces text byte-identical to Runtime.renderTag's own
// reflect.Value.MapIndex(...).Interface() boxing followed by the identical
// fmt.Sprintf("%v", ...) call, because both sides box the exact same
// concrete type before the exact same %v call runs on it — no new runtime
// helper is needed; this conversion is the entire scalar-spread feature.
// srcExpr is referenced twice (once by len, once by the range) but is always
// a side-effect-free field or scope-variable reference (resolveFieldExpr's
// own contract), so evaluating it twice is safe.
func genScalarSpreadBoxLiteral(srcExpr string) string {
	return "func() map[string]any { m := make(map[string]any, len(" + srcExpr + ")); for k, v := range " + srcExpr + " { m[k] = v }; return m }()"
}

// spreadBaseAttr is one base attribute of a &attributes spread tag, as
// classified by genSpreadBase. Exactly one of the three fields applies: a
// bare boolean (IsBare), a static (generate-time-known) literal value
// (Static), or — since the dynamic-base-value relaxation — a Go expression
// string (DynExpr) that genValueExpr already proved evaluates, at RUNTIME, to
// the same unescaped text Runtime.renderTag's own non-spread render branch
// would compute for that attribute. genSpreadBaseLiteral renders Static as a
// quoted Go string literal and DynExpr as a raw, UNQUOTED Go expression —
// spliced directly into the generated map[string]*gopug.AttributeValue
// composite literal, so it is evaluated exactly once, at the point that
// literal is constructed, just like any other dynamic attribute value this
// generator emits.
type spreadBaseAttr struct {
	IsBare  bool
	Static  string
	DynExpr string
}

// genSpreadBase builds base — a &attributes tag's own attributes excluding
// the &attributes entry itself, checked for the shapes genSpreadAttrs's
// field/variable path and genSpreadAttrsInlineObject's inline-object path
// both require: a bare boolean, a static quoted-string literal (whose
// "class" value, if any, has no leading/trailing or repeated internal
// whitespace), or — for any name other than "class" and any name
// isBooleanAttribute does not recognize — a DYNAMIC value-expr that
// genValueExpr can build and proves NON-FALLIBLE. Shared by both callers so
// the base-attribute rules cannot drift between the two spread-source
// shapes.
//
// The dynamic case relies on a fact confirmed by direct probe against
// Runtime.renderTag: a base attribute NOT touched by the runtime spread is
// rendered through renderTag's own general (non-spread) branch — for any
// name other than "class"/"style", that is exactly `evaluated,
// _ = r.evaluateExpr(val.Value)` followed by `htmlEscapeAttr(evaluated)` —
// the SAME evaluate-then-escape-once pipeline genAttributes's own dynamic
// non-"class" case already reproduces via genValueExpr + EscapeAttr for an
// ordinary tag. So folding genValueExpr(expr)'s result into base[name].Value
// (raw, unescaped) and letting gopug.WriteSpreadAttrs's shared core
// EscapeAttr it once, exactly as it already does for a static base literal,
// produces byte-identical output whether or not the runtime spread later
// overwrites that key: if it doesn't, both sides render the SAME evaluated
// value; if it does, both sides discard the base entirely and render the
// spread's own value instead (Runtime.renderTag never evaluates an
// overwritten base at all — see spreadFinal — and WriteSpreadAttrs's own
// merge unconditionally overwrites a non-"class" key the spread supplies).
//
// Three shapes are excluded from the dynamic case and DEFERRED instead, each
// confirmed unsafe by direct probe rather than assumed:
//
//   - "class" — Runtime.renderTag's spread-merge reads a base "class" as the
//     RAW, UNEVALUATED source text of merged["class"].Value (see the
//     spreadFinal-building loop), not its evaluated value, whenever the
//     runtime spread itself supplies a "class" key; a probe confirms this
//     produces genuinely different (and, for a dynamic base, nonsensical —
//     the literal identifier text, not the field's value) output than the
//     non-touched branch's own evaluated render. Since whether the runtime
//     spread supplies "class" is not knowable at generate time, a dynamic
//     class base can never be proven safe either way and stays deferred
//     unconditionally (unchanged from before this relaxation).
//   - a name isBooleanAttribute recognizes ("disabled", "checked", …) — the
//     non-spread branch additionally omits the attribute ENTIRELY whenever
//     its evaluated value stringifies to exactly "false" (Runtime.renderTag,
//     just below the render loop's evaluation step) — a rule that applies to
//     ANY dynamic value on such a name, not only a bool-typed field (probed
//     directly: a plain STRING field holding "false" is omitted the same
//     way). gopug.WriteSpreadAttrs's shared core has no such omission rule at
//     all for a base entry, so a dynamic value on one of these names is never
//     provably byte-identical and stays deferred.
//   - a FALLIBLE expression (genValueExpr's own fallible flag — a top-level
//     `/` or `%`) — this generator still has to CALL genValueExpr to build
//     the base map even when the runtime spread will end up overwriting that
//     very key, but Runtime.renderTag's non-touched branch never evaluates an
//     overwritten base at all (spreadFinal short-circuits it). A pure
//     (non-fallible) expression is harmless to evaluate and discard either
//     way — same value, or an unused value — but a fallible one is NOT: a
//     probe confirms the interpreter, when a fallible base IS overwritten,
//     renders the spread's value with NO error, having never touched the
//     fallible base at all, while codegen — unable to know at generate time
//     whether a given render call's runtime spread will overwrite the key —
//     would either need to guess or would eagerly evaluate (and error on) an
//     expression the interpreter never runs. Deferred unconditionally rather
//     than risk that divergence.
func (g *generator) genSpreadBase(tag *TagNode, expr string) (map[string]*spreadBaseAttr, error) {
	base := make(map[string]*spreadBaseAttr, len(tag.Attributes))
	for name, val := range tag.Attributes {
		if name == "&attributes" {
			continue
		}
		if val.Unescaped {
			return nil, fmt.Errorf("unsupported &attributes(%s) in codegen: base attribute %q is unescaped, which is not supported for a runtime spread", expr, name)
		}
		if val.IsBare {
			base[name] = &spreadBaseAttr{IsBare: true}
			continue
		}

		trimmed := strings.TrimSpace(val.Value)
		if lit, ok := unwrapQuotedLiteral(trimmed); ok {
			if name == "class" && lit != strings.Join(strings.Fields(lit), " ") {
				// A base class literal with leading/trailing or 2+ internal
				// spaces is only byte-identical to the interpreter when the
				// spread ITSELF touches "class" (both sides then go through the
				// shared mergeSpreadClass, which leaves whitespace untouched).
				// When the spread has no "class" key at all, the interpreter's
				// base class instead falls through to its ordinary (non-spread)
				// render path, resolveClassTokenList, which Fields-collapses
				// runs of whitespace and trims the ends — a transformation this
				// generator has no way to know at generate time whether the
				// RUNTIME spread map will or won't supply a "class" key, so it
				// cannot decide which of the two behaviors to reproduce. Rather
				// than guess (and risk emitting the raw, uncollapsed literal
				// when the interpreter would have collapsed it), defer this
				// shape entirely; a base class with no irregular whitespace is
				// unaffected either way and stays supported.
				return nil, fmt.Errorf("unsupported &attributes(%s) in codegen: base attribute \"class\" has leading/trailing or repeated internal whitespace, which is not supported for a runtime spread (its rendering depends on whether the runtime spread map supplies a \"class\" key, which is not knowable at generate time)", expr)
			}
			base[name] = &spreadBaseAttr{Static: lit}
			continue
		}

		dynamicUnsupportedErr := fmt.Errorf("unsupported &attributes(%s) in codegen: base attribute %q is dynamic, which is not supported for a runtime spread (only a static quoted-string or bare boolean base attribute is supported)", expr, name)

		if name == "class" {
			return nil, dynamicUnsupportedErr
		}
		if isBooleanAttribute(name) {
			return nil, fmt.Errorf("unsupported &attributes(%s) in codegen: base attribute %q is an HTML boolean attribute name with a dynamic value, which is not supported for a runtime spread (the interpreter's omit-on-\"false\" rule for these names is not reproduced by a runtime spread's base rendering)", expr, name)
		}

		goExpr, fallible, verr := g.genValueExpr(trimmed)
		if verr != nil {
			return nil, dynamicUnsupportedErr
		}
		if fallible {
			return nil, fmt.Errorf("unsupported &attributes(%s) in codegen: base attribute %q is a fallible expression (a division or modulo operator), which is not supported for a runtime spread (a fallible base can only be safely evaluated at generate time when the runtime spread never overwrites it, which is not knowable at generate time)", expr, name)
		}
		base[name] = &spreadBaseAttr{DynExpr: goExpr}
	}
	return base, nil
}

// genSpreadAttrsInlineObject handles a `&attributes({...})` tag whose spread
// source is an inline object literal, reached from genSpreadAttrs once it
// has confirmed expr looks like `{...}` (the same detection Runtime.
// renderTag itself uses at its own inline-object branch). Because
// parseInlineObject never evaluates a value — it only trims whitespace and
// strips a matching pair of surrounding quotes from the key and the value —
// the object's ENTIRE contents are fixed template source text, so calling
// parseInlineObject(expr) here, at GENERATE time, yields the exact same
// map[string]string Runtime.renderTag's own runtime call to parseInlineObject
// would build for the identical expr at RENDER time: there is no template
// data involved in the parse at all, so gen time and render time can never
// disagree. That static map is emitted as a Go map[string]string composite
// literal (genInlineObjectLiteral, sorted for reproducible codegen output)
// and handed to the exact same gopug.WriteSpreadAttrs entry point
// genSpreadAttrs's own field/variable map[string]string path already calls —
// no new render logic is introduced, so the merge/sort/escape behavior is
// byte-identical to the interpreter by construction, for the same reason the
// field/variable path already is.
func (g *generator) genSpreadAttrsInlineObject(tag *TagNode, expr string) error {
	base, err := g.genSpreadBase(tag, expr)
	if err != nil {
		return err
	}

	obj := parseInlineObject(expr)

	g.needsGopug = true
	g.writeRaw("if err := gopug.WriteSpreadAttrs(w, " + genSpreadBaseLiteral(base) + ", " + genInlineObjectLiteral(obj) + "); err != nil {\nreturn err\n}\n")
	return nil
}

// genInlineObjectLiteral renders obj — the map[string]string
// genSpreadAttrsInlineObject parsed from an inline object literal's source
// text at generate time — as a Go map[string]string composite literal, in a
// deterministic (sorted) key order so GenerateGo's output is reproducible
// across runs; runtime iteration order has no effect on the rendered result
// since gopug.WriteSpreadAttrs sorts the merged attribute set itself.
func genInlineObjectLiteral(obj map[string]string) string {
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	slices.Sort(names)

	var b strings.Builder
	b.WriteString("map[string]string{")
	for i, name := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(name))
		b.WriteString(": ")
		b.WriteString(strconv.Quote(obj[name]))
	}
	b.WriteString("}")
	return b.String()
}

// genSpreadBaseLiteral renders base — a &attributes tag's own bare/
// static-literal/dynamic-value-expr attributes, built by genSpreadAttrs — as
// a Go composite literal of type map[string]*gopug.AttributeValue, in a
// deterministic (sorted) key order so GenerateGo's output is reproducible
// across runs. A Static entry is emitted as a quoted Go string literal; a
// DynExpr entry is spliced in as a raw, UNQUOTED Go expression — it IS the Go
// source computing the value at runtime, so quoting it would turn working
// code into a literal string of source text.
func genSpreadBaseLiteral(base map[string]*spreadBaseAttr) string {
	names := make([]string, 0, len(base))
	for name := range base {
		names = append(names, name)
	}
	slices.Sort(names)

	var b strings.Builder
	b.WriteString("map[string]*gopug.AttributeValue{")
	for i, name := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		val := base[name]
		b.WriteString(strconv.Quote(name))
		b.WriteString(": ")
		switch {
		case val.IsBare:
			b.WriteString("{IsBare: true}")
		case val.DynExpr != "":
			b.WriteString("{Value: " + val.DynExpr + "}")
		default:
			b.WriteString("{Value: " + strconv.Quote(val.Static) + "}")
		}
	}
	b.WriteString("}")
	return b.String()
}

// staticCallAttrValue reduces a mixin call's own attribute value
// (`+foo(class="x", disabled, hidden=false)`) to the exact string
// Runtime.renderMixinCall's own attribute map would hold for it (attrMap[k],
// built from `v.IsBare ? "true" : evaluateExpr(v.Value)`), for the STATIC
// subset this slice supports: a bare attribute (no "="), an unquoted `true`/
// `false` keyword value, or a plain quoted-string literal value. ok is false
// for anything else (a dynamic/expression-valued attribute, an unescaped
// attribute, or a quoted literal containing a backslash or embedded quote —
// deliberately out of scope, since reproducing evaluateExpr's own escape-
// sequence handling for the merged, re-quoted value genAttributes will later
// re-parse is a distinct, untested claim this slice does not make).
func staticCallAttrValue(v *AttributeValue) (string, bool) {
	if v.Unescaped {
		return "", false
	}
	if v.IsBare {
		return "true", true
	}
	trimmed := strings.TrimSpace(v.Value)
	if trimmed == "true" || trimmed == "false" {
		return trimmed, true
	}
	lit, ok := unwrapQuotedLiteral(trimmed)
	if !ok || strings.ContainsAny(lit, `"\`) {
		return "", false
	}
	return lit, true
}

// genDynamicClass emits a dynamic class="..." attribute for a value
// genAttributes has already determined is not a pure static literal —
// parseAttributes's merge contract (parser.go's class-merge branch) means
// that value is either a bare dynamic token on its own (`div(class=cls)`),
// shorthand class tokens, individually quoted, followed by one bare
// dynamic token (`"text-end" cls`, `"btn" "large" variant`), a class
// object literal optionally preceded by a static shorthand prefix
// (`div.card(class={active: isActive})` -> trimmed `card {active: isActive}`
// — see genDynamicClassObject), or a fully-parenthesized whole-value
// ternary/operator expression (`div(class=(isActive ? "active" : ""))`,
// `div(class=(prefix + "-btn"))` — see the isFullyParenthesizedExpr branch
// below). The object-literal form is detected the same way Runtime.renderTag's
// own class branch detects it: the first `{` in trimmed, provided trimmed's
// last byte is `}`, marking everything from that `{` onward as the object
// and everything before it (trimmed) as a static prefix. The
// fully-parenthesized form is detected the same way, structurally: trimmed's
// first byte is `(` and the `)` that closes it sits at trimmed's very last
// byte — a shape that can never carry a leading static-class prefix (a
// prefix would put a non-`(` byte first), which is what makes it safe to
// hand the whole value straight to genValueExpr rather than reproducing the
// interpreter's static-prefix-mixed heuristic (runtime.go's
// containsParenExpr/isOperatorExpr word-splitting fallback) — that mixed
// form stays deferred. Anything else is tokenised with strings.Fields. When
// that leaves exactly one token — a bare dynamic value with no shorthand
// prefix, e.g. `div(class=tags)` — and it resolves (via resolveFieldExpr) to
// a slice/array field, the whole value is handed to genDynamicClassSlice
// instead, reproducing the interpreter's own reflect.Slice/reflect.Array
// branch (Runtime.renderTag, the containsParenExpr/isOperatorExpr-false
// evaluateExprRaw switch) byte for byte; when it resolves to a map field
// instead, the whole value is handed to genDynamicClassMap, reproducing that
// same switch's reflect.Map branch — rather than its resolveClassTokenList
// string-only fallback. A multi-token value (a shorthand prefix merged with
// a dynamic slice/map field, e.g. `div.card(class=tags)` -> trimmed
// `card tags`) is NOT reproduced here even though the interpreter itself
// resolves it (through resolveClassTokenList's own per-token flatten, a
// different code path) — that stays deferred, since this increment only
// proves the single-token evaluateExprRaw branch byte-identical, not
// resolveClassTokenList's. Every other token is classified as a static
// quoted literal or a bare identifier/dot-path resolving to a string field,
// then emitted as a single runtime write joining every token's Go
// expression through the exported JoinClasses (dropping an empty token,
// matching Runtime.renderTag's empty-token rule exactly) and escaping the
// joined result through EscapeAttr. An UNparenthesized ternary/operator
// class expression (caught by isOperatorExpr, since a fully-parenthesized
// value's operators sit at depth>0 and never trip it), a class array-literal
// value, or a token that isn't a static literal, a string-field reference,
// or (single-token only) a slice/array/map-field reference is out of scope
// for this increment and returns an error instead of guessing at output the
// interpreter might not produce; so does a nil Config.DataReflectType, since
// without type information neither a bare class token nor a parenthesized
// expression can be resolved (nor a class-object entry's value, which is
// compiled by genCondition — see genDynamicClassObject — and genCondition
// itself requires type information for most of its own supported shapes).
func (g *generator) genDynamicClass(trimmed string) error {
	if g.rootType == nil {
		return fmt.Errorf("unsupported dynamic class attribute in codegen (only static quoted values are supported without type information)")
	}

	if isFullyParenthesizedExpr(trimmed) {
		goExpr, fallible, err := g.genValueExpr(trimmed)
		if err != nil {
			return fmt.Errorf("unsupported dynamic class attribute in codegen (a parenthesized class expression genValueExpr cannot build: %w)", err)
		}
		if fallible {
			return fmt.Errorf("unsupported dynamic class attribute in codegen (a parenthesized class expression with a fallible inner value is not supported)")
		}
		g.needsGopug = true
		g.writeStatic(` class="`)
		g.writeExprWrite("gopug.EscapeAttr(" + goExpr + ")")
		g.writeStatic(`"`)
		return nil
	}

	if isOperatorExpr(trimmed) {
		return fmt.Errorf("unsupported dynamic class attribute in codegen (a ternary/operator class expression is not yet supported)")
	}

	if classObjStart := strings.IndexByte(trimmed, '{'); classObjStart >= 0 && strings.HasSuffix(trimmed, "}") {
		return g.genDynamicClassObject(trimmed, classObjStart)
	}

	words := strings.Fields(trimmed)
	for _, w := range words {
		if strings.HasPrefix(w, "{") || strings.HasPrefix(w, "[") {
			return fmt.Errorf("unsupported dynamic class attribute in codegen (a class object/array value is not yet supported)")
		}
	}

	if len(words) == 1 {
		if _, ok := unwrapQuotedLiteral(words[0]); !ok {
			if shape, _ := classifySimpleShape(words[0]); shape == shapeIdentifier || shape == shapeDotPath {
				if goExpr, typ, err := g.resolveFieldExpr(words[0]); err == nil && typ != nil {
					switch typ.Kind() {
					case reflect.Slice, reflect.Array:
						return g.genDynamicClassSlice(goExpr)
					case reflect.Map:
						return g.genDynamicClassMap(goExpr)
					}
				}
			}
		}
	}

	args := make([]string, 0, len(words))
	for _, w := range words {
		if lit, ok := unwrapQuotedLiteral(w); ok {
			args = append(args, strconv.Quote(lit))
			continue
		}

		shape, _ := classifySimpleShape(w)
		if shape != shapeIdentifier && shape != shapeDotPath {
			return fmt.Errorf("unsupported dynamic class attribute in codegen (class token %q is not a static literal or a plain field reference)", w)
		}
		goExpr, typ, err := g.resolveFieldExpr(w)
		if err != nil {
			return err
		}
		if typ == nil || typ.Kind() != reflect.String {
			return fmt.Errorf("unsupported dynamic class attribute in codegen (class token %q must resolve to a string field)", w)
		}
		args = append(args, convertExpr(goExpr, typ, reflectTypeString, "string"))
	}

	g.needsGopug = true
	g.writeStatic(` class="`)
	g.writeExprWrite("gopug.EscapeAttr(gopug.JoinClasses(" + strings.Join(args, ", ") + "))")
	g.writeStatic(`"`)
	return nil
}

// genDynamicClassSlice emits a dynamic class="..." attribute for the single-
// token case genDynamicClass hands off when the whole class value is one
// bare field resolving to a slice/array (`div(class=tags)`), reproducing
// Runtime.renderTag's own reflect.Slice/reflect.Array branch
// (runtime.go, the evaluateExprRaw switch) byte for byte: goExpr is the Go
// expression (already resolved by resolveFieldExpr) for the slice/array
// field itself, of its own concrete element type. A range loop builds a
// []string of each element's fmt.Sprintf("%v", …) form — the same
// per-element stringify genJoinValueExpr already uses for `.join`, which
// matches the interpreter's own fmt.Sprintf("%v", rv.Index(i).Interface())
// exactly because both apply the identical %v verb to the identical
// concrete element type. The parts are joined with strings.Join, NOT the
// exported gopug.JoinClasses helper used by the multi-token path above:
// Runtime.renderTag's slice branch calls strings.Join directly, which does
// not drop an empty-string element, so a `["a", "", "b"]` slice must render
// as `class="a  b"` (a doubled interior space) to match — JoinClasses would
// wrongly collapse it. A nil or empty slice/array produces an empty parts
// slice, so strings.Join yields "" and the attribute renders as `class=""`,
// matching the interpreter's rv.Len()==0 case. The joined string is escaped
// through gopug.EscapeAttr, mirroring htmlEscapeAttr at the interpreter's
// own class-attribute write site.
func (g *generator) genDynamicClassSlice(goExpr string) error {
	g.needsGopug = true
	g.needsStrings = true
	g.needsFmt = true

	n := g.nextTmp()
	partsVar := fmt.Sprintf("__clsParts%d", n)
	elemVar := fmt.Sprintf("__clsElem%d", n)

	var b strings.Builder
	fmt.Fprintf(&b, "var %s []string\n", partsVar)
	fmt.Fprintf(&b, "for _, %s := range %s {\n", elemVar, goExpr)
	fmt.Fprintf(&b, "%s = append(%s, fmt.Sprintf(\"%%v\", %s))\n", partsVar, partsVar, elemVar)
	b.WriteString("}\n")
	g.writeRaw(b.String())

	g.writeStatic(` class="`)
	g.writeExprWrite("gopug.EscapeAttr(strings.Join(" + partsVar + `, " "))`)
	g.writeStatic(`"`)
	return nil
}

// genDynamicClassMap emits a dynamic class="..." attribute for the single-
// token case genDynamicClass hands off when the whole class value is one
// bare field resolving to a map (`div(class=classMap)`), reproducing
// Runtime.renderTag's own reflect.Map branch (runtime.go, the
// evaluateExprRaw switch) byte for byte: goExpr is the Go expression
// (already resolved by resolveFieldExpr) for the map field itself, of its
// own concrete key/value types. A range loop visits every entry, keeping
// only those whose VALUE's fmt.Sprintf("%v", …) form is truthy under
// gopug.Truthy — the exported twin of the interpreter's own isTruthy, so the
// filter is identical bit for bit — and, for each kept entry, appending its
// KEY's fmt.Sprintf("%v", …) form to a []string. This mirrors the
// interpreter's own mvStr := fmt.Sprintf("%v", mv); if r.isTruthy(mvStr) {
// activeClasses = append(activeClasses, fmt.Sprintf("%v", mk.Interface())) }
// exactly, because both apply the identical %v verb to the identical
// concrete key/value types. Map iteration order is unspecified, and a Go
// map's key order isn't known until runtime regardless of key type, so the
// collected keys are sorted with a genuine runtime sort.Strings call (the
// first one generated code ever needs — see the needsSort field), matching
// the interpreter's own sort.Strings(activeClasses) exactly, including for
// a non-string key type: both sort the keys' STRING form, not their
// underlying numeric/other order (so int keys 1, 2, 10 sort as "1", "10",
// "2", not 1, 2, 10). The sorted parts are joined with strings.Join, NOT the
// exported gopug.JoinClasses helper used by the multi-token path above:
// Runtime.renderTag's map branch calls strings.Join directly, which does
// not drop an empty-string element, so a truthy key that stringifies to ""
// must still contribute an empty token — surfacing as a leading space once
// joined with any other kept key — to match; JoinClasses would wrongly drop
// it. A nil or empty map produces an empty parts slice, so sort.Strings is a
// no-op and strings.Join yields "", rendering `class=""`, matching the
// interpreter's own empty rv.MapKeys() case. The joined string is escaped
// through gopug.EscapeAttr, mirroring htmlEscapeAttr at the interpreter's
// own class-attribute write site.
func (g *generator) genDynamicClassMap(goExpr string) error {
	g.needsGopug = true
	g.needsStrings = true
	g.needsFmt = true
	g.needsSort = true

	n := g.nextTmp()
	partsVar := fmt.Sprintf("__clsMapKeys%d", n)
	keyVar := fmt.Sprintf("__clsMapKey%d", n)
	valVar := fmt.Sprintf("__clsMapVal%d", n)

	var b strings.Builder
	fmt.Fprintf(&b, "var %s []string\n", partsVar)
	fmt.Fprintf(&b, "for %s, %s := range %s {\n", keyVar, valVar, goExpr)
	fmt.Fprintf(&b, "if gopug.Truthy(fmt.Sprintf(\"%%v\", %s)) {\n", valVar)
	fmt.Fprintf(&b, "%s = append(%s, fmt.Sprintf(\"%%v\", %s))\n", partsVar, partsVar, keyVar)
	b.WriteString("}\n")
	b.WriteString("}\n")
	fmt.Fprintf(&b, "sort.Strings(%s)\n", partsVar)
	g.writeRaw(b.String())

	g.writeStatic(` class="`)
	g.writeExprWrite("gopug.EscapeAttr(strings.Join(" + partsVar + `, " "))`)
	g.writeStatic(`"`)
	return nil
}

// genDynamicClassObject emits a dynamic class="..." attribute for a class
// value containing an object literal (`div.card(class={active: isActive})`),
// reproducing Runtime.renderTag's own class-object branch (runtime.go, the
// `classObjStart` block) byte for byte. trimmed is genDynamicClass's whole
// class value; classObjStart is the index of trimmed's first `{`, already
// confirmed (by the caller) to start an object literal running through
// trimmed's last byte, `}` — exactly the same detection the interpreter's
// own loop-and-check performs on rawValExpr at render time.
//
// parseInlineObject never evaluates anything — it only splits the object's
// source text on top-level commas and colons and strips a matching pair of
// surrounding quotes from each key/value — so calling it here, at GENERATE
// time, on trimmed[classObjStart:] yields the exact same map[string]string
// Runtime.renderTag's own runtime call would build for the identical source
// text: there is no template data involved in the parse itself, only in
// each entry's VALUE once it's handed to evaluateExpr. That per-value
// evaluation is where genCondition comes in: the interpreter's per-entry
// test is `isTruthy(evaluateExpr(v))`, which is exactly what genCondition
// already reproduces as a native Go bool for every condition shape it
// supports (see genCondition's own doc comment) — the identical machinery
// `if`/`unless`/`case` conditions already compile through, so an object
// entry's truthiness is byte-identical to the interpreter's by
// construction, not by a second, separately-proven implementation. Any
// entry value genCondition can't compile (a fallible expression like `a/b`,
// an unresolvable bare word, a nested object/array, …) defers the WHOLE
// class-object attribute instead of guessing: the interpreter drops such an
// entry's key silently (evaluateExpr errors, evaled=="", isTruthy("")==
// false) and keeps rendering, a fallback a generated function that must
// either compile the value or refuse to compile at all cannot reproduce.
//
// Every key is fixed template source text, known at GENERATE time, so the
// keys are sorted with slices.Sort here — at generate time — and their `if`
// checks are emitted in that sorted order; the []string a passing entry's
// key is appended to is therefore built already in sorted order, matching
// Runtime.renderTag's own sort.Strings(activeClasses) exactly, with no
// runtime sort needed. The appended keys are joined with strings.Join (NOT
// the exported JoinClasses, which drops empty elements and would erase the
// interpreter's own trailing-space-when-empty quirk below), and a static
// prefix (trimmed[:classObjStart], then, if non-empty, a literal space) is
// prepended exactly the way Runtime.renderTag's own
// `evaluated = prefix + " " + evaluated` does, INCLUDING when the object
// itself evaluates to no active classes at all (yielding a lone trailing
// space after the prefix, e.g. `class="card "`, not `class="card"`). The
// joined-and-prefixed result is escaped through the exported EscapeAttr —
// the class-object form is not exempt from attribute escaping, mirroring
// runtime.go's own htmlEscapeAttr(evaluated) call applied to this same
// value.
func (g *generator) genDynamicClassObject(trimmed string, classObjStart int) error {
	objStr := trimmed[classObjStart:]
	obj := parseInlineObject(objStr)

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	conds := make([]string, len(keys))
	for i, k := range keys {
		cond, err := g.genCondition(obj[k])
		if err != nil {
			return fmt.Errorf("unsupported dynamic class object entry %q in codegen: %w", k, err)
		}
		conds[i] = cond
	}

	prefix := ""
	if classObjStart > 0 {
		prefix = strings.TrimSpace(trimmed[:classObjStart])
	}

	clsTmp := fmt.Sprintf("__cls%d", g.nextTmp())
	strTmp := fmt.Sprintf("__clsStr%d", g.nextTmp())

	g.writeRaw(fmt.Sprintf("var %s []string\n", clsTmp))
	for i, k := range keys {
		g.body.WriteString(fmt.Sprintf("if %s {\n%s = append(%s, %s)\n}\n", conds[i], clsTmp, clsTmp, strconv.Quote(k)))
	}
	g.needsStrings = true
	g.body.WriteString(fmt.Sprintf("%s := strings.Join(%s, \" \")\n", strTmp, clsTmp))
	if prefix != "" {
		g.body.WriteString(fmt.Sprintf("%s = %s + %s\n", strTmp, strconv.Quote(prefix+" "), strTmp))
	}

	g.needsGopug = true
	g.writeStatic(` class="`)
	g.writeExprWrite("gopug.EscapeAttr(" + strTmp + ")")
	g.writeStatic(`"`)
	return nil
}

// genInterpolation emits a write of an interpolation, either the default
// escaped `#{expr}` or, when n.Unescaped is set, `!{expr}`. The value is
// built by genValueExpr — a bare field/dot-path or an expression built from
// string/numeric/bool literals and the supported operators — the same way
// for both forms, since Runtime.renderInterpolation itself computes the
// identical value for either and differs only in whether it wraps that value
// in html.EscapeString (runtime.go's `if !interp.Unescaped` branch). The
// escaped form wraps the value in html.EscapeString and sets g.needsHTML so
// the "html" import is emitted; the unescaped form writes the value directly,
// with no wrapper and no needsHTML — it introduces no use of the "html"
// package, so a template using only unescaped output must not pull in an
// unused import.
func (g *generator) genInterpolation(n *InterpolationNode) error {
	valExpr, fallible, err := g.genValueExpr(n.Expression)
	if err != nil {
		return err
	}
	if fallible {
		valExpr = g.genFallibleExtraction(valExpr)
	}
	if n.Unescaped {
		g.writeExprWrite(valExpr)
		return nil
	}
	g.needsHTML = true
	g.writeExprWrite("html.EscapeString(" + valExpr + ")")
	return nil
}

// genCode emits a buffered code node, either the default escaped `= expr`
// (CodeBuffered) or the unescaped `!= expr` (CodeUnescaped), as a write of
// genValueExpr(expr) — wrapped in html.EscapeString for CodeBuffered, written
// raw for CodeUnescaped — mirroring Runtime.renderCode's own two cases
// exactly (runtime.go: CodeBuffered writes html.EscapeString(val), CodeUnescaped
// writes val raw; both compute val the same way via evaluateCode). Only
// CodeBuffered sets g.needsHTML, since only it emits an html.EscapeString
// call; CodeUnescaped introduces no use of the "html" package. An unbuffered
// statement (`- stmt`, executed for its side effect with no output) is
// dispatched to genUnbufferedStatement, which supports only the
// plain-assignment subset (`- var x = <rhs>`) described there; every other
// unbuffered shape (mutation, a bare expression statement) still returns a
// clear unsupported error.
func (g *generator) genCode(n *CodeNode) error {
	switch n.Type {
	case CodeBuffered:
		valExpr, fallible, err := g.genValueExpr(n.Expression)
		if err != nil {
			return err
		}
		if fallible {
			valExpr = g.genFallibleExtraction(valExpr)
		}
		g.needsHTML = true
		g.writeExprWrite("html.EscapeString(" + valExpr + ")")
		return nil
	case CodeUnescaped:
		valExpr, fallible, err := g.genValueExpr(n.Expression)
		if err != nil {
			return err
		}
		if fallible {
			valExpr = g.genFallibleExtraction(valExpr)
		}
		g.writeExprWrite(valExpr)
		return nil
	default:
		return g.genUnbufferedStatement(n.Expression)
	}
}

// genUnbufferedStatement emits an unbuffered code statement (`- stmt`),
// classified by classifyUnbufferedStmt — the SAME classifier
// Runtime.executeStatement itself uses, so codegen and the interpreter agree
// character-for-character on where a statement's operator sits. A plain
// assignment (`- var x = <rhs>`) is dispatched to genUnbufferedAssign; a
// mutation (`x++`/`x--`/`x += e`/`x -= e`) is dispatched to
// genUnbufferedMutation, which supports only the SAFE subset documented
// there (a float64 `- var` local already bound in scope, with a numeric
// `+=`/`-=` right-hand side) and defers every other shape; a bare expression
// statement (evaluated and discarded by the interpreter, possibly for a side
// effect or an error genValueExpr has no model for) returns its own clear,
// distinct unsupported error instead of guessing.
//
// While g.insideBlockClosure is set (generating a call site's block-content
// closure — see genMixinBlockClosure), EVERY unbuffered statement is refused
// unconditionally, even a literal-only assignment a later reference inside
// the same closure would resolve purely against its own local scope entry
// (never touching resolveFieldExpr's paramOnlyScope guard at all, since a
// scope-stack HIT is returned before that guard is ever reached — this was
// confirmed to generate valid, byte-identical Go for the narrow literal-RHS
// case during this feature's own development). That narrower case is
// deliberately NOT admitted here: this increment's contract for block
// content is "pure static markup, no identifier resolution of any kind", and
// widening it to "some `- var` locals are fine, if self-contained" is a
// distinct claim this increment does not make or test — a later increment's
// job, not an accidental side effect of this one.
func (g *generator) genUnbufferedStatement(stmt string) error {
	if g.insideBlockClosure {
		return fmt.Errorf("unsupported unbuffered code `- %s` in codegen: an unbuffered statement inside block content passed to a mixin call is not supported yet (block content is restricted to static markup in this increment)", stmt)
	}

	kind, varName, rhsExpr := classifyUnbufferedStmt(stmt)

	switch kind {
	case unbufferedAssign:
		return g.genUnbufferedAssign(varName, rhsExpr)
	case unbufferedIncrement, unbufferedDecrement, unbufferedAddAssign, unbufferedSubAssign:
		return g.genUnbufferedMutation(kind, varName, rhsExpr, stmt)
	default:
		return fmt.Errorf("unsupported unbuffered code - %s in codegen (a bare expression statement is not supported yet)", stmt)
	}
}

// genUnbufferedMutation emits `x++`/`x--`/`x += rhs`/`x -= rhs` for the one
// SAFE subset this slice proves byte-identical to Runtime.executeStatement:
// varName already bound in scope as a float64 `- var` local (isVarLocal,
// scopeVar.typ == reflectTypeFloat64), with — for `+=`/`-=` — a numeric
// right-hand side genNumericExpr itself accepts (a bare numeric literal or a
// bare numeric field/local, never an arithmetic expression). Everything
// else is deferred with a clean, distinct error rather than guessed at:
//
//   - varName not found in scope at all (lookupScope misses): this covers
//     both a genuinely undefined variable and a bare struct-field name
//     (neither is ever pushed onto the scope stack). Runtime.executeStatement
//     itself does something surprising here — its increment/decrement case
//     calls setVar unconditionally, which silently CREATES the variable at 0
//     in the top scope frame rather than erroring (confirmed empirically: `-
//     y++` with no prior `- y =` renders "1", no error) — so deferring on a
//     scope miss is not merely conservative, it is the ONLY choice that
//     avoids either an invalid Go reference to an undeclared identifier or a
//     silent mis-render that disagrees with this surprising create-on-miss
//     behavior.
//   - varName found but not a `- var` local (an each-loop item/index
//     variable): those are never reassignable in the interpreter's own
//     model either (each iteration rebinds them fresh), so mutating one is
//     out of scope for this slice.
//   - varName found as a `- var` local of any type OTHER than float64
//     (string, bool, or — in type-blind mode — untyped): a Go string/bool
//     local has no `++`/`+=`, and type-blind mode has no type information to
//     even prove the local is numeric.
//   - `+=`/`-=` whose right-hand side genNumericExpr does not accept: the
//     interpreter's own executeStatement branches into STRING CONCATENATION
//     for a non-numeric `+=` right-hand side, and into a hard ERROR for a
//     non-numeric `-=` right-hand side — two different behaviors, neither of
//     which this numeric-only slice attempts to replicate.
//
// A found float64 `- var` local, regardless of which scope frame pushed it
// (the current block or an ENCLOSING one), is exactly as safe to mutate as
// Runtime.setVar's own frame-walk: Go's lexical scoping mutating an outer
// `:=` local from inside a nested block/loop produces the identical
// observable effect (every subsequent read, in every sibling/nested scope,
// sees the mutated value) as setVar updating the innermost frame that
// actually holds the variable — there is no case where a found local's
// scope depth changes which Go statement gets emitted.
func (g *generator) genUnbufferedMutation(kind unbufferedStmtKind, varName, rhsExpr, stmt string) error {
	sv, ok := g.lookupScope(varName)
	if !ok {
		return fmt.Errorf("unsupported unbuffered mutation %q in codegen (mutating %q, which is not a variable bound in codegen scope, is not supported yet)", stmt, varName)
	}
	if !sv.isVarLocal {
		return fmt.Errorf("unsupported unbuffered mutation %q in codegen (mutating %q, an each-loop item/index variable rather than a `- var` local, is not supported yet)", stmt, varName)
	}
	if sv.typ == nil || sv.typ != reflectTypeFloat64 {
		return fmt.Errorf("unsupported unbuffered mutation %q in codegen (mutation is only supported for a float64 `- var` local in this increment)", stmt)
	}

	switch kind {
	case unbufferedIncrement:
		g.writeRaw(sv.goName + "++\n")
		return nil
	case unbufferedDecrement:
		g.writeRaw(sv.goName + "--\n")
		return nil
	}

	numExpr, numTyp, numOK := g.genNumericExpr(rhsExpr)
	if !numOK {
		return fmt.Errorf("unsupported unbuffered mutation %q in codegen (a `+=`/`-=` with a non-numeric or non-bare right-hand side is not supported in this increment)", stmt)
	}
	rhsFloat := convertExpr(numExpr, numTyp, reflectTypeFloat64, "float64")

	switch kind {
	case unbufferedAddAssign:
		g.writeRaw(sv.goName + " += " + rhsFloat + "\n")
	case unbufferedSubAssign:
		g.writeRaw(sv.goName + " -= " + rhsFloat + "\n")
	}
	return nil
}

// goLocalNameForVar derives the Go local variable name a `- var name = rhs`
// unbuffered assignment binds to: a fixed "__v_" prefix followed by name
// itself. The prefix guarantees the emitted local can never collide with the
// generated function's own receiver ("d"), writer ("w"), the "gopug" import
// alias, or any of the generator's own tmp-name families (__vN, __errN,
// __iN, __eN, __lN, __okN, all of which have a bare digit immediately after
// the letter, never an underscore) — so a Pug var literally named "data" or
// "d" still can never shadow the struct receiver.
func goLocalNameForVar(name string) string {
	return "__v_" + name
}

// validGoIdentifier reports whether name is a valid Go identifier: an
// underscore or ASCII letter followed by any number of underscores, ASCII
// letters, or digits. A Pug `- var` target normally already satisfies this
// (Pug identifiers share Go's basic identifier grammar, minus JS's leading
// "$"), but genUnbufferedAssign checks it explicitly rather than assume, so
// a stray character produces a clean deferral instead of malformed Go source.
func validGoIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		isLetter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if i > 0 && !isLetter && !isDigit {
			return false
		}
	}
	return true
}

// genUnbufferedAssign emits a `- var x = <rhs>` unbuffered assignment
// (varName/rhsExpr already split by classifyUnbufferedStmt) as a Go string
// local, then registers it in the generator's scope so later references to x
// resolve to that local instead of a struct field.
//
// Support is intentionally narrow — see genAssignRHS for the exact RHS
// grammar — because of the bounded-agreement invariant this whole feature
// rests on: Runtime.executeStatement stores r.evaluateExprRaw(rhs) (a RAW,
// un-stringified value) and only stringifies it later, at each USE site
// (Runtime.lookupAndStringify). Runtime.evaluateExprRaw special-cases very
// few RHS shapes (a top-level ternary, recursing into itself on the taken
// branch; a top-level `.split(...)` call; an inline object/array literal; a
// top-level index expression; a bare identifier/dot-path, resolved via
// Runtime.lookup and returned RAW/un-stringified) — every OTHER shape falls
// through to its own `s, _ := r.evaluateExpr(expr); return s`, meaning the
// "raw" value IS already the plain string genValueExpr independently proves
// equal to r.evaluateExpr(rhs). So a top-level ternary/`||`/`&&`/string
// literal/template literal/`+` concatenation is safe by that fallthrough
// (recursively, for whichever of these shapes each operand/branch is in
// turn) — genAssignRHS supports exactly these. A bare identifier/dot-path
// is the one shape that does NOT fall through: Runtime.lookup returns the
// field's raw Go value directly, so the invariant only holds when that
// value's later stringification (Runtime.lookupAndStringify's per-type
// switch) is byte-identical to genScalarStringify's per-type Go code — true
// for every scalar Go kind lookupAndStringify special-cases (string, bool,
// every sized int/uint kind, float64), by construction: genScalarStringify
// emits exactly those switch cases' own strconv calls. This increment still
// restricts the bare-field/dot-path leaf to a string-typed field only,
// matching the real-world shapes (a computed string attribute value) this
// increment targets; the broader scalar case is provably just as safe (the
// same per-Kind-matching argument above applies unchanged) but is left for a
// later increment rather than widened here. Every other RHS shape
// (`.split(...)`, an index expression, an array/object literal, a numeric
// field, a fallible operator `/`/`%`/`*`/`-`, a method call, …) is deferred.
//
// A second, later increment added a genuinely bool-typed local alongside
// this string-typed one: a comparison, a unary "!", a both-bool-operand
// "||"/"&&", or a bare bool-typed field/local is classified by genBoolExpr
// (tried first, below) and stored as a real Go bool rather than forced
// through this string grammar — see genBoolExpr's own doc comment for the
// full reasoning, in particular why "||"/"&&" needs an extra check that
// genAssignRHS's OWN "||"/"&&" case (immediately below) does not.
//
// A third increment added a genuinely numeric-typed local: a bare numeric
// field/dot-path (of any kind genScalarStringify itself supports) or a bare
// numeric literal is classified by genNumericExpr (tried after genBoolExpr,
// before genAssignRHS) and stored as a Go value of that exact numeric type —
// see genNumericExpr's own doc comment for why this is exactly as safe as
// the string-field case above, and why it must be restricted to a BARE
// field/literal rather than any arithmetic expression (arithmetic stays on
// genAssignRHS's existing STRING path, since Runtime.evaluateExprRaw itself
// only special-cases a bare identifier/dot-path, never an operator
// expression, as a raw un-stringified value).
//
// A `- var` re-declaring/re-assigning a name already bound in scope is
// handled by genUnbufferedReassign below when the bound entry is a `- var`
// local whose type matches the right-hand side's classified type — see that
// function's own doc comment for the full reasoning (in short: a Go `=` to
// the existing local is exactly as safe as Runtime.setVar's own in-place,
// frame-walking overwrite). Every OTHER already-bound case (an each-loop
// item/index variable, or a same-type proof that fails) is deferred there
// instead, with its own distinct error. A nil Config.DataReflectType
// (type-blind mode) is also deferred: there is no type information to prove
// the RHS is string- or bool-shaped at all.
func (g *generator) genUnbufferedAssign(varName, rhsExpr string) error {
	if g.rootType == nil {
		return fmt.Errorf("unsupported unbuffered assignment %q in codegen (Config.DataReflectType is required to type-check a `- var` right-hand side)", varName)
	}
	if !validGoIdentifier(varName) {
		return fmt.Errorf("unsupported unbuffered assignment target %q in codegen (only a bare identifier is supported as a `- var` left-hand side)", varName)
	}
	if sv, found := g.lookupScope(varName); found {
		return g.genUnbufferedReassign(varName, rhsExpr, sv)
	}

	if boolExpr, ok, err := g.genBoolExpr(rhsExpr); err != nil {
		return fmt.Errorf("unbuffered assignment to %q: %w", varName, err)
	} else if ok {
		goName := goLocalNameForVar(varName)
		g.writeRaw(fmt.Sprintf("%s := %s\n", goName, boolExpr))
		// See the matching comment in the string-local path below: the
		// interpreter always evaluates the RHS even when the variable is
		// never read afterward, so this blank-identifier use keeps that
		// evaluation intact without requiring a reference.
		g.body.WriteString(fmt.Sprintf("_ = %s\n", goName))
		g.pushScope(varName, goName, reflectTypeBool, true)
		return nil
	}

	if numExpr, numTyp, ok := g.genNumericExpr(rhsExpr); ok {
		goName := goLocalNameForVar(varName)
		g.writeRaw(fmt.Sprintf("%s := %s\n", goName, numExpr))
		// See the matching comment in the string-local path below: the
		// interpreter always evaluates the RHS even when the variable is
		// never read afterward, so this blank-identifier use keeps that
		// evaluation intact without requiring a reference.
		g.body.WriteString(fmt.Sprintf("_ = %s\n", goName))
		g.pushScope(varName, goName, numTyp, true)
		return nil
	}

	goExpr, err := g.genAssignRHS(rhsExpr)
	if err != nil {
		return fmt.Errorf("unbuffered assignment to %q: %w", varName, err)
	}

	goName := goLocalNameForVar(varName)
	g.writeRaw(fmt.Sprintf("%s := %s\n", goName, goExpr))
	// The interpreter always evaluates the RHS, even when the variable is
	// never read afterward (side effects, or simply an unused local); Go
	// rejects a declared-and-unused local, so this blank-identifier use
	// keeps the RHS's evaluation intact without requiring a reference.
	g.body.WriteString(fmt.Sprintf("_ = %s\n", goName))

	g.pushScope(varName, goName, reflectTypeString, true)
	return nil
}

// genUnbufferedReassign emits `- x = <rhs>` for the case genUnbufferedAssign
// already confirmed is a REASSIGNMENT: varName is already bound in scope
// (sv is that binding). This is the SAFE subset proven byte-identical to
// Runtime.setVar's own in-place overwrite (runtime.go's unbufferedAssign
// case: `rawVal := r.evaluateExprRaw(rhs); r.setVar(varName, rawVal)`,
// which mutates the innermost frame already holding varName rather than
// creating a fresh one) — a plain Go `=` assignment to the existing local,
// gated on the reassignment being provably the SAME Go type as the existing
// local, since a fixed-type Go local cannot otherwise take the assignment at
// all:
//
//   - sv must be a `- var` local (sv.isVarLocal), never an each-loop
//     item/index variable — those are rebound fresh every iteration in the
//     interpreter's own model too, so "reassigning" one is out of scope.
//   - the right-hand side is classified by the SAME trio the fresh-binding
//     path above uses, in the same order (genBoolExpr, then genNumericExpr,
//     then genAssignRHS) — deliberately duplicated rather than factored into
//     a shared helper, so the fresh-binding path above stays untouched by
//     this addition.
//   - the classified type must equal sv.typ exactly (reflect.Type is
//     comparable) — a mismatch means either a genuine dynamic type change
//     (`- x = 5` then `- x = "hi"`, a shape the interpreter's untyped scope
//     map allows but a fixed-type `__v_x` Go local cannot) or simply a
//     different literal shape that happens to classify differently, and
//     either way is deferred rather than guessed at.
//
// Once these hold, `sv.goName = <classified goExpr>` is exactly as safe as
// Runtime.setVar's own frame-walk, regardless of which Go block originally
// declared sv.goName (the current one or an ENCLOSING one): reassigning an
// OUTER var from inside an `if` (Runtime's renderConditional pushes no new
// scope frame, so setVar mutates the outer binding directly) or from inside
// an `each` (renderEach pushes a frame, but setVar walks outward past it to
// the pre-loop binding, so the mutation persists across iterations) both
// match Go's own lexical scoping: assigning to a `:=` local declared in an
// enclosing block from inside a nested block/loop mutates that same
// variable, visible to every subsequent read in every sibling/nested scope,
// with no case where the declaring block's depth changes which Go statement
// gets emitted here. No new scope entry is pushed (sv already IS the
// binding) and no fresh `_ = sv.goName` is emitted (the original binding
// already has one, and this reassignment is itself a use).
//
// The dangerous cross-BRANCH case — `- var x` declared in one `if` branch,
// then a same-named `- x = …` in a SIBLING branch — never reaches this
// function at all: scopeRestore pops x when that branch's body finishes and
// records it in leakedVarNames, so genUnbufferedAssign's lookupScope call
// MISSES for the sibling branch's `- x = …`, routing it through the ORDINARY
// fresh-binding path (a `:=`) instead of here.
func (g *generator) genUnbufferedReassign(varName, rhsExpr string, sv scopeVar) error {
	if !sv.isVarLocal {
		return fmt.Errorf("unsupported unbuffered assignment %q in codegen (reassigning %q, an each-loop item/index variable rather than a `- var` local, is not supported yet)", varName, varName)
	}
	if sv.typ == nil {
		return fmt.Errorf("unsupported unbuffered assignment %q in codegen (re-declaring or re-assigning an already-bound variable is not supported yet)", varName)
	}

	if boolExpr, ok, err := g.genBoolExpr(rhsExpr); err != nil {
		return fmt.Errorf("unbuffered assignment to %q: %w", varName, err)
	} else if ok {
		if sv.typ != reflectTypeBool {
			return fmt.Errorf("unsupported unbuffered assignment %q in codegen (reassigning %q from %s to bool is a type change, not supported yet)", varName, varName, sv.typ)
		}
		g.writeRaw(fmt.Sprintf("%s = %s\n", sv.goName, boolExpr))
		return nil
	}

	if numExpr, numTyp, ok := g.genNumericExpr(rhsExpr); ok {
		if sv.typ != numTyp {
			return fmt.Errorf("unsupported unbuffered assignment %q in codegen (reassigning %q from %s to %s is a type change, not supported yet)", varName, varName, sv.typ, numTyp)
		}
		g.writeRaw(fmt.Sprintf("%s = %s\n", sv.goName, numExpr))
		return nil
	}

	goExpr, err := g.genAssignRHS(rhsExpr)
	if err != nil {
		return fmt.Errorf("unbuffered assignment to %q: %w", varName, err)
	}
	if sv.typ != reflectTypeString {
		return fmt.Errorf("unsupported unbuffered assignment %q in codegen (reassigning %q from %s to string is a type change, not supported yet)", varName, varName, sv.typ)
	}
	g.writeRaw(fmt.Sprintf("%s = %s\n", sv.goName, goExpr))
	return nil
}

// genBoolExpr classifies rhs (a `- var x = <rhs>` right-hand side) as
// bool-valued and, if so, compiles it to a genuine Go bool expression by
// reusing genCondition — the SAME condition compiler `if`/`else` already
// uses, so a comparison, a unary "!", a both-bool-operand "||"/"&&", or a
// bare bool-typed field/local produces exactly the Go source genCondition
// would emit for that same text in condition position. ok is false (with a
// nil error) whenever rhs is definitively not one of these bool-valued
// shapes, so the caller (genUnbufferedAssign) falls through to
// genAssignRHS's STRING-local path unchanged; a non-nil error means rhs IS
// one of these shapes but genCondition/genComparison could not compile it
// (an incompatible comparison, an unsupported operand, …), which the caller
// propagates as a hard failure instead of silently guessing at a fallback.
//
// A comparison and a unary "!" are unconditionally safe to treat this way:
// Runtime.evaluateExpr's own comparison branch always returns the literal
// string "true" or "false" (never anything else) when the comparison
// succeeds, and its "!" branch always returns Not(inner) — likewise always
// exactly "true" or "false" — regardless of what the inner operand's own
// type or content is. So genCondition's Go bool for either shape agrees with
// the interpreter's stored/stringified value no matter what operand kind
// genCondition itself is able to compile (bool, numeric, or string).
//
// "||"/"&&" need an extra check the other two don't: in VALUE context (what
// a `- var` RHS actually is), the interpreter's evaluateExpr returns the
// first-truthy OPERAND'S VALUE, not a canonicalized "true"/"false" — so
// `Name || "anon"` evaluates to "Ada" or "anon", a plain string, and
// treating it as a Go bool would silently disagree with the interpreter the
// moment it is read back as a string. Only when BOTH operands are
// themselves provably bool-valued (a comparison, a unary "!", a bool
// field/local, or a nested "||"/"&&" that is itself provably bool by the
// same rule) does the first-truthy value happen to always be the canonical
// string "true" or "false" — isProvablyBoolOperand decides this for both
// operands before genCondition is ever invoked for a "||"/"&&" RHS; when it
// is not satisfied, ok is false and the RHS falls through to
// genAssignRHS's own, separately-proven "||"/"&&" string-local handling
// unchanged.
//
// A top-level ternary is out of scope for this function — genAssignRHS
// already owns ternary as a string-valued IIFE — so ok is simply false for
// it here.
//
// A bare array-literal `.includes(...)`/`.contains(...)` call (the
// manageGroupActive/navGroupActive idiom's simpler sibling —
// `["a","b"].includes(currentPage)` needs no `!== -1` at all, since
// MethodIncludesSlice already returns the interpreter's own canonical
// "true"/"false") is checked the same way the bare-bool-field case is,
// immediately before it: isArrayLiteralMethodCall recognizes the shape, and
// genCondition (via genOperandTruthiness) compiles it.
func (g *generator) genBoolExpr(expr string) (goExpr string, ok bool, err error) {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if idx := findTernary(expr); idx >= 0 {
		return "", false, nil
	}

	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		if !g.isProvablyBoolOperand(expr[:idx]) || !g.isProvablyBoolOperand(expr[idx+2:]) {
			return "", false, nil
		}
		goExpr, err := g.genCondition(expr)
		if err != nil {
			return "", false, err
		}
		return goExpr, true, nil
	}
	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		if !g.isProvablyBoolOperand(expr[:idx]) || !g.isProvablyBoolOperand(expr[idx+2:]) {
			return "", false, nil
		}
		goExpr, err := g.genCondition(expr)
		if err != nil {
			return "", false, err
		}
		return goExpr, true, nil
	}

	for _, op := range conditionComparisonOps {
		if idx := findBinaryOp(expr, op); idx >= 0 {
			goExpr, err := g.genCondition(expr)
			if err != nil {
				return "", false, err
			}
			return goExpr, true, nil
		}
	}

	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		goExpr, err := g.genCondition(expr)
		if err != nil {
			return "", false, err
		}
		return goExpr, true, nil
	}

	if isArrayLiteralMethodCall(expr, "includes", "contains") {
		goExpr, err := g.genCondition(expr)
		if err != nil {
			return "", false, err
		}
		return goExpr, true, nil
	}

	if g.exprIsBoolTyped(expr) {
		goExpr, err := g.genCondition(expr)
		if err != nil {
			return "", false, err
		}
		return goExpr, true, nil
	}

	return "", false, nil
}

// isProvablyBoolOperand reports whether expr, evaluated by the interpreter,
// is guaranteed to already be the canonical string "true" or "false" —
// equivalently, a genuine Go bool once codegen resolves it — rather than an
// arbitrary passed-through value. This is exactly the condition genBoolExpr
// needs of BOTH operands before it is safe to treat a "||"/"&&" RHS as
// bool-valued (see genBoolExpr's doc comment for why). It mirrors
// genCondition's own operator-precedence scan (ternary, ||, &&, the eight
// comparison operators, then a unary "!") so a nested combinator is
// classified consistently with how genCondition itself would compile it: a
// comparison and a unary "!" are unconditionally provably bool (the
// interpreter's evaluateExpr always canonicalizes both to "true"/"false"
// regardless of their own operand types), a nested "||"/"&&" is provably
// bool only when both of ITS operands are (checked recursively), and a bare
// leaf is provably bool only when it resolves, via resolveFieldExpr, to an
// actual bool-typed field or bool-typed `- var` local — never a string,
// numeric, or other scalar leaf, which the interpreter's "||"/"&&" would
// pass through unchanged as its own value rather than canonicalize.
func (g *generator) isProvablyBoolOperand(expr string) bool {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if idx := findTernary(expr); idx >= 0 {
		return false
	}
	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		return g.isProvablyBoolOperand(expr[:idx]) && g.isProvablyBoolOperand(expr[idx+2:])
	}
	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		return g.isProvablyBoolOperand(expr[:idx]) && g.isProvablyBoolOperand(expr[idx+2:])
	}
	for _, op := range conditionComparisonOps {
		if idx := findBinaryOp(expr, op); idx >= 0 {
			return true
		}
	}
	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		return true
	}
	if isArrayLiteralMethodCall(expr, "includes", "contains") {
		return true
	}
	return g.exprIsBoolTyped(expr)
}

// exprIsBoolTyped reports whether expr is a bare identifier or dot-path that
// resolveFieldExpr resolves to a field/local of reflect.Kind Bool — the only
// bare-leaf shape genBoolExpr (and isProvablyBoolOperand) treat as
// bool-valued; a numeric or string leaf is left to genAssignRHS's own,
// separately-proven string-local handling.
func (g *generator) exprIsBoolTyped(expr string) bool {
	_, typ, err := g.resolveFieldExpr(expr)
	return err == nil && typ != nil && typ.Kind() == reflect.Bool
}

// genNumericExpr classifies rhs (a `- var x = <rhs>` right-hand side) as one
// of the two shapes Runtime.evaluateExprRaw hands back as a genuine RAW,
// un-stringified numeric Go value rather than falling through to its own
// "evaluate then stringify" default: a bare identifier/dot-path resolving
// (via resolveFieldExpr) to a field or `- var` local whose reflect.Kind is
// one genScalarStringify itself has a case for (every sized int/uint kind,
// and float64 — deliberately NOT float32, which genScalarStringify has no
// case for and so cannot stringify byte-identically to lookupAndStringify's
// own Sprintf-based fallback for it), or a bare numeric literal
// (parseJSNumber), always modeled as a Go float64 local — the natural type
// for a bare JS number literal, matching how the interpreter itself stores
// every JS number.
//
// ok is false (with an empty goExpr/typ) whenever rhs is definitively not
// one of these two shapes, so the caller (genUnbufferedAssign, which tries
// this AFTER genBoolExpr and BEFORE genAssignRHS) falls through to
// genAssignRHS's existing STRING-local grammar unchanged. This is safe
// specifically because neither parseJSNumber nor resolveFieldExpr's own
// bare-identifier/dot-path shape check ever matches an expression containing
// a top-level operator: an arithmetic RHS (`a + b`) — which
// Runtime.evaluateExprRaw does NOT special-case, so it stays on the STRING
// path via its own `s, _ := r.evaluateExpr(expr); return s` fallthrough — is
// never intercepted here, and a comparison/bool RHS is never reached at all
// since genBoolExpr already claimed it first.
//
// Once a field/local is proven numeric this way, it is exactly as safe to
// store as its own native Go type as the string-field case above: the SAME
// argument applies unchanged (genScalarStringify's per-Kind switch is
// byte-identical to lookupAndStringify's own per-Kind switch by
// construction), just for the numeric Kind cases of that switch rather than
// its String case.
func (g *generator) genNumericExpr(expr string) (goExpr string, typ reflect.Type, ok bool) {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if f, isNum := parseJSNumber(expr); isNum {
		return "float64(" + formatCanonicalLiteral(f) + ")", reflectTypeFloat64, true
	}

	resolvedExpr, fieldTyp, err := g.resolveFieldExpr(expr)
	if err != nil || fieldTyp == nil {
		return "", nil, false
	}
	switch fieldTyp.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float64:
		return resolvedExpr, fieldTyp, true
	default:
		return "", nil, false
	}
}

// genAssignRHS compiles a `- var x = <rhs>` right-hand side to a Go string
// expression, for the narrow, PROVEN-safe grammar subset genUnbufferedAssign
// documents: a top-level ternary (condition via genCondition, both branches
// recursed through genAssignRHS, emitted as a func() string {}() IIFE — the
// same shape genTernaryValueExpr uses for a total ternary), `||`/`&&`
// (recursed operands combined via genOrValueExpr/genAndValueExpr, matching
// genValueExpr's own value-context logical operators exactly), a quoted
// string literal, a backtick template literal (delegated to
// genTemplateLiteral unchanged — genValueExpr's OWN grammar inside `${...}`
// is fine here even though it is broader than this function's, because
// Runtime.evaluateExprRaw never descends into a template literal's
// structure at all: ANY top-level template literal falls straight through to
// its `s, _ := r.evaluateExpr(expr); return s` fallback, regardless of what
// its `${...}` parts contain), `+` string concatenation (both operands
// recursed through genAssignRHS, combined via gopug.Add — matching
// genValueExpr's own total `+` case), and finally a bare identifier/dot-path
// that resolveFieldExpr proves is a string-typed field (see
// genUnbufferedAssign's doc comment for why the leaf is restricted to
// string). Every other shape — a numeric/bool/other-scalar field, an
// arithmetic operator, a comparison, `!`, an index expression, a method
// call, an array/object literal, anything genValueExpr itself already
// rejects — returns a descriptive "unsupported" error instead of guessing;
// this function is deliberately narrower than genValueExpr's own grammar,
// not a synonym for it, precisely because the bounded-agreement invariant
// this feature depends on is not proven for genValueExpr's full grammar in
// RAW-assignment position (see genUnbufferedAssign).
func (g *generator) genAssignRHS(expr string) (string, error) {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if idx := findTernary(expr); idx >= 0 {
		rest := expr[idx+1:]
		colonIdx := findBinaryOp(rest, ":")
		if colonIdx < 0 {
			return "", fmt.Errorf("malformed ternary expression in codegen unbuffered assignment: %s", expr)
		}
		condExpr, err := g.genCondition(expr[:idx])
		if err != nil {
			return "", err
		}
		trueExpr, err := g.genAssignRHS(rest[:colonIdx])
		if err != nil {
			return "", err
		}
		falseExpr, err := g.genAssignRHS(rest[colonIdx+1:])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("func() string {\n\tif %s {\n\t\treturn %s\n\t}\n\treturn %s\n}()", condExpr, trueExpr, falseExpr), nil
	}

	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		leftExpr, err := g.genAssignRHS(expr[:idx])
		if err != nil {
			return "", err
		}
		rightExpr, err := g.genAssignRHS(expr[idx+2:])
		if err != nil {
			return "", err
		}
		g.needsGopug = true
		goExpr, _ := g.genOrValueExpr(leftExpr, false, rightExpr, false)
		return goExpr, nil
	}
	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		leftExpr, err := g.genAssignRHS(expr[:idx])
		if err != nil {
			return "", err
		}
		rightExpr, err := g.genAssignRHS(expr[idx+2:])
		if err != nil {
			return "", err
		}
		g.needsGopug = true
		goExpr, _ := g.genAndValueExpr(leftExpr, false, rightExpr, false)
		return goExpr, nil
	}

	if lit, ok := unwrapQuotedLiteral(expr); ok {
		return strconv.Quote(lit), nil
	}
	if strings.HasPrefix(expr, "`") {
		return g.genTemplateLiteral(expr)
	}

	if idx := findBinaryOp(expr, "+"); idx >= 0 {
		leftExpr, err := g.genAssignRHS(expr[:idx])
		if err != nil {
			return "", err
		}
		rightExpr, err := g.genAssignRHS(expr[idx+1:])
		if err != nil {
			return "", err
		}
		g.needsGopug = true
		return "gopug.Add(" + leftExpr + ", " + rightExpr + ")", nil
	}

	resolvedExpr, typ, err := g.resolveFieldExpr(expr)
	if err != nil {
		return "", fmt.Errorf("unsupported unbuffered assignment right-hand side %q in codegen: %w", expr, err)
	}
	if typ != nil {
		switch typ.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float64:
			return "", fmt.Errorf("unsupported unbuffered assignment right-hand side %q in codegen (a numeric-valued `- var` local is not supported yet)", expr)
		}
	}
	if typ == nil || typ.Kind() != reflect.String {
		return "", fmt.Errorf("unsupported unbuffered assignment right-hand side %q in codegen (only a string literal, template literal, string concatenation, a ternary/||/&& over string operands, a bool-valued comparison/logical/bool-field (see genBoolExpr, tried before this function), or a string-typed field/dot-path is supported as a `- var` right-hand side in this increment)", expr)
	}
	return convertExpr(resolvedExpr, typ, reflectTypeString, "string"), nil
}

// genValueExpr emits a Go expression equal to what Runtime.evaluateExpr(expr)
// would return, for the grammar subset this increment supports: leaves (a
// bare field/dot-path, a quoted string literal, a numeric literal,
// true/false/null/undefined/nil), a top-level ternary, value-context
// `||`/`&&`/`!`/comparison, and the arithmetic operators `-`, `+`, `*`, `/`,
// and `%`. It walks the same operator-precedence order evaluateExpr does —
// strip balanced outer parens, then check in turn for a top-level ternary,
// `||`, `&&`, each comparison operator, a leading unary `!`, a quoted string
// literal, a template literal, an array/object literal, a numeric literal,
// the true/false/null keywords, subtraction, addition, multiplication,
// division, and finally modulo — so that when two operators are both
// present, genValueExpr splits on the same top-level one evaluateExpr would.
// A top-level ternary is checked first (matching evaluateExpr's own order)
// and delegates to genTernaryValueExpr, which reuses genCondition for the
// condition and recurses into genValueExpr for each branch.
//
// `||` and `&&` (genOrValueExpr/genAndValueExpr) return the operand VALUE,
// matching evaluateExpr's own value-returning (not merely truthy) semantics
// exactly: `||` returns the left operand unchanged when it is truthy,
// otherwise the right operand's value; `&&` returns the literal string
// "false" when the left operand is falsy, otherwise the right operand's
// value. Both are short-circuited — the right operand is only ever evaluated
// (and, if it is itself fallible, only ever extracted) when it is actually
// needed, exactly like evaluateExpr's own short-circuit `||`/`&&` branches. A
// leading `!` (guarded, like evaluateExpr's own check, against matching the
// `!=` comparison operator) returns "true"/"false" via the exported
// gopug.Not, wrapping a fallible inner operand in its own extraction IIFE
// when needed. A comparison operator delegates the whole expression to
// genCondition — the same bounded-agreement comparison compiler condition
// position uses — and wraps its bool result in strconv.FormatBool, which is
// byte-identical to evaluateExpr's own "true"/"false" comparison result by
// construction; if genCondition can't compile the comparison (its operand
// shapes are limited to the same bounded-agreement subset described on
// genCondition), that error propagates unchanged rather than guessing.
//
// The returned fallible flag distinguishes the two shapes goExpr can take.
// When fallible is false (every leaf, every `-`/`+`/`*` combination of total
// operands, a comparison, a `!`/`||`/`&&` whose operand(s) are all total),
// goExpr is a plain Go expression of type string — pure, inline, zero
// overhead. When fallible is true, goExpr is a Go expression of type (string,
// error): either a direct gopug.Div(...)/gopug.Mod(...) call (both operands
// total) or a `func() (string, error) {...}()` IIFE that extracts a fallible
// operand before combining (genArithCombinerIIFE), or a fallible-branch
// ternary/`||`/`&&`/`!` IIFE (genTernaryValueExpr, genOrValueExpr,
// genAndValueExpr, and genValueExpr's own `!` branch) — mirroring every case
// Runtime.evaluateExpr's own combiner/ternary/logical branches can abort
// Render with an error (a numeric zero divisor reached through `/` or `%`,
// however deep it sits in the expression tree). A caller consuming
// genValueExpr's top-level result directly (genInterpolation, genCode,
// genAttributes) extracts a fallible result via genFallibleExtraction before
// using it; a caller composing it into another combiner, ternary branch, or
// logical operand recurses through the same uniform (string, bool, error)
// contract, extracting it via genFallibleExtractionInline inside its own IIFE
// — so fallibility bubbles through arbitrarily nested arithmetic, ternaries,
// and logical combinators without ever losing an error the interpreter would
// have raised (a template-literal `${}` part is the one exception:
// Runtime.evaluateExpr's own template-literal walk discards a part's error
// rather than propagating it, so genTemplateLiteral matches that by
// discarding it too, via genFallibleTemplatePart, rather than erroring).
// Every other construct evaluateExpr supports beyond these — string method
// calls including the type-directed `.join`/`.toFixed`/`.toPrecision` (see
// genMethodCall, genJoinValueExpr, genToFixedOrPrecisionValueExpr), index
// expressions (`arr[i]`, see genIndexValueExpr), and value-context `.length`
// (see genLengthValueExpr) — ARE now supported; a non-string-keyed map
// index, an index-then-dot receiver (`arr[i].field`), and an array/object
// literal remain a later increment — returns a descriptive "unsupported"
// error here instead of emitting something that might not match — the
// correctness bar is byte-identical to the interpreter, not a best guess.
func (g *generator) genValueExpr(expr string) (goExpr string, fallible bool, err error) {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if idx := findTernary(expr); idx >= 0 {
		return g.genTernaryValueExpr(expr, idx)
	}
	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		leftExpr, leftFallible, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", false, err
		}
		rightExpr, rightFallible, err := g.genValueExpr(expr[idx+2:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		goExpr, fallible := g.genOrValueExpr(leftExpr, leftFallible, rightExpr, rightFallible)
		return goExpr, fallible, nil
	}
	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		leftExpr, leftFallible, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", false, err
		}
		rightExpr, rightFallible, err := g.genValueExpr(expr[idx+2:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		goExpr, fallible := g.genAndValueExpr(leftExpr, leftFallible, rightExpr, rightFallible)
		return goExpr, fallible, nil
	}
	for _, op := range conditionComparisonOps {
		if findBinaryOp(expr, op) >= 0 {
			condExpr, err := g.genCondition(expr)
			if err != nil {
				return "", false, err
			}
			g.needsStrconv = true
			return "strconv.FormatBool(" + condExpr + ")", false, nil
		}
	}
	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		innerExpr, innerFallible, err := g.genValueExpr(expr[1:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		if !innerFallible {
			return "gopug.Not(" + innerExpr + ")", false, nil
		}
		var b strings.Builder
		b.WriteString("func() (string, error) {\n")
		vv := g.genFallibleExtractionInline(&b, innerExpr)
		fmt.Fprintf(&b, "return gopug.Not(%s), nil\n", vv)
		b.WriteString("}()")
		return b.String(), true, nil
	}

	if lit, ok := unwrapQuotedLiteral(expr); ok {
		return strconv.Quote(lit), false, nil
	}
	if strings.HasPrefix(expr, "`") {
		goExpr, err := g.genTemplateLiteral(expr)
		return goExpr, false, err
	}
	// A pure bracket-wrapped array literal (the whole expression, not just its
	// prefix) is out of scope here — but `[...].indexOf(...)`/`.includes(...)`/
	// `.contains(...)` is NOT a pure array literal (it doesn't end in "]"), so
	// it must fall through the rest of this precedence chain down to the
	// dot/method-call handling below, exactly like Runtime.evaluateExpr's own
	// array-literal check (guarded by expr[len(expr)-1] == ']') never fires
	// for that shape either.
	if strings.HasPrefix(expr, "[") && strings.HasSuffix(expr, "]") && findMatchingCloseBracket(expr) == len(expr)-1 {
		return "", false, fmt.Errorf("unsupported array literal in codegen value expression %q", expr)
	}
	if strings.HasPrefix(expr, "{") {
		return "", false, fmt.Errorf("unsupported object literal in codegen value expression %q", expr)
	}

	if f, ok := parseJSNumber(expr); ok {
		return strconv.Quote(formatCanonicalLiteral(f)), false, nil
	}

	switch expr {
	case "true":
		return `"true"`, false, nil
	case "false":
		return `"false"`, false, nil
	case "null", "undefined", "nil":
		return `""`, false, nil
	}

	if idx := findSubtraction(expr); idx >= 0 {
		leftExpr, leftFallible, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", false, err
		}
		rightExpr, rightFallible, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		if leftFallible || rightFallible {
			return g.genArithCombinerIIFE(leftExpr, leftFallible, rightExpr, rightFallible, "gopug.Sub(%s, %s)", false), true, nil
		}
		return "gopug.Sub(" + leftExpr + ", " + rightExpr + ")", false, nil
	}

	if idx := findBinaryOp(expr, "+"); idx >= 0 {
		leftExpr, leftFallible, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", false, err
		}
		rightExpr, rightFallible, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		if leftFallible || rightFallible {
			return g.genArithCombinerIIFE(leftExpr, leftFallible, rightExpr, rightFallible, "gopug.Add(%s, %s)", false), true, nil
		}
		return "gopug.Add(" + leftExpr + ", " + rightExpr + ")", false, nil
	}

	if idx := findRightmostOp(expr, '*'); idx >= 0 {
		leftExpr, leftFallible, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", false, err
		}
		rightExpr, rightFallible, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		if leftFallible || rightFallible {
			return g.genArithCombinerIIFE(leftExpr, leftFallible, rightExpr, rightFallible, "gopug.Mul(%s, %s)", false), true, nil
		}
		return "gopug.Mul(" + leftExpr + ", " + rightExpr + ")", false, nil
	}
	if idx := findRightmostOp(expr, '/'); idx >= 0 {
		leftExpr, leftFallible, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", false, err
		}
		rightExpr, rightFallible, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		if leftFallible || rightFallible {
			return g.genArithCombinerIIFE(leftExpr, leftFallible, rightExpr, rightFallible, "gopug.Div(%s, %s)", true), true, nil
		}
		return "gopug.Div(" + leftExpr + ", " + rightExpr + ")", true, nil
	}
	if idx := findRightmostOp(expr, '%'); idx >= 0 {
		leftExpr, leftFallible, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", false, err
		}
		rightExpr, rightFallible, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		if leftFallible || rightFallible {
			return g.genArithCombinerIIFE(leftExpr, leftFallible, rightExpr, rightFallible, "gopug.Mod(%s, %s)", true), true, nil
		}
		return "gopug.Mod(" + leftExpr + ", " + rightExpr + ")", true, nil
	}
	if idx := findIndexOp(expr); idx >= 0 {
		return g.genIndexValueExpr(expr, idx)
	}

	if dotIdx := findTopLevelDot(expr); dotIdx > 0 {
		objExpr := expr[:dotIdx]
		rest := expr[dotIdx+1:]
		methodName := rest
		if before, _, found := strings.Cut(rest, "("); found {
			methodName = strings.TrimSpace(before)
		}
		if methodName == "length" {
			return g.genLengthValueExpr(objExpr, expr)
		}
		if strings.Contains(rest, "(") {
			return g.genMethodCall(objExpr, rest)
		}
	}

	resolvedExpr, typ, err := g.resolveFieldExpr(expr)
	if err != nil {
		return "", false, err
	}
	goExpr, err = g.genScalarStringify(resolvedExpr, typ)
	return goExpr, false, err
}

// genIndexValueExpr emits genValueExpr's handling of a top-level index
// expression `<objExpr>[<keyExpr>]` (idx is the position of the opening `[`
// findIndexOp already located), reproducing Runtime.evaluateExpr's own index
// branch (which evaluates objExpr RAW, keyExpr stringified, calls
// indexValue, and stringifies a non-nil result with fmt.Sprintf("%v", …))
// exactly, as a TOTAL immediately-invoked function literal — nil/absent/
// out-of-range all collapse to "" the same way a nil slice or a nil map read
// would, with no explicit nil guard needed. objExpr must resolve via
// resolveFieldExpr (a bare identifier or dot-path with a known reflect.Type)
// and keyExpr must be a TOTAL genValueExpr result; anything else — a
// fallible key, an untyped/index-then-dot receiver, a non-string-keyed map,
// or any other operand kind — returns an error rather than guessing, since
// the interpreter's key is always a string (so a non-string map key can
// never match, and silently emitting an always-"" constant would be a
// surprising trap for the generator's caller, not a helpful optimization).
func (g *generator) genIndexValueExpr(expr string, idx int) (string, bool, error) {
	objExpr := strings.TrimSpace(expr[:idx])
	keyExpr := strings.TrimSpace(expr[idx+1 : len(expr)-1])

	keyGoExpr, keyFallible, err := g.genValueExpr(keyExpr)
	if err != nil {
		return "", false, err
	}
	if keyFallible {
		return "", false, fmt.Errorf("unsupported index expression with a fallible key in codegen value expression %q (fallible index keys are not yet supported)", expr)
	}

	objGoExpr, typ, err := g.resolveFieldExpr(objExpr)
	if err != nil {
		return "", false, fmt.Errorf("index operand %q: %w", expr, err)
	}
	if typ == nil {
		return "", false, fmt.Errorf("unsupported index expression %q in codegen (Config.DataReflectType is required to classify the indexed operand)", expr)
	}

	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		g.needsStrconv = true
		g.needsStrings = true
		g.needsFmt = true
		n := g.nextTmp()
		iv := fmt.Sprintf("__i%d", n)
		ev := fmt.Sprintf("__e%d", n)
		var b strings.Builder
		b.WriteString("func() string {\n")
		fmt.Fprintf(&b, "%s, %s := strconv.Atoi(strings.TrimSpace(%s))\n", iv, ev, keyGoExpr)
		fmt.Fprintf(&b, "if %s != nil || %s < 0 || %s >= len(%s) {\nreturn \"\"\n}\n", ev, iv, iv, objGoExpr)
		fmt.Fprintf(&b, "return fmt.Sprintf(\"%%v\", (%s)[%s])\n", objGoExpr, iv)
		b.WriteString("}()")
		return b.String(), false, nil
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return "", false, fmt.Errorf("unsupported index expression %q in codegen (map key type %s is not string; the interpreter's index key is always a string so this could never match)", expr, typ.Key())
		}
		g.needsFmt = true
		n := g.nextTmp()
		vv := fmt.Sprintf("__v%d", n)
		okv := fmt.Sprintf("__ok%d", n)
		var b strings.Builder
		b.WriteString("func() string {\n")
		fmt.Fprintf(&b, "%s, %s := (%s)[%s]\n", vv, okv, objGoExpr, keyGoExpr)
		fmt.Fprintf(&b, "if !%s {\nreturn \"\"\n}\n", okv)
		fmt.Fprintf(&b, "return fmt.Sprintf(\"%%v\", %s)\n", vv)
		b.WriteString("}()")
		return b.String(), false, nil
	default:
		return "", false, fmt.Errorf("unsupported index expression %q in codegen (indexed operand type %s is not a slice, array, or map)", expr, typ)
	}
}

// genLengthValueExpr emits genValueExpr's handling of a value-context
// `.length`/`.length()` property (objExpr is everything before the final
// top-level dot findTopLevelDot located; fullExpr is the whole expression,
// used only for error messages), reusing genLengthOperand's exact
// type-directed len()/utf8.RuneCountInString(…) logic — the same
// interpreter property genOperandTruthiness/genComparison already reproduce
// for CONDITION position — and wrapping its int result in strconv.Itoa to
// match Runtime.evaluateExpr's own `.length` case, which stringifies with
// strconv.Itoa in VALUE position instead of comparing it against zero.
func (g *generator) genLengthValueExpr(objExpr, fullExpr string) (string, bool, error) {
	lenExpr, _, err := g.genLengthOperand(objExpr, fullExpr)
	if err != nil {
		return "", false, err
	}
	g.needsStrconv = true
	return "strconv.Itoa(" + lenExpr + ")", false, nil
}

// genMethodCall emits genValueExpr's handling of a string method call
// `<objExpr>.<rest>`, where rest is everything after the top-level dot
// findTopLevelDot located and is already known to contain "(" (a property
// access with no "(" is resolveFieldExpr's leaf case, not this one). It
// splits methodName/argsStr exactly the way Runtime.evaluateExpr's own
// method-dispatch switch does (strings.Cut on "(" then ")"). `join`,
// `toFixed`, and `toPrecision` are handled FIRST, before the receiver is
// ever stringified: unlike every other method here, they need the RAW typed
// receiver (resolveFieldExpr, not a genValueExpr recursion) — join iterates
// slice elements, toFixed/toPrecision need the numeric value — so they are
// dispatched to genJoinValueExpr/genToFixedOrPrecisionValueExpr up front
// (see those functions for the type-directed rules). Every other method
// resolves the receiver via a genValueExpr recursion on objExpr — the
// STRINGIFIED receiver, matching evaluateExpr's own objVal :=
// evaluateExpr(objExpr) — and dispatches on methodName. A trivial method (a
// single stdlib call with no argument-dependent logic) is emitted directly
// on the receiver expression; a non-trivial method (argument
// quote-stripping and/or multi-step logic) is emitted as a call to the
// matching single-sourced gopug.Method* helper (see runtime.go), with each
// argument itself resolved by a genValueExpr recursion, split on the same
// top-level comma findBinaryOp locates for a two-argument method. Both the
// receiver and every argument must be TOTAL in this increment — a fallible
// receiver or argument (e.g. `(a/b).toUpperCase()`) returns an error rather
// than growing the IIFE machinery for methods yet. `length` is intercepted
// by genValueExpr's own dot-handling before this function is ever reached
// (see genLengthValueExpr), so it never appears in the switch below; any
// other unrecognized name errors the same way Runtime.evaluateExpr's own
// "unsupported string method" fallback does.
func (g *generator) genMethodCall(objExpr, rest string) (string, bool, error) {
	methodName := rest
	argsStr := ""
	if before, inner, found := strings.Cut(rest, "("); found {
		methodName = before
		argsStr, _, _ = strings.Cut(strings.TrimSpace(inner), ")")
		argsStr = strings.TrimSpace(argsStr)
	}
	methodName = strings.TrimSpace(methodName)
	fullExpr := objExpr + "." + rest

	switch methodName {
	case "join":
		return g.genJoinValueExpr(objExpr, argsStr, fullExpr)
	case "toFixed", "toPrecision":
		return g.genToFixedOrPrecisionValueExpr(objExpr, methodName, argsStr, fullExpr)
	case "indexOf", "includes", "contains":
		if strings.HasPrefix(strings.TrimSpace(objExpr), "[") {
			return g.genArrayIndexOfValueExpr(objExpr, methodName, argsStr, fullExpr)
		}
	}

	recvExpr, recvFallible, err := g.genValueExpr(objExpr)
	if err != nil {
		return "", false, err
	}
	if recvFallible {
		return "", false, fmt.Errorf("unsupported method call on a fallible receiver in codegen value expression %q (method calls on a fallible receiver are not yet supported)", fullExpr)
	}

	switch methodName {
	case "toUpperCase", "toUppercase":
		g.needsStrings = true
		return "strings.ToUpper(" + recvExpr + ")", false, nil
	case "toLowerCase", "toLowercase":
		g.needsStrings = true
		return "strings.ToLower(" + recvExpr + ")", false, nil
	case "trim":
		g.needsStrings = true
		return "strings.TrimSpace(" + recvExpr + ")", false, nil
	case "trimLeft", "trimStart":
		g.needsStrings = true
		return "strings.TrimLeft(" + recvExpr + ", \" \\t\\n\\r\")", false, nil
	case "trimRight", "trimEnd":
		g.needsStrings = true
		return "strings.TrimRight(" + recvExpr + ", \" \\t\\n\\r\")", false, nil
	case "toString", "String":
		return recvExpr, false, nil
	}

	genArg := func(argExpr string) (string, error) {
		v, fallible, err := g.genValueExpr(argExpr)
		if err != nil {
			return "", err
		}
		if fallible {
			return "", fmt.Errorf("unsupported method call with a fallible argument in codegen value expression %q (fallible method arguments are not yet supported)", fullExpr)
		}
		return v, nil
	}

	switch methodName {
	case "repeat":
		if argsStr == "" {
			return recvExpr, false, nil
		}
		n, err := genArg(argsStr)
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		return "gopug.MethodRepeat(" + recvExpr + ", " + n + ")", false, nil

	case "split":
		sep := `""`
		if argsStr != "" {
			var err error
			sep, err = genArg(argsStr)
			if err != nil {
				return "", false, err
			}
		}
		g.needsGopug = true
		return "gopug.MethodSplit(" + recvExpr + ", " + sep + ")", false, nil

	case "replace":
		if argsStr == "" {
			return recvExpr, false, nil
		}
		commaIdx := findBinaryOp(argsStr, ",")
		if commaIdx <= 0 {
			return recvExpr, false, nil
		}
		oldArg, err := genArg(strings.TrimSpace(argsStr[:commaIdx]))
		if err != nil {
			return "", false, err
		}
		newArg, err := genArg(strings.TrimSpace(argsStr[commaIdx+1:]))
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		return "gopug.MethodReplace(" + recvExpr + ", " + oldArg + ", " + newArg + ")", false, nil

	case "slice":
		if argsStr == "" {
			return recvExpr, false, nil
		}
		g.needsGopug = true
		if commaIdx := findBinaryOp(argsStr, ","); commaIdx > 0 {
			startArg, err := genArg(strings.TrimSpace(argsStr[:commaIdx]))
			if err != nil {
				return "", false, err
			}
			endArg, err := genArg(strings.TrimSpace(argsStr[commaIdx+1:]))
			if err != nil {
				return "", false, err
			}
			return "gopug.MethodSlice2(" + recvExpr + ", " + startArg + ", " + endArg + ")", false, nil
		}
		startArg, err := genArg(argsStr)
		if err != nil {
			return "", false, err
		}
		return "gopug.MethodSlice1(" + recvExpr + ", " + startArg + ")", false, nil

	case "indexOf":
		if argsStr == "" {
			return `"-1"`, false, nil
		}
		needle, err := genArg(argsStr)
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		return "gopug.MethodIndexOf(" + recvExpr + ", " + needle + ")", false, nil

	case "includes", "contains":
		if argsStr == "" {
			return `"false"`, false, nil
		}
		needle, err := genArg(argsStr)
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		return "gopug.MethodIncludes(" + recvExpr + ", " + needle + ")", false, nil

	case "startsWith":
		if argsStr == "" {
			return `"false"`, false, nil
		}
		prefix, err := genArg(argsStr)
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		return "gopug.MethodStartsWith(" + recvExpr + ", " + prefix + ")", false, nil

	case "endsWith":
		if argsStr == "" {
			return `"false"`, false, nil
		}
		suffix, err := genArg(argsStr)
		if err != nil {
			return "", false, err
		}
		g.needsGopug = true
		return "gopug.MethodEndsWith(" + recvExpr + ", " + suffix + ")", false, nil

	case "padStart", "padEnd":
		if argsStr == "" {
			return recvExpr, false, nil
		}
		helper := "gopug.MethodPadStart"
		if methodName == "padEnd" {
			helper = "gopug.MethodPadEnd"
		}
		g.needsGopug = true
		if commaIdx := findBinaryOp(argsStr, ","); commaIdx > 0 {
			lenArg, err := genArg(strings.TrimSpace(argsStr[:commaIdx]))
			if err != nil {
				return "", false, err
			}
			chArg, err := genArg(strings.TrimSpace(argsStr[commaIdx+1:]))
			if err != nil {
				return "", false, err
			}
			return helper + "(" + recvExpr + ", " + lenArg + ", " + chArg + ")", false, nil
		}
		lenArg, err := genArg(argsStr)
		if err != nil {
			return "", false, err
		}
		return helper + "(" + recvExpr + ", " + lenArg + `, "")`, false, nil

	default:
		return "", false, fmt.Errorf("unsupported string method %q in codegen value expression %q", methodName, fullExpr)
	}
}

// genTotalArg resolves argExpr (a method-call argument) via genValueExpr,
// requiring the result be TOTAL — a fallible argument (e.g. `(a/b)`) returns
// an error tied to fullExpr, matching genMethodCall's own genArg closure,
// since no method call in this increment (including join/toFixed/toPrecision)
// grows the IIFE machinery needed to accept a fallible argument.
func (g *generator) genTotalArg(argExpr, fullExpr string) (string, error) {
	v, fallible, err := g.genValueExpr(argExpr)
	if err != nil {
		return "", err
	}
	if fallible {
		return "", fmt.Errorf("unsupported method call with a fallible argument in codegen value expression %q (fallible method arguments are not yet supported)", fullExpr)
	}
	return v, nil
}

// genJoinValueExpr emits genMethodCall's handling of `<objExpr>.join(sep)`,
// resolving the receiver via resolveFieldExpr — the RAW typed receiver,
// unlike every other string method in genMethodCall — since
// Runtime.evaluateExpr's own "join" case iterates r.evaluateExprRaw(objExpr)
// as a reflect.Value, not the stringified receiver. Only a Slice/Array
// receiver is supported: it emits a TOTAL immediately-invoked function
// literal that builds a []string of each element's fmt.Sprintf("%v", …) form
// (matching the interpreter's own per-element stringify exactly, including
// on a non-scalar element type such as a struct slice) and joins them with
// gopug.UnquoteArg(sep) — the same quote-strip UnquoteArg's doc comment
// describes, applied to sep the same way Runtime.evaluateExpr's own "join"
// case applies it. A 0-arg join passes the literal `""` for sep, matching
// the interpreter's own empty-argument default. Any other receiver kind
// (bool/struct/string/map/…) is a defer (error): the interpreter's own
// fallback for a non-slice "join" receiver is to silently return the
// stringified receiver unchanged, a weird edge codegen intentionally does
// NOT reproduce — erroring at generate time here is fail-closed, not a
// bounded-agreement breach, since it never emits output that might disagree
// with the interpreter. A nil Config.DataReflectType (the receiver's type is
// unknown) is likewise a defer (error), the same as every other
// type-directed construct in this file.
func (g *generator) genJoinValueExpr(objExpr, argsStr, fullExpr string) (string, bool, error) {
	objGoExpr, typ, err := g.resolveFieldExpr(objExpr)
	if err != nil {
		return "", false, fmt.Errorf("method %q receiver %q: %w", "join", objExpr, err)
	}
	if typ == nil {
		return "", false, fmt.Errorf("unsupported method %q in codegen value expression %q (Config.DataReflectType is required to classify the receiver)", "join", fullExpr)
	}
	if typ.Kind() != reflect.Slice && typ.Kind() != reflect.Array {
		return "", false, fmt.Errorf("unsupported method %q in codegen value expression %q (receiver type %s is not a slice or array)", "join", fullExpr, typ)
	}

	sep := `""`
	if argsStr != "" {
		sep, err = g.genTotalArg(argsStr, fullExpr)
		if err != nil {
			return "", false, err
		}
	}

	g.needsGopug = true
	g.needsStrings = true
	g.needsFmt = true
	n := g.nextTmp()
	pv := fmt.Sprintf("__p%d", n)
	iv := fmt.Sprintf("__i%d", n)
	ev := fmt.Sprintf("__e%d", n)
	var b strings.Builder
	b.WriteString("func() string {\n")
	fmt.Fprintf(&b, "%s := make([]string, len(%s))\n", pv, objGoExpr)
	fmt.Fprintf(&b, "for %s, %s := range %s {\n", iv, ev, objGoExpr)
	fmt.Fprintf(&b, "%s[%s] = fmt.Sprintf(\"%%v\", %s)\n", pv, iv, ev)
	b.WriteString("}\n")
	fmt.Fprintf(&b, "return strings.Join(%s, gopug.UnquoteArg(%s))\n", pv, sep)
	b.WriteString("}()")
	return b.String(), false, nil
}

// genToFixedOrPrecisionValueExpr emits genMethodCall's handling of
// `<objExpr>.toFixed(n)`/`<objExpr>.toPrecision(n)` (methodName selects
// which), resolving the receiver via resolveFieldExpr — the RAW typed
// receiver, matching Runtime.evaluateExpr's own r.evaluateExprRaw(objExpr)
// type-switch — rather than a genValueExpr recursion. precArg is resolved as
// a TOTAL genValueExpr result (a 0-arg call passes the literal `""`, letting
// gopug.ToFixed/gopug.ToPrecision apply their own interpreter-matching
// default), and the receiver's Kind decides the shape:
//
//   - a numeric field (any int/uint/float kind) is TOTAL: the field's own
//     value, converted to float64 via convertExpr (a no-op for an untyped
//     float64 field, an explicit float64(...) wrap for every other numeric
//     kind — mirroring genScalarStringify's Float64 case), is passed
//     directly to gopug.ToFixed/gopug.ToPrecision, which can never fail.
//   - a string field is FALLIBLE: gopug.ToFixedStr/gopug.ToPrecisionStr
//     parses it with strconv.ParseFloat at RENDER time, exactly like
//     Runtime.evaluateExpr's own default-branch ParseFloat(objVal) fallback,
//     and can return the interpreter's own "toFixed/toPrecision: value %q is
//     not a number" error — this is the one case in this function where the
//     returned goExpr is (string, error)-typed, and the fallible flag is
//     true, letting the caller (genInterpolation/genCode/genAttributes, via
//     genFallibleExtraction) propagate that error exactly like the
//     interpreter's own Render would.
//   - any other kind (bool, struct, slice, map, ptr, …) is a defer (error):
//     the interpreter would always ParseFloat-fail the stringified value at
//     render time for these kinds, so refusing to generate code for them is
//     fail-closed, not a bounded-agreement breach.
//
// A nil Config.DataReflectType is likewise a defer (error), the same as
// every other type-directed construct in this file.
func (g *generator) genToFixedOrPrecisionValueExpr(objExpr, methodName, argsStr, fullExpr string) (string, bool, error) {
	objGoExpr, typ, err := g.resolveFieldExpr(objExpr)
	if err != nil {
		return "", false, fmt.Errorf("method %q receiver %q: %w", methodName, objExpr, err)
	}
	if typ == nil {
		return "", false, fmt.Errorf("unsupported method %q in codegen value expression %q (Config.DataReflectType is required to classify the receiver)", methodName, fullExpr)
	}

	precArg := `""`
	if argsStr != "" {
		precArg, err = g.genTotalArg(argsStr, fullExpr)
		if err != nil {
			return "", false, err
		}
	}

	totalHelper := "gopug.ToFixed"
	fallibleHelper := "gopug.ToFixedStr"
	if methodName == "toPrecision" {
		totalHelper = "gopug.ToPrecision"
		fallibleHelper = "gopug.ToPrecisionStr"
	}

	switch typ.Kind() {
	case reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		g.needsGopug = true
		fConv := convertExpr(objGoExpr, typ, reflectTypeFloat64, "float64")
		return totalHelper + "(" + fConv + ", " + precArg + ")", false, nil
	case reflect.String:
		g.needsGopug = true
		sConv := convertExpr(objGoExpr, typ, reflectTypeString, "string")
		return fallibleHelper + "(" + sConv + ", " + precArg + ")", true, nil
	default:
		return "", false, fmt.Errorf("unsupported method %q in codegen value expression %q (receiver type %s is not numeric or string)", methodName, fullExpr, typ)
	}
}

// genStringArrayLiteral parses arrayExpr — the receiver of an
// `.indexOf`/`.includes`/`.contains` call the caller has already confirmed
// starts with "[" — as a JS-style array literal with STRING-LITERAL elements
// only, mirroring Runtime.evaluateExprRaw's own array-literal branch exactly:
// the same findMatchingCloseBracket/splitTopLevel top-level comma split, each
// element trimmed and required to be a plain quoted string literal
// (unwrapQuotedLiteral), so the parsed element set is byte-identical to what
// Runtime.evaluateExprRawAsStringSlice hands MethodIndexOfSlice/
// MethodIncludesSlice for the SAME literal. Each element is then emitted
// through strconv.Quote as a Go string literal, into a `[]string{...}`
// literal. Any element that is not itself a plain quoted string literal (a
// number, a bare identifier, a nested array/object literal, a template
// literal, an expression, …) is deferred: this increment only supports the
// STRING-array-literal shape the real manageGroupActive/navGroupActive idiom
// uses, not a general element-value compiler.
func (g *generator) genStringArrayLiteral(arrayExpr, fullExpr string) (string, error) {
	arrayExpr = strings.TrimSpace(arrayExpr)
	closeIdx := findMatchingCloseBracket(arrayExpr)
	if closeIdx != len(arrayExpr)-1 {
		return "", fmt.Errorf("unsupported array-literal receiver %q in codegen value expression %q", arrayExpr, fullExpr)
	}

	inner := strings.TrimSpace(arrayExpr[1 : len(arrayExpr)-1])
	if inner == "" {
		return "[]string{}", nil
	}

	parts := splitTopLevel(inner, ',')
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		lit, ok := unwrapQuotedLiteral(p)
		if !ok {
			return "", fmt.Errorf("unsupported array-literal element %q in codegen value expression %q (only a plain string-literal element is supported in this increment)", p, fullExpr)
		}
		quoted = append(quoted, strconv.Quote(lit))
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}", nil
}

// genFloat64ArrayLiteral is genStringArrayLiteral's numeric-literal
// counterpart: it parses arrayExpr as a JS-style array literal with
// NUMERIC-LITERAL elements only, using the same
// findMatchingCloseBracket/splitTopLevel top-level comma split, each element
// trimmed and required to parse via parseJSNumber — the exact parser the
// interpreter's own evaluateExprRaw array-literal branch ultimately falls
// through to for a bare numeric-literal element (ordinary decimal, hex,
// octal, scientific notation, …), so the parsed element set is byte-identical
// to what the interpreter would produce. Each element is emitted through
// formatCanonicalLiteral (never the original Pug token, for the same reason
// genOperand never does — see formatCanonicalLiteral's own doc comment) into
// a `[]float64{...}` literal. Any element that isn't itself a plain numeric
// literal (a quoted string, a bare identifier, a nested array/object literal,
// a template literal, an expression, …) is deferred: this function only
// supports the NUMERIC-array-literal shape, not a general element-value
// compiler.
func (g *generator) genFloat64ArrayLiteral(arrayExpr, fullExpr string) (string, error) {
	arrayExpr = strings.TrimSpace(arrayExpr)
	closeIdx := findMatchingCloseBracket(arrayExpr)
	if closeIdx != len(arrayExpr)-1 {
		return "", fmt.Errorf("unsupported array-literal receiver %q in codegen expression %q", arrayExpr, fullExpr)
	}

	inner := strings.TrimSpace(arrayExpr[1 : len(arrayExpr)-1])
	if inner == "" {
		return "[]float64{}", nil
	}

	parts := splitTopLevel(inner, ',')
	lits := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		f, ok := parseJSNumber(p)
		if !ok {
			return "", fmt.Errorf("unsupported array-literal element %q in codegen expression %q (only a plain numeric-literal element is supported in this increment)", p, fullExpr)
		}
		lits = append(lits, formatCanonicalLiteral(f))
	}
	return "[]float64{" + strings.Join(lits, ", ") + "}", nil
}

// genEachArrayLiteral emits genEach's handling of a whole-bracket-wrapped
// array literal used directly as an `each` collection (expr, already
// confirmed by genEach to span the whole CollectionExpr via
// findMatchingCloseBracket) — the other array-literal lever besides
// `.indexOf`/`.includes`/`.contains` iteration over a REAL slice/array
// method-call receiver. It splits the literal's elements with the same
// splitTopLevel top-level comma split genStringArrayLiteral/
// genFloat64ArrayLiteral use, classifying the WHOLE set in one pass: every
// element must be either a plain quoted string literal (unwrapQuotedLiteral)
// or a plain numeric literal (parseJSNumber), and every element must agree
// on which of those two kinds it is — a mix of both, or any element that is
// neither (a bare identifier, a field/dot-path, a nested array/object
// literal, a template literal, any other expression), is rejected rather
// than guessed at. An empty array literal (`[]`) is rejected too: with no
// element to infer a type from, there is no way to know what Go type the
// (never-executed, since a Go range over an empty slice runs zero times)
// loop body would need to compile the item variable against.
//
// A homogeneous string-literal array is compiled via genStringArrayLiteral
// into a `[]string`, elemTyp = string. A homogeneous numeric-literal array is
// compiled via genFloat64ArrayLiteral into a `[]float64` — the interpreter's
// own iteration of the SAME literal (Runtime.evaluateExprRaw's `[`-branch,
// which evaluates each element with a recursive evaluateExprRaw call that
// falls through, for a bare numeric-literal element, to
// `s, _ := r.evaluateExpr(expr); return s` — the SAME parseJSNumber-based
// canonical-decimal stringify genValueExpr's own bare-numeric-literal case
// uses) always renders a numeric-literal element in this same canonical
// decimal form, never the original token spelling, so modeling the element
// as a Go float64 and stringifying it through genScalarStringify's Float64
// case (strconv.FormatFloat with the same 'f', -1, 64 formatting) reproduces
// that canonical form exactly — elemTyp = float64. Either classification then
// hands collExpr and elemTyp to genEachLoopBody, the same loop-emission
// helper genEach's field-collection path uses, so the item variable (and,
// when n.IndexVar is set, the index variable too — modeled identically to
// the field-collection path, see genEachLoopBody's own doc comment) is
// scoped exactly like the field-collection path, and the loop body's
// existing scalar handling (a dynamic attribute value, `#{}`/`= expr`
// buffered code, a template literal `${...}` part, `if` truthiness) needs no
// new code at all to read either one back correctly. The numeric-literal
// classification additionally requires
// type-aware mode (a non-nil Config.DataReflectType): resolveFieldExpr only
// stringifies a scope var through its genuine reflect.Type in that mode, so
// a numeric item variable read back in type-blind mode would otherwise be
// used directly wherever a string is expected, invalid Go that GenerateGo's
// own gofmt-only pass has no way to catch. The string-literal classification
// has no such restriction: resolveFieldExpr's type-blind fallback returns
// the scope var's bare Go identifier unconverted, which is already a valid
// Go string, so it works correctly whether or not the generator is
// type-aware.
func (g *generator) genEachArrayLiteral(n *EachNode, expr string) error {
	inner := strings.TrimSpace(expr[1 : len(expr)-1])
	if inner == "" {
		return fmt.Errorf("unsupported empty array-literal each collection %q in codegen", n.CollectionExpr)
	}

	parts := splitTopLevel(inner, ',')
	allString, allNumeric := true, true
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if _, ok := unwrapQuotedLiteral(p); ok {
			allNumeric = false
			continue
		}
		if _, ok := parseJSNumber(p); ok {
			allString = false
			continue
		}
		return fmt.Errorf("unsupported array-literal each element %q in codegen each collection %q (only a plain string-literal or numeric-literal element is supported in this increment)", p, n.CollectionExpr)
	}

	var collExpr string
	var elemTyp reflect.Type
	var err error
	switch {
	case allString && !allNumeric:
		collExpr, err = g.genStringArrayLiteral(expr, n.CollectionExpr)
		elemTyp = reflectTypeString
	case allNumeric && !allString:
		// A numeric-literal loop item variable only stringifies correctly
		// through genScalarStringify's Float64 case, which resolveFieldExpr
		// reaches only in type-aware mode: in type-blind mode (rootType ==
		// nil) resolveFieldExpr deliberately discards every scope var's
		// type (see its own doc comment), so a later read of this item
		// variable would emit the raw float64 Go value directly wherever a
		// string is expected — invalid Go that GenerateGo's own gofmt-only
		// formatting pass cannot catch, only a real `go build` would. Since
		// genNumericExpr/genUnbufferedAssign impose this exact same
		// type-blind restriction on a numeric `- var` local for the
		// identical reason, requiring it here too is consistent, not an
		// additional restriction unique to this feature.
		if g.rootType == nil {
			return fmt.Errorf("unsupported numeric array-literal each collection %q in codegen under a nil DataReflectType (type-blind mode)", n.CollectionExpr)
		}
		collExpr, err = g.genFloat64ArrayLiteral(expr, n.CollectionExpr)
		elemTyp = reflectTypeFloat64
	default:
		err = fmt.Errorf("unsupported mixed string/numeric array-literal each collection %q in codegen (elements must be all string literals or all numeric literals)", n.CollectionExpr)
	}
	if err != nil {
		return err
	}

	return g.genEachLoopBody(n, collExpr, elemTyp)
}

// genArrayIndexOfValueExpr emits genMethodCall's handling of
// `<arrayLiteral>.indexOf(<arg>)` / `.includes(<arg>)` / `.contains(<arg>)`,
// recognized when objExpr is itself a string-literal array literal (see
// genStringArrayLiteral). It compiles the array literal to a Go []string and
// the argument to a Go string via genTotalArg (a TOTAL genValueExpr result —
// exactly what Runtime.evaluateExpr's own "indexOf"/"includes"/"contains"
// case passes to the corresponding helper, see MethodIndexOfSlice/
// MethodIncludesSlice's own doc comments), then calls the exported
// gopug.MethodIndexOfSlice/gopug.MethodIncludesSlice directly — the SAME
// helper Runtime's own array-receiver branch calls, so the two paths can
// never drift apart. A 0-arg call passes the interpreter's own no-argument
// default ("-1" for indexOf, "false" for includes/contains) without even
// resolving the array literal, matching Runtime.evaluateExpr's own early
// return for that shape.
func (g *generator) genArrayIndexOfValueExpr(objExpr, methodName, argsStr, fullExpr string) (string, bool, error) {
	if argsStr == "" {
		if methodName == "indexOf" {
			return `"-1"`, false, nil
		}
		return `"false"`, false, nil
	}

	elemsGoExpr, err := g.genStringArrayLiteral(objExpr, fullExpr)
	if err != nil {
		return "", false, err
	}
	needle, err := g.genTotalArg(argsStr, fullExpr)
	if err != nil {
		return "", false, err
	}

	g.needsGopug = true
	if methodName == "indexOf" {
		return "gopug.MethodIndexOfSlice(" + elemsGoExpr + ", " + needle + ")", false, nil
	}
	return "gopug.MethodIncludesSlice(" + elemsGoExpr + ", " + needle + ")", false, nil
}

// isArrayLiteralMethodCall reports whether expr is a bare
// `<arrayLiteral>.<methodName>(...)` call — the receiver starts with "[" and
// the method name (before the "(") is one of wantMethods — without building
// any Go code for it. It is the shared shape test genOperand (for
// `.indexOf` in a numeric comparison operand) and genOperandTruthiness/
// genBoolExpr/isProvablyBoolOperand (for a bare `.includes`/`.contains` bool
// value) use to decide whether to route an expression through
// genArrayIndexOfValueExpr at all.
func isArrayLiteralMethodCall(expr string, wantMethods ...string) bool {
	dotIdx := findTopLevelDot(expr)
	if dotIdx <= 0 {
		return false
	}
	objExpr := strings.TrimSpace(expr[:dotIdx])
	if !strings.HasPrefix(objExpr, "[") {
		return false
	}
	rest := expr[dotIdx+1:]
	before, _, found := strings.Cut(rest, "(")
	if !found {
		return false
	}
	methodName := strings.TrimSpace(before)
	for _, m := range wantMethods {
		if methodName == m {
			return true
		}
	}
	return false
}

// splitArrayLiteralMethodCall splits expr (already confirmed by
// isArrayLiteralMethodCall to be a bare `<arrayLiteral>.<methodName>(...)`
// call) into its receiver, method name, and argument text, the same way
// genValueExpr's own dot-handling block and genMethodCall do.
func splitArrayLiteralMethodCall(expr string) (objExpr, methodName, argsStr string) {
	dotIdx := findTopLevelDot(expr)
	objExpr = strings.TrimSpace(expr[:dotIdx])
	rest := expr[dotIdx+1:]
	before, inner, _ := strings.Cut(rest, "(")
	methodName = strings.TrimSpace(before)
	argsStr, _, _ = strings.Cut(strings.TrimSpace(inner), ")")
	argsStr = strings.TrimSpace(argsStr)
	return objExpr, methodName, argsStr
}

// tryArrayIndexOfNumericOperand recognizes a bare array-literal
// `.indexOf(...)` call as a condition/comparison operand — the receiver end
// of the manageGroupActive/navGroupActive `!== -1` idiom — and, if expr
// matches that shape, compiles it to a Go int expression via
// genArrayIndexOfValueExpr (which already emits the single-sourced
// gopug.MethodIndexOfSlice call) plus a guaranteed-safe strconv.Atoi of its
// decimal-integer result: MethodIndexOfSlice always returns either "-1" or a
// non-negative decimal index, never a value Atoi can fail to parse, so the
// parse error is intentionally discarded, the same way genLengthOperand's own
// len()/RuneCountInString wrapper never guards a computation that can't fail.
// ok is false when expr is not this shape (including a bare `.includes`/
// `.contains` call, which returns a bool-shaped result rather than a numeric
// index and is handled separately, in genOperandTruthiness), so genOperand's
// other cases apply unchanged.
func (g *generator) tryArrayIndexOfNumericOperand(expr string) (goExpr string, ok bool, err error) {
	if !isArrayLiteralMethodCall(expr, "indexOf") {
		return "", false, nil
	}
	objExpr, methodName, argsStr := splitArrayLiteralMethodCall(expr)

	callExpr, fallible, err := g.genArrayIndexOfValueExpr(objExpr, methodName, argsStr, expr)
	if err != nil {
		return "", false, err
	}
	if fallible {
		return "", false, fmt.Errorf("unsupported array %q call in codegen condition %q (a fallible result is not supported here)", methodName, expr)
	}

	g.needsStrconv = true
	n := g.nextTmp()
	iv := fmt.Sprintf("__i%d", n)
	return fmt.Sprintf("func() int {\n\t%s, _ := strconv.Atoi(%s)\n\treturn %s\n}()", iv, callExpr, iv), true, nil
}

// genTernaryValueExpr emits a value-context ternary `cond ? a : b` (expr,
// with idx the top-level '?' findTernary already located) as an
// immediately-invoked function literal. When BOTH branches are total (the
// common case, unchanged since the ternary's introduction) it returns
// string, exactly as before:
//
//	func() string {
//		if <genCondition(cond)> {
//			return <genValueExpr(trueBranch)>
//		}
//		return <genValueExpr(falseBranch)>
//	}()
//
// When EITHER branch is fallible (its own genValueExpr reports a top-level
// `/` or `%` reachable through it), the IIFE instead returns (string, error)
// — Pattern 2, the short-circuit composition shape — and each branch's
// fallible extraction lives INSIDE that branch's own if/else arm, via
// genFallibleExtractionInline:
//
//	func() (string, error) {
//		if <genCondition(cond)> {
//			__v0, __err0 := <trueBranch.goExpr>   // only if trueBranch is fallible
//			if __err0 != nil { return "", __err0 }
//			return <trueResolved>, nil
//		}
//		__v1, __err1 := <falseBranch.goExpr>       // only if falseBranch is fallible
//		if __err1 != nil { return "", __err1 }
//		return <falseResolved>, nil
//	}()
//
// This is the load-bearing property the whole ternary rests on: because a
// branch's fallible extraction sits inside that branch's own arm rather than
// up front, it only ever runs when that branch is actually taken — a
// division by zero in the UNTAKEN branch never executes and never errors,
// exactly matching Runtime.evaluateExpr's own short-circuit ternary
// (runtime.go:2128): the condition is isTruthy(evaluateExpr(cond)), reused
// unchanged here via genCondition; only the taken branch's evaluateExpr
// call ever runs, matched here by only the taken branch's extraction (or
// bare return, for a total branch) ever executing. A missing top-level ':'
// after the '?' is a malformed ternary and returns an error, mirroring the
// interpreter's own "malformed ternary expression" message. An error from
// the condition or either branch (a shape genCondition or genValueExpr can't
// compile at all — arithmetic in the condition, an unsupported operator in a
// branch) propagates unchanged.
func (g *generator) genTernaryValueExpr(expr string, idx int) (goExpr string, fallible bool, err error) {
	rest := expr[idx+1:]
	colonIdx := findBinaryOp(rest, ":")
	if colonIdx < 0 {
		return "", false, fmt.Errorf("malformed ternary expression in codegen value expression: %s", expr)
	}

	condExpr, err := g.genCondition(expr[:idx])
	if err != nil {
		return "", false, err
	}
	trueExpr, trueFallible, err := g.genValueExpr(rest[:colonIdx])
	if err != nil {
		return "", false, err
	}
	falseExpr, falseFallible, err := g.genValueExpr(rest[colonIdx+1:])
	if err != nil {
		return "", false, err
	}

	if !trueFallible && !falseFallible {
		return fmt.Sprintf("func() string {\n\tif %s {\n\t\treturn %s\n\t}\n\treturn %s\n}()", condExpr, trueExpr, falseExpr), false, nil
	}

	var b strings.Builder
	b.WriteString("func() (string, error) {\n")
	fmt.Fprintf(&b, "if %s {\n", condExpr)
	resolvedTrue := trueExpr
	if trueFallible {
		resolvedTrue = g.genFallibleExtractionInline(&b, trueExpr)
	}
	fmt.Fprintf(&b, "return %s, nil\n", resolvedTrue)
	b.WriteString("}\n")
	resolvedFalse := falseExpr
	if falseFallible {
		resolvedFalse = g.genFallibleExtractionInline(&b, falseExpr)
	}
	fmt.Fprintf(&b, "return %s, nil\n", resolvedFalse)
	b.WriteString("}()")
	return b.String(), true, nil
}

// genTemplateLiteral emits a Go string expression for a backtick template
// literal — expr is the whole literal genValueExpr received, including its
// opening backtick (literal text, any number of ${expr} interpolations, up
// to the next unescaped backtick). It walks expr's content exactly the way
// Runtime.evaluateExpr's own template-literal branch does — a backslash
// immediately before a backtick passes that backtick through literally
// (rather than closing the literal), a `${...}` (matched with nested-brace
// depth, mirroring an unclosed `${` by emitting the literal `$` byte and
// continuing the scan rather than erroring) is recursively compiled through
// genValueExpr, and a run of literal bytes becomes a Go string literal — so
// it splits the content into the exact same sequence of literal-versus-
// interpolated segments the interpreter's own walk produces. The segments
// are joined with native Go `+` (never gopug.Add: template-literal
// concatenation is unconditional, unlike the runtime-value-dependent `+`
// operator). An empty literal (no segments at all) emits `""`. A `${}` whose
// inner expression is outside genValueExpr's supported grammar propagates
// that "unsupported" error rather than guessing — a generate-time failure,
// not a divergence. A part genValueExpr marks fallible (a `/` or `%`
// reachable through it, however deep) is handled differently, matching an
// empirically-verified quirk of Runtime.evaluateExpr's own template-literal
// walk: it evaluates each `${...}` part with `val, _ := r.evaluateExpr(interp)`,
// discarding any error rather than propagating it or aborting Render — so a
// division/modulo-by-zero inside a `${}` part renders that segment as the
// empty string and the surrounding literal (and the whole render) still
// succeeds. genFallibleTemplatePart reproduces this exactly: it wraps the
// part's (string, error) goExpr in its own small IIFE that extracts the
// value and discards the error, so the resulting part is always a plain,
// never-fallible string, joined into the rest with the same native Go `+`
// as every other segment — genTemplateLiteral's own result therefore always
// reports fallible=false to its caller, regardless of whether any part was
// itself fallible. Escaping (html.EscapeString or EscapeAttr) is the
// caller's job, applied once to the whole result, exactly as for every other
// genValueExpr leaf.
func (g *generator) genTemplateLiteral(expr string) (string, error) {
	inner := expr[1:]
	var parts []string
	var literal strings.Builder

	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		parts = append(parts, strconv.Quote(literal.String()))
		literal.Reset()
	}

	i := 0
	for i < len(inner) {
		if inner[i] == '`' {
			break // closing backtick
		}
		if inner[i] == '\\' && i+1 < len(inner) {
			// escape sequence — pass the next char through literally
			literal.WriteByte(inner[i+1])
			i += 2
			continue
		}
		if inner[i] == '$' && i+1 < len(inner) && inner[i+1] == '{' {
			// find the matching closing brace, respecting nesting
			depth := 1
			j := i + 2
			for j < len(inner) && depth > 0 {
				if inner[j] == '{' {
					depth++
				} else if inner[j] == '}' {
					depth--
				}
				j++
			}
			if depth > 0 {
				// Unclosed ${: no matching brace found — emit the literal
				// character and let the rest of the string render as-is.
				literal.WriteByte(inner[i])
				i++
				continue
			}
			interp := strings.TrimSpace(inner[i+2 : j-1])
			valExpr, partFallible, err := g.genValueExpr(interp)
			if err != nil {
				return "", fmt.Errorf("template literal %q: %w", expr, err)
			}
			if partFallible {
				valExpr = g.genFallibleTemplatePart(valExpr)
			}
			flushLiteral()
			parts = append(parts, valExpr)
			i = j
			continue
		}
		literal.WriteByte(inner[i])
		i++
	}
	flushLiteral()

	if len(parts) == 0 {
		return `""`, nil
	}
	return strings.Join(parts, " + "), nil
}

// genScalarStringify returns a Go expression of type string that stringifies
// goExpr the way Runtime.lookupAndStringify would for a field of type typ —
// exactly genInterpolation's former per-type dispatch, extracted so
// genValueExpr's field/dot-path leaf case can share it, except a string
// value is returned bare rather than wrapped in html.EscapeString: escaping
// is the caller's job (genInterpolation and genCode both apply it to the
// whole value they build, not to each leaf), since an unescaped scalar
// nested inside a `+` still needs the concatenation's escaping to happen
// once, on the final result, not per-operand. typ nil (type-blind mode)
// assumes a string field, matching the untyped codegen skeleton's original
// behavior.
func (g *generator) genScalarStringify(goExpr string, typ reflect.Type) (string, error) {
	if typ == nil {
		return goExpr, nil
	}
	switch typ.Kind() {
	case reflect.String:
		return convertExpr(goExpr, typ, reflectTypeString, "string"), nil
	case reflect.Bool:
		g.needsStrconv = true
		return "strconv.FormatBool(" + convertExpr(goExpr, typ, reflectTypeBool, "bool") + ")", nil
	case reflect.Int:
		g.needsStrconv = true
		return "strconv.Itoa(" + convertExpr(goExpr, typ, reflectTypeInt, "int") + ")", nil
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		g.needsStrconv = true
		return "strconv.FormatInt(int64(" + goExpr + "), 10)", nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		g.needsStrconv = true
		return "strconv.FormatUint(uint64(" + goExpr + "), 10)", nil
	case reflect.Float64:
		g.needsStrconv = true
		return "strconv.FormatFloat(" + convertExpr(goExpr, typ, reflectTypeFloat64, "float64") + ", 'f', -1, 64)", nil
	default:
		return "", fmt.Errorf("unsupported non-scalar field type %s in codegen", typ)
	}
}

// resolveFieldExpr translates a Pug bare identifier or dot-path into the
// equivalent Go expression against the data parameter d, taking any
// currently bound each-loop variables/`- var` locals into account, and —
// when the generator is type-aware (rootType != nil) — resolves the
// expression's reflect.Type by walking the struct fields along the path (an
// each-loop variable's own type if the first segment is bound, otherwise
// rootType), dereferencing pointers at each step. In type-blind mode
// (rootType == nil) the returned type is always nil, preserving the untyped
// skeleton's behavior exactly. Anything that isn't a bare identifier or
// dot-path (an operator, method call, literal, index expression, …), or a
// dot-path segment that isn't a field of the struct type it's resolved
// against, is out of scope for this increment and returns an error — as is
// a first segment recorded in leakedVarNames (a `- var` local that went out
// of scope at a point where the interpreter's own scoping might still keep
// it live; see scopeRestore's doc comment), checked BEFORE ever falling
// through to struct-field resolution so a coincidentally-matching field
// name is never silently substituted for it. While g.paramOnlyScope is set
// — generating a mixin body (see genMixinFunc), where g.scope holds the
// mixin's own parameters, or generating a call site's block-content closure
// (see genMixinBlockClosure), where g.scope is empty — a scope-stack miss is
// ALWAYS an error, even before the leakedVarNames check: in the mixin-body
// case, the body's scope holds only its own parameters, so a reference to
// anything else (a top-level data field, a caller's `- var` local) must
// never fall through to struct-field resolution against d; in the
// block-closure case, EVERY identifier is a scope-stack miss by
// construction (the scope is empty), so this same guard is what makes pure
// static block content the only admitted shape — either way, falling
// through would silently disagree with the interpreter's own isolated
// mixin scope (Runtime.renderMixinCall/renderMixinBlockSlot).
func (g *generator) resolveFieldExpr(expr string) (string, reflect.Type, error) {
	expr = strings.TrimSpace(expr)

	shape, val := classifySimpleShape(expr)
	var segments []string
	switch shape {
	case shapeIdentifier:
		segments = []string{val}
	case shapeDotPath:
		segments = strings.Split(val, ".")
	default:
		return "", nil, fmt.Errorf("unsupported expression in codegen: %q (only bare identifiers and dot-paths of fields are supported in this increment)", expr)
	}

	first := segments[0]
	if sv, ok := g.lookupScope(first); ok {
		if g.rootType == nil {
			return val, nil, nil
		}
		typ, goPath, err := resolveFieldPath(sv.typ, expr, segments[1:])
		if err != nil {
			return "", nil, err
		}
		goExpr := sv.goName
		if goPath != "" {
			goExpr += "." + goPath
		}
		return goExpr, typ, nil
	}

	if g.paramOnlyScope {
		return "", nil, fmt.Errorf("reference to %q is not resolvable in this isolated scope (a mixin body can only see its declared parameters, and block content passed to a mixin call is generated in a fail-closed empty scope — dynamic block content is not supported in codegen yet)", expr)
	}

	if g.leakedVarNames[first] {
		return "", nil, fmt.Errorf("unsupported reference to %q in codegen: an unbuffered `- var %s` was declared inside an if/tag/each block the interpreter does not scope the same way codegen does, so it may still be live there at runtime even though codegen's own scope has already closed it — not supported yet", first, first)
	}

	if g.rootType == nil {
		return "d." + val, nil, nil
	}
	typ, goPath, err := resolveFieldPath(g.rootType, expr, segments)
	if err != nil {
		return "", nil, err
	}
	return "d." + goPath, typ, nil
}

// genEach emits a for-range loop over a slice field, or — the other
// array-literal lever besides `.indexOf`/`.includes`/`.contains` — over a
// whole-bracket-wrapped array literal collection (`each x in ["a", "b"]` /
// `each x in [1, 2, 3]`), delegated to genEachArrayLiteral. The
// single-variable form (`each x in <field>`) and the two-variable
// INDEX-variable form (`each x, i in <field>`) are both supported for both a
// slice/array field and an array-literal collection. An `each`/`else`
// empty-collection body is not supported in this increment, with or without
// an index variable. In type-aware mode, the loop item variable is scoped to
// the collection field's element type (dereferencing pointers on the
// collection and/or its element), so a dot-path rooted at the item
// variable inside the loop body resolves correctly too.
//
// The index variable is modeled as a STRING scope var holding
// `strconv.Itoa(<rawIntLocal>)`, mirroring Runtime.renderEach's own
// `scope[IndexVar] = strconv.Itoa(i)` (runtime.go) exactly — NOT as a raw
// Go int. This is load-bearing, not a style choice: it's what makes every
// downstream use of the index variable agree with the interpreter by
// construction — `#{i}`/a template literal stringifies to the same decimal
// text the interpreter stored, `if i` truthiness routes through
// gopug.Truthy (whose falsy set includes exactly "0", so the FIRST loop
// iteration's index is falsy, matching the interpreter precisely), and a
// numeric-literal comparison (`i == 0`, `i > 1`) is handled by a dedicated
// genComparison case keyed off scopeVar.indexRawGoName that compiles
// against the underlying raw int local instead of the string — see that
// case's own comment for why that's still exactly equivalent to
// compareValues' numeric-string coercion, not a shortcut around it.
func (g *generator) genEach(n *EachNode) error {
	if n.ItemVar == "" {
		return fmt.Errorf("unsupported each without an item variable in codegen")
	}
	if len(n.EmptyBody) > 0 {
		return fmt.Errorf("unsupported each/else in codegen")
	}

	collTrim := strings.TrimSpace(n.CollectionExpr)
	if strings.HasPrefix(collTrim, "[") && findMatchingCloseBracket(collTrim) == len(collTrim)-1 {
		return g.genEachArrayLiteral(n, collTrim)
	}

	collExpr, collTyp, err := g.resolveFieldExpr(n.CollectionExpr)
	if err != nil {
		return err
	}

	var elemTyp reflect.Type
	if collTyp != nil {
		ct := derefType(collTyp)
		if ct.Kind() != reflect.Slice && ct.Kind() != reflect.Array {
			return fmt.Errorf("unsupported each over non-slice field %q (%s) in codegen", n.CollectionExpr, collTyp)
		}
		elemTyp = derefType(ct.Elem())
	}

	return g.genEachLoopBody(n, collExpr, elemTyp)
}

// genEachLoopBody emits the for-range loop itself — the single-variable
// `for _, ItemVar := range collExpr` form when n.IndexVar is empty, or the
// two-variable index form otherwise — plus the item/index scope pushes and
// the body walk. It is the single source both genEach's field-collection
// path and genEachArrayLiteral's array-literal-collection path call, so an
// each loop with an index variable emits pattern-identical Go regardless of
// which kind of collection it ranges over — one implementation, not two
// hand-maintained copies that could drift apart.
//
// The index variable is modeled as a STRING scope var holding
// `strconv.Itoa(<rawIntLocal>)`, mirroring Runtime.renderEach's own
// `scope[IndexVar] = strconv.Itoa(i)` (runtime.go) exactly — NOT as a raw Go
// int. This is load-bearing, not a style choice: it's what makes every
// downstream use of the index variable agree with the interpreter by
// construction — `#{i}`/a template literal stringifies to the same decimal
// text the interpreter stored, `if i` truthiness routes through
// gopug.Truthy (whose falsy set includes exactly "0", so the FIRST loop
// iteration's index is falsy, matching the interpreter precisely), and a
// numeric-literal comparison (`i == 0`, `i > 1`) is handled by a dedicated
// genComparison case keyed off scopeVar.indexRawGoName that compiles
// against the underlying raw int local instead of the string — see that
// case's own comment for why that's still exactly equivalent to
// compareValues' numeric-string coercion, not a shortcut around it.
func (g *generator) genEachLoopBody(n *EachNode, collExpr string, elemTyp reflect.Type) error {
	if n.IndexVar == "" {
		g.writeRaw(fmt.Sprintf("for _, %s := range %s {\n", n.ItemVar, collExpr))
		// The body may not reference the item variable at all — a counting
		// loop whose body is only `- n++` never reads it — which the
		// interpreter itself imposes no restriction against, so the range
		// variable is unconditionally blank-used right after declaration,
		// exactly like the two-variable index form below already does for
		// both of its range variables.
		g.body.WriteString(fmt.Sprintf("_ = %s\n", n.ItemVar))
		mark := g.scopeMark()
		g.pushScope(n.ItemVar, n.ItemVar, elemTyp, false)
		for _, child := range n.Body {
			if err := g.genNode(child); err != nil {
				g.scopeRestore(mark)
				return err
			}
		}
		g.scopeRestore(mark)
		g.flushStatic()
		g.body.WriteString("}\n")
		return nil
	}

	idxTmp := fmt.Sprintf("__idx%d", g.nextTmp())
	g.writeRaw(fmt.Sprintf("for %s, %s := range %s {\n", idxTmp, n.ItemVar, collExpr))
	g.needsStrconv = true
	g.body.WriteString(fmt.Sprintf("%s := strconv.Itoa(%s)\n", n.IndexVar, idxTmp))
	// The body may reference neither, either, or both of the loop's two Go
	// range variables (the interpreter imposes no such restriction — it
	// just binds both into a plain scope map regardless of whether the
	// body reads them), so both are unconditionally blank-used right after
	// declaration — the same `_ = goName` pattern genUnbufferedAssign uses
	// to keep a possibly-unread `- var` local from tripping Go's
	// declared-and-not-used check.
	g.body.WriteString(fmt.Sprintf("_ = %s\n", n.IndexVar))
	g.body.WriteString(fmt.Sprintf("_ = %s\n", n.ItemVar))
	mark := g.scopeMark()
	g.pushScope(n.ItemVar, n.ItemVar, elemTyp, false)
	g.pushIndexScope(n.IndexVar, n.IndexVar, idxTmp)
	for _, child := range n.Body {
		if err := g.genNode(child); err != nil {
			g.scopeRestore(mark)
			return err
		}
	}
	g.scopeRestore(mark)
	g.flushStatic()
	g.body.WriteString("}\n")
	return nil
}

// genComment emits a buffered (`//`) comment's rendered form as static text
// matching renderComment exactly: "<!-- " + Content + " -->", written RAW
// (never HTML-escaped — renderComment writes Content straight into the
// output buffer). An unbuffered (`//-`) comment emits nothing, matching
// renderComment's no-op branch for it.
func (g *generator) genComment(n *CommentNode) error {
	if n.Buffered {
		g.writeStatic("<!-- " + n.Content + " -->")
	}
	return nil
}

// genConditional emits a Go if/else for `if <condition>` (or, when
// n.IsUnless is set, `unless <condition>`) with an optional plain `else`,
// including an else-if chain (`else if`, represented in the AST as a single
// *ConditionalNode with IsElseIf set inside Alternate — see ast.go's
// ConditionalNode doc comment). The recursive genNode call on that inner
// *ConditionalNode nests a second if/else inside the outer else block (`if a
// {...} else { if b {...} else {...} }`), which is valid Go and renders
// byte-identically to the interpreter's own equivalent nesting of else-if —
// the extra braces only affect Go source structure, never the HTML output.
// The condition itself is translated by genCondition, which supports bare
// bool/numeric/string-field truthiness, `.length` truthiness, a bounded set
// of comparison operators, and the `&&`/`||`/`!` combinators over any of
// those; anything else returns an error (including an else-if whose own
// condition genCondition can't compile, which propagates that error
// unchanged). `unless` reuses the exact same translated condition, wrapped in
// a Go `!(...)`: Runtime.renderConditional computes boolVal :=
// isTruthy(evaluateExpr(condition)) and then, only for `unless`, negates it
// before branching — genCondition already returns a native Go bool equal to
// isTruthy(val) for every shape it compiles, so `!(cond)` is that same
// negation, byte-identical, with Consequent/Alternate handled completely
// unchanged from the `if` path (an `unless`'s Alternate is its `else`, which
// the parser accepts identically to an `if`'s).
func (g *generator) genConditional(n *ConditionalNode) error {
	cond, err := g.genCondition(n.Condition)
	if err != nil {
		return err
	}

	if n.IsUnless {
		g.writeRaw(fmt.Sprintf("if !(%s) {\n", cond))
	} else {
		g.writeRaw(fmt.Sprintf("if %s {\n", cond))
	}
	mark := g.scopeMark()
	for _, child := range n.Consequent {
		if err := g.genNode(child); err != nil {
			g.scopeRestore(mark)
			return err
		}
	}
	g.scopeRestore(mark)
	g.flushStatic()

	if len(n.Alternate) > 0 {
		g.body.WriteString("} else {\n")
		for _, child := range n.Alternate {
			if err := g.genNode(child); err != nil {
				g.scopeRestore(mark)
				return err
			}
		}
		g.scopeRestore(mark)
		g.flushStatic()
	}

	g.body.WriteString("}\n")
	return nil
}

// genCase emits a `case`/`when`/`default` block. Runtime.renderCase evaluates
// the case expression once (caseVal), then walks c.Cases in source order:
// whenVal := evaluateExpr(when.Expression), and if caseVal == whenVal (STRING
// equality — both sides come from evaluateExpr's own stringification) it sets
// a sticky `matched` flag that is only ever set, never cleared. While matched,
// an empty-bodied when falls through (its "continue" reaches the next when's
// own comparison, still under the same sticky matched flag), while a
// non-empty matched when renders its body and stops for good; c.Default
// renders whenever the walk reaches the end of c.Cases without having
// rendered a non-empty matched when's body (whether because nothing ever
// matched, or because every matched when's body was empty and fall-through
// ran off the end).
//
// Whether a when's body is empty is known at GENERATE time (len(when.Body)),
// so the "does this empty when fall through" question needs no runtime
// state — only two runtime bools are needed: matchedTmp, mirroring the
// interpreter's own sticky `matched` (set, never cleared, by each when's
// comparison in turn), and doneTmp, which goes true the moment a non-empty
// matched when's body has rendered and short-circuits every later when's body
// (and the final default) exactly the way the interpreter's own `return nil`
// does after rendering a matched non-empty when. genValueExpr reproduces
// evaluateExpr's stringification for every construct it supports, so
// caseTmp == whenExpr in the generated Go is the same STRING comparison the
// interpreter performs, byte for byte. Every genValueExpr call this function
// makes for a when expression is unconditionally evaluated (not short-
// circuited the way the interpreter's own loop stops as soon as it renders a
// body) — harmless only because a non-fallible Go expression can't error or
// have an observable side effect, so evaluating a later when's expression
// that the interpreter would never have reached produces no divergence.
//
// A case expression or any when expression that genValueExpr reports as
// fallible (can produce a runtime error, e.g. a division) is refused outright
// rather than generated: the interpreter can abort partway through the when
// walk the instant such an expression errors, at whatever point in that walk
// it's reached, and codegen has no way to reproduce that exact error and its
// position faithfully — so the whole template is deferred instead of risking
// a silently different error (or none at all).
func (g *generator) genCase(n *CaseNode) error {
	caseExpr, caseFallible, err := g.genValueExpr(n.Expression)
	if err != nil {
		return fmt.Errorf("unsupported case expression %q in codegen: %w", n.Expression, err)
	}
	if caseFallible {
		return fmt.Errorf("unsupported fallible case expression %q in codegen: the interpreter could abort with an error partway through the when clauses, a point codegen cannot reproduce", n.Expression)
	}

	whenExprs := make([]string, len(n.Cases))
	for i, when := range n.Cases {
		whenExpr, whenFallible, err := g.genValueExpr(when.Expression)
		if err != nil {
			return fmt.Errorf("unsupported when expression %q in codegen: %w", when.Expression, err)
		}
		if whenFallible {
			return fmt.Errorf("unsupported fallible when expression %q in codegen: the interpreter could abort with an error partway through the when clauses, a point codegen cannot reproduce", when.Expression)
		}
		whenExprs[i] = whenExpr
	}

	if len(n.Cases) == 0 {
		// No `when` clauses at all: the interpreter's own loop over c.Cases
		// never runs, so `matched` stays false and c.Default renders
		// unconditionally whenever it's non-empty. caseExpr is still emitted
		// as a blank-identifier discard (never a named local) so it neither
		// trips Go's declared-and-not-used check nor leaves needsGopug/
		// needsStrconv true with nothing in the generated source that
		// actually references the package they gate.
		g.writeRaw(fmt.Sprintf("_ = %s\n", caseExpr))
		if len(n.Default) == 0 {
			return nil
		}
		mark := g.scopeMark()
		for _, child := range n.Default {
			if err := g.genNode(child); err != nil {
				g.scopeRestore(mark)
				return err
			}
		}
		g.scopeRestore(mark)
		g.flushStatic()
		return nil
	}

	caseTmp := fmt.Sprintf("__caseVal%d", g.nextTmp())
	matchedTmp := fmt.Sprintf("__matched%d", g.nextTmp())
	doneTmp := fmt.Sprintf("__done%d", g.nextTmp())

	g.writeRaw(fmt.Sprintf("%s := %s\n", caseTmp, caseExpr))
	g.body.WriteString(fmt.Sprintf("%s := false\n", matchedTmp))
	g.body.WriteString(fmt.Sprintf("%s := false\n", doneTmp))

	mark := g.scopeMark()
	for i, when := range n.Cases {
		g.body.WriteString(fmt.Sprintf("if %s == %s {\n%s = true\n}\n", caseTmp, whenExprs[i], matchedTmp))
		g.body.WriteString(fmt.Sprintf("if %s && !%s {\n", matchedTmp, doneTmp))
		if len(when.Body) > 0 {
			for _, child := range when.Body {
				if err := g.genNode(child); err != nil {
					g.scopeRestore(mark)
					return err
				}
			}
			g.scopeRestore(mark)
			g.flushStatic()
			g.body.WriteString(fmt.Sprintf("%s = true\n", doneTmp))
		}
		g.body.WriteString("}\n")
	}

	g.body.WriteString(fmt.Sprintf("if !%s {\n", doneTmp))
	if len(n.Default) > 0 {
		for _, child := range n.Default {
			if err := g.genNode(child); err != nil {
				g.scopeRestore(mark)
				return err
			}
		}
		g.scopeRestore(mark)
		g.flushStatic()
	}
	g.body.WriteString("}\n")

	return nil
}

// conditionComparisonOps lists the comparison operators genCondition
// recognizes, in the exact order Runtime.evaluateExpr scans for them
// (runtime.go's own literal op list), so a top-level operator is identified
// the same way in both places.
var conditionComparisonOps = []string{"===", "!==", "==", "!=", "<=", ">=", "<", ">"}

// isFullyParenthesizedExpr reports whether expr is a single, whole-value
// parenthesized expression: expr's first byte is `(`, and the `)` that
// closes it (by depth, at the same single-layer check stripBalancedOuterParens's
// own loop body performs) is expr's very last byte. This is a structural,
// content-blind test — it says nothing about what is inside the parens,
// only that nothing precedes or follows them — which is exactly why a value
// satisfying it can never carry a leading static-class prefix (`card (...)`
// has a non-`(` first byte) or a trailing top-level operator (`(a) + (b)`
// has the first `)` close before the last byte, at index 2, not the end).
func isFullyParenthesizedExpr(expr string) bool {
	if len(expr) < 2 || expr[0] != '(' {
		return false
	}
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i == len(expr)-1
			}
		}
	}
	return false
}

// stripBalancedOuterParens strips one or more layers of a fully-balanced
// enclosing `(...)` pair from expr, mirroring the paren-unwrap loop at the
// top of Runtime.evaluateExpr exactly (including its "isWrapped" check,
// which refuses to strip `(a) + (b)` — the parens there don't enclose the
// whole expression even though it starts with `(` and ends with `)`).
func stripBalancedOuterParens(expr string) string {
	for len(expr) >= 2 && expr[0] == '(' && expr[len(expr)-1] == ')' {
		depth := 0
		isWrapped := true
		for i, ch := range expr {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 && i < len(expr)-1 {
					isWrapped = false
					break
				}
			}
		}
		if !isWrapped {
			break
		}
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	return expr
}

// genCondition translates a Pug `if`/`else` condition expression into a Go
// boolean expression, reproducing the interpreter's compareValues/isTruthy
// semantics exactly for the bounded subset this increment supports, and
// erroring on everything else instead of guessing.
//
// It mirrors Runtime.evaluateExpr's own operator-precedence scan, in the
// same order: strip balanced outer parens, then check (in turn) for a
// top-level ternary, `||`, `&&`, one of the eight comparison operators, and
// a leading unary `!`.
//
// A top-level ternary changes evaluateExpr's returned VALUE in a way
// genCondition can't yet reproduce (it would need a value-context ternary
// compiler), so that still returns an error. `||`, `&&`, and a leading `!`
// are different: in CONDITION position the only thing that matters about
// their result is its truthiness, and applying isTruthy to each of the
// interpreter's own value-returning definitions collapses to native
// boolean-combinator semantics —
//
//	isTruthy(a || b) == isTruthy(a) || isTruthy(b)   (evaluateExpr's || returns
//	                                                   left if isTruthy(left),
//	                                                   else right)
//	isTruthy(a && b) == isTruthy(a) && isTruthy(b)   (evaluateExpr's && returns
//	                                                   "false" if !isTruthy(left),
//	                                                   else right)
//	isTruthy(!a)     == !isTruthy(a)                 (evaluateExpr's ! returns
//	                                                   "false"/"true" from
//	                                                   isTruthy(inner))
//
// — so genCondition can emit NATIVE Go `&&`/`||`/`!` (with Go's own
// short-circuit evaluation) recursing genCondition on each operand, where
// each operand's truthiness is exactly what genCondition already yields for
// it. A comparison operator is handed to genComparison, unchanged from
// before this recursion was added. With none of those present, the whole
// expression is a single operand (a bare field, or a `.length` expression)
// used for truthiness (genOperandTruthiness).
func (g *generator) genCondition(expr string) (string, error) {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if idx := findTernary(expr); idx >= 0 {
		return "", fmt.Errorf("unsupported ternary operator in codegen condition %q", expr)
	}

	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		left, err := g.genCondition(expr[:idx])
		if err != nil {
			return "", err
		}
		right, err := g.genCondition(expr[idx+2:])
		if err != nil {
			return "", err
		}
		return "(" + left + ") || (" + right + ")", nil
	}

	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		left, err := g.genCondition(expr[:idx])
		if err != nil {
			return "", err
		}
		right, err := g.genCondition(expr[idx+2:])
		if err != nil {
			return "", err
		}
		return "(" + left + ") && (" + right + ")", nil
	}

	for _, op := range conditionComparisonOps {
		if idx := findBinaryOp(expr, op); idx >= 0 {
			if g.rootType == nil {
				return "", fmt.Errorf("unsupported operator %q in codegen condition %q (Config.DataReflectType is required to classify operands)", op, expr)
			}
			return g.genComparison(expr[:idx], op, expr[idx+len(op):])
		}
	}

	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		inner, err := g.genCondition(expr[1:])
		if err != nil {
			return "", err
		}
		return "!(" + inner + ")", nil
	}

	return g.genOperandTruthiness(expr)
}

// genOperandTruthiness emits the truthiness form of a single condition
// operand with no top-level operator: a bare array-literal `.includes(...)`/
// `.contains(...)` call (MethodIncludesSlice always returns the canonical
// string "true" or "false", so its truthiness is a plain `== "true"`
// comparison — no gopug.Truthy needed, since that helper's falsy-set nuances
// never apply to a value restricted to exactly those two strings), a
// `.length` expression (needs Config.DataReflectType, since classifying its
// base requires a type), or a bare field/dot-path. A bool field is used bare
// (native, already the exact
// "true"/"false" form Runtime.isTruthy's FormatBool-derived check mirrors,
// with no need for gopug.Truthy) and a numeric field is compared against
// zero (also native — a numeric field's stringify can never produce one of
// isTruthy's falsy strings other than by being exactly zero). A string field
// routes through the exported gopug.Truthy, reproducing isTruthy's exact
// falsy set ("", "false", "0", "null", "undefined", "nil") for a value that,
// unlike bool/numeric, can actually contain one of those strings. Any other
// field type (slice/map/struct/pointer/…) is an error rather than guessing
// at its stringify-based truthiness — the interpreter would stringify it
// with fmt and isTruthy-test that (e.g. an empty slice stringifies to "[]",
// which is truthy, a footgun this increment doesn't try to reproduce).
func (g *generator) genOperandTruthiness(expr string) (string, error) {
	if isArrayLiteralMethodCall(expr, "includes", "contains") {
		goExpr, fallible, err := g.genValueExpr(expr)
		if err != nil {
			return "", err
		}
		if fallible {
			return "", fmt.Errorf("unsupported array includes/contains call in codegen condition %q (a fallible result is not supported here)", expr)
		}
		return goExpr + ` == "true"`, nil
	}

	if dotIdx := findTopLevelDot(expr); dotIdx > 0 && expr[dotIdx+1:] == "length" {
		if g.rootType == nil {
			return "", fmt.Errorf("unsupported .length condition %q in codegen (Config.DataReflectType is required to classify operands)", expr)
		}
		lenExpr, _, err := g.genLengthOperand(expr[:dotIdx], expr)
		if err != nil {
			return "", err
		}
		return lenExpr + " != 0", nil
	}

	condExpr, condTyp, err := g.resolveFieldExpr(expr)
	if err != nil {
		return "", err
	}
	if condTyp == nil {
		return condExpr, nil
	}
	switch condTyp.Kind() {
	case reflect.Bool:
		return condExpr, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float64:
		return condExpr + " != 0", nil
	case reflect.String:
		g.needsGopug = true
		return "gopug.Truthy(" + convertExpr(condExpr, condTyp, reflectTypeString, "string") + ")", nil
	default:
		return "", fmt.Errorf("unsupported condition field type %s in codegen if %q (only bool, numeric, and string fields are supported in this increment)", condTyp, expr)
	}
}

// operandShape classifies a condition operand genOperand resolved, for
// genComparison's compatibility checks.
type operandShape int

const (
	shapeOperandInvalid operandShape = iota
	shapeOperandBool
	shapeOperandNumericField
	shapeOperandStringField
	shapeOperandNumericLiteral
	shapeOperandStringLiteral
)

// operandKind carries a genOperand result's classification, plus the extra
// detail genComparison needs to decide whether two operands can be compared
// the way the interpreter's compareValues would: the exact numeric
// reflect.Type for a numeric field (direct Go comparison requires identical
// numeric types, not just the same reflect.Kind), the parsed value for a
// numeric literal — genOperand emits the literal's canonical decimal text
// (never the original Pug token), so numLiteralVal is the single source of
// truth checkLiteralAgainstFieldKind range-checks against — and whether a
// string literal's text itself looks numeric (parseNumber), which
// genComparison's stringish-vs-literal fast path uses to decide whether a
// raw Go `==`/`!=` is still safe (a numeric-looking literal must instead go
// through gopug.CompareValues, since compareValues numeric-compares it
// rather than string-comparing it). indexRawGoExpr is set only when this
// operand is a bare reference to an each-loop INDEX variable (see
// scopeVar.indexRawGoName): it carries the underlying raw `int` range-index
// Go expression, letting genComparison compile a numeric-literal comparison
// against the index directly against that int rather than the string.
type operandKind struct {
	shape          operandShape
	numType        reflect.Type
	numLiteralVal  float64
	numericLiteral bool
	indexRawGoExpr string
}

func (k operandKind) isNumeric() bool {
	return k.shape == shapeOperandNumericField || k.shape == shapeOperandNumericLiteral
}

func (k operandKind) isStringish() bool {
	return k.shape == shapeOperandStringField || k.shape == shapeOperandStringLiteral
}

// formatCanonicalLiteral renders f — a value genOperand parsed from a Pug
// numeric-literal token with parseJSNumber — as the plain base-10 Go literal
// text GenerateGo emits for it. genOperand never emits the original Pug
// token verbatim: Go's own numeric-literal grammar disagrees with sloppy
// JS's for several forms a Pug template can contain (a leading "0" followed
// by more digits is legacy octal to Go but, per parseJSNumber, only
// sometimes octal to JS; "08" is not valid Go syntax at all), so emitting
// the token as-is would either silently compare against the wrong value or
// fail to compile. Deriving the emitted text from the already-parsed
// float64 instead sidesteps every such mismatch at the cost of never
// round-tripping the token's original spelling into the generated source.
func formatCanonicalLiteral(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// genOperand resolves a single condition operand — one side of a
// comparison, or the whole condition when it's bare — to a Go expression
// plus its operandKind classification. The supported shapes, checked in
// this order: an array-literal `.indexOf(...)` call (see
// tryArrayIndexOfNumericOperand — the manageGroupActive/navGroupActive
// `!== -1` idiom's left operand), a `.length` expression, a numeric literal
// (parseJSNumber succeeds), a quoted string literal (unwrapQuotedLiteral
// succeeds), and a bare field/dot-path resolving (via resolveFieldExpr) to a
// bool, string, or numeric scalar. Anything else — including a non-scalar
// field, an operator sub-expression, or a method call other than `.length`/
// array-literal `.indexOf` — returns an error.
func (g *generator) genOperand(expr string) (string, operandKind, error) {
	expr = strings.TrimSpace(expr)

	if goExpr, ok, err := g.tryArrayIndexOfNumericOperand(expr); err != nil {
		return "", operandKind{}, err
	} else if ok {
		return goExpr, operandKind{shape: shapeOperandNumericField, numType: reflectTypeInt}, nil
	}

	if dotIdx := findTopLevelDot(expr); dotIdx > 0 && expr[dotIdx+1:] == "length" {
		return g.genLengthOperand(expr[:dotIdx], expr)
	}

	// parseJSNumber parses expr exactly the way the interpreter's
	// evaluateExpr does (runtime.go), so a token it rejects — an
	// underscore digit separator, or an octal-looking leading-zero prefix
	// directly followed by "." or an exponent ("00.5", "017.5") — falls
	// through to the field-resolution path below and fails there instead,
	// with no special-cased guard needed: those tokens aren't valid Pug
	// field names either, so resolveFieldExpr's own "not a field" error
	// already describes them correctly.
	if f, ok := parseJSNumber(expr); ok {
		lit := formatCanonicalLiteral(f)
		return lit, operandKind{shape: shapeOperandNumericLiteral, numLiteralVal: f}, nil
	}

	if lit, ok := unwrapQuotedLiteral(expr); ok {
		_, numericLooking := parseNumber(lit)
		return strconv.Quote(lit), operandKind{shape: shapeOperandStringLiteral, numericLiteral: numericLooking}, nil
	}

	goExpr, typ, err := g.resolveFieldExpr(expr)
	if err != nil {
		return "", operandKind{}, err
	}
	if typ == nil {
		return "", operandKind{}, fmt.Errorf("unsupported operand %q in codegen condition (Config.DataReflectType is required to classify operands)", expr)
	}

	switch typ.Kind() {
	case reflect.Bool:
		return convertExpr(goExpr, typ, reflectTypeBool, "bool"), operandKind{shape: shapeOperandBool}, nil
	case reflect.String:
		kind := operandKind{shape: shapeOperandStringField}
		if sv, ok := g.lookupScope(expr); ok {
			kind.indexRawGoExpr = sv.indexRawGoName
		}
		return convertExpr(goExpr, typ, reflectTypeString, "string"), kind, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float64:
		return goExpr, operandKind{shape: shapeOperandNumericField, numType: typ}, nil
	default:
		return "", operandKind{}, fmt.Errorf("unsupported operand %q in codegen condition (field type %s is not a supported scalar)", expr, typ)
	}
}

// genLengthOperand resolves base (the part of a `.length` expression before
// the final `.length`) and emits the Go equivalent of Runtime's `.length`
// property: len(...) for a slice/array/map field, matching
// reflect.Value.Len(); utf8.RuneCountInString(...) for a string field,
// matching the interpreter's len([]rune(value)) — a rune count, not a byte
// count, so it agrees on multibyte strings where plain len() would not. Any
// other field type (including one requiring pointer dereference) doesn't
// support `.length` in this increment and returns an error.
func (g *generator) genLengthOperand(base, fullExpr string) (string, operandKind, error) {
	goExpr, typ, err := g.resolveFieldExpr(base)
	if err != nil {
		return "", operandKind{}, fmt.Errorf("operand %q: %w", fullExpr, err)
	}
	if typ == nil {
		return "", operandKind{}, fmt.Errorf("unsupported operand %q in codegen condition (Config.DataReflectType is required to classify operands)", fullExpr)
	}

	switch typ.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return "len(" + goExpr + ")", operandKind{shape: shapeOperandNumericField, numType: reflectTypeInt}, nil
	case reflect.String:
		g.needsUtf8 = true
		return "utf8.RuneCountInString(" + convertExpr(goExpr, typ, reflectTypeString, "string") + ")", operandKind{shape: shapeOperandNumericField, numType: reflectTypeInt}, nil
	default:
		return "", operandKind{}, fmt.Errorf("unsupported operand %q in codegen condition (field type %s does not support .length)", fullExpr, typ)
	}
}

// genComparison emits a Go boolean expression for leftRaw <op> rightRaw,
// where op is one of the eight comparison operators Runtime.compareValues
// handles. It resolves both operands with genOperand and only emits a
// comparison for the bounded-agreement subset genComparison can prove
// byte-identical to compareValues:
//
//   - both operands numeric (a numeric field, `.length`, or a numeric
//     literal): a native Go comparison, `===`/`!==` folded to `==`/`!=`
//     (compareValues treats them identically — no strict-type distinction).
//     Two numeric fields must share the exact same Go type (a direct Go `==`
//     between different numeric types doesn't compile, and would risk a
//     silent-conversion divergence even if it did); a numeric literal
//     compared to a numeric field must be representable in the field's kind
//     — a fractional literal against an integer-kind field, or a negative
//     literal against an unsigned field, doesn't compile as a direct Go
//     comparison and is rejected instead of silently truncating/converting.
//   - both operands stringish (a string field, a string `- var` local, or a
//     string literal, in any combination): a string field/`- var` local
//     compared, with `==`/`!=` (or the `===`/`!==` aliases) only, to a
//     string literal that is NOT itself numeric-looking still takes the raw
//     Go `==`/`!=` fast path an earlier increment introduced (kept
//     unchanged, byte-for-byte, so a checked-in golden-source expectation
//     pinning that exact generated code stays pinned) — a numeric-looking
//     string literal would numeric-compare in compareValues (both operands
//     would parse as numbers), not string-compare, so a raw Go `==` there
//     would diverge, and is excluded from the fast path accordingly. Every
//     other stringish combination — an ordering compare, a numeric-looking
//     string literal, two string literals, two string fields/`- var`
//     locals, or a field/local compared against another field/local — is
//     emitted as a call to the exported gopug.CompareValues instead, the
//     same function compareValues itself now delegates to. Whether either
//     operand's runtime value is numeric-looking (so the comparison numeric-
//     compares) or not (so it string-compares, including ordering) can't
//     always be proven at generation time for these combinations — a field
//     or `- var` local's VALUE isn't known until render — so this defers
//     that decision to CompareValues at run time instead of guessing, which
//     is exactly what keeps it byte-identical to compareValues for every
//     stringish combination the earlier field-vs-literal-only increment
//     deferred.
//   - an each-loop INDEX variable (genOperand sets operandKind.indexRawGoExpr
//     for one — see scopeVar.indexRawGoName) compared, with ANY of the
//     eight operators, to a numeric literal: the index is always a decimal
//     string of a non-negative int (strconv.Itoa), so compareValues always
//     takes its numeric branch for it (parseNumber never fails on that
//     shape), and the comparison is compiled directly against the
//     underlying raw int local instead of the string — checked against
//     checkLiteralAgainstFieldKind(reflectTypeInt) exactly like a genuine
//     int-kind field, so the same safe-integer/representability guarantees
//     apply.
//
// Every other combination — a string operand against a numeric operand
// (other than the index-variable case above), or any operand genOperand
// itself rejected — returns an error instead of emitting a comparison that
// might not agree with the interpreter.
func (g *generator) genComparison(leftRaw, op, rightRaw string) (string, error) {
	leftRaw = strings.TrimSpace(leftRaw)
	rightRaw = strings.TrimSpace(rightRaw)

	leftExpr, leftKind, err := g.genOperand(leftRaw)
	if err != nil {
		return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition: %w", leftRaw, op, rightRaw, err)
	}
	rightExpr, rightKind, err := g.genOperand(rightRaw)
	if err != nil {
		return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition: %w", leftRaw, op, rightRaw, err)
	}

	goOp := op
	switch op {
	case "===":
		goOp = "=="
	case "!==":
		goOp = "!="
	}

	switch {
	case leftKind.indexRawGoExpr != "" && rightKind.shape == shapeOperandNumericLiteral:
		if err := checkLiteralAgainstFieldKind(rightKind.numLiteralVal, reflectTypeInt); err != nil {
			return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition: %w", leftRaw, op, rightRaw, err)
		}
		return leftKind.indexRawGoExpr + " " + goOp + " " + formatCanonicalLiteral(rightKind.numLiteralVal), nil

	case rightKind.indexRawGoExpr != "" && leftKind.shape == shapeOperandNumericLiteral:
		if err := checkLiteralAgainstFieldKind(leftKind.numLiteralVal, reflectTypeInt); err != nil {
			return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition: %w", leftRaw, op, rightRaw, err)
		}
		return formatCanonicalLiteral(leftKind.numLiteralVal) + " " + goOp + " " + rightKind.indexRawGoExpr, nil

	case leftKind.isNumeric() && rightKind.isNumeric():
		if err := checkNumericComparable(leftKind, rightKind); err != nil {
			return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition: %w", leftRaw, op, rightRaw, err)
		}
		return leftExpr + " " + goOp + " " + rightExpr, nil

	case leftKind.isStringish() && rightKind.isStringish():
		if goOp == "==" || goOp == "!=" {
			var litKind operandKind
			var isFieldVsLiteral bool
			switch {
			case leftKind.shape == shapeOperandStringField && rightKind.shape == shapeOperandStringLiteral:
				litKind, isFieldVsLiteral = rightKind, true
			case rightKind.shape == shapeOperandStringField && leftKind.shape == shapeOperandStringLiteral:
				litKind, isFieldVsLiteral = leftKind, true
			}
			if isFieldVsLiteral && !litKind.numericLiteral {
				return leftExpr + " " + goOp + " " + rightExpr, nil
			}
		}
		g.needsGopug = true
		return "gopug.CompareValues(" + leftExpr + ", " + rightExpr + ", " + strconv.Quote(op) + ")", nil

	default:
		return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition (these operand types are not comparable in this increment)", leftRaw, op, rightRaw)
	}
}

// checkNumericComparable reports an error unless left and right — both
// already known numeric (a numeric field, `.length`, or a numeric literal)
// — can be compared with a direct, compiling Go comparison that can't
// silently diverge from compareValues: two numeric fields (or `.length`
// results) must share the exact same Go type; a numeric literal compared
// against a numeric field must be representable in the field's kind.
func checkNumericComparable(left, right operandKind) error {
	switch {
	case left.shape == shapeOperandNumericField && right.shape == shapeOperandNumericField:
		if left.numType != right.numType {
			return fmt.Errorf("numeric fields of different Go types (%s vs %s) cannot be compared directly", left.numType, right.numType)
		}
		return nil
	case left.shape == shapeOperandNumericField:
		return checkLiteralAgainstFieldKind(right.numLiteralVal, left.numType)
	case right.shape == shapeOperandNumericField:
		return checkLiteralAgainstFieldKind(left.numLiteralVal, right.numType)
	default:
		// Two numeric literals: always a valid (if pointless) Go constant
		// comparison.
		return nil
	}
}

// integerKindBits returns the bit width to validate an integer literal
// against for the given numeric field kind: the kind's own width for a
// sized kind, or 64 for the platform-sized Int/Uint kind — every platform
// this package supports is 64-bit, so a literal that fits int64/uint64 is
// exactly what compiles against a plain int/uint field.
func integerKindBits(kind reflect.Kind) int {
	switch kind {
	case reflect.Int8, reflect.Uint8:
		return 8
	case reflect.Int16, reflect.Uint16:
		return 16
	case reflect.Int32, reflect.Uint32:
		return 32
	default:
		return 64
	}
}

// maxSafeCodegenInteger is the largest integer magnitude checkLiteralAgainstFieldKind
// will accept for an Int/Uint-kind field comparison: 2^53 − 1, JS's
// Number.MAX_SAFE_INTEGER — the largest integer magnitude for which NO
// distinct integer can round to the same float64. It matters here because
// the interpreter's compareValues doesn't compare a numeric field's value
// directly — it stringifies the field, then reparses that string with
// strconv.ParseFloat, exactly the way a literal itself is evaluated
// (parseJSNumber, runtime.go). The divergence this bound guards against
// originates on the FIELD side, not the literal side: 2^53 itself IS
// exactly representable in a float64, but an int64/uint64 field can hold
// 2^53 + 1 — a value float64 CANNOT represent exactly, which rounds
// (half-to-even) back down to 2^53 when the field is stringified and
// reparsed. So a literal of exactly 2^53 would compare equal, via the
// interpreter's lossy round trip, to a field actually holding 2^53 + 1,
// while codegen's direct, unrounded Go integer comparison would not. Below
// 2^53 (i.e. up to and including 2^53 − 1) every adjacent integer — the
// literal's neighbors on both sides — is still exactly representable, so no
// field value can alias onto it and the round trip is always exact.
// Refusing any literal beyond the boundary keeps every comparison codegen
// DOES emit provably byte-identical to compareValues, at the cost of the
// (vanishingly rare in real templates) literal comparison against an
// actual int64/uint64-range field value.
const maxSafeCodegenInteger = (1 << 53) - 1 // 9007199254740991, JS Number.MAX_SAFE_INTEGER

// checkLiteralAgainstFieldKind reports an error unless the numeric literal
// value f (parseJSNumber's parsed value — the exact value genOperand emits
// as f's canonical decimal text, see formatCanonicalLiteral) is
// representable as fieldType without truncation, overflow, or a precision
// loss codegen can't prove matches the interpreter's own: a fractional
// value against an integer-kind field, a negative value against an
// unsigned-kind field, a magnitude beyond what the field's sized kind
// (int8, uint8, int32, …) can hold, or an integer magnitude beyond
// maxSafeCodegenInteger are all rejected rather than emitted.
//
// Because genOperand only ever hands this function a literal it has itself
// already reduced to a canonical, exact decimal integer string when f is
// whole-valued, the strconv.ParseInt/ParseUint range check below always
// runs against a plain base-10 token — there is no scientific-notation or
// underscore-separator form left to fall back for by the time a literal
// reaches here.
func checkLiteralAgainstFieldKind(f float64, fieldType reflect.Type) error {
	switch fieldType.Kind() {
	case reflect.Float64, reflect.Float32:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f != math.Trunc(f) {
			return fmt.Errorf("a fractional numeric literal cannot be compared directly to integer field type %s", fieldType)
		}
		if math.Abs(f) > maxSafeCodegenInteger {
			return fmt.Errorf("numeric literal %v is out of range for codegen's safe-integer precision (integer literals compared to field type %s are limited to ±2^53 to guarantee agreement with the interpreter)", f, fieldType)
		}
		if _, err := strconv.ParseInt(formatCanonicalLiteral(f), 10, integerKindBits(fieldType.Kind())); err != nil {
			return fmt.Errorf("numeric literal %v is out of range for integer field type %s", f, fieldType)
		}
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if f != math.Trunc(f) || f < 0 {
			return fmt.Errorf("numeric literal %v is not a valid value of unsigned field type %s", f, fieldType)
		}
		if f > maxSafeCodegenInteger {
			return fmt.Errorf("numeric literal %v is out of range for codegen's safe-integer precision (integer literals compared to field type %s are limited to 2^53 to guarantee agreement with the interpreter)", f, fieldType)
		}
		if _, err := strconv.ParseUint(formatCanonicalLiteral(f), 10, integerKindBits(fieldType.Kind())); err != nil {
			return fmt.Errorf("numeric literal %v is out of range for unsigned field type %s", f, fieldType)
		}
		return nil
	default:
		return fmt.Errorf("unsupported numeric field type %s", fieldType)
	}
}

// sanitizeGoIdent converts name (a Pug mixin name, which may contain a
// hyphen or other character valid in Pug but not in a Go identifier) into a
// valid Go identifier fragment: every rune that isn't a valid identifier
// continuation (an ASCII letter, digit, or underscore) becomes an
// underscore, and a leading digit — valid mid-identifier but not as the
// first character — is handled by the caller's own fixed prefix ("pugMixin_"
// in GenerateGo), so the result is always used as a suffix, never alone.
func sanitizeGoIdent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// uniqueGoName returns candidate unchanged and records it in used, unless
// used already contains it (or, working backward, any earlier-numbered
// suffix this same function generated), in which case it appends "_2",
// "_3", … until it finds a name used does not yet contain — so two mixins
// whose names sanitize to the same Go identifier (or a mixin whose
// sanitized name happens to collide with the render function's own name,
// pre-seeded into used by the caller) still each get a distinct top-level Go
// function.
func uniqueGoName(candidate string, used map[string]bool) string {
	if !used[candidate] {
		used[candidate] = true
		return candidate
	}
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s_%d", candidate, i)
		if !used[c] {
			used[c] = true
			return c
		}
	}
}

// blockParamGoName is the fixed Go parameter name genMixinFunc adds to a
// slot-bearing mixin's helper signature for its block-callback. It can never
// collide with a positional argument (always named "argN") or with the
// writer parameter "w".
const blockParamGoName = "pugBlock"

// restParamGoName is the fixed Go parameter name genMixinFunc adds to a
// rest-parameter mixin's helper signature for its collected `[]string`
// argument. It can never collide with a positional argument (always named
// "argN"), with blockParamGoName, or with the writer parameter "w".
const restParamGoName = "restArgs"

// mixinBodyHasBlockSlot reports whether body contains a `block` node
// (*BlockNode, regardless of its Name — Runtime.renderNode's own dispatch
// for a *BlockNode reached while r.inMixin is true renders the caller's
// block content unconditionally, never consulting Name, so codegen mirrors
// that by treating every *BlockNode reachable from a mixin's decl body as
// its slot) anywhere reachable through the same node-nesting shapes genNode
// itself walks: a tag's children, a text run's own nodes, an if/else
// branch, or an each-loop body. The slot can be nested arbitrarily deep,
// e.g. inside a wrapping div.
func mixinBodyHasBlockSlot(body []Node) bool {
	for _, n := range body {
		if nodeHasBlockSlot(n) {
			return true
		}
	}
	return false
}

// nodeHasBlockSlot is mixinBodyHasBlockSlot's single-node recursive step.
func nodeHasBlockSlot(n Node) bool {
	switch node := n.(type) {
	case *BlockNode:
		return true
	case *TagNode:
		return mixinBodyHasBlockSlot(node.Children)
	case *TextRunNode:
		return mixinBodyHasBlockSlot(node.Nodes)
	case *ConditionalNode:
		return mixinBodyHasBlockSlot(node.Consequent) || mixinBodyHasBlockSlot(node.Alternate)
	case *EachNode:
		return mixinBodyHasBlockSlot(node.Body) || mixinBodyHasBlockSlot(node.EmptyBody)
	default:
		return false
	}
}

// mixinBodyUsesAttributesForward reports whether body contains a tag with an
// `&attributes` key in its Attributes map anywhere reachable through the same
// node-nesting shapes mixinBodyHasBlockSlot walks — regardless of what
// expression `&attributes(...)` spreads (that distinction, and whether it is
// literally the mixin's own special `attributes` variable, is checked later,
// at the specific call site, by genMixinCallAttrForward/genTag, so a clean
// per-call error can name the actual expression rather than only being able
// to say "not attributes" for the whole mixin regardless of which call
// triggered it).
func mixinBodyUsesAttributesForward(body []Node) bool {
	for _, n := range body {
		if nodeUsesAttributesForward(n) {
			return true
		}
	}
	return false
}

// nodeUsesAttributesForward is mixinBodyUsesAttributesForward's single-node
// recursive step.
func nodeUsesAttributesForward(n Node) bool {
	switch node := n.(type) {
	case *TagNode:
		if _, ok := node.Attributes["&attributes"]; ok {
			return true
		}
		return mixinBodyUsesAttributesForward(node.Children)
	case *TextRunNode:
		return mixinBodyUsesAttributesForward(node.Nodes)
	case *ConditionalNode:
		return mixinBodyUsesAttributesForward(node.Consequent) || mixinBodyUsesAttributesForward(node.Alternate)
	case *EachNode:
		return mixinBodyUsesAttributesForward(node.Body) || mixinBodyUsesAttributesForward(node.EmptyBody)
	default:
		return false
	}
}

// genMixinFunc generates decl's entire body as a standalone top-level Go
// function named fnName, `func <fnName>(w io.Writer, arg1 string, arg2
// string, …) error` (one string parameter per decl.Parameters, positionally
// named arg1, arg2, … rather than reusing the Pug parameter names
// themselves, so a mixin parameter can never collide with the writer
// parameter "w" or with any other generated identifier), and returns its
// full Go source text. When g.mixinHasSlot[decl.Name] is true (decl.Body
// contains a `block` node — see mixinBodyHasBlockSlot), the signature gains
// one further nilable parameter, `pugBlock func(io.Writer) error`, and every
// `*BlockNode` genNode reaches while generating this body invokes it (see
// genNode's own *BlockNode case) — once per occurrence, matching the
// interpreter rendering the caller's block content anew at each `block`
// keyword (Runtime.renderMixinBlockSlot).
//
// The body is generated in a PARAM-ONLY scope: g.scope holds only the
// mixin's own parameters (each pushed as a string-typed scope entry mapping
// the Pug parameter name to its Go argument), g.paramOnlyScope is set so
// resolveFieldExpr fails closed on any OTHER identifier instead of falling
// through to struct-field resolution against d (see resolveFieldExpr's own
// doc comment), and g.insideMixinBody is set so a nested mixin call inside
// the body is rejected by genMixinCall rather than attempted — reproducing
// Runtime.renderMixinCall's own full isolation (a mixin body sees only its
// parameters) as a compile-time, fail-closed GenerateGo error rather than a
// silent "" the way the interpreter's own lookup miss would render it. A
// rest parameter (decl.RestParamName != "") gains one further signature
// parameter, `restArgs []string`, pushed onto that same scope with
// reflectTypeStringSlice — a `[]string` scope var, unlike every other
// parameter's plain string one — so the body's `each`/`.length`/index/
// `.join` on it flow through the SAME existing slice-typed machinery those
// constructs already use for a slice-typed struct field (genEach requires
// Kind() Slice/Array; genLengthOperand, genIndexValueExpr, and
// genJoinValueExpr all switch on typ.Kind() the same way); no new per-op
// code was needed for any of those. A whole-slice-valued reference to it
// (`= items`, `#{items}`) still fails closed with a clean error, unchanged:
// genScalarStringify's own type switch has no Slice case, so it falls to its
// existing default error rather than any new code added here. A default
// parameter value needs no special handling here at all: a default is
// resolved entirely at the CALL SITE (see genMixinParamValue), so this
// function's own signature and body generation are identical whether or
// not any parameter has one.
//
// g.body/g.static are reset (never swapped by value — a strings.Builder must
// not be copied after first use) before returning; this is safe because
// both are always still completely empty at the point GenerateGo calls
// this, for every mixin, since every mixin helper is generated in its own
// pass before the main render function's own body walk ever writes to
// either one. g.scope is saved and restored by value (a plain slice, so
// copying it is unremarkable) and g.paramOnlyScope/g.insideMixinBody/
// g.blockParamName are restored to whatever they were before this call
// (all zero, unless this call is itself nested inside another — which
// genMixinFunc never does, since GenerateGo only ever calls it for a
// top-level mixin declaration).
func (g *generator) genMixinFunc(fnName string, decl *MixinDeclNode) (string, error) {
	hasSlot := g.mixinHasSlot[decl.Name]

	savedScope := g.scope
	savedParamOnly := g.paramOnlyScope
	savedInsideMixinBody := g.insideMixinBody
	savedBlockParamName := g.blockParamName

	g.scope = nil
	g.paramOnlyScope = true
	g.insideMixinBody = true
	if hasSlot {
		g.blockParamName = blockParamGoName
	} else {
		g.blockParamName = ""
	}

	argNames := make([]string, len(decl.Parameters))
	for i, p := range decl.Parameters {
		argName := fmt.Sprintf("arg%d", i+1)
		argNames[i] = argName
		g.pushScope(p, argName, reflectTypeString, false)
	}
	if decl.RestParamName != "" {
		g.pushScope(decl.RestParamName, restParamGoName, reflectTypeStringSlice, false)
	}

	var bodyErr error
	for _, child := range decl.Body {
		if err := g.genNode(child); err != nil {
			bodyErr = fmt.Errorf("mixin %q: %w", decl.Name, err)
			break
		}
	}
	if bodyErr == nil {
		g.flushStatic()
	}
	fnBody := g.body.String()
	g.body.Reset()
	g.static.Reset()

	g.scope = savedScope
	g.paramOnlyScope = savedParamOnly
	g.insideMixinBody = savedInsideMixinBody
	g.blockParamName = savedBlockParamName

	if bodyErr != nil {
		return "", bodyErr
	}

	var sig strings.Builder
	fmt.Fprintf(&sig, "func %s(w io.Writer", fnName)
	for _, argName := range argNames {
		fmt.Fprintf(&sig, ", %s string", argName)
	}
	if decl.RestParamName != "" {
		fmt.Fprintf(&sig, ", %s []string", restParamGoName)
	}
	if hasSlot {
		fmt.Fprintf(&sig, ", %s func(io.Writer) error", blockParamGoName)
	}
	sig.WriteString(") error {\n")
	sig.WriteString(fnBody)
	sig.WriteString("return nil\n}\n")
	return sig.String(), nil
}

// genMixinParamValue computes the Go value expression a call binds to
// decl.Parameters[i] — the one piece of "value for this parameter, or a
// fallback" logic every per-parameter binding site shares (genMixinCall's
// own inline/`__margN`-hoisted argument list below, and
// genMixinCallAttrForward's `__margN` hoist), so a defaulted parameter
// resolves identically in the helper call, in `&attributes` forwarding, and
// (since genMixinBlockClosure is always handed the SAME margNames
// genMixinCall computed here) in dynamic block content read from that same
// local.
//
// A present call argument (i < len(call.Arguments)) is genValueExpr'd in
// the CALLER's own scope exactly as before, fallible-extracted through
// genFallibleExtraction like every other fallible write site — unchanged
// from prior increments.
//
// A MISSING argument whose parameter has a default (decl.ParamDefaults) is
// treated as an IMPLICIT caller-side argument: genValueExpr(defaultExpr),
// the exact same code path and the exact same CALLER scope a present
// argument uses. This mirrors Runtime.renderMixinCall's own binding loop
// precisely (runtime.go's param-binding loop): the mixin's own scope frame
// is not pushed onto r.scopeStack until AFTER every parameter is bound, so
// r.evaluateExpr(defaultExpr) sees exactly the same data/locals a real
// argument would — including a SIBLING parameter's name, which resolves
// against the CALLER's own scope (almost always a lookup miss, since the
// caller has no such variable), never against another parameter of this
// same mixin; this is why `mixin g(a, b = a)` called as `+g("A")` binds `b`
// to `""`, not `"A"` — verified against the interpreter and pinned by a
// differential test.
//
// A default expression genValueExpr flags FALLIBLE is refused with a
// clean, distinct error rather than generated: Runtime.renderMixinCall
// falls back to the raw default-expression STRING when r.evaluateExpr
// errors (e.g. a division by zero), a fallback codegen cannot reproduce
// from a Go expression that would itself return a runtime error instead of
// a string — so this defers the whole mixin rather than risk silently
// diverging on exactly that error path. A default expression genValueExpr
// cannot compile at all (an unsupported shape) surfaces genValueExpr's own
// error unchanged.
//
// A missing argument whose parameter has NO default becomes the literal
// `""`, matching the interpreter's own missing-arg, no-default binding.
func (g *generator) genMixinParamValue(call *MixinCallNode, decl *MixinDeclNode, i int) (string, error) {
	if i < len(call.Arguments) {
		v, fallible, err := g.genValueExpr(call.Arguments[i])
		if err != nil {
			return "", fmt.Errorf("mixin %q argument %d: %w", call.Name, i+1, err)
		}
		if fallible {
			v = g.genFallibleExtraction(v)
		}
		return v, nil
	}

	param := decl.Parameters[i]
	if decl.ParamDefaults != nil {
		if defaultExpr, ok := decl.ParamDefaults[param]; ok {
			v, fallible, err := g.genValueExpr(defaultExpr)
			if err != nil {
				return "", fmt.Errorf("mixin %q: default value %q for parameter %q: %w", call.Name, defaultExpr, param, err)
			}
			if fallible {
				return "", fmt.Errorf("mixin %q: default value %q for parameter %q is a fallible expression and is not supported in codegen: Runtime.renderMixinCall falls back to the raw default-expression string when evaluation errors, a fallback codegen cannot reproduce", call.Name, defaultExpr, param)
			}
			return v, nil
		}
	}
	return `""`, nil
}

// genMixinRestArg computes the Go `[]string{...}` literal a call binds to
// decl's rest parameter, reproducing Runtime.renderMixinCall's own rest-
// argument collection loop exactly: every call argument BEYOND
// len(decl.Parameters) — call.Arguments[len(decl.Parameters):] — is
// evaluated in the CALLER's own scope, in order, the same genValueExpr call
// every positional argument uses, and becomes one element of the literal. A
// call supplying no extra arguments (len(call.Arguments) <= len(decl.
// Parameters), including a call with FEWER arguments than decl.Parameters
// itself) produces the empty literal `[]string{}`, matching the
// interpreter's own `rest := make([]any, 0)` when its collection loop never
// runs.
//
// A fallible rest-argument expression (e.g. a division) is refused with a
// clean, distinct error rather than generated: unlike a positional
// argument's own genFallibleExtraction (an IIFE returning (string, error)
// that can extract into a plain `:=` local before the call), a slice-literal
// ELEMENT position cannot host that same two-value extraction inline, and
// hoisting every element into its own local first is a distinct,
// untested claim this increment does not make — Runtime.renderMixinCall's
// own rest-collection loop returns the render error immediately on a
// fallible element (r.evaluateMixinArg), so refusing to generate it at all
// is fail-closed, not a bounded-agreement breach.
func (g *generator) genMixinRestArg(call *MixinCallNode, decl *MixinDeclNode) (string, error) {
	n := len(decl.Parameters)
	if len(call.Arguments) <= n {
		return "[]string{}", nil
	}

	elems := make([]string, 0, len(call.Arguments)-n)
	for i := n; i < len(call.Arguments); i++ {
		v, fallible, err := g.genValueExpr(call.Arguments[i])
		if err != nil {
			return "", fmt.Errorf("mixin %q rest argument %d (%q): %w", call.Name, i-n+1, call.Arguments[i], err)
		}
		if fallible {
			return "", fmt.Errorf("mixin %q: rest argument %d (%q) is a fallible expression and is not supported in codegen: a fallible slice-literal element has no equivalent codegen extraction site yet", call.Name, i-n+1, call.Arguments[i])
		}
		elems = append(elems, v)
	}
	return "[]string{" + strings.Join(elems, ", ") + "}", nil
}

// genMixinCall emits a call to call.Name's already-generated helper function
// (see genMixinFunc), reproducing Runtime.renderMixinCall's own argument
// binding exactly: an argument is built by genValueExpr in the CALLER's own
// scope (data-visible, unlike the callee's own isolated body) for each of
// the first len(decl.Parameters) call arguments — a missing trailing
// argument (fewer call arguments than parameters) becomes the default
// value's own genValueExpr result when the parameter has one, or the
// literal "" otherwise (see genMixinParamValue). A fallible POSITIONAL
// argument expression (e.g. a division) is extracted, in argument order,
// through the same genFallibleExtraction every other fallible write site
// uses, so the generated function returns that error immediately rather
// than call the mixin helper with a value that was never actually computed.
// When decl has a rest parameter, every call argument beyond the parameter
// count is instead collected into a `[]string{...}` literal (see
// genMixinRestArg) and passed as one further argument, positioned right
// after the positional ones and before the block-callback argument (if
// any) — matching genMixinFunc's own signature order.
//
// A nested mixin call (encountered while g.insideMixinBody or
// g.insideBlockClosure is set — i.e. one mixin's own body, or a call site's
// own block-content closure, calling another mixin) is a later increment and
// returns a distinct error instead of attempting to generate it. A call
// forwarding attributes (&attributes) to a mixin whose OWN body does not use
// `&attributes` anywhere is likewise a later increment (the call's
// attributes would simply be unused, but this increment does not attempt to
// prove that in general and refuses instead); a call to a mixin whose body
// DOES use `&attributes(attributes)` is handled entirely differently — see
// genMixinCallAttrForward, dispatched to below before either of these checks
// ever runs.
//
// Block content: when the callee's decl has a `block` slot
// (g.mixinHasSlot[call.Name]), a non-empty call.BlockContent is generated as
// a closure (see genMixinBlockClosure) and passed as the helper's final
// argument; empty block content passes a literal `nil` instead. When the
// callee's decl has NO slot, call.BlockContent — if any — is silently
// discarded without generating it at all, matching
// Runtime.renderMixinCall/renderMixinBlockSlot's own silent-discard exactly
// (callerBlock is set regardless of whether the body ever reads it).
//
// When the call has non-empty block content AND the callee has a slot (the
// only case genMixinBlockClosure's own param scope needs anything to bind),
// each call argument is hoisted into a stable `__margN` local — one per
// DECLARED parameter, in declaration order, "" for a missing trailing
// argument, extra call arguments beyond the parameter count ignored — BEFORE
// the call statement, and that same local (not the inline expression) is
// what both the helper call and the block-content closure use: a single
// evaluation feeds both, exactly mirroring
// Runtime.renderMixinCall/evaluateMixinArg evaluating each argument once
// caller-side into `scope[param]`, a value both the mixin body and (while its
// boundary+param frame is active) the block content read from that same map
// entry. Every OTHER call shape (no slot, or a slot with empty/no block
// content) is unchanged from before: each argument is still the inline
// genValueExpr result, no `__margN` local introduced at all.
func (g *generator) genMixinCall(call *MixinCallNode) error {
	if g.insideMixinBody {
		return fmt.Errorf("mixin %q: a nested mixin call inside a mixin body is not supported in codegen yet", call.Name)
	}
	if g.insideBlockClosure {
		return fmt.Errorf("mixin %q: a nested mixin call inside block content passed to a mixin call is not supported in codegen yet", call.Name)
	}

	decl, ok := g.mixinDecls[call.Name]
	if !ok {
		return fmt.Errorf("unsupported mixin call %q in codegen: mixin %q is not defined at the top level (only a TOP-LEVEL mixin declaration is collected, matching Runtime.collectMixins — a declaration reached only through a nested composition/include position is not visible to a call)", call.Name, call.Name)
	}

	if g.mixinAttrForward[call.Name] {
		return g.genMixinCallAttrForward(call, decl)
	}

	if len(call.Attributes) > 0 {
		return fmt.Errorf("mixin %q: attributes forwarded to a mixin call (&attributes) are not supported in codegen yet", call.Name)
	}

	fnName := g.mixinFuncNames[call.Name]

	hasSlot := g.mixinHasSlot[call.Name]
	dynamicBlock := hasSlot && len(call.BlockContent) > 0

	args := make([]string, 0, len(decl.Parameters)+2)
	var margNames []string
	var callID int
	if dynamicBlock {
		margNames = make([]string, len(decl.Parameters))
		callID = g.nextTmp()
	}
	for i := range decl.Parameters {
		valExpr, err := g.genMixinParamValue(call, decl, i)
		if err != nil {
			return err
		}
		if dynamicBlock {
			margName := fmt.Sprintf("__marg%d_%d", callID, i)
			g.writeRaw(fmt.Sprintf("%s := %s\n", margName, valExpr))
			margNames[i] = margName
			args = append(args, margName)
		} else {
			args = append(args, valExpr)
		}
	}

	if decl.RestParamName != "" {
		restExpr, err := g.genMixinRestArg(call, decl)
		if err != nil {
			return err
		}
		args = append(args, restExpr)
	}

	if hasSlot {
		if len(call.BlockContent) > 0 {
			closureExpr, err := g.genMixinBlockClosure(call.BlockContent, call.Name, decl.Parameters, margNames)
			if err != nil {
				return err
			}
			args = append(args, closureExpr)
		} else {
			args = append(args, "nil")
		}
	}

	callArgs := append([]string{"w"}, args...)
	g.writeRaw(fmt.Sprintf("if err := %s(%s); err != nil {\nreturn err\n}\n", fnName, strings.Join(callArgs, ", ")))
	return nil
}

// genMixinCallAttrForward generates decl's ENTIRE body inline, directly into
// the caller's own g.body/g.static — no shared helper function is ever
// created for a &attributes-forwarding mixin (see mixinAttrForward's own doc
// comment) — for call, ONE specific call site. decl's own parameters are
// handled exactly as any other mixin's (bound to hoisted `__margN` locals,
// one evaluation feeding every reference, exactly like genMixinCall's own
// dynamicBlock path).
//
// call.Attributes fall into one of two shapes, decided up front by
// staticCallAttrValue:
//
//   - ALL static (a bare attribute, an unquoted true/false keyword, or a
//     plain quoted-string literal — including the vacuous case of no call
//     attributes at all): the original GENERATE-TIME merge path, unchanged.
//     Every attribute a `&attributes`-spread tag reached from decl.Body will
//     render for THIS call is fully resolved at generate time, before
//     genTag/mergeForwardedAttributes ever need to consult
//     g.attrForwardCallAttrs (g.inAttrForwardBody is set so genTag routes
//     the body's own `&attributes(attributes)` tag there).
//   - ANY dynamic (an expression-valued attribute staticCallAttrValue
//     refuses): a RUNTIME path. genMixinCallAttrsMap builds a
//     map[string]string local, in the CALLER's own scope, holding every call
//     attribute's evaluated value under its own name — the exact value
//     Runtime.renderMixinCall's own attrMap would hold for it. That local is
//     then bound into decl's own param scope under the mixin's special
//     "attributes" name (reflectTypeStringMap), and g.inAttrForwardBody is
//     left FALSE, so the body's own `&attributes(attributes)` tag falls
//     through genTag's ordinary path to genSpreadAttrs instead of
//     mergeForwardedAttributes — genSpreadAttrs's pre-existing
//     map[string]string field/scope-variable path (backed by
//     gopug.WriteSpreadAttrs) and genSpreadBase's pre-existing dynamic-base
//     handling (including a base attribute referencing one of decl's own
//     `__margN` parameters) render it, with no new render logic introduced
//     by this function at all.
//
// A rest parameter and a `block` slot are both unsupported for a
// &attributes-forwarding mixin this slice (the latter is a deliberate,
// explicit scope cut: `&attributes` combined with block-content
// interactions is a distinct, untested claim this increment does not make,
// even though the two mechanisms don't obviously conflict) — checked up
// front, before any body generation, exactly like genMixinFunc's own
// equivalent check, and before either call-attribute path runs. A default
// parameter value needs no such check: a missing argument whose parameter
// has a default is hoisted into its `__margN` local via genMixinParamValue
// exactly like a real argument would be, the same mechanism genMixinCall's
// own dynamicBlock path uses — see genMixinParamValue's own doc comment for
// why this is byte-identical to Runtime.renderMixinCall's own caller-side
// default evaluation.
//
// decl.Body is generated in the SAME param-only, insideMixinBody-set scope
// genMixinFunc's own body generation uses (see its own doc comment for why:
// a reference to a declared parameter resolves to its hoisted local, while
// any OTHER identifier fails closed instead of guessing) — the only
// difference from genMixinFunc is that g.body/g.static are NOT swapped out
// first: this call is reached from within the render function's own body
// walk (like genMixinCall itself), so the generated statements must land
// directly in whatever g.body already holds, exactly like genConditional or
// genEach's own nested content generation.
func (g *generator) genMixinCallAttrForward(call *MixinCallNode, decl *MixinDeclNode) error {
	if decl.RestParamName != "" {
		return fmt.Errorf("mixin %q: a rest parameter (...%s) is not supported in codegen yet", decl.Name, decl.RestParamName)
	}
	if g.mixinHasSlot[call.Name] {
		return fmt.Errorf("mixin %q: &attributes combined with a block slot (`block`) is not supported in codegen yet", call.Name)
	}

	allStatic := true
	for _, v := range call.Attributes {
		if _, ok := staticCallAttrValue(v); !ok {
			allStatic = false
			break
		}
	}

	callID := g.nextTmp()
	margNames := make([]string, len(decl.Parameters))
	for i := range decl.Parameters {
		valExpr, err := g.genMixinParamValue(call, decl, i)
		if err != nil {
			return err
		}
		margName := fmt.Sprintf("__marg%d_%d", callID, i)
		g.writeRaw(fmt.Sprintf("%s := %s\n", margName, valExpr))
		// A merged &attributes tag can completely overwrite a base attribute
		// that referenced this very parameter (the "overwrite" rule — see
		// mergeForwardedAttributes), or the whole mixin body can spread a
		// runtime attribute map instead, in which case the parameter is
		// never read anywhere in the generated body at all. Unlike
		// genMixinFunc's own argN, which are Go FUNCTION PARAMETERS (never
		// flagged unused), margName is a plain := local here, so it must be
		// blank-used unconditionally to stay valid Go regardless of whether
		// the body ends up referencing it.
		g.writeRaw(fmt.Sprintf("_ = %s\n", margName))
		margNames[i] = margName
	}

	var callStatic map[string]string
	var attrsMapGoName string
	if allStatic {
		callStatic = make(map[string]string, len(call.Attributes))
		for name, v := range call.Attributes {
			valStr, _ := staticCallAttrValue(v)
			callStatic[name] = valStr
		}
	} else {
		var err error
		attrsMapGoName, err = g.genMixinCallAttrsMap(call, callID)
		if err != nil {
			return err
		}
	}

	savedScope := g.scope
	savedParamOnly := g.paramOnlyScope
	savedInsideMixinBody := g.insideMixinBody
	savedBlockParamName := g.blockParamName
	savedInAttrForwardBody := g.inAttrForwardBody
	savedAttrForwardCallAttrs := g.attrForwardCallAttrs

	g.scope = nil
	for i, p := range decl.Parameters {
		g.pushScope(p, margNames[i], reflectTypeString, false)
	}
	if allStatic {
		g.paramOnlyScope = true
		g.insideMixinBody = true
		g.blockParamName = ""
		g.inAttrForwardBody = true
		g.attrForwardCallAttrs = callStatic
	} else {
		g.pushScope("attributes", attrsMapGoName, reflectTypeStringMap, false)
		g.paramOnlyScope = true
		g.insideMixinBody = true
		g.blockParamName = ""
		g.inAttrForwardBody = false
		g.attrForwardCallAttrs = nil
	}

	var bodyErr error
	for _, child := range decl.Body {
		if err := g.genNode(child); err != nil {
			bodyErr = fmt.Errorf("mixin %q: %w", decl.Name, err)
			break
		}
	}

	g.scope = savedScope
	g.paramOnlyScope = savedParamOnly
	g.insideMixinBody = savedInsideMixinBody
	g.blockParamName = savedBlockParamName
	g.inAttrForwardBody = savedInAttrForwardBody
	g.attrForwardCallAttrs = savedAttrForwardCallAttrs

	return bodyErr
}

// genMixinCallAttrsMap builds, at the CALL SITE and in the CALLER's own
// ambient scope (reached before decl's own parameter scope is pushed, so it
// sees exactly what Runtime.renderMixinCall's own attrMap-building loop
// does), a map[string]string local named "__mixinAttrs<callID>" holding
// every one of call's own attributes under its own name: a bare attribute
// or any other value staticCallAttrValue classifies as static becomes that
// same literal string; anything else is built with genValueExpr(v.Value) —
// matching Runtime.renderMixinCall's own `v.IsBare ? "true" :
// evaluateExpr(v.Value)` build exactly, entry for entry. Returns the local's
// Go name, so genMixinCallAttrForward can bind it into decl's own parameter
// scope under the mixin's special "attributes" name.
//
// A FALLIBLE call attribute value (genValueExpr's own fallible flag — a
// top-level `/` or `%`) is refused with a clean, distinct error rather than
// generated: confirmed by direct probe against Runtime.renderMixinCall, a
// call attribute whose evaluateExpr call ERRORS (e.g. a division by zero)
// falls back to the RAW, UNEVALUATED expression STRING with NO render error
// at all — a fallback a Go expression that would itself return a runtime
// error instead of a string cannot reproduce. A call attribute value
// genValueExpr cannot compile at all (an unsupported shape) surfaces
// genValueExpr's own error unchanged.
func (g *generator) genMixinCallAttrsMap(call *MixinCallNode, callID int) (string, error) {
	names := make([]string, 0, len(call.Attributes))
	for name := range call.Attributes {
		names = append(names, name)
	}
	slices.Sort(names)

	entries := make([]string, 0, len(names))
	for _, name := range names {
		v := call.Attributes[name]
		if valStr, ok := staticCallAttrValue(v); ok {
			entries = append(entries, strconv.Quote(name)+": "+strconv.Quote(valStr))
			continue
		}
		goExpr, fallible, err := g.genValueExpr(v.Value)
		if err != nil {
			return "", fmt.Errorf("mixin %q: call attribute %q: %w", call.Name, name, err)
		}
		if fallible {
			return "", fmt.Errorf("mixin %q: call attribute %q is a fallible expression (a division or modulo operator) and is not supported in codegen: Runtime.renderMixinCall falls back to the raw, unevaluated expression string when its own evaluation errors at render time, with no render error at all — a fallback a runtime-erroring Go expression cannot reproduce", call.Name, name)
		}
		entries = append(entries, strconv.Quote(name)+": "+goExpr)
	}

	goName := fmt.Sprintf("__mixinAttrs%d", callID)
	g.writeRaw(goName + " := map[string]string{" + strings.Join(entries, ", ") + "}\n")
	return goName, nil
}

// genMixinBlockClosure generates content — the block content a call site
// passed to a slot-bearing mixin (call.BlockContent) — as a self-contained
// Go function literal expression, `func(w io.Writer) error { … return nil
// }`, suitable to pass directly as genMixinCall's block-callback argument.
//
// Runtime.renderMixinBlockSlot renders this exact content WHILE the callee's
// own isolated scope is active (Runtime.renderMixinCall pushes the mixin
// boundary sentinel and its own param frame together, before the body walk
// ever reaches the slot) — so a dynamic reference inside block content
// resolves against the CALLEE's PARAMETERS, bound to THIS call's argument
// values, never the caller's data or locals: the interpreter's own scope
// lookup (Runtime.lookup) stops descending at the mixin_boundary sentinel
// before ever reaching a caller frame, so even a caller local that happens to
// share a parameter's name is hidden entirely — the parameter wins.
//
// This models that scope directly rather than sidestepping it: params and
// margNames are genMixinCall's own decl.Parameters and the `__margN` local
// names it hoisted each call argument into (same order, one per declared
// parameter, "" for a missing trailing argument) — the SAME locals fed to
// the helper call itself, so the closure and the mixin body see the
// identical value from a single evaluation, exactly mirroring
// Runtime.renderMixinCall/evaluateMixinArg evaluating each argument once
// caller-side into `scope[param]`. The closure body is generated in a scope
// containing ONLY those bindings (g.scope holds exactly params[i] mapped to
// margNames[i], g.paramOnlyScope = true, reusing the exact guard
// genMixinFunc's own body generation uses): a reference to a declared
// parameter resolves to its `__margN` local, while ANY OTHER identifier (a
// top-level data field, a caller's `- var` local, `attributes`, the special
// `block` keyword used as a value) hits resolveFieldExpr's paramOnlyScope
// check and fails closed with a clean, generate-time error instead of
// guessing — a deliberate asymmetry: the interpreter renders "" for that
// missed lookup, this refuses to reproduce it silently.
//
// g.insideBlockClosure is set so a nested mixin call written as part of the
// block content (`+other(...)`) is also rejected, rather than attempted with
// the wrong scope, and so is ANY unbuffered code statement
// (genUnbufferedStatement's own g.insideBlockClosure check) — even a
// literal-only `- var x = "hi"` a later reference would resolve purely
// against its own local scope entry, never reaching this function's guard at
// all, is refused unconditionally: a param-scoped unbuffered local inside
// block content is a distinct, untested claim this increment deliberately
// does not make.
//
// g.body/g.static are saved and swapped out (not reset) for the duration,
// since — unlike genMixinFunc, which always runs before the main render
// function's own body walk has written anything — genMixinCall is reached
// FROM WITHIN that walk, so g.body may already hold real, unrelated
// statements that must survive this call untouched.
func (g *generator) genMixinBlockClosure(content []Node, mixinName string, params []string, margNames []string) (string, error) {
	savedBody := g.body
	savedStatic := g.static
	savedScope := g.scope
	savedParamOnly := g.paramOnlyScope
	savedInsideMixinBody := g.insideMixinBody
	savedInsideBlockClosure := g.insideBlockClosure
	savedBlockParamName := g.blockParamName

	g.body = strings.Builder{}
	g.static = strings.Builder{}
	g.scope = nil
	for i, p := range params {
		g.pushScope(p, margNames[i], reflectTypeString, false)
	}
	g.paramOnlyScope = true
	g.insideMixinBody = false
	g.insideBlockClosure = true
	g.blockParamName = ""

	var bodyErr error
	for _, child := range content {
		if err := g.genNode(child); err != nil {
			bodyErr = err
			break
		}
	}
	if bodyErr == nil {
		g.flushStatic()
	}
	closureBody := g.body.String()

	g.body = savedBody
	g.static = savedStatic
	g.scope = savedScope
	g.paramOnlyScope = savedParamOnly
	g.insideMixinBody = savedInsideMixinBody
	g.insideBlockClosure = savedInsideBlockClosure
	g.blockParamName = savedBlockParamName

	if bodyErr != nil {
		return "", fmt.Errorf("mixin %q: block content: %w", mixinName, bodyErr)
	}

	return "func(w io.Writer) error {\n" + closureBody + "return nil\n}", nil
}
