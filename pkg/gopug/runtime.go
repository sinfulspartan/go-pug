package gopug

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// Runtime renders an AST to HTML.
type Runtime struct {
	ast            *DocumentNode
	data           map[string]interface{}
	globals        map[string]interface{}
	out            io.Writer
	buf            *bytes.Buffer
	scope          []map[string]interface{} // stack of scopes for loops, conditionals, etc.
	doctype        string
	mixins         map[string]*MixinDeclNode // collected mixin declarations
	mixinBlock     []Node                    // block nodes passed by the current mixin call
	inMixinContext bool                      // true when rendering inside a mixin body
	opts           *Options                  // compilation options (Basedir, Globals, Filters)
	basedir        string                    // resolved base directory for includes
	includedPaths  []string                  // stack of currently-rendering paths for cycle detection
}

// NewRuntime creates a new runtime for rendering.
func NewRuntime(ast *DocumentNode, data map[string]interface{}) *Runtime {
	return NewRuntimeWithOptions(ast, data, nil)
}

// NewRuntimeWithOptions creates a new runtime with explicit Options (used for
// includes so that Basedir and Globals are available during rendering).
func NewRuntimeWithOptions(ast *DocumentNode, data map[string]interface{}, opts *Options) *Runtime {
	r := &Runtime{
		ast:           ast,
		data:          data,
		globals:       make(map[string]interface{}),
		buf:           &bytes.Buffer{},
		scope:         make([]map[string]interface{}, 1),
		doctype:       "html",
		mixins:        make(map[string]*MixinDeclNode),
		opts:          opts,
		includedPaths: make([]string, 0),
	}
	if opts != nil && opts.Basedir != "" {
		r.basedir = opts.Basedir
	}
	return r
}

// Render renders the AST to a string.
func (r *Runtime) Render() (string, error) {
	r.scope[0] = r.data

	// First pass: collect all mixin declarations so they are available
	// regardless of declaration order relative to call sites.
	r.collectMixins(r.ast.Children)

	// Check if the template uses inheritance (extends as first meaningful node).
	if ext := r.findExtendsNode(r.ast.Children); ext != nil {
		return r.renderExtends(ext)
	}

	for _, node := range r.ast.Children {
		if err := r.renderNode(node); err != nil {
			return "", err
		}
	}
	return r.buf.String(), nil
}

// findExtendsNode returns the first ExtendsNode in the node list, skipping
// over comments and mixin declarations (which are allowed before extends).
// Returns nil if there is no ExtendsNode.
func (r *Runtime) findExtendsNode(nodes []Node) *ExtendsNode {
	for _, node := range nodes {
		switch n := node.(type) {
		case *CommentNode, *MixinDeclNode:
			continue
		case *ExtendsNode:
			return n
		default:
			return nil
		}
	}
	return nil
}

// collectMixins walks a node list and registers every MixinDeclNode found.
func (r *Runtime) collectMixins(nodes []Node) {
	for _, node := range nodes {
		if m, ok := node.(*MixinDeclNode); ok {
			r.mixins[m.Name] = m
		}
	}
}

// renderExtends handles template inheritance.
//
// Algorithm:
//  1. Resolve the fully-patched root AST via resolveExtendsAST (handles
//     chained extends of any depth without goto / label spaghetti).
//  2. Merge all collected mixins into the runtime.
//  3. Render the patched root AST with the current data.
func (r *Runtime) renderExtends(ext *ExtendsNode) (string, error) {
	// We need to know the "current file path" for relative resolution.
	// If we are inside an include stack, the top of the stack is the current
	// file; otherwise we use basedir as a hint (we create a synthetic path).
	currentPath := ""
	if len(r.includedPaths) > 0 {
		currentPath = r.includedPaths[len(r.includedPaths)-1]
	} else if r.basedir != "" {
		// Synthesise a path so that relative extends resolution works.
		currentPath = filepath.Join(r.basedir, "_root_.pug")
	}

	// resolveExtendsAST expects the *child* AST (this template's AST) and
	// the path of the child file so it can resolve the parent path relatively.
	// Since r.ast IS the child AST, we wrap the call appropriately.
	rootAST, mixins, err := r.resolveExtendsAST(currentPath, r.ast)
	if err != nil {
		return "", err
	}

	// Merge collected mixins.
	for k, v := range mixins {
		r.mixins[k] = v
	}
	r.collectMixins(rootAST.Children)

	// Render the fully-patched root.
	for _, node := range rootAST.Children {
		if err := r.renderNode(node); err != nil {
			return "", err
		}
	}
	return r.buf.String(), nil
}

