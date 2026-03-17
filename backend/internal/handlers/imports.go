package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/gofiber/fiber/v2"

	appconfig "moevideo/backend/internal/config"
	"moevideo/backend/internal/pagination"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/util"
)

type importListCursor struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

const importJobTempFolderName = "import-jobs"

const urlInspectTokenTTL = 10 * time.Minute

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
	URL            string   `json:"url"`
	CategoryID     *int64   `json:"category_id"`
	Tags           []string `json:"tags"`
	Visibility     string   `json:"visibility"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	UserCookieID   string   `json:"user_cookie_id"`
	InspectToken   string   `json:"inspect_token"`
	CandidateIndex *int     `json:"candidate_index"`
}

type inspectURLImportRequest struct {
	URL          string `json:"url"`
	UserCookieID string `json:"user_cookie_id"`
}

type urlInspectTokenPayload struct {
	UserID          string                    `json:"user_id"`
	SourceURL       string                    `json:"source_url"`
	UserCookieID    string                    `json:"user_cookie_id,omitempty"`
	Candidates      []string                  `json:"candidates"`
	ResolverContext urlInspectResolverContext `json:"resolver_context"`
	ExpiresAt       int64                     `json:"expires_at"`
}

type urlInspectResolverContext struct {
	PageTitle     string            `json:"page_title,omitempty"`
	PageUserAgent string            `json:"page_user_agent,omitempty"`
	PageReferer   string            `json:"page_referer,omitempty"`
	PageOrigin    string            `json:"page_origin,omitempty"`
	PageHeaders   map[string]string `json:"page_headers,omitempty"`
}

type pageManifestResolverOutput struct {
	FinalURL      string            `json:"final_url"`
	Title         string            `json:"title"`
	Candidates    []string          `json:"candidates"`
	PageUserAgent string            `json:"page_user_agent"`
	PageReferer   string            `json:"page_referer"`
	PageOrigin    string            `json:"page_origin"`
	PageHeaders   map[string]string `json:"page_headers"`
	Reason        string            `json:"reason"`
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
			Selected:  true,
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
	?, ?, 0, 0, 0,
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
) VALUES (?, ?, ?, ?, ?, 1, 'pending', NULL, NULL, NULL, ?, ?)
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
			"id":                  jobID,
			"source_type":         "torrent",
			"source_filename":     req.Filename,
			"info_hash":           infoHash,
			"custom_title":        nil,
			"custom_title_prefix": nil,
			"custom_description":  "",
			"status":              "draft",
			"draft_expired":       false,
			"visibility":          "public",
			"tags":                []string{},
			"total_files":         len(candidates),
			"selected_files":      len(candidates),
			"completed_files":     0,
			"failed_files":        0,
			"progress":            0,
			"available_at":        nowStr,
			"expires_at":          expiresAt,
			"created_at":          nowStr,
			"updated_at":          nowStr,
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
	if req.CategoryID == nil {
		return response.Error(c, fiber.StatusBadRequest, "category_id is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var (
		status      string
		existingUID string
		expiresAt   string
	)
	err = tx.QueryRowContext(c.UserContext(), `
