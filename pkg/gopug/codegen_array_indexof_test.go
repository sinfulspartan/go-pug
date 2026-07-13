package gopug

import (
	"testing"
)

// manageGroupArrayLiteral and navGroupArrayLiteral are the exact array
// literals the real manageGroupActive (layout/admin.pug) and navGroupActive
// (layout/app.pug) nav-highlighting idiom uses, verbatim, so this file's
// differential coverage proves the two REAL corpus shapes work, not just a
// simplified stand-in.
const manageGroupArrayLiteral = `["users", "firms", "firm-applications", "subscription-plans", "tax-rates", "coverage-map", "audit-log", "feedback"]`
const navGroupArrayLiteral = `["offers", "my-tasks", "diary", "my-earnings", "profile", "settings", "admin"]`

// --- Headline: both real corpus shapes ---

// TestCodegenArrayIndexOfBoolLocalManageShape is the manageGroupActive
// headline shape: a `- var` assigned an array-literal `.indexOf(...) !== -1`
// comparison, read back in both a condition (`if`) and a stringify (`#{}`-
// equivalent `= x`) position, across every element-semantics case that
// matters — including "firm", a value that is only a SUBSTRING of the
// "firm-applications" element (the element-semantics case the interpreter's
// array-receiver `.indexOf` fix targets: a naive substring search would
// wrongly report this as found).
func TestCodegenArrayIndexOfBoolLocalManageShape(t *testing.T) {
	src := "- var manageGroupActive = " + manageGroupArrayLiteral + ".indexOf(Slug) !== -1\n" +
		"if manageGroupActive\n  p yes\nelse\n  p no\n" +
		"p=manageGroupActive\n"

	cases := []codegenUnbufferedCase{
		{name: "first element", data: map[string]any{"Slug": "users"}, dataLiteral: `opsData{Slug: "users"}`},
		{name: "later element", data: map[string]any{"Slug": "firms"}, dataLiteral: `opsData{Slug: "firms"}`},
		{name: "not a member", data: map[string]any{"Slug": "dashboard"}, dataLiteral: `opsData{Slug: "dashboard"}`},
		{name: "substring of an element but not itself an element", data: map[string]any{"Slug": "firm"}, dataLiteral: `opsData{Slug: "firm"}`},
	}
	for _, tc := range cases {
		tc.src = src
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenArrayIndexOfBoolLocalNavShape is the second real corpus shape,
// navGroupActive, proving the feature isn't accidentally specific to the
// first array's element count/content.
func TestCodegenArrayIndexOfBoolLocalNavShape(t *testing.T) {
	src := "- var navGroupActive = " + navGroupArrayLiteral + ".indexOf(Slug) !== -1\n" +
		"if navGroupActive\n  p yes\nelse\n  p no\n" +
		"p=navGroupActive\n"

	cases := []codegenUnbufferedCase{
		{name: "first element", data: map[string]any{"Slug": "offers"}, dataLiteral: `opsData{Slug: "offers"}`},
		{name: "last element", data: map[string]any{"Slug": "admin"}, dataLiteral: `opsData{Slug: "admin"}`},
		{name: "not a member", data: map[string]any{"Slug": "dashboard"}, dataLiteral: `opsData{Slug: "dashboard"}`},
	}
	for _, tc := range cases {
		tc.src = src
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenArrayIndexOfBoolLocalIndexZero specifically proves the index-0
// case: the FIRST array element's index stringifies to "0", and "0" !== "-1"
// must still numerically compare true (not accidentally be treated as falsy
// the way gopug.Truthy would treat a bare "0" string) — the crux of wiring
// the comparison as a genuine numeric coercion rather than a string-truthy
// check.
func TestCodegenArrayIndexOfBoolLocalIndexZero(t *testing.T) {
	src := "- var found = [\"users\", \"firms\"].indexOf(Slug) !== -1\n" +
		"if found\n  p yes\nelse\n  p no\n" +
		"p=found\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Slug": "users"},
		dataLiteral: `opsData{Slug: "users"}`,
	})
}

// --- .includes variant (no comparison needed at all) ---

// TestCodegenArrayIncludesBoolLocal proves the simpler `.includes(...)`
// sibling — used directly as a bool RHS, with no `!== -1` at all, since
// MethodIncludesSlice already returns the interpreter's own canonical
// "true"/"false" — reads back correctly for both membership outcomes.
func TestCodegenArrayIncludesBoolLocal(t *testing.T) {
	src := "- var manageGroupActive = " + manageGroupArrayLiteral + ".includes(Slug)\n" +
		"if manageGroupActive\n  p yes\nelse\n  p no\n" +
		"p=manageGroupActive\n"

	cases := []codegenUnbufferedCase{
		{name: "member", data: map[string]any{"Slug": "tax-rates"}, dataLiteral: `opsData{Slug: "tax-rates"}`},
		{name: "not a member", data: map[string]any{"Slug": "dashboard"}, dataLiteral: `opsData{Slug: "dashboard"}`},
	}
	for _, tc := range cases {
		tc.src = src
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenArrayContainsBoolLocal proves the `.contains` alias (this
// engine's own extension, not standard JS, but handled identically to
// `.includes` throughout) behaves the same way.
func TestCodegenArrayContainsBoolLocal(t *testing.T) {
	src := "- var isMember = [\"a\", \"b\", \"c\"].contains(Slug)\n" +
		"if isMember\n  p yes\nelse\n  p no\n" +
		"p=isMember\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Slug": "b"},
		dataLiteral: `opsData{Slug: "b"}`,
	})
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Slug": "z"},
		dataLiteral: `opsData{Slug: "z"}`,
	})
}

