package diff

import (
	"sort"
	"testing"
)

// TestDiffDeletedOrderIndependent locks in the diff behavior across the 5
// documented table cases. `deleted` is collected by iterating a map, whose
// order is random, so it MUST be sorted before comparing; comparing it bare
// with reflect.DeepEqual would flake on CI.
func TestDiffDeletedOrderIndependent(t *testing.T) {
	tests := []struct {
		name      string
		old, new  []string
		wantDel   []string
		wantAdded []string
	}{
		{
			name:      "basic add and delete",
			old:       []string{"a", "b", "c", "d"},
			new:       []string{"c", "e"},
			wantDel:   []string{"a", "b", "d"},
			wantAdded: []string{"e"},
		},
		{
			name:      "duplicate elements",
			old:       []string{"x", "x", "y"},
			new:       []string{"x"},
			wantDel:   []string{"x", "y"},
			wantAdded: []string{},
		},
		{
			name:      "both empty inputs",
			old:       []string{},
			new:       []string{},
			wantDel:   []string{},
			wantAdded: []string{},
		},
		{
			name:      "all added",
			old:       []string{},
			new:       []string{"a", "b"},
			wantDel:   []string{},
			wantAdded: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			del, added := Diff(tt.old, tt.new)

			sortedDel := append([]string(nil), del...)
			sort.Strings(sortedDel)
			sortedWant := append([]string(nil), tt.wantDel...)
			sort.Strings(sortedWant)

			if !equalStrings(sortedDel, sortedWant) {
				t.Errorf("Diff(%v, %v) deleted = %v (sorted %v), want sorted %v", tt.old, tt.new, del, sortedDel, tt.wantDel)
			}
			if !equalStrings(added, tt.wantAdded) {
				t.Errorf("Diff(%v, %v) added = %v, want %v", tt.old, tt.new, added, tt.wantAdded)
			}
			if len(del) != len(tt.wantDel) {
				t.Errorf("Diff(%v, %v) len(deleted) = %d, want %d", tt.old, tt.new, len(del), len(tt.wantDel))
			}
		})
	}
}

// TestDiffOrderIndependenceInt locks in that Diff treats two slices differing
// only in element order as having no differences, using the int instance of
// the generic type parameter.
func TestDiffOrderIndependenceInt(t *testing.T) {
	del, added := Diff([]int{1, 2}, []int{2, 1})
	if len(del) != 0 {
		t.Errorf("Diff([1 2], [2 1]) deleted = %v, want empty", del)
	}
	if len(added) != 0 {
		t.Errorf("Diff([1 2], [2 1]) added = %v, want empty", added)
	}
}

// TestDiffEmptyReturnsNil locks in that Diff([]string{}, []string{}) returns
// literal nil slices, not empty non-nil slices. This comes from the `var`
// zero-value path in the source and is the intentionally locked behavior.
func TestDiffEmptyReturnsNil(t *testing.T) {
	del, added := Diff([]string{}, []string{})
	if del != nil {
		t.Errorf("Diff of empty inputs returned deleted = %v (non-nil), want nil", del)
	}
	if added != nil {
		t.Errorf("Diff of empty inputs returned added = %v (non-nil), want nil", added)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
