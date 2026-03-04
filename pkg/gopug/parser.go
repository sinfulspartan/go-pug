package gopug

import (
	"fmt"
	"strings"
)

type Parser struct {
	tokens []Token
	pos    int
	cur    Token
}

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

func (p *Parser) Parse() (*DocumentNode, error) {
	doc := &DocumentNode{
		Children: make([]Node, 0),
	}

	for p.cur.Type != TokenEOF {
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

// parseNode: expectedDepth is the minimum token depth required at this level;
// a token with a smaller depth triggers a dedent error.
func (p *Parser) parseNode(expectedDepth int) (Node, error) {
	tok := p.cur

	if tok.Type != TokenEOF && tok.Type != TokenNewline && tok.IndentDepth < expectedDepth {
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

func (p *Parser) parseTag() (Node, error) {
	tag := &TagNode{
		Name:       "",
		Attributes: make(map[string]*AttributeValue),
		Children:   make([]Node, 0),
		Line:       p.cur.Line,
		Col:        p.cur.Col,
	}

	currentDepth := p.cur.IndentDepth

	switch p.cur.Type {
	case TokenTag:
		tag.Name = p.cur.Value
		p.advance()
	case TokenClass, TokenID:
		tag.Name = "div" // implicit div
	}

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
			// &attributes emitted outside an AttrStart/AttrEnd block by the lexer.
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
				p.advance()
			}

		case TokenDot:
			p.advance()
			p.skipNewlines()
			if p.cur.IndentDepth > currentDepth {
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
		}
	}

	if (p.cur.Type == TokenText || p.cur.Type == TokenInterpolation || p.cur.Type == TokenInterpolationUnescape) && p.cur.IndentDepth == currentDepth {
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
		if p.cur.IndentDepth <= currentDepth {
			return tag, nil
		}
	}

	if p.cur.Type == TokenCodeBuffered && p.cur.IndentDepth == currentDepth {
		code := &CodeNode{Expression: p.cur.Value, Type: CodeBuffered, Line: p.cur.Line, Col: p.cur.Col}
		p.advance()
		p.skipNewlines()
		tag.Children = append(tag.Children, code)
		return tag, nil
	}

	if p.cur.Type == TokenCodeUnescaped && p.cur.IndentDepth == currentDepth {
		code := &CodeNode{Expression: p.cur.Value, Type: CodeUnescaped, Line: p.cur.Line, Col: p.cur.Col}
		p.advance()
		p.skipNewlines()
		tag.Children = append(tag.Children, code)
		return tag, nil
	}

	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

// parseAttributes: for the "class" attribute, values from multiple sources
// (shorthand + explicit) are merged rather than overwritten.
func (p *Parser) parseAttributes(tag *TagNode) error {
	for p.cur.Type != TokenAttrEnd && p.cur.Type != TokenEOF {
		if p.cur.Type == TokenAttrName {
			name := p.cur.Value
			p.advance()

			if p.cur.Type == TokenAttrEqual || p.cur.Type == TokenAttrEqualUnescape {
				unescaped := p.cur.Type == TokenAttrEqualUnescape
				p.advance()

				if p.cur.Type == TokenAttrValue {
					value := p.cur.Value
					if name == "class" {
						if existing, ok := tag.Attributes["class"]; ok {
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
				tag.Attributes[name] = &AttributeValue{Value: name, Unescaped: false, IsBare: true}
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

// parseComment drains the TokenText body lines the lexer eagerly collected
// under the comment header and joins them into a single Content string.
func (p *Parser) parseComment() (*CommentNode, error) {
	buffered := p.cur.Type == TokenComment
	content := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	commentDepth := p.cur.IndentDepth
	p.advance()

	var bodyLines []string
	if content != "" {
		bodyLines = append(bodyLines, content)
	}
	for p.cur.Type == TokenText && p.cur.IndentDepth > commentDepth {
		bodyLines = append(bodyLines, p.cur.Value)
		p.advance()
	}
	if len(bodyLines) > 0 {
		content = strings.Join(bodyLines, "\n")
	}

	p.skipNewlines()
	return &CommentNode{Content: content, Buffered: buffered, Line: line, Col: col}, nil
}

func (p *Parser) parseUnbufferedCode() (*CodeNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &CodeNode{Expression: expr, Type: CodeUnbuffered, Line: line, Col: col}, nil
}

func (p *Parser) parseBufferedCode() (*CodeNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &CodeNode{Expression: expr, Type: CodeBuffered, Line: line, Col: col}, nil
}

func (p *Parser) parseUnescapedCode() (*CodeNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &CodeNode{Expression: expr, Type: CodeUnescaped, Line: line, Col: col}, nil
}

func (p *Parser) parsePipedText() (Node, error) {
	line := p.cur.Line
	col := p.cur.Col
	depth := p.cur.IndentDepth

	nodes := p.collectTextRun(depth)
	p.skipNewlines()

	if len(nodes) == 1 {
		if pipe, ok := nodes[0].(*PipeNode); ok {
			return pipe, nil
		}
	}

	return &TextRunNode{Nodes: nodes, Line: line, Col: col}, nil
}

func (p *Parser) collectTextRun(depth int) []Node {
	var nodes []Node
	for {
		if p.cur.IndentDepth != depth {
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

// parseTagInterpolation handles the inner content of a #[...] interpolation
// token by re-lexing and re-parsing it to produce a TagInterpolationNode.
func (p *Parser) parseTagInterpolation() (*TagInterpolationNode, error) {
	inner := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance() // consume TokenTagInterpolationStart
	if p.cur.Type == TokenTagInterpolationEnd {
		p.advance()
	}

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
		// Inner content didn't parse to a tag — wrap in a span.
		tag = &TagNode{
			Name:       "span",
			Attributes: make(map[string]*AttributeValue),
			Children:   doc.Children,
		}
	} else if tagIdx >= 0 && tagIdx < len(doc.Children)-1 {
		tag.Children = append(tag.Children, doc.Children[tagIdx+1:]...)
	}

	return &TagInterpolationNode{Tag: tag, Line: line, Col: col}, nil
}

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

func (p *Parser) parseTextNode() (*TextNode, error) {
	content := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &TextNode{Content: content, Line: line, Col: col}, nil
}

func (p *Parser) parseLiteralHTML() (*LiteralHTMLNode, error) {
	content := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &LiteralHTMLNode{Content: content, Line: line, Col: col}, nil
}

// parseConditional: the lexer folds "else if <cond>" onto a single TokenElse
// whose Value starts with "if ". A bare "else" has Value "else" (or empty).
// Each else-if is the sole element of its parent's Alternate slice so the
// runtime can walk the chain with a simple for loop.
func (p *Parser) parseConditional() (*ConditionalNode, error) {
	return p.parseConditionalWithCond(p.cur.Value, p.cur.IndentDepth, p.cur.Line, p.cur.Col, false)
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

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
		if p.cur.Type == TokenElse && p.cur.IndentDepth == currentDepth {
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

	// There is at most one TokenElse at this depth; we handle it and stop.
	if p.cur.Type == TokenElse && p.cur.IndentDepth == currentDepth {
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
			for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

func (p *Parser) parseUnless() (*ConditionalNode, error) {
	cond := &ConditionalNode{
		Condition:  p.cur.Value,
		Consequent: make([]Node, 0),
		Alternate:  make([]Node, 0),
		IsUnless:   true,
		Line:       p.cur.Line,
		Col:        p.cur.Col,
	}

	currentDepth := p.cur.IndentDepth
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
		if p.cur.Type == TokenElse && p.cur.IndentDepth == currentDepth {
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

	if p.cur.Type == TokenElse && p.cur.IndentDepth == currentDepth {
		p.advance() // consume TokenElse
		p.skipNewlines()
		for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

func (p *Parser) parseEach() (*EachNode, error) {
	expr := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col

	parts := strings.Split(expr, " in ")
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed each expression at line %d: %s", line, expr)
	}

	itemPart := strings.TrimSpace(parts[0])
	collectionPart := strings.TrimSpace(parts[1])

	var item, key string
	if strings.Contains(itemPart, ",") {
		kv := strings.Split(itemPart, ",")
		item = strings.TrimSpace(kv[0])
		key = strings.TrimSpace(kv[1])
	} else {
		item = itemPart
	}

	each := &EachNode{
		ItemVar:        item,
		IndexVar:       key,
		CollectionExpr: collectionPart,
		Body:           make([]Node, 0),
		EmptyBody:      make([]Node, 0),
		Line:           line,
		Col:            col,
	}

	currentDepth := p.cur.IndentDepth
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
		if p.cur.Type == TokenElse && p.cur.IndentDepth == currentDepth {
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

	if p.cur.Type == TokenElse && p.cur.IndentDepth == currentDepth {
		p.advance()
		p.skipNewlines()
		for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
			node, err := p.parseNode(currentDepth + 1)
			if err != nil {
				return nil, err
			}
			if node != nil {
				each.EmptyBody = append(each.EmptyBody, node)
			}
			p.skipNewlines()
		}
	}

	return each, nil
}

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

	currentDepth := p.cur.IndentDepth
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

	currentDepth := p.cur.IndentDepth
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

			for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth && p.cur.Type != TokenWhen && p.cur.Type != TokenDefault {
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

			for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth && p.cur.Type != TokenWhen && p.cur.Type != TokenDefault {
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

// parseMixinDecl: TokenMixin.Value is everything after the "mixin" keyword,
// e.g. "button(text, cls)" or "tag(name, ...attrs)".
func (p *Parser) parseMixinDecl() (*MixinDeclNode, error) {
	raw := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col

	mixinName := raw
	paramStr := ""
	if before, inner, found := strings.Cut(raw, "("); found {
		mixinName = strings.TrimSpace(before)
		paramStr, _, _ = strings.Cut(inner, ")")
		paramStr = strings.TrimSpace(paramStr)
	}

	mixin := &MixinDeclNode{
		Name:          mixinName,
		Parameters:    make([]string, 0),
		ParamDefaults: make(map[string]string),
		Body:          make([]Node, 0),
		Line:          line,
		Col:           col,
	}

	if paramStr != "" {
		for _, raw := range splitMixinParams(paramStr) {
			param := strings.TrimSpace(raw)
			if param == "" {
				continue
			}
			if strings.HasPrefix(param, "...") {
				mixin.RestParamName = strings.TrimSpace(strings.TrimPrefix(param, "..."))
			} else if paramName, defaultExpr, hasDefault := strings.Cut(param, "="); hasDefault {
				paramName = strings.TrimSpace(paramName)
				defaultExpr = strings.TrimSpace(defaultExpr)
				mixin.Parameters = append(mixin.Parameters, paramName)
				mixin.ParamDefaults[paramName] = defaultExpr
			} else {
				mixin.Parameters = append(mixin.Parameters, param)
			}
		}
	}

	currentDepth := p.cur.IndentDepth
	p.advance()
	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

// parseMixinCall: scanTagRest emits AttrStart/AttrName/AttrValue/AttrEnd for
// the argument list; we collect positional argument strings from those tokens.
// Any indented children become the call's BlockContent.
func (p *Parser) parseMixinCall() (*MixinCallNode, error) {
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	currentDepth := p.cur.IndentDepth
	p.advance()

	call := &MixinCallNode{
		Name:         name,
		Arguments:    make([]string, 0),
		Attributes:   make(map[string]*AttributeValue),
		BlockContent: make([]Node, 0),
		Line:         line,
		Col:          col,
	}

	// First attribute group: positional arguments (no AttrEqual) and/or
	// named HTML attributes (with AttrEqual) for &attributes forwarding.
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

	// Optional second attribute group: +btn("OK")(class="primary").
	// Merged into call.Attributes as the implicit `attributes` map.
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
					call.Attributes[attrName] = &AttributeValue{Value: attrName, IsBare: true}
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

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
		node, err := p.parseNode(currentDepth + 1)
		if err != nil {
			return nil, err
		}
		if node != nil {
			call.BlockContent = append(call.BlockContent, node)
		}
		p.skipNewlines()
	}

	return call, nil
}

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
	currentDepth := p.cur.IndentDepth // must be captured before advance
	p.advance()

	block := &BlockNode{
		Name: name,
		Mode: mode,
		Body: make([]Node, 0),
		Line: line,
		Col:  col,
	}

	p.skipNewlines()

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

// parseBlockModifier: TokenAppend/TokenPrepend carry the block name in Value.
// Legacy two-token form "append block <name>" is also handled for compatibility.
func (p *Parser) parseBlockModifier() (*BlockNode, error) {
	mode := BlockModeAppend
	if p.cur.Type == TokenPrepend {
		mode = BlockModePrepend
	}

	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	currentDepth := p.cur.IndentDepth // must be captured before advance
	p.advance()

	// If the name is empty, the lexer may have emitted a separate TokenBlock
	// with the actual name (legacy / alternative lexer behaviour).
	if name == "" {
		if p.cur.Type != TokenBlock {
			return nil, fmt.Errorf("expected block name after append/prepend at line %d", p.cur.Line)
		}
		name = p.cur.Value
		currentDepth = p.cur.IndentDepth
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

	for p.cur.Type != TokenEOF && p.cur.IndentDepth > currentDepth {
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

func (p *Parser) parseExtends() (*ExtendsNode, error) {
	path := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &ExtendsNode{Path: path, Line: line, Col: col}, nil
}

// parseInclude handles three forms:
//
//	include path/to/file.pug   — Pug include
//	include path/to/file.txt   — raw file include
//	include :filtername path   — raw file include with filter applied
func (p *Parser) parseInclude() (*IncludeNode, error) {
	raw := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()

	include := &IncludeNode{
		Line: line,
		Col:  col,
	}

	if strings.HasPrefix(raw, ":") {
		rest := raw[1:]
		if idx := strings.IndexAny(rest, " \t"); idx >= 0 {
			include.FilterName = strings.TrimSpace(rest[:idx])
			include.Path = strings.TrimSpace(rest[idx+1:])
		} else {
			// No path — runtime will error with a helpful message.
			include.FilterName = rest
			include.Path = ""
		}
	} else {
		include.Path = raw
	}

	// Legacy: if the NEXT token is TokenFilter, it was emitted by an older
	// lexer variant — consume it for backwards compatibility.
	if p.cur.Type == TokenFilter {
		include.FilterName = p.cur.Value
		p.advance()
	}

	p.skipNewlines()
	return include, nil
}

// parseFilter: body lines were eagerly collected by the lexer as TokenText.
// Token sequence:
//
//	TokenFilter{"name"}
//	TokenFilterColon{"sub"}  — repeated for each chained subfilter
//	TokenFilterOptions{...}  — optional (key=val) options block
//	TokenText{...}           — one per body line, or single inline token
func (p *Parser) parseFilter() (*FilterNode, error) {
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	filterDepth := p.cur.IndentDepth
	p.advance()

	filter := &FilterNode{
		Name: name,
		Line: line,
		Col:  col,
	}

	var subfilters []string
	collectSubfilters := func() {
		for p.cur.Type == TokenFilterColon {
			subfilters = append(subfilters, p.cur.Value)
			p.advance()
		}
	}

	collectSubfilters()

	if p.cur.Type == TokenFilterOptions {
		filter.Options = parseFilterOptions(p.cur.Value)
		p.advance()
	}

	collectSubfilters()

	if len(subfilters) > 0 {
		filter.Subfilter = strings.Join(subfilters, ":")
	}

	var lines []string
	for p.cur.Type == TokenText && p.cur.IndentDepth >= filterDepth {
		lines = append(lines, p.cur.Value)
		p.advance()
	}
	filter.Content = strings.Join(lines, "\n")

	p.skipNewlines()
	return filter, nil
}

// parseFilterOptions: bare keys without a value are stored as "true".
func parseFilterOptions(raw string) map[string]string {
	opts := make(map[string]string)
	s := strings.TrimSpace(raw)
	for s != "" {
		s = strings.TrimLeft(s, " \t,")
		if s == "" {
			break
		}

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
			opts[key] = "true"
			continue
		}
		s = s[1:]
		s = strings.TrimLeft(s, " \t")

		var value string
		if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
			quote := rune(s[0])
			s = s[1:]
			i := 0
			for i < len(s) && rune(s[i]) != quote {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				i++
			}
			value = s[:i]
			if i < len(s) {
				s = s[i+1:]
			} else {
				s = ""
			}
		} else {
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

func (p *Parser) parseBlockText(expectedDepth int) string {
	var lines []string
	for p.cur.Type != TokenEOF && p.cur.IndentDepth >= expectedDepth {
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

func (p *Parser) skipNewlines() {
	for p.cur.Type == TokenNewline {
		p.advance()
	}
}

func (p *Parser) advance() {
	if p.pos < len(p.tokens)-1 {
		p.pos++
		p.cur = p.tokens[p.pos]
	}
}
