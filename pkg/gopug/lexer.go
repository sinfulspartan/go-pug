package gopug

import (
	"fmt"
	"strings"
)

// emitTextWithInterpolations splits a raw text string on #{...}, !{...}, and
// #[...] markers and emits the appropriate tokens. Plain text segments become
// TokenText; #{expr} becomes TokenInterpolation; !{expr} becomes
// TokenInterpolationUnescape; #[tag] becomes a TokenTagInterpolationStart /
// TokenTagInterpolationEnd pair. A backslash before #{ is treated as an
// escape: \#{ emits a literal "#{" TokenText rather than an interpolation.
func (l *Lexer) emitTextWithInterpolations(text string, depth int) {
	savedDepth := l.indentDepth
	l.indentDepth = depth

	for len(text) > 0 {
		// Find the earliest interpolation marker: #{expr}, !{expr}, or #[tag]
		// Skip any marker that is preceded by a backslash (\#{) — that is an
		// escaped interpolation and should be emitted as literal "#{".
		hashBraceIdx := -1
		for i := 0; i < len(text)-1; i++ {
			if text[i] == '#' && text[i+1] == '{' {
				if i == 0 || text[i-1] != '\\' {
					hashBraceIdx = i
					break
				}
				// Escaped: replace \#{ with #{ as plain text and skip.
				// We do this by breaking the text at the backslash, emitting
				// everything before it, then continuing with the rest (minus \).
				if i > 0 {
					l.addToken(TokenText, text[:i-1])
				}
				l.addToken(TokenText, "#{")
				text = text[i+2:]
				hashBraceIdx = -2 // sentinel: restart outer loop
				break
			}
		}
		if hashBraceIdx == -2 {
			continue
		}

		bangIdx := strings.Index(text, "!{")
		hashBracketIdx := -1
		for i := 0; i < len(text)-1; i++ {
			if text[i] == '#' && text[i+1] == '[' {
				if i == 0 || text[i-1] != '\\' {
					hashBracketIdx = i
					break
				}
			}
		}

		type marker struct {
			pos       int
			kind      string // "expr", "unescape", "taginterp"
			markerLen int
		}
		candidates := []marker{}
		if hashBraceIdx >= 0 {
			candidates = append(candidates, marker{hashBraceIdx, "expr", 2})
		}
		if bangIdx >= 0 {
			candidates = append(candidates, marker{bangIdx, "unescape", 2})
		}
		if hashBracketIdx >= 0 {
			candidates = append(candidates, marker{hashBracketIdx, "taginterp", 2})
		}

		if len(candidates) == 0 {
			if text != "" {
				l.addToken(TokenText, text)
			}
			break
		}

		best := candidates[0]
		for _, c := range candidates[1:] {
			if c.pos < best.pos {
				best = c
			}
		}

		if best.pos > 0 {
			l.addToken(TokenText, text[:best.pos])
		}

		rest := text[best.pos+best.markerLen:]

		switch best.kind {
		case "expr":
			expr, remaining, ok := scanBalancedBraces(rest)
			if !ok {
				l.addToken(TokenText, text[best.pos:])
				text = ""
				break
			}
			l.addToken(TokenInterpolation, expr)
			text = remaining

		case "unescape":
			expr, remaining, ok := scanBalancedBraces(rest)
			if !ok {
				l.addToken(TokenText, text[best.pos:])
				text = ""
				break
			}
			l.addToken(TokenInterpolationUnescape, expr)
			text = remaining

		case "taginterp":
			inner, remaining, ok := scanBalancedBrackets(rest)
			if !ok {
				l.addToken(TokenText, text[best.pos:])
				text = ""
				break
			}
			l.addToken(TokenTagInterpolationStart, inner)
			l.addToken(TokenTagInterpolationEnd, "")
			text = remaining
		}
	}

	l.indentDepth = savedDepth
}

// scanBalancedBraces returns (expr, remaining, ok); remaining starts after the closing }.
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

// scanBalancedBrackets returns (inner, remaining, ok); remaining starts after the closing ].
func scanBalancedBrackets(s string) (string, string, bool) {
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
			if ch == '[' {
				depth++
			} else if ch == ']' {
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

type Lexer struct {
	input       string
	pos         int
	line        int
	col         int
	tokens      []Token
	indentDepth int
}

func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
		line:  1,
		col:   0,
	}
}

