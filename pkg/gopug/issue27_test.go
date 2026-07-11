package gopug

import (
	"strings"
	"testing"
)

func TestIssue27EmptyClassVarWithShorthandDropsToken(t *testing.T) {
	src := `div.text-end(class=cls) hi`
	data := map[string]any{"cls": ""}
	out := renderTest(t, src, data)
	t.Logf("output: %q", out)

	if !strings.Contains(out, `class="text-end"`) {
		t.Errorf("expected class=%q, got: %q", "text-end", out)
	}
	if strings.Contains(out, "cls") {
		t.Errorf("variable name leaked into output, got: %q", out)
	}
}

func TestIssue27NonEmptyClassVarWithShorthandMerges(t *testing.T) {
	src := `div.text-end(class=cls) hi`
	data := map[string]any{"cls": "fw-bold"}
	out := renderTest(t, src, data)
	t.Logf("output: %q", out)

	if !strings.Contains(out, `class="text-end fw-bold"`) {
		t.Errorf("expected class=%q, got: %q", "text-end fw-bold", out)
	}
}

func TestIssue27MultipleShorthandClassesWithEmptyVar(t *testing.T) {
	src := `div.text-end.mb-0(class=cls) hi`
	data := map[string]any{"cls": ""}
	out := renderTest(t, src, data)
	t.Logf("output: %q", out)

	if !strings.Contains(out, `class="text-end mb-0"`) {
		t.Errorf("expected class=%q, got: %q", "text-end mb-0", out)
	}
	if strings.Contains(out, "cls") {
		t.Errorf("variable name leaked into output, got: %q", out)
	}
}

func TestIssue27VarAssignedEmptyStringWithShorthand(t *testing.T) {
	src := "- var cls = \"\"\ndiv.text-end(class=cls) hi"
	out := renderTest(t, src, nil)
	t.Logf("output: %q", out)

	if !strings.Contains(out, `class="text-end"`) {
		t.Errorf("expected class=%q, got: %q", "text-end", out)
	}
	if strings.Contains(out, "cls") {
		t.Errorf("variable name leaked into output, got: %q", out)
	}
}

func TestIssue27PlainFormWithEmptyVarStillWorks(t *testing.T) {
	src := `div(class=cls) hi`
	data := map[string]any{"cls": ""}
	out := renderTest(t, src, data)
	t.Logf("output: %q", out)

	if !strings.Contains(out, `class=""`) {
		t.Errorf("expected empty class attribute, got: %q", out)
	}
}

func TestIssue27QuotedStaticNewValueStillMergesStatically(t *testing.T) {
	src := `div.base(class="foo bar") hi`
	out := renderTest(t, src, nil)
	t.Logf("output: %q", out)

	if !strings.Contains(out, `class="base foo bar"`) {
		t.Errorf("expected class=%q, got: %q", "base foo bar", out)
	}
}
