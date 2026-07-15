package gopug

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// csInner is csWrapper's pointer-to-struct field's pointee: a small struct
// with its own bool field, used both as the pointee of a nil-able pointer
// condition (csWrapper.Inner) and, once dereferenced, as a further bool
// leaf (csWrapper.Inner.ConfirmDialogs) — the exact two-hop shape the real
// motivating template pattern needs.
type csInner struct {
	ConfirmDialogs bool
}

// csUser is csData.CurrentUser's type — a plain VALUE struct (not a
// pointer), modeling the "guaranteed minimum" variant of the motivating
// `(!currentUser || currentUser.ConfirmDialogs) ? "true" : "false"` layout
// pattern, where currentUser never needs a pointer dereference at all.
type csUser struct {
	ConfirmDialogs bool
}

// csWrapper wraps a *csInner field one level down from the root, so a
// dot-path condition through it (csWrapper.Inner) is a genuine two-segment
// lookup, which is what makes the "!= nil" pointer rule provably safe (see
// genOperandTruthiness's doc comment): a bare, single-segment pointer
// identifier does not get the same rule, precisely because
// Runtime.lookup's root-segment resolution never calls getField.
type csWrapper struct {
	Inner *csInner
}

// csStringer implements fmt.Stringer with a value receiver whose String()
// always returns the empty string — deliberately chosen to land in
// isTruthy's falsy set, so a codegen rule that assumed "struct in condition
// position is always truthy" would silently disagree with the interpreter
// for this exact type. genOperandTruthiness's Struct case must defer for
// it rather than emit a bare "true".
type csStringer struct {
	X int
}

// String always returns the empty string, deliberately landing in
// isTruthy's falsy set.
func (s csStringer) String() string { return "" }

// csErrorer implements error with a value receiver whose Error() always
// returns the literal string "false" — another isTruthy falsy-set member,
// reached through fmt.Sprintf's error-interface honoring rather than
// fmt.Stringer's.
type csErrorer struct{}

// Error always returns the string "false", another isTruthy falsy-set member.
func (e csErrorer) Error() string { return "false" }

// csData is the root struct codegen_condition_struct_test.go's differential
// cases resolve condition operands against. CurrentUser is the guaranteed-
// safe value-struct case; Wrapper carries the pointer-to-struct case one
// level down so it's reached via a genuine dot-path; StrField/ErrField
// exercise the Stringer/error deferral; NamePtr is a pointer-to-scalar,
// which must defer for a different reason (content-dependent, not a nil
// check); Active/ActiveB/Count/Name are the pre-existing bool/numeric/
// string leaves, used to prove those cases stay byte-for-byte unchanged.
type csData struct {
	CurrentUser csUser
	Wrapper     csWrapper
	StrField    csStringer
	ErrField    csErrorer
	NamePtr     *string
	Active      bool
	ActiveB     bool
	Count       int
	Name        string
}

var csDataReflectType = reflect.TypeOf(csData{})

// csDataWithBarePtr is a second, separate root type used only by the
// bare-pointer deferral tests: it has NO dot-path prefix in front of its
// pointer field, unlike csData.Wrapper.Inner (always reached through
// Wrapper first) — CurrentUserP is itself the entire, single-segment
// condition operand, the shape genOperandTruthiness's Ptr case must defer
// for. It is only ever passed to GenerateGo (never rendered through
// runDifferentialBatch), so it has no matching structSrc counterpart.
type csDataWithBarePtr struct {
	CurrentUserP *csUser
}

// csDataStructSrc is csData's (and its field types') declarations, reused
// verbatim by the differential harness to assemble a standalone,
// compilable Go source file around a GenerateGo result — it must match the
// Go struct/method declarations above field for field and method for
// method.
const csDataStructSrc = `type csInner struct {
	ConfirmDialogs bool
}

type csUser struct {
	ConfirmDialogs bool
}

type csWrapper struct {
	Inner *csInner
}

type csStringer struct {
	X int
}

func (s csStringer) String() string { return "" }

type csErrorer struct{}

func (e csErrorer) Error() string { return "false" }

type csData struct {
	CurrentUser csUser
	Wrapper     csWrapper
	StrField    csStringer
	ErrField    csErrorer
	NamePtr     *string
	Active      bool
	ActiveB     bool
	Count       int
	Name        string
}
`

