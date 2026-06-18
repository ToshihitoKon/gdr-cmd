package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
)

func Test_splitParentPrefix_SplitsInputIntoParentAndPrefix(t *testing.T) {
	tests := []struct {
		name       string
		toComplete string
		wantParent string
		wantPrefix string
	}{
		{"intermediate directory and prefix", "/dir/fi", "/dir", "fi"},
		{"trailing slash gives empty prefix", "/dir/", "/dir", ""},
		{"prefix directly under root", "/fi", "/", "fi"},
		{"no slash is rooted", "fi", "/", "fi"},
		{"empty string lists everything under root", "", "/", ""},
		{"nested path", "/a/b/c", "/a/b", "c"},
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

func Test_buildCandidate_AppendsTrailingSlashToFolders(t *testing.T) {
	tests := []struct {
		name       string
		parentPath string
		file       drive.File
		want       string
	}{
		{
			name:       "file directly under root",
			parentPath: "/",
			file:       drive.File{Name: "report.pdf", MimeType: "application/pdf"},
			want:       "/report.pdf",
		},
		{
			name:       "folder directly under root",
			parentPath: "/",
			file:       drive.File{Name: "Documents", MimeType: "application/vnd.google-apps.folder"},
			want:       "/Documents/",
		},
		{
			name:       "nested file",
			parentPath: "/Documents",
			file:       drive.File{Name: "memo.txt", MimeType: "text/plain"},
			want:       "/Documents/memo.txt",
		},
		{
			name:       "nested folder",
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

func Test_formatModTime_RecentShowsTimeOldShowsYear(t *testing.T) {
	now := time.Now()

	t.Run("empty string yields hyphen", func(t *testing.T) {
		if got := formatModTime(""); got != "-" {
			t.Errorf("formatModTime(\"\") = %q, want %q", got, "-")
		}
	})

	t.Run("unparsable string is returned as-is", func(t *testing.T) {
		in := "not-a-time"
		if got := formatModTime(in); got != in {
			t.Errorf("formatModTime(%q) = %q, want %q", in, got, in)
		}
	})

	t.Run("recent time includes the time and omits the year", func(t *testing.T) {
		recent := now.Add(-24 * time.Hour).Format(time.RFC3339)
		got := formatModTime(recent)
		if !strings.Contains(got, ":") {
			t.Errorf("recent time has no time component: %q", got)
		}
		if strings.Contains(got, now.Format("2006")) {
			t.Errorf("recent time includes the year: %q", got)
		}
	})

	t.Run("old time includes the year and omits the time", func(t *testing.T) {
		old := now.AddDate(-2, 0, 0)
		got := formatModTime(old.Format(time.RFC3339))
		if !strings.Contains(got, old.Format("2006")) {
			t.Errorf("old time has no year: %q (want year %s)", got, old.Format("2006"))
		}
		if strings.Contains(got, ":") {
			t.Errorf("old time includes the time: %q", got)
		}
	})
}

func Test_humanSize_AddsBase1024Units(t *testing.T) {
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
