package http

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/rikiisworking/jkrt/internal/analyze"
	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/config"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/export"
	"github.com/rikiisworking/jkrt/internal/jlpt"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/schedule"
	"github.com/rikiisworking/jkrt/internal/scrape"
)

// App wraps the Fiber app and dependencies.
type App struct {
	Fiber       *fiber.App
	Config      config.Config
	Store       *auth.Store
	Sessions    *auth.Manager
	StaticDir   string
	AuthEnabled bool
	DB          *db.DB
	Analyzer    *analyze.Analyzer
	Review      *review.Service
	Export      *export.Service
	// HTTPClient is used by Scrape; nil → default client with scrape.DefaultTimeout.
	// Tests inject a fixture transport so no network is dialed.
	HTTPClient scrape.HTTPDoer
}

// Options configures the HTTP app.
type Options struct {
	Config     config.Config
	Store      *auth.Store
	Sessions   *auth.Manager
	StaticDir  string
	DB         *db.DB
	Analyzer   *analyze.Analyzer
	Review     *review.Service
	HTTPClient scrape.HTTPDoer
}

// New builds a Fiber application with Phase 0–6 routes.
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

	// Single schedule.Params source: prefer caller's Review; else defaults.
	// Sync onto DB so extract NewCard and LibraryCounts mature threshold match Review.
	params := schedule.DefaultParams()
	if opts.Review != nil {
		params = opts.Review.Params()
	}
	if opts.DB != nil {
		opts.DB.SetScheduleParams(params)
		// Production N2+ filter (tests call AllowAllWords after Open).
		// Skip if test already set AllowAllWords (wordEligible non-nil) — SetJLPT clears it.
		// Only wire when not bypassed: check by re-applying only for production path.
		// Tests that use AllowAllWords must call it *after* New — so wire JLPT first here,
		// then test helpers call AllowAllWords after New if needed.
		// Actually newTestAppOpts calls AllowAllWords before New — SetJLPT would wipe it.
		// So: only SetJLPT when wordEligible is nil (default production).
		// AllowAllWords sets wordEligible; we skip SetJLPT if already set.
		// open path: Open → (tests AllowAllWords) → New. If AllowAllWords first, skip SetJLPT.
		wireJLPT(opts.DB, opts.Config)
	}
	rev := opts.Review
	if rev == nil && opts.DB != nil {
		rev = review.New(opts.DB, params)
	}

	a := &App{
		Fiber:       f,
		Config:      opts.Config,
		Store:       opts.Store,
		Sessions:    opts.Sessions,
		StaticDir:   opts.StaticDir,
		AuthEnabled: opts.Config.AuthEnabled,
		DB:          opts.DB,
		Analyzer:    opts.Analyzer,
		Review:      rev,
		Export:      export.New(opts.DB),
		HTTPClient:  opts.HTTPClient,
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
	protected.Post("/api/scrape", a.handleScrape)
	protected.Get("/api/stats", a.handleStats)
	protected.Get("/api/export", a.handleExport)
	protected.Get("/review", a.handleReviewGet)
	protected.Post("/review", a.handleReviewPost)
	protected.Get("/articles", a.handleArticlesList)
	protected.Get("/articles/:id", a.handleArticleDetail)
	protected.Post("/articles/:id/sentences/:sid/extract", a.handleSentenceExtract)
}

// wireJLPT installs N2+ extract filter + optional headless classifier.
// No-op when DB already has AllowAllWords (tests).
func wireJLPT(d *db.DB, cfg config.Config) {
	if d == nil || d.HasWordEligibleOverride() {
		return
	}
	opt := jlpt.ResolveOptions{
		Cache:       &jlpt.SQLCache{SQL: d.SQL()},
		MaxClassify: cfg.JLPTClassifyMaxPerExt,
	}
	if cfg.JLPTClassifyMaxPerExt == 0 {
		opt.MaxClassify = 10
	}
	use := false
	switch cfg.JLPTClassify {
	case "on":
		use = true
	case "auto", "":
		use = jlpt.LookPath("grok")
	case "off":
		use = false
	default:
		use = jlpt.LookPath("grok")
	}
	if use {
		opt.Classifier = jlpt.NewHeadless(jlpt.HeadlessConfig{
			Model:   cfg.JLPTClassifyModel,
			Timeout: cfg.JLPTClassifyTimeout,
		})
	}
	d.SetJLPT(opt)
}

// newScraper builds a Scraper from app deps (all DefaultSources; scrape is always multi-feed).
func (a *App) newScraper() *scrape.Scraper {
	var client scrape.HTTPDoer = a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: scrape.DefaultTimeout}
	}
	sources := scrape.DefaultSources(a.Config.NHKMainRSSURL)
	return scrape.New(a.DB, sources, client)
}

// Listen starts the HTTP server.
func (a *App) Listen() error {
	return a.Fiber.Listen(a.Config.Addr)
}
