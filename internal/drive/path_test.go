package drive

import (
	"reflect"
	"testing"
)

func Test_splitPath_変則的な区切りを正規化する(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"ルートは空", "/", nil},
		{"空文字も空", "", nil},
		{"単一要素", "/foo", []string{"foo"}},
		{"複数要素", "/foo/bar/baz", []string{"foo", "bar", "baz"}},
		{"先頭スラッシュ無し", "foo/bar", []string{"foo", "bar"}},
		{"末尾スラッシュは無視", "/foo/bar/", []string{"foo", "bar"}},
		{"連続スラッシュは無視", "/foo//bar", []string{"foo", "bar"}},
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

func Test_joinPath_親パスに子名を連結する(t *testing.T) {
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

func Test_hasMeta_ワイルドカードを検出する(t *testing.T) {
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

func Test_escapeQueryValue_特殊文字をエスケープする(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"通常文字はそのまま", "report.pdf", "report.pdf"},
		{"シングルクォートをエスケープ", "it's mine", `it\'s mine`},
		{"バックスラッシュをエスケープ", `a\b`, `a\\b`},
		{"両方含む", `a'\b`, `a\'\\b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeQueryValue(tt.in); got != tt.want {
				t.Errorf("escapeQueryValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func Test_File_種別判定(t *testing.T) {
	folder := File{MimeType: folderMIME}
	if !folder.IsFolder() {
		t.Error("フォルダMIMEがIsFolder()でtrueにならない")
	}
	if folder.IsGoogleDoc() {
		t.Error("フォルダがIsGoogleDoc()でtrueになっている")
	}

	gdoc := File{MimeType: "application/vnd.google-apps.document"}
	if gdoc.IsFolder() {
		t.Error("Google DocがIsFolder()でtrueになっている")
	}
	if !gdoc.IsGoogleDoc() {
		t.Error("Google DocがIsGoogleDoc()でtrueにならない")
	}

	pdf := File{MimeType: "application/pdf"}
	if pdf.IsFolder() || pdf.IsGoogleDoc() {
		t.Error("通常ファイルが特殊種別と判定されている")
	}
}

func Test_SplitParent_親パスと末尾名に分ける(t *testing.T) {
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
