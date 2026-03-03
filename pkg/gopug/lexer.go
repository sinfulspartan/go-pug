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

		// Determine which marker comes first (treat -1 as "not found" / infinity)
		type marker struct {
			pos         int
			kind        string // "expr", "unescape", "taginterp"
			markerLen   int
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
			// No more interpolations — emit remaining text as-is
			if text != "" {
				l.addToken(TokenText, text)
			}
			break
		}

		// Pick the earliest marker
		best := candidates[0]
		for _, c := range candidates[1:] {
			if c.pos < best.pos {
				best = c
			}
		}

		// Emit any plain text before the marker
		if best.pos > 0 {
			l.addToken(TokenText, text[:best.pos])
		}

		rest := text[best.pos+best.markerLen:]

		switch best.kind {
		case "expr":
			// #{expr} — scan balanced braces
			expr, remaining, ok := scanBalancedBraces(rest)
			if !ok {
				l.addToken(TokenText, text[best.pos:])
				text = ""
				break
			}
			l.addToken(TokenInterpolation, expr)
			text = remaining

		case "unescape":
			// !{expr} — scan balanced braces
			expr, remaining, ok := scanBalancedBraces(rest)
			if !ok {
				l.addToken(TokenText, text[best.pos:])
				text = ""
				break
			}
			l.addToken(TokenInterpolationUnescape, expr)
			text = remaining

		case "taginterp":
			// #[tag content] — scan balanced brackets
			inner, remaining, ok := scanBalancedBrackets(rest)
			if !ok {
				// Malformed — treat as plain text
				l.addToken(TokenText, text[best.pos:])
				text = ""
				break
			}
			// Emit start/content/end tokens for the inline tag
			l.addToken(TokenTagInterpolationStart, inner)
			l.addToken(TokenTagInterpolationEnd, "")
			text = remaining
		}
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

// scanBalancedBrackets reads characters from s up to (but not including) the
// matching closing bracket ], handling nested brackets and quoted strings.
// Returns (inner, remaining, ok).  remaining starts after the closing ].
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

	// Remember the depth of the comment header line.
	commentDepth := l.depth

	// Capture any inline text on the same line as the comment marker.
	text := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		text += l.advanceStr()
	}
	text = strings.TrimSpace(text)

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

		// Count leading whitespace (raw space/tab count).
		bodyIndent := 0
		for l.pos < len(l.input) && (l.peek() == ' ' || l.peek() == '\t') {
			l.advance()
			bodyIndent++
		}

		// Blank or EOF line — skip and continue (may be inside the block).
		if l.isEOF() || l.peek() == '\n' || l.peek() == '\r' {
			l.skipToNewline()
			continue
		}

		// If this line is not indented strictly deeper than the comment header,
		// it does not belong to the comment body — restore position and stop.
		if bodyIndent <= commentDepth {
			l.pos = savedPos
			l.line = savedLine
			l.col = savedCol
			break
		}

		// Update l.depth so addToken records the correct depth for the parser.
		l.depth = bodyIndent
		// This line belongs to the comment body — read it verbatim.
		lineContent := ""
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			lineContent += l.advanceStr()
		}
		l.addToken(TokenText, lineContent)
		l.skipToNewline()
	}

	return nil
}

// scanUnbufferedCode handles - (unbuffered code block)
func (l *Lexer) scanUnbufferedCode() error {
	l.advance() // consume -
	l.skipSpaces()

	// Collect rest of line as code
	code := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		code += l.advanceStr()
	}

	headerDepth := l.depth
	trimmed := strings.TrimSpace(code)
	l.skipToNewline()

	// If the - line has no inline code (bare "-"), consume indented lines as
	// individual code statements — emitting one TokenCode per line.
	// Example:
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

			lineContent := ""
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				lineContent += l.advanceStr()
			}
			stmt := strings.TrimSpace(lineContent)
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

