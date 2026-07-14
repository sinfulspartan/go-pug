package gopug

import (
	"reflect"
	"strings"
	"testing"
)

// fieldMapTaskPreview is fieldMapData.TaskPreview's type: an exported
// PascalCase field (AdjusterName) reached through a nested dot-path segment
// that a lowercase Pug identifier (taskPreview.AdjusterName) must resolve
// to.
type fieldMapTaskPreview struct {
	AdjusterName string
}

// fieldMapData is the struct codegen_fieldmap_test.go's differential tests
// resolve field paths against: every EXPORTED field shape the Pug-identifier
// to Go-field mapping rule must handle — a PascalCase field reached by its
// lowercase form (TaskPreview), a `pug`-tagged field reached by its tag
// (FirmID via "firm_id"), and a Go initialism field reached by its
// lowercase form (ID via "id") — proving the mapping unblocks EXPORTED
// (cross-package-viable) struct fields, not just the unexported same-case
// fields codegen already supported.
type fieldMapData struct {
	TaskPreview fieldMapTaskPreview
	FirmID      string `pug:"firm_id"`
	ID          int
}

var fieldMapDataReflectType = reflect.TypeOf(fieldMapData{})

// fieldMapUnexportedData has an unexported field (secret) with no exported
// counterpart of any resolvable name/tag/case, used to prove
// resolveFieldExpr never resolves a Pug identifier onto an unexported Go
// field — resolveStructField reports no match for it, exactly as it would
// for any other nonexistent field, so GenerateGo returns its ordinary
// unresolvable-field error rather than emitting a d.secret reference that
// would neither compile across packages nor compile at all if secret were
// literally unaddressable outside its own package.
type fieldMapUnexportedData struct {
	secret string
}

var fieldMapUnexportedDataReflectType = reflect.TypeOf(fieldMapUnexportedData{})

// fieldMapDataStructSrc is fieldMapData's (and fieldMapTaskPreview's) field
// declarations, reused verbatim by buildFieldMapGo/runFieldMapGo to assemble
// a standalone, compilable Go source file around a GenerateGo result — it
// must match the fieldMapData struct above field for field, including the
// `pug` tag.
const fieldMapDataStructSrc = `type fieldMapTaskPreview struct {
	AdjusterName string
}

type fieldMapData struct {
	TaskPreview fieldMapTaskPreview
	FirmID      string ` + "`pug:\"firm_id\"`" + `
	ID          int
}
`

// TestCodegenFieldMapUnexportedFieldNeverResolves asserts that a Pug
// identifier whose only matching Go field is unexported is rejected by
// GenerateGo with the ordinary unresolvable-field error, never resolved to
// (and emitted as a reference to) the unexported field.
func TestCodegenFieldMapUnexportedFieldNeverResolves(t *testing.T) {
	src := "p= secret\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	_, err = GenerateGo(ast, Config{
		PackageName:     "gopug",
		FuncName:        "RenderFieldMap",
		DataType:        "fieldMapUnexportedData",
		DataReflectType: fieldMapUnexportedDataReflectType,
	})
	if err == nil {
		t.Fatalf("GenerateGo(%q): expected an unresolvable-field error, got nil", src)
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("GenerateGo(%q): error %q does not describe an unsupported construct", src, err.Error())
	}
}

// TestCodegenFieldMapEmitsResolvedGoFieldNames asserts that resolveFieldExpr
// emits the RESOLVED Go field name for a lowercase/tagged Pug identifier
// (d.TaskPreview.AdjusterName, d.FirmID, d.ID), not the verbatim Pug
// identifier (which would not compile against an exported-field struct).
func TestCodegenFieldMapEmitsResolvedGoFieldNames(t *testing.T) {
	src := "p= taskPreview.AdjusterName\np= firm_id\np= id\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderFieldMap",
		DataType:        "fieldMapData",
		DataReflectType: fieldMapDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	genStr := string(got)
	for _, want := range []string{"d.TaskPreview.AdjusterName", "d.FirmID", "d.ID"} {
		if !strings.Contains(genStr, want) {
			t.Errorf("GenerateGo output does not contain resolved Go field expression %q; got:\n%s", want, genStr)
		}
	}
	for _, unwanted := range []string{"d.taskPreview", "d.firm_id", "d.id"} {
		if strings.Contains(genStr, unwanted) {
			t.Errorf("GenerateGo output unexpectedly contains verbatim (unresolved) Pug identifier %q; got:\n%s", unwanted, genStr)
		}
	}
}

