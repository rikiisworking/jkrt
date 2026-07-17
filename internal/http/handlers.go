package http

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/rikiisworking/jkrt/internal/auth"
	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/export"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/snapshot"
)

// handleScrape runs dual NHK RSS Scrape and returns per-source JSON
// (DEVELOPMENT_PLAN HTTP surface: POST /api/scrape).
// HTMX requests (dashboard button) get a small HTML summary fragment.
func (a *App) handleScrape(c *fiber.Ctx) error {
	if a.DB == nil {
		if c.Get("HX-Request") == "true" {
			return c.Status(fiber.StatusInternalServerError).SendString("database not configured")
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database not configured"})
	}
	if a.Analyzer == nil {
		if c.Get("HX-Request") == "true" {
			return c.Status(fiber.StatusInternalServerError).SendString("analyzer not configured")
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "analyzer not configured"})
	}
	// Always 200 with partial-success per source (plan: 200 JSON with per-source errors).
	result := a.newScraper().Run(c.Context(), time.Now().UTC())
	if c.Get("HX-Request") == "true" {
		c.Type("html", "utf-8")
		return c.SendString(scrapeResultHTML(result))
	}
	return c.JSON(result)
}

// handleReviewGet serves the next Review Card (or empty state) as HTML.
func (a *App) handleReviewGet(c *fiber.Ctx) error {
	if a.Review == nil {
		return c.Status(fiber.StatusInternalServerError).SendString("review not configured")
	}
	now := time.Now().UTC()
	res, err := a.Review.Next(db.LearnerUserID, now)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.Type("html", "utf-8")
	if res.Empty {
		return c.SendString(reviewEmptyHTML())
	}
	return c.SendString(reviewHTML(res.Item, ""))
}

// handleReviewPost grades the shown Card then re-nexts.
// Non-HTMX: 302 to GET /review. HTMX: 200 partial for #review-main.
// Stale double-submit (ErrStaleCard) is treated as success and re-nexts.
func (a *App) handleReviewPost(c *fiber.Ctx) error {
	if a.Review == nil {
		return c.Status(fiber.StatusInternalServerError).SendString("review not configured")
	}

	cardID, err1 := strconv.ParseInt(c.FormValue("card_id"), 10, 64)
	sentenceID, err2 := strconv.ParseInt(c.FormValue("sentence_id"), 10, 64)
	grade := c.FormValue("grade")
	updatedAt := c.FormValue("card_updated_at")
	if err1 != nil || err2 != nil || cardID == 0 || sentenceID == 0 {
		return c.Status(fiber.StatusBadRequest).SendString("card_id and sentence_id required")
	}

	now := time.Now().UTC()
	err := a.Review.Grade(db.LearnerUserID, cardID, sentenceID, grade, updatedAt, now)
	if err != nil {
		switch {
		case errors.Is(err, review.ErrStaleCard):
			// Already graded this presentation — re-next without applying again.
		case errors.Is(err, review.ErrBadGrade):
			return a.reviewErrorResponse(c, fiber.StatusBadRequest, err.Error(), now)
		case errors.Is(err, review.ErrNotFound):
			return a.reviewErrorResponse(c, fiber.StatusNotFound, err.Error(), now)
		case errors.Is(err, review.ErrSentenceNotLinked):
			return a.reviewErrorResponse(c, fiber.StatusBadRequest, err.Error(), now)
		default:
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
	}

	return a.reviewNextResponse(c, now, "")
}

// reviewNextResponse returns HTMX partial or full-page redirect after grade.
func (a *App) reviewNextResponse(c *fiber.Ctx, now time.Time, errMsg string) error {
	if c.Get("HX-Request") == "true" {
		res, nerr := a.Review.Next(db.LearnerUserID, now)
		if nerr != nil {
			return fiber.NewError(fiber.StatusInternalServerError, nerr.Error())
		}
		c.Type("html", "utf-8")
		if res.Empty {
			return c.SendString(reviewEmptyPartial())
		}
		return c.SendString(reviewPartial(res.Item, errMsg))
	}
	return c.Redirect("/review", fiber.StatusFound)
}

// reviewErrorResponse surfaces validation errors: HTMX gets next card + banner; else plain status.
func (a *App) reviewErrorResponse(c *fiber.Ctx, status int, msg string, now time.Time) error {
	if c.Get("HX-Request") == "true" {
		// Prefer re-showing next (or empty) with a banner rather than a bare string.
		res, nerr := a.Review.Next(db.LearnerUserID, now)
		if nerr != nil {
			return c.Status(status).SendString(msg)
		}
		c.Status(status)
		c.Type("html", "utf-8")
		if res.Empty {
			return c.SendString(reviewEmptyPartial())
		}
		return c.SendString(reviewPartial(res.Item, msg))
	}
	return c.Status(status).SendString(msg)
}

func (a *App) handleHealth(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

func (a *App) handleIndex(c *fiber.Ctx) error {
	c.Type("html", "utf-8")
	// Phase 4: live dashboard when DB is wired. Fallback for misconfigured boots.
	if a.DB == nil {
		if a.StaticDir != "" {
			path := filepath.Join(a.StaticDir, "index.html")
			if err := c.SendFile(path); err == nil {
				return nil
			}
		}
		return c.SendString(placeholderHTML)
	}

	now := time.Now().UTC()
	view, err := snapshot.Load(a.Review, a.DB, db.LearnerUserID, now)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	data := DashboardData{
		Stats:        view.Queue,
		Library:      view.Library,
		ArticleCount: view.Library.Articles,
	}
	if fetched, ok, err := a.DB.LastArticleFetchedAt(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	} else if ok {
		data.LastFetchedAt = fetched
		data.HasLastFetch = true
	}
	return c.SendString(dashboardHTML(data))
}

// handleStats serves GET /api/stats (Phase 6 JSON queue + library numbers).
func (a *App) handleStats(c *fiber.Ctx) error {
	if a.DB == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "database not configured"})
	}
	now := time.Now().UTC()
	view, err := snapshot.Load(a.Review, a.DB, db.LearnerUserID, now)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{
		"queue":   view.Queue,
		"library": view.Library,
		"as_of":   view.AsOf.Format(time.RFC3339),
	})
}

