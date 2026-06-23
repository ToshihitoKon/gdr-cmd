package cmd

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/ignore"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

// gdrignoreFile is the name of the ignore file read from the local sync root.
// Its patterns exclude matching paths from sync in either direction; the file
// itself is always excluded.
const gdrignoreFile = ".gdrignore"

var (
	syncDelete   bool
	syncDryRun   bool
	syncChecksum bool
)

var syncCmd = &cobra.Command{
	Use:   "sync SOURCE DEST",
	Short: "Sync a directory one-way",
	Long: `Sync the SOURCE directory tree to DEST one-way. The direction is determined
by which of SOURCE and DEST has the drive: prefix:

  drive: -> local ... sync from Drive to local (download)
  local -> drive: ... sync from local to Drive (upload)

Differences are judged by size and modification time. Files are skipped when
the size matches and the destination is the same age or newer; otherwise they
are transferred. With --delete, files that exist only in DEST and not in SOURCE
are removed (trashed on the Drive side). With --dry-run, no transfer happens and
only the planned operations are shown.

Google-native formats (Google Docs etc.) are not synced.

If a .gdrignore file exists in the local sync root, its .gitignore-style
patterns exclude matching paths from sync in both directions. The .gdrignore
file itself is never synced.

Examples:
  gdr sync ./site drive:/backup/site
  gdr sync drive:/Photos ./photos
  gdr sync --delete ./site drive:/backup/site
  gdr sync --dry-run ./site drive:/backup/site`,
	Args:              cobra.ExactArgs(2),
	RunE:              runSync,
	ValidArgsFunction: completeLocationArgs,
}

func init() {
	syncCmd.Flags().BoolVar(&syncDelete, "delete", false, "remove files from DEST that are not in SOURCE (trashed on Drive)")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "do not transfer; only show the planned operations")
	syncCmd.Flags().BoolVar(&syncChecksum, "checksum", false, "also compare MD5 for same-size files and transfer if content differs (slower but reliable)")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	src := loc.Parse(args[0])
	dst := loc.Parse(args[1])

	switch {
	case src.IsDrive() && dst.IsLocal():
		return syncDriveToLocal(ctx, src.Path, dst.Path)
	case src.IsLocal() && dst.IsDrive():
		return syncLocalToDrive(ctx, src.Path, dst.Path)
	case src.IsDrive() && dst.IsDrive():
		return fmt.Errorf("Drive-to-Drive sync is not supported")
	default:
		return fmt.Errorf("local-to-local sync is not supported; add drive: to include a Drive side")
	}
}

// entry is the minimal metadata for one sync target, keyed by relative path.
type entry struct {
	size    int64
	modTime time.Time
	isDir   bool
	// driveID is the file ID of a Drive-side entry (empty on the local side).
	driveID string
	// isGoogleDoc reports whether this is a Drive-native format (used to exclude it from sync).
	isGoogleDoc bool
	// md5 is the Drive-side MD5 checksum (used for --checksum comparison; empty on the local side).
	md5 string
}

// needsTransfer reports whether src should be transferred to dst.
//
// The decision is as follows (close to rsync's default):
//   - dst does not exist -> transfer
//   - sizes differ -> transfer
//   - sizes match and src is newer than dst -> transfer
//   - otherwise (sizes match and dst is at least as new) -> skip
//
// Modification times have different precision on Drive (milliseconds) and
// locally (nanoseconds), so they are truncated to seconds before comparing.
func needsTransfer(srcSize int64, srcMod time.Time, dstExists bool, dstSize int64, dstMod time.Time) bool {
	if !dstExists {
		return true
	}
	if srcSize != dstSize {
		return true
	}
	return srcMod.Truncate(time.Second).After(dstMod.Truncate(time.Second))
}

// ---- local -> Drive ----

