package handlers

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/response"
	"moevideo/backend/internal/util"
)

const (
	defaultSiteTitle       = "MoeVideo"
	defaultSiteDescription = "MoeVideo VOD - Stitch design implementation"
	captchaCodeLength      = 5
	captchaTTL             = 5 * time.Minute
)

var (
	captchaAlphabet = []rune("23456789ABCDEFGHJKLMNPQRSTUVWXYZ")
	categorySlugRE  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	errCaptchaScene = errors.New("invalid captcha scene")
	errCaptchaInput = errors.New("captcha_id and captcha_code are required")
	errCaptchaCode  = errors.New("invalid or expired captcha")
	errCaptchaCheck = errors.New("failed to verify captcha")
)

type siteSettingsRow struct {
	SiteTitle       string
	SiteDescription string
	SiteLogoMediaID string
	SiteLogoURL     string
	RegisterEnabled bool
	UpdatedBy       string
	UpdatedAt       string
}

type adminPatchSiteSettingsRequest struct {
	SiteTitle       *string `json:"site_title"`
	SiteDescription *string `json:"site_description"`
	SiteLogoMediaID *string `json:"site_logo_media_id"`
	RegisterEnabled *bool   `json:"register_enabled"`
}

type adminCreateCategoryRequest struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	SortOrder *int64 `json:"sort_order"`
	IsActive  *bool  `json:"is_active"`
}

type adminPatchCategoryRequest struct {
	Slug      *string `json:"slug"`
	Name      *string `json:"name"`
	SortOrder *int64  `json:"sort_order"`
	IsActive  *bool   `json:"is_active"`
}

