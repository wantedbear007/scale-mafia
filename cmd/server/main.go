// Command server runs the baseline HTTP API.
//
// It also doubles as its own health check (`server -health-check`) so the
// runtime container image can stay free of shells and extra tools.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/bhanuprataps/scaling-systems/internal/config"
	"github.com/bhanuprataps/scaling-systems/internal/database"
	"github.com/bhanuprataps/scaling-systems/internal/observability"
	"github.com/bhanuprataps/scaling-systems/internal/server"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	healthCheck := flag.Bool("health-check", false, "probe the local /ready endpoint and exit 0/1 (used by Docker HEALTHCHECK)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("scaling-systems-api %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return
	}
	if *healthCheck {
		if err := probe(); err != nil {
			fmt.Fprintln(os.Stderr, "health check failed:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := observability.NewLogger(observability.LogConfig{
		Level:  cfg.Log.LogLevel(),
		Format: cfg.Log.Format,
		Env:    cfg.App.Env,
	})
	observability.SetAppInfo(observability.MetricsConfig{
		Env:     cfg.App.Env,
		Version: version,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting",
		"version", version,
		"addr", cfg.App.Addr(),
		"gomaxprocs", runtime.GOMAXPROCS(0),
		"num_cpu", runtime.NumCPU(),
	)

	db, err := openWithRetry(ctx, log, cfg.Database)
	if err != nil {
		return err
	}
	log.Info("database_connected",
		"dsn", cfg.Database.Redacted(),
		"max_open_connections", cfg.Database.MaxOpenConns,
		"max_idle_connections", cfg.Database.MaxIdleConns,
		"conn_max_lifetime", cfg.Database.ConnMaxLifetime.String(),
		"conn_max_idle_time", cfg.Database.ConnMaxIdleTime.String(),
	)

	app := server.New(server.Deps{Config: cfg.App, DB: db, Logger: log})

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.App.Addr())
		if err := app.Listen(cfg.App.Addr()); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		_ = db.Close()
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown_signal_received", "timeout", cfg.App.ShutdownTimeout.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx, app, db); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped")
	return nil
}

// openWithRetry connects to PostgreSQL, retrying until WaitTimeout elapses.
// Compose healthchecks already gate startup, so this is a safety net for
// database restarts and transient network failures.
func openWithRetry(ctx context.Context, log *slog.Logger, cfg config.DatabaseConfig) (*sql.DB, error) {
	deadline := time.Now().Add(cfg.WaitTimeout)
	attempt := 0

	for {
		attempt++
		db, err := database.Open(ctx, cfg)
		if err == nil {
			if attempt > 1 {
				log.Info("database_connected_after_retries", "attempts", attempt)
			}
			return db, nil
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return nil, fmt.Errorf("database unavailable after %s: %w", cfg.WaitTimeout, err)
		}
		log.Warn("database_unavailable_retrying",
			"attempt", attempt, "error", err, "retry_in", "1s")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// probe is used by the container HEALTHCHECK. It avoids needing curl/wget in
// the runtime image.
func probe() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/ready", cfg.App.Port)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}
