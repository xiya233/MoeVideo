package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
)

type adminListCursor struct {
	SortAt string `json:"sort_at"`
	ID     string `json:"id"`
}

type adminVideoActionRequest struct {
	Action string `json:"action"`
}

type adminCommentsActionRequest struct {
	Action     string   `json:"action"`
	CommentIDs []string `json:"comment_ids"`
}

type adminPatchUserRequest struct {
	Status *string `json:"status"`
	Role   *string `json:"role"`
}

type adminSetFeaturedBannersRequest struct {
	VideoIDs []string `json:"video_ids"`
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

type adminAuditSchema struct {
	actorCol        string
	resourceTypeCol string
	resourceIDCol   string
	hasIP           bool
	hasUserAgent    bool
}

func (h *Handler) loadAdminAuditSchema(ctx context.Context) (adminAuditSchema, error) {
	schema := adminAuditSchema{
		actorCol:        "admin_user_id",
		resourceTypeCol: "resource_type",
		resourceIDCol:   "resource_id",
		hasIP:           true,
		hasUserAgent:    true,
	}

	rows, err := h.app.DB.QueryContext(ctx, `PRAGMA table_info(admin_audit_logs)`)
	if err != nil {
		return schema, err
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var (
			cid        int64
			name       string
			colType    string
			notNull    int64
			defaultVal sql.NullString
			pk         int64
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return schema, err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return schema, err
	}
	if len(cols) == 0 {
		return schema, nil
	}

	if !cols["admin_user_id"] && cols["actor_user_id"] {
		schema.actorCol = "actor_user_id"
	}
	if !cols["resource_type"] && cols["target_type"] {
		schema.resourceTypeCol = "target_type"
	}
	if !cols["resource_id"] && cols["target_id"] {
		schema.resourceIDCol = "target_id"
	}
	schema.hasIP = cols["ip"]
	schema.hasUserAgent = cols["user_agent"]
	return schema, nil
}

func (h *Handler) RegisterAdminRoutes(admin fiber.Router) {
	admin.Get("/overview", h.AdminOverview)
	admin.Get("/banners/featured", h.AdminGetFeaturedBanners)
	admin.Put("/banners/featured", h.AdminSetFeaturedBanners)
	admin.Get("/site-settings", h.AdminGetSiteSettings)
	admin.Patch("/site-settings", h.AdminPatchSiteSettings)
	admin.Get("/site-settings/categories", h.AdminListSiteCategories)
	admin.Post("/site-settings/categories", h.AdminCreateSiteCategory)
	admin.Patch("/site-settings/categories/:id", h.AdminPatchSiteCategory)
	admin.Delete("/site-settings/categories/:id", h.AdminDeleteSiteCategory)
	admin.Get("/videos", h.AdminListVideos)
	admin.Get("/videos/:id", h.AdminGetVideo)
	admin.Post("/videos/:id/actions", h.AdminVideoAction)
	admin.Get("/transcode-jobs", h.AdminListTranscodeJobs)
	admin.Post("/transcode-jobs/:jobId/retry", h.AdminRetryTranscodeJob)
	admin.Get("/comments", h.AdminListComments)
	admin.Post("/comments/actions", h.AdminCommentsAction)
	admin.Delete("/imports", h.AdminClearFinishedImportJobs)
	admin.Get("/users", h.AdminListUsers)
	admin.Patch("/users/:id", h.AdminPatchUser)
	admin.Get("/audit-logs", h.AdminListAuditLogs)
}

func (h *Handler) queryFeaturedBannerItems(ctx context.Context) ([]fiber.Map, []string, error) {
	rows, err := h.app.DB.QueryContext(ctx, `
SELECT b.position, v.id, v.title,
       COALESCE(v.status, ''), COALESCE(v.visibility, 'public'),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, '')
FROM site_featured_banners b
JOIN videos v ON v.id = b.video_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
ORDER BY b.position ASC`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	items := make([]fiber.Map, 0, 5)
	videoIDs := make([]string, 0, 5)
	for rows.Next() {
		var (
			position                                   int64
			videoID, title, rowStatus, rowVisibility   string
			coverProvider, coverBucket, coverObjectKey string
		)
		if err := rows.Scan(
			&position,
			&videoID,
			&title,
			&rowStatus,
			&rowVisibility,
			&coverProvider,
			&coverBucket,
			&coverObjectKey,
		); err != nil {
			return nil, nil, err
		}
		videoIDs = append(videoIDs, videoID)
		items = append(items, fiber.Map{
			"position": position,
			"video": fiber.Map{
				"id":         videoID,
				"title":      title,
				"status":     rowStatus,
				"visibility": rowVisibility,
				"cover_url":  mediaURL(h.app.Storage, coverProvider, coverBucket, coverObjectKey),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return items, videoIDs, nil
}

func (h *Handler) queryFeaturedBannerIDsTx(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT video_id
FROM site_featured_banners
ORDER BY position ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0, 5)
	for rows.Next() {
		var videoID string
		if err := rows.Scan(&videoID); err != nil {
			return nil, err
		}
		out = append(out, videoID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) AdminGetFeaturedBanners(c *fiber.Ctx) error {
	items, videoIDs, err := h.queryFeaturedBannerItems(c.UserContext())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load featured banners")
	}
	return response.OK(c, fiber.Map{
		"items":     items,
		"video_ids": videoIDs,
	})
}

func (h *Handler) AdminSetFeaturedBanners(c *fiber.Ctx) error {
	var req adminSetFeaturedBannersRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if len(req.VideoIDs) != 5 {
		return response.Error(c, fiber.StatusBadRequest, "video_ids must contain exactly 5 items")
	}

	videoIDs := make([]string, 0, 5)
	seen := make(map[string]struct{}, 5)
	for idx, raw := range req.VideoIDs {
		videoID := strings.TrimSpace(raw)
		if videoID == "" {
			return response.Error(c, fiber.StatusBadRequest, fmt.Sprintf("video_ids[%d] is required", idx))
		}
		if _, exists := seen[videoID]; exists {
			return response.Error(c, fiber.StatusBadRequest, "video_ids contains duplicate values")
		}
		seen[videoID] = struct{}{}
		videoIDs = append(videoIDs, videoID)
	}

	ctx := c.UserContext()
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	beforeIDs, err := h.queryFeaturedBannerIDsTx(ctx, tx)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update featured banners")
	}

	for idx, videoID := range videoIDs {
		var exists int64
		if err := tx.QueryRowContext(
			ctx,
			`SELECT COUNT(1) FROM videos WHERE id = ? AND status = 'published' AND visibility = 'public'`,
			videoID,
		).Scan(&exists); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to validate featured banners")
		}
		if exists == 0 {
			return response.Error(c, fiber.StatusBadRequest, fmt.Sprintf("video_ids[%d] is invalid", idx))
		}
	}

	now := nowString()
	if _, err := tx.ExecContext(ctx, `DELETE FROM site_featured_banners`); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update featured banners")
	}
	for idx, videoID := range videoIDs {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO site_featured_banners (position, video_id, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			idx+1,
			videoID,
			now,
			now,
		); err != nil {
			if isConflictErr(err) {
				return response.Error(c, fiber.StatusConflict, "featured banners conflict")
			}
			return response.Error(c, fiber.StatusInternalServerError, "failed to update featured banners")
		}
	}

	if err := h.writeAdminAudit(ctx, tx, c, "site_banners.patch", "site_banners", "featured", fiber.Map{
		"before_video_ids": beforeIDs,
		"after_video_ids":  videoIDs,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update featured banners")
	}

	return h.AdminGetFeaturedBanners(c)
}

func (h *Handler) AdminOverview(c *fiber.Ctx) error {
	ctx := c.UserContext()
	auditSchema, err := h.loadAdminAuditSchema(ctx)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}

	var videosTotal, videosProcessing, transcodeFailed, usersTotal, usersActive int64
	var uploadsToday, usersToday int64
	today := nowUTC().Format("2006-01-02")

	if err := h.app.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM videos WHERE status != 'deleted'`).Scan(&videosTotal); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM videos WHERE status = 'processing'`).Scan(&videosProcessing); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM video_transcode_jobs WHERE status = 'failed'`).Scan(&transcodeFailed); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&usersTotal); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM users WHERE status = 'active'`).Scan(&usersActive); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM videos WHERE substr(created_at, 1, 10) = ?`, today).Scan(&uploadsToday); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	if err := h.app.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM users WHERE substr(created_at, 1, 10) = ?`, today).Scan(&usersToday); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}

	failedItems := make([]fiber.Map, 0)
	failedRows, err := h.app.DB.QueryContext(ctx, `
SELECT id, video_id, COALESCE(last_error, ''), updated_at
FROM video_transcode_jobs
WHERE status = 'failed'
ORDER BY updated_at DESC, id DESC
LIMIT 5`)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	defer failedRows.Close()
	for failedRows.Next() {
		var id, videoID, lastError, updatedAt string
		if err := failedRows.Scan(&id, &videoID, &lastError, &updatedAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse overview")
		}
		failedItems = append(failedItems, fiber.Map{
			"id":         id,
			"video_id":   videoID,
			"last_error": lastError,
			"updated_at": updatedAt,
		})
	}

	actionItems := make([]fiber.Map, 0)
	actionQuery := fmt.Sprintf(`
SELECT l.id, l.action, l.%s, l.%s, l.created_at, u.id, u.username
FROM admin_audit_logs l
JOIN users u ON u.id = l.%s
ORDER BY l.created_at DESC, l.id DESC
LIMIT 10`, auditSchema.resourceTypeCol, auditSchema.resourceIDCol, auditSchema.actorCol)
	actionRows, err := h.app.DB.QueryContext(ctx, actionQuery)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load overview")
	}
	defer actionRows.Close()
	for actionRows.Next() {
		var id, action, resourceType, resourceID, createdAt, actorID, actorName string
		if err := actionRows.Scan(&id, &action, &resourceType, &resourceID, &createdAt, &actorID, &actorName); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse overview")
		}
		actionItems = append(actionItems, fiber.Map{
			"id":            id,
			"action":        action,
			"resource_type": resourceType,
			"resource_id":   resourceID,
			"created_at":    createdAt,
			"actor": fiber.Map{
				"id":       actorID,
				"username": actorName,
			},
		})
	}

	return response.OK(c, fiber.Map{
		"metrics": fiber.Map{
			"videos_total":      videosTotal,
			"videos_processing": videosProcessing,
			"transcode_failed":  transcodeFailed,
			"users_total":       usersTotal,
			"users_active":      usersActive,
			"uploads_today":     uploadsToday,
			"users_today":       usersToday,
		},
		"recent_failed_jobs": failedItems,
		"recent_actions":     actionItems,
	})
}