func (h *Handler) ensureSiteSettingsRow(ctx context.Context) error {
	now := nowString()
	_, err := h.app.DB.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO site_settings (
		    id, site_title, site_description, site_logo_media_id, register_enabled, updated_by, created_at, updated_at
		) VALUES (1, ?, ?, NULL, 1, NULL, ?, ?)`,
		defaultSiteTitle,
		defaultSiteDescription,
		now,
		now,
	)
	return err
}

func (h *Handler) querySiteSettingsRow(ctx context.Context) (siteSettingsRow, error) {
	if err := h.ensureSiteSettingsRow(ctx); err != nil {
		return siteSettingsRow{}, err
	}

	var (
		row             siteSettingsRow
		registerEnabled int64
		logoProvider    string
		logoBucket      string
		logoObjectKey   string
	)

	if err := h.app.DB.QueryRowContext(ctx, `
SELECT s.site_title, s.site_description, COALESCE(s.site_logo_media_id, ''), s.register_enabled,
       COALESCE(s.updated_by, ''), s.updated_at,
       COALESCE(m.provider, ''), COALESCE(m.bucket, ''), COALESCE(m.object_key, '')
FROM site_settings s
LEFT JOIN media_objects m ON m.id = s.site_logo_media_id
WHERE s.id = 1
LIMIT 1`).Scan(
		&row.SiteTitle,
		&row.SiteDescription,
		&row.SiteLogoMediaID,
		&registerEnabled,
		&row.UpdatedBy,
		&row.UpdatedAt,
		&logoProvider,
		&logoBucket,
		&logoObjectKey,
	); err != nil {
		return siteSettingsRow{}, err
	}

	row.RegisterEnabled = registerEnabled == 1
	row.SiteLogoURL = mediaURL(h.app.Storage, logoProvider, logoBucket, logoObjectKey)
	return row, nil
}

func (h *Handler) isRegisterEnabled(ctx context.Context) (bool, error) {
	row, err := h.querySiteSettingsRow(ctx)
	if err != nil {
		return false, err
	}
	return row.RegisterEnabled, nil
}

func normalizeCaptchaCode(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func (h *Handler) consumeAuthCaptcha(ctx context.Context, scene, captchaID, captchaCode string) error {
	scene = strings.ToLower(strings.TrimSpace(scene))
	if scene != "login" && scene != "register" {
		return errCaptchaScene
	}

	captchaID = strings.TrimSpace(captchaID)
	captchaCode = normalizeCaptchaCode(captchaCode)
	if captchaID == "" || captchaCode == "" {
		return errCaptchaInput
	}

	now := nowString()
	res, err := h.app.DB.ExecContext(ctx, `
UPDATE auth_captcha_challenges
SET used_at = ?
WHERE id = ?
  AND scene = ?
  AND used_at IS NULL
  AND expires_at > ?
  AND answer_hash = ?`,
		now,
		captchaID,
		scene,
		now,
		util.SHA256Hex(captchaCode),
	)
	if err != nil {
		return errCaptchaCheck
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errCaptchaCode
	}
	return nil
}

func randomInt(max int64) (int64, error) {
	if max <= 0 {
		return 0, fmt.Errorf("max must be positive")
	}
	n, err := crand.Int(crand.Reader, big.NewInt(max))
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

func (h *Handler) buildCaptchaCode() (string, error) {
	builder := strings.Builder{}
	builder.Grow(captchaCodeLength)
	for i := 0; i < captchaCodeLength; i++ {
		idx, err := randomInt(int64(len(captchaAlphabet)))
		if err != nil {
			return "", err
		}
		builder.WriteRune(captchaAlphabet[idx])
	}
	return builder.String(), nil
}

func (h *Handler) buildCaptchaSVG(code string) (string, error) {
	colors := []string{"#0f172a", "#1e293b", "#334155", "#1d4ed8", "#0369a1"}
	var b strings.Builder
	b.Grow(1024)
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="140" height="46" viewBox="0 0 140 46">`)
	b.WriteString(`<rect x="0" y="0" width="140" height="46" rx="10" fill="#f8fafc"/>`)

	for i := 0; i < 8; i++ {
		x1, err := randomInt(140)
		if err != nil {
			return "", err
		}
		y1, err := randomInt(46)
		if err != nil {
			return "", err
		}
		x2, err := randomInt(140)
		if err != nil {
			return "", err
		}
		y2, err := randomInt(46)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#cbd5e1" stroke-width="1"/>`, x1, y1, x2, y2)
	}

	for i, r := range code {
		colorIdx, err := randomInt(int64(len(colors)))
		if err != nil {
			return "", err
		}
		jitterX, err := randomInt(5)
		if err != nil {
			return "", err
		}
		jitterY, err := randomInt(5)
		if err != nil {
			return "", err
		}
		rotation, err := randomInt(31)
		if err != nil {
			return "", err
		}

		x := 16 + i*24 + int(jitterX)
		y := 30 + int(jitterY) - 2
		rot := int(rotation) - 15
		color := colors[int(colorIdx)]
		fmt.Fprintf(
			&b,
			`<text x="%d" y="%d" fill="%s" font-size="24" font-weight="700" font-family="monospace" transform="rotate(%d %d %d)">%c</text>`,
			x,
			y,
			color,
			rot,
			x,
			y,
			r,
		)
	}

	b.WriteString(`</svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String())), nil
}

func (h *Handler) GetAuthCaptcha(c *fiber.Ctx) error {
	scene := strings.ToLower(strings.TrimSpace(c.Query("scene")))
	if scene != "login" && scene != "register" {
		return response.Error(c, fiber.StatusBadRequest, "scene must be login or register")
	}

	captchaCode, err := h.buildCaptchaCode()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to generate captcha")
	}
	imageData, err := h.buildCaptchaSVG(captchaCode)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to generate captcha")
	}

	now := nowUTC()
	expiresAt := now.Add(captchaTTL)
	createdAt := util.FormatTime(now)
	expiresAtStr := util.FormatTime(expiresAt)
	challengeID := newID()

	_, _ = h.app.DB.ExecContext(
		c.UserContext(),
		`DELETE FROM auth_captcha_challenges WHERE expires_at <= ? OR (used_at IS NOT NULL AND used_at <= ?)`,
		createdAt,
		util.FormatTime(now.Add(-24*time.Hour)),
	)

	if _, err := h.app.DB.ExecContext(
		c.UserContext(),
		`INSERT INTO auth_captcha_challenges (id, scene, answer_hash, expires_at, used_at, created_at, created_ip)
		 VALUES (?, ?, ?, ?, NULL, ?, ?)`,
		challengeID,
		scene,
		util.SHA256Hex(normalizeCaptchaCode(captchaCode)),
		expiresAtStr,
		createdAt,
		strings.TrimSpace(c.IP()),
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to issue captcha")
	}

	data := fiber.Map{
		"captcha_id": challengeID,
		"image_data": imageData,
		"expires_at": expiresAtStr,
	}
	if strings.EqualFold(h.app.Config.Env, "test") {
		data["captcha_code"] = captchaCode
	}
	return response.OK(c, data)
}

func (h *Handler) GetPublicSiteSettings(c *fiber.Ctx) error {
	row, err := h.querySiteSettingsRow(c.UserContext())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load site settings")
	}

	return response.OK(c, fiber.Map{
		"site_title":       row.SiteTitle,
		"site_description": row.SiteDescription,
		"site_logo_url":    row.SiteLogoURL,
		"register_enabled": row.RegisterEnabled,
	})
}

func (h *Handler) AdminGetSiteSettings(c *fiber.Ctx) error {
	row, err := h.querySiteSettingsRow(c.UserContext())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load site settings")
	}

	return response.OK(c, fiber.Map{
		"site_title":       row.SiteTitle,
		"site_description": row.SiteDescription,
		"site_logo_media_id": func() interface{} {
			if row.SiteLogoMediaID == "" {
				return nil
			}
			return row.SiteLogoMediaID
		}(),
		"site_logo_url":    row.SiteLogoURL,
		"register_enabled": row.RegisterEnabled,
		"updated_by": func() interface{} {
			if row.UpdatedBy == "" {
				return nil
			}
			return row.UpdatedBy
		}(),
		"updated_at": row.UpdatedAt,
	})
}

func (h *Handler) AdminPatchSiteSettings(c *fiber.Ctx) error {
	var req adminPatchSiteSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.SiteTitle == nil && req.SiteDescription == nil && req.SiteLogoMediaID == nil && req.RegisterEnabled == nil {
		return response.Error(c, fiber.StatusBadRequest, "at least one field is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		c.UserContext(),
		`INSERT OR IGNORE INTO site_settings (
		    id, site_title, site_description, site_logo_media_id, register_enabled, updated_by, created_at, updated_at
		) VALUES (1, ?, ?, NULL, 1, NULL, ?, ?)`,
		defaultSiteTitle,
		defaultSiteDescription,
		nowString(),
		nowString(),
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update site settings")
	}

	var (
		beforeTitle       string
		beforeDescription string
		beforeLogoMediaID sql.NullString
		beforeEnabled     int64
	)
	if err := tx.QueryRowContext(c.UserContext(), `
SELECT site_title, site_description, site_logo_media_id, register_enabled
FROM site_settings
WHERE id = 1
LIMIT 1`).Scan(&beforeTitle, &beforeDescription, &beforeLogoMediaID, &beforeEnabled); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update site settings")
	}

	setClauses := make([]string, 0, 6)
	args := make([]interface{}, 0, 8)

	if req.SiteTitle != nil {
		siteTitle := strings.TrimSpace(*req.SiteTitle)
		if siteTitle == "" {
			return response.Error(c, fiber.StatusBadRequest, "site_title cannot be empty")
		}
		if len([]rune(siteTitle)) > 120 {
			return response.Error(c, fiber.StatusBadRequest, "site_title is too long")
		}
		setClauses = append(setClauses, "site_title = ?")
		args = append(args, siteTitle)
	}

	if req.SiteDescription != nil {
		siteDescription := strings.TrimSpace(*req.SiteDescription)
		if len([]rune(siteDescription)) > 500 {
			return response.Error(c, fiber.StatusBadRequest, "site_description is too long")
		}
		setClauses = append(setClauses, "site_description = ?")
		args = append(args, siteDescription)
	}

	if req.SiteLogoMediaID != nil {
		siteLogoMediaID := strings.TrimSpace(*req.SiteLogoMediaID)
		if siteLogoMediaID == "" {
			setClauses = append(setClauses, "site_logo_media_id = NULL")
		} else {
			var mimeType string
			if err := tx.QueryRowContext(
				c.UserContext(),
				`SELECT mime_type FROM media_objects WHERE id = ? LIMIT 1`,
				siteLogoMediaID,
			).Scan(&mimeType); err != nil {
				if isNotFound(err) {
					return response.Error(c, fiber.StatusBadRequest, "site_logo_media_id is invalid")
				}
				return response.Error(c, fiber.StatusInternalServerError, "failed to validate site logo")
			}
			if _, ok := allowedCoverMIMEs[strings.ToLower(strings.TrimSpace(mimeType))]; !ok {
				return response.Error(c, fiber.StatusBadRequest, "site logo must be image/jpeg, image/png or image/webp")
			}
			setClauses = append(setClauses, "site_logo_media_id = ?")
			args = append(args, siteLogoMediaID)
		}
	}

	if req.RegisterEnabled != nil {
		registerEnabledInt := int64(0)
		if *req.RegisterEnabled {
			registerEnabledInt = 1
		}
		setClauses = append(setClauses, "register_enabled = ?")
		args = append(args, registerEnabledInt)
	}

	setClauses = append(setClauses, "updated_by = ?", "updated_at = ?")
	args = append(args, currentUserID(c), nowString(), 1)

	if _, err := tx.ExecContext(
		c.UserContext(),
		`UPDATE site_settings SET `+strings.Join(setClauses, ", ")+` WHERE id = ?`,
		args...,
	); err != nil {
		if isConflictErr(err) {
			return response.Error(c, fiber.StatusConflict, "site settings conflict")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to update site settings")
	}

	var (
		afterTitle       string
		afterDescription string
		afterLogoMediaID sql.NullString
		afterEnabled     int64
	)
	if err := tx.QueryRowContext(c.UserContext(), `
SELECT site_title, site_description, site_logo_media_id, register_enabled
FROM site_settings
WHERE id = 1
LIMIT 1`).Scan(&afterTitle, &afterDescription, &afterLogoMediaID, &afterEnabled); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update site settings")
	}

	if err := h.writeAdminAudit(c.UserContext(), tx, c, "site_settings.patch", "site_settings", "1", fiber.Map{
		"before": fiber.Map{
			"site_title":       beforeTitle,
			"site_description": beforeDescription,
			"site_logo_media_id": func() interface{} {
				if !beforeLogoMediaID.Valid {
					return nil
				}
				return beforeLogoMediaID.String
			}(),
			"register_enabled": beforeEnabled == 1,
		},
		"after": fiber.Map{
			"site_title":       afterTitle,
			"site_description": afterDescription,
			"site_logo_media_id": func() interface{} {
				if !afterLogoMediaID.Valid {
					return nil
				}
				return afterLogoMediaID.String
			}(),
			"register_enabled": afterEnabled == 1,
		},
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update site settings")
	}

	return h.AdminGetSiteSettings(c)
}

func parseAdminCategoryID(raw string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid category id")
	}
	return parsed, nil
}

func normalizeCategorySlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", fmt.Errorf("slug is required")
	}
	if !categorySlugRE.MatchString(slug) {
		return "", fmt.Errorf("slug must use lowercase letters, numbers and dashes")
	}
	return slug, nil
}

func normalizeCategoryName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if len([]rune(name)) > 32 {
		return "", fmt.Errorf("name is too long")
	}
	return name, nil
}

func (h *Handler) AdminListSiteCategories(c *fiber.Ctx) error {
	rows, err := h.app.DB.QueryContext(c.UserContext(), `
SELECT id, slug, name, sort_order, is_active
FROM categories
ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list categories")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	for rows.Next() {
		var id, sortOrder, isActive int64
		var slug, name string
		if err := rows.Scan(&id, &slug, &name, &sortOrder, &isActive); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse categories")
		}
		items = append(items, fiber.Map{
			"id":         id,
			"slug":       slug,
			"name":       name,
			"sort_order": sortOrder,
			"is_active":  isActive == 1,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list categories")
	}

	return response.OK(c, fiber.Map{"items": items})
}

