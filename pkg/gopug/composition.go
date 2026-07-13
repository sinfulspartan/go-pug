package gopug

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveComposition is codegen's generate-time counterpart to the
// interpreter's render-time renderExtends+renderInclude: it resolves ast's
// extends chain, inlines every `.pug` include (recursively), and flattens
// every remaining *BlockNode into its own already-merged Body, returning a
// DocumentNode GenerateGo can walk directly with no
// ExtendsNode/IncludeNode/BlockNode left in the tree.
//
// The three passes run in this order — resolve-extends, then inline
// includes, then reduce blocks — because that's the order the interpreter's
// own behavior implies: resolveExtendsAST merges the child's block overrides
// into the layout BEFORE any include is loaded, so a `block` that lives
// inside an included partial is never visible to applyBlockOverrides and
// always renders its own default body, never a same-named override from the
// extending child. Inlining includes after extends resolution and reducing
// blocks last preserves that: the include pass runs over the already-merged
// tree, and only after an included partial's own nodes (including any block
// of its own) are spliced in does the block-reduction pass see them.
//
// The extends+block MERGE itself is not reimplemented here — it reuses
// resolveExtendsAST, the interpreter's own tree transform, unchanged, by
// constructing a private, unshared *Runtime seeded from opts the same way
// renderExtends seeds the one it runs on (Basedir → includeBase, the
// unexported entryFile, both handled by NewRuntimeWithOptions already) and
// computing the same "current file path" renderExtends computes for a
// top-level render — the entry file if one was given, otherwise a synthetic
// path anchored at Basedir, otherwise empty. Likewise, include resolution and
// cycle detection reuse resolveIncludeAbs — the same helper renderInclude
// itself calls — and inlineIncludes pushes/pops the same r.includeStack
// around each include exactly as renderInclude does, so a nested include
// resolves relative to the including file and a cycle is reported on the
// same hop the interpreter would report it on. Because both passes are the
// interpreter's own code paths (or share its exact resolution logic), the
// flattened tree this returns is byte-identical by construction to what the
// interpreter would render for equivalent data.
//
// Only `.pug` includes are inlined this increment. A filtered
// (`include:filter path`) or raw non-`.pug` include returns a clear error —
// renderInclude only applies a filter to a non-`.pug` file, so mirroring
// that split is itself part of staying byte-identical, not an added
// restriction. Any mixin declaration or call — whether written directly or
// reached via an inlined include — is left in the tree for GenerateGo's
// existing "unsupported node" error, so a template using mixins stays
// deferred rather than silently mis-generating. A template with no extends
// is still safe to pass through ResolveComposition: resolveExtendsAST
// returns it unchanged, and the block-flattening pass still runs, since a
// standalone `block` (no extends at all) renders its own body exactly like
// renderBlockBody does.
func ResolveComposition(ast *DocumentNode, opts *Options) (*DocumentNode, error) {
	r := NewRuntimeWithOptions(ast, nil, opts)

	currentPath := ""
	if r.entryFile != "" {
		currentPath = r.entryFile
	} else if r.includeBase != "" {
		currentPath = filepath.Join(r.includeBase, "_root_.pug")
	}

	resolved, _, err := r.resolveExtendsAST(currentPath, ast)
	if err != nil {
		return nil, err
	}

	inlined, err := r.inlineIncludes(resolved.Children)
	if err != nil {
		return nil, err
	}

	return &DocumentNode{Children: reduceBlocks(inlined)}, nil
}

// ResolveCompositionFile is ResolveComposition's file-path-aware entry
// point: it reads path, parses it, and resolves its extends+block chain
// exactly as RenderFile does for the interpreter, so a caller that only has
// a file path — not an already-parsed AST — gets the same relative-extends
// resolution RenderFile gives.
//
// ResolveComposition alone cannot do this for a subdirectory child with a
// relative extends (e.g. "extends base.pug" in a file at layout/page.pug):
// resolving that path requires knowing the child's own directory, which
// resolveExtendsAST derives from Options.entryFile — an unexported field a
// caller outside this package cannot set. ResolveCompositionFile mirrors
// RenderFile's preamble (read the file, copy opts so the caller's struct is
// never mutated, default Basedir to the file's directory, set entryFile to
// path) before parsing and resolving, so the resolution is identical to
// what RenderFile would produce for the same file.
func ResolveCompositionFile(path string, opts *Options) (*DocumentNode, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
	}

	copied := Options{}
	if opts != nil {
		copied = *opts
	}
	if copied.Basedir == "" {
		copied.Basedir = filepath.Dir(path)
	}
	copied.entryFile = path

	ast, err := Parse(string(src), &copied)
	if err != nil {
		return nil, err
	}

	return ResolveComposition(ast, &copied)
}