// scanBufferedCode handles = (buffered code output)
func (l *Lexer) scanBufferedCode() error {
	l.advance() // consume =
	l.skipSpaces()

	code := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		code += l.advanceStr()
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
		code += l.advanceStr()
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
		text += l.advanceStr()
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
		html += l.advanceStr()
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

	// Remember the indentation depth of the filter header line so we can
	// recognise which subsequent lines belong to the filter body.
	filterDepth := l.depth

	l.addToken(TokenFilter, name)

	// Check for chained subfilters  :outer:inner  (may repeat).
	// Subfilters may appear before OR after the options block, so we scan
	// both positions:
	//   :outer:inner(opts)        — subfilter before options
	//   :outer(opts):inner        — subfilter after options
	//   :outer:mid(opts):inner    — mixed (uncommon but possible)
	scanSubfilters := func() {
		for l.peek() == ':' {
			l.advance()
			sub := l.scanIdentifier()
			if sub != "" {
				l.addToken(TokenFilterColon, sub)
			}
		}
	}

	scanSubfilters() // subfilters before the options block

	// Check for optional filter options in parentheses:
	//   :filtername(key=val, key2="val2") body…
	// We capture the raw content inside the parens as a TokenFilterOptions
	// token so the parser can decode the key=value pairs.
	l.skipSpaces()
	if l.peek() == '(' {
		l.advance() // consume '('
		raw := ""
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
			raw += l.advanceStr()
		}
		l.addToken(TokenFilterOptions, strings.TrimSpace(raw))
		l.skipSpaces()
	}

	scanSubfilters() // subfilters after the options block (e.g. :outer(opts):inner)

	// Capture any same-line inline content after the filter name(s) / options.
	// e.g.  :uppercase Hello World
	//               filter^  ^inline text
	inline := ""
	for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
		inline += l.advanceStr()
	}
	if inline != "" {
		// Emit inline content as a single TokenText — no further body lines.
		l.addToken(TokenText, strings.TrimRight(inline, " \t"))
		l.skipToNewline()
		return nil
	}

	// No inline content — consume the newline that ends the filter header,
	// then eagerly collect all indented body lines as verbatim TokenText
	// tokens (one per line).  This prevents the main scanLine dispatcher
	// from re-interpreting filter body content as Pug tags/keywords/etc.
	//
	// Depth comparison uses raw space counts throughout:
	//   filterDepth = l.depth  (raw spaces, set by scanLine before calling us)
	//   bodyIndent             (raw spaces counted below)
	// A body line must have strictly MORE leading spaces than the filter header.
	l.skipToNewline()

	for l.pos < len(l.input) {
		// Peek at the indentation of the next line without committing.
		savedPos := l.pos
		savedLine := l.line
		savedCol := l.col

		// Count leading spaces/tabs (raw count, not divided by 2).
		bodyIndent := 0
		for l.pos < len(l.input) && (l.peek() == ' ' || l.peek() == '\t') {
			l.advance()
			bodyIndent++
		}

		// Blank line — skip it and keep going (may be inside the block).
		if l.peek() == '\n' || l.peek() == '\r' || l.isEOF() {
			l.skipToNewline()
			continue
		}

		// If this line is not indented strictly deeper than the filter header
		// (using the same raw-space count stored in filterDepth), it does not
		// belong to the filter body — restore the scanner position and stop.
		if bodyIndent <= filterDepth {
			l.pos = savedPos
			l.line = savedLine
			l.col = savedCol
			break
		}

		// This line belongs to the filter body — read it verbatim.
		lineContent := ""
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			lineContent += l.advanceStr()
		}
		l.addToken(TokenText, lineContent)
		l.skipToNewline()
	}

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
				arg += l.advanceStr()
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
			cond += l.advanceStr()
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
// scanBlockTextBody eagerly consumes all lines that are indented more deeply
// than headerDepth, emitting each as a TokenText token.  This is used after a
// block-text dot (p.) so that the main scanLine dispatcher never tries to
// re-parse the literal text lines as Pug tags or keywords.
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

		// Update l.depth so addToken records the correct depth for the parser.
		l.depth = bodyIndent
		lineContent := ""
		for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
			lineContent += l.advanceStr()
		}
		l.addToken(TokenText, lineContent)
		l.skipToNewline()
	}
}

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
				// Standalone dot after a tag name — block text indicator.
				// Eagerly consume all indented body lines as TokenText so the
				// main scanLine dispatcher never re-parses them as Pug content.
				l.addToken(TokenDot, ".")
				l.scanBlockTextBody(l.depth)
				return nil
			}

		case '&':
			// &attributes(expr) — merge a map expression into the tag's attributes.
			// We peek ahead to confirm it is exactly "&attributes(".
			if strings.HasPrefix(l.input[l.pos:], "&attributes(") {
				// Skip "&attributes("
				for i := 0; i < len("&attributes("); i++ {
					l.advance()
				}
				// Scan the expression inside the parens using balanced-paren logic.
				expr := ""
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
					expr += l.advanceStr()
				}
				l.addToken(TokenAttrName, "&attributes")
				l.addToken(TokenAttrEqual, "=")
				l.addToken(TokenAttrValue, expr)
			} else {
				// Unknown & sequence — skip to end of line
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
					text += l.advanceStr()
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
			// Block expansion: tag: child
			// We must scan the child tag immediately — still on the same
			// logical line — so that all tokens it produces carry the
			// correct l.depth (the parent's indentation level).  If we
			// return here and let the Lex loop call scanLine again,
			// scanLine would invoke scanIndentation which counts zero
			// leading spaces (we already skipped them) and reset l.depth
			// to 0, making the child appear at the wrong depth and
			// causing the parser to nest subsequent siblings inside it.
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
			// Inline buffered code: p= expr
			l.advance()
			l.skipSpaces()
			code := ""
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				code += l.advanceStr()
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
					code += l.advanceStr()
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
				text += l.advanceStr()
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
	// If the value starts with a quote, scan the quoted string first, then
	// check whether an operator follows (e.g. "/user/" + uid).  If so,
	// continue reading the rest as an unquoted expression fragment.
	if l.peek() == '"' || l.peek() == '\'' || l.peek() == '`' {
		q := l.peek()
		value := l.scanQuotedString(rune(q))
		// After the closing quote, check for a following operator so that
		// expressions like `"/user/" + uid` are captured whole.
		l.skipSpaces()
		ch := l.peek()
		if ch == '+' || ch == '-' || ch == '*' || ch == '/' {
			// Consume the operator and the rest of the expression.
			for l.pos < len(l.input) && l.peek() != '\n' && l.peek() != '\r' {
				c := l.peek()
				if c == ')' || c == ',' {
					break
				}
				value += l.advanceStr()
			}
		}
		return strings.TrimSpace(value)
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
		value += l.advanceStr()
	}
	return strings.TrimSpace(value)
}

// scanQuotedString scans a quoted string and returns its content (including quotes).
func (l *Lexer) scanQuotedString(quote rune) string {
	value := l.advanceStr() // opening quote
	for l.pos < len(l.input) {
		ch := l.peek()
		if ch == '\\' {
			value += l.advanceStr()
			if l.pos < len(l.input) {
				value += l.advanceStr()
			}
		} else if rune(ch) == quote {
			value += l.advanceStr() // closing quote
			break
		} else {
			value += l.advanceStr()
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

// advanceStr advances one byte and returns it as a correct single-byte string.
// Unlike string(l.advance()), which re-encodes the byte value as a Unicode
// code point (corrupting non-ASCII bytes), this preserves the raw byte.
func (l *Lexer) advanceStr() string {
	return string([]byte{l.advance()})
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
