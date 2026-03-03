package gopug

import (
	"fmt"
	"strings"
)

// Parser builds an AST from a token stream.
type Parser struct {
	tokens []Token
	pos    int
	cur    Token
}

// NewParser creates a new parser for the given tokens.
func NewParser(tokens []Token) *Parser {
	p := &Parser{
		tokens: tokens,
		pos:    0,
	}
	if len(tokens) > 0 {
		p.cur = tokens[0]
	}
	return p
}

// Parse parses the token stream and returns the document AST.
func (p *Parser) Parse() (*DocumentNode, error) {
	doc := &DocumentNode{
		Children: make([]Node, 0),
	}

	for p.cur.Type != TokenEOF {
		// Skip newlines at top level
		if p.cur.Type == TokenNewline {
			p.advance()
			continue
		}

		node, err := p.parseNode(0)
		if err != nil {
			return nil, err
		}
		if node != nil {
			doc.Children = append(doc.Children, node)
		}
	}

	return doc, nil
}

// parseNode parses a single node at the given indentation depth.
func (p *Parser) parseNode(expectedDepth int) (Node, error) {
	tok := p.cur

	// Enforce indentation
	if tok.Type != TokenEOF && tok.Type != TokenNewline && tok.Depth < expectedDepth {
		return nil, fmt.Errorf("unexpected dedent at line %d", tok.Line)
	}

	switch tok.Type {
	case TokenEOF:
		return nil, nil
	case TokenNewline:
		p.advance()
		return p.parseNode(expectedDepth)
	case TokenDoctype:
		return p.parseDoctype()
	case TokenComment, TokenCommentUnbuffered:
		return p.parseComment()
	case TokenCode:
		return p.parseUnbufferedCode()
	case TokenCodeBuffered:
		return p.parseBufferedCode()
	case TokenCodeUnescaped:
		return p.parseUnescapedCode()
	case TokenPipe:
		return p.parsePipedText()
	case TokenText:
		return p.parseTextNode()
	case TokenInterpolation:
		return p.parseInterpolationNode(false)
	case TokenInterpolationUnescape:
		return p.parseInterpolationNode(true)
	case TokenTagInterpolationStart:
		return p.parseTagInterpolation()
	case TokenHTMLLiteral:
		return p.parseLiteralHTML()
	case TokenTag, TokenClass, TokenID:
		return p.parseTag()
	case TokenMixinCall:
		return p.parseMixinCall()
	case TokenFilter:
		return p.parseFilter()
	case TokenIf:
		return p.parseConditional()
	case TokenUnless:
		return p.parseUnless()
	case TokenEach, TokenFor:
		return p.parseEach()
	case TokenWhile:
		return p.parseWhile()
	case TokenCase:
		return p.parseCase()
	case TokenMixin:
		return p.parseMixinDecl()
	case TokenBlock, TokenBlockAppend, TokenBlockPrepend:
		return p.parseBlock()
	case TokenAppend, TokenPrepend:
		return p.parseBlockModifier()
	case TokenExtends:
		return p.parseExtends()
	case TokenInclude:
		return p.parseInclude()
	default:
		return nil, fmt.Errorf("unexpected token %s at line %d", tokenTypeName(tok.Type), tok.Line)
	}
}

