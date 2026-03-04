package gopug

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	prettyDepth    int                       // current indentation depth for pretty-print mode
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
		prettyDepth:   0,
	}
	if opts != nil && opts.Basedir != "" {
		r.basedir = opts.Basedir
	}
	return r
}

// pretty returns true when pretty-print mode is enabled.
func (r *Runtime) pretty() bool {
	return r.opts != nil && r.opts.Pretty
}

// prettyNewline writes a newline + indentation when pretty-print is on.
// Does nothing in compact mode.
func (r *Runtime) prettyNewline() {
	if !r.pretty() {
		return
	}
	r.buf.WriteByte('\n')
	for i := 0; i < r.prettyDepth; i++ {
		r.buf.WriteString("  ")
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
	case *TagInterpolationNode:
		return r.renderTagInterpolation(n)
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

// renderTagInterpolation renders an inline #[tag content] interpolation.
func (r *Runtime) renderTagInterpolation(n *TagInterpolationNode) error {
	return r.renderTag(n.Tag)
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

// writeNewlineAfterDoctype writes a newline after the doctype declaration when
// pretty-print is enabled, called by Render() before the first node walk.
func (r *Runtime) writeNewlineAfterDoctype(nodes []Node) {
	if !r.pretty() {
		return
	}
	for _, n := range nodes {
		if _, ok := n.(*DoctypeNode); ok {
			r.buf.WriteByte('\n')
			return
		}
	}
}

// renderTag renders an HTML tag and its children.
func (r *Runtime) renderTag(tag *TagNode) error {
	// Pretty-print: emit newline+indent before block-level tags (not inside
	// an inline tag context — the caller manages that via prettyDepth).
	if r.pretty() && !prettyInline(tag) {
		r.prettyNewline()
	}

	// Write opening tag
	r.buf.WriteString("<")
	r.buf.WriteString(tag.Name)

	// Build the final attribute map, resolving &attributes merges first.
	// We use a slice of (name, value, unescaped) to preserve a stable sort
	// order (id first, class second, then alphabetical) for deterministic output.
	type attrEntry struct {
		name      string
		value     string
		unescaped bool
		boolean   bool // no value, just the name
	}

	// Start with a copy of tag.Attributes, expanding &attributes spreads.
	// Pass 1: copy all non-&attributes entries first so that class merging
	// below can append to any shorthand classes already on the tag.
	merged := make(map[string]*AttributeValue)
	for k, v := range tag.Attributes {
		if k != "&attributes" {
			merged[k] = v
		}
	}

	// Pass 2: expand &attributes spreads and merge into the map.
	for k, v := range tag.Attributes {
		if k != "&attributes" {
			continue
		}

		expr := strings.TrimSpace(v.Value)

		// Build a flat map[string]interface{} from the spread expression.
		// Supported forms:
		//   1. A variable name resolving to map[string]interface{}
		//   2. An inline object literal {key: val, ...}
		spreadMap := map[string]interface{}{}

		if raw, ok := r.lookup(expr); ok && raw != nil {
			rv := reflect.ValueOf(raw)
			if rv.Kind() == reflect.Map {
				for _, mk := range rv.MapKeys() {
					spreadMap[fmt.Sprintf("%v", mk.Interface())] = rv.MapIndex(mk).Interface()
				}
			}
		} else if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
			// Inline object literal — parse key/value pairs.
			// parseInlineObject already strips quotes from both keys and values,
			// so values are plain strings (e.g. "Edit" not "\"Edit\"").
			// We still evaluate non-quoted tokens so variable references work.
			obj := parseInlineObject(expr)
			for key, val := range obj {
				spreadMap[key] = val
			}
		}

		// Merge the spread map into the merged attribute map.
		for attrKey, attrVal := range spreadMap {
			valStr := fmt.Sprintf("%v", attrVal)

			switch attrKey {
			case "class":
				// Merge: append spread class to any existing class value.
				if existing, ok := merged["class"]; ok {
					existingVal := strings.Trim(existing.Value, `"`)
					merged["class"] = &AttributeValue{Value: `"` + existingVal + " " + valStr + `"`}
				} else {
					merged["class"] = &AttributeValue{Value: `"` + valStr + `"`}
				}
			default:
				// Boolean detection: true → boolean attr, false → suppress.
				if valStr == "true" {
					merged[attrKey] = &AttributeValue{Boolean: true}
				} else if valStr == "false" {
					delete(merged, attrKey)
				} else {
					merged[attrKey] = &AttributeValue{Value: `"` + valStr + `"`}
				}
			}
		}
	}

	// Collect and sort attribute names for deterministic output:
	// id and class come first (in that order), then the rest alphabetically.
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

		// Evaluate the attribute value first so we can check for boolean
		// suppression before emitting anything.
		evaluated := ""
		if val.Value != "" {
			rawValExpr := strings.TrimSpace(val.Value)

			// style={color:'red', background:'green'} — convert object to CSS string
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
				// class={active:true, disabled:false} — inline object literal
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
					// class may be a variable holding a slice, map, or string.
					// The parser may also have produced a merged string like
					// `"bang" classes` (shorthand + variable). We need to split
					// on spaces, evaluate each token, and rejoin.
					//
					// However, if the expression contains top-level operators
					// (ternary ?, logical ||/&&, comparisons, concatenation +)
					// evaluate it as a whole first — word-splitting would break it.
					if isOperatorExpr(rawValExpr) {
						evaluated, _ = r.evaluateExpr(rawValExpr)
					} else {
						raw := r.evaluateExprRaw(rawValExpr)
						if raw != nil {
							rv := reflect.ValueOf(raw)
							switch rv.Kind() {
							case reflect.Slice, reflect.Array:
								// []string or []interface{} → join with spaces
								parts := make([]string, rv.Len())
								for i := 0; i < rv.Len(); i++ {
									parts[i] = fmt.Sprintf("%v", rv.Index(i).Interface())
								}
								evaluated = strings.Join(parts, " ")
							case reflect.Map:
								// map[string]interface{}{active:true} → truthy keys
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
								// String or other scalar. The parser may have merged a
								// shorthand literal with a variable name by producing a
								// quoted string like `"bang classes"` where "bang" is a
								// literal class and "classes" is a variable reference.
								// Strategy:
								//  1. If rawValExpr is a quoted string, unquote it and
								//     treat each space-separated word as either a literal
								//     (quoted) or a variable to resolve.
								//  2. Otherwise split on spaces and resolve each token.
								inner := rawValExpr
								if len(inner) >= 2 &&
									((inner[0] == '"' && inner[len(inner)-1] == '"') ||
										(inner[0] == '\'' && inner[len(inner)-1] == '\'')) {
									inner = inner[1 : len(inner)-1]
								}
								words := strings.Fields(inner)
								var resolved []string
								for _, word := range words {
									// Try as a variable first; if it resolves use it,
									// else use the word verbatim as a literal class name.
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
							// nil raw — the value is likely a quoted merged string.
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

		// Boolean suppression: if the raw attribute value is a variable
		// expression (not a quoted string literal) and it evaluates to
		// "false", omit the attribute entirely — matching Pug behaviour.
		if evaluated == "false" {
			rawVal := strings.TrimSpace(val.Value)
			isQuoted := len(rawVal) >= 2 &&
				((rawVal[0] == '"' && rawVal[len(rawVal)-1] == '"') ||
					(rawVal[0] == '\'' && rawVal[len(rawVal)-1] == '\''))
			if !isQuoted {
				continue
			}
		}

		r.buf.WriteString(" ")
		r.buf.WriteString(name)

		// Boolean attributes (bare `checked`, `disabled`, etc.) have no value.
		// Only omit the value when the attribute was explicitly marked as Boolean
		// by the parser — NOT when the expression happens to equal the attr name
		// (e.g. `href=href` must still emit the evaluated value).
		if !val.Boolean && val.Value != "" {
			// Escape unless marked as unescaped
			if !val.Unescaped {
				evaluated = html.EscapeString(evaluated)
			}

			r.buf.WriteString("=")
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

	// Pretty-print: indent children of block-level tags
	isInline := prettyInline(tag)
	if r.pretty() && !isInline {
		r.prettyDepth++
	}

	// Render children
	for _, child := range tag.Children {
		if err := r.renderNode(child); err != nil {
			return err
		}
	}

	// Pretty-print: close indent, newline before closing tag
	if r.pretty() && !isInline {
		r.prettyDepth--
		r.prettyNewline()
	}

	// Write closing tag
	r.buf.WriteString("</")
	r.buf.WriteString(tag.Name)
	r.buf.WriteString(">")

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
			// Pass through if this looks like a valid entity reference:
			//   named:   &word;   e.g. &copy; &amp; &nbsp;
			//   numeric: &#NNN;  or &#xHH;
			// Otherwise escape to &amp;.
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
		// Numeric entity: &#[x]digits;
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
	// Named entity: &[a-zA-Z][a-zA-Z0-9]*;
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

func isLetter(c byte) bool  { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func isAlphaNum(c byte) bool { return isLetter(c) || (c >= '0' && c <= '9') }

// renderText renders plain text (escaped by default).
func (r *Runtime) renderText(text *TextNode) error {
	r.buf.WriteString(htmlEscapeText(text.Content))
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
		r.prettyNewline()
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

	// Strip leading "var " / "let " / "const " keyword so that
	// "- var x = 3" works the same as "- x = 3".
	for _, kw := range []string{"var ", "let ", "const "} {
		if strings.HasPrefix(stmt, kw) {
			stmt = strings.TrimSpace(stmt[len(kw):])
			break
		}
	}

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
	// Strip optional surrounding parentheses: `if (cond)` → `if cond`
	condition := strings.TrimSpace(cond.Condition)
	if len(condition) >= 2 && condition[0] == '(' && condition[len(condition)-1] == ')' {
		condition = strings.TrimSpace(condition[1 : len(condition)-1])
	}

	// Evaluate the condition
	val, err := r.evaluateExpr(condition)
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
		// Fall back to raw expression evaluation so that method expressions like
		// csv.split(",") return a real []interface{} rather than a string.
		collVal = r.evaluateExprRaw(each.Collection)
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
	if r.pretty() {
		r.buf.WriteByte('\n')
	}
	return nil
}

// renderPipe renders piped text.
func (r *Runtime) renderPipe(pipe *PipeNode) error {
	r.prettyNewline()
	r.buf.WriteString(htmlEscapeText(pipe.Content))
	return nil
}

// renderBlockText renders block text (indented text after .).
// The content may contain #{...} / !{...} interpolations and #[tag] tag
// interpolations, so we process the tokens emitted by emitTextWithInterpolations
// directly rather than running the full parser (which would loop waiting for
// structural tokens that are never emitted for plain text content).
func (r *Runtime) renderBlockText(block *BlockTextNode) error {
	r.prettyNewline()

	// Use the lexer helper to split on interpolation markers and collect tokens.
	lx := NewLexer("")
	lx.emitTextWithInterpolations(block.Content, 0)

	for _, tok := range lx.tokens {
		switch tok.Type {
		case TokenText:
			r.buf.WriteString(htmlEscapeText(tok.Value))

		case TokenInterpolation:
			val, err := r.evaluateExpr(tok.Value)
			if err != nil {
				val = tok.Value
			}
			r.buf.WriteString(htmlEscapeText(val))

		case TokenInterpolationUnescape:
			val, err := r.evaluateExpr(tok.Value)
			if err != nil {
				val = tok.Value
			}
			r.buf.WriteString(val)

		case TokenTagInterpolationStart:
			// Parse the inner content as a mini tag and render it.
			innerLex := NewLexer(tok.Value)
			if _, err := innerLex.Lex(); err != nil {
				r.buf.WriteString(html.EscapeString(tok.Value))
				continue
			}
			innerParser := NewParser(innerLex.tokens)
			innerAST, err := innerParser.Parse()
			if err != nil || innerAST == nil || len(innerAST.Children) == 0 {
				r.buf.WriteString(html.EscapeString(tok.Value))
				continue
			}
			for _, node := range innerAST.Children {
				if err := r.renderNode(node); err != nil {
					return err
				}
			}

		case TokenTagInterpolationEnd:
			// already consumed by the Start case above — skip
		}
	}

	return nil
}

// renderLiteralHTML renders literal HTML (line starting with <).
func (r *Runtime) renderLiteralHTML(lit *LiteralHTMLNode) error {
	r.prettyNewline()
	r.buf.WriteString(lit.Content)
	return nil
}

// renderBlockExpansion renders block expansion (tag: child).
func (r *Runtime) renderBlockExpansion(exp *BlockExpansionNode) error {
	// Attach the child as a child of the parent tag so it is rendered nested
	// inside the parent's opening and closing tags, not as a sibling.
	exp.Parent.Children = append(exp.Parent.Children, exp.Child)
	return r.renderTag(exp.Parent)
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
		result, err := fn(string(raw), make(map[string]string))
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
			// Store as the evaluated string so that `href=href` inside the
			// mixin body resolves the param variable correctly.
			val, err := r.evaluateExpr(call.Arguments[i])
			if err != nil {
				return err
			}
			scope[param] = val
		} else if decl.DefaultValues != nil {
			if defaultExpr, ok := decl.DefaultValues[param]; ok {
				// Evaluate the default expression in the caller scope.
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

	// Always expose an "attributes" variable inside the mixin — even when
	// no HTML attributes were passed (empty map).  This lets `attributes.class`
	// resolve to "" rather than causing a lookup failure.
	attrMap := make(map[string]interface{})
	for k, v := range call.Attributes {
		var evaluated string
		if v.Boolean {
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

	// Normalise the options map: always non-nil so filter functions don't
	// need to guard against a nil map.
	options := filter.Options
	if options == nil {
		options = make(map[string]string)
	}

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
	// Options are only forwarded to the outermost (last) filter in the chain;
	// inner (subfilter) steps receive an empty options map because the
	// syntax only allows a single options block on the outermost name.
	for i, name := range chain {
		fn, ok := r.lookupFilter(name)
		if !ok {
			return fmt.Errorf("filter %q is not registered; register it via Options.Filters", name)
		}
		stepOpts := make(map[string]string)
		if i == len(chain)-1 {
			// Outermost filter receives the parsed options.
			stepOpts = options
		}
		result, err := fn(content, stepOpts)
		if err != nil {
			return fmt.Errorf("filter %q error: %w", name, err)
		}
		content = result
	}

	// Filter output is treated as trusted/raw — the filter function is
	// responsible for HTML-escaping its own output (just like Pug.js).
	// If the output contains newlines, strip a single trailing newline (a
	// common artifact of text-processing filters) and replace the remaining
	// interior newlines with <br> so browsers preserve the visual line breaks
	// without forcing monospace <pre> formatting.
	content = strings.TrimRight(content, "\n")
	if strings.Contains(content, "\n") {
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 {
				r.buf.WriteString("<br>")
			}
			r.buf.WriteString(line)
		}
	} else {
		r.buf.WriteString(content)
	}
	return nil
}

// lookupFilter finds a filter function by name. It checks Options.Filters.
func (r *Runtime) lookupFilter(name string) (FilterFunc, bool) {
	if r.opts != nil && r.opts.Filters != nil {
		if fn, ok := r.opts.Filters[name]; ok {
			return fn, true
		}
	}
	return nil, false
}

// evaluateExpr evaluates a simple expression against the current scope.
// evaluateExprRaw evaluates an expression and returns a raw interface{} value.
// This is used when the caller needs a real Go slice/map rather than a string
// (e.g. the collection expression in an each loop).  For most expressions it
// delegates to evaluateExpr and returns the string; for method expressions that
// produce slices (split) it returns the actual []interface{}.
func (r *Runtime) evaluateExprRaw(expr string) interface{} {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}

	// Method call producing a slice: obj.split(sep)
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
			// Resolve the object as a string.
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

	// Inline array literal: [a, b, c] — return a real []interface{} slice
	// so that each/for loops can iterate it properly.
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

	// Simple variable lookup — return raw value so slices stay as slices.
	if val, ok := r.lookup(expr); ok {
		return val
	}

	// Fallback: string evaluation.
	s, _ := r.evaluateExpr(expr)
	return s
}

func (r *Runtime) evaluateExpr(expr string) (string, error) {
	expr = strings.TrimSpace(expr)

	if expr == "" {
		return "", nil
	}

	// Strip matching outer parentheses: (expr) → expr
	// This allows nested ternaries like (cond ? a : b) to be evaluated correctly
	// when they appear as a branch of an outer ternary or logical expression.
	if len(expr) >= 2 && expr[0] == '(' && expr[len(expr)-1] == ')' {
		depth := 0
		isWrapped := true
		for i, ch := range expr {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				// If depth hits 0 before the last character, the outer parens
				// don't wrap the whole expression (e.g. "(a) + (b)").
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

	// String literals (double or single quoted).
	// We only treat the expression as a single string literal when the opening
	// and closing quotes are a *matched* pair — i.e. the very first character
	// is a quote and the matching closing quote is at the very end with no
	// unescaped quote of the same kind in between at the top level.
	if len(expr) >= 2 {
		q := rune(expr[0])
		if q == '"' || q == '\'' {
			// Walk forward looking for the matching closing quote.
			// Use range to iterate over runes (not bytes) so that multi-byte
			// UTF-8 characters (e.g. …, emoji) are handled correctly.
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
			// Only treat as a string literal when the close quote is at the
			// very end of the expression (no trailing operators / identifiers).
			if closeIdx == len(expr)-1 {
				return expr[1 : len(expr)-1], nil
			}
		}
	}

	// Special mixin keyword: `block` evaluates to truthy when the mixin was
	// called with block content, falsy when called without.
	if expr == "block" && r.inMixinContext {
		if len(r.mixinBlock) > 0 {
			return "true", nil
		}
		return "false", nil
	}

	// Inline array literal: [a, b, c]
	// Evaluate to a joined string for output; evaluateExprRaw handles the
	// real slice case for each/iteration.
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

	// Inline object literal: {key: val, key2: val2}
	// For style={color:'red'} we render as CSS; for class={active:true} we
	// return space-joined truthy keys.  In a generic context just return "".
	if len(expr) >= 2 && expr[0] == '{' && expr[len(expr)-1] == '}' {
		// Parsed by renderTag for special attributes; here return empty.
		return "", nil
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

	// Arithmetic operators — evaluated in ascending precedence order so that
	// lower-precedence operators are split first (correct left-associativity).
	//
	// Precedence (low → high):
	//   +  -   (additive)
	//   *  /  %  (multiplicative)
	//
	// We find the RIGHTMOST top-level occurrence of each additive operator so
	// that e.g. "1 - 2 - 3" splits as "(1-2)-3" not "1-(2-3)".
	// For multiplicative we do the same after additive has had a chance to split.

	// Subtraction: a - b
	// Use rightmost top-level '-' that is not a unary minus (i.e. has a
	// non-operator character to its left).
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

	// Addition / string concatenation: a + b
	// Use findBinaryOp so we don't split inside quoted strings or parens.
	if idx := findBinaryOp(expr, "+"); idx >= 0 {
		left, err := r.evaluateExpr(strings.TrimSpace(expr[:idx]))
		if err != nil {
			return "", err
		}
		right, err := r.evaluateExpr(strings.TrimSpace(expr[idx+1:]))
		if err != nil {
			return "", err
		}
		// If both sides look numeric, add them; otherwise concatenate as strings.
		lf, lok := toFloat(left)
		rf, rok := toFloat(right)
		if lok && rok {
			result := lf + rf
			return strconv.FormatFloat(result, 'f', -1, 64), nil
		}
		return left + right, nil
	}

	// Multiplication: a * b
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

	// Division: a / b
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

	// Modulo: a % b
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

	// Method / property access: expr.method() or expr.property
	// We check for a top-level dot that is NOT part of a number literal and
	// NOT inside parentheses/brackets/braces.  If found, evaluate the
	// left-hand side then apply the method/property on the right.
	if dotIdx := findTopLevelDot(expr); dotIdx > 0 {
		objExpr := expr[:dotIdx]
		rest := expr[dotIdx+1:] // everything after the dot

		objVal, err := r.evaluateExpr(objExpr)
		if err != nil {
			return "", err
		}

		// Determine method name and optional argument list.
		methodName := rest
		argsStr := ""
		if parenIdx := strings.Index(rest, "("); parenIdx >= 0 {
			methodName = rest[:parenIdx]
			// Extract arguments between the outermost parens.
			inner := rest[parenIdx+1:]
			if closeIdx := strings.LastIndex(inner, ")"); closeIdx >= 0 {
				argsStr = strings.TrimSpace(inner[:closeIdx])
			}
		}
		methodName = strings.TrimSpace(methodName)

		switch methodName {
		case "length":
			// If the raw object is a slice or array, return its element count.
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
			// split(sep) — when used in buffered code output, split the string
			// and rejoin with a single space so the result is a readable string.
			// When the result is needed as a real slice (e.g. for each or .join),
			// evaluateExprRaw returns the actual []interface{} instead.
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
			// join(sep) — joins a slice into a string using the given separator.
			// The receiver (objExpr) may be a simple variable OR a chained
			// expression such as words.split(" "), so we use evaluateExprRaw
			// to obtain the actual []interface{} slice rather than a lookup by
			// name alone.
			sep := ""
			if argsStr != "" {
				sep, _ = r.evaluateExpr(argsStr)
				if len(sep) >= 2 &&
					((sep[0] == '"' && sep[len(sep)-1] == '"') ||
						(sep[0] == '\'' && sep[len(sep)-1] == '\'')) {
					sep = sep[1 : len(sep)-1]
				}
			}
			// Try evaluateExprRaw first — handles both plain variables and
			// chained expressions like words.split(" ").
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
			// replace(old, new) — simple string replacement (first occurrence).
			if argsStr != "" {
				// Split on comma at top level.
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
			// slice(start[, end]) — substring by rune index.
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
				// Single argument — start only.
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
		// Unknown method — fall through to variable lookup below.
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

// isOperatorExpr reports whether expr contains a top-level operator that
// means it must be evaluated as a whole expression rather than split on
// spaces.  We check for: ternary (?), logical (||, &&), comparison
// (===, !==, ==, !=, <=, >=, <, >), and concatenation (+).
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

// findSubtraction finds the rightmost top-level '-' that is a binary subtraction
// operator (not a unary minus).  A '-' is considered unary when it appears at
// the start of the expression or is immediately preceded by another operator
// character (+, -, *, /, %, (, [, ,).
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
			// Determine whether this is a unary minus by looking at the previous
			// non-space character.
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
// operator character (*, /, %) that is not inside quotes or brackets.
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

// findTopLevelDot finds the position of a top-level dot (.) in an expression
// that represents method/property access (e.g. "name.toUpperCase()").
// It skips dots inside quoted strings, parentheses, brackets, and braces,
// and ignores dots that are part of numeric literals (e.g. "3.14").
// Returns the index of the dot, or -1 if not found.
// Only the LAST top-level dot is returned so that chained calls like
// "a.b.c" resolve right-to-left correctly.
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
			// Skip numeric literals like "3.14" — if the char before and after
			// are both digits, this dot is part of a number.
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

// splitTopLevel splits s on the given separator rune, but only at depth 0
// (not inside quotes, parens, brackets, or braces).
func splitTopLevel(s string, sep rune) []string {
	var parts []string
	depth := 0
	inDouble := false
	inSingle := false
	start := 0
	for i, ch := range s {
		switch {
		case ch == '\\' && (inDouble || inSingle):
			// skip next char — handled by range advancing bytes, good enough
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
		// Split on first colon
		colonIdx := strings.Index(pair, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(pair[:colonIdx])
		val := strings.TrimSpace(pair[colonIdx+1:])
		// Strip surrounding quotes from key (e.g. "aria-label" → aria-label)
		if len(key) >= 2 &&
			((key[0] == '"' && key[len(key)-1] == '"') ||
				(key[0] == '\'' && key[len(key)-1] == '\'')) {
			key = key[1 : len(key)-1]
		}
		// Strip surrounding quotes from value
		if len(val) >= 2 &&
			((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result
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
