package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/util"
)

type createVideoRequest struct {
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	CategoryID    *int64   `json:"category_id"`
	CoverMediaID  string   `json:"cover_media_id"`
	SourceMediaID string   `json:"source_media_id"`
	Tags          []string `json:"tags"`
	Visibility    string   `json:"visibility"`
}

func (h *Handler) ListVideos(c *fiber.Ctx) error {
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cards, nextCursor, err := h.queryVideoCardsWithCursor(c.UserContext(), videoQueryOptions{
		Limit:    limit,
		Cursor:   c.Query("cursor"),
		Query:    strings.TrimSpace(c.Query("q")),
		Category: strings.TrimSpace(c.Query("category")),
		Sort:     strings.TrimSpace(c.Query("sort")),
	})
	if err != nil {
		if strings.Contains(err.Error(), "decode cursor") {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to list videos")
	}
	return response.OK(c, fiber.Map{"items": cards, "next_cursor": nextCursor})
}

func (h *Handler) GetVideoDetail(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	query := `
SELECT v.id, v.title, v.description, v.status, v.duration_sec, v.views_count, v.likes_count, v.favorites_count, v.comments_count, v.shares_count,
       COALESCE(v.published_at, v.created_at), COALESCE(v.visibility,'public'),
       COALESCE(cat.name, ''),
       u.id, u.username, COALESCE(u.followers_count, 0),
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, ''),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       COALESCE(sm.provider, ''), COALESCE(sm.bucket, ''), COALESCE(sm.object_key, ''),
       COALESCE(hls.provider, ''), COALESCE(hls.bucket, ''), COALESCE(hls.master_object_key, ''), COALESCE(hls.variants_json, '')
FROM videos v
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories cat ON cat.id = v.category_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects sm ON sm.id = v.source_media_id
LEFT JOIN video_hls_assets hls ON hls.video_id = v.id
WHERE v.id = ?
LIMIT 1`

	var (
		id, title, description, videoStatus, publishedAt, visibility, category string
		durationSec, views, likes, favorites, comments, shares, followers      int64
		uploaderID, uploaderName                                               string
		avatarProvider, avatarBucket, avatarKey                                string
		coverProvider, coverBucket, coverKey                                   string
		sourceProvider, sourceBucket, sourceKey                                string
		hlsProvider, hlsBucket, hlsMasterKey, hlsVariantsJSON                  string
	)

	err := h.app.DB.QueryRowContext(c.UserContext(), query, videoID).Scan(
		&id,
		&title,
		&description,
		&videoStatus,
		&durationSec,
		&views,
		&likes,
		&favorites,
		&comments,
		&shares,
		&publishedAt,
		&visibility,
		&category,
		&uploaderID,
		&uploaderName,
		&followers,
		&avatarProvider,
		&avatarBucket,
		&avatarKey,
		&coverProvider,
		&coverBucket,
		&coverKey,
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

	viewerID := currentUserID(c)
	isOwner := viewerID != "" && viewerID == uploaderID
	if videoStatus == "deleted" {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}
	if !isOwner {
		if videoStatus != "published" || visibility != "public" {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
	}

	tagRows, err := h.app.DB.QueryContext(c.UserContext(), `
SELECT t.name
FROM video_tags vt
JOIN tags t ON t.id = vt.tag_id
WHERE vt.video_id = ?
ORDER BY t.name ASC`, videoID)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to query tags")
	}
	defer tagRows.Close()
	tags := make([]string, 0)
	for tagRows.Next() {
		var tag string
		if err := tagRows.Scan(&tag); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse tags")
		}
		tags = append(tags, tag)
	}

	liked, favorited, followingUploader := false, false, false
	if viewerID != "" {
		_ = h.app.DB.QueryRowContext(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM video_actions WHERE user_id = ? AND video_id = ? AND action_type = 'like')`, viewerID, videoID).Scan(&liked)
		_ = h.app.DB.QueryRowContext(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM video_actions WHERE user_id = ? AND video_id = ? AND action_type = 'favorite')`, viewerID, videoID).Scan(&favorited)
		if viewerID != uploaderID {
			_ = h.app.DB.QueryRowContext(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id = ? AND followee_id = ?)`, viewerID, uploaderID).Scan(&followingUploader)
		}
	}

	mp4URL := mediaURL(h.app.Storage, sourceProvider, sourceBucket, sourceKey)
	playbackStatus := "ready"
	if videoStatus == "processing" {
		playbackStatus = "processing"
	} else if videoStatus == "failed" {
		playbackStatus = "failed"
	}

	playbackType := ""
	hlsMasterURL := ""
	variants := make([]map[string]interface{}, 0)
	if playbackStatus == "ready" && hlsMasterKey != "" {
		playbackType = "hls"
		hlsMasterURL = mediaURL(h.app.Storage, hlsProvider, hlsBucket, hlsMasterKey)

		var parsed []struct {
			Name              string `json:"name"`
			Width             int64  `json:"width"`
			Height            int64  `json:"height"`
			Bandwidth         int64  `json:"bandwidth"`
			PlaylistObjectKey string `json:"playlist_object_key"`
		}
		if err := json.Unmarshal([]byte(hlsVariantsJSON), &parsed); err == nil {
			for _, item := range parsed {
				variants = append(variants, map[string]interface{}{
					"name":      item.Name,
					"width":     item.Width,
					"height":    item.Height,
					"bandwidth": item.Bandwidth,
					"url":       mediaURL(h.app.Storage, hlsProvider, hlsBucket, item.PlaylistObjectKey),
				})
			}
		}
	} else if playbackStatus == "ready" && mp4URL != "" {
		playbackType = "mp4"
	}

	playback := fiber.Map{
		"status": playbackStatus,
		"type":   playbackType,
	}
	if hlsMasterURL != "" {
		playback["hls_master_url"] = hlsMasterURL
	}
	if mp4URL != "" {
		playback["mp4_url"] = mp4URL
	}
	if len(variants) > 0 {
		playback["variants"] = variants
	}

	data := fiber.Map{
		"status": videoStatus,
		"video": fiber.Map{
			"id":             id,
			"title":          title,
			"cover_url":      mediaURL(h.app.Storage, coverProvider, coverBucket, coverKey),
			"duration_sec":   durationSec,
			"views_count":    views,
			"comments_count": comments,
			"published_at":   publishedAt,
			"category":       category,
			"author": fiber.Map{
				"id":              uploaderID,
				"username":        uploaderName,
				"followers_count": followers,
				"avatar_url":      mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarKey),
				"followed":        followingUploader,
			},
		},
		"source_url":  mp4URL,
		"playback":    playback,
		"description": description,
		"tags":        tags,
		"stats":       fiber.Map{"views_count": views, "likes_count": likes, "favorites_count": favorites, "comments_count": comments, "shares_count": shares},
		"uploader":    fiber.Map{"id": uploaderID, "username": uploaderName, "followers_count": followers, "avatar_url": mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarKey), "followed": followingUploader},
		"viewer_actions": fiber.Map{
			"liked":              liked,
			"favorited":          favorited,
			"following_uploader": followingUploader,
		},
	}

	return response.OK(c, data)
}

func (h *Handler) GetVideoRecommendations(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}
	limit := pagination.ClampLimit(c.Query("limit"), 8, maxLimit)

	var categoryID sql.NullInt64
	if err := h.app.DB.QueryRowContext(c.UserContext(), `SELECT category_id FROM videos WHERE id = ? AND status = 'published'`, videoID).Scan(&categoryID); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load video")
	}

	opts := videoQueryOptions{Limit: limit, Sort: "hot"}
	if categoryID.Valid {
		opts.Category = fmt.Sprintf("%d", categoryID.Int64)
	}
	cards, err := h.queryVideoCards(c.UserContext(), opts)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch recommendations")
	}

	filtered := make([]map[string]interface{}, 0, len(cards))
	for _, card := range cards {
		if card["id"] == videoID {
			continue
		}
		filtered = append(filtered, card)
		if len(filtered) >= limit {
			break
		}
	}

	return response.OK(c, fiber.Map{"items": filtered})
}

func (h *Handler) TrackVideoView(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	viewerKey := clientViewerKey(c)

	viewedMinute := nowUTC().Truncate(time.Minute)

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(c.UserContext(),
		`INSERT OR IGNORE INTO video_view_events (id, video_id, viewer_key, viewed_minute, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		newID(),
		videoID,
		viewerKey,
		util.FormatTime(viewedMinute),
		nowString(),
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to track view")
	}
	affected, _ := res.RowsAffected()
	if affected > 0 {
		if _, err := tx.ExecContext(c.UserContext(), `UPDATE videos SET views_count = views_count + 1, updated_at = ? WHERE id = ? AND status = 'published'`, nowString(), videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update views")
		}
		if err := h.recomputeHotScoreTx(c.UserContext(), tx, videoID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update hot score")
		}
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit view")
	}
	return response.OK(c, fiber.Map{"counted": affected > 0})
}

func (h *Handler) ToggleVideoLike(c *fiber.Ctx) error {
	return h.toggleVideoAction(c, "like", "likes_count")
}

func (h *Handler) ToggleVideoFavorite(c *fiber.Ctx) error {
	return h.toggleVideoAction(c, "favorite", "favorites_count")
}

func (h *Handler) toggleVideoAction(c *fiber.Ctx, actionType, counterColumn string) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}
	uid := currentUserID(c)

	var req toggleRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	if req.Active {
		res, err := tx.ExecContext(c.UserContext(),
			`INSERT OR IGNORE INTO video_actions (user_id, video_id, action_type, created_at) VALUES (?, ?, ?, ?)`,
			uid,
			videoID,
			actionType,
			nowString(),
		)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update action")
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(c.UserContext(), fmt.Sprintf(`UPDATE videos SET %s = %s + 1, updated_at = ? WHERE id = ? AND status = 'published'`, counterColumn, counterColumn), nowString(), videoID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update counter")
			}
			if err := h.recomputeHotScoreTx(c.UserContext(), tx, videoID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update hot score")
			}
		}
	} else {
		res, err := tx.ExecContext(c.UserContext(), `DELETE FROM video_actions WHERE user_id = ? AND video_id = ? AND action_type = ?`, uid, videoID, actionType)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update action")
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(c.UserContext(), fmt.Sprintf(`UPDATE videos SET %s = CASE WHEN %s > 0 THEN %s - 1 ELSE 0 END, updated_at = ? WHERE id = ? AND status = 'published'`, counterColumn, counterColumn, counterColumn), nowString(), videoID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update counter")
			}
			if err := h.recomputeHotScoreTx(c.UserContext(), tx, videoID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update hot score")
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit action")
	}
	return response.OK(c, fiber.Map{"active": req.Active})
}

