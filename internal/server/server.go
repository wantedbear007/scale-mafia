// Package server wires the HTTP layer together: dependencies, middleware and
// routes. Both cmd/server and the integration tests build the app from here so
// they exercise exactly the same stack.
package server

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bhanuprataps/scaling-systems/internal/config"
	"github.com/bhanuprataps/scaling-systems/internal/database"
	"github.com/bhanuprataps/scaling-systems/internal/handlers"
	appmiddleware "github.com/bhanuprataps/scaling-systems/internal/middleware"
	"github.com/bhanuprataps/scaling-systems/internal/observability"
	"github.com/bhanuprataps/scaling-systems/internal/repositories"
	"github.com/bhanuprataps/scaling-systems/internal/services"
)

// Deps are the runtime dependencies of the HTTP application.
type Deps struct {
	Config config.AppConfig
	DB     *sql.DB
	Logger *slog.Logger
}

// New builds the Fiber application with all routes registered.
func New(deps Deps) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "scaling-systems-api",
		DisableStartupMessage: true,
		ErrorHandler:          appmiddleware.ErrorHandler(deps.Logger),
		ReadTimeout:           deps.Config.ReadTimeout,
		WriteTimeout:          deps.Config.WriteTimeout,
		IdleTimeout:           deps.Config.IdleTimeout,
		BodyLimit:             deps.Config.BodyLimitBytes,
		// The baseline serves clients directly. X-Forwarded-* headers are only
		// honoured when APP_TRUST_PROXY=true, otherwise a client could spoof
		// its IP and corrupt the client-IP fields in logs.
		EnableTrustedProxyCheck: deps.Config.TrustProxy,
		ServerHeader:            "scaling-systems-api",
	})

	// Go runtime and process collectors come from observability; the
	// connection pool collector is specific to this process.
	observability.Register(database.NewPoolCollector(deps.DB))

	// --- middleware -------------------------------------------------------
	app.Use(appmiddleware.RequestID())
	app.Use(appmiddleware.Recoverer(deps.Logger))
	app.Use(appmiddleware.Metrics())
	app.Use(appmiddleware.Logger(deps.Logger))

	// --- repositories -> services -> handlers -----------------------------
	userRepo := repositories.NewUserRepository(deps.DB)
	productRepo := repositories.NewProductRepository(deps.DB)
	orderRepo := repositories.NewOrderRepository(deps.DB)

	userSvc := services.NewUserService(userRepo)
	productSvc := services.NewProductService(productRepo)
	orderSvc := services.NewOrderService(orderRepo, userRepo)

	userHandler := handlers.NewUserHandler(userSvc)
	productHandler := handlers.NewProductHandler(productSvc)
	orderHandler := handlers.NewOrderHandler(orderSvc)
	healthHandler := handlers.NewHealthHandler(deps.DB)

	// --- observability endpoints ------------------------------------------
	// No compression here on purpose: response compression would change
	// data_sent numbers and CPU cost in the benchmark.
	app.Get("/metrics", adaptor.HTTPHandler(promhttp.HandlerFor(observability.Registry(), promhttp.HandlerOpts{})))

	// --- health ------------------------------------------------------------
	app.Get("/health", healthHandler.Liveness)
	app.Get("/ready", healthHandler.Readiness)

	// --- API v1 ------------------------------------------------------------
	api := app.Group("/api/v1")
	registerCRUD(api.Group("/users"),
		userHandler.Create, userHandler.List, userHandler.Get, userHandler.Update, userHandler.Delete)
	registerCRUD(api.Group("/products"),
		productHandler.Create, productHandler.List, productHandler.Get, productHandler.Update, productHandler.Delete)
	registerCRUD(api.Group("/orders"),
		orderHandler.Create, orderHandler.List, orderHandler.Get, orderHandler.Update, orderHandler.Delete)

	// Everything else gets the same JSON 404 envelope.
	app.Use(func(c *fiber.Ctx) error { return fiber.ErrNotFound })

	return app
}

func registerCRUD(g fiber.Router, create, list, get, update, del fiber.Handler) {
	g.Post("/", create)
	g.Get("/", list)
	g.Get("/:id", get)
	g.Put("/:id", update)
	g.Delete("/:id", del)
}

// Shutdown gracefully stops the HTTP server, then closes the database pool.
func Shutdown(ctx context.Context, app *fiber.App, db *sql.DB) error {
	shutdownErr := app.ShutdownWithContext(ctx)
	closeErr := db.Close()
	if shutdownErr != nil {
		return shutdownErr
	}
	return closeErr
}
