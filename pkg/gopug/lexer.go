package gopug

import (
	"fmt"
	"strings"
)

// emitTextWithInterpolations splits a raw text string on #{...} and !{...}
// interpolation markers and emits the appropriate tokens (TokenText,
// TokenInterpolation, TokenInterpolationUnescape).  Plain text segments are
// emitted as TokenText; interpolated expressions are emitted as
// TokenInterpolation (escaped) or TokenInterpolationUnescape (!{}).
func (l *Lexer) emitTextWithInterpolations(text string, depth int) {
	savedDepth := l.depth
	l.depth = depth

	for len(text) > 0 {
		// Find the earliest interpolation marker
		hashIdx := strings.Index(text, "#{")
		bangIdx := strings.Index(text, "!{")

		// Determine which comes first (ignore -1 as "not found")
		first := -1
		isUnescaped := false
		markerLen := 2

		if hashIdx >= 0 && (bangIdx < 0 || hashIdx <= bangIdx) {
			first = hashIdx
			isUnescaped = false
		} else if bangIdx >= 0 {
			first = bangIdx
			isUnescaped = true
		}

		if first < 0 {
			// No more interpolations — emit remaining text as-is
			if text != "" {
				l.addToken(TokenText, text)
			}
			break
		}

		// Emit any plain text before the marker
		if first > 0 {
			l.addToken(TokenText, text[:first])
		}

		// Skip past the marker (#{ or !{)
		rest := text[first+markerLen:]

		// Scan balanced braces to find the closing }
		expr, remaining, ok := scanBalancedBraces(rest)
		if !ok {
			// Malformed interpolation — treat the rest as plain text
			l.addToken(TokenText, text[first:])
			break
		}

		if isUnescaped {
			l.addToken(TokenInterpolationUnescape, expr)
		} else {
			l.addToken(TokenInterpolation, expr)
		}

		text = remaining
	}

	l.depth = savedDepth
}

// scanBalancedBraces reads characters from s up to (but not including) the
// matching closing brace, handling nested braces and quoted strings.
// Returns (expr, remaining, ok).  remaining starts after the closing }.
func scanBalancedBraces(s string) (string, string, bool) {
	depth := 0
	inDouble := false
	inSingle := false
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '\\' && (inDouble || inSingle) {
			i += 2
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
		} else if ch == '\'' && !inDouble {
			inSingle = !inSingle
		} else if !inDouble && !inSingle {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				if depth == 0 {
					return s[:i], s[i+1:], true
				}
				depth--
			}
		}
		i++
	}
	return "", "", false
}

// Lexer tokenizes Pug source code.
type Lexer struct {
	input  string
	pos    int
	line   int
	col    int
	start  int
	tokens []Token
	depth  int // current indentation depth
}

// NewLexer creates a new lexer for the given input.
func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
		line:  1,
		col:   0,
	}
}

// Lex tokenizes the entire input and returns a slice of tokens.
func (l *Lexer) Lex() ([]Token, error) {
	for l.pos < len(l.input) {
		l.start = l.pos
		if err := l.scanLine(); err != nil {
			return nil, err
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenEOF, Line: l.line, Col: l.col})
	return l.tokens, nil
}

// scanLine processes a single line.
func (l *Lexer) scanLine() error {
	// Scan indentation
	indent := l.scanIndentation()
	if l.peek() == '\n' || l.peek() == '\r' || l.isEOF() {
		// Empty line
		l.skipToNewline()
		return nil
	}

	l.depth = indent

	// Determine what kind of line this is
	ch := l.peek()

	switch {
	case ch == '/':
		return l.scanComment()
	case ch == '-':
		return l.scanUnbufferedCode()
	case ch == '=':
		return l.scanBufferedCode()
	case ch == '!':
		return l.scanExclamation()
	case ch == '|':
		return l.scanPipedText()
	case ch == '<':
		return l.scanLiteralHTML()
	case ch == '+':
		return l.scanMixinCall()
	case ch == ':':
		return l.scanFilter()
	case ch == '.':
		return l.scanDotStart()
	case ch == '#':
		return l.scanHashStart()
	case isAlpha(ch):
		return l.scanTagOrKeyword()
	default:
		return l.errorf("unexpected character: %c", ch)
	}
}

// scanIndentation counts leading spaces/tabs and returns the depth.
func (l *Lexer) scanIndentation() int {
	depth := 0
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ' ' {
			depth++
			l.pos++
			l.col++
		} else if ch == '\t' {
			depth += 4 // treat tab as 4 spaces
			l.pos++
			l.col++
		} else {
			break
		}
	}
	return depth
}

