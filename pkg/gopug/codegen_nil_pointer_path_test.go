package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// npProfile is npUser.ProfilePtr's pointee: a small struct used only to give
// npUser a POINTER-typed LEAF field (as opposed to a pointer INTERMEDIATE),
// exercising the deliberately out-of-scope "pointer leaf reached through a
// pointer intermediate" deferral distinctly from the "pointer intermediate
// with a non-scalar (slice) leaf" deferral Items also exercises.
type npProfile struct {
	Bio string
}

// npUser is npData.User's pointee: the root-level pointer intermediate every
// single-pointer-level case in this file dot-paths through. Name/Age are its
// two scalar leaves (string and int, respectively — Age exists specifically
// to prove a NUMERIC leaf reached through a pointer intermediate still
// stringifies and compares byte-identically to the interpreter, which itself
// has no separate numeric code path for this shape — see
// TestCodegenNilPointerPathComparisonNumericLeaf). Items is a slice leaf,
// used to prove a pointer intermediate with a NON-SCALAR leaf defers rather
// than guessing at a wrong (string-shaped) closure. ProfilePtr is a pointer
// LEAF, used to prove that shape defers too, distinctly from the slice-leaf
// deferral.
type npUser struct {
	Name       string
	Age        int
	Items      []string
	ProfilePtr *npProfile
}

// npC is npB.CPtr's pointee, the second of the two pointer levels
// TestCodegenNilPointerPathTwoPointerLevels exercises.
type npC struct {
	Name string
}

// npB is npData.BPtr's pointee: its own CPtr field is a SECOND pointer
// intermediate one level further down than npData.BPtr itself, so
// "BPtr.CPtr.Name" has two independent nil checks to prove — either one nil
// must render "", and only both non-nil renders the leaf.
type npB struct {
	CPtr *npC
}

// npInnerTarget is npValueWrapper.PtrField's pointee, used only by the
// mixed value/pointer differential (TestCodegenNilPointerPathMixedValueThenPointer).
type npInnerTarget struct {
	Name string
}

// npValueWrapper wraps a pointer field one level down as a plain VALUE
// struct (not itself a pointer), so "Wrap.PtrField.Name" has exactly ONE
// pointer intermediate (PtrField) — Wrap itself can never be nil and so gets
// no guard — proving buildNilSafeScalarClosure only guards the segments that
// actually need it.
type npValueWrapper struct {
	PtrField *npInnerTarget
}

// npValueOnlyB and npValueOnlyA model a dot-path with NO pointer anywhere
// along it, used by TestCodegenNilPointerPathValueOnlyPathUnaffected to pin
// the byte-for-byte-unchanged regression this slice must not disturb: the
// overwhelmingly common all-value-struct case.
type npValueOnlyB struct {
	C string
}

type npValueOnlyA struct {
	B npValueOnlyB
}

// npData is this file's root struct. User is the single-pointer-level case
// (nil vs non-nil, scalar leaves of two different kinds, a slice leaf, and a
// pointer leaf). BPtr is the two-pointer-level case. Wrap is the mixed
// value-then-pointer case. A is the all-value-struct regression case.
type npData struct {
	User *npUser
	BPtr *npB
	Wrap npValueWrapper
	A    npValueOnlyA
}

var npDataReflectType = reflect.TypeOf(npData{})

// npDataStructSrc is npData's (and its field types') declarations, reused
// verbatim by the differential harness to assemble a standalone, compilable
// Go source file around a GenerateGo result — it must match the Go
// struct declarations above field for field.
const npDataStructSrc = `type npProfile struct {
	Bio string
}

type npUser struct {
	Name       string
	Age        int
	Items      []string
	ProfilePtr *npProfile
}

type npC struct {
	Name string
}

type npB struct {
	CPtr *npC
}

type npInnerTarget struct {
	Name string
}

type npValueWrapper struct {
	PtrField *npInnerTarget
}

type npValueOnlyB struct {
	C string
}

type npValueOnlyA struct {
	B npValueOnlyB
}

type npData struct {
	User *npUser
	BPtr *npB
	Wrap npValueWrapper
	A    npValueOnlyA
}
`

