package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// spreadAttrsIntKeyData is a separate declared struct used only to prove the
// non-string-key-map deferral (probe: map[int]any is refused with a distinct
// error from every other &attributes deferral). It is kept apart from
// spreadAttrsData (codegen_spread_attrs_test.go) rather than adding a field
// there, so this file never has to touch that struct or its spliced source
// string.
type spreadAttrsIntKeyData struct {
	AttrsIntKey map[int]any
}

var spreadAttrsIntKeyReflectType = reflect.TypeOf(spreadAttrsIntKeyData{})

// genSpreadIntKeyErr GenerateGoes src against spreadAttrsIntKeyData, always
// returning GenerateGo's error rather than fatally failing the test.
func genSpreadIntKeyErr(t *testing.T, src string) error {
	t.Helper()
	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	_, err = GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsIntKeyData",
		DataReflectType: spreadAttrsIntKeyReflectType,
	})
	return err
}

// TestCodegenSpreadAttrsAnyString proves the headline map[string]any probe:
// a string value spreads through byte-identically to a map[string]string
// spread of the same value (probe 1).
func TestCodegenSpreadAttrsAnyString(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"title": "hello"}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"title": "hello"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsAnyInt proves an int value stringifies via
// fmt.Sprintf("%v", v) identically on both backends (probe 2): %v(5) ==
// "5".
func TestCodegenSpreadAttrsAnyInt(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"count": 5}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"count": 5}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `count="5"`) {
		t.Fatalf("output %q does not exhibit int value %%v-stringified to \"5\"", got)
	}
}

// TestCodegenSpreadAttrsAnyFloatFractional proves a fractional float64 value
// stringifies with Go's default %v float format (probe 3a): %v(1.5) ==
// "1.5".
func TestCodegenSpreadAttrsAnyFloatFractional(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"ratio": 1.5}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"ratio": 1.5}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `ratio="1.5"`) {
		t.Fatalf("output %q does not exhibit float value %%v-stringified to \"1.5\"", got)
	}
}

// TestCodegenSpreadAttrsAnyFloatWhole proves a whole-number float64 value
// drops its trailing ".0" under Go's default %v float format (probe 3b):
// %v(1.0) == "1", not "1.0".
func TestCodegenSpreadAttrsAnyFloatWhole(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"ratio": 1.0}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"ratio": 1.0}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `ratio="1"`) {
		t.Fatalf("output %q does not exhibit whole float value %%v-stringified to \"1\" (with the trailing \".0\" dropped)", got)
	}
}

// TestCodegenSpreadAttrsAnyBoolTrue proves a REAL bool true value (not the
// string "true") hits the exact same bare-attribute branch a
// map[string]string spread's string "true" hits, because both backends
// compare against fmt.Sprintf("%v", true) == "true" (probe 4a).
func TestCodegenSpreadAttrsAnyBoolTrue(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"disabled": true}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"disabled": true}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, " disabled") || strings.Contains(got, `disabled="`) {
		t.Fatalf("output %q does not exhibit a bare \"disabled\" attribute for a real bool true value", got)
	}
}

// TestCodegenSpreadAttrsAnyBoolFalse proves a REAL bool false value (not the
// string "false") DELETES the attribute entirely, even when the tag's own
// base attribute of that name was itself a bare boolean (probe 4b).
func TestCodegenSpreadAttrsAnyBoolFalse(t *testing.T) {
	t.Parallel()
	src := "div(hidden)&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"hidden": false}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"hidden": false}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if strings.Contains(got, "hidden") {
		t.Fatalf("output %q still exhibits \"hidden\" after a real bool false value should have deleted it", got)
	}
}

// TestCodegenSpreadAttrsAnyNil proves a nil map value stringifies to the Go
// zero-interface %v text "<nil>" identically on both backends, and that text
// is then HTML-escaped like any other attribute value (probe 5: "<nil>"
// contains angle brackets, so this also exercises escaping).
func TestCodegenSpreadAttrsAnyNil(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"data-x": nil}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"data-x": nil}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `data-x="&lt;nil&gt;"`) {
		t.Fatalf("output %q does not exhibit a nil value %%v-stringified to \"<nil>\" and HTML-escaped", got)
	}
}