func (h *Handler) AdminClearFinishedImportJobs(c *fiber.Ctx) error {
	ctx := c.UserContext()
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to clear import jobs")
	}
	defer tx.Rollback()

	var beforeCount int64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(1) FROM video_import_jobs WHERE status IN ('succeeded', 'partial', 'failed')`,
	).Scan(&beforeCount); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to clear import jobs")
	}

	res, err := tx.ExecContext(
		ctx,
		`DELETE FROM video_import_jobs WHERE status IN ('succeeded', 'partial', 'failed')`,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to clear import jobs")
	}
	deleted, _ := res.RowsAffected()

	if err := h.writeAdminAudit(ctx, tx, c, "imports.clear_all_finished", "import_job", "all_finished", fiber.Map{
		"before_count": beforeCount,
		"deleted":      deleted,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to clear import jobs")
	}

	return response.OK(c, fiber.Map{"deleted": deleted})
}

func (h *Handler) AdminListVideos(c *fiber.Ctx) error {
	ctx := c.UserContext()
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursorRaw := strings.TrimSpace(c.Query("cursor"))

	q := strings.TrimSpace(c.Query("q"))
	status := strings.TrimSpace(c.Query("status"))
	visibility := strings.TrimSpace(c.Query("visibility"))
	uploaderID := strings.TrimSpace(c.Query("uploader_id"))
	categoryID := strings.TrimSpace(c.Query("category_id"))

	query := `