// parseTag parses a tag, attributes, classes, IDs, and children.
func (p *Parser) parseTag() (Node, error) {
	tag := &TagNode{
		Name:       "",
		Attributes: make(map[string]*AttributeValue),
		Children:   make([]Node, 0),
		Line:       p.cur.Line,
		Col:        p.cur.Col,
	}

	currentDepth := p.cur.Depth

	// Collect tag name
	if p.cur.Type == TokenTag {
		tag.Name = p.cur.Value
		p.advance()
	} else if p.cur.Type == TokenClass || p.cur.Type == TokenID {
		// Implicit div
		tag.Name = "div"
	}

	// Parse attributes, classes, IDs, and special characters
	for p.cur.Type == TokenClass || p.cur.Type == TokenID || p.cur.Type == TokenAttrStart || p.cur.Type == TokenDot || p.cur.Type == TokenColon || p.cur.Type == TokenTagEnd ||
		(p.cur.Type == TokenAttrName && p.cur.Value == "&attributes") {
		switch p.cur.Type {
		case TokenClass:
			className := p.cur.Value
			if existing, ok := tag.Attributes["class"]; ok {
				// Append to existing class list (strip surrounding quotes)
				existing.Value = `"` + existing.Value[1:len(existing.Value)-1] + " " + className + `"`
			} else {
				tag.Attributes["class"] = &AttributeValue{Value: `"` + className + `"`}
			}
			p.advance()

		case TokenID:
			idVal := p.cur.Value
			tag.Attributes["id"] = &AttributeValue{Value: `"` + idVal + `"`}
			p.advance()

		case TokenAttrStart:
			p.advance() // consume (
			if err := p.parseAttributes(tag); err != nil {
				return nil, err
			}

		case TokenAttrName:
			// &attributes(expr) emitted outside an AttrStart/AttrEnd block by the lexer.
			if p.cur.Value == "&attributes" {
				p.advance() // consume &attributes name
				// Consume the = token
				if p.cur.Type == TokenAttrEqual || p.cur.Type == TokenAttrEqualUnescape {
					p.advance()
				}
				// Consume the value (expression)
				if p.cur.Type == TokenAttrValue {
					tag.Attributes["&attributes"] = &AttributeValue{Value: p.cur.Value}
					p.advance()
				}
			} else {
				// Unknown bare AttrName outside attribute block — skip
				p.advance()
			}

		case TokenDot:
			p.advance()
			// Block text indicator
			p.skipNewlines()
			if p.cur.Depth > currentDepth {
				// Collect indented lines
				blockText := p.parseBlockText(currentDepth + 1)
				tag.Children = append(tag.Children, &BlockTextNode{Content: blockText, Line: p.cur.Line, Col: p.cur.Col})
			}
			return tag, nil

		case TokenColon:
			p.advance() // consume :
			// Block expansion: tag: child
			if p.cur.Type == TokenTag {
				childTag, err := p.parseTag()
				if err != nil {
					return nil, err
				}
				return &BlockExpansionNode{Parent: tag, Child: childTag, Line: tag.Line, Col: tag.Col}, nil
			}
			return nil, fmt.Errorf("expected tag after : at line %d", p.cur.Line)

		case TokenTagEnd:
			p.advance()
			tag.SelfClose = true

		default:
			break
		}
	}

	// Inline text — may be followed by interpolation tokens on the same line,
	// producing a mixed run: p Hello #{name} world
	if (p.cur.Type == TokenText || p.cur.Type == TokenInterpolation || p.cur.Type == TokenInterpolationUnescape) && p.cur.Depth == currentDepth {
		line := p.cur.Line
		col := p.cur.Col
		nodes := p.collectTextRun(currentDepth)
		p.skipNewlines()
		if len(nodes) == 1 {
			tag.Children = append(tag.Children, nodes[0])
		} else if len(nodes) > 1 {
			tag.Children = append(tag.Children, &TextRunNode{Nodes: nodes, Line: line, Col: col})
		}
		// Only fall through to the child-collection loop if there are tokens
		// at a deeper depth — otherwise return now to avoid consuming siblings.
		if p.cur.Depth <= currentDepth {
			return tag, nil
		}
	}

	// Inline buffered code: p= expr
	if p.cur.Type == TokenCodeBuffered && p.cur.Depth == currentDepth {
		code := &CodeNode{Expression: p.cur.Value, Type: CodeBuffered, Line: p.cur.Line, Col: p.cur.Col}
		p.advance()
		p.skipNewlines()
		tag.Children = append(tag.Children, code)
		return tag, nil
	}

	// Inline unescaped code: p!= expr
	if p.cur.Type == TokenCodeUnescaped && p.cur.Depth == currentDepth {
		code := &CodeNode{Expression: p.cur.Value, Type: CodeUnescaped, Line: p.cur.Line, Col: p.cur.Col}
		p.advance()
		p.skipNewlines()
		tag.Children = append(tag.Children, code)
		return tag, nil
	}

	p.skipNewlines()

	// Parse children
	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		child, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if child != nil {
			tag.Children = append(tag.Children, child)
		}
		p.skipNewlines()
	}

	return tag, nil
}

// parseAttributes parses tag attributes inside ( ... )
func (p *Parser) parseAttributes(tag *TagNode) error {
	for p.cur.Type != TokenAttrEnd && p.cur.Type != TokenEOF {
		if p.cur.Type == TokenAttrName {
			name := p.cur.Value
			p.advance()

			// Check for = or !=
			if p.cur.Type == TokenAttrEqual || p.cur.Type == TokenAttrEqualUnescape {
				unescaped := p.cur.Type == TokenAttrEqualUnescape
				p.advance()

				if p.cur.Type == TokenAttrValue {
					value := p.cur.Value
					// For the "class" attribute, merge with any existing value
					// (e.g. from a prior .shorthand) instead of overwriting it.
					if name == "class" {
						if existing, ok := tag.Attributes["class"]; ok {
							// Strip outer quotes from the existing value and the
							// new value, join them, then re-quote.
							existingInner := existing.Value
							if len(existingInner) >= 2 &&
								existingInner[0] == '"' &&
								existingInner[len(existingInner)-1] == '"' {
								existingInner = existingInner[1 : len(existingInner)-1]
							}
							newInner := value
							if len(newInner) >= 2 &&
								newInner[0] == '"' &&
								newInner[len(newInner)-1] == '"' {
								newInner = newInner[1 : len(newInner)-1]
							}
							tag.Attributes["class"] = &AttributeValue{
								Value:     `"` + existingInner + " " + newInner + `"`,
								Unescaped: unescaped,
							}
						} else {
							tag.Attributes[name] = &AttributeValue{Value: value, Unescaped: unescaped}
						}
					} else {
						tag.Attributes[name] = &AttributeValue{Value: value, Unescaped: unescaped}
					}
					p.advance()
				} else {
					return fmt.Errorf("expected attribute value at line %d", p.cur.Line)
				}
			} else {
				// Boolean attribute — no = was present; mark explicitly so the
				// runtime can distinguish bare `checked` from `href=href`.
				tag.Attributes[name] = &AttributeValue{Value: name, Unescaped: false, Boolean: true}
			}
		} else if p.cur.Type == TokenAttrComma {
			p.advance()
		} else {
			p.advance()
		}
	}

	if p.cur.Type != TokenAttrEnd {
		return fmt.Errorf("expected ) at line %d", p.cur.Line)
	}
	p.advance()
	return nil
}

