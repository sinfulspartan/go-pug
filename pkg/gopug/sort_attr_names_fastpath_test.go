package gopug

import (
	"reflect"
	"sort"
	"testing"
)

// referenceSortAttrNames is a byte-for-byte copy of sortAttrNames' original
// sort.Slice-based body. It exists only so the differential tests below can
// prove that swapping to slices.SortFunc never changes the returned
// ordering: sortAttrNames(attrs) must equal referenceSortAttrNames(attrs)
// for every input.
func referenceSortAttrNames(attrs map[string]*AttributeValue) []string {
	names := make([]string, 0, len(attrs))
	for k := range attrs {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		order := func(n string) int {
			switch n {
			case "id":
				return 0
			case "class":
				return 1
			default:
				return 2
			}
		}
		oi, oj := order(names[i]), order(names[j])
		if oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})
	return names
}

// sortAttrNamesFastPathCases is the shared table every differential test
// below runs both sortAttrNames and referenceSortAttrNames against: id/class
// in every interleaving with other attributes, id-only, class-only,
// neither-id-nor-class, a single attribute, many attributes, and names that
// sort around "id"/"class" alphabetically ("idx", "classy", "href",
// "aria-x", "data-y") to prove the bucket logic uses an exact string match
// (not a prefix match) and the tiebreak matches the old `names[i] <
// names[j]` ordering.
var sortAttrNamesFastPathCases = []struct {
	name  string
	attrs map[string]*AttributeValue
}{
	{"empty", map[string]*AttributeValue{}},
	{"single non-id-non-class", map[string]*AttributeValue{"href": {}}},
	{"id only", map[string]*AttributeValue{"id": {}}},
	{"class only", map[string]*AttributeValue{"class": {}}},
	{"id and class only", map[string]*AttributeValue{"id": {}, "class": {}}},
	{"class before id in map, id must sort first", map[string]*AttributeValue{"class": {}, "id": {}}},
	{"neither id nor class, alphabetical", map[string]*AttributeValue{"href": {}, "aria-x": {}, "data-y": {}}},
	{"id, class, and others mixed", map[string]*AttributeValue{
		"href": {}, "id": {}, "aria-x": {}, "class": {}, "data-y": {},
	}},
	{"idx and classy must NOT be bucketed as id/class", map[string]*AttributeValue{
		"idx": {}, "classy": {}, "id": {}, "class": {}, "href": {}, "aria-x": {}, "data-y": {},
	}},
	{"idx alone sorts alphabetically among others", map[string]*AttributeValue{
		"idx": {}, "href": {}, "aria-x": {},
	}},
	{"classy alone sorts alphabetically among others", map[string]*AttributeValue{
		"classy": {}, "href": {}, "data-y": {},
	}},
	{"many attributes, no id or class", map[string]*AttributeValue{
		"aria-x": {}, "data-y": {}, "href": {}, "src": {}, "title": {}, "type": {}, "value": {},
	}},
	{"many attributes with id and class interleaved", map[string]*AttributeValue{
		"zzz": {}, "aaa": {}, "id": {}, "mmm": {}, "class": {}, "bbb": {}, "yyy": {},
	}},
}

// TestSortAttrNamesFastPathByteIdenticalToReferenceSort is the byte-identity
// proof for the slices.SortFunc swap: for every case in the table, the
// refactored sortAttrNames must return the exact same slice as the original
// sort.Slice-based implementation. Map keys are unique by construction, so
// the comparator (order-bucket, then strings.Compare tiebreak) defines a
// strict total order with no ties — both sort implementations are therefore
// guaranteed to produce the identical ordering for identical input.
func TestSortAttrNamesFastPathByteIdenticalToReferenceSort(t *testing.T) {
	for _, tc := range sortAttrNamesFastPathCases {
		got := sortAttrNames(tc.attrs)
		want := referenceSortAttrNames(tc.attrs)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: sortAttrNames(%v) = %v, reference sort.Slice = %v", tc.name, tc.attrs, got, want)
		}
	}
}

// TestSortAttrNamesFastPathExpectedOrder pins the concrete expected ordering
// for the two cases most likely to catch a broadened id/class bucket match:
// "idx"/"classy" must sort alphabetically alongside the other non-bucketed
// names, not be swept into the id/class buckets.
func TestSortAttrNamesFastPathExpectedOrder(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]*AttributeValue
		want  []string
	}{
		{
			"id, class, idx, classy, and alphabetical others",
			map[string]*AttributeValue{
				"idx": {}, "classy": {}, "id": {}, "class": {}, "href": {}, "aria-x": {}, "data-y": {},
			},
			[]string{"id", "class", "aria-x", "classy", "data-y", "href", "idx"},
		},
		{
			"id and class always lead",
			map[string]*AttributeValue{"zzz": {}, "class": {}, "id": {}, "aaa": {}},
			[]string{"id", "class", "aaa", "zzz"},
		},
	}
	for _, tc := range tests {
		got := sortAttrNames(tc.attrs)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: sortAttrNames(%v) = %v, want %v", tc.name, tc.attrs, got, tc.want)
		}
	}
}
