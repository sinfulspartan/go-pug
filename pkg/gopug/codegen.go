package gopug

import (
	"fmt"
	"go/format"
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
// interpreter's Runtime.lookupAndStringify/isTruthy exactly. It also emits a
// runtime write for a dynamic scalar attribute value on a non-"class"
// attribute (a bare field or dot-path resolving to a scalar type — see
// genAttributes), escaped through the exported EscapeAttr for a string value
// and via bare strconv for every other scalar kind, and a conditional bare
// write for a bool-typed field on an HTML boolean-attribute name (present
// iff true, matching pug's omit-on-false). Any other field type (struct,
// slice, map, pointer, interface, float32, …) is out of scope and returns an
// error rather than guessing. When DataReflectType is nil, every field is
// assumed to be a string (the original untyped skeleton behavior), and only
// static/bare attributes are supported, unchanged.
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

// genConditional emits a Go if/else for `if <bool or numeric field>` with an
// optional plain `else`. `unless` and else-if chains are later increments.
// In type-aware mode, a numeric condition field (any int/uint kind or
// float64) is compared against zero to match the interpreter's stringify-
// then-isTruthy semantics (a numeric field stringifies to "0" only when it
// is zero); a bool field is used bare, as before. Any other condition field
// type — string, slice, map, pointer, struct, float32 — has quirky
// stringify-based truthiness (see the codegen design notes) and is a later
// increment, so it returns an error rather than guessing.
func (g *generator) genConditional(n *ConditionalNode) error {
	if n.IsUnless {
		return fmt.Errorf("unsupported unless in codegen")
	}
	if len(n.Alternate) == 1 {
		if _, ok := n.Alternate[0].(*ConditionalNode); ok {
			return fmt.Errorf("unsupported else-if chain in codegen")
		}
	}

	condExpr, condTyp, err := g.resolveFieldExpr(n.Condition)
	if err != nil {
		return err
	}

	cond := condExpr
	if condTyp != nil {
		switch condTyp.Kind() {
		case reflect.Bool:
			// Used bare: already the exact form Runtime.isTruthy's
			// FormatBool-derived "true"/"false" mirrors.
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float64:
			cond = condExpr + " != 0"
		default:
			return fmt.Errorf("unsupported condition field type %s in codegen if %q (only bool and numeric fields are supported in this increment)", condTyp, n.Condition)
		}
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
