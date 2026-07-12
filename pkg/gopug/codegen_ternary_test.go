package gopug

import (
	"strings"
	"testing"
)

// codegenTernaryCase is a differential test case for a value-context ternary
// expression: src is rendered through both GenerateGo (built and run as a
// standalone Go program via runGeneratedGo) and the interpreter's own
// Compile().Render, against the same data, and the two outputs must match
// exactly — the interpreter's Render output is always the oracle, never a
// hand-computed expectation.
type codegenTernaryCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

func runCodegenTernaryDifferential(t *testing.T, tc codegenTernaryCase) {
	t.Helper()

	ast, err := Parse(tc.src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", tc.src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", tc.src, err)
	}

	tmpl, err := Compile(tc.src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", tc.src, err)
	}
	want, err := tmpl.Render(tc.data)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	got := runGeneratedGo(t, generated, tc.dataLiteral)
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, tc.src)
	}
}

// TestCodegenTernaryBoolCondition proves the headline case — a bool-field
// condition, in both buffered-code and dynamic-attribute-value position —
// compiles to the func() string{}() IIFE and picks the same branch the
// interpreter's isTruthy(evaluateExpr(cond)) does for both a true and a
// false condition value.
func TestCodegenTernaryBoolCondition(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "true condition, buffered code",
			src:         "p= Flag ? \"on\" : \"off\"\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "false condition, buffered code",
			src:         "p= Flag ? \"on\" : \"off\"\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
		{
			name:        "true condition, attribute value",
			src:         "a(data-state=Flag ? \"on\" : \"off\")\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "false condition, attribute value",
			src:         "a(data-state=Flag ? \"on\" : \"off\")\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryComparisonCondition proves a comparison condition
// (`Count > 0`) — routed through genCondition's genComparison, reused
// unchanged by the ternary's IIFE — picks the correct branch for both a
// positive and a non-positive value.
func TestCodegenTernaryComparisonCondition(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "positive count",
			src:         "p= Count > 0 ? \"pos\" : \"nonpos\"\n",
			data:        map[string]any{"Count": 5},
			dataLiteral: "opsData{Count: 5}",
		},
		{
			name:        "zero count",
			src:         "p= Count > 0 ? \"pos\" : \"nonpos\"\n",
			data:        map[string]any{"Count": 0},
			dataLiteral: "opsData{Count: 0}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryStringFieldCondition proves a bare string-field
// condition (`Name ? Name : "anon"`) routes through genOperandTruthiness's
// gopug.Truthy call for both the condition AND reuses the same field for the
// true branch's genValueExpr — an empty string is falsy (isTruthy's empty-
// string case) and a non-empty one is truthy.
func TestCodegenTernaryStringFieldCondition(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "empty string is falsy",
			src:         "p= Name ? Name : \"anon\"\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
		{
			name:        "non-empty string is truthy",
			src:         "p= Name ? Name : \"anon\"\n",
			data:        map[string]any{"Name": "x"},
			dataLiteral: `opsData{Name: "x"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryLogicalCondition proves a `&&`-combinator condition
// (genCondition's logical-combinator support) composes correctly as a
// ternary's condition.
func TestCodegenTernaryLogicalCondition(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "both true",
			src:         "p= Flag && FlagB ? \"both\" : \"not\"\n",
			data:        map[string]any{"Flag": true, "FlagB": true},
			dataLiteral: "opsData{Flag: true, FlagB: true}",
		},
		{
			name:        "one false",
			src:         "p= Flag && FlagB ? \"both\" : \"not\"\n",
			data:        map[string]any{"Flag": true, "FlagB": false},
			dataLiteral: "opsData{Flag: true, FlagB: false}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryArithmeticBranches proves each branch of a ternary is
// itself a full genValueExpr expression — here, `+`/`-` arithmetic on the
// same field the condition inspects.
func TestCodegenTernaryArithmeticBranches(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "true branch arithmetic",
			src:         "p= Flag ? Count + 1 : Count - 1\n",
			data:        map[string]any{"Flag": true, "Count": 5},
			dataLiteral: "opsData{Flag: true, Count: 5}",
		},
		{
			name:        "false branch arithmetic",
			src:         "p= Flag ? Count + 1 : Count - 1\n",
			data:        map[string]any{"Flag": false, "Count": 5},
			dataLiteral: "opsData{Flag: false, Count: 5}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryNestedBranch proves a ternary nested inside another
// ternary's true branch recurses correctly through genValueExpr — the outer
// IIFE's true-branch return is itself an inner IIFE call expression.
func TestCodegenTernaryNestedBranch(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "outer true, inner true",
			src:         "p= Flag ? (FlagB ? \"a\" : \"b\") : \"c\"\n",
			data:        map[string]any{"Flag": true, "FlagB": true},
			dataLiteral: "opsData{Flag: true, FlagB: true}",
		},
		{
			name:        "outer true, inner false",
			src:         "p= Flag ? (FlagB ? \"a\" : \"b\") : \"c\"\n",
			data:        map[string]any{"Flag": true, "FlagB": false},
			dataLiteral: "opsData{Flag: true, FlagB: false}",
		},
		{
			name:        "outer false",
			src:         "p= Flag ? (FlagB ? \"a\" : \"b\") : \"c\"\n",
			data:        map[string]any{"Flag": false, "FlagB": true},
			dataLiteral: "opsData{Flag: false, FlagB: true}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryCompositionTemplateLiteral proves a ternary embedded in a
// `${...}` interpolation inside a backtick template literal composes: the
// IIFE call expression becomes one of the template literal's concatenated
// segments.
func TestCodegenTernaryCompositionTemplateLiteral(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "true condition",
			src:         "p= `state: ${Flag ? \"on\" : \"off\"}`\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "false condition",
			src:         "p= `state: ${Flag ? \"on\" : \"off\"}`\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryCompositionInterpolation proves a ternary as a `#{...}`
// interpolation composes — the IIFE call expression is wrapped in
// html.EscapeString, exactly as any other genValueExpr result reaching
// genInterpolation is.
func TestCodegenTernaryCompositionInterpolation(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "true condition",
			src:         "p #{Flag ? \"y\" : \"n\"}\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "false condition",
			src:         "p #{Flag ? \"y\" : \"n\"}\n",
			data:        map[string]any{"Flag": false},
			dataLiteral: "opsData{Flag: false}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenTernaryDifferential(t, tc)
		})
	}
}

// TestCodegenTernaryCompositionAttrHTMLSpecialChar proves a ternary attribute
// value whose taken branch contains HTML-special characters is still passed
// through gopug.EscapeAttr — the IIFE composes under EscapeAttr exactly like
// any other genValueExpr result reaching genAttributes, so the "<"/">"/"&"
// in the branch text end up entity-escaped in the rendered attribute.
func TestCodegenTernaryCompositionAttrHTMLSpecialChar(t *testing.T) {
	src := "a(title=Flag ? \"<a> & b\" : \"plain\")\n"

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

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	want, err := tmpl.Render(map[string]any{"Flag": true})
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if !strings.Contains(want, "&lt;a&gt;") {
		t.Fatalf("interpreter Render sanity check: expected an entity-escaped branch in %q", want)
	}

	got := runGeneratedGo(t, generated, "opsData{Flag: true}")
	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, src)
	}
}

// TestCodegenTernaryWhitespace proves whitespace around `?`/`:` doesn't
// change the compiled output — genCondition and genValueExpr both
// TrimSpace/strip-parens internally (mirroring how the interpreter's
// evaluateExpr callees trim the raw slices findTernary/findBinaryOp locate),
// so a ternary written with no surrounding spaces at all must render
// byte-identically to the same ternary with conventional spacing.
func TestCodegenTernaryWhitespace(t *testing.T) {
	cases := []codegenTernaryCase{
		{
			name:        "spaces around ? and :",
			src:         "p= Flag ? \"a\" : \"b\"\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
		{
			name:        "no spaces around ? and :",
			src:         "p=Flag?\"a\":\"b\"\n",
			data:        map[string]any{"Flag": true},
			dataLiteral: "opsData{Flag: true}",
		},
	}

	var outputs []string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.src, err)
			}
			generated, err := GenerateGo(ast, Config{
				PackageName:     "main",
				FuncName:        "RenderOps",
				DataType:        "opsData",
				DataReflectType: opsDataReflectType,
			})
			if err != nil {
				t.Fatalf("GenerateGo(%q): %v", tc.src, err)
			}

			tmpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tc.src, err)
			}
			want, err := tmpl.Render(tc.data)
			if err != nil {
				t.Fatalf("interpreter Render: %v", err)
			}

			got := runGeneratedGo(t, generated, tc.dataLiteral)
			if got != want {
				t.Errorf("codegen output %q does not match interpreter output %q for %q", got, want, tc.src)
			}
			outputs = append(outputs, got)
		})
	}

	if len(outputs) == 2 && outputs[0] != outputs[1] {
		t.Errorf("whitespace around ?/: changed the rendered output: %q vs %q", outputs[0], outputs[1])
	}
}

