package gopug

import (
	"strings"
	"testing"
)

// codegenUnbufferedCase is a differential test case for a `- var x = <rhs>`
// unbuffered assignment: src is rendered through both GenerateGo (built and
// run as a standalone Go program via runGeneratedGo) and the interpreter's
// own Compile().Render, against the same data, and the two outputs must
// match exactly — the interpreter's Render output is always the oracle,
// never a hand-computed expectation.
type codegenUnbufferedCase struct {
	name        string
	src         string
	data        map[string]any
	dataLiteral string
}

func runCodegenUnbufferedDifferential(t *testing.T, tc codegenUnbufferedCase) {
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

// runCodegenUnbufferedDifferentialBatch is runCodegenUnbufferedDifferential
// generalized to a whole slice of cases: every case's GenerateGo output and
// interpreter oracle (Compile().Render) are prepared up front, then
// submitted to a SINGLE runDifferentialBatch call instead of one `go run`
// per case. Each case's own pass/fail is still reported through its own
// t.Run(tc.name, ...), matched to its batch result by index.
func runCodegenUnbufferedDifferentialBatch(t *testing.T, cases []codegenUnbufferedCase) {
	t.Helper()

	if len(cases) == 0 {
		return
	}

	type prepared struct {
		tc   codegenUnbufferedCase
		want string
	}

	var diffCases []diffCase
	var prep []prepared

	for _, tc := range cases {
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

		diffCases = append(diffCases, diffCase{name: tc.name, generated: generated, dataLiteral: tc.dataLiteral})
		prep = append(prep, prepared{tc: tc, want: want})
	}

	results := runDifferentialBatch(t, opsDataStructSrc, "RenderOps", diffCases)

	for i, p := range prep {
		t.Run(p.tc.name, func(t *testing.T) {
			result := results[i]
			if result.Err != "" {
				t.Fatalf("generated RenderOps(%q): unexpected error %q", p.tc.src, result.Err)
			}
			if result.Out != p.want {
				t.Errorf("codegen output %q does not match interpreter output %q for %q", result.Out, p.want, p.tc.src)
			}
		})
	}
}

func genUnbufferedErr(t *testing.T, src string) error {
	t.Helper()
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
	return err
}

// TestCodegenUnbufferedAssignTernaryStringLiteral is the headline case: a
// ternary selecting between two string literals, gated on a `||`/`!`
// condition over bool fields — the shape the real shared layouts use for
// `confirmDialogsAttr = (!User || User.ConfirmDialogs) ? "true" : "false"`
// (mirrored here with bool fields already declared on opsData, since
// pointer/nil-truthiness in condition position is not yet supported by
// genCondition — a separate, later concern from this slice's assignment
// support) — assigned to a local, then read back in BOTH a `#{}`-style
// buffered-code use and a dynamic attribute value, byte-identical to the
// interpreter across every combination of the two operands.
func TestCodegenUnbufferedAssignTernaryStringLiteral(t *testing.T) {
	t.Parallel()
	src := "- var confirmDialogsAttr = (!Flag || FlagB) ? \"true\" : \"false\"\n" +
		"button(data-confirm=confirmDialogsAttr)= confirmDialogsAttr\n"

	cases := []codegenUnbufferedCase{
		{
			name:        "Flag false (negation makes condition true)",
			src:         src,
			data:        map[string]any{"Flag": false, "FlagB": false},
			dataLiteral: "opsData{Flag: false, FlagB: false}",
		},
		{
			name:        "Flag true, FlagB true (condition true via FlagB)",
			src:         src,
			data:        map[string]any{"Flag": true, "FlagB": true},
			dataLiteral: "opsData{Flag: true, FlagB: true}",
		},
		{
			name:        "Flag true, FlagB false (condition false)",
			src:         src,
			data:        map[string]any{"Flag": true, "FlagB": false},
			dataLiteral: "opsData{Flag: true, FlagB: false}",
		},
	}

	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedAssignStringField proves a bare dot-path RHS
// (resolveFieldExpr's leaf case, restricted to a string-typed field) reads
// back correctly.
func TestCodegenUnbufferedAssignStringField(t *testing.T) {
	t.Parallel()
	src := "- var greeting = User.Name\np=greeting\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"User": map[string]any{"Name": "Ada"}},
		dataLiteral: `opsData{User: opsUser{Name: "Ada"}}`,
	})
}

