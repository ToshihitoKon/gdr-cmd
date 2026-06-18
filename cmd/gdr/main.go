// Command gdr は Google Drive を操作する CLI ツール。
//
// バイナリ名を go install / GoReleaser の両方で gdr に揃えるため、main は
// この cmd/gdr ディレクトリに置く (go install はモジュールパス末尾ではなく
// main パッケージのディレクトリ名をバイナリ名にするため)。
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
