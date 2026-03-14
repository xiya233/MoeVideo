package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
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

type updateVideoRequest struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Visibility  *string   `json:"visibility"`
	Tags        *[]string `json:"tags"`
}

type updateProgressRequest struct {
	PositionSec float64 `json:"position_sec"`
	DurationSec float64 `json:"duration_sec"`
	Completed   bool    `json:"completed"`
}

func (h *Handler) ListVideos(c *fiber.Ctx) error {
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cards, nextCursor, err := h.queryVideoCardsWithCursor(c.UserContext(), videoQueryOptions{
		Limit:    limit,
		Cursor:   c.Query("cursor"),
		Query:    strings.TrimSpace(c.Query("q")),
		Category: strings.TrimSpace(c.Query("category")),
		Tag:      strings.TrimSpace(c.Query("tag")),
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
       COALESCE(cat.name, ''), v.category_id,
       u.id, u.username, COALESCE(u.followers_count, 0),
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, ''),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       COALESCE(pm.provider, ''), COALESCE(pm.bucket, ''), COALESCE(pm.object_key, ''),
       COALESCE(sm.provider, ''), COALESCE(sm.bucket, ''), COALESCE(sm.object_key, ''),
       COALESCE(hls.provider, ''), COALESCE(hls.bucket, ''), COALESCE(hls.master_object_key, ''), COALESCE(hls.variants_json, ''),
       COALESCE(hls.thumbnail_vtt_object_key, ''), COALESCE(hls.thumbnail_sprite_object_key, '')
FROM videos v
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories cat ON cat.id = v.category_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects pm ON pm.id = v.preview_media_id
LEFT JOIN media_objects sm ON sm.id = v.source_media_id
LEFT JOIN video_hls_assets hls ON hls.video_id = v.id
WHERE v.id = ?
LIMIT 1`

	var (
		id, title, description, videoStatus, publishedAt, visibility, category string
		categoryID                                                             sql.NullInt64
		durationSec, views, likes, favorites, comments, shares, followers      int64
		uploaderID, uploaderName                                               string
		avatarProvider, avatarBucket, avatarKey                                string
		coverProvider, coverBucket, coverKey                                   string
		previewProvider, previewBucket, previewKey                             string
		sourceProvider, sourceBucket, sourceKey                                string
		hlsProvider, hlsBucket, hlsMasterKey, hlsVariantsJSON                  string
		hlsThumbnailVTTKey, hlsThumbnailSpriteKey                              string
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
		&categoryID,
		&uploaderID,
		&uploaderName,
		&followers,
		&avatarProvider,
		&avatarBucket,
		&avatarKey,
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
		&hlsThumbnailVTTKey,
		&hlsThumbnailSpriteKey,
	)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to get video")
	}

	viewerID := currentUserID(c)
	visible := videoVisibility{
		UploaderID: uploaderID,
		Status:     videoStatus,
		Visibility: visibility,
	}
	if !canReadVideo(visible, viewerID) {
		return response.Error(c, fiber.StatusNotFound, "video not found")
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
	viewerProgressSec := int64(0)
	if viewerID != "" {
		_ = h.app.DB.QueryRowContext(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM video_actions WHERE user_id = ? AND video_id = ? AND action_type = 'like')`, viewerID, videoID).Scan(&liked)
		_ = h.app.DB.QueryRowContext(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM video_actions WHERE user_id = ? AND video_id = ? AND action_type = 'favorite')`, viewerID, videoID).Scan(&favorited)
		if viewerID != uploaderID {
			_ = h.app.DB.QueryRowContext(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id = ? AND followee_id = ?)`, viewerID, uploaderID).Scan(&followingUploader)
		}
		if err := h.app.DB.QueryRowContext(c.UserContext(),
			`SELECT position_sec FROM user_video_progress WHERE user_id = ? AND video_id = ? LIMIT 1`,
			viewerID,
			videoID,
		).Scan(&viewerProgressSec); err != nil && !isNotFound(err) {
			return response.Error(c, fiber.StatusInternalServerError, "failed to load playback progress")
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
	if playbackStatus == "ready" && hlsThumbnailVTTKey != "" && hlsThumbnailSpriteKey != "" {
		playback["vtt_thumbnail_url"] = mediaURL(h.app.Storage, hlsProvider, hlsBucket, hlsThumbnailVTTKey)
	}

	videoData := fiber.Map{
		"id":               id,
		"title":            title,
		"cover_url":        mediaURL(h.app.Storage, coverProvider, coverBucket, coverKey),
		"preview_webp_url": mediaURL(h.app.Storage, previewProvider, previewBucket, previewKey),
		"visibility":       visibility,
		"duration_sec":     durationSec,
		"views_count":      views,
		"comments_count":   comments,
		"published_at":     publishedAt,
		"category":         category,
		"author": fiber.Map{
			"id":              uploaderID,
			"username":        uploaderName,
			"followers_count": followers,
			"avatar_url":      mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarKey),
			"followed":        followingUploader,
		},
	}
	if categoryID.Valid {
		videoData["category_id"] = categoryID.Int64
	}

	data := fiber.Map{
		"status":      videoStatus,
		"video":       videoData,
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
	if viewerID != "" {
		data["viewer_progress_sec"] = viewerProgressSec
	}

	return response.OK(c, data)
}

