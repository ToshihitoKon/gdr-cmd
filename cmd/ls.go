package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/drive"
	"github.com/ToshihitoKon/gdr-cmd/internal/loc"
	"github.com/spf13/cobra"
)

var (
	lsLong     bool
	lsHuman    bool
	lsListDirs bool // -d: list folders themselves instead of their contents
)

var lsCmd = &cobra.Command{
	Use:   "ls [PATH...]",
	Short: "List files on Drive",
	Long: `List files/folders matching a My Drive-relative path.

With no arguments, lists the contents of the root. If the path points to a
folder, its contents are shown; if it points to a file, the file itself is
shown (like Unix ls). Paths support wildcards (*, ?, [...]).

Examples:
  gdr ls
  gdr ls /Documents
  gdr ls -l /Documents/*.pdf
  gdr ls -d /Documents      # show the folder itself`,
	RunE:              runLs,
	ValidArgsFunction: completeDrivePath,
}

func init() {
	lsCmd.Flags().BoolVarP(&lsLong, "long", "l", false, "use the long format (kind, size, modified time)")
	// -h is reserved by cobra as the shorthand for --help, so this flag is long-only.
	lsCmd.Flags().BoolVar(&lsHuman, "human-readable", false, "show sizes in human-readable units")
	lsCmd.Flags().BoolVarP(&lsListDirs, "directory", "d", false, "list folders themselves instead of their contents")
	rootCmd.AddCommand(lsCmd)
}

func runLs(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := drive.New(ctx)
	if err != nil {
		return err
	}

	// No arguments means target the root.
	if len(args) == 0 {
		args = []string{"/"}
	}

	// ls treats arguments as Drive paths. It also accepts the drive: prefix, so
	// normalize everything to a "/"-rooted My Drive-relative path.
	for i, a := range args {
		args[i] = loc.ParseDriveDefault(a).Path
	}

	// Whether to print headings when multiple paths are given.
	multiHeading := false
	if !lsListDirs {
		multiHeading = len(args) > 1 || hasFolderTarget(ctx, client, args)
	} else {
		multiHeading = len(args) > 1
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	defer w.Flush()

	var firstErr error
	for i, p := range args {
		nodes, err := client.Resolve(ctx, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ls: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(nodes) == 0 {
			fmt.Fprintf(os.Stderr, "ls: no match: %s\n", p)
			continue
		}

		if err := listNodes(ctx, w, client, nodes, multiHeading, i > 0); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// hasFolderTarget reports whether any argument resolves to a folder (used to
// decide whether headings are needed). This is a lookahead, so errors are ignored.
func hasFolderTarget(ctx context.Context, client *drive.Client, args []string) bool {
	for _, p := range args {
		nodes, err := client.Resolve(ctx, p)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if n.File.IsFolder() {
				return true
			}
		}
	}
	return false
}

// listNodes prints the resolved nodes. Folders are expanded to their contents
// (except when -d is given), and files are printed as-is.
func listNodes(ctx context.Context, w *tabwriter.Writer, client *drive.Client, nodes []drive.Node, withHeading, leadingBlank bool) error {
	// Separate folders from non-folders, and print non-folders first like Unix ls.
	var files []drive.File
	var folders []drive.Node
	for _, n := range nodes {
		if n.File.IsFolder() && !lsListDirs {
			folders = append(folders, n)
		} else {
			files = append(files, n.File)
		}
	}

	// Print directly named non-folders (and folders themselves under -d) first.
	if len(files) > 0 {
		sortFiles(files)
		for _, f := range files {
			printFileLine(w, f)
		}
		w.Flush()
	}

	// Expand folders to show their contents.
	for _, folder := range folders {
		children, err := client.ListChildren(ctx, folder.File.ID)
		if err != nil {
			return err
		}
		if leadingBlank || withHeading {
			if len(files) > 0 || leadingBlank {
				fmt.Fprintln(os.Stdout)
			}
		}
		if withHeading {
			fmt.Fprintf(os.Stdout, "%s:\n", folder.Path)
		}
		sortFiles(children)
		for _, c := range children {
			printFileLine(w, c)
		}
		w.Flush()
	}
	return nil
}

// sortFiles puts folders first, then sorts by name within each group.
func sortFiles(files []drive.File) {
	sort.SliceStable(files, func(i, j int) bool {
		fi, fj := files[i], files[j]
		if fi.IsFolder() != fj.IsFolder() {
			return fi.IsFolder() // folders first
		}
		return fi.Name < fj.Name
	})
}

// printFileLine prints one file according to the current flags.
func printFileLine(w *tabwriter.Writer, f drive.File) {
	name := f.Name
	if f.IsFolder() {
		name += "/"
	}
	if !lsLong {
		fmt.Fprintln(w, name)
		return
	}

	kind := fileKind(f)
	size := formatSize(f)
	modified := formatModTime(f.ModifiedTime)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", kind, size, modified, name)
}

// fileKind returns the kind label.
func fileKind(f drive.File) string {
	switch {
	case f.IsFolder():
		return "dir"
	case f.IsGoogleDoc():
		return "gdoc"
	default:
		return "file"
	}
}

// formatSize returns the display string for the size column. Folders and
// Google-native formats have no size, so they are shown as "-".
func formatSize(f drive.File) string {
	if f.IsFolder() || f.IsGoogleDoc() {
		return "-"
	}
	if lsHuman {
		return humanSize(f.Size)
	}
	return fmt.Sprintf("%d", f.Size)
}

// humanSize formats a byte count as a base-1024 string with a unit suffix.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// formatModTime formats the modified time in the style of Unix ls -l.
//
// Following ls conventions, it returns "month day time" (e.g. "Jun 18 14:30")
// for times within roughly the last six months, and "month day  year"
// (e.g. "Sep 15  2021") for older ones. The day is space-padded (_2) for
// alignment. If the input cannot be parsed, the original string is returned.
func formatModTime(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	t = t.Local()
	// Six months is about 182.5 days. Use the absolute value so future times
	// are not treated as old.
	const recent = 182*24*time.Hour + 12*time.Hour
	if d := time.Since(t); d >= -recent && d <= recent {
		return t.Format("Jan _2 15:04")
	}
	return t.Format("Jan _2  2006")
}