func (l *Lexer) Lex() ([]Token, error) {
	for l.pos < len(l.input) {
		if err := l.scanLine(); err != nil {
			return nil, err
		}
	}
	l.tokens = append(l.tokens, Token{Type: TokenEOF, Line: l.line, Col: l.col})
	return l.tokens, nil
}

func (l *Lexer) scanLine() error {
	indent := l.scanIndentation()
	if l.peek() == '\n' || l.peek() == '\r' || l.isEOF() {
		l.skipToNewline()
		return nil
	}

	l.indentDepth = indent

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

func (l *Lexer) scanIndentation() int {
	depth := 0
loop:
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		switch ch {
		case ' ':
			depth++
			l.pos++
			l.col++
		case '\t':
			depth += 4 // treat tab as 4 spaces
			l.pos++
			l.col++
		default:
			break loop
		}
	}
	return depth
}

func (l *Lexer) scanComment() error {
	l.advance() // consume first /
	if !l.match('/') {
		return l.errorf("expected '/' after first '/'")
	}

	unbuffered := l.match('-')
	commentDepth := l.indentDepth

	var textB strings.Builder
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advanceInto(&textB)
	}
	text := strings.TrimSpace(textB.String())

	if unbuffered {
		l.addToken(TokenCommentUnbuffered, text)
	} else {
		l.addToken(TokenComment, text)
	}

	// Eagerly consume all indented body lines that follow the comment header,
	// emitting them as TokenText so the main scanLine dispatcher never
	// re-interprets comment body content as Pug tags/keywords.
	l.skipToNewline()

	for l.pos < len(l.input) {
		savedPos := l.pos
		savedLine := l.line
		savedCol := l.col

		bodyIndent := 0
		for l.pos < len(l.input) && (l.peek() == ' ' || l.peek() == '\t') {
			l.advance()
			bodyIndent++
		}

		if l.isEOF() || l.peek() == '\n' || l.peek() == '\r' {
			l.skipToNewline()
			continue
		}

		if bodyIndent <= commentDepth {
			l.pos = savedPos
			l.line = savedLine
			l.col = savedCol
			break
		}

		l.indentDepth = bodyIndent
		var lineContentB strings.Builder
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			l.advanceInto(&lineContentB)
		}
		l.addToken(TokenText, lineContentB.String())
		l.skipToNewline()
	}

	return nil
}

func (l *Lexer) scanUnbufferedCode() error {
	l.advance() // consume -
	l.skipSpaces()

	var codeB strings.Builder
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advanceInto(&codeB)
	}

	headerDepth := l.indentDepth
	trimmed := strings.TrimSpace(codeB.String())
	l.skipToNewline()

	// A bare "-" line introduces an indented block of code statements —
	// one TokenCode per line.
	//
	//   -
	//     var x = 1
	//     var y = 2
	if trimmed == "" {
		for l.pos < len(l.input) {
			savedPos := l.pos
			savedLine := l.line
			savedCol := l.col

			bodyIndent := 0
			for l.pos < len(l.input) && (l.peek() == ' ' || l.peek() == '\t') {
				l.advance()
				bodyIndent++
			}

			if l.isEOF() || l.peek() == '\n' || l.peek() == '\r' {
				l.skipToNewline()
				continue
			}

			if bodyIndent <= headerDepth {
				l.pos = savedPos
				l.line = savedLine
				l.col = savedCol
				break
			}

			var lineContentB strings.Builder
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				l.advanceInto(&lineContentB)
			}
			stmt := strings.TrimSpace(lineContentB.String())
			if stmt != "" {
				l.addToken(TokenCode, stmt)
			}
			l.skipToNewline()
		}
		return nil
	}

	l.addToken(TokenCode, trimmed)
	return nil
}

func (l *Lexer) scanBufferedCode() error {
	l.advance() // consume =
	l.skipSpaces()

	var codeB strings.Builder
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advanceInto(&codeB)
	}

	l.addToken(TokenCodeBuffered, strings.TrimSpace(codeB.String()))
	l.skipToNewline()
	return nil
}

func (l *Lexer) scanExclamation() error {
	l.advance() // consume !
	if !l.match('=') {
		return l.errorf("expected '=' after '!'")
	}
	l.skipSpaces()

	var codeB strings.Builder
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advanceInto(&codeB)
	}

	l.addToken(TokenCodeUnescaped, strings.TrimSpace(codeB.String()))
	l.skipToNewline()
	return nil
}

