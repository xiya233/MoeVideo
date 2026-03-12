package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/response"
)

func RequireAdmin(a *app.App) fiber.Handler {
	return func(c *fiber.Ctx) error {
		uid := CurrentUserID(c)
		if uid == "" {
			return response.Error(c, fiber.StatusUnauthorized, "missing bearer token")
		}

		var role, status string
		err := a.DB.QueryRowContext(c.UserContext(),
			`SELECT COALESCE(role, 'user'), status FROM users WHERE id = ? LIMIT 1`,
			uid,
		).Scan(&role, &status)
		if err != nil {
			return response.Error(c, fiber.StatusForbidden, "admin access required")
		}

		if status != "active" || !strings.EqualFold(role, "admin") {
			return response.Error(c, fiber.StatusForbidden, "admin access required")
		}
		return c.Next()
	}
}
