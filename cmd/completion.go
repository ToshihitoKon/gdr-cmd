package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/spf13/cobra"
)

// completionTimeout は Tab 補完時に Drive API 呼び出しへ与える制限時間。
// 補完はインタラクティブなので、遅延でシェルが固まらないよう短めにする。
const completionTimeout = 3 * time.Second

// completeDrivePath は Drive 上のパスを動的に補完する ValidArgsFunction。
//
// 入力中の toComplete (例 "/dir/fi") を「親パス + 接頭辞」に分け、親フォルダ
// 直下の子要素のうち接頭辞に前方一致するものを候補として返す。フォルダ候補は
// 末尾に "/" を付け、連続補完を促す。
//
// 補完は失敗してもシェルを壊さないよう、エラー時は候補なし + NoFileComp を返す。
func completeDrivePath(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// 補完値にメタ文字が含まれる場合、Drive API での前方一致が困難なため
	// ファイル補完を抑止しつつ何も返さない (利用者の入力を尊重)。
	if strings.ContainsAny(toComplete, "*?[") {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	parentPath, prefix := splitParentPrefix(toComplete)

	ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
	defer cancel()

	client, err := drive.New(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// 親パスを解決する。glob を含まない単一フォルダのはずだが、念のため
	// 解決結果のうちフォルダであるものすべての子要素を候補に含める。
	parents, err := client.Resolve(ctx, parentPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var candidates []string
	seen := make(map[string]struct{})
	for _, parent := range parents {
		if !parent.File.IsFolder() {
			continue
		}
		children, err := client.ListChildren(ctx, parent.File.ID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, child := range children {
			if !strings.HasPrefix(child.Name, prefix) {
				continue
			}
			cand := buildCandidate(parentPath, child)
			if _, dup := seen[cand]; dup {
				continue
			}
			seen[cand] = struct{}{}
			candidates = append(candidates, cand)
		}
	}

	// フォルダ候補は末尾が "/" なので、シェルが空白を付けないようにする。
	directive := cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	return candidates, directive
}

// completeLocationArgs は cobra の ValidArgsFunction シグネチャに合わせた
// completeLocationArg のラッパー。引数位置によらず同じ補完規則を適用する。
func completeLocationArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeLocationArg(cmd, toComplete)
}

// completeLocationArg は drive: 記法に対応した引数補完を行う。
//
// 入力が "drive:" で始まれば Drive パスを動的補完し、候補にも "drive:" を
// 付け直す。それ以外はローカルとみなし、シェルの既定ファイル補完に委ねる。
// cp/sync/mkdir/rm/mv のように両端を取りうるコマンドで使う。
func completeLocationArg(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	const prefix = "drive:"
	if !strings.HasPrefix(toComplete, prefix) {
		// ローカルパス: シェルにファイル補完させる。
		return nil, cobra.ShellCompDirectiveDefault
	}
	drivePart := strings.TrimPrefix(toComplete, prefix)
	candidates, directive := completeDrivePath(cmd, nil, drivePart)
	// 候補に drive: を付け直す。
	prefixed := make([]string, len(candidates))
	for i, c := range candidates {
		prefixed[i] = prefix + c
	}
	return prefixed, directive
}

// splitParentPrefix は補完入力を (親パス, 末尾要素の接頭辞) に分ける。
//
//	"/dir/fi" -> ("/dir", "fi")
//	"/dir/"   -> ("/dir", "")
//	"/fi"     -> ("/",    "fi")
//	"fi"      -> ("/",    "fi")   (先頭スラッシュ無しはマイドライブ起点として扱う)
//	""        -> ("/",    "")
func splitParentPrefix(toComplete string) (parent, prefix string) {
	idx := strings.LastIndex(toComplete, "/")
	if idx < 0 {
		// スラッシュ無し: ルート直下の補完とみなす。
		return "/", toComplete
	}
	parent = toComplete[:idx]
	if parent == "" {
		parent = "/"
	}
	prefix = toComplete[idx+1:]
	return parent, prefix
}

// buildCandidate は親パスと子要素から補完候補文字列を組み立てる。
// フォルダには末尾 "/" を付ける。
func buildCandidate(parentPath string, child drive.File) string {
	var b strings.Builder
	if parentPath == "/" {
		b.WriteString("/")
		b.WriteString(child.Name)
	} else {
		b.WriteString(parentPath)
		b.WriteString("/")
		b.WriteString(child.Name)
	}
	if child.IsFolder() {
		b.WriteString("/")
	}
	return b.String()
}