// resolveExtendsAST resolves a chain of extends declarations and returns the
// fully-patched root DocumentNode along with all collected mixins. This is
// used to resolve multi-level inheritance chains without rendering.
func (r *Runtime) resolveExtendsAST(currentPath string, childAST *DocumentNode) (*DocumentNode, map[string]*MixinDeclNode, error) {
	mixins := make(map[string]*MixinDeclNode)

	// Track the current file in the path stack so that cycle detection can
	// catch mutual-extends cycles (e.g. a extends b, b extends a).
	// We only push real file paths, not the synthetic "_root_.pug" sentinel.
	if currentPath != "" && !strings.HasSuffix(currentPath, "_root_.pug") {
		absCurrentPath, err := filepath.Abs(currentPath)
		if err == nil {
			// Check for cycle before pushing.
			for _, p := range r.includedPaths {
				if p == absCurrentPath {
					return nil, nil, fmt.Errorf("extends: cycle — %q", absCurrentPath)
				}
			}
			r.includedPaths = append(r.includedPaths, absCurrentPath)
			defer func() { r.includedPaths = r.includedPaths[:len(r.includedPaths)-1] }()
		}
	}

	// Find ExtendsNode in childAST.
	var ext *ExtendsNode
	for _, node := range childAST.Children {
		switch node.(type) {
		case *CommentNode, *MixinDeclNode:
			if m, ok := node.(*MixinDeclNode); ok {
				mixins[m.Name] = m
			}
			continue
		case *ExtendsNode:
			ext = node.(*ExtendsNode)
		default:
		}
		break
	}

	if ext == nil {
		// No further extends — this is the root. Return as-is.
		for _, node := range childAST.Children {
			if m, ok := node.(*MixinDeclNode); ok {
				mixins[m.Name] = m
			}
		}
		return childAST, mixins, nil
	}

	// Resolve parent path.
	parentPath := ext.Path
	if len(parentPath) >= 2 &&
		((parentPath[0] == '"' && parentPath[len(parentPath)-1] == '"') ||
			(parentPath[0] == '\'' && parentPath[len(parentPath)-1] == '\'')) {
		parentPath = parentPath[1 : len(parentPath)-1]
	}

	base := filepath.Dir(currentPath)
	var resolved string
	if filepath.IsAbs(parentPath) {
		if r.basedir != "" {
			resolved = filepath.Join(r.basedir, parentPath)
		} else {
			resolved = parentPath
		}
	} else {
		resolved = filepath.Join(base, parentPath)
	}

	abs, err := filepath.Abs(resolved)
	if err != nil {
		return nil, nil, fmt.Errorf("extends: cannot resolve %q: %w", parentPath, err)
	}
	// Cycle detection.
	for _, p := range r.includedPaths {
		if p == abs {
			return nil, nil, fmt.Errorf("extends: cycle — %q", abs)
		}
	}
	if filepath.Ext(abs) == "" {
		abs += ".pug"
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("extends: cannot read %q: %w", abs, err)
	}
	lx := NewLexer(string(src))
	toks, err := lx.Lex()
	if err != nil {
		return nil, nil, fmt.Errorf("extends: lex error in %q: %w", abs, err)
	}
	pr := NewParser(toks)
	parentAST, err := pr.Parse()
	if err != nil {
		return nil, nil, fmt.Errorf("extends: parse error in %q: %w", abs, err)
	}

	// Recursively resolve the parent's parent chain.
	// (cycle detection is now handled at the top of resolveExtendsAST via the
	// currentPath push, so we no longer need a separate push/pop here)
	rootAST, parentMixins, err := r.resolveExtendsAST(abs, parentAST)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range parentMixins {
		mixins[k] = v
	}

	// Apply childAST's block overrides onto the resolved root.
	childBlocks := r.collectBlocks(childAST.Children)
	for _, node := range childAST.Children {
		if m, ok := node.(*MixinDeclNode); ok {
			mixins[m.Name] = m
		}
	}
	r.applyBlockOverrides(rootAST.Children, childBlocks)

	return rootAST, mixins, nil
}

