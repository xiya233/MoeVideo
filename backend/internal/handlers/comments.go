package handlers

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
)

type createCommentRequest struct {
	Content         string `json:"content"`
	ParentCommentID string `json:"parent_comment_id"`
}

func (h *Handler) ListComments(c *fiber.Ctx) error {
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
	if !canReadVideo(visible, viewerID) {
		return response.Error(c, fiber.StatusNotFound, "video not found")
	}
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursor := c.Query("cursor")

	query := `
SELECT cm.id, cm.video_id, cm.user_id, cm.parent_comment_id, cm.content, cm.like_count, cm.created_at,
       u.username, u.bio,
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, ''),
       CASE WHEN ? <> '' THEN EXISTS(
         SELECT 1 FROM comment_likes cl WHERE cl.user_id = ? AND cl.comment_id = cm.id
       ) ELSE 0 END AS liked
FROM comments cm
JOIN users u ON u.id = cm.user_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
WHERE cm.video_id = ? AND cm.parent_comment_id IS NULL
`
	args := []interface{}{viewerID, viewerID, videoID}

	var cur listCursor
	if cursor != "" {
		if err := pagination.Decode(cursor, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		query += ` AND (cm.created_at < ? OR (cm.created_at = ? AND cm.id < ?))`
		args = append(args, cur.PublishedAt, cur.PublishedAt, cur.ID)
	}
	query += ` ORDER BY cm.created_at DESC, cm.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to fetch comments")
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			id, rowVideoID, userID, content, createdAt string
			parentID                                   sql.NullString
			likeCount                                  int64
			likedInt                                   int64
			username, bio                              string
			avatarProvider, avatarBucket, avatarObject string
		)
		if err := rows.Scan(
			&id,
			&rowVideoID,
			&userID,
			&parentID,
			&content,
			&likeCount,
			&createdAt,
			&username,
			&bio,
			&avatarProvider,
			&avatarBucket,
			&avatarObject,
			&likedInt,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse comments")
		}

		replies, err := h.fetchReplies(c, id, viewerID)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to fetch comment replies")
		}

		items = append(items, map[string]interface{}{
			"id":                id,
			"video_id":          rowVideoID,
			"user":              map[string]interface{}{"id": userID, "username": username, "bio": bio, "avatar_url": mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarObject)},
			"content":           content,
			"like_count":        likeCount,
			"liked":             likedInt > 0,
			"created_at":        createdAt,
			"parent_comment_id": maybeStringPtr(parentID),
			"replies":           replies,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to read comments")
	}

	nextCursor := ""
	if len(items) > limit {
		last := items[limit-1]
		items = items[:limit]
		encoded, err := pagination.Encode(listCursor{PublishedAt: last["created_at"].(string), ID: last["id"].(string)})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode cursor")
		}
		nextCursor = encoded
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) fetchReplies(c *fiber.Ctx, parentCommentID, viewerID string) ([]map[string]interface{}, error) {
	rows, err := h.app.DB.QueryContext(c.UserContext(), `
SELECT cm.id, cm.video_id, cm.user_id, cm.parent_comment_id, cm.content, cm.like_count, cm.created_at,
       u.username, u.bio,
       COALESCE(am.provider, ''), COALESCE(am.bucket, ''), COALESCE(am.object_key, ''),
       CASE WHEN ? <> '' THEN EXISTS(
         SELECT 1 FROM comment_likes cl WHERE cl.user_id = ? AND cl.comment_id = cm.id
       ) ELSE 0 END AS liked
FROM comments cm
JOIN users u ON u.id = cm.user_id
LEFT JOIN media_objects am ON am.id = u.avatar_media_id
WHERE cm.parent_comment_id = ?
ORDER BY cm.created_at ASC, cm.id ASC`, viewerID, viewerID, parentCommentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	replies := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			id, videoID, userID, content, createdAt    string
			parentID                                   sql.NullString
			likeCount                                  int64
			likedInt                                   int64
			username, bio                              string
			avatarProvider, avatarBucket, avatarObject string
		)
		if err := rows.Scan(
			&id,
			&videoID,
			&userID,
			&parentID,
			&content,
			&likeCount,
			&createdAt,
			&username,
			&bio,
			&avatarProvider,
			&avatarBucket,
			&avatarObject,
			&likedInt,
		); err != nil {
			return nil, err
		}
		replies = append(replies, map[string]interface{}{
			"id":                id,
			"video_id":          videoID,
			"user":              map[string]interface{}{"id": userID, "username": username, "bio": bio, "avatar_url": mediaURL(h.app.Storage, avatarProvider, avatarBucket, avatarObject)},
			"content":           content,
			"like_count":        likeCount,
			"liked":             likedInt > 0,
			"created_at":        createdAt,
			"parent_comment_id": maybeStringPtr(parentID),
			"replies":           []map[string]interface{}{},
		})
	}
	return replies, rows.Err()
}

func (h *Handler) CreateComment(c *fiber.Ctx) error {
	uid := currentUserID(c)
	videoID := strings.TrimSpace(c.Params("videoId"))
	if videoID == "" {
		return response.Error(c, fiber.StatusBadRequest, "videoId is required")
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

	var req createCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.Content = strings.TrimSpace(req.Content)
	req.ParentCommentID = strings.TrimSpace(req.ParentCommentID)
	if req.Content == "" {
		return response.Error(c, fiber.StatusBadRequest, "content is required")
	}
	if len(req.Content) > 2000 {
		return response.Error(c, fiber.StatusBadRequest, "content exceeds 2000 characters")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	if req.ParentCommentID != "" {
		var parentVideoID string
		var parentParent sql.NullString
		if err := tx.QueryRowContext(c.UserContext(), `SELECT video_id, parent_comment_id FROM comments WHERE id = ?`, req.ParentCommentID).Scan(&parentVideoID, &parentParent); err != nil {
			if isNotFound(err) {
				return response.Error(c, fiber.StatusBadRequest, "parent_comment_id not found")
			}
			return response.Error(c, fiber.StatusInternalServerError, "failed to validate parent comment")
		}
		if parentVideoID != videoID {
			return response.Error(c, fiber.StatusBadRequest, "parent_comment_id does not belong to the video")
		}
		if parentParent.Valid {
			return response.Error(c, fiber.StatusBadRequest, "nested replies are not allowed")
		}
	}

	commentID := newID()
	_, err = tx.ExecContext(c.UserContext(),
		`INSERT INTO comments (id, video_id, user_id, parent_comment_id, content, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`,
		commentID,
		videoID,
		uid,
		nullableString(req.ParentCommentID),
		req.Content,
		nowString(),
		nowString(),
	)
	if err != nil {
		if isNestedReplyErr(err) {
			return response.Error(c, fiber.StatusBadRequest, "nested replies are not allowed")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to create comment")
	}
	if req.ParentCommentID != "" {
		if _, err := tx.ExecContext(c.UserContext(), `UPDATE comments SET reply_count = reply_count + 1, updated_at = ? WHERE id = ?`, nowString(), req.ParentCommentID); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update parent reply count")
		}
	}
	if _, err := tx.ExecContext(c.UserContext(), `UPDATE videos SET comments_count = comments_count + 1, updated_at = ? WHERE id = ?`, nowString(), videoID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update comments count")
	}
	if err := h.recomputeHotScoreTx(c.UserContext(), tx, videoID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update hot score")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit comment")
	}
	return response.Created(c, fiber.Map{"id": commentID})
}

func (h *Handler) ToggleCommentLike(c *fiber.Ctx) error {
	uid := currentUserID(c)
	commentID := strings.TrimSpace(c.Params("commentId"))
	if commentID == "" {
		return response.Error(c, fiber.StatusBadRequest, "commentId is required")
	}

	var req toggleRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	var visible videoVisibility
	if err := h.app.DB.QueryRowContext(c.UserContext(), `
SELECT v.uploader_id, v.status, COALESCE(v.visibility, 'public'), COALESCE(v.duration_sec, 0)
FROM comments c
JOIN videos v ON v.id = c.video_id
WHERE c.id = ?
LIMIT 1`, commentID).Scan(&visible.UploaderID, &visible.Status, &visible.Visibility, &visible.Duration); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "comment not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate comment")
	}
	if !canReadVideo(visible, uid) {
		return response.Error(c, fiber.StatusNotFound, "comment not found")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	if req.Active {
		res, err := tx.ExecContext(c.UserContext(), `INSERT OR IGNORE INTO comment_likes (user_id, comment_id, created_at) VALUES (?, ?, ?)`, uid, commentID, nowString())
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to like comment")
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(c.UserContext(), `UPDATE comments SET like_count = like_count + 1, updated_at = ? WHERE id = ?`, nowString(), commentID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update comment like count")
			}
		}
	} else {
		res, err := tx.ExecContext(c.UserContext(), `DELETE FROM comment_likes WHERE user_id = ? AND comment_id = ?`, uid, commentID)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to unlike comment")
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := tx.ExecContext(c.UserContext(), `UPDATE comments SET like_count = CASE WHEN like_count > 0 THEN like_count - 1 ELSE 0 END, updated_at = ? WHERE id = ?`, nowString(), commentID); err != nil {
				return response.Error(c, fiber.StatusInternalServerError, "failed to update comment like count")
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit comment like")
	}
	return response.OK(c, fiber.Map{"active": req.Active})
}

func (h *Handler) DeleteComment(c *fiber.Ctx) error {
	uid := currentUserID(c)
	commentID := strings.TrimSpace(c.Params("commentId"))
	if commentID == "" {
		return response.Error(c, fiber.StatusBadRequest, "commentId is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var (
		videoID, ownerID, status string
		parentID                 sql.NullString
	)
	if err := tx.QueryRowContext(c.UserContext(), `SELECT video_id, user_id, parent_comment_id, status FROM comments WHERE id = ?`, commentID).Scan(&videoID, &ownerID, &parentID, &status); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "comment not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to query comment")
	}
	if ownerID != uid {
		return response.Error(c, fiber.StatusForbidden, "cannot delete others' comments")
	}
	if status == "deleted" {
		return response.OK(c, fiber.Map{"deleted": true})
	}

	if _, err := tx.ExecContext(c.UserContext(), `UPDATE comments SET status = 'deleted', content = '[deleted]', updated_at = ? WHERE id = ?`, nowString(), commentID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to delete comment")
	}
	if parentID.Valid {
		if _, err := tx.ExecContext(c.UserContext(), `UPDATE comments SET reply_count = CASE WHEN reply_count > 0 THEN reply_count - 1 ELSE 0 END, updated_at = ? WHERE id = ?`, nowString(), parentID.String); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to update parent reply count")
		}
	}
	if _, err := tx.ExecContext(c.UserContext(), `UPDATE videos SET comments_count = CASE WHEN comments_count > 0 THEN comments_count - 1 ELSE 0 END, updated_at = ? WHERE id = ?`, nowString(), videoID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update comment count")
	}
	if err := h.recomputeHotScoreTx(c.UserContext(), tx, videoID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update hot score")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit delete")
	}
	return response.OK(c, fiber.Map{"deleted": true})
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	items := make([]string, n)
	for i := 0; i < n; i++ {
		items[i] = "?"
	}
	return strings.Join(items, ",")
}

func inArgs(values []string) []interface{} {
	args := make([]interface{}, len(values))
	for i, v := range values {
		args[i] = v
	}
	return args
}

func commentsError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}
