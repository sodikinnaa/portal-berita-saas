package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	appcms "porta-berita/internal/application/cms"
	"porta-berita/internal/config"
	"porta-berita/internal/httpserver"
	"porta-berita/internal/infrastructure/persistence/jsonstore"
	"porta-berita/internal/infrastructure/persistence/postgresstore"
	"porta-berita/internal/web"
)

func main() {
	cfg, err := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: config.LogLevel()}))
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	templates, err := web.ParseTemplates()
	if err != nil {
		log.Error("parse templates", "error", err)
		os.Exit(1)
	}

	var store appcms.ContentStore
	
	// Determine database store type automatically
	useJSONStore := false
	if cfg.DatabaseURL == "" || cfg.DatabaseURL == "json" || cfg.DatabaseURL == "local" || !strings.HasPrefix(cfg.DatabaseURL, "postgres://") {
		useJSONStore = true
	}

	if useJSONStore {
		dbPath := "data/portal_db.json"
		if cfg.DatabaseURL != "" && cfg.DatabaseURL != "json" && cfg.DatabaseURL != "local" {
			dbPath = cfg.DatabaseURL
		}
		jsonStore, jsonErr := jsonstore.OpenStore(dbPath)
		err = jsonErr
		store = jsonStore
		log.Info("using local JSON file store (no external database needed)", "path", dbPath)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ReadTimeout)
		pgStore, pgErr := postgresstore.OpenPostgresStore(ctx, cfg.DatabaseURL)
		cancel()
		err = pgErr
		store = pgStore
		log.Info("using postgres store")
	}

	if err != nil {
		log.Error("open store", "error", err)
		os.Exit(1)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	server := httpserver.New(cfg, log, templates, store)

	errCh := make(chan error, 1)
	go func() {
		log.Info("server starting", "addr", cfg.Addr, "environment", cfg.Environment)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Error("server failed", "error", err)
		os.Exit(1)
	case sig := <-stopCh:
		log.Info("shutdown signal received", "signal", sig.String())
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped")
}
