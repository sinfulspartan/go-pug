package gopug

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Runtime renders an AST to HTML.
type Runtime struct {
	ast          *DocumentNode
	data         map[string]interface{}
	globals      map[string]interface{}
	htmlBuf      *bytes.Buffer
	scopeStack   []map[string]interface{}
	doctype      string
	mixinDecls   map[string]*MixinDeclNode
	callerBlock  []Node
	inMixin      bool
	opts         *Options
	includeBase  string
	includeStack []string
	prettyIndent int
}

// NewRuntime creates a Runtime with no options.
func NewRuntime(ast *DocumentNode, data map[string]interface{}) *Runtime {
	return NewRuntimeWithOptions(ast, data, nil)
}

// NewRuntimeWithOptions creates a Runtime with explicit Options. Basedir and
// Globals from opts are available during rendering (e.g. for includes).
func NewRuntimeWithOptions(ast *DocumentNode, data map[string]interface{}, opts *Options) *Runtime {
	r := &Runtime{
		ast:          ast,
		data:         data,
		globals:      make(map[string]interface{}),
		htmlBuf:      &bytes.Buffer{},
		scopeStack:   make([]map[string]interface{}, 1),
		doctype:      "html",
		mixinDecls:   make(map[string]*MixinDeclNode),
		opts:         opts,
		includeStack: make([]string, 0),
	}
	if opts != nil && opts.Basedir != "" {
		r.includeBase = opts.Basedir
	}
	return r
}

func (r *Runtime) pretty() bool {
	return r.opts != nil && r.opts.Pretty
}

// prettyNewline writes a newline followed by two-space indentation per level.
// Does nothing in compact mode.
func (r *Runtime) prettyNewline() {
	if !r.pretty() {
		return
	}
	r.htmlBuf.WriteByte('\n')
	for i := 0; i < r.prettyIndent; i++ {
		r.htmlBuf.WriteString("  ")
	}
}

// prettyInline returns true when the tag should be rendered without child
// indentation (inline elements and tags whose only child is a text node).
func prettyInline(tag *TagNode) bool {
	inline := map[string]bool{
		"a": true, "abbr": true, "acronym": true, "b": true, "bdo": true,
		"big": true, "br": true, "button": true, "cite": true, "code": true,
		"dfn": true, "em": true, "i": true, "img": true, "input": true,
		"kbd": true, "label": true, "map": true, "object": true, "output": true,
		"q": true, "samp": true, "select": true, "small": true, "span": true,
		"strong": true, "sub": true, "sup": true, "textarea": true, "time": true,
		"tt": true, "var": true,
	}
	if inline[tag.Name] {
		return true
	}
	// Single text-only child — keep on one line
	if len(tag.Children) == 1 {
		switch tag.Children[0].(type) {
		case *TextNode, *PipeNode, *BlockTextNode, *TextRunNode,
			*InterpolationNode, *CodeNode:
			return true
		}
	}
	return false
}