// --- === -1 (negated) variant ---

// TestCodegenArrayIndexOfBoolLocalNegated proves the `=== -1` polarity (the
// complement of `!== -1`) reads back correctly.
func TestCodegenArrayIndexOfBoolLocalNegated(t *testing.T) {
	src := "- var notInGroup = " + manageGroupArrayLiteral + ".indexOf(Slug) === -1\n" +
		"if notInGroup\n  p yes\nelse\n  p no\n" +
		"p=notInGroup\n"

	cases := []codegenUnbufferedCase{
		{name: "member (=== -1 is false)", data: map[string]any{"Slug": "users"}, dataLiteral: `opsData{Slug: "users"}`},
		{name: "not a member (=== -1 is true)", data: map[string]any{"Slug": "dashboard"}, dataLiteral: `opsData{Slug: "dashboard"}`},
	}
	for _, tc := range cases {
		tc.src = src
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// --- The real-world composite idiom: a chained `||` reading a prior bool local ---

// TestCodegenArrayIndexOfBoolLocalOrCombinator proves app.pug's own second
// line — `navToggleActive = navGroupActive || currentPage === "dashboard"` —
// works too: an array-indexOf-derived bool local used as the LEFT operand of
// a later `- var`'s "||", both operands provably bool.
func TestCodegenArrayIndexOfBoolLocalOrCombinator(t *testing.T) {
	src := "- var navGroupActive = " + navGroupArrayLiteral + ".indexOf(Slug) !== -1\n" +
		"- var navToggleActive = navGroupActive || Slug === \"dashboard\"\n" +
		"if navToggleActive\n  p yes\nelse\n  p no\n" +
		"p=navToggleActive\n"

	cases := []codegenUnbufferedCase{
		{name: "in nav group", data: map[string]any{"Slug": "offers"}, dataLiteral: `opsData{Slug: "offers"}`},
		{name: "dashboard (not in nav group, but matches the OR's right operand)", data: map[string]any{"Slug": "dashboard"}, dataLiteral: `opsData{Slug: "dashboard"}`},
		{name: "neither", data: map[string]any{"Slug": "users"}, dataLiteral: `opsData{Slug: "users"}`},
	}
	for _, tc := range cases {
		tc.src = src
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// --- Value context beyond the bool local: works "for free" via genMethodCall ---

// TestCodegenArrayIndexOfNumericContextInterpolationWorks proves a bare
// array-literal `.indexOf(...)` used directly in an interpolation (a numeric-
// looking VALUE, not compared to anything) already works, as a byproduct of
// genMethodCall's general array-literal `.indexOf` support — it is not
// restricted to the `!== -1` comparison shape.
func TestCodegenArrayIndexOfNumericContextInterpolationWorks(t *testing.T) {
	src := "p=" + manageGroupArrayLiteral + ".indexOf(Slug)\n"
	cases := []codegenUnbufferedCase{
		{name: "found at index 0", data: map[string]any{"Slug": "users"}, dataLiteral: `opsData{Slug: "users"}`},
		{name: "found at a later index", data: map[string]any{"Slug": "audit-log"}, dataLiteral: `opsData{Slug: "audit-log"}`},
		{name: "not found", data: map[string]any{"Slug": "dashboard"}, dataLiteral: `opsData{Slug: "dashboard"}`},
	}
	for _, tc := range cases {
		tc.src = src
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenArrayIndexOfNumericContextRawInterpolationWorks is the same
// numeric-context proof as TestCodegenArrayIndexOfNumericContextInterpolationWorks
// but through the literal `#{...}` escaped-interpolation syntax (genInterpolation)
// rather than buffered code (`=`, genCode) — both funnel through genValueExpr
// identically, but this proves the `#{}` spelling the task's own wording uses
// explicitly, not just its buffered-code equivalent.
func TestCodegenArrayIndexOfNumericContextRawInterpolationWorks(t *testing.T) {
	src := "p #{" + manageGroupArrayLiteral + ".indexOf(Slug)}\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Slug": "coverage-map"},
		dataLiteral: `opsData{Slug: "coverage-map"}`,
	})
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Slug": "dashboard"},
		dataLiteral: `opsData{Slug: "dashboard"}`,
	})
}

// --- Deferrals ---

// TestCodegenArrayIndexOfNonStringLiteralElementDeferred asserts an array
// literal containing a non-string-literal element (a bare number here) is
// rejected with a clean, distinct error rather than guessed at — this
// increment only compiles the STRING-array-literal shape the real corpus
// uses.
func TestCodegenArrayIndexOfNonStringLiteralElementDeferred(t *testing.T) {
	src := "- var x = [1, 2, 3].indexOf(Slug) !== -1\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-element error, got nil", src)
	}
}