// parseDoctype parses a doctype declaration.
func (p *Parser) parseDoctype() (*DoctypeNode, error) {
	line := p.cur.Line
	col := p.cur.Col
	value := p.cur.Value
	if value == "" {
		value = "html"
	}
	p.advance()
	p.skipNewlines()
	return &DoctypeNode{Value: value, Line: line, Col: col}, nil
}

// parseComment parses a comment.
func (p *Parser) parseComment() (*CommentNode, error) {
	buffered := p.cur.Type == TokenComment
	content := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	commentDepth := p.cur.Depth
	p.advance()

	// Collect indented body lines emitted by the lexer as TokenText tokens.
	// The lexer eagerly consumed all lines that are indented more deeply than
	// the comment header, so we just need to drain them here.
	var bodyLines []string
	if content != "" {
		bodyLines = append(bodyLines, content)
	}
	for p.cur.Type == TokenText && p.cur.Depth > commentDepth {
		bodyLines = append(bodyLines, p.cur.Value)
		p.advance()
	}
	if len(bodyLines) > 0 {
		content = strings.Join(bodyLines, "\n")
	}

	p.skipNewlines()
	return &CommentNode{Content: content, Buffered: buffered, Line: line, Col: col}, nil
}

// parseUnbufferedCode parses unbuffered code (-).
func (p *Parser) parseUnbufferedCode() (*CodeNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &CodeNode{Expression: expr, Type: CodeUnbuffered, Line: line, Col: col}, nil
}

// parseBufferedCode parses buffered code (=).
func (p *Parser) parseBufferedCode() (*CodeNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &CodeNode{Expression: expr, Type: CodeBuffered, Line: line, Col: col}, nil
}

// parseUnescapedCode parses unescaped code (!=).
func (p *Parser) parseUnescapedCode() (*CodeNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &CodeNode{Expression: expr, Type: CodeUnescaped, Line: line, Col: col}, nil
}

// parsePipedText parses piped text (|).
// The lexer may have split the line into a run of TokenText /
// TokenInterpolation / TokenInterpolationUnescape tokens at the same depth.
// If there is only a single plain-text token we return a PipeNode for
// back-compat; otherwise we wrap everything in a MixedTextNode via the
// tag-children path — but since PipeNode is only used at top/child level and
// the runtime just escapes its Content, we promote the whole run into the
// parent as individual nodes by returning a synthetic wrapper.
// Simplest correct approach: collect all same-depth text/interp tokens and
// return them wrapped in a DocumentNode (the caller appends its Children).
// Actually the cleanest approach that requires no new node type: collect the
// run and return a *TextRunNode.  We already have InterpolationNode; we just
// need to return multiple nodes.  Since parseNode returns a single Node we use
// a lightweight *textRunNode that the runtime knows how to render.
func (p *Parser) parsePipedText() (Node, error) {
	line := p.cur.Line
	col := p.cur.Col
	depth := p.cur.Depth

	nodes := p.collectTextRun(depth)
	p.skipNewlines()

	if len(nodes) == 1 {
		if pipe, ok := nodes[0].(*PipeNode); ok {
			return pipe, nil
		}
	}

	return &TextRunNode{Nodes: nodes, Line: line, Col: col}, nil
}

// collectTextRun collects a consecutive run of TokenPipe, TokenText,
// TokenInterpolation, and TokenInterpolationUnescape tokens at the given depth,
// returning them as a slice of Nodes (PipeNode, TextNode, InterpolationNode).
func (p *Parser) collectTextRun(depth int) []Node {
	var nodes []Node
	for {
		if p.cur.Depth != depth {
			break
		}
		switch p.cur.Type {
		case TokenPipe:
			if p.cur.Value != "" {
				nodes = append(nodes, &PipeNode{Content: p.cur.Value, Line: p.cur.Line, Col: p.cur.Col})
			}
			p.advance()
		case TokenText:
			nodes = append(nodes, &TextNode{Content: p.cur.Value, Line: p.cur.Line, Col: p.cur.Col})
			p.advance()
		case TokenInterpolation:
			nodes = append(nodes, &InterpolationNode{Expression: p.cur.Value, Unescaped: false, Line: p.cur.Line, Col: p.cur.Col})
			p.advance()
		case TokenInterpolationUnescape:
			nodes = append(nodes, &InterpolationNode{Expression: p.cur.Value, Unescaped: true, Line: p.cur.Line, Col: p.cur.Col})
			p.advance()
		case TokenTagInterpolationStart:
			tagInterp, err := p.parseTagInterpolation()
			if err != nil {
				goto done
			}
			nodes = append(nodes, tagInterp)
		default:
			goto done
		}
	}
done:
	return nodes
}