// reduceBlocks returns a new node slice equal to nodes with every top-level
// *BlockNode spliced out and replaced by its own Body — exactly what
// renderBlockBody renders for a block node at run time — and recurses into
// every other node's own child node list(s) so a block nested inside a tag,
// conditional, each, while, case, or mixin declaration is flattened too. By
// the time ResolveComposition calls this, resolveExtendsAST's own
// applyBlockOverrides pass has already merged every child override into the
// matching parent BlockNode's Body (append/prepend/replace all resolved), so
// reduceBlocks only ever needs to splice a Body it does not need to merge.
//
// The node-type coverage here is intentionally identical to
// applyBlockOverrides's own deep walk (runtime.go): those are exactly the
// places resolveExtendsAST's override merge considers a nested block
// reachable, so this reduction never needs to look anywhere
// applyBlockOverrides did not already look.
func reduceBlocks(nodes []Node) []Node {
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if b, ok := node.(*BlockNode); ok {
			out = append(out, reduceBlocks(b.Body)...)
			continue
		}
		reduceBlocksInPlace(node)
		out = append(out, node)
	}
	return out
}

// reduceBlocksInPlace mutates a single non-BlockNode's own child node
// list(s) in place, replacing any nested *BlockNode the way reduceBlocks
// does for a top-level list. node's own identity and position in its
// parent's list is unchanged — reduceBlocks itself is the only place a
// *BlockNode is spliced away.
func reduceBlocksInPlace(node Node) {
	switch n := node.(type) {
	case *TagNode:
		n.Children = reduceBlocks(n.Children)
	case *ConditionalNode:
		n.Consequent = reduceBlocks(n.Consequent)
		n.Alternate = reduceBlocks(n.Alternate)
	case *EachNode:
		n.Body = reduceBlocks(n.Body)
		n.EmptyBody = reduceBlocks(n.EmptyBody)
	case *WhileNode:
		n.Body = reduceBlocks(n.Body)
	case *CaseNode:
		for _, when := range n.Cases {
			when.Body = reduceBlocks(when.Body)
		}
		n.Default = reduceBlocks(n.Default)
	case *MixinDeclNode:
		n.Body = reduceBlocks(n.Body)
	case *BlockExpansionNode:
		if b, ok := n.Child.(*BlockNode); ok {
			// BlockExpansionNode.Child is a single Node, so a block with
			// exactly one body node splices cleanly. A block with zero or
			// more than one body node has no single-node representation
			// here; it is left as the unflattened *BlockNode rather than
			// dropped or guessed at — GenerateGo does not support
			// BlockExpansionNode at all yet, so this stays a safe no-op.
			if reduced := reduceBlocks(b.Body); len(reduced) == 1 {
				n.Child = reduced[0]
			}
			return
		}
		reduceBlocksInPlace(n.Child)
	}
}

