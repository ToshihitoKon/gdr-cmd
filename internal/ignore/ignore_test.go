package ignore

import (
	"strings"
	"testing"
)

func newMatcher(t *testing.T, content string) *Matcher {
	t.Helper()
	m, err := parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return m
}

func Test_Match_BaseNameAtAnyDepth(t *testing.T) {
	m := newMatcher(t, "*.log\n")
	cases := []struct {
		rel  string
		want bool
	}{
		{"a.log", true},
		{"sub/a.log", true},
		{"sub/deep/a.log", true},
		{"a.txt", false},
		{"log", false},
	}
	for _, c := range cases {
		if got := m.Match(c.rel, false); got != c.want {
			t.Errorf("Match(%q)=%v, want %v", c.rel, got, c.want)
		}
	}
}

func Test_Match_AnchoredPathIsRootRelative(t *testing.T) {
	m := newMatcher(t, "build/out\n")
	if !m.Match("build/out", false) {
		t.Error("anchored pattern should match the root-relative path")
	}
	if m.Match("sub/build/out", false) {
		t.Error("anchored pattern should not match a nested path")
	}
}

func Test_Match_LeadingSlashAnchorsBaseName(t *testing.T) {
	m := newMatcher(t, "/tmp\n")
	if !m.Match("tmp", true) {
		t.Error("/tmp should match the root-level tmp")
	}
	if m.Match("sub/tmp", true) {
		t.Error("/tmp should not match a nested tmp")
	}
}

func Test_Match_DirectorySubtreeExcluded(t *testing.T) {
	m := newMatcher(t, "node_modules/\n")
	if !m.Match("node_modules", true) {
		t.Error("the directory itself should be ignored")
	}
	if !m.Match("node_modules/pkg/index.js", false) {
		t.Error("contents under an ignored directory should be ignored")
	}
}

func Test_Match_DirOnlyDoesNotMatchFile(t *testing.T) {
	m := newMatcher(t, "cache/\n")
	if m.Match("cache", false) {
		t.Error("a dir-only pattern must not match a same-named file")
	}
}

func Test_Match_NegationReincludes(t *testing.T) {
	m := newMatcher(t, "*.log\n!keep.log\n")
	if !m.Match("debug.log", false) {
		t.Error("debug.log should stay ignored")
	}
	if m.Match("keep.log", false) {
		t.Error("keep.log should be re-included by the negation")
	}
}

func Test_Match_LastMatchWins(t *testing.T) {
	// Re-exclude after a negation: the last matching pattern decides.
	m := newMatcher(t, "*.log\n!keep.log\nkeep.log\n")
	if !m.Match("keep.log", false) {
		t.Error("the trailing re-exclusion should win over the negation")
	}
}

func Test_Match_CommentsAndBlankLinesIgnored(t *testing.T) {
	m := newMatcher(t, "# comment\n\n  \n*.tmp\n")
	if !m.Match("x.tmp", false) {
		t.Error("*.tmp should be active despite comments and blanks")
	}
	if m.Match("comment", false) {
		t.Error("a comment line must not become a pattern")
	}
}

func Test_Match_TrailingWhitespaceTrimmed(t *testing.T) {
	m := newMatcher(t, "*.bak   \n")
	if !m.Match("x.bak", false) {
		t.Error("trailing whitespace should be trimmed from the pattern")
	}
}

func Test_Load_MissingFileIgnoresNothing(t *testing.T) {
	m, err := Load("/nonexistent/path/.gdrignore")
	if err != nil {
		t.Fatalf("Load of a missing file should not error: %v", err)
	}
	if m.Match("anything", false) {
		t.Error("an empty matcher must ignore nothing")
	}
}
