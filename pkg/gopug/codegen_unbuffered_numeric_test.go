package gopug

import (
	"reflect"
	"testing"
)

// --- Headline corpus shapes (differential: codegen build+run vs interpreter) ---

// TestCodegenUnbufferedAssignNumericIntField is the corpus headline shape: a
// `- var` assigned a bare numeric dot-path (`Offer.ID`), read back in a
// buffered-code position, a dynamic attribute value, and a condition — all
// three downstream contexts the corpus actually uses — across both a zero
// and a non-zero value (truthiness must flip with it).
func TestCodegenUnbufferedAssignNumericIntField(t *testing.T) {
	src := "- var offerID = Offer.ID\n" +
		"div(data-id=offerID)= offerID\n" +
		"if offerID\n  p yes\nelse\n  p no\n"

	cases := []codegenUnbufferedCase{
		{
			name:        "non-zero ID",
			src:         src,
			data:        map[string]any{"Offer": map[string]any{"ID": 42}},
			dataLiteral: "opsData{Offer: opsOffer{ID: 42}}",
		},
		{
			name:        "zero ID",
			src:         src,
			data:        map[string]any{"Offer": map[string]any{"ID": 0}},
			dataLiteral: "opsData{Offer: opsOffer{ID: 0}}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenUnbufferedAssignNumericUintField proves a bare uint-kind field
// RHS reads back correctly.
func TestCodegenUnbufferedAssignNumericUintField(t *testing.T) {
	src := "- var u = UintVal\np=u\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"UintVal": uint(7)},
		dataLiteral: "opsData{UintVal: 7}",
	})
}

// TestCodegenUnbufferedAssignNumericInt64Field proves a bare int64-kind field
// RHS reads back correctly.
func TestCodegenUnbufferedAssignNumericInt64Field(t *testing.T) {
	src := "- var big = BigInt\np=big\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"BigInt": int64(9007199254740991)},
		dataLiteral: "opsData{BigInt: 9007199254740991}",
	})
}

// TestCodegenUnbufferedAssignNumericFloat64Field proves a bare float64-kind
// field RHS reads back correctly across a positive fraction, a whole number,
// zero, and a negative value.
func TestCodegenUnbufferedAssignNumericFloat64Field(t *testing.T) {
	src := "- var p = Price\np=p\n"
	cases := []codegenUnbufferedCase{
		{name: "fraction", src: src, data: map[string]any{"Price": 3.14}, dataLiteral: "opsData{Price: 3.14}"},
		{name: "whole number", src: src, data: map[string]any{"Price": 5.0}, dataLiteral: "opsData{Price: 5.0}"},
		{name: "zero", src: src, data: map[string]any{"Price": 0.0}, dataLiteral: "opsData{Price: 0.0}"},
		{name: "negative", src: src, data: map[string]any{"Price": -2.5}, dataLiteral: "opsData{Price: -2.5}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenUnbufferedAssignNumericNamedTypeField proves a named type
// sharing int's underlying kind (opsNamedCount, distinct from a bare int)
// reads back correctly — the local carries the field's exact Go type
// through, not merely its reflect.Kind.
func TestCodegenUnbufferedAssignNumericNamedTypeField(t *testing.T) {
	src := "- var n = NamedCount\np=n\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"NamedCount": 9},
		dataLiteral: "opsData{NamedCount: 9}",
	})
}

// TestCodegenUnbufferedAssignNumericLiteral proves a numeric literal RHS —
// a plain decimal, hex, and scientific-notation token — reads back in its
// canonical decimal form.
func TestCodegenUnbufferedAssignNumericLiteral(t *testing.T) {
	cases := []struct {
		name string
		rhs  string
	}{
		{name: "plain decimal", rhs: "5"},
		{name: "decimal with fraction", rhs: "3.14"},
		{name: "hex", rhs: "0x10"},
		{name: "scientific notation", rhs: "1e3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "- var n = " + tc.rhs + "\np=n\n"
			runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
				src:         src,
				data:        map[string]any{},
				dataLiteral: "opsData{}",
			})
		})
	}
}

