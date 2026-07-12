package gopug

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// opsData is the declared struct codegen_ops_test.go's error cases resolve
// condition operands against: one field of each shape the bounded-agreement
// operator rules need to exercise the constructs that must be rejected,
// including a couple of narrow sized-integer kinds (Age int8, B uint8) whose
// range is easy to overflow with an ordinary-looking Pug numeric literal,
// and explicit BigInt int64/BigUint uint64 fields for exercising the exact
// int64/uint64 boundary (2^63 / 2^64) a plain platform-sized int/uint field
// shares but doesn't name as precisely.
type opsData struct {
	Name    string
	Count   int
	Price   float64
	Flag    bool
	Items   []string
	Age     int8
	B       uint8
	BigInt  int64
	BigUint uint64
}

var opsDataReflectType = reflect.TypeOf(opsData{})

// opsDataStructSrc is opsData's field declarations, reused verbatim by
// buildGeneratedGo to assemble a standalone, compilable Go source file
// around a GenerateGo result — it must match the opsData struct above field
// for field.
const opsDataStructSrc = `type opsData struct {
	Name    string
	Count   int
	Price   float64
	Flag    bool
	Items   []string
	Age     int8
	B       uint8
	BigInt  int64
	BigUint uint64
}
`

// buildGeneratedGo writes generated (a GenerateGo result whose Config.PackageName
// is "opsbuild") into a throwaway module alongside a copy of the opsData struct
// declaration, and runs `go build` on it — the only way to prove a generated
// condition that GenerateGo accepted actually compiles, since GenerateGo itself
// only runs it through gofmt (go/format.Source), which does no type checking
// and would happily hand back syntactically valid but semantically broken Go
// (e.g. a numeric constant that overflows its comparison target's type).
func buildGeneratedGo(t *testing.T, generated []byte) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module opsbuild\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	// generated already opens with its own "package opsbuild" clause and
	// import block; Go requires imports to precede every other declaration,
	// so the struct has to be spliced in right after the import block ends
	// (at the blank line before "func "), not simply appended after it.
	genStr := string(generated)
	funcIdx := strings.Index(genStr, "\nfunc ")
	if funcIdx < 0 {
		t.Fatalf("generated source has no \"func \" to splice the struct declaration before:\n%s", genStr)
	}

	var src strings.Builder
	src.WriteString(genStr[:funcIdx])
	src.WriteString("\n\n")
	src.WriteString(opsDataStructSrc)
	src.WriteString(genStr[funcIdx:])

	if err := os.WriteFile(filepath.Join(dir, "render.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatalf("writing render.go: %v", err)
	}

	cmd := exec.Command("go", "build", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build on generated code failed (this means GenerateGo accepted a condition it should have rejected):\n%s\n--- source ---\n%s", out, src.String())
	}
}