func (l *Lexer) scanPipedText() error {
	l.advance() // consume |
	if l.peek() == ' ' {
		l.advance()
	}

	var textB strings.Builder
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advanceInto(&textB)
	}

	l.emitTextWithInterpolations(textB.String(), l.indentDepth)
	l.skipToNewline()
	return nil
}

func (l *Lexer) scanLiteralHTML() error {
	var htmlB strings.Builder
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advanceInto(&htmlB)
	}

	l.addToken(TokenHTMLLiteral, htmlB.String())
	l.skipToNewline()
	return nil
}

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

// scanFilter handles a filter line starting with ':'. It emits TokenFilter for
// the filter name, optional TokenFilterColon tokens for chained subfilters
// (:outer:inner), and a TokenFilterOptions token for any (key=val) options
// block. Body lines indented deeper than the filter header are eagerly consumed
// as TokenText so the main scanLine dispatcher never re-interprets them as Pug.
func (l *Lexer) scanFilter() error {
	l.advance() // consume :
	name := l.scanIdentifier()
	if name == "" {
		return l.errorf("expected filter name after ':'")
	}

	filterDepth := l.indentDepth
	l.addToken(TokenFilter, name)

	// Subfilters may appear before OR after the options block:
	//   :outer:inner(opts)      — subfilter before options
	//   :outer(opts):inner      — subfilter after options
	//   :outer:mid(opts):inner  — mixed
	scanSubfilters := func() {
		for l.peek() == ':' {
			l.advance()
			sub := l.scanIdentifier()
			if sub != "" {
				l.addToken(TokenFilterColon, sub)
			}
		}
	}

	scanSubfilters()

	l.skipSpaces()
	if l.peek() == '(' {
		l.advance() // consume '('
		var rawB strings.Builder
		depth := 1
		for l.pos < len(l.input) && depth > 0 {
			ch := l.peek()
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					l.advance() // consume closing ')'
					break
				}
			}
			l.advanceInto(&rawB)
		}
		l.addToken(TokenFilterOptions, strings.TrimSpace(rawB.String()))
		l.skipSpaces()
	}

	scanSubfilters()

	var inlineB strings.Builder
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		l.advanceInto(&inlineB)
	}
	inline := inlineB.String()
	if inline != "" {
		l.addToken(TokenText, strings.TrimRight(inline, " \t"))
		l.skipToNewline()
		return nil
	}

	// A body line must have strictly MORE leading spaces than the filter header.
	// filterDepth uses the same raw space count set by scanLine before calling us.
	l.skipToNewline()

	for l.pos < len(l.input) {
		savedPos := l.pos
		savedLine := l.line
		savedCol := l.col

		bodyIndent := 0
		for l.pos < len(l.input) && (l.peek() == ' ' || l.peek() == '\t') {
			l.advance()
			bodyIndent++
		}

		if l.peek() == '\n' || l.peek() == '\r' || l.isEOF() {
			l.skipToNewline()
			continue
		}

		if bodyIndent <= filterDepth {
			l.pos = savedPos
			l.line = savedLine
			l.col = savedCol
			break
		}

		var lineContentB strings.Builder
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			l.advanceInto(&lineContentB)
		}
		l.addToken(TokenText, lineContentB.String())
		l.skipToNewline()
	}

	return nil
}

func (l *Lexer) scanDotStart() error {
	l.advance() // consume .
	className := l.scanIdentifier()

	if className == "" {
		l.addToken(TokenTag, "div")
		l.addToken(TokenDot, ".")
	} else {
		l.addToken(TokenTag, "div")
		l.addToken(TokenClass, className)
	}

	return l.scanTagRest()
}

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