// handleExport serves GET /api/export?format=json|csv (Phase 6).
func (a *App) handleExport(c *fiber.Ctx) error {
	if a.DB == nil || a.Export == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "export not configured"})
	}
	format, err := export.ParseFormat(c.Query("format", "json"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	now := time.Now().UTC()
	view, err := snapshot.Load(a.Review, a.DB, db.LearnerUserID, now)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	snap, err := a.Export.BuildSnapshot(db.LearnerUserID, view, now)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	switch format {
	case export.FormatCSV:
		var buf bytes.Buffer
		if err := export.WriteCardsCSV(&buf, snap.Cards); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		c.Set("Content-Type", "text/csv; charset=utf-8")
		c.Set("Content-Disposition", `attachment; filename="jkrt-cards.csv"`)
		return c.Send(buf.Bytes())
	default:
		var buf bytes.Buffer
		if err := export.WriteJSON(&buf, snap); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		c.Set("Content-Type", "application/json; charset=utf-8")
		c.Set("Content-Disposition", `attachment; filename="jkrt-export.json"`)
		return c.Send(buf.Bytes())
	}
}

// handleArticlesList serves GET /articles (browse).
func (a *App) handleArticlesList(c *fiber.Ctx) error {
	if a.DB == nil {
		return c.Status(fiber.StatusInternalServerError).SendString("database not configured")
	}
	items, err := a.DB.ListArticles(db.DefaultArticleListLimit)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.Type("html", "utf-8")
	return c.SendString(articlesListHTML(items))
}

// handleArticleDetail serves GET /articles/:id (sentences under one Article).
func (a *App) handleArticleDetail(c *fiber.Ctx) error {
	if a.DB == nil {
		return c.Status(fiber.StatusInternalServerError).SendString("database not configured")
	}
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return c.Status(fiber.StatusBadRequest).SendString("invalid article id")
	}
	art, sents, found, err := a.DB.GetArticle(id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	c.Type("html", "utf-8")
	if !found {
		c.Status(fiber.StatusNotFound)
		return c.SendString(articleNotFoundHTML())
	}
	return c.SendString(articleDetailHTML(art, sents))
}

// handleSentenceExtract opts a Sentence into study (Words/Cards) — ADR 0006.
// Non-HTMX: 302 back to article. HTMX: 200 sentence row partial.
func (a *App) handleSentenceExtract(c *fiber.Ctx) error {
	if a.DB == nil {
		return c.Status(fiber.StatusInternalServerError).SendString("database not configured")
	}
	if a.Analyzer == nil {
		return c.Status(fiber.StatusInternalServerError).SendString("analyzer not configured")
	}
	articleID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || articleID <= 0 {
		return c.Status(fiber.StatusBadRequest).SendString("invalid article id")
	}
	sentenceID, err := strconv.ParseInt(c.Params("sid"), 10, 64)
	if err != nil || sentenceID <= 0 {
		return c.Status(fiber.StatusBadRequest).SendString("invalid sentence id")
	}

	now := time.Now().UTC()
	res, err := a.DB.ExtractSentenceForArticle(db.LearnerUserID, articleID, sentenceID, a.Analyzer, now)
	if err != nil {
		if errors.Is(err, db.ErrSentenceNotFound) || errors.Is(err, db.ErrArticleMismatch) {
			return c.Status(fiber.StatusNotFound).SendString("sentence not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	sent, found, err := a.DB.GetSentence(articleID, sentenceID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if !found {
		return c.Status(fiber.StatusNotFound).SendString("sentence not found")
	}

	if c.Get("HX-Request") != "" {
		c.Type("html", "utf-8")
		return c.SendString(sentenceRowHTML(articleID, sent, res))
	}
	return c.Redirect(fmt.Sprintf("/articles/%d", articleID), fiber.StatusFound)
}

func (a *App) handleLoginGet(c *fiber.Ctx) error {
	if !a.AuthEnabled {
		return c.Redirect("/", fiber.StatusFound)
	}
	// Already logged in → home
	if a.sessionOK(c) {
		return c.Redirect("/", fiber.StatusFound)
	}
	c.Type("html", "utf-8")
	return c.SendString(loginHTML(""))
}

func (a *App) handleLoginPost(c *fiber.Ctx) error {
	if !a.AuthEnabled {
		return c.Redirect("/", fiber.StatusFound)
	}

	password := c.FormValue("password")
	hash, err := a.Store.PasswordHash()
	if err != nil || !auth.CheckPassword(hash, password) {
		c.Status(fiber.StatusUnauthorized)
		c.Type("html", "utf-8")
		return c.SendString(loginHTML("Invalid password."))
	}

	now := time.Now().UTC()
	val, exp, err := a.Sessions.Issue(auth.UserID, now)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not create session")
	}

	// HttpOnly + SameSite=Lax always. Secure when the request is HTTPS (or
	// X-Forwarded-Proto=https behind Cloudflare Tunnel). Expires/MaxAge follow
	// JKRT_SESSION_TTL (default 168h). See docs/auth-and-tunnel.md.
	maxAge := int(a.Sessions.TTL().Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	c.Cookie(&fiber.Cookie{
		Name:     auth.CookieName,
		Value:    val,
		Path:     "/",
		Expires:  exp,
		MaxAge:   maxAge,
		HTTPOnly: true,
		SameSite: "Lax",
		Secure:   isHTTPS(c),
	})
	return c.Redirect("/", fiber.StatusFound)
}

func (a *App) handleLogout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: "Lax",
		Secure:   isHTTPS(c),
	})
	return c.Redirect("/login", fiber.StatusFound)
}

func (a *App) requireAuth(c *fiber.Ctx) error {
	if !a.AuthEnabled {
		return c.Next()
	}
	if a.sessionOK(c) {
		return c.Next()
	}
	// API-ish paths get 401 JSON; HTML gets redirect to login.
	if isAPIPath(c.Path()) || wantsJSONAPI(c) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	return c.Redirect("/login", fiber.StatusFound)
}

func (a *App) sessionOK(c *fiber.Ctx) bool {
	if a.Sessions == nil {
		return false
	}
	raw := c.Cookies(auth.CookieName)
	if raw == "" {
		return false
	}
	sess, err := a.Sessions.Parse(raw, time.Now().UTC())
	if err != nil {
		return false
	}
	// v1 single Learner: only user id=1 is valid.
	return sess.UserID == auth.UserID
}

func isHTTPS(c *fiber.Ctx) bool {
	if c.Protocol() == "https" {
		return true
	}
	proto := c.Get("X-Forwarded-Proto")
	return strings.EqualFold(proto, "https")
}

func isAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

func wantsJSONAPI(c *fiber.Ctx) bool {
	accept := c.Get("Accept")
	return strings.Contains(accept, "application/json")
}
