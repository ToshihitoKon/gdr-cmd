package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var cpRecursive bool

var cpCmd = &cobra.Command{
	Use:   "cp SOURCE... DEST",
	Short: "Drive とローカルの間でファイルをコピーする",
	Long: `Drive とローカルの間でファイルをコピー (ダウンロード/アップロード) します。

Drive 側のパスは drive: プレフィックスで明示します (例 drive:/Documents/a.pdf)。
プレフィックスの無いパスはローカルとして扱います。方向は両端の種別で決まります:

  drive: → ローカル … ダウンロード
  ローカル → drive: … アップロード

SOURCE が複数 (または glob で複数) にマッチする場合、DEST はディレクトリで
なければなりません。フォルダを扱うには -r を指定します。Drive のワイルドカード
(*, ?, [...]) とローカルの glob の両方に対応します。

Google ネイティブ形式 (Google ドキュメント等) はダウンロードできないためスキップします。

例:
  gdr cp drive:/Documents/report.pdf .          # ダウンロード
  gdr cp drive:/Documents/*.pdf ./pdfs/          # 複数ダウンロード
  gdr cp -r drive:/Documents/project ./backup/   # フォルダをダウンロード
  gdr cp ./report.pdf drive:/Documents/          # アップロード
  gdr cp -r ./project drive:/backup/             # フォルダをアップロード`,
	Args:              cobra.MinimumNArgs(2),
	RunE:              runCp,
	ValidArgsFunction: completeCpArgs,
}

func init() {
	cpCmd.Flags().BoolVarP(&cpRecursive, "recursive", "r", false, "フォルダを再帰的にコピーする")
	rootCmd.AddCommand(cpCmd)
}

func runCp(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	rawSources := args[:len(args)-1]
	rawDest := args[len(args)-1]
	dest := loc.Parse(rawDest)

	// SOURCE はすべて同じ側 (全部 Drive か全部ローカル) であることを要求する。
	// 方向が一意に定まらない混在を避けるため。
	sources := make([]loc.Location, len(rawSources))
	for i, s := range rawSources {
		sources[i] = loc.Parse(s)
		if sources[i].Kind != sources[0].Kind {
			return fmt.Errorf("コピー元は Drive とローカルを混在できません")
		}
	}
	srcKind := sources[0].Kind

	switch {
	case srcKind == loc.Drive && dest.IsLocal():
		return downloadSources(ctx, sources, dest, rawDest)
	case srcKind == loc.Local && dest.IsDrive():
		return uploadSources(ctx, sources, dest)
	case srcKind == loc.Drive && dest.IsDrive():
		return fmt.Errorf("Drive 内のコピーは未対応です。移動なら `gdr mv` を使ってください")
	default: // ローカル → ローカル
		return fmt.Errorf("ローカル同士のコピーは OS の cp を使ってください。Drive を含む場合は drive: を付けてください")
	}
}

// ---- ダウンロード (Drive → ローカル) ----