// Render renders the AST to a string.
func (r *Runtime) Render() (string, error) {
	r.scopeStack[0] = r.data

	r.collectMixins(r.ast.Children)

	if ext := r.findExtendsNode(r.ast.Children); ext != nil {
		return r.renderExtends(ext)
	}

	for _, node := range r.ast.Children {
		if err := r.renderNode(node); err != nil {
			return "", err
		}
	}
	return r.htmlBuf.String(), nil
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

func (r *Runtime) collectMixins(nodes []Node) {
	for _, node := range nodes {
		if m, ok := node.(*MixinDeclNode); ok {
			r.mixinDecls[m.Name] = m
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
	if len(r.includeStack) > 0 {
		currentPath = r.includeStack[len(r.includeStack)-1]
	} else if r.includeBase != "" {
		currentPath = filepath.Join(r.includeBase, "_root_.pug")
	}

	rootAST, mixins, err := r.resolveExtendsAST(currentPath, r.ast)
	if err != nil {
		return "", err
	}

	for k, v := range mixins {
		r.mixinDecls[k] = v
	}
	r.collectMixins(rootAST.Children)

	for _, node := range rootAST.Children {
		if err := r.renderNode(node); err != nil {
			return "", err
		}
	}
	return r.htmlBuf.String(), nil
}

// resolveExtendsAST resolves a chain of extends declarations and returns the
// fully-patched root DocumentNode along with all collected mixins. This is
// used to resolve multi-level inheritance chains without rendering.
func (r *Runtime) resolveExtendsAST(currentPath string, childAST *DocumentNode) (*DocumentNode, map[string]*MixinDeclNode, error) {
	mixins := make(map[string]*MixinDeclNode)

	if currentPath != "" && !strings.HasSuffix(currentPath, "_root_.pug") {
		absCurrentPath, err := filepath.Abs(currentPath)
		if err == nil {
			for _, p := range r.includeStack {
				if p == absCurrentPath {
					return nil, nil, fmt.Errorf("extends: cycle — %q", absCurrentPath)
				}
			}
			r.includeStack = append(r.includeStack, absCurrentPath)
			defer func() { r.includeStack = r.includeStack[:len(r.includeStack)-1] }()
		}
	}

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
		for _, node := range childAST.Children {
			if m, ok := node.(*MixinDeclNode); ok {
				mixins[m.Name] = m
			}
		}
		return childAST, mixins, nil
	}

	parentPath := ext.Path
	if len(parentPath) >= 2 &&
		((parentPath[0] == '"' && parentPath[len(parentPath)-1] == '"') ||
			(parentPath[0] == '\'' && parentPath[len(parentPath)-1] == '\'')) {
		parentPath = parentPath[1 : len(parentPath)-1]
	}

	base := filepath.Dir(currentPath)
	var resolved string
	if filepath.IsAbs(parentPath) {
		if r.includeBase != "" {
			resolved = filepath.Join(r.includeBase, parentPath)
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
	for _, p := range r.includeStack {
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

	rootAST, parentMixins, err := r.resolveExtendsAST(abs, parentAST)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range parentMixins {
		mixins[k] = v
	}

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
			r.applyBlockOverrides(n.Body, overrides)

		case *TagNode:
			r.applyBlockOverrides(n.Children, overrides)
		case *ConditionalNode:
			r.applyBlockOverrides(n.Consequent, overrides)
			r.applyBlockOverrides(n.Alternate, overrides)
		case *EachNode:
			r.applyBlockOverrides(n.Body, overrides)
			r.applyBlockOverrides(n.EmptyBody, overrides)
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
	case *TagInterpolationNode:
		return r.renderTagInterpolation(n)
	case *FilterNode:
		return r.renderFilter(n)
	case *MixinDeclNode:
		return nil
	case *MixinCallNode:
		return r.renderMixinCall(n)
	case *BlockNode:
		if r.inMixin {
			return r.renderMixinBlockSlot()
		}
		return r.renderBlockBody(n)
	case *ExtendsNode:
		return nil
	case *IncludeNode:
		return r.renderInclude(n)
	default:
		return fmt.Errorf("unknown node type: %T", node)
	}
}

func (r *Runtime) renderTagInterpolation(n *TagInterpolationNode) error {
	return r.renderTag(n.Tag)
}

func (r *Runtime) renderBlockBody(b *BlockNode) error {
	for _, node := range b.Body {
		if err := r.renderNode(node); err != nil {
			return err
		}
	}
	return nil
}

// writeNewlineAfterDoctype emits a newline after a doctype in pretty-print mode.
func (r *Runtime) writeNewlineAfterDoctype(nodes []Node) {
	if !r.pretty() {
		return
	}
	for _, n := range nodes {
		if _, ok := n.(*DoctypeNode); ok {
			r.htmlBuf.WriteByte('\n')
			return
		}
	}
}

// renderTag renders an HTML tag and its children.
func (r *Runtime) renderTag(tag *TagNode) error {
	if r.pretty() && !prettyInline(tag) {
		r.prettyNewline()
	}

	r.htmlBuf.WriteString("<")
	r.htmlBuf.WriteString(tag.Name)

	type attrEntry struct {
		name      string
		value     string
		unescaped bool
		boolean   bool
	}

	merged := make(map[string]*AttributeValue)
	for k, v := range tag.Attributes {
		if k != "&attributes" {
			merged[k] = v
		}
	}

	for k, v := range tag.Attributes {
		if k != "&attributes" {
			continue
		}

		expr := strings.TrimSpace(v.Value)

		spreadMap := map[string]interface{}{}

		if raw, ok := r.lookup(expr); ok && raw != nil {
			rv := reflect.ValueOf(raw)
			if rv.Kind() == reflect.Map {
				for _, mk := range rv.MapKeys() {
					spreadMap[fmt.Sprintf("%v", mk.Interface())] = rv.MapIndex(mk).Interface()
				}
			}
		} else if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
			obj := parseInlineObject(expr)
			for key, val := range obj {
				spreadMap[key] = val
			}
		}

		for attrKey, attrVal := range spreadMap {
			valStr := fmt.Sprintf("%v", attrVal)

			switch attrKey {
			case "class":
				if existing, ok := merged["class"]; ok {
					existingVal := strings.Trim(existing.Value, `"`)
					merged["class"] = &AttributeValue{Value: `"` + existingVal + " " + valStr + `"`}
				} else {
					merged["class"] = &AttributeValue{Value: `"` + valStr + `"`}
				}
			default:
				if valStr == "true" {
					merged[attrKey] = &AttributeValue{IsBare: true}
				} else if valStr == "false" {
					delete(merged, attrKey)
				} else {
					merged[attrKey] = &AttributeValue{Value: `"` + valStr + `"`}
				}
			}
		}
	}

	names := make([]string, 0, len(merged))
	for k := range merged {
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

	for _, name := range names {
		val := merged[name]

		evaluated := ""
		if val.Value != "" {
			rawValExpr := strings.TrimSpace(val.Value)

			if name == "style" && len(rawValExpr) >= 2 && rawValExpr[0] == '{' && rawValExpr[len(rawValExpr)-1] == '}' {
				obj := parseInlineObject(rawValExpr)
				if obj != nil {
					var parts []string
					for k, v := range obj {
						parts = append(parts, k+":"+v)
					}
					sort.Strings(parts)
					evaluated = strings.Join(parts, ";") + ";"
				}
			} else if name == "class" {
				if len(rawValExpr) >= 2 && rawValExpr[0] == '{' && rawValExpr[len(rawValExpr)-1] == '}' {
					obj := parseInlineObject(rawValExpr)
					if obj != nil {
						var activeClasses []string
						for k, v := range obj {
							evaled, _ := r.evaluateExpr(v)
							if r.isTruthy(evaled) {
								activeClasses = append(activeClasses, k)
							}
						}
						sort.Strings(activeClasses)
						evaluated = strings.Join(activeClasses, " ")
					}
				} else {
					if isOperatorExpr(rawValExpr) {
						evaluated, _ = r.evaluateExpr(rawValExpr)
					} else {
						raw := r.evaluateExprRaw(rawValExpr)
						if raw != nil {
							rv := reflect.ValueOf(raw)
							switch rv.Kind() {
							case reflect.Slice, reflect.Array:
								parts := make([]string, rv.Len())
								for i := 0; i < rv.Len(); i++ {
									parts[i] = fmt.Sprintf("%v", rv.Index(i).Interface())
								}
								evaluated = strings.Join(parts, " ")
							case reflect.Map:
								var activeClasses []string
								for _, mk := range rv.MapKeys() {
									mv := rv.MapIndex(mk).Interface()
									mvStr := fmt.Sprintf("%v", mv)
									if r.isTruthy(mvStr) {
										activeClasses = append(activeClasses, fmt.Sprintf("%v", mk.Interface()))
									}
								}
								sort.Strings(activeClasses)
								evaluated = strings.Join(activeClasses, " ")
							default:
								inner := rawValExpr
								if len(inner) >= 2 &&
									((inner[0] == '"' && inner[len(inner)-1] == '"') ||
										(inner[0] == '\'' && inner[len(inner)-1] == '\'')) {
									inner = inner[1 : len(inner)-1]
								}
								words := strings.Fields(inner)
								var resolved []string
								for _, word := range words {
									rawWord := r.evaluateExprRaw(word)
									if rawWord != nil {
										wv := reflect.ValueOf(rawWord)
										if wv.Kind() == reflect.Slice || wv.Kind() == reflect.Array {
											for i := 0; i < wv.Len(); i++ {
												resolved = append(resolved, fmt.Sprintf("%v", wv.Index(i).Interface()))
											}
											continue
										}
									}
									v, _ := r.evaluateExpr(word)
									if v != "" {
										resolved = append(resolved, v)
									} else if word != "" {
										resolved = append(resolved, word)
									}
								}
								if len(resolved) > 0 {
									evaluated = strings.Join(resolved, " ")
								} else {
									evaluated, _ = r.evaluateExpr(rawValExpr)
								}
							}
						} else {
							inner := rawValExpr
							if len(inner) >= 2 &&
								((inner[0] == '"' && inner[len(inner)-1] == '"') ||
									(inner[0] == '\'' && inner[len(inner)-1] == '\'')) {
								inner = inner[1 : len(inner)-1]
							}
							words := strings.Fields(inner)
							var resolved []string
							for _, word := range words {
								rawWord := r.evaluateExprRaw(word)
								if rawWord != nil {
									wv := reflect.ValueOf(rawWord)
									if wv.Kind() == reflect.Slice || wv.Kind() == reflect.Array {
										for i := 0; i < wv.Len(); i++ {
											resolved = append(resolved, fmt.Sprintf("%v", wv.Index(i).Interface()))
										}
										continue
									}
								}
								v, _ := r.evaluateExpr(word)
								if v != "" {
									resolved = append(resolved, v)
								} else if word != "" {
									resolved = append(resolved, word)
								}
							}
							if len(resolved) > 0 {
								evaluated = strings.Join(resolved, " ")
							} else {
								evaluated, _ = r.evaluateExpr(rawValExpr)
							}
						}
					}
				}
			} else {
				var err error
				evaluated, err = r.evaluateExpr(val.Value)
				if err != nil {
					evaluated = val.Value
				}
			}
		}

		if evaluated == "false" {
			rawVal := strings.TrimSpace(val.Value)
			isQuoted := len(rawVal) >= 2 &&
				((rawVal[0] == '"' && rawVal[len(rawVal)-1] == '"') ||
					(rawVal[0] == '\'' && rawVal[len(rawVal)-1] == '\''))
			if !isQuoted {
				continue
			}
		}

		r.htmlBuf.WriteString(" ")
		r.htmlBuf.WriteString(name)

		if !val.IsBare && val.Value != "" {
			if !val.Unescaped {
				evaluated = html.EscapeString(evaluated)
			}
			r.htmlBuf.WriteString("=")
			r.htmlBuf.WriteString("\"")
			r.htmlBuf.WriteString(evaluated)
			r.htmlBuf.WriteString("\"")
		}
	}

	if tag.SelfClose {
		r.htmlBuf.WriteString(" />")
		return nil
	}

	if isVoidElement(tag.Name) {
		r.htmlBuf.WriteString(">")
		return nil
	}

	r.htmlBuf.WriteString(">")

	isInline := prettyInline(tag)
	if r.pretty() && !isInline {
		r.prettyIndent++
	}

	for _, child := range tag.Children {
		if err := r.renderNode(child); err != nil {
			return err
		}
	}

	if r.pretty() && !isInline {
		r.prettyIndent--
		r.prettyNewline()
	}

	r.htmlBuf.WriteString("</")
	r.htmlBuf.WriteString(tag.Name)
	r.htmlBuf.WriteString(">")

	return nil
}

// htmlEscapeText escapes only the characters that must be escaped in HTML
// text content: <, >, and bare & (i.e. & not already part of a valid HTML
// entity reference like &copy; or &#169;).  Single and double quotes are
// left as-is because they are safe in text nodes.
func htmlEscapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '<':
			b.WriteString("&lt;")
			i++
		case '>':
			b.WriteString("&gt;")
			i++
		case '&':
			if end := entityEnd(s, i); end > i {
				b.WriteString(s[i:end])
				i = end
			} else {
				b.WriteString("&amp;")
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// entityEnd returns the index just past the closing ';' if s[start:] begins
// a valid HTML entity reference (&name; or &#digits; or &#xhex;).
// Returns start if no valid entity is found.
func entityEnd(s string, start int) int {
	if start >= len(s) || s[start] != '&' {
		return start
	}
	i := start + 1
	if i >= len(s) {
		return start
	}
	if s[i] == '#' {
		i++
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			i++ // hex
			start2 := i
			for i < len(s) && isHexDigit(s[i]) {
				i++
			}
			if i > start2 && i < len(s) && s[i] == ';' {
				return i + 1
			}
		} else {
			start2 := i
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i > start2 && i < len(s) && s[i] == ';' {
				return i + 1
			}
		}
		return start
	}
	if !isLetter(s[i]) {
		return start
	}
	for i < len(s) && isAlphaNum(s[i]) {
		i++
	}
	if i < len(s) && s[i] == ';' {
		return i + 1
	}
	return start
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func isAlphaNum(c byte) bool { return isLetter(c) || (c >= '0' && c <= '9') }

func (r *Runtime) renderText(text *TextNode) error {
	r.htmlBuf.WriteString(htmlEscapeText(text.Content))
	return nil
}

func (r *Runtime) renderInterpolation(interp *InterpolationNode) error {
	val, err := r.evaluateExpr(interp.Expression)
	if err != nil {
		return err
	}

	if !interp.Unescaped {
		val = html.EscapeString(val)
	}

	r.htmlBuf.WriteString(val)
	return nil
}

func (r *Runtime) renderComment(comment *CommentNode) error {
	if comment.Buffered {
		r.prettyNewline()
		r.htmlBuf.WriteString("<!-- ")
		r.htmlBuf.WriteString(comment.Content)
		r.htmlBuf.WriteString(" -->")
	}
	return nil
}

func (r *Runtime) renderCode(code *CodeNode) error {
	switch code.Type {
	case CodeUnbuffered:
		return r.executeStatement(code.Expression)
	case CodeBuffered:
		val, err := r.evaluateExpr(code.Expression)
		if err != nil {
			return err
		}
		r.htmlBuf.WriteString(html.EscapeString(val))
		return nil
	case CodeUnescaped:
		val, err := r.evaluateExpr(code.Expression)
		if err != nil {
			return err
		}
		r.htmlBuf.WriteString(val)
		return nil
	}
	return nil
}

// executeStatement executes an unbuffered code statement, handling assignment
// (var = expr), increment (var++), and decrement (var--).
// For anything else the expression is evaluated and the result discarded.
func (r *Runtime) executeStatement(stmt string) error {
	stmt = strings.TrimSpace(stmt)

	for _, kw := range []string{"var ", "let ", "const "} {
		if strings.HasPrefix(stmt, kw) {
			stmt = strings.TrimSpace(stmt[len(kw):])
			break
		}
	}

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

	if idx := findAssignOp(stmt); idx >= 0 {
		varName := strings.TrimSpace(stmt[:idx])
		rhsExpr := strings.TrimSpace(stmt[idx+1:])
		rhs := strings.TrimSpace(rhsExpr)
		if (len(rhs) >= 2 && rhs[0] == '{' && rhs[len(rhs)-1] == '}') ||
			(len(rhs) >= 2 && rhs[0] == '[' && rhs[len(rhs)-1] == ']') {
			rawVal := r.evaluateExprRaw(rhs)
			r.setVar(varName, rawVal)
			return nil
		}
		val, err := r.evaluateExpr(rhsExpr)
		if err != nil {
			return err
		}
		r.setVar(varName, val)
		return nil
	}

	_, err := r.evaluateExpr(stmt)
	return err
}

// setVar writes a variable, updating the innermost scope that already contains
// it, or creating it in the top scope if not found anywhere.
func (r *Runtime) setVar(name string, val interface{}) {
	for i := len(r.scopeStack) - 1; i >= 0; i-- {
		if r.scopeStack[i] == nil {
			continue
		}
		if _, exists := r.scopeStack[i][name]; exists {
			r.scopeStack[i][name] = val
			return
		}
	}
	top := len(r.scopeStack) - 1
	if r.scopeStack[top] == nil {
		r.scopeStack[top] = make(map[string]interface{})
	}
	r.scopeStack[top][name] = val
}

// findAssignOp finds the position of a simple = assignment operator that is
// not part of ==, !=, <=, >=.  Returns -1 if not found.
func findAssignOp(stmt string) int {
	for i := 0; i < len(stmt); i++ {
		if stmt[i] == '=' {
			if i > 0 {
				prev := stmt[i-1]
				if prev == '!' || prev == '<' || prev == '>' || prev == '=' {
					continue
				}
			}
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

func (r *Runtime) renderConditional(cond *ConditionalNode) error {
	condition := strings.TrimSpace(cond.Condition)
	if len(condition) >= 2 && condition[0] == '(' && condition[len(condition)-1] == ')' {
		condition = strings.TrimSpace(condition[1 : len(condition)-1])
	}

	val, err := r.evaluateExpr(condition)
	if err != nil {
		return err
	}

	boolVal := r.isTruthy(val)
	if cond.IsUnless {
		boolVal = !boolVal
	}

	if boolVal {
		for _, node := range cond.Consequent {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
	} else if len(cond.Alternate) > 0 {
		for _, node := range cond.Alternate {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
	}

	return nil
}

// renderEach renders each/for loops. The collection is looked up as a raw
// value first so slices and maps are iterable; method expressions like
// csv.split(",") fall back to evaluateExprRaw which returns []interface{}.
func (r *Runtime) renderEach(each *EachNode) error {
	collVal, ok := r.lookup(each.CollectionExpr)
	if !ok {
		collVal = r.evaluateExprRaw(each.CollectionExpr)
	}

	if collVal != nil {
		v := reflect.ValueOf(collVal)
		if v.Kind() == reflect.Map {
			if v.Len() == 0 {
				for _, node := range each.EmptyBody {
					if err := r.renderNode(node); err != nil {
						return err
					}
				}
				return nil
			}
			for _, mapKey := range v.MapKeys() {
				scope := make(map[string]interface{})
				scope[each.ItemVar] = v.MapIndex(mapKey).Interface()
				if each.IndexVar != "" {
					scope[each.IndexVar] = fmt.Sprintf("%v", mapKey.Interface())
				}
				r.scopeStack = append(r.scopeStack, scope)
				for _, node := range each.Body {
					if err := r.renderNode(node); err != nil {
						r.scopeStack = r.scopeStack[:len(r.scopeStack)-1]
						return err
					}
				}
				r.scopeStack = r.scopeStack[:len(r.scopeStack)-1]
			}
			return nil
		}
	}

	items := r.toSlice(collVal)

	if len(items) == 0 {
		for _, node := range each.EmptyBody {
			if err := r.renderNode(node); err != nil {
				return err
			}
		}
		return nil
	}

	for i, item := range items {
		scope := make(map[string]interface{})
		scope[each.ItemVar] = item
		if each.IndexVar != "" {
			scope[each.IndexVar] = strconv.Itoa(i)
		}
		r.scopeStack = append(r.scopeStack, scope)
		for _, node := range each.Body {
			if err := r.renderNode(node); err != nil {
				r.scopeStack = r.scopeStack[:len(r.scopeStack)-1]
				return err
			}
		}
		r.scopeStack = r.scopeStack[:len(r.scopeStack)-1]
	}

	return nil
}

// renderWhile renders while loops, capped at 10000 iterations to prevent
// runaway templates.
func (r *Runtime) renderWhile(w *WhileNode) error {
	const maxIterations = 10000
	iterations := 0

	for iterations < maxIterations {
		val, err := r.evaluateExpr(w.Condition)
		if err != nil {
			return err
		}
		if !r.isTruthy(val) {
			break
		}
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

// renderCase renders case/when/default. Pug fall-through rule: an empty when
// (no body) falls through to the next when/default; a non-empty when stops.
func (r *Runtime) renderCase(c *CaseNode) error {
	caseVal, err := r.evaluateExpr(c.Expression)
	if err != nil {
		return err
	}

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
				continue // fall through to the next clause
			}
			for _, node := range when.Body {
				if err := r.renderNode(node); err != nil {
					return err
				}
			}
			return nil
		}
	}

	if matched {
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

func (r *Runtime) renderDoctype(dt *DoctypeNode) error {
	doctype := r.formatDoctype(dt.Value)
	r.htmlBuf.WriteString(doctype)
	r.doctype = dt.Value
	if r.pretty() {
		r.htmlBuf.WriteByte('\n')
	}
	return nil
}

func (r *Runtime) renderPipe(pipe *PipeNode) error {
	r.prettyNewline()
	r.htmlBuf.WriteString(htmlEscapeText(pipe.Content))
	return nil
}

// renderBlockText renders dot-block text. Content may contain #{...}, !{...},
// and #[...] interpolations, which are processed by re-using the lexer's
// emitTextWithInterpolations helper rather than running the full parser.
func (r *Runtime) renderBlockText(block *BlockTextNode) error {
	r.prettyNewline()

	lx := NewLexer("")
	lx.emitTextWithInterpolations(block.Content, 0)

	for _, tok := range lx.tokens {
		switch tok.Type {
		case TokenText:
			r.htmlBuf.WriteString(htmlEscapeText(tok.Value))

		case TokenInterpolation:
			val, err := r.evaluateExpr(tok.Value)
			if err != nil {
				val = tok.Value
			}
			r.htmlBuf.WriteString(htmlEscapeText(val))

		case TokenInterpolationUnescape:
			val, err := r.evaluateExpr(tok.Value)
			if err != nil {
				val = tok.Value
			}
			r.htmlBuf.WriteString(val)

		case TokenTagInterpolationStart:
			innerLex := NewLexer(tok.Value)
			if _, err := innerLex.Lex(); err != nil {
				r.htmlBuf.WriteString(html.EscapeString(tok.Value))
				continue
			}
			innerParser := NewParser(innerLex.tokens)
			innerAST, err := innerParser.Parse()
			if err != nil || innerAST == nil || len(innerAST.Children) == 0 {
				r.htmlBuf.WriteString(html.EscapeString(tok.Value))
				continue
			}
			for _, node := range innerAST.Children {
				if err := r.renderNode(node); err != nil {
					return err
				}
			}

		case TokenTagInterpolationEnd:
		}
	}

	return nil
}

func (r *Runtime) renderLiteralHTML(lit *LiteralHTMLNode) error {
	r.prettyNewline()
	r.htmlBuf.WriteString(lit.Content)
	return nil
}

func (r *Runtime) renderBlockExpansion(exp *BlockExpansionNode) error {
	exp.Parent.Children = append(exp.Parent.Children, exp.Child)
	return r.renderTag(exp.Parent)
}

// renderInclude resolves and renders an include directive.
//
// Path resolution: absolute paths are anchored to includeBase; relative paths
// are resolved against the directory of the innermost active include, falling
// back to includeBase. .pug files (or no extension) are lexed, parsed, and
// rendered; all other files are written raw. Cycle detection is via includeStack.
func (r *Runtime) renderInclude(inc *IncludeNode) error {
	inclPath := inc.Path

	if len(inclPath) >= 2 &&
		((inclPath[0] == '"' && inclPath[len(inclPath)-1] == '"') ||
			(inclPath[0] == '\'' && inclPath[len(inclPath)-1] == '\'')) {
		inclPath = inclPath[1 : len(inclPath)-1]
	}

	var resolved string
	if filepath.IsAbs(inclPath) {
		if r.includeBase != "" {
			resolved = filepath.Join(r.includeBase, inclPath)
		} else {
			resolved = inclPath
		}
	} else {
		base := r.includeBase
		if len(r.includeStack) > 0 {
			base = filepath.Dir(r.includeStack[len(r.includeStack)-1])
		}
		if base == "" {
			base = "."
		}
		resolved = filepath.Join(base, inclPath)
	}

	abs, err := filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("include: cannot resolve path %q: %w", inclPath, err)
	}

	for _, p := range r.includeStack {
		if p == abs {
			return fmt.Errorf("include: cycle detected — %q includes itself", abs)
		}
	}

	if filepath.Ext(abs) == "" {
		abs += ".pug"
	}

	ext := strings.ToLower(filepath.Ext(abs))

	r.includeStack = append(r.includeStack, abs)
	defer func() { r.includeStack = r.includeStack[:len(r.includeStack)-1] }()

	if ext == ".pug" {
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

		r.collectMixins(ast.Children)

		for _, node := range ast.Children {
			if err := r.renderNode(node); err != nil {
				return fmt.Errorf("include: render error in %q: %w", abs, err)
			}
		}
		return nil
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("include: cannot read %q: %w", abs, err)
	}

	if inc.FilterName != "" {
		fn, ok := r.lookupFilter(inc.FilterName)
		if !ok {
			return fmt.Errorf("include: filter %q is not registered; register it via Options.Filters", inc.FilterName)
		}
		result, err := fn(string(raw), make(map[string]string))
		if err != nil {
			return fmt.Errorf("include: filter %q error on %q: %w", inc.FilterName, abs, err)
		}
		r.htmlBuf.WriteString(result)
		return nil
	}

	r.htmlBuf.Write(raw)
	return nil
}

func (r *Runtime) renderTextRun(run *TextRunNode) error {
	for _, node := range run.Nodes {
		if err := r.renderNode(node); err != nil {
			return err
		}
	}
	return nil
}

// renderMixinCall pushes a fresh scope containing evaluated arguments, renders
// the mixin body, then restores the previous scope and block slot.
func (r *Runtime) renderMixinCall(call *MixinCallNode) error {
	decl, ok := r.mixinDecls[call.Name]
	if !ok {
		return fmt.Errorf("mixin %q is not defined", call.Name)
	}

	scope := make(map[string]interface{})

	for i, param := range decl.Parameters {
		if i < len(call.Arguments) {
			val, err := r.evaluateExpr(call.Arguments[i])
			if err != nil {
				return err
			}
			scope[param] = val
		} else if decl.ParamDefaults != nil {
			if defaultExpr, ok := decl.ParamDefaults[param]; ok {
				val, err := r.evaluateExpr(defaultExpr)
				if err != nil {
					val = defaultExpr
				}
				scope[param] = val
			} else {
				scope[param] = "" // missing arg with no default
			}
		} else {
			scope[param] = "" // missing arg defaults to empty string
		}
	}

	if decl.RestParamName != "" {
		rest := make([]interface{}, 0)
		for i := len(decl.Parameters); i < len(call.Arguments); i++ {
			val, err := r.evaluateExpr(call.Arguments[i])
			if err != nil {
				return err
			}
			rest = append(rest, val)
		}
		scope[decl.RestParamName] = rest
	}

	attrMap := make(map[string]interface{})
	for k, v := range call.Attributes {
		var evaluated string
		if v.IsBare {
			evaluated = "true"
		} else {
			var err error
			evaluated, err = r.evaluateExpr(v.Value)
			if err != nil {
				evaluated = v.Value
			}
		}
		attrMap[k] = evaluated
	}
	scope["attributes"] = attrMap

	prevBlock := r.callerBlock
	r.callerBlock = call.BlockContent

	r.scopeStack = append(r.scopeStack, scope)
	prevInMixin := r.inMixin
	r.inMixin = true
	for _, node := range decl.Body {
		if err := r.renderNode(node); err != nil {
			r.scopeStack = r.scopeStack[:len(r.scopeStack)-1]
			r.inMixin = prevInMixin
			r.callerBlock = prevBlock
			return err
		}
	}
	r.scopeStack = r.scopeStack[:len(r.scopeStack)-1]
	r.inMixin = prevInMixin
	r.callerBlock = prevBlock
	return nil
}

// renderMixinBlockSlot renders the block content supplied by the caller.
// Renders nothing when the caller provided no block.
func (r *Runtime) renderMixinBlockSlot() error {
	for _, node := range r.callerBlock {
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

	options := filter.Options
	if options == nil {
		options = make(map[string]string)
	}

	var chain []string
	if filter.Subfilter != "" {
		parts := strings.Split(filter.Subfilter, ":")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				chain = append(chain, parts[i])
			}
		}
	}
	chain = append(chain, filter.Name)

	for i, name := range chain {
		fn, ok := r.lookupFilter(name)
		if !ok {
			return fmt.Errorf("filter %q is not registered; register it via Options.Filters", name)
		}
		stepOpts := make(map[string]string)
		if i == len(chain)-1 {
			stepOpts = options
		}
		result, err := fn(content, stepOpts)
		if err != nil {
			return fmt.Errorf("filter %q error: %w", name, err)
		}
		content = result
	}

	content = strings.TrimRight(content, "\n")
	if strings.Contains(content, "\n") {
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 {
				r.htmlBuf.WriteString("<br>")
			}
			r.htmlBuf.WriteString(line)
		}
	} else {
		r.htmlBuf.WriteString(content)
	}
	return nil
}

func (r *Runtime) lookupFilter(name string) (FilterFunc, bool) {
	if r.opts != nil && r.opts.Filters != nil {
		if fn, ok := r.opts.Filters[name]; ok {
			return fn, true
		}
	}
	return nil, false
}

// evaluateExprRaw evaluates an expression and returns a raw interface{} value
// rather than a string. Used when the caller needs a real Go slice or map
// (e.g. the collection in an each loop). Special-cased for split, inline
// object literals, and inline array literals; falls back to evaluateExpr.
func (r *Runtime) evaluateExprRaw(expr string) interface{} {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}

	if dotIdx := findTopLevelDot(expr); dotIdx > 0 {
		objExpr := expr[:dotIdx]
		rest := expr[dotIdx+1:]
		methodName := rest
		argsStr := ""
		if parenIdx := strings.Index(rest, "("); parenIdx >= 0 {
			methodName = rest[:parenIdx]
			inner := rest[parenIdx+1:]
			if closeIdx := strings.LastIndex(inner, ")"); closeIdx >= 0 {
				argsStr = strings.TrimSpace(inner[:closeIdx])
			}
		}
		methodName = strings.TrimSpace(methodName)

		if methodName == "split" {
			objStr, _ := r.evaluateExpr(objExpr)
			sep := ""
			if argsStr != "" {
				sep, _ = r.evaluateExpr(argsStr)
				if len(sep) >= 2 &&
					((sep[0] == '"' && sep[len(sep)-1] == '"') ||
						(sep[0] == '\'' && sep[len(sep)-1] == '\'')) {
					sep = sep[1 : len(sep)-1]
				}
			}
			parts := strings.Split(objStr, sep)
			result := make([]interface{}, len(parts))
			for i, p := range parts {
				result[i] = p
			}
			return result
		}
	}

	if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
		obj := parseInlineObject(expr)
		if obj != nil {
			result := make(map[string]interface{}, len(obj))
			for k, v := range obj {
				result[k] = v
			}
			return result
		}
	}

	if len(expr) >= 2 && expr[0] == '[' && expr[len(expr)-1] == ']' {
		inner := strings.TrimSpace(expr[1 : len(expr)-1])
		if inner == "" {
			return []interface{}{}
		}
		parts := splitTopLevel(inner, ',')
		result := make([]interface{}, 0, len(parts))
		for _, p := range parts {
			v, _ := r.evaluateExpr(strings.TrimSpace(p))
			result = append(result, v)
		}
		return result
	}

	if val, ok := r.lookup(expr); ok {
		return val
	}

	s, _ := r.evaluateExpr(expr)
	return s
}

// evaluateExpr evaluates an expression string against the current scope and
// returns a string result. Operator precedence (low to high): ternary,
// logical OR/AND, comparison, logical NOT, arithmetic, index/dot access.
func (r *Runtime) evaluateExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)

	if expr == "" {
		return "", nil
	}

	if len(expr) >= 2 && expr[0] == '(' && expr[len(expr)-1] == ')' {
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
		if isWrapped {
			expr = strings.TrimSpace(expr[1 : len(expr)-1])
		}
	}

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

	if len(expr) >= 2 {
		q := rune(expr[0])
		if q == '"' || q == '\'' {
			escaped := false
			closeIdx := -1
			for byteIdx, ch := range expr[1:] {
				realIdx := byteIdx + 1 // offset back into expr
				if escaped {
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == q {
					closeIdx = realIdx
					break
				}
			}
			if closeIdx == len(expr)-1 {
				return expr[1 : len(expr)-1], nil
			}
		}
	}

	if expr == "block" && r.inMixin {
		if len(r.callerBlock) > 0 {
			return "true", nil
		}
		return "false", nil
	}

	if len(expr) >= 2 && expr[0] == '[' && expr[len(expr)-1] == ']' {
		inner := expr[1 : len(expr)-1]
		parts := splitTopLevel(inner, ',')
		strs := make([]string, 0, len(parts))
		for _, p := range parts {
			v, _ := r.evaluateExpr(strings.TrimSpace(p))
			strs = append(strs, v)
		}
		return strings.Join(strs, ","), nil
	}

	if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
		return "", nil
	}

	if _, err := strconv.ParseFloat(expr, 64); err == nil {
		return expr, nil
	}

	switch expr {
	case "true":
		return "true", nil
	case "false":
		return "false", nil
	case "null", "undefined", "nil":
		return "", nil
	}

	if idx := findSubtraction(expr); idx >= 0 {
		left, err := r.evaluateExpr(strings.TrimSpace(expr[:idx]))
		if err != nil {
			return "", err
		}
		right, err := r.evaluateExpr(strings.TrimSpace(expr[idx+1:]))
		if err != nil {
			return "", err
		}
		lf, lok := toFloat(left)
		rf, rok := toFloat(right)
		if lok && rok {
			return strconv.FormatFloat(lf-rf, 'f', -1, 64), nil
		}
		return "", nil
	}

	if idx := findBinaryOp(expr, "+"); idx >= 0 {
		left, err := r.evaluateExpr(strings.TrimSpace(expr[:idx]))
		if err != nil {
			return "", err
		}
		right, err := r.evaluateExpr(strings.TrimSpace(expr[idx+1:]))
		if err != nil {
			return "", err
		}
		lf, lok := toFloat(left)
		rf, rok := toFloat(right)
		if lok && rok {
			result := lf + rf
			return strconv.FormatFloat(result, 'f', -1, 64), nil
		}
		return left + right, nil
	}

	if idx := findRightmostOp(expr, '*'); idx >= 0 {
		left, err := r.evaluateExpr(strings.TrimSpace(expr[:idx]))
		if err != nil {
			return "", err
		}
		right, err := r.evaluateExpr(strings.TrimSpace(expr[idx+1:]))
		if err != nil {
			return "", err
		}
		lf, lok := toFloat(left)
		rf, rok := toFloat(right)
		if lok && rok {
			return strconv.FormatFloat(lf*rf, 'f', -1, 64), nil
		}
		return "", nil
	}

	if idx := findRightmostOp(expr, '/'); idx >= 0 {
		left, err := r.evaluateExpr(strings.TrimSpace(expr[:idx]))
		if err != nil {
			return "", err
		}
		right, err := r.evaluateExpr(strings.TrimSpace(expr[idx+1:]))
		if err != nil {
			return "", err
		}
		lf, lok := toFloat(left)
		rf, rok := toFloat(right)
		if lok && rok {
			if rf == 0 {
				return "", fmt.Errorf("division by zero")
			}
			return strconv.FormatFloat(lf/rf, 'f', -1, 64), nil
		}
		return "", nil
	}

	if idx := findRightmostOp(expr, '%'); idx >= 0 {
		left, err := r.evaluateExpr(strings.TrimSpace(expr[:idx]))
		if err != nil {
			return "", err
		}
		right, err := r.evaluateExpr(strings.TrimSpace(expr[idx+1:]))
		if err != nil {
			return "", err
		}
		lf, lok := toFloat(left)
		rf, rok := toFloat(right)
		if lok && rok {
			if rf == 0 {
				return "", fmt.Errorf("modulo by zero")
			}
			return strconv.FormatFloat(float64(int64(lf)%int64(rf)), 'f', -1, 64), nil
		}
		return "", nil
	}

	if idx := findIndexOp(expr); idx >= 0 {
		objExpr := expr[:idx]
		keyExpr := expr[idx+1 : len(expr)-1]
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

	if dotIdx := findTopLevelDot(expr); dotIdx > 0 {
		objExpr := expr[:dotIdx]
		rest := expr[dotIdx+1:] // everything after the dot

		objVal, err := r.evaluateExpr(objExpr)
		if err != nil {
			return "", err
		}

		methodName := rest
		argsStr := ""
		if parenIdx := strings.Index(rest, "("); parenIdx >= 0 {
			methodName = rest[:parenIdx]
			inner := rest[parenIdx+1:]
			if closeIdx := strings.LastIndex(inner, ")"); closeIdx >= 0 {
				argsStr = strings.TrimSpace(inner[:closeIdx])
			}
		}
		methodName = strings.TrimSpace(methodName)

		switch methodName {
		case "length":
			if rawObj, ok2 := r.lookup(objExpr); ok2 {
				rv := reflect.ValueOf(rawObj)
				if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
					return strconv.Itoa(rv.Len()), nil
				}
				if rv.Kind() == reflect.Map {
					return strconv.Itoa(rv.Len()), nil
				}
			}
			return strconv.Itoa(len([]rune(objVal))), nil

		case "toUpperCase", "toUppercase":
			return strings.ToUpper(objVal), nil

		case "toLowerCase", "toLowercase":
			return strings.ToLower(objVal), nil

		case "trim":
			return strings.TrimSpace(objVal), nil

		case "trimLeft", "trimStart":
			return strings.TrimLeft(objVal, " \t\n\r"), nil

		case "trimRight", "trimEnd":
			return strings.TrimRight(objVal, " \t\n\r"), nil

		case "repeat":
			if argsStr != "" {
				n, err2 := r.evaluateExpr(argsStr)
				if err2 == nil {
					if count, err3 := strconv.Atoi(n); err3 == nil && count >= 0 {
						return strings.Repeat(objVal, count), nil
					}
				}
			}
			return objVal, nil

		case "split":
			sep := ""
			if argsStr != "" {
				sep, _ = r.evaluateExpr(argsStr)
				if len(sep) >= 2 &&
					((sep[0] == '"' && sep[len(sep)-1] == '"') ||
						(sep[0] == '\'' && sep[len(sep)-1] == '\'')) {
					sep = sep[1 : len(sep)-1]
				}
			}
			parts := strings.Split(objVal, sep)
			return strings.Join(parts, " "), nil

		case "join":
			sep := ""
			if argsStr != "" {
				sep, _ = r.evaluateExpr(argsStr)
				if len(sep) >= 2 &&
					((sep[0] == '"' && sep[len(sep)-1] == '"') ||
						(sep[0] == '\'' && sep[len(sep)-1] == '\'')) {
					sep = sep[1 : len(sep)-1]
				}
			}
			if rawObj := r.evaluateExprRaw(objExpr); rawObj != nil {
				rv := reflect.ValueOf(rawObj)
				if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
					parts := make([]string, rv.Len())
					for i := 0; i < rv.Len(); i++ {
						parts[i] = fmt.Sprintf("%v", rv.Index(i).Interface())
					}
					return strings.Join(parts, sep), nil
				}
			}
			return objVal, nil

		case "replace":
			if argsStr != "" {
				commaIdx := findBinaryOp(argsStr, ",")
				if commaIdx > 0 {
					oldArg, _ := r.evaluateExpr(strings.TrimSpace(argsStr[:commaIdx]))
					newArg, _ := r.evaluateExpr(strings.TrimSpace(argsStr[commaIdx+1:]))
					for _, s := range []string{oldArg, newArg} {
						if len(s) >= 2 &&
							((s[0] == '"' && s[len(s)-1] == '"') ||
								(s[0] == '\'' && s[len(s)-1] == '\'')) {
							if s == oldArg {
								oldArg = s[1 : len(s)-1]
							} else {
								newArg = s[1 : len(s)-1]
							}
						}
					}
					return strings.Replace(objVal, oldArg, newArg, 1), nil
				}
			}
			return objVal, nil

		case "slice":
			runes := []rune(objVal)
			if argsStr != "" {
				commaIdx := findBinaryOp(argsStr, ",")
				if commaIdx > 0 {
					startStr, _ := r.evaluateExpr(strings.TrimSpace(argsStr[:commaIdx]))
					endStr, _ := r.evaluateExpr(strings.TrimSpace(argsStr[commaIdx+1:]))
					start, _ := strconv.Atoi(startStr)
					end, _ := strconv.Atoi(endStr)
					if start < 0 {
						start = len(runes) + start
					}
					if end < 0 {
						end = len(runes) + end
					}
					if start < 0 {
						start = 0
					}
					if end > len(runes) {
						end = len(runes)
					}
					if start <= end {
						return string(runes[start:end]), nil
					}
					return "", nil
				}
				startStr, _ := r.evaluateExpr(argsStr)
				start, _ := strconv.Atoi(startStr)
				if start < 0 {
					start = len(runes) + start
				}
				if start < 0 {
					start = 0
				}
				if start <= len(runes) {
					return string(runes[start:]), nil
				}
				return "", nil
			}
			return objVal, nil

		case "indexOf":
			if argsStr != "" {
				needle, _ := r.evaluateExpr(argsStr)
				if len(needle) >= 2 &&
					((needle[0] == '"' && needle[len(needle)-1] == '"') ||
						(needle[0] == '\'' && needle[len(needle)-1] == '\'')) {
					needle = needle[1 : len(needle)-1]
				}
				return strconv.Itoa(strings.Index(objVal, needle)), nil
			}
			return "-1", nil

		case "includes", "contains":
			if argsStr != "" {
				needle, _ := r.evaluateExpr(argsStr)
				if len(needle) >= 2 &&
					((needle[0] == '"' && needle[len(needle)-1] == '"') ||
						(needle[0] == '\'' && needle[len(needle)-1] == '\'')) {
					needle = needle[1 : len(needle)-1]
				}
				if strings.Contains(objVal, needle) {
					return "true", nil
				}
				return "false", nil
			}
			return "false", nil

		case "startsWith":
			if argsStr != "" {
				prefix, _ := r.evaluateExpr(argsStr)
				if len(prefix) >= 2 &&
					((prefix[0] == '"' && prefix[len(prefix)-1] == '"') ||
						(prefix[0] == '\'' && prefix[len(prefix)-1] == '\'')) {
					prefix = prefix[1 : len(prefix)-1]
				}
				if strings.HasPrefix(objVal, prefix) {
					return "true", nil
				}
				return "false", nil
			}
			return "false", nil

		case "endsWith":
			if argsStr != "" {
				suffix, _ := r.evaluateExpr(argsStr)
				if len(suffix) >= 2 &&
					((suffix[0] == '"' && suffix[len(suffix)-1] == '"') ||
						(suffix[0] == '\'' && suffix[len(suffix)-1] == '\'')) {
					suffix = suffix[1 : len(suffix)-1]
				}
				if strings.HasSuffix(objVal, suffix) {
					return "true", nil
				}
				return "false", nil
			}
			return "false", nil

		case "toString", "String":
			return objVal, nil
		}
	}

	if val, ok := r.lookup(expr); ok {
		if val == nil {
			return "", nil
		}
		if f, ok := val.(float64); ok {
			return strconv.FormatFloat(f, 'f', -1, 64), nil
		}
		return fmt.Sprintf("%v", val), nil
	}

	return "", nil
}

// isOperatorExpr reports whether expr contains a top-level operator (ternary,
// logical, comparison, or concatenation) that requires it to be evaluated as
// a whole rather than split on spaces.
func isOperatorExpr(expr string) bool {
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
		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '?', '+':
			if depth == 0 {
				return true
			}
		case '|':
			if depth == 0 && i+1 < len(expr) && expr[i+1] == '|' {
				return true
			}
		case '&':
			if depth == 0 && i+1 < len(expr) && expr[i+1] == '&' {
				return true
			}
		case '=':
			if depth == 0 && i+1 < len(expr) && expr[i+1] == '=' {
				return true
			}
		case '!':
			if depth == 0 && i+1 < len(expr) && expr[i+1] == '=' {
				return true
			}
		case '<', '>':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// findSubtraction finds the rightmost top-level '-' that is binary (not unary).
// A '-' is unary when preceded by another operator character (+,-,*,/,%,(,[,,).
func findSubtraction(expr string) int {
	depth := 0
	inDouble := false
	inSingle := false
	result := -1
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
			continue
		}
		if ch == ')' || ch == ']' || ch == '}' {
			depth--
			continue
		}
		if depth == 0 && ch == '-' {
			prev := byte(0)
			for j := i - 1; j >= 0; j-- {
				if expr[j] != ' ' && expr[j] != '\t' {
					prev = expr[j]
					break
				}
			}
			isBinary := prev != 0 &&
				prev != '+' && prev != '-' && prev != '*' &&
				prev != '/' && prev != '%' && prev != '(' &&
				prev != '[' && prev != ','
			if isBinary {
				result = i // keep going to find rightmost
			}
		}
	}
	return result
}

// findRightmostOp finds the rightmost top-level occurrence of a single-byte
// operator (*, /, %) outside quotes and brackets.
func findRightmostOp(expr string, op byte) int {
	depth := 0
	inDouble := false
	inSingle := false
	result := -1
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
			continue
		}
		if ch == ')' || ch == ']' || ch == '}' {
			depth--
			continue
		}
		if depth == 0 && ch == op {
			result = i // keep going to find rightmost
		}
	}
	return result
}

