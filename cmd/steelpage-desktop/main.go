package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] [archive-path]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Opens your Markdown archive. With archive-path, opens that")
		fmt.Fprintln(flag.CommandLine.Output(), "folder for this launch only (your saved preference is unchanged).")
		fmt.Fprintln(flag.CommandLine.Output(), "\nFlags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	p, err := prefs.Load()
	if err != nil {
		log.Fatalf("prefs: %v", err)
	}

	// An optional positional argument opens a specific archive for this
	// launch only — it overrides the saved content dir without persisting.
	// (Ignored in server mode, where the remote server is the source.)
	if arg := flag.Arg(0); arg != "" {
		abs, err := filepath.Abs(arg)
		if err != nil {
			log.Fatalf("resolve archive path %q: %v", arg, err)
		}
		p.ContentDir = abs
	}

	configDir, err := prefs.Dir()
	if err != nil {
		log.Fatalf("prefs dir: %v", err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		log.Fatalf("create config dir: %v", err)
	}

	dist, err := fs.Sub(steelpage.FrontendFS, "frontend/dist")
	if err != nil {
		log.Fatalf("embed: %v", err)
	}

	bindAddr := "127.0.0.1:0"
	if *bind != "" {
		bindAddr = *bind
	}

	// handler + cleanup differ by mode; everything after StartLoopback is shared.
	var handler http.Handler
	cleanup := func() {}

	if p.Remote() {
		log.Printf("server mode: connecting to %s", p.ServerURL)
		h, err := server.NewProxy(p.ServerURL, p.ServerToken, dist)
		if err != nil {
			log.Fatalf("server mode: %v", err)
		}
		handler = h
	} else {
		if err := os.MkdirAll(p.ContentDir, 0o755); err != nil {
			log.Fatalf("create content dir: %v", err)
		}
		cfg := config.FromPrefs(p, configDir)

		dbConn, err := db.Open(cfg.DB.Path)
		if err != nil {
			log.Fatalf("db: %v", err)
		}
		if err := db.Migrate(dbConn); err != nil {
			log.Fatalf("db migrate: %v", err)
		}
		if err := syncLocalUser(dbConn); err != nil {
			log.Printf("sync local user: %v (continuing)", err)
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
		handler = server.NewLocal(a, dist)
		cleanup = func() {
			if err := dbConn.Close(); err != nil {
				log.Printf("db close: %v", err)
			}
		}
		log.Printf("local mode (content: %s, db: %s)", cfg.Repo.Path, cfg.DB.Path)
	}

	url, shutdown, err := server.StartLoopback(bindAddr, handler)
	if err != nil {
		log.Fatalf("loopback: %v", err)
	}

	log.Printf("Steelpage at %s", url)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	if *headless {
		<-sigCh
		_ = shutdown(context.Background())
		cleanup()
		return
	}

	app := application.New(application.Options{
		Name:        "Steelpage",
		Description: "Personal Markdown archive",
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:              "Steelpage",
		URL:                url,
		Width:              1280,
		Height:             800,
		MinWidth:           700,
		MinHeight:          400,
		UseApplicationMenu: true,
	})

	app.Menu.Set(buildMenu(app, win))

	// Drain in-flight saves and release the SQLite WAL before the process
	// exits so no -wal/-shm files are left locked behind (acceptance #6).
	app.OnShutdown(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			log.Printf("loopback shutdown: %v", err)
		}
		cleanup()
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
