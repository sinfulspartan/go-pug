package gopug

import "fmt"

// Node is implemented by every AST node type. The unexported node() method
// seals the interface so that only types defined in this package can satisfy
// it, keeping the runtime's type-switch exhaustive.
type Node interface {
	node()
	String() string
}

type DocumentNode struct {
	Children []Node
}

func (n *DocumentNode) node() {}
func (n *DocumentNode) String() string {
	return fmt.Sprintf("DocumentNode{Children: %d}", len(n.Children))
}

type TagNode struct {
	Name       string
	Attributes map[string]*AttributeValue
	Children   []Node
	SelfClose  bool
	Line       int
	Col        int

	// noSpread and sortedAttrNames are a Compile-time precompute, populated
	// by compileTagAttrs (see expr_compile.go) before the AST is ever
	// rendered, and read-only afterward — so concurrent renders of the same
	// compiled Template may read them safely without synchronization.
	//
	// noSpread is true iff Attributes has no "&attributes" key. When it is
	// true, the tag's rendered attribute-name set is fixed at parse time (a
	// spread is the only thing that can add or remove keys at render time),
	// so sortedAttrNames holds sortAttrNames(Attributes) computed once here
	// instead of on every render. sortedAttrNames is nil and unused when
	// noSpread is false; renderTag falls back to sorting the render-time
	// merged attribute map for that case, and also falls back for any
	// TagNode this pass never reaches, since noSpread's zero value is false.
	noSpread        bool
	sortedAttrNames []string

	// staticClass and hasStaticClass are a Compile-time precompute (see
	// compileTagAttrs, expr_compile.go), read-only afterward — same
	// single-writer-then-read-only lifecycle as noSpread/sortedAttrNames
	// above, so concurrent renders of the same compiled Template may read
	// them safely without synchronization.
	//
	// hasStaticClass is true only for a no-spread tag whose "class" value is
	// PROVABLY a pure function of template text: it reaches
	// resolveClassTokenList's whole-quoted, non-empty branch at render time
	// with no possibility of evaluating a variable, operator, or object
	// literal (classifyStaticClassAttr proves this using the exact same
	// classification functions renderTag itself uses, so the two can never
	// disagree). When true, staticClass holds the already-resolved string
	// and renderTag uses it directly instead of re-parsing the raw value on
	// every render. hasStaticClass defaults to false — for any tag this pass
	// does not prove static (including every tag with an "&attributes"
	// spread, since a spread can inject or merge a class value at render
	// time), renderTag takes its unchanged runtime dispatch.
	staticClass    string
	hasStaticClass bool
}

func (n *TagNode) node() {}
func (n *TagNode) String() string {
	return fmt.Sprintf("TagNode{Name: %s, Children: %d, SelfClose: %v}", n.Name, len(n.Children), n.SelfClose)
}

// AttributeValue holds the raw string form of an attribute's value as it appeared in the Pug source. The runtime evaluates Value at render time.
type AttributeValue struct {
	Value     string
	Unescaped bool
	IsBare    bool // attribute with no value (e.g. checked, disabled)
}

type TextNode struct {
	Content string
	Line    int
	Col     int
}

func (n *TextNode) node() {}
func (n *TextNode) String() string {
	return fmt.Sprintf("TextNode{Content: %q}", n.Content)
}

type InterpolationNode struct {
	Expression string
	Unescaped  bool
	Line       int
	Col        int
}

func (n *InterpolationNode) node() {}
func (n *InterpolationNode) String() string {
	return fmt.Sprintf("InterpolationNode{Expr: %q, Unescaped: %v}", n.Expression, n.Unescaped)
}

type TagInterpolationNode struct {
	Tag  *TagNode
	Line int
	Col  int
}

func (n *TagInterpolationNode) node() {}
func (n *TagInterpolationNode) String() string {
	return fmt.Sprintf("TagInterpolationNode{Tag: %s}", n.Tag.Name)
}

// CommentNode: buffered (// — rendered as <!-- -->) or unbuffered (//- — stripped from output).
type CommentNode struct {
	Content  string
	Buffered bool
	Line     int
	Col      int
}

