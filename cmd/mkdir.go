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
	Short: "Drive 上にフォルダを作成する",
	Long: `Drive 上にフォルダを作成します。パスは drive: プレフィックスで指定します。

既定では親フォルダが存在している必要があります。-p を付けると親フォルダごと
作成し、既に存在していてもエラーにしません (mkdir -p と同じ)。

例:
  gdr mkdir drive:/Documents/newdir
  gdr mkdir -p drive:/a/b/c`,
	Args:              cobra.MinimumNArgs(1),
	RunE:              runMkdir,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	mkdirCmd.Flags().BoolVarP(&mkdirParents, "parents", "p", false, "必要な親フォルダも作成し、既存でもエラーにしない")
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
			err := fmt.Errorf("mkdir は Drive のパスのみ対応します (drive: を付けてください): %s", arg)
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

// makeDir は Drive 絶対パスのフォルダを作成する。
func makeDir(ctx context.Context, client *drive.Client, absPath string) error {
	if mkdirParents {
		// 親ごと作成し、既存は再利用する (冪等)。
		_, err := client.EnsureFolderPath(ctx, absPath)
		return err
	}

	parentPath, name := drive.SplitParent(absPath)
	if name == "" {
		return fmt.Errorf("ルートは作成できません")
	}

	// 親フォルダを解決する (存在しなければエラー)。
	parents, err := client.Resolve(ctx, parentPath)
	if err != nil {
		return fmt.Errorf("親フォルダが見つかりません (%s)。-p を付けると親ごと作成します: %w", parentPath, err)
	}
	var parentID string
	for _, p := range parents {
		if p.File.IsFolder() {
			parentID = p.File.ID
			break
		}
	}
	if parentID == "" {
		return fmt.Errorf("親パスがフォルダではありません: %s", parentPath)
	}

	// 同名フォルダが既にあれば何もしない (冪等)。
	existing, err := client.ListChildrenByName(ctx, parentID, name)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.IsFolder() {
			return fmt.Errorf("既に存在します: %s", absPath)
		}
	}

	if _, err := client.Mkdir(ctx, parentID, name); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "作成: drive:%s\n", absPath)
	return nil
}
