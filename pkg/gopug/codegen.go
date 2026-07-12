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
// through genValueExpr). Any value genValueExpr can't build (a ternary, a
// method call, …), or a non-scalar field type (struct, slice, map, pointer,
// interface, float32, …), is out of scope and returns an error rather than
// guessing. When DataReflectType is nil, every field is assumed to be a
// string (the original untyped skeleton behavior), and only static/bare
// attributes and bare-field conditions are supported, unchanged.
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
	if g.needsHTML {
		src.WriteString("\t\"html\"\n")
	}
	src.WriteString("\t\"io\"\n")
	if g.needsStrconv {
		src.WriteString("\t\"strconv\"\n")
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
	// scope holds each-loop item variables currently in scope, innermost
	// last, so a bare identifier or dot-path whose first segment matches one
	// of them resolves to the Go loop variable directly instead of being
	// treated as a field of d.
	scope []scopeVar
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
}

// scopeVar is one entry in the generator's each-loop variable scope stack: a
// Go loop variable name paired with its element reflect.Type (nil in
// type-blind mode).
type scopeVar struct {
	name string
	typ  reflect.Type
}

// lookupScope searches the scope stack innermost-first for name, returning
// its bound type and whether it was found. A found entry's typ is nil only
// when the generator itself is in type-blind mode (rootType == nil).
func (g *generator) lookupScope(name string) (reflect.Type, bool) {
	for i := len(g.scope) - 1; i >= 0; i-- {
		if g.scope[i].name == name {
			return g.scope[i].typ, true
		}
	}
	return nil, false
}

func (g *generator) isBound(name string) bool {
	_, ok := g.lookupScope(name)
	return ok
}

func (g *generator) pushScope(name string, typ reflect.Type) {
	g.scope = append(g.scope, scopeVar{name: name, typ: typ})
}

func (g *generator) popScope() {
	g.scope = g.scope[:len(g.scope)-1]
}