func syncLocalToDrive(ctx context.Context, localRoot, driveRoot string) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	info, err := os.Stat(localRoot)
	if err != nil {
		return fmt.Errorf("source not found (%s): %w", localRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("sync source must be a directory: %s", localRoot)
	}

	// .gdrignore lives in the local root and applies to both trees, keyed by the
	// shared relative path.
	matcher, err := ignore.Load(filepath.Join(localRoot, gdrignoreFile))
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", gdrignoreFile, err)
	}

	srcTree, err := buildLocalTree(localRoot, matcher)
	if err != nil {
		return err
	}

	// Ensure the DEST root folder.
	destRootID, err := client.EnsureFolderPath(ctx, driveRoot)
	if err != nil {
		return err
	}
	dstTree, err := buildDriveTree(ctx, client, destRootID)
	if err != nil {
		return err
	}
	// Ignored Drive-side entries must also drop out of dstTree; otherwise
	// --delete would treat them as "extra in DEST" and remove them.
	filterIgnored(dstTree, matcher)

	// Process relative paths shallowest-first so folders are created before
	// their files are uploaded. Subtrees whose type conflicts (folder on one
	// side, file on the other) are skipped entirely.
	var skipSubtrees []string
	for _, rel := range sortedKeys(srcTree) {
		se := srcTree[rel]
		de, exists := dstTree[rel]

		if isUnderAny(rel, skipSubtrees) {
			continue
		}

		// If the type (folder/file) differs between SOURCE and DEST, skip it so
		// we don't accidentally overwrite a folder with a file or vice versa.
		if exists && se.isDir != de.isDir {
			fmt.Fprintf(os.Stderr, "sync: skip (type differs from destination): drive:%s\n", path.Join(driveRoot, rel))
			if se.isDir {
				skipSubtrees = append(skipSubtrees, rel)
			}
			continue
		}

		if se.isDir {
			if !exists {
				if syncDryRun {
					fmt.Fprintf(os.Stderr, "[dry-run] mkdir drive:%s\n", path.Join(driveRoot, rel))
					continue
				}
				if _, err := client.EnsureFolderPath(ctx, path.Join(driveRoot, rel)); err != nil {
					return err
				}
			}
			continue
		}

		// If the destination is a Google-native format, overwriting its content
		// with octet-stream would destroy the document, so skip the transfer
		// (excluded just like in the download direction).
		if exists && de.isGoogleDoc {
			fmt.Fprintf(os.Stderr, "sync: skip (cannot overwrite a Google-native format): drive:%s\n", path.Join(driveRoot, rel))
			continue
		}

		localPath := filepath.Join(localRoot, filepath.FromSlash(rel))
		transfer := needsTransfer(se.size, se.modTime, exists, de.size, de.modTime)
		// With --checksum, even when size matches and mtime would skip it,
		// compare MD5 and transfer if content differs (avoids missing size collisions).
		if !transfer && syncChecksum && exists && se.size == de.size {
			differs, err := contentDiffers(localPath, de.md5)
			if err != nil {
				return err
			}
			transfer = differs
		}
		if !transfer {
			continue
		}

		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] upload %s -> drive:%s\n", localPath, path.Join(driveRoot, rel))
			continue
		}
		if err := uploadForSync(ctx, client, localPath, rel, driveRoot, destRootID, de, exists); err != nil {
			return err
		}
	}

	if syncDelete {
		if err := deleteExtraOnDrive(ctx, client, srcTree, dstTree, driveRoot); err != nil {
			return err
		}
	}
	return nil
}

// uploadForSync uploads one sync file to Drive. If it already exists, its
// content is updated; otherwise the parent folder is ensured and it is created.
func uploadForSync(ctx context.Context, client *drive.Client, localPath, rel, driveRoot, destRootID string, existing entry, exists bool) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}
	defer f.Close()

	if exists && existing.driveID != "" {
		if _, err := client.UpdateContent(ctx, existing.driveID, f, info.ModTime()); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Updated: %s -> drive:%s\n", localPath, path.Join(driveRoot, rel))
		return nil
	}

	// Ensure the parent folder and upload as a new file.
	parentRel := path.Dir(rel)
	parentID := destRootID
	if parentRel != "." && parentRel != "/" {
		parentID, err = client.EnsureFolderPath(ctx, path.Join(driveRoot, parentRel))
		if err != nil {
			return err
		}
	}
	if _, err := client.Upload(ctx, parentID, path.Base(rel), f, info.ModTime()); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Upload: %s -> drive:%s\n", localPath, path.Join(driveRoot, rel))
	return nil
}

// deleteExtraOnDrive removes Drive-side entries that exist only in DEST and not
// in SOURCE. It processes deepest-first so folders are removed last.
func deleteExtraOnDrive(ctx context.Context, client *drive.Client, srcTree, dstTree map[string]entry, driveRoot string) error {
	keys := sortedKeys(dstTree)
	// Reverse to deepest-first (delete children before their parents).
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	for _, rel := range keys {
		if _, ok := srcTree[rel]; ok {
			continue
		}
		de := dstTree[rel]
		full := path.Join(driveRoot, rel)
		// Google-native formats are excluded from sync, so don't delete them
		// even with --delete (deleting what the transfer path can't handle
		// would be inconsistent).
		if de.isGoogleDoc {
			fmt.Fprintf(os.Stderr, "sync: skip delete (Google-native format is excluded): drive:%s\n", full)
			continue
		}
		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] delete drive:%s\n", full)
			continue
		}
		if err := client.Trash(ctx, de.driveID); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Trashed: drive:%s\n", full)
	}
	return nil
}