// parseTagInterpolation parses #[tag content] inline tag interpolation.
// The token value contains the full inner content string (e.g. "strong Hello").
// We re-lex and re-parse it as a mini document to produce a TagNode.
func (p *Parser) parseTagInterpolation() (*TagInterpolationNode, error) {
	inner := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance() // consume TokenTagInterpolationStart
	// Consume the paired TokenTagInterpolationEnd if present
	if p.cur.Type == TokenTagInterpolationEnd {
		p.advance()
	}

	// Re-lex the inner content as a standalone Pug snippet.
	lx := NewLexer(inner)
	tokens, err := lx.Lex()
	if err != nil {
		return nil, fmt.Errorf("tag interpolation lex error: %w", err)
	}
	pr := NewParser(tokens)
	doc, err := pr.Parse()
	if err != nil {
		return nil, fmt.Errorf("tag interpolation parse error: %w", err)
	}

	// Expect exactly one TagNode at the top level, followed by optional
	// sibling text/tag-interpolation nodes that belong inside it.
	// e.g. "span.badge #[strong ★] Featured" parses as:
	//   TagNode{span.badge}, TagInterpolationNode{strong ★}, TextNode{" Featured"}
	// The siblings after the tag must be promoted to children of the tag.
	var tag *TagNode
	tagIdx := -1
	for i, node := range doc.Children {
		if t, ok := node.(*TagNode); ok {
			tag = t
			tagIdx = i
			break
		}
	}
	if tag == nil {
		// Fallback: wrap as a span if the inner content didn't parse to a tag.
		tag = &TagNode{
			Name:       "span",
			Attributes: make(map[string]*AttributeValue),
			Children:   doc.Children,
		}
	} else if tagIdx >= 0 && tagIdx < len(doc.Children)-1 {
		// Attach any nodes that follow the tag as its children.
		tag.Children = append(tag.Children, doc.Children[tagIdx+1:]...)
	}

	return &TagInterpolationNode{Tag: tag, Line: line, Col: col}, nil
}

// parseInterpolationNode parses a standalone interpolation token.
func (p *Parser) parseInterpolationNode(unescaped bool) (*InterpolationNode, error) {
	node := &InterpolationNode{
		Expression: p.cur.Value,
		Unescaped:  unescaped,
		Line:       p.cur.Line,
		Col:        p.cur.Col,
	}
	p.advance()
	p.skipNewlines()
	return node, nil
}

// parseTextNode parses a bare text token as a TextNode.
func (p *Parser) parseTextNode() (*TextNode, error) {
	content := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &TextNode{Content: content, Line: line, Col: col}, nil
}

// parseLiteralHTML parses literal HTML.
func (p *Parser) parseLiteralHTML() (*LiteralHTMLNode, error) {
	content := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &LiteralHTMLNode{Content: content, Line: line, Col: col}, nil
}

// parseConditional parses if/else if/else blocks.
//
// The lexer folds "else if <cond>" onto a single TokenElse whose Value starts
// with "if ".  A bare "else" has Value "else" (or empty).  We build a proper
// linked chain: each else-if is the *sole* element of its parent's Alternate
// slice, so the runtime can walk the chain with a simple for loop.
func (p *Parser) parseConditional() (*ConditionalNode, error) {
	return p.parseConditionalWithCond(p.cur.Value, p.cur.Depth, p.cur.Line, p.cur.Col, false)
}

// parseConditionalWithCond does the actual work. condition/depth/line/col are
// passed explicitly so that else-if nodes can be constructed without injecting
// a synthetic token (which would cause p.advance() to skip a real body token).
// p.cur must already point at the FIRST body token (or the next else/EOF) when
// called for an else-if; for the root if it points at the TokenIf itself, so
// we advance past it first.
func (p *Parser) parseConditionalWithCond(condition string, currentDepth, line, col int, isElseIf bool) (*ConditionalNode, error) {
	cond := &ConditionalNode{
		Condition:  condition,
		Consequent: make([]Node, 0),
		Alternate:  make([]Node, 0),
		IsElseIf:   isElseIf,
		IsUnless:   false,
		Line:       line,
		Col:        col,
	}

	if !isElseIf {
		// Root if — p.cur is the TokenIf; advance past it to the first body token.
		p.advance()
		p.skipNewlines()
	}
	// For else-if, p.cur already points at the first body token (caller
	// consumed the TokenElse and called skipNewlines).

	// Parse consequent body
	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		if p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
			break
		}
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			cond.Consequent = append(cond.Consequent, node)
		}
		p.skipNewlines()
	}

	// Parse else / else-if tail.
	// There is at most one TokenElse at this depth; we handle it and stop.
	if p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
		elseToken := p.cur
		p.advance() // consume TokenElse
		p.skipNewlines()

		if strings.HasPrefix(elseToken.Value, "if ") {
			// else if — p.cur now points at the first body token of the else-if.
			// Build the child node without consuming any extra tokens.
			elseIfCond := strings.TrimSpace(strings.TrimPrefix(elseToken.Value, "if "))
			child, err := p.parseConditionalWithCond(elseIfCond, currentDepth, elseToken.Line, elseToken.Col, true)
			if err != nil {
				return nil, err
			}
			cond.Alternate = append(cond.Alternate, child)
		} else {
			// bare else — collect body nodes directly into cond.Alternate
			for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
				node, err := p.parseNode(currentDepth + 1)
				if err != nil {
					return nil, err
				}
				if node != nil {
					cond.Alternate = append(cond.Alternate, node)
				}
				p.skipNewlines()
			}
		}
	}

	return cond, nil
}