// collectBlocks returns a map of block name → *BlockNode for all named blocks
// found at the top level of the given node list (child template overrides).
func (r *Runtime) collectBlocks(nodes []Node) map[string]*BlockNode {
	blocks := make(map[string]*BlockNode)
	for _, node := range nodes {
		if b, ok := node.(*BlockNode); ok {
			blocks[b.Name] = b
		}
	}
	return blocks
}

// applyBlockOverrides recursively walks a node slice (the parent/root AST) and
// replaces, appends to, or prepends each BlockNode whose name appears in the
// overrides map. The walk is deep so blocks nested inside tags, conditionals,
// etc. are also patched.
func (r *Runtime) applyBlockOverrides(nodes []Node, overrides map[string]*BlockNode) {
	for i, node := range nodes {
		switch n := node.(type) {
		case *BlockNode:
			override, ok := overrides[n.Name]
			if !ok {
				// No child override — keep parent default body but still walk
				// into it for nested blocks.
				r.applyBlockOverrides(n.Body, overrides)
				continue
			}
			switch override.Mode {
			case BlockModeAppend:
				n.Body = append(n.Body, override.Body...)
			case BlockModePrepend:
				n.Body = append(override.Body, n.Body...)
			default: // BlockModeReplace
				n.Body = override.Body
			}
			nodes[i] = n
			// Walk inside the now-patched body for any nested block slots.
			r.applyBlockOverrides(n.Body, overrides)

		case *TagNode:
			r.applyBlockOverrides(n.Children, overrides)
		case *ConditionalNode:
			r.applyBlockOverrides(n.Consequent, overrides)
			r.applyBlockOverrides(n.Alternate, overrides)
		case *EachNode:
			r.applyBlockOverrides(n.Body, overrides)
			r.applyBlockOverrides(n.ElseBody, overrides)
		case *WhileNode:
			r.applyBlockOverrides(n.Body, overrides)
		case *CaseNode:
			for _, when := range n.Cases {
				r.applyBlockOverrides(when.Body, overrides)
			}
			r.applyBlockOverrides(n.Default, overrides)
		case *MixinDeclNode:
			r.applyBlockOverrides(n.Body, overrides)
		case *BlockExpansionNode:
			r.applyBlockOverrides([]Node{n.Child}, overrides)
		}
	}
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
	case *TextRunNode:
		return r.renderTextRun(n)
	case *FilterNode:
		return r.renderFilter(n)
	case *MixinDeclNode:
		// Already collected in first pass — skip during render walk.
		return nil
	case *MixinCallNode:
		return r.renderMixinCall(n)
	case *BlockNode:
		if r.inMixinContext {
			// Inside a mixin body — render the caller's block content.
			return r.renderMixinBlockSlot()
		}
		// In the inheritance context (or top-level default), render the
		// block's body. By the time we reach here the block has already been
		// patched with child overrides (or kept with its parent default body).
		return r.renderBlockBody(n)
	case *ExtendsNode:
		// Extends is resolved before render — skip during normal walk.
		return nil
	case *IncludeNode:
		return r.renderInclude(n)
	default:
		return fmt.Errorf("unknown node type: %T", node)
	}
}