SELECT status, user_id, COALESCE(expires_at, '')
FROM video_import_jobs
WHERE id = ?
LIMIT 1`, req.JobID).Scan(&status, &existingUID, &expiresAt)
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
	if isImportDraftExpired(status, expiresAt, nowUTC()) {
		return response.Error(c, fiber.StatusConflict, "import draft is expired, please inspect torrent again")
	}
	activeJobs, err := countUserActiveImportJobsTx(c.UserContext(), tx, uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to check import concurrency")
	}
	if activeJobs >= 3 {
		return h.respondRateLimited(c, "import.concurrent.active", time.Minute, "too many active import jobs")
	}

	var exists int
	if err := tx.QueryRowContext(c.UserContext(), `SELECT 1 FROM categories WHERE id = ? AND is_active = 1 LIMIT 1`, *req.CategoryID).Scan(&exists); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusBadRequest, "category_id is invalid")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate category")
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
	downloaded_bytes = 0,
	uploaded_bytes = 0,
	download_speed_bps = 0,
	upload_speed_bps = 0,
	transfer_updated_at = NULL,
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

func (h *Handler) InspectURLImport(c *fiber.Ctx) error {
	uid := currentUserID(c)

	var req inspectURLImportRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	req.URL = strings.TrimSpace(req.URL)
	req.UserCookieID = strings.TrimSpace(req.UserCookieID)
	if req.URL == "" {
		return response.Error(c, fiber.StatusBadRequest, "url is required")
	}
	if len(req.URL) > 2048 || !isValidImportURL(req.URL) {
		return response.Error(c, fiber.StatusBadRequest, "url is invalid")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	_, ytdlpMetaArgsJSON, _, err := h.resolveYTDLPSnapshotForJob(c.UserContext(), tx)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, err.Error())
	}

	selectedCookie, err := h.resolveUserYTDLPCookieSelection(c.UserContext(), tx, uid, req.URL, req.UserCookieID)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to load yt-dlp settings")
	}

	metadataArgs, err := parseArgJSONArray(ytdlpMetaArgsJSON)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "invalid yt-dlp settings")
	}
	if selectedCookie != nil && !hasYTDLPCookieSourceArg(metadataArgs) {
		cookieArgs, cleanup, buildErr := h.prepareRuntimeYTDLPCookieArgs(selectedCookie)
		if buildErr != nil {
			return response.Error(c, fiber.StatusBadRequest, buildErr.Error())
		}
		defer cleanup()
		metadataArgs = append(metadataArgs, cookieArgs...)
	}

	forcedFallback := shouldForceFallbackForImportURL(req.URL, h.app.Config.ImportForceFallbackDomains)
	unsupported := forcedFallback
	if !forcedFallback {
		var checkErr error
		unsupported, checkErr = h.checkURLMetadataSupport(c.UserContext(), req.URL, metadataArgs)
		if checkErr != nil {
			return response.Error(c, fiber.StatusBadRequest, checkErr.Error())
		}
	}
	if !unsupported {
		return response.OK(c, fiber.Map{
			"mode":           "direct_supported",
			"source_url":     req.URL,
			"user_cookie_id": req.UserCookieID,
		})
	}

	if !h.app.Config.ImportPageResolverEnabled {
		if forcedFallback {
			return response.Error(c, fiber.StatusBadRequest, "import.url.page_resolver_unavailable: forced fallback domain matched but resolver is disabled")
		}
		return response.Error(c, fiber.StatusBadRequest, "import.url.page_resolver_unavailable: unsupported url and resolver is disabled")
	}

	candidates, resolverResult, err := h.resolveURLImportCandidates(c.UserContext(), req.URL)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, fmt.Sprintf("import.url.page_resolver_unavailable: %v", err))
	}
	resolverContext := buildURLInspectResolverContext(req.URL, resolverResult)

	token, err := h.signURLInspectToken(urlInspectTokenPayload{
		UserID:          uid,
		SourceURL:       req.URL,
		UserCookieID:    req.UserCookieID,
		Candidates:      candidates,
		ResolverContext: resolverContext,
		ExpiresAt:       nowUTC().Add(urlInspectTokenTTL).Unix(),
	})
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to create inspect token")
	}

	return response.OK(c, fiber.Map{
		"mode":           "candidate_required",
		"source_url":     req.URL,
		"candidates":     candidates,
		"inspect_token":  token,
		"page_title":     resolverContext.PageTitle,
		"user_cookie_id": req.UserCookieID,
		"reason":         strings.TrimSpace(resolverResult.Reason),
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
	req.InspectToken = strings.TrimSpace(req.InspectToken)
	req.UserCookieID = strings.TrimSpace(req.UserCookieID)
	forcedFallback := shouldForceFallbackForImportURL(req.URL, h.app.Config.ImportForceFallbackDomains)

	hasManualCandidate := req.InspectToken != "" || req.CandidateIndex != nil
	selectedCandidateURL := ""
	resolverName := ""
	resolverMetaJSON := ""
	var resolverMeta fiber.Map
	if forcedFallback {
		if !h.app.Config.ImportPageResolverEnabled {
			return response.Error(c, fiber.StatusBadRequest, "import.url.page_resolver_unavailable: forced fallback domain matched but resolver is disabled")
		}
		if !hasManualCandidate {
			return response.Error(c, fiber.StatusBadRequest, "inspect_token and candidate_index are required for forced fallback domain")
		}
	}
	if hasManualCandidate {
		if req.InspectToken == "" || req.CandidateIndex == nil {
			return response.Error(c, fiber.StatusBadRequest, "inspect_token and candidate_index must be provided together")
		}
		inspectPayload, verifyErr := h.verifyURLInspectToken(req.InspectToken, uid, req.URL)
		if verifyErr != nil {
			return response.Error(c, fiber.StatusBadRequest, verifyErr.Error())
		}
		if strings.TrimSpace(inspectPayload.UserCookieID) != req.UserCookieID {
			return response.Error(c, fiber.StatusBadRequest, "user_cookie_id does not match inspect_token")
		}
		idx := *req.CandidateIndex
		if idx < 0 || idx >= len(inspectPayload.Candidates) {
			return response.Error(c, fiber.StatusBadRequest, "candidate_index is invalid")
		}
		selectedCandidateURL = strings.TrimSpace(inspectPayload.Candidates[idx])
		if !isValidImportURL(selectedCandidateURL) {
			return response.Error(c, fiber.StatusBadRequest, "selected candidate url is invalid")
		}
		resolverName = "page_manifest+user_selected"
		resolverMeta = fiber.Map{
			"page_url":               req.URL,
			"selected_candidate_url": selectedCandidateURL,
			"candidate_index":        idx,
			"candidate_total":        len(inspectPayload.Candidates),
			"selection_mode":         "user_selected",
		}
		if inspectPayload.ResolverContext.PageTitle != "" {
			resolverMeta["page_title"] = inspectPayload.ResolverContext.PageTitle
		}
		if inspectPayload.ResolverContext.PageUserAgent != "" {
			resolverMeta["page_user_agent"] = inspectPayload.ResolverContext.PageUserAgent
		}
		if inspectPayload.ResolverContext.PageReferer != "" {
			resolverMeta["page_referer"] = inspectPayload.ResolverContext.PageReferer
		}
		if inspectPayload.ResolverContext.PageOrigin != "" {
			resolverMeta["page_origin"] = inspectPayload.ResolverContext.PageOrigin
		}
		if len(inspectPayload.ResolverContext.PageHeaders) > 0 {
			resolverMeta["page_headers"] = inspectPayload.ResolverContext.PageHeaders
		}
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
	if req.CategoryID == nil {
		return response.Error(c, fiber.StatusBadRequest, "category_id is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()
	activeJobs, err := countUserActiveImportJobsTx(c.UserContext(), tx, uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to check import concurrency")
	}
	if activeJobs >= 3 {
		return h.respondRateLimited(c, "import.concurrent.active", time.Minute, "too many active import jobs")
	}

	var exists int
	if err := tx.QueryRowContext(c.UserContext(), `SELECT 1 FROM categories WHERE id = ? AND is_active = 1 LIMIT 1`, *req.CategoryID).Scan(&exists); err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusBadRequest, "category_id is invalid")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to validate category")
	}

	ytdlpMode, ytdlpMetaArgsJSON, ytdlpDownArgsJSON, err := h.resolveYTDLPSnapshotForJob(c.UserContext(), tx)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, err.Error())
	}
	selectedCookie, err := h.resolveUserYTDLPCookieSelection(c.UserContext(), tx, uid, req.URL, req.UserCookieID)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	if selectedCookie != nil {
		if resolverMeta == nil {
			resolverMeta = fiber.Map{
				"page_url": req.URL,
			}
		}
		resolverMeta["user_cookie"] = fiber.Map{
			"profile_id":  selectedCookie.ID,
			"label":       selectedCookie.Label,
			"domain_rule": selectedCookie.DomainRule,
			"format":      selectedCookie.Format,
			"cipher_text": selectedCookie.CipherText,
			"nonce":       selectedCookie.Nonce,
		}
	}
	if resolverMeta != nil {
		resolverMetaBytes, _ := json.Marshal(resolverMeta)
		resolverMetaJSON = string(resolverMetaBytes)
	}

	now := nowUTC()
	nowStr := util.FormatTime(now)
	expiresAt := util.FormatTime(now.Add(24 * time.Hour))
	maxAttempts := h.app.Config.ImportMaxTry
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if hasManualCandidate || forcedFallback {
		maxAttempts = 1
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
	?, ?, ?, ?,
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
		nullableString(selectedCandidateURL),
		nullableString(resolverName),
		nullableString(resolverMetaJSON),
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
	sourceType := strings.ToLower(strings.TrimSpace(c.Query("source_type")))
	if sourceType != "" && sourceType != "url" && sourceType != "torrent" {
		return response.Error(c, fiber.StatusBadRequest, "invalid source_type")
	}

	query := `
