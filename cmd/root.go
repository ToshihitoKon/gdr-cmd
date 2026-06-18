// Package cmd provides the command definitions for the gdr CLI.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd is the root command of gdr.
var rootCmd = &cobra.Command{
	Use:   "gdr",
	Short: "CLI tool for working with Google Drive",
	Long: `gdr is a tool for working with Google Drive from the command line.

Specify files by My Drive-relative paths (e.g. /dir/file.txt) to list them with
ls and download them with cp. Paths support wildcards (*, ?, [...]) and Tab
completion.

Authentication does not use a service account key; instead it logs in to your
Google account via OAuth. Run 'gdr auth login' the first time.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// SetVersionInfo sets the build-time version info on the root command so it can
// be shown via `gdr --version` / `gdr version`.
func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

// Execute runs the root command. Called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
