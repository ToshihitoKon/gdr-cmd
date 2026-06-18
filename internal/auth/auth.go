// Package auth は OAuth 2.0 による Google アカウント認証を扱う。
//
// サービスアカウント鍵を手元に持たず、ユーザー自身が GCP Console で作成した
// OAuth クライアント (デスクトップアプリ種別) を使って認証する。取得した
// トークンは設定ディレクトリにキャッシュし、refresh token で自動更新する。
//
// 認証フローについて:
//
//	Google は従来の OOB フロー (redirect_uri=urn:ietf:wg:oauth:2.0:oob) を
//	2022 年に廃止した。このため本パッケージは loopback リダイレクト
//	(http://127.0.0.1:<port>) を用いる。
//
//	既定ではローカルに一時 HTTP サーバを立て、ブラウザの承認後に
//	リダイレクトされる認可コードを自動で受け取る。SSH 越しなどブラウザを
//	ローカルで開けない環境向けに、--no-browser 指定時は手動コピペ方式
//	(リダイレクト先 URL を貼り付けてコードを抽出) にフォールバックする。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	driveapi "google.golang.org/api/drive/v3"
)

// Scopes は要求する OAuth スコープ。
//
// 将来のアップロード対応を見据え、読み書き可能な drive スコープを既定とする
// (ダウンロードだけなら drive.readonly で足りるが、再認証を避けるため最初から
// 書き込み権限まで取得しておく)。
var Scopes = []string{driveapi.DriveScope}

// loginTimeout はブラウザ承認を待つ最大時間。これを超えたら認証を打ち切る。
const loginTimeout = 5 * time.Minute

// Authenticator は OAuth 設定とトークン入出力をまとめて扱う。
type Authenticator struct {
	oauthConfig *oauth2.Config
	tokenPath   string
}

// New は設定ディレクトリの認証情報から認証器を構築する。
// redirectURL は loopback サーバ起動後に確定するため、ここでは空のままにする。
func New() (*Authenticator, error) {
	creds, err := config.LoadClientCredentials()
	if err != nil {
		return nil, err
	}
	tokenPath, err := config.TokenPath()
	if err != nil {
		return nil, err
	}
	return &Authenticator{
		oauthConfig: &oauth2.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			Scopes:       Scopes,
			Endpoint:     google.Endpoint,
		},
		tokenPath: tokenPath,
	}, nil
}

// Client は認証済みの HTTP クライアントを返す。
//
// キャッシュ済みトークンがあればそれを使い (期限切れなら refresh token で
// 自動更新)、無ければエラーを返す。未認証時は呼び出し側で `auth login` を
// 案内する。更新されたトークンはベストエフォートで保存し直す。
func (a *Authenticator) Client(ctx context.Context) (*http.Client, error) {
	tok, err := a.loadToken()
	if err != nil {
		return nil, err
	}

	src := a.oauthConfig.TokenSource(ctx, tok)
	refreshed, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("トークンの更新に失敗しました。再度 `gdr auth login` を実行してください: %w", err)
	}
	// refresh により新しいアクセストークンが発行された場合は保存し直す。
	if refreshed.AccessToken != tok.AccessToken {
		if saveErr := a.saveToken(refreshed); saveErr != nil {
			fmt.Fprintf(os.Stderr, "警告: 更新したトークンの保存に失敗しました: %v\n", saveErr)
		}
	}
	return oauth2.NewClient(ctx, oauth2.StaticTokenSource(refreshed)), nil
}

// HasToken はキャッシュ済みトークンが存在するかを返す。
func (a *Authenticator) HasToken() bool {
	_, err := os.Stat(a.tokenPath)
	return err == nil
}

// Login は対話的に OAuth フローを実行し、取得したトークンを保存する。
// noBrowser が true の場合はブラウザ自動起動と loopback 自動受信を行わず、
// 手動コピペ方式で認可コードを受け取る。
func (a *Authenticator) Login(ctx context.Context, noBrowser bool) error {
	if noBrowser {
		return a.loginManual(ctx)
	}
	return a.loginLoopback(ctx)
}

// loginLoopback はローカル HTTP サーバで認可コードを自動受信する。
func (a *Authenticator) loginLoopback(ctx context.Context) error {
	// ポートを OS に選ばせ、確定したアドレスで redirect_uri を組み立てる。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("ローカルサーバの起動に失敗しました。--no-browser での手動認証を試してください: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	a.oauthConfig.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	state, err := randomState()
	if err != nil {
		return err
	}
	authURL := a.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	// 認可コードまたはエラーをハンドラからメインへ渡すチャネル。
	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)

	srv := &http.Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			writeBrowserMessage(w, "認証が拒否されました: "+e)
			resultCh <- result{err: fmt.Errorf("認証が拒否されました: %s", e)}
			return
		}
		if q.Get("state") != state {
			writeBrowserMessage(w, "state が一致しません。認証をやり直してください。")
			resultCh <- result{err: fmt.Errorf("state の不一致: CSRF の可能性があります")}
			return
		}
		code := q.Get("code")
		if code == "" {
			writeBrowserMessage(w, "認可コードを取得できませんでした。")
			resultCh <- result{err: fmt.Errorf("認可コードが空です")}
			return
		}
		writeBrowserMessage(w, "認証に成功しました。このタブを閉じてターミナルに戻ってください。")
		resultCh <- result{code: code}
	})
	srv.Handler = mux

	go func() { _ = srv.Serve(listener) }()
	defer srv.Close()

	fmt.Fprintln(os.Stderr, "ブラウザで以下の URL を開いて認証してください:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+authURL)
	fmt.Fprintln(os.Stderr, "")
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintln(os.Stderr, "(ブラウザの自動起動に失敗しました。上記 URL を手動で開いてください)")
	}
	fmt.Fprintln(os.Stderr, "認証の完了を待っています...")

	timeoutCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return fmt.Errorf("認証がタイムアウトしました (%s)", loginTimeout)
	case res := <-resultCh:
		if res.err != nil {
			return res.err
		}
		return a.exchangeAndSave(ctx, res.code)
	}
}