SELECT id, source_type, COALESCE(source_filename, ''), COALESCE(info_hash, ''), status,
       COALESCE(source_url, ''), COALESCE(resolved_media_url, ''), COALESCE(resolver_name, ''),
       COALESCE(ytdlp_param_mode, 'safe'),
       COALESCE(custom_title, ''), COALESCE(custom_title_prefix, ''), COALESCE(custom_description, ''),
       COALESCE(category_id, 0), tags_json, visibility,
       total_files, selected_files, completed_files, failed_files, progress,
       COALESCE(downloaded_bytes, 0), COALESCE(uploaded_bytes, 0),
       COALESCE(download_speed_bps, 0), COALESCE(upload_speed_bps, 0), COALESCE(transfer_updated_at, ''),
       COALESCE(available_at, ''), COALESCE(started_at, ''), COALESCE(finished_at, ''),
       COALESCE(expires_at, ''), COALESCE(error_message, ''),
       created_at, updated_at
FROM video_import_jobs
WHERE user_id = ?`
	args := []interface{}{uid}
	if sourceType != "" {
		query += " AND source_type = ?"
		args = append(args, sourceType)
	}

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
		CustomTitle   string
		CustomPrefix  string
		CustomDesc    string
		CategoryID    int64
		TagsJSON      string
		Visibility    string
		TotalFiles    int64
		SelectedFiles int64
		Completed     int64
		Failed        int64
		Progress      float64
		Downloaded    int64
		Uploaded      int64
		DownSpeedBPS  float64
		UpSpeedBPS    float64
		TransferAt    string
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
			&item.CustomTitle,
			&item.CustomPrefix,
			&item.CustomDesc,
			&item.CategoryID,
			&item.TagsJSON,
			&item.Visibility,
			&item.TotalFiles,
			&item.SelectedFiles,
			&item.Completed,
			&item.Failed,
			&item.Progress,
			&item.Downloaded,
			&item.Uploaded,
			&item.DownSpeedBPS,
			&item.UpSpeedBPS,
			&item.TransferAt,
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
	now := nowUTC()
	for _, job := range jobs {
		items = append(items, fiber.Map{
			"id":                  job.ID,
			"source_type":         job.SourceType,
			"source_filename":     job.SourceFile,
			"info_hash":           job.InfoHash,
			"source_url":          job.SourceURL,
			"resolved_media_url":  job.ResolvedURL,
			"resolver_name":       job.ResolverName,
			"ytdlp_param_mode":    job.YTDLPMode,
			"custom_title":        nullableString(job.CustomTitle),
			"custom_title_prefix": nullableString(job.CustomPrefix),
			"custom_description":  job.CustomDesc,
			"status":              job.Status,
			"draft_expired":       isImportDraftExpired(job.Status, job.ExpiresAt, now),
			"category_id":         nullableCategory(job.CategoryID),
			"tags":                parseImportTags(job.TagsJSON),
			"visibility":          job.Visibility,
			"total_files":         job.TotalFiles,
			"selected_files":      job.SelectedFiles,
			"completed_files":     job.Completed,
			"failed_files":        job.Failed,
			"progress":            job.Progress,
			"downloaded_bytes":    job.Downloaded,
			"uploaded_bytes":      job.Uploaded,
			"download_speed_bps":  job.DownSpeedBPS,
			"upload_speed_bps":    job.UpSpeedBPS,
			"transfer_updated_at": nullableString(job.TransferAt),
			"available_at":        job.AvailableAt,
			"started_at":          job.StartedAt,
			"finished_at":         job.FinishedAt,
			"expires_at":          job.ExpiresAt,
			"error_message":       job.ErrorMessage,
			"created_at":          job.CreatedAt,
			"updated_at":          job.UpdatedAt,
		})
	}

	return response.OK(c, fiber.Map{"items": items, "next_cursor": nextCursor})
}

func (h *Handler) ClearFinishedImportJobs(c *fiber.Ctx) error {
	uid := currentUserID(c)
	scope := strings.TrimSpace(strings.ToLower(c.Query("scope")))
	if scope == "" {
		scope = "finished"
	}
	sourceType := strings.ToLower(strings.TrimSpace(c.Query("source_type")))
	if sourceType != "" && sourceType != "url" && sourceType != "torrent" {
		return response.Error(c, fiber.StatusBadRequest, "invalid source_type")
	}
	var (
		query string
		args  []interface{}
	)
	switch scope {
	case "finished":
		query = `DELETE FROM video_import_jobs
		 WHERE user_id = ?
		   AND status IN ('succeeded', 'partial', 'failed')`
		args = []interface{}{uid}
	case "expired":
		query = `DELETE FROM video_import_jobs
		 WHERE user_id = ?
		   AND status = 'draft'
		   AND expires_at <> ''
		   AND expires_at < ?`
		args = []interface{}{uid, nowString()}
	case "all_clearable":
		query = `DELETE FROM video_import_jobs
		 WHERE user_id = ?
		   AND (
		    status IN ('succeeded', 'partial', 'failed')
		    OR (status = 'draft' AND expires_at <> '' AND expires_at < ?)
		   )`
		args = []interface{}{uid, nowString()}
	default:
		return response.Error(c, fiber.StatusBadRequest, "invalid scope")
	}
	if sourceType != "" {
		query += " AND source_type = ?"
		args = append(args, sourceType)
	}
	res, err := h.app.DB.ExecContext(c.UserContext(), query, args...)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to clear import jobs")
	}
	deleted, _ := res.RowsAffected()
	return response.OK(c, fiber.Map{"deleted": deleted})
}

func (h *Handler) CancelImportJob(c *fiber.Ctx) error {
	uid := currentUserID(c)
	jobID := strings.TrimSpace(c.Params("jobId"))
	if jobID == "" {
		return response.Error(c, fiber.StatusBadRequest, "jobId is required")
	}

	tx, err := h.app.DB.BeginTx(c.UserContext(), nil)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to begin transaction")
	}
	defer tx.Rollback()

	var (
		jobUserID string
		status    string
	)
	err = tx.QueryRowContext(c.UserContext(), `