// findTernary returns the index of the top-level ? operator, or -1.
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

// findIndexOp finds a top-level [...] index at the end of an expression
// (e.g. arr[0]). Returns the position of the opening [, or -1.
func findIndexOp(expr string) int {
	if len(expr) == 0 || expr[len(expr)-1] != ']' {
		return -1
	}
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

func (r *Runtime) indexValue(obj interface{}, key string) interface{} {
	v := reflect.ValueOf(obj)
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
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

// findBinaryOp returns the position of op at the top level of expr (outside
// quotes and balanced brackets), or -1 if not found.
func findBinaryOp(expr, op string) int {
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
			continue
		}
		if ch == ')' || ch == ']' || ch == '}' {
			depth--
			continue
		}
		if depth == 0 && i+len(op) <= len(expr) && expr[i:i+len(op)] == op {
			return i
		}
	}
	return -1
}

// findTopLevelDot returns the index of the last top-level dot in expr, used
// for method/property access. Dots inside quotes, brackets, and numeric
// literals (e.g. 3.14) are skipped. Returns -1 if not found.
func findTopLevelDot(expr string) int {
	depth := 0
	inDouble := false
	inSingle := false
	result := -1
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
			continue
		}
		if ch == ')' || ch == ']' || ch == '}' {
			depth--
			continue
		}
		if ch == '.' && depth == 0 {
			prevIsDigit := i > 0 && expr[i-1] >= '0' && expr[i-1] <= '9'
			nextIsDigit := i+1 < len(expr) && expr[i+1] >= '0' && expr[i+1] <= '9'
			if prevIsDigit && nextIsDigit {
				continue
			}
			result = i
		}
	}
	return result
}

