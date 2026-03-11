package handlers

import (
	"database/sql"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
)

type followRequest struct {
	Active bool `json:"active"`
}

type listCursor struct {
	PublishedAt string `json:"published_at"`
	ID          string `json:"id"`
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
	if viewerID != "" && viewerID != userID {
		row := h.app.DB.QueryRow(`SELECT 1 FROM follows WHERE follower_id = ? AND followee_id = ? LIMIT 1`, viewerID, userID)
		var tmp int
		if err := row.Scan(&tmp); err == nil {
			followed = true
		} else if !isNotFound(err) {
			return response.Error(c, fiber.StatusInternalServerError, "failed to fetch follow status")
		}
	}

	return response.OK(c, fiber.Map{
		"user":     user,
		"followed": followed,
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
SELECT v.id, v.title, v.duration_sec, v.views_count, v.comments_count, COALESCE(v.published_at, v.created_at),
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

	cards, err := h.scanVideoCards(rows)
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