// npCase is one runNpDifferentialBatch case: a full Pug source, the
// interpreter data (a map[string]any, whose values may themselves be nil or
// non-nil pointers — Runtime.getField's own nil-intermediate propagation is
// exercised the same way it would be against real caller data), and
// dataLiteral, an npData composite literal describing the same data for the
// generated Go side.
type npCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

// npResult is one npCase's outcome: the generated code's rendered output,
// the interpreter's own rendered output for the same logical data (computed
// fresh per case, never hardcoded, so a case's "want" can never silently
// drift from the interpreter it's supposed to be pinned against), and any
// generated-code render error.
type npResult struct {
	out  string
	want string
	err  string
}

// runNpDifferentialBatch is this file's differential harness: for each case
// it runs the interpreter (Compile/Template.Render) to compute the
// authoritative want, generates Go via GenerateGo, and batches every case's
// generated code through a single runDifferentialBatch call so a run of many
// cases costs one `go run .`, not one per case.
func runNpDifferentialBatch(t *testing.T, cases []npCase) []npResult {
	t.Helper()

	if len(cases) == 0 {
		return nil
	}

	var diffCases []diffCase
	var wants []string

	for _, c := range cases {
		ast, err := Parse(c.src, nil)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.src, err)
		}
		generated, err := GenerateGo(ast, Config{
			PackageName:     "main",
			FuncName:        "RenderNp",
			DataType:        "npData",
			DataReflectType: npDataReflectType,
		})
		if err != nil {
			t.Fatalf("GenerateGo(%q) case %q: expected no error, got: %v", c.src, c.name, err)
		}

		tmpl, err := Compile(c.src, nil)
		if err != nil {
			t.Fatalf("Compile(%q): %v", c.src, err)
		}
		want, err := tmpl.Render(c.data)
		if err != nil {
			t.Fatalf("interpreter Render(%q) with data %v: %v", c.src, c.data, err)
		}

		diffCases = append(diffCases, diffCase{name: c.name, generated: generated, dataLiteral: c.dataLiteral})
		wants = append(wants, want)
	}

	batchResults := runDifferentialBatch(t, npDataStructSrc, "RenderNp", diffCases)

	results := make([]npResult, len(cases))
	for i, r := range batchResults {
		results[i] = npResult{out: r.Out, want: wants[i], err: r.Err}
	}
	return results
}

// assertNpDiffResult asserts an npResult rendered without error and matches
// the interpreter oracle exactly.
func assertNpDiffResult(t *testing.T, name string, r npResult) {
	t.Helper()
	if r.err != "" {
		t.Fatalf("%s: generated RenderNp: unexpected error %q", name, r.err)
	}
	if r.out != r.want {
		t.Errorf("%s: codegen output %q does not match interpreter output %q", name, r.out, r.want)
	}
}

