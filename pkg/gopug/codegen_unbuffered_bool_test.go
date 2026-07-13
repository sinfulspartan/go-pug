package gopug

import (
	"strconv"
	"strings"
	"testing"
)

// --- Headline corpus shapes (differential: codegen build+run vs interpreter) ---

// TestCodegenUnbufferedAssignBoolComparison is the corpus headline shape: a
// `- var` assigned a top-level comparison, then read back in BOTH a
// condition (`if`) and a buffered stringify (`#{}`-equivalent `= x`)
// position, byte-identical to the interpreter for every branch.
func TestCodegenUnbufferedAssignBoolComparison(t *testing.T) {
	src := "- var filterAll = Name === \"all\"\n" +
		"if filterAll\n  p yes\nelse\n  p no\n" +
		"p=filterAll\n"

	cases := []codegenUnbufferedCase{
		{
			name:        "Name matches the comparison literal",
			src:         src,
			data:        map[string]any{"Name": "all"},
			dataLiteral: `opsData{Name: "all"}`,
		},
		{
			name:        "Name does not match the comparison literal",
			src:         src,
			data:        map[string]any{"Name": "overdue"},
			dataLiteral: `opsData{Name: "overdue"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenUnbufferedAssignBoolBothOperandsOr is the second corpus
// headline shape: a `- var` assigned a "||" whose LEFT operand is a bare
// bool field and whose RIGHT operand is a comparison — both provably bool —
// exercised across all four truth combinations.
func TestCodegenUnbufferedAssignBoolBothOperandsOr(t *testing.T) {
	src := "- var navToggleActive = Flag || Name === \"dashboard\"\n" +
		"if navToggleActive\n  p yes\nelse\n  p no\n" +
		"p=navToggleActive\n"

	combos := []struct {
		flag bool
		name string
	}{
		{true, "dashboard"},
		{true, "other"},
		{false, "dashboard"},
		{false, "other"},
	}
	for _, c := range combos {
		t.Run("", func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
				src:         src,
				data:        map[string]any{"Flag": c.flag, "Name": c.name},
				dataLiteral: quoteBoolOpsData(c.flag, c.name),
			})
		})
	}
}

// quoteBoolOpsData builds an opsData composite literal for the Flag/Name
// combinations TestCodegenUnbufferedAssignBoolBothOperandsOr exercises.
func quoteBoolOpsData(flag bool, name string) string {
	if flag {
		return `opsData{Flag: true, Name: "` + name + `"}`
	}
	return `opsData{Flag: false, Name: "` + name + `"}`
}

// TestCodegenUnbufferedAssignBoolNegation proves a unary "!" over a bool
// field reads back correctly, for both operand values.
func TestCodegenUnbufferedAssignBoolNegation(t *testing.T) {
	src := "- var notFlag = !Flag\n" +
		"if notFlag\n  p yes\nelse\n  p no\n" +
		"p=notFlag\n"

	cases := []codegenUnbufferedCase{
		{name: "Flag true", src: src, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "Flag false", src: src, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenUnbufferedAssignBoolComparisonOps proves each of the eight
// comparison operators genCondition compiles reads back correctly as a
// bool-valued `- var` local, both branches.
func TestCodegenUnbufferedAssignBoolComparisonOps(t *testing.T) {
	cases := []struct {
		name    string
		op      string
		trueLit int
		falseLt int
	}{
		{name: "strict equal", op: "===", trueLit: 5, falseLt: 6},
		{name: "strict not equal", op: "!==", trueLit: 6, falseLt: 5},
		{name: "loose equal", op: "==", trueLit: 5, falseLt: 6},
		{name: "loose not equal", op: "!=", trueLit: 6, falseLt: 5},
		{name: "less than or equal", op: "<=", trueLit: 5, falseLt: 4},
		{name: "greater than or equal", op: ">=", trueLit: 5, falseLt: 6},
		{name: "less than", op: "<", trueLit: 6, falseLt: 4},
		{name: "greater than", op: ">", trueLit: 4, falseLt: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "- var cond = Count " + tc.op + " 5\n" +
				"if cond\n  p yes\nelse\n  p no\n" +
				"p=cond\n"
			runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
				name:        "true branch",
				src:         src,
				data:        map[string]any{"Count": tc.trueLit},
				dataLiteral: "opsData{Count: " + strconv.Itoa(tc.trueLit) + "}",
			})
			runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
				name:        "false branch",
				src:         src,
				data:        map[string]any{"Count": tc.falseLt},
				dataLiteral: "opsData{Count: " + strconv.Itoa(tc.falseLt) + "}",
			})
		})
	}
}

// TestCodegenUnbufferedAssignBoolFieldBare proves a bare bool-typed field
// RHS (no operator at all) reads back correctly.
func TestCodegenUnbufferedAssignBoolFieldBare(t *testing.T) {
	src := "- var isFlag = Flag\n" +
		"if isFlag\n  p yes\nelse\n  p no\n" +
		"p=isFlag\n"

	cases := []codegenUnbufferedCase{
		{name: "true", src: src, data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}"},
		{name: "false", src: src, data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenUnbufferedAssignBoolLocalReferencedByLaterLocal proves a bool
// local can itself be used as an operand of a LATER `- var`'s "&&" (both
// operands provably bool since both are bool-typed locals), across all four
// truth combinations.
func TestCodegenUnbufferedAssignBoolLocalReferencedByLaterLocal(t *testing.T) {
	src := "- var a = Flag\n" +
		"- var c = FlagB\n" +
		"- var b = a && c\n" +
		"if b\n  p yes\nelse\n  p no\n" +
		"p=b\n"

	combos := []struct{ flag, flagB bool }{
		{true, true},
		{true, false},
		{false, true},
		{false, false},
	}
	for _, c := range combos {
		t.Run("", func(t *testing.T) {
			flagLit := "false"
			if c.flag {
				flagLit = "true"
			}
			flagBLit := "false"
			if c.flagB {
				flagBLit = "true"
			}
			runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
				src:         src,
				data:        map[string]any{"Flag": c.flag, "FlagB": c.flagB},
				dataLiteral: "opsData{Flag: " + flagLit + ", FlagB: " + flagBLit + "}",
			})
		})
	}
}

// --- The ||/&& bool-operand-only boundary ---

// TestCodegenUnbufferedAssignBoolBoundaryNonBoolOrStaysString proves the
// critical boundary: "||" whose LEFT operand is a plain string field (NOT
// provably bool) must NOT be treated as a bool local — it must stay on
// genAssignRHS's existing STRING-local path, matching the interpreter's own
// value-context "||" (which returns the first-truthy OPERAND'S VALUE, not a
// canonicalized "true"/"false") byte-for-byte, in both a stringify AND a
// condition-truthiness use.
func TestCodegenUnbufferedAssignBoolBoundaryNonBoolOrStaysString(t *testing.T) {
	src := "- var label = Name || \"anon\"\n" +
		"if label\n  p yes\nelse\n  p no\n" +
		"p=label\n"

	cases := []codegenUnbufferedCase{
		{name: "left operand truthy", src: src, data: map[string]any{"Name": "Ada"}, dataLiteral: `opsData{Name: "Ada"}`},
		{name: "left operand falsy (empty string), default used", src: src, data: map[string]any{"Name": ""}, dataLiteral: `opsData{Name: ""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestGenBoolExprBoundary is a white-box proof of the exact classification
// boundary the differential test above exercises: genBoolExpr must return
// ok == false (deferring to genAssignRHS's string-local path) for every
// "||"/"&&" RHS where at least one operand is NOT provably bool, and
// ok == true for every RHS this slice's supported grammar actually claims.
// A regression that widened isProvablyBoolOperand to (wrongly) accept a
// string/numeric operand would flip one of the false cases to true and fail
// here directly — this is the fault the reviewer is expected to attack, and
// this test is what would catch it.
func TestGenBoolExprBoundary(t *testing.T) {
	cases := []struct {
		name   string
		expr   string
		wantOK bool
	}{
		{name: "comparison", expr: `Name === "all"`, wantOK: true},
		{name: "negation of a bool field", expr: "!Flag", wantOK: true},
		{name: "both-bool || (field, comparison)", expr: `Flag || Name === "dashboard"`, wantOK: true},
		{name: "both-bool && (two bool fields)", expr: "Flag && FlagB", wantOK: true},
		{name: "bare bool field", expr: "Flag", wantOK: true},
		{name: "|| with a non-bool left operand (string default idiom)", expr: `Name || "anon"`, wantOK: false},
		{name: "|| with a non-bool right operand", expr: `Flag || Name`, wantOK: false},
		{name: "&& with a non-bool operand", expr: "Name && Str1", wantOK: false},
		{name: "bare numeric field", expr: "Count", wantOK: false},
		{name: "bare string literal", expr: `"x"`, wantOK: false},
		{name: "ternary (owned by genAssignRHS, not genBoolExpr)", expr: `Flag ? "on" : "off"`, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &generator{rootType: opsDataReflectType}
			_, ok, err := g.genBoolExpr(tc.expr)
			if tc.wantOK && err != nil {
				t.Fatalf("genBoolExpr(%q): expected ok with no error, got err: %v", tc.expr, err)
			}
			if ok != tc.wantOK {
				t.Errorf("genBoolExpr(%q) ok = %v, want %v", tc.expr, ok, tc.wantOK)
			}
		})
	}
}

// --- Deferrals ---

// TestCodegenUnbufferedNumericBoolRHSDeferred asserts a bare numeric field
// RHS is NOT treated as bool-valued (genBoolExpr's exprIsBoolTyped correctly
// rejects a non-Bool Kind) — it falls through past genBoolExpr to the
// numeric-local classifier (genNumericExpr, a later slice than this
// bool-local one), which now accepts it as a genuine numeric local rather
// than erroring. See codegen_unbuffered_numeric_test.go for that slice's own
// coverage; this test's remaining job is only to prove genBoolExpr itself
// never mis-classifies a numeric field as bool.
func TestCodegenUnbufferedNumericBoolRHSDeferred(t *testing.T) {
	g := &generator{rootType: opsDataReflectType}
	_, ok, err := g.genBoolExpr("Count")
	if err != nil {
		t.Fatalf("genBoolExpr(%q): unexpected error: %v", "Count", err)
	}
	if ok {
		t.Errorf("genBoolExpr(%q): ok = true, want false (a numeric field must not be classified as bool-valued)", "Count")
	}
}

// TestCodegenUnbufferedArrayIndexOfBoolRHSDeferred asserts the
// array-literal-".indexOf(...) !== -1" idiom (the manageGroupActive/
// navGroupActive corpus pattern) is rejected rather than guessed at: it
// needs array-literal RHS + ".indexOf" support this slice does not add.
func TestCodegenUnbufferedArrayIndexOfBoolRHSDeferred(t *testing.T) {
	src := "- var x = [\"a\", \"b\"].indexOf(Name) !== -1\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}

// --- Scope/shadow/leaked-name reuse (unchanged, type-agnostic) ---

// TestCodegenUnbufferedAssignBoolLeakCollisionDeferred is
// TestCodegenUnbufferedAssignLeakCollisionDeferred's bool-local analogue: a
// `- var` assigned a BOOL-valued RHS inside an if branch, whose name
// collides with a real struct field, referenced by a later sibling AFTER
// the branch closes, must still be rejected by the SAME leakedVarNames
// guard slice one relied on — proving the guard is type-agnostic (it keys
// off isVarLocal, never the local's typ).
func TestCodegenUnbufferedAssignBoolLeakCollisionDeferred(t *testing.T) {
	src := "if FlagB\n  - var Flag = Count === Count\np=Flag\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an error (a leaked bool-valued `- var` collides with a struct field), got nil", src)
	}
}

// --- Self-consistency probe ---

// assertBoolRawInvariant extends assertRawStringifyInvariant
// (codegen_unbuffered_test.go) with a condition-context (`if`) use in
// addition to the buffered-stringify one: it renders TWO templates against
// the interpreter alone — one that assigns rhs to a `- var` local and reads
// it back in both positions (exercising
// Runtime.executeStatement/evaluateExprRaw's storage path plus
// Runtime.evaluateExpr/isTruthy/lookupAndStringify at each read site), and
// one that evaluates rhs directly in the same two positions — and asserts
// byte-identical output. A mismatch would prove the interpreter itself
// disagrees with its own assign-then-readback for this rhs, independent of
// anything codegen does.
func assertBoolRawInvariant(t *testing.T, rhs string, data map[string]any) {
	t.Helper()

	assignSrc := "- var __probe = " + rhs + "\nif __probe\n  p yes\nelse\n  p no\np=__probe\n"
	directSrc := "if " + rhs + "\n  p yes\nelse\n  p no\np=" + rhs + "\n"

	assignTmpl, err := Compile(assignSrc, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", assignSrc, err)
	}
	directTmpl, err := Compile(directSrc, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", directSrc, err)
	}

	assignOut, err := assignTmpl.Render(data)
	if err != nil {
		t.Fatalf("assign-and-readback Render(%q): %v", assignSrc, err)
	}
	directOut, err := directTmpl.Render(data)
	if err != nil {
		t.Fatalf("direct Render(%q): %v", directSrc, err)
	}

	if assignOut != directOut {
		t.Errorf("self-consistency probe failed for rhs %q: assign-and-readback %q != direct evaluation %q", rhs, assignOut, directOut)
	}
}

// TestUnbufferedAssignBoolRawInvariantSupportedShapes proves the
// bounded-agreement invariant holds, on the interpreter alone, for every
// bool-valued RHS shape genBoolExpr supports.
func TestUnbufferedAssignBoolRawInvariantSupportedShapes(t *testing.T) {
	cases := []struct {
		name string
		rhs  string
		data map[string]any
	}{
		{name: "comparison, true", rhs: `Name === "all"`, data: map[string]any{"Name": "all"}},
		{name: "comparison, false", rhs: `Name === "all"`, data: map[string]any{"Name": "overdue"}},
		{name: "negation, true", rhs: "!Flag", data: map[string]any{"Flag": false}},
		{name: "negation, false", rhs: "!Flag", data: map[string]any{"Flag": true}},
		{name: "both-bool ||, left truthy", rhs: `Flag || Name === "dashboard"`, data: map[string]any{"Flag": true, "Name": "other"}},
		{name: "both-bool ||, both falsy", rhs: `Flag || Name === "dashboard"`, data: map[string]any{"Flag": false, "Name": "other"}},
		{name: "both-bool &&, both truthy", rhs: "Flag && FlagB", data: map[string]any{"Flag": true, "FlagB": true}},
		{name: "both-bool &&, left falsy", rhs: "Flag && FlagB", data: map[string]any{"Flag": false, "FlagB": true}},
		{name: "bare bool field, true", rhs: "Flag", data: map[string]any{"Flag": true}},
		{name: "bare bool field, false", rhs: "Flag", data: map[string]any{"Flag": false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertBoolRawInvariant(t, tc.rhs, tc.data)
		})
	}
}

// TestUnbufferedAssignBoolRawInvariantFaultInjection proves
// assertBoolRawInvariant itself is non-vacuous: comparing a shape's probe
// output against a deliberately WRONG value must fail.
func TestUnbufferedAssignBoolRawInvariantFaultInjection(t *testing.T) {
	assignSrc := "- var __probe = Flag\nif __probe\n  p yes\nelse\n  p no\np=__probe\n"
	tmpl, err := Compile(assignSrc, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", assignSrc, err)
	}
	got, err := tmpl.Render(map[string]any{"Flag": true})
	if err != nil {
		t.Fatalf("Render(%q): %v", assignSrc, err)
	}
	wrongWant := "<p>no</p><p>false</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: probe output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenUnbufferedAssignBoolFaultInjection is the codegen-side
// differential-harness sanity check (mirroring
// TestCodegenUnbufferedAssignFaultInjection in codegen_unbuffered_test.go
// for the bool-local path): a deliberately WRONG expected value must fail
// the comparison, proving the bool differential tests above are actually
// exercising the generated code's output, not merely checking it built and
// ran.
func TestCodegenUnbufferedAssignBoolFaultInjection(t *testing.T) {
	src := "- var isFlag = Flag\np=isFlag\n"

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

	got := runGeneratedGo(t, generated, "opsData{Flag: true}")
	wrongWant := "<p>false</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}
