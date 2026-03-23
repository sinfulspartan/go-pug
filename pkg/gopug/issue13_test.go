package gopug

import (
	"strings"
	"testing"
)

func TestIssue13TernaryWithClassShorthand(t *testing.T) {
	// Issue #13: Ternary expression in class= is rendered literally when combined with class shorthand
	src := `.card.notif-item(class= !notification.IsRead ? "bg-body-secondary" : "")`
	data := map[string]interface{}{
		"notification": map[string]interface{}{"IsRead": false},
	}
	out := renderTest(t, src, data)
	t.Logf("output: %q", out)

	// Expected: <div class="card notif-item bg-body-secondary"></div>
	// Bug: class attribute contains literal expression string

	if strings.Contains(out, "!notification.IsRead") || strings.Contains(out, "?") {
		t.Errorf("ternary expression was rendered literally, got: %q", out)
	}
	if !strings.Contains(out, "card") || !strings.Contains(out, "notif-item") {
		t.Errorf("shorthand classes missing, got: %q", out)
	}
	if !strings.Contains(out, "bg-body-secondary") {
		t.Errorf("ternary result missing from output, got: %q", out)
	}
}

func TestIssue13ClassObjectWithClassShorthand(t *testing.T) {
	// Issue #13: class-object expression in class= is rendered literally when combined with class shorthand
	src := `.card.notif-item(class={"bg-body-secondary": !notification.IsRead})`
	data := map[string]interface{}{
		"notification": map[string]interface{}{"IsRead": false},
	}
	out := renderTest(t, src, data)
	t.Logf("output: %q", out)

	// Expected: <div class="card notif-item bg-body-secondary"></div>
	if strings.Contains(out, "{") || strings.Contains(out, "}") {
		t.Errorf("class-object was rendered literally, got: %q", out)
	}
}

func TestIssue13ClassObjectWithoutShorthand(t *testing.T) {
	// Issue #13: class-object without class shorthand works correctly
	src := `div(class={"active": isActive, "disabled": false})`
	data := map[string]interface{}{
		"isActive": true,
	}
	out := renderTest(t, src, data)
	t.Logf("output without shorthand: %q", out)

	// This should work - just verify
	if !strings.Contains(out, "active") {
		t.Errorf("class-object without shorthand should work, got: %q", out)
	}
}