// parseUnless parses unless blocks, including an optional else clause.
func (p *Parser) parseUnless() (*ConditionalNode, error) {
	cond := &ConditionalNode{
		Condition:  p.cur.Value,
		Consequent: make([]Node, 0),
		Alternate:  make([]Node, 0),
		IsUnless:   true,
		Line:       p.cur.Line,
		Col:        p.cur.Col,
	}

	currentDepth := p.cur.Depth
	p.advance()
	p.skipNewlines()

	// Parse consequent body
	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		if p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
			break
		}
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			cond.Consequent = append(cond.Consequent, node)
		}
		p.skipNewlines()
	}

	// Optional else clause
	if p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
		p.advance() // consume TokenElse
		p.skipNewlines()
		for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
			node, err := p.parseNode(currentDepth + 1)
			if err != nil {
				return nil, err
			}
			if node != nil {
				cond.Alternate = append(cond.Alternate, node)
			}
			p.skipNewlines()
		}
	}

	return cond, nil
}

// parseEach parses each/for loops.
func (p *Parser) parseEach() (*EachNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col

	// Parse: each item in collection or each key, item in collection
	parts := strings.Split(expr, " in ")
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed each expression at line %d: %s", line, expr)
	}

	itemPart := strings.TrimSpace(parts[0])
	collectionPart := strings.TrimSpace(parts[1])

	// Pug syntax: each value, key in collection
	// kv[0] is the value variable, kv[1] is the key/index variable
	var item, key string
	if strings.Contains(itemPart, ",") {
		kv := strings.Split(itemPart, ",")
		item = strings.TrimSpace(kv[0])
		key = strings.TrimSpace(kv[1])
	} else {
		item = itemPart
	}

	each := &EachNode{
		Item:       item,
		Key:        key,
		Collection: collectionPart,
		Body:       make([]Node, 0),
		ElseBody:   make([]Node, 0),
		Line:       line,
		Col:        col,
	}

	currentDepth := p.cur.Depth
	p.advance()
	p.skipNewlines()

	// Parse body
	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		if p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
			break
		}
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			each.Body = append(each.Body, node)
		}
		p.skipNewlines()
	}

	// Parse else
	if p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
		p.advance()
		p.skipNewlines()
		for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
			node, err := p.parseNode(currentDepth + 1)
			if err != nil {
				return nil, err
			}
			if node != nil {
				each.ElseBody = append(each.ElseBody, node)
			}
			p.skipNewlines()
		}
	}

	return each, nil
}

// parseWhile parses while loops.
func (p *Parser) parseWhile() (*WhileNode, error) {
	condition := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col

	w := &WhileNode{
		Condition: condition,
		Body:      make([]Node, 0),
		Line:      line,
		Col:       col,
	}

	currentDepth := p.cur.Depth
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			w.Body = append(w.Body, node)
		}
		p.skipNewlines()
	}

	return w, nil
}

// parseCase parses case/when/default statements.
func (p *Parser) parseCase() (*CaseNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col

	c := &CaseNode{
		Expression: expr,
		Cases:      make([]*WhenNode, 0),
		Default:    make([]Node, 0),
		Line:       line,
		Col:        col,
	}

	currentDepth := p.cur.Depth
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		if p.cur.Type == TokenWhen {
			whenExpr := p.cur.Value
			whenLine := p.cur.Line
			whenCol := p.cur.Col
			p.advance()
			p.skipNewlines()

			when := &WhenNode{
				Expression: whenExpr,
				Body:       make([]Node, 0),
				Line:       whenLine,
				Col:        whenCol,
			}

			for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth && p.cur.Type != TokenWhen && p.cur.Type != TokenDefault {
				node, err := p.parseNode(currentDepth + 1)
				if err != nil {
					return nil, err
				}
				if node != nil {
					when.Body = append(when.Body, node)
				}
				p.skipNewlines()
			}

			c.Cases = append(c.Cases, when)
		} else if p.cur.Type == TokenDefault {
			p.advance()
			p.skipNewlines()

			for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth && p.cur.Type != TokenWhen && p.cur.Type != TokenDefault {
				node, err := p.parseNode(currentDepth + 1)
				if err != nil {
					return nil, err
				}
				if node != nil {
					c.Default = append(c.Default, node)
				}
				p.skipNewlines()
			}
		} else {
			p.advance()
		}
	}

	return c, nil
}

