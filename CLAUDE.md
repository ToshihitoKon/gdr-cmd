# gdr-cmd

Google Drive を操作する CLI ツール。マイドライブ起点のパスで `ls` (一覧)、
`cp` (DL/UL 双方向)、`sync` (一方向同期)、`mkdir` / `rm` / `mv` (名前空間操作) が
できる。ワイルドカードと Tab 補完に対応し、認証はサービスアカウント鍵を使わず
ユーザー自身の OAuth クライアントで行う。

## アーキテクチャ

依存方向は cmd → internal で、internal 内は drive → auth → config、および
cmd/drive → loc の順。

| パッケージ | 責務 |
|-----------|------|
| `cmd/gdr/main.go` | エントリポイント。バイナリ名を `gdr` に揃えるため main はここに置く (go install はディレクトリ名をバイナリ名にする) |
| `cmd/` | cobra のコマンド定義 (root/auth/ls/cp/sync/mkdir/rm/mv) と動的補完 |
| `internal/config/` | XDG 準拠の設定パス解決、OAuth クライアント情報の読込 |
| `internal/auth/` | OAuth フロー、トークン永続化・自動更新、認証済み HTTP クライアント生成 |
| `internal/drive/` | Drive API ラッパー。読み取り (client.go: list/download/解決/glob) と書き込み (write.go: upload/mkdir/trash/delete/move/rename) |
| `internal/loc/` | 引数の `drive:` 記法を Drive/ローカルに分類する |

### パス記法 (internal/loc)

`drive:` プレフィックスで Drive 側を、無印でローカル側を表す。`drive:foo` も
`drive:/foo` も同じ `/foo` に正規化する。`sync`/`cp`/`mkdir`/`rm`/`mv` は `loc.Parse`
で両端を分類し、転送方向や操作対象を決める。`ls` は従来互換のため `ParseDriveDefault`
で無印もDrive扱いにする (drive: 付きも受理)。

### パス解決と glob (internal/drive/path.go)

マイドライブ起点のパス `/a/b/c` を階層ごとに Drive API で辿って fileID へ解決する。
各階層要素ごとに `path.Match` でワイルドカード (`*`, `?`, `[...]`) を評価するため、
`*` がパス区切りをまたぐ問題は起きない (Drive はファイル名に `/` を含められない)。
**Drive は同名ファイルを許す**ため、解決結果は常に複数件を許す設計にしている。

### 認証フロー (internal/auth/auth.go)

Google は従来の OOB フロー (`urn:ietf:wg:oauth:2.0:oob`) を 2022 年に廃止した。
このため loopback リダイレクト (`http://127.0.0.1:<port>`) を使う。既定はローカルに
一時 HTTP サーバを立てて認可コードを自動受信し、`--no-browser` 時は loopback URL を
手動コピペで受け取る。

## ビルド・テスト

```sh
go build ./...   # ビルド
go test ./...    # テスト
go vet ./...     # 静的解析
gofmt -l .       # 整形チェック
```

API 非依存の純粋ロジック (パス分解・glob 判定・認可コード抽出・サイズ整形・
更新時刻整形) には単体テストがある。Drive API を叩く部分は実認証が要るため
手動確認となる。

## 設計上の決定 (変更時に意識すること)

- **対象はマイドライブのみ**。共有ドライブ (Shared Drives) は対象外。対応する場合は
  Drive API 呼び出しに `supportsAllDrives` 等のパラメータ追加が必要になる
- **OAuth スコープは読み書き可能な `drive`**。アップロード・削除・移動に必要。
- **`cp` は Drive 側に `drive:` 必須**。転送方向 (DL/UL) を一意にするため、無印同士や
  Drive→Drive・ローカル→ローカルはエラーにする。これは初期実装からの破壊的変更。
- **`sync` の差分判定は size + mtime**。mtime は Drive (ミリ秒) とローカル (ナノ秒) で
  精度が違うため秒単位に丸めて比較する (`needsTransfer`)。内容ハッシュ比較はしない。
- **`rm` と `sync --delete` は既定でゴミ箱送り** (`trashed=true`)。`--permanent` で完全削除。
- **`-h` は cobra が `--help` のショートハンドに使う**。サブコマンドで `-h` を別フラグに
  割り当てると起動時に panic するため避ける (ls の `--human-readable` はロングのみ)
- **Drive クエリの文字列リテラルはエスケープ必須** (`escapeQueryValue`)。ファイル名に
  含まれる `'` や `\` をエスケープしないとクエリが壊れる
- **Tab 補完は API を叩く**ため 3 秒のタイムアウトを設け、失敗時は候補なしで返して
  シェルを固まらせない