// TestCodegenConditionOperatorUnsupported asserts that every condition
// construct outside 2d-ops-1's bounded-agreement subset — the `&&`/`||`/`!`
// combinators, arithmetic, ternary, string ordering compares, a
// numeric-looking string literal compared to a string field, an
// incompatible numeric-field-vs-numeric-field comparison, and an operator
// used in interpolation rather than condition position — returns an error
// instead of emitting a comparison that might not agree with the
// interpreter's compareValues.
func TestCodegenConditionOperatorUnsupported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "&& combinator",
			src:  "if Flag && Count\n  p yes\n",
		},
		{
			name: "|| combinator",
			src:  "if Flag || Count\n  p yes\n",
		},
		{
			name: "leading ! operator",
			src:  "if !Flag\n  p yes\n",
		},
		{
			name: "arithmetic in a comparison operand",
			src:  "if Count + 1 > 2\n  p yes\n",
		},
		{
			name: "ternary condition",
			src:  "if Count > 0 ? true : false\n  p yes\n",
		},
		{
			name: "numeric-looking string literal compared to a string field",
			src:  `if Name == "5"` + "\n  p yes\n",
		},
		{
			name: "string ordering comparison",
			src:  `if Name > "m"` + "\n  p yes\n",
		},
		{
			name: "int field compared to a float64 field",
			src:  "if Count == Price\n  p yes\n",
		},
		{
			name: "operator in interpolation rather than condition position",
			src:  "p #{Count > 0}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-operator error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenConditionOperatorLiteralOverflow asserts that a numeric literal
// whose magnitude doesn't fit the compared field's sized Go kind — an int8
// field against a literal outside [-128,127], a uint8 field against a
// literal outside [0,255], a plain int field against a literal beyond what
// even int64 can hold, or — the exact int64/uint64 boundary — a BigInt
// int64 field against 2^63 (one more than int64's actual max) and a
// BigUint uint64 field against 2^64 — is rejected with a descriptive error
// instead of GenerateGo silently emitting a direct Go comparison (e.g.
// `d.Age == 1000`, or `d.BigInt == 9223372036854775808`) that fails to
// compile: gofmt's formatter (what GenerateGo actually runs) doesn't
// type-check, so an unchecked overflow here would only surface as a
// `go build` failure of the generated file, not as a GenerateGo error.
//
// The 2^63/2^64 cases are the precision hole a naive float64-based range
// check falls into: strconv.ParseFloat rounds the valid literal
// "9223372036854775807" (int64's actual max) UP to the exact same float64
// value as the invalid "9223372036854775808" (2^63) — both parse to
// 9223372036854775808.0 — so any check based only on the parsed float
// cannot tell them apart. TestCodegenConditionOperatorLiteralOverflowAccepts
// proves the valid boundary literal is still accepted and compiles despite
// this.
func TestCodegenConditionOperatorLiteralOverflow(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "int8 field compared to a literal beyond int8's range",
			src:  "if Age == 1000\n  p yes\n",
		},
		{
			name: "uint8 field compared to a literal beyond uint8's range",
			src:  "if B == 300\n  p yes\n",
		},
		{
			name: "int field compared to a literal beyond int64's range",
			src:  "if Count == 1e19\n  p yes\n",
		},
		{
			name: "int64 field compared to 2^63 (one more than int64's actual max)",
			src:  "if BigInt == 9223372036854775808\n  p yes\n",
		},
		{
			name: "uint64 field compared to 2^64 (one more than uint64's actual max)",
			src:  "if BigUint == 18446744073709551616\n  p yes\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an out-of-range error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
			if !strings.Contains(err.Error(), "out of range") {
				t.Errorf("GenerateGo(%q): error %q does not describe an out-of-range literal", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenConditionOperatorLiteralOverflowAccepts asserts the flip side
// of TestCodegenConditionOperatorLiteralOverflow: a numeric literal that
// exactly equals a sized field kind's true maximum (or, for a signed field,
// minimum) — int8's 127, uint8's 255, int64's actual max 9223372036854775807
// (MaxInt64, one less than the 2^63 the previous test proved rejected), and
// uint64's actual max 18446744073709551615 (MaxUint64, one less than 2^64)
// — is still accepted by GenerateGo AND that the emitted comparison
// actually `go build`s. The MaxInt64/MaxUint64 cases are the ones a naive
// `>=` fix at the rounded float64 boundary would have wrongly rejected,
// since "9223372036854775807" parses to the identical float64 value as the
// invalid "9223372036854775808" (both round to 2^63) — this test only
// passes if the range check uses the literal's exact decimal text rather
// than that lossy float64 approximation.
func TestCodegenConditionOperatorLiteralOverflowAccepts(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "int8 field compared to its exact maximum", src: "if Age == 127\n  p yes\n"},
		{name: "int8 field compared to its exact minimum", src: "if Age == -128\n  p yes\n"},
		{name: "uint8 field compared to its exact maximum", src: "if B == 255\n  p yes\n"},
		{name: "int64 field compared to MaxInt64 exactly", src: "if BigInt == 9223372036854775807\n  p yes\n"},
		{name: "int64 field compared to MinInt64 exactly", src: "if BigInt == -9223372036854775808\n  p yes\n"},
		{name: "uint64 field compared to MaxUint64 exactly", src: "if BigUint == 18446744073709551615\n  p yes\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			got, err := GenerateGo(ast, Config{
				PackageName:     "opsbuild",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err != nil {
				t.Fatalf("GenerateGo(%q): expected no error for an in-range boundary literal, got: %v", tc.src, err)
			}

			buildGeneratedGo(t, got)
		})
	}
}

// TestCodegenConditionLeadingZeroLiteralUnsupported asserts that a numeric
// literal Go's compiler would read as a different value than the
// interpreter does — a leading zero followed by more digits ("0100", "007",
// "009", the negated "-0100"), the same forms with an underscore digit
// separator ("0_100", "-0_100", "0_18" — Go's octal grammar and the
// interpreter's parseNumber both accept "_" separators, so they're just as
// much an octal/decimal disagreement as the unseparated forms), and a
// "+"-prefixed leading-zero literal ("+0100" — a leading "+" is ordinary
// comparison syntax both Go and parseNumber accept, so it reaches this path
// too) — is rejected with a clean unsupported-literal error, instead of
// either silently emitting a Go integer literal Go parses as octal (a
// byte-identical breach: the interpreter reads "0100" as decimal 100, but
// Go reads the emitted d.Count == 0100 as octal 64) or letting an
// invalid-octal token like "009"/"0_18" reach go/format.Source and surface
// as a misleading "generator bug" gofmt-failure error.
func TestCodegenConditionLeadingZeroLiteralUnsupported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "leading-zero decimal read as octal by Go", src: "if Count == 0100\n  p yes\n"},
		{name: "leading-zero decimal, single extra digit", src: "if Count == 007\n  p yes\n"},
		{name: "invalid-octal digit (9), previously a misleading gofmt error", src: "if Count == 009\n  p yes\n"},
		{name: "negated leading-zero decimal", src: "if Count == -0100\n  p yes\n"},
		{name: "leading-zero decimal with underscore separator", src: "if Count == 0_100\n  p yes\n"},
		{name: "negated leading-zero decimal with underscore separator", src: "if Count == -0_100\n  p yes\n"},
		{name: "invalid-octal digit with underscore separator", src: "if Count == 0_18\n  p yes\n"},
		{name: "plus-prefixed leading-zero decimal", src: "if Count == +0100\n  p yes\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-literal error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported numeric literal") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported numeric literal", tc.src, err.Error())
			}
			if strings.Contains(err.Error(), "generator bug") {
				t.Errorf("GenerateGo(%q): error %q still surfaces as a misleading gofmt/generator-bug failure", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenConditionLeadingZeroLiteralAccepts asserts the forms the
// leading-zero guard must leave untouched still succeed: a bare "0", a
// leading-zero float ("0.5", read identically by Go and the interpreter
// since a "." always makes it a base-10 floating-point literal in Go), an
// ordinary decimal integer, a negative decimal integer, and — proving the
// sign-stripping fix for "+" doesn't over-reject a legitimately-signed
// base-10 literal — a "+"-prefixed decimal integer and a "+"-prefixed
// leading-zero float. A couple build the emitted comparison to prove the
// guard didn't collaterally break the existing verbatim-emission path.
func TestCodegenConditionLeadingZeroLiteralAccepts(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		build bool
	}{
		{name: "bare zero", src: "if Count == 0\n  p yes\n", build: true},
		{name: "leading-zero float", src: "if Price == 0.5\n  p yes\n", build: true},
		{name: "ordinary decimal integer", src: "if Count == 100\n  p yes\n"},
		{name: "negative decimal integer", src: "if Count == -5\n  p yes\n"},
		{name: "plus-prefixed decimal integer", src: "if Count == +100\n  p yes\n", build: true},
		{name: "plus-prefixed leading-zero float", src: "if Price == +0.5\n  p yes\n", build: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			got, err := GenerateGo(ast, Config{
				PackageName:     "opsbuild",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err != nil {
				t.Fatalf("GenerateGo(%q): expected no error, got: %v", tc.src, err)
			}

			if tc.build {
				buildGeneratedGo(t, got)
			}
		})
	}
}

// TestCodegenConditionOperatorNilReflectType asserts that a comparison or
// `.length` condition — both of which need field types to classify their
// operands — is rejected under a nil Config.DataReflectType, since only the
// bare-field truthiness path can be resolved without type information.
func TestCodegenConditionOperatorNilReflectType(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "comparison", src: "if Count > 0\n  p yes\n"},
		{name: ".length truthiness", src: "if Items.length\n  p yes\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName: "gopug",
				FuncName:    "RenderOps",
				DataType:    "opsData",
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q) with nil DataReflectType: expected an error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}
