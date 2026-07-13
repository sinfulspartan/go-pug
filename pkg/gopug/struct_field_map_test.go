package gopug

import (
	"strings"
	"testing"
)

// structFieldMapOwner is a nested struct whose only field, Name, is reached
// through a second dot-path segment (firm.owner.name) that must itself be
// case-insensitively resolved.
type structFieldMapOwner struct {
	Name string
}

// structFieldMapTaskPreview mirrors a real Track-B struct: an EXPORTED
// PascalCase field reached by a lowercase Pug identifier.
type structFieldMapTaskPreview struct {
	AdjusterName string
}

// structFieldMapFirm has an initialism field (ID), a `pug`-tagged field
// (FirmID), a nested struct field (Owner), and a pointer-to-struct field
// (Manager) — every shape the resolver rule must handle at struct-field
// depth beyond the root.
type structFieldMapFirm struct {
	ID      int
	FirmID  string `pug:"firm_id"`
	Owner   structFieldMapOwner
	Manager *structFieldMapOwner
}

// structFieldMapUnexported has an unexported field (secret) reachable only
// by exact name or case-insensitive match, and an unexported `pug`-tagged
// field (tagged) — proving neither the exact-name, tag, nor
// case-insensitive tier ever hands back a value obtained from an unexported
// field, which would panic reflect.Value.Interface.
type structFieldMapUnexported struct {
	secret string
	tagged string `pug:"visible"`
}

// structFieldMapCaseCollision has an unexported field (foo) that
// case-collides with an exported field (Foo), proving the exported field
// wins rather than the unexported one silently shadowing it.
type structFieldMapCaseCollision struct {
	foo string
	Foo string
}

// TestStructFieldMapLowercaseLocalBindsToExportedField asserts that a Pug
// template accessing a lowercase local (taskPreview.AdjusterName) against a
// struct value whose Go field is EXPORTED PascalCase (TaskPreview) now
// renders that value, where it previously rendered "" because the
// interpreter's getField only tried an exact-name, case-sensitive
// FieldByName.
func TestStructFieldMapLowercaseLocalBindsToExportedField(t *testing.T) {
	tmpl, err := Compile("p= taskPreview.AdjusterName\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"taskPreview": structFieldMapTaskPreview{AdjusterName: "Jane Doe"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p>Jane Doe</p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapInitialismBindsToExportedField asserts that a Pug
// template's "id" identifier binds to a struct's Go initialism field "ID".
func TestStructFieldMapInitialismBindsToExportedField(t *testing.T) {
	tmpl, err := Compile("p= firm.id\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"firm": structFieldMapFirm{ID: 42},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p>42</p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapPugTagBindsToExportedField asserts that a Pug
// template's snake_case identifier binds to a struct field via its
// `pug:"..."` tag.
func TestStructFieldMapPugTagBindsToExportedField(t *testing.T) {
	tmpl, err := Compile("p= firm.firm_id\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"firm": structFieldMapFirm{FirmID: "F-100"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p>F-100</p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapNestedStructField asserts that a two-level dot-path
// (firm.owner.name) resolves at BOTH segments (Owner, then Name) against
// nested struct values.
func TestStructFieldMapNestedStructField(t *testing.T) {
	tmpl, err := Compile("p= firm.owner.name\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"firm": structFieldMapFirm{Owner: structFieldMapOwner{Name: "Acme"}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p>Acme</p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapPointerField asserts that a pointer-to-struct field
// (Manager) reached through a case-insensitively resolved identifier still
// dereferences correctly, both when non-nil and when nil.
func TestStructFieldMapPointerField(t *testing.T) {
	tmpl, err := Compile("p= firm.manager.name\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"firm": structFieldMapFirm{Manager: &structFieldMapOwner{Name: "Bob"}},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p>Bob</p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapNilPointerFieldRendersEmpty asserts that a nil pointer
// field reached through the resolver still renders as empty rather than
// panicking or producing a nil-dereference error, preserving getField's
// existing pointer-field-nil-safety for a resolved (not just exact-name)
// field.
func TestStructFieldMapNilPointerFieldRendersEmpty(t *testing.T) {
	tmpl, err := Compile("p=firm.manager\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"firm": structFieldMapFirm{Manager: nil},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p></p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapExactNameStillWorks asserts that the pre-existing
// exact-name, case-sensitive access path is unaffected by the new
// resolution tiers.
func TestStructFieldMapExactNameStillWorks(t *testing.T) {
	tmpl, err := Compile("p= firm.ID\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"firm": structFieldMapFirm{ID: 7},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p>7</p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapUnresolvableFieldRendersEmpty asserts that an
// identifier which resolves to no field under any of the three tiers still
// renders empty, exactly like the pre-existing exact-name-only miss case.
func TestStructFieldMapUnresolvableFieldRendersEmpty(t *testing.T) {
	tmpl, err := Compile("p=firm.doesNotExist\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"firm": structFieldMapFirm{},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p></p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
	if strings.Contains(got, "doesNotExist") {
		t.Errorf("Render: unexpectedly echoed the unresolvable field name into output: %q", got)
	}
}

// TestStructFieldMapUnexportedCaseCollisionRendersEmptyNotPanic asserts
// that when a struct's only case-insensitive match for a Pug identifier is
// an UNEXPORTED field, the template renders "" instead of the resolver
// handing getField a value whose Interface() call would panic.
func TestStructFieldMapUnexportedCaseCollisionRendersEmptyNotPanic(t *testing.T) {
	tmpl, err := Compile("p=obj.SECRET\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"obj": structFieldMapUnexported{secret: "hidden"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p></p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapUnexportedTaggedFieldRendersEmptyNotPanic asserts that
// a `pug` tag on an UNEXPORTED field is never matched, so accessing it by
// its tag name renders "" instead of panicking.
func TestStructFieldMapUnexportedTaggedFieldRendersEmptyNotPanic(t *testing.T) {
	tmpl, err := Compile("p=obj.visible\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"obj": structFieldMapUnexported{tagged: "hidden"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p></p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapUnexportedExactNameRendersEmptyNotPanic asserts that an
// exact-name access against a field that is literally unexported (obj.secret
// where the Go field is unexported "secret") renders "" instead of
// panicking — this is a pre-existing panic in the fast path that this
// resolver change also fixes: FieldByName("secret") finds the unexported
// field, and calling .Interface() on it panics with "reflect.Value.Interface:
// cannot return value obtained from unexported field or method"; the fix
// treats an unexported exact match as a miss, exactly like the tag/CI
// tiers, rather than returning it.
func TestStructFieldMapUnexportedExactNameRendersEmptyNotPanic(t *testing.T) {
	tmpl, err := Compile("p=obj.secret\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"obj": structFieldMapUnexported{secret: "hidden"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p></p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}

// TestStructFieldMapUnexportedCollisionResolvesToExported asserts that when
// a struct has both an unexported field (foo) and an exported field (Foo)
// that case-collide, a Pug identifier matching the unexported field's name
// resolves to the EXPORTED field instead — the unexported field is skipped
// entirely, never shadowing the exported one.
func TestStructFieldMapUnexportedCollisionResolvesToExported(t *testing.T) {
	tmpl, err := Compile("p=obj.foo\n", nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	got, err := tmpl.Render(map[string]any{
		"obj": structFieldMapCaseCollision{foo: "hidden", Foo: "visible"},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<p>visible</p>"; got != want {
		t.Errorf("Render: got %q, want %q", got, want)
	}
}