// renderBlockBody renders the body of a BlockNode (used during inheritance rendering).
func (r *Runtime) renderBlockBody(b *BlockNode) error {
	for _, node := range b.Body {
		if err := r.renderNode(node); err != nil {
			return err
		}
	}
	return nil
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
		// Unbuffered code is executed but not output.
		// Supported forms:
		//   var = expr      (assignment)
		//   var++           (increment)
		//   var--           (decrement)
		// Anything else is evaluated and discarded.
		return r.executeStatement(code.Expression)
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

// executeStatement executes an unbuffered code statement, handling assignment
// (var = expr), increment (var++), and decrement (var--).
// For anything else the expression is evaluated and the result discarded.
func (r *Runtime) executeStatement(stmt string) error {
	stmt = strings.TrimSpace(stmt)

	// var++ / var--
	if strings.HasSuffix(stmt, "++") {
		varName := strings.TrimSpace(stmt[:len(stmt)-2])
		val, _ := r.lookup(varName)
		n, ok := toFloat(val)
		if !ok {
			n = 0
		}
		r.setVar(varName, n+1)
		return nil
	}
	if strings.HasSuffix(stmt, "--") {
		varName := strings.TrimSpace(stmt[:len(stmt)-2])
		val, _ := r.lookup(varName)
		n, ok := toFloat(val)
		if !ok {
			n = 0
		}
		r.setVar(varName, n-1)
		return nil
	}

	// var = expr  (simple assignment — not ==)
	// Find the first = that is not preceded by !, <, >, = and not followed by =
	if idx := findAssignOp(stmt); idx >= 0 {
		varName := strings.TrimSpace(stmt[:idx])
		rhsExpr := strings.TrimSpace(stmt[idx+1:])
		val, err := r.evaluateExpr(rhsExpr)
		if err != nil {
			return err
		}
		r.setVar(varName, val)
		return nil
	}

	// Fallback — evaluate and discard
	_, err := r.evaluateExpr(stmt)
	return err
}

// setVar writes a variable into the innermost scope.
func (r *Runtime) setVar(name string, val interface{}) {
	// Walk from innermost to outermost to update an existing binding
	for i := len(r.scope) - 1; i >= 0; i-- {
		if r.scope[i] == nil {
			continue
		}
		if _, exists := r.scope[i][name]; exists {
			r.scope[i][name] = val
			return
		}
	}
	// Not found in any scope — create in the innermost scope
	top := len(r.scope) - 1
	if r.scope[top] == nil {
		r.scope[top] = make(map[string]interface{})
	}
	r.scope[top][name] = val
}

// findAssignOp finds the position of a simple = assignment operator that is
// not part of ==, !=, <=, >=.  Returns -1 if not found.
func findAssignOp(stmt string) int {
	for i := 0; i < len(stmt); i++ {
		if stmt[i] == '=' {
			// Check character before
			if i > 0 {
				prev := stmt[i-1]
				if prev == '!' || prev == '<' || prev == '>' || prev == '=' {
					continue
				}
			}
			// Check character after
			if i+1 < len(stmt) && stmt[i+1] == '=' {
				continue
			}
			return i
		}
	}
	return -1
}

// toFloat converts an interface{} value to float64.
func toFloat(v interface{}) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return f, err == nil
	}
	return 0, false
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

	// Handle map iteration separately so we can expose both key and value.
	if collVal != nil {
		v := reflect.ValueOf(collVal)
		if v.Kind() == reflect.Map {
			if v.Len() == 0 {
				for _, node := range each.ElseBody {
					if err := r.renderNode(node); err != nil {
						return err
					}
				}
				return nil
			}
			for _, mapKey := range v.MapKeys() {
				scope := make(map[string]interface{})
				scope[each.Item] = v.MapIndex(mapKey).Interface()
				if each.Key != "" {
					scope[each.Key] = fmt.Sprintf("%v", mapKey.Interface())
				}
				r.scope = append(r.scope, scope)
				for _, node := range each.Body {
					if err := r.renderNode(node); err != nil {
						r.scope = r.scope[:len(r.scope)-1]
						return err
					}
				}
				r.scope = r.scope[:len(r.scope)-1]
			}
			return nil
		}
	}

	// Convert to slice for arrays/slices and other values.
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

	// Check each when clause.
	// Pug fall-through rule: an empty when (no body) falls through to the
	// next when/default; a when WITH a body stops after rendering that body.
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
			if len(when.Body) == 0 {
				// Empty when — fall through to the next clause
				continue
			}
			// Non-empty when — render and stop
			for _, node := range when.Body {
				if err := r.renderNode(node); err != nil {
					return err
				}
			}
			return nil
		}
	}

	// Render default if no when matched (or all matching whens were empty)
	if matched {
		// All matching whens had empty bodies and fell through to default
		for _, node := range c.Default {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
	} else if len(c.Default) > 0 {
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

// renderInclude resolves and renders an include directive.
//
// Resolution order:
//  1. If the path starts with / it is resolved relative to r.basedir.
//  2. Otherwise it is resolved relative to the directory of the currently
//     rendering file (tracked via r.includedPaths), falling back to r.basedir.
//
// File handling:
//   - .pug (or no extension) → lex → parse → render into current buffer.
//   - Any other extension    → read raw and write directly into the buffer.
//
// Cycle detection: if the resolved absolute path is already in
// r.includedPaths we return an error to prevent infinite recursion.
func (r *Runtime) renderInclude(inc *IncludeNode) error {
	inclPath := inc.Path

	// Strip surrounding quotes if present (lexer may keep them)
	if len(inclPath) >= 2 &&
		((inclPath[0] == '"' && inclPath[len(inclPath)-1] == '"') ||
			(inclPath[0] == '\'' && inclPath[len(inclPath)-1] == '\'')) {
		inclPath = inclPath[1 : len(inclPath)-1]
	}

	// Resolve the path.
	var resolved string
	if filepath.IsAbs(inclPath) {
		// Absolute path — anchor to basedir if set, otherwise use as-is.
		if r.basedir != "" {
			resolved = filepath.Join(r.basedir, inclPath)
		} else {
			resolved = inclPath
		}
	} else {
		// Relative path — resolve relative to the directory of the currently
		// included file, or basedir if we are at the top level.
		base := r.basedir
		if len(r.includedPaths) > 0 {
			base = filepath.Dir(r.includedPaths[len(r.includedPaths)-1])
		}
		if base == "" {
			base = "."
		}
		resolved = filepath.Join(base, inclPath)
	}

	// Normalise and make absolute so cycle detection is reliable.
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("include: cannot resolve path %q: %w", inclPath, err)
	}

	// Cycle detection.
	for _, p := range r.includedPaths {
		if p == abs {
			return fmt.Errorf("include: cycle detected — %q includes itself", abs)
		}
	}

	// Add extension if missing (default to .pug).
	if filepath.Ext(abs) == "" {
		abs += ".pug"
	}

	ext := strings.ToLower(filepath.Ext(abs))

	// Push onto the include stack.
	r.includedPaths = append(r.includedPaths, abs)
	defer func() { r.includedPaths = r.includedPaths[:len(r.includedPaths)-1] }()

	if ext == ".pug" {
		// Read, lex, parse and render the included Pug file.
		src, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("include: cannot read %q: %w", abs, err)
		}

		lexer := NewLexer(string(src))
		tokens, err := lexer.Lex()
		if err != nil {
			return fmt.Errorf("include: lex error in %q: %w", abs, err)
		}

		parser := NewParser(tokens)
		ast, err := parser.Parse()
		if err != nil {
			return fmt.Errorf("include: parse error in %q: %w", abs, err)
		}

		// Collect mixins declared in the included file.
		r.collectMixins(ast.Children)

		// Render the included AST into the current buffer.
		for _, node := range ast.Children {
			if err := r.renderNode(node); err != nil {
				return fmt.Errorf("include: render error in %q: %w", abs, err)
			}
		}
		return nil
	}

	// Non-Pug file — read raw, optionally apply a filter, then write.
	raw, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("include: cannot read %q: %w", abs, err)
	}

	if inc.Filter != "" {
		// Apply the named filter to the raw file content.
		fn, ok := r.lookupFilter(inc.Filter)
		if !ok {
			return fmt.Errorf("include: filter %q is not registered; register it via Options.Filters", inc.Filter)
		}
		result, err := fn(string(raw))
		if err != nil {
			return fmt.Errorf("include: filter %q error on %q: %w", inc.Filter, abs, err)
		}
		r.buf.WriteString(result)
		return nil
	}

	r.buf.Write(raw)
	return nil
}