SELECT user_id, status
FROM video_import_jobs
WHERE id = ?
LIMIT 1`, jobID).Scan(&jobUserID, &status)
	if err != nil {
		if isNotFound(err) {
			return response.Error(c, fiber.StatusNotFound, "import job not found")
		}
		return response.Error(c, fiber.StatusInternalServerError, "failed to query import job")
	}
	if jobUserID != uid {
		return response.Error(c, fiber.StatusForbidden, "forbidden")
	}
	if status != "queued" && status != "downloading" {
		return response.Error(c, fiber.StatusConflict, "import job is not cancellable")
	}

	now := nowString()
	res, err := tx.ExecContext(c.UserContext(), `
UPDATE video_import_jobs
SET status = 'failed',
	error_message = ?,
	download_speed_bps = 0,
	upload_speed_bps = 0,
	transfer_updated_at = ?,
	finished_at = ?,
	updated_at = ?
WHERE id = ?
  AND user_id = ?
  AND status IN ('queued', 'downloading')`,
		"cancelled by user",
		now,
		now,
		now,
		jobID,
		uid,
	)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to cancel import job")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return response.Error(c, fiber.StatusConflict, "import job is not cancellable")
	}

	if _, err := tx.ExecContext(c.UserContext(), `
UPDATE video_import_items
SET status = 'failed',
	error_message = ?,
	updated_at = ?