// csConditionCase is one runCsConditionDifferentialBatch case: it builds
// "if <cond>\n  p yes\nelse\n  p no\n" (or, when unless is set, the "unless"
// keyword instead of "if"), compared through both the interpreter
// (Compile/Template.Render, against data) and the codegen backend
// (GenerateGo/runDifferentialBatch, against dataLiteral — a csData
// composite literal describing the same data).
type csConditionCase struct {
	name        string
	cond        string
	unless      bool
	data        map[string]any
	dataLiteral string
}

// csConditionResult is one csConditionCase's outcome.
type csConditionResult struct {
	out  string
	want string
	err  string
}

// runCsConditionDifferentialBatch is the csData analogue of
// runConditionDifferentialBatch (codegen_condition_logic_test.go), reused
// here rather than that opsData-rooted helper because this file's cases
// need struct/pointer/Stringer/error fields opsData doesn't declare.
func runCsConditionDifferentialBatch(t *testing.T, cases []csConditionCase) []csConditionResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	type prepared struct {
		c    csConditionCase
		want string
	}

	var diffCases []diffCase
	var prep []prepared

	for _, c := range cases {
		keyword := "if"
		if c.unless {
			keyword = "unless"
		}
		src := keyword + " " + c.cond + "\n  p yes\nelse\n  p no\n"

		ast, err := Parse(src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		generated, err := GenerateGo(ast, Config{
			PackageName:     "main",
			FuncName:        "RenderCs",
			DataType:        "csData",
			DataReflectType: csDataReflectType,
		})
		if err != nil {
			t.Fatalf("GenerateGo(%q): expected no error, got: %v", src, err)
		}

		tmpl, err := Compile(src, nil)
		if err != nil {
			t.Fatalf("Compile(%q): %v", src, err)
		}
		want, err := tmpl.Render(c.data)
		if err != nil {
			t.Fatalf("interpreter Render(%q) with data %v: %v", src, c.data, err)
		}

		diffCases = append(diffCases, diffCase{name: c.name, generated: generated, dataLiteral: c.dataLiteral})
		prep = append(prep, prepared{c: c, want: want})
	}

	batchResults := runDifferentialBatch(t, csDataStructSrc, "RenderCs", diffCases)

	results := make([]csConditionResult, len(cases))
	for i, p := range prep {
		r := batchResults[i]
		results[i] = csConditionResult{out: r.Out, want: p.want, err: r.Err}
	}
	return results
}

// assertCsConditionDiffResult asserts a csConditionResult renders without
// error and matches the interpreter oracle exactly.
func assertCsConditionDiffResult(t *testing.T, cond, dataLiteral string, r csConditionResult) string {
	t.Helper()
	if r.err != "" {
		t.Fatalf("generated RenderCs: unexpected error %q for condition %q (data literal %s)", r.err, cond, dataLiteral)
	}
	if r.out != r.want {
		t.Errorf("codegen output %q does not match interpreter output %q for condition %q with data literal %s", r.out, r.want, cond, dataLiteral)
	}
	return r.out
}

// csBoolBranch is the rendered "p yes"/"p no" output a csConditionCase's
// template produces for a true/false condition.
func csBoolBranch(b bool) string {
	if b {
		return "<p>yes</p>"
	}
	return "<p>no</p>"
}