func (h *Handler) GetVideoRecommendations(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}
	limit := pagination.ClampLimit(c.Query("limit"), 8, maxLimit)
	randomMode := strings.TrimSpace(c.Query("random")) == "1"
	excludeIDs := parseRecommendationExcludeIDs(c.Query("exclude_ids"))
	viewerID := currentUserID(c)

	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate video")
	}
	if !canReadVideo(visible, viewerID) {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}

	var categoryID sql.NullInt64
	if err := h.app.DB.QueryRowContext(c.UserContext(), `SELECT category_id FROM videos WHERE id = ?`, videoID).Scan(&categoryID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load video")
	}

	excludeSet := make(map[string]struct{}, len(excludeIDs)+1)
	excludeSet[videoID] = struct{}{}
	for _, id := range excludeIDs {
		excludeSet[id] = struct{}{}
	}

	if randomMode {
		var categoryPtr *int64
		if categoryID.Valid {
			v := categoryID.Int64
			categoryPtr = &v
		}
		cards, err := h.queryRandomRecommendations(c.UserContext(), limit, categoryPtr, excludeSet)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to fetch recommendations")
		}
		return response.OK(c, fiber.Map{"items": cards})
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
		cardID, _ := card["id"].(string)
		if _, ok := excludeSet[cardID]; ok {
			continue
		}
		filtered = append(filtered, card)
		if len(filtered) >= limit {
			break
		}
	}

	return response.OK(c, fiber.Map{"items": filtered})
}

func parseRecommendationExcludeIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= 100 {
			break
		}
	}
	return out
}

