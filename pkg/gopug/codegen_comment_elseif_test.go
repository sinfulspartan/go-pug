package gopug

import (
	"fmt"
	"strings"
	"testing"
)

// TestCodegenCommentBuffered proves a buffered (`//`) comment is emitted
// through genNode's *CommentNode case as the same "<!-- " + Content + " -->"
// static text renderComment writes, matching the interpreter byte for byte.
func TestCodegenCommentBuffered(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "plain buffered comment",
		src:         "// hello world\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenCommentUnbuffered proves an unbuffered (`//-`) comment produces
// no output at all in codegen, matching renderComment's no-op branch.
func TestCodegenCommentUnbuffered(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "unbuffered comment produces no output",
		src:         "//- secret\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenCommentUnescapedContent proves a buffered comment's Content is
// emitted RAW, not HTML-escaped, matching renderComment's direct
// r.htmlBuf.WriteString(comment.Content) with no html.EscapeString call —
// the `<`, `&`, and `"` characters below would come out as entities if the
// codegen path routed Content through htmlEscapeText instead of the static
// text path.
func TestCodegenCommentUnescapedContent(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "buffered comment content with HTML-special characters stays raw",
		src:         `// <b>&"quoted"` + "\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenCommentInterleavedWithTags proves a buffered comment's position
// and surrounding whitespace match the interpreter's when it sits between
// two tags.
func TestCodegenCommentInterleavedWithTags(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "comment interleaved with tags",
		src:         "p Before\n// note\np After\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenCommentBlock proves a multi-line (block) buffered comment, whose
// parser joins the indented body lines with "\n" into a single Content
// string, round-trips through the static-text path unchanged.
func TestCodegenCommentBlock(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "block buffered comment",
		src:         "//\n  first line\n  second line\np after\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenCommentUnbufferedBlock proves a multi-line unbuffered comment
// also produces no output, matching the plain unbuffered case.
func TestCodegenCommentUnbufferedBlock(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "block unbuffered comment produces no output",
		src:         "//-\n  first line\n  second line\np after\n",
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenElseIfBranchSelection proves an `if a / else if b / else c`
// chain selects the same branch in codegen as in the interpreter across
// every combination of the two conditions' truth values: a true (first
// branch wins regardless of b), a false/b true (second branch), and both
// false (the trailing else).
func TestCodegenElseIfBranchSelection(t *testing.T) {
	cases := []struct {
		name string
		a, b bool
	}{
		{name: "a true, b true: first branch", a: true, b: true},
		{name: "a true, b false: first branch", a: true, b: false},
		{name: "a false, b true: second branch", a: false, b: true},
		{name: "a false, b false: else branch", a: false, b: false},
	}

	src := "if Flag\n  p a\nelse if FlagB\n  p b\nelse\n  p c\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.a, "FlagB": tc.b},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v, FlagB: %v}", tc.a, tc.b),
			})
		})
	}
}

// TestCodegenElseIfNoTrailingElse proves an else-if chain with no final
// `else` (falling through to nothing when every condition is false) matches
// the interpreter, both when a branch is taken and when none is.
func TestCodegenElseIfNoTrailingElse(t *testing.T) {
	src := "if Flag\n  p a\nelse if FlagB\n  p b\n"

	cases := []struct {
		name string
		a, b bool
	}{
		{name: "a true: first branch", a: true, b: false},
		{name: "b true: second branch", a: false, b: true},
		{name: "neither true: no output", a: false, b: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.a, "FlagB": tc.b},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v, FlagB: %v}", tc.a, tc.b),
			})
		})
	}
}

// TestCodegenElseIfThreeDeep proves a three-level else-if chain
// (if/else-if/else-if/else) resolves each of its four possible branches
// identically to the interpreter.
func TestCodegenElseIfThreeDeep(t *testing.T) {
	src := "if Flag\n  p a\nelse if FlagB\n  p b\nelse if FlagC\n  p c\nelse\n  p d\n"

	cases := []struct {
		name    string
		a, b, c bool
	}{
		{name: "a true: branch a", a: true, b: false, c: false},
		{name: "b true: branch b", a: false, b: true, c: false},
		{name: "c true: branch c", a: false, b: false, c: true},
		{name: "none true: branch d (else)", a: false, b: false, c: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.a, "FlagB": tc.b, "FlagC": tc.c},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v, FlagB: %v, FlagC: %v}", tc.a, tc.b, tc.c),
			})
		})
	}
}

