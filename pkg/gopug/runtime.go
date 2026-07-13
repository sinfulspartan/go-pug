package gopug

import (
	"bytes"
	"fmt"
	"html"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// mixinScopeBoundary is a sentinel frame pushed onto scopeStack immediately
// before a mixin's own parameter frame. lookup stops descending when it finds
// this frame, enforcing hard scope isolation: mixin bodies cannot read
// variables from the caller's scope. The sentinel key uses a null byte prefix
// that cannot appear in any valid Pug identifier.
var mixinScopeBoundary = map[string]any{"\x00mixin_boundary": true}

type Runtime struct {
	ast              *DocumentNode
	data             map[string]any
	globals          map[string]any
	htmlBuf          *bytes.Buffer
	scopeStack       []map[string]any
	doctype          string
	mixinDecls       map[string]*MixinDeclNode
	callerBlock      []Node
	inMixin          bool
	inRawTextElement bool
	opts             *Options
	includeBase      string
	includeStack     []string
	entryFile        string
	prettyIndent     int
}

func NewRuntime(ast *DocumentNode, data map[string]any) *Runtime {
	return NewRuntimeWithOptions(ast, data, nil)
}

func NewRuntimeWithOptions(ast *DocumentNode, data map[string]any, opts *Options) *Runtime {
	r := &Runtime{
		ast:          ast,
		data:         data,
		globals:      make(map[string]any),
		htmlBuf:      &bytes.Buffer{},
		scopeStack:   make([]map[string]any, 1),
		doctype:      "html",
		mixinDecls:   make(map[string]*MixinDeclNode),
		opts:         opts,
		includeStack: make([]string, 0),
	}
	if opts != nil && opts.Basedir != "" {
		r.includeBase = opts.Basedir
	}
	if opts != nil && opts.entryFile != "" {
		r.entryFile = opts.entryFile
	}
	return r
}

func (r *Runtime) pretty() bool {
	return r.opts != nil && r.opts.Pretty
}

func (r *Runtime) prettyNewline() {
	if !r.pretty() {
		return
	}
	r.htmlBuf.WriteByte('\n')
	for i := 0; i < r.prettyIndent; i++ {
		r.htmlBuf.WriteString("  ")
	}
}

// inlineTagNames is the set of HTML tag names treated as inline for pretty
// printing (rendered without child indentation). Built once at package init
// so prettyInline doesn't allocate a fresh map on every call.
var inlineTagNames = map[string]bool{
	"a": true, "abbr": true, "acronym": true, "b": true, "bdo": true,
	"big": true, "br": true, "button": true, "cite": true, "code": true,
	"dfn": true, "em": true, "i": true, "img": true, "input": true,
	"kbd": true, "label": true, "map": true, "object": true, "output": true,
	"q": true, "samp": true, "select": true, "small": true, "span": true,
	"strong": true, "sub": true, "sup": true, "textarea": true, "time": true,
	"tt": true, "var": true,
}

// prettyInline returns true when the tag should be rendered without child
// indentation (inline elements and tags whose only child is a text node).
func prettyInline(tag *TagNode) bool {
	if inlineTagNames[tag.Name] {
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

func (r *Runtime) Render() (string, error) {
	r.scopeStack[0] = r.data

	r.collectMixins(r.ast.Children)

	if r.findExtendsNode(r.ast.Children) != nil {
		return r.renderExtends()
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
func (r *Runtime) renderExtends() (string, error) {
	// We need to know the "current file path" for relative resolution.
	// If we are inside an include stack, the top of the stack is the current
	// file; otherwise we use basedir as a hint (we create a synthetic path).
	currentPath := ""
	if len(r.includeStack) > 0 {
		currentPath = r.includeStack[len(r.includeStack)-1]
	} else if r.entryFile != "" {
		// Top-level file render: extends resolves relative to the entry file's
		// own directory (standard Pug), matching relative includes.
		currentPath = r.entryFile
	} else if r.includeBase != "" {
		// String render (no entry file): fall back to Basedir-relative via a
		// synthetic root path anchored at Basedir.
		currentPath = filepath.Join(r.includeBase, "_root_.pug")
	}

	rootAST, mixins, err := r.resolveExtendsAST(currentPath, r.ast)
	if err != nil {
		return "", err
	}

	maps.Copy(r.mixinDecls, mixins)
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
			if slices.Contains(r.includeStack, absCurrentPath) {
				return nil, nil, fmt.Errorf("extends: cycle — %q", absCurrentPath)
			}
			r.includeStack = append(r.includeStack, absCurrentPath)
			defer func() { r.includeStack = r.includeStack[:len(r.includeStack)-1] }()
		}
	}

	var ext *ExtendsNode
	for _, node := range childAST.Children {
		switch n := node.(type) {
		case *CommentNode:
			continue
		case *MixinDeclNode:
			mixins[n.Name] = n
			continue
		case *ExtendsNode:
			ext = n
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
	if strings.HasPrefix(parentPath, "/") || strings.HasPrefix(parentPath, "\\") {
		// Treat leading slash as basedir-relative on all OSes (on Windows
		// filepath.IsAbs("/foo") is false, so we handle this case first).
		if r.includeBase != "" {
			resolved = filepath.Join(r.includeBase, parentPath)
		} else {
			resolved = parentPath
		}
	} else if filepath.IsAbs(parentPath) {
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
	if slices.Contains(r.includeStack, abs) {
		return nil, nil, fmt.Errorf("extends: cycle — %q", abs)
	}
	if filepath.Ext(abs) == "" {
		abs += ".pug"
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("extends: cannot read %q: %w%s", abs, err, r.basedirResolveHint(parentPath))
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
	maps.Copy(mixins, parentMixins)

	childBlocks := r.collectBlocks(childAST.Children)
	for _, node := range childAST.Children {
		if m, ok := node.(*MixinDeclNode); ok {
			mixins[m.Name] = m
		}
	}
	r.applyBlockOverrides(rootAST.Children, childBlocks)

	return rootAST, mixins, nil
}

// collectBlocks returns a map of block name → []*BlockNode for all named
// blocks found at the top level of the given node list (child template
// overrides). Multiple overrides for the same block name (e.g. one
// block prepend and one block append) are preserved in declaration order.
func (r *Runtime) collectBlocks(nodes []Node) map[string][]*BlockNode {
	blocks := make(map[string][]*BlockNode)
	for _, node := range nodes {
		if b, ok := node.(*BlockNode); ok {
			blocks[b.Name] = append(blocks[b.Name], b)
		}
	}
	return blocks
}

// applyBlockOverrides recursively walks a node slice (the parent/root AST) and
// replaces, appends to, or prepends each BlockNode whose name appears in the
// overrides map. The walk is deep so blocks nested inside tags, conditionals,
// etc. are also patched.
//
// Multiple overrides for the same block name are applied in declaration order.
// A replace override resets the body; subsequent append/prepend overrides then
// operate on that new body. This means a child can legitimately write both
// "block prepend foo" and "block append foo" and get [prepend, parent, append].
func (r *Runtime) applyBlockOverrides(nodes []Node, overrides map[string][]*BlockNode) {
	for i, node := range nodes {
		switch n := node.(type) {
		case *BlockNode:
			overrideList, ok := overrides[n.Name]
			if !ok {
				r.applyBlockOverrides(n.Body, overrides)
				continue
			}
			for _, override := range overrideList {
				switch override.Mode {
				case BlockModeAppend:
					n.Body = append(n.Body, override.Body...)
				case BlockModePrepend:
					n.Body = append(override.Body, n.Body...)
				default: // BlockModeReplace
					n.Body = override.Body
				}
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

		spreadMap := map[string]any{}

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
				switch valStr {
				case "true":
					merged[attrKey] = &AttributeValue{IsBare: true}
				case "false":
					delete(merged, attrKey)
				default:
					merged[attrKey] = &AttributeValue{Value: `"` + valStr + `"`}
				}
			}
		}
	}

	names := sortAttrNames(merged)

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
				classObjStart := -1
				for i := 0; i < len(rawValExpr); i++ {
					if rawValExpr[i] == '{' {
						classObjStart = i
						break
					}
				}
				if classObjStart >= 0 && classObjStart < len(rawValExpr)-1 && rawValExpr[len(rawValExpr)-1] == '}' {
					objStr := rawValExpr[classObjStart:]
					obj := parseInlineObject(objStr)
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
						if classObjStart > 0 {
							prefix := strings.TrimSpace(rawValExpr[:classObjStart])
							if prefix != "" {
								evaluated = prefix + " " + evaluated
							}
						}
					}
				} else {
					if isOperatorExpr(rawValExpr) {
						evaluated, _ = r.evaluateExpr(rawValExpr)
						if evaluated == "" {
							words := strings.Fields(rawValExpr)
							if len(words) > 1 {
								exprStart := -1
								// First, try to find an expression word that
								// starts with a known expression prefix.
								for i, word := range words {
									if len(word) > 0 && (word[0] == '!' || word[0] == '(' || word[0] == '[' || word[0] == '{') {
										exprStart = i
										break
									}
								}
								// If no prefix-based match, look for a ternary
								// `?` in the words and treat the word before it
								// as the start of the expression (it is the
								// condition).  E.g. for
								//   card isActive ? "active" : ""
								// words = [card, isActive, ?, "active", :, ""]
								// The expression starts at index 1 (isActive).
								if exprStart < 0 {
									for i, word := range words {
										if word == "?" {
											if i > 0 {
												exprStart = i - 1
											} else {
												exprStart = 0
											}
											break
										}
									}
								}
								if exprStart > 0 {
									staticWords := words[:exprStart]
									exprWords := words[exprStart:]
									if len(exprWords) > 0 {
										exprStr := strings.Join(exprWords, " ")
										evaled, _ := r.evaluateExpr(exprStr)
										staticPart := strings.Join(staticWords, " ")
										if evaled != "" {
											evaluated = staticPart + " " + evaled
										} else {
											// Expression evaluated to empty
											// (e.g. false branch of ternary
											// returns ""); keep static classes.
											evaluated = staticPart
										}
									}
								} else if exprStart == 0 {
									// The entire value is an expression — just
									// evaluate it directly (already attempted
									// above but try once more with trimmed
									// input).
									evaluated, _ = r.evaluateExpr(rawValExpr)
								}
							}
						}
					} else if containsParenExpr(rawValExpr) {
						// The raw value mixes static class names with a
						// parenthesised expression, e.g.
						//   card (isActive ? "active" : "")
						// isOperatorExpr missed it because the operator
						// lives inside parens.  Split into static prefix
						// and expression, then evaluate the expression.
						words := strings.Fields(rawValExpr)
						exprStart := -1
						for i, word := range words {
							if len(word) > 0 && word[0] == '(' {
								exprStart = i
								break
							}
						}
						if exprStart >= 0 {
							exprStr := strings.Join(words[exprStart:], " ")
							evaled, _ := r.evaluateExpr(exprStr)
							if exprStart > 0 {
								staticPart := strings.Join(words[:exprStart], " ")
								if evaled != "" {
									evaluated = staticPart + " " + evaled
								} else {
									evaluated = staticPart
								}
							} else {
								evaluated = evaled
							}
						} else {
							evaluated, _ = r.evaluateExpr(rawValExpr)
						}
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
								wasQuoted := false
								if len(inner) >= 2 &&
									((inner[0] == '"' && inner[len(inner)-1] == '"') ||
										(inner[0] == '\'' && inner[len(inner)-1] == '\'')) {
									inner = inner[1 : len(inner)-1]
									wasQuoted = true
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
									} else if wasQuoted && word != "" {
										// Static class token from a quoted list —
										// keep it literally. An unquoted word that
										// resolved to "" is a variable/expression
										// whose value is empty, so drop it rather
										// than leaking the identifier name.
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
							wasQuoted := false
							if len(inner) >= 2 &&
								((inner[0] == '"' && inner[len(inner)-1] == '"') ||
									(inner[0] == '\'' && inner[len(inner)-1] == '\'')) {
								inner = inner[1 : len(inner)-1]
								wasQuoted = true
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
								} else if wasQuoted && word != "" {
									// See note above: only keep literal tokens that
									// came from a quoted static class list.
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

		// Omit a falsy attribute only for HTML boolean attributes (checked,
		// disabled, selected, …), mirroring pugjs's omit-on-false. For every
		// other attribute (data-*, aria-*, value, custom) a value of "false"
		// is a legitimate string and must be rendered (e.g. data-x="false").
		// go-pug is stringly-typed, so it cannot distinguish a real boolean
		// false from the string "false"; the boolean-attribute name set is the
		// heuristic. A quoted literal ("false") is always kept.
		if evaluated == "false" && isBooleanAttribute(name) {
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
				evaluated = htmlEscapeAttr(evaluated)
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

	isInline := r.pretty() && prettyInline(tag)
	if r.pretty() && !isInline {
		r.prettyIndent++
	}

	if isRawTextElement(tag.Name) {
		r.inRawTextElement = true
	}

	for _, child := range tag.Children {
		if err := r.renderNode(child); err != nil {
			return err
		}
	}

	if isRawTextElement(tag.Name) {
		r.inRawTextElement = false
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

// isRawTextElement reports whether name is an HTML raw-text element whose
// content must never be HTML-entity-encoded. The HTML5 spec defines script
// and style as raw text elements: the browser passes their text content
// directly to the JS engine or CSS parser without entity-decoding.
func isRawTextElement(name string) bool {
	return name == "script" || name == "style"
}

// htmlEscapeAttr escapes the characters that must be escaped inside a
// double-quoted HTML attribute value: <, >, ", and bare & (i.e. & not already
// part of a valid entity reference).  Single quotes do NOT need escaping
// inside double-quoted attributes, so they are passed through unchanged —
// this is important for inline JS event handlers such as onclick="alert('x')".
func htmlEscapeAttr(s string) string {
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
		case '"':
			b.WriteString("&quot;")
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

// EscapeAttr escapes s for safe inclusion inside a double-quoted HTML
// attribute value, with exactly the same semantics Runtime.renderTag uses
// for a dynamic attribute value: it escapes <, >, ", and bare & but leaves a
// single quote untouched, and it never double-escapes an already-valid
// &entity; reference. It is exported so codegen-generated code can call it
// directly, keeping attribute escaping single-sourced in htmlEscapeAttr
// rather than duplicating (and risking diverging from) that logic in
// generated code.
func EscapeAttr(s string) string {
	return htmlEscapeAttr(s)
}

// JoinClasses returns classes joined by a single space, dropping any empty
// element. It is exported so codegen-generated code can reproduce
// Runtime.renderTag's dynamic class-merge rule exactly: a shorthand class
// token (`.foo`) is always kept, but a bare class field/dot-path token that
// evaluates to the empty string is dropped rather than leaving a trailing
// space or leaking the field's value as a literal empty string.
func JoinClasses(classes ...string) string {
	kept := make([]string, 0, len(classes))
	for _, c := range classes {
		if c != "" {
			kept = append(kept, c)
		}
	}
	return strings.Join(kept, " ")
}

// Add reproduces Runtime.evaluateExpr's `+` operator on two already-
// stringified operands: if both left and right parse as numbers (toFloat),
// it returns their numeric sum formatted the same way every other numeric
// result is (strconv.FormatFloat with the 'f' verb, shortest round-tripping
// precision); otherwise it returns the plain string concatenation of left
// and right. This disambiguation happens on the operands' RUNTIME VALUES,
// not on any static type, so the same two operand strings always produce
// the same result regardless of caller — "5"+"3" is the number 8, but
// "a"+"5" is the string "a5". It is exported so codegen-generated code can
// call it directly, keeping the `+` operator's value semantics
// single-sourced in this one implementation rather than reproducing (and
// risking diverging from) evaluateExpr's own logic.
func Add(left, right string) string {
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if lok && rok {
		return strconv.FormatFloat(lf+rf, 'f', -1, 64)
	}
	return left + right
}

// Sub reproduces Runtime.evaluateExpr's `-` operator on two already-
// stringified operands: if both left and right parse as numbers (toFloat),
// it returns their numeric difference formatted the same way every other
// numeric result is (strconv.FormatFloat with the 'f' verb, shortest
// round-tripping precision); otherwise it returns the empty string — unlike
// `+`, non-numeric operands never fall back to concatenation. This
// disambiguation happens on the operands' RUNTIME VALUES, not on any static
// type, so the same two operand strings always produce the same result
// regardless of caller. It is exported so codegen-generated code can call it
// directly, keeping the `-` operator's value semantics single-sourced in
// this one implementation rather than reproducing (and risking diverging
// from) evaluateExpr's own logic.
func Sub(left, right string) string {
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if lok && rok {
		return strconv.FormatFloat(lf-rf, 'f', -1, 64)
	}
	return ""
}

// Mul reproduces Runtime.evaluateExpr's `*` operator on two already-
// stringified operands: if both left and right parse as numbers (toFloat),
// it returns their numeric product formatted the same way every other
// numeric result is (strconv.FormatFloat with the 'f' verb, shortest
// round-tripping precision); otherwise it returns the empty string. This
// disambiguation happens on the operands' RUNTIME VALUES, not on any static
// type, so the same two operand strings always produce the same result
// regardless of caller. It is exported so codegen-generated code can call it
// directly, keeping the `*` operator's value semantics single-sourced in
// this one implementation rather than reproducing (and risking diverging
// from) evaluateExpr's own logic.
func Mul(left, right string) string {
	lf, lok := toFloat(left)
	rf, rok := toFloat(right)
	if lok && rok {
		return strconv.FormatFloat(lf*rf, 'f', -1, 64)
	}
	return ""
}

// Div reproduces Runtime.evaluateExpr's `/` operator on two already-
// stringified operands: if both left and right parse as numbers (toFloat),
// it returns their numeric quotient formatted the same way every other
// numeric result is (strconv.FormatFloat with the 'f' verb, shortest
// round-tripping precision) and a nil error — unless the right operand is
// exactly zero, in which case it returns an empty string and an error
// (matching evaluateExpr's own division-by-zero abort, which propagates out
// of Render); when either operand is not numeric it returns the empty string
// with a nil error, exactly like Sub and Mul. This disambiguation happens on
// the operands' RUNTIME VALUES, not on any static type, so the same two
// operand strings always produce the same result regardless of caller. It is
// exported so codegen-generated code can call it directly, keeping the `/`
// operator's value semantics — including its one fallible case — single-
// sourced in this one implementation rather than reproducing (and risking
// diverging from) evaluateExpr's own logic.
func Div(left, right string) (string, error) {
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

// Mod reproduces Runtime.evaluateExpr's `%` operator on two already-
// stringified operands: if both left and right parse as numbers (toFloat),
// it returns their integer remainder — each operand truncated to an int64
// before the Go `%` operator is applied, exactly like evaluateExpr's own
// modulo branch — formatted the same way every other numeric result is
// (strconv.FormatFloat with the 'f' verb, shortest round-tripping precision)
// and a nil error, unless the right operand is exactly zero, in which case it
// returns an empty string and an error (matching evaluateExpr's own
// modulo-by-zero abort, which propagates out of Render); when either operand
// is not numeric it returns the empty string with a nil error, exactly like
// Sub and Mul. This disambiguation happens on the operands' RUNTIME VALUES,
// not on any static type, so the same two operand strings always produce the
// same result regardless of caller. It is exported so codegen-generated code
// can call it directly, keeping the `%` operator's value semantics —
// including its one fallible case — single-sourced in this one
// implementation rather than reproducing (and risking diverging from)
// evaluateExpr's own logic.
func Mod(left, right string) (string, error) {
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

// Not reproduces Runtime.evaluateExpr's unary `!` operator on an already-
// stringified operand: "false" when Truthy(val) is true, "true" otherwise —
// the interpreter's own `!` branch is this one-line rule, factored out here
// as the single source of truth so it isn't duplicated (and risks diverging)
// between Runtime.evaluateExpr's `!expr` handling and codegen-generated
// code's own `!` value-context expressions, exactly like Add/Sub/Mul/Div/Mod
// are shared between the two. It is exported so codegen-generated code can
// call it directly.
func Not(val string) string {
	if Truthy(val) {
		return "false"
	}
	return "true"
}

// UnquoteArg strips a single layer of matching double or single quotes from
// s, exactly the way Runtime.evaluateExpr's string-method dispatch strips a
// method argument after evaluating it: only when s is at least two bytes
// long and its first and last byte are the same quote character. It is the
// single source of that quote-strip quirk, called both by the method-
// dispatch switch below (on the value evaluateExpr(argExpr) already
// produced) and by codegen-generated code (on the value genValueExpr(argExpr)
// already produced, including its own Method* calls and the "join" value
// expression's separator), so the two paths cannot drift. It is exported so
// codegen-generated code can call it directly.
func UnquoteArg(s string) string {
	if len(s) >= 2 &&
		((s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

// MethodRepeat reproduces Runtime.evaluateExpr's "repeat" string-method case
// on already-evaluated operands: n parses as a non-negative integer, recv
// repeated that many times; otherwise recv unchanged. It is exported so
// codegen-generated code can call it directly, keeping the "repeat" method's
// semantics single-sourced in this one implementation rather than
// reproducing (and risking diverging from) evaluateExpr's own logic.
func MethodRepeat(recv, n string) string {
	if count, err := strconv.Atoi(n); err == nil && count >= 0 {
		return strings.Repeat(recv, count)
	}
	return recv
}

// MethodSplit reproduces Runtime.evaluateExpr's "split" string-method case on
// already-evaluated operands: recv split on sep (after UnquoteArg), then
// re-joined with a single space — matching evaluateExpr's own
// strings.Join(strings.Split(...), " ") exactly. It is exported so
// codegen-generated code can call it directly, keeping the "split" method's
// semantics single-sourced in this one implementation.
func MethodSplit(recv, sep string) string {
	sep = UnquoteArg(sep)
	return strings.Join(strings.Split(recv, sep), " ")
}

// MethodReplace reproduces Runtime.evaluateExpr's "replace" string-method case
// on already-evaluated operands: the first occurrence of oldArg replaced
// with newArg in recv. The quote-strip here is intentionally NOT the plain
// per-argument UnquoteArg: it reproduces the original case's own loop over
// the two-element snapshot []string{oldArg, newArg}, which compares each
// snapshot element s against the (possibly already-reassigned) oldArg
// variable to decide which of oldArg/newArg to overwrite. When oldArg and
// newArg hold the same string, the first iteration's reassignment of oldArg
// makes the second iteration's `s == oldArg` comparison miss its own
// original snapshot value, so newArg is left un-stripped even though it
// looks quoted — a real, byte-for-byte quirk of the original interpreter
// this helper must reproduce exactly, not "fix": changing it would be a
// separate, deliberate, pugjs-validated interpreter-semantics decision, not
// a side effect of single-sourcing a codegen helper. It is exported so
// codegen-generated code can call it directly, keeping the "replace"
// method's semantics single-sourced in this one implementation.
func MethodReplace(recv, oldArg, newArg string) string {
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
	return strings.Replace(recv, oldArg, newArg, 1)
}

// MethodSlice1 reproduces Runtime.evaluateExpr's one-argument "slice"
// string-method case on an already-evaluated operand: a RUNE-based (not
// byte-based) slice of recv from startStr (parsed as an integer, a negative
// value counting back from the end and clamped to 0) to the end. It is
// exported so codegen-generated code can call it directly, keeping the
// one-argument "slice" semantics single-sourced in this one implementation.
func MethodSlice1(recv, startStr string) string {
	runes := []rune(recv)
	start, _ := strconv.Atoi(startStr)
	if start < 0 {
		start = len(runes) + start
	}
	if start < 0 {
		start = 0
	}
	if start <= len(runes) {
		return string(runes[start:])
	}
	return ""
}

// MethodSlice2 reproduces Runtime.evaluateExpr's two-argument "slice"
// string-method case on already-evaluated operands: a RUNE-based (not
// byte-based) slice of recv from startStr to endStr (each parsed as an
// integer, a negative value counting back from the end, start clamped to 0
// and end clamped to len(runes)); when the clamped start is after the
// clamped end, the empty string. It is exported so codegen-generated code
// can call it directly, keeping the two-argument "slice" semantics
// single-sourced in this one implementation.
func MethodSlice2(recv, startStr, endStr string) string {
	runes := []rune(recv)
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
		return string(runes[start:end])
	}
	return ""
}

// MethodIndexOf reproduces Runtime.evaluateExpr's "indexOf" string-method case
// on already-evaluated operands: the byte index of needle (after
// UnquoteArg) in recv, formatted as a decimal string ("-1" when not found —
// strings.Index's own not-found sentinel). It is exported so codegen-
// generated code can call it directly, keeping the "indexOf" method's
// semantics single-sourced in this one implementation.
func MethodIndexOf(recv, needle string) string {
	return strconv.Itoa(strings.Index(recv, UnquoteArg(needle)))
}

// MethodIncludes reproduces Runtime.evaluateExpr's "includes"/"contains"
// string-method case on already-evaluated operands: "true" when recv
// contains needle (after UnquoteArg), "false" otherwise. It is exported so
// codegen-generated code can call it directly, keeping the
// "includes"/"contains" method's semantics single-sourced in this one
// implementation.
func MethodIncludes(recv, needle string) string {
	if strings.Contains(recv, UnquoteArg(needle)) {
		return "true"
	}
	return "false"
}

// MethodStartsWith reproduces Runtime.evaluateExpr's "startsWith"
// string-method case on already-evaluated operands: "true" when recv starts
// with prefix (after UnquoteArg), "false" otherwise. It is exported so
// codegen-generated code can call it directly, keeping the "startsWith"
// method's semantics single-sourced in this one implementation.
func MethodStartsWith(recv, prefix string) string {
	if strings.HasPrefix(recv, UnquoteArg(prefix)) {
		return "true"
	}
	return "false"
}

// MethodEndsWith reproduces Runtime.evaluateExpr's "endsWith" string-method
// case on already-evaluated operands: "true" when recv ends with suffix
// (after UnquoteArg), "false" otherwise. It is exported so codegen-generated
// code can call it directly, keeping the "endsWith" method's semantics
// single-sourced in this one implementation.
func MethodEndsWith(recv, suffix string) string {
	if strings.HasSuffix(recv, UnquoteArg(suffix)) {
		return "true"
	}
	return "false"
}

// methodPad is the shared RUNE-based padding logic behind MethodPadStart and
// MethodPadEnd, reproducing Runtime.evaluateExpr's "padStart"/"padEnd"
// string-method cases on already-evaluated operands: when targetLenStr
// (after TrimSpace, matching the original case's own
// strconv.Atoi(strings.TrimSpace(lenStr)) call — the trim lives HERE, not at
// either caller, so the interpreter and codegen-generated code, which pass
// targetLenStr as a raw already-evaluated value with no trim of their own,
// agree even when that value carries surrounding whitespace) parses to a
// length greater than recv's rune count, padChar (after UnquoteArg,
// defaulting to a single space when empty either because it wasn't given or
// because it evaluated to the empty string) is repeated, rune by rune, to
// fill the gap; recv unchanged otherwise. atStart selects which side the
// padding is written to.
func methodPad(recv, targetLenStr, padCharArg string, atStart bool) string {
	targetLen, _ := strconv.Atoi(strings.TrimSpace(targetLenStr))
	padChar := UnquoteArg(padCharArg)
	if padChar == "" {
		padChar = " "
	}
	runes := []rune(recv)
	if targetLen <= len(runes) {
		return recv
	}
	padRunes := []rune(padChar)
	needed := targetLen - len(runes)
	var b strings.Builder
	if atStart {
		for i := 0; i < needed; i++ {
			b.WriteRune(padRunes[i%len(padRunes)])
		}
		b.WriteString(recv)
	} else {
		b.WriteString(recv)
		for i := 0; i < needed; i++ {
			b.WriteRune(padRunes[i%len(padRunes)])
		}
	}
	return b.String()
}

// MethodPadStart reproduces Runtime.evaluateExpr's "padStart" string-method
// case on already-evaluated operands. It is exported so codegen-generated
// code can call it directly, keeping the "padStart" method's semantics
// single-sourced in this one implementation.
func MethodPadStart(recv, targetLenStr, padCharArg string) string {
	return methodPad(recv, targetLenStr, padCharArg, true)
}

// MethodPadEnd reproduces Runtime.evaluateExpr's "padEnd" string-method case
// on already-evaluated operands. It is exported so codegen-generated code
// can call it directly, keeping the "padEnd" method's semantics
// single-sourced in this one implementation.
func MethodPadEnd(recv, targetLenStr, padCharArg string) string {
	return methodPad(recv, targetLenStr, padCharArg, false)
}

// ToFixed reproduces Runtime.evaluateExpr's "toFixed" numeric formatting on
// an already-typed float64 value: precArg (TrimSpace'd) parses as the
// decimal-place count, defaulting to 0 on a parse failure or a negative
// result (matching the original case's own `prec, _ := strconv.Atoi(...)`
// followed by its `if prec < 0 { prec = 0 }` clamp exactly), then f is
// formatted with fmt.Sprintf("%.<prec>f", f). It is exported so codegen-
// generated code can call it directly on a numeric field's own raw float64
// value — no ParseFloat round-trip — keeping the "toFixed" method's numeric
// formatting single-sourced in this one implementation for both engines.
func ToFixed(f float64, precArg string) string {
	prec, err := strconv.Atoi(strings.TrimSpace(precArg))
	if err != nil || prec < 0 {
		prec = 0
	}
	return fmt.Sprintf("%."+strconv.Itoa(prec)+"f", f)
}

// ToFixedStr reproduces Runtime.evaluateExpr's fallback "toFixed" case for a
// receiver whose raw value isn't itself numeric: recv is parsed with
// strconv.ParseFloat, and on success formatted via ToFixed; on failure it
// returns the interpreter's own "toFixed: value %q is not a number" error.
// It is exported so codegen-generated code can call it directly for a
// string-typed field, keeping this fallible path single-sourced.
func ToFixedStr(recv, precArg string) (string, error) {
	f, err := strconv.ParseFloat(recv, 64)
	if err != nil {
		return "", fmt.Errorf("toFixed: value %q is not a number", recv)
	}
	return ToFixed(f, precArg), nil
}

// ToPrecision reproduces Runtime.evaluateExpr's "toPrecision" numeric
// formatting on an already-typed float64 value: prec defaults to 6, and is
// only overridden when precArg (TrimSpace'd) parses to a strictly positive
// integer — matching the original case's own `p > 0` guard exactly — then f
// is formatted with fmt.Sprintf("%.<prec>g", f). It is exported so codegen-
// generated code can call it directly on a numeric field's own raw float64
// value, keeping the "toPrecision" method's numeric formatting
// single-sourced in this one implementation for both engines.
func ToPrecision(f float64, precArg string) string {
	prec := 6
	if p, err := strconv.Atoi(strings.TrimSpace(precArg)); err == nil && p > 0 {
		prec = p
	}
	return fmt.Sprintf("%."+strconv.Itoa(prec)+"g", f)
}

// ToPrecisionStr is ToFixedStr's "toPrecision" analogue, reproducing
// Runtime.evaluateExpr's fallback case for a non-numeric receiver:
// strconv.ParseFloat on recv, then ToPrecision on success, or the
// interpreter's own "toPrecision: value %q is not a number" error on
// failure. It is exported so codegen-generated code can call it directly for
// a string-typed field.
func ToPrecisionStr(recv, precArg string) (string, error) {
	f, err := strconv.ParseFloat(recv, 64)
	if err != nil {
		return "", fmt.Errorf("toPrecision: value %q is not a number", recv)
	}
	return ToPrecision(f, precArg), nil
}

// sortAttrNames returns the keys of attrs ordered the way HTML tag output
// renders them: id first, then class, then every other attribute name
// alphabetically. Runtime.renderTag and the codegen backend's genAttributes
// both call this single helper so the two attribute-serialisation paths
// cannot drift apart.
func sortAttrNames(attrs map[string]*AttributeValue) []string {
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
	return names
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
		val, err := r.evaluateCode(code)
		if err != nil {
			return err
		}
		r.htmlBuf.WriteString(html.EscapeString(val))
		return nil
	case CodeUnescaped:
		val, err := r.evaluateCode(code)
		if err != nil {
			return err
		}
		r.htmlBuf.WriteString(val)
		return nil
	}
	return nil
}

// evaluateCode returns the same string evaluateExpr(code.Expression) would,
// using the closure-compiled version of the expression when classifyExpr
// found one at Compile time, and falling back to the string interpreter
// otherwise.
func (r *Runtime) evaluateCode(code *CodeNode) (string, error) {
	if code.compiled != nil {
		return code.compiled(r)
	}
	return r.evaluateExpr(code.Expression)
}

// evaluateMixinArg returns the same string r.evaluateExpr(call.Arguments[i])
// would, using the closure-compiled version of that argument when
// classifyExpr found one at Compile time, and falling back to the string
// interpreter otherwise. Mixin arguments are stringified today (never typed
// values), so both paths return a string.
func (r *Runtime) evaluateMixinArg(call *MixinCallNode, i int) (string, error) {
	if call.compiledArgs != nil {
		if fn := call.compiledArgs[i]; fn != nil {
			return fn(r)
		}
	}
	return r.evaluateExpr(call.Arguments[i])
}

// unbufferedStmtKind classifies an unbuffered code statement's shape, as
// classifyUnbufferedStmt determines it.
type unbufferedStmtKind int

const (
	unbufferedOther     unbufferedStmtKind = iota // bare expression, evaluated and discarded
	unbufferedIncrement                           // x++
	unbufferedDecrement                           // x--
	unbufferedAddAssign                           // x += rhs
	unbufferedSubAssign                           // x -= rhs
	unbufferedAssign                              // x = rhs
)

// classifyUnbufferedStmt strips a leading var/let/const keyword from stmt and
// classifies its shape in the exact order executeStatement's own dispatch
// chain checks them, so both the interpreter and the codegen backend split a
// statement at the same character position — a single source of truth for
// WHERE a `-` code statement's operator sits, rather than two independently
// maintained copies of the same scan that could quietly drift apart.
//
// varName and rhsExpr are populated for every kind except unbufferedOther,
// where the whole (var-stripped) statement is returned as rhsExpr for the
// caller to evaluate-and-discard, matching the interpreter's own fallback
// behavior for a bare expression statement.
func classifyUnbufferedStmt(stmt string) (kind unbufferedStmtKind, varName, rhsExpr string) {
	stmt = strings.TrimSpace(stmt)

	for _, kw := range []string{"var ", "let ", "const "} {
		if strings.HasPrefix(stmt, kw) {
			stmt = strings.TrimSpace(stmt[len(kw):])
			break
		}
	}

	if strings.HasSuffix(stmt, "++") {
		return unbufferedIncrement, strings.TrimSpace(stmt[:len(stmt)-2]), ""
	}
	if strings.HasSuffix(stmt, "--") {
		return unbufferedDecrement, strings.TrimSpace(stmt[:len(stmt)-2]), ""
	}

	if idx := strings.Index(stmt, "+="); idx > 0 {
		return unbufferedAddAssign, strings.TrimSpace(stmt[:idx]), strings.TrimSpace(stmt[idx+2:])
	}

	if idx := strings.Index(stmt, "-="); idx > 0 {
		return unbufferedSubAssign, strings.TrimSpace(stmt[:idx]), strings.TrimSpace(stmt[idx+2:])
	}

	if idx := findAssignOp(stmt); idx >= 0 {
		return unbufferedAssign, strings.TrimSpace(stmt[:idx]), strings.TrimSpace(stmt[idx+1:])
	}

	return unbufferedOther, "", stmt
}

// executeStatement executes an unbuffered code statement, handling assignment
// (var = expr), increment (var++), decrement (var--), and += / -=.
// For anything else the expression is evaluated and the result discarded.
func (r *Runtime) executeStatement(stmt string) error {
	kind, varName, rhsExpr := classifyUnbufferedStmt(stmt)

	switch kind {
	case unbufferedIncrement:
		val, _ := r.lookup(varName)
		n, ok := toFloat(val)
		if !ok {
			n = 0
		}
		r.setVar(varName, n+1)
		return nil

	case unbufferedDecrement:
		val, _ := r.lookup(varName)
		n, ok := toFloat(val)
		if !ok {
			n = 0
		}
		r.setVar(varName, n-1)
		return nil

	case unbufferedAddAssign:
		cur, _ := r.lookup(varName)
		curF, _ := toFloat(cur)
		rhs, err := r.evaluateExpr(rhsExpr)
		if err != nil {
			return err
		}
		rhsF, ok := toFloat(rhs)
		if ok {
			r.setVar(varName, curF+rhsF)
		} else {
			r.setVar(varName, fmt.Sprintf("%v", cur)+rhs)
		}
		return nil

	case unbufferedSubAssign:
		cur, _ := r.lookup(varName)
		curF, _ := toFloat(cur)
		rhs, err := r.evaluateExpr(rhsExpr)
		if err != nil {
			return err
		}
		rhsF, ok := toFloat(rhs)
		if !ok {
			// Non-numeric RHS: no meaningful subtraction — leave the
			// variable unchanged and return an error so the template
			// author is informed rather than silently losing the update.
			return fmt.Errorf("operator -=: cannot subtract non-numeric value %q from %v", rhs, cur)
		}
		r.setVar(varName, curF-rhsF)
		return nil

	case unbufferedAssign:
		rawVal := r.evaluateExprRaw(rhsExpr)
		r.setVar(varName, rawVal)
		return nil

	default: // unbufferedOther
		_, err := r.evaluateExpr(rhsExpr)
		return err
	}
}

// setVar writes a variable, updating the innermost scope that already contains
// it, or creating it in the top scope if not found anywhere.
func (r *Runtime) setVar(name string, val any) {
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
		r.scopeStack[top] = make(map[string]any)
	}
	r.scopeStack[top][name] = val
}

// findAssignOp finds the position of a simple = assignment operator that is
// not part of ==, !=, <=, >=, +=, -=.  Returns -1 if not found.
func findAssignOp(stmt string) int {
	for i := 0; i < len(stmt); i++ {
		if stmt[i] == '=' {
			if i > 0 {
				prev := stmt[i-1]
				if prev == '!' || prev == '<' || prev == '>' || prev == '=' ||
					prev == '+' || prev == '-' {
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

// mayBeFloat reports whether a trimmed string could possibly be a valid
// strconv.ParseFloat input, based on its first byte alone. A valid float
// (decimal, hex, or one of the inf/infinity/nan spellings) must start with
// a digit, a sign, a decimal point, or the first letter of "inf"/"nan"
// (case-insensitive). Anything else can never parse, so callers can skip
// the ParseFloat call — and the error allocation it makes on failure —
// entirely. This is a cheap pre-filter only; it does not itself validate
// the rest of the string.
func mayBeFloat(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	switch trimmed[0] {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'+', '-', '.', 'i', 'I', 'n', 'N':
		return true
	}
	return false
}

func toFloat(v any) (float64, bool) {
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
		trimmed := strings.TrimSpace(val)
		if !mayBeFloat(trimmed) {
			return 0, false
		}
		f, err := strconv.ParseFloat(trimmed, 64)
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
// csv.split(",") fall back to evaluateExprRaw which returns []any.
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
				scope := make(map[string]any)
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
		scope := make(map[string]any)
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
			if r.inRawTextElement {
				r.htmlBuf.WriteString(tok.Value)
			} else {
				r.htmlBuf.WriteString(htmlEscapeText(tok.Value))
			}

		case TokenInterpolation:
			val, err := r.evaluateExpr(tok.Value)
			if err != nil {
				val = tok.Value
			}
			if r.inRawTextElement {
				r.htmlBuf.WriteString(val)
			} else {
				r.htmlBuf.WriteString(htmlEscapeText(val))
			}

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
// basedirResolveHint returns a migration hint for a failed relative
// include/extends when the *same* path would resolve against Basedir. It only
// fires when such a file actually exists, so genuine typos don't get a
// misleading "did you mean /..." suggestion. Returns "" otherwise.
func (r *Runtime) basedirResolveHint(inclPath string) string {
	if r.includeBase == "" {
		return ""
	}
	if strings.HasPrefix(inclPath, "/") || strings.HasPrefix(inclPath, "\\") {
		return "" // already Basedir-relative
	}
	cand := filepath.Join(r.includeBase, inclPath)
	if filepath.Ext(cand) == "" {
		cand += ".pug"
	}
	if _, err := os.Stat(cand); err != nil {
		return ""
	}
	return fmt.Sprintf(" — a file exists at %q; did you mean a leading-slash (Basedir-relative) path %q?",
		cand, "/"+strings.TrimLeft(inclPath, "/\\"))
}

// resolveIncludeAbs applies renderInclude's own path-resolution and
// cycle-detection rules to inc, without doing any I/O or touching
// includeStack: a leading slash is Basedir-relative; every other relative
// path resolves against the directory of the file doing the including — the
// innermost active include when nested, or the entry file when at the top
// level (falling back to Basedir for string renders); a cycle is any
// already-active include path reappearing on r.includeStack; a path with no
// extension gets ".pug" appended. It is shared by renderInclude and
// codegen's generate-time include inliner (composition.go) so the two agree
// byte-for-byte on which file an include resolves to and when a cycle fires.
// unquoted is inc.Path with any surrounding quotes stripped, for callers that
// need it in a basedirResolveHint.
func (r *Runtime) resolveIncludeAbs(inc *IncludeNode) (abs string, unquoted string, err error) {
	inclPath := inc.Path

	if len(inclPath) >= 2 &&
		((inclPath[0] == '"' && inclPath[len(inclPath)-1] == '"') ||
			(inclPath[0] == '\'' && inclPath[len(inclPath)-1] == '\'')) {
		inclPath = inclPath[1 : len(inclPath)-1]
	}

	var resolved string
	if strings.HasPrefix(inclPath, "/") || strings.HasPrefix(inclPath, "\\") {
		// Treat leading slash as basedir-relative on all OSes (on Windows
		// filepath.IsAbs("/foo") is false, so we handle this case first).
		if r.includeBase != "" {
			resolved = filepath.Join(r.includeBase, inclPath)
		} else {
			resolved = inclPath
		}
	} else if filepath.IsAbs(inclPath) {
		if r.includeBase != "" {
			resolved = filepath.Join(r.includeBase, inclPath)
		} else {
			resolved = inclPath
		}
	} else {
		// Relative include (standard Pug): resolve against the directory of the
		// file doing the including.
		//   - nested include → dir of the innermost active include
		//   - top-level file → dir of the entry file (RenderFile/CompileFile)
		//   - string render  → fall back to Basedir (no entry file to anchor to)
		var base string
		switch {
		case len(r.includeStack) > 0:
			base = filepath.Dir(r.includeStack[len(r.includeStack)-1])
		case r.entryFile != "":
			base = filepath.Dir(r.entryFile)
		default:
			base = r.includeBase
		}
		if base == "" {
			base = "."
		}
		resolved = filepath.Join(base, inclPath)
	}

	abs, err = filepath.Abs(resolved)
	if err != nil {
		return "", inclPath, fmt.Errorf("include: cannot resolve path %q: %w", inclPath, err)
	}

	if slices.Contains(r.includeStack, abs) {
		return "", inclPath, fmt.Errorf("include: cycle detected — %q includes itself", abs)
	}

	if filepath.Ext(abs) == "" {
		abs += ".pug"
	}

	return abs, inclPath, nil
}

// renderInclude resolves and renders an include directive. .pug files (or no
// extension) are lexed, parsed, and rendered; all other files are written
// raw (optionally through a registered filter). Cycle detection is via
// includeStack; path resolution is shared with codegen's include inliner via
// resolveIncludeAbs.
func (r *Runtime) renderInclude(inc *IncludeNode) error {
	abs, inclPath, err := r.resolveIncludeAbs(inc)
	if err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(abs))

	r.includeStack = append(r.includeStack, abs)
	defer func() { r.includeStack = r.includeStack[:len(r.includeStack)-1] }()

	if ext == ".pug" {
		src, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("include: cannot read %q: %w%s", abs, err, r.basedirResolveHint(inclPath))
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
		return fmt.Errorf("include: cannot read %q: %w%s", abs, err, r.basedirResolveHint(inclPath))
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

func (r *Runtime) renderMixinCall(call *MixinCallNode) error {
	decl, ok := r.mixinDecls[call.Name]
	if !ok {
		return fmt.Errorf("mixin %q is not defined", call.Name)
	}

	scope := make(map[string]any)

	for i, param := range decl.Parameters {
		if i < len(call.Arguments) {
			val, err := r.evaluateMixinArg(call, i)
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
		rest := make([]any, 0)
		for i := len(decl.Parameters); i < len(call.Arguments); i++ {
			val, err := r.evaluateMixinArg(call, i)
			if err != nil {
				return err
			}
			rest = append(rest, val)
		}
		scope[decl.RestParamName] = rest
	}

	attrMap := make(map[string]any)
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

	// Push the boundary sentinel first, then the mixin's own scope frame.
	// lookup stops at the sentinel, so caller variables are not visible inside
	// the mixin body. Both frames are popped together on exit.
	r.scopeStack = append(r.scopeStack, mixinScopeBoundary, scope)
	prevInMixin := r.inMixin
	r.inMixin = true
	for _, node := range decl.Body {
		if err := r.renderNode(node); err != nil {
			r.scopeStack = r.scopeStack[:len(r.scopeStack)-2]
			r.inMixin = prevInMixin
			r.callerBlock = prevBlock
			return err
		}
	}
	r.scopeStack = r.scopeStack[:len(r.scopeStack)-2]
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
			if fn == nil {
				return nil, false
			}
			return fn, true
		}
	}
	return nil, false
}

// evaluateExprRaw evaluates an expression and returns a raw any value
// rather than a string. Used when the caller needs a real Go slice or map
// (e.g. the collection in an each loop). Special-cased for split, inline
// object literals, and inline array literals; falls back to evaluateExpr.
func (r *Runtime) evaluateExprRaw(expr string) any {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}

	if idx := findTernary(expr); idx >= 0 {
		cond, err := r.evaluateExpr(expr[:idx])
		if err == nil {
			rest := expr[idx+1:]
			if colonIdx := findBinaryOp(rest, ":"); colonIdx >= 0 {
				if r.isTruthy(cond) {
					return r.evaluateExprRaw(rest[:colonIdx])
				}
				return r.evaluateExprRaw(rest[colonIdx+1:])
			}
		}
	}

	if dotIdx := findTopLevelDot(expr); dotIdx > 0 {
		objExpr := expr[:dotIdx]
		rest := expr[dotIdx+1:]
		methodName := rest
		argsStr := ""
		if before, inner, found := strings.Cut(rest, "("); found {
			methodName = before
			argsStr, _, _ = strings.Cut(strings.TrimSpace(inner), ")")
			argsStr = strings.TrimSpace(argsStr)
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
			result := make([]any, len(parts))
			for i, p := range parts {
				result[i] = p
			}
			return result
		}
	}

	if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
		obj := parseInlineObject(expr)
		if obj != nil {
			result := make(map[string]any, len(obj))
			for k, v := range obj {
				result[k] = v
			}
			return result
		}
	}

	if len(expr) >= 2 && expr[0] == '[' && expr[len(expr)-1] == ']' {
		closeIdx := findMatchingCloseBracket(expr)
		if closeIdx != len(expr)-1 {
			goto NOT_ARRAY_LITERAL
		}
		inner := strings.TrimSpace(expr[1 : len(expr)-1])
		if inner == "" {
			return []any{}
		}
		parts := splitTopLevel(inner, ',')
		result := make([]any, 0, len(parts))
		for _, p := range parts {
			v := r.evaluateExprRaw(strings.TrimSpace(p))
			result = append(result, v)
		}
		return result
	}
NOT_ARRAY_LITERAL:

	if idx := findIndexOp(expr); idx >= 0 {
		objExpr := expr[:idx]
		keyExpr := expr[idx+1 : len(expr)-1]
		obj := r.evaluateExprRaw(strings.TrimSpace(objExpr))
		if obj == nil {
			return nil
		}
		key, _ := r.evaluateExpr(keyExpr)
		return r.indexValue(obj, key)
	}

	if val, ok := r.lookup(expr); ok {
		return val
	}

	s, _ := r.evaluateExpr(expr)
	return s
}

// exprFastPathDisabledForTests forces evaluateExpr (including every
// recursive sub-expression call it makes) to skip the tryEvalSimple
// fast-path and run the full operator-scan chain below, regardless of
// shape. It exists solely so tests can reconstruct evaluateExpr's
// pre-fast-path behavior for differential comparison; production code must
// never set it.
var exprFastPathDisabledForTests bool

// evaluateExpr evaluates an expression string against the current scope and
// returns a string result. Operator precedence (low to high): ternary,
// logical OR/AND, comparison, logical NOT, arithmetic, index/dot access.
func (r *Runtime) evaluateExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)

	if expr == "" {
		return "", nil
	}

	if !exprFastPathDisabledForTests {
		if s, ok := r.tryEvalSimple(expr); ok {
			return s, nil
		}
	}

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
		return Not(inner), nil
	}

	if len(expr) >= 2 {
		q := rune(expr[0])
		if q == '"' || q == '\'' {
			if lit, ok := unwrapQuotedLiteral(expr); ok {
				return lit, nil
			}
		}

		if q == '`' {
			// Template literal: `text ${expr} text`
			// Walk the content, evaluate each ${...} interpolation,
			// and concatenate literal segments with evaluated results.
			inner := expr[1:]
			var result strings.Builder
			i := 0
			for i < len(inner) {
				if inner[i] == '`' {
					break // closing backtick
				}
				if inner[i] == '\\' && i+1 < len(inner) {
					// escape sequence — pass the next char through literally
					result.WriteByte(inner[i+1])
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
						// characters and let the rest of the string render as-is.
						result.WriteByte(inner[i])
						i++
						continue
					}
					interp := inner[i+2 : j-1]
					val, _ := r.evaluateExpr(strings.TrimSpace(interp))
					result.WriteString(val)
					i = j
					continue
				}
				result.WriteByte(inner[i])
				i++
			}
			return result.String(), nil
		}
	}

	if expr == "block" && r.inMixin {
		if len(r.callerBlock) > 0 {
			return "true", nil
		}
		return "false", nil
	}

	if len(expr) >= 2 && expr[0] == '[' && expr[len(expr)-1] == ']' {
		closeIdx := findMatchingCloseBracket(expr)
		if closeIdx != len(expr)-1 {
			goto CHECK_INDEX_OP
		}
		inner := expr[1 : len(expr)-1]
		parts := splitTopLevel(inner, ',')
		strs := make([]string, 0, len(parts))
		for _, p := range parts {
			v, _ := r.evaluateExpr(strings.TrimSpace(p))
			strs = append(strs, v)
		}
		return strings.Join(strs, ","), nil
	}
CHECK_INDEX_OP:

	if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
		return "", nil
	}

	if n, ok := parseJSNumber(expr); ok {
		return strconv.FormatFloat(n, 'f', -1, 64), nil
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
		return Sub(left, right), nil
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
		return Add(left, right), nil
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
		return Mul(left, right), nil
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
		return Div(left, right)
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
		return Mod(left, right)
	}

	if idx := findIndexOp(expr); idx >= 0 {
		objExpr := expr[:idx]
		keyExpr := expr[idx+1 : len(expr)-1]
		obj := r.evaluateExprRaw(strings.TrimSpace(objExpr))
		if obj == nil {
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

		if bracketIdx := findIndexOp(objExpr); bracketIdx >= 0 {
			baseExpr := objExpr[:bracketIdx]
			keyExpr := objExpr[bracketIdx+1 : len(objExpr)-1]
			base, ok := r.lookup(strings.TrimSpace(baseExpr))
			if !ok {
				return "", nil
			}
			key, _ := r.evaluateExpr(keyExpr)
			result := r.indexValue(base, key)
			if result == nil {
				return "", nil
			}
			rv := reflect.ValueOf(result)
			if rest != "" {
				if rv.Kind() == reflect.Map {
					fv := rv.MapIndex(reflect.ValueOf(rest))
					if fv.IsValid() {
						return fmt.Sprintf("%v", fv.Interface()), nil
					}
				} else if rv.Kind() == reflect.Struct {
					if sf, ok := resolveStructField(rv.Type(), rest); ok {
						if fv := rv.FieldByName(sf.Name); fv.IsValid() {
							return fmt.Sprintf("%v", fv.Interface()), nil
						}
					}
				}
			}
			return fmt.Sprintf("%v", result), nil
		}

		methodName := rest
		argsStr := ""
		if before, inner, found := strings.Cut(rest, "("); found {
			methodName = before
			argsStr, _, _ = strings.Cut(strings.TrimSpace(inner), ")")
			argsStr = strings.TrimSpace(argsStr)
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
					return MethodRepeat(objVal, n), nil
				}
			}
			return objVal, nil

		case "split":
			sep := ""
			if argsStr != "" {
				sep, _ = r.evaluateExpr(argsStr)
			}
			return MethodSplit(objVal, sep), nil

		case "join":
			sep := ""
			if argsStr != "" {
				sep, _ = r.evaluateExpr(argsStr)
			}
			sep = UnquoteArg(sep)
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
					return MethodReplace(objVal, oldArg, newArg), nil
				}
			}
			return objVal, nil

		case "slice":
			if argsStr != "" {
				commaIdx := findBinaryOp(argsStr, ",")
				if commaIdx > 0 {
					startStr, _ := r.evaluateExpr(strings.TrimSpace(argsStr[:commaIdx]))
					endStr, _ := r.evaluateExpr(strings.TrimSpace(argsStr[commaIdx+1:]))
					return MethodSlice2(objVal, startStr, endStr), nil
				}
				startStr, _ := r.evaluateExpr(argsStr)
				return MethodSlice1(objVal, startStr), nil
			}
			return objVal, nil

		case "indexOf":
			if argsStr != "" {
				needle, _ := r.evaluateExpr(argsStr)
				return MethodIndexOf(objVal, needle), nil
			}
			return "-1", nil

		case "includes", "contains":
			if argsStr != "" {
				needle, _ := r.evaluateExpr(argsStr)
				return MethodIncludes(objVal, needle), nil
			}
			return "false", nil

		case "startsWith":
			if argsStr != "" {
				prefix, _ := r.evaluateExpr(argsStr)
				return MethodStartsWith(objVal, prefix), nil
			}
			return "false", nil

		case "endsWith":
			if argsStr != "" {
				suffix, _ := r.evaluateExpr(argsStr)
				return MethodEndsWith(objVal, suffix), nil
			}
			return "false", nil

		case "toString", "String":
			return objVal, nil

		case "toFixed":
			prec := ""
			if argsStr != "" {
				prec, _ = r.evaluateExpr(argsStr)
			}
			if rawObj := r.evaluateExprRaw(objExpr); rawObj != nil {
				rv := reflect.ValueOf(rawObj)
				var f float64
				switch rv.Kind() {
				case reflect.Float32, reflect.Float64:
					f = rv.Float()
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					f = float64(rv.Int())
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					f = float64(rv.Uint())
				default:
					return ToFixedStr(objVal, prec)
				}
				return ToFixed(f, prec), nil
			}
			return ToFixedStr(objVal, prec)

		case "toPrecision":
			prec := ""
			if argsStr != "" {
				prec, _ = r.evaluateExpr(argsStr)
			}
			if rawObj := r.evaluateExprRaw(objExpr); rawObj != nil {
				rv := reflect.ValueOf(rawObj)
				var f float64
				switch rv.Kind() {
				case reflect.Float32, reflect.Float64:
					f = rv.Float()
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					f = float64(rv.Int())
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					f = float64(rv.Uint())
				default:
					return ToPrecisionStr(objVal, prec)
				}
				return ToPrecision(f, prec), nil
			}
			return ToPrecisionStr(objVal, prec)

		case "padStart":
			if argsStr != "" {
				commaIdx := findBinaryOp(argsStr, ",")
				if commaIdx > 0 {
					lenStr, _ := r.evaluateExpr(strings.TrimSpace(argsStr[:commaIdx]))
					ch, _ := r.evaluateExpr(strings.TrimSpace(argsStr[commaIdx+1:]))
					return MethodPadStart(objVal, lenStr, ch), nil
				}
				lenStr, _ := r.evaluateExpr(argsStr)
				return MethodPadStart(objVal, lenStr, ""), nil
			}
			return objVal, nil

		case "padEnd":
			if argsStr != "" {
				commaIdx := findBinaryOp(argsStr, ",")
				if commaIdx > 0 {
					lenStr, _ := r.evaluateExpr(strings.TrimSpace(argsStr[:commaIdx]))
					ch, _ := r.evaluateExpr(strings.TrimSpace(argsStr[commaIdx+1:]))
					return MethodPadEnd(objVal, lenStr, ch), nil
				}
				lenStr, _ := r.evaluateExpr(argsStr)
				return MethodPadEnd(objVal, lenStr, ""), nil
			}
			return objVal, nil
		}

		// If the switch above did not match, the method call is unsupported.
		// Return an error so the failure is visible rather than silently returning "".
		// Only fire when rest contains "(" (i.e. it is a method call, not a property
		// access) and the receiver variable actually exists in scope.
		if strings.Contains(rest, "(") {
			if rawObj := r.evaluateExprRaw(objExpr); rawObj != nil {
				rv := reflect.ValueOf(rawObj)
				switch rv.Kind() {
				case reflect.Float32, reflect.Float64,
					reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					return "", fmt.Errorf("unsupported number method: %q", methodName)
				case reflect.String:
					return "", fmt.Errorf("unsupported string method: %q", methodName)
				}
			}
		}
	}

	return r.lookupAndStringify(expr), nil
}

// lookupAndStringify resolves expr (a plain variable name or dot-path) via
// lookup and stringifies the result exactly as evaluateExpr's fallback path
// does: a missing variable or nil value renders as "", a float64 uses
// FormatFloat (matching the rest of the interpreter's number formatting),
// and everything else falls back to fmt.Sprintf("%v", ...). It's factored
// out so the closure-compiled identifier/dot-path shapes in expr_compile.go
// can call the exact same resolution/stringification code evaluateExpr
// uses, rather than reimplementing it.
//
// string, bool, and the sized int/uint kinds get dedicated strconv
// fast-paths ahead of the Sprintf fallback, since this is the single
// value-to-string site every buffered output on the render hot path funnels
// through. Each fast-path case is chosen to be byte-identical to what
// fmt.Sprintf("%v", val) would produce for that exact static type; a value
// whose type is merely string/int/etc.-shaped but implements fmt.Stringer or
// error (or is a distinct named type) does not match these cases and still
// falls through to Sprintf, so its String()/Error() method is honored.
func (r *Runtime) lookupAndStringify(expr string) string {
	val, ok := r.lookup(expr)
	if !ok || val == nil {
		return ""
	}
	switch t := val.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int8:
		return strconv.FormatInt(int64(t), 10)
	case int16:
		return strconv.FormatInt(int64(t), 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint:
		return strconv.FormatUint(uint64(t), 10)
	case uint8:
		return strconv.FormatUint(uint64(t), 10)
	case uint16:
		return strconv.FormatUint(uint64(t), 10)
	case uint32:
		return strconv.FormatUint(uint64(t), 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	default:
		return fmt.Sprintf("%v", val)
	}
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

// containsParenExpr reports whether expr contains a parenthesised
// sub-expression that includes an operator (e.g. `card (isActive ? "x" : "y")`).
// This is used for the class attribute where static class names may be followed
// by a parenthesised ternary/logical expression.
func containsParenExpr(expr string) bool {
	inDouble := false
	inSingle := false
	depth := 0
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
		if ch == '(' {
			depth++
			if depth == 1 {
				// Scan ahead inside this paren group for operators.
				for j := i + 1; j < len(expr); j++ {
					c2 := expr[j]
					if c2 == '\\' && (inDouble || inSingle) {
						j++
						continue
					}
					if c2 == '"' && !inSingle {
						inDouble = !inDouble
						continue
					}
					if c2 == '\'' && !inDouble {
						inSingle = !inSingle
						continue
					}
					if inDouble || inSingle {
						continue
					}
					if c2 == '?' || c2 == '+' {
						return true
					}
					if c2 == '|' && j+1 < len(expr) && expr[j+1] == '|' {
						return true
					}
					if c2 == '&' && j+1 < len(expr) && expr[j+1] == '&' {
						return true
					}
					if c2 == ')' {
						break
					}
				}
			}
		} else if ch == ')' {
			if depth > 0 {
				depth--
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

// findMatchingCloseBracket finds the index of the closing bracket that matches
// the opening bracket at position 0. Returns -1 if no matching bracket is found.
func findMatchingCloseBracket(expr string) int {
	if len(expr) < 2 || expr[0] != '[' {
		return -1
	}
	depth := 0
	inDouble := false
	inSingle := false
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if ch == '\\' && (inDouble || inSingle) {
			i++ // skip next char
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
		} else if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if !inDouble && !inSingle {
			if ch == '[' {
				depth++
			} else if ch == ']' {
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}
	return -1
}

func (r *Runtime) indexValue(obj any, key string) any {
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

// parseJSNumber parses token as a numeric literal using the same grammar
// pug's sloppy-mode JS compiles expressions against: "0x"/"0X", "0o"/"0O",
// and "0b"/"0B" radix prefixes; legacy octal for a leading zero followed by
// one or more further digits that are all "0"-"7" (e.g. "0100" is 64, not
// 100); "NonOctalDecimal" for the same shape when any of those further
// digits is "8" or "9" (e.g. "08" is 8, "0118" is 118); and ordinary
// decimal/float/exponent forms otherwise (delegated to strconv.ParseFloat).
// A leading "+" or "-" sign is accepted and applied to the parsed magnitude.
//
// It returns ok=false for anything the JS grammar does not accept as a
// number literal: an underscore digit separator (Go's ParseFloat accepts
// "1_000", pug does not, so every such token is rejected outright), a
// malformed radix literal (no digits after the prefix, or a digit outside
// the radix), and an octal-looking leading-zero integer prefix (every digit
// "0"-"7") immediately followed by "." or "e"/"E" (e.g. "00.5", "017.5",
// "01e2" — all SyntaxErrors in pug, since a legacy octal integer literal has
// no fractional/exponent form). A leading-zero prefix that instead contains
// an "8" or "9" is a NonOctalDecimalIntegerLiteral, so a following "." or
// exponent is a valid, ordinary DecimalLiteral and is accepted (e.g.
// "08.5" is 8.5, "08e2" is 800). A single leading "0" directly followed by
// "." or "e"/"E" is likewise an ordinary decimal float ("0.5", "0e0") and is
// accepted.
//
// This only recognizes literal tokens — the octal/NonOctalDecimal rules are
// a property of JS source-code number syntax, not of runtime string-to-
// number coercion (JS `Number("0100")` is decimal 100), so callers must not
// route arbitrary data values through this function.
//
// Two narrow gaps versus real JS are accepted as out of scope: a radix
// literal whose magnitude overflows uint64 (e.g. a "0x" token for 2^64 or
// larger) is rejected here rather than parsed as the float JS would produce,
// and the returned float64 is stringified elsewhere with
// strconv.FormatFloat's 'f' verb, which never switches to exponential
// notation the way JS does for very large magnitudes (e.g. "1e21" stringifies
// as "1000000000000000000000", not JS's "1e+21") — that stringification
// behavior predates this function and is unchanged here.
func parseJSNumber(token string) (float64, bool) {
	if token == "" || strings.ContainsRune(token, '_') {
		return 0, false
	}

	s := token
	neg := false
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}

	mag, ok := parseJSNumberMagnitude(s)
	if !ok {
		return 0, false
	}
	if neg {
		mag = -mag
	}
	return mag, true
}

// parseJSNumberMagnitude parses the unsigned digit portion of a JS numeric
// literal (i.e. token with any leading "+"/"-" already stripped by the
// caller). See parseJSNumber for the grammar.
func parseJSNumberMagnitude(s string) (float64, bool) {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return parseRadixMagnitude(s[2:], 16)
	}
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'o' || s[1] == 'O') {
		return parseRadixMagnitude(s[2:], 8)
	}
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'b' || s[1] == 'B') {
		return parseRadixMagnitude(s[2:], 2)
	}

	if s[0] == '0' && len(s) >= 2 && isDigit(s[1]) {
		i := 1
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		digits := s[:i]
		rest := s[i:]

		allOctal := true
		for j := 0; j < len(digits); j++ {
			if digits[j] > '7' {
				allOctal = false
				break
			}
		}

		if rest != "" {
			if allOctal {
				// An octal-looking leading-zero integer prefix (every digit
				// is 0-7) directly followed by "." or an exponent marker is
				// not valid JS number syntax (e.g. "00.5", "017.5", "01e2")
				// -- legacy octal integer literals have no fractional or
				// exponent form.
				return 0, false
			}
			// A leading-zero integer prefix containing an 8 or 9 is not
			// octal -- JS reads it as an ordinary (if oddly-spelled)
			// DecimalIntegerLiteral, so a following "." or exponent makes
			// the whole token a normal DecimalLiteral, e.g. "08.5" -> 8.5,
			// "08e2" -> 800.
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return 0, false
			}
			return f, true
		}

		base := 10
		if allOctal {
			base = 8
		}
		n, err := strconv.ParseUint(digits, base, 64)
		if err != nil {
			return 0, false
		}
		return float64(n), true
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// parseRadixMagnitude parses digits (with no radix prefix) as an unsigned
// integer in the given base, rejecting an empty digit run or any digit
// outside the base's range.
func parseRadixMagnitude(digits string, base int) (float64, bool) {
	if digits == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return 0, false
	}
	return float64(n), true
}

// lookup retrieves a value by name from the scope stack (innermost first),
// falling back to globals. Dot notation ("user.name") is resolved by walking
// the chain with getField.
//
// The key is split on "." without allocating a []string: a bare identifier
// (no dot) resolves directly, and a dotted path is walked segment-by-segment
// with strings.IndexByte. Every segment (including the root) is trimmed with
// strings.TrimSpace, matching what strings.Split(key, ".") plus a per-part
// TrimSpace would have produced, degenerate inputs (leading/trailing/doubled
// dots) included.
func (r *Runtime) lookup(key string) (any, bool) {
	dot := strings.IndexByte(key, '.')

	var root string
	if dot < 0 {
		root = strings.TrimSpace(key)
	} else {
		root = strings.TrimSpace(key[:dot])
	}

	var rootVal any
	found := false
	for i := len(r.scopeStack) - 1; i >= 0; i-- {
		frame := r.scopeStack[i]
		if frame == nil {
			continue
		}
		// Hard mixin scope boundary: stop descending into caller frames.
		if _, isBoundary := frame["\x00mixin_boundary"]; isBoundary {
			break
		}
		if val, ok := frame[root]; ok {
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

	if dot < 0 {
		return rootVal, true
	}

	current := rootVal
	rest := key[dot+1:]
	for {
		next := strings.IndexByte(rest, '.')
		var part string
		if next < 0 {
			part = strings.TrimSpace(rest)
		} else {
			part = strings.TrimSpace(rest[:next])
		}

		current = r.getField(current, part)
		if current == nil {
			return nil, false
		}

		if next < 0 {
			break
		}
		rest = rest[next+1:]
	}

	return current, true
}

// resolveStructField maps a Pug identifier to an EXPORTED Go struct field of
// t (a struct type, already pointer-dereferenced), mirroring encoding/json's
// field-matching precedence so that lowercase Pug locals and reserved-word
// or snake_case identifiers can bind to exported Go fields:
//  1. An exact field name — reflect.Type.FieldByName(name). This is the fast
//     path: the common case (an already-matching field, or a map, which
//     never reaches this helper) costs nothing beyond the single call. If
//     the exact match is unexported, it is not returned — resolution falls
//     through to the loop below instead (an unexported field can still be
//     found there via a tag or a case-insensitive match on a DIFFERENT,
//     exported field).
//  2. A `pug:"name"` struct tag — an explicit escape hatch for any mismatch.
//     A blank or "-" tag is never matched.
//  3. A case-insensitive name match — handles the common lowercase-Pug-local
//     to PascalCase-Go-field case, including Go initialisms (id → ID,
//     url → URL). The first case-insensitive match in struct declaration
//     order wins if no tag matches.
//
// An unexported field is never returned by any tier: reflect.Value.Interface
// panics on a value obtained from an unexported field, and an unexported
// field is unreachable from generated code living in a separate package
// anyway, so treating it as a miss (the Pug identifier renders "", exactly
// like any other unresolvable field) is the only safe behavior — matching
// encoding/json, which likewise never matches unexported fields.
//
// Both the interpreter's field access and the codegen backend's struct-field
// resolution call this single helper so the two engines can never diverge on
// how a Pug identifier maps onto a Go struct field.
func resolveStructField(t reflect.Type, name string) (reflect.StructField, bool) {
	if sf, ok := t.FieldByName(name); ok && sf.IsExported() {
		return sf, true
	}

	var ciMatch reflect.StructField
	haveCI := false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if tag := f.Tag.Get("pug"); tag != "" && tag != "-" && tag == name {
			return f, true
		}
		if !haveCI && strings.EqualFold(f.Name, name) {
			ciMatch = f
			haveCI = true
		}
	}
	if haveCI {
		return ciMatch, true
	}
	return reflect.StructField{}, false
}

func (r *Runtime) getField(obj any, field string) any {
	if obj == nil {
		return nil
	}

	v := reflect.ValueOf(obj)

	// Dereference a pointer-to-struct (or pointer-to-map) so that field
	// access works whether the caller passes T or *T as data.
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	if v.Kind() == reflect.Map {
		val := v.MapIndex(reflect.ValueOf(field))
		if val.IsValid() {
			return val.Interface()
		}
	} else if v.Kind() == reflect.Struct {
		if sf, ok := resolveStructField(v.Type(), field); ok {
			fieldVal := v.FieldByName(sf.Name)
			if fieldVal.IsValid() {
				// Dereference pointer-typed fields: nil pointer → nil so that
				// isTruthy returns false and || / ternary fallbacks are reachable;
				// non-nil pointer → the pointed-to value so it renders correctly.
				if fieldVal.Kind() == reflect.Ptr {
					if fieldVal.IsNil() {
						return nil
					}
					return fieldVal.Elem().Interface()
				}
				return fieldVal.Interface()
			}
		}
	}

	return nil
}

func (r *Runtime) toSlice(val any) []any {
	if val == nil {
		return []any{}
	}

	v := reflect.ValueOf(val)
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		result := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = v.Index(i).Interface()
		}
		return result
	}

	if v.Kind() == reflect.Map {
		result := make([]any, 0)
		for _, key := range v.MapKeys() {
			result = append(result, v.MapIndex(key).Interface())
		}
		return result
	}

	return []any{val}
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
		key, val, found := strings.Cut(pair, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
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

// Truthy reports whether val — an already-stringified expression result —
// is truthy under the interpreter's rules: false for exactly "", "false",
// "0", "null", "undefined", and "nil"; true for every other string. This is
// the single source of truth for that falsy set: Runtime.isTruthy is a thin
// wrapper around it, and it is exported so codegen-generated code can call
// it directly for a string-typed field/operand's truthiness, keeping that
// quirky falsy set single-sourced rather than duplicated (and risking
// diverging) in generated code.
func Truthy(val string) bool {
	switch val {
	case "", "false", "0", "null", "undefined", "nil":
		return false
	}
	return true
}

func (r *Runtime) isTruthy(val string) bool {
	return Truthy(val)
}

// htmlBooleanAttributes is the set of HTML "boolean" attributes — their mere
// presence is the value, so a falsy value omits the whole attribute (mirroring
// pugjs's omit-on-false behaviour for these). For every other attribute name a
// value of "false" is a legitimate string and is rendered literally
// (e.g. data-confirm-dialogs="false"). Names are compared lowercased.
var htmlBooleanAttributes = map[string]bool{
	"allowfullscreen": true,
	"async":           true,
	"autofocus":       true,
	"autoplay":        true,
	"checked":         true,
	"controls":        true,
	"default":         true,
	"defer":           true,
	"disabled":        true,
	"formnovalidate":  true,
	"hidden":          true,
	"inert":           true,
	"ismap":           true,
	"itemscope":       true,
	"loop":            true,
	"multiple":        true,
	"muted":           true,
	"nomodule":        true,
	"novalidate":      true,
	"open":            true,
	"playsinline":     true,
	"readonly":        true,
	"required":        true,
	"reversed":        true,
	"selected":        true,
}

// isBooleanAttribute reports whether name is an HTML boolean attribute, for
// which a falsy value omits the attribute entirely.
func isBooleanAttribute(name string) bool {
	return htmlBooleanAttributes[strings.ToLower(name)]
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
