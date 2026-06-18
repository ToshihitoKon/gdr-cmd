package cmd

import (
	"reflect"
	"testing"
	"time"
)

func Test_needsTransfer_サイズと更新時刻で判定する(t *testing.T) {
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
		{"宛先が無ければ転送", 100, base, false, 0, time.Time{}, true},
		{"サイズが違えば転送", 100, base, true, 200, base, true},
		{"同サイズで元が新しければ転送", 100, newer, true, 100, base, true},
		{"同サイズで宛先が新しければスキップ", 100, older, true, 100, base, false},
		{"同サイズ同時刻はスキップ", 100, base, true, 100, base, false},
		{"秒未満の差は無視してスキップ", 100, base.Add(500 * time.Millisecond), true, 100, base, false},
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

func Test_pathDepth_階層の深さを返す(t *testing.T) {
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

func Test_sortedKeys_浅い順かつ辞書順に並べる(t *testing.T) {
	tree := map[string]entry{
		"b/c/d": {},
		"a":     {},
		"b/a":   {},
		"a/z":   {},
		"b":     {},
	}
	got := sortedKeys(tree)
	// 深さ優先 (浅い順)、同深さは辞書順。親が子より先に来ることを保証する。
	want := []string{"a", "b", "a/z", "b/a", "b/c/d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedKeys() = %v, want %v", got, want)
	}
}
