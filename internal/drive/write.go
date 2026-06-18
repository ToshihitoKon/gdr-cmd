package drive

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

// Upload はローカルの内容を Drive の parentID 直下に name で新規作成する。
// modTime が非ゼロなら Drive 側の更新日時として設定する (sync の差分判定で
// ローカルの mtime を保つため)。作成されたファイルのメタデータを返す。
func (c *Client) Upload(ctx context.Context, parentID, name string, content io.Reader, modTime time.Time) (File, error) {
	meta := &drive.File{
		Name:    name,
		Parents: []string{parentID},
	}
	if !modTime.IsZero() {
		meta.ModifiedTime = modTime.UTC().Format(time.RFC3339)
	}
	created, err := c.svc.Files.Create(meta).
		Media(content, googleapi.ContentType("application/octet-stream")).
		Fields("id, name, mimeType, size, modifiedTime, md5Checksum").
		Context(ctx).
		Do()
	if err != nil {
		return File{}, fmt.Errorf("アップロードに失敗しました (%s): %w", name, err)
	}
	return toFile(created), nil
}

// UpdateContent は既存ファイル fileID の内容を上書きする。
// 同名のままバージョンを更新したい場合 (sync の差分転送) に使う。
func (c *Client) UpdateContent(ctx context.Context, fileID string, content io.Reader, modTime time.Time) (File, error) {
	meta := &drive.File{}
	if !modTime.IsZero() {
		meta.ModifiedTime = modTime.UTC().Format(time.RFC3339)
	}
	updated, err := c.svc.Files.Update(fileID, meta).
		Media(content, googleapi.ContentType("application/octet-stream")).
		Fields("id, name, mimeType, size, modifiedTime, md5Checksum").
		Context(ctx).
		Do()
	if err != nil {
		return File{}, fmt.Errorf("ファイル内容の更新に失敗しました: %w", err)
	}
	return toFile(updated), nil
}

// Mkdir は parentID 直下に name のフォルダを 1 つ作成する。
func (c *Client) Mkdir(ctx context.Context, parentID, name string) (File, error) {
	meta := &drive.File{
		Name:     name,
		MimeType: folderMIME,
		Parents:  []string{parentID},
	}
	created, err := c.svc.Files.Create(meta).
		Fields("id, name, mimeType").
		Context(ctx).
		Do()
	if err != nil {
		return File{}, fmt.Errorf("フォルダの作成に失敗しました (%s): %w", name, err)
	}
	return toFile(created), nil
}

// EnsureFolderPath はマイドライブ起点の絶対パスのフォルダ階層を、無い段を
// 作りながら解決し、末端フォルダの ID を返す (mkdir -p 相当)。
//
// 各階層で同名フォルダが既にあればそれを再利用する。同名でフォルダとファイルが
// 混在する場合はフォルダを優先する。
func (c *Client) EnsureFolderPath(ctx context.Context, absPath string) (string, error) {
	rootID, err := c.RootID(ctx)
	if err != nil {
		return "", err
	}
	parentID := rootID
	for _, name := range splitPath(absPath) {
		children, err := c.ListChildrenByName(ctx, parentID, name)
		if err != nil {
			return "", err
		}
		var folderID string
		for _, ch := range children {
			if ch.IsFolder() {
				folderID = ch.ID
				break
			}
		}
		if folderID == "" {
			created, err := c.Mkdir(ctx, parentID, name)
			if err != nil {
				return "", err
			}
			folderID = created.ID
		}
		parentID = folderID
	}
	return parentID, nil
}

// Trash は fileID をゴミ箱へ移動する (trashed=true)。復元可能。
func (c *Client) Trash(ctx context.Context, fileID string) error {
	_, err := c.svc.Files.Update(fileID, &drive.File{Trashed: true}).
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("ゴミ箱への移動に失敗しました: %w", err)
	}
	return nil
}

// Delete は fileID を完全に削除する (ゴミ箱を経由せず復元不可)。
func (c *Client) Delete(ctx context.Context, fileID string) error {
	if err := c.svc.Files.Delete(fileID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("完全削除に失敗しました: %w", err)
	}
	return nil
}

// Move は fileID の親を oldParentID から newParentID へ付け替える。
// Drive ではメタデータ更新だけで移動でき、内容の再アップロードは不要。
func (c *Client) Move(ctx context.Context, fileID, newParentID, oldParentID string) error {
	_, err := c.svc.Files.Update(fileID, &drive.File{}).
		AddParents(newParentID).
		RemoveParents(oldParentID).
		Fields("id, parents").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("移動に失敗しました: %w", err)
	}
	return nil
}

// Rename は fileID の名前を newName に変更する。
func (c *Client) Rename(ctx context.Context, fileID, newName string) error {
	_, err := c.svc.Files.Update(fileID, &drive.File{Name: newName}).
		Fields("id, name").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("名前の変更に失敗しました: %w", err)
	}
	return nil
}