// TestCodegenFieldMapDifferentialMatchesInterpreter is the bounded-agreement
// proof for the struct field-name mapping rule: a template that accesses a
// lowercase local through a nested struct field (taskPreview.AdjusterName),
// a `pug`-tagged field (firm_id), and a Go initialism field (id) — against a
// fieldMapData value with EXPORTED PascalCase fields — produces
// byte-identical output between the compiled/run generated Go and the
// interpreter's Compile().Render() called against an equivalent MAP whose
// keys are the same lowercase/tagged Pug identifiers (map access is
// unaffected by this change — a map key is always a literal string — so the
// map-keyed render is the correct oracle for what the STRUCT-keyed
// generated code and STRUCT-keyed interpreter render should also produce).
// The generated code is compiled against fieldMapData's EXPORTED fields,
// proving the resolved-name mapping is cross-package viable (an unexported
// field would be unreachable from a generated render package built
// separately from the data-model package).
func TestCodegenFieldMapDifferentialMatchesInterpreter(t *testing.T) {
	t.Parallel()
	src := "p= taskPreview.AdjusterName\np= firm_id\np= id\n"

	ast, err := Parse(src, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	generated, err := GenerateGo(ast, Config{
		PackageName:     "main",
		FuncName:        "RenderFieldMap",
		DataType:        "fieldMapData",
		DataReflectType: fieldMapDataReflectType,
	})
	if err != nil {
		t.Fatalf("GenerateGo: %v", err)
	}

	tmpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	structData := fieldMapData{
		TaskPreview: fieldMapTaskPreview{AdjusterName: "Jane Doe"},
		FirmID:      "F-100",
		ID:          42,
	}
	mapData := map[string]any{
		"taskPreview": map[string]any{"AdjusterName": structData.TaskPreview.AdjusterName},
		"firm_id":     structData.FirmID,
		"id":          structData.ID,
	}

	want, err := tmpl.Render(mapData)
	if err != nil {
		t.Fatalf("interpreter Render: %v", err)
	}

	dataLiteral := `fieldMapData{TaskPreview: fieldMapTaskPreview{AdjusterName: "Jane Doe"}, FirmID: "F-100", ID: 42}`
	results := runDifferentialBatch(t, fieldMapDataStructSrc, "RenderFieldMap", []diffCase{
		{name: "field map differential", generated: generated, dataLiteral: dataLiteral},
	})
	result := results[0]
	if result.Err != "" {
		t.Fatalf("generated RenderFieldMap: unexpected error %q", result.Err)
	}
	got := result.Out

	if got != want {
		t.Errorf("codegen output %q does not match interpreter output %q for template %q", got, want, src)
	}

	// Also prove the interpreter itself resolves the equivalent EXPORTED
	// struct value (not just the map above) to the same output, so the
	// three-way agreement (struct interpreter, map interpreter, codegen)
	// all matches.
	structWant, err := tmpl.Render(map[string]any{
		"taskPreview": structData.TaskPreview,
		"firm_id":     structData.FirmID,
		"id":          structData.ID,
	})
	if err != nil {
		t.Fatalf("interpreter Render against nested struct value: %v", err)
	}
	if structWant != want {
		t.Errorf("interpreter output against a nested struct value %q does not match its map-keyed equivalent %q", structWant, want)
	}
}
