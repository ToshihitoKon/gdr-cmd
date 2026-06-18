package loc

import "testing"

func Test_Parse_ClassifiesDriveNotationAndLocal(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		wantKind Kind
		wantPath string
	}{
		{"drive: absolute path", "drive:/Documents/a.pdf", Drive, "/Documents/a.pdf"},
		{"drive: relative is normalized to absolute", "drive:Documents/a.pdf", Drive, "/Documents/a.pdf"},
		{"drive: root", "drive:/", Drive, "/"},
		{"drive: empty is root", "drive:", Drive, "/"},
		{"drive: trailing slash is stripped", "drive:/Documents/", Drive, "/Documents"},
		{"local relative", "./foo", Local, "./foo"},
		{"local absolute", "/tmp/bar", Local, "/tmp/bar"},
		{"local bare name", "foo", Local, "foo"},
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

func Test_ParseDriveDefault_TreatsUnprefixedAsDrive(t *testing.T) {
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
