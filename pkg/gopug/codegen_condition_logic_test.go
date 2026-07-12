package gopug

import (
	"fmt"
	"strings"
	"testing"
)

// runConditionDifferential builds "if <cond>\n  p yes\nelse\n  p no\n",
// renders it through both the interpreter (Compile/Template.Render, against
// data) and the codegen backend (GenerateGo/runGeneratedGo, against
// dataLiteral — an opsData composite literal describing the same data), and
// asserts the two outputs are byte-identical. It is the shared machinery
// every truth-table/precedence/string-truthiness case in this file drives.
func runConditionDifferential(t *testing.T, cond string, data map[string]any, dataLiteral string) string {
	t.Helper()

	src := "if " + cond + "\n  p yes\nelse\n  p no\n"

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
		t.Fatalf("GenerateGo(%q): expected no error, got: %v", src, err)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render(%q) with data %v: %v", src, data, err)
	}

	got := runGeneratedGo(t, generated, dataLiteral)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for template %q with data literal %s", got, want, src, dataLiteral)
	}
	return got
}

// TestCodegenConditionLogicTruthTable proves genCondition's `&&`/`||`
// restructure against every combination of two independent bool fields,
// for both the plain interpreter data map and the codegen data literal, so
// a divergence in either operand's truthiness or in Go's own short-circuit
// evaluation would show up as a mismatched branch.
func TestCodegenConditionLogicTruthTable(t *testing.T) {
	combos := []struct{ a, b bool }{
		{true, true},
		{true, false},
		{false, true},
		{false, false},
	}

	for _, op := range []string{"&&", "||"} {
		for _, c := range combos {
			name := fmt.Sprintf("Flag=%v %s FlagB=%v", c.a, op, c.b)
			t.Run(name, func(t *testing.T) {
				cond := "Flag " + op + " FlagB"
				data := map[string]any{"Flag": c.a, "FlagB": c.b}
				dataLiteral := fmt.Sprintf("opsData{Flag: %v, FlagB: %v}", c.a, c.b)

				var want string
				if op == "&&" {
					want = boolBranch(c.a && c.b)
				} else {
					want = boolBranch(c.a || c.b)
				}

				got := runConditionDifferential(t, cond, data, dataLiteral)
				if got != want {
					t.Errorf("condition %q with Flag=%v FlagB=%v: got %q, want %q", cond, c.a, c.b, got, want)
				}
			})
		}
	}
}

// boolBranch is the rendered "p yes"/"p no" output runConditionDifferential's
// template produces for a true/false condition, used by tests that want to
// assert the specific branch taken (not just interpreter/codegen agreement).
func boolBranch(b bool) string {
	if b {
		return "<p>yes</p>"
	}
	return "<p>no</p>"
}

// TestCodegenConditionLogicMixedOperands proves genCondition's `&&`/`||`
// restructure over operands of DIFFERENT shapes — a numeric comparison
// combined with a bool field, and a string-equality comparison combined
// with a numeric comparison — so genCondition's own recursive call into
// genComparison for one side, and genOperandTruthiness for the other, is
// exercised within the same condition.
func TestCodegenConditionLogicMixedOperands(t *testing.T) {
	cases := []struct {
		name        string
		cond        string
		data        map[string]any
		dataLiteral string
		want        bool
	}{
		{
			name:        "Count > 0 && Flag, both true",
			cond:        "Count > 0 && Flag",
			data:        map[string]any{"Count": 5, "Flag": true},
			dataLiteral: "opsData{Count: 5, Flag: true}",
			want:        true,
		},
		{
			name:        "Count > 0 && Flag, comparison false",
			cond:        "Count > 0 && Flag",
			data:        map[string]any{"Count": 0, "Flag": true},
			dataLiteral: "opsData{Count: 0, Flag: true}",
			want:        false,
		},
		{
			name:        "Count > 0 && Flag, bool false",
			cond:        "Count > 0 && Flag",
			data:        map[string]any{"Count": 5, "Flag": false},
			dataLiteral: "opsData{Count: 5, Flag: false}",
			want:        false,
		},
		{
			name:        `Name == "x" || Count > 3, string side true`,
			cond:        `Name == "x" || Count > 3`,
			data:        map[string]any{"Name": "x", "Count": 0},
			dataLiteral: `opsData{Name: "x", Count: 0}`,
			want:        true,
		},
		{
			name:        `Name == "x" || Count > 3, numeric side true`,
			cond:        `Name == "x" || Count > 3`,
			data:        map[string]any{"Name": "y", "Count": 4},
			dataLiteral: `opsData{Name: "y", Count: 4}`,
			want:        true,
		},
		{
			name:        `Name == "x" || Count > 3, both false`,
			cond:        `Name == "x" || Count > 3`,
			data:        map[string]any{"Name": "y", "Count": 0},
			dataLiteral: `opsData{Name: "y", Count: 0}`,
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runConditionDifferential(t, tc.cond, tc.data, tc.dataLiteral)
			if want := boolBranch(tc.want); got != want {
				t.Errorf("condition %q: got %q, want %q", tc.cond, got, want)
			}
		})
	}
}

