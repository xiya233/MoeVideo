package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"moevideo/backend/internal/media"
	"moevideo/backend/internal/response"
)

type createLiveSessionRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	CategoryID  *int64   `json:"category_id"`
	Tags        []string `json:"tags"`
	Visibility  string   `json:"visibility"`
}

type liveSessionRow struct {
	ID           string
	UserID       string
	VideoID      string
	StreamKey    string
	AppName      string
	Title        string
	Description  string
	CategoryID   sql.NullInt64
	TagsJSON     string
	Visibility   string
	Status       string
	PublishURL   string
	PlaybackURL  string
	RecordPath   string
	StartedAt    string
	EndedAt      string
	LastError    string
	CreatedAt    string
	UpdatedAt    string
	VideoIsLive  bool
	VideoLiveURL string
	VideoStatus  string
}

func (h *Handler) CreateLiveSession(c *fiber.Ctx) error {
	if !h.app.Config.LiveEnabled {
		return response.Error(c, fiber.StatusServiceUnavailable, "live streaming is disabled")
	}

	uid := currentUserID(c)
	var req createLiveSessionRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		return response.Error(c, fiber.StatusBadRequest, "title is required")
	}
	if len([]rune(title)) > 120 {
		return response.Error(c, fiber.StatusBadRequest, "title is too long")
	}
	if req.CategoryID == nil {
		return response.Error(c, fiber.StatusBadRequest, "category_id is required")
	}
	visibility := normalizeImportVisibility(req.Visibility)
	if visibility == "" {
		return response.Error(c, fiber.StatusBadRequest, "invalid visibility")
	}
	description, err := normalizeImportCustomDescription(req.Description)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	tags := normalizeImportTags(req.Tags)
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to encode tags")
	}

	now := nowString()
	sessionID := newID()
	videoID := uuid.NewString()
	mediaID := uuid.NewString()
	streamKey := strings.ReplaceAll(newID(), "-", "")
	appName := strings.TrimSpace(h.app.Config.LiveAppName)
	publishURL := buildLivePublishURL(h.app.Config.LiveRTMPServerURL, appName)
	playbackURL := buildLivePlaybackURL(h.app.Config.LivePlaybackBaseURL, streamKey)
	objectKey := fmt.Sprintf("live/%s/%s/source-placeholder.txt", uid, sessionID)

	tmpFile, err := os.CreateTemp(h.app.Config.TaskTempDir, "live-placeholder-*")
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to prepare live placeholder")
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString("live-placeholder"); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return response.Error(c, fiber.StatusInternalServerError, "failed to prepare live placeholder")
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return response.Error(c, fiber.StatusInternalServerError, "failed to prepare live placeholder")
	}
	defer os.Remove(tmpPath)

	if err := h.app.Storage.UploadFile(c.UserContext(), objectKey, "text/plain", tmpPath); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to initialize live source")
	}
	stat, err := os.Stat(tmpPath)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to initialize live source")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var activeSessionID string
	err = tx.QueryRowContext(c.UserContext(), `
SELECT id
FROM live_sessions
WHERE user_id = ?
  AND status IN ('waiting', 'live')
ORDER BY created_at DESC
LIMIT 1`, uid).Scan(&activeSessionID)
	if err == nil {
		return response.Error(c, fiber.StatusConflict, "there is already an active live session")
	}
	if err != nil && !isNotFound(err) {
		return response.Error(c, fiber.StatusInternalServerError, "failed to check active live session")
	}

	var categoryExists int
	if err := tx.QueryRowContext(c.UserContext(), `SELECT 1 FROM categories WHERE id = ? AND is_active = 1 LIMIT 1`, *req.CategoryID).Scan(&categoryExists); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusBadRequest, "category_id is invalid")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate category")
	}

	provider := h.app.Storage.Driver()
	bucket := ""
	if provider == "s3" {
		bucket = h.app.Storage.Bucket()
	}
	if _, err := tx.ExecContext(c.UserContext(), `
INSERT INTO media_objects (
	id, provider, bucket, object_key, original_filename, mime_type, size_bytes,
	checksum_sha256, duration_sec, width, height, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, 0, NULL, NULL, ?, ?)`,
		mediaID,
		provider,
		nullableString(bucket),
		objectKey,
		"source-placeholder.txt",
		"text/plain",
		stat.Size(),
		uid,
		now,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create live source media")
	}

	if _, err := tx.ExecContext(c.UserContext(), `
INSERT INTO videos (
	id, uploader_id, title, description, category_id, cover_media_id, source_media_id,
	status, visibility, duration_sec, published_at, views_count, likes_count,
	favorites_count, comments_count, shares_count, hot_score,
	is_live, live_hls_url, live_started_at,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, NULL, ?,
	'published', ?, 0, ?, 0, 0,
	0, 0, 0, 0,
	1, ?, ?,
	?, ?)`,
		videoID,
		uid,
		title,
		description,
		req.CategoryID,
		mediaID,
		visibility,
		now,
		playbackURL,
		now,
		now,
		now,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create live video")
	}

	if err := h.syncVideoTagsTx(c.UserContext(), tx, videoID, tags, now); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to save live tags")
	}

	if _, err := tx.ExecContext(c.UserContext(), `
INSERT INTO live_sessions (
	id, user_id, video_id, stream_key, app_name, title, description,
	category_id, tags_json, visibility, status,
	publish_url, playback_url, record_file_path,
	started_at, ended_at, last_error,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?,
	?, ?, ?, 'waiting',
	?, ?, NULL,
	NULL, NULL, NULL,
	?, ?)`,
		sessionID,
		uid,
		videoID,
		streamKey,
		appName,
		title,
		description,
		req.CategoryID,
		string(tagsJSON),
		visibility,
		publishURL,
		playbackURL,
		now,
		now,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create live session")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create live session")
	}

	return response.Created(c, fiber.Map{
		"session": fiber.Map{
			"id":           sessionID,
			"video_id":     videoID,
			"title":        title,
			"description":  description,
			"category_id":  req.CategoryID,
			"tags":         tags,
			"visibility":   visibility,
			"status":       "waiting",
			"stream_key":   streamKey,
			"publish_url":  publishURL,
			"playback_url": playbackURL,
			"is_live":      true,
			"created_at":   now,
			"updated_at":   now,
		},
		"obs": fiber.Map{
			"server_url":   publishURL,
			"stream_key":   streamKey,
			"stream_url":   publishURL + "/" + streamKey,
			"playback_url": playbackURL,
		},
	})
}

