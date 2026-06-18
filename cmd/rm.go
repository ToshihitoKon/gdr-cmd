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
	Short: "Drive 上のファイル/フォルダを削除する",
	Long: `Drive 上のファイル/フォルダを削除します。パスは drive: プレフィックスで指定し、
ワイルドカード (*, ?, [...]) を使えます。

既定ではゴミ箱へ移動します (Drive のゴミ箱から復元可能)。--permanent を付けると
ゴミ箱を経由せず完全に削除します (復元不可)。フォルダを削除するには -r が必要です。

例:
  gdr rm drive:/Documents/old.pdf
  gdr rm drive:/tmp/*.log
  gdr rm -r drive:/Documents/oldproject
  gdr rm --permanent drive:/secret.txt`,
	Args:              cobra.MinimumNArgs(1),
	RunE:              runRm,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	rmCmd.Flags().BoolVarP(&rmRecursive, "recursive", "r", false, "フォルダを (中身ごと) 削除する")
	rmCmd.Flags().BoolVar(&rmPermanent, "permanent", false, "ゴミ箱を経由せず完全に削除する (復元不可)")
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
			err := fmt.Errorf("rm は Drive のパスのみ対応します (drive: を付けてください): %s", arg)
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
			err := fmt.Errorf("該当なし: %s", arg)
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

// removeNode は 1 つの Node を削除する。フォルダは -r が無いと拒否する。
func removeNode(cmd *cobra.Command, client *drive.Client, node drive.Node) error {
	if node.File.IsFolder() && !rmRecursive {
		return fmt.Errorf("%s はフォルダです (-r を指定してください)", node.Path)
	}

	ctx := cmd.Context()
	if rmPermanent {
		if err := client.Delete(ctx, node.File.ID); err != nil {
			return fmt.Errorf("%s: %w", node.Path, err)
		}
		fmt.Fprintf(os.Stderr, "完全削除: drive:%s\n", node.Path)
		return nil
	}

	if err := client.Trash(ctx, node.File.ID); err != nil {
		return fmt.Errorf("%s: %w", node.Path, err)
	}
	fmt.Fprintf(os.Stderr, "ゴミ箱へ移動: drive:%s\n", node.Path)
	return nil
}