// renderTextRun renders a mixed sequence of text and interpolation nodes.
func (r *Runtime) renderTextRun(run *TextRunNode) error {
	for _, node := range run.Nodes {
		if err := r.renderNode(node); err != nil {
			return err
		}
	}
	return nil
}

// renderMixinCall renders a mixin call by pushing a new scope containing the
// named arguments, then walking the mixin's body nodes.
func (r *Runtime) renderMixinCall(call *MixinCallNode) error {
	decl, ok := r.mixins[call.Name]
	if !ok {
		return fmt.Errorf("mixin %q is not defined", call.Name)
	}

	// Build the argument scope: map parameter names to evaluated call arguments.
	scope := make(map[string]interface{})

	for i, param := range decl.Parameters {
		if i < len(call.Arguments) {
			// Evaluate the argument expression in the current (caller) scope.
			val, err := r.evaluateExpr(call.Arguments[i])
			if err != nil {
				return err
			}
			scope[param] = val
		} else {
			scope[param] = "" // missing arg defaults to empty string
		}
	}

	// Handle rest parameter — collect remaining arguments as a slice.
	if decl.RestParam != "" {
		rest := make([]interface{}, 0)
		for i := len(decl.Parameters); i < len(call.Arguments); i++ {
			val, err := r.evaluateExpr(call.Arguments[i])
			if err != nil {
				return err
			}
			rest = append(rest, val)
		}
		scope[decl.RestParam] = rest
	}

	// Expose caller-supplied HTML attributes as the "attributes" variable.
	// Each entry is the evaluated attribute value string.
	if len(call.Attributes) > 0 {
		attrMap := make(map[string]interface{})
		for k, v := range call.Attributes {
			evaluated, err := r.evaluateExpr(v.Value)
			if err != nil {
				evaluated = v.Value
			}
			attrMap[k] = evaluated
		}
		scope["attributes"] = attrMap
	}

	// Save the previous mixin block and install the call's block content.
	prevBlock := r.mixinBlock
	r.mixinBlock = call.Block

	// Push scope and render the mixin body.
	r.scope = append(r.scope, scope)
	prevMixinContext := r.inMixinContext
	r.inMixinContext = true
	for _, node := range decl.Body {
		if err := r.renderNode(node); err != nil {
			r.scope = r.scope[:len(r.scope)-1]
			r.inMixinContext = prevMixinContext
			r.mixinBlock = prevBlock
			return err
		}
	}
	r.scope = r.scope[:len(r.scope)-1]
	r.inMixinContext = prevMixinContext

	// Restore the previous mixin block.
	r.mixinBlock = prevBlock
	return nil
}

