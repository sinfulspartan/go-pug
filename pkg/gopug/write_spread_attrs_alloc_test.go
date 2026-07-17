package gopug

import (
	"io"
	"testing"
)

// TestWriteSpreadAttrsCoreAllocs guards writeSpreadAttrsCore (reached via the
// exported WriteSpreadAttrs, the entry point codegen-generated code calls for
// a runtime `&attributes(...)` spread whose keys are unknown at generate
// time) against its two per-attribute allocation sources: a heap-allocated
// *AttributeValue for every spread entry merged, and a temporary string
// built by `+` concatenation for every attribute written. With one base
// attribute and two spread attributes to merge, sort, and write, the whole
// call should allocate only the merged map itself and the sorted name slice
// sortAttrNames-equivalent ordering produces — never one allocation per
// attribute merged or written.
func TestWriteSpreadAttrsCoreAllocs(t *testing.T) {
	base := map[string]*AttributeValue{"data-id": {Value: "item-1"}}
	spread := map[string]string{"data-index": "0", "role": "listitem"}

	allocs := testing.AllocsPerRun(100, func() {
		if err := WriteSpreadAttrs(io.Discard, base, spread); err != nil {
			t.Fatalf("WriteSpreadAttrs: %v", err)
		}
	})
	if allocs > 2 {
		t.Errorf("WriteSpreadAttrs allocs/run = %v, want <= 2 (merged map + sorted name slice only)", allocs)
	}
}
