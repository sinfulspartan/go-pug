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
	for p.cur.Type == TokenClass || p.cur.Type == TokenID || p.cur.Type == TokenAttrStart || p.cur.Type == TokenDot || p.cur.Type == TokenColon || p.cur.Type == TokenTagEnd {
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

	// Inline text
	if p.cur.Type == TokenText && p.cur.Depth == currentDepth {
		tag.Children = append(tag.Children, &TextNode{Content: p.cur.Value, Line: p.cur.Line, Col: p.cur.Col})
		p.advance()
		p.skipNewlines()
		return tag, nil
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
					tag.Attributes[name] = &AttributeValue{Value: value, Unescaped: unescaped}
					p.advance()
				} else {
					return fmt.Errorf("expected attribute value at line %d", p.cur.Line)
				}
			} else {
				// Boolean attribute
				tag.Attributes[name] = &AttributeValue{Value: `"` + name + `"`, Unescaped: false}
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
	p.advance()
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
func (p *Parser) parsePipedText() (*PipeNode, error) {
	content := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()
	p.skipNewlines()
	return &PipeNode{Content: content, Line: line, Col: col}, nil
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
func (p *Parser) parseConditional() (*ConditionalNode, error) {
	cond := &ConditionalNode{
		Condition:  p.cur.Value,
		Consequent: make([]Node, 0),
		Alternate:  make([]Node, 0),
		IsUnless:   false,
		Line:       p.cur.Line,
		Col:        p.cur.Col,
	}

	currentDepth := p.cur.Depth
	p.advance()
	p.skipNewlines()

	// Parse consequent
	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		if p.cur.Type == TokenElse {
			// else or else if
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

	// Parse else/else if.
	// The lexer emits "else if <cond>" as a single TokenElse whose Value starts with "if ".
	// A bare "else" is emitted as TokenElse with Value "else" (or empty).
	for p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
		elseToken := p.cur
		p.advance() // consume the TokenElse token
		p.skipNewlines()

		// Detect "else if …" — the lexer folds "else if <cond>" into one token
		// whose Value is "if <cond>".
		if strings.HasPrefix(elseToken.Value, "if ") {
			elseIfCond := strings.TrimPrefix(elseToken.Value, "if ")
			elseCond := &ConditionalNode{
				Condition:  strings.TrimSpace(elseIfCond),
				Consequent: make([]Node, 0),
				Alternate:  make([]Node, 0),
				IsElseIf:   true,
				Line:       elseToken.Line,
				Col:        elseToken.Col,
			}

			// Parse the else-if consequent body
			for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
				if p.cur.Type == TokenElse {
					break
				}
				node, err := p.parseNode(currentDepth + 1)
				if err != nil {
					return nil, err
				}
				if node != nil {
					elseCond.Consequent = append(elseCond.Consequent, node)
				}
				p.skipNewlines()
			}

			// Recursively collect any further else-if / else chains into this node
			for p.cur.Type == TokenElse && p.cur.Depth == currentDepth {
				innerElse := p.cur
				p.advance()
				p.skipNewlines()

				if strings.HasPrefix(innerElse.Value, "if ") {
					innerCond := strings.TrimSpace(strings.TrimPrefix(innerElse.Value, "if "))
					innerNode := &ConditionalNode{
						Condition:  innerCond,
						Consequent: make([]Node, 0),
						Alternate:  make([]Node, 0),
						IsElseIf:   true,
						Line:       innerElse.Line,
						Col:        innerElse.Col,
					}
					for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
						if p.cur.Type == TokenElse {
							break
						}
						node, err := p.parseNode(currentDepth + 1)
						if err != nil {
							return nil, err
						}
						if node != nil {
							innerNode.Consequent = append(innerNode.Consequent, node)
						}
						p.skipNewlines()
					}
					elseCond.Alternate = append(elseCond.Alternate, innerNode)
					elseCond = innerNode
				} else {
					// bare else — collect its body into the current innermost else-if
					for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
						node, err := p.parseNode(currentDepth + 1)
						if err != nil {
							return nil, err
						}
						if node != nil {
							elseCond.Alternate = append(elseCond.Alternate, node)
						}
						p.skipNewlines()
					}
					break
				}
			}

			cond.Alternate = append(cond.Alternate, elseCond)
		} else {
			// bare else — collect body directly into cond.Alternate
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
			break
		}
	}

	return cond, nil
}