// TestCodegenUnbufferedAssignStringLiteral proves a plain quoted string
// literal RHS reads back correctly, including when the variable is used
// more than once.
func TestCodegenUnbufferedAssignStringLiteral(t *testing.T) {
	t.Parallel()
	src := "- var greeting = \"hello\"\np=greeting\np=greeting\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenUnbufferedAssignConcat proves a top-level `+` string
// concatenation RHS (both operands string fields) reads back correctly.
func TestCodegenUnbufferedAssignConcat(t *testing.T) {
	t.Parallel()
	src := "- var full = Str1 + Str2\np=full\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Str1": "foo", "Str2": "bar"},
		dataLiteral: `opsData{Str1: "foo", Str2: "bar"}`,
	})
}

// TestCodegenUnbufferedAssignTemplateLiteral proves a backtick template
// literal RHS reads back correctly, including one whose `${...}` part uses a
// construct broader than genAssignRHS's own grammar (here, a numeric field)
// — safe because Runtime.evaluateExprRaw never descends into a template
// literal's structure at all (see genAssignRHS's doc comment).
func TestCodegenUnbufferedAssignTemplateLiteral(t *testing.T) {
	t.Parallel()
	src := "- var label = `count: ${Count}`\np=label\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Count": 5},
		dataLiteral: "opsData{Count: 5}",
	})
}

// TestCodegenUnbufferedAssignOrDefault proves the `||` default-value idiom
// (`Name || "anon"`) reads back correctly for both a truthy and a falsy left
// operand.
func TestCodegenUnbufferedAssignOrDefault(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "left truthy",
			src:         "- var display = Name || \"anon\"\np=display\n",
			data:        map[string]any{"Name": "Ada"},
			dataLiteral: `opsData{Name: "Ada"}`,
		},
		{
			name:        "left falsy (empty string)",
			src:         "- var display = Name || \"anon\"\np=display\n",
			data:        map[string]any{"Name": ""},
			dataLiteral: `opsData{Name: ""}`,
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedAssignAndCombinator proves the `&&` value-context
// combinator reads back correctly for both a truthy and a falsy left
// operand.
func TestCodegenUnbufferedAssignAndCombinator(t *testing.T) {
	t.Parallel()
	cases := []codegenUnbufferedCase{
		{
			name:        "left truthy",
			src:         "- var display = Name && Str1\np=display\n",
			data:        map[string]any{"Name": "Ada", "Str1": "x"},
			dataLiteral: `opsData{Name: "Ada", Str1: "x"}`,
		},
		{
			name:        "left falsy (empty string)",
			src:         "- var display = Name && Str1\np=display\n",
			data:        map[string]any{"Name": "", "Str1": "x"},
			dataLiteral: `opsData{Name: "", Str1: "x"}`,
		},
	}
	runCodegenUnbufferedDifferentialBatch(t, cases)
}

// TestCodegenUnbufferedAssignVisibleBeforeEach proves a `- var` set before an
// each-loop is visible (and correctly resolved) inside the loop body.
func TestCodegenUnbufferedAssignVisibleBeforeEach(t *testing.T) {
	t.Parallel()
	src := "- var label = \"item: \"\neach x in Items\n  p=label + x\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Items": []any{"a", "b"}},
		dataLiteral: `opsData{Items: []string{"a", "b"}}`,
	})
}

// TestCodegenUnbufferedAssignUnusedVarStillCompiles proves an unreferenced
// `- var` still compiles and renders — the interpreter always evaluates the
// RHS even when the variable is never read, and codegen must not silently
// drop that evaluation (or fail to compile a declared-and-unused Go local).
func TestCodegenUnbufferedAssignUnusedVarStillCompiles(t *testing.T) {
	t.Parallel()
	src := "- var unused = \"hi\"\np static\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{},
		dataLiteral: "opsData{}",
	})
}

