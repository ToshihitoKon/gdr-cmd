package cmd

import (
	"reflect"
	"testing"
	"time"
)

func Test_needsTransfer_DecidesBySizeAndModTime(t *testing.T) {
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	newer := base.Add(time.Hour)
	older := base.Add(-time.Hour)

	tests := []struct {
		name      string
		srcSize   int64
		srcMod    time.Time
		dstExists bool
		dstSize   int64
		dstMod    time.Time
		want      bool
	}{
		{"transfer when destination is missing", 100, base, false, 0, time.Time{}, true},
		{"transfer when sizes differ", 100, base, true, 200, base, true},
		{"transfer when same size and source is newer", 100, newer, true, 100, base, true},
		{"skip when same size and destination is newer", 100, older, true, 100, base, false},
		{"skip when same size and same time", 100, base, true, 100, base, false},
		{"skip sub-second differences", 100, base.Add(500 * time.Millisecond), true, 100, base, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsTransfer(tt.srcSize, tt.srcMod, tt.dstExists, tt.dstSize, tt.dstMod)
			if got != tt.want {
				t.Errorf("needsTransfer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_pathDepth_ReturnsHierarchyDepth(t *testing.T) {
	tests := []struct {
		rel  string
		want int
	}{
		{"a", 1},
		{"a/b", 2},
		{"a/b/c", 3},
	}
	for _, tt := range tests {
		if got := pathDepth(tt.rel); got != tt.want {
			t.Errorf("pathDepth(%q) = %d, want %d", tt.rel, got, tt.want)
		}
	}
}

func Test_sortedKeys_OrdersShallowestThenLexically(t *testing.T) {
	tree := map[string]entry{
		"b/c/d": {},
		"a":     {},
		"b/a":   {},
		"a/z":   {},
		"b":     {},
	}
	got := sortedKeys(tree)
	// Shallowest first, lexical within the same depth. Guarantees parents come
	// before their children.
	want := []string{"a", "b", "a/z", "b/a", "b/c/d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedKeys() = %v, want %v", got, want)
	}
}