// renderMixinBlockSlot renders the block content supplied by the mixin caller.
// If no block was provided, nothing is rendered (empty slot).
func (r *Runtime) renderMixinBlockSlot() error {
	for _, node := range r.mixinBlock {
		if err := r.renderNode(node); err != nil {
			return err
		}
	}
	return nil
}

// renderFilter applies a named filter to its content and writes the result.
//
// Filter lookup order:
//  1. r.opts.Filters (user-registered filters supplied via Options)
//
// Subfilter chaining: filter.Subfilter is a colon-separated list of filter
// names (e.g. "inner" for :outer:inner). The innermost filter is applied
// first, then each outer filter in turn, matching Pug semantics.
//
//	:outer:inner
//	  content
//
// is equivalent to: outer(inner(content))
func (r *Runtime) renderFilter(filter *FilterNode) error {
	content := filter.Content

	// Build the ordered list of filters to apply: innermost first.
	// filter.Name is the outermost; filter.Subfilter is a colon-separated
	// chain of inner filters (may be empty).
	var chain []string
	if filter.Subfilter != "" {
		// Subfilter string is already colon-separated innermost→outermost order
		// as stored by the parser; split and reverse so we apply inner-first.
		parts := strings.Split(filter.Subfilter, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				chain = append(chain, parts[i])
			}
		}
	}
	// Outermost filter is always last to be applied.
	chain = append(chain, filter.Name)

	// Apply each filter in the chain.
	for _, name := range chain {
		fn, ok := r.lookupFilter(name)
		if !ok {
			return fmt.Errorf("filter %q is not registered; register it via Options.Filters", name)
		}
		result, err := fn(content)
		if err != nil {
			return fmt.Errorf("filter %q error: %w", name, err)
		}
		content = result
	}

	r.buf.WriteString(content)
	return nil
}