WHERE job_id = ?
  AND status IN ('pending', 'downloading')`,
		"cancelled by user",
		now,
		jobID,
	); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to update import items")
	}

	if err := tx.Commit(); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to cancel import job")
	}

	if h.app.ImportCtl != nil {
		_ = h.app.ImportCtl.CancelJob(jobID)
	}
	_ = os.RemoveAll(filepath.Join(h.app.Config.TaskTempDir, importJobTempFolderName, jobID))

	return response.OK(c, fiber.Map{
		"cancelled": true,
		"job_id":    jobID,
	})
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
		customTitle    string
		customPrefix   string
		customDesc     string
		status         string
		categoryID     sql.NullInt64
		tagsJSON       string
		visibility     string
		totalFiles     int64
		selectedFiles  int64
		completedFiles int64
		failedFiles    int64
		progress       float64
		downloaded     int64
		uploaded       int64
		downSpeedBPS   float64
		upSpeedBPS     float64
		transferAt     sql.NullString
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
       COALESCE(custom_title, ''), COALESCE(custom_title_prefix, ''), COALESCE(custom_description, ''),
       status,
       category_id, tags_json, visibility,
       total_files, selected_files, completed_files, failed_files, progress,
       COALESCE(downloaded_bytes, 0), COALESCE(uploaded_bytes, 0),
       COALESCE(download_speed_bps, 0), COALESCE(upload_speed_bps, 0),
       transfer_updated_at,
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
		&customTitle,
		&customPrefix,
		&customDesc,
		&status,
		&categoryID,
		&tagsJSON,
		&visibility,
		&totalFiles,
		&selectedFiles,
		&completedFiles,
		&failedFiles,
		&progress,
		&downloaded,
		&uploaded,
		&downSpeedBPS,
		&upSpeedBPS,
		&transferAt,
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
			"id":                  jobID,
			"source_type":         sourceType,
			"source_filename":     sourceFilename,
			"info_hash":           infoHash,
			"source_url":          sourceURL,
			"resolved_media_url":  resolvedMedia,
			"resolver_name":       resolverName,
			"ytdlp_param_mode":    ytdlpMode,
			"custom_title":        nullableString(customTitle),
			"custom_title_prefix": nullableString(customPrefix),
			"custom_description":  customDesc,
			"status":              status,
			"draft_expired":       isImportDraftExpired(status, maybeString(expiresAt), nowUTC()),
			"category_id":         nullableCategoryFromNull(categoryID),
			"tags":                parseImportTags(tagsJSON),
			"visibility":          visibility,
			"total_files":         totalFiles,
			"selected_files":      selectedFiles,
			"completed_files":     completedFiles,
			"failed_files":        failedFiles,
			"progress":            progress,
			"downloaded_bytes":    downloaded,
			"uploaded_bytes":      uploaded,
			"download_speed_bps":  downSpeedBPS,
			"upload_speed_bps":    upSpeedBPS,
			"transfer_updated_at": maybeString(transferAt),
			"available_at":        maybeString(availableAt),
			"started_at":          maybeString(startedAt),
			"finished_at":         maybeString(finishedAt),
			"expires_at":          maybeString(expiresAt),
			"error_message":       maybeString(errorMessage),
			"created_at":          createdAt,
			"updated_at":          updatedAt,
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

func shouldForceFallbackForImportURL(rawURL string, domains []string) bool {
	if len(domains) == 0 {
		return false
	}
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return appconfig.IsHostMatchedByDomainList(parsed.Hostname(), domains)
}

var allowedResolverHeaders = map[string]struct{}{
	"Accept":          {},
	"Accept-Language": {},
}

func buildURLInspectResolverContext(sourceURL string, result pageManifestResolverOutput) urlInspectResolverContext {
	ctx := urlInspectResolverContext{
		PageTitle:     truncText(strings.TrimSpace(result.Title), 300),
		PageUserAgent: truncText(strings.TrimSpace(result.PageUserAgent), 1024),
		PageReferer:   chooseValidResolverURL(result.PageReferer, sourceURL),
		PageOrigin:    chooseValidResolverOrigin(result.PageOrigin),
	}
	if ctx.PageReferer == "" {
		ctx.PageReferer = sourceURL
	}
	if len(result.PageHeaders) > 0 {
		headers := make(map[string]string, len(result.PageHeaders))
		for rawKey, rawVal := range result.PageHeaders {
			key := strings.TrimSpace(rawKey)
			val := strings.TrimSpace(rawVal)
			if _, ok := allowedResolverHeaders[key]; !ok || val == "" {
				continue
			}
			headers[key] = truncText(val, 1024)
		}
		if len(headers) > 0 {
			ctx.PageHeaders = headers
		}
	}
	return ctx
}

func chooseValidResolverURL(value string, fallback string) string {
	candidate := strings.TrimSpace(value)
	if !isValidImportURL(candidate) {
		candidate = strings.TrimSpace(fallback)
	}
	if !isValidImportURL(candidate) {
		return ""
	}
	return candidate
}

func chooseValidResolverOrigin(value string) string {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return ""
	}
	parsed, err := neturl.Parse(candidate)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func parseArgJSONArray(raw string) ([]string, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return []string{}, nil
	}
	var args []string
	if err := json.Unmarshal([]byte(content), &args); err != nil {
		return nil, fmt.Errorf("invalid arg json")
	}
	out := make([]string, 0, len(args))
	for _, item := range args {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out, nil
}

func (h *Handler) checkURLMetadataSupport(ctx context.Context, sourceURL string, extraArgs []string) (unsupported bool, err error) {
	timeout := h.app.Config.ImportURLTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"--dump-single-json", "--no-playlist", "--no-warnings"}
	args = append(args, extraArgs...)
	args = append(args, sourceURL)
	cmd := exec.CommandContext(runCtx, h.app.Config.YTDLPBin, args...)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return false, fmt.Errorf("yt-dlp metadata timeout")
		}
		outText := truncText(string(out), 400)
		if strings.Contains(strings.ToLower(string(out)), "unsupported url") {
			return true, nil
		}
		return false, fmt.Errorf("yt-dlp metadata failed: %v: %s", runErr, outText)
	}
	return false, nil
}

func (h *Handler) resolveURLImportCandidates(ctx context.Context, sourceURL string) ([]string, pageManifestResolverOutput, error) {
	timeout := h.app.Config.ImportPageResolverTimeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	maxCandidates := h.app.Config.ImportPageResolverMax
	if maxCandidates <= 0 {
		maxCandidates = 20
	}

	cmdLine := strings.TrimSpace(h.app.Config.ImportPageResolverCmd)
	if cmdLine == "" {
		cmdLine = "bun scripts/page_manifest_resolver.mjs"
	}
	cmdParts, err := splitCommandArgs(cmdLine)
	if err != nil {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("invalid IMPORT_PAGE_RESOLVER_CMD: %w", err)
	}
	if len(cmdParts) == 0 {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("IMPORT_PAGE_RESOLVER_CMD is empty")
	}

	args := append([]string{}, cmdParts[1:]...)
	args = append(args,
		"--url", sourceURL,
		"--timeout-ms", strconv.FormatInt(timeout.Milliseconds(), 10),
		"--max-candidates", strconv.Itoa(maxCandidates),
	)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, cmdParts[0], args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, pageManifestResolverOutput{}, fmt.Errorf("page resolver timeout")
		}
		errText := strings.TrimSpace(stderr.String())
		if errText == "" {
			errText = truncText(stdout.String(), 400)
		} else {
			errText = truncText(errText, 400)
		}
		return nil, pageManifestResolverOutput{}, fmt.Errorf("page resolver failed: %w: %s", err, errText)
	}

	out, err := parsePageManifestResolverOutput(strings.TrimSpace(stdout.String()))
	if err != nil {
		return nil, pageManifestResolverOutput{}, fmt.Errorf("parse page resolver output: %w", err)
	}

	candidates := normalizeResolverCandidates(sourceURL, out, maxCandidates)
	if len(candidates) == 0 {
		reason := strings.TrimSpace(out.Reason)
		if reason == "" {
			reason = "no media candidates found"
		}
		return nil, out, fmt.Errorf("%s", reason)
	}
	return candidates, out, nil
}

func parsePageManifestResolverOutput(raw string) (pageManifestResolverOutput, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return pageManifestResolverOutput{}, fmt.Errorf("empty output")
	}

	var out pageManifestResolverOutput
	if err := json.Unmarshal([]byte(text), &out); err == nil {
		return out, nil
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return pageManifestResolverOutput{}, fmt.Errorf("invalid json output")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return pageManifestResolverOutput{}, fmt.Errorf("invalid json output")
	}
	return out, nil
}

func normalizeResolverCandidates(sourceURL string, result pageManifestResolverOutput, maxCandidates int) []string {
	baseURL := strings.TrimSpace(result.FinalURL)
	if baseURL == "" {
		baseURL = sourceURL
	}

	candidates := make([]string, 0, len(result.Candidates))
	seen := make(map[string]struct{}, len(result.Candidates))
	for _, rawCandidate := range result.Candidates {
		candidate := normalizeCandidateURL(baseURL, rawCandidate)
		if candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			continue
		}
		if !looksLikeMediaCandidateURL(candidate) {
			continue
		}
		seen[key] = struct{}{}
		candidates = append(candidates, candidate)
		if maxCandidates > 0 && len(candidates) >= maxCandidates {
			break
		}
	}
	return candidates
}

func normalizeCandidateURL(baseURL, rawCandidate string) string {
	candidate := strings.TrimSpace(rawCandidate)
	if candidate == "" {
		return ""
	}
	candidate = strings.Trim(candidate, `"'`)
	if candidate == "" {
		return ""
	}
	if strings.HasPrefix(candidate, "data:") || strings.HasPrefix(candidate, "blob:") || strings.HasPrefix(candidate, "javascript:") {
		return ""
	}
	if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
		return candidate
	}
	if strings.HasPrefix(candidate, "//") {
		base, err := neturl.Parse(baseURL)
		if err != nil || base.Scheme == "" {
			return "https:" + candidate
		}
		return base.Scheme + ":" + candidate
	}
	base, err := neturl.Parse(baseURL)
	if err != nil {
		return ""
	}
	relative, err := neturl.Parse(candidate)
	if err != nil {
		return ""
	}
	return base.ResolveReference(relative).String()
}

