package gopug

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
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
// shares but doesn't name as precisely. Str1/Str2 are a second and third
// plain string field (Name already being the first), used by the
// value-expr `+` differential tests to prove the runtime-value-dependent
// disambiguation gopug.Add performs — the same two fields hold numeric-
// looking strings in one subtest and non-numeric strings in another. Slug is
// a fourth plain string field, used by the attribute-value concat
// differential tests so the field under test isn't also one of Str1/Str2's
// `+`-disambiguation cases. Firms is a slice of a nested struct with its own
// int field, used by the attribute template-literal differential test to
// prove a dot-path rooted at an each-loop variable (`firm.ID`) resolves
// correctly inside a `${...}` interpolation. FlagB/FlagC are a second and
// third independent bool field (Flag already being the first), used by the
// `&&`/`||`/`!` condition-combinator differential tests to exercise
// multi-operand truth tables and `||`-before-`&&` precedence. Zero is an int
// field always holding 0, used by the fallible-value-expression differential
// tests to exercise `/`/`%`'s one error case (a numeric zero divisor).
type opsData struct {
	Name    string
	Count   int
	Price   float64
	Flag    bool
	FlagB   bool
	FlagC   bool
	Items   []string
	Age     int8
	B       uint8
	BigInt  int64
	BigUint uint64
	Str1    string
	Str2    string
	Slug    string
	Firms   []opsFirm
	Zero    int
}

// opsFirm is opsData.Firms's element type.
type opsFirm struct {
	ID int
}

var opsDataReflectType = reflect.TypeOf(opsData{})

// opsDataStructSrc is opsData's (and opsFirm's) field declarations, reused
// verbatim by buildGeneratedGo to assemble a standalone, compilable Go
// source file around a GenerateGo result — it must match the opsData struct
// above field for field.
const opsDataStructSrc = `type opsFirm struct {
	ID int
}

type opsData struct {
	Name    string
	Count   int
	Price   float64
	Flag    bool
	FlagB   bool
	FlagC   bool
	Items   []string
	Age     int8
	B       uint8
	BigInt  int64
	BigUint uint64
	Str1    string
	Str2    string
	Slug    string
	Firms   []opsFirm
	Zero    int
}
`