SELECT v.id, v.title, v.status, v.visibility, COALESCE(v.published_at, ''), v.created_at, v.updated_at,
       v.duration_sec, v.views_count, v.comments_count, v.likes_count, v.favorites_count, v.shares_count,
       u.id, u.username,
       COALESCE(c.id, 0), COALESCE(c.slug, ''), COALESCE(c.name, '')
FROM videos v
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories c ON c.id = v.category_id
WHERE 1=1`
	args := make([]interface{}, 0)

	if q != "" {
		kw := "%" + q + "%"
		query += ` AND (v.title LIKE ? OR v.description LIKE ?)`
		args = append(args, kw, kw)
	}
	if status != "" {
		query += ` AND v.status = ?`
		args = append(args, status)
	}
	if visibility != "" {
		query += ` AND v.visibility = ?`
		args = append(args, visibility)
	}
	if uploaderID != "" {
		query += ` AND v.uploader_id = ?`
		args = append(args, uploaderID)
	}
	if categoryID != "" {
		parsed, err := strconv.ParseInt(categoryID, 10, 64)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid category_id")
		}
		query += ` AND v.category_id = ?`
		args = append(args, parsed)
	}

	if cursorRaw != "" {
		var cur adminListCursor
		if err := pagination.Decode(cursorRaw, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (v.updated_at < ? OR (v.updated_at = ? AND v.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}

	query += ` ORDER BY v.updated_at DESC, v.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list videos")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	for rows.Next() {
		var (
			id, title, rowStatus, rowVisibility, publishedAt, createdAt, updatedAt string
			durationSec, viewsCount, commentsCount, likesCount                     int64
			favoritesCount, sharesCount                                            int64
			authorID, authorName                                                   string
			catID                                                                  int64
			catSlug, catName                                                       string
		)
		if err := rows.Scan(
			&id,
			&title,
			&rowStatus,
			&rowVisibility,
			&publishedAt,
			&createdAt,
			&updatedAt,
			&durationSec,
			&viewsCount,
			&commentsCount,
			&likesCount,
			&favoritesCount,
			&sharesCount,
			&authorID,
			&authorName,
			&catID,
			&catSlug,
			&catName,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse videos")
		}
		item := fiber.Map{
			"id":              id,
			"title":           title,
			"status":          rowStatus,
			"visibility":      rowVisibility,
			"published_at":    publishedAt,
			"created_at":      createdAt,
			"updated_at":      updatedAt,
			"duration_sec":    durationSec,
			"views_count":     viewsCount,
			"comments_count":  commentsCount,
			"likes_count":     likesCount,
			"favorites_count": favoritesCount,
			"shares_count":    sharesCount,
			"uploader": fiber.Map{
				"id":       authorID,
				"username": authorName,
			},
		}
		if catID > 0 {
			item["category"] = fiber.Map{
				"id":   catID,
				"slug": catSlug,
				"name": catName,
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list videos")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(adminListCursor{SortAt: last["updated_at"].(string), ID: last["id"].(string)})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) AdminGetVideo(c *fiber.Ctx) error {
	ctx := c.UserContext()
	videoID := strings.TrimSpace(c.Params("id"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "id is required")
	}

	query := `
SELECT v.id, v.title, v.description, v.status, v.visibility,
       v.duration_sec, v.views_count, v.likes_count, v.favorites_count, v.comments_count, v.shares_count,
       COALESCE(v.published_at, ''), v.created_at, v.updated_at,
       COALESCE(cat.id, 0), COALESCE(cat.slug, ''), COALESCE(cat.name, ''),
       u.id, u.username, COALESCE(u.role, 'user'), u.status,
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       COALESCE(pm.provider, ''), COALESCE(pm.bucket, ''), COALESCE(pm.object_key, ''),
       COALESCE(sm.provider, ''), COALESCE(sm.bucket, ''), COALESCE(sm.object_key, ''),
       COALESCE(hls.provider, ''), COALESCE(hls.bucket, ''), COALESCE(hls.master_object_key, ''), COALESCE(hls.variants_json, '')
FROM videos v
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories cat ON cat.id = v.category_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects pm ON pm.id = v.preview_media_id
LEFT JOIN media_objects sm ON sm.id = v.source_media_id
LEFT JOIN video_hls_assets hls ON hls.video_id = v.id
WHERE v.id = ?
LIMIT 1`

	var (
		id, title, description, rowStatus, visibility          string
		durationSec, views, likes, favorites, comments, shares int64
		publishedAt, createdAt, updatedAt                      string
		catID                                                  int64
		catSlug, catName                                       string
		uploaderID, uploaderName, uploaderRole, uploaderStatus string
		coverProvider, coverBucket, coverKey                   string
		previewProvider, previewBucket, previewKey             string
		sourceProvider, sourceBucket, sourceKey                string
		hlsProvider, hlsBucket, hlsMasterKey, hlsVariantsJSON  string
	)

	err := h.app.DB.QueryRowContext(ctx, query, videoID).Scan(
		&id,
		&title,
		&description,
		&rowStatus,
		&visibility,
		&durationSec,
		&views,
		&likes,
		&favorites,
		&comments,
		&shares,
		&publishedAt,
		&createdAt,
		&updatedAt,
		&catID,
		&catSlug,
		&catName,
		&uploaderID,
		&uploaderName,
		&uploaderRole,
		&uploaderStatus,
		&coverProvider,
		&coverBucket,
		&coverKey,
		&previewProvider,
		&previewBucket,
		&previewKey,
		&sourceProvider,
		&sourceBucket,
		&sourceKey,
		&hlsProvider,
		&hlsBucket,
		&hlsMasterKey,
		&hlsVariantsJSON,
	)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to get video")
	}

	tagRows, err := h.app.DB.QueryContext(ctx, `
SELECT t.name
FROM video_tags vt
JOIN tags t ON t.id = vt.tag_id
WHERE vt.video_id = ?
ORDER BY t.name ASC`, videoID)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to get video")
	}
	defer tagRows.Close()
	tags := make([]string, 0)
	for tagRows.Next() {
		var tag string
		if err := tagRows.Scan(&tag); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to get video")
		}
		tags = append(tags, tag)
	}

	var jobID, jobStatus, jobLastError, jobAvailableAt, jobUpdatedAt string
	var attempts, maxAttempts int64
	job := fiber.Map{}
	if err := h.app.DB.QueryRowContext(ctx, `
SELECT id, status, attempts, max_attempts, COALESCE(last_error, ''), available_at, updated_at
FROM video_transcode_jobs
WHERE video_id = ?
LIMIT 1`, videoID).Scan(&jobID, &jobStatus, &attempts, &maxAttempts, &jobLastError, &jobAvailableAt, &jobUpdatedAt); err == nil {
		job = fiber.Map{
			"id":           jobID,
			"status":       jobStatus,
			"attempts":     attempts,
			"max_attempts": maxAttempts,
			"last_error":   jobLastError,
			"available_at": jobAvailableAt,
			"updated_at":   jobUpdatedAt,
		}
	}

	result := fiber.Map{
		"id":              id,
		"title":           title,
		"description":     description,
		"status":          rowStatus,
		"visibility":      visibility,
		"duration_sec":    durationSec,
		"views_count":     views,
		"likes_count":     likes,
		"favorites_count": favorites,
		"comments_count":  comments,
		"shares_count":    shares,
		"published_at":    publishedAt,
		"created_at":      createdAt,
		"updated_at":      updatedAt,
		"tags":            tags,
		"uploader": fiber.Map{
			"id":       uploaderID,
			"username": uploaderName,
			"role":     uploaderRole,
			"status":   uploaderStatus,
		},
		"media": fiber.Map{
			"cover_url":        mediaURL(h.app.Storage, coverProvider, coverBucket, coverKey),
			"preview_webp_url": mediaURL(h.app.Storage, previewProvider, previewBucket, previewKey),
			"source_url":       mediaURL(h.app.Storage, sourceProvider, sourceBucket, sourceKey),
		},
		"transcode_job": job,
	}
	if catID > 0 {
		result["category"] = fiber.Map{"id": catID, "slug": catSlug, "name": catName}
	}
	if hlsMasterKey != "" {
		result["hls"] = fiber.Map{
			"provider":       hlsProvider,
			"bucket":         hlsBucket,
			"master_url":     mediaURL(h.app.Storage, hlsProvider, hlsBucket, hlsMasterKey),
			"variants_json":  hlsVariantsJSON,
			"master_obj_key": hlsMasterKey,
		}
	}

	return response.OK(c, result)
}