// TestCodegenTernaryMalformedNoColon asserts a ternary missing its top-level
// `:` is rejected with an error describing a malformed ternary, mirroring
// the interpreter's own "malformed ternary expression" message intent
// (Runtime.evaluateExpr, runtime.go:2080) rather than emitting a Go IIFE with
// no false-branch return.
func TestCodegenTernaryMalformedNoColon(t *testing.T) {
	src := "p= Flag ? \"a\"\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}

	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a malformed-ternary error, got nil", src)
	}
	if !strings.Contains(err.Error(), "malformed ternary") {
		t.Errorf("GenerateGo(%q): error %q does not describe a malformed ternary", src, err.Error())
	}
}

// TestCodegenTernaryUnsupportedCondition asserts a ternary whose CONDITION is
// a shape genCondition can't yet compile (here, arithmetic) propagates that
// error unchanged rather than emitting an IIFE with a broken or divergent
// condition — genValueExpr's ternary support reuses genCondition unmodified,
// so a condition genCondition rejects in plain `if` position is rejected
// here too.
func TestCodegenTernaryUnsupportedCondition(t *testing.T) {
	src := "p #{(Count + 1) ? \"a\" : \"b\"}\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}

	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-condition error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported condition", src, err.Error())
	}
}

// TestCodegenTernaryUnsupportedBranch asserts a ternary BRANCH containing a
// fallible operator (`/`) propagates genValueExpr's division-deferral error
// unchanged — the branch is compiled by the same genValueExpr the rest of
// this increment already rejects `/` from, and the ternary wrapper doesn't
// swallow or reword that error.
func TestCodegenTernaryUnsupportedBranch(t *testing.T) {
	src := "p= Flag ? Count / 2 : \"x\"\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}

	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a division-deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("GenerateGo(%q): error %q does not describe the division deferral", src, err.Error())
	}
}

// TestCodegenTernaryUnrelatedLogicalStillUnsupported is a regression proof
// that adding ternary support to genValueExpr did NOT accidentally unlock
// value-context `&&`/`||` for expressions with no ternary at all — that
// combinator support is a distinct, still-deferred increment.
func TestCodegenTernaryUnrelatedLogicalStillUnsupported(t *testing.T) {
	src := "p #{Flag && FlagB}\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}

	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported && operator error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}
