package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gofiber/fiber/v2"

	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/util"
)

type importListCursor struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type inspectTorrentRequest struct {
	Filename      string `json:"filename"`
	TorrentBase64 string `json:"torrent_base64"`
}

type startTorrentImportRequest struct {
	JobID               string   `json:"job_id"`
	SelectedFileIndexes []int    `json:"selected_file_indexes"`
	CategoryID          *int64   `json:"category_id"`
	Tags                []string `json:"tags"`
	Visibility          string   `json:"visibility"`
	Title               string   `json:"title"`
	TitlePrefix         string   `json:"title_prefix"`
	Description         string   `json:"description"`
}

type startURLImportRequest struct {
	URL         string   `json:"url"`
	CategoryID  *int64   `json:"category_id"`
	Tags        []string `json:"tags"`
	Visibility  string   `json:"visibility"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
}

func (h *Handler) InspectTorrentImport(c *fiber.Ctx) error {
	uid := currentUserID(c)
	var req inspectTorrentRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.Filename = strings.TrimSpace(req.Filename)
	if req.Filename == "" {
		return response.Error(c, fiber.StatusBadRequest, "filename is required")
	}
	if strings.ToLower(filepath.Ext(req.Filename)) != ".torrent" {
		return response.Error(c, fiber.StatusBadRequest, "only .torrent files are supported")
	}

	torrentBytes, err := decodeBase64Payload(req.TorrentBase64)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid torrent_base64")
	}
	if len(torrentBytes) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "torrent file is empty")
	}
	if int64(len(torrentBytes)) > h.app.Config.ImportTorrentMax {
		return response.Error(c, fiber.StatusBadRequest, "torrent file exceeds size limit")
	}

	mi, err := metainfo.Load(bytes.NewReader(torrentBytes))
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "failed to parse torrent file")
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "failed to read torrent info")
	}

	files := info.UpvertedFiles()
	type candidateItem struct {
		ID           string
		FileIndex    int
		FilePath     string
		FileSize     int64
		Selected     bool
		Status       string
		ErrorMessage string
	}
	candidates := make([]candidateItem, 0, len(files))
	for idx, fi := range files {
		displayPath := cleanTorrentDisplayPath(fi.DisplayPath(&info))
		if displayPath == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(displayPath))
		if _, ok := allowedVideoExts[ext]; !ok {
			continue
		}
		candidates = append(candidates, candidateItem{
			ID:        newID(),
			FileIndex: idx,
			FilePath:  displayPath,
			FileSize:  fi.Length,
			Status:    "pending",
		})
	}

	now := nowUTC()
	nowStr := util.FormatTime(now)
	expiresAt := util.FormatTime(now.Add(24 * time.Hour))
	maxAttempts := h.app.Config.ImportMaxTry
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	jobID := newID()
	infoHash := strings.ToLower(mi.HashInfoBytes().HexString())

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(c.UserContext(), `
INSERT INTO video_import_jobs (
	id, user_id, source_type, source_filename, info_hash, torrent_data, status,
	category_id, tags_json, visibility, attempts, max_attempts,
	total_files, selected_files, completed_files, failed_files, progress,
	available_at, started_at, finished_at, expires_at, error_message,
	created_at, updated_at
) VALUES (?, ?, 'torrent', ?, ?, ?, 'draft',
	NULL, '[]', 'public', 0, ?,
	?, 0, 0, 0, 0,
	?, NULL, NULL, ?, NULL,
	?, ?)
`,
		jobID,
		uid,
		req.Filename,
		infoHash,
		torrentBytes,
		maxAttempts,
		len(candidates),
		nowStr,
		expiresAt,
		nowStr,
		nowStr,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create import job")
	}

	for _, item := range candidates {
		_, err := tx.ExecContext(c.UserContext(), `
INSERT INTO video_import_items (
	id, job_id, file_index, file_path, file_size_bytes,
	selected, status, error_message, media_object_id, video_id,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, 0, 'pending', NULL, NULL, NULL, ?, ?)
`,
			item.ID,
			jobID,
			item.FileIndex,
			item.FilePath,
			item.FileSize,
			nowStr,
			nowStr,
		)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to create import items")
		}
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to save import draft")
	}

	itemPayload := make([]fiber.Map, 0, len(candidates))
	for _, item := range candidates {
		itemPayload = append(itemPayload, fiber.Map{
			"id":              item.ID,
			"file_index":      item.FileIndex,
			"file_path":       item.FilePath,
			"file_size_bytes": item.FileSize,
			"selected":        item.Selected,
			"status":          item.Status,
			"error_message":   item.ErrorMessage,
		})
	}

	return response.Created(c, fiber.Map{
		"job": fiber.Map{
			"id":              jobID,
			"source_type":     "torrent",
			"source_filename": req.Filename,
			"info_hash":       infoHash,
			"status":          "draft",
			"visibility":      "public",
			"tags":            []string{},
			"total_files":     len(candidates),
			"selected_files":  0,
			"completed_files": 0,
			"failed_files":    0,
			"progress":        0,
			"available_at":    nowStr,
			"expires_at":      expiresAt,
			"created_at":      nowStr,
			"updated_at":      nowStr,
		},
		"items": itemPayload,
	})
}

func (h *Handler) StartTorrentImport(c *fiber.Ctx) error {
	uid := currentUserID(c)

	var req startTorrentImportRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.JobID = strings.TrimSpace(req.JobID)
	if req.JobID == "" {
		return response.Error(c, fiber.StatusBadRequest, "job_id is required")
	}

	visibility := normalizeImportVisibility(req.Visibility)
	if visibility == "" {
		return response.Error(c, fiber.StatusBadRequest, "invalid visibility")
	}
	customTitle, err := normalizeImportCustomTitle(req.Title)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	customTitlePrefix, err := normalizeImportCustomTitlePrefix(req.TitlePrefix)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	customDescription, err := normalizeImportCustomDescription(req.Description)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	selectedSet := make(map[int]struct{}, len(req.SelectedFileIndexes))
	for _, idx := range req.SelectedFileIndexes {
		if idx < 0 {
			return response.Error(c, fiber.StatusBadRequest, "selected_file_indexes contains invalid index")
		}
		selectedSet[idx] = struct{}{}
	}
	if len(selectedSet) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "selected_file_indexes is required")
	}
	if len(selectedSet) > h.app.Config.ImportMaxFiles {
		return response.Error(c, fiber.StatusBadRequest, fmt.Sprintf("selected files exceed limit (%d)", h.app.Config.ImportMaxFiles))
	}

	selectedIndexes := make([]int, 0, len(selectedSet))
	for idx := range selectedSet {
		selectedIndexes = append(selectedIndexes, idx)
	}
	sort.Ints(selectedIndexes)

	tags := normalizeImportTags(req.Tags)
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to encode tags")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var (
		status      string
		existingUID string
	)
	err = tx.QueryRowContext(c.UserContext(), `
SELECT status, user_id
FROM video_import_jobs
WHERE id = ?
LIMIT 1`, req.JobID).Scan(&status, &existingUID)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "import job not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to query import job")
	}
	if existingUID != uid {
		return response.Error(c, fiber.StatusForbidden, "forbidden")
	}
	if status != "draft" {
		return response.Error(c, fiber.StatusConflict, "import job is not in draft status")
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

	existingIndexes := map[int]struct{}{}
	rows, err := tx.QueryContext(c.UserContext(), `SELECT file_index FROM video_import_items WHERE job_id = ?`, req.JobID)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to query import items")
	}
	defer rows.Close()
	for rows.Next() {
		var fileIndex int
		if err := rows.Scan(&fileIndex); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse import items")
		}
		existingIndexes[fileIndex] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to read import items")
	}
	for _, idx := range selectedIndexes {
		if _, ok := existingIndexes[idx]; !ok {
			return response.Error(c, fiber.StatusBadRequest, "selected_file_indexes contains unknown item")
		}
	}

	now := nowString()
	maxAttempts := h.app.Config.ImportMaxTry
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	if _, err := tx.ExecContext(c.UserContext(), `
UPDATE video_import_items
SET selected = 0,
	status = 'skipped',
	error_message = NULL,
	updated_at = ?
WHERE job_id = ?`, now, req.JobID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to reset import items")
	}

	for _, idx := range selectedIndexes {
		if _, err := tx.ExecContext(c.UserContext(), `
UPDATE video_import_items
SET selected = 1,
	status = 'pending',
	error_message = NULL,
	updated_at = ?
WHERE job_id = ? AND file_index = ?`, now, req.JobID, idx); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to mark selected import item")
		}
	}

	if _, err := tx.ExecContext(c.UserContext(), `
UPDATE video_import_jobs
SET status = 'queued',
	category_id = ?,
	tags_json = ?,
	visibility = ?,
	custom_title = ?,
	custom_title_prefix = ?,
	custom_description = ?,
	selected_files = ?,
	completed_files = 0,
	failed_files = 0,
	progress = 0,
	attempts = 0,
	max_attempts = ?,
	error_message = NULL,
	available_at = ?,
	started_at = NULL,
	finished_at = NULL,
	updated_at = ?
WHERE id = ?`, req.CategoryID, string(tagsJSON), visibility, nullableString(customTitle), nullableString(customTitlePrefix), customDescription, len(selectedIndexes), maxAttempts, now, now, req.JobID); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to queue import job")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to start import job")
	}

	return response.OK(c, fiber.Map{
		"job_id":         req.JobID,
		"status":         "queued",
		"selected_files": len(selectedIndexes),
	})
}

func (h *Handler) StartURLImport(c *fiber.Ctx) error {
	uid := currentUserID(c)

	var req startURLImportRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		return response.Error(c, fiber.StatusBadRequest, "url is required")
	}
	if len(req.URL) > 2048 || !isValidImportURL(req.URL) {
		return response.Error(c, fiber.StatusBadRequest, "url is invalid")
	}

	visibility := normalizeImportVisibility(req.Visibility)
	if visibility == "" {
		return response.Error(c, fiber.StatusBadRequest, "invalid visibility")
	}
	customTitle, err := normalizeImportCustomTitle(req.Title)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	customDescription, err := normalizeImportCustomDescription(req.Description)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	tags := normalizeImportTags(req.Tags)
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to encode tags")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	if req.CategoryID != nil {
		var exists int
		if err := tx.QueryRowContext(c.UserContext(), `SELECT 1 FROM categories WHERE id = ? AND is_active = 1 LIMIT 1`, *req.CategoryID).Scan(&exists); err != nil {
			if isNotFound(err) {
				return response.Error(c, fiber.StatusBadRequest, "category_id is invalid")
			}
			return response.Error(c, fiber.StatusInternalServerError, "failed to validate category")
		}
	}

	ytdlpMode, ytdlpMetaArgsJSON, ytdlpDownArgsJSON, err := h.resolveYTDLPSnapshotForJob(c.UserContext(), tx)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, err.Error())
	}

	now := nowUTC()
	nowStr := util.FormatTime(now)
	expiresAt := util.FormatTime(now.Add(24 * time.Hour))
	maxAttempts := h.app.Config.ImportMaxTry
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	jobID := newID()
	itemID := newID()
	sourceFilename := buildImportSourceFilename(req.URL)

	if _, err := tx.ExecContext(c.UserContext(), `
INSERT INTO video_import_jobs (
	id, user_id, source_type, source_filename, info_hash, torrent_data,
	source_url, resolved_media_url, resolver_name, resolver_meta_json,
	ytdlp_param_mode, ytdlp_metadata_args_json, ytdlp_download_args_json,
	custom_title, custom_title_prefix, custom_description,
	status, category_id, tags_json, visibility, attempts, max_attempts,
	total_files, selected_files, completed_files, failed_files, progress,
	available_at, started_at, finished_at, expires_at, error_message,
	created_at, updated_at
) VALUES (?, ?, 'url', ?, NULL, NULL,
	?, NULL, NULL, NULL,
	?, ?, ?,
	?, NULL, ?,
	'queued', ?, ?, ?, 0, ?,
	1, 1, 0, 0, 0,
	?, NULL, NULL, ?, NULL,
	?, ?)
`,
		jobID,
		uid,
		sourceFilename,
		req.URL,
		ytdlpMode,
		ytdlpMetaArgsJSON,
		ytdlpDownArgsJSON,
		nullableString(customTitle),
		customDescription,
		req.CategoryID,
		string(tagsJSON),
		visibility,
		maxAttempts,
		nowStr,
		expiresAt,
		nowStr,
		nowStr,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create import job")
	}

	if _, err := tx.ExecContext(c.UserContext(), `
INSERT INTO video_import_items (
	id, job_id, file_index, file_path, file_size_bytes,
	selected, status, error_message, media_object_id, video_id,
	created_at, updated_at
) VALUES (?, ?, 0, ?, 0, 1, 'pending', NULL, NULL, NULL, ?, ?)
`, itemID, jobID, req.URL, nowStr, nowStr); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create import item")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to queue import job")
	}

	return response.OK(c, fiber.Map{
		"job_id":         jobID,
		"status":         "queued",
		"selected_files": 1,
	})
}

func (h *Handler) ListImportJobs(c *fiber.Ctx) error {
	uid := currentUserID(c)
	limit := pagination.ClampLimit(c.Query("limit"), defaultLimit, maxLimit)
	cursorRaw := strings.TrimSpace(c.Query("cursor"))

	query := `
SELECT id, source_type, COALESCE(source_filename, ''), COALESCE(info_hash, ''), status,
       COALESCE(source_url, ''), COALESCE(resolved_media_url, ''), COALESCE(resolver_name, ''),
       COALESCE(ytdlp_param_mode, 'safe'),
       COALESCE(category_id, 0), tags_json, visibility,
       total_files, selected_files, completed_files, failed_files, progress,
       COALESCE(available_at, ''), COALESCE(started_at, ''), COALESCE(finished_at, ''),
       COALESCE(expires_at, ''), COALESCE(error_message, ''),
       created_at, updated_at
FROM video_import_jobs
WHERE user_id = ?`
	args := []interface{}{uid}

	if cursorRaw != "" {
		var cur importListCursor
		if err := pagination.Decode(cursorRaw, &cur); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid cursor")
		}
		if cur.CreatedAt != "" && cur.ID != "" {
			query += " AND (created_at < ? OR (created_at = ? AND id < ?))"
			args = append(args, cur.CreatedAt, cur.CreatedAt, cur.ID)
		}
	}

	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := h.app.DB.QueryContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list import jobs")
	}
	defer rows.Close()

	type listJob struct {
		ID            string
		SourceType    string
		SourceFile    string
		InfoHash      string
		Status        string
		SourceURL     string
		ResolvedURL   string
		ResolverName  string
		YTDLPMode     string
		CategoryID    int64
		TagsJSON      string
		Visibility    string
		TotalFiles    int64
		SelectedFiles int64
		Completed     int64
		Failed        int64
		Progress      float64
		AvailableAt   string
		StartedAt     string
		FinishedAt    string
		ExpiresAt     string
		ErrorMessage  string
		CreatedAt     string
		UpdatedAt     string
	}

	jobs := make([]listJob, 0, limit+1)
	for rows.Next() {
		var item listJob
		if err := rows.Scan(
			&item.ID,
			&item.SourceType,
			&item.SourceFile,
			&item.InfoHash,
			&item.Status,
			&item.SourceURL,
			&item.ResolvedURL,
			&item.ResolverName,
			&item.YTDLPMode,
			&item.CategoryID,
			&item.TagsJSON,
			&item.Visibility,
			&item.TotalFiles,
			&item.SelectedFiles,
			&item.Completed,
			&item.Failed,
			&item.Progress,
			&item.AvailableAt,
			&item.StartedAt,
			&item.FinishedAt,
			&item.ExpiresAt,
			&item.ErrorMessage,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse import job")
		}
		jobs = append(jobs, item)
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to read import jobs")
	}

	nextCursor := ""
	if len(jobs) > limit {
		last := jobs[limit-1]
		encoded, err := pagination.Encode(importListCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to encode next cursor")
		}
		nextCursor = encoded
		jobs = jobs[:limit]
	}

	items := make([]fiber.Map, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, fiber.Map{
			"id":                 job.ID,
			"source_type":        job.SourceType,
			"source_filename":    job.SourceFile,
			"info_hash":          job.InfoHash,
			"source_url":         job.SourceURL,
			"resolved_media_url": job.ResolvedURL,
			"resolver_name":      job.ResolverName,
			"ytdlp_param_mode":   job.YTDLPMode,
			"status":             job.Status,
			"category_id":        nullableCategory(job.CategoryID),
			"tags":               parseImportTags(job.TagsJSON),
			"visibility":         job.Visibility,
			"total_files":        job.TotalFiles,
			"selected_files":     job.SelectedFiles,
			"completed_files":    job.Completed,
			"failed_files":       job.Failed,
			"progress":           job.Progress,
			"available_at":       job.AvailableAt,
			"started_at":         job.StartedAt,
			"finished_at":        job.FinishedAt,
			"expires_at":         job.ExpiresAt,
			"error_message":      job.ErrorMessage,
			"created_at":         job.CreatedAt,
			"updated_at":         job.UpdatedAt,
		})
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) GetImportJobDetail(c *fiber.Ctx) error {
	uid := currentUserID(c)
	jobID := strings.TrimSpace(c.Params("jobId"))
	if jobID == "" {
		return response.Error(c, fiber.StatusBadRequest, "jobId is required")
	}

	var (
		jobUserID      string
		sourceType     string
		sourceFilename string
		infoHash       string
		sourceURL      string
		resolvedMedia  string
		resolverName   string
		ytdlpMode      string
		status         string
		categoryID     sql.NullInt64
		tagsJSON       string
		visibility     string
		totalFiles     int64
		selectedFiles  int64
		completedFiles int64
		failedFiles    int64
		progress       float64
		availableAt    sql.NullString
		startedAt      sql.NullString
		finishedAt     sql.NullString
		expiresAt      sql.NullString
		errorMessage   sql.NullString
		createdAt      string
		updatedAt      string
	)

	err := h.app.DB.QueryRowContext(c.UserContext(), `
SELECT user_id, source_type, COALESCE(source_filename, ''), COALESCE(info_hash, ''),
       COALESCE(source_url, ''), COALESCE(resolved_media_url, ''), COALESCE(resolver_name, ''),
       COALESCE(ytdlp_param_mode, 'safe'),
       status,
       category_id, tags_json, visibility,
       total_files, selected_files, completed_files, failed_files, progress,
       available_at, started_at, finished_at, expires_at, error_message,
       created_at, updated_at
FROM video_import_jobs
WHERE id = ?
LIMIT 1`, jobID).Scan(
		&jobUserID,
		&sourceType,
		&sourceFilename,
		&infoHash,
		&sourceURL,
		&resolvedMedia,
		&resolverName,
		&ytdlpMode,
		&status,
		&categoryID,
		&tagsJSON,
		&visibility,
		&totalFiles,
		&selectedFiles,
		&completedFiles,
		&failedFiles,
		&progress,
		&availableAt,
		&startedAt,
		&finishedAt,
		&expiresAt,
		&errorMessage,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "import job not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to query import job")
	}
	if jobUserID != uid {
		return response.Error(c, fiber.StatusForbidden, "forbidden")
	}

	rows, err := h.app.DB.QueryContext(c.UserContext(), `
SELECT id, file_index, file_path, file_size_bytes, selected, status,
       COALESCE(error_message, ''), COALESCE(media_object_id, ''), COALESCE(video_id, ''),
       created_at, updated_at
FROM video_import_items
WHERE job_id = ?
ORDER BY file_index ASC`, jobID)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to list import items")
	}
	defer rows.Close()

	items := make([]fiber.Map, 0)
	createdVideoIDs := make([]string, 0)
	for rows.Next() {
		var (
			id            string
			fileIndex     int64
			filePath      string
			fileSizeBytes int64
			selected      int64
			itemStatus    string
			itemError     string
			mediaObjectID string
			videoID       string
			itemCreatedAt string
			itemUpdatedAt string
		)
		if err := rows.Scan(
			&id,
			&fileIndex,
			&filePath,
			&fileSizeBytes,
			&selected,
			&itemStatus,
			&itemError,
			&mediaObjectID,
			&videoID,
			&itemCreatedAt,
			&itemUpdatedAt,
		); err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to parse import items")
		}
		if videoID != "" {
			createdVideoIDs = append(createdVideoIDs, videoID)
		}
		items = append(items, fiber.Map{
			"id":              id,
			"file_index":      fileIndex,
			"file_path":       filePath,
			"file_size_bytes": fileSizeBytes,
			"selected":        selected > 0,
			"status":          itemStatus,
			"error_message":   itemError,
			"media_object_id": mediaObjectID,
			"video_id":        videoID,
			"created_at":      itemCreatedAt,
			"updated_at":      itemUpdatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to read import items")
	}

	return response.OK(c, fiber.Map{
		"job": fiber.Map{
			"id":                 jobID,
			"source_type":        sourceType,
			"source_filename":    sourceFilename,
			"info_hash":          infoHash,
			"source_url":         sourceURL,
			"resolved_media_url": resolvedMedia,
			"resolver_name":      resolverName,
			"ytdlp_param_mode":   ytdlpMode,
			"status":             status,
			"category_id":        nullableCategoryFromNull(categoryID),
			"tags":               parseImportTags(tagsJSON),
			"visibility":         visibility,
			"total_files":        totalFiles,
			"selected_files":     selectedFiles,
			"completed_files":    completedFiles,
			"failed_files":       failedFiles,
			"progress":           progress,
			"available_at":       maybeString(availableAt),
			"started_at":         maybeString(startedAt),
			"finished_at":        maybeString(finishedAt),
			"expires_at":         maybeString(expiresAt),
			"error_message":      maybeString(errorMessage),
			"created_at":         createdAt,
			"updated_at":         updatedAt,
		},
		"items":             items,
		"created_video_ids": createdVideoIDs,
	})
}

func (h *Handler) resolveYTDLPSnapshotForJob(ctx context.Context, tx *sql.Tx) (mode string, metadataJSON string, downloadJSON string, err error) {
	now := nowString()
	if _, err = tx.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO site_settings (
		    id, site_title, site_description, site_logo_media_id, register_enabled, updated_by, created_at, updated_at
		) VALUES (1, ?, ?, NULL, 1, NULL, ?, ?)`,
		defaultSiteTitle,
		defaultSiteDescription,
		now,
		now,
	); err != nil {
		return "", "", "", fmt.Errorf("failed to read yt-dlp settings")
	}

	var (
		modeRaw     string
		safeJSON    string
		metaRaw     string
		downloadRaw string
	)
	if err = tx.QueryRowContext(ctx, `
SELECT COALESCE(ytdlp_param_mode, 'safe'),
       COALESCE(ytdlp_safe_json, '{}'),
       COALESCE(ytdlp_metadata_args_raw, ''),
       COALESCE(ytdlp_download_args_raw, '')
FROM site_settings
WHERE id = 1
LIMIT 1`).Scan(&modeRaw, &safeJSON, &metaRaw, &downloadRaw); err != nil {
		return "", "", "", fmt.Errorf("failed to read yt-dlp settings")
	}

	mode, err = normalizeYTDLPMode(modeRaw)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid yt-dlp settings")
	}

	var (
		metaArgs []string
		downArgs []string
	)
	switch mode {
	case ytdlpModeSafe:
		safeCfg, parseErr := parseYTDLPSafeJSON(safeJSON)
		if parseErr != nil {
			return "", "", "", fmt.Errorf("invalid yt-dlp settings")
		}
		metaArgs, downArgs, parseErr = buildYTDLPArgsFromSafe(safeCfg)
		if parseErr != nil {
			return "", "", "", fmt.Errorf("invalid yt-dlp settings")
		}
	case ytdlpModeAdvanced:
		metaArgs, err = splitCommandArgs(metaRaw)
		if err != nil {
			return "", "", "", fmt.Errorf("invalid yt-dlp settings")
		}
		downArgs, err = splitCommandArgs(downloadRaw)
		if err != nil {
			return "", "", "", fmt.Errorf("invalid yt-dlp settings")
		}
		if err = validateYTDLPArgTokens(metaArgs); err != nil {
			return "", "", "", fmt.Errorf("invalid yt-dlp settings")
		}
		if err = validateYTDLPArgTokens(downArgs); err != nil {
			return "", "", "", fmt.Errorf("invalid yt-dlp settings")
		}
	default:
		return "", "", "", fmt.Errorf("invalid yt-dlp settings")
	}

	metadataJSON, err = marshalArgTokenJSON(metaArgs)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to encode yt-dlp settings")
	}
	downloadJSON, err = marshalArgTokenJSON(downArgs)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to encode yt-dlp settings")
	}
	return mode, metadataJSON, downloadJSON, nil
}