// TestCodegenArrayIndexOfMixedElementDeferred asserts a MOSTLY-string-literal
// array with one non-literal element (a bare identifier) is rejected too —
// proving the element check applies to every element, not just the first.
func TestCodegenArrayIndexOfMixedElementDeferred(t *testing.T) {
	src := "- var x = [\"a\", Slug, \"c\"].indexOf(Slug) !== -1\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-element error, got nil", src)
	}
}

// TestCodegenArrayIndexOfVariableReceiverDeferred asserts a DYNAMIC array
// receiver (`Items.indexOf(...)`, a slice-typed FIELD, not an array literal)
// is rejected with a clean, distinct error: this is deliberately NOT the same
// code path as the array-literal case, and the interpreter's own
// element-vs-substring distinction for a variable receiver is already
// handled by Runtime directly (see array_method_semantics_test.go) — this
// increment does not extend codegen to that shape.
func TestCodegenArrayIndexOfVariableReceiverDeferred(t *testing.T) {
	src := "- var x = Items.indexOf(Slug) !== -1\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenArrayIndexOfBareNumericLocalDeferred asserts that assigning a
// bare array-literal `.indexOf(...)` result (no comparison at all) directly
// to a `- var` is still rejected: genBoolExpr doesn't classify it (no
// comparison operator present), and genNumericExpr/genAssignRHS don't model a
// method call either, so it correctly falls all the way through to a clean
// "unsupported" error — the numeric-context USE that works for free is
// restricted to a direct value position (interpolation/buffered code/
// attribute), not a `- var` assignment target. See
// TestCodegenArrayIndexOfNumericContextInterpolationWorks for the value-
// context shape that DOES work.
func TestCodegenArrayIndexOfBareNumericLocalDeferred(t *testing.T) {
	src := "- var idx = " + manageGroupArrayLiteral + ".indexOf(Slug)\np=idx\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenArrayLiteralEachIterationNowSupported proves that an array
// literal used as an `each` collection — a wholly different lever from this
// file's `.indexOf`/`.includes`/`.contains` receiver support, array-literal
// ITERATION — is now supported (see codegen_each_array_literal_test.go for
// the full differential coverage), unaffected by and independent of this
// file's own `.indexOf`/`.includes` support.
func TestCodegenArrayLiteralEachIterationNowSupported(t *testing.T) {
	src := "each opt in [\"a\", \"b\", \"c\"]\n  p=opt\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenArrayIndexOfBareArrayLiteralValueStillDeferred asserts a bare
// array literal used directly as a value (no `.indexOf`/`.includes`/
// `.contains` call at all) is still rejected — this file's fix to
// genValueExpr's array-literal guard only widens it to fall through for a
// TRAILING method call; a pure array-literal value remains out of scope.
func TestCodegenArrayIndexOfBareArrayLiteralValueStillDeferred(t *testing.T) {
	src := "p=[1, 2, 3]\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported array-literal error, got nil", src)
	}
}

// --- Scope / poison-guard reuse ---

// TestCodegenArrayIndexOfBoolLocalLeakCollisionDeferred proves the
// array-indexOf-derived bool local obeys the SAME leaked-name guard every
// other `- var` local does: declared inside an if-branch under a name that
// collides with a real struct field, referenced by a later sibling after the
// branch closes, must be rejected rather than silently resolved to the
// struct field.
func TestCodegenArrayIndexOfBoolLocalLeakCollisionDeferred(t *testing.T) {
	src := "if FlagB\n  - var Slug = [\"a\", \"b\"].indexOf(Slug) !== -1\np=Slug\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an error (a leaked array-indexOf bool local collides with a struct field), got nil", src)
	}
}