// TestCodegenSpreadAttrsAnyDataAttrInt proves a data-* attribute name with an
// int value renders identically to any other non-"class" name (probe 6).
func TestCodegenSpreadAttrsAnyDataAttrInt(t *testing.T) {
	t.Parallel()
	src := "div&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"data-count": 42}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"data-count": 42}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `data-count="42"`) {
		t.Fatalf("output %q does not exhibit data-count=\"42\"", got)
	}
}

// TestCodegenSpreadAttrsAnyClassMergeString proves an ordinary string
// "class" value in a map[string]any spread merges with a base class exactly
// like the map[string]string path (probe 7a).
func TestCodegenSpreadAttrsAnyClassMergeString(t *testing.T) {
	t.Parallel()
	src := "div.base&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"class": "extra"}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"class": "extra"}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsAnyClassMergeNonString proves a NON-string "class"
// value (an int) is first %v-stringified, then merged exactly like any
// other spread class value — both backends %v the value before the class
// merge ever sees it, so there is no special-casing needed for a
// non-string class value (probe 7b).
func TestCodegenSpreadAttrsAnyClassMergeNonString(t *testing.T) {
	t.Parallel()
	src := "div.base&attributes(AttrsAny)\n"
	data := map[string]any{"AttrsAny": map[string]any{"class": 5}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"class": 5}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `class="base 5"`) {
		t.Fatalf("output %q does not exhibit the base class merged with the %%v-stringified int class value \"5\"", got)
	}
}

// TestCodegenSpreadAttrsAnyEmptyMap proves an empty map[string]any spread
// renders only the tag's own base attributes, no error (probe 8).
func TestCodegenSpreadAttrsAnyEmptyMap(t *testing.T) {
	t.Parallel()
	src := `div(id="x")&attributes(AttrsAny)` + "\n"
	data := map[string]any{"AttrsAny": map[string]any{}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{}}`
	runSpreadDifferential(t, src, data, dataLiteral)
}

// TestCodegenSpreadAttrsAnySortOrder pins the id/class/rest attribute order
// for a map[string]any spread mixing several value kinds — a string id, a
// string class, and int "rest" values — proving sortAttrNames orders the
// merged, already-%v-stringified result exactly like the map[string]string
// path (probe 9).
func TestCodegenSpreadAttrsAnySortOrder(t *testing.T) {
	t.Parallel()
	src := `div(z2="base")&attributes(AttrsAny)` + "\n"
	data := map[string]any{"AttrsAny": map[string]any{"id": "i", "class": "c", "z": 9, "a": "a"}}
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"id": "i", "class": "c", "z": 9, "a": "a"}}`
	got := runSpreadDifferential(t, src, data, dataLiteral)

	if !strings.Contains(got, `id="i" class="c" a="a" z="9" z2="base"`) {
		t.Fatalf("output %q does not exhibit the expected id/class/rest order — this test's own pinned assumption about the runtime sort order is stale", got)
	}
}

// TestCodegenSpreadAttrsAnyBaseClassWhitespaceStillDefers proves the
// pre-existing irregular-base-class-whitespace deferral still fires for a
// map[string]any spread source too, not just map[string]string —
// the check runs in genSpreadBase, which is called after the spread
// source's element kind has already been inspected, but the check itself
// never looks at that element kind, so it is source-type-agnostic by
// construction; this test exercises that directly rather than merely
// asserting it by reading the code.
func TestCodegenSpreadAttrsAnyBaseClassWhitespaceStillDefers(t *testing.T) {
	src := `div(class="a  b")&attributes(AttrsAny)` + "\n"
	err := genSpreadErr(t, src, false)
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
	}
	if !strings.Contains(err.Error(), "leading/trailing or repeated internal whitespace") {
		t.Errorf("GenerateGo error %q does not mention the irregular-whitespace base class deferral", err.Error())
	}
}

