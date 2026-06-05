package main

import (
	"context"
	"database/sql"
	"flag"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

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
	// Dev overrides: -bind gives the Vite proxy a fixed port, -headless
	// runs the loopback API without a window (CI / backend work).
	bind := flag.String("bind", "", "override loopback bind address (dev only)")
	headless := flag.Bool("headless", false, "run the API without a window (dev only)")
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

	log.Printf("Steelpage at %s (content: %s, db: %s)", url, cfg.Repo.Path, cfg.DB.Path)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	if *headless {
		<-sigCh
		_ = shutdown(context.Background())
		return
	}

	app := application.New(application.Options{
		Name:        "Steelpage",
		Description: "Personal Markdown archive",
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Menu.Set(buildMenu(app))

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:              "Steelpage",
		URL:                url,
		Width:              1280,
		Height:             800,
		MinWidth:           700,
		MinHeight:          400,
		UseApplicationMenu: true,
	})

	// Drain in-flight saves and release the SQLite WAL before the process
	// exits so no -wal/-shm files are left locked behind (acceptance #6).
	app.OnShutdown(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			log.Printf("loopback shutdown: %v", err)
		}
		if err := dbConn.Close(); err != nil {
			log.Printf("db close: %v", err)
		}
	})

	// Route Ctrl-C / SIGTERM through the same shutdown path as closing
	// the window so the DB always closes cleanly.
	go func() {
		<-sigCh
		app.Quit()
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// syncLocalUser keeps the seeded users row in step with the OS user so
// comment authorship and git authorship agree.
func syncLocalUser(dbConn *sql.DB) error {
	id := localidentity.Default()
	_, err := dbConn.Exec(`UPDATE users SET display_name = ? WHERE id = 1`, id.DisplayName)
	return err
}
