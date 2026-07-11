package gopug

import (
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"
)

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
}

// GenerateGo translates ast into a gofmt-ed Go source file defining
//
//	func <FuncName>(w io.Writer, d <DataType>) error
//
// which writes the same HTML byte sequence the interpreter (Template.Render)
// would produce for equivalent data, for the minimal grammar subset this
// increment supports: doctype, nested tags including void elements, static
// attributes and class/id shorthand, plain text, #{field-or-dot-path}
// interpolation of string fields, one level of `each item in <slice field>`,
// and `if <bool field>` with an optional `else`.
//
// GenerateGo assumes complete, well-typed data and does no nil-guarding —
// like Pug itself, and unlike the interpreter's lenient missing-value
// handling. Any node or expression shape outside the supported subset (mixins,
// includes/extends, dynamic attributes, operators, method calls, unless/case,
// unescaped output, comments, …) returns a descriptive error instead of
// silently emitting something incorrect; those shapes are later increments.
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

	g := &generator{}
	for _, child := range ast.Children {
		if err := g.genNode(child); err != nil {
			return nil, err
		}
	}
	g.flushStatic()

	var src strings.Builder
	fmt.Fprintf(&src, "package %s\n\n", cfg.PackageName)
	src.WriteString("import (\n\t\"html\"\n\t\"io\"\n)\n\n")
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
	// scope holds the names of each-loop item variables currently in scope,
	// innermost last, so a bare identifier or dot-path whose first segment
	// matches one of them resolves to the Go loop variable directly instead
	// of being treated as a field of d.
	scope []string
}

func (g *generator) isBound(name string) bool {
	for _, b := range g.scope {
		if b == name {
			return true
		}
	}
	return false
}

func (g *generator) pushScope(name string) {
	g.scope = append(g.scope, name)
}

func (g *generator) popScope() {
	g.scope = g.scope[:len(g.scope)-1]
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

	attrStr, err := staticAttrsString(tag.Attributes)
	if err != nil {
		return fmt.Errorf("tag %q: %w", tag.Name, err)
	}
	g.writeStatic(attrStr)

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

// staticAttrsString serialises a tag's attributes exactly the way
// Runtime.renderTag does for the static-only subset this increment supports:
// same id/class/alphabetical ordering, same quoting, same htmlEscapeAttr
// escaping. Every value must be a plain quoted string literal (or a bare
// boolean attribute); anything else — &attributes spreads, unescaped
// attributes, or an expression — is a later increment and returns an error.
func staticAttrsString(attrs map[string]*AttributeValue) (string, error) {
	if _, ok := attrs["&attributes"]; ok {
		return "", fmt.Errorf("unsupported dynamic &attributes in codegen")
	}

	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		order := func(n string) int {
			switch n {
			case "id":
				return 0
			case "class":
				return 1
			default:
				return 2
			}
		}
		oi, oj := order(names[i]), order(names[j])
		if oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})

	var b strings.Builder
	for _, name := range names {
		val := attrs[name]

		if val.IsBare {
			b.WriteString(" ")
			b.WriteString(name)
			continue
		}

		if val.Unescaped {
			return "", fmt.Errorf("unsupported unescaped attribute %q in codegen", name)
		}

		lit, ok := unwrapQuotedLiteral(strings.TrimSpace(val.Value))
		if !ok {
			return "", fmt.Errorf("unsupported dynamic attribute %q in codegen (only static quoted values are supported in this increment)", name)
		}

		b.WriteString(" ")
		b.WriteString(name)
		b.WriteString(`="`)
		b.WriteString(htmlEscapeAttr(lit))
		b.WriteString(`"`)
	}
	return b.String(), nil
}

// genInterpolation emits an escaped write of a #{expr} interpolation, where
// expr must be a bare identifier or dot-path (resolveFieldExpr enforces
// that); unescaped interpolation is not yet supported.
func (g *generator) genInterpolation(n *InterpolationNode) error {
	if n.Unescaped {
		return fmt.Errorf("unsupported unescaped interpolation !{%s} in codegen", n.Expression)
	}
	goExpr, err := g.resolveFieldExpr(n.Expression)
	if err != nil {
		return err
	}
	g.writeExprWrite("html.EscapeString(" + goExpr + ")")
	return nil
}

// resolveFieldExpr translates a Pug bare identifier or dot-path into the
// equivalent Go expression against the data parameter d, taking any
// currently bound each-loop variables into account. Anything that isn't one
// of those two trivial shapes (an operator, method call, literal, index
// expression, …) is out of scope for this increment and returns an error.
func (g *generator) resolveFieldExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)

	shape, val := classifySimpleShape(expr)
	switch shape {
	case shapeIdentifier:
		if g.isBound(val) {
			return val, nil
		}
		return "d." + val, nil
	case shapeDotPath:
		first := val
		if idx := strings.IndexByte(val, '.'); idx >= 0 {
			first = val[:idx]
		}
		if g.isBound(first) {
			return val, nil
		}
		return "d." + val, nil
	default:
		return "", fmt.Errorf("unsupported expression in codegen: %q (only bare identifiers and dot-paths of fields are supported in this increment)", expr)
	}
}

// genEach emits a for-range loop over a slice field. Only the single-variable
// form (`each x in <field>`) with no index variable and no `each`/`else`
// empty-collection body is supported in this increment.
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

	collExpr, err := g.resolveFieldExpr(n.CollectionExpr)
	if err != nil {
		return err
	}

	g.writeRaw(fmt.Sprintf("for _, %s := range %s {\n", n.ItemVar, collExpr))
	g.pushScope(n.ItemVar)
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

// genConditional emits a Go if/else for `if <bool field>` with an optional
// plain `else`. `unless` and else-if chains are later increments.
func (g *generator) genConditional(n *ConditionalNode) error {
	if n.IsUnless {
		return fmt.Errorf("unsupported unless in codegen")
	}
	if len(n.Alternate) == 1 {
		if _, ok := n.Alternate[0].(*ConditionalNode); ok {
			return fmt.Errorf("unsupported else-if chain in codegen")
		}
	}

	condExpr, err := g.resolveFieldExpr(n.Condition)
	if err != nil {
		return err
	}

	g.writeRaw(fmt.Sprintf("if %s {\n", condExpr))
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
