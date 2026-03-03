package gopug

import "fmt"

// Node represents a node in the abstract syntax tree.
type Node interface {
	node()
	String() string
}

// DocumentNode is the root node containing all parsed content.
type DocumentNode struct {
	Children []Node
}

func (n *DocumentNode) node() {}
func (n *DocumentNode) String() string {
	return fmt.Sprintf("DocumentNode{Children: %d}", len(n.Children))
}

// TagNode represents an HTML tag with attributes and children.
type TagNode struct {
	Name       string
	Attributes map[string]*AttributeValue
	Children   []Node
	SelfClose  bool
	Line       int
	Col        int
}

func (n *TagNode) node() {}
func (n *TagNode) String() string {
	return fmt.Sprintf("TagNode{Name: %s, Children: %d, SelfClose: %v}", n.Name, len(n.Children), n.SelfClose)
}

// AttributeValue represents the value of an attribute (can be a literal or an expression).
type AttributeValue struct {
	Value     string
	Unescaped bool
}

// TextNode represents plain text content.
type TextNode struct {
	Content string
	Line    int
	Col     int
}

func (n *TextNode) node() {}
func (n *TextNode) String() string {
	return fmt.Sprintf("TextNode{Content: %q}", n.Content)
}

// InterpolationNode represents #{...} or !{...} interpolation.
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

// TagInterpolationNode represents #[tag content] inline tag interpolation.
type TagInterpolationNode struct {
	Tag  *TagNode
	Line int
	Col  int
}

func (n *TagInterpolationNode) node() {}
func (n *TagInterpolationNode) String() string {
	return fmt.Sprintf("TagInterpolationNode{Tag: %s}", n.Tag.Name)
}

// CommentNode represents an HTML comment or unbuffered comment.
type CommentNode struct {
	Content  string
	Buffered bool // true for <!-- -->, false for silent comment
	Line     int
	Col      int
}

func (n *CommentNode) node() {}
func (n *CommentNode) String() string {
	return fmt.Sprintf("CommentNode{Buffered: %v, Content: %q}", n.Buffered, n.Content)
}

// CodeNode represents unbuffered, buffered, or unescaped code.
type CodeNode struct {
	Expression string
	Type       CodeType
	Line       int
	Col        int
}

type CodeType int

const (
	CodeUnbuffered CodeType = iota
	CodeBuffered
	CodeUnescaped
)

func (n *CodeNode) node() {}
func (n *CodeNode) String() string {
	return fmt.Sprintf("CodeNode{Type: %d, Expr: %q}", n.Type, n.Expression)
}

// ConditionalNode represents if/else if/else blocks.
type ConditionalNode struct {
	Condition  string
	Consequent []Node
	Alternate  []Node // else or else if
	IsElseIf   bool
	IsUnless   bool
	Line       int
	Col        int
}

func (n *ConditionalNode) node() {}
func (n *ConditionalNode) String() string {
	return fmt.Sprintf("ConditionalNode{Cond: %q, IsUnless: %v}", n.Condition, n.IsUnless)
}

// EachNode represents an each/for loop.
type EachNode struct {
	Item       string // variable name
	Key        string // optional key variable
	Collection string // expression
	Body       []Node
	ElseBody   []Node
	Line       int
	Col        int
}

func (n *EachNode) node() {}
func (n *EachNode) String() string {
	return fmt.Sprintf("EachNode{Item: %s, Key: %s, Collection: %q}", n.Item, n.Key, n.Collection)
}

// WhileNode represents a while loop.
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

// CaseNode represents a case/switch statement.
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

// WhenNode represents a when clause within a case.
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

// MixinDeclNode represents a mixin declaration.
type MixinDeclNode struct {
	Name       string
	Parameters []string
	RestParam  string // for ...args
	Body       []Node
	Line       int
	Col        int
}