func (h *Handler) AdminCreateSiteCategory(c *fiber.Ctx) error {
	var req adminCreateCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	slug, err := normalizeCategorySlug(req.Slug)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	name, err := normalizeCategoryName(req.Name)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	sortOrder := int64(0)
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	isActive := int64(1)
	if req.IsActive != nil && !*req.IsActive {
		isActive = 0
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		c.UserContext(),
		`INSERT INTO categories (slug, name, sort_order, is_active) VALUES (?, ?, ?, ?)`,
		slug,
		name,
		sortOrder,
		isActive,
	); err != nil {
		if isConflictErr(err) {
			return response.Error(c, fiber.StatusConflict, "slug or name already exists")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to create category")
	}

	var categoryID int64
	if err := tx.QueryRowContext(c.UserContext(), `SELECT id FROM categories WHERE slug = ? LIMIT 1`, slug).Scan(&categoryID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create category")
	}

	if err := h.writeAdminAudit(c.UserContext(), tx, c, "category.create", "category", strconv.FormatInt(categoryID, 10), fiber.Map{
		"slug":       slug,
		"name":       name,
		"sort_order": sortOrder,
		"is_active":  isActive == 1,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create category")
	}

	return response.Created(c, fiber.Map{
		"id":         categoryID,
		"slug":       slug,
		"name":       name,
		"sort_order": sortOrder,
		"is_active":  isActive == 1,
	})
}

func (h *Handler) AdminPatchSiteCategory(c *fiber.Ctx) error {
	categoryID, err := parseAdminCategoryID(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid category id")
	}

	var req adminPatchCategoryRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Slug == nil && req.Name == nil && req.SortOrder == nil && req.IsActive == nil {
		return response.Error(c, fiber.StatusBadRequest, "at least one field is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var (
		beforeSlug      string
		beforeName      string
		beforeSortOrder int64
		beforeIsActive  int64
	)
	if err := tx.QueryRowContext(
		c.UserContext(),
		`SELECT slug, name, sort_order, is_active FROM categories WHERE id = ? LIMIT 1`,
		categoryID,
	).Scan(&beforeSlug, &beforeName, &beforeSortOrder, &beforeIsActive); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "category not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to update category")
	}

	setClauses := make([]string, 0, 4)
	args := make([]interface{}, 0, 5)

	if req.Slug != nil {
		slug, err := normalizeCategorySlug(*req.Slug)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, err.Error())
		}
		setClauses = append(setClauses, "slug = ?")
		args = append(args, slug)
	}
	if req.Name != nil {
		name, err := normalizeCategoryName(*req.Name)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, err.Error())
		}
		setClauses = append(setClauses, "name = ?")
		args = append(args, name)
	}
	if req.SortOrder != nil {
		setClauses = append(setClauses, "sort_order = ?")
		args = append(args, *req.SortOrder)
	}
	if req.IsActive != nil {
		nextIsActive := int64(0)
		if *req.IsActive {
			nextIsActive = 1
		}
		setClauses = append(setClauses, "is_active = ?")
		args = append(args, nextIsActive)
	}

	args = append(args, categoryID)
	if _, err := tx.ExecContext(
		c.UserContext(),
		`UPDATE categories SET `+strings.Join(setClauses, ", ")+` WHERE id = ?`,
		args...,
	); err != nil {
		if isConflictErr(err) {
			return response.Error(c, fiber.StatusConflict, "slug or name already exists")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to update category")
	}

	var (
		afterSlug      string
		afterName      string
		afterSortOrder int64
		afterIsActive  int64
	)
	if err := tx.QueryRowContext(
		c.UserContext(),
		`SELECT slug, name, sort_order, is_active FROM categories WHERE id = ? LIMIT 1`,
		categoryID,
	).Scan(&afterSlug, &afterName, &afterSortOrder, &afterIsActive); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update category")
	}

	if err := h.writeAdminAudit(c.UserContext(), tx, c, "category.update", "category", strconv.FormatInt(categoryID, 10), fiber.Map{
		"before": fiber.Map{
			"slug":       beforeSlug,
			"name":       beforeName,
			"sort_order": beforeSortOrder,
			"is_active":  beforeIsActive == 1,
		},
		"after": fiber.Map{
			"slug":       afterSlug,
			"name":       afterName,
			"sort_order": afterSortOrder,
			"is_active":  afterIsActive == 1,
		},
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update category")
	}

	return response.OK(c, fiber.Map{
		"id":         categoryID,
		"slug":       afterSlug,
		"name":       afterName,
		"sort_order": afterSortOrder,
		"is_active":  afterIsActive == 1,
	})
}

