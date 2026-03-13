package handlers

import (
	"context"
	"database/sql"
	"math"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
)

type followRequest struct {
	Active bool `json:"active"`
}

type patchMeRequest struct {
	Bio             *string `json:"bio"`
	AvatarMediaID   *string `json:"avatar_media_id"`
	ProfilePublic   *bool   `json:"profile_public"`
	PublicVideos    *bool   `json:"public_videos"`
	PublicFavorites *bool   `json:"public_favorites"`
	PublicFollowing *bool   `json:"public_following"`
	PublicFollowers *bool   `json:"public_followers"`
}

type listCursor struct {
	PublishedAt string `json:"published_at"`
	ID          string `json:"id"`
}

type timeCursor struct {
	SortAt string `json:"sort_at"`
	ID     string `json:"id"`
}

func (h *Handler) GetMe(c *fiber.Ctx) error {
	uid := currentUserID(c)
	user, err := fetchUserBrief(h.app.DB, h.app.Storage, uid, true)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "user not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch user")
	}
	return response.OK(c, user)
}

func (h *Handler) UpdateMe(c *fiber.Ctx) error {
	uid := currentUserID(c)

	var req patchMeRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if req.Bio == nil &&
		req.AvatarMediaID == nil &&
		req.ProfilePublic == nil &&
		req.PublicVideos == nil &&
		req.PublicFavorites == nil &&
		req.PublicFollowing == nil &&
		req.PublicFollowers == nil {
		return response.Error(c, fiber.StatusBadRequest, "nothing to update")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	setClauses := make([]string, 0, 8)
	args := make([]interface{}, 0, 4)

	if req.Bio != nil {
		bio := strings.TrimSpace(*req.Bio)
		if len([]rune(bio)) > 500 {
			return response.Error(c, fiber.StatusBadRequest, "bio is too long")
		}
		setClauses = append(setClauses, "bio = ?")
		args = append(args, bio)
	}

	if req.AvatarMediaID != nil {
		avatarMediaID := strings.TrimSpace(*req.AvatarMediaID)
		if avatarMediaID == "" {
			setClauses = append(setClauses, "avatar_media_id = NULL")
		} else {
			var mimeType string
			if err := tx.QueryRowContext(
				c.UserContext(),
				`SELECT mime_type FROM media_objects WHERE id = ? AND created_by = ? LIMIT 1`,
				avatarMediaID,
				uid,
			).Scan(&mimeType); err != nil {
				if isNotFound(err) {
					return response.Error(c, fiber.StatusBadRequest, "avatar_media_id is invalid")
				}
				return response.Error(c, fiber.StatusInternalServerError, "failed to validate avatar media")
			}
			if _, ok := allowedCoverMIMEs[strings.ToLower(strings.TrimSpace(mimeType))]; !ok {
				return response.Error(c, fiber.StatusBadRequest, "avatar_media_id must be an image")
			}

			setClauses = append(setClauses, "avatar_media_id = ?")
			args = append(args, avatarMediaID)
		}
	}

	if req.ProfilePublic != nil {
		setClauses = append(setClauses, "profile_public = ?")
		args = append(args, boolToInt64(*req.ProfilePublic))
	}
	if req.PublicVideos != nil {
		setClauses = append(setClauses, "public_videos = ?")
		args = append(args, boolToInt64(*req.PublicVideos))
	}
	if req.PublicFavorites != nil {
		setClauses = append(setClauses, "public_favorites = ?")
		args = append(args, boolToInt64(*req.PublicFavorites))
	}
	if req.PublicFollowing != nil {
		setClauses = append(setClauses, "public_following = ?")
		args = append(args, boolToInt64(*req.PublicFollowing))
	}
	if req.PublicFollowers != nil {
		setClauses = append(setClauses, "public_followers = ?")
		args = append(args, boolToInt64(*req.PublicFollowers))
	}

	if len(setClauses) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "nothing to update")
	}

	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, nowString(), uid)

	query := `UPDATE users SET ` + strings.Join(setClauses, ", ") + ` WHERE id = ?`
	if _, err := tx.ExecContext(c.UserContext(), query, args...); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update user")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update user")
	}

	user, err := fetchUserBrief(h.app.DB, h.app.Storage, uid, true)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch user")
	}

	return response.OK(c, fiber.Map{"user": user})
}