// inlineIncludes returns a new node slice equal to nodes with every
// top-level `.pug` *IncludeNode spliced out and replaced by that included
// file's own children — recursively include-inlined themselves — and
// recurses into every other node's own child node list(s) so an include
// nested inside a tag, conditional, each, while, case, mixin declaration, or
// block is reached too.
//
// Path resolution and cycle detection are resolveIncludeAbs, the exact
// helper renderInclude itself calls, so which file an include resolves to
// (and when a cycle is reported) matches the interpreter byte-for-byte. A
// filtered or raw (non-`.pug`) include is not supported by this codegen
// increment and returns a clear error rather than being silently dropped or
// mis-generated.
//
// The node-type coverage here is intentionally identical to
// applyBlockOverrides/reduceBlocks's own deep walk (runtime.go and above):
// those are exactly the places a nested node is reachable, so this pass
// never needs to look anywhere those do not already look. Unlike
// reduceBlocks, this pass must also recurse into a *BlockNode's own Body
// without splicing it away — reduceBlocks runs afterward and is the only
// pass that splices a BlockNode — so an include living inside a block (in
// either the layout or a child override) is still reached.
func (r *Runtime) inlineIncludes(nodes []Node) ([]Node, error) {
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if inc, ok := node.(*IncludeNode); ok {
			inlined, err := r.inlineInclude(inc)
			if err != nil {
				return nil, err
			}
			out = append(out, inlined...)
			continue
		}
		if err := r.inlineIncludesInPlace(node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, nil
}

// inlineIncludesInPlace mutates a single non-IncludeNode's own child node
// list(s) in place, inlining any include reachable inside it.
func (r *Runtime) inlineIncludesInPlace(node Node) error {
	var err error
	switch n := node.(type) {
	case *TagNode:
		n.Children, err = r.inlineIncludes(n.Children)
	case *ConditionalNode:
		if n.Consequent, err = r.inlineIncludes(n.Consequent); err != nil {
			return err
		}
		n.Alternate, err = r.inlineIncludes(n.Alternate)
	case *EachNode:
		if n.Body, err = r.inlineIncludes(n.Body); err != nil {
			return err
		}
		n.EmptyBody, err = r.inlineIncludes(n.EmptyBody)
	case *WhileNode:
		n.Body, err = r.inlineIncludes(n.Body)
	case *CaseNode:
		for _, when := range n.Cases {
			if when.Body, err = r.inlineIncludes(when.Body); err != nil {
				return err
			}
		}
		n.Default, err = r.inlineIncludes(n.Default)
	case *MixinDeclNode:
		n.Body, err = r.inlineIncludes(n.Body)
	case *BlockNode:
		n.Body, err = r.inlineIncludes(n.Body)
	case *BlockExpansionNode:
		return r.inlineIncludesInPlace(n.Child)
	}
	return err
}

// inlineInclude resolves inc exactly as renderInclude does (via the shared
// resolveIncludeAbs) and, for a `.pug` include, returns that file's own
// children with any includes inside them already inlined too. It pushes
// inc's own resolved path onto r.includeStack before recursing — exactly as
// renderInclude does before rendering an included file's children — so a
// nested include inside the included file resolves relative to THAT file,
// and a cycle back to an ancestor include is caught on the same hop the
// interpreter would catch it on.
//
// Extends inside an included file is intentionally left unresolved
// (renderInclude never resolves it either — an included file's own `extends`
// is not a construct the interpreter supports). A mixin declaration or call
// inside an included file is left as-is for GenerateGo to reject, since
// codegen does not support mixins yet — matching renderInclude, which does
// collect an included file's mixins for later CALLS but does not need this
// pass to do so, since it never evaluates a mixin call itself.
//
// Filtered and raw (non-`.pug`) includes mirror renderInclude's own
// extension-first branch (a `.pug` file is always rendered as Pug even if
// FilterName is set — the filter only applies to a non-`.pug` file) and
// return a clear "not supported by codegen yet" error instead of silently
// dropping the include or mis-generating its output.
func (r *Runtime) inlineInclude(inc *IncludeNode) ([]Node, error) {
	abs, unquoted, err := r.resolveIncludeAbs(inc)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(abs))

	r.includeStack = append(r.includeStack, abs)
	defer func() { r.includeStack = r.includeStack[:len(r.includeStack)-1] }()

	if ext != ".pug" {
		if inc.FilterName != "" {
			return nil, fmt.Errorf("include: filtered includes are not supported by codegen yet (%q)", abs)
		}
		return nil, fmt.Errorf("include: raw (non-.pug) includes are not supported by codegen yet (%q)", abs)
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("include: cannot read %q: %w%s", abs, err, r.basedirResolveHint(unquoted))
	}

	lexer := NewLexer(string(src))
	tokens, err := lexer.Lex()
	if err != nil {
		return nil, fmt.Errorf("include: lex error in %q: %w", abs, err)
	}

	parser := NewParser(tokens)
	ast, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("include: parse error in %q: %w", abs, err)
	}

	return r.inlineIncludes(ast.Children)
}