// TestCodegenElseIfConditionOperator proves an else-if condition using a
// comparison operator (Count > 3) resolves through genCondition the same way
// a top-level if condition does.
func TestCodegenElseIfConditionOperator(t *testing.T) {
	src := "if Flag\n  p a\nelse if Count > 3\n  p b\nelse\n  p c\n"

	cases := []struct {
		name  string
		flag  bool
		count int
	}{
		{name: "flag true: branch a", flag: true, count: 0},
		{name: "flag false, count > 3: branch b", flag: false, count: 5},
		{name: "flag false, count <= 3: branch c (else)", flag: false, count: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.flag, "Count": tc.count},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v, Count: %d}", tc.flag, tc.count),
			})
		})
	}
}

// TestCodegenElseIfConditionCombinator proves an else-if condition using a
// `&&`/`||` combinator resolves through genCondition's own combinator
// restructure the same way a top-level if condition does.
func TestCodegenElseIfConditionCombinator(t *testing.T) {
	src := "if Flag\n  p a\nelse if FlagB && FlagC\n  p b\nelse\n  p c\n"

	cases := []struct {
		name       string
		flag, b, c bool
	}{
		{name: "flag true: branch a", flag: true, b: false, c: false},
		{name: "flag false, b&&c true: branch b", flag: false, b: true, c: true},
		{name: "flag false, only b true: branch c (else)", flag: false, b: true, c: false},
		{name: "all false: branch c (else)", flag: false, b: false, c: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.flag, "FlagB": tc.b, "FlagC": tc.c},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v, FlagB: %v, FlagC: %v}", tc.flag, tc.b, tc.c),
			})
		})
	}
}

// TestCodegenElseIfNested proves an else-if branch whose body contains its
// own nested `if` (a tag with an inner conditional) matches the interpreter,
// exercising genNode's recursion through both the else-if restructure and an
// ordinary nested conditional in the same template.
func TestCodegenElseIfNested(t *testing.T) {
	src := "if Flag\n  p a\nelse if FlagB\n  div\n    if FlagC\n      p nested-yes\n    else\n      p nested-no\nelse\n  p c\n"

	cases := []struct {
		name    string
		a, b, c bool
	}{
		{name: "a true: branch a, nested not reached", a: true, b: false, c: false},
		{name: "b true, nested true", a: false, b: true, c: true},
		{name: "b true, nested false", a: false, b: true, c: false},
		{name: "neither a nor b: branch c (else)", a: false, b: false, c: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, codegenArithCase{
				name:        tc.name,
				src:         src,
				data:        map[string]any{"Flag": tc.a, "FlagB": tc.b, "FlagC": tc.c},
				dataLiteral: fmt.Sprintf("opsData{Flag: %v, FlagB: %v, FlagC: %v}", tc.a, tc.b, tc.c),
			})
		})
	}
}

// TestCodegenPlainIfRegression proves a plain `if`/`if-else` (no else-if)
// still generates and renders byte-identically to the interpreter, guarding
// against a regression from removing genConditional's else-if rejection.
func TestCodegenPlainIfRegression(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "plain if, true",
			src:         "if Flag\n  p yes\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "plain if, false, no output",
			src:         "if Flag\n  p yes\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
		{
			name:        "plain if-else, true branch",
			src:         "if Flag\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "plain if-else, false branch",
			src:         "if Flag\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenUnlessStillRejected proves `unless` remains an unsupported
// construct after the else-if guard is removed — the else-if fix must not
// accidentally widen genConditional's IsUnless rejection.
func TestCodegenUnlessStillRejected(t *testing.T) {
	src := "unless Flag\n  p yes\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}
