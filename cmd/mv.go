package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var mvCmd = &cobra.Command{
	Use:   "mv drive:SOURCE... drive:DEST",
	Short: "Move or rename files/folders on Drive",
	Long: `Move or rename files/folders on Drive. Both SOURCE and DEST must be
specified with the drive: prefix (Drive-to-Drive operations only).

If DEST is an existing folder, sources are moved into it (names are kept). If
DEST does not exist, a single SOURCE is renamed to that name (and moved to a
different folder if needed). When there are multiple SOURCEs, DEST must be an
existing folder.

Drive moves only update metadata, so no content re-upload occurs. To move
between local and Drive, use cp to copy and then rm.

Examples:
  gdr mv drive:/a.txt drive:/Documents/      # move into a folder
  gdr mv drive:/old.txt drive:/new.txt        # rename
  gdr mv drive:/x.txt drive:/y.txt drive:/box/ # move multiple into a folder`,
	Args:              cobra.MinimumNArgs(2),
	RunE:              runMv,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	rootCmd.AddCommand(mvCmd)
}

func runMv(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Require every argument to be on Drive.
	locs := make([]loc.Location, len(args))
	for i, a := range args {
		locs[i] = loc.Parse(a)
		if !locs[i].IsDrive() {
			return fmt.Errorf("mv only supports Drive-to-Drive operations (add drive:): %s\n"+
				"to move between local and Drive, use cp to copy and then rm", a)
		}
	}

	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	destPath := locs[len(locs)-1].Path
	srcPaths := locs[:len(locs)-1]

	// Resolve and collect the sources.
	var sources []drive.Node
	for _, s := range srcPaths {
		nodes, err := client.Resolve(ctx, s.Path)
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return fmt.Errorf("no match: %s", s)
		}
		sources = append(sources, nodes...)
	}

	// Check whether DEST is an existing folder.
	destFolderID, destIsFolder, err := resolveExistingFolder(ctx, client, destPath)
	if err != nil {
		return err
	}

	if len(sources) > 1 && !destIsFolder {
		return fmt.Errorf("multiple sources given; destination %q must be an existing folder", destPath)
	}

	if destIsFolder {
		return moveIntoFolder(ctx, client, sources, destFolderID, destPath)
	}
	// DEST does not exist -> rename/move a single SOURCE.
	return renameTo(ctx, client, sources[0], destPath)
}

// resolveExistingFolder returns (ID, true, nil) if absPath is an existing
// folder, or ("", false, nil) if it does not exist or is not a folder. Real
// errors (e.g. API failures) propagate as ("", false, err) so they are not
// confused with a plain not-found.
func resolveExistingFolder(ctx context.Context, client *drive.Client, absPath string) (string, bool, error) {
	nodes, err := client.Resolve(ctx, absPath)
	if err != nil {
		if errors.Is(err, drive.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	for _, n := range nodes {
		if n.File.IsFolder() {
			return n.File.ID, true, nil
		}
	}
	return "", false, nil
}

// moveIntoFolder moves sources directly under destFolderID (names are kept).
func moveIntoFolder(ctx context.Context, client *drive.Client, sources []drive.Node, destFolderID, destPath string) error {
	var firstErr error
	for _, src := range sources {
		// Reject moving a folder into itself or one of its descendants, which
		// would create a loop (the Drive API also rejects it, but we give a
		// clearer error up front).
		if src.File.ID == destFolderID || isSelfOrDescendant(src.Path, destPath) {
			err := fmt.Errorf("cannot move into itself or a descendant: %s -> %s", src.Path, destPath)
			fmt.Fprintln(os.Stderr, "mv:", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := client.Move(ctx, src.File.ID, destFolderID, src.ParentID); err != nil {
			fmt.Fprintf(os.Stderr, "mv: %s: %v\n", src.Path, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "Moved: drive:%s -> drive:%s/%s\n", src.Path, destPath, src.File.Name)
	}
	return firstErr
}

// renameTo renames src to destPath. If destPath's parent differs from src's,
// it also moves src.
func renameTo(ctx context.Context, client *drive.Client, src drive.Node, destPath string) error {
	destParentPath, destName := drive.SplitParent(destPath)
	if destName == "" {
		return fmt.Errorf("invalid destination: %s", destPath)
	}

	destParentID, ok, err := resolveExistingFolder(ctx, client, destParentPath)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("destination parent folder not found: %s", destParentPath)
	}

	// Reject moving a folder into itself or a descendant, which would loop.
	if isSelfOrDescendant(src.Path, destPath) {
		return fmt.Errorf("cannot move into itself or a descendant: %s -> %s", src.Path, destPath)
	}

	// Reparent and rename atomically in a single update (no intermediate state).
	newParentID := ""
	if destParentID != src.ParentID {
		newParentID = destParentID
	}
	newName := ""
	if destName != src.File.Name {
		newName = destName
	}
	if newParentID == "" && newName == "" {
		// Same location and same name; nothing to do.
		fmt.Fprintf(os.Stderr, "Moved: no change: drive:%s\n", src.Path)
		return nil
	}

	// If a file/folder with the same name already exists at the destination,
	// Drive would allow a duplicate, but silently creating two looks like an
	// accidental overwrite. Treat it as a collision and error out explicitly
	// (excluding src itself).
	siblings, err := client.ListChildrenByName(ctx, destParentID, destName)
	if err != nil {
		return err
	}
	for _, s := range siblings {
		if s.ID != src.File.ID {
			return fmt.Errorf("destination already has an entry with the same name (overwrite not supported): %s", destPath)
		}
	}

	if err := client.MoveRename(ctx, src.File.ID, newParentID, src.ParentID, newName); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Moved: drive:%s -> drive:%s\n", src.Path, destPath)
	return nil
}

// isSelfOrDescendant returns whether target equals src or is under the src
// folder. Used to prevent loops when moving a folder into itself/a descendant.
func isSelfOrDescendant(srcPath, target string) bool {
	return target == srcPath || strings.HasPrefix(target, srcPath+"/")
}
