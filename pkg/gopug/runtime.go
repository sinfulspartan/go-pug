package gopug

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// Runtime renders an AST to HTML.
type Runtime struct {
	ast     *DocumentNode
	data    map[string]interface{}
	globals map[string]interface{}
	out     io.Writer
	buf     *bytes.Buffer
	scope   []map[string]interface{} // stack of scopes for loops, conditionals, etc.
	doctype string
}

// NewRuntime creates a new runtime for rendering.
func NewRuntime(ast *DocumentNode, data map[string]interface{}) *Runtime {
	return &Runtime{
		ast:     ast,
		data:    data,
		globals: make(map[string]interface{}),
		buf:     &bytes.Buffer{},
		scope:   make([]map[string]interface{}, 1),
		doctype: "html",
	}
}

// Render renders the AST to a string.
func (r *Runtime) Render() (string, error) {
	r.scope[0] = r.data
	for _, node := range r.ast.Children {
		if err := r.renderNode(node); err != nil {
			return "", err
		}
	}
	return r.buf.String(), nil
}

// renderNode renders a single node.
func (r *Runtime) renderNode(node Node) error {
	switch n := node.(type) {
	case *TagNode:
		return r.renderTag(n)
	case *TextNode:
		return r.renderText(n)
	case *InterpolationNode:
		return r.renderInterpolation(n)
	case *CommentNode:
		return r.renderComment(n)
	case *CodeNode:
		return r.renderCode(n)
	case *ConditionalNode:
		return r.renderConditional(n)
	case *EachNode:
		return r.renderEach(n)
	case *WhileNode:
		return r.renderWhile(n)
	case *CaseNode:
		return r.renderCase(n)
	case *DoctypeNode:
		return r.renderDoctype(n)
	case *PipeNode:
		return r.renderPipe(n)
	case *BlockTextNode:
		return r.renderBlockText(n)
	case *LiteralHTMLNode:
		return r.renderLiteralHTML(n)
	case *BlockExpansionNode:
		return r.renderBlockExpansion(n)
	case *FilterNode:
		return r.renderFilter(n)
	case *MixinDeclNode:
		// Mixins are collected at compile time, skip rendering
		return nil
	case *BlockNode:
		// Blocks are resolved at compile time, skip rendering
		return nil
	case *ExtendsNode:
		// Extends is resolved at compile time, skip rendering
		return nil
	case *IncludeNode:
		// Includes are resolved at compile time, skip rendering
		return nil
	default:
		return fmt.Errorf("unknown node type: %T", node)
	}
}

// renderTag renders an HTML tag and its children.
func (r *Runtime) renderTag(tag *TagNode) error {
	// Write opening tag
	r.buf.WriteString("<")
	r.buf.WriteString(tag.Name)

	// Write attributes
	for name, val := range tag.Attributes {
		r.buf.WriteString(" ")
		r.buf.WriteString(name)

		if val.Value != "" && val.Value != name {
			r.buf.WriteString("=")

			// Evaluate the attribute value
			evaluated, err := r.evaluateExpr(val.Value)
			if err != nil {
				evaluated = val.Value
			}

			// Escape unless marked as unescaped
			if !val.Unescaped {
				evaluated = html.EscapeString(evaluated)
			}

			r.buf.WriteString("\"")
			r.buf.WriteString(evaluated)
			r.buf.WriteString("\"")
		}
	}

	if tag.SelfClose {
		r.buf.WriteString(" />")
		return nil
	}

	// Void elements never have a closing tag
	if isVoidElement(tag.Name) {
		r.buf.WriteString(">")
		return nil
	}

	r.buf.WriteString(">")

	// Render children
	for _, child := range tag.Children {
		if err := r.renderNode(child); err != nil {
			return err
		}
	}

	// Write closing tag
	r.buf.WriteString("</")
	r.buf.WriteString(tag.Name)
	r.buf.WriteString(">")

	return nil
}

// renderText renders plain text (escaped by default).
func (r *Runtime) renderText(text *TextNode) error {
	r.buf.WriteString(html.EscapeString(text.Content))
	return nil
}

// renderInterpolation renders #{...} or !{...} interpolation.
func (r *Runtime) renderInterpolation(interp *InterpolationNode) error {
	val, err := r.evaluateExpr(interp.Expression)
	if err != nil {
		return err
	}

	if !interp.Unescaped {
		val = html.EscapeString(val)
	}

	r.buf.WriteString(val)
	return nil
}

// renderComment renders HTML comments.
func (r *Runtime) renderComment(comment *CommentNode) error {
	if comment.Buffered {
		r.buf.WriteString("<!-- ")
		r.buf.WriteString(comment.Content)
		r.buf.WriteString(" -->")
	}
	return nil
}