func (h *Handler) queryRandomRecommendations(
	ctx context.Context,
	limit int,
	categoryID *int64,
	excludeSet map[string]struct{},
) ([]map[string]interface{}, error) {
	if limit <= 0 {
		return []map[string]interface{}{}, nil
	}

	excludedIDs := make([]string, 0, len(excludeSet))
	for id := range excludeSet {
		excludedIDs = append(excludedIDs, id)
	}

	queryPart := func(categoryOnly bool) (string, []interface{}) {
		query := `
SELECT v.id, v.title, v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
       COALESCE(v.hot_score, 0),
       COALESCE(c.name, ''),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       COALESCE(pm.provider, ''), COALESCE(pm.bucket, ''), COALESCE(pm.object_key, ''),
       u.id, u.username, COALESCE(u.followers_count, 0),
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, '')
FROM videos v
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories c ON c.id = v.category_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects pm ON pm.id = v.preview_media_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
WHERE v.status = 'published' AND v.visibility = 'public'
`
		args := make([]interface{}, 0, 4+len(excludedIDs))
		if categoryOnly && categoryID != nil {
			query += ` AND v.category_id = ?`
			args = append(args, *categoryID)
		}
		if len(excludedIDs) > 0 {
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(excludedIDs)), ",")
			query += ` AND v.id NOT IN (` + placeholders + `)`
			for _, id := range excludedIDs {
				args = append(args, id)
			}
		}
		query += ` ORDER BY RANDOM() LIMIT ?`
		args = append(args, limit)
		return query, args
	}

	runQuery := func(categoryOnly bool) ([]map[string]interface{}, error) {
		query, args := queryPart(categoryOnly)
		rows, err := h.app.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return h.scanVideoCards(rows)
	}

	result := make([]map[string]interface{}, 0, limit)
	if categoryID != nil {
		cards, err := runQuery(true)
		if err != nil {
			return nil, err
		}
		for _, card := range cards {
			result = append(result, card)
			if id, ok := card["id"].(string); ok && id != "" {
				if _, exists := excludeSet[id]; !exists {
					excludeSet[id] = struct{}{}
					excludedIDs = append(excludedIDs, id)
				}
			}
		}
	}

	if len(result) < limit {
		cards, err := runQuery(false)
		if err != nil {
			return nil, err
		}
		for _, card := range cards {
			id, _ := card["id"].(string)
			if id != "" {
				if _, exists := excludeSet[id]; exists {
					continue
				}
				excludeSet[id] = struct{}{}
			}
			result = append(result, card)
			if len(result) >= limit {
				break
			}
		}
	}

	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (h *Handler) TrackVideoView(c *fiber.Ctx) error {
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}
	viewerID := currentUserID(c)

	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate video")
	}
	if !canReadVideo(visible, viewerID) || visible.Status != "published" {
		return response.Error(c, fiber.StatusNotFound, "video not found")
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

	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate video")
	}
	if !canReadVideo(visible, uid) || visible.Status != "published" {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}

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
	viewerID := currentUserID(c)

	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate video")
	}
	if !canReadVideo(visible, viewerID) || visible.Status != "published" {
		return response.Error(c, fiber.StatusNotFound, "video not found")
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

func (h *Handler) UpdateVideoProgress(c *fiber.Ctx) error {
	uid := currentUserID(c)
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	var req updateProgressRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	positionSec := int64(math.Round(req.PositionSec))
	durationSec := int64(math.Round(req.DurationSec))
	if positionSec < 0 {
		positionSec = 0
	}
	if durationSec < 0 {
		durationSec = 0
	}
	if durationSec > 0 && positionSec > durationSec {
		positionSec = durationSec
	}

	visible, err := h.loadVideoVisibility(c.UserContext(), videoID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate video")
	}
	if !canReadVideo(visible, uid) {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}
	if durationSec <= 0 {
		durationSec = visible.Duration
	}

	shouldClear := req.Completed
	if !shouldClear && durationSec > 0 && positionSec >= durationSec-15 {
		shouldClear = true
	}

	if shouldClear || positionSec <= 0 {
		if _, err := h.app.DB.ExecContext(
			c.UserContext(),
			`DELETE FROM user_video_progress WHERE user_id = ? AND video_id = ?`,
			uid,
			videoID,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to clear progress")
		}
		return response.OK(c, fiber.Map{"saved": true, "position_sec": int64(0)})
	}

	now := nowString()
	if _, err := h.app.DB.ExecContext(
		c.UserContext(),
		`INSERT INTO user_video_progress (user_id, video_id, position_sec, duration_sec, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, video_id) DO UPDATE SET
			position_sec = excluded.position_sec,
			duration_sec = excluded.duration_sec,
			updated_at = excluded.updated_at`,
		uid,
		videoID,
		positionSec,
		durationSec,
		now,
		now,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to save progress")
	}

	return response.OK(c, fiber.Map{"saved": true, "position_sec": positionSec})
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
	if req.CategoryID == nil {
		return response.Error(c, fiber.StatusBadRequest, "category_id is required")
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
	var exists int
	if err := tx.QueryRowContext(c.UserContext(), `SELECT 1 FROM categories WHERE id = ? AND is_active = 1 LIMIT 1`, *req.CategoryID).Scan(&exists); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusBadRequest, "category_id is invalid")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate category")
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

	if err := h.syncVideoTagsTx(c.UserContext(), tx, videoID, normalizeVideoTags(req.Tags), now); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to sync tags")
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

func (h *Handler) UpdateVideo(c *fiber.Ctx) error {
	uid := currentUserID(c)
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
	}

	var req updateVideoRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Title == nil && req.Description == nil && req.Visibility == nil && req.Tags == nil {
		return response.Error(c, fiber.StatusBadRequest, "at least one field is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var uploaderID, status string
	if err := tx.QueryRowContext(c.UserContext(),
		`SELECT uploader_id, status FROM videos WHERE id = ? LIMIT 1`,
		videoID,
	).Scan(&uploaderID, &status); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "video not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to query video")
	}
	if uploaderID != uid || status == "deleted" {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}

	setClauses := make([]string, 0, 3)
	args := make([]interface{}, 0, 4)
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return response.Error(c, fiber.StatusBadRequest, "title is required")
		}
		if len([]rune(title)) > 120 {
			return response.Error(c, fiber.StatusBadRequest, "title is too long")
		}
		setClauses = append(setClauses, "title = ?")
		args = append(args, title)
	}
	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		setClauses = append(setClauses, "description = ?")
		args = append(args, description)
	}
	if req.Visibility != nil {
		visibility := strings.TrimSpace(*req.Visibility)
		if visibility == "" {
			visibility = "public"
		}
		if visibility != "public" && visibility != "private" && visibility != "unlisted" {
			return response.Error(c, fiber.StatusBadRequest, "invalid visibility")
		}
		setClauses = append(setClauses, "visibility = ?")
		args = append(args, visibility)
	}
	now := nowString()
	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = ?")
		args = append(args, now, videoID)
		if _, err := tx.ExecContext(c.UserContext(),
			`UPDATE videos SET `+strings.Join(setClauses, ", ")+` WHERE id = ?`,
			args...,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update video")
		}
	}
	if req.Tags != nil {
		if err := h.syncVideoTagsTx(c.UserContext(), tx, videoID, normalizeVideoTags(*req.Tags), now); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to sync tags")
		}
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit video update")
	}
	return response.OK(c, fiber.Map{"updated": true})
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

