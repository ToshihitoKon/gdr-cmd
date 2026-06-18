// Package config は gdr-cmd の設定・認証情報の保存先パスを解決する。
//
// 保存先は XDG Base Directory 仕様に従う:
//   - 設定/トークン: $XDG_CONFIG_HOME/gdr-cmd/ (既定 ~/.config/gdr-cmd/)
//
// OAuth クライアント情報 (client_id / client_secret) は次の優先順で読む:
//  1. 環境変数 GDR_CLIENT_ID / GDR_CLIENT_SECRET
//  2. 設定ファイル credentials.json (GCP Console からダウンロードした形式)
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// appDir は XDG config ディレクトリ配下に作るアプリ専用ディレクトリ名。
	appDir = "gdr-cmd"

	credentialsFileName = "credentials.json"
	tokenFileName       = "token.json"

	envClientID     = "GDR_CLIENT_ID"
	envClientSecret = "GDR_CLIENT_SECRET"
)

// ClientCredentials は OAuth クライアントの認証情報。
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

// Dir は gdr-cmd の設定ディレクトリの絶対パスを返す。
// $XDG_CONFIG_HOME が未設定なら ~/.config を使う。
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリの取得に失敗: %w", err)
	}
	return filepath.Join(home, ".config", appDir), nil
}

// EnsureDir は設定ディレクトリを作成し、その絶対パスを返す。
// トークンや認証情報を含むため、パーミッションは 0700 とする。
func EnsureDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("設定ディレクトリの作成に失敗 (%s): %w", dir, err)
	}
	return dir, nil
}

// TokenPath は OAuth トークンキャッシュファイルのパスを返す。
func TokenPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tokenFileName), nil
}

// gcpCredentialsFile は GCP Console がダウンロードさせる
// credentials.json のスキーマ。デスクトップアプリ種別では "installed"、
// ウェブアプリ種別では "web" キーの下に値が入るため両方を許容する。
type gcpCredentialsFile struct {
	Installed *oauthClientSection `json:"installed"`
	Web       *oauthClientSection `json:"web"`
}

type oauthClientSection struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// LoadClientCredentials は OAuth クライアント情報を解決して返す。
//
// 環境変数 GDR_CLIENT_ID / GDR_CLIENT_SECRET が両方設定されていれば
// それを優先する。なければ credentials.json を読む。どちらも無ければ
// 設定方法を案内するエラーを返す。
func LoadClientCredentials() (ClientCredentials, error) {
	if id := os.Getenv(envClientID); id != "" {
		secret := os.Getenv(envClientSecret)
		if secret == "" {
			return ClientCredentials{}, fmt.Errorf("%s が設定されていますが %s が未設定です", envClientID, envClientSecret)
		}
		return ClientCredentials{ClientID: id, ClientSecret: secret}, nil
	}

	dir, err := Dir()
	if err != nil {
		return ClientCredentials{}, err
	}
	credPath := filepath.Join(dir, credentialsFileName)
	data, err := os.ReadFile(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ClientCredentials{}, fmt.Errorf(
				"OAuth クライアント情報が見つかりません。\n"+
					"環境変数 %s / %s を設定するか、GCP Console でダウンロードした\n"+
					"認証情報 JSON を %s に配置してください。",
				envClientID, envClientSecret, credPath)
		}
		return ClientCredentials{}, fmt.Errorf("認証情報ファイルの読み込みに失敗 (%s): %w", credPath, err)
	}

	var f gcpCredentialsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return ClientCredentials{}, fmt.Errorf("認証情報ファイルの解析に失敗 (%s): %w", credPath, err)
	}
	section := f.Installed
	if section == nil {
		section = f.Web
	}
	if section == nil || section.ClientID == "" || section.ClientSecret == "" {
		return ClientCredentials{}, fmt.Errorf(
			"認証情報ファイルに client_id / client_secret が見つかりません (%s)。\n"+
				"GCP Console の「デスクトップアプリ」種別の OAuth クライアントをダウンロードしてください。", credPath)
	}
	return ClientCredentials{ClientID: section.ClientID, ClientSecret: section.ClientSecret}, nil
}

// CredentialsPath は credentials.json の想定パスを返す (案内表示用)。
func CredentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}
