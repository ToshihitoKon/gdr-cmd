// Package cmd は gdr CLI のコマンド定義を提供する。
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd は gdr のルートコマンド。
var rootCmd = &cobra.Command{
	Use:   "gdr",
	Short: "Google Drive を操作する CLI ツール",
	Long: `gdr は Google Drive をコマンドラインから操作するツールです。

マイドライブ起点のパス (例: /dir/file.txt) でファイルを指定し、
ls での一覧表示と cp でのダウンロードができます。パスにはワイルドカード
(*, ?, [...]) を使え、Tab 補完にも対応します。

認証はサービスアカウント鍵を使わず、OAuth でユーザーの Google アカウントに
ログインします。初回は 'gdr auth login' を実行してください。`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// SetVersionInfo はビルド時に注入されたバージョン情報をルートコマンドへ設定する。
// これにより `gdr --version` / `gdr version` で表示できる。
func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

// Execute はルートコマンドを実行する。main から呼ばれる。
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
}
