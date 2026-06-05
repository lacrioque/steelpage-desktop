# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->


## Build & Test

```bash
make build      # SPA build + embed + Go binary (./steelpage-desktop)
make test       # go test ./... + svelte-check
make dev-backend   # headless loopback API on 127.0.0.1:18080
make dev-frontend  # Vite HMR on :5173, proxies /api + /docs to :18080
```

Linux build needs `gtk4-devel` + `webkitgtk6.0-devel` (Wails v3 webview).
Frontend type-check: `cd frontend && npm run check`.

## Architecture Overview

Single-user desktop app (Wails v3 webview → loopback chi HTTP API).
Markdown archive in `~/Documents/Steelpage` versioned by an embedded
go-git engine (`internal/gitstore`, no git binary needed). SQLite
(`internal/db`, migrations 001/002/008) holds comments + FTS5 search
index. Prefs live in `prefs.json` at the platform config dir
(`internal/prefs`); `internal/config.FromPrefs` synthesizes the runtime
config. Identity is the constant local user (`internal/localidentity`,
id=1). Entry point: `cmd/steelpage-desktop` (`-headless`, `-bind` dev
flags). Plan + decision log: `docs/plans/steelpage-desktop.md`.

## Conventions & Patterns

- No auth/multi-user/email code — this fork deliberately deleted it;
  re-derive from upstream `lacrioque/steelpage` if ever needed.
- Remote git interaction is push-only and opt-in (no pull/rebase).
- The loopback server must never bind a non-127.0.0.1 address.
- Angular commit conventions (see user's global rules).
