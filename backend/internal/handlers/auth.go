package handlers

import (
	"context"
	"database/sql"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/auth"
	"moevideo/backend/internal/models"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/util"
)

type registerRequest struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"password_confirm"`
}

type loginRequest struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Register(c *fiber.Ctx) error {
	var req registerRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Username == "" || req.Email == "" || req.Password == "" || req.PasswordConfirm == "" {
		return response.Error(c, fiber.StatusBadRequest, "username, email, password and password_confirm are required")
	}
	if req.Password != req.PasswordConfirm {
		return response.Error(c, fiber.StatusBadRequest, "password and password_confirm do not match")
	}
	if len(req.Password) < 8 {
		return response.Error(c, fiber.StatusBadRequest, "password must be at least 8 characters")
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to hash password")
	}

	now := nowString()
	userID := newID()
	_, err = h.app.DB.Exec(
		`INSERT INTO users (id, username, email, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID,
		req.Username,
		req.Email,
		passwordHash,
		now,
		now,
	)
	if err != nil {
		if isConflictErr(err) {
			return response.Error(c, fiber.StatusConflict, "username or email already exists")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to create user")
	}

	tokens, err := h.issueSession(c.UserContext(), userID, req.Username)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create session")
	}

	user, err := fetchUserBrief(h.app.DB, h.app.Storage, userID, true)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load user")
	}

	return response.Created(c, fiber.Map{
		"user":   user,
		"tokens": tokens,
	})
}

func (h *Handler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" || req.Password == "" {
		return response.Error(c, fiber.StatusBadRequest, "account and password are required")
	}

	var userID, username, passwordHash, status string
	row := h.app.DB.QueryRow(
		`SELECT id, username, password_hash, status
		 FROM users
		 WHERE username = ? OR email = ?
		 LIMIT 1`,
		req.Account,
		strings.ToLower(req.Account),
	)
	if err := row.Scan(&userID, &username, &passwordHash, &status); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusUnauthorized, "invalid credentials")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to login")
	}
	if status != "active" {
		return response.Error(c, fiber.StatusForbidden, "user is disabled")
	}
	if err := auth.ComparePassword(passwordHash, req.Password); err != nil {
		return response.Error(c, fiber.StatusUnauthorized, "invalid credentials")
	}

	tokens, err := h.issueSession(c.UserContext(), userID, username)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create session")
	}
	user, err := fetchUserBrief(h.app.DB, h.app.Storage, userID, true)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load user")
	}

	return response.OK(c, fiber.Map{
		"user":   user,
		"tokens": tokens,
	})
}

func (h *Handler) Refresh(c *fiber.Ctx) error {
	var req refreshRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		return response.Error(c, fiber.StatusBadRequest, "refresh_token is required")
	}

	hash := util.SHA256Hex(req.RefreshToken)
	var userID, username, expiresAt string
	row := h.app.DB.QueryRow(
		`SELECT rt.user_id, u.username, rt.expires_at
		 FROM auth_refresh_tokens rt
		 JOIN users u ON u.id = rt.user_id
		 WHERE rt.token_hash = ?
		   AND rt.revoked_at IS NULL
		 LIMIT 1`,
		hash,
	)
	if err := row.Scan(&userID, &username, &expiresAt); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusUnauthorized, "invalid refresh token")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to refresh token")
	}

	expiresTime, err := util.ParseTime(expiresAt)
	if err != nil || expiresTime.Before(nowUTC()) {
		_, _ = h.app.DB.Exec(`UPDATE auth_refresh_tokens SET revoked_at = ? WHERE token_hash = ?`, nowString(), hash)
		return response.Error(c, fiber.StatusUnauthorized, "refresh token expired")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to refresh token")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(c.UserContext(), `UPDATE auth_refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`, nowString(), hash); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to rotate token")
	}

	tokens, err := h.issueSessionTx(c.UserContext(), tx, userID, username)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to issue tokens")
	}
	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit refresh")
	}

	return response.OK(c, fiber.Map{"tokens": tokens})
}

func (h *Handler) Logout(c *fiber.Ctx) error {
	var req logoutRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		return response.Error(c, fiber.StatusBadRequest, "refresh_token is required")
	}

	uid := currentUserID(c)
	hash := util.SHA256Hex(req.RefreshToken)
	res, err := h.app.DB.Exec(
		`UPDATE auth_refresh_tokens
		 SET revoked_at = ?
		 WHERE token_hash = ? AND user_id = ? AND revoked_at IS NULL`,
		nowString(),
		hash,
		uid,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to logout")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return response.Error(c, fiber.StatusNotFound, "refresh token not found")
	}
	return response.OK(c, fiber.Map{"revoked": true})
}

func (h *Handler) issueSession(ctx context.Context, userID, username string) (models.TokenPair, error) {
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return models.TokenPair{}, err
	}
	defer tx.Rollback()

	pair, err := h.issueSessionTx(ctx, tx, userID, username)
	if err != nil {
		return models.TokenPair{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.TokenPair{}, err
	}
	return pair, nil
}

func (h *Handler) issueSessionTx(ctx context.Context, tx *sql.Tx, userID, username string) (models.TokenPair, error) {
	accessToken, accessExpiresAt, err := h.app.JWT.GenerateAccessToken(userID, username, h.app.Config.AccessTokenTTL)
	if err != nil {
		return models.TokenPair{}, err
	}
	refreshToken, err := util.RandomToken(48)
	if err != nil {
		return models.TokenPair{}, err
	}
	refreshExpiresAt := nowUTC().Add(h.app.Config.RefreshTokenTTL)
	refreshHash := util.SHA256Hex(refreshToken)

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO auth_refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		newID(),
		userID,
		refreshHash,
		util.FormatTime(refreshExpiresAt),
		nowString(),
	)
	if err != nil {
		return models.TokenPair{}, err
	}

	return models.TokenPair{
		AccessToken:      accessToken,
		AccessExpiresAt:  util.FormatTime(accessExpiresAt),
		RefreshToken:     refreshToken,
		RefreshExpiresAt: util.FormatTime(refreshExpiresAt),
	}, nil
}