func (h *Handler) TrackVideoShare(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(c.UserContext(), `UPDATE videos SET shares_count = shares_count + 1, updated_at = ? WHERE id = ? AND status = 'published'`, nowString(), videoID)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update shares")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}
	if err := h.recomputeHotScoreTx(c.UserContext(), tx, videoID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update hot score")
	}
	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit share")
	}
	return response.OK(c, fiber.Map{"shared": true})
}

func (h *Handler) CreateVideo(c *fiber.Ctx) error {
	uid := currentUserID(c)

	var req createVideoRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.SourceMediaID = strings.TrimSpace(req.SourceMediaID)
	req.CoverMediaID = strings.TrimSpace(req.CoverMediaID)
	if req.Title == "" {
		return response.Error(c, fiber.StatusBadRequest, "title is required")
	}
	if req.SourceMediaID == "" {
		return response.Error(c, fiber.StatusBadRequest, "source_media_id is required")
	}
	if req.Visibility == "" {
		req.Visibility = "public"
	}
	if req.Visibility != "public" && req.Visibility != "private" && req.Visibility != "unlisted" {
		return response.Error(c, fiber.StatusBadRequest, "invalid visibility")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var sourceDuration int64
	if err := tx.QueryRowContext(c.UserContext(), `SELECT COALESCE(duration_sec, 0) FROM media_objects WHERE id = ? AND created_by = ?`, req.SourceMediaID, uid).Scan(&sourceDuration); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusBadRequest, "source_media_id is invalid")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate source media")
	}
	if req.CoverMediaID != "" {
		var tmp string
		if err := tx.QueryRowContext(c.UserContext(), `SELECT id FROM media_objects WHERE id = ? AND created_by = ?`, req.CoverMediaID, uid).Scan(&tmp); err != nil {
			if isNotFound(err) {
				return response.Error(c, fiber.StatusBadRequest, "cover_media_id is invalid")
			}
			return response.Error(c, fiber.StatusInternalServerError, "failed to validate cover media")
		}
	}
	if req.CategoryID != nil {
		var exists int
		if err := tx.QueryRowContext(c.UserContext(), `SELECT 1 FROM categories WHERE id = ? AND is_active = 1 LIMIT 1`, *req.CategoryID).Scan(&exists); err != nil {
			if isNotFound(err) {
				return response.Error(c, fiber.StatusBadRequest, "category_id is invalid")
			}
			return response.Error(c, fiber.StatusInternalServerError, "failed to validate category")
		}
	}

	videoID := newID()
	now := nowString()
	_, err = tx.ExecContext(c.UserContext(),
		`INSERT INTO videos (id, uploader_id, title, description, category_id, cover_media_id, source_media_id, status, visibility, duration_sec, published_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'processing', ?, ?, NULL, ?, ?)`,
		videoID,
		uid,
		req.Title,
		req.Description,
		req.CategoryID,
		nullableString(req.CoverMediaID),
		req.SourceMediaID,
		req.Visibility,
		sourceDuration,
		now,
		now,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create video")
	}

	seen := map[string]struct{}{}
	for _, raw := range req.Tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if len(tag) > 32 {
			tag = tag[:32]
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}

		if _, err := tx.ExecContext(c.UserContext(), `INSERT INTO tags (name, use_count, created_at) VALUES (?, 0, ?) ON CONFLICT(name) DO NOTHING`, tag, now); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to upsert tags")
		}
		var tagID int64
		if err := tx.QueryRowContext(c.UserContext(), `SELECT id FROM tags WHERE name = ?`, tag).Scan(&tagID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to query tag")
		}
		res, err := tx.ExecContext(c.UserContext(), `INSERT OR IGNORE INTO video_tags (video_id, tag_id) VALUES (?, ?)`, videoID, tagID)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to attach tag")
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(c.UserContext(), `UPDATE tags SET use_count = use_count + 1 WHERE id = ?`, tagID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update tag count")
			}
		}
	}

	maxAttempts := h.app.Config.TranscodeMaxTry
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	_, err = tx.ExecContext(c.UserContext(),
		`INSERT INTO video_transcode_jobs (id, video_id, status, attempts, max_attempts, available_at, created_at, updated_at)
		 VALUES (?, ?, 'queued', 0, ?, ?, ?, ?)`,
		newID(),
		videoID,
		maxAttempts,
		now,
		now,
		now,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to queue transcode job")
	}

	if err := h.recomputeHotScoreTx(c.UserContext(), tx, videoID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to compute hot score")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit video")
	}

	return response.Created(c, fiber.Map{"id": videoID, "status": "processing"})
}

