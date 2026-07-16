package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// typeAwareData is the declared struct codegen_typeaware_test.go's cases
// resolve field types against: one field of each shape the type-aware
// interpolation/condition rules must accept or reject.
type typeAwareData struct {
	Name    string
	Count   int
	Price32 float32
	Author  typeAwareAuthor
	Tags    []string
}

type typeAwareAuthor struct {
	Bio string
}

// TestCodegenInterpolationTypeErrors asserts that #{} interpolation of a
// non-scalar or unsupported-scalar field type (float32, a struct, a slice)
// returns a descriptive error instead of guessing at a stringification the
// interpreter's lookupAndStringify wouldn't produce.
func TestCodegenInterpolationTypeErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "float32 field", src: "p #{Price32}"},
		{name: "struct field", src: "p #{Author}"},
		{name: "slice field", src: "p #{Tags}"},
	}

	typ := reflect.TypeOf(typeAwareData{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderTypeAware",
				DataType:        "typeAwareData",
				DataReflectType: typ,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a type error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenConditionTypeErrors asserts that `if <field>` on a field type
// whose stringify-then-isTruthy truthiness isn't reproducible by a Go
// condition — a slice field, whose interpreter truthiness stringifies to
// something like "[]" (truthy) rather than testing emptiness — is rejected
// rather than emitting a Go if that would diverge from the interpreter. A
// string field is NOT in this list: it routes through the exported
// gopug.Truthy, which reproduces isTruthy's exact falsy set (including a
// string field holding "false" or "0") — see
// TestCodegenConditionLogicStringTruthiness in
// codegen_condition_logic_test.go.
func TestCodegenConditionTypeErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "slice field", src: "if Tags\n  p yes\n"},
	}

	typ := reflect.TypeOf(typeAwareData{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := Parse(tc.src, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			_, err = GenerateGo(ast, Config{
				PackageName:     "gopug",
				FuncName:        "RenderTypeAware",
				DataType:        "typeAwareData",
				DataReflectType: typ,
			})
			if err == nil {
				t.Fatalf("GenerateGo(%q): expected a type error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), "unsupported") {
				t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", tc.src, err.Error())
			}
		})
	}
}

// TestCodegenInterpolationNilReflectType asserts that a nil
// Config.DataReflectType keeps interpolation in its original, type-blind,
// string-assuming form — the back-compat path every pre-increment-2a caller
// relies on.
func TestCodegenInterpolationNilReflectType(t *testing.T) {
	ast, err := Parse("p #{Count}", nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := GenerateGo(ast, Config{
		PackageName: "gopug",
		FuncName:    "RenderTypeAware",
		DataType:    "typeAwareData",
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	if !strings.Contains(string(got), "gopug.EscapeHTML(d.Count)") {
		t.Errorf("GenerateGo with nil DataReflectType did not emit the string-assuming form; got:\n%s", got)
	}
	if strings.Contains(string(got), "strconv") {
		t.Errorf("GenerateGo with nil DataReflectType unexpectedly emitted strconv; got:\n%s", got)
	}
}
