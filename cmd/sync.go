package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var (
	syncDelete bool
	syncDryRun bool
)

var syncCmd = &cobra.Command{
	Use:   "sync SOURCE DEST",
	Short: "ディレクトリを一方向に同期する",
	Long: `SOURCE のディレクトリ階層を DEST へ一方向に同期します。SOURCE と DEST の
一方を drive: プレフィックスで指定することで方向が決まります:

  drive: → ローカル … Drive からローカルへ同期 (ダウンロード)
  ローカル → drive: … ローカルから Drive へ同期 (アップロード)

差分判定はサイズと更新時刻で行います。サイズが同じで宛先が同じか新しければ
スキップし、それ以外は転送します。--delete を付けると、SOURCE に存在せず DEST
にのみあるファイルを削除します (Drive 側はゴミ箱へ移動)。--dry-run で実際の
転送をせず予定だけ表示します。

Google ネイティブ形式 (Google ドキュメント等) は同期対象外です。

例:
  gdr sync ./site drive:/backup/site
  gdr sync drive:/Photos ./photos
  gdr sync --delete ./site drive:/backup/site
  gdr sync --dry-run ./site drive:/backup/site`,
	Args:              cobra.ExactArgs(2),
	RunE:              runSync,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	syncCmd.Flags().BoolVar(&syncDelete, "delete", false, "SOURCE に無いファイルを DEST から削除する (Drive はゴミ箱へ)")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "実際には転送せず、予定される操作だけ表示する")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	src := loc.Parse(args[0])
	dst := loc.Parse(args[1])

	switch {
	case src.IsDrive() && dst.IsLocal():
		return syncDriveToLocal(ctx, src.Path, dst.Path)
	case src.IsLocal() && dst.IsDrive():
		return syncLocalToDrive(ctx, src.Path, dst.Path)
	case src.IsDrive() && dst.IsDrive():
		return fmt.Errorf("Drive 同士の同期は未対応です")
	default:
		return fmt.Errorf("ローカル同士の同期は未対応です。Drive を含める場合は drive: を付けてください")
	}
}

// entry は同期対象 1 件の最小メタデータ。相対パスをキーに突き合わせる。
type entry struct {
	size    int64
	modTime time.Time
	isDir   bool
	// driveID は Drive 側エントリのファイル ID (ローカル側では空)。
	driveID string
	// isGoogleDoc は Drive ネイティブ形式かどうか (同期対象外の判定用)。
	isGoogleDoc bool
}

// needsTransfer は src を dst へ転送すべきかを返す。
//
// 判定は次の通り (rsync の既定に近い):
//   - dst が存在しない → 転送
//   - サイズが異なる → 転送
//   - サイズが同じで src が dst より新しい → 転送
//   - それ以外 (サイズ同じかつ dst が同等以上に新しい) → スキップ
//
// 更新時刻は Drive (ミリ秒) とローカル (ナノ秒) で精度が異なるため、秒単位に
// 丸めて比較する。
func needsTransfer(srcSize int64, srcMod time.Time, dstExists bool, dstSize int64, dstMod time.Time) bool {
	if !dstExists {
		return true
	}
	if srcSize != dstSize {
		return true
	}
	return srcMod.Truncate(time.Second).After(dstMod.Truncate(time.Second))
}

// ---- ローカル → Drive ----