func (l *Lexer) scanTagOrKeyword() error {
	name := l.scanIdentifier()
	if name == "" {
		return l.errorf("expected identifier")
	}

	if tt, isKeyword := Keywords[name]; isKeyword {
		l.skipSpaces()
		if name == "doctype" {
			l.addToken(tt, name)
			var argB strings.Builder
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				l.advanceInto(&argB)
			}
			arg := argB.String()
			if arg != "" {
				if len(l.tokens) > 0 {
					l.tokens[len(l.tokens)-1].Value = strings.TrimSpace(arg)
				}
			}
			l.skipToNewline()
			return nil
		}
		if name == "block" {
			// "block" may be followed by "append" or "prepend":
			//   block append <name>  → TokenBlockAppend{value: <name>}
			//   block prepend <name> → TokenBlockPrepend{value: <name>}
			//   block <name>         → TokenBlock{value: <name>}
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
		// For other keywords (if, each, while, …) the rest of the line is the
		// condition/expression — fold it into the token value.
		var condB strings.Builder
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			l.advanceInto(&condB)
		}
		cond := condB.String()
		if cond != "" {
			l.tokens[len(l.tokens)-1].Value = strings.TrimSpace(cond)
		}
		l.skipToNewline()
		return nil
	}

	l.addToken(TokenTag, name)
	return l.scanTagRest()
}

// scanBlockTextBody is called after a block-text dot (p.) so the main scanLine
// dispatcher never tries to re-parse the literal body lines as Pug tags or keywords.
func (l *Lexer) scanBlockTextBody(headerDepth int) {
	l.skipToNewline()
	for l.pos < len(l.input) {
		savedPos := l.pos
		savedLine := l.line
		savedCol := l.col

		bodyIndent := 0
		for l.pos < len(l.input) && (l.peek() == ' ' || l.peek() == '\t') {
			l.advance()
			bodyIndent++
		}

		if l.isEOF() || l.peek() == '\n' || l.peek() == '\r' {
			l.skipToNewline()
			continue
		}

		if bodyIndent <= headerDepth {
			l.pos = savedPos
			l.line = savedLine
			l.col = savedCol
			break
		}

		l.indentDepth = bodyIndent
		var lineContentB strings.Builder
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			l.advanceInto(&lineContentB)
		}
		l.addToken(TokenText, lineContentB.String())
		l.skipToNewline()
	}
}

func (l *Lexer) scanTagRest() error {
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		ch := l.peek()

		switch ch {
		case '(':
			l.advance()
			l.addToken(TokenAttrStart, "(")
			if err := l.scanAttributes(); err != nil {
				return err
			}

		case '.':
			l.advance()
			className := l.scanIdentifier()
			if className != "" {
				l.addToken(TokenClass, className)
			} else {
				// Standalone dot — block text indicator. Eagerly consume all
				// indented body lines so the main dispatcher never re-parses them.
				l.addToken(TokenDot, ".")
				l.scanBlockTextBody(l.indentDepth)
				return nil
			}

		case '&':
			if strings.HasPrefix(l.input[l.pos:], "&attributes(") {
				for i := 0; i < len("&attributes("); i++ {
					l.advance()
				}
				var exprB strings.Builder
				depth := 1
				for l.pos < len(l.input) && depth > 0 {
					ch := l.peek()
					if ch == '(' {
						depth++
					} else if ch == ')' {
						depth--
						if depth == 0 {
							l.advance()
							break
						}
					}
					l.advanceInto(&exprB)
				}
				expr := exprB.String()
				l.addToken(TokenAttrName, "&attributes")
				l.addToken(TokenAttrEqual, "=")
				l.addToken(TokenAttrValue, expr)
			} else {
				l.skipToNewline()
				return nil
			}

		case '#':
			// Distinguish #id shorthand from #{expr} tag-body interpolation.
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '{' {
				var textB strings.Builder
				for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
					l.advanceInto(&textB)
				}
				l.emitTextWithInterpolations(textB.String(), l.indentDepth)
				l.skipToNewline()
				return nil
			}
			l.advance()
			idName := l.scanIdentifier()
			l.addToken(TokenID, idName)

		case ':':
			// We must scan the child tag immediately — still on the same logical
			// line — so that all tokens it produces carry the correct indentDepth
			// (the parent's indentation level). If we returned here and let Lex
			// call scanLine again, scanLine would invoke scanIndentation which
			// counts zero leading spaces and reset indentDepth to 0, making the
			// child appear at the wrong depth and causing the parser to nest
			// subsequent siblings inside it.
			l.advance()
			l.addToken(TokenColon, ":")
			l.skipSpaces()
			if !l.isEOF() && l.peek() != '\n' && l.peek() != '\r' {
				if err := l.scanTagOrKeyword(); err != nil {
					return err
				}
			}
			return nil

		case '=':
			l.advance()
			l.skipSpaces()
			var codeB strings.Builder
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				l.advanceInto(&codeB)
			}
			l.addToken(TokenCodeBuffered, strings.TrimSpace(codeB.String()))
			l.skipToNewline()
			return nil

		case '!':
			if l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
				l.advance() // consume !
				l.advance() // consume =
				l.skipSpaces()
				var codeB strings.Builder
				for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
					l.advanceInto(&codeB)
				}
				l.addToken(TokenCodeUnescaped, strings.TrimSpace(codeB.String()))
				l.skipToNewline()
				return nil
			}
			l.skipToNewline()
			return nil

		case '/':
			l.advance()
			l.addToken(TokenTagEnd, "/")

		case ' ', '\t':
			l.skipSpaces()
			var textB strings.Builder
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				l.advanceInto(&textB)
			}
			text := textB.String()
			if text != "" {
				l.emitTextWithInterpolations(text, l.indentDepth)
			}
			l.skipToNewline()
			return nil

		default:
			l.skipToNewline()
			return nil
		}
	}

	l.skipToNewline()
	return nil
}

