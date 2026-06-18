<div align="center">

# 🗂️ gdr-cmd

### ✨ A delightful CLI for your Google Drive — `ls` and `cp`, with wildcards and tab completion 🚀

<p>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go&logoColor=white">
  <img alt="Google Drive API" src="https://img.shields.io/badge/Google%20Drive-API%20v3-4285F4?logo=googledrive&logoColor=white">
  <img alt="OAuth 2.0" src="https://img.shields.io/badge/Auth-OAuth%202.0-EB5424?logo=auth0&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/License-MIT-yellow.svg">
</p>

<p>
  <em>Browse and download your Drive from the comfort of your terminal —<br>
  no service-account keys, just your own Google account. 🔐</em>
</p>

</div>

---

## 📑 Table of Contents

- [🌟 Features](#-features)
- [📦 Installation](#-installation)
- [🔑 Setup: Create an OAuth Client](#-setup-create-an-oauth-client)
- [🚪 Logging In](#-logging-in)
- [📂 Usage](#-usage)
- [⌨️ Tab Completion](#️-tab-completion)
- [⚠️ Design Notes & Known Limitations](#️-design-notes--known-limitations)
- [🛠️ Development](#️-development)

---

## 🌟 Features

| | |
|---|---|
| 📋 **`ls`** | List files and folders in My Drive (detailed view with `-l`) |
| 🔁 **`cp`** | Copy **both ways** — download (`drive:` → local) and upload (local → `drive:`), recursive with `-r` |
| 🔄 **`sync`** | One-way directory sync in either direction, with `--delete` and `--dry-run` |
| 📁 **`mkdir`** | Create folders on Drive (`-p` for parents) |
| 🗑️ **`rm`** | Delete files/folders (trash by default, `--permanent` to skip the trash) |
| ✂️ **`mv`** | Move/rename within Drive (metadata-only, no re-upload) |
| ✳️ **Wildcards** | Use `*`, `?`, `[...]` at every level of a path |
| ⌨️ **Tab Completion** | Subcommands, flags, **and dynamic completion of Drive paths** |
| 🔐 **No service-account keys** | Authenticate with **your own** OAuth client and Google account |

---

## 📦 Installation

**⬇️ Download a prebuilt binary** for macOS / Linux / Windows from the
[Releases](https://github.com/ToshihitoKon/gdr-cmd/releases) page, extract it, and put
`gdr` somewhere on your `PATH`.

**🐹 Or install with Go:**

```sh
go install github.com/ToshihitoKon/gdr-cmd/cmd/gdr@latest
```

The binary is named **`gdr`**. Alternatively, clone the repo and build it yourself:

```sh
git clone https://github.com/ToshihitoKon/gdr-cmd.git
cd gdr-cmd
go build -o gdr ./cmd/gdr
```

---

## 🔑 Setup: Create an OAuth Client

Instead of using a service-account key, you create an OAuth client **once** in your own
Google Cloud project. 🛠️

1. **Create a project** in the [Google Cloud Console](https://console.cloud.google.com/)
   (an existing one works too).
2. Go to **APIs & Services → Library** and enable the **Google Drive API**.
3. Configure the **OAuth consent screen** under **APIs & Services**.
   - For internal company use, pick **Internal**; for a personal Google account, pick **External**.
   - If **External** and unpublished, add your own account as a **test user**.
   - Add the scope `.../auth/drive` (read & write to Google Drive).
4. Go to **APIs & Services → Credentials → Create Credentials → OAuth client ID**,
   choose **Desktop app**, and create it.
5. **Download the JSON** (named something like `client_secret_xxx.json`).

### 📥 Provide the credentials

Drop the JSON into the config directory, or pass it via environment variables.

<details open>
<summary><strong>Option A — Place the JSON file (recommended) ✅</strong></summary>

```sh
mkdir -p ~/.config/gdr-cmd
cp ~/Downloads/client_secret_xxx.json ~/.config/gdr-cmd/credentials.json
```

</details>

<details>
<summary><strong>Option B — Use environment variables 🌱</strong></summary>

```sh
export GDR_CLIENT_ID="xxxxx.apps.googleusercontent.com"
export GDR_CLIENT_SECRET="xxxxx"
```

When set, environment variables take precedence over the JSON file.

</details>

> 💡 The config directory is `$XDG_CONFIG_HOME/gdr-cmd/` (defaults to `~/.config/gdr-cmd/`).
> Because it holds credentials and tokens, the directory is created with `0700` and the
> token file with `0600`.

---

## 🚪 Logging In

```sh
gdr auth login
```

Your browser opens and asks you to authorize with your Google account. Once approved,
`gdr` receives the authorization code on a temporary local port and saves the token to
`~/.config/gdr-cmd/token.json`. From then on the **refresh token** keeps it current — no
need to log in again. 🎉

🌐 **No local browser?** (e.g. over SSH) Use the manual copy-paste flow:

```sh
gdr auth login --no-browser
```

Open the printed URL in a browser on any device, approve, and paste the **entire**
redirect URL (`http://127.0.0.1:9999/...`) back into the terminal. The browser will show a
"can't connect to 127.0.0.1" error — **that's expected** (the authorization code lives in
the address bar, and that's what we read).

Other auth commands:

```sh
gdr auth status   # 🔍 Check login state
gdr auth logout   # 🚪 Remove the saved token
```

> 🧭 **A note on the auth flow:** Google **discontinued** the legacy OOB flow
> (`redirect_uri=urn:ietf:wg:oauth:2.0:oob`) in 2022. `gdr` uses its successor — the
> **loopback redirect** (`http://127.0.0.1:<port>`). The `--no-browser` mode delivers the
> same experience by reading the code out of that loopback URL.

---

## 📂 Usage

### 🧭 Path notation: `drive:`

Paths on Google Drive are written with a **`drive:`** prefix (e.g. `drive:/Documents/a.pdf`);
paths without it are local. This lets transfer commands tell the two sides apart.

- `ls` treats its argument as a Drive path by default, so `gdr ls /Documents` still works
  (and `gdr ls drive:/Documents` is equivalent).
- `cp`, `sync`, `mkdir`, `rm`, and `mv` use the prefix to decide what is Drive and what is local.

> 🔀 **Breaking change in this version:** `cp` now requires `drive:` on the Drive side to make
> the transfer direction unambiguous. Use `gdr cp drive:/Documents/x.pdf .` instead of the
> old `gdr cp /Documents/x.pdf .`.

### 📋 `ls` — List

```sh
gdr ls                       # 🏠 Root of My Drive
gdr ls /Documents            # 📁 Contents of a folder
gdr ls -l /Documents         # 📊 Detailed view (kind / size / modified / name)
gdr ls -l --human-readable / # 📏 Human-readable sizes
gdr ls -d /Documents         # 🗃️  Show the folder itself (don't expand it)
gdr ls '/Documents/*.pdf'    # ✳️ Wildcards
```

The detailed view columns are `kind / size / modified / name`. **kind** is one of
`dir` (folder), `file` (regular file), or `gdoc` (Google-native format).

### 🔁 `cp` — Download & Upload

Direction is decided by which side carries the `drive:` prefix.

```sh
# Download (drive: → local)
gdr cp drive:/Documents/report.pdf .          # 📥 Into the current directory
gdr cp drive:/Documents/report.pdf ./out.pdf  # 🏷️  Save under a chosen name
gdr cp 'drive:/Documents/*.pdf' ./pdfs/        # 🗂️  Multiple files into a directory
gdr cp -r drive:/Documents/project ./backup/   # 📁 Download a folder recursively

# Upload (local → drive:)
gdr cp ./report.pdf drive:/Documents/          # 📤 Upload a file
gdr cp './*.pdf' drive:/Documents/             # 🗂️  Upload multiple files
gdr cp -r ./project drive:/backup/             # 📁 Upload a folder recursively
```

- If the **source** matches multiple items (e.g. via a glob), the **destination** must be a
  directory (an existing local dir for downloads; a Drive folder for uploads).
- On local name collisions during download, a counter is appended like `name (1).ext`.
- 🚧 Google-native formats (Google Docs/Sheets/etc.) can't be downloaded normally and are
  **skipped with a warning** for now.

> ⚠️ Quote paths containing wildcards (e.g. `'drive:/Documents/*.pdf'`) so your shell doesn't
> expand them against **local** filenames.

### 🔄 `sync` — One-way directory sync

```sh
gdr sync ./site drive:/backup/site        # ⬆️  Local → Drive
gdr sync drive:/Photos ./photos           # ⬇️  Drive → Local
gdr sync --delete ./site drive:/backup    # 🧹 Remove extras at the destination
gdr sync --dry-run ./site drive:/backup   # 👀 Preview without transferring
```

Files are compared by **size and modification time**: same size and a destination that is
as new or newer is skipped; otherwise the file is transferred. `--delete` removes files that
exist only at the destination (moved to trash on the Drive side). Google-native formats are
skipped.

### 📁 `mkdir` — Create folders

```sh
gdr mkdir drive:/Documents/newdir   # 📁 Requires the parent to exist
gdr mkdir -p drive:/a/b/c           # 🌳 Create parents as needed (idempotent)
```

### 🗑️ `rm` — Delete

```sh
gdr rm drive:/Documents/old.pdf       # 🗑️  Move to trash (recoverable)
gdr rm 'drive:/tmp/*.log'             # ✳️ Wildcards
gdr rm -r drive:/Documents/oldproject # 📁 Delete a folder
gdr rm --permanent drive:/secret.txt  # ⚠️ Permanent (skips the trash, unrecoverable)
```

Deletion moves items to the Drive trash by default. Folders need `-r`.

### ✂️ `mv` — Move & rename (within Drive)

```sh
gdr mv drive:/a.txt drive:/Documents/    # 📁 Move into a folder
gdr mv drive:/old.txt drive:/new.txt     # 🏷️  Rename
```

Moves are metadata-only on Drive, so no re-upload happens. To move between Drive and local,
`cp` then `rm`.

---

## ⌨️ Tab Completion

`cobra` generates completion scripts for each shell. Drive paths are completed
**dynamically** by querying the Drive API as you type — folder candidates get a trailing
`/`. ✨

> ⏱️ Dynamic completion hits the Drive API on each request. To keep your shell from
> hanging on slow responses, completion calls time out after **3 seconds**.

<details>
<summary>🐚 <strong>bash</strong></summary>

```sh
# Enable for the current session
source <(gdr completion bash)

# Persist (Linux)
gdr completion bash | sudo tee /etc/bash_completion.d/gdr > /dev/null
```

</details>

<details>
<summary>🦓 <strong>zsh</strong></summary>

```sh
# If using completion for the first time, enable compinit
echo "autoload -U compinit; compinit" >> ~/.zshrc

# Place the completion script somewhere on your fpath
gdr completion zsh > "${fpath[1]}/_gdr"
```

Restart your shell to take effect.

</details>

<details>
<summary>🐟 <strong>fish</strong></summary>

```sh
gdr completion fish > ~/.config/fish/completions/gdr.fish
```

</details>

---

## ⚠️ Design Notes & Known Limitations

- 🏠 Scoped to **My Drive only** — Shared Drives are not supported.
- 🔁 `cp` transfers between Drive and local in **both directions**, but not Drive-to-Drive
  (use `mv`) or local-to-local (use your OS `cp`).
- 📄 Exporting Google-native formats (Docs → PDF, etc.) is **not yet supported**; they are
  skipped by `cp` and `sync`.
- 🔄 `sync` is **one-way** (the direction set by the arguments) and compares by size and
  modification time, not by content hash.
- 👯 Drive allows **duplicate names**. When several items share a name, `ls` lists them all,
  and `cp` downloads them all, appending a counter on collision.

---

## 🛠️ Development

```sh
go build ./...   # 🔨 Build
go test ./...    # 🧪 Test
go vet ./...     # 🔎 Static analysis
```

---

<div align="center">
<sub>Built with 🐹 Go and a little ✨ AI assistance.</sub>
</div>