// renderCode renders unbuffered, buffered, or unescaped code.
func (r *Runtime) renderCode(code *CodeNode) error {
	switch code.Type {
	case CodeUnbuffered:
		// Unbuffered code is executed but not output
		_, err := r.evaluateExpr(code.Expression)
		return err
	case CodeBuffered:
		// Buffered code is output (escaped)
		val, err := r.evaluateExpr(code.Expression)
		if err != nil {
			return err
		}
		r.buf.WriteString(html.EscapeString(val))
		return nil
	case CodeUnescaped:
		// Unescaped code is output as-is
		val, err := r.evaluateExpr(code.Expression)
		if err != nil {
			return err
		}
		r.buf.WriteString(val)
		return nil
	}
	return nil
}

// renderConditional renders if/else if/else blocks.
func (r *Runtime) renderConditional(cond *ConditionalNode) error {
	// Evaluate the condition
	val, err := r.evaluateExpr(cond.Condition)
	if err != nil {
		return err
	}

	// Convert to boolean
	boolVal := r.isTruthy(val)
	if cond.IsUnless {
		boolVal = !boolVal
	}

	if boolVal {
		// Render consequent
		for _, node := range cond.Consequent {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
	} else if len(cond.Alternate) > 0 {
		// Render alternate (else or else if)
		for _, node := range cond.Alternate {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderEach renders each/for loops.
func (r *Runtime) renderEach(each *EachNode) error {
	// Look up collection as raw value (not string-converted) so we can iterate it.
	collVal, ok := r.lookup(each.Collection)
	if !ok {
		// Fall back to expression evaluation for literals/expressions
		str, err := r.evaluateExpr(each.Collection)
		if err != nil {
			return err
		}
		collVal = str
	}

	// Convert to slice/map
	items := r.toSlice(collVal)

	if len(items) == 0 {
		// Render else body if present
		for _, node := range each.ElseBody {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
		return nil
	}

	// Render body for each item
	for i, item := range items {
		// Push new scope
		scope := make(map[string]interface{})

		// Set loop variables
		scope[each.Item] = item
		if each.Key != "" {
			scope[each.Key] = strconv.Itoa(i)
		}

		r.scope = append(r.scope, scope)

		// Render body
		for _, node := range each.Body {
			if err := r.renderNode(node); err != nil {
				r.scope = r.scope[:len(r.scope)-1]
				return err
			}
		}

		// Pop scope
		r.scope = r.scope[:len(r.scope)-1]
	}

	return nil
}

// renderWhile renders while loops.
func (r *Runtime) renderWhile(w *WhileNode) error {
	iterations := 0
	maxIterations := 10000 // prevent infinite loops

	for iterations < maxIterations {
		val, err := r.evaluateExpr(w.Condition)
		if err != nil {
			return err
		}

		if !r.isTruthy(val) {
			break
		}

		// Render body
		for _, node := range w.Body {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}

		iterations++
	}

	if iterations >= maxIterations {
		return fmt.Errorf("while loop exceeded max iterations")
	}

	return nil
}

// renderCase renders case/when/default statements.
func (r *Runtime) renderCase(c *CaseNode) error {
	// Evaluate case expression
	caseVal, err := r.evaluateExpr(c.Expression)
	if err != nil {
		return err
	}

	// Check each when clause
	matched := false
	for _, when := range c.Cases {
		whenVal, err := r.evaluateExpr(when.Expression)
		if err != nil {
			return err
		}

		if caseVal == whenVal {
			matched = true
		}

		if matched {
			// Render when body
			for _, node := range when.Body {
				if err := r.renderNode(node); err != nil {
					return err
				}
			}
			// No break in Pug case statements (fall-through by default)
		}
	}

	// Render default if no match
	if !matched && len(c.Default) > 0 {
		for _, node := range c.Default {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderDoctype renders doctype declarations.
func (r *Runtime) renderDoctype(dt *DoctypeNode) error {
	doctype := r.formatDoctype(dt.Value)
	r.buf.WriteString(doctype)
	r.doctype = dt.Value
	return nil
}

// renderPipe renders piped text.
func (r *Runtime) renderPipe(pipe *PipeNode) error {
	r.buf.WriteString(html.EscapeString(pipe.Content))
	return nil
}

// renderBlockText renders block text (indented text after .).
func (r *Runtime) renderBlockText(block *BlockTextNode) error {
	r.buf.WriteString(html.EscapeString(block.Content))
	return nil
}

// renderLiteralHTML renders literal HTML (line starting with <).
func (r *Runtime) renderLiteralHTML(lit *LiteralHTMLNode) error {
	r.buf.WriteString(lit.Content)
	return nil
}

// renderBlockExpansion renders block expansion (tag: child).
func (r *Runtime) renderBlockExpansion(exp *BlockExpansionNode) error {
	if err := r.renderTag(exp.Parent); err != nil {
		return err
	}
	return r.renderNode(exp.Child)
}

// renderFilter renders filter blocks (placeholder for now).
func (r *Runtime) renderFilter(filter *FilterNode) error {
	// Filters are typically applied at compile time.
	// For now, just output the content as-is.
	r.buf.WriteString(filter.Content)
	return nil
}

// evaluateExpr evaluates a simple expression against the current scope.
func (r *Runtime) evaluateExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)

	if expr == "" {
		return "", nil
	}

	// String literals (double or single quoted)
	if len(expr) >= 2 {
		if (expr[0] == '"' && expr[len(expr)-1] == '"') ||
			(expr[0] == '\'' && expr[len(expr)-1] == '\'') {
			return expr[1 : len(expr)-1], nil
		}
	}

	// Numeric literals
	if _, err := strconv.ParseFloat(expr, 64); err == nil {
		return expr, nil
	}

	// Boolean literals
	switch expr {
	case "true":
		return "true", nil
	case "false":
		return "false", nil
	case "null", "undefined", "nil":
		return "", nil
	}

	// String concatenation: "a" + variable or variable + "b"
	if strings.Contains(expr, " + ") {
		parts := strings.SplitN(expr, " + ", 2)
		left, err := r.evaluateExpr(strings.TrimSpace(parts[0]))
		if err != nil {
			return "", err
		}
		right, err := r.evaluateExpr(strings.TrimSpace(parts[1]))
		if err != nil {
			return "", err
		}
		return left + right, nil
	}

	// Variable lookup (with dot notation support)
	if val, ok := r.lookup(expr); ok {
		if val == nil {
			return "", nil
		}
		return fmt.Sprintf("%v", val), nil
	}

	// Unrecognised expression — return empty rather than error for now
	return "", nil
}

// lookup retrieves a value from the scope stack, searching innermost scope first.
// Supports dot notation: "user.name" will look up "user" then access field "name".
func (r *Runtime) lookup(key string) (interface{}, bool) {
	// Support dot notation: user.name
	parts := strings.Split(key, ".")
	root := strings.TrimSpace(parts[0])

	// Search from innermost to outermost scope
	var rootVal interface{}
	found := false
	for i := len(r.scope) - 1; i >= 0; i-- {
		if r.scope[i] == nil {
			continue
		}
		if val, ok := r.scope[i][root]; ok {
			rootVal = val
			found = true
			break
		}
	}

	if !found {
		// Check globals as fallback
		if val, ok := r.globals[root]; ok {
			rootVal = val
			found = true
		}
	}

	if !found {
		return nil, false
	}

	// Follow dot chain
	current := rootVal
	for j := 1; j < len(parts); j++ {
		part := strings.TrimSpace(parts[j])
		current = r.getField(current, part)
		if current == nil {
			return nil, false
		}
	}

	return current, true
}

// getField retrieves a field from an object (map or struct).
func (r *Runtime) getField(obj interface{}, field string) interface{} {
	if obj == nil {
		return nil
	}

	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Map {
		val := v.MapIndex(reflect.ValueOf(field))
		if val.IsValid() {
			return val.Interface()
		}
	} else if v.Kind() == reflect.Struct {
		fieldVal := v.FieldByName(field)
		if fieldVal.IsValid() {
			return fieldVal.Interface()
		}
	}

	return nil
}

// toSlice converts a value to a slice.
func (r *Runtime) toSlice(val interface{}) []interface{} {
	if val == nil {
		return []interface{}{}
	}

	v := reflect.ValueOf(val)
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		result := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = v.Index(i).Interface()
		}
		return result
	}

	if v.Kind() == reflect.Map {
		result := make([]interface{}, 0)
		for _, key := range v.MapKeys() {
			result = append(result, v.MapIndex(key).Interface())
		}
		return result
	}

	return []interface{}{val}
}

// isTruthy determines if a string-evaluated value is truthy.
func (r *Runtime) isTruthy(val string) bool {
	switch val {
	case "", "false", "0", "null", "undefined", "nil":
		return false
	}
	return true
}

// formatDoctype formats a doctype declaration.
func (r *Runtime) formatDoctype(dt string) string {
	switch strings.ToLower(dt) {
	case "html", "5":
		return "<!DOCTYPE html>"
	case "xml":
		return `<?xml version="1.0" encoding="utf-8" ?>`
	case "transitional":
		return `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`
	case "strict":
		return `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">`
	case "frameset":
		return `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Frameset//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-frameset.dtd">`
	case "1.1":
		return `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">`
	case "basic":
		return `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML Basic 1.1//EN" "http://www.w3.org/TR/xhtml-basic/xhtml-basic11.dtd">`
	case "mobile":
		return `<!DOCTYPE html PUBLIC "-//WAPFORUM//DTD XHTML Mobile 1.2//EN" "http://www.openmobilealliance.org/tech/DTD/xhtml-mobile12.dtd">`
	default:
		return fmt.Sprintf("<!DOCTYPE %s>", dt)
	}
}

// isVoidElement returns true if the tag name is an HTML void element
// (self-closing, no closing tag required).
func isVoidElement(tag string) bool {
	switch strings.ToLower(tag) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}
