// Package observability sets up structured logging and the Prometheus
// metrics registry used by the baseline API.
package observability

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// NewLogger builds a JSON (or text) structured logger writing to stdout.
//
// slog is used directly to keep dependencies minimal; the JSON handler is
// part of the standard library.
func NewLogger(cfg LogConfig) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: cfg.Level,
	}

	var h slog.Handler
	if cfg.Format == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}

	l := slog.New(h).With(
		slog.String("service", "api"),
		slog.String("env", cfg.Env),
	)
	return l
}

type LogConfig struct {
	Level  slog.Level
	Format string
	Env    string
}

type MetricsConfig struct {
	Env     string
	Version string
}

var (
	// HTTP metrics. `route` is the *route pattern* (e.g. /api/v1/users/:id),
	// never the raw path, so label cardinality stays bounded.
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests handled, by method, route pattern and status code.",
	}, []string{"method", "route", "status"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by method and route pattern.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	httpRequestsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Number of HTTP requests currently being served.",
	})

	httpRequestBodyBytes = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_size_bytes",
		Help:    "Size of request bodies in bytes, by method and route pattern.",
		Buckets: []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576},
	}, []string{"method", "route"})

	httpResponseBodyBytes = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_response_size_bytes",
		Help:    "Size of response bodies in bytes, by method, route pattern and status class.",
		Buckets: []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576},
	}, []string{"method", "route", "status"})

	// Repository/query level metrics. `operation` is a small fixed set
	// (e.g. "users.list") - never a table name built from user input.
	dbQueryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "SQL round-trip latency in seconds, by operation name.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})

	dbQueryTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "db_queries_total",
		Help: "Total SQL statements executed, by operation name and outcome.",
	}, []string{"operation", "outcome"})

	// Application info gauge: static values useful when comparing runs.
	appInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "app_info",
		Help: "Static build/runtime information. Value is always 1.",
	}, []string{"go_version", "goos", "goarch", "num_cpu", "gomaxprocs", "version", "env"})
)

// registry owns every metric this process exposes. Using a private registry
// (rather than the global default) keeps tests isolated and makes it explicit
// what /metrics actually serves.
var registry = prometheus.NewRegistry()

func init() {
	registry.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		httpRequestsInFlight,
		httpRequestBodyBytes,
		httpResponseBodyBytes,
		dbQueryDuration,
		dbQueryTotal,
	)
	// Go runtime + process collectors give us goroutines, heap allocation,
	// GC activity/pauses and CPU counters with no extra code.
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// Registry exposes the metrics registry for additional collectors.
func Registry() *prometheus.Registry { return registry }

// Register adds collectors, tolerating ones that are already registered
// (which happens when the app is constructed more than once in a test binary).
func Register(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := registry.Register(c); err != nil {
			var already prometheus.AlreadyRegisteredError
			if !errors.As(err, &already) {
				panic(err)
			}
		}
	}
}

// ObserveHTTPRequest records one completed HTTP request.
func ObserveHTTPRequest(method, route string, status int, durationSec float64, reqBytes, respBytes int) {
	statusStr := strconv.Itoa(status)
	httpRequestsTotal.WithLabelValues(method, route, statusStr).Inc()
	httpRequestDuration.WithLabelValues(method, route).Observe(durationSec)
	if reqBytes >= 0 {
		httpRequestBodyBytes.WithLabelValues(method, route).Observe(float64(reqBytes))
	}
	if respBytes >= 0 {
		httpResponseBodyBytes.WithLabelValues(method, route, statusStr).Observe(float64(respBytes))
	}
}

// IncInFlight / DecInFlight track currently served requests.
func IncInFlight() { httpRequestsInFlight.Inc() }
func DecInFlight() { httpRequestsInFlight.Dec() }

// ObserveQuery records one SQL round trip from the repository layer.
func ObserveQuery(operation, outcome string, seconds float64) {
	dbQueryDuration.WithLabelValues(operation).Observe(seconds)
	dbQueryTotal.WithLabelValues(operation, outcome).Inc()
}

// SetAppInfo publishes static runtime information. Called once at startup.
func SetAppInfo(cfg MetricsConfig) {
	appInfo.WithLabelValues(
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
		strconv.Itoa(runtime.NumCPU()),
		strconv.Itoa(runtime.GOMAXPROCS(0)),
		cfg.Version,
		cfg.Env,
	).Set(1)
}

// WithRequest returns a context that logging middleware can use to attach
// the request id to every log line emitted while handling the request.
type ctxKey int

const requestIDKey ctxKey = iota

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}