// TestCodegenNilPointerPathInterpolation is this slice's headline case: a
// single-hop pointer intermediate (User is *npUser) read in interpolation
// position. A nil User renders the empty string, matching getField's own
// nil-propagation plus lookupAndStringify(nil) == ""; a non-nil User renders
// its Name field, proving the non-nil branch is genScalarStringify's plain
// per-kind stringify, unweakened by the guard.
func TestCodegenNilPointerPathInterpolation(t *testing.T) {
	t.Parallel()
	cases := []npCase{
		{
			name:        "nil User",
			src:         "p #{User.Name}\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
		{
			name:        "non-nil User",
			src:         "p #{User.Name}\n",
			data:        map[string]any{"User": &npUser{Name: "Ada"}},
			dataLiteral: `npData{User: &npUser{Name: "Ada"}}`,
		},
	}
	results := runNpDifferentialBatch(t, cases)
	for i, c := range cases {
		assertNpDiffResult(t, c.name, results[i])
	}
	if results[0].out != "<p></p>" {
		t.Errorf("nil User: got %q, want \"<p></p>\"", results[0].out)
	}
	if results[1].out != "<p>Ada</p>" {
		t.Errorf("non-nil User: got %q, want \"<p>Ada</p>\"", results[1].out)
	}
}

// TestCodegenNilPointerPathCondition proves the guarded closure composes
// correctly with genOperandTruthiness's String case (gopug.Truthy) for
// "if"/"unless": nil is falsy, a non-nil non-empty leaf is truthy, and a
// non-nil but EMPTY leaf is still falsy — the empty-leaf case is what
// distinguishes "nil-safe" from "nil-or-empty-collapsed-together", proving
// the guard only fires on the pointer itself, not on the leaf's own value.
func TestCodegenNilPointerPathCondition(t *testing.T) {
	t.Parallel()
	cases := []npCase{
		{
			name:        "if, nil User",
			src:         "if User.Name\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
		{
			name:        "if, non-nil User, non-empty Name",
			src:         "if User.Name\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": &npUser{Name: "Ada"}},
			dataLiteral: `npData{User: &npUser{Name: "Ada"}}`,
		},
		{
			name:        "if, non-nil User, empty Name",
			src:         "if User.Name\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": &npUser{Name: ""}},
			dataLiteral: `npData{User: &npUser{Name: ""}}`,
		},
		{
			name:        "unless, nil User",
			src:         "unless User.Name\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
	}
	results := runNpDifferentialBatch(t, cases)
	for i, c := range cases {
		assertNpDiffResult(t, c.name, results[i])
	}
	wants := []string{"<p>no</p>", "<p>yes</p>", "<p>no</p>", "<p>yes</p>"}
	for i, want := range wants {
		if results[i].out != want {
			t.Errorf("%s: got %q, want %q", cases[i].name, results[i].out, want)
		}
	}
}

// TestCodegenNilPointerPathComparisonStringLeaf proves the guarded closure
// composes correctly with genComparison's default stringify-both fallback
// for a STRING leaf: a nil intermediate compares "" against the literal (no
// match), a non-nil intermediate with a matching leaf compares true.
func TestCodegenNilPointerPathComparisonStringLeaf(t *testing.T) {
	t.Parallel()
	cases := []npCase{
		{
			name:        "nil User == \"admin\"",
			src:         "if User.Name == \"admin\"\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
		{
			name:        "non-nil User, Name matches",
			src:         "if User.Name == \"admin\"\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": &npUser{Name: "admin"}},
			dataLiteral: `npData{User: &npUser{Name: "admin"}}`,
		},
	}
	results := runNpDifferentialBatch(t, cases)
	for i, c := range cases {
		assertNpDiffResult(t, c.name, results[i])
	}
	wants := []string{"<p>no</p>", "<p>yes</p>"}
	for i, want := range wants {
		if results[i].out != want {
			t.Errorf("%s: got %q, want %q", cases[i].name, results[i].out, want)
		}
	}
}

// TestCodegenNilPointerPathComparisonNumericLeaf is
// TestCodegenNilPointerPathComparisonStringLeaf's numeric-leaf sibling,
// proving the design's central claim: a NUMERIC leaf (Age int) reached
// through a pointer intermediate is returned typed as reflectTypeString (not
// Int), so it routes through the SAME stringify-then-gopug.CompareValues
// path a string leaf does — a nil intermediate stringifies to "", which
// gopug.CompareValues numeric-coerce-compares against "30" as false; a
// non-nil intermediate with Age 30 stringifies to "30", comparing true.
func TestCodegenNilPointerPathComparisonNumericLeaf(t *testing.T) {
	t.Parallel()
	cases := []npCase{
		{
			name:        "nil User == 30",
			src:         "if User.Age == 30\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
		{
			name:        "non-nil User, Age 30",
			src:         "if User.Age == 30\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": &npUser{Age: 30}},
			dataLiteral: "npData{User: &npUser{Age: 30}}",
		},
	}
	results := runNpDifferentialBatch(t, cases)
	for i, c := range cases {
		assertNpDiffResult(t, c.name, results[i])
	}
	wants := []string{"<p>no</p>", "<p>yes</p>"}
	for i, want := range wants {
		if results[i].out != want {
			t.Errorf("%s: got %q, want %q", cases[i].name, results[i].out, want)
		}
	}
}

// TestCodegenNilPointerPathTwoPointerLevels proves EACH pointer intermediate
// along a two-hop path (BPtr.CPtr.Name) is independently guarded, in order:
// BPtr nil alone must render "", BPtr non-nil but CPtr nil must ALSO render
// "" (the second guard fires even though the first didn't), and only when
// BOTH are non-nil does the leaf render.
func TestCodegenNilPointerPathTwoPointerLevels(t *testing.T) {
	t.Parallel()
	cases := []npCase{
		{
			name:        "BPtr nil",
			src:         "p #{BPtr.CPtr.Name}\n",
			data:        map[string]any{"BPtr": (*npB)(nil)},
			dataLiteral: "npData{BPtr: nil}",
		},
		{
			name:        "BPtr non-nil, CPtr nil",
			src:         "p #{BPtr.CPtr.Name}\n",
			data:        map[string]any{"BPtr": &npB{CPtr: nil}},
			dataLiteral: "npData{BPtr: &npB{}}",
		},
		{
			name:        "both non-nil",
			src:         "p #{BPtr.CPtr.Name}\n",
			data:        map[string]any{"BPtr": &npB{CPtr: &npC{Name: "Z"}}},
			dataLiteral: `npData{BPtr: &npB{CPtr: &npC{Name: "Z"}}}`,
		},
	}
	results := runNpDifferentialBatch(t, cases)
	for i, c := range cases {
		assertNpDiffResult(t, c.name, results[i])
	}
	wants := []string{"<p></p>", "<p></p>", "<p>Z</p>"}
	for i, want := range wants {
		if results[i].out != want {
			t.Errorf("%s: got %q, want %q", cases[i].name, results[i].out, want)
		}
	}
}

// TestCodegenNilPointerPathLength proves the guarded closure composes
// correctly with genLengthValueExpr/genLengthOperand's String case
// (utf8.RuneCountInString): a nil intermediate's "" length is 0, matching
// the interpreter's own len([]rune(lookupAndStringify(nil))); a non-nil
// intermediate's length is the leaf string's rune count.
func TestCodegenNilPointerPathLength(t *testing.T) {
	t.Parallel()
	cases := []npCase{
		{
			name:        "nil User .length",
			src:         "p= User.Name.length\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
		{
			name:        "non-nil User .length",
			src:         "p= User.Name.length\n",
			data:        map[string]any{"User": &npUser{Name: "Ada"}},
			dataLiteral: `npData{User: &npUser{Name: "Ada"}}`,
		},
	}
	results := runNpDifferentialBatch(t, cases)
	for i, c := range cases {
		assertNpDiffResult(t, c.name, results[i])
	}
	wants := []string{"<p>0</p>", "<p>3</p>"}
	for i, want := range wants {
		if results[i].out != want {
			t.Errorf("%s: got %q, want %q", cases[i].name, results[i].out, want)
		}
	}
}

// TestCodegenNilPointerPathMixedValueThenPointer proves
// buildNilSafeScalarClosure only guards the segment that is ACTUALLY a
// pointer: "Wrap.PtrField.Name" has a value-struct first hop (Wrap, which
// can never be nil) and a pointer second hop (PtrField) — only PtrField gets
// a nil check, and the render still matches the interpreter for both a nil
// and a non-nil PtrField.
func TestCodegenNilPointerPathMixedValueThenPointer(t *testing.T) {
	t.Parallel()
	generated := generateNpForInspection(t, "p #{Wrap.PtrField.Name}\n")
	genStr := string(generated)
	if strings.Contains(genStr, "d.Wrap == nil") {
		t.Errorf("generated code guards d.Wrap for nil, but Wrap is a value struct field that can never be nil:\n%s", genStr)
	}
	if !strings.Contains(genStr, "d.Wrap.PtrField == nil") {
		t.Errorf("generated code does not guard d.Wrap.PtrField for nil, the one genuine pointer intermediate on this path:\n%s", genStr)
	}

	cases := []npCase{
		{
			name:        "nil PtrField",
			src:         "p #{Wrap.PtrField.Name}\n",
			data:        map[string]any{"Wrap": npValueWrapper{}},
			dataLiteral: "npData{Wrap: npValueWrapper{}}",
		},
		{
			name:        "non-nil PtrField",
			src:         "p #{Wrap.PtrField.Name}\n",
			data:        map[string]any{"Wrap": npValueWrapper{PtrField: &npInnerTarget{Name: "M"}}},
			dataLiteral: `npData{Wrap: npValueWrapper{PtrField: &npInnerTarget{Name: "M"}}}`,
		},
	}
	results := runNpDifferentialBatch(t, cases)
	for i, c := range cases {
		assertNpDiffResult(t, c.name, results[i])
	}
	wants := []string{"<p></p>", "<p>M</p>"}
	for i, want := range wants {
		if results[i].out != want {
			t.Errorf("%s: got %q, want %q", cases[i].name, results[i].out, want)
		}
	}
}

// generateNpForInspection runs GenerateGo against npData for src and returns
// the raw generated bytes, for tests that need to inspect the generated Go
// text itself rather than only its rendered output.
func generateNpForInspection(t *testing.T, src string) []byte {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderNp",
		DataType:        "npData",
		DataReflectType: npDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): expected no error, got: %v", src, err)
	}
	return generated
}

