package gopug

import (
	"reflect"
	"testing"
)

// referenceGetField is a byte-for-byte copy of getField's reflect-only body,
// with no map[string]any type-assertion fast path in front of it. It exists
// only so the differential tests below can prove the fast path never changes
// a single returned value: getField(obj, field) must equal
// referenceGetField(obj, field) for every input, fast-pathed or not.
func referenceGetField(obj any, field string) any {
	if obj == nil {
		return nil
	}

	v := reflect.ValueOf(obj)

	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	if v.Kind() == reflect.Map {
		val := v.MapIndex(reflect.ValueOf(field))
		if val.IsValid() {
			return val.Interface()
		}
	} else if v.Kind() == reflect.Struct {
		if sf, ok := resolveStructField(v.Type(), field); ok {
			fieldVal := v.FieldByName(sf.Name)
			if fieldVal.IsValid() {
				if fieldVal.Kind() == reflect.Ptr {
					if fieldVal.IsNil() {
						return nil
					}
					return fieldVal.Elem().Interface()
				}
				return fieldVal.Interface()
			}
		}
	}

	return nil
}

type getFieldFastPathSample struct{ Name string }

// getFieldFastPathCases is the shared table every differential test below
// runs both getField and referenceGetField against: a present key, an
// absent key, a present key whose stored value is nil, and a spread of
// value types (string/int/bool/slice/map/nested map[string]any/struct/
// pointer) plus an empty map, so the fast path's `m[field]` lookup can't
// silently diverge from the reflect path for any shape it might see.
var getFieldFastPathCases = []struct {
	name  string
	obj   map[string]any
	field string
}{
	{"present string", map[string]any{"name": "Ada"}, "name"},
	{"absent key", map[string]any{"name": "Ada"}, "missing"},
	{"present nil value", map[string]any{"name": nil}, "name"},
	{"present int", map[string]any{"count": 42}, "count"},
	{"present bool true", map[string]any{"ok": true}, "ok"},
	{"present bool false", map[string]any{"ok": false}, "ok"},
	{"present slice", map[string]any{"items": []any{1, 2, 3}}, "items"},
	{"present nested map[string]any", map[string]any{"user": map[string]any{"name": "Bob"}}, "user"},
	{"present map[string]string", map[string]any{"tags": map[string]string{"a": "b"}}, "tags"},
	{"present struct", map[string]any{"row": getFieldFastPathSample{Name: "Carol"}}, "row"},
	{"present pointer", map[string]any{"ptr": &getFieldFastPathSample{Name: "Dana"}}, "ptr"},
	{"present nil pointer", map[string]any{"ptr": (*getFieldFastPathSample)(nil)}, "ptr"},
	{"empty map absent key", map[string]any{}, "anything"},
	{"field name empty string", map[string]any{"": "blank key"}, ""},
	{"field collides with nothing", map[string]any{"a": 1, "b": 2}, "c"},
}

// TestGetFieldFastPathByteIdenticalToReflectPath is the byte-identity proof
// required for the map[string]any fast path: for every case in the table,
// the fast-pathed getField must return the exact same value (via
// reflect.DeepEqual, since the results are `any`) as the unguarded,
// reflect-only reference implementation.
func TestGetFieldFastPathByteIdenticalToReflectPath(t *testing.T) {
	r := &Runtime{}
	for _, tc := range getFieldFastPathCases {
		got := r.getField(tc.obj, tc.field)
		want := referenceGetField(tc.obj, tc.field)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: getField(%#v, %q) = %#v, reference (reflect-only) = %#v", tc.name, tc.obj, tc.field, got, want)
		}
	}
}

// TestGetFieldFastPathNilObjStillNil confirms the pre-existing `obj == nil`
// guard is unchanged and still runs before the new fast path (a nil `any`
// never matches the map[string]any type assertion anyway, but the ordering
// matters for documentation/clarity).
func TestGetFieldFastPathNilObjStillNil(t *testing.T) {
	r := &Runtime{}
	if got := r.getField(nil, "anything"); got != nil {
		t.Errorf("getField(nil, %q) = %#v, want nil", "anything", got)
	}
}

// TestGetFieldFastPathPointerToMapUnaffected confirms a *map[string]any does
// NOT match the map[string]any type assertion and so still falls through to
// the reflect path (which dereferences the pointer) — the fast path must be
// scoped to exactly one concrete type.
func TestGetFieldFastPathPointerToMapUnaffected(t *testing.T) {
	r := &Runtime{}
	m := map[string]any{"name": "Eve"}
	got := r.getField(&m, "name")
	want := referenceGetField(&m, "name")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getField(&map[string]any, %q) = %#v, reference = %#v", "name", got, want)
	}
	if got != "Eve" {
		t.Errorf("getField(&map[string]any, %q) = %#v, want %q", "name", got, "Eve")
	}
}

// TestGetFieldFastPathOtherMapTypesUnaffected confirms a map type other than
// map[string]any (e.g. map[string]string) does not match the fast-path
// assertion and still resolves identically via the reflect path.
func TestGetFieldFastPathOtherMapTypesUnaffected(t *testing.T) {
	r := &Runtime{}

	strMap := map[string]string{"name": "Frank"}
	if got, want := r.getField(strMap, "name"), referenceGetField(strMap, "name"); !reflect.DeepEqual(got, want) {
		t.Errorf("getField(map[string]string, %q) = %#v, reference = %#v", "name", got, want)
	}
}

// TestGetFieldFastPathStructUnaffected confirms a plain struct (and a
// pointer-to-struct) never matches the map[string]any assertion, so struct
// field resolution is untouched by the fast path.
func TestGetFieldFastPathStructUnaffected(t *testing.T) {
	r := &Runtime{}
	s := getFieldFastPathSample{Name: "Grace"}

	if got, want := r.getField(s, "Name"), referenceGetField(s, "Name"); !reflect.DeepEqual(got, want) {
		t.Errorf("getField(struct, %q) = %#v, reference = %#v", "Name", got, want)
	}
	if got, want := r.getField(&s, "Name"), referenceGetField(&s, "Name"); !reflect.DeepEqual(got, want) {
		t.Errorf("getField(*struct, %q) = %#v, reference = %#v", "Name", got, want)
	}
}