func (h *Handler) GetCurrentLiveSession(c *fiber.Ctx) error {
	uid := currentUserID(c)
	row, found, err := h.queryCurrentLiveSession(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load live session")
	}
	if !found {
		return response.OK(c, fiber.Map{"session": nil})
	}
	return response.OK(c, fiber.Map{"session": liveSessionToPayload(row)})
}

func (h *Handler) EndCurrentLiveSession(c *fiber.Ctx) error {
	uid := currentUserID(c)
	row, found, err := h.queryCurrentLiveSession(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load live session")
	}
	if !found {
		return response.Error(c, fiber.StatusNotFound, "active live session not found")
	}

	if _, err := h.transitionSessionEnded(c.UserContext(), row, "", true); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to end live session")
	}
	return response.OK(c, fiber.Map{"ended": true, "session_id": row.ID, "video_id": row.VideoID})
}

func (h *Handler) HandleSRSCallback(c *fiber.Ctx) error {
	if !h.app.Config.LiveEnabled {
		return response.Error(c, fiber.StatusServiceUnavailable, "live streaming is disabled")
	}

	rawBody := c.Body()
	if err := h.validateLiveCallbackSignature(c, rawBody); err != nil {
		return response.Error(c, fiber.StatusForbidden, "forbidden callback")
	}

	fields := parseLiveCallbackFields(c, rawBody)
	action := strings.ToLower(firstNonEmptyField(fields, "action", "event", "call"))
	streamKey := strings.TrimSpace(firstNonEmptyField(fields, "stream", "stream_key", "name"))
	appName := strings.TrimSpace(firstNonEmptyField(fields, "app"))
	if appName == "" {
		appName = strings.TrimSpace(h.app.Config.LiveAppName)
	}
	recordPathRaw := strings.TrimSpace(firstNonEmptyField(fields, "file", "record_file", "record_file_path"))

	if streamKey == "" {
		return response.Error(c, fiber.StatusBadRequest, "stream key is required")
	}

	switch action {
	case "on_publish", "publish":
		row, found, err := h.transitionSessionLive(c.UserContext(), appName, streamKey)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to mark live session started")
		}
		return response.OK(c, fiber.Map{"accepted": true, "found": found, "session_id": row.ID, "video_id": row.VideoID})
	case "on_unpublish", "unpublish":
		row, found, err := h.findSessionByStream(c.UserContext(), appName, streamKey)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to load live session")
		}
		if !found {
			return response.OK(c, fiber.Map{"accepted": true, "found": false})
		}
		queued, err := h.transitionSessionEnded(c.UserContext(), row, recordPathRaw, false)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to mark live session ended")
		}
		return response.OK(c, fiber.Map{"accepted": true, "found": true, "replay_queued": queued})
	case "on_record", "record":
		row, found, err := h.findSessionByStream(c.UserContext(), appName, streamKey)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to load live session")
		}
		if !found {
			return response.OK(c, fiber.Map{"accepted": true, "found": false})
		}
		queued, err := h.transitionSessionEnded(c.UserContext(), row, recordPathRaw, false)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to process live record")
		}
		return response.OK(c, fiber.Map{"accepted": true, "found": true, "replay_queued": queued})
	default:
		return response.OK(c, fiber.Map{"accepted": true, "ignored": true})
	}
}