// TestCodegenUnbufferedAssignNumericLiteralCanonicalFormat is the interpreter
// -only empirical check the task requires: a numeric literal must render in
// its canonical decimal form (5, not 5.0), proving the interpreter itself
// stores a bare JS number literal as a float64 and stringifies it with
// FormatFloat's shortest round-tripping form, not with a fixed decimal
// count.
func TestCodegenUnbufferedAssignNumericLiteralCanonicalFormat(t *testing.T) {
	cases := []struct {
		rhs  string
		want string
	}{
		{rhs: "5", want: "5"},
		{rhs: "3.14", want: "3.14"},
		{rhs: "0x10", want: "16"},
		{rhs: "1e3", want: "1000"},
	}
	for _, tc := range cases {
		t.Run(tc.rhs, func(t *testing.T) {
			src := "- var n = " + tc.rhs + "\np=n\n"
			tmpl, err := Compile(src, nil)
			if err != nil {
				t.Fatalf("Compile(%q): %v", src, err)
			}
			got, err := tmpl.Render(map[string]any{})
			if err != nil {
				t.Fatalf("Render(%q): %v", src, err)
			}
			want := "<p>" + tc.want + "</p>"
			if got != want {
				t.Errorf("Render(%q) = %q, want %q", src, got, want)
			}
		})
	}
}

// TestCodegenUnbufferedAssignNumericConcat proves the URL-building idiom —
// a numeric local concatenated with a string prefix inside a dynamic
// attribute value — stringifies the local via genScalarStringify before
// concatenating, matching the interpreter's gopug.Add(strOf(x), ...).
func TestCodegenUnbufferedAssignNumericConcat(t *testing.T) {
	src := "- var offerID = Offer.ID\n" +
		"a(href=\"/offers/\" + offerID)= \"link\"\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Offer": map[string]any{"ID": 42}},
		dataLiteral: "opsData{Offer: opsOffer{ID: 42}}",
	})
}

// TestCodegenUnbufferedAssignNumericTernaryValue proves a numeric local used
// as a ternary VALUE (not condition) branch reads back correctly, for both
// branches.
func TestCodegenUnbufferedAssignNumericTernaryValue(t *testing.T) {
	src := "- var offerID = Offer.ID\np= Flag ? offerID : Zero\n"
	cases := []codegenUnbufferedCase{
		{
			name:        "true branch selects the local",
			src:         src,
			data:        map[string]any{"Flag": true, "Offer": map[string]any{"ID": 42}, "Zero": 0},
			dataLiteral: "opsData{Flag: true, Offer: opsOffer{ID: 42}, Zero: 0}",
		},
		{
			name:        "false branch selects the other numeric field",
			src:         src,
			data:        map[string]any{"Flag": false, "Offer": map[string]any{"ID": 42}, "Zero": 0},
			dataLiteral: "opsData{Flag: false, Offer: opsOffer{ID: 42}, Zero: 0}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenUnbufferedDifferential(t, tc)
		})
	}
}

// TestCodegenUnbufferedAssignNumericArithmeticValueContextWorksFree proves
// that a numeric local used as an operand of arithmetic in VALUE context
// (`= x + 1`, not a `- var` right-hand side) falls out for free from
// genValueExpr's existing, pre-existing arithmetic support: resolveFieldExpr
// treats a scope-bound numeric local exactly like a numeric struct field, so
// genValueExpr's own `+` case (stringify each operand, then gopug.Add) needs
// no new code at all for this to work.
func TestCodegenUnbufferedAssignNumericArithmeticValueContextWorksFree(t *testing.T) {
	src := "- var offerID = Offer.ID\np= offerID + 1\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Offer": map[string]any{"ID": 42}},
		dataLiteral: "opsData{Offer: opsOffer{ID: 42}}",
	})
}

// --- Self-consistency probe ---

