package gopug

import (
	"strconv"
	"strings"
)

// compiledExpr is a closure-compiled stand-in for a call to
// r.evaluateExpr(expr) on a buffered/unescaped CodeNode. It returns exactly
// the same string evaluateExpr would return for the expression it was
// compiled from, for any Runtime it is later called with. classifyExpr is
// the only place these closures are constructed, and it only returns one
// for expression shapes it can prove are equivalent to the interpreter.
type compiledExpr func(r *Runtime) (string, error)

// reservedDotMethodNames are the identifiers evaluateExpr treats as builtin
// method/property names when they are the final segment of a dot path
// (obj.method(...) or the bare obj.length-style property). A dot-path whose
// final segment is one of these must fall back to the interpreter, since
// the interpreter would dispatch to that special-cased behavior instead of
// a plain field/map lookup.
var reservedDotMethodNames = map[string]bool{
	"length": true, "toUpperCase": true, "toUppercase": true,
	"toLowerCase": true, "toLowercase": true, "trim": true,
	"trimLeft": true, "trimStart": true, "trimRight": true, "trimEnd": true,
	"repeat": true, "split": true, "join": true, "replace": true,
	"slice": true, "indexOf": true, "includes": true, "contains": true,
	"startsWith": true, "endsWith": true, "toString": true, "String": true,
	"toFixed": true, "toPrecision": true, "padStart": true, "padEnd": true,
}

// reservedBareIdentifiers are whole-expression identifiers evaluateExpr
// treats specially rather than as a plain variable lookup.
var reservedBareIdentifiers = map[string]bool{
	"true": true, "false": true, "null": true, "undefined": true, "nil": true,
	// "block" only gets special handling when the runtime is inside a
	// mixin body; excluding it unconditionally keeps the classifier from
	// having to reason about that runtime-only state.
	"block": true,
}

// simpleShape identifies which of the three trivial expression shapes
// classifySimpleShape recognized, if any.
type simpleShape int

const (
	shapeNone simpleShape = iota
	shapeLiteral
	shapeIdentifier
	shapeDotPath
)

// classifySimpleShape is the single, non-allocating detection routine shared
// by classifyExpr (compile-time closure construction) and tryEvalSimple
// (render-time interpreter fast-path). It reports which trivial shape expr
// matches — a quoted string literal, a bare identifier, or a plain dot-path —
// along with the value each caller needs to produce a result: the unwrapped
// literal text for shapeLiteral, or expr itself (to hand to
// lookupAndStringify) for shapeIdentifier/shapeDotPath. It returns
// shapeNone for anything else, doing no allocation in any case.
//
// Supported shapes:
//   - a quoted string literal with no unescaped interior quote
//   - a bare identifier that isn't a reserved literal and doesn't parse as a
//     number (Go's strconv.ParseFloat also accepts "Inf"/"NaN" spellings)
//   - a dot-path of plain identifier segments, none of which is one of the
//     interpreter's builtin method/property names
//
// Anything with operators, ternary, parens, indexing, method calls,
// interpolation, or that doesn't match one of the shapes above yields
// shapeNone, unchanged.
func classifySimpleShape(expr string) (simpleShape, string) {
	if lit, ok := unwrapQuotedLiteral(expr); ok {
		return shapeLiteral, lit
	}
	if isPlainIdentifier(expr) {
		return shapeIdentifier, expr
	}
	if isPlainDotPath(expr) {
		return shapeDotPath, expr
	}
	return shapeNone, ""
}

// classifyExpr returns a compiledExpr for expressions whose shape is
// unambiguous enough to prove, by construction, that it produces the exact
// same string evaluateExpr would. Anything else returns nil, which tells
// renderCode to keep using the string interpreter for that node.
func classifyExpr(expr string) compiledExpr {
	expr = strings.TrimSpace(expr)

	switch shape, value := classifySimpleShape(expr); shape {
	case shapeLiteral:
		return func(*Runtime) (string, error) {
			return value, nil
		}
	case shapeIdentifier, shapeDotPath:
		name := value
		return func(r *Runtime) (string, error) {
			return r.lookupAndStringify(name), nil
		}
	default:
		return nil
	}
}

// tryEvalSimple is evaluateExpr's non-allocating fast-path: it detects the
// same three trivial shapes classifyExpr recognizes and, if expr matches
// one, evaluates it directly — short-circuiting the paren-unwrap/ternary/
// operator-scan chain the general interpreter runs. It returns ok=false for
// anything else, in which case the caller must fall back to the full
// evaluateExpr logic unchanged. Unlike classifyExpr, it never builds a
// closure, so it is safe to call on every evaluateExpr invocation.
func (r *Runtime) tryEvalSimple(expr string) (string, bool) {
	switch shape, value := classifySimpleShape(expr); shape {
	case shapeLiteral:
		return value, true
	case shapeIdentifier, shapeDotPath:
		return r.lookupAndStringify(value), true
	default:
		return "", false
	}
}