// scanComment handles // and //-
func (l *Lexer) scanComment() error {
	l.advance() // consume first /
	if !l.match('/') {
		return l.errorf("expected '/' after first '/'")
	}

	// Check for unbuffered comment //-
	unbuffered := false
	if l.match('-') {
		unbuffered = true
	}

	// Skip to end of line to get comment text
	text := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		text += string(l.advance())
	}
	text = strings.TrimSpace(text)

	if unbuffered {
		l.addToken(TokenCommentUnbuffered, text)
	} else {
		l.addToken(TokenComment, text)
	}

	l.skipToNewline()
	return nil
}

// scanUnbufferedCode handles - (unbuffered code block)
func (l *Lexer) scanUnbufferedCode() error {
	l.advance() // consume -
	l.skipSpaces()

	// Collect rest of line as code
	code := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		code += string(l.advance())
	}

	l.addToken(TokenCode, strings.TrimSpace(code))
	l.skipToNewline()
	return nil
}

// scanBufferedCode handles = (buffered code output)
func (l *Lexer) scanBufferedCode() error {
	l.advance() // consume =
	l.skipSpaces()

	code := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		code += string(l.advance())
	}

	l.addToken(TokenCodeBuffered, strings.TrimSpace(code))
	l.skipToNewline()
	return nil
}

// scanExclamation handles ! (unescaped code or !=)
func (l *Lexer) scanExclamation() error {
	l.advance() // consume !
	if !l.match('=') {
		return l.errorf("expected '=' after '!'")
	}
	l.skipSpaces()

	code := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		code += string(l.advance())
	}

	l.addToken(TokenCodeUnescaped, strings.TrimSpace(code))
	l.skipToNewline()
	return nil
}

// scanPipedText handles | (piped text)
func (l *Lexer) scanPipedText() error {
	l.advance() // consume |
	if l.peek() == ' ' {
		l.advance()
	}

	text := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		text += string(l.advance())
	}

	// Split on #{} / !{} interpolations
	l.emitTextWithInterpolations(text, l.depth)
	l.skipToNewline()
	return nil
}

// scanLiteralHTML handles < (literal HTML)
func (l *Lexer) scanLiteralHTML() error {
	html := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		html += string(l.advance())
	}

	l.addToken(TokenHTMLLiteral, html)
	l.skipToNewline()
	return nil
}

// scanMixinCall handles + (mixin call)
func (l *Lexer) scanMixinCall() error {
	l.advance() // consume +
	l.skipSpaces()

	name := l.scanIdentifier()
	if name == "" {
		return l.errorf("expected mixin name after '+'")
	}

	l.addToken(TokenMixinCall, name)
	return l.scanTagRest()
}

// scanFilter handles : (filter)
func (l *Lexer) scanFilter() error {
	l.advance() // consume :
	name := l.scanIdentifier()
	if name == "" {
		return l.errorf("expected filter name after ':'")
	}

	l.addToken(TokenFilter, name)

	// Check for nested filter :subfilter
	if l.peek() == ':' {
		l.advance()
		sub := l.scanIdentifier()
		if sub != "" {
			l.addToken(TokenFilterColon, sub)
		}
	}

	l.skipToNewline()
	return nil
}

// scanDotStart handles . at line start (implicit div with class or block text)
func (l *Lexer) scanDotStart() error {
	l.advance() // consume .
	className := l.scanIdentifier()

	if className == "" {
		// Standalone dot = block text indicator for a div
		l.addToken(TokenTag, "div")
		l.addToken(TokenDot, ".")
	} else {
		// .classname = implicit div with class
		l.addToken(TokenTag, "div")
		l.addToken(TokenClass, className)
	}

	return l.scanTagRest()
}

// scanHashStart handles # at line start (implicit div with ID or inline ID)
func (l *Lexer) scanHashStart() error {
	l.advance() // consume #
	idName := l.scanIdentifier()
	if idName == "" {
		return l.errorf("expected ID name after '#'")
	}

	l.addToken(TokenTag, "div")
	l.addToken(TokenID, idName)
	return l.scanTagRest()
}