// scanAttributes: tries to scan an identifier first; if one is found and
// followed by = or !=, treats it as a named attribute; otherwise emits it as
// a positional TokenAttrName with no following TokenAttrEqual.
func (l *Lexer) scanAttributes() error {
	for l.pos < len(l.input) && l.peek() != ')' {
		l.skipSpaces()

		if l.peek() == ')' {
			break
		}

		var name string
		if isAlpha(l.peek()) || l.peek() == '_' || l.peek() == '@' || l.peek() == ':' {
			name = l.scanAttrName()
		}

		if name == "" {
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

		if l.peek() == '=' {
			l.advance()
			l.addToken(TokenAttrEqual, "=")
		} else if l.peek() == '!' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=' {
			l.advance()
			l.advance()
			l.addToken(TokenAttrEqualUnescape, "!=")
		} else {
			if l.peek() == ',' {
				l.advance()
				l.addToken(TokenAttrComma, ",")
			}
			continue
		}

		l.skipSpaces()

		value := l.scanAttrValueFull()
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

// scanAttributeValue scans a single operand: a quoted string or an unquoted
// token (identifier, number, bare keyword).  Operator stitching across spaces
// is handled by the caller, scanAttrValueFull.
func (l *Lexer) scanAttributeValue() string {
	if l.peek() == '"' || l.peek() == '\'' || l.peek() == '`' {
		q := l.peek()
		return l.scanQuotedString(rune(q))
	}

	// Unquoted value: read until whitespace (at depth 0), comma, closing
	// paren, or end of line.  Whitespace at depth 0 is a potential attribute
	// separator; the caller (scanAttrValueFull) will decide whether to extend
	// the value across an operator that follows the space.
	var valueB strings.Builder
	depth := 0
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		ch := l.peek()
		if (ch == ' ' || ch == '\t') && depth == 0 {
			break
		}
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
		l.advanceInto(&valueB)
	}
	return strings.TrimSpace(valueB.String())
}

// isAttrOpChar reports whether ch is an infix operator character that can
// appear between two sub-expressions in an attribute value
// (e.g. the | in `a || b`, the ? in `c ? x : y`).
func isAttrOpChar(ch byte) bool {
	switch ch {
	case '|', '&', '?', ':', '+', '-', '*', '/', '!', '<', '>', '=':
		return true
	}
	return false
}

// scanAttrValueFull scans a complete attribute value expression, including
// space-separated operator continuations such as `a || b` and `x ? "y" : "z"`.
//
// The ambiguity between attribute separators and expression operators is
// resolved by peeking at the first non-space character after each token:
//
//   - If it is an infix operator char, we are still inside the expression →
//     consume the space, operator, and the next operand, then repeat.
//   - If it is anything else (letter, digit, _, @, :, ), ,, newline, EOF) →
//     we have reached the boundary between attributes → stop.
//
// Operands may themselves be quoted strings, bracket-balanced sub-expressions,
// or bare identifiers / numbers; scanAttributeValue handles all of those cases
// for the initial token, and the extension loop handles subsequent ones.
func (l *Lexer) scanAttrValueFull() string {
	// Scan the first operand.
	first := l.scanAttributeValue()
	if first == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(first)

	for {
		// Save position so we can backtrack if there is no operator ahead.
		saved := l.pos
		savedCol := l.col

		// Skip whitespace.
		for l.pos < len(l.input) && (l.input[l.pos] == ' ' || l.input[l.pos] == '\t') {
			l.pos++
			l.col++
		}

		// Nothing left, or hard boundary — restore and stop.
		if l.isEOF() || l.peek() == ')' || l.peek() == ',' ||
			l.peek() == '\n' || l.peek() == '\r' {
			l.pos = saved
			l.col = savedCol
			break
		}

		if !isAttrOpChar(l.peek()) {
			// Next non-space is not an operator → attribute boundary.
			l.pos = saved
			l.col = savedCol
			break
		}

		// Special case: a bare ':' that is immediately followed by a letter,
		// digit, '_', or '@' (with no intervening space) is the start of an
		// Alpine.js / x-bind attribute name (e.g. `:disabled`, `:class`),
		// not the else-branch of a ternary operator.  A ternary ':' always
		// has at least one space between the colon and the next operand
		// (e.g. `x ? "a" : "b"`), so the character right after ':' will be
		// a space when it is used as an operator.
		if l.peek() == ':' && l.pos+1 < len(l.input) {
			after := l.input[l.pos+1]
			if isAlpha(after) || isDigit(after) || after == '_' || after == '@' {
				// Looks like :attrName — treat as attribute boundary.
				l.pos = saved
				l.col = savedCol
				break
			}
		}

		// Consume the operator token (may be multi-char: ||, &&, <=, >=, !=, ==).
		b.WriteByte(' ')
		for l.pos < len(l.input) && isAttrOpChar(l.input[l.pos]) {
			b.WriteByte(l.input[l.pos])
			l.col++
			l.pos++
		}

		// Skip whitespace between operator and next operand.
		for l.pos < len(l.input) && (l.input[l.pos] == ' ' || l.input[l.pos] == '\t') {
			l.pos++
			l.col++
		}

		if l.isEOF() || l.peek() == ')' || l.peek() == ',' ||
			l.peek() == '\n' || l.peek() == '\r' {
			// Trailing operator with no operand — stop (leave at boundary).
			break
		}

		// Consume the next operand.
		b.WriteByte(' ')
		operand := l.scanAttributeValue()
		b.WriteString(operand)
	}

	return b.String()
}

func (l *Lexer) scanQuotedString(quote rune) string {
	var valueB strings.Builder
	l.advanceInto(&valueB) // opening quote
	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '\\' {
			l.advanceInto(&valueB)
			if l.pos < len(l.input) {
				l.advanceInto(&valueB)
			}
		} else if rune(ch) == quote {
			l.advanceInto(&valueB) // closing quote
			break
		} else {
			l.advanceInto(&valueB)
		}
	}
	return valueB.String()
}