// TestCodegenUnbufferedAssignFaultInjection proves the differential harness
// itself is non-vacuous: a deliberately WRONG expected value must fail the
// comparison, so a passing differential test above is actually exercising
// the generated code's output, not merely checking it built and ran.
func TestCodegenUnbufferedAssignFaultInjection(t *testing.T) {
	t.Parallel()
	src := "- var greeting = \"hello\"\np=greeting\n"

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

	got := runGeneratedGo(t, generated, "opsData{}")
	wrongWant := "<p>goodbye</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// --- Scope ---

// TestCodegenUnbufferedAssignLeakPastEachDeferred asserts that a `- var`
// declared inside an each-loop body, referenced only by a sibling AFTER the
// loop closes, is rejected with a clear error rather than either failing to
// compile or silently guessing at a value: codegen pops the scope binding
// when the each-loop body's node list finishes generating (mirroring the Go
// lexical block the `:=` actually lives in), recording it into
// leakedVarNames, so the later reference is rejected by resolveFieldExpr's
// leaked-name guard before it can ever fall through to struct-field
// resolution. See TestCodegenUnbufferedAssignLeakCollisionDeferred for the
// case where a real struct field WOULD otherwise silently match.
func TestCodegenUnbufferedAssignLeakPastEachDeferred(t *testing.T) {
	src := "each x in Items\n  - var loopLocal = x\np=loopLocal\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an error (loopLocal used outside its each-loop scope), got nil", src)
	}
}

// TestCodegenUnbufferedAssignLeakCollisionDeferred is the regression test
// for the silent divergence found in review: a `- var` declared inside an
// `if`/tag-children/each-loop block, referenced by a later sibling AFTER
// that block closes, whose NAME happens to also match a real struct field
// (exactly, or only differing in case) MUST be rejected with a clean
// GenerateGo error — not silently resolved to that field's (wrong) value.
//
// Runtime's own scopeStack frames a scope ONLY at an each-loop iteration and
// a mixin call (verified empirically: renderConditional and a tag's
// children loop push no frame at all — see runtime.go). So a `- var`
// assigned inside an `if` branch or a tag's children is stored, via
// Runtime.setVar, into whichever frame is already innermost at that point —
// typically the function-wide root frame, which is quite literally the SAME
// map instance as the data the caller passed to Render (Runtime.Render sets
// `r.scopeStack[0] = r.data`) — so it stays live and readable by a LATER
// SIBLING after the block closes, for as long as that frame survives.
// Empirically (differential, both proven directly against the interpreter,
// see TestUnbufferedAssignLeakEmpiricalMatrix):
//   - An EXACT-name collision with an already-existing top-level key:
//     Runtime.setVar's "walk scopeStack for an existing binding, overwrite
//     it in place" behavior finds and MUTATES that key's value directly —
//     so a later read returns whatever was LAST assigned inside the block,
//     not any "original" field value a struct-based codegen read would ever
//     produce.
//   - A case-DIFFERING collision (no exact top-level key, but a
//     differently-cased one): Runtime.setVar creates a genuinely fresh
//     binding (still in the same un-framed enclosing scope) that survives
//     past the block close; a naive codegen fallback would resolve the
//     reference via resolveStructField's case-insensitive tier to the
//     UNRELATED existing field instead.
//
// This is proven to affect an each-loop body's `- var` too, not just an
// if/tag-children one (an each-loop iteration's OWN frame pop is real and
// matches the interpreter, but nothing about that stops an exact-name
// collision from mutating an OUTER frame's key during the loop, or a
// case-differing collision from matching a struct field afterward) — so
// resolveFieldExpr's leaked-name guard applies uniformly to every
// scopeRestore call site (if/tag-children/each-loop alike), deliberately
// wider than the exact shape review first found it in, rather than only
// covering that one shape and leaving an identically-shaped hole in the
// each-loop path.
func TestCodegenUnbufferedAssignLeakCollisionDeferred(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "if branch, exact-name collision with a struct field",
			src:  "if Flag\n  - var Name = \"local\"\np=Name\n",
		},
		{
			name: "if branch, case-differing collision with a struct field",
			src:  "if Flag\n  - var name = \"local\"\np=name\n",
		},
		{
			name: "tag children, exact-name collision with a struct field",
			src:  "div\n  - var Name = \"local\"\np=Name\n",
		},
		{
			name: "tag children, case-differing collision with a struct field",
			src:  "div\n  - var name = \"local\"\np=name\n",
		},
		{
			name: "each-loop body, exact-name collision with a struct field",
			src:  "each item in Items\n  - var Name = item\np=Name\n",
		},
		{
			name: "each-loop body, case-differing collision with a struct field",
			src:  "each item in Items\n  - var name = item\np=name\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genUnbufferedErr(t, tc.src)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an error (a leaked `- var` collides with a struct field), got nil", tc.src)
			}
		})
	}
}

