// Package ignore implements .gdrignore matching for sync.
//
// The format follows a practical subset of .gitignore:
//
//   - Blank lines and lines starting with "#" are ignored.
//   - A leading "!" negates a pattern (re-includes a path a prior pattern excluded).
//   - A pattern containing no "/" (other than a trailing one) matches by base name
//     at any depth (e.g. "*.log" matches "a.log" and "sub/a.log").
//   - A pattern with a "/" is anchored to the ignore-file root (e.g. "build/out"
//     matches only "build/out", not "sub/build/out").
//   - A trailing "/" restricts the pattern to directories.
//   - When a directory is ignored, its whole subtree is ignored too.
//
// The last matching pattern wins, so a later "!" can re-include something an
// earlier pattern excluded. Matching is evaluated against slash-separated paths
// relative to the ignore-file root.
package ignore

import (
	"bufio"
	"io"
	"os"
	"path"
	"strings"
)

// Matcher holds the compiled patterns from a .gdrignore file.
type Matcher struct {
	patterns []pattern
}

type pattern struct {
	// glob is the pattern body with any leading "!" and surrounding "/" stripped.
	glob string
	// negate is true for "!" patterns (re-include).
	negate bool
	// dirOnly is true when the pattern ended with "/" (matches directories only).
	dirOnly bool
	// anchored is true when the pattern was anchored to the root (contained a
	// non-trailing "/"), so it matches against the full relative path rather than
	// the base name.
	anchored bool
}

// Load reads a .gdrignore file from the given path. A missing file is not an
// error: it returns an empty matcher that ignores nothing.
func Load(filePath string) (*Matcher, error) {
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Matcher{}, nil
		}
		return nil, err
	}
	defer f.Close()
	return parse(f)
}

func parse(r io.Reader) (*Matcher, error) {
	m := &Matcher{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := pattern{}
		if strings.HasPrefix(line, "!") {
			p.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		// A "/" anywhere (now that any trailing one is stripped) anchors the
		// pattern to the root. A leading "/" only anchors; it is not part of the
		// path to match.
		trimmed := strings.TrimPrefix(line, "/")
		if strings.Contains(trimmed, "/") || strings.HasPrefix(line, "/") {
			p.anchored = true
		}
		p.glob = trimmed
		if p.glob == "" {
			continue
		}
		m.patterns = append(m.patterns, p)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// Match reports whether the relative path (slash-separated, relative to the
// ignore-file root) is ignored. isDir indicates whether the path is a directory,
// which matters for dir-only patterns.
//
// The last matching pattern decides the result, so a later "!" re-includes a
// path that an earlier pattern excluded.
func (m *Matcher) Match(rel string, isDir bool) bool {
	ignored := false
	for _, p := range m.patterns {
		if p.matches(rel, isDir) {
			ignored = !p.negate
		}
	}
	return ignored
}

// matches reports whether the pattern matches rel. isDir applies only to the
// terminal segment (rel itself); any ancestor segment is necessarily a
// directory, so a dir-only pattern still excludes a file deep under a matched
// ancestor directory.
func (p pattern) matches(rel string, isDir bool) bool {
	for i, seg := range ancestors(rel) {
		// Only the deepest segment (i == 0, i.e. rel itself) is constrained by
		// the caller's isDir; ancestors are always directories.
		if p.dirOnly && i == 0 && !isDir {
			continue
		}
		// Anchored patterns match the full ancestor path; unanchored ones match
		// the ancestor's base name at any depth.
		target := seg
		if !p.anchored {
			target = path.Base(seg)
		}
		if ok, _ := path.Match(p.glob, target); ok {
			return true
		}
	}
	return false
}

// ancestors returns rel and each of its ancestor prefixes, deepest first
// (e.g. "a/b/c" -> ["a/b/c", "a/b", "a"]).
func ancestors(rel string) []string {
	var out []string
	for {
		out = append(out, rel)
		parent := path.Dir(rel)
		if parent == "." || parent == "/" || parent == rel {
			break
		}
		rel = parent
	}
	return out
}