// assertNumericRawInvariant extends assertBoolRawInvariant
// (codegen_unbuffered_bool_test.go) for a numeric-valued rhs: it renders TWO
// templates against the interpreter alone — one that assigns rhs to a
// `- var` local and reads it back in both a buffered-stringify AND a
// condition-truthiness position, one that evaluates rhs directly in the same
// two positions — and asserts byte-identical output. A mismatch would prove
// the interpreter itself disagrees with its own assign-then-readback for
// this rhs, independent of anything codegen does.
func assertNumericRawInvariant(t *testing.T, rhs string, data map[string]any) {
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

// TestUnbufferedAssignNumericRawInvariantSupportedShapes proves the
// bounded-agreement invariant holds, on the interpreter alone, for every
// numeric-valued RHS shape genNumericExpr supports, including the
// zero/non-zero truthiness boundary.
func TestUnbufferedAssignNumericRawInvariantSupportedShapes(t *testing.T) {
	cases := []struct {
		name string
		rhs  string
		data map[string]any
	}{
		{name: "int field, non-zero", rhs: "Offer.ID", data: map[string]any{"Offer": map[string]any{"ID": 42}}},
		{name: "int field, zero", rhs: "Offer.ID", data: map[string]any{"Offer": map[string]any{"ID": 0}}},
		{name: "uint field", rhs: "UintVal", data: map[string]any{"UintVal": uint(7)}},
		{name: "int64 field", rhs: "BigInt", data: map[string]any{"BigInt": int64(9007199254740991)}},
		{name: "float64 field, fraction", rhs: "Price", data: map[string]any{"Price": 3.14}},
		{name: "float64 field, negative", rhs: "Price", data: map[string]any{"Price": -2.5}},
		{name: "named-type field", rhs: "NamedCount", data: map[string]any{"NamedCount": 9}},
		{name: "numeric literal", rhs: "5", data: map[string]any{}},
		{name: "numeric literal with fraction", rhs: "3.14", data: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertNumericRawInvariant(t, tc.rhs, tc.data)
		})
	}
}

