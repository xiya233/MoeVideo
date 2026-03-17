package handlers

import (
	"context"
	"database/sql"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"

	appconfig "moevideo/backend/internal/config"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/security"
)

const (
	ytdlpCookieFormatHeader     = "header"
	ytdlpCookieFormatCookiesTxt = "cookies_txt"

	userYTDLPCookieMaxLabelLen      = 80
	userYTDLPCookieMaxDomainRuleLen = 255
	userYTDLPCookieMaxHeaderLen     = 16384
	userYTDLPCookieMaxTxtLen        = 262144

	userYTDLPCookieCryptoPurpose = "user-ytdlp-cookie-v1"
)

type userYTDLPCookieRecord struct {
	ID         string
	UserID     string
	Label      string
	DomainRule string
	Format     string
	CipherText string
	Nonce      string
	CreatedAt  string
	UpdatedAt  string
}

type createUserYTDLPCookieRequest struct {
	Label      string `json:"label"`
	DomainRule string `json:"domain_rule"`
	Format     string `json:"format"`
	Content    string `json:"content"`
}

type patchUserYTDLPCookieRequest struct {
	Label      *string `json:"label"`
	DomainRule *string `json:"domain_rule"`
	Format     *string `json:"format"`
	Content    *string `json:"content"`
}

