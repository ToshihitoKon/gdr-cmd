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
| ⬇️ **`cp`** | Download files from Drive to your local machine (recursive folders with `-r`) |
| ✳️ **Wildcards** | Use `*`, `?`, `[...]` at every level of a path |
| ⌨️ **Tab Completion** | Subcommands, flags, **and dynamic completion of Drive paths** |
| 🔐 **No service-account keys** | Authenticate with **your own** OAuth client and Google account |

---

## 📦 Installation

```sh
go install github.com/ToshihitoKon/gdr-cmd@latest
```

The binary is named **`gdr`**. Alternatively, clone the repo and build it yourself:

```sh
git clone https://github.com/ToshihitoKon/gdr-cmd.git
cd gdr-cmd
go build -o gdr .
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

### ⬇️ `cp` — Download

```sh
gdr cp /Documents/report.pdf .          # 📥 Into the current directory
gdr cp /Documents/report.pdf ./out.pdf  # 🏷️  Save under a chosen name
gdr cp '/Documents/*.pdf' ./pdfs/       # 🗂️  Multiple files into a directory
gdr cp -r /Documents/project ./backup/  # 🔁 Download a folder recursively
```

- If the **source** matches multiple items (e.g. via a glob), the **destination** must be
  an existing directory.
- On name collisions within the same directory, a counter is appended like `name (1).ext`.
- 🚧 Google-native formats (Google Docs/Sheets/etc.) can't be downloaded normally and are
  **skipped with a warning** for now.

> ⚠️ Quote paths containing wildcards (e.g. `'/Documents/*.pdf'`) so your shell doesn't
> expand them against **local** filenames.

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
- ⬇️ `cp` supports **download only** (Drive → local). The OAuth scope is already the
  read-write `drive` scope (with future uploads in mind), so upload support can be added
  without re-authentication.
- 📄 Exporting Google-native formats (Docs → PDF, etc.) is **not yet supported**.
- 👯 Drive allows **duplicate names**. When several items share a name, `ls` lists them
  all, and `cp` downloads them all, appending a counter on collision.

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