func (n *MixinDeclNode) node() {}
func (n *MixinDeclNode) String() string {
	return fmt.Sprintf("MixinDeclNode{Name: %s, Params: %v}", n.Name, n.Parameters)
}

// MixinCallNode represents a mixin call (+name).
type MixinCallNode struct {
	Name       string
	Arguments  []string
	Attributes map[string]*AttributeValue
	Block      []Node // content passed to mixin
	Line       int
	Col        int
}

func (n *MixinCallNode) node() {}
func (n *MixinCallNode) String() string {
	return fmt.Sprintf("MixinCallNode{Name: %s, Args: %v}", n.Name, n.Arguments)
}

// BlockNode represents a named block (for template inheritance).
type BlockNode struct {
	Name string
	Body []Node
	Mode BlockMode
	Line int
	Col  int
}

type BlockMode int

const (
	BlockModeReplace BlockMode = iota
	BlockModeAppend
	BlockModePrepend
)

func (n *BlockNode) node() {}
func (n *BlockNode) String() string {
	return fmt.Sprintf("BlockNode{Name: %s, Mode: %d}", n.Name, n.Mode)
}

// ExtendsNode represents an extends declaration.
type ExtendsNode struct {
	Path string
	Line int
	Col  int
}

func (n *ExtendsNode) node() {}
func (n *ExtendsNode) String() string {
	return fmt.Sprintf("ExtendsNode{Path: %q}", n.Path)
}

// IncludeNode represents an include directive.
type IncludeNode struct {
	Path   string
	Filter string // optional filter name
	Line   int
	Col    int
}

func (n *IncludeNode) node() {}
func (n *IncludeNode) String() string {
	return fmt.Sprintf("IncludeNode{Path: %q, Filter: %q}", n.Path, n.Filter)
}

// FilterNode represents a filtered block (:markdown-it, etc).
type FilterNode struct {
	Name      string
	Args      string
	Content   string
	Subfilter string
	Line      int
	Col       int
}

func (n *FilterNode) node() {}
func (n *FilterNode) String() string {
	return fmt.Sprintf("FilterNode{Name: %s, Args: %q}", n.Name, n.Args)
}

// DoctypeNode represents a doctype declaration.
type DoctypeNode struct {
	Value string // e.g., "html", "xml", "transitional"
	Line  int
	Col   int
}

func (n *DoctypeNode) node() {}
func (n *DoctypeNode) String() string {
	return fmt.Sprintf("DoctypeNode{Value: %q}", n.Value)
}

// PipeNode represents piped text (plain text block).
type PipeNode struct {
	Content string
	Line    int
	Col     int
}

func (n *PipeNode) node() {}
func (n *PipeNode) String() string {
	return fmt.Sprintf("PipeNode{Content: %q}", n.Content)
}

// BlockTextNode represents block text (indented text after .).
type BlockTextNode struct {
	Content string
	Line    int
	Col     int
}

func (n *BlockTextNode) node() {}
func (n *BlockTextNode) String() string {
	return fmt.Sprintf("BlockTextNode{Content: %q}", n.Content)
}

// LiteralHTMLNode represents literal HTML (line starting with <).
type LiteralHTMLNode struct {
	Content string
	Line    int
	Col     int
}

func (n *LiteralHTMLNode) node() {}
func (n *LiteralHTMLNode) String() string {
	return fmt.Sprintf("LiteralHTMLNode{Content: %q}", n.Content)
}

// BlockExpansionNode represents inline nested tag (tag: nested).
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

// TextRunNode holds a mixed sequence of TextNode, PipeNode, and
// InterpolationNode values produced when a single line contains both plain
// text and #{...} / !{...} interpolations.  The runtime renders each child
// node in order.
type TextRunNode struct {
	Nodes []Node
	Line  int
	Col   int
}

func (n *TextRunNode) node() {}
func (n *TextRunNode) String() string {
	return fmt.Sprintf("TextRunNode{Nodes: %d}", len(n.Nodes))
}