// parseMixinDecl parses mixin declarations.
//
// The lexer emits TokenMixin whose Value is everything after the "mixin"
// keyword on the same line, e.g. "button(text, cls)" or "tag(name, ...attrs)".
// We split that into the mixin name and an optional parameter list.
func (p *Parser) parseMixinDecl() (*MixinDeclNode, error) {
	raw := p.cur.Value // e.g. "button(text, cls)"
	line := p.cur.Line
	col := p.cur.Col

	// Split name from optional parameter list.
	mixinName := raw
	paramStr := ""
	if idx := strings.Index(raw, "("); idx >= 0 {
		mixinName = strings.TrimSpace(raw[:idx])
		// find matching closing paren
		end := strings.LastIndex(raw, ")")
		if end > idx {
			paramStr = raw[idx+1 : end]
		}
	}

	mixin := &MixinDeclNode{
		Name:          mixinName,
		Parameters:    make([]string, 0),
		DefaultValues: make(map[string]string),
		Body:          make([]Node, 0),
		Line:          line,
		Col:           col,
	}

	// Parse parameter list: "text, cls", "name, ...attrs", or
	// "title=\"Default\"" (default values).
	// We split on commas that are not inside quotes or parens.
	if paramStr != "" {
		for _, raw := range splitMixinParams(paramStr) {
			param := strings.TrimSpace(raw)
			if param == "" {
				continue
			}
			if strings.HasPrefix(param, "...") {
				mixin.RestParam = strings.TrimSpace(strings.TrimPrefix(param, "..."))
			} else if eqIdx := strings.Index(param, "="); eqIdx >= 0 {
				// Default value: name=expr or name="Default"
				paramName := strings.TrimSpace(param[:eqIdx])
				defaultExpr := strings.TrimSpace(param[eqIdx+1:])
				mixin.Parameters = append(mixin.Parameters, paramName)
				mixin.DefaultValues[paramName] = defaultExpr
			} else {
				mixin.Parameters = append(mixin.Parameters, param)
			}
		}
	}

	currentDepth := p.cur.Depth // capture depth from the TokenMixin token
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			mixin.Body = append(mixin.Body, node)
		}
		p.skipNewlines()
	}

	return mixin, nil
}

// splitMixinParams splits a mixin parameter string on top-level commas
// (i.e. commas not inside quotes or nested parentheses/brackets).
// For example: `title="Hello, World", cls` → [`title="Hello, World"`, ` cls`]
func splitMixinParams(s string) []string {
	var parts []string
	depth := 0
	inDouble := false
	inSingle := false
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '\\' && (inDouble || inSingle):
			i++ // skip escaped char
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case (ch == '(' || ch == '[' || ch == '{') && !inDouble && !inSingle:
			depth++
		case (ch == ')' || ch == ']' || ch == '}') && !inDouble && !inSingle:
			depth--
		case ch == ',' && depth == 0 && !inDouble && !inSingle:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseMixinCall parses mixin calls (+name).
//
// The lexer emits TokenMixinCall whose Value is the mixin name, then calls
// scanTagRest which emits AttrStart/AttrName/AttrValue/AttrEnd tokens for
// any argument list written as (+name(arg1, arg2)).  We collect those as
// positional argument strings.  Any indented children become the call's Block.
func (p *Parser) parseMixinCall() (*MixinCallNode, error) {
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	currentDepth := p.cur.Depth // capture depth from the TokenMixinCall token before advancing
	p.advance()

	call := &MixinCallNode{
		Name:       name,
		Arguments:  make([]string, 0),
		Attributes: make(map[string]*AttributeValue),
		Block:      make([]Node, 0),
		Line:       line,
		Col:        col,
	}

	// Collect arguments emitted as attribute tokens by scanTagRest.
	// Each argument appears as AttrName (the value expression) with no AttrEqual
	// following it, OR as AttrName + AttrEqual + AttrValue for named attrs.
	// We distinguish: if there is no AttrEqual it's a positional argument;
	// if there is an AttrEqual it's a named HTML attribute for &attributes.
	if p.cur.Type == TokenAttrStart {
		p.advance() // consume (
		for p.cur.Type != TokenAttrEnd && p.cur.Type != TokenEOF {
			if p.cur.Type == TokenAttrComma {
				p.advance()
				continue
			}
			if p.cur.Type == TokenAttrName {
				attrName := p.cur.Value
				p.advance()
				if p.cur.Type == TokenAttrEqual || p.cur.Type == TokenAttrEqualUnescape {
					// named attribute — store for &attributes
					unescaped := p.cur.Type == TokenAttrEqualUnescape
					p.advance() // consume = or !=
					val := ""
					if p.cur.Type == TokenAttrValue {
						val = p.cur.Value
						p.advance()
					}
					call.Attributes[attrName] = &AttributeValue{Value: val, Unescaped: unescaped}
				} else {
					// positional argument — attrName is the expression
					call.Arguments = append(call.Arguments, attrName)
				}
				continue
			}
			p.advance() // skip unexpected tokens inside ()
		}
		if p.cur.Type == TokenAttrEnd {
			p.advance() // consume )
		}
	}

	// A mixin call may be followed by a second attribute group for HTML
	// attributes: +btn("OK")(class="primary").  Consume it and merge into
	// call.Attributes (these become the implicit `attributes` map inside the
	// mixin body).
	if p.cur.Type == TokenAttrStart {
		p.advance() // consume (
		for p.cur.Type != TokenAttrEnd && p.cur.Type != TokenEOF {
			if p.cur.Type == TokenAttrComma {
				p.advance()
				continue
			}
			if p.cur.Type == TokenAttrName {
				attrName := p.cur.Value
				p.advance()
				if p.cur.Type == TokenAttrEqual || p.cur.Type == TokenAttrEqualUnescape {
					unescaped := p.cur.Type == TokenAttrEqualUnescape
					p.advance() // consume = or !=
					val := ""
					if p.cur.Type == TokenAttrValue {
						val = p.cur.Value
						p.advance()
					}
					call.Attributes[attrName] = &AttributeValue{Value: val, Unescaped: unescaped}
				} else {
					// Boolean attribute (no value)
					call.Attributes[attrName] = &AttributeValue{Value: attrName, Boolean: true}
				}
				continue
			}
			p.advance() // skip unexpected tokens
		}
		if p.cur.Type == TokenAttrEnd {
			p.advance() // consume )
		}
	}

	p.skipNewlines()

	// Collect indented block children
	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			call.Block = append(call.Block, node)
		}
		p.skipNewlines()
	}

	return call, nil
}