// isIdentByte reports whether b can appear in a bare identifier segment
// under the classifier's whitelist: ASCII letters, digits, underscore, or
// dollar sign.
func isIdentByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// isIdentStartByte reports whether b can start a bare identifier segment:
// the same set as isIdentByte, minus digits.
func isIdentStartByte(b byte) bool {
	return b == '_' || b == '$' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isIdentSegment reports whether s matches ^[A-Za-z_$][A-Za-z0-9_$]*$.
func isIdentSegment(s string) bool {
	if s == "" || !isIdentStartByte(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

// isPlainIdentifier reports whether expr is a single bare identifier that
// classifyExpr can safely compile: a valid identifier, not one of the
// interpreter's reserved literal words, and not something ParseFloat would
// accept as a numeric literal (evaluateExpr checks ParseFloat before ever
// treating the expression as a variable lookup).
func isPlainIdentifier(expr string) bool {
	if !isIdentSegment(expr) {
		return false
	}
	if reservedBareIdentifiers[expr] {
		return false
	}
	// isIdentSegment already guarantees expr starts with a letter, '_', or
	// '$', so ParseFloat can only ever succeed for the inf/infinity/nan
	// spellings; mayBeFloat's first-byte check gates on exactly that same
	// letter subset, so skipping the call otherwise avoids the syntaxError
	// allocation ParseFloat makes on every failed parse without changing
	// which identifiers are accepted.
	if mayBeFloat(expr) {
		if _, err := strconv.ParseFloat(expr, 64); err == nil {
			return false
		}
	}
	return true
}

// isPlainDotPath reports whether expr is two or more plain identifier
// segments joined by single dots (ident(.ident)+), with no segment matching
// one of the interpreter's reserved dot-method names anywhere in the path —
// not just the last segment, since evaluateExpr's dot-path handling
// recurses on every prefix of the path and an intermediate segment
// triggering special method dispatch could change the result (or make it
// error) in ways a plain field lookup would not.
func isPlainDotPath(expr string) bool {
	if expr == "" {
		return false
	}
	start := 0
	segments := 0
	for i := 0; i <= len(expr); i++ {
		if i == len(expr) || expr[i] == '.' {
			seg := expr[start:i]
			if !isIdentSegment(seg) {
				return false
			}
			if reservedDotMethodNames[seg] {
				return false
			}
			segments++
			start = i + 1
		}
	}
	return segments >= 2
}

// unwrapQuotedLiteral strips the surrounding quotes from a single- or
// double-quoted string literal, using the exact same scan evaluateExpr uses
// to find the matching close quote, so a classify-time constant and a
// render-time evaluateExpr call agree byte-for-byte. It returns ok=false for
// anything that doesn't parse as a single quoted literal spanning the whole
// expression (backticked template literals are not returned as bare
// constants, since they can contain ${...} interpolations).
func unwrapQuotedLiteral(expr string) (string, bool) {
	if len(expr) < 2 {
		return "", false
	}
	q := expr[0]
	if q != '"' && q != '\'' {
		return "", false
	}

	escaped := false
	closeIdx := -1
	for byteIdx, ch := range expr[1:] {
		realIdx := byteIdx + 1
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == rune(q) {
			closeIdx = realIdx
			break
		}
	}

	if closeIdx == len(expr)-1 {
		return expr[1 : len(expr)-1], true
	}
	return "", false
}

// compileExprs walks the whole AST once, at Compile time, and populates
// compiled on every buffered/unescaped CodeNode whose expression classifies
// to a supported shape. It must only be called once per compile, never per
// render — the AST is built once and reused read-only across renders.
func compileExprs(nodes []Node) {
	walkCodeNodes(nodes, func(code *CodeNode) {
		if code.Type == CodeBuffered || code.Type == CodeUnescaped {
			code.compiled = classifyExpr(code.Expression)
		}
	})
}

// walkCodeNodes recurses through every node kind that can contain a
// CodeNode (tag/each/conditional/mixin/etc. bodies) and calls visit for
// each CodeNode found, regardless of its Type.
func walkCodeNodes(nodes []Node, visit func(*CodeNode)) {
	for _, n := range nodes {
		switch v := n.(type) {
		case *CodeNode:
			visit(v)
		case *DocumentNode:
			walkCodeNodes(v.Children, visit)
		case *TagNode:
			walkCodeNodes(v.Children, visit)
		case *TagInterpolationNode:
			walkCodeNodes([]Node{v.Tag}, visit)
		case *ConditionalNode:
			walkCodeNodes(v.Consequent, visit)
			walkCodeNodes(v.Alternate, visit)
		case *EachNode:
			walkCodeNodes(v.Body, visit)
			walkCodeNodes(v.EmptyBody, visit)
		case *WhileNode:
			walkCodeNodes(v.Body, visit)
		case *CaseNode:
			for _, w := range v.Cases {
				walkCodeNodes(w.Body, visit)
			}
			walkCodeNodes(v.Default, visit)
		case *MixinDeclNode:
			walkCodeNodes(v.Body, visit)
		case *MixinCallNode:
			walkCodeNodes(v.BlockContent, visit)
		case *BlockNode:
			walkCodeNodes(v.Body, visit)
		case *BlockExpansionNode:
			walkCodeNodes([]Node{v.Parent, v.Child}, visit)
		case *TextRunNode:
			walkCodeNodes(v.Nodes, visit)
		}
	}
}

// compileTagAttrs walks the whole AST once, at Compile time, and precomputes
// each no-`&attributes`-spread TagNode's sorted attribute-name list. The
// attribute-name SET for such a tag is fixed at parse time — nothing at
// render time adds or removes a key, only a spread can do that — and
// sortAttrNames depends only on the key set, not the values, so the sorted
// order computed here from tag.Attributes is byte-identical to the order
// renderTag would compute at render time from its merged copy of the same
// keys. It must only be called once per compile, never per render.
func compileTagAttrs(nodes []Node) {
	walkTagNodes(nodes, func(tag *TagNode) {
		_, hasSpread := tag.Attributes["&attributes"]
		tag.noSpread = !hasSpread
		if tag.noSpread {
			tag.sortedAttrNames = sortAttrNames(tag.Attributes)
		}
	})
}

// walkTagNodes recurses through every node kind that can contain a TagNode
// (document/tag/each/conditional/mixin/etc. bodies, tag-interpolation's
// wrapped tag, and block-expansion's parent/child) and calls visit for each
// TagNode found.
func walkTagNodes(nodes []Node, visit func(*TagNode)) {
	for _, n := range nodes {
		switch v := n.(type) {
		case *TagNode:
			visit(v)
			walkTagNodes(v.Children, visit)
		case *DocumentNode:
			walkTagNodes(v.Children, visit)
		case *TagInterpolationNode:
			walkTagNodes([]Node{v.Tag}, visit)
		case *ConditionalNode:
			walkTagNodes(v.Consequent, visit)
			walkTagNodes(v.Alternate, visit)
		case *EachNode:
			walkTagNodes(v.Body, visit)
			walkTagNodes(v.EmptyBody, visit)
		case *WhileNode:
			walkTagNodes(v.Body, visit)
		case *CaseNode:
			for _, w := range v.Cases {
				walkTagNodes(w.Body, visit)
			}
			walkTagNodes(v.Default, visit)
		case *MixinDeclNode:
			walkTagNodes(v.Body, visit)
		case *MixinCallNode:
			walkTagNodes(v.BlockContent, visit)
		case *BlockNode:
			walkTagNodes(v.Body, visit)
		case *BlockExpansionNode:
			walkTagNodes([]Node{v.Parent, v.Child}, visit)
		case *TextRunNode:
			walkTagNodes(v.Nodes, visit)
		}
	}
}

// compileMixinArgs walks the whole AST once, at Compile time, and populates
// compiledArgs on every MixinCallNode whose arguments include at least one
// expression that classifies to a supported shape. It must only be called
// once per compile, never per render.
func compileMixinArgs(nodes []Node) {
	walkMixinCallNodes(nodes, func(call *MixinCallNode) {
		if len(call.Arguments) == 0 {
			return
		}
		call.compiledArgs = make([]compiledExpr, len(call.Arguments))
		for i, arg := range call.Arguments {
			call.compiledArgs[i] = classifyExpr(arg)
		}
	})
}

// walkMixinCallNodes recurses through every node kind that can contain a
// MixinCallNode (tag/each/conditional/mixin bodies, and a mixin call's own
// block content, since a caller can pass another mixin call as block
// content) and calls visit for each MixinCallNode found.
func walkMixinCallNodes(nodes []Node, visit func(*MixinCallNode)) {
	for _, n := range nodes {
		switch v := n.(type) {
		case *MixinCallNode:
			visit(v)
			walkMixinCallNodes(v.BlockContent, visit)
		case *DocumentNode:
			walkMixinCallNodes(v.Children, visit)
		case *TagNode:
			walkMixinCallNodes(v.Children, visit)
		case *TagInterpolationNode:
			walkMixinCallNodes([]Node{v.Tag}, visit)
		case *ConditionalNode:
			walkMixinCallNodes(v.Consequent, visit)
			walkMixinCallNodes(v.Alternate, visit)
		case *EachNode:
			walkMixinCallNodes(v.Body, visit)
			walkMixinCallNodes(v.EmptyBody, visit)
		case *WhileNode:
			walkMixinCallNodes(v.Body, visit)
		case *CaseNode:
			for _, w := range v.Cases {
				walkMixinCallNodes(w.Body, visit)
			}
			walkMixinCallNodes(v.Default, visit)
		case *MixinDeclNode:
			walkMixinCallNodes(v.Body, visit)
		case *BlockNode:
			walkMixinCallNodes(v.Body, visit)
		case *BlockExpansionNode:
			walkMixinCallNodes([]Node{v.Parent, v.Child}, visit)
		case *TextRunNode:
			walkMixinCallNodes(v.Nodes, visit)
		}
	}
}
