// Command gdr は Google Drive を操作する CLI ツール。
package main

import (
	"github.com/ToshihitoKon/gdr-cmd/cmd"
)

// ビルド時に GoReleaser の ldflags で注入される。開発ビルドでは既定値のまま。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.SetVersionInfo(version, commit, date)
	cmd.Execute()
}