// lookupFilter finds a filter function by name. It checks Options.Filters.
func (r *Runtime) lookupFilter(name string) (func(string) (string, error), bool) {
	if r.opts != nil && r.opts.Filters != nil {
		if fn, ok := r.opts.Filters[name]; ok {
			return fn, true
		}
	}
	return nil, false
}

// evaluateExpr evaluates a simple expression against the current scope.
func (r *Runtime) evaluateExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)

	if expr == "" {
		return "", nil
	}

	// Ternary: cond ? a : b  (lowest precedence of all)
	if idx := findTernary(expr); idx >= 0 {
		cond, err := r.evaluateExpr(expr[:idx])
		if err != nil {
			return "", err
		}
		rest := expr[idx+1:] // everything after ?
		colonIdx := findBinaryOp(rest, ":")
		if colonIdx < 0 {
			return "", fmt.Errorf("malformed ternary expression: %s", expr)
		}
		if r.isTruthy(cond) {
			return r.evaluateExpr(rest[:colonIdx])
		}
		return r.evaluateExpr(rest[colonIdx+1:])
	}

	// Logical OR: a || b  (lowest precedence, evaluated left-to-right)
	if idx := findBinaryOp(expr, "||"); idx >= 0 {
		left, err := r.evaluateExpr(expr[:idx])
		if err != nil {
			return "", err
		}
		if r.isTruthy(left) {
			return left, nil
		}
		return r.evaluateExpr(expr[idx+2:])
	}

	// Logical AND: a && b
	if idx := findBinaryOp(expr, "&&"); idx >= 0 {
		left, err := r.evaluateExpr(expr[:idx])
		if err != nil {
			return "", err
		}
		if !r.isTruthy(left) {
			return "false", nil
		}
		return r.evaluateExpr(expr[idx+2:])
	}

	// Comparison operators (order matters: check longer operators first)
	for _, op := range []string{"===", "!==", "==", "!=", "<=", ">=", "<", ">"} {
		if idx := findBinaryOp(expr, op); idx >= 0 {
			left, err := r.evaluateExpr(expr[:idx])
			if err != nil {
				return "", err
			}
			right, err := r.evaluateExpr(expr[idx+len(op):])
			if err != nil {
				return "", err
			}
			result := r.compareValues(left, right, op)
			if result {
				return "true", nil
			}
			return "false", nil
		}
	}

	// Logical NOT: !expr
	if strings.HasPrefix(expr, "!") && !strings.HasPrefix(expr, "!=") {
		inner, err := r.evaluateExpr(expr[1:])
		if err != nil {
			return "", err
		}
		if r.isTruthy(inner) {
			return "false", nil
		}
		return "true", nil
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

	// Array/map index access: expr[key]
	if idx := findIndexOp(expr); idx >= 0 {
		objExpr := expr[:idx]
		keyExpr := expr[idx+1 : len(expr)-1] // strip [ and ]
		obj, ok := r.lookup(strings.TrimSpace(objExpr))
		if !ok {
			return "", nil
		}
		key, err := r.evaluateExpr(keyExpr)
		if err != nil {
			return "", err
		}
		result := r.indexValue(obj, key)
		if result == nil {
			return "", nil
		}
		return fmt.Sprintf("%v", result), nil
	}

	// Variable lookup (with dot notation support)
	if val, ok := r.lookup(expr); ok {
		if val == nil {
			return "", nil
		}
		// Format float64 cleanly — strip trailing zeros so that a counter
		// incremented via ++ renders as "1" not "1" (already fine) but also
		// "1.5" not "1.500000".
		if f, ok := val.(float64); ok {
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		}
		return fmt.Sprintf("%v", val), nil
	}

	// Unrecognised expression — return empty rather than error for now
	return "", nil
}

