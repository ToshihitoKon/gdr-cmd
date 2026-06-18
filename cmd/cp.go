package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/spf13/cobra"
)

var cpRecursive bool

var cpCmd = &cobra.Command{
	Use:   "cp SOURCE... DEST",
	Short: "Drive 上のファイルをローカルにダウンロードする",
	Long: `Drive 上のファイル (SOURCE) をローカルのパス (DEST) にダウンロードします。

SOURCE はマイドライブ起点のパスで、ワイルドカード (*, ?, [...]) を使えます。
SOURCE が複数 (または glob で複数) にマッチする場合、DEST は既存のディレクトリで
なければなりません。フォルダをダウンロードするには -r を指定します。

Google ネイティブ形式 (Google ドキュメント/スプレッドシート等) は通常の
ダウンロードができないため、現時点ではスキップして警告します。

例:
  gdr cp /Documents/report.pdf .
  gdr cp /Documents/*.pdf ./pdfs/
  gdr cp -r /Documents/project ./backup/`,
	Args:              cobra.MinimumNArgs(2),
	RunE:              runCp,
	ValidArgsFunction: completeCpArgs,
}

func init() {
	cpCmd.Flags().BoolVarP(&cpRecursive, "recursive", "r", false, "フォルダを再帰的にダウンロードする")
	rootCmd.AddCommand(cpCmd)
}

// completeCpArgs は cp の引数補完を行う。
//
// cp は "SOURCE... DEST" という可変構造で、補完時点では入力中の引数が
// 最後 (DEST) になるか中間 (SOURCE) になるかを確定できない。曖昧さを避け、
// 実用本位で次のように割り切る:
//   - 1 番目の引数 (args が空): Drive パスを動的補完する
//   - 2 番目以降: DEST (ローカルパス) とみなしシェルの既定ファイル補完に委ねる
//
// この割り切りにより、複数 Drive ソースの 2 個目以降は Drive 補完されないが、
// 最も一般的な「1 ソース → ローカル宛」を最短で補完できる。
func completeCpArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 1 {
		return nil, cobra.ShellCompDirectiveDefault // シェルの既定ファイル補完
	}
	return completeDrivePath(cmd, args, toComplete)
}

func runCp(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	sources := args[:len(args)-1]
	dest := args[len(args)-1]

	// 全 SOURCE を解決してマッチを集める。
	var matched []drive.Node
	for _, src := range sources {
		nodes, err := client.Resolve(ctx, src)
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return fmt.Errorf("該当なし: %s", src)
		}
		matched = append(matched, nodes...)
	}

	destIsDir := isExistingDir(dest)

	// 複数マッチ・再帰・末尾スラッシュのいずれかなら DEST はディレクトリ必須。
	multi := len(matched) > 1
	if multi && !destIsDir {
		return fmt.Errorf("コピー元が複数あります。コピー先 %q は既存のディレクトリである必要があります", dest)
	}

	// 同一ディレクトリ内での名前衝突に連番を振るため、使用済み名を記録する。
	used := make(map[string]struct{})

	var firstErr error
	for _, node := range matched {
		if err := copyNode(ctx, client, node, dest, destIsDir, used); err != nil {
			fmt.Fprintf(os.Stderr, "cp: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// copyNode は 1 つの Node (ファイルまたはフォルダ) をダウンロードする。
func copyNode(ctx context.Context, client *drive.Client, node drive.Node, dest string, destIsDir bool, used map[string]struct{}) error {
	if node.File.IsFolder() {
		if !cpRecursive {
			return fmt.Errorf("%s はフォルダです (-r を指定してください)", node.Path)
		}
		// フォルダは dest 配下に同名ディレクトリを作って再帰コピーする。
		target := filepath.Join(dest, node.File.Name)
		if !destIsDir {
			// 単一フォルダを非ディレクトリ宛にする場合は dest 自体をフォルダ名に使う。
			target = dest
		}
		return copyFolderRecursive(ctx, client, node.File.ID, target)
	}

	if node.File.IsGoogleDoc() {
		fmt.Fprintf(os.Stderr, "cp: スキップ (Google ネイティブ形式は未対応): %s\n", node.Path)
		return nil
	}

	// 出力先パスを決める。
	var outPath string
	if destIsDir {
		outPath = uniquePath(dest, node.File.Name, used)
	} else {
		outPath = dest
	}
	return downloadFile(ctx, client, node.File, outPath)
}

// copyFolderRecursive はフォルダ配下を再帰的にダウンロードする。
func copyFolderRecursive(ctx context.Context, client *drive.Client, folderID, target string) error {
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
			sub := filepath.Join(target, child.Name)
			if err := copyFolderRecursive(ctx, client, child.ID, sub); err != nil {
				return err
			}
		case child.IsGoogleDoc():
			fmt.Fprintf(os.Stderr, "cp: スキップ (Google ネイティブ形式は未対応): %s/%s\n", target, child.Name)
		default:
			out := filepath.Join(target, child.Name)
			if err := downloadFile(ctx, client, child, out); err != nil {
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

	// 親ディレクトリが無い場合に備えて作成する。
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