// TestCodegenNilPointerPathValueOnlyRegression pins the byte-for-byte
// regression this slice must not disturb: a dot-path with NO pointer
// anywhere along it (A.B.C, all value structs) must emit the exact same
// plain selector it did before this slice — no guarded closure, no
// reflectTypeString override — since buildNilSafeScalarClosure is only ever
// invoked when resolveFieldPath reports at least one pointer intermediate.
func TestCodegenNilPointerPathValueOnlyRegression(t *testing.T) {
	t.Parallel()
	generated := generateNpForInspection(t, "p #{A.B.C}\n")
	genStr := string(generated)
	if strings.Contains(genStr, "func() string") {
		t.Errorf("an all-value-struct dot-path must not emit a guarded closure:\n%s", genStr)
	}
	if !strings.Contains(genStr, "d.A.B.C") {
		t.Errorf("expected the plain selector d.A.B.C in generated code:\n%s", genStr)
	}

	results := runNpDifferentialBatch(t, []npCase{
		{
			name:        "all-value-struct path",
			src:         "p #{A.B.C}\n",
			data:        map[string]any{"A": npValueOnlyA{B: npValueOnlyB{C: "hi"}}},
			dataLiteral: `npData{A: npValueOnlyA{B: npValueOnlyB{C: "hi"}}}`,
		},
	})
	assertNpDiffResult(t, "all-value-struct path", results[0])
	if results[0].out != "<p>hi</p>" {
		t.Errorf("got %q, want \"<p>hi</p>\"", results[0].out)
	}
}

