package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var (
	lsLong     bool
	lsHuman    bool
	lsListDirs bool // -d: フォルダの中身ではなくフォルダ自身を表示
)

var lsCmd = &cobra.Command{
	Use:   "ls [PATH...]",
	Short: "Drive 上のファイルを一覧表示する",
	Long: `マイドライブ起点のパスにマッチするファイル/フォルダを一覧表示します。

引数を省略するとルート直下を表示します。パスがフォルダを指す場合はその中身を、
ファイルを指す場合はそのファイル自身を表示します (Unix の ls と同様)。
パスにはワイルドカード (*, ?, [...]) を使えます。

例:
  gdr ls
  gdr ls /Documents
  gdr ls -l /Documents/*.pdf
  gdr ls -d /Documents      # フォルダ自身を表示`,
	RunE:              runLs,
	ValidArgsFunction: completeDrivePath,
}

func init() {
	lsCmd.Flags().BoolVarP(&lsLong, "long", "l", false, "詳細形式 (種別・サイズ・更新日時) で表示する")
	// -h は cobra が --help のショートハンドに使うため、ここでは long フラグのみにする。
	lsCmd.Flags().BoolVar(&lsHuman, "human-readable", false, "サイズを人間が読みやすい単位で表示する")
	lsCmd.Flags().BoolVarP(&lsListDirs, "directory", "d", false, "フォルダの中身ではなくフォルダ自身を表示する")
	rootCmd.AddCommand(lsCmd)
}

func runLs(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	// 引数なしはルートを対象にする。
	if len(args) == 0 {
		args = []string{"/"}
	}

	// ls は引数を Drive パスとして扱う。drive: プレフィックス付きも受け付けるため
	// 正規化して "/" 始まりのマイドライブ起点パスに揃える。
	for i, a := range args {
		args[i] = loc.ParseDriveDefault(a).Path
	}

	// 複数パス指定時に見出しを付けるかどうか。
	multiHeading := false
	if !lsListDirs {
		multiHeading = len(args) > 1 || hasFolderTarget(ctx, client, args)
	} else {
		multiHeading = len(args) > 1
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	defer w.Flush()

	var firstErr error
	for i, p := range args {
		nodes, err := client.Resolve(ctx, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ls: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(nodes) == 0 {
			fmt.Fprintf(os.Stderr, "ls: 該当なし: %s\n", p)
			continue
		}

		if err := listNodes(ctx, w, client, nodes, multiHeading, i > 0); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// hasFolderTarget は引数のいずれかがフォルダに解決されるかを調べる
// (見出し表示の要否判定用)。判定のための先読みなのでエラーは無視する。
func hasFolderTarget(ctx context.Context, client *drive.Client, args []string) bool {
	for _, p := range args {
		nodes, err := client.Resolve(ctx, p)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if n.File.IsFolder() {
				return true
			}
		}
	}
	return false
}

// listNodes は解決済み Node 群を表示する。
// フォルダはその中身を展開し (-d 指定時を除く)、ファイルはそのまま表示する。
func listNodes(ctx context.Context, w *tabwriter.Writer, client *drive.Client, nodes []drive.Node, withHeading, leadingBlank bool) error {
	// フォルダと非フォルダを分け、Unix ls と同様に非フォルダを先に出す。
	var files []drive.File
	var folders []drive.Node
	for _, n := range nodes {
		if n.File.IsFolder() && !lsListDirs {
			folders = append(folders, n)
		} else {
			files = append(files, n.File)
		}
	}

	// 直接指定された非フォルダ (とフォルダ自身表示) をまず出力。
	if len(files) > 0 {
		sortFiles(files)
		for _, f := range files {
			printFileLine(w, f)
		}
		w.Flush()
	}

	// フォルダはその中身を展開表示する。
	for _, folder := range folders {
		children, err := client.ListChildren(ctx, folder.File.ID)
		if err != nil {
			return err
		}
		if leadingBlank || withHeading {
			if len(files) > 0 || leadingBlank {
				fmt.Fprintln(os.Stdout)
			}
		}
		if withHeading {
			fmt.Fprintf(os.Stdout, "%s:\n", folder.Path)
		}
		sortFiles(children)
		for _, c := range children {
			printFileLine(w, c)
		}
		w.Flush()
	}
	return nil
}

// sortFiles はフォルダを先に、その中で名前順に並べる。
func sortFiles(files []drive.File) {
	sort.SliceStable(files, func(i, j int) bool {
		fi, fj := files[i], files[j]
		if fi.IsFolder() != fj.IsFolder() {
			return fi.IsFolder() // フォルダが先
		}
		return fi.Name < fj.Name
	})
}

// printFileLine は 1 ファイルを現在のフラグに従って出力する。
func printFileLine(w *tabwriter.Writer, f drive.File) {
	name := f.Name
	if f.IsFolder() {
		name += "/"
	}
	if !lsLong {
		fmt.Fprintln(w, name)
		return
	}

	kind := fileKind(f)
	size := formatSize(f)
	modified := formatModTime(f.ModifiedTime)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", kind, size, modified, name)
}

// fileKind は種別ラベルを返す。
func fileKind(f drive.File) string {
	switch {
	case f.IsFolder():
		return "dir"
	case f.IsGoogleDoc():
		return "gdoc"
	default:
		return "file"
	}
}

// formatSize はサイズ列の表示文字列を返す。
// フォルダや Google ネイティブ形式はサイズを持たないため "-" とする。
func formatSize(f drive.File) string {
	if f.IsFolder() || f.IsGoogleDoc() {
		return "-"
	}
	if lsHuman {
		return humanSize(f.Size)
	}
	return fmt.Sprintf("%d", f.Size)
}

// humanSize はバイト数を 1024 進の単位付き文字列にする。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// formatModTime は更新時刻を UNIX の ls -l 風に整える。
//
// ls の慣習に倣い、おおむね半年以内なら "月 日 時刻" (例 "Jun 18 14:30")、
// それより古ければ "月 日  年" (例 "Sep 15  2021") を返す。日はスペース
// パディング (_2) で桁を揃える。解析できない場合は元の文字列を返す。
func formatModTime(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	t = t.Local()
	// 6 ヶ月 = 約 182.5 日。未来の日時も古い扱いにならないよう絶対値で見る。
	const recent = 182*24*time.Hour + 12*time.Hour
	if d := time.Since(t); d >= -recent && d <= recent {
		return t.Format("Jan _2 15:04")
	}
	return t.Format("Jan _2  2006")
}