func (h *Handler) GetUserByID(c *fiber.Ctx) error {
	userID := strings.TrimSpace(c.Params("userId"))
	if userID == "" {
		return response.Error(c, fiber.StatusBadRequest, "userId is required")
	}

	user, err := fetchUserBrief(h.app.DB, h.app.Storage, userID, false)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "user not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch user")
	}

	followed := false
	viewerID := currentUserID(c)
	isOwner := viewerID != "" && viewerID == userID
	if viewerID != "" && viewerID != userID {
		row := h.app.DB.QueryRow(`SELECT 1 FROM follows WHERE follower_id = ? AND followee_id = ? LIMIT 1`, viewerID, userID)
		var tmp int
		if err := row.Scan(&tmp); err == nil {
			followed = true
		} else if !isNotFound(err) {
			return response.Error(c, fiber.StatusInternalServerError, "failed to fetch follow status")
		}
	}

	profileAccessible := user.ProfilePublic || isOwner
	return response.OK(c, fiber.Map{
		"user": fiber.Map{
			"id":               user.ID,
			"username":         user.Username,
			"bio":              user.Bio,
			"avatar_url":       user.AvatarURL,
			"followers_count":  user.FollowersCount,
			"following_count":  user.FollowingCount,
			"profile_public":   user.ProfilePublic,
			"public_videos":    user.PublicVideos,
			"public_favorites": user.PublicFavorites,
			"public_following": user.PublicFollowing,
			"public_followers": user.PublicFollowers,
		},
		"followed":           followed,
		"profile_accessible": profileAccessible,
	})
}

func (h *Handler) ToggleFollow(c *fiber.Ctx) error {
	viewerID := currentUserID(c)
	targetUserID := strings.TrimSpace(c.Params("userId"))
	if targetUserID == "" {
		return response.Error(c, fiber.StatusBadRequest, "userId is required")
	}
	if viewerID == targetUserID {
		return response.Error(c, fiber.StatusBadRequest, "cannot follow yourself")
	}

	var req followRequest
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
			`INSERT OR IGNORE INTO follows (follower_id, followee_id, created_at) VALUES (?, ?, ?)`,
			viewerID,
			targetUserID,
			nowString(),
		)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to follow user")
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(c.UserContext(), `UPDATE users SET followers_count = followers_count + 1, updated_at = ? WHERE id = ?`, nowString(), targetUserID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update followers_count")
			}
			if _, err := tx.ExecContext(c.UserContext(), `UPDATE users SET following_count = following_count + 1, updated_at = ? WHERE id = ?`, nowString(), viewerID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update following_count")
			}
		}
	} else {
		res, err := tx.ExecContext(c.UserContext(), `DELETE FROM follows WHERE follower_id = ? AND followee_id = ?`, viewerID, targetUserID)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to unfollow user")
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(c.UserContext(), `UPDATE users SET followers_count = CASE WHEN followers_count > 0 THEN followers_count - 1 ELSE 0 END, updated_at = ? WHERE id = ?`, nowString(), targetUserID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update followers_count")
			}
			if _, err := tx.ExecContext(c.UserContext(), `UPDATE users SET following_count = CASE WHEN following_count > 0 THEN following_count - 1 ELSE 0 END, updated_at = ? WHERE id = ?`, nowString(), viewerID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update following_count")
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit follow operation")
	}
	return response.OK(c, fiber.Map{"active": req.Active})
}

func (h *Handler) ListMyVideos(c *fiber.Ctx) error {
	uid := currentUserID(c)
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := c.Query("cursor")

	args := []interface{}{uid}
	query := `
SELECT v.id, v.title, v.status, v.visibility, v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
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
WHERE v.uploader_id = ? AND v.status != 'deleted'
`

	var cur listCursor
	if cursor != "" {
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (COALESCE(v.published_at, v.created_at) < ? OR (COALESCE(v.published_at, v.created_at) = ? AND v.id < ?))`
		args = append(args, cur.PublishedAt, cur.PublishedAt, cur.ID)
	}
	query += ` ORDER BY COALESCE(v.published_at, v.created_at) DESC, v.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list videos")
	}
	defer rows.Close()

	cards, err := h.scanMyVideoCards(rows)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to parse videos")
	}

	nextCursor := ""
	if len(cards) > limit {
		last := cards[limit-1]
		cards = cards[:limit]
		nextCursor, err = pagination.Encode(listCursor{
			PublishedAt: last["published_at"].(string),
			ID:          last["id"].(string),
		})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
	}

	return response.OK(c, fiber.Map{"items": cards, "next_cursor": nextCursor})
}