func normalizeVideoTags(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		tag := strings.TrimSpace(item)
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
		out = append(out, tag)
	}
	return out
}

func (h *Handler) syncVideoTagsTx(ctx context.Context, tx *sql.Tx, videoID string, tags []string, now string) error {
	existingTagIDs := make(map[int64]struct{})
	rows, err := tx.QueryContext(ctx, `SELECT tag_id FROM video_tags WHERE video_id = ?`, videoID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tagID int64
		if err := rows.Scan(&tagID); err != nil {
			return err
		}
		existingTagIDs[tagID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	nextTagIDs := make(map[int64]struct{}, len(tags))
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tags (name, use_count, created_at) VALUES (?, 0, ?) ON CONFLICT(name) DO NOTHING`,
			tag, now,
		); err != nil {
			return err
		}
		var tagID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? LIMIT 1`, tag).Scan(&tagID); err != nil {
			return err
		}
		nextTagIDs[tagID] = struct{}{}
	}

	for tagID := range existingTagIDs {
		if _, keep := nextTagIDs[tagID]; keep {
			continue
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM video_tags WHERE video_id = ? AND tag_id = ?`, videoID, tagID)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tags SET use_count = CASE WHEN use_count > 0 THEN use_count - 1 ELSE 0 END WHERE id = ?`,
				tagID,
			); err != nil {
				return err
			}
		}
	}

	for tagID := range nextTagIDs {
		if _, exists := existingTagIDs[tagID]; exists {
			continue
		}
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO video_tags (video_id, tag_id) VALUES (?, ?)`, videoID, tagID)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE tags SET use_count = use_count + 1 WHERE id = ?`, tagID); err != nil {
				return err
			}
		}
	}
	return nil
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