// scanTagOrKeyword handles tag names and keywords
func (l *Lexer) scanTagOrKeyword() error {
	name := l.scanIdentifier()
	if name == "" {
		return l.errorf("expected identifier")
	}

	// Check if it's a keyword
	if tt, isKeyword := Keywords[name]; isKeyword {
		// Keywords often have arguments on the same line
		l.skipSpaces()
		if name == "doctype" {
			l.addToken(tt, name)
			// Doctype has an optional argument
			arg := ""
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				arg += string(l.advance())
			}
			if arg != "" {
				// Update last token with the full doctype value
				if len(l.tokens) > 0 {
					l.tokens[len(l.tokens)-1].Value = strings.TrimSpace(arg)
				}
			}
			l.skipToNewline()
			return nil
		}
		if name == "block" {
			// "block" may be followed by "append" or "prepend" modifier:
			//   block append <name>   → TokenBlockAppend{value: <name>}
			//   block prepend <name>  → TokenBlockPrepend{value: <name>}
			//   block <name>          → TokenBlock{value: <name>}
			modifier := l.scanIdentifier()
			l.skipSpaces()
			switch modifier {
			case "append":
				blockName := l.scanIdentifier()
				l.addToken(TokenBlockAppend, blockName)
			case "prepend":
				blockName := l.scanIdentifier()
				l.addToken(TokenBlockPrepend, blockName)
			default:
				// modifier is actually the block name itself
				l.addToken(TokenBlock, modifier)
			}
			l.skipToNewline()
			return nil
		}
		l.addToken(tt, name)
		// For other keywords (if, each, etc.), collect the rest as the condition
		cond := ""
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			cond += string(l.advance())
		}
		if cond != "" {
			l.tokens[len(l.tokens)-1].Value = strings.TrimSpace(cond)
		}
		l.skipToNewline()
		return nil
	}

	// It's a tag name
	l.addToken(TokenTag, name)
	return l.scanTagRest()
}

// scanTagRest handles everything after a tag name: classes, IDs, attributes, text, etc.
func (l *Lexer) scanTagRest() error {
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		ch := l.peek()

		switch ch {
		case '(':
			// Attributes
			l.advance()
			l.addToken(TokenAttrStart, "(")
			if err := l.scanAttributes(); err != nil {
				return err
			}

		case '.':
			l.advance()
			className := l.scanIdentifier()
			if className != "" {
				// .classname — class shorthand
				l.addToken(TokenClass, className)
			} else {
				// standalone dot — block text indicator
				l.addToken(TokenDot, ".")
				l.skipToNewline()
				return nil
			}

		case '#':
			// Distinguish #id shorthand from #{expr} tag-body interpolation.
			// If the character after # is '{', it is an inline interpolation that
			// belongs to the text content — collect the rest of the line as text
			// and let emitTextWithInterpolations handle it.
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '{' {
				// Collect from the current position to end of line as text
				text := ""
				for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
					text += string(l.advance())
				}
				l.emitTextWithInterpolations(text, l.depth)
				l.skipToNewline()
				return nil
			}
			// ID shorthand
			l.advance()
			idName := l.scanIdentifier()
			l.addToken(TokenID, idName)

		case ':':
			// Block expansion
			l.advance()
			l.addToken(TokenColon, ":")
			// After colon, scan the nested tag at increased depth
			l.skipSpaces()
			// Don't recurse here; let the parser handle block expansion
			return nil

		case '=':
			// Inline buffered code: p= expr
			l.advance()
			l.skipSpaces()
			code := ""
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				code += string(l.advance())
			}
			l.addToken(TokenCodeBuffered, strings.TrimSpace(code))
			l.skipToNewline()
			return nil

		case '!':
			// Inline unescaped code: p!= expr
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.advance() // consume !
				l.advance() // consume =
				l.skipSpaces()
				code := ""
				for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
					code += string(l.advance())
				}
				l.addToken(TokenCodeUnescaped, strings.TrimSpace(code))
				l.skipToNewline()
				return nil
			}
			// Not !=, fall through to default
			l.skipToNewline()
			return nil

		case '/':
			// Self-closing tag
			l.advance()
			l.addToken(TokenTagEnd, "/")

		case ' ', '\t':
			// Text content follows
			l.skipSpaces()
			text := ""
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				text += string(l.advance())
			}
			if text != "" {
				// Split on #{} / !{} interpolations
				l.emitTextWithInterpolations(text, l.depth)
			}
			l.skipToNewline()
			return nil

		default:
			// End of tag definition
			l.skipToNewline()
			return nil
		}
	}

	l.skipToNewline()
	return nil
}

