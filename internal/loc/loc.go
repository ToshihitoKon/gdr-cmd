// Package loc classifies a CLI path argument as either a Drive-side or a
// local-side path.
//
// Notation:
//   - "drive:/foo/bar" or "drive:foo/bar" is a path on Drive
//   - anything else (e.g. "./foo", "/tmp/bar", "foo") is a local path
//
// This is used by commands like sync and cp, where the transfer direction is
// determined by the arguments, to explicitly tell which side each end refers
// to. A path without the drive: prefix is always treated as local; to make a
// path explicitly local you can write it as "./foo".
package loc

import "strings"

// drivePrefix marks a path as being on the Drive side.
const drivePrefix = "drive:"

// Kind is the kind of location a path points to.
type Kind int

const (
	// Local is a path on the local file system.
	Local Kind = iota
	// Drive is a path on Google Drive.
	Drive
)

// Location is a classified path argument.
type Location struct {
	Kind Kind
	// Path is the "bare" path for each kind.
	//   - Drive: an absolute path rooted at My Drive (normalized to start with "/")
	//   - Local: the input local path as is
	Path string
}

// IsDrive reports whether the path is on Drive.
func (l Location) IsDrive() bool { return l.Kind == Drive }

// IsLocal reports whether the path is local.
func (l Location) IsLocal() bool { return l.Kind == Local }

// String returns a display string that restores the notation (for error messages).
func (l Location) String() string {
	if l.Kind == Drive {
		return drivePrefix + l.Path
	}
	return l.Path
}

// Parse classifies an argument into a Location.
//
// If it has the "drive:" prefix, it is treated as Drive and the following path
// is normalized to start with "/" ("drive:foo" and "drive:/foo" both point to
// the same /foo). Without the prefix, it is treated as local and the input is
// kept as is.
func Parse(arg string) Location {
	if rest, ok := strings.CutPrefix(arg, drivePrefix); ok {
		return Location{Kind: Drive, Path: normalizeDrivePath(rest)}
	}
	return Location{Kind: Local, Path: arg}
}

// ParseDriveDefault treats a path without the prefix as a Drive path.
//
// For commands like ls/cp that keep the legacy behavior of "arguments are Drive
// paths by default". It also accepts the "drive:" prefix, so both the new and
// old notations are allowed. To make a path explicitly local, the caller should
// use Parse instead.
func ParseDriveDefault(arg string) Location {
	if rest, ok := strings.CutPrefix(arg, drivePrefix); ok {
		return Location{Kind: Drive, Path: normalizeDrivePath(rest)}
	}
	return Location{Kind: Drive, Path: normalizeDrivePath(arg)}
}

// HasTrailingSlash reports whether the original argument ends with "/" (i.e. is
// directory-oriented). It only looks at the presence of a trailing slash,
// regardless of the "drive:" prefix or whether it is local. cp/sync use this to
// decide whether to treat the destination as a directory.
func HasTrailingSlash(arg string) bool {
	s := strings.TrimPrefix(arg, drivePrefix)
	return strings.HasSuffix(s, "/")
}

// normalizeDrivePath normalizes a Drive path to start with "/".
// Empty or "." is treated as the root "/". A trailing slash is removed (except
// for the root).
func normalizeDrivePath(p string) string {
	if p == "" || p == "." || p == "/" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// A trailing slash other than the root is unnecessary for resolution, so drop it.
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}
