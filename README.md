# gdr-cmd

Google Drive をコマンドラインから操作する CLI ツールです。マイドライブ起点の
パスでファイルを指定し、`ls` での一覧表示と `cp` でのダウンロードができます。
パスにはワイルドカード (`*`, `?`, `[...]`) を使え、Tab 補完にも対応します。

認証はサービスアカウント鍵を手元に持たず、OAuth でユーザー自身の Google
アカウントにログインする方式です。

## 特徴

- `ls`: Drive 上のファイル/フォルダを一覧表示 (詳細表示 `-l` 対応)
- `cp`: Drive 上のファイルをローカルへダウンロード (フォルダ再帰 `-r` 対応)
- ワイルドカード: パスの各階層で `*`, `?`, `[...]` が使える
- Tab 補完: サブコマンド・フラグに加え、**Drive 上のパスを動的に補完**
- 認証: ユーザー自身の OAuth クライアントを使い、サービスアカウント鍵を持たない

## インストール

```sh
go install github.com/ToshihitoKon/gdr-cmd@latest
```

ビルド済みバイナリは `gdr` という名前で生成されます。または、リポジトリを
クローンして `go build -o gdr .` でビルドできます。

## 事前準備: OAuth クライアントの作成

サービスアカウント鍵を使わない代わりに、自分の Google Cloud プロジェクトで
OAuth クライアントを 1 度だけ作成します。