// TestUnbufferedAssignLeakEmpiricalMatrix is the interpreter-only evidence
// TestCodegenUnbufferedAssignLeakCollisionDeferred's doc comment cites: it
// proves, directly against Compile/Render with no codegen involved, that an
// if-scoped `- var` genuinely leaks past its block (contradicting a
// naive/incorrect assumption that only an each-loop's real frame pop
// matters), and characterizes BOTH collision mechanisms — exact-name
// mutation-in-place, and a case-differing fresh binding that outlives the
// block — so the "not supported yet" deferral above is justified by a
// concrete repro, not a guess.
func TestUnbufferedAssignLeakEmpiricalMatrix(t *testing.T) {
	cases := []struct {
		name string
		src  string
		data map[string]any
		want string
	}{
		{
			name: "if true, exact-name collision: mutates the top-level key in place",
			src:  "if Flag\n  - var Name = \"local\"\np=Name\n",
			data: map[string]any{"Flag": true, "Name": "orig"},
			want: "<p>local</p>",
		},
		{
			name: "if false, exact-name collision: var never assigned, original field value untouched",
			src:  "if Flag\n  - var Name = \"local\"\np=Name\n",
			data: map[string]any{"Flag": false, "Name": "orig"},
			want: "<p>orig</p>",
		},
		{
			name: "if true, case-differing collision: fresh binding leaks, unrelated field untouched",
			src:  "if Flag\n  - var name = \"local\"\np=name\n",
			data: map[string]any{"Flag": true, "Name": "orig"},
			want: "<p>local</p>",
		},
		{
			name: "each-loop, exact-name collision: last iteration's mutation survives past the loop",
			src:  "each item in Items\n  - var Name = item\np=Name\n",
			data: map[string]any{"Items": []any{"a", "b"}, "Name": "orig"},
			want: "<p>b</p>",
		},
		{
			name: "each-loop, case-differing collision: loop-scoped binding does NOT leak (frame really pops), unrelated field untouched",
			src:  "each item in Items\n  - var name = item\np=name\n",
			data: map[string]any{"Items": []any{"a", "b"}, "Name": "orig"},
			want: "<p></p>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Compile(tc.src, nil)
			if err != nil {
				t.Fatalf("Compile(%q): %v", tc.src, err)
			}
			got, err := tmpl.Render(tc.data)
			if err != nil {
				t.Fatalf("Render(%q): %v", tc.src, err)
			}
			if got != tc.want {
				t.Errorf("Render(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestCodegenUnbufferedAssignTopLevelSurvivesFieldCollision proves the
// leaked-name guard does NOT over-reach: a top-level `- var` is never
// scope-restored (no call site does it for the top-level statement list —
// it stays live for the rest of the generated function, exactly like
// Runtime's own root-frame `- var`), so it must still resolve to the LOCAL,
// not a same-named struct field, even when a real field of that exact name
// exists — the real-world headline shape this feature targets (a layout
// computing a string attribute value once, used later in the same
// template) MUST keep working.
func TestCodegenUnbufferedAssignTopLevelSurvivesFieldCollision(t *testing.T) {
	t.Parallel()
	src := "- var Name = \"local\"\np=Name\n"
	runCodegenUnbufferedDifferential(t, codegenUnbufferedCase{
		src:         src,
		data:        map[string]any{"Name": "orig"},
		dataLiteral: `opsData{Name: "orig"}`,
	})
}

// TestCodegenUnbufferedAssignRedeclareDeferred asserts that re-declaring an
// already-bound name — an inner `- var x` where x is already bound, whether
// from an outer `- var` or an each-loop item variable — is rejected with a
// clear, distinct error. Runtime.setVar does not create a fresh, shadowed
// binding in this case: it MUTATES the existing one wherever in the
// interpreter's scopeStack it lives, which does not, in general, correspond
// to Go's own block-scoped `:=` shadowing — so this shape is deferred rather
// than risk emitting code whose visible-value semantics silently disagree
// with the interpreter's.
func TestCodegenUnbufferedAssignRedeclareDeferred(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "re-declaring an outer var inside an if branch",
			src:  "- var x = \"outer\"\nif Flag\n  - var x = \"inner\"\np=x\n",
		},
		{
			name: "re-declaring an each-loop item variable",
			src:  "each x in Items\n  - var x = \"shadow\"\n  p=x\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genUnbufferedErr(t, tc.src)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a re-declaration error, got nil", tc.src)
			}
		})
	}
}

