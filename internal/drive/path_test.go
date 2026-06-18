package drive

import (
	"reflect"
	"testing"
)

func Test_splitPath_NormalizesIrregularSeparators(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"root is empty", "/", nil},
		{"empty string is empty", "", nil},
		{"single element", "/foo", []string{"foo"}},
		{"multiple elements", "/foo/bar/baz", []string{"foo", "bar", "baz"}},
		{"no leading slash", "foo/bar", []string{"foo", "bar"}},
		{"trailing slash is ignored", "/foo/bar/", []string{"foo", "bar"}},
		{"consecutive slashes are ignored", "/foo//bar", []string{"foo", "bar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitPath(tt.in)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitPath(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func Test_joinPath_JoinsChildNameToParentPath(t *testing.T) {
	tests := []struct {
		parent string
		name   string
		want   string
	}{
		{"/", "foo", "/foo"},
		{"/foo", "bar", "/foo/bar"},
		{"/foo/bar", "baz", "/foo/bar/baz"},
	}
	for _, tt := range tests {
		got := joinPath(tt.parent, tt.name)
		if got != tt.want {
			t.Errorf("joinPath(%q, %q) = %q, want %q", tt.parent, tt.name, got, tt.want)
		}
	}
}

func Test_hasMeta_DetectsWildcards(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"plain", false},
		{"with*star", true},
		{"with?question", true},
		{"with[bracket", true},
		{"normal-name.txt", false},
	}
	for _, tt := range tests {
		if got := hasMeta(tt.in); got != tt.want {
			t.Errorf("hasMeta(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func Test_escapeQueryValue_EscapesSpecialCharacters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary characters are unchanged", "report.pdf", "report.pdf"},
		{"single quote is escaped", "it's mine", `it\'s mine`},
		{"backslash is escaped", `a\b`, `a\\b`},
		{"both are escaped", `a'\b`, `a\'\\b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeQueryValue(tt.in); got != tt.want {
				t.Errorf("escapeQueryValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func Test_File_KindDetection(t *testing.T) {
	folder := File{MimeType: folderMIME}
	if !folder.IsFolder() {
		t.Error("folder MIME does not return true from IsFolder()")
	}
	if folder.IsGoogleDoc() {
		t.Error("folder returns true from IsGoogleDoc()")
	}

	gdoc := File{MimeType: "application/vnd.google-apps.document"}
	if gdoc.IsFolder() {
		t.Error("Google Doc returns true from IsFolder()")
	}
	if !gdoc.IsGoogleDoc() {
		t.Error("Google Doc does not return true from IsGoogleDoc()")
	}

	pdf := File{MimeType: "application/pdf"}
	if pdf.IsFolder() || pdf.IsGoogleDoc() {
		t.Error("ordinary file is classified as a special kind")
	}
}

func Test_SplitParent_SplitsIntoParentPathAndName(t *testing.T) {
	tests := []struct {
		in         string
		wantParent string
		wantName   string
	}{
		{"/a/b/c", "/a/b", "c"},
		{"/a", "/", "a"},
		{"/", "", ""},
		{"/Documents/report.pdf", "/Documents", "report.pdf"},
	}
	for _, tt := range tests {
		parent, name := SplitParent(tt.in)
		if parent != tt.wantParent || name != tt.wantName {
			t.Errorf("SplitParent(%q) = (%q, %q), want (%q, %q)",
				tt.in, parent, name, tt.wantParent, tt.wantName)
		}
	}
}
