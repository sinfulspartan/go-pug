package gopug

import (
	"reflect"
	"testing"
)

// resolveFieldTestStruct exercises resolveStructField's three-tier
// resolution rule: an exact field name, then a `pug:"..."` struct tag, then
// a case-insensitive name match.
type resolveFieldTestStruct struct {
	Exact string
	Snake string `pug:"snake_case"`
	ID    string
	URL   string
}

// resolveFieldPrecedenceStruct has a field whose name exactly matches one
// search target and a second field whose `pug` tag also matches that same
// target, so exact-match precedence (over tag) can be asserted.
type resolveFieldPrecedenceStruct struct {
	Foo string
	Bar string `pug:"Foo"`
}

// resolveFieldTagOverCIStruct has a field that is only a case-insensitive
// match for "foo" (declared first) and a second field, declared after it,
// whose `pug` tag is an exact match for "foo", so tag-over-case-insensitive
// precedence can be asserted regardless of declaration order.
type resolveFieldTagOverCIStruct struct {
	Foo string
	Bar string `pug:"foo"`
}

// resolveFieldDupCIStruct has two fields that are both case-insensitive
// matches for "foo" (Go allows distinct field identifiers that differ only
// in case), so first-in-declaration-order determinism can be asserted.
type resolveFieldDupCIStruct struct {
	Foo string
	FOO string
}

// resolveFieldDashTagStruct has a field whose `pug` tag is literally "-",
// which must never be treated as an explicit mapping for a search name of
// "-" (mirrors encoding/json's "-" being a "skip this field" marker, not a
// literal name).
type resolveFieldDashTagStruct struct {
	Foo string `pug:"-"`
}

// resolveFieldUnexportedStruct has an unexported field (secret) reachable
// only via an exact-name search or a `pug` tag, and an unexported field
// (tagged) reachable only via its tag — proving neither tier ever returns
// an unexported field (reflect.Value.Interface panics on one, and it is
// unreachable from generated code in a separate package anyway).
type resolveFieldUnexportedStruct struct {
	secret string
	tagged string `pug:"visible"`
}

// resolveFieldUnexportedCollisionStruct has an unexported field (foo) that
// case-collides with an exported field (Foo), proving the unexported field
// is skipped in favor of the exported one rather than shadowing it.
type resolveFieldUnexportedCollisionStruct struct {
	foo string
	Foo string
}

func TestResolveStructFieldExactMatch(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldTestStruct{})

	sf, ok := resolveStructField(typ, "Exact")
	if !ok {
		t.Fatalf("resolveStructField(%q): expected a match", "Exact")
	}
	if sf.Name != "Exact" {
		t.Errorf("resolveStructField(%q): got field %q, want %q", "Exact", sf.Name, "Exact")
	}
}

func TestResolveStructFieldPugTag(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldTestStruct{})

	sf, ok := resolveStructField(typ, "snake_case")
	if !ok {
		t.Fatalf("resolveStructField(%q): expected a match", "snake_case")
	}
	if sf.Name != "Snake" {
		t.Errorf("resolveStructField(%q): got field %q, want %q", "snake_case", sf.Name, "Snake")
	}
}

func TestResolveStructFieldCaseInsensitive(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldTestStruct{})

	sf, ok := resolveStructField(typ, "exact")
	if !ok {
		t.Fatalf("resolveStructField(%q): expected a match", "exact")
	}
	if sf.Name != "Exact" {
		t.Errorf("resolveStructField(%q): got field %q, want %q", "exact", sf.Name, "Exact")
	}
}

func TestResolveStructFieldInitialism(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldTestStruct{})

	cases := []struct {
		name string
		want string
	}{
		{"id", "ID"},
		{"url", "URL"},
	}
	for _, tc := range cases {
		sf, ok := resolveStructField(typ, tc.name)
		if !ok {
			t.Fatalf("resolveStructField(%q): expected a match", tc.name)
		}
		if sf.Name != tc.want {
			t.Errorf("resolveStructField(%q): got field %q, want %q", tc.name, sf.Name, tc.want)
		}
	}
}

