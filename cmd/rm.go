package cmd

import (
	"fmt"
	"os"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var (
	rmRecursive bool
	rmPermanent bool
)

var rmCmd = &cobra.Command{
	Use:   "rm drive:PATH...",
	Short: "Delete files/folders on Drive",
	Long: `Delete files/folders on Drive. Specify the path with the drive: prefix;
wildcards (*, ?, [...]) are supported.

By default, items are moved to Trash (recoverable from Drive's trash). With
--permanent, they are deleted without going through Trash (unrecoverable).
Deleting a folder requires -r.

Examples:
  gdr rm drive:/Documents/old.pdf
  gdr rm drive:/tmp/*.log
  gdr rm -r drive:/Documents/oldproject
  gdr rm --permanent drive:/secret.txt`,
	Args:              cobra.MinimumNArgs(1),
	RunE:              runRm,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	rmCmd.Flags().BoolVarP(&rmRecursive, "recursive", "r", false, "delete folders (and their contents)")
	rmCmd.Flags().BoolVar(&rmPermanent, "permanent", false, "delete permanently without using Trash (unrecoverable)")
	rootCmd.AddCommand(rmCmd)
}

func runRm(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	var firstErr error
	for _, arg := range args {
		l := loc.Parse(arg)
		if !l.IsDrive() {
			err := fmt.Errorf("rm only supports Drive paths (add drive:): %s", arg)
			fmt.Fprintln(os.Stderr, "rm:", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		nodes, err := client.Resolve(ctx, l.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rm: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(nodes) == 0 {
			err := fmt.Errorf("no match: %s", arg)
			fmt.Fprintln(os.Stderr, "rm:", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		for _, node := range nodes {
			if err := removeNode(cmd, client, node); err != nil {
				fmt.Fprintf(os.Stderr, "rm: %v\n", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// removeNode deletes a single node. Folders are rejected without -r.
func removeNode(cmd *cobra.Command, client *drive.Client, node drive.Node) error {
	if node.File.IsFolder() && !rmRecursive {
		return fmt.Errorf("%s is a folder (use -r)", node.Path)
	}

	ctx := cmd.Context()
	if rmPermanent {
		if err := client.Delete(ctx, node.File.ID); err != nil {
			return fmt.Errorf("%s: %w", node.Path, err)
		}
		fmt.Fprintf(os.Stderr, "Deleted permanently: drive:%s\n", node.Path)
		return nil
	}

	if err := client.Trash(ctx, node.File.ID); err != nil {
		return fmt.Errorf("%s: %w", node.Path, err)
	}
	fmt.Fprintf(os.Stderr, "Trashed: drive:%s\n", node.Path)
	return nil
}