func (h *Handler) queryCurrentLiveSession(ctx context.Context, userID string) (liveSessionRow, bool, error) {
	row := liveSessionRow{}
	err := h.app.DB.QueryRowContext(ctx, `
SELECT ls.id, ls.user_id, ls.video_id, ls.stream_key, ls.app_name,
       ls.title, ls.description, ls.category_id, ls.tags_json, ls.visibility, ls.status,
       ls.publish_url, ls.playback_url, COALESCE(ls.record_file_path, ''),
       COALESCE(ls.started_at, ''), COALESCE(ls.ended_at, ''), COALESCE(ls.last_error, ''),
       ls.created_at, ls.updated_at,
       COALESCE(v.is_live, 0), COALESCE(v.live_hls_url, ''), COALESCE(v.status, '')
FROM live_sessions ls
LEFT JOIN videos v ON v.id = ls.video_id
WHERE ls.user_id = ?
  AND ls.status IN ('waiting', 'live')
ORDER BY ls.created_at DESC
LIMIT 1`, userID).Scan(
		&row.ID,
		&row.UserID,
		&row.VideoID,
		&row.StreamKey,
		&row.AppName,
		&row.Title,
		&row.Description,
		&row.CategoryID,
		&row.TagsJSON,
		&row.Visibility,
		&row.Status,
		&row.PublishURL,
		&row.PlaybackURL,
		&row.RecordPath,
		&row.StartedAt,
		&row.EndedAt,
		&row.LastError,
		&row.CreatedAt,
		&row.UpdatedAt,
		&row.VideoIsLive,
		&row.VideoLiveURL,
		&row.VideoStatus,
	)
	if err != nil {
		if isNotFound(err) {
			return liveSessionRow{}, false, nil
		}
		return liveSessionRow{}, false, err
	}
	return row, true, nil
}

func (h *Handler) findSessionByStream(ctx context.Context, appName, streamKey string) (liveSessionRow, bool, error) {
	row := liveSessionRow{}
	err := h.app.DB.QueryRowContext(ctx, `
SELECT ls.id, ls.user_id, ls.video_id, ls.stream_key, ls.app_name,
       ls.title, ls.description, ls.category_id, ls.tags_json, ls.visibility, ls.status,
       ls.publish_url, ls.playback_url, COALESCE(ls.record_file_path, ''),
       COALESCE(ls.started_at, ''), COALESCE(ls.ended_at, ''), COALESCE(ls.last_error, ''),
       ls.created_at, ls.updated_at,
       COALESCE(v.is_live, 0), COALESCE(v.live_hls_url, ''), COALESCE(v.status, '')
FROM live_sessions ls
LEFT JOIN videos v ON v.id = ls.video_id
WHERE ls.app_name = ?
  AND ls.stream_key = ?
ORDER BY ls.created_at DESC
LIMIT 1`, appName, streamKey).Scan(
		&row.ID,
		&row.UserID,
		&row.VideoID,
		&row.StreamKey,
		&row.AppName,
		&row.Title,
		&row.Description,
		&row.CategoryID,
		&row.TagsJSON,
		&row.Visibility,
		&row.Status,
		&row.PublishURL,
		&row.PlaybackURL,
		&row.RecordPath,
		&row.StartedAt,
		&row.EndedAt,
		&row.LastError,
		&row.CreatedAt,
		&row.UpdatedAt,
		&row.VideoIsLive,
		&row.VideoLiveURL,
		&row.VideoStatus,
	)
	if err != nil {
		if isNotFound(err) {
			return liveSessionRow{}, false, nil
		}
		return liveSessionRow{}, false, err
	}
	return row, true, nil
}

