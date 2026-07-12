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
// genInterpolation) and per-type truthiness for conditions (bool used bare,
// numeric kinds compared against zero — see genConditional), matching the
// interpreter's Runtime.lookupAndStringify/isTruthy exactly. A condition may
// also use a numeric comparison, a string-equality comparison, or a
// `.length` operand (see genCondition) — but only for the bounded-agreement
// subset provably byte-identical to the interpreter's compareValues; the
// combinators `&&`/`||`/`!`, arithmetic, ternary, and every other operand
// combination return an error instead. It also emits a runtime write for a
// dynamic scalar attribute value on a non-"class" attribute (a bare field or
// dot-path resolving to a scalar type — see genAttributes), escaped through
// the exported EscapeAttr for a string value and via bare strconv for every
// other scalar kind, and a conditional bare write for a bool-typed field on
// an HTML boolean-attribute name (present iff true, matching pug's
// omit-on-false). Any other field type (struct, slice, map, pointer,
// interface, float32, …) is out of scope and returns an error rather than
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
//     for these names. A non-bool-typed value on such a name is deferred
//     (error) rather than risking a byte-identical breach: the interpreter
//     also omits a boolean-attribute-named value that merely stringifies to
//     "false" (e.g. a string field literally holding "false"), a general
//     rule this increment doesn't reproduce;
//   - a dynamic value on any other, non-"class" name that resolves (via
//     resolveFieldExpr) to a scalar field type emits a runtime write: a
//     string field is escaped through the exported EscapeAttr (never
//     html.EscapeString — attribute escaping has different rules from text
//     escaping); every other scalar kind stringifies with strconv, unescaped,
//     since none of those stringifications can contain an HTML-special
//     character;
//   - a dynamic "class" value merging shorthand class tokens with one or
//     more bare string-field tokens (see genDynamicClass) emits a runtime
//     write joining the tokens with the exported JoinClasses (which drops an
//     empty token, matching the interpreter's empty-token rule) and escapes
//     the joined result with EscapeAttr;
//
// A style object, `&attributes`, an unescaped attribute, a class-object/
// array/ternary value, or any value that isn't a static literal / bare
// identifier / dot-path scalar (an operator, method call, index expression)
// is out of scope for this increment and returns an error rather than
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

		goExpr, typ, err := g.resolveFieldExpr(trimmed)
		if err != nil {
			return fmt.Errorf("attribute %q: %w", name, err)
		}

		if isBooleanAttribute(name) {
			if typ.Kind() != reflect.Bool {
				return fmt.Errorf("unsupported dynamic attribute %q in codegen (only a bool-typed value is supported for an HTML boolean attribute name in this increment)", name)
			}
			boolExpr := convertExpr(goExpr, typ, reflectTypeBool, "bool")
			g.writeRaw(fmt.Sprintf("if %s {\n", boolExpr))
			g.body.WriteString("io.WriteString(w, " + strconv.Quote(" "+name+`="true"`) + ")\n")
			g.body.WriteString("}\n")
			continue
		}

		g.writeStatic(" " + name + `="`)
		switch typ.Kind() {
		case reflect.String:
			g.needsGopug = true
			g.writeExprWrite("gopug.EscapeAttr(" + convertExpr(goExpr, typ, reflectTypeString, "string") + ")")
		case reflect.Bool:
			g.needsStrconv = true
			g.writeExprWrite("strconv.FormatBool(" + convertExpr(goExpr, typ, reflectTypeBool, "bool") + ")")
		case reflect.Int:
			g.needsStrconv = true
			g.writeExprWrite("strconv.Itoa(" + convertExpr(goExpr, typ, reflectTypeInt, "int") + ")")
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			g.needsStrconv = true
			g.writeExprWrite("strconv.FormatInt(int64(" + goExpr + "), 10)")
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			g.needsStrconv = true
			g.writeExprWrite("strconv.FormatUint(uint64(" + goExpr + "), 10)")
		case reflect.Float64:
			g.needsStrconv = true
			g.writeExprWrite("strconv.FormatFloat(" + convertExpr(goExpr, typ, reflectTypeFloat64, "float64") + ", 'f', -1, 64)")
		default:
			return fmt.Errorf("unsupported non-scalar field type %s in codegen attribute %q", typ, name)
		}
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

