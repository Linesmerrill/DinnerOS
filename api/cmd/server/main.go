// Command server runs the DinnerOS HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/httpapi"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/logging"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

const (
	startupTimeout  = 20 * time.Second
	shutdownTimeout = 25 * time.Second // Heroku allows 30s after SIGTERM before SIGKILL.
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("api starting", "config", cfg)

	// Fail fast: without its database the API cannot serve anything useful, and
	// a crashing dyno is more visible than a silently degraded one.
	startupCtx, cancelStartup := context.WithTimeout(ctx, startupTimeout)
	defer cancelStartup()

	db, err := mongodb.Connect(startupCtx, mongodb.Config{
		URI:      cfg.MongoURI,
		Database: cfg.MongoDatabase,
		AppName:  cfg.AppName,
	})
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(closeCtx); err != nil {
			logger.Warn("mongodb disconnect failed", "error", err)
		}
	}()

	// Each domain package registers its IndexSet here as it is implemented.
	if err := db.EnsureIndexes(startupCtx); err != nil {
		return err
	}
	cancelStartup()
	logger.Info("mongodb connected", "database", cfg.MongoDatabase)

	srv := &http.Server{
		Addr: cfg.Addr(),
		Handler: httpapi.NewRouter(httpapi.Options{
			Logger:       logger,
			AppName:      cfg.AppName,
			Version:      cfg.Version,
			MaxBodyBytes: cfg.MaxBodyBytes,
			ReadinessChecks: []httpapi.ReadinessCheck{
				{Name: "mongodb", Check: db.Ping},
			},
		}),
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		logger.Info("api shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("api stopped")
	return nil
}