// ---- Drive -> local ----

func syncDriveToLocal(ctx context.Context, driveRoot, localRoot string) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	// Resolve the SOURCE Drive folder.
	srcID, ok, err := resolveExistingFolder(ctx, client, driveRoot)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("source Drive folder not found: %s", driveRoot)
	}

	// .gdrignore is read from the local DEST root and applied to both sides,
	// keyed by the shared relative path. Read it before creating the directory
	// so a pre-existing ignore file is honored.
	matcher, err := ignore.Load(filepath.Join(localRoot, gdrignoreFile))
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", gdrignoreFile, err)
	}

	srcTree, err := buildDriveTree(ctx, client, srcID)
	if err != nil {
		return err
	}
	filterIgnored(srcTree, matcher)

	if !syncDryRun {
		if err := os.MkdirAll(localRoot, 0o755); err != nil {
			return fmt.Errorf("failed to create destination (%s): %w", localRoot, err)
		}
	}
	dstTree, err := buildLocalTree(localRoot, matcher)
	if err != nil {
		return err
	}

	// Subtrees whose type conflicts (folder on one side, file on the other) are skipped entirely.
	var skipSubtrees []string
	for _, rel := range sortedKeys(srcTree) {
		se := srcTree[rel]
		de, exists := dstTree[rel]
		localPath := filepath.Join(localRoot, filepath.FromSlash(rel))

		if isUnderAny(rel, skipSubtrees) {
			continue
		}

		// Skip when the type differs between SOURCE (Drive) and DEST (local)
		// (writing a file where a folder is, or vice versa, would make
		// os.MkdirAll and the like fail).
		if exists && se.isDir != de.isDir {
			fmt.Fprintf(os.Stderr, "sync: skip (type differs from destination): %s\n", localPath)
			if se.isDir {
				skipSubtrees = append(skipSubtrees, rel)
			}
			continue
		}

		if se.isDir {
			if !exists {
				if syncDryRun {
					fmt.Fprintf(os.Stderr, "[dry-run] mkdir %s\n", localPath)
					continue
				}
				if err := os.MkdirAll(localPath, 0o755); err != nil {
					return err
				}
			}
			continue
		}

		if se.isGoogleDoc {
			fmt.Fprintf(os.Stderr, "sync: skip (Google-native format is not supported): drive:%s\n", path.Join(driveRoot, rel))
			continue
		}

		transfer := needsTransfer(se.size, se.modTime, exists, de.size, de.modTime)
		// With --checksum, even when size matches and mtime would skip it,
		// compare MD5 and transfer if content differs. The Drive-side md5 is
		// matched against the local file.
		if !transfer && syncChecksum && exists && se.size == de.size {
			differs, err := contentDiffers(localPath, se.md5)
			if err != nil {
				return err
			}
			transfer = differs
		}
		if !transfer {
			continue
		}

		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] download drive:%s -> %s\n", path.Join(driveRoot, rel), localPath)
			continue
		}
		if err := downloadForSync(ctx, client, se, localPath); err != nil {
			return err
		}
	}

	if syncDelete {
		if err := deleteExtraOnLocal(srcTree, dstTree, localRoot); err != nil {
			return err
		}
	}
	return nil
}

// downloadForSync writes one sync file to local and matches its mtime to the
// Drive side (to keep later difference checks stable).
func downloadForSync(ctx context.Context, client *drive.Client, se entry, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	body, err := client.Download(ctx, se.driveID)
	if err != nil {
		return err
	}
	defer body.Close()

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create output file (%s): %w", localPath, err)
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return fmt.Errorf("failed to write %s: %w", localPath, err)
	}
	// Write errors can surface only at Close time (e.g. ENOSPC), so always
	// check Close's return value too. Don't treat a truncated file as success.
	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", localPath, err)
	}

	if !se.modTime.IsZero() {
		// Set the access time to the modification time (atime is not tracked separately).
		_ = os.Chtimes(localPath, se.modTime, se.modTime)
	}
	fmt.Fprintf(os.Stderr, "Download: %s\n", localPath)
	return nil
}

// deleteExtraOnLocal removes local entries that exist only in DEST and not in SOURCE.
func deleteExtraOnLocal(srcTree, dstTree map[string]entry, localRoot string) error {
	keys := sortedKeys(dstTree)
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	for _, rel := range keys {
		if _, ok := srcTree[rel]; ok {
			continue
		}
		localPath := filepath.Join(localRoot, filepath.FromSlash(rel))
		if syncDryRun {
			fmt.Fprintf(os.Stderr, "[dry-run] delete %s\n", localPath)
			continue
		}
		if err := os.RemoveAll(localPath); err != nil {
			return fmt.Errorf("failed to remove %s: %w", localPath, err)
		}
		fmt.Fprintf(os.Stderr, "Removed: %s\n", localPath)
	}
	return nil
}