// TestCodegenConditionStructValueTruthy proves genOperandTruthiness's new
// Struct case: a plain VALUE struct field is always
// truthy — the interpreter's getField returns the struct value unchanged,
// and stringifying it falls through to fmt.Sprintf's "{...}"-shaped,
// never-empty, never-falsy-set default. "if CurrentUser" always renders the
// consequent; "unless CurrentUser" always renders the (skipped-consequent)
// alternate, regardless of the struct's own field values.
func TestCodegenConditionStructValueTruthy(t *testing.T) {
	t.Parallel()
	cases := []csConditionCase{
		{
			name:        "if a value struct field, ConfirmDialogs false",
			cond:        "CurrentUser",
			data:        map[string]any{"CurrentUser": csUser{ConfirmDialogs: false}},
			dataLiteral: "csData{CurrentUser: csUser{ConfirmDialogs: false}}",
		},
		{
			name:        "if a value struct field, ConfirmDialogs true",
			cond:        "CurrentUser",
			data:        map[string]any{"CurrentUser": csUser{ConfirmDialogs: true}},
			dataLiteral: "csData{CurrentUser: csUser{ConfirmDialogs: true}}",
		},
		{
			name:        "unless a value struct field, ConfirmDialogs false",
			cond:        "CurrentUser",
			unless:      true,
			data:        map[string]any{"CurrentUser": csUser{ConfirmDialogs: false}},
			dataLiteral: "csData{CurrentUser: csUser{ConfirmDialogs: false}}",
		},
	}
	results := runCsConditionDifferentialBatch(t, cases)
	wants := []bool{true, true, false}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertCsConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			if want := csBoolBranch(wants[i]); got != want {
				t.Errorf("condition %q (unless=%v): got %q, want %q", tc.cond, tc.unless, got, want)
			}
		})
	}
}

// TestCodegenConditionPointerNilNonNil proves genOperandTruthiness's new
// Ptr case: a pointer-to-struct field reached through a
// dot-path (Wrapper.Inner is a two-segment lookup, not a bare identifier)
// is nil-falsy, non-nil-truthy — both plain and negated.
func TestCodegenConditionPointerNilNonNil(t *testing.T) {
	t.Parallel()
	cases := []csConditionCase{
		{
			name:        "if Wrapper.Inner, nil",
			cond:        "Wrapper.Inner",
			data:        map[string]any{"Wrapper": csWrapper{}},
			dataLiteral: "csData{Wrapper: csWrapper{}}",
		},
		{
			name:        "if Wrapper.Inner, non-nil",
			cond:        "Wrapper.Inner",
			data:        map[string]any{"Wrapper": csWrapper{Inner: &csInner{}}},
			dataLiteral: "csData{Wrapper: csWrapper{Inner: &csInner{}}}",
		},
		{
			name:        "if !Wrapper.Inner, nil",
			cond:        "!Wrapper.Inner",
			data:        map[string]any{"Wrapper": csWrapper{}},
			dataLiteral: "csData{Wrapper: csWrapper{}}",
		},
		{
			name:        "if !Wrapper.Inner, non-nil",
			cond:        "!Wrapper.Inner",
			data:        map[string]any{"Wrapper": csWrapper{Inner: &csInner{}}},
			dataLiteral: "csData{Wrapper: csWrapper{Inner: &csInner{}}}",
		},
	}
	results := runCsConditionDifferentialBatch(t, cases)
	wants := []bool{false, true, true, false}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertCsConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			if want := csBoolBranch(wants[i]); got != want {
				t.Errorf("condition %q: got %q, want %q", tc.cond, got, want)
			}
		})
	}
}

