package gopug

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Bug #1: Unclosed ${  in template literal → inverted slice → panic
// runtime.go:1953 — inner[i+2 : j-1] when j == i+2 yields inner[i+2:i+1]
//
// Trigger: evaluateExpr receives `${ (backtick + dollar + open-brace, no
// closing brace or backtick).  inner = "${", len 2.  The depth loop
// doesn't execute (j=2, j<2 is false), then inner[2:1] panics.
// ---------------------------------------------------------------------------

func TestTemplateLiteralUnclosedInterpolationPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic on unclosed ${: %v", r)
		}
	}()

	// `= ` followed by a backtick-string that ends immediately after ${
	// The lexer captures the whole rest-of-line as the expression,
	// so evaluateExpr receives literally: `${
	src := "= `${"
	_, err := Render(src, nil, nil)
	_ = err
}

func TestTemplateLiteralUnclosedWithContent(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic on unclosed ${expr: %v", r)
		}
	}()

	// Unclosed interpolation with content inside — depth never reaches 0,
	// j lands at len(inner), interp silently drops the last char.
	src := "- x = 5\n= `hello ${x"
	out, err := Render(src, nil, nil)
	_ = err
	// If it doesn't panic, the output should ideally contain "5",
	// but the silent truncation may produce wrong results.
	_ = out
}

// ---------------------------------------------------------------------------
// Bug #2: scanBalancedBraces — escape i+=2 without bounds check
// lexer.go:137 — if backslash is at the last position of s, i becomes
// len(s)+1; the loop exits and returns false even though the closing
// brace exists after the escape sequence.
//
// In practice the overrun is benign (loop condition catches it), but it
// can cause a semantic error: skipping one position too many inside a
// quoted string.  We test the concrete case where a trailing escaped
// quote in a string is followed by "}rest" — the function should find
// the closing brace.
// ---------------------------------------------------------------------------

func TestScanBalancedBracesEscapeAtBoundary(t *testing.T) {
	// String that contains an escaped quote right before the closing quote:
	// s = `"ab\""}rest`   →  the content is: "ab\"" followed by }rest
	//
	// Correct parse: the \" at position 4 is an escape, position 5 is the
	// closing quote, position 6 is }, position 7.. is rest.
	//
	// With the i+=2 bug: if the escape at position 4 causes i to jump to 6,
	// it skips the closing quote at 5 and the } at 6, leaving inDouble=true
	// and the function never finds the closing brace.
	s := `"ab\""}rest`
	expr, remaining, ok := scanBalancedBraces(s)
	if !ok {
		t.Fatalf("scanBalancedBraces(%q) returned ok=false; expected true\n"+
			"expr=%q remaining=%q", s, expr, remaining)
	}
	if remaining != "rest" {
		t.Errorf("remaining = %q, want %q", remaining, "rest")
	}
	_ = expr
}

func TestScanBalancedBracketsEscapeAtBoundary(t *testing.T) {
	s := `"ab\""]rest`
	expr, remaining, ok := scanBalancedBrackets(s)
	if !ok {
		t.Fatalf("scanBalancedBrackets(%q) returned ok=false; expected true\n"+
			"expr=%q remaining=%q", s, expr, remaining)
	}
	if remaining != "rest" {
		t.Errorf("remaining = %q, want %q", remaining, "rest")
	}
	_ = expr
}

// ---------------------------------------------------------------------------
// Bug #4: CompileFile mutates caller's opts.Basedir in place
// gopug.go:136 — sets opts.Basedir without copying the struct
// ---------------------------------------------------------------------------

func TestCompileFileMutatesCallerBasedir(t *testing.T) {
	ClearCache()
	defer ClearCache()

	dir1 := t.TempDir()
	dir2 := t.TempDir()

	os.WriteFile(filepath.Join(dir1, "a.pug"), []byte("p from-dir1"), 0644)
	os.WriteFile(filepath.Join(dir2, "b.pug"), []byte("p from-dir2"), 0644)

	opts := &Options{}

	_, err := CompileFile(filepath.Join(dir1, "a.pug"), opts)
	if err != nil {
		t.Fatalf("CompileFile a.pug: %v", err)
	}

	basedirAfterFirst := opts.Basedir

	_, err = CompileFile(filepath.Join(dir2, "b.pug"), opts)
	if err != nil {
		t.Fatalf("CompileFile b.pug: %v", err)
	}

	if basedirAfterFirst != "" && opts.Basedir == basedirAfterFirst {
		t.Errorf("CompileFile mutated caller's opts.Basedir to %q on first call "+
			"and kept it for the second call (should be %q)",
			opts.Basedir, filepath.Dir(filepath.Join(dir2, "b.pug")))
	}
}

// ---------------------------------------------------------------------------
// Bug #5: Nil FilterFunc causes panic
// runtime.go:1527,1681 — fn(...) called without nil check
// ---------------------------------------------------------------------------

func TestNilFilterFuncPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil FilterFunc caused a panic instead of a graceful error: %v", r)
		}
	}()

	src := ":myfilter\n  some content"
	opts := &Options{
		Filters: map[string]FilterFunc{
			"myfilter": nil,
		},
	}
	_, err := Render(src, nil, opts)
	if err == nil {
		t.Errorf("expected an error for nil FilterFunc, got nil")
	}
}

// ---------------------------------------------------------------------------
// Bug #6: -= operator silently skips assignment for non-numeric RHS
// runtime.go:1071-1074 — no else branch, unlike += which has string fallback
// ---------------------------------------------------------------------------

func TestMinusEqualsNonNumericRHS(t *testing.T) {
	// Before the fix: -=  with a non-numeric RHS silently left the variable
	// unchanged and returned nil.  After the fix: returns a descriptive error.
	src := "- x = 10\n- x -= \"hello\"\n= x"
	_, err := Render(src, nil, nil)
	if err == nil {
		t.Errorf("-= with non-numeric RHS should return an error, got nil")
	}
}