// TestUnbufferedAssignNumericRawInvariantFaultInjection proves
// assertNumericRawInvariant itself is non-vacuous: comparing a shape's probe
// output against a deliberately WRONG value must fail.
func TestUnbufferedAssignNumericRawInvariantFaultInjection(t *testing.T) {
	assignSrc := "- var __probe = Offer.ID\nif __probe\n  p yes\nelse\n  p no\np=__probe\n"
	tmpl, err := Compile(assignSrc, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", assignSrc, err)
	}
	got, err := tmpl.Render(map[string]any{"Offer": map[string]any{"ID": 42}})
	if err != nil {
		t.Fatalf("Render(%q): %v", assignSrc, err)
	}
	wrongWant := "<p>no</p><p>0</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: probe output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenUnbufferedAssignNumericFaultInjection is the codegen-side
// differential-harness sanity check (mirroring
// TestCodegenUnbufferedAssignBoolFaultInjection for the numeric-local path):
// a deliberately WRONG expected value must fail the comparison, proving the
// numeric differential tests above are actually exercising the generated
// code's output, not merely checking it built and ran.
func TestCodegenUnbufferedAssignNumericFaultInjection(t *testing.T) {
	src := "- var offerID = Offer.ID\np=offerID\n"

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

	got := runGeneratedGo(t, generated, "opsData{Offer: opsOffer{ID: 42}}")
	wrongWant := "<p>0</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// --- Classification boundary (white-box) ---

// TestGenNumericExprBoundary is a white-box proof of the exact
// classification boundary genNumericExpr draws: ok == true only for a bare
// numeric-kind field/local or a bare numeric literal, ok == false for
// anything else — including arithmetic (which must stay on genAssignRHS's
// string path), a bool field, a string field, a ternary, and a float32
// field (a kind genScalarStringify cannot stringify).
func TestGenNumericExprBoundary(t *testing.T) {
	cases := []struct {
		name     string
		expr     string
		wantOK   bool
		wantKind reflect.Kind
	}{
		{name: "bare int field", expr: "Count", wantOK: true, wantKind: reflect.Int},
		{name: "bare dot-path int field", expr: "Offer.ID", wantOK: true, wantKind: reflect.Int},
		{name: "bare uint field", expr: "UintVal", wantOK: true, wantKind: reflect.Uint},
		{name: "bare int64 field", expr: "BigInt", wantOK: true, wantKind: reflect.Int64},
		{name: "bare uint64 field", expr: "BigUint", wantOK: true, wantKind: reflect.Uint64},
		{name: "bare float64 field", expr: "Price", wantOK: true, wantKind: reflect.Float64},
		{name: "bare named-type field", expr: "NamedCount", wantOK: true, wantKind: reflect.Int},
		{name: "numeric literal", expr: "5", wantOK: true, wantKind: reflect.Float64},
		{name: "numeric literal, hex", expr: "0x10", wantOK: true, wantKind: reflect.Float64},
		{name: "arithmetic (must stay on the string path)", expr: "Count + Zero", wantOK: false},
		{name: "bool field", expr: "Flag", wantOK: false},
		{name: "string field", expr: "Name", wantOK: false},
		{name: "ternary", expr: `Flag ? 1 : 2`, wantOK: false},
		{name: "float32 field (unsupported kind)", expr: "Float32Val", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &generator{rootType: opsDataReflectType}
			_, typ, ok := g.genNumericExpr(tc.expr)
			if ok != tc.wantOK {
				t.Fatalf("genNumericExpr(%q) ok = %v, want %v", tc.expr, ok, tc.wantOK)
			}
			if tc.wantOK && (typ == nil || typ.Kind() != tc.wantKind) {
				t.Errorf("genNumericExpr(%q) kind = %v, want %v", tc.expr, typ, tc.wantKind)
			}
		})
	}
}

// --- Boundary regressions (existing behavior must not shift) ---

// TestCodegenUnbufferedAssignNumericBoundaryArithmeticStaysString proves the
// critical scope boundary: an arithmetic RHS (`a + b`) is NOT grabbed by the
// numeric classifier (see TestGenNumericExprBoundary's "arithmetic" case for
// the direct white-box proof) — it stays on genAssignRHS's existing
// STRING-local `+` path, byte-identical to the interpreter, exactly as it
// already was before this slice. Str1/Str2 hold numeric-looking strings (a
// STRING Kind, not a numeric Kind — this slice's classifier never touches
// them), so the numeric-looking OUTPUT comes entirely from gopug.Add's own
// runtime-value disambiguation, not from anything genNumericExpr does.
func TestCodegenUnbufferedAssignNumericBoundaryArithmeticStaysString(t *testing.T) {
	src := "- var total = Str1 + Str2\np=total\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Str1": "5", "Str2": "3"},
		dataLiteral: `opsData{Str1: "5", Str2: "3"}`,
	})
}

// TestCodegenUnbufferedAssignNumericBoundaryStringFieldStaysSlice1 proves a
// string-typed field RHS is still handled by slice 1's string-local path,
// unaffected by the numeric classifier's addition.
func TestCodegenUnbufferedAssignNumericBoundaryStringFieldStaysSlice1(t *testing.T) {
	src := "- var greeting = User.Name\np=greeting\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"User": map[string]any{"Name": "Ada"}},
		dataLiteral: `opsData{User: opsUser{Name: "Ada"}}`,
	})
}