// TestCodegenConditionStructLogicalCompounds proves that a
// struct/pointer operand composes correctly with `||`/`&&` alongside a bool
// field, through genCondition's pre-existing, unmodified recursion.
func TestCodegenConditionStructLogicalCompounds(t *testing.T) {
	t.Parallel()
	cases := []csConditionCase{
		{
			name:        "CurrentUser && Active, Active true",
			cond:        "CurrentUser && Active",
			data:        map[string]any{"CurrentUser": csUser{}, "Active": true},
			dataLiteral: "csData{CurrentUser: csUser{}, Active: true}",
		},
		{
			name:        "CurrentUser && Active, Active false",
			cond:        "CurrentUser && Active",
			data:        map[string]any{"CurrentUser": csUser{}, "Active": false},
			dataLiteral: "csData{CurrentUser: csUser{}, Active: false}",
		},
		{
			name:        "Wrapper.Inner || Active, nil pointer, Active true",
			cond:        "Wrapper.Inner || Active",
			data:        map[string]any{"Wrapper": csWrapper{}, "Active": true},
			dataLiteral: "csData{Wrapper: csWrapper{}, Active: true}",
		},
		{
			name:        "Wrapper.Inner || Active, nil pointer, Active false",
			cond:        "Wrapper.Inner || Active",
			data:        map[string]any{"Wrapper": csWrapper{}, "Active": false},
			dataLiteral: "csData{Wrapper: csWrapper{}, Active: false}",
		},
		{
			name:        "Wrapper.Inner || Active, non-nil pointer, Active false",
			cond:        "Wrapper.Inner || Active",
			data:        map[string]any{"Wrapper": csWrapper{Inner: &csInner{}}, "Active": false},
			dataLiteral: "csData{Wrapper: csWrapper{Inner: &csInner{}}, Active: false}",
		},
	}
	results := runCsConditionDifferentialBatch(t, cases)
	wants := []bool{true, false, true, false, true}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertCsConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			if want := csBoolBranch(wants[i]); got != want {
				t.Errorf("condition %q: got %q, want %q", tc.cond, got, want)
			}
		})
	}
}

// TestCodegenConditionStructRegressionScalarLeaves proves that
// the pre-existing bool/numeric/string condition leaves are unaffected by
// the new Struct/Ptr cases sharing the same switch, exercised here against
// csData specifically (rather than only opsData elsewhere in the suite) so
// this file's own struct/pointer additions are proven not to have disturbed
// the switch's other, untouched arms.
func TestCodegenConditionStructRegressionScalarLeaves(t *testing.T) {
	t.Parallel()
	cases := []csConditionCase{
		{name: "bool field true", cond: "Active", data: map[string]any{"Active": true}, dataLiteral: "csData{Active: true}"},
		{name: "bool field false", cond: "Active", data: map[string]any{"Active": false}, dataLiteral: "csData{Active: false}"},
		{name: "numeric field non-zero", cond: "Count", data: map[string]any{"Count": 5}, dataLiteral: "csData{Count: 5}"},
		{name: "numeric field zero", cond: "Count", data: map[string]any{"Count": 0}, dataLiteral: "csData{Count: 0}"},
		{name: "string field non-empty", cond: "Name", data: map[string]any{"Name": "x"}, dataLiteral: `csData{Name: "x"}`},
		{name: "string field empty", cond: "Name", data: map[string]any{"Name": ""}, dataLiteral: `csData{Name: ""}`},
		{name: "string field \"false\"", cond: "Name", data: map[string]any{"Name": "false"}, dataLiteral: `csData{Name: "false"}`},
	}
	results := runCsConditionDifferentialBatch(t, cases)
	wants := []bool{true, false, true, false, true, false, false}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertCsConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			if want := csBoolBranch(wants[i]); got != want {
				t.Errorf("condition %q: got %q, want %q", tc.cond, got, want)
			}
		})
	}
}

// csTernaryCase is one runCsTernaryDifferentialBatch case: a value-context
// ternary `- var x = <cond> ? "true" : "false"` followed by `p= x`,
// compared through both GenerateGo/runDifferentialBatch and the
// interpreter's own Compile().Render, against the same data.
type csTernaryCase struct {
	name        string
	cond        string
	data        map[string]any
	dataLiteral string
}