// genInterpolation emits a write of a #{expr} interpolation, where expr must
// be a bare identifier or dot-path (resolveFieldExpr enforces that);
// unescaped interpolation is not yet supported. In type-aware mode (a
// non-nil Config.DataReflectType), the emitted stringify matches
// Runtime.lookupAndStringify's per-type cases exactly: string is
// html.EscapeString'd, every numeric/bool scalar is a bare strconv.* call
// (none of their stringifications can contain HTML-special characters, so
// escaping them would be wasted work); any non-scalar field type (struct,
// slice, map, pointer, interface, float32, complex, …) is unsupported and
// returns an error rather than guessing at a stringification the interpreter
// wouldn't produce. In type-blind mode (nil DataReflectType), the field is
// assumed to be a string, matching the untyped codegen skeleton.
func (g *generator) genInterpolation(n *InterpolationNode) error {
	if n.Unescaped {
		return fmt.Errorf("unsupported unescaped interpolation !{%s} in codegen", n.Expression)
	}
	goExpr, typ, err := g.resolveFieldExpr(n.Expression)
	if err != nil {
		return err
	}

	if typ == nil {
		g.needsHTML = true
		g.writeExprWrite("html.EscapeString(" + goExpr + ")")
		return nil
	}

	switch typ.Kind() {
	case reflect.String:
		g.needsHTML = true
		g.writeExprWrite("html.EscapeString(" + convertExpr(goExpr, typ, reflectTypeString, "string") + ")")
	case reflect.Bool:
		g.needsStrconv = true
		g.writeExprWrite("strconv.FormatBool(" + convertExpr(goExpr, typ, reflectTypeBool, "bool") + ")")
	case reflect.Int:
		g.needsStrconv = true
		g.writeExprWrite("strconv.Itoa(" + convertExpr(goExpr, typ, reflectTypeInt, "int") + ")")
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		g.needsStrconv = true
		g.writeExprWrite("strconv.FormatInt(int64(" + goExpr + "), 10)")
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		g.needsStrconv = true
		g.writeExprWrite("strconv.FormatUint(uint64(" + goExpr + "), 10)")
	case reflect.Float64:
		g.needsStrconv = true
		g.writeExprWrite("strconv.FormatFloat(" + convertExpr(goExpr, typ, reflectTypeFloat64, "float64") + ", 'f', -1, 64)")
	default:
		return fmt.Errorf("unsupported non-scalar field type %s in codegen interpolation #{%s}", typ, n.Expression)
	}
	return nil
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
// bool/numeric-field truthiness (as before), `.length` truthiness, and a
// bounded set of comparison operators; anything else returns an error.
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
// a leading unary `!`. A ternary/`||`/`&&`/`!` — and, later, arithmetic —
// changes evaluateExpr's returned VALUE, not just a boolean result, so
// reproducing it exactly would require reproducing the interpreter's
// string-valued operator semantics in full; that is a later increment, so
// each of these returns an error here. A comparison operator is handed to
// genComparison. With none of those present, the whole expression is a
// single operand (a bare field, or a `.length` expression) used for
// truthiness.
func (g *generator) genCondition(expr string) (string, error) {
	expr = stripBalancedOuterParens(strings.TrimSpace(expr))

	if idx := findTernary(expr); idx >= 0 {
		return "", fmt.Errorf("unsupported ternary operator in codegen condition %q", expr)
	}
	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		return "", fmt.Errorf("unsupported || operator in codegen condition %q", expr)
	}
	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		return "", fmt.Errorf("unsupported && operator in codegen condition %q", expr)
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
		return "", fmt.Errorf("unsupported ! operator in codegen condition %q", expr)
	}

	return g.genOperandTruthiness(expr)
}

// genOperandTruthiness emits the truthiness form of a single condition
// operand with no top-level operator: a `.length` expression (needs
// Config.DataReflectType, since classifying its base requires a type), or a
// bare field/dot-path — unchanged from increment 2a's genConditional (and,
// in type-blind mode, from increment 1's): a bool field is used bare, a
// numeric field is compared against zero, and any other field type is an
// error rather than guessing at its stringify-based truthiness.
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
		// Used bare: already the exact form Runtime.isTruthy's
		// FormatBool-derived "true"/"false" mirrors.
		return condExpr, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float64:
		return condExpr + " != 0", nil
	default:
		return "", fmt.Errorf("unsupported condition field type %s in codegen if %q (only bool and numeric fields are supported in this increment)", condTyp, expr)
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
// numeric types, not just the same reflect.Kind), the parsed value AND the
// original token text for a numeric literal (checkLiteralAgainstFieldKind
// needs the exact text — a plain decimal integer token like
// "9223372036854775807" carries more precision than float64 can hold, so a
// range check based only on the parsed float64 can't reliably tell a
// boundary-valid literal from an out-of-range one), and whether a string
// literal's text itself looks numeric (parseNumber), since the
// interpreter's compareValues numeric-compares a numeric-looking string
// operand rather than string-comparing it.
type operandKind struct {
	shape          operandShape
	numType        reflect.Type
	numLiteralVal  float64
	numLiteralText string
	numericLiteral bool
}

