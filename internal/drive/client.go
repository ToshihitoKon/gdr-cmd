// Package drive provides a thin wrapper around the Google Drive API (v3).
//
// It exposes only the operations needed by the upper layers (path resolution,
// glob expansion, commands): listing children, getting the root, and
// downloading files. It is limited to operations rooted at My Drive; shared
// drives are out of scope.
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

// folderMIME is the MIME type of a folder in Drive.
const folderMIME = "application/vnd.google-apps.folder"

// googleAppPrefix is the MIME prefix of Google-native formats (Docs/Sheets, etc.).
// These cannot be downloaded as plain binaries and require Export instead.
const googleAppPrefix = "application/vnd.google-apps."

// listFields are the fields requested when listing. It is narrowed to only what
// is needed to keep the response light. nextPageToken is required for paging.
const listFields = "nextPageToken, files(id, name, mimeType, size, modifiedTime, md5Checksum)"

// Client wraps the Drive API service.
type Client struct {
	svc *drive.Service
}

// File is the minimal metadata of a file/folder on Drive.
// Rather than exposing drive.File directly, it is shaped into a form that is
// easy for the upper layers to handle.
type File struct {
	ID           string
	Name         string
	MimeType     string
	Size         int64
	ModifiedTime string
	MD5          string
}

// IsFolder reports whether this is a folder.
func (f File) IsFolder() bool {
	return f.MimeType == folderMIME
}

// IsGoogleDoc reports whether this is a Google-native format (Docs/Sheets, etc.).
// These cannot be downloaded without an Export.
func (f File) IsGoogleDoc() bool {
	return strings.HasPrefix(f.MimeType, googleAppPrefix) && !f.IsFolder()
}

// New creates a Drive client from an authenticated client.
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
		return nil, fmt.Errorf("failed to initialize the Drive service: %w", err)
	}
	return &Client{svc: svc}, nil
}

// RootID returns the My Drive root folder ID.
// The Drive API lets you reference the root with the alias "root", but the
// actual ID is needed for things like parent comparison, so it is resolved and returned.
func (c *Client) RootID(ctx context.Context) (string, error) {
	f, err := c.svc.Files.Get("root").
		Fields("id").
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("failed to get the root folder: %w", err)
	}
	return f.Id, nil
}

// ListChildren returns all (non-trashed) children directly under the given folder.
// Paging is handled internally.
func (c *Client) ListChildren(ctx context.Context, parentID string) ([]File, error) {
	query := fmt.Sprintf("'%s' in parents and trashed = false", escapeQueryValue(parentID))
	return c.listByQuery(ctx, query)
}

// ListChildrenByName returns the children directly under the given folder whose
// name exactly matches name. Drive allows duplicate names, so multiple results
// may be returned.
func (c *Client) ListChildrenByName(ctx context.Context, parentID, name string) ([]File, error) {
	query := fmt.Sprintf("'%s' in parents and name = '%s' and trashed = false",
		escapeQueryValue(parentID), escapeQueryValue(name))
	return c.listByQuery(ctx, query)
}

// listByQuery runs a Drive query and gathers all results while paging.
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
			return nil, fmt.Errorf("failed to list files: %w", err)
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

// Download returns a ReadCloser for reading the file's content.
// It cannot be used for Google-native formats (the caller must check IsGoogleDoc).
func (c *Client) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	resp, err := c.svc.Files.Get(fileID).
		Context(ctx).
		Download()
	if err != nil {
		return nil, fmt.Errorf("failed to download the file: %w", err)
	}
	return resp.Body, nil
}

// toFile converts a drive.File into the internal representation.
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

// escapeQueryValue escapes a value so it can be embedded in a Drive query string
// literal. The backslash and single quote are the special characters.
// Reference: https://developers.google.com/drive/api/guides/ref-search-terms
func escapeQueryValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
