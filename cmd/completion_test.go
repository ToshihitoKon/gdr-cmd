package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
)

func Test_splitParentPrefix_補完入力を親と接頭辞に分ける(t *testing.T) {
	tests := []struct {
		name       string
		toComplete string
		wantParent string
		wantPrefix string
	}{
		{"中間ディレクトリと接頭辞", "/dir/fi", "/dir", "fi"},
		{"末尾スラッシュは接頭辞空", "/dir/", "/dir", ""},
		{"ルート直下の接頭辞", "/fi", "/", "fi"},
		{"スラッシュ無しはルート起点", "fi", "/", "fi"},
		{"空文字はルートの全件", "", "/", ""},
		{"ネストしたパス", "/a/b/c", "/a/b", "c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent, prefix := splitParentPrefix(tt.toComplete)
			if parent != tt.wantParent || prefix != tt.wantPrefix {
				t.Errorf("splitParentPrefix(%q) = (%q, %q), want (%q, %q)",
					tt.toComplete, parent, prefix, tt.wantParent, tt.wantPrefix)
			}
		})
	}
}

func Test_buildCandidate_フォルダには末尾スラッシュを付ける(t *testing.T) {
	tests := []struct {
		name       string
		parentPath string
		file       drive.File
		want       string
	}{
		{
			name:       "ルート直下のファイル",
			parentPath: "/",
			file:       drive.File{Name: "report.pdf", MimeType: "application/pdf"},
			want:       "/report.pdf",
		},
		{
			name:       "ルート直下のフォルダ",
			parentPath: "/",
			file:       drive.File{Name: "Documents", MimeType: "application/vnd.google-apps.folder"},
			want:       "/Documents/",
		},
		{
			name:       "ネストしたファイル",
			parentPath: "/Documents",
			file:       drive.File{Name: "memo.txt", MimeType: "text/plain"},
			want:       "/Documents/memo.txt",
		},
		{
			name:       "ネストしたフォルダ",
			parentPath: "/Documents",
			file:       drive.File{Name: "sub", MimeType: "application/vnd.google-apps.folder"},
			want:       "/Documents/sub/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildCandidate(tt.parentPath, tt.file); got != tt.want {
				t.Errorf("buildCandidate(%q, %v) = %q, want %q", tt.parentPath, tt.file.Name, got, tt.want)
			}
		})
	}
}

func Test_formatModTime_最近は時刻_古い日付は年を出す(t *testing.T) {
	now := time.Now()

	t.Run("空文字はハイフン", func(t *testing.T) {
		if got := formatModTime(""); got != "-" {
			t.Errorf("formatModTime(\"\") = %q, want %q", got, "-")
		}
	})

	t.Run("解析不能な文字列はそのまま返す", func(t *testing.T) {
		in := "not-a-time"
		if got := formatModTime(in); got != in {
			t.Errorf("formatModTime(%q) = %q, want %q", in, got, in)
		}
	})

	t.Run("最近の日時は時刻を含み年を含まない", func(t *testing.T) {
		recent := now.Add(-24 * time.Hour).Format(time.RFC3339)
		got := formatModTime(recent)
		if !strings.Contains(got, ":") {
			t.Errorf("最近の日時に時刻が含まれない: %q", got)
		}
		if strings.Contains(got, now.Format("2006")) {
			t.Errorf("最近の日時に年が含まれている: %q", got)
		}
	})

	t.Run("古い日時は年を含み時刻を含まない", func(t *testing.T) {
		old := now.AddDate(-2, 0, 0)
		got := formatModTime(old.Format(time.RFC3339))
		if !strings.Contains(got, old.Format("2006")) {
			t.Errorf("古い日時に年が含まれない: %q (want year %s)", got, old.Format("2006"))
		}
		if strings.Contains(got, ":") {
			t.Errorf("古い日時に時刻が含まれている: %q", got)
		}
	})
}

func Test_humanSize_1024進で単位を付ける(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{1073741824, "1.0GB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.bytes); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}