func (k operandKind) isNumeric() bool {
	return k.shape == shapeOperandNumericField || k.shape == shapeOperandNumericLiteral
}

func (k operandKind) isStringish() bool {
	return k.shape == shapeOperandStringField || k.shape == shapeOperandStringLiteral
}

// checkLiteralGoSafe rejects a numeric literal token that Go's compiler
// would read as a different value than the interpreter does. The
// interpreter always parses a numeric token as base-10 (via parseNumber /
// strconv.ParseFloat), but a numeric literal emitted verbatim into
// generated Go source is parsed by Go's own literal rules: a token
// beginning with "0x"/"0X" (hex), "0o"/"0O" (octal), or "0b"/"0B" (binary)
// is read in that base, and a plain integer token with a leading "0"
// followed by more digits (no "." or exponent) is read as legacy octal —
// e.g. "0100" is decimal 100 to the interpreter but octal 64 (= decimal 64)
// to Go. Emitting such a literal verbatim would compile cleanly yet
// silently compare against the wrong value, breaking bounded agreement. A
// leading sign ("-" or "+") is stripped before the check, since Go applies
// the same base rules to the digits that follow either unary sign, and both
// Go and the interpreter's parseNumber accept a "+"-prefixed literal.
// Within the remaining digits, an underscore digit separator ("0_100") is
// tolerated as part of the octal-integer run rather than treated as an
// "unrecognized character, must be safe" signal — Go's octal grammar allows
// "_" separators, and parseNumber's strconv.ParseFloat accepts them too, so
// a token like "0_100" is just as much an octal/decimal disagreement as
// "0100". Forms Go reads identically to the interpreter — "0" alone, and
// any literal containing "." or "e"/"E" (always decimal floating-point in
// Go, regardless of a leading zero) — are left untouched.
func checkLiteralGoSafe(token string) error {
	digits := strings.TrimLeft(token, "+-")

	if len(digits) < 2 || digits[0] != '0' {
		return nil
	}

	switch digits[1] {
	case 'x', 'X', 'o', 'O', 'b', 'B':
		return fmt.Errorf("unsupported numeric literal %q in codegen (leading-zero/base-prefixed literals are read differently by Go)", token)
	}

	if strings.ContainsAny(digits, ".eE") {
		return nil
	}

	if strings.Trim(digits, "0123456789_") == "" {
		return fmt.Errorf("unsupported numeric literal %q in codegen (leading-zero/base-prefixed literals are read differently by Go)", token)
	}
	return nil
}

