package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var mvCmd = &cobra.Command{
	Use:   "mv drive:SOURCE... drive:DEST",
	Short: "Drive 上でファイル/フォルダを移動・リネームする",
	Long: `Drive 上でファイル/フォルダを移動・リネームします。SOURCE と DEST は
どちらも drive: プレフィックスで指定します (Drive 内の操作のみ対応)。

DEST が既存のフォルダなら、その中へ移動します (名前は維持)。DEST が存在しない
場合は、単一の SOURCE をその名前へリネーム (必要なら別フォルダへ移動) します。
SOURCE が複数の場合、DEST は既存のフォルダでなければなりません。

Drive ではメタデータの更新だけで移動するため、内容の再アップロードは発生しません。
ローカルとの間で移したい場合は cp でコピーしてから rm してください。

例:
  gdr mv drive:/a.txt drive:/Documents/      # フォルダへ移動
  gdr mv drive:/old.txt drive:/new.txt        # リネーム
  gdr mv drive:/x.txt drive:/y.txt drive:/box/ # 複数をフォルダへ移動`,
	Args:              cobra.MinimumNArgs(2),
	RunE:              runMv,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	rootCmd.AddCommand(mvCmd)
}

func runMv(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// 全引数が Drive であることを要求する。
	locs := make([]loc.Location, len(args))
	for i, a := range args {
		locs[i] = loc.Parse(a)
		if !locs[i].IsDrive() {
			return fmt.Errorf("mv は Drive 内の操作のみ対応します (drive: を付けてください): %s\n"+
				"ローカルとの間で移すには cp でコピーしてから rm してください", a)
		}
	}

	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	destPath := locs[len(locs)-1].Path
	srcPaths := locs[:len(locs)-1]

	// コピー元を解決して集める。
	var sources []drive.Node
	for _, s := range srcPaths {
		nodes, err := client.Resolve(ctx, s.Path)
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return fmt.Errorf("該当なし: %s", s)
		}
		sources = append(sources, nodes...)
	}

	// DEST が既存フォルダかを調べる。
	destFolderID, destIsFolder := resolveExistingFolder(ctx, client, destPath)

	if len(sources) > 1 && !destIsFolder {
		return fmt.Errorf("コピー元が複数あります。コピー先 %q は既存のフォルダである必要があります", destPath)
	}

	if destIsFolder {
		return moveIntoFolder(ctx, client, sources, destFolderID, destPath)
	}
	// DEST 非存在 → 単一 SOURCE のリネーム/移動。
	return renameTo(ctx, client, sources[0], destPath)
}

// resolveExistingFolder は absPath が既存フォルダなら (ID, true) を返す。
// 解決できない/フォルダでない場合は ("", false)。
func resolveExistingFolder(ctx context.Context, client *drive.Client, absPath string) (string, bool) {
	nodes, err := client.Resolve(ctx, absPath)
	if err != nil {
		return "", false
	}
	for _, n := range nodes {
		if n.File.IsFolder() {
			return n.File.ID, true
		}
	}
	return "", false
}

// moveIntoFolder は sources を destFolderID 直下へ移動する (名前は維持)。
func moveIntoFolder(ctx context.Context, client *drive.Client, sources []drive.Node, destFolderID, destPath string) error {
	var firstErr error
	for _, src := range sources {
		// 自分自身、または自分の子孫フォルダへの移動はループになるので弾く
		// (Drive API も拒否するが、事前に分かりやすいエラーを出す)。
		if src.File.ID == destFolderID || isSelfOrDescendant(src.Path, destPath) {
			err := fmt.Errorf("自分自身または配下へは移動できません: %s -> %s", src.Path, destPath)
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
		fmt.Fprintf(os.Stderr, "移動: drive:%s -> drive:%s/%s\n", src.Path, destPath, src.File.Name)
	}
	return firstErr
}

// renameTo は src を destPath へリネームする。destPath の親が src と異なる
// 場合は移動も伴う。
func renameTo(ctx context.Context, client *drive.Client, src drive.Node, destPath string) error {
	destParentPath, destName := drive.SplitParent(destPath)
	if destName == "" {
		return fmt.Errorf("コピー先が不正です: %s", destPath)
	}

	destParentID, ok := resolveExistingFolder(ctx, client, destParentPath)
	if !ok {
		return fmt.Errorf("コピー先の親フォルダが見つかりません: %s", destParentPath)
	}

	// フォルダを自分自身や配下へ移動するとループになるので弾く。
	if isSelfOrDescendant(src.Path, destPath) {
		return fmt.Errorf("自分自身または配下へは移動できません: %s -> %s", src.Path, destPath)
	}

	// 親の付け替えと名前変更を 1 回の更新で原子的に行う (中間状態を残さない)。
	newParentID := ""
	if destParentID != src.ParentID {
		newParentID = destParentID
	}
	newName := ""
	if destName != src.File.Name {
		newName = destName
	}
	if newParentID == "" && newName == "" {
		// 同じ場所・同じ名前。何もしない。
		fmt.Fprintf(os.Stderr, "移動: 変更なし: drive:%s\n", src.Path)
		return nil
	}
	if err := client.MoveRename(ctx, src.File.ID, newParentID, src.ParentID, newName); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "移動: drive:%s -> drive:%s\n", src.Path, destPath)
	return nil
}

// isSelfOrDescendant は target が src と同じか、src フォルダの配下かを返す。
// フォルダを自分自身/子孫へ移動するループを防ぐために使う。
func isSelfOrDescendant(srcPath, target string) bool {
	return target == srcPath || strings.HasPrefix(target, srcPath+"/")
}