func (n *CommentNode) node() {}
func (n *CommentNode) String() string {
	return fmt.Sprintf("CommentNode{Buffered: %v, Content: %q}", n.Buffered, n.Content)
}

type CodeType int

const (
	CodeUnbuffered CodeType = iota // - expr  — executed, output discarded
	CodeBuffered                   // = expr  — evaluated and HTML-escaped into output
	CodeUnescaped                  // != expr — evaluated and written raw into output
)

type CodeNode struct {
	Expression string
	Type       CodeType
	Line       int
	Col        int

	// TagShorthand is true when this code was written immediately after a
	// tag on the tag's own source line (e.g. "title= pageTitle"), as opposed
	// to being its own indented child statement (e.g. "title" on one line,
	// "  = pageTitle" on the next). pug.js's own parser makes exactly this
	// grammatical distinction: tag-shorthand code is inline (it can never
	// start a new pretty-print line of its own), while the same code parsed
	// as a standalone child statement is not.
	TagShorthand bool

	// compiled holds a closure-compiled version of Expression for the
	// buffered/unescaped output shapes classifyExpr recognizes as safe to
	// precompute at Compile time (see expr_compile.go). It is nil for any
	// expression the classifier can't prove identical to the string
	// interpreter, in which case renderCode falls back to evaluateExpr.
	// It is populated exactly once, by compileExprs during Compile, before
	// the AST is ever rendered, and is read-only afterward — so concurrent
	// renders of the same compiled Template may read it safely without
	// synchronization.
	compiled compiledExpr
}

func (n *CodeNode) node() {}
func (n *CodeNode) String() string {
	return fmt.Sprintf("CodeNode{Type: %d, Expr: %q}", n.Type, n.Expression)
}

// ConditionalNode: when IsElseIf is true, Alternate contains a single ConditionalNode.
type ConditionalNode struct {
	Condition  string
	Consequent []Node
	Alternate  []Node
	IsElseIf   bool
	IsUnless   bool
	Line       int
	Col        int
}

func (n *ConditionalNode) node() {}
func (n *ConditionalNode) String() string {
	return fmt.Sprintf("ConditionalNode{Cond: %q, IsUnless: %v}", n.Condition, n.IsUnless)
}

type EachNode struct {
	ItemVar        string
	IndexVar       string
	CollectionExpr string
	Body           []Node
	EmptyBody      []Node
	Line           int
	Col            int
}

func (n *EachNode) node() {}
func (n *EachNode) String() string {
	return fmt.Sprintf("EachNode{Item: %s, Index: %s, Collection: %q}", n.ItemVar, n.IndexVar, n.CollectionExpr)
}

type WhileNode struct {
	Condition string
	Body      []Node
	Line      int
	Col       int
}

func (n *WhileNode) node() {}
func (n *WhileNode) String() string {
	return fmt.Sprintf("WhileNode{Cond: %q}", n.Condition)
}

// CaseNode: Default holds the body of the default clause (may be nil).
type CaseNode struct {
	Expression string
	Cases      []*WhenNode
	Default    []Node
	Line       int
	Col        int
}

func (n *CaseNode) node() {}
func (n *CaseNode) String() string {
	return fmt.Sprintf("CaseNode{Expr: %q, Cases: %d}", n.Expression, len(n.Cases))
}

type WhenNode struct {
	Expression string
	Body       []Node
	Line       int
	Col        int
}

func (n *WhenNode) node() {}
func (n *WhenNode) String() string {
	return fmt.Sprintf("WhenNode{Expr: %q}", n.Expression)
}

type MixinDeclNode struct {
	Name          string
	Parameters    []string
	ParamDefaults map[string]string // param name → default expression
	RestParamName string            // name of the ...rest parameter; empty if none
	Body          []Node
	Line          int
	Col           int
}

func (n *MixinDeclNode) node() {}
func (n *MixinDeclNode) String() string {
	return fmt.Sprintf("MixinDeclNode{Name: %s, Params: %v}", n.Name, n.Parameters)
}

// MixinCallNode: BlockContent holds any indented content passed to the mixin's block slot.
type MixinCallNode struct {
	Name         string
	Arguments    []string
	Attributes   map[string]*AttributeValue
	BlockContent []Node
	Line         int
	Col          int
}