func looksLikeMediaCandidateURL(candidate string) bool {
	lower := strings.ToLower(strings.TrimSpace(candidate))
	if lower == "" {
		return false
	}
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	switch {
	case strings.Contains(lower, ".m3u8"):
		return true
	case strings.Contains(lower, ".mp4"):
		return true
	case strings.Contains(lower, ".webm"):
		return true
	case strings.Contains(lower, ".mov"):
		return true
	case strings.Contains(lower, ".mkv"):
		return true
	case strings.Contains(lower, ".ts"):
		return true
	default:
		return false
	}
}

func (h *Handler) signURLInspectToken(payload urlInspectTokenPayload) (string, error) {
	secret := strings.TrimSpace(h.app.Config.JWTSecret)
	if secret == "" {
		return "", fmt.Errorf("JWT_SECRET is required")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadPart))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payloadPart + "." + signature, nil
}

func (h *Handler) verifyURLInspectToken(token, userID, sourceURL string) (urlInspectTokenPayload, error) {
	secret := strings.TrimSpace(h.app.Config.JWTSecret)
	if secret == "" {
		return urlInspectTokenPayload{}, fmt.Errorf("invalid inspect_token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return urlInspectTokenPayload{}, fmt.Errorf("invalid inspect_token")
	}
	payloadPart := strings.TrimSpace(parts[0])
	signPart := strings.TrimSpace(parts[1])
	if payloadPart == "" || signPart == "" {
		return urlInspectTokenPayload{}, fmt.Errorf("invalid inspect_token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadPart))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signPart), []byte(expected)) {
		return urlInspectTokenPayload{}, fmt.Errorf("invalid inspect_token")
	}

	payloadRaw, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return urlInspectTokenPayload{}, fmt.Errorf("invalid inspect_token")
	}

	var payload urlInspectTokenPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return urlInspectTokenPayload{}, fmt.Errorf("invalid inspect_token")
	}

	if strings.TrimSpace(payload.UserID) != strings.TrimSpace(userID) {
		return urlInspectTokenPayload{}, fmt.Errorf("inspect_token is not for current user")
	}
	if strings.TrimSpace(payload.SourceURL) != strings.TrimSpace(sourceURL) {
		return urlInspectTokenPayload{}, fmt.Errorf("inspect_token does not match source url")
	}
	if payload.ExpiresAt <= nowUTC().Unix() {
		return urlInspectTokenPayload{}, fmt.Errorf("inspect_token has expired")
	}
	if len(payload.Candidates) == 0 {
		return urlInspectTokenPayload{}, fmt.Errorf("inspect_token has no candidates")
	}
	payload.UserCookieID = strings.TrimSpace(payload.UserCookieID)
	for _, candidate := range payload.Candidates {
		if !isValidImportURL(candidate) {
			return urlInspectTokenPayload{}, fmt.Errorf("inspect_token has invalid candidate")
		}
	}
	payload.ResolverContext = buildURLInspectResolverContext(payload.SourceURL, pageManifestResolverOutput{
		Title:         payload.ResolverContext.PageTitle,
		PageUserAgent: payload.ResolverContext.PageUserAgent,
		PageReferer:   payload.ResolverContext.PageReferer,
		PageOrigin:    payload.ResolverContext.PageOrigin,
		PageHeaders:   payload.ResolverContext.PageHeaders,
	})
	return payload, nil
}

func truncText(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen]
}

func countUserActiveImportJobsTx(ctx context.Context, tx *sql.Tx, userID string) (int64, error) {
	var count int64
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM video_import_jobs
WHERE user_id = ?
  AND status IN ('queued', 'downloading')
`, userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
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

func isImportDraftExpired(status, expiresAt string, now time.Time) bool {
	if status != "draft" {
		return false
	}
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return false
	}
	expires, err := util.ParseTime(expiresAt)
	if err != nil {
		return false
	}
	return now.After(expires)
}
