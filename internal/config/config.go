// Package config loads all runtime configuration from environment variables.
//
// Every value has a sane baseline default so the application can boot with
// zero configuration. Defaults are deliberately plain (not tuned) - see
// README "Baseline configuration values" for the reasoning.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	App      AppConfig
	Log      LogConfig
	Database DatabaseConfig
	Seed     SeedConfig
}

type AppConfig struct {
	Env             string
	Host            string
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	BodyLimitBytes  int
	// TrustProxy makes Fiber read X-Forwarded-* headers. Disabled by default:
	// the baseline talks to clients directly and trusting proxies would make
	// client-IP metrics misleading.
	TrustProxy bool
}

type LogConfig struct {
	Level  string
	Format string
}

type DatabaseConfig struct {
	Host            string
	Port            int
	Name            string
	User            string
	Password        string
	SSLMode         string
	ConnectTimeout  time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	// WaitTimeout bounds how long startup retries the initial connection
	// before giving up. Compose healthchecks gate startup, but retrying makes
	// the container resilient to transient PostgreSQL restarts.
	WaitTimeout time.Duration
	// URL, when set, overrides the discrete DATABASE_* settings.
	URL string
}

type SeedConfig struct {
	Users     int
	Products  int
	Orders    int
	BatchSize int
	Force     bool
}

// DSN builds the PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	if d.URL != "" {
		return d.URL
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&connect_timeout=%d",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode, int(d.ConnectTimeout.Seconds()),
	)
}

// Redacted returns the DSN with the password replaced, safe for logs.
func (d DatabaseConfig) Redacted() string {
	if d.URL != "" {
		return redactURLPassword(d.URL)
	}
	return fmt.Sprintf("postgres://%s:***@%s:%d/%s?sslmode=%s", d.User, d.Host, d.Port, d.Name, d.SSLMode)
}

func redactURLPassword(u string) string {
	at := strings.LastIndex(u, "@")
	scheme := strings.Index(u, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return u
	}
	return u[:scheme+3] + "***" + u[at:]
}

// Addr is the listen address for the HTTP server.
func (a AppConfig) Addr() string {
	return fmt.Sprintf("%s:%d", a.Host, a.Port)
}

// BaseURL returns the in-cluster/base URL used by tooling and tests.
func (a AppConfig) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", a.Host, a.Port)
}

func Load() (Config, error) {
	cfg := Config{
		App: AppConfig{
			Env:             env("APP_ENV", "development"),
			Host:            env("APP_HOST", "0.0.0.0"),
			Port:            envInt("APP_PORT", 8080),
			ReadTimeout:     envDuration("APP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    envDuration("APP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     envDuration("APP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: envDuration("APP_SHUTDOWN_TIMEOUT", 20*time.Second),
			BodyLimitBytes:  envInt("APP_BODY_LIMIT_BYTES", 1<<20),
			TrustProxy:      envBool("APP_TRUST_PROXY", false),
		},
		Log: LogConfig{
			Level:  strings.ToLower(env("LOG_LEVEL", "info")),
			Format: strings.ToLower(env("LOG_FORMAT", "json")),
		},
		Database: DatabaseConfig{
			Host:            env("DATABASE_HOST", "localhost"),
			Port:            envInt("DATABASE_PORT", 5432),
			Name:            env("DATABASE_NAME", "scaling"),
			User:            env("DATABASE_USER", "scaling"),
			Password:        env("DATABASE_PASSWORD", "scaling"),
			SSLMode:         env("DATABASE_SSLMODE", "disable"),
			ConnectTimeout:  envDuration("DATABASE_CONNECT_TIMEOUT", 10*time.Second),
			MaxOpenConns:    envInt("DATABASE_MAX_OPEN_CONNECTIONS", 100),
			MaxIdleConns:    envInt("DATABASE_MAX_IDLE_CONNECTIONS", 50),
			ConnMaxLifetime: envDuration("DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
			ConnMaxIdleTime: envDuration("DATABASE_CONN_MAX_IDLE_TIME", 5*time.Minute),
			WaitTimeout:     envDuration("DATABASE_WAIT_TIMEOUT", 60*time.Second),
			URL:             env("DATABASE_URL", ""),
		},
		Seed: SeedConfig{
			Users:     envInt("SEED_USERS", 100_000),
			Products:  envInt("SEED_PRODUCTS", 50_000),
			Orders:    envInt("SEED_ORDERS", 500_000),
			BatchSize: envInt("SEED_BATCH_SIZE", 5_000),
			Force:     envBool("SEED_FORCE", false),
		},
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.App.Port < 1 || c.App.Port > 65535 {
		return fmt.Errorf("APP_PORT must be in 1..65535, got %d", c.App.Port)
	}
	if c.Database.MaxOpenConns < 1 {
		return fmt.Errorf("DATABASE_MAX_OPEN_CONNECTIONS must be >= 1, got %d", c.Database.MaxOpenConns)
	}
	if c.Database.MaxIdleConns < 0 {
		return fmt.Errorf("DATABASE_MAX_IDLE_CONNECTIONS must be >= 0, got %d", c.Database.MaxIdleConns)
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("DATABASE_MAX_IDLE_CONNECTIONS (%d) must not exceed DATABASE_MAX_OPEN_CONNECTIONS (%d)",
			c.Database.MaxIdleConns, c.Database.MaxOpenConns)
	}
	if c.Seed.BatchSize < 1 {
		return fmt.Errorf("SEED_BATCH_SIZE must be >= 1, got %d", c.Seed.BatchSize)
	}
	return nil
}

// LogLevel maps the configured level string to a slog level.
func (l LogConfig) LogLevel() slog.Level {
	switch l.Level {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s=%q is not an integer, using default %d\n", key, v, def)
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a boolean, using default %t\n", key, v, def)
		return def
	}
	return b
}

func envDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a duration, using default %s\n", key, v, def)
		return def
	}
	return d
}