func (h *Handler) AdminVideoAction(c *fiber.Ctx) error {
	ctx := c.UserContext()
	videoID := strings.TrimSpace(c.Params("id"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "id is required")
	}

	var req adminVideoActionRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	action := strings.TrimSpace(req.Action)
	switch action {
	case "publish", "hide", "soft_delete", "restore", "retry_transcode":
	default:
		return response.Error(c, fiber.StatusBadRequest, "invalid action")
	}

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var currentStatus, currentVisibility string
	if err := tx.QueryRowContext(ctx,
		`SELECT status, visibility FROM videos WHERE id = ? LIMIT 1`,
		videoID,
	).Scan(&currentStatus, &currentVisibility); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to update video")
	}

	now := nowString()
	switch action {
	case "publish":
		if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET status = 'published',
    visibility = 'public',
    published_at = CASE WHEN published_at IS NULL OR published_at = '' THEN ? ELSE published_at END,
    updated_at = ?
WHERE id = ?`, now, now, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update video")
		}
	case "hide":
		if _, err := tx.ExecContext(ctx, `UPDATE videos SET visibility = 'private', updated_at = ? WHERE id = ?`, now, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update video")
		}
	case "soft_delete":
		if _, err := tx.ExecContext(ctx, `UPDATE videos SET status = 'deleted', updated_at = ? WHERE id = ?`, now, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update video")
		}
	case "restore":
		if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET status = 'published',
    published_at = CASE WHEN published_at IS NULL OR published_at = '' THEN ? ELSE published_at END,
    updated_at = ?
WHERE id = ?`, now, now, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update video")
		}
	case "retry_transcode":
		if currentStatus == "deleted" {
			return response.Error(c, fiber.StatusBadRequest, "cannot retry transcode for deleted video")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE videos SET status = 'processing', updated_at = ? WHERE id = ?`, now, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to queue transcode")
		}
		maxAttempts := h.app.Config.TranscodeMaxTry
		if maxAttempts <= 0 {
			maxAttempts = 3
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO video_transcode_jobs (
    id, video_id, status, attempts, max_attempts, last_error, available_at,
    locked_at, started_at, finished_at, created_at, updated_at
) VALUES (?, ?, 'queued', 0, ?, NULL, ?, NULL, NULL, NULL, ?, ?)
ON CONFLICT(video_id) DO UPDATE SET
    status = 'queued',
    attempts = 0,
    max_attempts = excluded.max_attempts,
    last_error = NULL,
    available_at = excluded.available_at,
    locked_at = NULL,
    started_at = NULL,
    finished_at = NULL,
    updated_at = excluded.updated_at`,
			newID(),
			videoID,
			maxAttempts,
			now,
			now,
			now,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to queue transcode")
		}
	}

	if err := h.writeAdminAudit(ctx, tx, c, "video."+action, "video", videoID, fiber.Map{
		"action":            action,
		"before_status":     currentStatus,
		"before_visibility": currentVisibility,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit action")
	}
	return response.OK(c, fiber.Map{"applied": true, "action": action})
}

