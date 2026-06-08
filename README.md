<p align="center">
  <img src="frontend/public/logo.png" alt="Steelpage" width="160">
</p>

# Steelpage Desktop

> Your personal Markdown archive as a desktop app: own files on disk, own git
> history, full-text search, line-anchored comments — in a single binary with
> no server, no account, no network listener.

A single-user fork of [Steelpage](https://github.com/lacrioque/steelpage)
repackaged with [Wails v3](https://v3.wails.io). Markdown is the source of
truth; an **embedded git engine** ([go-git](https://github.com/go-git/go-git),
pure Go — no git installation required) records every save as a commit;
SQLite holds the live context (comments, search index). The Go backend renders
Markdown → HTML; the Svelte/Carbon SPA runs in the OS-native webview, talking
to a loopback HTTP API on `127.0.0.1:<random>` that only the webview can reach.

---

## Features

- **Backend Markdown rendering** — Goldmark + Chroma syntax highlighting +
  bluemonday sanitization. Mermaid blocks render client-side.
- **Git as the audit log** — every save is a commit authored as your OS user.
  The archive repo is auto-initialized on first launch; no git binary needed.
- **Version history** — per-document commit dropdown, view any revision.
- **Line-anchored comments** with replies and a fuzzy re-anchor ladder on save
  (`exact line → ±10 line scan → fuzzy match → orphan`).
- **Full-text search** via SQLite FTS5 with BM25-weighted snippets (Ctrl/Cmd+K).
- **Opt-in push-only backup** — set an HTTPS remote + token in Preferences and
  every save fans out to it in the background. No pull, no rebase, no surprises.
- **Bot-ready output** at `/docs/<path>?botready=1` for AI agents.
- **i18n** — English and German.

## What it deliberately does NOT have

No accounts, no OIDC/MFA, no permissions, no email, no listening port exposed
to the network. If you want the multi-user wiki, use upstream
[Steelpage](https://github.com/lacrioque/steelpage).

---

## Storage layout

| | Linux | macOS | Windows |
|---|---|---|---|
| Prefs + DB | `~/.config/steelpage-desktop/` | `~/Library/Application Support/Steelpage/` | `%APPDATA%\Steelpage\` |
| Archive (default) | `~/Documents/Steelpage/` | `~/Documents/Steelpage/` | `%USERPROFILE%\Documents\Steelpage\` |

The archive folder is a plain git repository — point any other git tool at it
whenever you like. Change it via **File → Open Archive…** (applies on next
launch).

---

## Connect to a steelpage server (optional)

As an alternative to the local archive, the desktop can act as a thin client
of a multi-user [Steelpage](https://github.com/lacrioque/steelpage) server,
authenticated with one of that server's API tokens.

1. On the server's web UI, go to **Account → Tokens**, create a token with the
   scopes you need (`read`, `comment`, `write`), and copy it (`spt_…` — shown
   once).
2. In the desktop, open **Preferences**, choose **Steelpage server**, paste the
   server URL + token, and hit **Test connection** (it shows "connected as
   *you*"). Save and relaunch.

While connected, the desktop serves its own UI but proxies all document,
comment, and search traffic to the server with your token — the server
enforces its own permissions and scopes. A header chip shows which server
you're on. Clear the server fields in Preferences to return to the local
archive. (The token is stored in `prefs.json`; OS-keychain storage is a
planned follow-up.)

---

## Install

Pre-built artifacts are attached to each [GitHub release](https://github.com/lacrioque/steelpage-desktop/releases):

| Platform | Download |
|---|---|
| **Linux** | `.deb` / `.rpm` (depend on system GTK4 + WebKitGTK 6.0), `.AppImage`, or raw `.tar.gz` |
| **macOS** (Apple Silicon) | `.dmg` (ad-hoc signed — right-click → Open the first time) |
| **Windows** | Setup `.exe` (NSIS) or raw `.zip` |

The `.deb`/`.rpm` pull in the GTK/WebKit runtime via your package manager
(`sudo apt install ./steelpage-desktop-*.deb` / `sudo dnf install ./steelpage-desktop-*.rpm`).
macOS and Windows builds are not yet code-signed/notarized, so you'll see a
Gatekeeper / SmartScreen prompt until that's added.

---

## Build from source

Requirements: Go ≥ 1.26, Node ≥ 20. On Linux additionally the webview headers:

```bash
# Fedora
sudo dnf install gtk4-devel webkitgtk6.0-devel
# Debian/Ubuntu
sudo apt install libgtk-4-dev libwebkitgtk-6.0-dev
```

```bash
make build     # builds the SPA, embeds it, compiles ./steelpage-desktop
./steelpage-desktop                 # opens your default archive
./steelpage-desktop ~/notes/wiki    # opens a specific archive (this launch only)
```

The optional path argument overrides the archive location for that launch
without changing your saved preference — handy for keeping several archives
and opening one ad-hoc. The folder is created (and git-initialized) if it
doesn't exist yet. To change the default permanently, use **File → Open
Archive…**.

## Development

```bash
# Full app (embedded SPA, real window)
go run ./cmd/steelpage-desktop

# Frontend work with HMR — two terminals:
make dev-backend    # headless API on 127.0.0.1:18080
make dev-frontend   # Vite on http://localhost:5173 (proxies /api + /docs)

# Tests
make test           # go test ./... + svelte-check
```

`-headless` runs the loopback API without a window; `-bind 127.0.0.1:18080`
pins the dev port for the Vite proxy.

---

## Architecture

```
 prefs.json ─┐                       ┌────────────────────────────┐
             ▼                       │  OS-native webview (Wails) │
   ┌─────────────────────────┐       │   Svelte + Carbon SPA      │
   │  Go binary              │ HTTP  │                            │
   │   • chi router          │◄──────┤  http://127.0.0.1:<rand>   │
   │   • goldmark renderer   │       └────────────────────────────┘
   │   • go-git engine       │
   │   • FTS5 search         │            ~/Documents/Steelpage
   │   • comments + reanchor │◄──────────  (your git repo)
   │   • SQLite (WAL)        │
   └─────────────────────────┘
```

Plan + decision log: [`docs/plans/steelpage-desktop.md`](docs/plans/steelpage-desktop.md).

## License

Same as upstream Steelpage.