func syncLocalToDrive(ctx context.Context, localRoot, driveRoot string) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	info, err := os.Stat(localRoot)
	if err != nil {
		return fmt.Errorf("コピー元が見つかりません (%s): %w", localRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("sync のコピー元はディレクトリである必要があります: %s", localRoot)
	}

	srcTree, err := buildLocalTree(localRoot)
	if err != nil {
		return err
	}

	// DEST のルートフォルダを確保する。
	destRootID, err := client.EnsureFolderPath(ctx, driveRoot)
	if err != nil {
		return err
	}
	dstTree, err := buildDriveTree(ctx, client, destRootID)
	if err != nil {
		return err
	}

	// 相対パスを浅い順に処理し、フォルダを先に作ってからファイルを上げる。
	// 型不一致 (片方がフォルダ、もう片方がファイル) のサブツリーはまるごとスキップする。
	var skipSubtrees []string
	for _, rel := range sortedKeys(srcTree) {
		se := srcTree[rel]
		de, exists := dstTree[rel]

		if isUnderAny(rel, skipSubtrees) {
			continue
		}

		// SOURCE と DEST で種別 (フォルダ/ファイル) が食い違う場合は、誤って
		// フォルダをファイルで上書きしたり逆をしたりしないよう、スキップする。
		if exists && se.isDir != de.isDir {
			fmt.Fprintf(os.Stderr, "sync: スキップ (種別がコピー先と異なります): drive:%s\n", path.Join(driveRoot, rel))
			if se.isDir {
				skipSubtrees = append(skipSubtrees, rel)
			}
			continue
		}

		if se.isDir {
			if !exists {
				if syncDryRun {
					fmt.Fprintf(os.Stderr, "[dry-run] mkdir drive:%s\n", path.Join(driveRoot, rel))
					continue
				}
				if _, err := client.EnsureFolderPath(ctx, path.Join(driveRoot, rel)); err != nil {
					return err
				}
			}
			continue
		}

		// 宛先が Google ネイティブ形式の場合、内容を octet-stream で上書きすると
		// 文書を破壊するため転送しない (ダウンロード方向と同じく対象外扱い)。
		if exists && de.isGoogleDoc {
			fmt.Fprintf(os.Stderr, "sync: スキップ (Google ネイティブ形式は上書きできません): drive:%s\n", path.Join(driveRoot, rel))
			continue
		}

		if !needsTransfer(se.size, se.modTime, exists, de.size, de.modTime) {
			continue
		}

		localPath := filepath.Join(localRoot, filepath.FromSlash(rel))
		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] upload %s -> drive:%s\n", localPath, path.Join(driveRoot, rel))
			continue
		}
		if err := uploadForSync(ctx, client, localPath, rel, driveRoot, destRootID, de, exists); err != nil {
			return err
		}
	}

	if syncDelete {
		if err := deleteExtraOnDrive(ctx, client, srcTree, dstTree, driveRoot); err != nil {
			return err
		}
	}
	return nil
}

// uploadForSync は同期の 1 ファイルを Drive へ上げる。既存なら内容を更新し、
// 無ければ親フォルダを確保して新規作成する。
func uploadForSync(ctx context.Context, client *drive.Client, localPath, rel, driveRoot, destRootID string, existing entry, exists bool) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}
	defer f.Close()

	if exists && existing.driveID != "" {
		if _, err := client.UpdateContent(ctx, existing.driveID, f, info.ModTime()); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "更新: %s -> drive:%s\n", localPath, path.Join(driveRoot, rel))
		return nil
	}

	// 親フォルダを確保して新規アップロード。
	parentRel := path.Dir(rel)
	parentID := destRootID
	if parentRel != "." && parentRel != "/" {
		parentID, err = client.EnsureFolderPath(ctx, path.Join(driveRoot, parentRel))
		if err != nil {
			return err
		}
	}
	if _, err := client.Upload(ctx, parentID, path.Base(rel), f, info.ModTime()); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "アップロード: %s -> drive:%s\n", localPath, path.Join(driveRoot, rel))
	return nil
}

