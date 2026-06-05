package main

import (
	"database/sql"
	"flag"
	"io/fs"
	"log"
	"os"

	steelpage "github.com/markusfluer/steelpage-desktop"
	"github.com/markusfluer/steelpage-desktop/internal/api"
	"github.com/markusfluer/steelpage-desktop/internal/comments"
	"github.com/markusfluer/steelpage-desktop/internal/config"
	"github.com/markusfluer/steelpage-desktop/internal/db"
	"github.com/markusfluer/steelpage-desktop/internal/gitstore"
	"github.com/markusfluer/steelpage-desktop/internal/localidentity"
	"github.com/markusfluer/steelpage-desktop/internal/prefs"
	"github.com/markusfluer/steelpage-desktop/internal/render"
	"github.com/markusfluer/steelpage-desktop/internal/search"
	"github.com/markusfluer/steelpage-desktop/internal/server"
)

func main() {
	// Dev override: -bind 127.0.0.1:18080 gives the Vite proxy a fixed port.
	bind := flag.String("bind", "", "override loopback bind address (dev only)")
	flag.Parse()

	p, err := prefs.Load()
	if err != nil {
		log.Fatalf("prefs: %v", err)
	}
	configDir, err := prefs.Dir()
	if err != nil {
		log.Fatalf("prefs dir: %v", err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		log.Fatalf("create config dir: %v", err)
	}
	if err := os.MkdirAll(p.ContentDir, 0o755); err != nil {
		log.Fatalf("create content dir: %v", err)
	}

	cfg := config.FromPrefs(p, configDir)
	if *bind != "" {
		cfg.Server.Bind = *bind
	}

	dbConn, err := db.Open(cfg.DB.Path)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer dbConn.Close()
	if err := db.Migrate(dbConn); err != nil {
		log.Fatalf("db migrate: %v", err)
	}
	if err := syncLocalUser(dbConn); err != nil {
		log.Printf("sync local user: %v (continuing)", err)
	}

	dist, err := fs.Sub(steelpage.FrontendFS, "frontend/dist")
	if err != nil {
		log.Fatalf("embed: %v", err)
	}

	r := render.New(cfg.Render)
	g, err := gitstore.Open(cfg.Repo.Path)
	if err != nil {
		log.Fatalf("git: %v", err)
	}
	if p.PushRemote != "" {
		if err := g.EnsureRemote(cfg.Repo.PushRemote, p.PushRemote); err != nil {
			log.Printf("git: ensure remote: %v (continuing without push)", err)
		}
		g.SetPushToken(p.PushToken)
	}
	c := comments.New(dbConn)
	idx := search.New(dbConn, g)
	ss := search.NewStore(dbConn)

	if n, err := idx.IndexAll(cfg.Repo.Path); err != nil {
		log.Printf("search: index walk failed: %v (continuing)", err)
	} else {
		log.Printf("search: indexed %d document(s) on startup", n)
	}

	a := api.New(cfg, r, g, c, idx, ss)
	handler := server.New(a, dist)

	url, shutdown, err := server.StartLoopback(cfg.Server.Bind, handler)
	if err != nil {
		log.Fatalf("loopback: %v", err)
	}
	defer func() { _ = shutdown(nil) }()

	log.Printf("Steelpage at %s (content: %s, db: %s)", url, cfg.Repo.Path, cfg.DB.Path)

	// TODO(M7): replace with the Wails v3 application + webview window.
	select {}
}

// syncLocalUser keeps the seeded users row in step with the OS user so
// comment authorship and git authorship agree.
func syncLocalUser(dbConn *sql.DB) error {
	id := localidentity.Default()
	_, err := dbConn.Exec(`UPDATE users SET display_name = ? WHERE id = 1`, id.DisplayName)
	return err
}