func (h *Handler) DeleteVideo(c *fiber.Ctx) error {
	uid := currentUserID(c)
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	res, err := h.app.DB.ExecContext(c.UserContext(),
		`UPDATE videos
		 SET status = 'deleted', updated_at = ?
		 WHERE id = ? AND uploader_id = ? AND status != 'deleted'`,
		nowString(),
		videoID,
		uid,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to delete video")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}
	return response.OK(c, fiber.Map{"deleted": true})
}

func (h *Handler) recomputeHotScoreTx(ctx context.Context, tx *sql.Tx, videoID string) error {
	var views, likes, favorites, comments, shares int64
	if err := tx.QueryRowContext(ctx,
		`SELECT views_count, likes_count, favorites_count, comments_count, shares_count FROM videos WHERE id = ?`,
		videoID,
	).Scan(&views, &likes, &favorites, &comments, &shares); err != nil {
		return err
	}
	hot := computeHotScore(views, likes, favorites, comments, shares)
	_, err := tx.ExecContext(ctx, `UPDATE videos SET hot_score = ?, updated_at = ? WHERE id = ?`, hot, nowString(), videoID)
	return err
}

func nullableString(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func clientViewerKey(c *fiber.Ctx) string {
	viewerKey := strings.TrimSpace(c.Get("X-Viewer-Key"))
	if viewerKey != "" {
		return viewerKey
	}
	ip := c.IP()
	if parsed := net.ParseIP(ip); parsed != nil {
		ip = parsed.String()
	}
	return util.SHA256Hex(ip + "|" + c.Get("User-Agent"))
}
