package http

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/rikiisworking/jkrt/internal/auth"
)

func (a *App) handleHealth(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

func (a *App) handleIndex(c *fiber.Ctx) error {
	// Prefer static index.html when available; fall back to inline placeholder.
	if a.StaticDir != "" {
		path := a.StaticDir + "/index.html"
		if err := c.SendFile(path); err == nil {
			return nil
		}
	}
	c.Type("html", "utf-8")
	return c.SendString(placeholderHTML)
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

	c.Cookie(&fiber.Cookie{
		Name:     auth.CookieName,
		Value:    val,
		Path:     "/",
		Expires:  exp,
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
	_, err := a.Sessions.Parse(raw, time.Now().UTC())
	return err == nil
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