func (h *Handler) ListMyFavorites(c *fiber.Ctx) error {
	uid := currentUserID(c)
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT va.created_at,
       v.id, v.title, v.status, COALESCE(v.visibility, 'public'), v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
       COALESCE(c.name, ''),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       COALESCE(pm.provider, ''), COALESCE(pm.bucket, ''), COALESCE(pm.object_key, ''),
       u.id, u.username, COALESCE(u.followers_count, 0),
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, '')
FROM video_actions va
JOIN videos v ON v.id = va.video_id
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories c ON c.id = v.category_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects pm ON pm.id = v.preview_media_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
WHERE va.user_id = ?
  AND va.action_type = 'favorite'
  AND v.status != 'deleted'
  AND ((v.status = 'published' AND COALESCE(v.visibility, 'public') = 'public') OR v.uploader_id = ?)
`
	args := []interface{}{uid, uid}

	if cursor != "" {
		var cur timeCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (va.created_at < ? OR (va.created_at = ? AND v.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY va.created_at DESC, v.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list favorites")
	}
	defer rows.Close()

	type itemWithSort struct {
		SortAt string
		ID     string
		Item   map[string]interface{}
	}
	items := make([]itemWithSort, 0)
	for rows.Next() {
		var (
			sortAt, id, title, status, visibility, publishedAt, category string
			durationSec, viewsCount, commentsCount                       int64
			coverProvider, coverBucket, coverObjectKey                   string
			previewProvider, previewBucket, previewObjectKey             string
			authorID, authorName                                         string
			authorFollowers                                              int64
			authorProvider, authorBucket, authorObjectKey                string
		)
		if err := rows.Scan(
			&sortAt,
			&id,
			&title,
			&status,
			&visibility,
			&durationSec,
			&viewsCount,
			&commentsCount,
			&publishedAt,
			&category,
			&coverProvider,
			&coverBucket,
			&coverObjectKey,
			&previewProvider,
			&previewBucket,
			&previewObjectKey,
			&authorID,
			&authorName,
			&authorFollowers,
			&authorProvider,
			&authorBucket,
			&authorObjectKey,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse favorites")
		}

		items = append(items, itemWithSort{
			SortAt: sortAt,
			ID:     id,
			Item: map[string]interface{}{
				"id":               id,
				"title":            title,
				"status":           status,
				"visibility":       visibility,
				"cover_url":        mediaURL(h.app.Storage, coverProvider, coverBucket, coverObjectKey),
				"preview_webp_url": mediaURL(h.app.Storage, previewProvider, previewBucket, previewObjectKey),
				"duration_sec":     durationSec,
				"views_count":      viewsCount,
				"comments_count":   commentsCount,
				"published_at":     publishedAt,
				"category":         category,
				"author": map[string]interface{}{
					"id":              authorID,
					"username":        authorName,
					"followers_count": authorFollowers,
					"avatar_url":      mediaURL(h.app.Storage, authorProvider, authorBucket, authorObjectKey),
				},
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list favorites")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(timeCursor{SortAt: last.SortAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, item.Item)
	}

	return response.OK(c, fiber.Map{"items": payload, "next_cursor": nextCursor})
}

func (h *Handler) ListMyFollowing(c *fiber.Ctx) error {
	uid := currentUserID(c)
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT f.created_at,
       u.id, u.username, u.bio, COALESCE(u.followers_count, 0), COALESCE(u.following_count, 0),
       COALESCE(m.provider, ''), COALESCE(m.bucket, ''), COALESCE(m.object_key, '')
FROM follows f
JOIN users u ON u.id = f.followee_id
LEFT JOIN media_objects m ON m.id = u.avatar_media_id
WHERE f.follower_id = ? AND u.status = 'active'
`
	args := []interface{}{uid}
	if cursor != "" {
		var cur timeCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (f.created_at < ? OR (f.created_at = ? AND u.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY f.created_at DESC, u.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list following")
	}
	defer rows.Close()

	type userWithSort struct {
		SortAt string
		ID     string
		Item   map[string]interface{}
	}
	items := make([]userWithSort, 0)
	for rows.Next() {
		var (
			sortAt, id, username, bio               string
			followersCount, followingCount          int64
			avatarProvider, avatarBucket, avatarKey string
		)
		if err := rows.Scan(
			&sortAt,
			&id,
			&username,
			&bio,
			&followersCount,
			&followingCount,
			&avatarProvider,
			&avatarBucket,
			&avatarKey,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse following")
		}
		items = append(items, userWithSort{
			SortAt: sortAt,
			ID:     id,
			Item: map[string]interface{}{
				"id":              id,
				"username":        username,
				"bio":             bio,
				"followers_count": followersCount,
				"following_count": followingCount,
				"avatar_url":      mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarKey),
				"followed":        true,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list following")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(timeCursor{SortAt: last.SortAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, item.Item)
	}

	return response.OK(c, fiber.Map{"items": payload, "next_cursor": nextCursor})
}

func (h *Handler) ListMyFollowers(c *fiber.Ctx) error {
	uid := currentUserID(c)
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT f.created_at,
       u.id, u.username, u.bio, COALESCE(u.followers_count, 0), COALESCE(u.following_count, 0),
       COALESCE(m.provider, ''), COALESCE(m.bucket, ''), COALESCE(m.object_key, ''),
       CASE WHEN EXISTS (
         SELECT 1 FROM follows fx WHERE fx.follower_id = ? AND fx.followee_id = u.id
       ) THEN 1 ELSE 0 END AS followed
FROM follows f
JOIN users u ON u.id = f.follower_id
LEFT JOIN media_objects m ON m.id = u.avatar_media_id
WHERE f.followee_id = ? AND u.status = 'active'
`
	args := []interface{}{uid, uid}
	if cursor != "" {
		var cur timeCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (f.created_at < ? OR (f.created_at = ? AND u.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY f.created_at DESC, u.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list followers")
	}
	defer rows.Close()

	type userWithSort struct {
		SortAt string
		ID     string
		Item   map[string]interface{}
	}
	items := make([]userWithSort, 0)
	for rows.Next() {
		var (
			sortAt, id, username, bio                string
			followersCount, followingCount, followed int64
			avatarProvider, avatarBucket, avatarKey  string
		)
		if err := rows.Scan(
			&sortAt,
			&id,
			&username,
			&bio,
			&followersCount,
			&followingCount,
			&avatarProvider,
			&avatarBucket,
			&avatarKey,
			&followed,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse followers")
		}
		items = append(items, userWithSort{
			SortAt: sortAt,
			ID:     id,
			Item: map[string]interface{}{
				"id":              id,
				"username":        username,
				"bio":             bio,
				"followers_count": followersCount,
				"following_count": followingCount,
				"avatar_url":      mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarKey),
				"followed":        followed > 0,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list followers")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(timeCursor{SortAt: last.SortAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, item.Item)
	}

	return response.OK(c, fiber.Map{"items": payload, "next_cursor": nextCursor})
}

func (h *Handler) ListMyContinueWatching(c *fiber.Ctx) error {
	uid := currentUserID(c)
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT uvp.updated_at, uvp.position_sec, uvp.duration_sec,
       v.id, v.title, v.status, COALESCE(v.visibility, 'public'), v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
       COALESCE(c.name, ''),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       COALESCE(pm.provider, ''), COALESCE(pm.bucket, ''), COALESCE(pm.object_key, ''),
       u.id, u.username, COALESCE(u.followers_count, 0),
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, '')
FROM user_video_progress uvp
JOIN videos v ON v.id = uvp.video_id
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories c ON c.id = v.category_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects pm ON pm.id = v.preview_media_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
WHERE uvp.user_id = ?
  AND v.status != 'deleted'
  AND ((v.status = 'published' AND COALESCE(v.visibility, 'public') = 'public') OR v.uploader_id = ?)
`
	args := []interface{}{uid, uid}
	if cursor != "" {
		var cur timeCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (uvp.updated_at < ? OR (uvp.updated_at = ? AND v.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY uvp.updated_at DESC, v.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list continue watching")
	}
	defer rows.Close()

	type continueItem struct {
		SortAt string
		ID     string
		Item   map[string]interface{}
	}
	items := make([]continueItem, 0)
	for rows.Next() {
		var (
			sortAt, id, title, status, visibility, publishedAt, category string
			positionSec, progressDurationSec, videoDurationSec           int64
			viewsCount, commentsCount                                    int64
			coverProvider, coverBucket, coverObjectKey                   string
			previewProvider, previewBucket, previewObjectKey             string
			authorID, authorName                                         string
			authorFollowers                                              int64
			authorProvider, authorBucket, authorObjectKey                string
		)
		if err := rows.Scan(
			&sortAt,
			&positionSec,
			&progressDurationSec,
			&id,
			&title,
			&status,
			&visibility,
			&videoDurationSec,
			&viewsCount,
			&commentsCount,
			&publishedAt,
			&category,
			&coverProvider,
			&coverBucket,
			&coverObjectKey,
			&previewProvider,
			&previewBucket,
			&previewObjectKey,
			&authorID,
			&authorName,
			&authorFollowers,
			&authorProvider,
			&authorBucket,
			&authorObjectKey,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse continue watching")
		}

		totalDuration := progressDurationSec
		if totalDuration <= 0 {
			totalDuration = videoDurationSec
		}
		progressPercent := float64(0)
		if totalDuration > 0 {
			progressPercent = (float64(positionSec) / float64(totalDuration)) * 100
		}
		progressPercent = math.Max(0, math.Min(100, math.Round(progressPercent*10)/10))

		items = append(items, continueItem{
			SortAt: sortAt,
			ID:     id,
			Item: map[string]interface{}{
				"video": map[string]interface{}{
					"id":               id,
					"title":            title,
					"status":           status,
					"visibility":       visibility,
					"cover_url":        mediaURL(h.app.Storage, coverProvider, coverBucket, coverObjectKey),
					"preview_webp_url": mediaURL(h.app.Storage, previewProvider, previewBucket, previewObjectKey),
					"duration_sec":     videoDurationSec,
					"views_count":      viewsCount,
					"comments_count":   commentsCount,
					"published_at":     publishedAt,
					"category":         category,
					"author": map[string]interface{}{
						"id":              authorID,
						"username":        authorName,
						"followers_count": authorFollowers,
						"avatar_url":      mediaURL(h.app.Storage, authorProvider, authorBucket, authorObjectKey),
					},
				},
				"position_sec":     positionSec,
				"duration_sec":     totalDuration,
				"progress_percent": progressPercent,
				"updated_at":       sortAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list continue watching")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(timeCursor{SortAt: last.SortAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, item.Item)
	}

	return response.OK(c, fiber.Map{"items": payload, "next_cursor": nextCursor})
}

type userProfilePrivacy struct {
	ProfilePublic   bool
	PublicVideos    bool
	PublicFavorites bool
	PublicFollowing bool
	PublicFollowers bool
}

func (h *Handler) loadUserProfilePrivacy(ctx context.Context, userID string) (userProfilePrivacy, error) {
	var (
		profilePublic   int64
		publicVideos    int64
		publicFavorites int64
		publicFollowing int64
		publicFollowers int64
	)
	if err := h.app.DB.QueryRowContext(
		ctx,
		`SELECT COALESCE(profile_public, 1), COALESCE(public_videos, 1), COALESCE(public_favorites, 0), COALESCE(public_following, 0), COALESCE(public_followers, 0)
		 FROM users
		 WHERE id = ?
		 LIMIT 1`,
		userID,
	).Scan(&profilePublic, &publicVideos, &publicFavorites, &publicFollowing, &publicFollowers); err != nil {
		return userProfilePrivacy{}, err
	}
	return userProfilePrivacy{
		ProfilePublic:   profilePublic > 0,
		PublicVideos:    publicVideos > 0,
		PublicFavorites: publicFavorites > 0,
		PublicFollowing: publicFollowing > 0,
		PublicFollowers: publicFollowers > 0,
	}, nil
}

func (h *Handler) canViewProfileSection(viewerID, targetUserID, section string, privacy userProfilePrivacy) bool {
	if viewerID != "" && viewerID == targetUserID {
		return true
	}
	if !privacy.ProfilePublic {
		return false
	}
	switch section {
	case "videos":
		return privacy.PublicVideos
	case "favorites":
		return privacy.PublicFavorites
	case "following":
		return privacy.PublicFollowing
	case "followers":
		return privacy.PublicFollowers
	default:
		return false
	}
}

func (h *Handler) ListUserVideos(c *fiber.Ctx) error {
	targetUserID := strings.TrimSpace(c.Params("userId"))
	if targetUserID == "" {
		return response.Error(c, fiber.StatusBadRequest, "userId is required")
	}
	viewerID := currentUserID(c)
	privacy, err := h.loadUserProfilePrivacy(c.UserContext(), targetUserID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "user not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load user")
	}
	if !h.canViewProfileSection(viewerID, targetUserID, "videos", privacy) {
		return response.Error(c, fiber.StatusForbidden, "profile content is private")
	}

	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))
	isOwner := viewerID != "" && viewerID == targetUserID

	args := []interface{}{targetUserID}
	query := `
SELECT v.id, v.title, v.status, v.visibility, v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
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
WHERE v.uploader_id = ?
`
	if isOwner {
		query += ` AND v.status != 'deleted'`
	} else {
		query += ` AND v.status = 'published' AND COALESCE(v.visibility, 'public') = 'public'`
	}

	if cursor != "" {
		var cur listCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (COALESCE(v.published_at, v.created_at) < ? OR (COALESCE(v.published_at, v.created_at) = ? AND v.id < ?))`
		args = append(args, cur.PublishedAt, cur.PublishedAt, cur.ID)
	}
	query += ` ORDER BY COALESCE(v.published_at, v.created_at) DESC, v.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list videos")
	}
	defer rows.Close()

	cards, err := h.scanMyVideoCards(rows)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to parse videos")
	}

	nextCursor := ""
	if len(cards) > limit {
		last := cards[limit-1]
		cards = cards[:limit]
		nextCursor, err = pagination.Encode(listCursor{
			PublishedAt: last["published_at"].(string),
			ID:          last["id"].(string),
		})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
	}

	return response.OK(c, fiber.Map{"items": cards, "next_cursor": nextCursor})
}

func (h *Handler) ListUserFavorites(c *fiber.Ctx) error {
	targetUserID := strings.TrimSpace(c.Params("userId"))
	if targetUserID == "" {
		return response.Error(c, fiber.StatusBadRequest, "userId is required")
	}
	viewerID := currentUserID(c)
	privacy, err := h.loadUserProfilePrivacy(c.UserContext(), targetUserID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "user not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load user")
	}
	if !h.canViewProfileSection(viewerID, targetUserID, "favorites", privacy) {
		return response.Error(c, fiber.StatusForbidden, "profile content is private")
	}

	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))
	isOwner := viewerID != "" && viewerID == targetUserID

	query := `
SELECT va.created_at,
       v.id, v.title, v.status, COALESCE(v.visibility, 'public'), v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
       COALESCE(c.name, ''),
       COALESCE(cm.provider, ''), COALESCE(cm.bucket, ''), COALESCE(cm.object_key, ''),
       COALESCE(pm.provider, ''), COALESCE(pm.bucket, ''), COALESCE(pm.object_key, ''),
       u.id, u.username, COALESCE(u.followers_count, 0),
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, '')
FROM video_actions va
JOIN videos v ON v.id = va.video_id
JOIN users u ON u.id = v.uploader_id
LEFT JOIN categories c ON c.id = v.category_id
LEFT JOIN media_objects cm ON cm.id = v.cover_media_id
LEFT JOIN media_objects pm ON pm.id = v.preview_media_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
WHERE va.user_id = ?
  AND va.action_type = 'favorite'
  AND v.status != 'deleted'
  AND ((v.status = 'published' AND COALESCE(v.visibility, 'public') = 'public') OR (? = 1 AND v.uploader_id = ?))
`
	args := []interface{}{targetUserID, boolToInt64(isOwner), targetUserID}

	if cursor != "" {
		var cur timeCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (va.created_at < ? OR (va.created_at = ? AND v.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY va.created_at DESC, v.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list favorites")
	}
	defer rows.Close()

	type itemWithSort struct {
		SortAt string
		ID     string
		Item   map[string]interface{}
	}
	items := make([]itemWithSort, 0)
	for rows.Next() {
		var (
			sortAt, id, title, status, visibility, publishedAt, category string
			durationSec, viewsCount, commentsCount                       int64
			coverProvider, coverBucket, coverObjectKey                   string
			previewProvider, previewBucket, previewObjectKey             string
			authorID, authorName                                         string
			authorFollowers                                              int64
			authorProvider, authorBucket, authorObjectKey                string
		)
		if err := rows.Scan(
			&sortAt,
			&id,
			&title,
			&status,
			&visibility,
			&durationSec,
			&viewsCount,
			&commentsCount,
			&publishedAt,
			&category,
			&coverProvider,
			&coverBucket,
			&coverObjectKey,
			&previewProvider,
			&previewBucket,
			&previewObjectKey,
			&authorID,
			&authorName,
			&authorFollowers,
			&authorProvider,
			&authorBucket,
			&authorObjectKey,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse favorites")
		}

		items = append(items, itemWithSort{
			SortAt: sortAt,
			ID:     id,
			Item: map[string]interface{}{
				"id":               id,
				"title":            title,
				"status":           status,
				"visibility":       visibility,
				"cover_url":        mediaURL(h.app.Storage, coverProvider, coverBucket, coverObjectKey),
				"preview_webp_url": mediaURL(h.app.Storage, previewProvider, previewBucket, previewObjectKey),
				"duration_sec":     durationSec,
				"views_count":      viewsCount,
				"comments_count":   commentsCount,
				"published_at":     publishedAt,
				"category":         category,
				"author": map[string]interface{}{
					"id":              authorID,
					"username":        authorName,
					"followers_count": authorFollowers,
					"avatar_url":      mediaURL(h.app.Storage, authorProvider, authorBucket, authorObjectKey),
				},
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list favorites")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(timeCursor{SortAt: last.SortAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, item.Item)
	}
	return response.OK(c, fiber.Map{"items": payload, "next_cursor": nextCursor})
}

func (h *Handler) ListUserFollowing(c *fiber.Ctx) error {
	targetUserID := strings.TrimSpace(c.Params("userId"))
	if targetUserID == "" {
		return response.Error(c, fiber.StatusBadRequest, "userId is required")
	}
	viewerID := currentUserID(c)
	privacy, err := h.loadUserProfilePrivacy(c.UserContext(), targetUserID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "user not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load user")
	}
	if !h.canViewProfileSection(viewerID, targetUserID, "following", privacy) {
		return response.Error(c, fiber.StatusForbidden, "profile content is private")
	}

	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT f.created_at,
       u.id, u.username, u.bio, COALESCE(u.followers_count, 0), COALESCE(u.following_count, 0),
       COALESCE(m.provider, ''), COALESCE(m.bucket, ''), COALESCE(m.object_key, ''),
       CASE WHEN ? <> '' THEN EXISTS (
         SELECT 1 FROM follows fx WHERE fx.follower_id = ? AND fx.followee_id = u.id
       ) ELSE 0 END AS followed
FROM follows f
JOIN users u ON u.id = f.followee_id
LEFT JOIN media_objects m ON m.id = u.avatar_media_id
WHERE f.follower_id = ? AND u.status = 'active'
`
	args := []interface{}{viewerID, viewerID, targetUserID}
	if cursor != "" {
		var cur timeCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (f.created_at < ? OR (f.created_at = ? AND u.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY f.created_at DESC, u.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list following")
	}
	defer rows.Close()

	type userWithSort struct {
		SortAt string
		ID     string
		Item   map[string]interface{}
	}
	items := make([]userWithSort, 0)
	for rows.Next() {
		var (
			sortAt, id, username, bio                string
			followersCount, followingCount, followed int64
			avatarProvider, avatarBucket, avatarKey  string
		)
		if err := rows.Scan(
			&sortAt,
			&id,
			&username,
			&bio,
			&followersCount,
			&followingCount,
			&avatarProvider,
			&avatarBucket,
			&avatarKey,
			&followed,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse following")
		}
		items = append(items, userWithSort{
			SortAt: sortAt,
			ID:     id,
			Item: map[string]interface{}{
				"id":              id,
				"username":        username,
				"bio":             bio,
				"followers_count": followersCount,
				"following_count": followingCount,
				"avatar_url":      mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarKey),
				"followed":        followed > 0,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list following")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(timeCursor{SortAt: last.SortAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, item.Item)
	}
	return response.OK(c, fiber.Map{"items": payload, "next_cursor": nextCursor})
}

func (h *Handler) ListUserFollowers(c *fiber.Ctx) error {
	targetUserID := strings.TrimSpace(c.Params("userId"))
	if targetUserID == "" {
		return response.Error(c, fiber.StatusBadRequest, "userId is required")
	}
	viewerID := currentUserID(c)
	privacy, err := h.loadUserProfilePrivacy(c.UserContext(), targetUserID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "user not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to load user")
	}
	if !h.canViewProfileSection(viewerID, targetUserID, "followers", privacy) {
		return response.Error(c, fiber.StatusForbidden, "profile content is private")
	}

	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT f.created_at,
       u.id, u.username, u.bio, COALESCE(u.followers_count, 0), COALESCE(u.following_count, 0),
       COALESCE(m.provider, ''), COALESCE(m.bucket, ''), COALESCE(m.object_key, ''),
       CASE WHEN ? <> '' THEN EXISTS (
         SELECT 1 FROM follows fx WHERE fx.follower_id = ? AND fx.followee_id = u.id
       ) ELSE 0 END AS followed
FROM follows f
JOIN users u ON u.id = f.follower_id
LEFT JOIN media_objects m ON m.id = u.avatar_media_id
WHERE f.followee_id = ? AND u.status = 'active'
`
	args := []interface{}{viewerID, viewerID, targetUserID}
	if cursor != "" {
		var cur timeCursor
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (f.created_at < ? OR (f.created_at = ? AND u.id < ?))`
		args = append(args, cur.SortAt, cur.SortAt, cur.ID)
	}
	query += ` ORDER BY f.created_at DESC, u.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list followers")
	}
	defer rows.Close()

	type userWithSort struct {
		SortAt string
		ID     string
		Item   map[string]interface{}
	}
	items := make([]userWithSort, 0)
	for rows.Next() {
		var (
			sortAt, id, username, bio                string
			followersCount, followingCount, followed int64
			avatarProvider, avatarBucket, avatarKey  string
		)
		if err := rows.Scan(
			&sortAt,
			&id,
			&username,
			&bio,
			&followersCount,
			&followingCount,
			&avatarProvider,
			&avatarBucket,
			&avatarKey,
			&followed,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse followers")
		}
		items = append(items, userWithSort{
			SortAt: sortAt,
			ID:     id,
			Item: map[string]interface{}{
				"id":              id,
				"username":        username,
				"bio":             bio,
				"followers_count": followersCount,
				"following_count": followingCount,
				"avatar_url":      mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarKey),
				"followed":        followed > 0,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list followers")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(timeCursor{SortAt: last.SortAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		payload = append(payload, item.Item)
	}
	return response.OK(c, fiber.Map{"items": payload, "next_cursor": nextCursor})
}

func (h *Handler) scanMyVideoCards(rows *sql.Rows) ([]map[string]interface{}, error) {
	cards := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			id, title, status, visibility, publishedAt, category string
			durationSec, viewsCount, commentsCount               int64
			coverProvider, coverBucket, coverObjectKey           string
			previewProvider, previewBucket, previewObjectKey     string
			authorID, authorName                                 string
			authorFollowers                                      int64
			authorProvider, authorBucket, authorObjectKey        string
		)
		if err := rows.Scan(
			&id,
			&title,
			&status,
			&visibility,
			&durationSec,
			&viewsCount,
			&commentsCount,
			&publishedAt,
			&category,
			&coverProvider,
			&coverBucket,
			&coverObjectKey,
			&previewProvider,
			&previewBucket,
			&previewObjectKey,
			&authorID,
			&authorName,
			&authorFollowers,
			&authorProvider,
			&authorBucket,
			&authorObjectKey,
		); err != nil {
			return nil, err
		}
		cards = append(cards, map[string]interface{}{
			"id":               id,
			"title":            title,
			"status":           status,
			"visibility":       visibility,
			"cover_url":        mediaURL(h.app.Storage, coverProvider, coverBucket, coverObjectKey),
			"preview_webp_url": mediaURL(h.app.Storage, previewProvider, previewBucket, previewObjectKey),
			"duration_sec":     durationSec,
			"views_count":      viewsCount,
			"comments_count":   commentsCount,
			"published_at":     publishedAt,
			"category":         category,
			"author": map[string]interface{}{
				"id":              authorID,
				"username":        authorName,
				"followers_count": authorFollowers,
				"avatar_url":      mediaURL(h.app.Storage, authorProvider, authorBucket, authorObjectKey),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cards, nil
}

func (h *Handler) scanVideoCards(rows *sql.Rows) ([]map[string]interface{}, error) {
	cards := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			id, title, publishedAt, category                 string
			durationSec, viewsCount, commentsCount           int64
			coverProvider, coverBucket, coverObjectKey       string
			previewProvider, previewBucket, previewObjectKey string
			authorID, authorName                             string
			authorFollowers                                  int64
			authorProvider, authorBucket, authorObjectKey    string
		)
		if err := rows.Scan(
			&id,
			&title,
			&durationSec,
			&viewsCount,
			&commentsCount,
			&publishedAt,
			&category,
			&coverProvider,
			&coverBucket,
			&coverObjectKey,
			&previewProvider,
			&previewBucket,
			&previewObjectKey,
			&authorID,
			&authorName,
			&authorFollowers,
			&authorProvider,
			&authorBucket,
			&authorObjectKey,
		); err != nil {
			return nil, err
		}
		cards = append(cards, map[string]interface{}{
			"id":               id,
			"title":            title,
			"cover_url":        mediaURL(h.app.Storage, coverProvider, coverBucket, coverObjectKey),
			"preview_webp_url": mediaURL(h.app.Storage, previewProvider, previewBucket, previewObjectKey),
			"duration_sec":     durationSec,
			"views_count":      viewsCount,
			"comments_count":   commentsCount,
			"published_at":     publishedAt,
			"category":         category,
			"author": map[string]interface{}{
				"id":              authorID,
				"username":        authorName,
				"followers_count": authorFollowers,
				"avatar_url":      mediaURL(h.app.Storage, authorProvider, authorBucket, authorObjectKey),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cards, nil
}
