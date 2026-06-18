package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var mkdirParents bool

var mkdirCmd = &cobra.Command{
	Use:   "mkdir drive:PATH...",
	Short: "Create a folder on Drive",
	Long: `Create a folder on Drive. Specify the path with the drive: prefix.

By default the parent folder must already exist. With -p, parent folders are
created too and an existing path is not an error (same as mkdir -p).

Examples:
  gdr mkdir drive:/Documents/newdir
  gdr mkdir -p drive:/a/b/c`,
	Args:              cobra.MinimumNArgs(1),
	RunE:              runMkdir,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	mkdirCmd.Flags().BoolVarP(&mkdirParents, "parents", "p", false, "create parent folders as needed and don't error if they exist")
	rootCmd.AddCommand(mkdirCmd)
}

func runMkdir(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	var firstErr error
	for _, arg := range args {
		l := loc.Parse(arg)
		if !l.IsDrive() {
			err := fmt.Errorf("mkdir only supports Drive paths (add drive:): %s", arg)
			fmt.Fprintln(os.Stderr, "mkdir:", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := makeDir(ctx, client, l.Path); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// makeDir creates the folder at a Drive absolute path.
func makeDir(ctx context.Context, client *drive.Client, absPath string) error {
	if mkdirParents {
		// Create parents too and reuse existing ones (idempotent).
		_, err := client.EnsureFolderPath(ctx, absPath)
		return err
	}

	parentPath, name := drive.SplitParent(absPath)
	if name == "" {
		return fmt.Errorf("cannot create the root")
	}

	// Resolve the parent folder (error if it doesn't exist).
	parents, err := client.Resolve(ctx, parentPath)
	if err != nil {
		return fmt.Errorf("parent folder not found (%s); use -p to create parents too: %w", parentPath, err)
	}
	var parentID string
	for _, p := range parents {
		if p.File.IsFolder() {
			parentID = p.File.ID
			break
		}
	}
	if parentID == "" {
		return fmt.Errorf("parent path is not a folder: %s", parentPath)
	}

	// Do nothing if a same-named folder already exists (idempotent).
	existing, err := client.ListChildrenByName(ctx, parentID, name)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.IsFolder() {
			return fmt.Errorf("already exists: %s", absPath)
		}
	}

	if _, err := client.Mkdir(ctx, parentID, name); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Created: drive:%s\n", absPath)
	return nil
}