func TestResolveStructFieldNoMatch(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldTestStruct{})

	if sf, ok := resolveStructField(typ, "doesNotExist"); ok {
		t.Errorf("resolveStructField(%q): expected no match, got field %q", "doesNotExist", sf.Name)
	}
}

func TestResolveStructFieldExactPreferredOverTag(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldPrecedenceStruct{})

	sf, ok := resolveStructField(typ, "Foo")
	if !ok {
		t.Fatalf("resolveStructField(%q): expected a match", "Foo")
	}
	if sf.Name != "Foo" {
		t.Errorf("resolveStructField(%q): got field %q, want the exact-name match %q (not the pug-tagged field)", "Foo", sf.Name, "Foo")
	}
}

func TestResolveStructFieldTagPreferredOverCaseInsensitive(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldTagOverCIStruct{})

	sf, ok := resolveStructField(typ, "foo")
	if !ok {
		t.Fatalf("resolveStructField(%q): expected a match", "foo")
	}
	if sf.Name != "Bar" {
		t.Errorf("resolveStructField(%q): got field %q, want the pug-tagged field %q (not the case-insensitive match %q)", "foo", sf.Name, "Bar", "Foo")
	}
}

func TestResolveStructFieldCaseInsensitiveFirstDeclarationWins(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldDupCIStruct{})

	sf, ok := resolveStructField(typ, "foo")
	if !ok {
		t.Fatalf("resolveStructField(%q): expected a match", "foo")
	}
	if sf.Name != "Foo" {
		t.Errorf("resolveStructField(%q): got field %q, want the first-declared case-insensitive match %q", "foo", sf.Name, "Foo")
	}
}

func TestResolveStructFieldDashTagNotMatchedAsLiteralName(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldDashTagStruct{})

	if sf, ok := resolveStructField(typ, "-"); ok {
		t.Errorf("resolveStructField(%q): expected no match (a \"-\" tag must never be treated as an explicit mapping), got field %q", "-", sf.Name)
	}
}

// TestResolveStructFieldUnexportedExactMatchNotReturned asserts that an
// exact-name match against an UNEXPORTED field is not returned by the
// fast-path tier — reflect.Value.Interface would panic on it, and the
// resolver must never hand back a value that panics its own callers.
func TestResolveStructFieldUnexportedExactMatchNotReturned(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldUnexportedStruct{})

	if sf, ok := resolveStructField(typ, "secret"); ok {
		t.Errorf("resolveStructField(%q): expected no match (the only matching field is unexported), got field %q", "secret", sf.Name)
	}
}

// TestResolveStructFieldUnexportedTagNotReturned asserts that a `pug` tag
// on an UNEXPORTED field is never matched.
func TestResolveStructFieldUnexportedTagNotReturned(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldUnexportedStruct{})

	if sf, ok := resolveStructField(typ, "visible"); ok {
		t.Errorf("resolveStructField(%q): expected no match (the only tagged field is unexported), got field %q", "visible", sf.Name)
	}
}

// TestResolveStructFieldUnexportedCaseInsensitiveNotReturned asserts that a
// case-insensitive match against an UNEXPORTED field is never returned.
func TestResolveStructFieldUnexportedCaseInsensitiveNotReturned(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldUnexportedStruct{})

	if sf, ok := resolveStructField(typ, "SECRET"); ok {
		t.Errorf("resolveStructField(%q): expected no match (the only case-insensitive match is unexported), got field %q", "SECRET", sf.Name)
	}
}

// TestResolveStructFieldUnexportedCollisionPrefersExported asserts that
// when an unexported field case-collides with an exported one, the
// unexported field is skipped and the exported field is returned instead
// of the unexported field silently shadowing it.
func TestResolveStructFieldUnexportedCollisionPrefersExported(t *testing.T) {
	typ := reflect.TypeOf(resolveFieldUnexportedCollisionStruct{})

	sf, ok := resolveStructField(typ, "foo")
	if !ok {
		t.Fatalf("resolveStructField(%q): expected a match", "foo")
	}
	if sf.Name != "Foo" {
		t.Errorf("resolveStructField(%q): got field %q, want the exported field %q (the unexported %q must be skipped)", "foo", sf.Name, "Foo", "foo")
	}
}
