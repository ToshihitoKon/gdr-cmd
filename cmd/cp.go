package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var cpRecursive bool

var cpCmd = &cobra.Command{
	Use:   "cp SOURCE... DEST",
	Short: "Copy files between Drive and local",
	Long: `Copy files between Drive and local (download/upload).

Drive-side paths are marked with the drive: prefix (e.g. drive:/Documents/a.pdf).
Paths without the prefix are treated as local. The direction is determined by the
kind of each end:

  drive: -> local ... download
  local -> drive: ... upload

If SOURCE matches multiple entries (or via glob), DEST must be a directory.
Use -r to handle folders. Both Drive wildcards (*, ?, [...]) and local globs
are supported.

Google-native format (Google Docs, etc.) cannot be downloaded and is skipped.

Examples:
  gdr cp drive:/Documents/report.pdf .          # download
  gdr cp drive:/Documents/*.pdf ./pdfs/          # download multiple
  gdr cp -r drive:/Documents/project ./backup/   # download a folder
  gdr cp ./report.pdf drive:/Documents/          # upload
  gdr cp -r ./project drive:/backup/             # upload a folder`,
	Args:              cobra.MinimumNArgs(2),
	RunE:              runCp,
	ValidArgsFunction: completeCpArgs,
}

func init() {
	cpCmd.Flags().BoolVarP(&cpRecursive, "recursive", "r", false, "copy folders recursively")
	rootCmd.AddCommand(cpCmd)
}

func runCp(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	rawSources := args[:len(args)-1]
	rawDest := args[len(args)-1]
	dest := loc.Parse(rawDest)

	// Require all SOURCE entries to be on the same side (all Drive or all local)
	// to avoid a mix where the direction can't be determined uniquely.
	sources := make([]loc.Location, len(rawSources))
	for i, s := range rawSources {
		sources[i] = loc.Parse(s)
		if sources[i].Kind != sources[0].Kind {
			return fmt.Errorf("sources cannot mix Drive and local")
		}
	}
	srcKind := sources[0].Kind

	switch {
	case srcKind == loc.Drive && dest.IsLocal():
		return downloadSources(ctx, sources, dest, rawDest)
	case srcKind == loc.Local && dest.IsDrive():
		return uploadSources(ctx, sources, dest, rawDest)
	case srcKind == loc.Drive && dest.IsDrive():
		return fmt.Errorf("copy within Drive is not supported; use `gdr mv` to move")
	default: // local -> local
		return fmt.Errorf("use the OS cp for local-to-local copies; add drive: when Drive is involved")
	}
}

// ---- Download (Drive -> local) ----

func downloadSources(ctx context.Context, sources []loc.Location, dest loc.Location, rawDest string) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	var matched []drive.Node
	for _, src := range sources {
		nodes, err := client.Resolve(ctx, src.Path)
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return fmt.Errorf("no match: %s", src)
		}
		matched = append(matched, nodes...)
	}

	destIsDir := isExistingDir(dest.Path)
	if len(matched) > 1 && !destIsDir {
		return fmt.Errorf("multiple sources; destination %q must be an existing directory", dest.Path)
	}

	used := make(map[string]struct{})
	var firstErr error
	for _, node := range matched {
		if err := copyNodeDown(ctx, client, node, dest.Path, destIsDir, used); err != nil {
			fmt.Fprintf(os.Stderr, "cp: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// copyNodeDown downloads a single Drive node to local.
func copyNodeDown(ctx context.Context, client *drive.Client, node drive.Node, dest string, destIsDir bool, used map[string]struct{}) error {
	if node.File.IsFolder() {
		if !cpRecursive {
			return fmt.Errorf("%s is a folder (use -r)", node.Path)
		}
		target := dest
		if destIsDir {
			// Make the output directory name unique so multiple same-named Drive
			// folders don't merge into one local directory and mix their contents.
			target = uniquePath(dest, node.File.Name, used)
		}
		return downloadFolder(ctx, client, node.File.ID, target)
	}

	if node.File.IsGoogleDoc() {
		fmt.Fprintf(os.Stderr, "cp: skip (Google-native format not supported): %s\n", node.Path)
		return nil
	}

	outPath := dest
	if destIsDir {
		outPath = uniquePath(dest, node.File.Name, used)
	}
	return downloadFile(ctx, client, node.File, outPath)
}

// downloadFolder recursively downloads the contents of a folder.
// Same-named children within a folder (which Drive allows) are kept by making
// output names unique via a used set scoped to this level, so none are lost to overwrite.
func downloadFolder(ctx context.Context, client *drive.Client, folderID, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("failed to create directory (%s): %w", target, err)
	}
	children, err := client.ListChildren(ctx, folderID)
	if err != nil {
		return err
	}
	used := make(map[string]struct{})
	for _, child := range children {
		switch {
		case child.IsFolder():
			if err := downloadFolder(ctx, client, child.ID, uniquePath(target, child.Name, used)); err != nil {
				return err
			}
		case child.IsGoogleDoc():
			fmt.Fprintf(os.Stderr, "cp: skip (Google-native format not supported): %s/%s\n", target, child.Name)
		default:
			if err := downloadFile(ctx, client, child, uniquePath(target, child.Name, used)); err != nil {
				return err
			}
		}
	}
	return nil
}

// downloadFile downloads a single file and writes it to outPath.
func downloadFile(ctx context.Context, client *drive.Client, f drive.File, outPath string) error {
	body, err := client.Download(ctx, f.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	defer body.Close()

	if dir := filepath.Dir(outPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create output directory (%s): %w", dir, err)
		}
	}
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create output file (%s): %w", outPath, err)
	}

	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return fmt.Errorf("failed to write %s: %w", outPath, err)
	}
	// Write errors may only surface at Close time (e.g. ENOSPC), so check the
	// Close return value too and don't treat a truncated write as success.
	if err := out.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", outPath, err)
	}
	fmt.Fprintf(os.Stderr, "Download: %s -> %s\n", f.Name, outPath)
	return nil
}

