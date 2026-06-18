package drive

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

// Upload creates a new file with name directly under parentID on Drive from the
// local content. If modTime is non-zero, it is set as the modified time on the
// Drive side (to preserve the local mtime for sync's diff detection). It returns
// the metadata of the created file.
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
		return File{}, fmt.Errorf("failed to upload (%s): %w", name, err)
	}
	return toFile(created), nil
}

// UpdateContent overwrites the content of an existing file fileID.
// Used when you want to update the version while keeping the same name (sync's diff transfer).
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
		return File{}, fmt.Errorf("failed to update file content: %w", err)
	}
	return toFile(updated), nil
}

// Mkdir creates a single folder named name directly under parentID.
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
		return File{}, fmt.Errorf("failed to create folder (%s): %w", name, err)
	}
	return toFile(created), nil
}

// EnsureChildFolder ensures a folder named name directly under parentID and returns its ID.
//
// If a folder with the same name already exists, it is reused. If there is no
// folder with that name but a "file" with the same name exists, it returns an
// error instead of silently creating a confusing folder next to the file.
// displayPath is the display path for the error message (may be empty).
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
		return "", fmt.Errorf("cannot create folder because a file with the same name already exists: %s", where)
	}
	created, err := c.Mkdir(ctx, parentID, name)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// EnsureFolderPath resolves the folder hierarchy of a My Drive-rooted absolute
// path, creating any missing levels along the way, and returns the ID of the
// leaf folder (equivalent to mkdir -p). Each level is ensured with
// EnsureChildFolder, so it errors on a same-name file collision.
func (c *Client) EnsureFolderPath(ctx context.Context, absPath string) (string, error) {
	rootID, err := c.RootID(ctx)
	if err != nil {
		return "", err
	}
	parentID := rootID
	resolved := "" // the absolute path traversed so far (for error messages)
	for _, name := range splitPath(absPath) {
		resolved = joinPath(resolved, name)
		parentID, err = c.EnsureChildFolder(ctx, parentID, name, resolved)
		if err != nil {
			return "", err
		}
	}
	return parentID, nil
}

// Trash moves fileID to the trash (trashed=true). Recoverable.
func (c *Client) Trash(ctx context.Context, fileID string) error {
	_, err := c.svc.Files.Update(fileID, &drive.File{Trashed: true}).
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to move to trash: %w", err)
	}
	return nil
}

// Delete permanently deletes fileID (bypassing the trash, unrecoverable).
func (c *Client) Delete(ctx context.Context, fileID string) error {
	if err := c.svc.Files.Delete(fileID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("failed to permanently delete: %w", err)
	}
	return nil
}

// Move reparents fileID from oldParentID to newParentID.
// In Drive, a move is just a metadata update; re-uploading the content is unnecessary.
func (c *Client) Move(ctx context.Context, fileID, newParentID, oldParentID string) error {
	_, err := c.svc.Files.Update(fileID, &drive.File{}).
		AddParents(newParentID).
		RemoveParents(oldParentID).
		Fields("id, parents").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to move: %w", err)
	}
	return nil
}

// MoveRename reparents and renames in a single update (atomically).
// If oldParentID/newParentID are the same (or empty), no reparenting is done. If
// newName is empty, the name is not changed. Used so that a rename-with-move
// leaves no intermediate state.
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
		return fmt.Errorf("failed to move/rename: %w", err)
	}
	return nil
}

// Rename changes the name of fileID to newName.
func (c *Client) Rename(ctx context.Context, fileID, newName string) error {
	_, err := c.svc.Files.Update(fileID, &drive.File{Name: newName}).
		Fields("id, name").
		Context(ctx).
		Do()
	if err != nil {
		return fmt.Errorf("failed to rename: %w", err)
	}
	return nil
}
