package gopug

import "path/filepath"

// ResolveComposition is codegen's generate-time counterpart to the
// interpreter's render-time renderExtends: it resolves ast's extends chain
// and flattens every remaining *BlockNode into its own already-merged Body,
// returning a DocumentNode GenerateGo can walk directly with no
// ExtendsNode/BlockNode left in the tree.
//
// The extends+block MERGE itself is not reimplemented here — it reuses
// resolveExtendsAST, the interpreter's own tree transform, unchanged, by
// constructing a private, unshared *Runtime seeded from opts the same way
// renderExtends seeds the one it runs on (Basedir → includeBase, the
// unexported entryFile, both handled by NewRuntimeWithOptions already) and
// computing the same "current file path" renderExtends computes for a
// top-level render — the entry file if one was given, otherwise a synthetic
// path anchored at Basedir, otherwise empty. Because the merge itself is the
// interpreter's own code path, the flattened tree this returns is
// byte-identical by construction to what the interpreter would render for
// equivalent data.
//
// Only extends+block are resolved. An IncludeNode is left in the tree
// untouched (a later codegen increment); so is any mixin declaration/call —
// GenerateGo already returns a clear "unsupported node" error for both, so a
// template that also uses include or mixins stays deferred rather than
// silently mis-generating. A template with no extends is still safe to pass
// through ResolveComposition: resolveExtendsAST returns it unchanged, and
// the block-flattening pass still runs, since a standalone `block` (no
// extends at all) renders its own body exactly like renderBlockBody does.
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

	return &DocumentNode{Children: reduceBlocks(resolved.Children)}, nil
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