func (h *Handler) AdminListTranscodeJobs(c *fiber.Ctx) error {
	ctx := c.UserContext()
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursorRaw := strings.TrimSpace(c.Query("cursor"))
	status := strings.TrimSpace(c.Query("status"))
	videoID := strings.TrimSpace(c.Query("video_id"))

	query := `
SELECT id, video_id, status, attempts, max_attempts, COALESCE(last_error, ''),
       available_at, COALESCE(locked_at, ''), COALESCE(started_at, ''), COALESCE(finished_at, ''),
       created_at, updated_at
FROM video_transcode_jobs
WHERE 1=1`
	args := make([]interface{}, 0)
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if videoID != "" {
		query += ` AND video_id = ?`
		args = append(args, videoID)
	}
	if cursorRaw != "" {
		var cur adminListCursor
		if err := pagination.Decode(cursorRaw, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list transcode jobs")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	for rows.Next() {
		var id, rowVideoID, rowStatus, lastError, availableAt, lockedAt, startedAt, finishedAt, createdAt, updatedAt string
		var attempts, maxAttempts int64
		if err := rows.Scan(&id, &rowVideoID, &rowStatus, &attempts, &maxAttempts, &lastError, &availableAt, &lockedAt, &startedAt, &finishedAt, &createdAt, &updatedAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse transcode jobs")
		}
		items = append(items, fiber.Map{
			"id":           id,
			"video_id":     rowVideoID,
			"status":       rowStatus,
			"attempts":     attempts,
			"max_attempts": maxAttempts,
			"last_error":   lastError,
			"available_at": availableAt,
			"locked_at":    lockedAt,
			"started_at":   startedAt,
			"finished_at":  finishedAt,
			"created_at":   createdAt,
			"updated_at":   updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list transcode jobs")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(adminListCursor{SortAt: last["updated_at"].(string), ID: last["id"].(string)})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) AdminRetryTranscodeJob(c *fiber.Ctx) error {
	ctx := c.UserContext()
	jobID := strings.TrimSpace(c.Params("jobId"))
	if jobID == "" {
		return response.Error(c, fiber.StatusBadRequest, "jobId is required")
	}

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var videoID, currentStatus string
	if err := tx.QueryRowContext(ctx,
		`SELECT video_id, status FROM video_transcode_jobs WHERE id = ? LIMIT 1`,
		jobID,
	).Scan(&videoID, &currentStatus); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "transcode job not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to retry transcode job")
	}

	now := nowString()
	maxAttempts := h.app.Config.TranscodeMaxTry
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE video_transcode_jobs
SET status = 'queued',
    attempts = 0,
    max_attempts = ?,
    last_error = NULL,
    available_at = ?,
    locked_at = NULL,
    started_at = NULL,
    finished_at = NULL,
    updated_at = ?
WHERE id = ?`, maxAttempts, now, now, jobID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to retry transcode job")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE videos SET status = CASE WHEN status = 'deleted' THEN status ELSE 'processing' END, updated_at = ? WHERE id = ?`,
		now,
		videoID,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update video status")
	}

	if err := h.writeAdminAudit(ctx, tx, c, "transcode.retry", "transcode_job", jobID, fiber.Map{
		"video_id":      videoID,
		"before_status": currentStatus,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to retry transcode job")
	}

	return response.OK(c, fiber.Map{"queued": true, "job_id": jobID, "video_id": videoID})
}

func (h *Handler) AdminListComments(c *fiber.Ctx) error {
	ctx := c.UserContext()
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursorRaw := strings.TrimSpace(c.Query("cursor"))
	q := strings.TrimSpace(c.Query("q"))
	videoID := strings.TrimSpace(c.Query("video_id"))
	userID := strings.TrimSpace(c.Query("user_id"))
	status := strings.TrimSpace(c.Query("status"))

	query := `
SELECT cm.id, cm.video_id, COALESCE(v.title, ''), cm.user_id, COALESCE(u.username, ''),
       COALESCE(cm.parent_comment_id, ''), cm.content, cm.status, cm.like_count, cm.reply_count,
       cm.created_at, cm.updated_at
FROM comments cm
LEFT JOIN users u ON u.id = cm.user_id
LEFT JOIN videos v ON v.id = cm.video_id
WHERE 1=1`
	args := make([]interface{}, 0)

	if q != "" {
		kw := "%" + q + "%"
		query += ` AND cm.content LIKE ?`
		args = append(args, kw)
	}
	if videoID != "" {
		query += ` AND cm.video_id = ?`
		args = append(args, videoID)
	}
	if userID != "" {
		query += ` AND cm.user_id = ?`
		args = append(args, userID)
	}
	if status != "" {
		query += ` AND cm.status = ?`
		args = append(args, status)
	}
	if cursorRaw != "" {
		var cur adminListCursor
		if err := pagination.Decode(cursorRaw, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (cm.created_at < ? OR (cm.created_at = ? AND cm.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}

	query += ` ORDER BY cm.created_at DESC, cm.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list comments")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	for rows.Next() {
		var id, rowVideoID, videoTitle, rowUserID, username, parentID, content, rowStatus, createdAt, updatedAt string
		var likeCount, replyCount int64
		if err := rows.Scan(&id, &rowVideoID, &videoTitle, &rowUserID, &username, &parentID, &content, &rowStatus, &likeCount, &replyCount, &createdAt, &updatedAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse comments")
		}
		item := fiber.Map{
			"id":                id,
			"video_id":          rowVideoID,
			"video_title":       videoTitle,
			"user_id":           rowUserID,
			"username":          username,
			"content":           content,
			"status":            rowStatus,
			"like_count":        likeCount,
			"reply_count":       replyCount,
			"created_at":        createdAt,
			"updated_at":        updatedAt,
			"parent_comment_id": nil,
		}
		if parentID != "" {
			item["parent_comment_id"] = parentID
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list comments")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(adminListCursor{SortAt: last["created_at"].(string), ID: last["id"].(string)})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) AdminCommentsAction(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var req adminCommentsActionRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	action := strings.TrimSpace(req.Action)
	if action != "delete" && action != "restore" {
		return response.Error(c, fiber.StatusBadRequest, "invalid action")
	}

	ids := make([]string, 0, len(req.CommentIDs))
	seen := map[string]struct{}{}
	for _, raw := range req.CommentIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "comment_ids is required")
	}

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	now := nowString()
	affected := int64(0)
	videoDelta := map[string]int64{}
	parentDelta := map[string]int64{}

	for _, commentID := range ids {
		var rowVideoID, rowStatus string
		var parentID sql.NullString
		if err := tx.QueryRowContext(ctx,
			`SELECT video_id, parent_comment_id, status FROM comments WHERE id = ? LIMIT 1`,
			commentID,
		).Scan(&rowVideoID, &parentID, &rowStatus); err != nil {
			if isNotFound(err) {
				continue
			}
			return response.Error(c, fiber.StatusInternalServerError, "failed to update comments")
		}

		delta := int64(0)
		if action == "delete" && rowStatus == "active" {
			if _, err := tx.ExecContext(ctx, `UPDATE comments SET status = 'deleted', updated_at = ? WHERE id = ?`, now, commentID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update comments")
			}
			delta = -1
			affected++
		}
		if action == "restore" && rowStatus == "deleted" {
			if _, err := tx.ExecContext(ctx, `UPDATE comments SET status = 'active', updated_at = ? WHERE id = ?`, now, commentID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update comments")
			}
			delta = 1
			affected++
		}

		if delta != 0 {
			videoDelta[rowVideoID] += delta
			if parentID.Valid {
				parentDelta[parentID.String] += delta
			}
		}
	}

	for videoID, delta := range videoDelta {
		if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET comments_count = CASE WHEN comments_count + ? < 0 THEN 0 ELSE comments_count + ? END,
    updated_at = ?
WHERE id = ?`, delta, delta, now, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update comments")
		}
		if err := h.recomputeHotScoreTx(ctx, tx, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update comments")
		}
	}
	for parentID, delta := range parentDelta {
		if _, err := tx.ExecContext(ctx, `
UPDATE comments
SET reply_count = CASE WHEN reply_count + ? < 0 THEN 0 ELSE reply_count + ? END,
    updated_at = ?
WHERE id = ?`, delta, delta, now, parentID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update comments")
		}
	}

	if err := h.writeAdminAudit(ctx, tx, c, "comment."+action, "comment", "batch", fiber.Map{
		"action":      action,
		"affected":    affected,
		"comment_ids": ids,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update comments")
	}

	return response.OK(c, fiber.Map{"action": action, "affected": affected})
}

func (h *Handler) AdminListUsers(c *fiber.Ctx) error {
	ctx := c.UserContext()
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursorRaw := strings.TrimSpace(c.Query("cursor"))
	q := strings.TrimSpace(c.Query("q"))
	status := strings.TrimSpace(c.Query("status"))
	role := strings.TrimSpace(c.Query("role"))

	query := `
SELECT u.id, u.username, u.email, COALESCE(u.role, 'user'), u.status,
       u.followers_count, u.following_count, u.created_at, u.updated_at,
       COALESCE((SELECT COUNT(1) FROM videos v WHERE v.uploader_id = u.id AND v.status != 'deleted'), 0)
FROM users u
WHERE 1=1`
	args := make([]interface{}, 0)

	if q != "" {
		kw := "%" + q + "%"
		query += ` AND (u.username LIKE ? OR u.email LIKE ?)`
		args = append(args, kw, kw)
	}
	if status != "" {
		query += ` AND u.status = ?`
		args = append(args, status)
	}
	if role != "" {
		query += ` AND COALESCE(u.role, 'user') = ?`
		args = append(args, role)
	}
	if cursorRaw != "" {
		var cur adminListCursor
		if err := pagination.Decode(cursorRaw, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (u.created_at < ? OR (u.created_at = ? AND u.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY u.created_at DESC, u.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list users")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	for rows.Next() {
		var id, username, email, rowRole, rowStatus, createdAt, updatedAt string
		var followersCount, followingCount, videosCount int64
		if err := rows.Scan(&id, &username, &email, &rowRole, &rowStatus, &followersCount, &followingCount, &createdAt, &updatedAt, &videosCount); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse users")
		}
		items = append(items, fiber.Map{
			"id":              id,
			"username":        username,
			"email":           email,
			"role":            rowRole,
			"status":          rowStatus,
			"followers_count": followersCount,
			"following_count": followingCount,
			"videos_count":    videosCount,
			"created_at":      createdAt,
			"updated_at":      updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list users")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(adminListCursor{SortAt: last["created_at"].(string), ID: last["id"].(string)})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) AdminPatchUser(c *fiber.Ctx) error {
	ctx := c.UserContext()
	userID := strings.TrimSpace(c.Params("id"))
	if userID == "" {
		return response.Error(c, fiber.StatusBadRequest, "id is required")
	}

	var req adminPatchUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Status == nil && req.Role == nil {
		return response.Error(c, fiber.StatusBadRequest, "status or role is required")
	}

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var prevRole, prevStatus string
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(role, 'user'), status FROM users WHERE id = ? LIMIT 1`,
		userID,
	).Scan(&prevRole, &prevStatus); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "user not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to update user")
	}

	nextRole := prevRole
	nextStatus := prevStatus
	if req.Role != nil {
		candidate := strings.TrimSpace(*req.Role)
		if candidate != "user" && candidate != "admin" {
			return response.Error(c, fiber.StatusBadRequest, "invalid role")
		}
		nextRole = candidate
	}
	if req.Status != nil {
		candidate := strings.TrimSpace(*req.Status)
		if candidate != "active" && candidate != "disabled" {
			return response.Error(c, fiber.StatusBadRequest, "invalid status")
		}
		nextStatus = candidate
	}

	if prevRole == "admin" && nextRole != "admin" {
		var adminCount int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM users WHERE COALESCE(role, 'user') = 'admin'`).Scan(&adminCount); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update user")
		}
		if adminCount <= 1 {
			return response.Error(c, fiber.StatusBadRequest, "at least one admin must remain")
		}
	}

	now := nowString()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET role = ?, status = ?, updated_at = ? WHERE id = ?`,
		nextRole,
		nextStatus,
		now,
		userID,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update user")
	}

	if err := h.writeAdminAudit(ctx, tx, c, "user.patch", "user", userID, fiber.Map{
		"before_role":   prevRole,
		"before_status": prevStatus,
		"after_role":    nextRole,
		"after_status":  nextStatus,
	}); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write audit log")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update user")
	}

	return response.OK(c, fiber.Map{"id": userID, "role": nextRole, "status": nextStatus})
}

func (h *Handler) AdminListAuditLogs(c *fiber.Ctx) error {
	ctx := c.UserContext()
	auditSchema, err := h.loadAdminAuditSchema(ctx)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list audit logs")
	}
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursorRaw := strings.TrimSpace(c.Query("cursor"))
	actorID := strings.TrimSpace(c.Query("actor_id"))
	action := strings.TrimSpace(c.Query("action"))
	resourceType := strings.TrimSpace(c.Query("resource_type"))
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))

	ipSelect := "''"
	if auditSchema.hasIP {
		ipSelect = "COALESCE(l.ip, '')"
	}
	userAgentSelect := "''"
	if auditSchema.hasUserAgent {
		userAgentSelect = "COALESCE(l.user_agent, '')"
	}

	query := fmt.Sprintf(`
SELECT l.id, l.%s, u.username, l.action, l.%s, l.%s,
       l.payload_json, %s, %s, l.created_at
FROM admin_audit_logs l
JOIN users u ON u.id = l.%s
WHERE 1=1`,
		auditSchema.actorCol,
		auditSchema.resourceTypeCol,
		auditSchema.resourceIDCol,
		ipSelect,
		userAgentSelect,
		auditSchema.actorCol,
	)
	args := make([]interface{}, 0)

	if actorID != "" {
		query += fmt.Sprintf(` AND l.%s = ?`, auditSchema.actorCol)
		args = append(args, actorID)
	}
	if action != "" {
		query += ` AND l.action = ?`
		args = append(args, action)
	}
	if resourceType != "" {
		query += fmt.Sprintf(` AND l.%s = ?`, auditSchema.resourceTypeCol)
		args = append(args, resourceType)
	}
	if from != "" {
		query += ` AND l.created_at >= ?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND l.created_at <= ?`
		args = append(args, to)
	}
	if cursorRaw != "" {
		var cur adminListCursor
		if err := pagination.Decode(cursorRaw, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (l.created_at < ? OR (l.created_at = ? AND l.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY l.created_at DESC, l.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list audit logs")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	for rows.Next() {
		var id, rowActorID, actorName, rowAction, rowResourceType, rowResourceID, payloadJSON, ip, userAgent, createdAt string
		if err := rows.Scan(&id, &rowActorID, &actorName, &rowAction, &rowResourceType, &rowResourceID, &payloadJSON, &ip, &userAgent, &createdAt); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse audit logs")
		}

		var payload interface{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			payload = payloadJSON
		}

		items = append(items, fiber.Map{
			"id":            id,
			"action":        rowAction,
			"resource_type": rowResourceType,
			"resource_id":   rowResourceID,
			"payload":       payload,
			"ip":            ip,
			"user_agent":    userAgent,
			"created_at":    createdAt,
			"actor": fiber.Map{
				"id":       rowActorID,
				"username": actorName,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list audit logs")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(adminListCursor{SortAt: last["created_at"].(string), ID: last["id"].(string)})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) writeAdminAudit(ctx context.Context, exec sqlExecutor, c *fiber.Ctx, action, resourceType, resourceID string, payload interface{}) error {
	if strings.TrimSpace(action) == "" {
		action = "unknown"
	}
	if strings.TrimSpace(resourceType) == "" {
		resourceType = "unknown"
	}
	if strings.TrimSpace(resourceID) == "" {
		resourceID = "-"
	}

	payloadJSON := "{}"
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal audit payload: %w", err)
		}
		payloadJSON = string(b)
	}

	auditSchema, err := h.loadAdminAuditSchema(ctx)
	if err != nil {
		return fmt.Errorf("load audit schema: %w", err)
	}

	cols := []string{"id", auditSchema.actorCol, "action", auditSchema.resourceTypeCol, auditSchema.resourceIDCol, "payload_json", "created_at"}
	args := []interface{}{
		newID(),
		currentUserID(c),
		action,
		resourceType,
		resourceID,
		payloadJSON,
		nowString(),
	}
	if auditSchema.hasIP {
		cols = append(cols, "ip")
		args = append(args, strings.TrimSpace(c.IP()))
	}
	if auditSchema.hasUserAgent {
		cols = append(cols, "user_agent")
		args = append(args, strings.TrimSpace(c.Get("User-Agent")))
	}
	placeholder := strings.TrimRight(strings.Repeat("?,", len(cols)), ",")
	query := fmt.Sprintf(`INSERT INTO admin_audit_logs (%s) VALUES (%s)`, strings.Join(cols, ", "), placeholder)

	_, err = exec.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}
