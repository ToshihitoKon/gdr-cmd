package drive

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrNotFound indicates that a literal path did not exist during Resolve.
// It is checked with errors.Is to distinguish it from real errors such as API failures.
var ErrNotFound = errors.New("path not found")

// Node is one result of path resolution. It holds a file on Drive and its absolute path.
// A single path expression can resolve to multiple Nodes due to duplicate names or globs.
type Node struct {
	File File
	// Path is the absolute path rooted at My Drive (starting with "/").
	// After glob expansion, it becomes a concrete path made of actual file names.
	Path string
	// ParentID is the ID of the parent folder. Used by mv (reparenting) to specify the old parent.
	// Empty for the Node of the root itself.
	ParentID string
}

// splitPath splits a My Drive-rooted path into its components.
// Leading/trailing slashes and empty components (such as "//") are ignored.
// The root ("/" or "") returns an empty slice.
func splitPath(p string) []string {
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// hasMeta reports whether a path component contains glob meta characters.
func hasMeta(component string) bool {
	return strings.ContainsAny(component, "*?[")
}

// Resolve resolves a My Drive-rooted path expression and returns the matched Nodes.
//
// If the path contains wildcards, candidates are expanded level by level. If
// there is not a single match, an empty slice is returned (not an error). Only
// when a literal path does not exist does it return a NotFound-equivalent error.
func (c *Client) Resolve(ctx context.Context, p string) ([]Node, error) {
	rootID, err := c.RootID(ctx)
	if err != nil {
		return nil, err
	}
	components := splitPath(p)

	// The case where it points to the root itself.
	root := Node{
		File: File{ID: rootID, Name: "", MimeType: folderMIME},
		Path: "/",
	}
	if len(components) == 0 {
		return []Node{root}, nil
	}

	current := []Node{root}
	for i, comp := range components {
		isLast := i == len(components)-1
		next, err := c.expandComponent(ctx, current, comp, isLast)
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			// No match at an intermediate/last component containing a glob means "zero results".
			// If it consists of literals only and disappeared during expansion, it is not found.
			if hasMetaAnywhere(components) {
				return nil, nil
			}
			// Wrap with ErrNotFound so the caller can distinguish "simply does not exist"
			// from real errors such as API failures.
			return nil, fmt.Errorf("%w: %s", ErrNotFound, p)
		}
		current = next
	}
	return current, nil
}

// expandComponent gathers the children directly under each of the current Nodes
// that match comp, and builds the next level of Nodes.
//
// If comp contains meta characters, it lists the children with ListChildren and
// filters them with path.Match; if not, it looks up an exact match with
// ListChildrenByName (which is lighter on the API).
func (c *Client) expandComponent(ctx context.Context, current []Node, comp string, isLast bool) ([]Node, error) {
	var next []Node
	literal := !hasMeta(comp)

	for _, parent := range current {
		// If the parent is not a folder, its contents cannot be traversed (leaves are not expanded).
		if !parent.File.IsFolder() {
			continue
		}

		var children []File
		var err error
		if literal {
			children, err = c.ListChildrenByName(ctx, parent.File.ID, comp)
		} else {
			children, err = c.ListChildren(ctx, parent.File.ID)
		}
		if err != nil {
			return nil, err
		}

		for _, child := range children {
			if !literal {
				matched, merr := path.Match(comp, child.Name)
				if merr != nil {
					return nil, fmt.Errorf("invalid wildcard pattern %q: %w", comp, merr)
				}
				if !matched {
					continue
				}
			}
			next = append(next, Node{
				File:     child,
				Path:     joinPath(parent.Path, child.Name),
				ParentID: parent.File.ID,
			})
		}
	}
	return next, nil
}

// joinPath appends a child component name to a My Drive-rooted path.
func joinPath(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}
	return parent + "/" + name
}

// SplitParent splits a My Drive-rooted absolute path into (the parent folder's
// absolute path, the last component name). Used to decide the upload destination
// or rename target.
//
//	"/a/b/c" -> ("/a/b", "c")
//	"/a"     -> ("/",    "a")
//	"/"      -> ("",     "")   (the root itself has no parent)
func SplitParent(absPath string) (parent, name string) {
	comps := splitPath(absPath)
	if len(comps) == 0 {
		return "", ""
	}
	name = comps[len(comps)-1]
	parentComps := comps[:len(comps)-1]
	if len(parentComps) == 0 {
		return "/", name
	}
	return "/" + strings.Join(parentComps, "/"), name
}

// hasMetaAnywhere reports whether any of the path components contains glob meta characters.
func hasMetaAnywhere(components []string) bool {
	for _, c := range components {
		if hasMeta(c) {
			return true
		}
	}
	return false
}
