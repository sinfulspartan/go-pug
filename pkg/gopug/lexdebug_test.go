package gopug

import (
	"fmt"
	"testing"
)

func TestDebugElseIf(t *testing.T) {
	src := "if val == 1\n  p One\nelse if val == 2\n  p Two\nelse\n  p Other"
	l := NewLexer(src)
	tokens, _ := l.Lex()
	p := NewParser(tokens)
	doc, err := p.Parse()
	if err != nil {
		t.Fatal("parse error:", err)
	}
	c := doc.Children[0].(*ConditionalNode)
	c2 := c.Alternate[0].(*ConditionalNode)
	fmt.Printf("c2 condition: %q\n", c2.Condition)
	fmt.Printf("c2 consequent[0]: %T %s\n", c2.Consequent[0], c2.Consequent[0])
	fmt.Printf("c2 alternate[0]:  %T %s\n", c2.Alternate[0], c2.Alternate[0])
}