// --- Deferrals ---

// TestCodegenUnbufferedMutationDeferred asserts `x++`/`x--`/`x += e`/
// `x -= e` are each rejected with a clear, distinct "mutation" error rather
// than attempted — a Go string local can't `++`, and a numeric-typing
// decision for these is a separate, later slice.
func TestCodegenUnbufferedMutationDeferred(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "increment", src: "- var x = \"a\"\n- x++\np=x\n"},
		{name: "decrement", src: "- var x = \"a\"\n- x--\np=x\n"},
		{name: "add-assign", src: "- var x = \"a\"\n- x += \"b\"\np=x\n"},
		{name: "sub-assign", src: "- var x = \"a\"\n- x -= \"b\"\np=x\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genUnbufferedErr(t, tc.src)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a mutation-unsupported error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "mutation") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported mutation", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenUnbufferedBareExprDeferred asserts a bare expression statement
// (`- someFunc()` style — the interpreter evaluates and discards it,
// possibly for a side effect or an error) is rejected with a clear error.
func TestCodegenUnbufferedBareExprDeferred(t *testing.T) {
	src := "- Count.toFixed(2)\np ok\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a bare-expression-statement error, got nil", src)
	}
	if !strings.Contains(err.Error(), "bare expression") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported bare expression statement", src, err.Error())
	}
}

// TestCodegenUnbufferedFallibleRHSDeferred asserts a fallible RHS (a
// division that CAN divide by zero) is rejected rather than guessed at — see
// TestUnbufferedAssignDivisionByZeroSwallowed for the empirical finding
// about what the interpreter itself actually does in this case (spoiler:
// unlike a buffered `= a/b`, it swallows the error rather than propagating
// it, storing "" — see that test's doc comment for the reasoning), which is
// exactly why this slice declines to guess rather than trying to replicate
// that surprising swallow-vs-propagate asymmetry.
func TestCodegenUnbufferedFallibleRHSDeferred(t *testing.T) {
	src := "- var x = Count / Zero\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenUnbufferedNonStringRawRHSDeferred asserts a `.split(...)` RHS —
// whose raw stored value is a []any, not a string, diverging from
// Runtime.evaluateExpr's own `.split` case which returns a joined string —
// is rejected rather than guessed at.
func TestCodegenUnbufferedNonStringRawRHSDeferred(t *testing.T) {
	src := "- var parts = Str1.split(\",\")\np=parts\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenUnbufferedNonStringFieldRHSDeferred asserts a bare RHS that
// resolves to a scalar field kind neither this string-local path nor the
// numeric-local classifier (genNumericExpr) supports — a float32 field,
// which genScalarStringify itself has no case for — is still rejected. Every
// OTHER numeric kind (int, the sized int/uint kinds, float64) is now a
// supported numeric local; see codegen_unbuffered_numeric_test.go.
func TestCodegenUnbufferedNonStringFieldRHSDeferred(t *testing.T) {
	src := "- var x = Float32Val\np=x\n"
	err := genUnbufferedErr(t, src)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unsupported-RHS error, got nil", src)
	}
}

// TestCodegenUnbufferedDownstreamNonStringUseDeferred asserts that using a
// codegen-emitted string local in a context that requires a slice/array/map
// (`each` over it, or indexing into it) is rejected: the local is a plain Go
// string, so those positions correctly fail type resolution rather than
// being silently accepted. `.length` on a string local is DELIBERATELY not
// in this list: a string-typed local is resolved exactly like any other
// string field by resolveFieldExpr, so genCondition's existing `.length`
// support (utf8.RuneCountInString) already handles it correctly — not a
// divergence, since the local genuinely IS a Go string.
func TestCodegenUnbufferedDownstreamNonStringUseDeferred(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "each over a string local", src: "- var x = \"a,b\"\neach y in x\n  p=y\n"},
		{name: "index into a string local", src: "- var x = \"ab\"\np=x[0]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := genUnbufferedErr(t, tc.src)
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected an unsupported-use error, got nil", tc.src)
			}
		})
	}
}

