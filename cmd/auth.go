package cmd

import (
	"fmt"
	"os"

	"github.com/ToshihitoKon/gdr-cmd/internal/auth"
	"github.com/ToshihitoKon/gdr-cmd/internal/config"
	"github.com/spf13/cobra"
)

var authNoBrowser bool

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "認証を管理する",
	Long:  "OAuth による Google アカウントへのログイン・ログアウト・状態確認を行います。",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Google アカウントにログインする",
	Long: `OAuth フローを実行してトークンを取得・保存します。

既定ではブラウザを自動起動し、ローカルの一時ポートで認可コードを受信します。
SSH 越しなどブラウザをローカルで開けない環境では --no-browser を指定すると、
表示された URL を任意の端末のブラウザで開き、リダイレクト先 URL を貼り付ける
手動方式になります。

事前に GCP Console で「デスクトップアプリ」種別の OAuth クライアントを作成し、
認証情報を環境変数か credentials.json で設定しておく必要があります
(詳細は README を参照)。`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		authn, err := auth.New()
		if err != nil {
			return err
		}
		return authn.Login(cmd.Context(), authNoBrowser)
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "保存済みトークンを削除する",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		authn, err := auth.New()
		if err != nil {
			return err
		}
		if err := authn.Logout(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "ログアウトしました。")
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "認証状態を表示する",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenPath, err := config.TokenPath()
		if err != nil {
			return err
		}
		authn, err := auth.New()
		if err != nil {
			// 認証情報自体が無い場合もここに来る。状態としては未設定。
			fmt.Fprintln(os.Stderr, "認証情報が未設定です:", err)
			return nil
		}
		if authn.HasToken() {
			fmt.Printf("ログイン済み (トークン: %s)\n", tokenPath)
		} else {
			fmt.Println("未ログインです。`gdr auth login` を実行してください。")
		}
		return nil
	},
}

func init() {
	authLoginCmd.Flags().BoolVar(&authNoBrowser, "no-browser", false,
		"ブラウザを自動起動せず手動コピペで認証する (SSH 越しなどブラウザを開けない環境向け)")

	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