// scanAttrName scans an attribute name, which may contain letters, digits,
// hyphens, underscores, colons, dots, and a leading @ or :.  It is broader
// than scanIdentifier (which is used for tag names, keywords, etc.) and must
// only be called from within scanAttributes.
//
// The broader character set supports framework attribute syntaxes such as:
//   - Alpine.js / Vue shorthand:  @click.prevent, :class, :key
//   - x-on long form with modifiers: x-on:click.outside, x-on:keyup.shift.enter
//   - x-bind long form: x-bind:placeholder, x-bind:class
func (l *Lexer) scanAttrName() string {
	start := l.pos
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if isAlpha(ch) || isDigit(ch) || ch == '-' || ch == '_' || ch == '@' || ch == ':' || ch == '.' {
			l.pos++
			l.col++
		} else {
			break
		}
	}
	return l.input[start:l.pos]
}

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

// advanceStr advances one byte and returns it as a string. Unlike
// string(l.advance()), which re-encodes the byte value as a Unicode code point
// (corrupting non-ASCII bytes), this preserves the raw byte.
func (l *Lexer) advanceStr() string {
	return string([]byte{l.advance()})
}

func (l *Lexer) advanceInto(b *strings.Builder) {
	b.WriteByte(l.advance())
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
		Type:        tt,
		Value:       value,
		Line:        l.line,
		Col:         l.col,
		IndentDepth: l.indentDepth,
	})
}

func (l *Lexer) errorf(format string, args ...any) error {
	return fmt.Errorf("lexer error at line %d, col %d: %s", l.line, l.col, fmt.Sprintf(format, args...))
}

func isAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}