func decodeBase64Payload(input string) ([]byte, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return nil, fmt.Errorf("empty payload")
	}
	if idx := strings.Index(raw, ","); idx >= 0 && strings.Contains(raw[:idx], "base64") {
		raw = raw[idx+1:]
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(raw)
	if rawErr == nil {
		return decoded, nil
	}
	decoded, urlErr := base64.RawURLEncoding.DecodeString(raw)
	if urlErr == nil {
		return decoded, nil
	}
	return nil, err
}

func cleanTorrentDisplayPath(raw string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if normalized == "" {
		return ""
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+normalized), "/")
	if cleaned == "" || cleaned == "." {
		return ""
	}
	parts := strings.Split(cleaned, "/")
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		part = strings.Map(func(r rune) rune {
			if r < 32 {
				return -1
			}
			return r
		}, part)
		if part == "" {
			continue
		}
		safe = append(safe, part)
	}
	return strings.Join(safe, "/")
}

func buildImportSourceFilename(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return rawURL
	}
	host := strings.TrimSpace(parsed.Hostname())
	pathPart := strings.TrimSpace(parsed.EscapedPath())
	if host == "" {
		return rawURL
	}
	if pathPart == "" || pathPart == "/" {
		return host
	}
	out := host + pathPart
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func isValidImportURL(raw string) bool {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return false
	}
	return true
}