// compareValues compares left and right with op. Numeric comparison is used
// when both sides parse as float64; otherwise string comparison is used.
func (r *Runtime) compareValues(left, right, op string) bool {
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

func parseNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}

// lookup retrieves a value by name from the scope stack (innermost first),
// falling back to globals. Dot notation ("user.name") is resolved by walking
// the chain with getField.
func (r *Runtime) lookup(key string) (interface{}, bool) {
	parts := strings.Split(key, ".")
	root := strings.TrimSpace(parts[0])

	var rootVal interface{}
	found := false
	for i := len(r.scopeStack) - 1; i >= 0; i-- {
		if r.scopeStack[i] == nil {
			continue
		}
		if val, ok := r.scopeStack[i][root]; ok {
			rootVal = val
			found = true
			break
		}
	}

	if !found {
		if val, ok := r.globals[root]; ok {
			rootVal = val
			found = true
		}
	}

	if !found {
		return nil, false
	}

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

// splitTopLevel splits s on sep at depth 0 (outside quotes and brackets).
func splitTopLevel(s string, sep rune) []string {
	var parts []string
	depth := 0
	inDouble := false
	inSingle := false
	start := 0
	for i, ch := range s {
		switch {
		case ch == '\\' && (inDouble || inSingle):
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case (ch == '(' || ch == '[' || ch == '{') && !inDouble && !inSingle:
			depth++
		case (ch == ')' || ch == ']' || ch == '}') && !inDouble && !inSingle:
			depth--
		case ch == sep && depth == 0 && !inDouble && !inSingle:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseInlineObject parses a JS-style object literal string like
// {color: "red", background: "green"} into a map[string]string.
// Keys and values are trimmed; string values have their quotes stripped.
func parseInlineObject(s string) map[string]string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil
	}
	inner := s[1 : len(s)-1]
	result := make(map[string]string)
	for _, pair := range splitTopLevel(inner, ',') {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		colonIdx := strings.Index(pair, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(pair[:colonIdx])
		val := strings.TrimSpace(pair[colonIdx+1:])
		if len(key) >= 2 &&
			((key[0] == '"' && key[len(key)-1] == '"') ||
				(key[0] == '\'' && key[len(key)-1] == '\'')) {
			key = key[1 : len(key)-1]
		}
		if len(val) >= 2 &&
			((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result
}

func (r *Runtime) isTruthy(val string) bool {
	switch val {
	case "", "false", "0", "null", "undefined", "nil":
		return false
	}
	return true
}

func (r *Runtime) formatDoctype(dt string) string {
	switch strings.ToLower(dt) {
	case "", "html", "5", "doctype":
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

// isVoidElement reports whether tag is an HTML void element (no closing tag).
func isVoidElement(tag string) bool {
	switch strings.ToLower(tag) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}