// ---- Upload (local -> Drive) ----

func uploadSources(ctx context.Context, sources []loc.Location, dest loc.Location, rawDest string) error {
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	// Expand local globs to gather sources.
	var localPaths []string
	for _, src := range sources {
		matches, err := filepath.Glob(src.Path)
		if err != nil {
			return fmt.Errorf("invalid pattern %q: %w", src.Path, err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no match: %s", src.Path)
		}
		localPaths = append(localPaths, matches...)
	}

	// Decide whether to treat the destination as a filename or as a folder.
	// Only a single file sent to a drive: path with no trailing slash and no
	// existing folder counts as an "upload with rename"; otherwise dest is a folder.
	rename, err := isSingleFileRename(ctx, client, localPaths, rawDest, dest.Path)
	if err != nil {
		return err
	}
	if rename {
		parentPath, name := drive.SplitParent(dest.Path)
		parentID, err := client.EnsureFolderPath(ctx, parentPath)
		if err != nil {
			return err
		}
		info, err := os.Stat(localPaths[0])
		if err != nil {
			return fmt.Errorf("%s: %w", localPaths[0], err)
		}
		return uploadFileAs(ctx, client, localPaths[0], info, parentID, name, parentPath)
	}

	// Ensure the destination Drive folder exists (like mkdir -p).
	destFolderID, err := client.EnsureFolderPath(ctx, dest.Path)
	if err != nil {
		return err
	}

	var firstErr error
	for _, lp := range localPaths {
		if err := uploadPath(ctx, client, lp, destFolderID, dest.Path); err != nil {
			fmt.Fprintf(os.Stderr, "cp: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// isSingleFileRename reports whether the destination should be treated as a
// filename (upload with rename). True only when a single local file is sent to a
// path with no trailing slash that is not an existing Drive folder. Multiple
// sources, directories, trailing slash, and existing-folder destinations are all
// treated as a folder (false). An API failure during the folder check is returned as err.
func isSingleFileRename(ctx context.Context, client *drive.Client, localPaths []string, rawDest, destPath string) (bool, error) {
	if len(localPaths) != 1 || loc.HasTrailingSlash(rawDest) {
		return false, nil
	}
	if info, err := os.Stat(localPaths[0]); err != nil || info.IsDir() {
		return false, nil
	}
	// If the destination is already a Drive folder, put the file inside it (not a filename).
	_, isFolder, err := resolveExistingFolder(ctx, client, destPath)
	if err != nil {
		return false, err
	}
	return !isFolder, nil
}

// uploadPath uploads a local file/directory directly under the Drive parentID.
// destDrivePath is the Drive absolute path corresponding to parentID, used for log output.
func uploadPath(ctx context.Context, client *drive.Client, localPath, parentID, destDrivePath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}

	if info.IsDir() {
		if !cpRecursive {
			return fmt.Errorf("%s is a directory (use -r)", localPath)
		}
		// Ensure a same-named folder under parentID (reuse if it exists) and upload recursively.
		subDrivePath := path.Join(destDrivePath, info.Name())
		subID, err := client.EnsureChildFolder(ctx, parentID, info.Name(), subDrivePath)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(localPath)
		if err != nil {
			return fmt.Errorf("failed to read directory (%s): %w", localPath, err)
		}
		for _, e := range entries {
			if err := uploadPath(ctx, client, filepath.Join(localPath, e.Name()), subID, subDrivePath); err != nil {
				return err
			}
		}
		return nil
	}

	return uploadFile(ctx, client, localPath, info, parentID, destDrivePath)
}

// uploadFile uploads a single local file directly under the Drive parentID,
// keeping the local filename.
func uploadFile(ctx context.Context, client *drive.Client, localPath string, info os.FileInfo, parentID, destDrivePath string) error {
	return uploadFileAs(ctx, client, localPath, info, parentID, filepath.Base(localPath), destDrivePath)
}

// uploadFileAs uploads a single local file to Drive under the name driveName
// (used for upload with rename).
// destDrivePath is the Drive absolute path of the destination folder, used for log output.
func uploadFileAs(ctx context.Context, client *drive.Client, localPath string, info os.FileInfo, parentID, driveName, destDrivePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("%s: %w", localPath, err)
	}
	defer f.Close()

	if _, err := client.Upload(ctx, parentID, driveName, f, info.ModTime()); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Upload: %s -> drive:%s\n", localPath, path.Join(destDrivePath, driveName))
	return nil
}

// ---- Completion / helpers ----

// completeCpArgs completes cp arguments. Input starting with drive: gets dynamic
// Drive path completion; otherwise it defers to the shell's default file completion.
func completeCpArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeLocationArg(cmd, toComplete)
}

// isExistingDir reports whether the path is an existing directory.
func isExistingDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// uniquePath returns a path under dir where name does not collide.
// If it is already used (within this command run) or exists on disk, it appends
// a sequence number like "name (1).ext".
func uniquePath(dir, name string, used map[string]struct{}) string {
	candidate := filepath.Join(dir, name)
	if !isUsed(candidate, used) {
		used[candidate] = struct{}{}
		return candidate
	}
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	for i := 1; ; i++ {
		alt := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if !isUsed(alt, used) {
			used[alt] = struct{}{}
			return alt
		}
	}
}

// isUsed reports whether the candidate path is already used or exists on disk.
func isUsed(path string, used map[string]struct{}) bool {
	if _, ok := used[path]; ok {
		return true
	}
	_, err := os.Stat(path)
	return err == nil
}