func normalizeImportVisibility(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "public"
	}
	if v == "public" || v == "private" || v == "unlisted" {
		return v
	}
	return ""
}

func normalizeImportTags(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, tag := range raw {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		if len(t) > 32 {
			t = t[:32]
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func normalizeImportCustomTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", nil
	}
	if len([]rune(title)) > 120 {
		return "", fmt.Errorf("title is too long")
	}
	return title, nil
}

func normalizeImportCustomTitlePrefix(raw string) (string, error) {
	prefix := strings.TrimSpace(raw)
	if prefix == "" {
		return "", nil
	}
	if len([]rune(prefix)) > 120 {
		return "", fmt.Errorf("title_prefix is too long")
	}
	return prefix, nil
}

func normalizeImportCustomDescription(raw string) (string, error) {
	description := strings.TrimSpace(raw)
	if len([]rune(description)) > 5000 {
		return "", fmt.Errorf("description is too long")
	}
	return description, nil
}

func parseImportTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return []string{}
	}
	return normalizeImportTags(tags)
}

func nullableCategory(v int64) interface{} {
	if v <= 0 {
		return nil
	}
	return v
}

func nullableCategoryFromNull(v sql.NullInt64) interface{} {
	if !v.Valid || v.Int64 <= 0 {
		return nil
	}
	return v.Int64
}
