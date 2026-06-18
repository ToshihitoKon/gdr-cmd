// Command gdr is a CLI tool for working with Google Drive.
//
// main lives in this cmd/gdr directory so the binary is named gdr under both
// go install and GoReleaser (go install names the binary after the main
// package's directory, not the trailing module path element).
package main

import (
	"github.com/ToshihitoKon/gdr-cmd/cmd"
)

// Injected at build time via GoReleaser ldflags. Development builds keep the defaults.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	cmd.Execute()
}
