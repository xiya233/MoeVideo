package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/response"
)

const (
	localUserID   = "auth_user_id"
	localUsername = "auth_username"
)

func RequireAuth(a *app.App) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := bearerToken(c.Get("Authorization"))
		if token == "" {
			return response.Error(c, fiber.StatusUnauthorized, "missing bearer token")
		}
		claims, err := a.JWT.ParseAccessToken(token)
		if err != nil {
			return response.Error(c, fiber.StatusUnauthorized, "invalid access token")
		}
		c.Locals(localUserID, claims.UserID)
		c.Locals(localUsername, claims.Username)
		return c.Next()
	}
}

func OptionalAuth(a *app.App) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := bearerToken(c.Get("Authorization"))
		if token == "" {
			return c.Next()
		}
		claims, err := a.JWT.ParseAccessToken(token)
		if err != nil {
			return c.Next()
		}
		c.Locals(localUserID, claims.UserID)
		c.Locals(localUsername, claims.Username)
		return c.Next()
	}
}

func CurrentUserID(c *fiber.Ctx) string {
	if v := c.Locals(localUserID); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func CurrentUsername(c *fiber.Ctx) string {
	if v := c.Locals(localUsername); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func bearerToken(authHeader string) string {
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