// --- Fault injection (non-vacuous differential proof) ---

// TestCodegenArrayIndexOfBoolLocalFaultInjection proves the differential
// tests above are actually exercising the generated code's output, not
// merely checking it built and ran: a deliberately WRONG expected value must
// fail the comparison.
func TestCodegenArrayIndexOfBoolLocalFaultInjection(t *testing.T) {
	src := "- var manageGroupActive = " + manageGroupArrayLiteral + ".indexOf(Slug) !== -1\np=manageGroupActive\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runGeneratedGo(t, generated, `opsData{Slug: "users"}`)
	wrongWant := "<p>false</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// --- White-box: genBoolExpr/isProvablyBoolOperand classify the new shapes correctly ---

// TestGenBoolExprArrayIndexOfClassification is a white-box proof of the exact
// classification boundary this file's differential tests exercise:
// genBoolExpr must return (ok=true, err=nil) for an array-literal
// `.indexOf(...) !== -1`/`=== -1` comparison and for a bare `.includes(...)`/
// `.contains(...)` call; (ok=false, err=nil) for a bare `.indexOf(...)` with
// no comparison at all (not this shape, falls through to
// genAssignRHS/genNumericExpr instead); and (ok=false, err!=nil) for a
// comparison genBoolExpr DOES recognize as this shape but can't compile — a
// non-string-literal array element — which must be a hard failure, not a
// silent fallthrough (genUnbufferedAssign propagates it as such).
func TestGenBoolExprArrayIndexOfClassification(t *testing.T) {
	cases := []struct {
		name    string
		expr    string
		wantOK  bool
		wantErr bool
	}{
		{name: "indexOf !== -1", expr: `["a", "b"].indexOf(Slug) !== -1`, wantOK: true},
		{name: "indexOf === -1", expr: `["a", "b"].indexOf(Slug) === -1`, wantOK: true},
		{name: "bare includes", expr: `["a", "b"].includes(Slug)`, wantOK: true},
		{name: "bare contains", expr: `["a", "b"].contains(Slug)`, wantOK: true},
		{name: "bare indexOf with no comparison is not bool-valued", expr: `["a", "b"].indexOf(Slug)`, wantOK: false},
		{name: "non-string-literal element is recognized but fails to compile", expr: `[1, 2].indexOf(Slug) !== -1`, wantOK: false, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &generator{rootType: opsDataReflectType}
			_, ok, err := g.genBoolExpr(tc.expr)
			if ok != tc.wantOK {
				t.Errorf("genBoolExpr(%q) ok = %v, want %v (err: %v)", tc.expr, ok, tc.wantOK, err)
			}
			if tc.wantErr && err == nil {
				t.Errorf("genBoolExpr(%q): expected a non-nil error, got nil", tc.expr)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("genBoolExpr(%q): expected a nil error, got %v", tc.expr, err)
			}
		})
	}
}

// TestIsProvablyBoolOperandArrayIncludes proves a bare array-literal
// `.includes(...)`/`.contains(...)` call is recognized as a provably-bool
// operand — the same recognition genBoolExpr's "||"/"&&" branches rely on —
// so it can itself be combined with another bool operand.
func TestIsProvablyBoolOperandArrayIncludes(t *testing.T) {
	g := &generator{rootType: opsDataReflectType}
	if !g.isProvablyBoolOperand(`["a", "b"].includes(Slug)`) {
		t.Errorf(`isProvablyBoolOperand(["a", "b"].includes(Slug)) = false, want true`)
	}
	if !g.isProvablyBoolOperand(`["a", "b"].contains(Slug)`) {
		t.Errorf(`isProvablyBoolOperand(["a", "b"].contains(Slug)) = false, want true`)
	}
}
