// Package loc は CLI 引数のパス指定を Drive 側かローカル側かに分類する。
//
// 記法:
//   - "drive:/foo/bar" または "drive:foo/bar" は Drive 上のパス
//   - それ以外 (例 "./foo", "/tmp/bar", "foo") はローカルパス
//
// sync や cp のように転送方向が引数で決まるコマンドで、両端がどちら側かを
// 明示的に判別するために使う。drive: プレフィックスを付けないパスは常に
// ローカル扱いとし、ローカルを明示したいときは "./foo" のように書ける。
package loc

import "strings"

// drivePrefix は Drive 側を示すプレフィックス。
const drivePrefix = "drive:"

// Kind はパスが指す場所の種別。
type Kind int

const (
	// Local はローカルファイルシステム上のパス。
	Local Kind = iota
	// Drive は Google Drive 上のパス。
	Drive
)

// Location は分類済みのパス指定。
type Location struct {
	Kind Kind
	// Path は種別ごとの「素の」パス。
	//   - Drive の場合: マイドライブ起点の絶対パス ("/" 始まりに正規化済み)
	//   - Local の場合: 入力のローカルパスそのまま
	Path string
}

// IsDrive は Drive 上のパスかどうかを返す。
func (l Location) IsDrive() bool { return l.Kind == Drive }

// IsLocal はローカルパスかどうかを返す。
func (l Location) IsLocal() bool { return l.Kind == Local }

// String は記法を復元した表示用文字列を返す (エラーメッセージ用)。
func (l Location) String() string {
	if l.Kind == Drive {
		return drivePrefix + l.Path
	}
	return l.Path
}

// Parse は引数を Location に分類する。
//
// "drive:" プレフィックスがあれば Drive とし、続くパスを "/" 始まりへ正規化する
// ("drive:foo" も "drive:/foo" も同じ /foo を指す)。プレフィックスが無ければ
// ローカルとして入力をそのまま保持する。
func Parse(arg string) Location {
	if rest, ok := strings.CutPrefix(arg, drivePrefix); ok {
		return Location{Kind: Drive, Path: normalizeDrivePath(rest)}
	}
	return Location{Kind: Local, Path: arg}
}

// ParseDriveDefault はプレフィックスの無いパスを Drive 扱いにする。
//
// ls/cp など「引数は基本 Drive パス」という従来挙動を保つコマンド向け。
// "drive:" 付きも受け付けるため、新記法と旧記法の両方を許容できる。
// ローカルを明示したい場合は呼び出し側で Parse を使い分けること。
func ParseDriveDefault(arg string) Location {
	if rest, ok := strings.CutPrefix(arg, drivePrefix); ok {
		return Location{Kind: Drive, Path: normalizeDrivePath(rest)}
	}
	return Location{Kind: Drive, Path: normalizeDrivePath(arg)}
}

// HasTrailingSlash は元の引数が "/" で終わるか (ディレクトリ志向か) を返す。
// "drive:" プレフィックスやローカルかを問わず、末尾スラッシュの有無だけを見る。
// cp/sync で「コピー先をディレクトリとして扱うか」の判断に使う。
func HasTrailingSlash(arg string) bool {
	s := strings.TrimPrefix(arg, drivePrefix)
	return strings.HasSuffix(s, "/")
}

// normalizeDrivePath は Drive パスを "/" 始まりに正規化する。
// 空や "." はルート "/" とみなす。末尾スラッシュは (ルートを除き) 取り除く。
func normalizeDrivePath(p string) string {
	if p == "" || p == "." || p == "/" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// ルート以外の末尾スラッシュは解決時に不要なので落とす。
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}