1. [Google Cloud Console](https://console.cloud.google.com/) でプロジェクトを作成
   (既存のものでも可)。
2. **API とサービス → ライブラリ** で「Google Drive API」を有効化する。
3. **API とサービス → OAuth 同意画面** を設定する。
   - User Type は社内利用なら「内部」、個人の Google アカウントなら「外部」を選ぶ。
   - 「外部」かつ公開していない場合はテストユーザーに自分のアカウントを追加する。
   - スコープに `.../auth/drive` (Google Drive の読み書き) を追加する。
4. **API とサービス → 認証情報 → 認証情報を作成 → OAuth クライアント ID** で、
   アプリケーションの種類に **デスクトップアプリ** を選んで作成する。
5. 作成後、JSON をダウンロードする (`client_secret_xxx.json` のような名前)。

### 認証情報の設定

ダウンロードした JSON を設定ディレクトリに配置するか、環境変数で渡します。

**方法 A: JSON を配置する (推奨)**

```sh
mkdir -p ~/.config/gdr-cmd
cp ~/Downloads/client_secret_xxx.json ~/.config/gdr-cmd/credentials.json
```

**方法 B: 環境変数で渡す**

```sh
export GDR_CLIENT_ID="xxxxx.apps.googleusercontent.com"
export GDR_CLIENT_SECRET="xxxxx"
```

環境変数が設定されている場合はそちらが優先されます。

> 設定ディレクトリは `$XDG_CONFIG_HOME/gdr-cmd/` (未設定なら `~/.config/gdr-cmd/`)
> です。認証情報とトークンを含むため、ディレクトリは `0700`、トークンファイルは
> `0600` で保存されます。

## ログイン

```sh
gdr auth login
```

ブラウザが自動で開き、Google アカウントでの承認を求められます。承認すると
ローカルの一時ポートで認可コードを受け取り、トークンを
`~/.config/gdr-cmd/token.json` に保存します。以降は refresh token により自動で
更新されるため、再ログインは不要です。

SSH 越しなどブラウザをローカルで開けない環境では、手動コピペ方式を使います。

```sh
gdr auth login --no-browser
```

表示された URL を任意の端末のブラウザで開いて承認し、リダイレクト先
(`http://127.0.0.1:9999/...`) の URL 全体をターミナルに貼り付けます。
ブラウザは「127.0.0.1 に接続できない」と表示しますが、これは正常です
(アドレスバーの URL に認可コードが含まれているため、それを使います)。

その他の認証コマンド:

```sh
gdr auth status   # ログイン状態を確認
gdr auth logout   # 保存済みトークンを削除
```

> **認証フローについての補足**: Google は従来の OOB フロー
> (`redirect_uri=urn:ietf:wg:oauth:2.0:oob`) を 2022 年に廃止しました。本ツールは
> その後継である loopback リダイレクト (`http://127.0.0.1:<port>`) を使います。
> `--no-browser` の手動コピペ方式も、この loopback URL からコードを読み取る形で
> 同等の体験を実現しています。

## 使い方

### ls — 一覧表示

```sh
gdr ls                       # マイドライブのルート直下
gdr ls /Documents            # フォルダの中身
gdr ls -l /Documents         # 詳細表示 (種別・サイズ・更新日時)
gdr ls -l --human-readable / # サイズを単位付きで表示
gdr ls -d /Documents         # フォルダ自身を表示 (中身を展開しない)
gdr ls '/Documents/*.pdf'    # ワイルドカード
```

詳細表示の列は `種別 / サイズ / 更新日時 / 名前` です。種別は `dir` (フォルダ)、
`file` (通常ファイル)、`gdoc` (Google ネイティブ形式) を示します。

### cp — ダウンロード

```sh
gdr cp /Documents/report.pdf .          # カレントディレクトリへ
gdr cp /Documents/report.pdf ./out.pdf  # 名前を指定して保存
gdr cp '/Documents/*.pdf' ./pdfs/       # 複数ファイルをディレクトリへ
gdr cp -r /Documents/project ./backup/  # フォルダを再帰ダウンロード
```

- コピー元が複数 (または glob で複数) にマッチする場合、コピー先は既存の
  ディレクトリである必要があります。
- 同じディレクトリ内で名前が衝突する場合は `name (1).ext` のように連番が付きます。
- Google ネイティブ形式 (Google ドキュメント/スプレッドシート等) は通常の
  ダウンロードができないため、現時点ではスキップして警告します。

> パスにワイルドカードを含む場合、シェルがローカルのファイル名に展開しないよう
> シングルクォートで囲んでください。

## Tab 補完の設定

cobra が各シェル向けの補完スクリプトを生成します。Drive 上のパスは入力中に
Drive API を呼び出して動的に補完されます (フォルダ候補には末尾 `/` が付きます)。

> 動的補完は補完のたびに Drive API へ問い合わせます。応答が遅い場合に
> シェルが固まらないよう、補完時の API 呼び出しには 3 秒のタイムアウトを
> 設けています。

### bash

```sh
# 一時的に有効化
source <(gdr completion bash)

# 永続化 (Linux)
gdr completion bash | sudo tee /etc/bash_completion.d/gdr > /dev/null
```

### zsh

```sh
# 補完を初めて使う場合は compinit を有効化
echo "autoload -U compinit; compinit" >> ~/.zshrc

# 補完スクリプトを fpath の通ったディレクトリに置く
gdr completion zsh > "${fpath[1]}/_gdr"
```

新しいシェルを開くと有効になります。

### fish

```sh
gdr completion fish > ~/.config/fish/completions/gdr.fish
```

## 設計上の制約・既知の制限

- 対象は**マイドライブのみ**で、共有ドライブ (Shared Drives) は対象外です。
- `cp` は**ダウンロード方向のみ** (Drive → ローカル) に対応しています。
  OAuth スコープは将来のアップロード対応を見据えて読み書き可能な `drive` を
  取得済みのため、アップロード機能は後方互換のまま追加できます。
- Google ネイティブ形式のエクスポート (Docs → PDF など) は未対応です。
- Drive は同名ファイルを許容するため、同名が複数ある場合は `ls` は全件表示し、
  `cp` は全件をダウンロードして名前衝突時に連番を付けます。

## 開発

```sh
go build ./...   # ビルド
go test ./...    # テスト
go vet ./...     # 静的解析
```