// runCsTernaryDifferentialBatch is the csData analogue of
// runCodegenTernaryDifferentialBatch (codegen_ternary_test.go), reused here
// for the same reason as runCsConditionDifferentialBatch: this file's cases
// need csData's struct/pointer fields.
func runCsTernaryDifferentialBatch(t *testing.T, cases []csTernaryCase) []csConditionResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	type prepared struct {
		c    csTernaryCase
		want string
	}

	var diffCases []diffCase
	var prep []prepared

	for _, tc := range cases {
		src := fmt.Sprintf("- var x = (%s) ? \"true\" : \"false\"\np= x\n", tc.cond)

		ast, err := Parse(src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		generated, err := GenerateGo(ast, Config{
			PackageName:     "main",
			FuncName:        "RenderCs",
			DataType:        "csData",
			DataReflectType: csDataReflectType,
		})
		if err != nil {
			t.Fatalf("GenerateGo(%q): expected no error, got: %v", src, err)
		}

		tmpl, err := Compile(src, nil)
		if err != nil {
			t.Fatalf("Compile(%q): %v", src, err)
		}
		want, err := tmpl.Render(tc.data)
		if err != nil {
			t.Fatalf("interpreter Render(%q) with data %v: %v", src, tc.data, err)
		}

		diffCases = append(diffCases, diffCase{name: tc.name, generated: generated, dataLiteral: tc.dataLiteral})
		prep = append(prep, prepared{c: tc, want: want})
	}

	batchResults := runDifferentialBatch(t, csDataStructSrc, "RenderCs", diffCases)

	results := make([]csConditionResult, len(cases))
	for i, p := range prep {
		r := batchResults[i]
		results[i] = csConditionResult{out: r.Out, want: p.want, err: r.Err}
	}
	return results
}

// TestCodegenConditionStructMoneyCaseValue proves the
// guaranteed-minimum variant: the real-world layout pattern
// `(!currentUser || currentUser.ConfirmDialogs) ? "true" : "false"` with
// currentUser modeled as a plain VALUE struct field, rendering
// byte-identically to the interpreter for both a false and a true
// ConfirmDialogs. Because CurrentUser never needs a pointer dereference,
// this variant needs nothing from the new Ptr case at all — only the new
// Struct case, applied to CurrentUser's own bare-identifier truthiness in
// "!CurrentUser".
func TestCodegenConditionStructMoneyCaseValue(t *testing.T) {
	t.Parallel()
	cases := []csTernaryCase{
		{
			name:        "ConfirmDialogs false",
			cond:        "!CurrentUser || CurrentUser.ConfirmDialogs",
			data:        map[string]any{"CurrentUser": csUser{ConfirmDialogs: false}},
			dataLiteral: "csData{CurrentUser: csUser{ConfirmDialogs: false}}",
		},
		{
			name:        "ConfirmDialogs true",
			cond:        "!CurrentUser || CurrentUser.ConfirmDialogs",
			data:        map[string]any{"CurrentUser": csUser{ConfirmDialogs: true}},
			dataLiteral: "csData{CurrentUser: csUser{ConfirmDialogs: true}}",
		},
	}
	results := runCsTernaryDifferentialBatch(t, cases)
	wants := []string{"false", "true"}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertCsConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			want := "<p>" + wants[i] + "</p>"
			if got != want {
				t.Errorf("money-case ternary %q: got %q, want %q", tc.cond, got, want)
			}
		})
	}
}