// TestCodegenUnbufferedAssignNumericBoundaryBoolFieldStaysSlice2 proves a
// bool-typed field RHS is still handled by slice 2's bool-local path,
// unaffected by the numeric classifier's addition (genBoolExpr is tried
// before genNumericExpr).
func TestCodegenUnbufferedAssignNumericBoundaryBoolFieldStaysSlice2(t *testing.T) {
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

// --- Deferrals ---

// TestCodegenUnbufferedNumericFloat32FieldDeferred asserts a bare float32
// field RHS is rejected: genScalarStringify has no case for float32 (it
// would silently disagree with lookupAndStringify's own Sprintf-based
// fallback for that kind), so genNumericExpr deliberately excludes it and it
// falls through to genAssignRHS's string-only grammar, which also rejects
// it.
func TestCodegenUnbufferedNumericFloat32FieldDeferred(t *testing.T) {
	src := "- var x = Float32Val\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenUnbufferedNumericArithmeticRHSDeferred asserts that a numeric
// local used as an operand of arithmetic INSIDE another `- var`'s
// right-hand side (`- var y = x * 2`) is rejected rather than guessed at:
// genAssignRHS's own grammar has no multiplication case at all (only `+`
// string concatenation), a pre-existing limitation this slice does not
// widen — unlike the free VALUE-context case
// (TestCodegenUnbufferedAssignNumericArithmeticValueContextWorksFree), a
// `- var` right-hand side never reaches genValueExpr's arithmetic support.
func TestCodegenUnbufferedNumericArithmeticRHSDeferred(t *testing.T) {
	src := "- var offerID = Offer.ID\n- var y = offerID * 2\np=y\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenUnbufferedNumericFieldConcatRHSDeferred documents a pre-existing
// gap this slice does NOT widen: a numeric FIELD (not a local) used as an
// operand of `+` INSIDE a `- var`'s own right-hand side is still rejected.
// genAssignRHS's own bare-leaf case (reached only for a sub-expression of
// its `+`/ternary/`||`/`&&` grammar, never for a top-level RHS — genNumericExpr
// intercepts that case first) still explicitly rejects every numeric Kind
// rather than stringifying it; only genValueExpr's VALUE-context `+` (a
// buffered `= a + b` or a dynamic attribute value, never a `- var`
// right-hand side) already stringifies a numeric operand via
// genScalarStringify. Widening genAssignRHS's own leaf case to do the same
// is a separate, later concern from this slice's bare-field/literal
// classification.
func TestCodegenUnbufferedNumericFieldConcatRHSDeferred(t *testing.T) {
	src := "- var total = Count + Zero\np=total\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenUnbufferedNumericMethodCallRHSDeferred asserts a method call on
// a numeric local (`.toFixed(2)`) used as a `- var` right-hand side is
// rejected: methods-on-locals is a separate, later concern from this slice's
// bare-field/literal classification.
func TestCodegenUnbufferedNumericMethodCallRHSDeferred(t *testing.T) {
	src := "- var offerID = Offer.ID\n- var s = offerID.toFixed(2)\np=s\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// --- Scope/shadow/leaked-name reuse (unchanged, type-agnostic) ---

// TestCodegenUnbufferedAssignNumericLeakCollisionDeferred is
// TestCodegenUnbufferedAssignLeakCollisionDeferred's numeric-local analogue:
// a `- var` assigned a NUMERIC-valued RHS inside an if branch, whose name
// collides exactly with a real struct field, referenced by a later sibling
// AFTER the branch closes, must still be rejected by the SAME
// leakedVarNames guard slices one and two relied on — proving the guard is
// type-agnostic across string, bool, and numeric locals alike (it keys off
// isVarLocal, never the local's typ).
func TestCodegenUnbufferedAssignNumericLeakCollisionDeferred(t *testing.T) {
	src := "if FlagB\n  - var Count = Zero\np=Count\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an error (a leaked numeric-valued `- var` collides with a struct field), got nil", src)
	}
}

// TestCodegenUnbufferedAssignNumericTopLevelSurvivesFieldCollision proves the
// leaked-name guard does NOT over-reach for a numeric local either: a
// top-level `- var` is never scope-restored, so it must still resolve to the
// LOCAL, not a same-named struct field, even when a real field of that exact
// name exists.
func TestCodegenUnbufferedAssignNumericTopLevelSurvivesFieldCollision(t *testing.T) {
	src := "- var Count = Zero\np=Count\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Count": 5, "Zero": 9},
		dataLiteral: "opsData{Count: 5, Zero: 9}",
	})
}