// TestCodegenConditionLogicNegation proves genCondition's leading-`!`
// restructure: `!` over a bare bool field, and `!` over a parenthesized
// comparison — the two shapes evaluateExpr's own `!` branch has to handle
// identically (a leading `!` that is NOT the start of a `!=` comparison).
func TestCodegenConditionLogicNegation(t *testing.T) {
	cases := []struct {
		name        string
		cond        string
		data        map[string]any
		dataLiteral string
		want        bool
	}{
		{name: "!Flag, Flag true", cond: "!Flag", data: map[string]any{"Flag": true}, dataLiteral: "opsData{Flag: true}", want: false},
		{name: "!Flag, Flag false", cond: "!Flag", data: map[string]any{"Flag": false}, dataLiteral: "opsData{Flag: false}", want: true},
		{name: "!(Count > 0), true count", cond: "!(Count > 0)", data: map[string]any{"Count": 5}, dataLiteral: "opsData{Count: 5}", want: false},
		{name: "!(Count > 0), zero count", cond: "!(Count > 0)", data: map[string]any{"Count": 0}, dataLiteral: "opsData{Count: 0}", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runConditionDifferential(t, tc.cond, tc.data, tc.dataLiteral)
			if want := boolBranch(tc.want); got != want {
				t.Errorf("condition %q: got %q, want %q", tc.cond, got, want)
			}
		})
	}
}

// TestCodegenConditionLogicStringTruthiness proves the string-field leaf
// case added to genOperandTruthiness — routed through the exported
// gopug.Truthy — reproduces isTruthy's exact quirky falsy set on a string
// value, not just "empty vs non-empty": "0" and "false" are falsy strings
// too, the same way a bool/numeric field's OWN stringify could never
// accidentally produce them.
func TestCodegenConditionLogicStringTruthiness(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "non-empty string", value: "hello", want: true},
		{name: "empty string", value: "", want: false},
		{name: "the string \"0\"", value: "0", want: false},
		{name: "the string \"false\"", value: "false", want: false},
		{name: "the string \"null\"", value: "null", want: false},
		{name: "a numeric-looking non-zero string", value: "5", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{"Name": tc.value}
			dataLiteral := fmt.Sprintf("opsData{Name: %q}", tc.value)

			got := runConditionDifferential(t, "Name", data, dataLiteral)
			if want := boolBranch(tc.want); got != want {
				t.Errorf("condition \"Name\" with Name=%q: got %q, want %q", tc.value, got, want)
			}
		})
	}
}

// TestCodegenConditionLogicStringTruthinessCombinator proves the string
// truthiness leaf composes correctly with `&&` — a string operand's
// gopug.Truthy result feeds the same native Go `&&` a bool/numeric operand
// would, so "Name && Flag" only takes the true branch when Name is a
// non-falsy string AND Flag is true.
func TestCodegenConditionLogicStringTruthinessCombinator(t *testing.T) {
	cases := []struct {
		name    string
		nameVal string
		flag    bool
		want    bool
	}{
		{name: "non-empty name, true flag", nameVal: "hello", flag: true, want: true},
		{name: "non-empty name, false flag", nameVal: "hello", flag: false, want: false},
		{name: "empty name, true flag", nameVal: "", flag: true, want: false},
		{name: "falsy-string name \"0\", true flag", nameVal: "0", flag: true, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{"Name": tc.nameVal, "Flag": tc.flag}
			dataLiteral := fmt.Sprintf("opsData{Name: %q, Flag: %v}", tc.nameVal, tc.flag)

			got := runConditionDifferential(t, "Name && Flag", data, dataLiteral)
			if want := boolBranch(tc.want); got != want {
				t.Errorf("condition \"Name && Flag\" with Name=%q Flag=%v: got %q, want %q", tc.nameVal, tc.flag, got, want)
			}
		})
	}
}