func (h *Handler) ListMyYTDLPCookies(c *fiber.Ctx) error {
	uid := currentUserID(c)
	forURL := strings.TrimSpace(c.Query("for_url"))

	filterHost := ""
	if forURL != "" {
		parsed, err := neturl.Parse(forURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(parsed.Hostname()) == "" {
			return response.Error(c, fiber.StatusBadRequest, "for_url is invalid")
		}
		filterHost = strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	}

	rows, err := h.app.DB.QueryContext(c.UserContext(), `
SELECT id, user_id, label, domain_rule, format, cipher_text, nonce, created_at, updated_at
FROM user_ytdlp_cookies
WHERE user_id = ?
ORDER BY updated_at DESC`, uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list yt-dlp cookies")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0, 16)
	for rows.Next() {
		var row userYTDLPCookieRecord
		if err := rows.Scan(
			&row.ID,
			&row.UserID,
			&row.Label,
			&row.DomainRule,
			&row.Format,
			&row.CipherText,
			&row.Nonce,
			&row.CreatedAt,
			&row.UpdatedAt,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to read yt-dlp cookies")
		}
		if filterHost != "" && !appconfig.IsHostMatchedByDomainList(filterHost, []string{row.DomainRule}) {
			continue
		}
		items = append(items, fiber.Map{
			"id":          row.ID,
			"label":       row.Label,
			"domain_rule": row.DomainRule,
			"format":      row.Format,
			"updated_at":  row.UpdatedAt,
			"created_at":  row.CreatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list yt-dlp cookies")
	}

	return response.OK(c, fiber.Map{"items": items})
}

func (h *Handler) CreateMyYTDLPCookie(c *fiber.Ctx) error {
	uid := currentUserID(c)

	var req createUserYTDLPCookieRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	label, domainRule, format, content, err := normalizeUserYTDLPCookieInput(req.Label, req.DomainRule, req.Format, req.Content)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	cipherText, nonce, err := security.EncryptString(h.app.Config.JWTSecret, userYTDLPCookieCryptoPurpose, content)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to encrypt cookie content")
	}

	now := nowString()
	id := newID()
	if _, err := h.app.DB.ExecContext(c.UserContext(), `
INSERT INTO user_ytdlp_cookies (
	id, user_id, label, domain_rule, format, cipher_text, nonce, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		uid,
		label,
		domainRule,
		format,
		cipherText,
		nonce,
		now,
		now,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create yt-dlp cookie")
	}

	return response.OK(c, fiber.Map{
		"id":          id,
		"label":       label,
		"domain_rule": domainRule,
		"format":      format,
		"created_at":  now,
		"updated_at":  now,
	})
}

func (h *Handler) UpdateMyYTDLPCookie(c *fiber.Ctx) error {
	uid := currentUserID(c)
	id := strings.TrimSpace(c.Params("cookieId"))
	if id == "" {
		return response.Error(c, fiber.StatusBadRequest, "cookieId is required")
	}

	var req patchUserYTDLPCookieRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Label == nil && req.DomainRule == nil && req.Format == nil && req.Content == nil {
		return response.Error(c, fiber.StatusBadRequest, "nothing to update")
	}

	record, err := h.getUserYTDLPCookieByID(c.UserContext(), uid, id)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "yt-dlp cookie not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load yt-dlp cookie")
	}

	nextLabel := record.Label
	nextDomainRule := record.DomainRule
	nextFormat := record.Format
	nextCipherText := record.CipherText
	nextNonce := record.Nonce

	if req.Label != nil {
		nextLabel = strings.TrimSpace(*req.Label)
	}
	if req.DomainRule != nil {
		normalizedRule, normErr := normalizeYTDLPCookieDomainRule(*req.DomainRule)
		if normErr != nil {
			return response.Error(c, fiber.StatusBadRequest, normErr.Error())
		}
		nextDomainRule = normalizedRule
	}
	if req.Format != nil {
		nextFormat = strings.ToLower(strings.TrimSpace(*req.Format))
	}
	if req.Format != nil && req.Content == nil {
		return response.Error(c, fiber.StatusBadRequest, "content is required when format is changed")
	}

	nextLabel = strings.TrimSpace(nextLabel)
	if nextLabel == "" {
		return response.Error(c, fiber.StatusBadRequest, "label is required")
	}
	if len([]rune(nextLabel)) > userYTDLPCookieMaxLabelLen {
		return response.Error(c, fiber.StatusBadRequest, "label is too long")
	}
	switch nextFormat {
	case ytdlpCookieFormatHeader, ytdlpCookieFormatCookiesTxt:
	default:
		return response.Error(c, fiber.StatusBadRequest, "format is invalid")
	}

	if req.Content != nil {
		content := strings.TrimSpace(*req.Content)
		if contentErr := validateYTDLPCookieContent(nextFormat, content); contentErr != nil {
			return response.Error(c, fiber.StatusBadRequest, contentErr.Error())
		}
		cipherText, nonce, encErr := security.EncryptString(h.app.Config.JWTSecret, userYTDLPCookieCryptoPurpose, content)
		if encErr != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encrypt cookie content")
		}
		nextCipherText = cipherText
		nextNonce = nonce
	}

	now := nowString()
	res, err := h.app.DB.ExecContext(c.UserContext(), `
UPDATE user_ytdlp_cookies
SET label = ?, domain_rule = ?, format = ?, cipher_text = ?, nonce = ?, updated_at = ?
WHERE id = ? AND user_id = ?`,
		nextLabel,
		nextDomainRule,
		nextFormat,
		nextCipherText,
		nextNonce,
		now,
		id,
		uid,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update yt-dlp cookie")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return response.Error(c, fiber.StatusNotFound, "yt-dlp cookie not found")
	}

	return response.OK(c, fiber.Map{
		"id":          id,
		"label":       nextLabel,
		"domain_rule": nextDomainRule,
		"format":      nextFormat,
		"updated_at":  now,
	})
}

func (h *Handler) DeleteMyYTDLPCookie(c *fiber.Ctx) error {
	uid := currentUserID(c)
	id := strings.TrimSpace(c.Params("cookieId"))
	if id == "" {
		return response.Error(c, fiber.StatusBadRequest, "cookieId is required")
	}

	res, err := h.app.DB.ExecContext(c.UserContext(), `DELETE FROM user_ytdlp_cookies WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to delete yt-dlp cookie")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return response.Error(c, fiber.StatusNotFound, "yt-dlp cookie not found")
	}
	return response.OK(c, fiber.Map{"deleted": true, "id": id})
}

func (h *Handler) getUserYTDLPCookieByID(ctx context.Context, userID, cookieID string) (userYTDLPCookieRecord, error) {
	var row userYTDLPCookieRecord
	err := h.app.DB.QueryRowContext(ctx, `
SELECT id, user_id, label, domain_rule, format, cipher_text, nonce, created_at, updated_at
FROM user_ytdlp_cookies
WHERE id = ? AND user_id = ?
LIMIT 1`,
		cookieID,
		userID,
	).Scan(
		&row.ID,
		&row.UserID,
		&row.Label,
		&row.DomainRule,
		&row.Format,
		&row.CipherText,
		&row.Nonce,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		return userYTDLPCookieRecord{}, err
	}
	return row, nil
}

func (h *Handler) resolveUserYTDLPCookieSelection(ctx context.Context, tx *sql.Tx, userID, sourceURL, cookieID string) (*userYTDLPCookieRecord, error) {
	cookieID = strings.TrimSpace(cookieID)
	if cookieID == "" {
		return nil, nil
	}
	sourceURL = strings.TrimSpace(sourceURL)
	if !isValidImportURL(sourceURL) {
		return nil, fmt.Errorf("url is invalid")
	}
	parsed, err := neturl.Parse(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("url is invalid")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return nil, fmt.Errorf("url is invalid")
	}

	var row userYTDLPCookieRecord
	err = tx.QueryRowContext(ctx, `
SELECT id, user_id, label, domain_rule, format, cipher_text, nonce, created_at, updated_at
FROM user_ytdlp_cookies
WHERE id = ? AND user_id = ?
LIMIT 1`, cookieID, userID).Scan(
		&row.ID,
		&row.UserID,
		&row.Label,
		&row.DomainRule,
		&row.Format,
		&row.CipherText,
		&row.Nonce,
		&row.CreatedAt,
		&row.UpdatedAt,
	)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("user_cookie_id is invalid")
		}
		return nil, fmt.Errorf("failed to load user_cookie_id")
	}
	if !appconfig.IsHostMatchedByDomainList(host, []string{row.DomainRule}) {
		return nil, fmt.Errorf("user_cookie_id does not match current url domain")
	}
	return &row, nil
}

func normalizeUserYTDLPCookieInput(label, domainRule, format, content string) (string, string, string, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", "", "", "", fmt.Errorf("label is required")
	}
	if len([]rune(label)) > userYTDLPCookieMaxLabelLen {
		return "", "", "", "", fmt.Errorf("label is too long")
	}

	normalizedRule, err := normalizeYTDLPCookieDomainRule(domainRule)
	if err != nil {
		return "", "", "", "", err
	}

	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case ytdlpCookieFormatHeader, ytdlpCookieFormatCookiesTxt:
	default:
		return "", "", "", "", fmt.Errorf("format is invalid")
	}

	content = strings.TrimSpace(content)
	if err := validateYTDLPCookieContent(format, content); err != nil {
		return "", "", "", "", err
	}
	return label, normalizedRule, format, content, nil
}

