package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/config"
	"github.com/rikiisworking/jkrt/internal/db"
	jkrthttp "github.com/rikiisworking/jkrt/internal/http"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("jkrt: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	workdir, err := os.Getwd()
	if err != nil {
		return err
	}
	migrationsDir := filepath.Join(workdir, "migrations")
	if _, err := os.Stat(filepath.Join(migrationsDir, "001_init.sql")); err != nil {
		// Fall back to walking up for go.mod / migrations.
		if found, findErr := db.FindMigrationsDir(); findErr == nil {
			migrationsDir = found
		}
	}

	database, err := db.Open(cfg.DBPath, migrationsDir)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer func() { _ = database.Close() }()

	ana, err := analyze.New()
	if err != nil {
		return fmt.Errorf("analyzer: %w", err)
	}

	store := auth.NewStore(database.SQL())
	var sessions *auth.Manager

	if cfg.AuthEnabled {
		if err := auth.Bootstrap(store, true, cfg.Password); err != nil {
			return fmt.Errorf("auth bootstrap: %w", err)
		}
		sessions = auth.NewManager(cfg.SessionSecret, cfg.SessionTTL)
	} else {
		// Auth off: still need users.id=1 for Card FKs (extract / scrape).
		if err := auth.EnsureLearnerRow(store); err != nil {
			return fmt.Errorf("ensure learner: %w", err)
		}
		store = nil // no login paths when auth is off
	}

	staticDir := filepath.Join(workdir, "web", "static")
	if _, err := os.Stat(staticDir); err != nil {
		// Allow running from other cwd if static is missing; handlers fall back to inline HTML.
		staticDir = ""
	}

	app := jkrthttp.New(jkrthttp.Options{
		Config:    cfg,
		Store:     store,
		Sessions:  sessions,
		StaticDir: staticDir,
		DB:        database,
		Analyzer:  ana,
	})

	log.Printf("jkrt listening on %s (auth=%v)", cfg.Addr, cfg.AuthEnabled)
	return app.Listen()
}