// TestCodegenConditionLogicPrecedence proves genCondition splits on the SAME
// top-level operator the interpreter's evaluateExpr does — `||` before
// `&&` — for "A || B && C", which must associate as "A || (B && C)", not
// "(A || B) && C". Flag=true, FlagB=false, FlagC=false is the
// disambiguating combination: the correct grouping yields
// true || (false && false) == true, while the wrong left-to-right grouping
// would yield (true || false) && false == false.
func TestCodegenConditionLogicPrecedence(t *testing.T) {
	cases := []struct {
		name               string
		a, b, c            bool
		wantOrThenAndGroup bool
	}{
		{name: "disambiguating case: correct grouping is true, wrong grouping is false", a: true, b: false, c: false, wantOrThenAndGroup: true},
		{name: "all false", a: false, b: false, c: false, wantOrThenAndGroup: false},
		{name: "all true", a: true, b: true, c: true, wantOrThenAndGroup: true},
		{name: "A false, B&&C true", a: false, b: true, c: true, wantOrThenAndGroup: true},
		{name: "A false, B&&C false (B only)", a: false, b: true, c: false, wantOrThenAndGroup: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{"Flag": tc.a, "FlagB": tc.b, "FlagC": tc.c}
			dataLiteral := fmt.Sprintf("opsData{Flag: %v, FlagB: %v, FlagC: %v}", tc.a, tc.b, tc.c)

			// The correct ("A || (B && C)") grouping, computed independently
			// of both the interpreter and codegen, as the test's own oracle.
			wantCorrectGrouping := tc.a || (tc.b && tc.c)
			if wantCorrectGrouping != tc.wantOrThenAndGroup {
				t.Fatalf("test bug: table's wantOrThenAndGroup=%v does not match A || (B && C) = %v", tc.wantOrThenAndGroup, wantCorrectGrouping)
			}

			got := runConditionDifferential(t, "Flag || FlagB && FlagC", data, dataLiteral)
			if want := boolBranch(wantCorrectGrouping); got != want {
				t.Errorf("condition \"Flag || FlagB && FlagC\" with Flag=%v FlagB=%v FlagC=%v: got %q, want %q (A || (B && C) grouping)", tc.a, tc.b, tc.c, got, want)
			}
		})
	}
}

// TestCodegenConditionLogicUnsupported asserts the scope boundary this
// increment holds: a top-level `&&`/`||`/`!` combinator IS now supported in
// CONDITION position (unlike VALUE-context `#{...}`/`= expr`, which still
// errors on all three — a separate, later increment), a ternary condition
// still errors even underneath a combinator, and a non-scalar (slice) field
// used as a bare truthiness operand — including underneath a combinator —
// still errors rather than reproducing the interpreter's stringify-then-
// isTruthy footgun for that shape.
func TestCodegenConditionLogicUnsupported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "value-context #{} interpolation && combinator still errors", src: "p #{Flag && FlagB}\n"},
		{name: "value-context #{} interpolation || combinator still errors", src: "p #{Flag || FlagB}\n"},
		{name: "value-context #{} interpolation ! operator still errors", src: "p #{!Flag}\n"},
		{name: "ternary condition still errors", src: "if Count > 0 ? true : false\n  p yes\n"},
		{name: "ternary underneath && still errors", src: "if Flag && (Count > 0 ? true : false)\n  p yes\n"},
		{name: "bare slice field condition still errors", src: "if Items\n  p yes\n"},
		{name: "slice field underneath && still errors", src: "if Flag && Items\n  p yes\n"},
		{name: "slice field underneath || still errors", src: "if Items || Flag\n  p yes\n"},
		{name: "slice field underneath ! still errors", src: "if !Items\n  p yes\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-construct error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}
