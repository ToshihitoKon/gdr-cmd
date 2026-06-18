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

// EnsureChildFolder は parentID 直下に name のフォルダを確保し、その ID を返す。
//
// 同名フォルダが既にあればそれを再利用する。同名のフォルダは無いが同名の「ファイル」
// が存在する場合は、ファイルの横に紛らわしいフォルダを黙って作らずエラーを返す。
// displayPath はエラーメッセージ用の表示パス (空でも可)。
func (c *Client) EnsureChildFolder(ctx context.Context, parentID, name, displayPath string) (string, error) {
	children, err := c.ListChildrenByName(ctx, parentID, name)
	if err != nil {
		return "", err
	}
	fileExists := false
	for _, ch := range children {
		if ch.IsFolder() {
			return ch.ID, nil
		}
		fileExists = true
	}
	if fileExists {
		where := displayPath
		if where == "" {
			where = name
		}
		return "", fmt.Errorf("同名のファイルが既に存在するためフォルダを作成できません: %s", where)
	}
	created, err := c.Mkdir(ctx, parentID, name)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// EnsureFolderPath はマイドライブ起点の絶対パスのフォルダ階層を、無い段を
// 作りながら解決し、末端フォルダの ID を返す (mkdir -p 相当)。各階層は
// EnsureChildFolder で確保するため、同名ファイル衝突時はエラーになる。
func (c *Client) EnsureFolderPath(ctx context.Context, absPath string) (string, error) {
	rootID, err := c.RootID(ctx)
	if err != nil {
		return "", err
	}
	parentID := rootID
	resolved := "" // ここまで辿った絶対パス (エラーメッセージ用)
	for _, name := range splitPath(absPath) {
		resolved = joinPath(resolved, name)
		parentID, err = c.EnsureChildFolder(ctx, parentID, name, resolved)
		if err != nil {
			return "", err
		}
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

// MoveRename は親の付け替えと名前変更を 1 回の更新で行う (原子的)。
// oldParentID/newParentID が同じ (または空) なら付け替えはしない。newName が
// 空なら名前は変えない。リネーム付き移動で中間状態が残らないようにするために使う。
func (c *Client) MoveRename(ctx context.Context, fileID, newParentID, oldParentID, newName string) error {
	meta := &drive.File{}
	if newName != "" {
		meta.Name = newName
	}
	call := c.svc.Files.Update(fileID, meta)
	if newParentID != "" && newParentID != oldParentID {
		call = call.AddParents(newParentID).RemoveParents(oldParentID)
	}
	if _, err := call.Fields("id, name, parents").Context(ctx).Do(); err != nil {
		return fmt.Errorf("移動・リネームに失敗しました: %w", err)
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