func (h *Handler) AdminDeleteSiteCategory(c *fiber.Ctx) error {
	categoryID, err := parseAdminCategoryID(c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid category id")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var slug, name string
	if err := tx.QueryRowContext(
		c.UserContext(),
		`SELECT slug, name FROM categories WHERE id = ? LIMIT 1`,
		categoryID,
	).Scan(&slug, &name); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "category not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to delete category")
	}

	var usedByVideos int64
	if err := tx.QueryRowContext(
		c.UserContext(),
		`SELECT COUNT(1) FROM videos WHERE category_id = ?`,
		categoryID,
	).Scan(&usedByVideos); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to delete category")
	}
	if usedByVideos > 0 {
		return response.Error(c, fiber.StatusConflict, "category is in use by videos")
	}

	if _, err := tx.ExecContext(c.UserContext(), `DELETE FROM categories WHERE id = ?`, categoryID); err != nil {
		if isConflictErr(err) {
			return response.Error(c, fiber.StatusConflict, "category is in use by videos")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to delete category")
	}

	if err := h.writeAdminAudit(c.UserContext(), tx, c, "category.delete", "category", strconv.FormatInt(categoryID, 10), fiber.Map{
		"slug": slug,
		"name": name,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to delete category")
	}

	return response.OK(c, fiber.Map{"deleted": true, "id": categoryID})
}