// parseBlock parses named blocks for inheritance.
func (p *Parser) parseBlock() (*BlockNode, error) {
	var mode BlockMode
	switch p.cur.Type {
	case TokenBlockAppend:
		mode = BlockModeAppend
	case TokenBlockPrepend:
		mode = BlockModePrepend
	default:
		mode = BlockModeReplace
	}

	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	// Capture the block's own indentation depth BEFORE advancing so that
	// child nodes (which are at depth+1) are correctly collected into the
	// block body instead of being treated as siblings.
	currentDepth := p.cur.Depth
	p.advance()

	block := &BlockNode{
		Name: name,
		Mode: mode,
		Body: make([]Node, 0),
		Line: line,
		Col:  col,
	}

	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			block.Body = append(block.Body, node)
		}
		p.skipNewlines()
	}

	return block, nil
}

// parseBlockModifier parses standalone append/prepend keywords.
//
// Two forms are supported:
//
//  1. Standalone keyword: "append <name>" or "prepend <name>"
//     The lexer emits TokenAppend/TokenPrepend with the block name already
//     stored in the token's Value field (the rest of the line after the
//     keyword).
//
//  2. Legacy two-token form: "append block <name>" or "prepend block <name>"
//     where a separate TokenBlock token follows (kept for compatibility with
//     any future lexer variant that splits the tokens).
func (p *Parser) parseBlockModifier() (*BlockNode, error) {
	mode := BlockModeAppend
	if p.cur.Type == TokenPrepend {
		mode = BlockModePrepend
	}

	// The token's Value holds the block name directly (set by the lexer when
	// it collects the rest of the keyword line as the value).
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	// Capture the modifier token's own depth BEFORE advancing so that child
	// nodes (at depth+1) are correctly collected into the block body.
	currentDepth := p.cur.Depth
	p.advance()

	// If the name is empty, the lexer may have emitted a separate TokenBlock
	// with the actual name (legacy / alternative lexer behaviour).
	if name == "" {
		if p.cur.Type != TokenBlock {
			return nil, fmt.Errorf("expected block name after append/prepend at line %d", p.cur.Line)
		}
		name = p.cur.Value
		currentDepth = p.cur.Depth
		p.advance()
	}

	block := &BlockNode{
		Name: name,
		Mode: mode,
		Body: make([]Node, 0),
		Line: line,
		Col:  col,
	}

	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			block.Body = append(block.Body, node)
		}
		p.skipNewlines()
	}

	return block, nil
}

// parseExtends parses extends declarations.
func (p *Parser) parseExtends() (*ExtendsNode, error) {
	path := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &ExtendsNode{Path: path, Line: line, Col: col}, nil
}

// parseInclude parses include directives.
//
// Supported forms:
//
//	include path/to/file.pug       — normal Pug include
//	include path/to/file.md        — raw file include
//	include :filtername path       — raw file include with filter applied
//
// The lexer emits a single TokenInclude whose Value is the entire rest of the
// line after the "include" keyword.  When that value starts with ":" we split
// it into a filter name and a path here in the parser.
func (p *Parser) parseInclude() (*IncludeNode, error) {
	raw := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()

	include := &IncludeNode{
		Line: line,
		Col:  col,
	}

	// "include :filtername path" — the value starts with ":"
	if strings.HasPrefix(raw, ":") {
		// Strip the leading colon and split on the first whitespace.
		rest := raw[1:]
		if idx := strings.IndexAny(rest, " \t"); idx >= 0 {
			include.Filter = strings.TrimSpace(rest[:idx])
			include.Path = strings.TrimSpace(rest[idx+1:])
		} else {
			// No space — the whole thing is the filter name with no path.
			// Treat it as a filter name with an empty path; the runtime will
			// error with a helpful message.
			include.Filter = rest
			include.Path = ""
		}
	} else {
		include.Path = raw
	}

	// Legacy: if the NEXT token is TokenFilter, it was emitted by an older
	// lexer variant — consume it for backwards compatibility.
	if p.cur.Type == TokenFilter {
		include.Filter = p.cur.Value
		p.advance()
	}

	p.skipNewlines()
	return include, nil
}