// derefType unwraps t through any number of pointer indirections, returning
// the first non-pointer type reached (or nil if t is nil).
func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// typeForPath walks a dot-path of already-split field-name segments starting
// from typ, dereferencing pointers at each step, and returns the resolved
// leaf reflect.Type. path is the original dotted expression, used only for
// the error message. An empty segments slice returns typ unchanged (the
// path is exactly the starting type itself, e.g. a bound each-loop scalar
// used bare).
func typeForPath(typ reflect.Type, path string, segments []string) (reflect.Type, error) {
	cur := typ
	for _, seg := range segments {
		cur = derefType(cur)
		if cur == nil || cur.Kind() != reflect.Struct {
			return nil, fmt.Errorf("unsupported field path %q: %s is not a field of a struct", path, seg)
		}
		f, ok := cur.FieldByName(seg)
		if !ok {
			return nil, fmt.Errorf("unsupported field path %q: %q is not a field of %s", path, seg, cur)
		}
		cur = f.Type
	}
	return cur, nil
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

	for _, child := range tag.Children {
		if err := g.genNode(child); err != nil {
			return err
		}
	}

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
//     (a bare field/dot-path, a `+` concat, or a backtick template literal —
//     genValueExpr's whole supported grammar) and always escaped through the
//     exported EscapeAttr (never html.EscapeString — attribute escaping has
//     different rules from text escaping), applied once to the built value
//     as a whole rather than per-leaf, exactly as genInterpolation and genCode
//     do for the same value-context grammar;
//   - a dynamic "class" value merging shorthand class tokens with one or
//     more bare string-field tokens (see genDynamicClass) emits a runtime
//     write joining the tokens with the exported JoinClasses (which drops an
//     empty token, matching the interpreter's empty-token rule) and escapes
//     the joined result with EscapeAttr;
//
// A style object, `&attributes`, an unescaped attribute, a class-object/
// array/ternary value, or any value genValueExpr can't build (a ternary, a
// method call, an index expression, …) is out of scope for this increment
// and returns an error rather than guessing at output that might not match
// the interpreter. With a nil Config.DataReflectType (type-blind mode), a
// dynamic value can't be classified as scalar or bool at all (nor a class
// token confirmed a string field), so only static/bare attributes are
// supported there, matching increment 1 unchanged.
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

		valExpr, err := g.genValueExpr(trimmed)
		if err != nil {
			return fmt.Errorf("attribute %q: %w", name, err)
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
	valExpr, err := g.genValueExpr(n.Expression)
	if err != nil {
		return err
	}
	g.needsHTML = true
	g.writeExprWrite("html.EscapeString(" + valExpr + ")")
	return nil
}

// genCode emits a buffered, escaped code node (`= expr`) as a write of
// html.EscapeString(genValueExpr(expr)) — the same escaping genInterpolation
// applies, since `= expr` and `#{expr}` are both HTML-escaped-by-default
// value positions in the interpreter. An unbuffered statement (`- expr`,
// executed for its side effect with no output) and an unescaped buffered
// node (`!= expr`, written raw) are both out of scope for this increment —
// the interpreter's unbuffered path can run arbitrary variable
// assignment/loop statements genValueExpr has no model for, and unescaped
// output would need a value-context compiler decision about whether the
// emitted Go is trusted not to need escaping — so both return a clear
// unsupported error instead of guessing.
func (g *generator) genCode(n *CodeNode) error {
	switch n.Type {
	case CodeBuffered:
		valExpr, err := g.genValueExpr(n.Expression)
		if err != nil {
			return err
		}
		g.needsHTML = true
		g.writeExprWrite("html.EscapeString(" + valExpr + ")")
		return nil
	case CodeUnescaped:
		return fmt.Errorf("unsupported unescaped code != %s in codegen", n.Expression)
	default:
		return fmt.Errorf("unsupported unbuffered code - %s in codegen", n.Expression)
	}
}

// genValueExpr emits a Go expression of type string equal to what
// Runtime.evaluateExpr(expr) would return, for the grammar subset this
// increment supports: leaves (a bare field/dot-path, a quoted string
// literal, a numeric literal, true/false/null/undefined/nil) and the total
// arithmetic operators `-`, `+`, and `*`. It walks the same
// operator-precedence order evaluateExpr does — strip balanced outer
// parens, then check in turn for a top-level ternary, `||`, `&&`, each
// comparison operator, a leading unary `!`, a quoted string literal, a
// template literal, an array/object literal, a numeric literal, the
// true/false/null keywords, subtraction, addition, and finally
// multiplication — so that when two operators are both present, genValueExpr
// splits on the same top-level one evaluateExpr would. Division and modulo
// are fallible at runtime (a zero divisor aborts Render with an error) and
// still return a descriptive "not yet supported" error here, since a value
// expression as currently modeled has no way to emit the statement prelude a
// fallible op would need. Every other construct evaluateExpr supports beyond
// these (method calls, index expressions, template/array/object literals) is
// a later increment and returns a descriptive "unsupported" error here
// instead of emitting something that might not match — the correctness bar
// is byte-identical to the interpreter, not a best guess.
func (g *generator) genValueExpr(expr string) (string, error) {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if idx := findTernary(expr); idx >= 0 {
		return "", fmt.Errorf("unsupported ternary operator in codegen value expression %q", expr)
	}
	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		return "", fmt.Errorf("unsupported || operator in codegen value expression %q", expr)
	}
	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		return "", fmt.Errorf("unsupported && operator in codegen value expression %q", expr)
	}
	for _, op := range conditionComparisonOps {
		if idx := findBinaryOp(expr, op); idx >= 0 {
			return "", fmt.Errorf("unsupported comparison operator %q in codegen value expression %q", op, expr)
		}
	}
	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		return "", fmt.Errorf("unsupported ! operator in codegen value expression %q", expr)
	}

	if lit, ok := unwrapQuotedLiteral(expr); ok {
		return strconv.Quote(lit), nil
	}
	if strings.HasPrefix(expr, "`") {
		return g.genTemplateLiteral(expr)
	}
	if strings.HasPrefix(expr, "[") {
		return "", fmt.Errorf("unsupported array literal in codegen value expression %q", expr)
	}
	if strings.HasPrefix(expr, "{") {
		return "", fmt.Errorf("unsupported object literal in codegen value expression %q", expr)
	}

	if f, ok := parseJSNumber(expr); ok {
		return strconv.Quote(formatCanonicalLiteral(f)), nil
	}

	switch expr {
	case "true":
		return `"true"`, nil
	case "false":
		return `"false"`, nil
	case "null", "undefined", "nil":
		return `""`, nil
	}

	if idx := findSubtraction(expr); idx >= 0 {
		leftExpr, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", err
		}
		rightExpr, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", err
		}
		g.needsGopug = true
		return "gopug.Sub(" + leftExpr + ", " + rightExpr + ")", nil
	}

	if idx := findBinaryOp(expr, "+"); idx >= 0 {
		leftExpr, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", err
		}
		rightExpr, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", err
		}
		g.needsGopug = true
		return "gopug.Add(" + leftExpr + ", " + rightExpr + ")", nil
	}

	if idx := findRightmostOp(expr, '*'); idx >= 0 {
		leftExpr, err := g.genValueExpr(expr[:idx])
		if err != nil {
			return "", err
		}
		rightExpr, err := g.genValueExpr(expr[idx+1:])
		if err != nil {
			return "", err
		}
		g.needsGopug = true
		return "gopug.Mul(" + leftExpr + ", " + rightExpr + ")", nil
	}
	if idx := findRightmostOp(expr, '/'); idx >= 0 {
		return "", fmt.Errorf("division in codegen not yet supported (fallible value-expression) %q", expr)
	}
	if idx := findRightmostOp(expr, '%'); idx >= 0 {
		return "", fmt.Errorf("modulo in codegen not yet supported (fallible value-expression) %q", expr)
	}
	if idx := findIndexOp(expr); idx >= 0 {
		return "", fmt.Errorf("unsupported index expression in codegen value expression %q", expr)
	}

	goExpr, typ, err := g.resolveFieldExpr(expr)
	if err != nil {
		return "", err
	}
	return g.genScalarStringify(goExpr, typ)
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
// that "unsupported" error rather than guessing. Escaping (html.EscapeString
// or EscapeAttr) is the caller's job, applied once to the whole result,
// exactly as for every other genValueExpr leaf.
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
			valExpr, err := g.genValueExpr(interp)
			if err != nil {
				return "", fmt.Errorf("template literal %q: %w", expr, err)
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
// currently bound each-loop variables into account, and — when the
// generator is type-aware (rootType != nil) — resolves the expression's
// reflect.Type by walking the struct fields along the path (an each-loop
// variable's own type if the first segment is bound, otherwise rootType),
// dereferencing pointers at each step. In type-blind mode (rootType == nil)
// the returned type is always nil, preserving the untyped skeleton's
// behavior exactly. Anything that isn't a bare identifier or dot-path (an
// operator, method call, literal, index expression, …), or a dot-path
// segment that isn't a field of the struct type it's resolved against, is
// out of scope for this increment and returns an error.
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
	if boundTyp, ok := g.lookupScope(first); ok {
		if g.rootType == nil {
			return val, nil, nil
		}
		typ, err := typeForPath(boundTyp, expr, segments[1:])
		if err != nil {
			return "", nil, err
		}
		return val, typ, nil
	}

	goExpr := "d." + val
	if g.rootType == nil {
		return goExpr, nil, nil
	}
	typ, err := typeForPath(g.rootType, expr, segments)
	if err != nil {
		return "", nil, err
	}
	return goExpr, typ, nil
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
	g.pushScope(n.ItemVar, elemTyp)
	for _, child := range n.Body {
		if err := g.genNode(child); err != nil {
			g.popScope()
			return err
		}
	}
	g.popScope()
	g.flushStatic()
	g.body.WriteString("}\n")
	return nil
}