// TestCodegenConditionStructMoneyCasePointer proves the pointer-to-struct
// variant of the same layout pattern, modeled one level down (Wrapper.Inner
// standing in for currentUser, ConfirmDialogs the same bool field on the
// pointee) so that BOTH the bare truthiness operand ("!Wrapper.Inner") and
// the dereferencing operand ("Wrapper.Inner.ConfirmDialogs") are reached
// through a genuine two-segment dot-path — the shape the new Ptr rule
// proves safe — rather than through a bare single-segment identifier, which
// the new Ptr rule deliberately does NOT cover (see
// TestCodegenConditionStructBarePointerDefers). All three states — nil,
// non-nil/false, non-nil/true — render byte-identically to the
// interpreter.
func TestCodegenConditionStructMoneyCasePointer(t *testing.T) {
	t.Parallel()
	cases := []csTernaryCase{
		{
			name:        "nil pointer",
			cond:        "!Wrapper.Inner || Wrapper.Inner.ConfirmDialogs",
			data:        map[string]any{"Wrapper": csWrapper{}},
			dataLiteral: "csData{Wrapper: csWrapper{}}",
		},
		{
			name:        "non-nil, ConfirmDialogs false",
			cond:        "!Wrapper.Inner || Wrapper.Inner.ConfirmDialogs",
			data:        map[string]any{"Wrapper": csWrapper{Inner: &csInner{ConfirmDialogs: false}}},
			dataLiteral: "csData{Wrapper: csWrapper{Inner: &csInner{ConfirmDialogs: false}}}",
		},
		{
			name:        "non-nil, ConfirmDialogs true",
			cond:        "!Wrapper.Inner || Wrapper.Inner.ConfirmDialogs",
			data:        map[string]any{"Wrapper": csWrapper{Inner: &csInner{ConfirmDialogs: true}}},
			dataLiteral: "csData{Wrapper: csWrapper{Inner: &csInner{ConfirmDialogs: true}}}",
		},
	}
	results := runCsTernaryDifferentialBatch(t, cases)
	wants := []string{"true", "false", "true"}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertCsConditionDiffResult(t, tc.cond, tc.dataLiteral, results[i])
			want := "<p>" + wants[i] + "</p>"
			if got != want {
				t.Errorf("money-case ternary %q: got %q, want %q", tc.cond, got, want)
			}
		})
	}
}

// TestCodegenConditionStructStringerDefers proves that a struct
// type implementing fmt.Stringer (csStringer, whose String() always returns
// the falsy empty string) is NOT admitted as "always truthy" — genOperandTruthiness's
// Struct case defers rather than emitting a bare "true" that would disagree
// with the interpreter every time this exact type is used, since its
// runtime String() output could be any string, including a falsy one.
func TestCodegenConditionStructStringerDefers(t *testing.T) {
	src := "if StrField\n  p yes\nelse\n  p no\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderCs",
		DataType:        "csData",
		DataReflectType: csDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error for a Stringer-implementing struct condition, got nil", src)
	}
	if !strings.Contains(err.Error(), "Stringer") {
		t.Errorf("GenerateGo(%q): error %q does not mention the Stringer deferral reason", src, err.Error())
	}
}

// TestCodegenConditionStructErrorDefers is TestCodegenConditionStructStringerDefers's
// error-interface sibling: csErrorer implements error (not fmt.Stringer),
// exercised separately because lookupAndStringify's Sprintf fallback honors
// error via a DIFFERENT interface satisfaction check than Stringer, and
// implementsStringerOrError must catch both.
func TestCodegenConditionStructErrorDefers(t *testing.T) {
	src := "if ErrField\n  p yes\nelse\n  p no\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderCs",
		DataType:        "csData",
		DataReflectType: csDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error for an error-implementing struct condition, got nil", src)
	}
	if !strings.Contains(err.Error(), "Stringer") {
		t.Errorf("GenerateGo(%q): error %q does not mention the Stringer/error deferral reason", src, err.Error())
	}
}

// TestCodegenConditionPointerToScalarDefers proves that a
// pointer to a SCALAR type (here *string) is not given the struct-pointer
// "!= nil" treatment — getField dereferences it to the pointed-to scalar,
// whose truthiness is content-dependent (an empty or "false"/"0"-valued
// *string is falsy), not a nil check, so genOperandTruthiness must defer
// rather than emit a nil comparison that would disagree with the
// interpreter whenever the pointee holds a falsy string.
func TestCodegenConditionPointerToScalarDefers(t *testing.T) {
	src := "if NamePtr\n  p yes\nelse\n  p no\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderCs",
		DataType:        "csData",
		DataReflectType: csDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error for a pointer-to-scalar condition, got nil", src)
	}
}