// TestCodegenSpreadAttrsAnyDeferrals collects every distinct clean error this
// increment's own scope cut refuses, rather than guessing at.
func TestCodegenSpreadAttrsAnyDeferrals(t *testing.T) {
	t.Run("map[string][]string source (non-scalar value)", func(t *testing.T) {
		src := "div&attributes(AttrsSlice)\n"
		err := genSpreadErr(t, src, false)
		if err == nil {
			t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
		}
		if !strings.Contains(err.Error(), "map[string]string-typed") {
			t.Errorf("GenerateGo error %q does not mention the map[string]string-typed-or-map[string]any-typed deferral", err.Error())
		}
	})

	t.Run("map[int]any source (non-string key)", func(t *testing.T) {
		src := "div&attributes(AttrsIntKey)\n"
		err := genSpreadIntKeyErr(t, src)
		if err == nil {
			t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
		}
		if !strings.Contains(err.Error(), "string-keyed") {
			t.Errorf("GenerateGo error %q does not mention the non-string-key deferral", err.Error())
		}
	})

	t.Run("nil DataReflectType", func(t *testing.T) {
		src := "div&attributes(AttrsAny)\n"
		err := genSpreadErr(t, src, true)
		if err == nil {
			t.Fatalf("GenerateGo(%q): expected a deferral error, got nil", src)
		}
		if !strings.Contains(err.Error(), "DataReflectType") {
			t.Errorf("GenerateGo error %q does not mention the nil-DataReflectType deferral", err.Error())
		}
	})
}

// TestCodegenSpreadAttrsAnyFaultInjection proves the differential harness
// itself is non-vacuous for the map[string]any entry point: a deliberately
// WRONG expected value must fail the comparison.
func TestCodegenSpreadAttrsAnyFaultInjection(t *testing.T) {
	t.Parallel()
	src := "div.base&attributes(AttrsAny)\n"
	dataLiteral := `spreadAttrsData{AttrsAny: map[string]any{"class": "extra"}}`

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderSpread",
		DataType:        "spreadAttrsData",
		DataReflectType: spreadAttrsReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo(%q): %v", src, err)
	}

	got := runComposedGo(t, generated, spreadAttrsDataStructSrc, dataLiteral, "RenderSpread")
	wrongWant := `<div class="wrong"></div>`
	if got == wrongWant {
		t.Fatalf("fault injection did not fault: generated output %q unexpectedly matched the deliberately wrong expectation %q", got, wrongWant)
	}
}

// TestCodegenSpreadAttrsAnyMapStringStringSuiteUnperturbed re-confirms a
// representative slice of the pre-existing map[string]string suite (basic
// spread, class merge, and the bool-true/bool-false branches) after this
// increment's runtime helper refactor (writeSpreadAttrsCore), proving the
// map[string]string entry point (gopug.WriteSpreadAttrs) is unperturbed —
// the full original suite lives in codegen_spread_attrs_test.go and is run
// unchanged alongside this file.
func TestCodegenSpreadAttrsAnyMapStringStringSuiteUnperturbed(t *testing.T) {
	t.Parallel()
	t.Run("basic", func(t *testing.T) {
		src := "div&attributes(Attrs)\n"
		data := map[string]any{"Attrs": map[string]string{"data-x": "1", "role": "btn"}}
		dataLiteral := `spreadAttrsData{Attrs: map[string]string{"data-x": "1", "role": "btn"}}`
		runSpreadDifferential(t, src, data, dataLiteral)
	})
	t.Run("class merge", func(t *testing.T) {
		src := "div.base&attributes(Attrs)\n"
		data := map[string]any{"Attrs": map[string]string{"class": "extra"}}
		dataLiteral := `spreadAttrsData{Attrs: map[string]string{"class": "extra"}}`
		runSpreadDifferential(t, src, data, dataLiteral)
	})
	t.Run("bool true", func(t *testing.T) {
		src := "div&attributes(Attrs)\n"
		data := map[string]any{"Attrs": map[string]string{"disabled": "true"}}
		dataLiteral := `spreadAttrsData{Attrs: map[string]string{"disabled": "true"}}`
		runSpreadDifferential(t, src, data, dataLiteral)
	})
	t.Run("bool false", func(t *testing.T) {
		src := "div(hidden)&attributes(Attrs)\n"
		data := map[string]any{"Attrs": map[string]string{"hidden": "false"}}
		dataLiteral := `spreadAttrsData{Attrs: map[string]string{"hidden": "false"}}`
		runSpreadDifferential(t, src, data, dataLiteral)
	})
}