// repoModuleReplaceDirectives returns the extra go.mod lines a throwaway
// module's go.mod needs to resolve github.com/sinfulspartan/go-pug/pkg/gopug
// against this checkout, rather than trying (and failing) to fetch it from a
// module proxy: a require for the module path, satisfied by a replace
// pointing at the repository root this test file itself lives under
// (computed from runtime.Caller so the throwaway module works regardless of
// the current working directory the test binary happens to run from).
// Generated code that calls an exported gopug helper — gopug.Add,
// gopug.EscapeAttr, gopug.JoinClasses — needs this to actually build and run
// in the throwaway module buildGeneratedGo/runGeneratedGo assemble.
func repoModuleReplaceDirectives(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller: could not determine this test file's own path")
	}
	// thisFile is <repoRoot>/pkg/gopug/codegen_ops_test.go.
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return fmt.Sprintf("\nrequire github.com/sinfulspartan/go-pug v0.0.0\n\nreplace github.com/sinfulspartan/go-pug => %s\n", strconv.Quote(repoRoot))
}

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
	goMod := "module opsbuild\n\ngo 1.26\n" + repoModuleReplaceDirectives(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
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

// runGeneratedGo builds and runs generated (a GenerateGo result whose
// Config.PackageName is "main" and Config.FuncName is "RenderOps") in a
// throwaway module, alongside a copy of the opsData struct declaration and
// an appended main() that constructs dataLiteral (an opsData composite
// literal, e.g. `opsData{Count: 64}`) and writes RenderOps's output to
// stdout, then returns that output. This is the only way to compare a
// GenerateGo result's actual rendered HTML against the interpreter's own
// Render output — buildGeneratedGo only proves the generated code compiles,
// not what it produces.
func runGeneratedGo(t *testing.T, generated []byte, dataLiteral string) string {
	t.Helper()

	dir := t.TempDir()
	goMod := "module opsbuild\n\ngo 1.26\n" + repoModuleReplaceDirectives(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	genStr := string(generated)
	funcIdx := strings.Index(genStr, "\nfunc ")
	if funcIdx < 0 {
		t.Fatalf("generated source has no \"func \" to splice the struct declaration before:\n%s", genStr)
	}

	// The "os" import must precede every non-import declaration (Go
	// requires all ImportDecls before the first TopLevelDecl), so it's
	// spliced in right alongside the struct — before "func RenderOps" —
	// rather than appended next to main() at the end of the file.
	var src strings.Builder
	src.WriteString(genStr[:funcIdx])
	src.WriteString("\n\nimport \"os\"\n\n")
	src.WriteString(opsDataStructSrc)
	src.WriteString(genStr[funcIdx:])
	src.WriteString("\nfunc main() {\n\td := ")
	src.WriteString(dataLiteral)
	src.WriteString("\n\tif err := RenderOps(os.Stdout, d); err != nil {\n\t\tpanic(err)\n\t}\n}\n")

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatalf("writing main.go: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run on generated code failed:\n%s\n--- source ---\n%s", out, src.String())
	}
	return string(out)
}

// TestCodegenConditionOperatorUnsupported asserts that every condition
// construct outside the bounded-agreement subset — arithmetic, ternary,
// string ordering compares, a numeric-looking string literal compared to a
// string field, and an incompatible numeric-field-vs-numeric-field comparison
// — returns an error instead of emitting a comparison that might not agree
// with the interpreter's compareValues. The `&&`/`||`/`!` combinators are NOT
// in this list: they are supported in CONDITION position (see
// TestCodegenConditionLogicTruthTable, TestCodegenConditionLogicMixedOperands,
// TestCodegenConditionLogicNegation, TestCodegenConditionLogicStringTruthiness,
// and TestCodegenConditionLogicPrecedence in codegen_condition_logic_test.go)
// and, as of the value-context logical/comparison increment, in VALUE
// context too (`#{}`/`= expr` interpolation — see
// codegen_logical_value_test.go), so "an operator used in interpolation
// rather than condition position" is no longer in this list either.
func TestCodegenConditionOperatorUnsupported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "&& combinator with an operand codegen still can't resolve (a non-scalar field)",
			src:  "if Flag && Items\n  p yes\n",
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
// whose magnitude doesn't fit the compared field — an int8 field against a
// literal outside [-128,127], a uint8 field against a literal outside
// [0,255], or an integer literal beyond codegen's safe-integer bound of
// ±(2^53 − 1), JS's Number.MAX_SAFE_INTEGER (a plain int field against a
// literal far beyond it, and — the exact case the bound is drawn to cover —
// a BigInt int64 field against MaxInt64/MinInt64/exactly ±2^53 and a
// BigUint uint64 field against MaxUint64/exactly 2^53, all of which fit
// their field's actual Go range but not the float64-exactness bound
// codegen requires to guarantee agreement with the interpreter's own
// stringify-then-reparse comparison, see checkLiteralAgainstFieldKind) —
// is rejected with a descriptive error instead of GenerateGo silently
// emitting a direct Go comparison (e.g. `d.Age == 1000`) that fails to
// compile: gofmt's formatter (what GenerateGo actually runs) doesn't
// type-check, so an unchecked overflow here would only surface as a
// `go build` failure of the generated file, not as a GenerateGo error.
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
		{
			name: "int64 field compared to its actual MaxInt64, still beyond codegen's ±2^53 safe-integer bound",
			src:  "if BigInt == 9223372036854775807\n  p yes\n",
		},
		{
			name: "int64 field compared to its actual MinInt64, still beyond codegen's ±2^53 safe-integer bound",
			src:  "if BigInt == -9223372036854775808\n  p yes\n",
		},
		{
			name: "uint64 field compared to its actual MaxUint64, still beyond codegen's 2^53 safe-integer bound",
			src:  "if BigUint == 18446744073709551615\n  p yes\n",
		},
		{
			// 2^53 itself is exactly representable in a float64, but an
			// int64 field can hold 2^53 + 1 (not exactly representable),
			// which rounds back down to 2^53 when the interpreter
			// stringifies-then-reparses it — so 2^53 is the first literal
			// value beyond codegen's safe-integer bound, not one past it.
			name: "int64 field compared to exactly 2^53, one past codegen's safe-integer bound",
			src:  "if BigInt == 9007199254740992\n  p yes\n",
		},
		{
			name: "int64 field compared to exactly -2^53, one past codegen's negative safe-integer bound",
			src:  "if BigInt == -9007199254740992\n  p yes\n",
		},
		{
			name: "uint64 field compared to exactly 2^53, one past codegen's safe-integer bound",
			src:  "if BigUint == 9007199254740992\n  p yes\n",
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
// minimum) — int8's 127, uint8's 255 — or exactly equals codegen's
// ±(2^53 − 1) safe-integer bound (JS's Number.MAX_SAFE_INTEGER) against a
// 64-bit field, is still accepted by GenerateGo AND that the emitted
// comparison actually `go build`s. The bound is 2^53 − 1, not 2^53: at
// exactly 2^53 an int64/uint64 field could hold 2^53 + 1 — a value a
// float64 can't represent exactly, which rounds back down to 2^53 when the
// interpreter's compareValues stringifies-then-reparses it, so a literal of
// exactly 2^53 could diverge (see checkLiteralAgainstFieldKind and
// TestCodegenConditionOperatorLiteralOverflow's 2^53 reject cases). At
// 2^53 − 1, both neighboring integers are still exactly representable, so
// no field value can alias onto it. Unlike codegen's previous
// verbatim-token literal emission, a BigInt/BigUint field's actual
// MaxInt64/MinInt64/MaxUint64 is no longer among these accepted cases: it
// now falls, deliberately, on the rejected side in
// TestCodegenConditionOperatorLiteralOverflow — see that test's doc comment
// for why bounded agreement requires refusing it.
func TestCodegenConditionOperatorLiteralOverflowAccepts(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "int8 field compared to its exact maximum", src: "if Age == 127\n  p yes\n"},
		{name: "int8 field compared to its exact minimum", src: "if Age == -128\n  p yes\n"},
		{name: "uint8 field compared to its exact maximum", src: "if B == 255\n  p yes\n"},
		{name: "int64 field compared to exactly codegen's safe-integer bound (2^53 - 1)", src: "if BigInt == 9007199254740991\n  p yes\n"},
		{name: "int64 field compared to exactly negative codegen's safe-integer bound (-(2^53 - 1))", src: "if BigInt == -9007199254740991\n  p yes\n"},
		{name: "uint64 field compared to exactly codegen's safe-integer bound (2^53 - 1)", src: "if BigUint == 9007199254740991\n  p yes\n"},
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

// TestCodegenConditionNumericLiteralNotAField asserts that a token
// parseJSNumber does not recognize as a valid sloppy-JS numeric literal —
// an underscore digit separator ("0_100", "-0_100", "1_000": Go's ParseFloat
// accepts these, but pug's JS grammar does not, so parseJSNumber rejects
// every one of them), and an octal-looking leading-zero integer prefix
// (every digit "0"-"7") directly followed by "." or an exponent marker
// ("00.5", "017.5", "01e2": a legacy octal integer literal has no
// fractional/exponent form in JS, so these are SyntaxErrors) — falls
// through genOperand's numeric-literal path to field resolution and is
// rejected there instead, since none of these tokens is a valid Pug field
// name either. This is deliberate: codegen no longer guards against
// leading-zero literals the way it once did — they are now SUPPORTED (see
// TestCodegenConditionLeadingZeroLiteralAccepts) — in favor of letting
// parseJSNumber itself be the single source of truth for what counts as a
// numeric literal.
func TestCodegenConditionNumericLiteralNotAField(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "leading-zero decimal with underscore separator", src: "if Count == 0_100\n  p yes\n"},
		{name: "negated leading-zero decimal with underscore separator", src: "if Count == -0_100\n  p yes\n"},
		{name: "underscore separator in an ordinary decimal", src: "if Count == 1_000\n  p yes\n"},
		{name: "octal-looking leading zero directly followed by a fraction (bare 0)", src: "if Price == 00.5\n  p yes\n"},
		{name: "octal-looking leading zero directly followed by a fraction", src: "if Price == 017.5\n  p yes\n"},
		{name: "octal-looking leading zero directly followed by an exponent", src: "if Count == 01e2\n  p yes\n"},
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
				t.Fatalf("GenerateGo(%q): expected an unsupported-expression error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
			if strings.Contains(err.Error(), "generator bug") {
				t.Errorf("GenerateGo(%q): error %q still surfaces as a misleading gofmt/generator-bug failure", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenConditionLeadingZeroLiteralAccepts asserts that a leading-zero
// numeric literal — legacy octal ("0100", "007"), NonOctalDecimal ("009",
// a leading-zero prefix containing an 8 or 9), the negated and
// "+"-prefixed forms of both, a bare "0", and a leading-zero float ("0.5",
// read identically by Go and the interpreter since a "." always makes it a
// base-10 floating-point literal) — is accepted by GenerateGo and, for the
// octal/NonOctalDecimal forms, compiles to a comparison against the
// literal's CANONICAL DECIMAL value: "0100" is octal 64 in JS, not literal
// Go source octal, so GenerateGo must never emit "0100" verbatim. A couple
// build the emitted comparison to prove it actually compiles.
func TestCodegenConditionLeadingZeroLiteralAccepts(t *testing.T) {
	cases := []struct {
		name         string
		src          string
		build        bool
		wantContains string
	}{
		{name: "bare zero", src: "if Count == 0\n  p yes\n", build: true},
		{name: "leading-zero float", src: "if Price == 0.5\n  p yes\n", build: true},
		{name: "ordinary decimal integer", src: "if Count == 100\n  p yes\n"},
		{name: "negative decimal integer", src: "if Count == -5\n  p yes\n"},
		{name: "plus-prefixed decimal integer", src: "if Count == +100\n  p yes\n", build: true},
		{name: "plus-prefixed leading-zero float", src: "if Price == +0.5\n  p yes\n", build: true},
		{name: "legacy octal literal", src: "if Count == 0100\n  p yes\n", build: true, wantContains: "64"},
		{name: "legacy octal literal, single extra digit", src: "if Count == 007\n  p yes\n", wantContains: "7"},
		{name: "NonOctalDecimal literal (contains a 9)", src: "if Count == 009\n  p yes\n", build: true, wantContains: "9"},
		{name: "negated legacy octal literal", src: "if Count == -0100\n  p yes\n", wantContains: "-64"},
		{name: "plus-prefixed legacy octal literal", src: "if Count == +0100\n  p yes\n", build: true, wantContains: "64"},
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

			if tc.wantContains != "" {
				if strings.Contains(string(got), "0100") || strings.Contains(string(got), "007") || strings.Contains(string(got), "009") {
					t.Errorf("GenerateGo(%q): emitted source still contains the original leading-zero token verbatim, not its canonical decimal value:\n%s", tc.src, got)
				}
				if !strings.Contains(string(got), tc.wantContains) {
					t.Errorf("GenerateGo(%q): emitted source %q does not contain the expected canonical decimal value %q", tc.src, got, tc.wantContains)
				}
			}

			if tc.build {
				buildGeneratedGo(t, got)
			}
		})
	}
}

// TestCodegenNumericLiteralDifferentialMatchesInterpreter is the
// three-way-agreement proof for codegen's numeric-literal handling: for
// every numeric-literal form the interpreter's parseJSNumber recognizes
// (legacy octal, hex,
// binary, modern octal, NonOctalDecimal, plain decimal, exponent, and the
// negated/"+"-prefixed forms), compiling and running GenerateGo's output
// against a matching field produces the exact same rendered output —
// for both the true and false branches — as Compile/Template.Render (the
// interpreter). Since a prior differential test
// (TestNumericLiteralInterpreterMatchesPug, numeric_literal_test.go)
// already anchors the interpreter's parseJSNumber to pug.js's own values,
// this test transitively proves pug.js == interpreter == codegen for every
// literal it covers.
func TestCodegenNumericLiteralDifferentialMatchesInterpreter(t *testing.T) {
	cases := []struct {
		name     string
		token    string
		field    string
		expected float64
	}{
		{name: "legacy octal 0100", token: "0100", field: "Count", expected: 64},
		{name: "legacy octal 0777", token: "0777", field: "Count", expected: 511},
		{name: "hex 0x10", token: "0x10", field: "Count", expected: 16},
		{name: "hex 0xff", token: "0xff", field: "Count", expected: 255},
		{name: "binary 0b101", token: "0b101", field: "Count", expected: 5},
		{name: "modern octal 0o17", token: "0o17", field: "Count", expected: 15},
		{name: "NonOctalDecimal 08", token: "08", field: "Count", expected: 8},
		{name: "NonOctalDecimal 019", token: "019", field: "Count", expected: 19},
		{name: "plain decimal 100", token: "100", field: "Count", expected: 100},
		{name: "exponent 1e3", token: "1e3", field: "Count", expected: 1000},
		{name: "negated legacy octal -0100", token: "-0100", field: "Count", expected: -64},
		{name: "signed hex +0x10", token: "+0x10", field: "Count", expected: 16},
		{name: "float 3.14", token: "3.14", field: "Price", expected: 3.14},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "if " + tc.field + " == " + tc.token + "\n  p yes\nelse\n  p no\n"

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
				t.Fatalf("GenerateGo(%q): expected no error for a within-bounds literal, got: %v", src, err)
			}

			tmpl, err := Compile(src, nil)
			if err != nil {
				t.Fatalf("Compile(%q): %v", src, err)
			}

			branches := []struct {
				name  string
				value float64
			}{
				{"matching branch", tc.expected},
				{"non-matching branch", tc.expected + 1},
			}

			for _, br := range branches {
				t.Run(br.name, func(t *testing.T) {
					var dataKey string
					var dataLiteral string
					var mapValue any
					if tc.field == "Price" {
						dataKey = "Price"
						dataLiteral = fmt.Sprintf("opsData{Price: %v}", br.value)
						mapValue = br.value
					} else {
						dataKey = "Count"
						dataLiteral = fmt.Sprintf("opsData{Count: %d}", int64(br.value))
						mapValue = int(br.value)
					}

					want, err := tmpl.Render(map[string]any{dataKey: mapValue})
					if err != nil {
						t.Fatalf("interpreter Render: %v", err)
					}

					got := runGeneratedGo(t, generated, dataLiteral)
					if got != want {
						t.Errorf("codegen output %q does not match interpreter output %q for template %q with %s = %v", got, want, src, dataKey, mapValue)
					}
				})
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