// TestCodegenNilPointerPathDefersNonScalarLeaf proves a pointer intermediate
// with a NON-SCALAR leaf (User.Items, a []string) defers at generate time —
// both in condition/`.length` position and as an `each` collection — rather
// than emitting a wrong (string-shaped) closure for a slice.
func TestCodegenNilPointerPathDefersNonScalarLeaf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{name: "each over a pointer-intermediate slice leaf", src: "each x in User.Items\n  p= x\n"},
		{name: ".length of a pointer-intermediate slice leaf", src: "p #{User.Items.length}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			_, err = GenerateGo(ast, Config{
				PackageName:     "main",
				FuncName:        "RenderNp",
				DataType:        "npData",
				DataReflectType: npDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a deferral error for a pointer-intermediate slice leaf, got nil", tc.src)
			}
		})
	}
}

// TestCodegenNilPointerPathDefersPointerLeaf proves a pointer intermediate
// whose FINAL segment is itself a pointer (User.ProfilePtr — User is the
// pointer intermediate, ProfilePtr is the pointer leaf) defers at generate
// time, distinctly from the non-scalar-leaf deferral above: without this
// deferral, genOperandTruthiness's existing dot-path "!= nil" Ptr rule would
// emit "d.User.ProfilePtr != nil", which still panics when d.User itself is
// nil (the exact hole this slice closes) — so the pointer-leaf shape must be
// refused at generate time rather than silently falling through to that
// unsound rule.
func TestCodegenNilPointerPathDefersPointerLeaf(t *testing.T) {
	t.Parallel()
	src := "if User.ProfilePtr\n  p yes\nelse\n  p no\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderNp",
		DataType:        "npData",
		DataReflectType: npDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error for a pointer-intermediate pointer-leaf path, got nil", src)
	}
}

