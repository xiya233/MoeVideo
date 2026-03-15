package middleware

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/config"
	"moevideo/backend/internal/response"
)

func RequireSameOriginWrites(cfg config.Config) fiber.Handler {
	allowed := make(map[string]struct{})
	for _, raw := range cfg.CORSAllowedOrigins {
		origin := normalizeOrigin(raw)
		if origin == "" {
			continue
		}
		allowed[origin] = struct{}{}
	}
	if publicOrigin := originFromURL(cfg.PublicBaseURL); publicOrigin != "" {
		allowed[publicOrigin] = struct{}{}
	}

	return func(c *fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
		default:
			return c.Next()
		}

		// Internal tools / tests using Bearer tokens can bypass origin checks.
		if strings.TrimSpace(c.Get("Authorization")) != "" {
			return c.Next()
		}

		accessCookie := strings.TrimSpace(c.Cookies(AccessTokenCookieName))
		refreshCookie := strings.TrimSpace(c.Cookies(RefreshTokenCookieName))
		if accessCookie == "" && refreshCookie == "" {
			return c.Next()
		}

		origin := normalizeOrigin(c.Get("Origin"))
		if origin == "" {
			origin = normalizeOrigin(originFromURL(c.Get("Referer")))
		}
		if origin == "" {
			return response.Error(c, fiber.StatusForbidden, "forbidden origin")
		}
		if _, ok := allowed["*"]; ok {
			return c.Next()
		}
		if _, ok := allowed[origin]; ok {
			return c.Next()
		}
		return response.Error(c, fiber.StatusForbidden, "forbidden origin")
	}
}

func normalizeOrigin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return strings.TrimRight(trimmed, "/")
}

func originFromURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