// TestCodegenConditionStructBarePointerDefers proves the safety boundary
// this slice draws around the Ptr case: a BARE (single-segment, no
// dot-path) pointer-to-struct identifier defers, even though its
// reflect.Kind is identical to the dot-path-reached case
// TestCodegenConditionPointerNilNonNil proves safe. This is load-bearing,
// not overcautious — Runtime.lookup resolves the FIRST segment of any
// lookup key directly off the scope stack / data map, calling getField only
// for a SECOND-OR-LATER segment, so a bare *T value (nil or not) reaches
// lookupAndStringify's Sprintf("%v", ...) fallback unprocessed and
// stringifies to a non-empty, never-falsy string ("<nil>" for nil,
// "&{...}" for non-nil) regardless of nilness — making a bare pointer
// condition unconditionally truthy in the interpreter, not a nil check.
func TestCodegenConditionStructBarePointerDefers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{name: "bare pointer field alone", src: "if CurrentUserP\n  p yes\n"},
		{name: "negated bare pointer field alone", src: "if !CurrentUserP\n  p yes\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderCs",
				DataType:        "csDataWithBarePtr",
				DataReflectType: reflect.TypeOf(csDataWithBarePtr{}),
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a deferral error for a bare pointer-to-struct condition, got nil", tc.src)
			}
		})
	}
}

// TestCodegenConditionStructBarePointerMoneyCaseDefers proves the
// composed-with-dot-path-continuation shape of the money case with a
// GENUINELY bare (root-level, not nested) pointer variable still defers as
// a whole: "!currentUser || currentUser.ConfirmDialogs" with currentUser
// itself a bare *csUser field is rejected outright — since genCondition
// compiles the "!currentUser" operand independently and it alone must
// defer (see TestCodegenConditionStructBarePointerDefers), the whole
// expression's GenerateGo call propagates that error rather than silently
// emitting an unsound partial translation. This is deliberate, not a
// residual gap this slice tries to paper over: composing a provably-correct
// nil check for the bare operand is not possible without either disagreeing
// with the interpreter on a nil bare-pointer condition's own truthiness, or
// letting Go evaluate an unguarded nil-pointer field dereference the
// interpreter itself would have short-circuited away from safely.
func TestCodegenConditionStructBarePointerMoneyCaseDefers(t *testing.T) {
	src := "if !CurrentUserP || CurrentUserP.ConfirmDialogs\n  p yes\nelse\n  p no\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}

	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderCs",
		DataType:        "csDataWithBarePtr",
		DataReflectType: reflect.TypeOf(csDataWithBarePtr{}),
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected the composed bare-pointer money-case shape to defer, got nil", src)
	}
}

// TestCodegenConditionStructFaultInjectionPointerNilFalse asserts the
// fault-injection the task's own review checklist calls for: an expectation
// that treats a NIL *csInner condition as TRUTHY must fail against the real
// codegen/interpreter agreement — proving the differential in
// TestCodegenConditionPointerNilNonNil actually pins nil-to-falsy rather
// than vacuously passing regardless of the branch taken.
func TestCodegenConditionStructFaultInjectionPointerNilFalse(t *testing.T) {
	results := runCsConditionDifferentialBatch(t, []csConditionCase{
		{
			name:        "nil pointer",
			cond:        "Wrapper.Inner",
			data:        map[string]any{"Wrapper": csWrapper{}},
			dataLiteral: "csData{Wrapper: csWrapper{}}",
		},
	})
	got := assertCsConditionDiffResult(t, "Wrapper.Inner", "csData{Wrapper: csWrapper{}}", results[0])
	wrongExpectedTruthy := csBoolBranch(true)
	if got == wrongExpectedTruthy {
		t.Fatalf("fault injection failed to catch a wrong expectation: a nil *csInner condition rendered %q, matching the WRONG (treat-nil-as-truthy) expectation instead of falsy", got)
	}
}

