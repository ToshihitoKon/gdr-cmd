// Package drive は Google Drive API (v3) の薄いラッパーを提供する。
//
// 上位レイヤ (パス解決・glob 展開・コマンド) が必要とする操作だけを公開する:
// 子要素の列挙、ルートの取得、ファイルのダウンロード。マイドライブ起点の
// 操作に絞り、共有ドライブは対象外とする。
package drive

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ToshihitoKon/gdr-cmd/internal/auth"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// folderMIME は Drive におけるフォルダの MIME タイプ。
const folderMIME = "application/vnd.google-apps.folder"

// googleAppPrefix は Google ネイティブ形式 (Docs/Sheets 等) の MIME 接頭辞。
// これらは通常のバイナリダウンロードができず Export が必要になる。
const googleAppPrefix = "application/vnd.google-apps."

// listFields は一覧取得で要求するフィールド。必要なものだけに絞って
// レスポンスを軽くする。nextPageToken はページングに必須。
const listFields = "nextPageToken, files(id, name, mimeType, size, modifiedTime, md5Checksum)"

// Client は Drive API サービスをラップする。
type Client struct {
	svc *drive.Service
}

// File は Drive 上のファイル/フォルダの最小限のメタデータ。
// drive.File をそのまま外へ出さず、上位レイヤが扱いやすい形に整える。
type File struct {
	ID           string
	Name         string
	MimeType     string
	Size         int64
	ModifiedTime string
	MD5          string
}

// IsFolder はフォルダかどうかを返す。
func (f File) IsFolder() bool {
	return f.MimeType == folderMIME
}

// IsGoogleDoc は Google ネイティブ形式 (Docs/Sheets 等) かどうかを返す。
// これらは Export しないとダウンロードできない。
func (f File) IsGoogleDoc() bool {
	return strings.HasPrefix(f.MimeType, googleAppPrefix) && !f.IsFolder()
}

// New は認証済みクライアントから Drive クライアントを生成する。
func New(ctx context.Context) (*Client, error) {
	authn, err := auth.New()
	if err != nil {
		return nil, err
	}
	httpClient, err := authn.Client(ctx)
	if err != nil {
		return nil, err
	}
	svc, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("Drive サービスの初期化に失敗しました: %w", err)
	}
	return &Client{svc: svc}, nil
}

// RootID はマイドライブのルートフォルダ ID を返す。
// Drive API ではエイリアス "root" でルートを参照できるが、parent 比較などで
// 実体 ID が要るため解決して返す。
func (c *Client) RootID(ctx context.Context) (string, error) {
	f, err := c.svc.Files.Get("root").
		Fields("id").
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("ルートフォルダの取得に失敗しました: %w", err)
	}
	return f.Id, nil
}

// ListChildren は指定フォルダ直下の (ゴミ箱を除く) 子要素を全件返す。
// ページングは内部で処理する。
func (c *Client) ListChildren(ctx context.Context, parentID string) ([]File, error) {
	query := fmt.Sprintf("'%s' in parents and trashed = false", escapeQueryValue(parentID))
	return c.listByQuery(ctx, query)
}

// ListChildrenByName は指定フォルダ直下で name に完全一致する子要素を返す。
// Drive は同名を許すため複数件返りうる。
func (c *Client) ListChildrenByName(ctx context.Context, parentID, name string) ([]File, error) {
	query := fmt.Sprintf("'%s' in parents and name = '%s' and trashed = false",
		escapeQueryValue(parentID), escapeQueryValue(name))
	return c.listByQuery(ctx, query)
}

// listByQuery は Drive クエリを実行し、ページングしながら全件を集める。
func (c *Client) listByQuery(ctx context.Context, query string) ([]File, error) {
	var files []File
	pageToken := ""
	for {
		call := c.svc.Files.List().
			Q(query).
			Fields(listFields).
			PageSize(1000).
			OrderBy("name").
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("ファイル一覧の取得に失敗しました: %w", err)
		}
		for _, f := range resp.Files {
			files = append(files, toFile(f))
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return files, nil
}

// Download はファイルの内容を読み出すための ReadCloser を返す。
// Google ネイティブ形式には使えない (呼び出し側で IsGoogleDoc を確認すること)。
func (c *Client) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	resp, err := c.svc.Files.Get(fileID).
		Context(ctx).
		Download()
	if err != nil {
		return nil, fmt.Errorf("ファイルのダウンロードに失敗しました: %w", err)
	}
	return resp.Body, nil
}

// toFile は drive.File を内部表現へ変換する。
func toFile(f *drive.File) File {
	return File{
		ID:           f.Id,
		Name:         f.Name,
		MimeType:     f.MimeType,
		Size:         f.Size,
		ModifiedTime: f.ModifiedTime,
		MD5:          f.Md5Checksum,
	}
}

// escapeQueryValue は Drive クエリの文字列リテラルに値を埋め込めるよう
// エスケープする。バックスラッシュとシングルクォートが特殊文字。
// 参考: https://developers.google.com/drive/api/guides/ref-search-terms
func escapeQueryValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