// TestCodegenUnbufferedNilReflectTypeDeferred asserts a `- var` assignment
// under a nil Config.DataReflectType (type-blind mode) is rejected: there is
// no type information to prove the RHS is string-shaped at all.
func TestCodegenUnbufferedNilReflectTypeDeferred(t *testing.T) {
	src := "- var x = \"hi\"\np=x\n"
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName: "gopug",
		FuncName:    "RenderOps",
		DataType:    "opsData",
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q) with nil DataReflectType: expected an error, got nil", src)
	}
}

// --- Self-consistency probe ---

// assertRawStringifyInvariant is the self-consistency probe the task's
// bounded-agreement proof rests on: it renders TWO templates against the
// interpreter — one that assigns rhs to a `- var` local and reads it back
// (exercising Runtime.executeStatement's r.evaluateExprRaw(rhs) storage path
// plus Runtime.lookupAndStringify at the read site), and one that evaluates
// rhs directly in the same position (Runtime.evaluateExpr(rhs), the
// reference/interpreter value) — and asserts they render BYTE-IDENTICAL
// output. Since both templates are rendered by the SAME interpreter against
// the SAME data, a mismatch here would prove
// stringify(evaluateExprRaw(rhs)) != evaluateExpr(rhs) directly on the
// interpreter itself, independent of anything codegen does — exactly the
// invariant genUnbufferedAssign's doc comment claims holds for every
// genAssignRHS-supported shape.
func assertRawStringifyInvariant(t *testing.T, rhs string, data map[string]any) {
	t.Helper()

	assignSrc := "- var __probe = " + rhs + "\np=__probe\n"
	directSrc := "p=" + rhs + "\n"

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
		t.Errorf("self-consistency probe failed for rhs %q: assign-and-readback %q != direct evaluateExpr %q (stringify(evaluateExprRaw(rhs)) != evaluateExpr(rhs) on the interpreter itself)", rhs, assignOut, directOut)
	}
}

// TestUnbufferedAssignRawStringifyInvariantSupportedShapes proves the
// bounded-agreement invariant holds, on the interpreter alone, for every RHS
// shape genAssignRHS supports.
func TestUnbufferedAssignRawStringifyInvariantSupportedShapes(t *testing.T) {
	cases := []struct {
		name string
		rhs  string
		data map[string]any
	}{
		{name: "string literal", rhs: `"hello"`, data: map[string]any{}},
		{name: "ternary selecting string literals, true", rhs: `Flag ? "on" : "off"`, data: map[string]any{"Flag": true}},
		{name: "ternary selecting string literals, false", rhs: `Flag ? "on" : "off"`, data: map[string]any{"Flag": false}},
		{name: "string field", rhs: "User.Name", data: map[string]any{"User": map[string]any{"Name": "Ada"}}},
		{name: "string concat", rhs: "Str1 + Str2", data: map[string]any{"Str1": "foo", "Str2": "bar"}},
		{name: "template literal", rhs: "`count: ${Count}`", data: map[string]any{"Count": 5}},
		{name: "|| default, left truthy", rhs: `Name || "anon"`, data: map[string]any{"Name": "Ada"}},
		{name: "|| default, left falsy", rhs: `Name || "anon"`, data: map[string]any{"Name": ""}},
		{name: "&& combinator, left truthy", rhs: "Name && Str1", data: map[string]any{"Name": "Ada", "Str1": "x"}},
		{name: "&& combinator, left falsy", rhs: "Name && Str1", data: map[string]any{"Name": "", "Str1": "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRawStringifyInvariant(t, tc.rhs, tc.data)
		})
	}
}