// findTernary locates the top-level ? operator for ternary expressions.
// Returns the index of ? or -1 if not found.
func findTernary(expr string) int {
	depth := 0
	inDouble := false
	inSingle := false
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if ch == '\\' && (inDouble || inSingle) {
			i++
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if inDouble || inSingle {
			continue
		}
		if ch == '(' || ch == '[' || ch == '{' {
			depth++
		} else if ch == ')' || ch == ']' || ch == '}' {
			depth--
		} else if ch == '?' && depth == 0 {
			return i
		}
	}
	return -1
}

// findIndexOp finds a top-level [...] index operation at the end of an
// expression, e.g. "arr[0]" or "obj["key"]".
// Returns the position of the opening [ or -1 if not found.
func findIndexOp(expr string) int {
	if len(expr) == 0 || expr[len(expr)-1] != ']' {
		return -1
	}
	// Walk backwards to find the matching [
	depth := 0
	inDouble := false
	inSingle := false
	for i := len(expr) - 1; i >= 0; i-- {
		ch := expr[i]
		if ch == ']' && !inDouble && !inSingle {
			depth++
		} else if ch == '[' && !inDouble && !inSingle {
			depth--
			if depth == 0 {
				if i == 0 {
					return -1 // bare [key] with no object
				}
				return i
			}
		} else if ch == '"' {
			inDouble = !inDouble
		} else if ch == '\'' {
			inSingle = !inSingle
		}
	}
	return -1
}

// indexValue retrieves element at string key/index from a slice or map.
func (r *Runtime) indexValue(obj interface{}, key string) interface{} {
	v := reflect.ValueOf(obj)
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		// key should be a numeric index
		i, err := strconv.Atoi(strings.TrimSpace(key))
		if err != nil || i < 0 || i >= v.Len() {
			return nil
		}
		return v.Index(i).Interface()
	case reflect.Map:
		val := v.MapIndex(reflect.ValueOf(key))
		if val.IsValid() {
			return val.Interface()
		}
	}
	return nil
}

// findBinaryOp finds the position of a binary operator in an expression,
// skipping over quoted strings and balanced parentheses.
// Returns -1 if the operator is not found at the top level.
func findBinaryOp(expr, op string) int {
	depth := 0
	inDouble := false
	inSingle := false
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if ch == '\\' && (inDouble || inSingle) {
			i++ // skip escaped character
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if inDouble || inSingle {
			continue
		}
		if ch == '(' || ch == '[' || ch == '{' {
			depth++
			continue
		}
		if ch == ')' || ch == ']' || ch == '}' {
			depth--
			continue
		}
		if depth == 0 && i+len(op) <= len(expr) && expr[i:i+len(op)] == op {
			// Make sure it's surrounded by non-operator characters (rough boundary check)
			return i
		}
	}
	return -1
}

// compareValues compares two string-represented values with the given operator.
// Numeric comparison is used when both sides parse as numbers.
func (r *Runtime) compareValues(left, right, op string) bool {
	// Normalise: strip surrounding quotes if any (from literal evaluation)
	leftF, leftIsNum := parseNumber(left)
	rightF, rightIsNum := parseNumber(right)

	if leftIsNum && rightIsNum {
		switch op {
		case "==", "===":
			return leftF == rightF
		case "!=", "!==":
			return leftF != rightF
		case "<":
			return leftF < rightF
		case ">":
			return leftF > rightF
		case "<=":
			return leftF <= rightF
		case ">=":
			return leftF >= rightF
		}
	}

	// String comparison
	switch op {
	case "==", "===":
		return left == right
	case "!=", "!==":
		return left != right
	case "<":
		return left < right
	case ">":
		return left > right
	case "<=":
		return left <= right
	case ">=":
		return left >= right
	}
	return false
}

// parseNumber attempts to parse a string as a float64.
func parseNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
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
