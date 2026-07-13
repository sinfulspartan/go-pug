package gopug

import (
	"fmt"
	"go/format"
	"math"
	"reflect"
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
// handling. Any node or expression shape outside the supported subset (mixins,
// includes/extends, dynamic class/style attributes, &attributes spreads,
// operators, method calls, unless/case, unescaped output, comments, …)
// returns a descriptive error instead of silently emitting something
// incorrect; those shapes are later increments.
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
	// tmpCounter is the next unused index nextTmp will hand out, so every
	// fallible value-expression extracted at a write site (genInterpolation,
	// genCode, genAttributes) gets its own uniquely named __vN/__errN locals
	// within the generated function body, even when several such extractions
	// appear in the same function.
	tmpCounter int
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
	case *CommentNode:
		return g.genComment(node)
	default:
		return fmt.Errorf("unsupported node/expr in codegen: %T", n)
	}
}

// genTag emits a tag's open tag (name + static attributes), its children
// (unless it is self-closing or a void element), and its close tag.
func (g *generator) genTag(tag *TagNode) error {
	g.writeStatic("<" + tag.Name)

	if err := g.genAttributes(tag.Attributes); err != nil {
		return fmt.Errorf("tag %q: %w", tag.Name, err)
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

	mark := g.scopeMark()
	for _, child := range tag.Children {
		if err := g.genNode(child); err != nil {
			g.scopeRestore(mark)
			return err
		}
	}
	g.scopeRestore(mark)

	g.writeStatic("</" + tag.Name + ">")
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
//   - a plain quoted string literal is escaped and baked into the static
//     buffer at generate time (htmlEscapeAttr), also unchanged;
//   - a dynamic value whose name IS an HTML boolean attribute
//     (isBooleanAttribute) requires a bool-typed field: it emits a
//     conditional bare write — ` name="true"` iff the field is true, nothing
//     at all iff false — matching the interpreter's omit-on-false behaviour
//     for these names. This path stays resolveFieldExpr-only (a bare field,
//     never a general value expression): a non-bool-typed value on such a
//     name is deferred (error) rather than risking a byte-identical breach —
//     the interpreter also omits a boolean-attribute-named value that merely
//     stringifies to "false" (e.g. a string field literally holding
//     "false"), a general rule this increment doesn't reproduce;
//   - a dynamic value on any other, non-"class" name is built by genValueExpr
//     (genValueExpr's whole supported grammar) and always escaped through the
//     exported EscapeAttr (never html.EscapeString — attribute escaping has
//     different rules from text escaping), applied once to the built value
//     as a whole rather than per-leaf, exactly as genInterpolation and genCode
//     do for the same value-context grammar. When genValueExpr reports the
//     value fallible (a top-level `/` or `%`), its genFallibleExtraction
//     prelude is emitted BEFORE the attribute's own static ` name="` text —
//     the extraction is a statement, so it must land as its own line ahead
//     of the write sequence it feeds, never interleaved inside it;
//   - a dynamic "class" value merging shorthand class tokens with one or
//     more bare string-field tokens (see genDynamicClass) emits a runtime
//     write joining the tokens with the exported JoinClasses (which drops an
//     empty token, matching the interpreter's empty-token rule) and escapes
//     the joined result with EscapeAttr;
//
// A style object, `&attributes`, an unescaped attribute, a class-object/
// array value, or any value genValueExpr still can't build (a
// non-string-keyed map index, most method calls, an array/object literal,
// …) is out of scope for this increment and returns an error rather than
// guessing at output that might not match the interpreter. With a nil
// Config.DataReflectType (type-blind mode), a dynamic value can't be
// classified as scalar or bool at all (nor a class token confirmed a string
// field), so only static/bare attributes are supported there, matching
// increment 1 unchanged.
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

		if val.Unescaped {
			return fmt.Errorf("unsupported unescaped attribute %q in codegen", name)
		}

		trimmed := strings.TrimSpace(val.Value)
		if lit, ok := unwrapQuotedLiteral(trimmed); ok {
			g.writeStatic(" " + name + `="` + htmlEscapeAttr(lit) + `"`)
			continue
		}

		if name == "class" {
			if err := g.genDynamicClass(trimmed); err != nil {
				return fmt.Errorf("attribute %q: %w", name, err)
			}
			continue
		}

		if g.rootType == nil {
			return fmt.Errorf("unsupported dynamic attribute %q in codegen (only static quoted values are supported in this increment)", name)
		}

		if isBooleanAttribute(name) {
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
		if fallible {
			valExpr = g.genFallibleExtraction(valExpr)
		}
		g.needsGopug = true
		g.writeStatic(" " + name + `="`)
		g.writeExprWrite("gopug.EscapeAttr(" + valExpr + ")")
		g.writeStatic(`"`)
	}
	return nil
}

// genDynamicClass emits a dynamic class="..." attribute for a value
// genAttributes has already determined is not a pure static literal —
// parseAttributes's merge contract (parser.go's class-merge branch) means
// that value is either a bare dynamic token on its own (`div(class=cls)`)
// or shorthand class tokens, individually quoted, followed by one bare
// dynamic token (`"text-end" cls`, `"btn" "large" variant`). It tokenises
// the value with strings.Fields and classifies each token as a static
// quoted literal or a bare identifier/dot-path resolving to a string field,
// then emits a single runtime write joining every token's Go expression
// through the exported JoinClasses (dropping an empty token, matching
// Runtime.renderTag's empty-token rule exactly) and escaping the joined
// result through EscapeAttr. A ternary/operator class expression, a class
// object/array value, or a token that isn't a static literal or a
// string-field reference is out of scope for this increment and returns an
// error instead of guessing at output the interpreter might not produce;
// so does a nil Config.DataReflectType, since without type information a
// bare class token can't be confirmed to resolve to a string field.
func (g *generator) genDynamicClass(trimmed string) error {
	if g.rootType == nil {
		return fmt.Errorf("unsupported dynamic class attribute in codegen (only static quoted values are supported without type information)")
	}
	if isOperatorExpr(trimmed) {
		return fmt.Errorf("unsupported dynamic class attribute in codegen (a ternary/operator class expression is not yet supported)")
	}

	words := strings.Fields(trimmed)
	for _, w := range words {
		if strings.HasPrefix(w, "{") || strings.HasPrefix(w, "[") {
			return fmt.Errorf("unsupported dynamic class attribute in codegen (a class object/array value is not yet supported)")
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

// genInterpolation emits a write of a #{expr} interpolation. The value is
// built by genValueExpr — a bare field/dot-path (unchanged from before this
// increment) or, as of the value-context expression compiler, an expression
// built from string/numeric/bool literals and the `+` operator — then always
// wrapped in html.EscapeString. Escaping a value genValueExpr already knows
// can't contain an HTML-special character (a bare numeric/bool field's
// stringify) is redundant work, but it is never wrong: it can't change the
// bytes those stringifications produce, so wrapping unconditionally keeps
// this function simple without breaking byte-identical output. Unescaped
// interpolation (`!{expr}`) is not yet supported.
func (g *generator) genInterpolation(n *InterpolationNode) error {
	if n.Unescaped {
		return fmt.Errorf("unsupported unescaped interpolation !{%s} in codegen", n.Expression)
	}
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
}

// genCode emits a buffered, escaped code node (`= expr`) as a write of
// html.EscapeString(genValueExpr(expr)) — the same escaping genInterpolation
// applies, since `= expr` and `#{expr}` are both HTML-escaped-by-default
// value positions in the interpreter. An unescaped buffered node (`!= expr`,
// written raw) is out of scope for this increment — it would need a
// value-context compiler decision about whether the emitted Go is trusted
// not to need escaping — so it returns a clear unsupported error instead of
// guessing. An unbuffered statement (`- stmt`, executed for its side effect
// with no output) is dispatched to genUnbufferedStatement, which supports
// only the plain-assignment subset (`- var x = <rhs>`) described there; every
// other unbuffered shape (mutation, a bare expression statement) still
// returns a clear unsupported error.
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
		return fmt.Errorf("unsupported unescaped code != %s in codegen", n.Expression)
	default:
		return g.genUnbufferedStatement(n.Expression)
	}
}

// genUnbufferedStatement emits an unbuffered code statement (`- stmt`),
// classified by classifyUnbufferedStmt — the SAME classifier
// Runtime.executeStatement itself uses, so codegen and the interpreter agree
// character-for-character on where a statement's operator sits. Only a plain
// assignment (`- var x = <rhs>`, dispatched to genUnbufferedAssign) is
// supported in this increment; a mutation (`x++`/`x--`/`x += e`/`x -= e`) and
// a bare expression statement (evaluated and discarded by the interpreter,
// possibly for a side effect or an error genValueExpr has no model for) each
// return their own clear, distinct unsupported error instead of guessing.
func (g *generator) genUnbufferedStatement(stmt string) error {
	kind, varName, rhsExpr := classifyUnbufferedStmt(stmt)

	switch kind {
	case unbufferedAssign:
		return g.genUnbufferedAssign(varName, rhsExpr)
	case unbufferedIncrement, unbufferedDecrement:
		return fmt.Errorf("unsupported unbuffered mutation %q in codegen (increment/decrement is not supported yet)", stmt)
	case unbufferedAddAssign, unbufferedSubAssign:
		return fmt.Errorf("unsupported unbuffered mutation %q in codegen (+=/-= is not supported yet)", stmt)
	default:
		return fmt.Errorf("unsupported unbuffered code - %s in codegen (a bare expression statement is not supported yet)", stmt)
	}
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
// A `- var` re-declaring a name already bound in scope (whether from an
// outer `- var`, an each-loop item variable, or an outer var of the same
// name) is also deferred: Runtime.setVar does not create a fresh binding in
// that case — it MUTATES the existing one in place, wherever in the
// interpreter's scopeStack it lives, which does not (in general) correspond
// to Go's own block-scoped shadowing that a nested `:=` would produce. A nil
// Config.DataReflectType (type-blind mode) is also deferred: there is no
// type information to prove the RHS is string- or bool-shaped at all.
func (g *generator) genUnbufferedAssign(varName, rhsExpr string) error {
	if g.rootType == nil {
		return fmt.Errorf("unsupported unbuffered assignment %q in codegen (Config.DataReflectType is required to type-check a `- var` right-hand side)", varName)
	}
	if !validGoIdentifier(varName) {
		return fmt.Errorf("unsupported unbuffered assignment target %q in codegen (only a bare identifier is supported as a `- var` left-hand side)", varName)
	}
	if g.isBound(varName) {
		return fmt.Errorf("unsupported unbuffered assignment %q in codegen (re-declaring or re-assigning an already-bound variable is not supported yet)", varName)
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
// name is never silently substituted for it.
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

// genEach emits a for-range loop over a slice field. Only the single-variable
// form (`each x in <field>`) with no index variable and no `each`/`else`
// empty-collection body is supported in this increment. In type-aware mode,
// the loop item variable is scoped to the collection field's element type
// (dereferencing pointers on the collection and/or its element), so a
// dot-path rooted at the item variable inside the loop body resolves
// correctly too.
func (g *generator) genEach(n *EachNode) error {
	if n.ItemVar == "" {
		return fmt.Errorf("unsupported each without an item variable in codegen")
	}
	if n.IndexVar != "" {
		return fmt.Errorf("unsupported each index variable %q in codegen", n.IndexVar)
	}
	if len(n.EmptyBody) > 0 {
		return fmt.Errorf("unsupported each/else in codegen")
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

	g.writeRaw(fmt.Sprintf("for _, %s := range %s {\n", n.ItemVar, collExpr))
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

// genConditional emits a Go if/else for `if <condition>` with an optional
// plain `else`, including an else-if chain (`else if`, represented in the
// AST as a single *ConditionalNode with IsElseIf set inside Alternate — see
// ast.go's ConditionalNode doc comment). The recursive genNode call on that
// inner *ConditionalNode nests a second if/else inside the outer else block
// (`if a {...} else { if b {...} else {...} }`), which is valid Go and
// renders byte-identically to the interpreter's own equivalent nesting of
// else-if — the extra braces only affect Go source structure, never the
// HTML output. `unless` is a later increment. The condition itself is
// translated by genCondition, which supports bare bool/numeric/string-field
// truthiness, `.length` truthiness, a bounded set of comparison operators,
// and the `&&`/`||`/`!` combinators over any of those; anything else
// returns an error (including an else-if whose own condition genCondition
// can't compile, which propagates that error unchanged).
func (g *generator) genConditional(n *ConditionalNode) error {
	if n.IsUnless {
		return fmt.Errorf("unsupported unless in codegen")
	}

	cond, err := g.genCondition(n.Condition)
	if err != nil {
		return err
	}

	g.writeRaw(fmt.Sprintf("if %s {\n", cond))
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

// conditionComparisonOps lists the comparison operators genCondition
// recognizes, in the exact order Runtime.evaluateExpr scans for them
// (runtime.go's own literal op list), so a top-level operator is identified
// the same way in both places.
var conditionComparisonOps = []string{"===", "!==", "==", "!=", "<=", ">=", "<", ">"}

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
// string literal's text itself looks numeric (parseNumber), since the
// interpreter's compareValues numeric-compares a numeric-looking string
// operand rather than string-comparing it.
type operandKind struct {
	shape          operandShape
	numType        reflect.Type
	numLiteralVal  float64
	numericLiteral bool
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
		return convertExpr(goExpr, typ, reflectTypeString, "string"), operandKind{shape: shapeOperandStringField}, nil
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
//   - a string field compared, with `==`/`!=` (or the `===`/`!==` aliases)
//     only, to a string literal that is NOT itself numeric-looking: a
//     numeric-looking string literal would numeric-compare in compareValues
//     (both operands would parse as numbers), not string-compare, so
//     emitting a Go string `==` there would diverge.
//
// Every other combination — an ordering compare (`< > <= >=`) between
// strings, two string literals, two string fields, a string operand against
// a numeric operand, or any operand genOperand itself rejected — returns an
// error instead of emitting a comparison that might not agree with the
// interpreter.
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
	case leftKind.isNumeric() && rightKind.isNumeric():
		if err := checkNumericComparable(leftKind, rightKind); err != nil {
			return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition: %w", leftRaw, op, rightRaw, err)
		}
		return leftExpr + " " + goOp + " " + rightExpr, nil

	case leftKind.isStringish() && rightKind.isStringish():
		if goOp != "==" && goOp != "!=" {
			return "", fmt.Errorf("unsupported string ordering comparison %q in codegen condition (%q %s %q)", op, leftRaw, op, rightRaw)
		}
		var litKind operandKind
		switch {
		case leftKind.shape == shapeOperandStringField && rightKind.shape == shapeOperandStringLiteral:
			litKind = rightKind
		case rightKind.shape == shapeOperandStringField && leftKind.shape == shapeOperandStringLiteral:
			litKind = leftKind
		default:
			return "", fmt.Errorf("unsupported string comparison %q %s %q in codegen condition (only a string field compared to a string literal is supported in this increment)", leftRaw, op, rightRaw)
		}
		if litKind.numericLiteral {
			return "", fmt.Errorf("unsupported comparison %q %s %q in codegen condition (a numeric-looking string literal compares numerically in the interpreter, not as a string)", leftRaw, op, rightRaw)
		}
		return leftExpr + " " + goOp + " " + rightExpr, nil

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