func (h *Handler) transitionSessionLive(ctx context.Context, appName, streamKey string) (liveSessionRow, bool, error) {
	row, found, err := h.findSessionByStream(ctx, appName, streamKey)
	if err != nil || !found {
		return row, found, err
	}
	if row.Status == "ended" || row.Status == "failed" {
		return row, true, nil
	}

	now := nowString()
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return liveSessionRow{}, false, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
UPDATE live_sessions
SET status = 'live',
	started_at = COALESCE(started_at, ?),
	updated_at = ?
WHERE id = ?`, now, now, row.ID); err != nil {
		return liveSessionRow{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET status = 'published',
	is_live = 1,
	live_hls_url = ?,
	live_started_at = COALESCE(live_started_at, ?),
	updated_at = ?
WHERE id = ? AND status != 'deleted'`, row.PlaybackURL, now, now, row.VideoID); err != nil {
		return liveSessionRow{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return liveSessionRow{}, false, err
	}

	row.Status = "live"
	row.StartedAt = now
	row.VideoIsLive = true
	row.VideoLiveURL = row.PlaybackURL
	row.VideoStatus = "published"
	return row, true, nil
}

func (h *Handler) transitionSessionEnded(ctx context.Context, row liveSessionRow, recordPathRaw string, manual bool) (bool, error) {
	if row.Status == "failed" {
		return false, nil
	}
	if row.Status == "ended" && strings.TrimSpace(row.RecordPath) != "" {
		return false, nil
	}
	recordPath, err := h.resolveLiveRecordPath(recordPathRaw)
	if err != nil {
		return false, err
	}
	if recordPath == "" {
		recordPath = strings.TrimSpace(row.RecordPath)
	}

	now := nowString()
	videoStatus := "processing"
	sessionStatus := "ended"
	lastError := ""
	if manual && strings.TrimSpace(recordPath) == "" {
		videoStatus = "failed"
		sessionStatus = "failed"
		lastError = "live session ended manually without record file"
	}

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
UPDATE live_sessions
SET status = ?,
	record_file_path = COALESCE(NULLIF(?, ''), record_file_path),
	ended_at = COALESCE(ended_at, ?),
	last_error = NULLIF(?, ''),
	updated_at = ?
WHERE id = ?`,
		sessionStatus,
		recordPath,
		now,
		lastError,
		now,
		row.ID,
	); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET status = ?,
	is_live = 0,
	live_hls_url = NULL,
	live_started_at = NULL,
	updated_at = ?
WHERE id = ? AND status != 'deleted'`, videoStatus, now, row.VideoID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}

	if strings.TrimSpace(recordPath) == "" {
		return false, nil
	}
	if err := h.queueReplayFromRecord(ctx, row, recordPath); err != nil {
		_ = h.markLiveReplayFailure(ctx, row.ID, row.VideoID, err)
		return false, err
	}
	return true, nil
}