// genOperand resolves a single condition operand — one side of a
// comparison, or the whole condition when it's bare — to a Go expression
// plus its operandKind classification. The supported shapes, checked in
// this order: a `.length` expression, a numeric literal (parseNumber
// succeeds), a quoted string literal (unwrapQuotedLiteral succeeds), and a
// bare field/dot-path resolving (via resolveFieldExpr) to a bool, string, or
// numeric scalar. Anything else — including a non-scalar field, an operator
// sub-expression, or a method call other than `.length` — returns an error.
func (g *generator) genOperand(expr string) (string, operandKind, error) {
	expr = strings.TrimSpace(expr)

	if dotIdx := findTopLevelDot(expr); dotIdx > 0 && expr[dotIdx+1:] == "length" {
		return g.genLengthOperand(expr[:dotIdx], expr)
	}

	if f, ok := parseNumber(expr); ok {
		if err := checkLiteralGoSafe(expr); err != nil {
			return "", operandKind{}, err
		}
		return expr, operandKind{shape: shapeOperandNumericLiteral, numLiteralVal: f, numLiteralText: expr}, nil
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
		return checkLiteralAgainstFieldKind(right.numLiteralText, right.numLiteralVal, left.numType)
	case right.shape == shapeOperandNumericField:
		return checkLiteralAgainstFieldKind(left.numLiteralText, left.numLiteralVal, right.numType)
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

// float64IntBoundary and float64UintBoundary are the exact float64 values
// of 2^63 and 2^64 — the first magnitude that no longer fits in int64/
// uint64, respectively. Both are exact powers of two, so — unlike
// math.MaxInt64/math.MaxUint64 — they round-trip through float64 with no
// precision loss, which matters here: math.MaxInt64 (2^63-1) itself is NOT
// exactly representable in float64 and rounds UP to 2^63 when converted, so
// comparing a parsed literal's float64 approximation against a
// float64-rounded math.MaxInt64 cannot distinguish the valid literal
// "9223372036854775807" from the invalid "9223372036854775808" — both parse
// to the identical float64 value 2^63. These exact boundary constants are
// only used as a fallback for a literal form checkLiteralAgainstFieldKind
// can't range-check exactly (see its doc comment); the common plain-decimal
// case never reaches them.
const (
	float64IntBoundary  = 9223372036854775808.0  // 2^63
	float64UintBoundary = 18446744073709551616.0 // 2^64
)

// checkLiteralAgainstFieldKind reports an error unless the numeric literal
// — litText its original token text, f its strconv.ParseFloat value — is
// representable as fieldType without truncation or overflow: a fractional
// value against an integer-kind field, a negative value against an
// unsigned-kind field, or a magnitude beyond what the field's sized kind
// (int8, uint8, int32, …) can hold would either fail to compile as a direct
// Go comparison or silently change the field's meaning, so all three are
// rejected rather than emitted.
//
// The range check is exact whenever litText is a plain base-10 integer
// token (optionally signed): strconv.ParseInt/ParseUint, given the field
// kind's own bit width, parses litText's arbitrary-precision decimal digits
// directly and returns a range error if it doesn't fit — the identical test
// the Go compiler itself applies to an integer constant, so it correctly
// tells "9223372036854775807" (int64 field, fits exactly) apart from
// "9223372036854775808" (one more, doesn't) even though both round to the
// SAME float64 value (2^63) and are therefore indistinguishable by any
// float64-only check.
//
// A literal that ISN'T a plain base-10 integer token (scientific notation
// like "1e19", underscores, a whole-valued decimal like "3.0") falls back
// to an approximate float64 bound check against the exact power-of-two
// boundary constants above, then reflect.Value.OverflowInt/OverflowUint for
// a sized kind narrower than the field's own comparison type. This fallback
// carries the same float64-precision residual near the int64/uint64
// boundary as the exact path avoids, but it's unreachable for the plain
// decimal literals this increment's tests (and realistic Pug templates)
// actually use.
func checkLiteralAgainstFieldKind(litText string, f float64, fieldType reflect.Type) error {
	switch fieldType.Kind() {
	case reflect.Float64, reflect.Float32:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f != math.Trunc(f) {
			return fmt.Errorf("a fractional numeric literal cannot be compared directly to integer field type %s", fieldType)
		}
		if _, err := strconv.ParseInt(litText, 10, integerKindBits(fieldType.Kind())); err != nil {
			numErr, ok := err.(*strconv.NumError)
			if !ok || numErr.Err != strconv.ErrRange {
				// Not a plain base-10 integer token — fall back to the
				// approximate float64 bound check.
				if f < -float64IntBoundary || f >= float64IntBoundary {
					return fmt.Errorf("numeric literal %v is out of range for integer field type %s", f, fieldType)
				}
				if reflect.New(fieldType).Elem().OverflowInt(int64(f)) {
					return fmt.Errorf("numeric literal %v is out of range for integer field type %s", f, fieldType)
				}
				return nil
			}
			return fmt.Errorf("numeric literal %s is out of range for integer field type %s", litText, fieldType)
		}
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if f != math.Trunc(f) || f < 0 {
			return fmt.Errorf("numeric literal %v is not a valid value of unsigned field type %s", f, fieldType)
		}
		if _, err := strconv.ParseUint(litText, 10, integerKindBits(fieldType.Kind())); err != nil {
			numErr, ok := err.(*strconv.NumError)
			if !ok || numErr.Err != strconv.ErrRange {
				// Not a plain base-10 integer token — fall back to the
				// approximate float64 bound check.
				if f >= float64UintBoundary {
					return fmt.Errorf("numeric literal %v is out of range for unsigned field type %s", f, fieldType)
				}
				if reflect.New(fieldType).Elem().OverflowUint(uint64(f)) {
					return fmt.Errorf("numeric literal %v is out of range for unsigned field type %s", f, fieldType)
				}
				return nil
			}
			return fmt.Errorf("numeric literal %s is out of range for unsigned field type %s", litText, fieldType)
		}
		return nil
	default:
		return fmt.Errorf("unsupported numeric field type %s", fieldType)
	}
}
