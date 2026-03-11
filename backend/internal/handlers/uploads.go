package handlers

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/response"
	"moevideo/backend/internal/util"
)

var unsafeFileChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type presignRequest struct {
	Purpose       string `json:"purpose"`
	Filename      string `json:"filename"`
	ContentType   string `json:"content_type"`
	FileSizeBytes int64  `json:"file_size_bytes"`
}

type completeUploadRequest struct {
	ChecksumSHA256 string `json:"checksum_sha256"`
	DurationSec    int64  `json:"duration_sec"`
	Width          int64  `json:"width"`
	Height         int64  `json:"height"`
}

func (h *Handler) CreateUploadPresign(c *fiber.Ctx) error {
	uid := currentUserID(c)

	var req presignRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	req.Purpose = strings.ToLower(strings.TrimSpace(req.Purpose))
	req.Filename = strings.TrimSpace(req.Filename)
	req.ContentType = strings.ToLower(strings.TrimSpace(req.ContentType))
	if req.Purpose != "video" && req.Purpose != "cover" {
		return response.Error(c, fiber.StatusBadRequest, "purpose must be video or cover")
	}
	if req.Filename == "" || req.ContentType == "" {
		return response.Error(c, fiber.StatusBadRequest, "filename and content_type are required")
	}
	if req.FileSizeBytes <= 0 || req.FileSizeBytes > h.app.Config.MaxUploadBytes {
		return response.Error(c, fiber.StatusBadRequest, "file_size_bytes is invalid")
	}
	if !isAllowedMIME(req.Purpose, req.ContentType) {
		return response.Error(c, fiber.StatusBadRequest, "content_type is not allowed")
	}

	uploadID := newID()
	uploadToken, err := util.RandomToken(32)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to generate upload token")
	}
	objectKey := buildObjectKey(req.Purpose, uid, uploadID, req.Filename)
	expiresAt := nowUTC().Add(h.app.Config.UploadURLExpires)

	uploadURL := ""
	headers := map[string]string{}
	method := "PUT"
	provider := h.app.Storage.Driver()
	if provider == "s3" {
		uploadURL, headers, err = h.app.Storage.PresignS3Put(c.UserContext(), objectKey, req.ContentType, h.app.Config.UploadURLExpires)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to create s3 presigned url")
		}
	} else {
		uploadURL = strings.TrimRight(h.app.Config.PublicBaseURL, "/") + "/api/v1/uploads/local/" + uploadToken
	}

	_, err = h.app.DB.ExecContext(c.UserContext(),
		`INSERT INTO upload_sessions (id, user_id, purpose, provider, object_key, content_type, original_filename, file_size_bytes, max_size_bytes, status, upload_token, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, 'initiated', ?, ?, ?)`,
		uploadID,
		uid,
		req.Purpose,
		provider,
		objectKey,
		req.ContentType,
		req.Filename,
		h.app.Config.MaxUploadBytes,
		uploadToken,
		util.FormatTime(expiresAt),
		nowString(),
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create upload session")
	}

	return response.Created(c, fiber.Map{
		"upload_id":      uploadID,
		"provider":       provider,
		"method":         method,
		"upload_url":     uploadURL,
		"headers":        headers,
		"object_key":     objectKey,
		"expires_at":     util.FormatTime(expiresAt),
		"max_size_bytes": h.app.Config.MaxUploadBytes,
	})
}

func (h *Handler) UploadToLocal(c *fiber.Ctx) error {
	uploadToken := strings.TrimSpace(c.Params("uploadToken"))
	if uploadToken == "" {
		return response.Error(c, fiber.StatusBadRequest, "uploadToken is required")
	}

	var (
		sessionID string
		provider  string
		objectKey string
		maxSize   int64
		expiresAt string
	)
	err := h.app.DB.QueryRowContext(c.UserContext(),
		`SELECT id, provider, object_key, max_size_bytes, expires_at
		 FROM upload_sessions
		 WHERE upload_token = ? AND status IN ('initiated', 'uploaded')
		 LIMIT 1`,
		uploadToken,
	).Scan(&sessionID, &provider, &objectKey, &maxSize, &expiresAt)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "upload session not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to query upload session")
	}
	if provider != "local" {
		return response.Error(c, fiber.StatusBadRequest, "upload session is not local provider")
	}
	expiry, err := util.ParseTime(expiresAt)
	if err != nil || expiry.Before(nowUTC()) {
		return response.Error(c, fiber.StatusGone, "upload session expired")
	}

	targetPath := h.app.Storage.LocalObjectPath(objectKey)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to prepare upload directory")
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create upload file")
	}
	defer file.Close()

	stream := c.Context().RequestBodyStream()
	if stream == nil {
		stream = bytes.NewReader(c.BodyRaw())
	}
	reader := io.LimitReader(stream, maxSize+1)
	written, err := io.Copy(file, reader)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to write upload file")
	}
	if written > maxSize {
		_ = os.Remove(targetPath)
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "file exceeds max_size_bytes")
	}

	_, err = h.app.DB.ExecContext(c.UserContext(),
		`UPDATE upload_sessions
		 SET status = 'uploaded', file_size_bytes = ?, expires_at = expires_at
		 WHERE id = ?`,
		written,
		sessionID,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update upload session")
	}

	return response.OK(c, fiber.Map{"uploaded": true, "size_bytes": written})
}