// deleteExtraOnDrive は SOURCE に無く DEST にのみある Drive 側エントリを削除する。
// 深い順に処理し、フォルダを後から消す。
func deleteExtraOnDrive(ctx context.Context, client *drive.Client, srcTree, dstTree map[string]entry, driveRoot string) error {
	keys := sortedKeys(dstTree)
	// 深い順 (子を先に削除) にする。
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	for _, rel := range keys {
		if _, ok := srcTree[rel]; ok {
			continue
		}
		de := dstTree[rel]
		full := path.Join(driveRoot, rel)
		// Google ネイティブ形式は同期対象外なので、--delete でも削除しない
		// (転送経路で扱えないものを削除だけするのは一貫しない)。
		if de.isGoogleDoc {
			fmt.Fprintf(os.Stderr, "sync: 削除スキップ (Google ネイティブ形式は対象外): drive:%s\n", full)
			continue
		}
		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] delete drive:%s\n", full)
			continue
		}
		if err := client.Trash(ctx, de.driveID); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ゴミ箱へ移動: drive:%s\n", full)
	}
	return nil
}

// ---- Drive → ローカル ----

func syncDriveToLocal(ctx context.Context, driveRoot, localRoot string) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	// SOURCE の Drive フォルダを解決する。
	srcID, ok := resolveExistingFolder(ctx, client, driveRoot)
	if !ok {
		return fmt.Errorf("コピー元の Drive フォルダが見つかりません: %s", driveRoot)
	}

	srcTree, err := buildDriveTree(ctx, client, srcID)
	if err != nil {
		return err
	}

	if !syncDryRun {
		if err := os.MkdirAll(localRoot, 0o755); err != nil {
			return fmt.Errorf("コピー先の作成に失敗しました (%s): %w", localRoot, err)
		}
	}
	dstTree, err := buildLocalTree(localRoot)
	if err != nil {
		return err
	}

	// 型不一致 (片方がフォルダ、もう片方がファイル) のサブツリーはまるごとスキップする。
	var skipSubtrees []string
	for _, rel := range sortedKeys(srcTree) {
		se := srcTree[rel]
		de, exists := dstTree[rel]
		localPath := filepath.Join(localRoot, filepath.FromSlash(rel))

		if isUnderAny(rel, skipSubtrees) {
			continue
		}

		// SOURCE(Drive) と DEST(ローカル) で種別が食い違う場合はスキップする
		// (フォルダのある場所へファイルを書く/その逆で os.MkdirAll などが失敗するため)。
		if exists && se.isDir != de.isDir {
			fmt.Fprintf(os.Stderr, "sync: スキップ (種別がコピー先と異なります): %s\n", localPath)
			if se.isDir {
				skipSubtrees = append(skipSubtrees, rel)
			}
			continue
		}

		if se.isDir {
			if !exists {
				if syncDryRun {
					fmt.Fprintf(os.Stderr, "[dry-run] mkdir %s\n", localPath)
					continue
				}
				if err := os.MkdirAll(localPath, 0o755); err != nil {
					return err
				}
			}
			continue
		}

		if se.isGoogleDoc {
			fmt.Fprintf(os.Stderr, "sync: スキップ (Google ネイティブ形式は未対応): drive:%s\n", path.Join(driveRoot, rel))
			continue
		}

		if !needsTransfer(se.size, se.modTime, exists, de.size, de.modTime) {
			continue
		}

		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] download drive:%s -> %s\n", path.Join(driveRoot, rel), localPath)
			continue
		}
		if err := downloadForSync(ctx, client, se, localPath); err != nil {
			return err
		}
	}

	if syncDelete {
		if err := deleteExtraOnLocal(srcTree, dstTree, localRoot); err != nil {
			return err
		}
	}
	return nil
}

// downloadForSync は同期の 1 ファイルをローカルへ書き出し、mtime を Drive 側に
// 合わせる (次回以降の差分判定を安定させるため)。
func downloadForSync(ctx context.Context, client *drive.Client, se entry, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	body, err := client.Download(ctx, se.driveID)
	if err != nil {
		return err
	}
	defer body.Close()

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("出力ファイルの作成に失敗しました (%s): %w", localPath, err)
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return fmt.Errorf("%s の書き込みに失敗しました: %w", localPath, err)
	}
	out.Close()

	if !se.modTime.IsZero() {
		// アクセス時刻は変更時刻に合わせる (atime を別管理しないため)。
		_ = os.Chtimes(localPath, se.modTime, se.modTime)
	}
	fmt.Fprintf(os.Stderr, "ダウンロード: %s\n", localPath)
	return nil
}

