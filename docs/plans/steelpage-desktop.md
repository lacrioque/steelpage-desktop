# steelpage-desktop — Technical Outline

> A fork of [Steelpage](https://github.com/lacrioque/steelpage) repackaged as a
> single-user desktop application via [Wails](https://wails.io). Same source
> of truth (Git-backed Markdown), same renderer, same Svelte/Carbon UI —
> different deployment shape (no listening port, no auth surface, no SMTP).

---

## Mission (one paragraph)

A self-contained binary that drops the operator into a personal Markdown
archive — own files on disk, own git history, optional remote push when they
want it. No SSO, no multi-user, no email. Open the app, see your archive,
edit, save (which commits to your local repo). Closes cleanly when the
window closes. Same look and feel as the server version because the entire
SPA is reused unchanged.

---

## Non-goals (v1)

Decisions that simplify the fork. Revisit only when there's a concrete user
request behind them.

- **Multi-user / sessions / OIDC / MFA / permissions / API tokens.** Single
  local user. Delete the entire `internal/auth/`, `internal/middleware/`,
  `internal/permissions/`, `internal/tokens/`, `internal/groups/`,
  `internal/users/` layer.
- **SMTP / email verification / password reset.** No mailer at all.
- **Live config editing.** Settings UI uses Wails dialogs / a simple
  preferences pane instead of `configsvc`. Keep the package only if you
  decide to surface a few hot-reloadable knobs.
- **Listening HTTP port exposed to the network.** The local backend (if
  kept — see "Integration shape" below) binds to `127.0.0.1:0` with an OS-
  assigned port, only the webview talks to it. No public bind, no remote
  access by accident.
- **Search across multiple archives.** One archive at a time. Switching is a
  preferences action.
- **Mobile / web.** Wails is desktop only (macOS, Windows, Linux).

---

## Locked architecture decisions

| Decision | Choice | Why |
|---|---|---|
| GUI framework | **Wails v3** (or v2 if v3 still feels beta) | Closest Go-to-Tauri analog. OS-native webview, Svelte SPA reused, single binary. |
| Frontend | Reuse current `frontend/` from steelpage | Carbon-Svelte SPA already exists, type-checked, i18n-ready. |
| Integration shape | **Loopback HTTP + webview** for v1 | Steelpage's API barely changes. Wails bindings can come later for native menu / file dialog integration. |
| Storage location | Platform conventional + visible to user | Content in `~/Documents/Steelpage` (XDG / `~/Library/Application Support` on macOS for the SQLite DB). |
| Git interface | **Start with `git` CLI shim** (existing code), migrate to **`go-git`** package-by-package | CLI works on every dev machine; go-git is the real long-term play because users won't always have git installed. |
| Concurrency model | Single user, single window, single process | No sessions, no locking ceremonies. |

### Decisions to resolve in the first session

These are the things the new session has to answer before it starts deleting
code:

1. **Wails v2 vs v3** — v3 is more ergonomic (cleaner bindings, better
   menus); v2 is more battle-tested. Read the v3 status page before
   choosing.
2. **Keep `internal/api` HTTP router or replace with Wails bindings** —
   recommended: keep it for v1 (the SPA already calls `/api/*`, nothing
   needs to change), but plan a v2 migration to Wails bindings for the
   "native" feel (no localhost port, no JSON marshalling overhead).
3. **What to do with the optional remote push** — keep it (operator can add
   a remote and push their archive to GitHub) or drop it? Recommend
   **keep**, opt-in per config.

---

## Repo layout proposal

Fork the existing repo, rename module path, prune aggressively. Result:

```
steelpage-desktop/
├── cmd/
│   └── steelpage-desktop/
│       └── main.go             # Wails bootstrap, embed SPA, set up signals
├── internal/
│   ├── api/                    # /api/* handlers — TRIMMED (delete admin, auth, tokens, permissions, comments-by-user, etc.)
│   ├── comments/               # Same package; author field becomes "you"
│   ├── config/                 # User-prefs flavor (no auth/oidc/email blocks)
│   ├── db/                     # Migrations 1, 2, 8 (init + search + comment replies). Drop 3 (auth), 4 (perms), 5 (tokens), 6 (email flows), 7 (mfa), 9 (config overrides), 10 (user prefs).
│   ├── docs/                   # Unchanged
│   ├── frontmatter/            # Unchanged
│   ├── gitstore/               # Keep, but plan go-git migration (see "Git" section)
│   ├── render/                 # Unchanged
│   └── search/                 # Unchanged
├── frontend/                   # Copied verbatim, then trimmed:
│   ├── src/
│   │   ├── routes/             # Delete LoginView, AdminView, AccountView, ForgotView, ResetView, VerifyView. CarbonShell loses auth/admin/account routing.
│   │   ├── components/         # Delete EmailVerifyBanner, ConfigEditor, FileActions (keep), etc.
│   │   └── lib/                # Delete auth-api, admin-api, mfa-api, tokens-api, config-api. Keep api, docs-api, comments-api, search-api, router, document-store, editor, fonts.
│   └── ...
├── docs/
│   └── steelpage-desktop.md    # this doc, updated as decisions are made
├── scripts/
│   ├── build.sh                # wails build wrapper
│   └── package_macos.sh        # codesign + notarize hooks
└── go.mod                      # module github.com/<you>/steelpage-desktop
```

**Rename rules**:

- Module path: `github.com/lacrioque/steelpage` → `github.com/<you>/steelpage-desktop`. One `find . -type f -name '*.go' -exec sed -i 's|github.com/lacrioque/steelpage|github.com/<you>/steelpage-desktop|g' {} +`.
- Binary name: `steelpage` → `steelpage-desktop` (or just `steelpage` if you don't intend to ever run both side-by-side).
- App identifier for macOS code-signing: `app.steelpage.desktop` or similar.

---

## What carries over from `steelpage` unchanged

These packages have no auth dependency and just work:

- `internal/docs/` — file walking + safe-join
- `internal/frontmatter/` — YAML frontmatter parser
- `internal/render/` — Goldmark + Chroma + bluemonday pipeline
- `internal/search/` — FTS5 indexer + extractor + store
- `internal/db/` — migration runner (just point it at a different migrations
  dir)
- `internal/comments/` — full package (the levenshtein re-anchor ladder is
  the killer feature; keep it). Authors collapse to a single hardcoded
  "you" record so the existing FK structure stays valid.

Frontend modules unchanged:

- `lib/api.ts`, `lib/docs-api.ts`, `lib/comments-api.ts`, `lib/search-api.ts`
- `lib/router.ts` (minus auth-protected routes)
- `lib/document-store.ts`, `lib/editor.ts`, `lib/fonts.ts`, `lib/i18n.ts`
- `lib/locales/{en,de}.json` — trim the `auth/*`, `mfa/*`, `email_verify/*`,
  `admin/*`, `password_reset/*` namespaces
- `components/ArchiveTree`, `CommentsSidebar`, `AddCommentModal`,
  `SearchOverlay`, `LocaleToggle`, `VersionMenu`, `FileActions`
- `routes/DocumentView`, `routes/NotFound`
- All `@fontsource/*` packages
- All Carbon CSS + Mermaid + CodeMirror setup

---

## What gets deleted

Hard prune. Don't keep "just in case" — re-derive from the upstream repo if
the desktop product ever grows back into multi-user.

**Backend**:

- `internal/auth/` (whole package)
- `internal/users/` (whole package — replaced by a single hardcoded local
  identity helper)
- `internal/groups/`
- `internal/permissions/`
- `internal/tokens/`
- `internal/middleware/` (the identity middleware in particular)
- `internal/mailer/` (whole package)
- `internal/configsvc/` (optional — see decision #2 above)
- `internal/api/`: `admin*.go`, `me*.go`, `authorize.go`, `history.go` if
  not surfaced. Keep `docs.go`, `docs_ops.go`, `comments.go`, `render.go`,
  `search.go`, `tree.go`.
- `cmd/steelpage/main.go` — replaced by `cmd/steelpage-desktop/main.go`

**Frontend**:

- Routes: `LoginView`, `AdminView`, `AccountView`, `ForgotView`,
  `ResetView`, `VerifyView`
- Components: `EmailVerifyBanner`, `ConfigEditor`
- Lib: `auth-api`, `admin-api`, `mfa-api`, `tokens-api`, `config-api`,
  `identity` (replace with a stub returning a fixed local user)

**Migrations**: keep `001_init.sql`, `002_search_relocated.sql`,
`008_comment_replies.sql`. Drop the rest.

---

## What gets written fresh

### `cmd/steelpage-desktop/main.go`

Wails app entrypoint. Roughly:

```go
package main

import (
    "embed"
    // wails imports
    "github.com/<you>/steelpage-desktop/internal/server"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
    app := application.New(application.Options{
        Name:        "Steelpage",
        Description: "Personal Markdown archive",
        Bind: []any{ /* optional native bindings */ },
    })

    // Start the loopback HTTP API on a random local port.
    addr, shutdown := server.StartLoopback()
    defer shutdown()

    window := app.NewWebviewWindow(application.WebviewWindowOptions{
        Title: "Steelpage",
        URL:   addr,                       // e.g. http://127.0.0.1:54321
        Width: 1280, Height: 800,
        Menu:  buildMenu(),                // File / Edit / View / Help
    })
    _ = window

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

### `internal/server/loopback.go`

Trimmed version of the existing `internal/server/server.go` — same chi
routes, no auth middleware, binds to `127.0.0.1:0` and returns the
allocated address. Same `internal/api` handlers behind it.

### `internal/localidentity/localidentity.go` (or fold into `internal/api`)

Returns the single-user identity record that handlers + comments use. No DB
lookup — just a constant:

```go
type LocalIdentity struct {
    ID          int64
    DisplayName string
    Email       string
}

func Default() LocalIdentity {
    name := os.Getenv("USER")
    if name == "" { name = "You" }
    return LocalIdentity{ID: 1, DisplayName: name, Email: name + "@localhost"}
}
```

Comments still need an author_id FK satisfied — insert a single row at first
boot and reuse `id=1` forever. Migration `001_init.sql` can be patched to do
this inline.

### `internal/prefs/prefs.go`

Replaces `configsvc` with something dead simple. Reads + writes a small JSON
file at the platform-appropriate config dir (e.g.,
`~/.config/steelpage-desktop/prefs.json`). Stores:

- Content directory (default `~/Documents/Steelpage`)
- Theme / font preference
- Last opened doc

A Wails menu item ("Preferences…") opens a small Carbon modal that edits
these.

### `cmd/steelpage-desktop/menu.go`

Native menu: File → New / Open Archive / Preferences; Edit → standard
edit shortcuts; View → Show comments / Search; Help → About + Open
GitHub.

---

## Storage layout per platform

Don't hard-code; use [`os.UserConfigDir`](https://pkg.go.dev/os#UserConfigDir)
+ `os.UserHomeDir`.

| OS | Config + DB | Content (default) |
|---|---|---|
| Linux | `~/.config/steelpage-desktop/` | `~/Documents/Steelpage/` |
| macOS | `~/Library/Application Support/Steelpage/` | `~/Documents/Steelpage/` |
| Windows | `%APPDATA%\Steelpage\` | `%USERPROFILE%\Documents\Steelpage\` |

DB filename: `steelpage.db`. Content dir is a git repo initialized on first
launch if empty.

---

## Git — phased migration off the CLI

`internal/gitstore/` shells out to `git`. Desktop users won't always have
git installed. Two-phase plan:

1. **Phase 1 (week 1)**: keep the CLI shim, but detect missing git at
   startup. If absent, show a Wails dialog: "Git not found. Install git or
   skip versioning (you'll lose history)." Allow both paths.
2. **Phase 2**: port one operation at a time to `github.com/go-git/go-git/v5`.
   Order of difficulty (easy → hard):
   - `History(path)` → `repo.Log(&git.LogOptions{FileName: &path})`
   - `Commit(path, msg, author)` → straightforward
   - `MoveFile`, `RemoveFile` → tracked via worktree status
   - `PullRebase` → not directly supported by go-git; either skip or fall
     back to the CLI when remote sync is enabled
   - `Push` → `repo.Push(...)` works for HTTPS; SSH needs ssh-agent
     handling

go-git is pure-Go so it cross-compiles cleanly. Bundle size goes up ~5 MB.

---

## UI changes from server steelpage

Mostly removals.

**Header**:

- Drop the user avatar / sign-in / admin / sign-out icons.
- Drop the locale toggle if you want (or keep it).
- Add a "Choose archive…" button that triggers the Wails directory picker
  and reloads the SPA against the new content path.

**SideNav**: unchanged.

**Document view**: unchanged (edit, save, history, comments, search all
work the same).

**No `/login`, `/admin`, `/account`, `/forgot`, `/reset`, `/verify`
routes.** The route guard reduces to "always render the doc route".

**Preferences**: small modal opened from the native menu. Fields: content
dir (read-only display + "Change…" button), theme (light/dark when you
add dark mode), default font.

---

## Packaging + signing per platform

| OS | Tooling | Output | Signing |
|---|---|---|---|
| Linux | `wails build` + `appimagetool` | `Steelpage-x86_64.AppImage` (or .deb/.rpm if you want native installers) | Not required; consider signing with GPG. |
| macOS | `wails build -platform darwin/universal` | `.app` bundle, then `.dmg` | Apple Developer ID + notarization. ~$99/yr. Skippable for personal use; users get the "unidentified developer" warning. |
| Windows | `wails build -platform windows/amd64` | `.exe` + optional `.msi` (use [WiX](https://wixtoolset.org/) or NSIS) | Windows code-signing certificate (EV or OV). Annual cost. Skippable but triggers SmartScreen warning. |

The existing GitHub Actions release workflow (`.github/workflows/release.yml`)
can be adapted: same tag-triggered fan-out, but each matrix entry runs
`wails build` and uploads platform-specific artifacts instead of a tarball.

---

## v1 acceptance criteria

1. **Cold launch**: double-click the binary → window opens within 2 s →
   shows an empty archive with a "Create your first doc" prompt.
2. **Content path**: defaults to `~/Documents/Steelpage`, can be changed
   via Preferences, change takes effect on next launch.
3. **Doc round-trip**: create a doc, edit, save → file written to disk,
   git commit recorded with the OS user's name + email.
4. **Search**: type in the search box, find the doc by content.
5. **Comments**: add a line-anchored comment, edit the doc, comment
   re-anchors via the existing fuzzy ladder.
6. **Quit**: close the window → process exits cleanly, no orphan SQLite
   locks.
7. **Cross-platform**: binary builds for darwin/arm64, darwin/amd64,
   linux/amd64, windows/amd64. (linux/arm64 nice-to-have.)
8. **No network access by default**: confirm with `tcpdump` or similar
   that the app makes zero outbound connections on idle.

---

## Out of scope (file as follow-ups, do NOT do in v1)

- Multi-archive switching from inside the running app (close + reopen is
  fine).
- Cloud sync / Dropbox-style backend.
- Dark mode (separate task — Carbon supports it well).
- Plugin system.
- Mobile companion app.
- Encryption-at-rest for the content dir.
- Auto-update from inside the app (use GitHub Releases for now).

---

## First-session plan (suggested order)

1. `git clone` + rename module path + prune deletions.
2. `wails init` in the repo root, then move the existing `frontend/` over
   the Wails-generated one.
3. Get the SPA building inside Wails (`wails dev`).
4. Wire `internal/server/loopback.go` and point the webview at it.
5. Strip the auth-protected handlers from `internal/api/`.
6. Patch migration 001 to seed the single local-user row.
7. Hello-world: open the app, see the README from a default
   `~/Documents/Steelpage` archive.
8. **Ship a working v0.1.0** — binary that loads, lists docs, opens one.
   Then iterate.

---

## Reference paths in upstream Steelpage

So the new session can grep for prior art:

- HTTP routing wiring: `internal/server/server.go`
- chi middleware stack: same file, lines ~40–110
- Render pipeline: `internal/render/render.go`
- Comment re-anchor ladder: `internal/comments/comments.go` `MarkPath`
- Git operations: `internal/gitstore/gitstore.go`
- SPA → backend boundary: `frontend/src/lib/api.ts`,
  `frontend/src/lib/document-store.ts`
- Embedded SPA: `frontend_embed.go`
- Live config (delete pattern, but useful reference for env-vars):
  `internal/configsvc/configsvc.go`

---

## Open questions for the first session

1. Wails v2 or v3?
2. Keep the loopback HTTP server long-term, or migrate to Wails bindings
   in v2?
3. Drop the `comments` feature for v1 (simpler) or keep it (the fuzzy
   re-anchor ladder is genuinely useful for a personal archive too)?
   Recommend: **keep**.
4. Drop the `search` feature for v1? Recommend: **keep** — FTS5 over a
   personal archive is fast and adds a lot of value, and the code already
   exists.
5. Drop the `i18n` system for v1? Recommend: **drop the German locale**,
   keep the i18n machinery for future-proofing.
6. Drop the version-history dropdown? Recommend: **keep** — it's the
   git history view, naturally useful for a personal log.