// TestUnbufferedAssignRawStringifyInvariantFaultInjection proves
// assertRawStringifyInvariant itself is non-vacuous: comparing a shape's
// probe output against a DELIBERATELY WRONG value must fail.
func TestUnbufferedAssignRawStringifyInvariantFaultInjection(t *testing.T) {
	assignSrc := "- var __probe = \"hello\"\np=__probe\n"
	assignTmpl, err := Compile(assignSrc, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", assignSrc, err)
	}
	got, err := assignTmpl.Render(map[string]any{})
	if err != nil {
		t.Fatalf("Render(%q): %v", assignSrc, err)
	}
	wrongWant := "<p>goodbye</p>"
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: probe output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestUnbufferedAssignRawStringifyInvariantFailsForSplit proves the probe
// correctly DETECTS a shape whose invariant does NOT hold — `.split(...)`,
// whose raw stored value is a []any (Go's default slice stringify), while a
// direct `= s.split(",")` goes through Runtime.evaluateExpr's own dedicated
// `.split` case (MethodSplit, a comma-joined string) — so this shape belongs
// in the DEFER set, not the supported set, and this test is the evidence.
func TestUnbufferedAssignRawStringifyInvariantFailsForSplit(t *testing.T) {
	assignSrc := "- var __probe = Str1.split(\",\")\np=__probe\n"
	directSrc := "p=Str1.split(\",\")\n"
	data := map[string]any{"Str1": "a,b,c"}

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

	if assignOut == directOut {
		t.Fatalf(".split(...) was expected to FAIL the raw-vs-stringify invariant (proving it belongs in the DEFER set), but assign-and-readback %q matched direct evaluation %q", assignOut, directOut)
	}
}

// TestUnbufferedAssignDivisionByZeroSwallowed is the empirical finding the
// task requires: unlike a buffered `= a/b` (whose division-by-zero error
// PROPAGATES and aborts Render — see Runtime.Div/evaluateExpr), an
// unbuffered `- var x = a/b` SWALLOWS a division-by-zero. This is because
// Runtime.evaluateExprRaw has no special case for `/`, so it falls through
// to its default `s, _ := r.evaluateExpr(expr); return s` — the underscore
// discards evaluateExpr's error, storing the empty string, and
// Runtime.executeStatement's own assignment branch never observes an error
// either (Runtime.evaluateExprRaw's signature has no error return at all).
// Render succeeds and the variable simply renders empty. This asymmetry is
// exactly why this slice defers a fallible RHS instead of guessing at which
// behavior to replicate.
func TestUnbufferedAssignDivisionByZeroSwallowed(t *testing.T) {
	src := "- var x = Count / Zero\np(data-x=x)= \"after:\" + x\n"
	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	got, err := tmpl.Render(map[string]any{"Count": 10, "Zero": 0})
	if err != nil {
		t.Fatalf("Render(%q): expected the division-by-zero to be SWALLOWED (no error), got: %v", src, err)
	}
	want := `<p data-x="">after:</p>`
	if got != want {
		t.Errorf("Render(%q) = %q, want %q (x stored as the empty string)", src, got, want)
	}

	directSrc := "p=Count / Zero\n"
	directTmpl, err := Compile(directSrc, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", directSrc, err)
	}
	_, err = directTmpl.Render(map[string]any{"Count": 10, "Zero": 0})
	if err == nil {
		t.Fatalf("Render(%q): expected a buffered `= a/b` division-by-zero to PROPAGATE and error, got nil (contradicts the swallow-vs-propagate asymmetry this test documents)", directSrc)
	}
}