func downloadSources(ctx context.Context, sources []loc.Location, dest loc.Location, rawDest string) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	var matched []drive.Node
	for _, src := range sources {
		nodes, err := client.Resolve(ctx, src.Path)
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return fmt.Errorf("該当なし: %s", src)
		}
		matched = append(matched, nodes...)
	}

	destIsDir := isExistingDir(dest.Path)
	if len(matched) > 1 && !destIsDir {
		return fmt.Errorf("コピー元が複数あります。コピー先 %q は既存のディレクトリである必要があります", dest.Path)
	}

	used := make(map[string]struct{})
	var firstErr error
	for _, node := range matched {
		if err := copyNodeDown(ctx, client, node, dest.Path, destIsDir, used); err != nil {
			fmt.Fprintf(os.Stderr, "cp: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// copyNodeDown は 1 つの Drive Node をローカルへダウンロードする。
func copyNodeDown(ctx context.Context, client *drive.Client, node drive.Node, dest string, destIsDir bool, used map[string]struct{}) error {
	if node.File.IsFolder() {
		if !cpRecursive {
			return fmt.Errorf("%s はフォルダです (-r を指定してください)", node.Path)
		}
		target := filepath.Join(dest, node.File.Name)
		if !destIsDir {
			target = dest
		}
		return downloadFolder(ctx, client, node.File.ID, target)
	}

	if node.File.IsGoogleDoc() {
		fmt.Fprintf(os.Stderr, "cp: スキップ (Google ネイティブ形式は未対応): %s\n", node.Path)
		return nil
	}

	outPath := dest
	if destIsDir {
		outPath = uniquePath(dest, node.File.Name, used)
	}
	return downloadFile(ctx, client, node.File, outPath)
}

// downloadFolder はフォルダ配下を再帰的にダウンロードする。
func downloadFolder(ctx context.Context, client *drive.Client, folderID, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("ディレクトリの作成に失敗しました (%s): %w", target, err)
	}
	children, err := client.ListChildren(ctx, folderID)
	if err != nil {
		return err
	}
	for _, child := range children {
		switch {
		case child.IsFolder():
			if err := downloadFolder(ctx, client, child.ID, filepath.Join(target, child.Name)); err != nil {
				return err
			}
		case child.IsGoogleDoc():
			fmt.Fprintf(os.Stderr, "cp: スキップ (Google ネイティブ形式は未対応): %s/%s\n", target, child.Name)
		default:
			if err := downloadFile(ctx, client, child, filepath.Join(target, child.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// downloadFile は 1 ファイルをダウンロードして outPath に書き出す。
func downloadFile(ctx context.Context, client *drive.Client, f drive.File, outPath string) error {
	body, err := client.Download(ctx, f.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	defer body.Close()

	if dir := filepath.Dir(outPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("出力先ディレクトリの作成に失敗しました (%s): %w", dir, err)
		}
	}
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("出力ファイルの作成に失敗しました (%s): %w", outPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, body); err != nil {
		return fmt.Errorf("%s の書き込みに失敗しました: %w", outPath, err)
	}
	fmt.Fprintf(os.Stderr, "ダウンロード: %s -> %s\n", f.Name, outPath)
	return nil
}

// ---- アップロード (ローカル → Drive) ----

func uploadSources(ctx context.Context, sources []loc.Location, dest loc.Location) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	// ローカルの glob を展開してコピー元を集める。
	var localPaths []string
	for _, src := range sources {
		matches, err := filepath.Glob(src.Path)
		if err != nil {
			return fmt.Errorf("不正なパターン %q: %w", src.Path, err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("該当なし: %s", src.Path)
		}
		localPaths = append(localPaths, matches...)
	}

	// コピー先の Drive フォルダを確保する (mkdir -p 相当)。末尾スラッシュ、複数元、
	// またはディレクトリのアップロードでは dest 自体をフォルダとして扱う。
	destFolderID, err := client.EnsureFolderPath(ctx, dest.Path)
	if err != nil {
		return err
	}

	var firstErr error
	for _, lp := range localPaths {
		if err := uploadPath(ctx, client, lp, destFolderID); err != nil {
			fmt.Fprintf(os.Stderr, "cp: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// uploadPath はローカルのファイル/ディレクトリを Drive の parentID 直下へ上げる。
func uploadPath(ctx context.Context, client *drive.Client, localPath, parentID string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}

	if info.IsDir() {
		if !cpRecursive {
			return fmt.Errorf("%s はディレクトリです (-r を指定してください)", localPath)
		}
		// parentID 直下に同名フォルダを作って再帰アップロードする。
		sub, err := client.Mkdir(ctx, parentID, info.Name())
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(localPath)
		if err != nil {
			return fmt.Errorf("ディレクトリの読み取りに失敗しました (%s): %w", localPath, err)
		}
		for _, e := range entries {
			if err := uploadPath(ctx, client, filepath.Join(localPath, e.Name()), sub.ID); err != nil {
				return err
			}
		}
		return nil
	}

	return uploadFile(ctx, client, localPath, info, parentID)
}

// uploadFile は 1 つのローカルファイルを Drive へアップロードする。
func uploadFile(ctx context.Context, client *drive.Client, localPath string, info os.FileInfo, parentID string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}
	defer f.Close()

	name := filepath.Base(localPath)
	if _, err := client.Upload(ctx, parentID, name, f, info.ModTime()); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "アップロード: %s -> drive:%s\n", localPath, name)
	return nil
}

// ---- 補完・補助 ----

// completeCpArgs は cp の引数を補完する。drive: で始まる入力は Drive パスを
// 動的補完し、それ以外はシェルの既定ファイル補完に委ねる。
func completeCpArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeLocationArg(cmd, toComplete)
}

// isExistingDir はパスが既存のディレクトリかを返す。
func isExistingDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// uniquePath は dir 配下で name が衝突しないパスを返す。
// 既に使用済み (このコマンド実行内) か、ディスク上に存在する場合は
// "name (1).ext" のように連番を付ける。
func uniquePath(dir, name string, used map[string]struct{}) string {
	candidate := filepath.Join(dir, name)
	if !isUsed(candidate, used) {
		used[candidate] = struct{}{}
		return candidate
	}
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	for i := 1; ; i++ {
		alt := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if !isUsed(alt, used) {
			used[alt] = struct{}{}
			return alt
		}
	}
}

// isUsed は候補パスが既に使用済みか実在するかを返す。
func isUsed(path string, used map[string]struct{}) bool {
	if _, ok := used[path]; ok {
		return true
	}
	_, err := os.Stat(path)
	return err == nil
}