// deleteExtraOnLocal は SOURCE に無く DEST にのみあるローカルのエントリを消す。
func deleteExtraOnLocal(srcTree, dstTree map[string]entry, localRoot string) error {
	keys := sortedKeys(dstTree)
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	for _, rel := range keys {
		if _, ok := srcTree[rel]; ok {
			continue
		}
		localPath := filepath.Join(localRoot, filepath.FromSlash(rel))
		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] delete %s\n", localPath)
			continue
		}
		if err := os.RemoveAll(localPath); err != nil {
			return fmt.Errorf("%s の削除に失敗しました: %w", localPath, err)
		}
		fmt.Fprintf(os.Stderr, "削除: %s\n", localPath)
	}
	return nil
}

// ---- ツリー構築 ----

// buildLocalTree は localRoot 配下を再帰して相対パス → entry のマップにする。
// ルート自身は含めない。
func buildLocalTree(localRoot string) (map[string]entry, error) {
	tree := make(map[string]entry)
	err := filepath.WalkDir(localRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == localRoot {
			return nil
		}
		rel, err := filepath.Rel(localRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		tree[rel] = entry{
			size:    info.Size(),
			modTime: info.ModTime(),
			isDir:   d.IsDir(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ローカルディレクトリの走査に失敗しました: %w", err)
	}
	return tree, nil
}

// buildDriveTree は Drive フォルダ配下を再帰して相対パス → entry のマップにする。
func buildDriveTree(ctx context.Context, client *drive.Client, rootID string) (map[string]entry, error) {
	tree := make(map[string]entry)
	var walk func(folderID, relBase string) error
	walk = func(folderID, relBase string) error {
		children, err := client.ListChildren(ctx, folderID)
		if err != nil {
			return err
		}
		for _, ch := range children {
			rel := ch.Name
			if relBase != "" {
				rel = relBase + "/" + ch.Name
			}
			// Drive は同一フォルダ内の同名を許すが、ツリーは相対パスをキーにする。
			// 同名が複数あると 1 件しか扱えないため、黙って取りこぼさず警告する。
			if _, dup := tree[rel]; dup {
				fmt.Fprintf(os.Stderr, "sync: 警告: 同名が複数あるため 1 件のみ同期します: drive:%s\n", rel)
				continue
			}
			modTime, _ := time.Parse(time.RFC3339, ch.ModifiedTime)
			tree[rel] = entry{
				size:        ch.Size,
				modTime:     modTime,
				isDir:       ch.IsFolder(),
				driveID:     ch.ID,
				isGoogleDoc: ch.IsGoogleDoc(),
			}
			if ch.IsFolder() {
				if err := walk(ch.ID, rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(rootID, ""); err != nil {
		return nil, err
	}
	return tree, nil
}

// sortedKeys は相対パスを浅い順 (区切り数の少ない順、同数は辞書順) に並べる。
// フォルダを親から先に処理するため。
func sortedKeys(tree map[string]entry) []string {
	keys := make([]string, 0, len(tree))
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		di, dj := pathDepth(keys[i]), pathDepth(keys[j])
		if di != dj {
			return di < dj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// isUnderAny は相対パス rel が prefixes のいずれかと一致するか、その配下かを返す。
// 型不一致でスキップしたフォルダのサブツリー全体を除外するために使う。
func isUnderAny(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// pathDepth は相対パスの階層の深さ ("/" の数 + 1) を返す。
func pathDepth(rel string) int {
	depth := 1
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			depth++
		}
	}
	return depth
}