// parseUnless parses unless blocks.
func (p *Parser) parseUnless() (*ConditionalNode, error) {
	cond := &ConditionalNode{
		Condition:  p.cur.Value,
		Consequent: make([]Node, 0),
		IsUnless:   true,
		Line:       p.cur.Line,
		Col:        p.cur.Col,
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
			cond.Consequent = append(cond.Consequent, node)
		}
		p.skipNewlines()
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
func (p *Parser) parseMixinDecl() (*MixinDeclNode, error) {
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()

	mixin := &MixinDeclNode{
		Name:       name,
		Parameters: make([]string, 0),
		Body:       make([]Node, 0),
		Line:       line,
		Col:        col,
	}

	// TODO: Parse parameters from attributes

	currentDepth := p.cur.Depth
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

// parseMixinCall parses mixin calls.
func (p *Parser) parseMixinCall() (*MixinCallNode, error) {
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()

	call := &MixinCallNode{
		Name:       name,
		Arguments:  make([]string, 0),
		Attributes: make(map[string]*AttributeValue),
		Block:      make([]Node, 0),
		Line:       line,
		Col:        col,
	}

	// TODO: Parse arguments and attributes

	currentDepth := p.cur.Depth
	p.skipNewlines()

	// Mixin calls can have inline content
	if p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
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
	p.advance()

	block := &BlockNode{
		Name: name,
		Mode: mode,
		Body: make([]Node, 0),
		Line: line,
		Col:  col,
	}

	currentDepth := p.cur.Depth
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

// parseBlockModifier parses standalone append/prepend.
func (p *Parser) parseBlockModifier() (*BlockNode, error) {
	mode := BlockModeAppend
	if p.cur.Type == TokenPrepend {
		mode = BlockModePrepend
	}

	p.advance()
	if p.cur.Type != TokenBlock {
		return nil, fmt.Errorf("expected block name after append/prepend at line %d", p.cur.Line)
	}

	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()

	block := &BlockNode{
		Name: name,
		Mode: mode,
		Body: make([]Node, 0),
		Line: line,
		Col:  col,
	}

	currentDepth := p.cur.Depth
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
func (p *Parser) parseInclude() (*IncludeNode, error) {
	path := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()

	include := &IncludeNode{
		Path: path,
		Line: line,
		Col:  col,
	}

	// Check for filter
	if p.cur.Type == TokenFilter {
		include.Filter = p.cur.Value
		p.advance()
	}

	p.skipNewlines()
	return include, nil
}

// parseFilter parses filter blocks.
func (p *Parser) parseFilter() (*FilterNode, error) {
	name := p.cur.Value
	line := p.cur.Line
	col := p.cur.Col
	p.advance()

	filter := &FilterNode{
		Name:    name,
		Args:    "",
		Content: "",
		Line:    line,
		Col:     col,
	}

	// Check for subfilter
	if p.cur.Type == TokenFilterColon {
		filter.Subfilter = p.cur.Value
		p.advance()
	}

	currentDepth := p.cur.Depth
	p.skipNewlines()

	// Collect filter body text
	content := ""
	for p.cur.Type != TokenEOF && p.cur.Depth > currentDepth {
		if p.cur.Type == TokenText || p.cur.Type == TokenPipe {
			content += p.cur.Value + "\n"
		}
		p.advance()
	}
	filter.Content = strings.TrimSpace(content)

	return filter, nil
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