func (h *Handler) queueReplayFromRecord(ctx context.Context, row liveSessionRow, recordPath string) error {
	recordPath = strings.TrimSpace(recordPath)
	if recordPath == "" {
		return fmt.Errorf("record file path is empty")
	}
	stat, err := os.Stat(recordPath)
	if err != nil {
		return fmt.Errorf("stat record file: %w", err)
	}
	if stat.IsDir() {
		return fmt.Errorf("record file is a directory")
	}
	if stat.Size() <= 0 {
		return fmt.Errorf("record file is empty")
	}

	ext := strings.ToLower(filepath.Ext(recordPath))
	if ext == "" {
		ext = ".mp4"
	}
	objectKey := fmt.Sprintf("live-replay/%s/%s/source%s", row.UserID, row.VideoID, ext)
	contentType := replayContentTypeByExt(ext)
	if err := h.app.Storage.UploadFile(ctx, objectKey, contentType, recordPath); err != nil {
		return fmt.Errorf("upload replay source: %w", err)
	}

	durationSec, width, height, probeErr := media.ProbeVideoFileMetadata(ctx, h.app.Config.FFprobeBin, recordPath)
	if probeErr != nil {
		durationSec = 0
		width = 0
		height = 0
	}

	now := nowString()
	mediaID := uuid.NewString()
	provider := h.app.Storage.Driver()
	bucket := ""
	if provider == "s3" {
		bucket = h.app.Storage.Bucket()
	}

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var activeTranscodeCount int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM video_transcode_jobs
WHERE video_id = ?
  AND status IN ('queued', 'processing')`, row.VideoID).Scan(&activeTranscodeCount); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO media_objects (
	id, provider, bucket, object_key, original_filename, mime_type, size_bytes,
	checksum_sha256, duration_sec, width, height, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)`,
		mediaID,
		provider,
		nullableString(bucket),
		objectKey,
		filepath.Base(recordPath),
		contentType,
		stat.Size(),
		durationSec,
		nullableInt(width),
		nullableInt(height),
		row.UserID,
		now,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET source_media_id = ?,
	duration_sec = CASE WHEN ? > 0 THEN ? ELSE duration_sec END,
	status = 'processing',
	is_live = 0,
	live_hls_url = NULL,
	live_started_at = NULL,
	updated_at = ?
WHERE id = ?`, mediaID, durationSec, durationSec, now, row.VideoID); err != nil {
		return err
	}

	if activeTranscodeCount == 0 {
		maxTranscodeAttempts := h.app.Config.TranscodeMaxTry
		if maxTranscodeAttempts <= 0 {
			maxTranscodeAttempts = 3
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO video_transcode_jobs (
	id, video_id, status, attempts, max_attempts, last_error,
	available_at, locked_at, started_at, finished_at, created_at, updated_at
) VALUES (?, ?, 'queued', 0, ?, NULL, ?, NULL, NULL, NULL, ?, ?)`,
			newID(),
			row.VideoID,
			maxTranscodeAttempts,
			now,
			now,
			now,
		); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE live_sessions
SET status = 'ended',
	record_file_path = ?,
	last_error = NULL,
	ended_at = COALESCE(ended_at, ?),
	updated_at = ?
WHERE id = ?`, recordPath, now, now, row.ID); err != nil {
		return err
	}

	return tx.Commit()
}

func (h *Handler) markLiveReplayFailure(ctx context.Context, sessionID, videoID string, cause error) error {
	errText := truncateErr(cause, 1000)
	now := nowString()
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
UPDATE live_sessions
SET status = 'failed',
	last_error = ?,
	updated_at = ?
WHERE id = ?`, errText, now, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET status = 'failed',
	is_live = 0,
	live_hls_url = NULL,
	live_started_at = NULL,
	updated_at = ?
