package gopug

import "testing"

// TestParseJSNumber validates parseJSNumber against the empirical pug 3.0.4
// reference table for every documented numeric-literal shape, plus every
// form pug rejects as a SyntaxError. This is the contract parseJSNumber must
// satisfy; the interpreter and the codegen backend both build on it.
func TestParseJSNumber(t *testing.T) {
	accepted := []struct {
		token string
		want  float64
	}{
		{"0100", 64},
		{"0777", 511},
		{"017", 15},
		{"00", 0},
		{"0o100", 64},
		{"0O17", 15},
		{"3.14", 3.14},
		{"0.5", 0.5},
		{"0.0", 0},
		{"-0100", -64},
		{"08", 8},
		{"09", 9},
		{"019", 19},
		{"0118", 118},
		{"0128", 128},
		{"100", 100},
		{".5", 0.5},
		{"0e0", 0},
		{"-100", -100},
		{"+0x10", 16},
		{"0x10", 16},
		{"0xff", 255},
		{"0XA", 10},
		{"0x1F", 31},
		{"0b101", 5},
		{"0B11", 3},
		{"1e3", 1000},
		{"1.5e2", 150},
		{"+100", 100},
		// A leading-zero integer prefix containing an 8 or 9 is not octal —
		// JS reads it as an ordinary DecimalIntegerLiteral, so a following
		// "." or exponent is a normal DecimalLiteral, not a SyntaxError.
		{"08.5", 8.5},
		{"019.5", 19.5},
		{"0118.5", 118.5},
		{"08e2", 800},
		{"0128.5", 128.5},
	}

	for _, tc := range accepted {
		t.Run(tc.token, func(t *testing.T) {
			got, ok := parseJSNumber(tc.token)
			if !ok {
				t.Fatalf("parseJSNumber(%q): got ok=false, want value %v", tc.token, tc.want)
			}
			if got != tc.want {
				t.Errorf("parseJSNumber(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}

	rejected := []string{
		"1_000",
		"0x1_0",
		"1_000.5",
		"0b1_01",
		"00.5",
		"0x",
		"0o",
		"0b",
		"0x1G",
		"0o8",
		"0b2",
		"",
		"+",
		"-",
		"01e2",
		// All-octal leading-zero integer prefixes have no fractional or
		// exponent form; these remain SyntaxErrors in JS.
		"0100.5",
		"017.5",
	}

	for _, token := range rejected {
		t.Run("reject_"+token, func(t *testing.T) {
			if got, ok := parseJSNumber(token); ok {
				t.Errorf("parseJSNumber(%q) = %v, ok=true, want ok=false", token, got)
			}
		})
	}
}

// TestNumericLiteralInterpreterMatchesPug is a differential test asserting
// that Compile("p= <literal>").Render(nil) produces the same value pug 3.0.4
// produces for the literal, for every shape in the reference table.
func TestNumericLiteralInterpreterMatchesPug(t *testing.T) {
	cases := []struct {
		literal string
		want    string
	}{
		{"0100", "64"},
		{"0777", "511"},
		{"017", "15"},
		{"00", "0"},
		{"0o100", "64"},
		{"0O17", "15"},
		{"3.14", "3.14"},
		{"0.5", "0.5"},
		{"0.0", "0"},
		{"-0100", "-64"},
		{"08", "8"},
		{"09", "9"},
		{"019", "19"},
		{"0118", "118"},
		{"0128", "128"},
		{"100", "100"},
		{".5", "0.5"},
		{"0e0", "0"},
		{"-100", "-100"},
		{"+0x10", "16"},
		{"0x10", "16"},
		{"0xff", "255"},
		{"0XA", "10"},
		{"0x1F", "31"},
		{"0b101", "5"},
		{"0B11", "3"},
		{"1e3", "1000"},
		{"1.5e2", "150"},
		{"+100", "100"},
		{"0", "0"},
		{"08.5", "8.5"},
		{"08e2", "800"},
	}

	for _, tc := range cases {
		t.Run(tc.literal, func(t *testing.T) {
			out := renderTest(t, "p= "+tc.literal+"\n", nil)
			want := "<p>" + tc.want + "</p>"
			if out != want {
				t.Errorf("p= %s: got %q, want %q", tc.literal, out, want)
			}
		})
	}
}

// TestNumericLiteralInterpolation confirms the octal/NonOctalDecimal rules
// apply through string interpolation, not just a buffered code node.
func TestNumericLiteralInterpolation(t *testing.T) {
	out := renderTest(t, "p #{0100}\n", nil)
	assertEqual(t, out, "<p>64</p>")

	out = renderTest(t, "p #{08}\n", nil)
	assertEqual(t, out, "<p>8</p>")

	out = renderTest(t, "p #{1e3}\n", nil)
	assertEqual(t, out, "<p>1000</p>")
}

// TestNumericLiteralComparison confirms a leading-zero literal is evaluated
// to its JS numeric value before comparison, so "if 0100 == 64" takes the
// true branch and "if 0100 == 100" takes the false branch.
func TestNumericLiteralComparison(t *testing.T) {
	out := renderTest(t, "if 0100 == 64\n  p yes\nelse\n  p no\n", nil)
	assertEqual(t, out, "<p>yes</p>")

	out = renderTest(t, "if 0100 == 100\n  p yes\nelse\n  p no\n", nil)
	assertEqual(t, out, "<p>no</p>")
}

// TestNumericLiteralAssignment confirms an unbuffered assignment RHS
// evaluates a leading-zero literal to its JS numeric value.
func TestNumericLiteralAssignment(t *testing.T) {
	out := renderTest(t, "- var n = 0o17\np= n\n", nil)
	assertEqual(t, out, "<p>15</p>")
}

// TestNumericLiteralDecimalUnchanged asserts that plain decimal literals
// (no leading-zero octal/NonOctalDecimal shape) render byte-identically to
// pre-existing behavior.
func TestNumericLiteralDecimalUnchanged(t *testing.T) {
	assertEqual(t, renderTest(t, "p= 100\n", nil), "<p>100</p>")
	assertEqual(t, renderTest(t, "p= 3.14\n", nil), "<p>3.14</p>")
	assertEqual(t, renderTest(t, "p= 0\n", nil), "<p>0</p>")
}

// TestNumericLiteralValueCoercionUntouched proves the octal/NonOctalDecimal
// literal grammar applies only to source-code literals, never to runtime
// string data. A leading-zero string datum (e.g. a zip code) must stay
// decimal text, matching JS Number("0100") === 100 (and, since it's never
// even coerced to a number here, the string is simply passed through
// unchanged).
func TestNumericLiteralValueCoercionUntouched(t *testing.T) {
	out := renderTest(t, "p #{zip}\n", map[string]interface{}{"zip": "0100"})
	assertEqual(t, out, "<p>0100</p>")

	out = renderTest(t, `p #{"0100"}`+"\n", nil)
	assertEqual(t, out, "<p>0100</p>")
}