func (n *MixinCallNode) node() {}
func (n *MixinCallNode) String() string {
	return fmt.Sprintf("MixinCallNode{Name: %s, Args: %v}", n.Name, n.Arguments)
}

type BlockMode int

const (
	BlockModeReplace BlockMode = iota // block name         — replaces the parent body
	BlockModeAppend                   // block append name  — appended after the parent body
	BlockModePrepend                  // block prepend name — prepended before the parent body
)

// BlockNode: Mode controls whether the child's content replaces, appends to, or prepends to the parent block's default body.
type BlockNode struct {
	Name string
	Body []Node
	Mode BlockMode
	Line int
	Col  int
}

func (n *BlockNode) node() {}
func (n *BlockNode) String() string {
	return fmt.Sprintf("BlockNode{Name: %s, Mode: %d}", n.Name, n.Mode)
}

type ExtendsNode struct {
	Path string
	Line int
	Col  int
}

func (n *ExtendsNode) node() {}
func (n *ExtendsNode) String() string {
	return fmt.Sprintf("ExtendsNode{Path: %q}", n.Path)
}

// IncludeNode: FilterName is non-empty when the include uses a filter (include:filtername path).
type IncludeNode struct {
	Path       string
	FilterName string
	Line       int
	Col        int
}

func (n *IncludeNode) node() {}
func (n *IncludeNode) String() string {
	return fmt.Sprintf("IncludeNode{Path: %q, Filter: %q}", n.Path, n.FilterName)
}

// FilterNode: Subfilter is non-empty for chained filters (:outer:inner); the runtime applies the innermost filter first. Options holds key=value pairs from the parenthesised argument list.
type FilterNode struct {
	Name      string
	Content   string
	Subfilter string
	Options   map[string]string
	Line      int
	Col       int
}

func (n *FilterNode) node() {}
func (n *FilterNode) String() string {
	return fmt.Sprintf("FilterNode{Name: %s, Content: %q}", n.Name, n.Content)
}

type DoctypeNode struct {
	Value string
	Line  int
	Col   int
}

func (n *DoctypeNode) node() {}
func (n *DoctypeNode) String() string {
	return fmt.Sprintf("DoctypeNode{Value: %q}", n.Value)
}

// PipeNode: unlike TextNode, produced at the top level of the document rather than as a tag child.
type PipeNode struct {
	Content string
	Line    int
	Col     int
}

func (n *PipeNode) node() {}
func (n *PipeNode) String() string {
	return fmt.Sprintf("PipeNode{Content: %q}", n.Content)
}

// BlockTextNode: indented block of text (tag.) passed through verbatim as the tag's text content.
type BlockTextNode struct {
	Content string
	Line    int
	Col     int
}

func (n *BlockTextNode) node() {}
func (n *BlockTextNode) String() string {
	return fmt.Sprintf("BlockTextNode{Content: %q}", n.Content)
}

// LiteralHTMLNode: written to output without escaping or processing.
type LiteralHTMLNode struct {
	Content string
	Line    int
	Col     int
}

func (n *LiteralHTMLNode) node() {}
func (n *LiteralHTMLNode) String() string {
	return fmt.Sprintf("LiteralHTMLNode{Content: %q}", n.Content)
}

// BlockExpansionNode: a parent tag with exactly one inline child tag (tag: child).
type BlockExpansionNode struct {
	Parent *TagNode
	Child  Node
	Line   int
	Col    int
}

func (n *BlockExpansionNode) node() {}
func (n *BlockExpansionNode) String() string {
	return fmt.Sprintf("BlockExpansionNode{Parent: %s}", n.Parent.Name)
}

// TextRunNode holds a mixed sequence of TextNode, InterpolationNode, and TagInterpolationNode produced when a single line contains both plain text and #{...} / !{...} / #[...] interpolations.
type TextRunNode struct {
	Nodes []Node
	Line  int
	Col   int
}

func (n *TextRunNode) node() {}
func (n *TextRunNode) String() string {
	return fmt.Sprintf("TextRunNode{Nodes: %d}", len(n.Nodes))
}