// genConditional emits a Go if/else for `if <condition>` with an optional
// plain `else`. `unless` and else-if chains are later increments. The
// condition itself is translated by genCondition, which supports bare
// bool/numeric/string-field truthiness, `.length` truthiness, a bounded set
// of comparison operators, and the `&&`/`||`/`!` combinators over any of
// those; anything else returns an error.
func (g *generator) genConditional(n *ConditionalNode) error {
	if n.IsUnless {
		return fmt.Errorf("unsupported unless in codegen")
	}
	if len(n.Alternate) == 1 {
		if _, ok := n.Alternate[0].(*ConditionalNode); ok {
			return fmt.Errorf("unsupported else-if chain in codegen")
		}
	}

	cond, err := g.genCondition(n.Condition)
	if err != nil {
		return err
	}

	g.writeRaw(fmt.Sprintf("if %s {\n", cond))
	for _, child := range n.Consequent {
		if err := g.genNode(child); err != nil {
			return err
		}
	}
	g.flushStatic()

	if len(n.Alternate) > 0 {
		g.body.WriteString("} else {\n")
		for _, child := range n.Alternate {
			if err := g.genNode(child); err != nil {
				return err
			}
		}
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
// operand with no top-level operator: a `.length` expression (needs
// Config.DataReflectType, since classifying its base requires a type), or a
// bare field/dot-path. A bool field is used bare (native, already the exact
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
// this order: a `.length` expression, a numeric literal (parseJSNumber
// succeeds), a quoted string literal (unwrapQuotedLiteral succeeds), and a
// bare field/dot-path resolving (via resolveFieldExpr) to a bool, string, or
// numeric scalar. Anything else — including a non-scalar field, an operator
// sub-expression, or a method call other than `.length` — returns an error.
func (g *generator) genOperand(expr string) (string, operandKind, error) {
	expr = strings.TrimSpace(expr)

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
