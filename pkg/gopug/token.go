package gopug

import "fmt"

type TokenType int

const (
	// Special tokens
	TokenEOF TokenType = iota
	TokenError
	TokenNewline
	TokenIndent
	TokenDedent

	// Structural tokens
	TokenTag
	TokenClass
	TokenID
	TokenDot
	TokenColon
	TokenPipe

	// Attributes
	TokenAttrStart
	TokenAttrEnd
	TokenAttrName
	TokenAttrValue
	TokenAttrComma
	TokenAttrEqual
	TokenAttrEqualUnescape

	// Code and expressions
	TokenCode
	TokenCodeBuffered
	TokenCodeUnescaped
	TokenInterpolation
	TokenInterpolationUnescape
	TokenTagInterpolationStart
	TokenTagInterpolationEnd

	// Control flow keywords
	TokenIf
	TokenElseIf
	TokenElse
	TokenUnless
	TokenEach
	TokenFor
	TokenWhile
	TokenIn
	TokenCase
	TokenWhen
	TokenDefault

	// Template inheritance
	TokenExtends
	TokenBlock
	TokenBlockAppend
	TokenBlockPrepend
	TokenAppend
	TokenPrepend

	// Mixins
	TokenMixin
	TokenMixinCall

	// Includes
	TokenInclude

	// Filters
	TokenFilter
	TokenFilterColon
	TokenFilterOptions // raw "(key=val, ...)" string captured after filter name

	// Comments
	TokenComment
	TokenCommentUnbuffered

	// Doctype
	TokenDoctype

	// Text content
	TokenText
	TokenTextBlock

	// HTML literal
	TokenHTMLLiteral

	// Self-closing tag marker
	TokenTagEnd
)

type Token struct {
	Type        TokenType
	Value       string
	Line        int
	Col         int
	IndentDepth int
}

func (t Token) String() string {
	return fmt.Sprintf("Token{Type: %s, Value: %q, Line: %d, Col: %d, IndentDepth: %d}",
		tokenTypeName(t.Type), t.Value, t.Line, t.Col, t.IndentDepth)
}

func tokenTypeName(tt TokenType) string {
	names := map[TokenType]string{
		TokenEOF:                   "EOF",
		TokenError:                 "Error",
		TokenNewline:               "Newline",
		TokenIndent:                "Indent",
		TokenDedent:                "Dedent",
		TokenTag:                   "Tag",
		TokenClass:                 "Class",
		TokenID:                    "ID",
		TokenDot:                   "Dot",
		TokenColon:                 "Colon",
		TokenPipe:                  "Pipe",
		TokenAttrStart:             "AttrStart",
		TokenAttrEnd:               "AttrEnd",
		TokenAttrName:              "AttrName",
		TokenAttrValue:             "AttrValue",
		TokenAttrComma:             "AttrComma",
		TokenAttrEqual:             "AttrEqual",
		TokenAttrEqualUnescape:     "AttrEqualUnescape",
		TokenCode:                  "Code",
		TokenCodeBuffered:          "CodeBuffered",
		TokenCodeUnescaped:         "CodeUnescaped",
		TokenInterpolation:         "Interpolation",
		TokenInterpolationUnescape: "InterpolationUnescape",
		TokenTagInterpolationStart: "TagInterpolationStart",
		TokenTagInterpolationEnd:   "TagInterpolationEnd",
		TokenIf:                    "If",
		TokenElseIf:                "ElseIf",
		TokenElse:                  "Else",
		TokenUnless:                "Unless",
		TokenEach:                  "Each",
		TokenFor:                   "For",
		TokenWhile:                 "While",
		TokenIn:                    "In",
		TokenCase:                  "Case",
		TokenWhen:                  "When",
		TokenDefault:               "Default",
		TokenExtends:               "Extends",
		TokenBlock:                 "Block",
		TokenBlockAppend:           "BlockAppend",
		TokenBlockPrepend:          "BlockPrepend",
		TokenAppend:                "Append",
		TokenPrepend:               "Prepend",
		TokenMixin:                 "Mixin",
		TokenMixinCall:             "MixinCall",
		TokenInclude:               "Include",
		TokenFilter:                "Filter",
		TokenFilterColon:           "FilterColon",
		TokenFilterOptions:         "FilterOptions",
		TokenComment:               "Comment",
		TokenCommentUnbuffered:     "CommentUnbuffered",
		TokenDoctype:               "Doctype",
		TokenText:                  "Text",
		TokenTextBlock:             "TextBlock",
		TokenHTMLLiteral:           "HTMLLiteral",
		TokenTagEnd:                "TagEnd",
	}
	if name, ok := names[tt]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", tt)
}

var Keywords = map[string]TokenType{
	"if":      TokenIf,
	"else":    TokenElse,
	"unless":  TokenUnless,
	"each":    TokenEach,
	"for":     TokenFor,
	"while":   TokenWhile,
	"in":      TokenIn,
	"case":    TokenCase,
	"when":    TokenWhen,
	"default": TokenDefault,
	"extends": TokenExtends,
	"block":   TokenBlock,
	"append":  TokenAppend,
	"prepend": TokenPrepend,
	"mixin":   TokenMixin,
	"include": TokenInclude,
	"doctype": TokenDoctype,
}