// scanAttributes handles attributes inside ( ... )
func (l *Lexer) scanAttributes() error {
	for l.pos < len(l.input) && l.peek() != ')' {
		l.skipSpaces()

		if l.peek() == ')' {
			break
		}

		// Scan attribute name or positional expression.
		// An attribute is either:
		//   name          — boolean attribute or mixin positional arg (identifier only)
		//   name=value    — standard HTML attribute
		//   name!=value   — unescaped attribute
		//   "expr"        — positional mixin argument (quoted string, starts non-alpha)
		//   expr          — positional mixin argument (any expression, e.g. variable)
		//
		// Strategy: try to scan an identifier first.  If we get one AND the next
		// non-space character is = or !=, treat it as a named attribute.  Otherwise
		// treat whatever we scanned (identifier or raw expression) as a positional
		// argument emitted as TokenAttrName with no following TokenAttrEqual.
		var name string
		if isAlpha(l.peek()) || l.peek() == '_' {
			name = l.scanIdentifier()
		}

		if name == "" {
			// Non-identifier start (quoted string, number, etc.) — scan as a
			// raw expression value up to the next comma or closing paren.
			name = l.scanAttributeValue()
			if name == "" {
				return l.errorf("expected attribute name")
			}
			l.addToken(TokenAttrName, name)
			l.skipSpaces()
			if l.peek() == ',' {
				l.advance()
				l.addToken(TokenAttrComma, ",")
			}
			continue
		}

		l.addToken(TokenAttrName, name)

		l.skipSpaces()

		// Check for = or !=
		if l.peek() == '=' {
			l.advance()
			if l.peek() == '=' {
				// Handle == as a comparison, not an assignment
				l.advance()
				l.addToken(TokenAttrEqual, "==")
			} else {
				l.addToken(TokenAttrEqual, "=")
			}
		} else if l.peek() == '!' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
			l.advance()
			l.advance()
			l.addToken(TokenAttrEqualUnescape, "!=")
		} else {
			// Boolean attribute or positional identifier argument — no value follows.
			if l.peek() == ',' {
				l.advance()
				l.addToken(TokenAttrComma, ",")
			}
			continue
		}

		l.skipSpaces()

		// Scan attribute value
		value := l.scanAttributeValue()
		l.addToken(TokenAttrValue, value)

		l.skipSpaces()

		if l.peek() == ',' {
			l.advance()
			l.addToken(TokenAttrComma, ",")
		}
	}

	if l.peek() != ')' {
		return l.errorf("expected ')' to close attributes")
	}
	l.advance()
	l.addToken(TokenAttrEnd, ")")
	return nil
}

// scanAttributeValue scans a quoted string, backtick string, or expression.
func (l *Lexer) scanAttributeValue() string {
	if l.peek() == '"' {
		return l.scanQuotedString('"')
	}
	if l.peek() == '\'' {
		return l.scanQuotedString('\'')
	}
	if l.peek() == '`' {
		return l.scanQuotedString('`')
	}

	// Unquoted value: read until comma, closing paren, or end of line
	value := ""
	depth := 0
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		ch := l.peek()
		if ch == '(' || ch == '[' || ch == '{' {
			depth++
		} else if ch == ')' || ch == ']' || ch == '}' {
			if depth == 0 && ch == ')' {
				break
			}
			depth--
		} else if ch == ',' && depth == 0 {
			break
		}
		value += string(l.advance())
	}
	return strings.TrimSpace(value)
}

// scanQuotedString scans a quoted string and returns its content (including quotes).
func (l *Lexer) scanQuotedString(quote rune) string {
	value := string(l.advance()) // opening quote
	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '\\' {
			value += string(l.advance())
			if l.pos < len(l.input) {
				value += string(l.advance())
			}
		} else if rune(ch) == quote {
			value += string(l.advance()) // closing quote
			break
		} else {
			value += string(l.advance())
		}
	}
	return value
}

// scanIdentifier reads an identifier (alphanumeric + underscore/hyphen)
func (l *Lexer) scanIdentifier() string {
	start := l.pos
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if isAlpha(ch) || isDigit(ch) || ch == '-' || ch == '_' {
			l.pos++
			l.col++
		} else {
			break
		}
	}
	return l.input[start:l.pos]
}

// Helper methods

func (l *Lexer) peek() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) advance() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	ch := l.input[l.pos]
	l.pos++
	l.col++
	if ch == '\n' {
		l.line++
		l.col = 0
	}
	return ch
}

func (l *Lexer) match(expected byte) bool {
	if l.pos >= len(l.input) || l.input[l.pos] != expected {
		return false
	}
	l.advance()
	return true
}

func (l *Lexer) skipSpaces() {
	for l.pos < len(l.input) && (l.peek() == ' ' || l.peek() == '\t') {
		l.advance()
	}
}

func (l *Lexer) skipToNewline() {
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advance()
	}
	if l.peek() == '\r' {
		l.advance()
	}
	if l.peek() == '\n' {
		l.advance()
	}
}

func (l *Lexer) isEOF() bool {
	return l.pos >= len(l.input)
}

func (l *Lexer) addToken(tt TokenType, value string) {
	l.tokens = append(l.tokens, Token{
		Type:  tt,
		Value: value,
		Line:  l.line,
		Col:   l.col,
		Depth: l.depth,
	})
}

func (l *Lexer) errorf(format string, args ...interface{}) error {
	return fmt.Errorf("lexer error at line %d, col %d: %s", l.line, l.col, fmt.Sprintf(format, args...))
}

// isAlpha checks if a character is alphabetic.
func isAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

// isDigit checks if a character is a digit.
func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}