// loginManual は手動コピペ方式で認可コードを受け取る。
//
// loopback リダイレクト先 (127.0.0.1) は実際には待ち受けないため、ブラウザは
// 「接続できない」旨を表示する。利用者にはそのアドレスバーの URL 全体を
// 貼り付けてもらい、そこから code クエリを抽出する。素の認可コードの貼り付けも
// 許容する。
func (a *Authenticator) loginManual(ctx context.Context) error {
	// 待ち受けないが、ブラウザのアドレスバーから code を読めれば十分。
	// 固定ポートにして利用者が URL を判別しやすくする。
	const manualPort = 9999
	a.oauthConfig.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", manualPort)

	state, err := randomState()
	if err != nil {
		return err
	}
	authURL := a.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	fmt.Fprintln(os.Stderr, "ブラウザ (どの端末でも可) で以下の URL を開いて認証してください:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+authURL)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "承認後、ブラウザは 127.0.0.1 への接続エラーを表示します (正常です)。")
	fmt.Fprintln(os.Stderr, "そのときのアドレスバーの URL 全体を、ここに貼り付けて Enter を押してください:")
	fmt.Fprint(os.Stderr, "> ")

	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		return fmt.Errorf("入力の読み取りに失敗しました: %w", err)
	}
	code, err := extractCode(input, state)
	if err != nil {
		return err
	}
	return a.exchangeAndSave(ctx, code)
}

// extractCode は貼り付け文字列から認可コードを取り出す。
// URL 形式なら code クエリを抽出して state を検証し、そうでなければ
// 入力そのものを認可コードとして扱う。
func extractCode(input, wantState string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("入力が空です")
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("URL の解析に失敗しました: %w", err)
		}
		q := u.Query()
		if e := q.Get("error"); e != "" {
			return "", fmt.Errorf("認証が拒否されました: %s", e)
		}
		if s := q.Get("state"); s != "" && s != wantState {
			return "", fmt.Errorf("state が一致しません: CSRF の可能性があります")
		}
		code := q.Get("code")
		if code == "" {
			return "", fmt.Errorf("URL に認可コード (code) が含まれていません")
		}
		return code, nil
	}
	return input, nil
}

// exchangeAndSave は認可コードをトークンに交換して保存する。
func (a *Authenticator) exchangeAndSave(ctx context.Context, code string) error {
	tok, err := a.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("認可コードのトークン交換に失敗しました: %w", err)
	}
	if tok.RefreshToken == "" {
		// refresh token が無いと次回以降に再認証が必要になる。
		// ApprovalForce を付けているため通常は発行されるが、念のため警告する。
		fmt.Fprintln(os.Stderr, "警告: refresh token が取得できませんでした。次回も認証が必要になる場合があります。")
	}
	if err := a.saveToken(tok); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "認証に成功し、トークンを保存しました。")
	return nil
}

// Logout はキャッシュ済みトークンを削除する。
func (a *Authenticator) Logout() error {
	err := os.Remove(a.tokenPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("トークンの削除に失敗しました: %w", err)
	}
	return nil
}

// loadToken はキャッシュ済みトークンを読み込む。
func (a *Authenticator) loadToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(a.tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("未認証です。先に `gdr auth login` を実行してください")
		}
		return nil, fmt.Errorf("トークンの読み込みに失敗しました: %w", err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("トークンの解析に失敗しました。`gdr auth login` で再認証してください: %w", err)
	}
	return &tok, nil
}

// saveToken はトークンを 0600 で保存する (認証情報のため所有者のみ読み書き)。
func (a *Authenticator) saveToken(tok *oauth2.Token) error {
	if _, err := config.EnsureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("トークンのシリアライズに失敗しました: %w", err)
	}
	if err := os.WriteFile(a.tokenPath, data, 0o600); err != nil {
		return fmt.Errorf("トークンの保存に失敗しました: %w", err)
	}
	return nil
}

// randomState は CSRF 対策の state 値を生成する。
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("state の生成に失敗しました: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// writeBrowserMessage はブラウザ向けに簡単な HTML メッセージを返す。
func writeBrowserMessage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body style='font-family:sans-serif;padding:2em'><p>%s</p></body></html>", msg)
}

// openBrowser は OS 既定のブラウザで URL を開く。
func openBrowser(rawURL string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", rawURL}
	default: // linux 等
		cmd = "xdg-open"
		args = []string{rawURL}
	}
	return exec.Command(cmd, args...).Start()
}