// ---- tree construction ----

// buildLocalTree recurses under localRoot and builds a map of relative path ->
// entry. The root itself is not included. Paths matched by matcher (and the
// .gdrignore file itself) are excluded; an ignored directory is pruned with its
// whole subtree.
func buildLocalTree(localRoot string, matcher *ignore.Matcher) (map[string]entry, error) {
	tree := make(map[string]entry)
	err := filepath.WalkDir(localRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == localRoot {
			return nil
		}
		rel, err := filepath.Rel(localRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if isIgnored(rel, d.IsDir(), matcher) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		tree[rel] = entry{
			size:    info.Size(),
			modTime: info.ModTime(),
			isDir:   d.IsDir(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk the local directory: %w", err)
	}
	return tree, nil
}

// filterIgnored removes ignored entries from an already-built tree (used for the
// Drive side, where the tree can't be pruned during a directory walk).
func filterIgnored(tree map[string]entry, matcher *ignore.Matcher) {
	for rel, e := range tree {
		if isIgnored(rel, e.isDir, matcher) {
			delete(tree, rel)
		}
	}
}

// isIgnored reports whether the relative path is excluded from sync, either by
// being the .gdrignore file itself (always excluded) or by matching a pattern.
func isIgnored(rel string, isDir bool, matcher *ignore.Matcher) bool {
	if rel == gdrignoreFile {
		return true
	}
	return matcher.Match(rel, isDir)
}

// buildDriveTree recurses under a Drive folder and builds a map of relative path -> entry.
func buildDriveTree(ctx context.Context, client *drive.Client, rootID string) (map[string]entry, error) {
	tree := make(map[string]entry)
	var walk func(folderID, relBase string) error
	walk = func(folderID, relBase string) error {
		children, err := client.ListChildren(ctx, folderID)
		if err != nil {
			return err
		}
		for _, ch := range children {
			rel := ch.Name
			if relBase != "" {
				rel = relBase + "/" + ch.Name
			}
			// Drive allows duplicate names within a folder, but the tree is
			// keyed by relative path. Since only one of the duplicates can be
			// handled, warn instead of silently dropping them.
			if _, dup := tree[rel]; dup {
				fmt.Fprintf(os.Stderr, "sync: warning: multiple entries with the same name; syncing only one: drive:%s\n", rel)
				continue
			}
			modTime, _ := time.Parse(time.RFC3339, ch.ModifiedTime)
			tree[rel] = entry{
				size:        ch.Size,
				modTime:     modTime,
				isDir:       ch.IsFolder(),
				driveID:     ch.ID,
				isGoogleDoc: ch.IsGoogleDoc(),
				md5:         ch.MD5,
			}
			if ch.IsFolder() {
				if err := walk(ch.ID, rel); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(rootID, ""); err != nil {
		return nil, err
	}
	return tree, nil
}

// sortedKeys orders relative paths shallowest-first (fewer separators first,
// lexicographic when equal), so folders are processed parent-first.
func sortedKeys(tree map[string]entry) []string {
	keys := make([]string, 0, len(tree))
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		di, dj := pathDepth(keys[i]), pathDepth(keys[j])
		if di != dj {
			return di < dj
		}
		return keys[i] < keys[j]
	})
	return keys
}

// isUnderAny returns whether the relative path rel equals any of prefixes or is
// under one. Used to exclude the entire subtree of a folder skipped on a type mismatch.
func isUnderAny(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// contentDiffers returns whether the local file's MD5 differs from driveMD5
// (used for --checksum content comparison). If driveMD5 is empty, the Drive
// side has no checksum (folders or some special files), so comparison is not
// possible: it returns "no difference (false)" and defers to the size/mtime check.
func contentDiffers(localPath, driveMD5 string) (bool, error) {
	if driveMD5 == "" {
		return false, nil
	}
	f, err := os.Open(localPath)
	if err != nil {
		return false, fmt.Errorf("%s: %w", localPath, err)
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("failed to compute MD5 of %s: %w", localPath, err)
	}
	return hex.EncodeToString(h.Sum(nil)) != driveMD5, nil
}

// pathDepth returns the depth of a relative path (number of "/" + 1).
func pathDepth(rel string) int {
	depth := 1
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			depth++
		}
	}
	return depth
}
