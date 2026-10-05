// Package database opens the PostgreSQL connection pool using Go's
// database/sql package (via the pgx stdlib driver) and exposes pool metrics.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/bhanuprataps/scaling-systems/internal/config"
)

// Open creates a *sql.DB configured from cfg and verifies connectivity.
func Open(ctx context.Context, cfg config.DatabaseConfig) (*sql.DB, error) {
	driver, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Pool sizing is part of the baseline configuration, not a tuned value.
	driver.SetMaxOpenConns(cfg.MaxOpenConns)
	driver.SetMaxIdleConns(cfg.MaxIdleConns)
	driver.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	driver.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := driver.PingContext(pingCtx); err != nil {
		_ = driver.Close()
		return nil, fmt.Errorf("ping postgres at %s: %w", cfg.Redacted(), err)
	}
	return driver, nil
}

// PoolCollector exports database/sql pool statistics as Prometheus metrics.
type PoolCollector struct {
	db *sql.DB

	connectionsOpen       *prometheus.Desc
	connectionsInUse      *prometheus.Desc
	connectionsIdle       *prometheus.Desc
	connectionsMaxOpen    *prometheus.Desc
	connectionsWaitCount  *prometheus.Desc
	connectionsWaitTime   *prometheus.Desc
	connectionsMaxIdleCl  *prometheus.Desc
	connectionsMaxLifeCl  *prometheus.Desc
	connectionsMaxIdleTCl *prometheus.Desc
}

func NewPoolCollector(db *sql.DB) *PoolCollector {
	return &PoolCollector{
		db:                    db,
		connectionsOpen:       prometheus.NewDesc("db_pool_connections_open", "Total number of connections established with PostgreSQL, both in use and idle.", nil, nil),
		connectionsInUse:      prometheus.NewDesc("db_pool_connections_in_use", "Number of connections currently in use.", nil, nil),
		connectionsIdle:       prometheus.NewDesc("db_pool_connections_idle", "Number of idle connections.", nil, nil),
		connectionsMaxOpen:    prometheus.NewDesc("db_pool_connections_max_open", "Maximum number of open connections allowed (DATABASE_MAX_OPEN_CONNECTIONS).", nil, nil),
		connectionsWaitCount:  prometheus.NewDesc("db_pool_wait_count_total", "Total number of times a goroutine had to wait for a connection.", nil, nil),
		connectionsWaitTime:   prometheus.NewDesc("db_pool_wait_duration_seconds_total", "Total time goroutines spent waiting for a connection.", nil, nil),
		connectionsMaxIdleCl:  prometheus.NewDesc("db_pool_max_idle_closed_total", "Total number of connections closed because they exceeded MaxIdleConns.", nil, nil),
		connectionsMaxLifeCl:  prometheus.NewDesc("db_pool_max_lifetime_closed_total", "Total number of connections closed because they exceeded ConnMaxLifetime.", nil, nil),
		connectionsMaxIdleTCl: prometheus.NewDesc("db_pool_max_idle_time_closed_total", "Total number of connections closed because they exceeded ConnMaxIdleTime.", nil, nil),
	}
}

func (c *PoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connectionsOpen
	ch <- c.connectionsInUse
	ch <- c.connectionsIdle
	ch <- c.connectionsMaxOpen
	ch <- c.connectionsWaitCount
	ch <- c.connectionsWaitTime
	ch <- c.connectionsMaxIdleCl
	ch <- c.connectionsMaxLifeCl
	ch <- c.connectionsMaxIdleTCl
}

func (c *PoolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.db.Stats()
	f := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}
	f(c.connectionsOpen, float64(s.OpenConnections))
	f(c.connectionsInUse, float64(s.InUse))
	f(c.connectionsIdle, float64(s.Idle))
	f(c.connectionsMaxOpen, float64(s.MaxOpenConnections))
	f(c.connectionsWaitCount, float64(s.WaitCount))
	f(c.connectionsWaitTime, s.WaitDuration.Seconds())
	f(c.connectionsMaxIdleCl, float64(s.MaxIdleClosed))
	f(c.connectionsMaxLifeCl, float64(s.MaxLifetimeClosed))
	f(c.connectionsMaxIdleTCl, float64(s.MaxIdleTimeClosed))
}

// PoolSnapshot is a small, log-friendly summary of pool state.
type PoolSnapshot struct {
	Open        int
	InUse       int
	Idle        int
	MaxOpen     int
	WaitCount   int64
	WaitSeconds float64
}

func Snapshot(db *sql.DB) PoolSnapshot {
	s := db.Stats()
	return PoolSnapshot{
		Open:        s.OpenConnections,
		InUse:       s.InUse,
		Idle:        s.Idle,
		MaxOpen:     s.MaxOpenConnections,
		WaitCount:   s.WaitCount,
		WaitSeconds: s.WaitDuration.Seconds(),
	}
}

func (p PoolSnapshot) String() string {
	return fmt.Sprintf("open=%d in_use=%d idle=%d max_open=%d wait_count=%d wait_seconds=%s",
		p.Open, p.InUse, p.Idle, p.MaxOpen, p.WaitCount, strconv.FormatFloat(p.WaitSeconds, 'f', 3, 64)+"s")
}

// WaitForReady polls Ping until it succeeds or the timeout elapses.
func WaitForReady(ctx context.Context, cfg config.DatabaseConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := Open(ctx, cfg)
		if err == nil {
			return db.Close()
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("postgres not ready after %s: %w", timeout, lastErr)
}
