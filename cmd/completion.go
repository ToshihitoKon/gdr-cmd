package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/spf13/cobra"
)

// completionTimeout bounds the Drive API calls made during Tab completion.
// Completion is interactive, so keep it short to avoid freezing the shell.
const completionTimeout = 3 * time.Second

// completeDrivePath is a ValidArgsFunction that dynamically completes Drive paths.
//
// It splits toComplete (e.g. "/dir/fi") into a parent path and a prefix, then
// returns children directly under the parent folder whose names start with that
// prefix. Folder candidates get a trailing "/" to encourage continued completion.
//
// Completion must never break the shell, so on error it returns no candidates
// plus NoFileComp.
func completeDrivePath(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// When the value contains meta characters, prefix matching against the Drive
	// API is hard, so suppress file completion and return nothing (respecting
	// the user's input).
	if strings.ContainsAny(toComplete, "*?[") {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	parentPath, prefix := splitParentPrefix(toComplete)

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	client, err := drive.New(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Resolve the parent path. It should be a single glob-free folder, but to be
	// safe we include children of every folder among the resolved results.
	parents, err := client.Resolve(ctx, parentPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var candidates []string
	seen := make(map[string]struct{})
	for _, parent := range parents {
		if !parent.File.IsFolder() {
			continue
		}
		children, err := client.ListChildren(ctx, parent.File.ID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, child := range children {
			if !strings.HasPrefix(child.Name, prefix) {
				continue
			}
			cand := buildCandidate(parentPath, child)
			if _, dup := seen[cand]; dup {
				continue
			}
			seen[cand] = struct{}{}
			candidates = append(candidates, cand)
		}
	}

	// Folder candidates already end in "/", so tell the shell not to append a space.
	directive := cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	return candidates, directive
}

// completeLocationArgs wraps completeLocationArg to match cobra's
// ValidArgsFunction signature. The same completion rules apply regardless of
// argument position.
func completeLocationArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeLocationArg(cmd, toComplete)
}

// completeLocationArg completes arguments that support the drive: notation.
//
// If the input starts with "drive:", it dynamically completes a Drive path and
// re-attaches the "drive:" prefix to the candidates. Otherwise it is treated as
// local and deferred to the shell's default file completion. Used by commands
// that can take either side, such as cp/sync/mkdir/rm/mv.
func completeLocationArg(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	const prefix = "drive:"
	if !strings.HasPrefix(toComplete, prefix) {
		// Local path: let the shell complete files.
		return nil, cobra.ShellCompDirectiveDefault
	}
	drivePart := strings.TrimPrefix(toComplete, prefix)
	candidates, directive := completeDrivePath(cmd, nil, drivePart)
	// Re-attach the drive: prefix to the candidates.
	prefixed := make([]string, len(candidates))
	for i, c := range candidates {
		prefixed[i] = prefix + c
	}
	return prefixed, directive
}

// splitParentPrefix splits the completion input into (parent path, last-element prefix).
//
//	"/dir/fi" -> ("/dir", "fi")
//	"/dir/"   -> ("/dir", "")
//	"/fi"     -> ("/",    "fi")
//	"fi"      -> ("/",    "fi")   (a missing leading slash is treated as My Drive root)
//	""        -> ("/",    "")
func splitParentPrefix(toComplete string) (parent, prefix string) {
	idx := strings.LastIndex(toComplete, "/")
	if idx < 0 {
		// No slash: treat it as completion directly under the root.
		return "/", toComplete
	}
	parent = toComplete[:idx]
	if parent == "" {
		parent = "/"
	}
	prefix = toComplete[idx+1:]
	return parent, prefix
}

// buildCandidate builds a completion candidate string from the parent path and
// a child. Folders get a trailing "/".
func buildCandidate(parentPath string, child drive.File) string {
	var b strings.Builder
	if parentPath == "/" {
		b.WriteString("/")
		b.WriteString(child.Name)
	} else {
		b.WriteString(parentPath)
		b.WriteString("/")
		b.WriteString(child.Name)
	}
	if child.IsFolder() {
		b.WriteString("/")
	}
	return b.String()
}
