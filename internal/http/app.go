package http

import (
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/config"
)

// App wraps the Fiber app and auth dependencies.
type App struct {
	Fiber       *fiber.App
	Config      config.Config
	Store       *auth.Store
	Sessions    *auth.Manager
	StaticDir   string
	AuthEnabled bool
}

// Options configures the HTTP app.
type Options struct {
	Config    config.Config
	Store     *auth.Store
	Sessions  *auth.Manager
	StaticDir string
}

// New builds a Fiber application with Phase 0 routes.
func New(opts Options) *App {
	f := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			if strings.Contains(c.Get("Accept"), "application/json") ||
				c.Path() == "/health" ||
				strings.HasPrefix(c.Path(), "/api/") {
				return c.Status(code).JSON(fiber.Map{"error": err.Error()})
			}
			return c.Status(code).SendString(err.Error())
		},
	})
	f.Use(recover.New())

	a := &App{
		Fiber:       f,
		Config:      opts.Config,
		Store:       opts.Store,
		Sessions:    opts.Sessions,
		StaticDir:   opts.StaticDir,
		AuthEnabled: opts.Config.AuthEnabled,
	}
	a.routes()
	return a
}

func (a *App) routes() {
	// Public
	a.Fiber.Get("/health", a.handleHealth)
	a.Fiber.Get("/login", a.handleLoginGet)
	a.Fiber.Post("/login", a.handleLoginPost)

	// Public static assets only (CSS/images under web/static/assets).
	// Do not mount HTML here — index is served only via authenticated handleIndex.
	if a.StaticDir != "" {
		a.Fiber.Static("/static", filepath.Join(a.StaticDir, "assets"))
	}

	// Protected
	protected := a.Fiber.Group("/", a.requireAuth)
	protected.Get("/", a.handleIndex)
	protected.Post("/logout", a.handleLogout)
}

// Listen starts the HTTP server.
func (a *App) Listen() error {
	return a.Fiber.Listen(a.Config.Addr)
}

// Shutdown gracefully shuts down the server.
func (a *App) Shutdown() error {
	return a.Fiber.Shutdown()
}