func (h *Handler) CompleteUpload(c *fiber.Ctx) error {
	uid := currentUserID(c)
	uploadID := strings.TrimSpace(c.Params("uploadId"))
	if uploadID == "" {
		return response.Error(c, fiber.StatusBadRequest, "uploadId is required")
	}

	var req completeUploadRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var (
		provider, bucket, objectKey, contentType, originalFilename, status string
		fileSize                                                           int64
		expiresAt                                                          string
		mediaObjectID                                                      sql.NullString
	)
	err = tx.QueryRowContext(c.UserContext(),
		`SELECT provider, CASE WHEN provider='s3' THEN ? ELSE '' END, object_key, content_type, original_filename, file_size_bytes, status, expires_at, media_object_id
		 FROM upload_sessions
		 WHERE id = ? AND user_id = ?
		 LIMIT 1`,
		h.app.Storage.Bucket(),
		uploadID,
		uid,
	).Scan(&provider, &bucket, &objectKey, &contentType, &originalFilename, &fileSize, &status, &expiresAt, &mediaObjectID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "upload session not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to query upload session")
	}

	if mediaObjectID.Valid && status == "completed" {
		mediaID := mediaObjectID.String
		var mediaProvider, mediaBucket, mediaKey string
		if err := tx.QueryRowContext(c.UserContext(), `SELECT provider, COALESCE(bucket,''), object_key FROM media_objects WHERE id = ?`, mediaID).Scan(&mediaProvider, &mediaBucket, &mediaKey); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to query media object")
		}
		if err := tx.Commit(); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to complete upload")
		}
		return response.OK(c, fiber.Map{"media_object_id": mediaID, "object_key": mediaKey, "url": mediaURL(h.app.Storage, mediaProvider, mediaBucket, mediaKey)})
	}

	expiry, err := util.ParseTime(expiresAt)
	if err != nil || expiry.Before(nowUTC()) {
		return response.Error(c, fiber.StatusGone, "upload session expired")
	}

	if provider == "local" {
		localPath := h.app.Storage.LocalObjectPath(objectKey)
		fi, err := os.Stat(localPath)
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "local file not uploaded yet")
		}
		fileSize = fi.Size()
	}

	mediaID := newID()
	now := nowString()
	_, err = tx.ExecContext(c.UserContext(),
		`INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, checksum_sha256, duration_sec, width, height, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mediaID,
		provider,
		nullableString(bucket),
		objectKey,
		originalFilename,
		contentType,
		fileSize,
		nullableString(strings.TrimSpace(req.ChecksumSHA256)),
		req.DurationSec,
		nullableInt(req.Width),
		nullableInt(req.Height),
		uid,
		now,
	)
	if err != nil {
		if isConflictErr(err) {
			var existingID string
			if err := tx.QueryRowContext(c.UserContext(), `SELECT id FROM media_objects WHERE object_key = ?`, objectKey).Scan(&existingID); err == nil {
				mediaID = existingID
			} else {
				return response.Error(c, fiber.StatusInternalServerError, "failed to reuse media object")
			}
		} else {
			return response.Error(c, fiber.StatusInternalServerError, "failed to create media object")
		}
	}

	_, err = tx.ExecContext(c.UserContext(),
		`UPDATE upload_sessions
		 SET status = 'completed', completed_at = ?, media_object_id = ?, file_size_bytes = ?
		 WHERE id = ?`,
		now,
		mediaID,
		fileSize,
		uploadID,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to complete upload session")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to commit upload completion")
	}

	return response.OK(c, fiber.Map{
		"media_object_id": mediaID,
		"object_key":      objectKey,
		"url":             mediaURL(h.app.Storage, provider, bucket, objectKey),
	})
}

func buildObjectKey(purpose, userID, uploadID, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	base = strings.TrimSpace(base)
	if base == "" {
		base = purpose
	}
	base = unsafeFileChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-._")
	if base == "" {
		base = purpose
	}
	if len(base) > 64 {
		base = base[:64]
	}
	return purpose + "/" + userID + "/" + uploadID + "/" + base + ext
}

func isAllowedMIME(purpose, mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if purpose == "video" {
		switch mime {
		case "video/mp4", "video/quicktime", "video/x-msvideo", "video/webm":
			return true
		default:
			return false
		}
	}
	switch mime {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func nullableInt(v int64) interface{} {
	if v <= 0 {
		return nil
	}
	return v
}
