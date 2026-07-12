package gopug

import (
	"strings"
	"testing"
)

// This file proves value-context string methods — genMethodCall, wired into
// genValueExpr's dot-handling via findTopLevelDot exactly like
// Runtime.evaluateExpr's own method-dispatch switch (runtime.go). Every
// differential case is checked against Compile().Render (the oracle for
// both rendered output and any error), reusing the
// codegenArithCase/runCodegenArithDifferential machinery
// codegen_arith_test.go established (the harness has no arithmetic-specific
// behavior; codegen_logical_value_test.go already reuses it for a different
// value-context construct the same way).

// TestCodegenMethodTrivialStringMethods proves the six trivial single-
// stdlib-call methods — toUpperCase, toLowerCase, trim, trimLeft, trimRight,
// toString — emit the interpreter's exact stdlib call on the receiver, plus
// every documented alias (toUppercase, toLowercase, trimStart, trimEnd,
// String) produces output identical to its primary spelling.
func TestCodegenMethodTrivialStringMethods(t *testing.T) {
	cases := []codegenArithCase{
		{name: "toUpperCase", src: "p= Name.toUpperCase()\n", data: map[string]any{"Name": "Hello World"}, dataLiteral: `opsData{Name: "Hello World"}`},
		{name: "toUppercase alias", src: "p= Name.toUppercase()\n", data: map[string]any{"Name": "Hello World"}, dataLiteral: `opsData{Name: "Hello World"}`},
		{name: "toLowerCase", src: "p= Name.toLowerCase()\n", data: map[string]any{"Name": "Hello World"}, dataLiteral: `opsData{Name: "Hello World"}`},
		{name: "toLowercase alias", src: "p= Name.toLowercase()\n", data: map[string]any{"Name": "Hello World"}, dataLiteral: `opsData{Name: "Hello World"}`},
		{name: "trim", src: "p= Name.trim()\n", data: map[string]any{"Name": "  padded  "}, dataLiteral: `opsData{Name: "  padded  "}`},
		{name: "trimLeft", src: "p= Name.trimLeft()\n", data: map[string]any{"Name": "  padded  "}, dataLiteral: `opsData{Name: "  padded  "}`},
		{name: "trimStart alias", src: "p= Name.trimStart()\n", data: map[string]any{"Name": "  padded  "}, dataLiteral: `opsData{Name: "  padded  "}`},
		{name: "trimRight", src: "p= Name.trimRight()\n", data: map[string]any{"Name": "  padded  "}, dataLiteral: `opsData{Name: "  padded  "}`},
		{name: "trimEnd alias", src: "p= Name.trimEnd()\n", data: map[string]any{"Name": "  padded  "}, dataLiteral: `opsData{Name: "  padded  "}`},
		{name: "toString", src: "p= Name.toString()\n", data: map[string]any{"Name": "Hello"}, dataLiteral: `opsData{Name: "Hello"}`},
		{name: "String alias", src: "p= Name.String()\n", data: map[string]any{"Name": "Hello"}, dataLiteral: `opsData{Name: "Hello"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenMethodAliasesMatchPrimary proves each alias pair produces
// byte-identical generated-code output to its primary spelling, not merely
// output that separately matches the interpreter.
func TestCodegenMethodAliasesMatchPrimary(t *testing.T) {
	pairs := []struct {
		name      string
		primary   string
		alias     string
		fieldName string
		fieldVal  string
	}{
		{"toUppercase", "toUpperCase", "toUppercase", "Name", "mixedCase"},
		{"toLowercase", "toLowerCase", "toLowercase", "Name", "MixedCase"},
		{"trimStart", "trimLeft", "trimStart", "Name", "  x"},
		{"trimEnd", "trimRight", "trimEnd", "Name", "x  "},
		{"contains", "includes", "contains", "Name", "needle-here"},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			var primarySrc, aliasSrc string
			if p.name == "contains" {
				primarySrc = "p= " + p.fieldName + ".includes('needle')\n"
				aliasSrc = "p= " + p.fieldName + ".contains('needle')\n"
			} else {
				primarySrc = "p= " + p.fieldName + "." + p.primary + "()\n"
				aliasSrc = "p= " + p.fieldName + "." + p.alias + "()\n"
			}
			data := map[string]any{p.fieldName: p.fieldVal}
			dataLiteral := `opsData{` + p.fieldName + `: "` + p.fieldVal + `"}`

			runCodegenArithDifferential(t, codegenArithCase{name: "primary", src: primarySrc, data: data, dataLiteral: dataLiteral})
			runCodegenArithDifferential(t, codegenArithCase{name: "alias", src: aliasSrc, data: data, dataLiteral: dataLiteral})
		})
	}
}

// TestCodegenMethodContexts proves a string method call resolves identically
// in the three write contexts genValueExpr feeds: `#{}` interpolation,
// buffered code (`= expr`), and a dynamic (non-class) attribute value.
func TestCodegenMethodContexts(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "interpolation",
			src:         "p #{Name.toUpperCase()}\n",
			data:        map[string]any{"Name": "world"},
			dataLiteral: `opsData{Name: "world"}`,
		},
		{
			name:        "buffered code",
			src:         "p= Name.trim()\n",
			data:        map[string]any{"Name": "  world  "},
			dataLiteral: `opsData{Name: "  world  "}`,
		},
		{
			name:        "dynamic attribute value",
			src:         "a(data-x=Name.toLowerCase())\n",
			data:        map[string]any{"Name": "WORLD"},
			dataLiteral: `opsData{Name: "WORLD"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenMethodRepeatSplitReplace proves the repeat/split/replace
// non-trivial helpers, including split's quote-strip quirk: a single-quoted
// separator (`split(',')`) and a double-quoted separator (`split(",")`)
// both match the interpreter (and therefore each other).
func TestCodegenMethodRepeatSplitReplace(t *testing.T) {
	cases := []codegenArithCase{
		{name: "repeat", src: "p= Name.repeat(3)\n", data: map[string]any{"Name": "ab"}, dataLiteral: `opsData{Name: "ab"}`},
		{name: "repeat zero", src: "p= Name.repeat(0)\n", data: map[string]any{"Name": "ab"}, dataLiteral: `opsData{Name: "ab"}`},
		{name: "repeat no args", src: "p= Name.repeat()\n", data: map[string]any{"Name": "ab"}, dataLiteral: `opsData{Name: "ab"}`},
		{name: "split single-quoted separator", src: "p= Name.split(',')\n", data: map[string]any{"Name": "a,b,c"}, dataLiteral: `opsData{Name: "a,b,c"}`},
		{name: "split double-quoted separator", src: `p= Name.split(",")` + "\n", data: map[string]any{"Name": "a,b,c"}, dataLiteral: `opsData{Name: "a,b,c"}`},
		{name: "split no args", src: "p= Name.split()\n", data: map[string]any{"Name": "abc"}, dataLiteral: `opsData{Name: "abc"}`},
		{name: "replace", src: "p= Name.replace('a', 'b')\n", data: map[string]any{"Name": "banana"}, dataLiteral: `opsData{Name: "banana"}`},
		{name: "replace no args", src: "p= Name.replace()\n", data: map[string]any{"Name": "banana"}, dataLiteral: `opsData{Name: "banana"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestMethodReplaceAliasedQuoteStripQuirk pins MethodReplace to the ORIGINAL
// (pre-refactor) "replace" case's exact behavior: a shared-variable
// shadowing loop, not an independent per-argument quote-strip. The original
// loop ranges over a fixed two-element snapshot []string{oldArg, newArg} and
// compares each snapshot element s against the CURRENT (possibly
// already-reassigned) oldArg variable — so when the two arguments are the
// same string, or one argument's stripped form happens to equal the other's
// original form, the comparison can alias and leave the second argument
// un-stripped even though it looks quoted. Concretely: oldArg = `"'y'"` (5
// bytes: a double quote, a single-quoted "y", a double quote) is quoted by
// double quotes and strips to `'y'` (3 bytes); newArg's own snapshot value
// is that same `'y'` (3 bytes), so once oldArg has been reassigned to `'y'`
// in the first loop iteration, the second iteration's `s == oldArg` compares
// newArg's ORIGINAL snapshot against oldArg's NEW value — they're equal — so
// the branch reassigns oldArg AGAIN (stripping its single quotes down to a
// bare `y`) instead of the `else` branch that would have stripped newArg.
// newArg is therefore left as `'y'` (still quoted), not the bare `y` a naive
// independent-unquoteArg reading would produce. This is a genuine quirk of
// the original interpreter, not a bug this task is licensed to fix — a
// codegen refactor must reproduce it byte-for-byte, matching whatever both
// engines (which now share this one implementation) still produce.
func TestMethodReplaceAliasedQuoteStripQuirk(t *testing.T) {
	oldArg := `"'y'"`
	newArg := `'y'`
	got := MethodReplace("xyz", oldArg, newArg)
	want := "x'y'z"
	if got != want {
		t.Errorf("MethodReplace(%q, %q, %q) = %q, want %q (the aliased-quote-strip quirk must be reproduced exactly)", "xyz", oldArg, newArg, got, want)
	}
}

// TestCodegenMethodReplaceAliasedQuoteStripQuirkThroughFields proves the
// aliased-quote-strip quirk TestMethodReplaceAliasedQuoteStripQuirk pins
// directly on MethodReplace also reproduces correctly when reached through the
// full pipeline (Pug source -> field lookup -> evaluated argument -> the
// shared MethodReplace call), for BOTH the interpreter and codegen-generated
// code, agreeing with each other exactly because they are the same
// implementation.
func TestCodegenMethodReplaceAliasedQuoteStripQuirkThroughFields(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "aliased quote-strip via field arguments",
		src:         "p= Name.replace(Str1, Str2)\n",
		data:        map[string]any{"Name": "xyz", "Str1": `"'y'"`, "Str2": `'y'`},
		dataLiteral: `opsData{Name: "xyz", Str1: "\"'y'\"", Str2: "'y'"}`,
	})
}

// TestCodegenMethodSplitQuoteStripBothQuoteStylesAgree proves split(',') and
// split(",") produce IDENTICAL generated-code output to each other, not
// merely output that separately matches the interpreter.
func TestCodegenMethodSplitQuoteStripBothQuoteStylesAgree(t *testing.T) {
	data := map[string]any{"Name": "a,b,c"}
	dataLiteral := `opsData{Name: "a,b,c"}`

	singleQuoted := runCodegenMethodOutput(t, "p= Name.split(',')\n", dataLiteral)
	doubleQuoted := runCodegenMethodOutput(t, `p= Name.split(",")`+"\n", dataLiteral)
	if singleQuoted != doubleQuoted {
		t.Errorf("split(',') generated %q but split(\",\") generated %q — expected the quote style to make no difference", singleQuoted, doubleQuoted)
	}

	tmpl, err := Compile("p= Name.split(',')\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want, err := tmpl.Render(data)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}
	if singleQuoted != want {
		t.Errorf("codegen output %q does not match interpreter output %q", singleQuoted, want)
	}
}

// runCodegenMethodOutput is a small helper for
// TestCodegenMethodSplitQuoteStripBothQuoteStylesAgree that only needs the
// generated code's own output, not a fresh differential comparison every
// call.
func runCodegenMethodOutput(t *testing.T, src, dataLiteral string) string {
	t.Helper()
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
	return runGeneratedGo(t, generated, dataLiteral)
}

// TestCodegenMethodQuoteStripAppliesToFieldValuesToo proves unquoteArg's
// quote-strip is applied to an ALREADY-EVALUATED argument, not merely to a
// literal token in the Pug source: a field whose runtime string value
// happens to start and end with a matching quote character has that quote
// character stripped before use, exactly like the interpreter's own
// evaluateExpr(argsStr) + quote-strip sequence does.
func TestCodegenMethodQuoteStripAppliesToFieldValuesToo(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "indexOf needle field value looks quoted",
		src:         "p= Name.indexOf(Str1)\n",
		data:        map[string]any{"Name": "abcxdef", "Str1": `"x"`},
		dataLiteral: `opsData{Name: "abcxdef", Str1: "\"x\""}`,
	})
}

// TestCodegenMethodSlice proves the rune-based slice helpers, including the
// interpreter's exact negative-index (counts back from the end) and
// out-of-range clamp behavior for both the one- and two-argument forms.
func TestCodegenMethodSlice(t *testing.T) {
	data := map[string]any{"Name": "abcdef"}
	dataLiteral := `opsData{Name: "abcdef"}`

	cases := []codegenArithCase{
		{name: "two-arg slice", src: "p= Name.slice(1, 3)\n", data: data, dataLiteral: dataLiteral},
		{name: "one-arg slice", src: "p= Name.slice(2)\n", data: data, dataLiteral: dataLiteral},
		{name: "negative one-arg index", src: "p= Name.slice(-2)\n", data: data, dataLiteral: dataLiteral},
		{name: "negative two-arg start", src: "p= Name.slice(-4, 5)\n", data: data, dataLiteral: dataLiteral},
		{name: "negative two-arg end", src: "p= Name.slice(0, -1)\n", data: data, dataLiteral: dataLiteral},
		{name: "one-arg start past end clamps to empty", src: "p= Name.slice(10)\n", data: data, dataLiteral: dataLiteral},
		{name: "two-arg end past length clamps", src: "p= Name.slice(2, 100)\n", data: data, dataLiteral: dataLiteral},
		{name: "two-arg start after end returns empty", src: "p= Name.slice(4, 1)\n", data: data, dataLiteral: dataLiteral},
		{name: "no args returns receiver unchanged", src: "p= Name.slice()\n", data: data, dataLiteral: dataLiteral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenMethodIndexOfIncludesStartsEndsWith proves the four boolean/
// index-returning helpers, each in both their true/found and false/not-found
// forms, plus the no-argument sentinel each returns.
func TestCodegenMethodIndexOfIncludesStartsEndsWith(t *testing.T) {
	data := map[string]any{"Name": "hello world"}
	dataLiteral := `opsData{Name: "hello world"}`

	cases := []codegenArithCase{
		{name: "indexOf found", src: "p= Name.indexOf('world')\n", data: data, dataLiteral: dataLiteral},
		{name: "indexOf not found", src: "p= Name.indexOf('zzz')\n", data: data, dataLiteral: dataLiteral},
		{name: "indexOf no args", src: "p= Name.indexOf()\n", data: data, dataLiteral: dataLiteral},
		{name: "includes true", src: "p= Name.includes('world')\n", data: data, dataLiteral: dataLiteral},
		{name: "includes false", src: "p= Name.includes('zzz')\n", data: data, dataLiteral: dataLiteral},
		{name: "contains alias true", src: "p= Name.contains('world')\n", data: data, dataLiteral: dataLiteral},
		{name: "includes no args", src: "p= Name.includes()\n", data: data, dataLiteral: dataLiteral},
		{name: "startsWith true", src: "p= Name.startsWith('hello')\n", data: data, dataLiteral: dataLiteral},
		{name: "startsWith false", src: "p= Name.startsWith('world')\n", data: data, dataLiteral: dataLiteral},
		{name: "startsWith no args", src: "p= Name.startsWith()\n", data: data, dataLiteral: dataLiteral},
		{name: "endsWith true", src: "p= Name.endsWith('world')\n", data: data, dataLiteral: dataLiteral},
		{name: "endsWith false", src: "p= Name.endsWith('hello')\n", data: data, dataLiteral: dataLiteral},
		{name: "endsWith no args", src: "p= Name.endsWith()\n", data: data, dataLiteral: dataLiteral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenMethodPad proves the rune-based padStart/padEnd helpers, both
// with an explicit pad character and relying on the default space, and the
// no-op case when the target length is not longer than the receiver.
func TestCodegenMethodPad(t *testing.T) {
	data := map[string]any{"Name": "ab"}
	dataLiteral := `opsData{Name: "ab"}`

	cases := []codegenArithCase{
		{name: "padStart with explicit char", src: "p= Name.padStart(5, '0')\n", data: data, dataLiteral: dataLiteral},
		{name: "padStart default space", src: "p= Name.padStart(5)\n", data: data, dataLiteral: dataLiteral},
		{name: "padEnd with explicit char", src: "p= Name.padEnd(5, '0')\n", data: data, dataLiteral: dataLiteral},
		{name: "padEnd default space", src: "p= Name.padEnd(5)\n", data: data, dataLiteral: dataLiteral},
		{name: "padStart target not longer than receiver is a no-op", src: "p= Name.padStart(1)\n", data: data, dataLiteral: dataLiteral},
		{name: "padStart no args is a no-op", src: "p= Name.padStart()\n", data: data, dataLiteral: dataLiteral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenMethodPadLengthArgWithWhitespace proves the target-length
// argument is TrimSpace'd before being parsed as an integer regardless of
// whether it reaches methodPad through the interpreter (which evaluates the
// Pug source argument at render time) or through codegen (which resolves
// the same field via genValueExpr at generate time): a field whose runtime
// string value carries surrounding whitespace (`" 5"`, as opposed to a
// literal token, whose whitespace is already stripped from the Pug source
// text before either engine ever evaluates it) must still parse to 5 and pad
// on both paths, not silently no-op on one of them.
func TestCodegenMethodPadLengthArgWithWhitespace(t *testing.T) {
	cases := []codegenArithCase{
		{
			name:        "padStart, one-arg, whitespace-padded length field",
			src:         "p= Name.padStart(Str1)\n",
			data:        map[string]any{"Name": "ab", "Str1": " 5"},
			dataLiteral: `opsData{Name: "ab", Str1: " 5"}`,
		},
		{
			name:        "padStart, two-arg, whitespace-padded length field",
			src:         "p= Name.padStart(Str1, '0')\n",
			data:        map[string]any{"Name": "ab", "Str1": " 5"},
			dataLiteral: `opsData{Name: "ab", Str1: " 5"}`,
		},
		{
			name:        "padEnd, one-arg, whitespace-padded length field",
			src:         "p= Name.padEnd(Str1)\n",
			data:        map[string]any{"Name": "ab", "Str1": " 5"},
			dataLiteral: `opsData{Name: "ab", Str1: " 5"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenMethodDotPathReceiver proves the receiver may itself be a
// dot-path (`User.Name.toUpperCase()`), resolved by recursing genValueExpr
// on the receiver text before dispatching the method.
func TestCodegenMethodDotPathReceiver(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "nested struct field receiver",
		src:         "p= User.Name.toUpperCase()\n",
		data:        map[string]any{"User": map[string]any{"Name": "nested"}},
		dataLiteral: `opsData{User: opsUser{Name: "nested"}}`,
	})
}

// TestCodegenMethodChainedCalls proves chained method calls
// (`.trim().toUpperCase()`) work, since the outermost call's receiver is
// itself resolved by the same genMethodCall recursion.
func TestCodegenMethodChainedCalls(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "trim then toUpperCase",
		src:         "p= Name.trim().toUpperCase()\n",
		data:        map[string]any{"Name": "  chained  "},
		dataLiteral: `opsData{Name: "  chained  "}`,
	})
}

// TestCodegenMethodNonStringReceiverStringified proves a non-string field's
// STRINGIFIED value is what the method operates on — `Count.toString()`
// returns the int's own decimal string form, matching
// evaluateExpr(objExpr)'s stringify-first contract.
func TestCodegenMethodNonStringReceiverStringified(t *testing.T) {
	runCodegenArithDifferential(t, codegenArithCase{
		name:        "int field toString",
		src:         "p= Count.toString()\n",
		data:        map[string]any{"Count": 42},
		dataLiteral: `opsData{Count: 42}`,
	})
}

// TestCodegenMethodMultibyte proves slice and padStart operate on RUNE
// boundaries, not byte boundaries, on a receiver with multi-byte characters
// — a plain byte slice/pad would corrupt UTF-8 or count the wrong length.
func TestCodegenMethodMultibyte(t *testing.T) {
	data := map[string]any{"Name": "日本語ABC"}
	dataLiteral := `opsData{Name: "日本語ABC"}`

	cases := []codegenArithCase{
		{name: "slice by rune count", src: "p= Name.slice(1, 3)\n", data: data, dataLiteral: dataLiteral},
		{name: "negative slice by rune count", src: "p= Name.slice(-3)\n", data: data, dataLiteral: dataLiteral},
		{name: "padStart with a multi-byte pad char", src: "p= Name.padStart(8, '語')\n", data: data, dataLiteral: dataLiteral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCodegenArithDifferential(t, tc)
		})
	}
}

// TestCodegenMethodDeferredAndUnknown proves the still-deferred methods
// (join, toFixed, toPrecision) and an unrecognized method name each return a
// clear error from GenerateGo, instead of silently emitting something that
// might not match the interpreter. `.length` (bare property and method-call
// spelling) and index expressions (`arr[0]`) are no longer deferred — see
// codegen_index_length_test.go for their differential coverage — but a
// non-scalar `.length` receiver (a struct-typed field) still errors, since
// genLengthOperand only supports slice/array/map/string.
func TestCodegenMethodDeferredAndUnknown(t *testing.T) {
	cases := []struct {
		name          string
		src           string
		wantSubstring string
	}{
		{name: "toFixed", src: "p= Name.toFixed(2)\n", wantSubstring: "unsupported"},
		{name: "toPrecision", src: "p= Name.toPrecision(3)\n", wantSubstring: "unsupported"},
		{name: "join", src: "p= Items.join(',')\n", wantSubstring: "unsupported"},
		{name: "length on an unsupported (struct) receiver", src: "p= User.length\n", wantSubstring: "unsupported"},
		{name: "unknown method", src: "p= Name.frobnicate()\n", wantSubstring: "unsupported string method"},
		{name: "fallible receiver", src: "p= (Count / Zero).toUpperCase()\n", wantSubstring: "fallible"},
		{name: "fallible argument", src: "p= Name.repeat(Count / Zero)\n", wantSubstring: "fallible"},
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
				t.Fatalf("GenerateGo(%q): expected an error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantSubstring) {
				t.Errorf("GenerateGo(%q): error %q does not contain %q", tc.src, err.Error(), tc.wantSubstring)
			}
		})
	}
}

// TestCodegenMethodUnknownMatchesInterpreterErrorFamily proves that when
// GenerateGo rejects an unknown method call on a string receiver, the
// message names the same failure Runtime.evaluateExpr's own "unsupported
// string method" fallback reports for the identical template.
func TestCodegenMethodUnknownMatchesInterpreterErrorFamily(t *testing.T) {
	src := "p= Name.frobnicate()\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, genErr := GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderOps",
		DataType:        "opsData",
		DataReflectType: opsDataReflectType,
	})
	if genErr == nil {
		t.Fatalf("GenerateGo(%q): expected an error, got nil", src)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	_, interpErr := tmpl.Render(map[string]any{"Name": "hi"})
	if interpErr == nil {
		t.Fatalf("interpreter Render(%q): expected an error, got nil", src)
	}

	if !strings.Contains(genErr.Error(), "unsupported string method") {
		t.Errorf("codegen error %q does not describe an unsupported string method", genErr.Error())
	}
	if !strings.Contains(interpErr.Error(), "unsupported string method") {
		t.Errorf("interpreter error %q does not describe an unsupported string method", interpErr.Error())
	}
}