func normalizeYTDLPCookieDomainRule(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", fmt.Errorf("domain_rule is required")
	}
	value = strings.TrimSuffix(value, ".")
	value = strings.TrimPrefix(value, "*.")
	value = strings.TrimPrefix(value, ".")
	if value == "" || strings.Contains(value, "/") {
		return "", fmt.Errorf("domain_rule is invalid")
	}
	if strings.Contains(value, ":") {
		parsed, err := neturl.Parse("http://" + value)
		if err != nil || parsed.Hostname() == "" {
			return "", fmt.Errorf("domain_rule is invalid")
		}
		value = parsed.Hostname()
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("domain_rule is invalid")
	}
	for _, label := range labels {
		if label == "" {
			return "", fmt.Errorf("domain_rule is invalid")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return "", fmt.Errorf("domain_rule is invalid")
		}
	}
	if len(value) > userYTDLPCookieMaxDomainRuleLen {
		return "", fmt.Errorf("domain_rule is too long")
	}
	return value, nil
}

func validateYTDLPCookieContent(format, content string) error {
	switch format {
	case ytdlpCookieFormatHeader:
		if content == "" {
			return fmt.Errorf("content is required")
		}
		if len(content) > userYTDLPCookieMaxHeaderLen {
			return fmt.Errorf("content is too long")
		}
		if strings.Contains(content, "\n") || strings.Contains(content, "\r") {
			return fmt.Errorf("header cookie must not contain line breaks")
		}
		return nil
	case ytdlpCookieFormatCookiesTxt:
		if content == "" {
			return fmt.Errorf("content is required")
		}
		if len(content) > userYTDLPCookieMaxTxtLen {
			return fmt.Errorf("content is too long")
		}
		validLineCount := 0
		lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_") {
				continue
			}
			normalized := strings.TrimPrefix(line, "#HttpOnly_")
			parts := strings.Split(normalized, "\t")
			if len(parts) < 7 {
				return fmt.Errorf("cookies_txt format is invalid")
			}
			validLineCount++
		}
		if validLineCount == 0 {
			return fmt.Errorf("cookies_txt format is invalid")
		}
		return nil
	default:
		return fmt.Errorf("format is invalid")
	}
}

