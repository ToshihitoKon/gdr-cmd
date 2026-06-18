package loc

import "testing"

func Test_Parse_drive記法とローカルを分類する(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		wantKind Kind
		wantPath string
	}{
		{"drive: 絶対パス", "drive:/Documents/a.pdf", Drive, "/Documents/a.pdf"},
		{"drive: 相対は絶対へ正規化", "drive:Documents/a.pdf", Drive, "/Documents/a.pdf"},
		{"drive: ルート", "drive:/", Drive, "/"},
		{"drive: 空はルート", "drive:", Drive, "/"},
		{"drive: 末尾スラッシュは除去", "drive:/Documents/", Drive, "/Documents"},
		{"ローカル相対", "./foo", Local, "./foo"},
		{"ローカル絶対", "/tmp/bar", Local, "/tmp/bar"},
		{"ローカル素の名前", "foo", Local, "foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.arg)
			if got.Kind != tt.wantKind || got.Path != tt.wantPath {
				t.Errorf("Parse(%q) = {%v %q}, want {%v %q}",
					tt.arg, got.Kind, got.Path, tt.wantKind, tt.wantPath)
			}
		})
	}
}

func Test_ParseDriveDefault_無印もDrive扱い(t *testing.T) {
	tests := []struct {
		arg      string
		wantPath string
	}{
		{"/Documents", "/Documents"},
		{"Documents", "/Documents"},
		{"drive:/Documents", "/Documents"},
		{"/", "/"},
	}
	for _, tt := range tests {
		got := ParseDriveDefault(tt.arg)
		if !got.IsDrive() || got.Path != tt.wantPath {
			t.Errorf("ParseDriveDefault(%q) = {%v %q}, want Drive %q",
				tt.arg, got.Kind, got.Path, tt.wantPath)
		}
	}
}

func Test_HasTrailingSlash(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"drive:/Documents/", true},
		{"drive:/Documents", false},
		{"./local/", true},
		{"./local", false},
		{"drive:/", true},
	}
	for _, tt := range tests {
		if got := HasTrailingSlash(tt.arg); got != tt.want {
			t.Errorf("HasTrailingSlash(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}