// TestCodegenConditionStructFaultInjectionPointerNonNilTrue is
// TestCodegenConditionStructFaultInjectionPointerNilFalse's non-nil
// sibling: an expectation that treats a NON-NIL *csInner condition as FALSY
// must fail.
func TestCodegenConditionStructFaultInjectionPointerNonNilTrue(t *testing.T) {
	results := runCsConditionDifferentialBatch(t, []csConditionCase{
		{
			name:        "non-nil pointer",
			cond:        "Wrapper.Inner",
			data:        map[string]any{"Wrapper": csWrapper{Inner: &csInner{}}},
			dataLiteral: "csData{Wrapper: csWrapper{Inner: &csInner{}}}",
		},
	})
	got := assertCsConditionDiffResult(t, "Wrapper.Inner", "csData{Wrapper: csWrapper{Inner: &csInner{}}}", results[0])
	wrongExpectedFalsy := csBoolBranch(false)
	if got == wrongExpectedFalsy {
		t.Fatalf("fault injection failed to catch a wrong expectation: a non-nil *csInner condition rendered %q, matching the WRONG (treat-non-nil-as-falsy) expectation instead of truthy", got)
	}
}

// TestCodegenConditionStructFaultInjectionValueStructFalsy asserts the
// task's third required fault injection: an expectation that treats a
// VALUE struct's "!CurrentUser" as true (i.e. CurrentUser itself as falsy)
// must fail — proving the money-case value-struct differential
// (TestCodegenConditionStructMoneyCaseValue) actually pins
// value-struct-is-always-truthy rather than vacuously agreeing by
// coincidence.
func TestCodegenConditionStructFaultInjectionValueStructFalsy(t *testing.T) {
	results := runCsTernaryDifferentialBatch(t, []csTernaryCase{
		{
			name:        "ConfirmDialogs false",
			cond:        "!CurrentUser || CurrentUser.ConfirmDialogs",
			data:        map[string]any{"CurrentUser": csUser{ConfirmDialogs: false}},
			dataLiteral: "csData{CurrentUser: csUser{ConfirmDialogs: false}}",
		},
	})
	got := assertCsConditionDiffResult(t, "!CurrentUser || CurrentUser.ConfirmDialogs", "csData{CurrentUser: csUser{ConfirmDialogs: false}}", results[0])
	wrongExpectedTrue := "<p>true</p>"
	if got == wrongExpectedTrue {
		t.Fatalf(`fault injection failed to catch a wrong expectation: rendered %q, matching the WRONG (treat-value-struct-as-falsy, so !CurrentUser is true) expectation instead of "false"`, got)
	}
}

// TestCodegenConditionStructFaultInjectionStringerNotAlwaysTruthy re-asserts
// the task's fourth required fault injection in its own dedicated test: the
// Stringer-implementing struct case MUST defer at generate time — a
// generator that instead accepted it and blindly emitted "true" would
// silently disagree with the interpreter (whose isTruthy(csStringer{}.String())
// is false, since String() always returns the empty string).
func TestCodegenConditionStructFaultInjectionStringerNotAlwaysTruthy(t *testing.T) {
	src := "if StrField\n  p yes\nelse\n  p no\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, genErr := GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderCs",
		DataType:        "csData",
		DataReflectType: csDataReflectType,
	})
	if genErr == nil {
		t.Fatalf("GenerateGo(%q): a Stringer-implementing struct condition must defer (return an error) rather than blindly emit \"true\", since the interpreter's own isTruthy(csStringer{}.String()) is false for this type", src)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"StrField": csStringer{}})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if want != csBoolBranch(false) {
		t.Fatalf("test sanity check failed: interpreter Render(%q) with an empty-String() csStringer = %q, want %q (a generator that emitted a bare \"true\" for this type would disagree with this exact interpreter output)", src, want, csBoolBranch(false))
	}
}