// parseFilter parses filter blocks.
//
// Supported forms:
//
//	:filtername               — block filter; indented body lines follow
//	:filtername inline text   — inline filter; content on the same line
//	:outer:inner              — chained subfilter; inner applied first
//
// The lexer now eagerly collects filter body lines as consecutive TokenText
// tokens (one per body line) so the parser never sees Pug tag/keyword tokens
// inside the filter body.  For inline filters the lexer emits a single
// TokenText with the same-line content.
//
// Token stream produced by the lexer:
//
//	TokenFilter{"name"}        — primary filter name
//	TokenFilterColon{"sub"}    — optional chained subfilter (may repeat)
//	TokenText{"line..."}...    — body lines (one token per line) OR single
//	                             inline-content token
func (p *Parser) parseFilter() (*FilterNode, error) {
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	// The filter token's depth is the reference depth.  Body TokenText tokens
	// emitted by the lexer carry a greater depth, but since the lexer now
	// handles ALL body collection internally we only need to gather the
	// consecutive TokenText tokens that immediately follow the filter header.
	filterDepth := p.cur.Depth
	p.advance()

	filter := &FilterNode{
		Name: name,
		Line: line,
		Col:  col,
	}

	// Collect any chained subfilter names (:outer:inner → Subfilter chain).
	// Accumulated as a colon-separated string so the runtime can split and
	// apply them in order (innermost first).
	// Subfilters may appear before OR after the options block, matching the
	// lexer which also scans both positions:
	//   :outer:inner(opts)     — subfilters before options
	//   :outer(opts):inner     — subfilters after options
	var subfilters []string
	collectSubfilters := func() {
		for p.cur.Type == TokenFilterColon {
			subfilters = append(subfilters, p.cur.Value)
			p.advance()
		}
	}

	collectSubfilters() // subfilters before the options block

	// Parse optional filter options from a TokenFilterOptions token.
	// The token value contains the raw content inside the parentheses, e.g.
	// for ":markdown(flavor="gfm" pretty=true)" the value is
	// `flavor="gfm" pretty=true`.  We decode that into a map[string]string.
	if p.cur.Type == TokenFilterOptions {
		filter.Options = parseFilterOptions(p.cur.Value)
		p.advance()
	}

	collectSubfilters() // subfilters after the options block (e.g. :outer(opts):inner)

	if len(subfilters) > 0 {
		filter.Subfilter = strings.Join(subfilters, ":")
	}

	// Collect consecutive TokenText tokens emitted by the lexer for this
	// filter (both inline single-token form and multi-line block form).
	// We only consume tokens whose depth is greater than the filter header's
	// depth, or that sit at the same depth immediately after the header (the
	// inline case where the lexer emits TokenText at the same depth level).
	var lines []string
	for p.cur.Type == TokenText && p.cur.Depth >= filterDepth {
		lines = append(lines, p.cur.Value)
		p.advance()
	}
	filter.Content = strings.Join(lines, "\n")

	p.skipNewlines()
	return filter, nil
}

// parseFilterOptions decodes a raw options string of the form
//
//	key=value key2="quoted value" flag
//
// into a map[string]string.  Values may be bare identifiers/numbers or
// single/double-quoted strings.  A bare key without = is stored with value
// "true" so it can be treated as a boolean flag.
func parseFilterOptions(raw string) map[string]string {
	opts := make(map[string]string)
	s := strings.TrimSpace(raw)
	for s != "" {
		// Skip commas and whitespace between pairs.
		s = strings.TrimLeft(s, " \t,")
		if s == "" {
			break
		}

		// Read the key (identifier).
		end := 0
		for end < len(s) && s[end] != '=' && s[end] != ' ' && s[end] != '\t' && s[end] != ',' {
			end++
		}
		key := s[:end]
		s = s[end:]

		if key == "" {
			break
		}

		s = strings.TrimLeft(s, " \t")
		if !strings.HasPrefix(s, "=") {
			// Boolean flag — no value.
			opts[key] = "true"
			continue
		}
		// Consume the '='.
		s = s[1:]
		s = strings.TrimLeft(s, " \t")

		var value string
		if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
			// Quoted value.
			quote := rune(s[0])
			s = s[1:]
			i := 0
			for i < len(s) && rune(s[i]) != quote {
				if s[i] == '\\' && i+1 < len(s) {
					i++ // skip escaped char
				}
				i++
			}
			value = s[:i]
			if i < len(s) {
				s = s[i+1:] // consume closing quote
			} else {
				s = ""
			}
		} else {
			// Bare value: read until whitespace or comma.
			end = 0
			for end < len(s) && s[end] != ' ' && s[end] != '\t' && s[end] != ',' {
				end++
			}
			value = s[:end]
			s = s[end:]
		}
		opts[key] = value
	}
	return opts
}

// parseBlockText collects indented lines as block text.
func (p *Parser) parseBlockText(expectedDepth int) string {
	var lines []string
	for p.cur.Type != TokenEOF && p.cur.Depth >= expectedDepth {
		if p.cur.Type == TokenText {
			lines = append(lines, p.cur.Value)
		} else if p.cur.Type == TokenPipe {
			lines = append(lines, p.cur.Value)
		} else {
			break
		}
		p.advance()
	}
	return strings.Join(lines, "\n")
}

// skipNewlines skips newline tokens.
func (p *Parser) skipNewlines() {
	for p.cur.Type == TokenNewline {
		p.advance()
	}
}

// advance moves to the next token.
func (p *Parser) advance() {
	if p.pos < len(p.tokens)-1 {
		p.pos++
		p.cur = p.tokens[p.pos]
	}
}