func (h *Handler) prepareRuntimeYTDLPCookieArgs(record *userYTDLPCookieRecord) ([]string, func(), error) {
	if record == nil {
		return nil, func() {}, nil
	}
	content, err := security.DecryptString(h.app.Config.JWTSecret, userYTDLPCookieCryptoPurpose, record.CipherText, record.Nonce)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt user_cookie_id")
	}
	content = strings.TrimSpace(content)
	if err := validateYTDLPCookieContent(record.Format, content); err != nil {
		return nil, nil, err
	}
	switch record.Format {
	case ytdlpCookieFormatHeader:
		return []string{"--add-header", "Cookie: " + content}, func() {}, nil
	case ytdlpCookieFormatCookiesTxt:
		tmpDir, err := os.MkdirTemp(h.app.Config.TaskTempDir, "inspect-cookie-*")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to prepare cookie temp dir")
		}
		cookieFile := filepath.Join(tmpDir, "cookies.txt")
		if err := os.WriteFile(cookieFile, []byte(content), 0600); err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, nil, fmt.Errorf("failed to prepare cookie file")
		}
		return []string{"--cookies", cookieFile}, func() {
			_ = os.RemoveAll(tmpDir)
		}, nil
	default:
		return nil, nil, fmt.Errorf("format is invalid")
	}
}

func hasYTDLPCookieSourceArg(args []string) bool {
	for i := 0; i < len(args); i++ {
		raw := strings.TrimSpace(args[i])
		token := strings.ToLower(raw)
		if token == "" {
			continue
		}
		if token == "--cookies" || token == "--cookies-from-browser" {
			return true
		}
		if strings.HasPrefix(token, "--cookies=") || strings.HasPrefix(token, "--cookies-from-browser=") {
			return true
		}
		if token == "--add-header" || raw == "-H" {
			if i+1 >= len(args) {
				continue
			}
			if headerKeyEquals(args[i+1], "cookie") {
				return true
			}
			continue
		}
		if strings.HasPrefix(token, "--add-header=") {
			if headerKeyEquals(strings.TrimPrefix(token, "--add-header="), "cookie") {
				return true
			}
			continue
		}
	}
	return false
}

func headerKeyEquals(rawHeader, key string) bool {
	sep := strings.Index(rawHeader, ":")
	if sep < 0 {
		return false
	}
	left := strings.ToLower(strings.TrimSpace(rawHeader[:sep]))
	right := strings.ToLower(strings.TrimSpace(key))
	return left != "" && left == right
}