WHERE id = ? AND status != 'deleted'`, now, videoID); err != nil {
		return err
	}
	return tx.Commit()
}

func parseLiveCallbackFields(c *fiber.Ctx, rawBody []byte) map[string]string {
	out := make(map[string]string)
	for key, value := range c.Queries() {
		out[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}

	body := strings.TrimSpace(string(rawBody))
	if body == "" {
		return out
	}

	contentType := strings.ToLower(strings.TrimSpace(c.Get("Content-Type")))
	if strings.Contains(contentType, "application/json") || strings.HasPrefix(body, "{") {
		var payload map[string]interface{}
		if err := json.Unmarshal(rawBody, &payload); err == nil {
			for key, value := range payload {
				k := strings.ToLower(strings.TrimSpace(key))
				if k == "" {
					continue
				}
				out[k] = strings.TrimSpace(fmt.Sprintf("%v", value))
			}
			return out
		}
	}

	values, err := url.ParseQuery(body)
	if err != nil {
		return out
	}
	for key, list := range values {
		k := strings.ToLower(strings.TrimSpace(key))
		if k == "" || len(list) == 0 {
			continue
		}
		out[k] = strings.TrimSpace(list[0])
	}
	return out
}

func firstNonEmptyField(fields map[string]string, keys ...string) string {
	for _, key := range keys {
		v := strings.TrimSpace(fields[strings.ToLower(strings.TrimSpace(key))])
		if v != "" {
			return v
		}
	}
	return ""
}

func (h *Handler) validateLiveCallbackSignature(c *fiber.Ctx, rawBody []byte) error {
	secret := strings.TrimSpace(h.app.Config.LiveCallbackSecret)
	if secret == "" {
		return nil
	}

	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		token = strings.TrimSpace(c.Get("X-Live-Callback-Token"))
	}
	if token != "" && token == secret {
		return nil
	}

	signature := strings.TrimSpace(c.Get("X-Live-Signature"))
	if signature == "" {
		return fmt.Errorf("missing callback signature")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	sum := mac.Sum(nil)
	expectedHex := hex.EncodeToString(sum)
	expectedBase64 := base64.StdEncoding.EncodeToString(sum)
	if hmac.Equal([]byte(strings.ToLower(signature)), []byte(strings.ToLower(expectedHex))) {
		return nil
	}
	if hmac.Equal([]byte(signature), []byte(expectedBase64)) {
		return nil
	}
	return fmt.Errorf("invalid callback signature")
}

func buildLivePublishURL(serverURL, appName string) string {
	base := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	app := strings.Trim(strings.TrimSpace(appName), "/")
	if base == "" {
		return ""
	}
	if app == "" {
		return base
	}
	return base + "/" + app
}

func buildLivePlaybackURL(baseURL, streamKey string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	key := strings.TrimSpace(streamKey)
	if base == "" || key == "" {
		return ""
	}
	return base + "/" + key + ".m3u8"
}

func liveSessionToPayload(row liveSessionRow) fiber.Map {
	payload := fiber.Map{
		"id":            row.ID,
		"video_id":      row.VideoID,
		"title":         row.Title,
		"description":   row.Description,
		"visibility":    row.Visibility,
		"status":        row.Status,
		"stream_key":    row.StreamKey,
		"app_name":      row.AppName,
		"publish_url":   row.PublishURL,
		"playback_url":  row.PlaybackURL,
		"record_path":   nullableString(row.RecordPath),
		"started_at":    nullableString(row.StartedAt),
		"ended_at":      nullableString(row.EndedAt),
		"last_error":    nullableString(row.LastError),
		"created_at":    row.CreatedAt,
		"updated_at":    row.UpdatedAt,
		"is_live":       row.VideoIsLive,
		"video_status":  row.VideoStatus,
		"video_hls_url": nullableString(row.VideoLiveURL),
		"tags":          parseImportTags(row.TagsJSON),
	}
	if row.CategoryID.Valid {
		payload["category_id"] = row.CategoryID.Int64
	}
	return payload
}

func (h *Handler) resolveLiveRecordPath(raw string) (string, error) {
	recordPath := strings.TrimSpace(raw)
	if recordPath == "" {
		return "", nil
	}

	baseDir := filepath.Clean(strings.TrimSpace(h.app.Config.LiveRecordDir))
	if baseDir == "" {
		return "", fmt.Errorf("LIVE_RECORD_DIR is not configured")
	}

	var resolved string
	if filepath.IsAbs(recordPath) {
		resolved = filepath.Clean(recordPath)
	} else {
		resolved = filepath.Clean(filepath.Join(baseDir, recordPath))
	}

	if !filepath.IsAbs(resolved) {
		resolved = filepath.Clean(filepath.Join(baseDir, resolved))
	}

	baseWithSep := baseDir + string(os.PathSeparator)
	if resolved != baseDir && !strings.HasPrefix(resolved, baseWithSep) {
		return "", fmt.Errorf("record file is outside LIVE_RECORD_DIR")
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("record file not found: %w", err)
	}
	if stat.IsDir() {
		return "", fmt.Errorf("record path is a directory")
	}
	return resolved, nil
}

func replayContentTypeByExt(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".flv":
		return "video/x-flv"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

func truncateErr(err error, limit int) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}
