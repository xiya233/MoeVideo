package handlers

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"moevideo/backend/internal/middleware"
	"moevideo/backend/internal/models"
	"moevideo/backend/internal/storage"
	"moevideo/backend/internal/util"
)

const (
	defaultLimit = 20
	maxLimit     = 50
)

type toggleRequest struct {
	Active bool `json:"active"`
}

func newID() string {
	return uuid.NewString()
}

func nowUTC() time.Time {
	return util.NowUTC()
}

func nowString() string {
	return util.FormatTime(nowUTC())
}

func currentUserID(c *fiber.Ctx) string {
	return middleware.CurrentUserID(c)
}

func currentUsername(c *fiber.Ctx) string {
	return middleware.CurrentUsername(c)
}

func maybeInt64(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}

func maybeString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func maybeStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func isConflictErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}

func isNestedReplyErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "nested replies")
}

func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func computeHotScore(views, likes, favorites, comments, shares int64) float64 {
	return float64(views) + float64(likes*5) + float64(favorites*4) + float64(comments*3) + float64(shares*4)
}

func mediaURL(s *storage.Service, provider, bucket, objectKey string) string {
	if objectKey == "" {
		return ""
	}
	return s.ObjectURL(provider, bucket, objectKey)
}

func fetchUserBrief(db *sql.DB, s *storage.Service, userID string, includeEmail bool) (models.UserBrief, error) {
	var u models.UserBrief
	var provider, bucket, objectKey sql.NullString
	row := db.QueryRow(`SELECT u.id, u.username, u.email, COALESCE(u.role, 'user'), u.bio, u.followers_count, u.following_count,
		COALESCE(m.provider,''), COALESCE(m.bucket,''), COALESCE(m.object_key,'')
		FROM users u
		LEFT JOIN media_objects m ON m.id = u.avatar_media_id
		WHERE u.id = ?`, userID)
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Bio, &u.FollowersCount, &u.FollowingCount, &provider, &bucket, &objectKey); err != nil {
		return models.UserBrief{}, err
	}
	if !includeEmail {
		u.Email = ""
		u.Role = ""
	}
	u.AvatarURL = mediaURL(s, maybeString(provider), maybeString(bucket), maybeString(objectKey))
	return u, nil
}