// TestCodegenNilPointerPathFaultInjectionInterpolationNilNotEmpty is the
// task's first required fault injection: an expectation that a nil-User
// interpolation renders non-empty text must fail.
func TestCodegenNilPointerPathFaultInjectionInterpolationNilNotEmpty(t *testing.T) {
	results := runNpDifferentialBatch(t, []npCase{
		{
			name:        "nil User",
			src:         "p #{User.Name}\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
	})
	assertNpDiffResult(t, "nil User", results[0])
	wrongExpectedNonEmpty := "<p>Ada</p>"
	if results[0].out == wrongExpectedNonEmpty {
		t.Fatalf("fault injection failed to catch a wrong expectation: a nil-intermediate interpolation rendered %q, matching a WRONG non-empty expectation instead of \"<p></p>\"", results[0].out)
	}
}

// TestCodegenNilPointerPathFaultInjectionNumericComparisonNilTrue is the
// task's third required fault injection: an expectation that treats a
// nil-intermediate numeric-leaf comparison (User.Age == 30, User nil) as
// TRUE must fail — proving nil collapses to "" and "" != "30".
func TestCodegenNilPointerPathFaultInjectionNumericComparisonNilTrue(t *testing.T) {
	results := runNpDifferentialBatch(t, []npCase{
		{
			name:        "nil User == 30",
			src:         "if User.Age == 30\n  p yes\nelse\n  p no\n",
			data:        map[string]any{"User": (*npUser)(nil)},
			dataLiteral: "npData{User: nil}",
		},
	})
	assertNpDiffResult(t, "nil User == 30", results[0])
	wrongExpectedTrue := "<p>yes</p>"
	if results[0].out == wrongExpectedTrue {
		t.Fatalf("fault injection failed to catch a wrong expectation: a nil-intermediate numeric comparison rendered %q, matching the WRONG (nil-as-30) expectation instead of \"<p>no</p>\"", results[0].out)
	}
}

// TestCodegenNilPointerPathFaultInjectionSecondLevelNotGuarded is the task's
// fourth required fault injection: an expectation that a SECOND pointer
// level being nil (BPtr non-nil, CPtr nil) still renders the leaf's text
// must fail — proving every pointer intermediate is independently guarded,
// not only the first.
func TestCodegenNilPointerPathFaultInjectionSecondLevelNotGuarded(t *testing.T) {
	results := runNpDifferentialBatch(t, []npCase{
		{
			name:        "BPtr non-nil, CPtr nil",
			src:         "p #{BPtr.CPtr.Name}\n",
			data:        map[string]any{"BPtr": &npB{CPtr: nil}},
			dataLiteral: "npData{BPtr: &npB{}}",
		},
	})
	assertNpDiffResult(t, "BPtr non-nil, CPtr nil", results[0])
	wrongExpectedLeafText := "<p>Z</p>"
	if results[0].out == wrongExpectedLeafText {
		t.Fatalf("fault injection failed to catch a wrong expectation: a nil SECOND pointer level rendered %q, matching a WRONG (second-level-unguarded) expectation instead of \"<p></p>\"", results[0].out)
	}
}
