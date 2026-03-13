package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/google/uuid"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/media"
	"moevideo/backend/internal/util"
)

type logger interface {
	Printf(format string, args ...interface{})
}

type Worker struct {
	app          *app.App
	logger       logger
	pollInterval time.Duration
}

type Option func(*Worker)

func WithLogger(l logger) Option {
	return func(w *Worker) {
		if l != nil {
			w.logger = l
		}
	}
}

func WithPollInterval(interval time.Duration) Option {
	return func(w *Worker) {
		if interval > 0 {
			w.pollInterval = interval
		}
	}
}

func NewWorker(a *app.App, opts ...Option) *Worker {
	w := &Worker{
		app:          a,
		logger:       log.Default(),
		pollInterval: a.Config.ImportPoll,
	}
	if w.pollInterval <= 0 {
		w.pollInterval = time.Second
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		hasWork, err := w.RunOnce(ctx)
		if err != nil {
			w.logger.Printf("import worker error: %v", err)
		}
		if hasWork {
			continue
		}

		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	job, err := w.claimJob(ctx)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}

	result, err := w.processJob(ctx, *job)
	if err != nil {
		if markErr := w.markJobFailure(ctx, *job, err); markErr != nil {
			return true, fmt.Errorf("import job %s failed and could not update state: %w (original: %v)", job.ID, markErr, err)
		}
		return true, nil
	}
	if err := w.markJobComplete(ctx, *job, *result); err != nil {
		return true, err
	}
	return true, nil
}

type claimedJob struct {
	ID          string
	UserID      string
	Attempts    int
	MaxAttempts int
}

func (w *Worker) claimJob(ctx context.Context) (*claimedJob, error) {
	now := nowString()

	tx, err := w.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin import claim tx: %w", err)
	}
	defer tx.Rollback()

	var job claimedJob
	err = tx.QueryRowContext(ctx, `
SELECT id, user_id, attempts, max_attempts
FROM video_import_jobs
WHERE status = 'queued' AND available_at <= ?
ORDER BY created_at ASC
LIMIT 1`, now).Scan(&job.ID, &job.UserID, &job.Attempts, &job.MaxAttempts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit empty import claim tx: %w", err)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("query queued import job: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
UPDATE video_import_jobs
SET status = 'downloading',
    attempts = attempts + 1,
    started_at = COALESCE(started_at, ?),
    updated_at = ?
WHERE id = ? AND status = 'queued'`, now, now, job.ID)
	if err != nil {
		return nil, fmt.Errorf("claim queued import job: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit import claim race tx: %w", err)
		}
		return nil, nil
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit import claim tx: %w", err)
	}

	job.Attempts++
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = max(1, w.app.Config.ImportMaxTry)
	}
	return &job, nil
}

type selectedItem struct {
	ID        string
	FileIndex int
	FilePath  string
}

type processResult struct {
	Status        string
	Completed     int
	Failed        int
	SelectedTotal int
	ErrorMessage  string
}

func (w *Worker) processJob(ctx context.Context, job claimedJob) (*processResult, error) {
	var (
		jobUserID   string
		sourceType  string
		torrentData []byte
		sourceURL   sql.NullString
		ytdlpMode   string
		ytdlpMeta   string
		ytdlpDown   string
		categoryID  sql.NullInt64
		tagsJSON    string
		visibility  string
	)
	err := w.app.DB.QueryRowContext(ctx, `
SELECT user_id, source_type, torrent_data, source_url,
       COALESCE(ytdlp_param_mode, 'safe'),
       COALESCE(ytdlp_metadata_args_json, '[]'),
       COALESCE(ytdlp_download_args_json, '[]'),
       category_id, tags_json, visibility
FROM video_import_jobs
WHERE id = ?
LIMIT 1`, job.ID).Scan(&jobUserID, &sourceType, &torrentData, &sourceURL, &ytdlpMode, &ytdlpMeta, &ytdlpDown, &categoryID, &tagsJSON, &visibility)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, permanentError{err: fmt.Errorf("import job %s not found", job.ID)}
		}
		return nil, fmt.Errorf("query import job: %w", err)
	}
	if jobUserID != job.UserID {
		return nil, permanentError{err: fmt.Errorf("import job %s owner mismatch", job.ID)}
	}

	tags := parseTags(tagsJSON)
	selectedRows, err := w.app.DB.QueryContext(ctx, `
SELECT id, file_index, file_path
FROM video_import_items
WHERE job_id = ? AND selected = 1
ORDER BY file_index ASC`, job.ID)
	if err != nil {
		return nil, fmt.Errorf("query selected import items: %w", err)
	}
	defer selectedRows.Close()

	selected := make([]selectedItem, 0)
	for selectedRows.Next() {
		var item selectedItem
		if err := selectedRows.Scan(&item.ID, &item.FileIndex, &item.FilePath); err != nil {
			return nil, fmt.Errorf("scan selected import items: %w", err)
		}
		selected = append(selected, item)
	}
	if err := selectedRows.Err(); err != nil {
		return nil, fmt.Errorf("read selected import items: %w", err)
	}
	if len(selected) == 0 {
		return nil, permanentError{err: fmt.Errorf("import job %s has no selected files", job.ID)}
	}

	var categoryPtr *int64
	if categoryID.Valid {
		v := categoryID.Int64
		categoryPtr = &v
	}

	switch sourceType {
	case "url":
		metaArgs, downArgs, parseErr := parseJobYTDLPArgs(ytdlpMode, ytdlpMeta, ytdlpDown)
		if parseErr != nil {
			return nil, permanentError{err: fmt.Errorf("import.ytdlp.invalid_args: %v", parseErr)}
		}
		return w.processURLJob(ctx, job, selected, strings.TrimSpace(sourceURL.String), categoryPtr, tags, visibility, metaArgs, downArgs)
	case "torrent":
	default:
		return nil, permanentError{err: fmt.Errorf("import job %s has unsupported source_type: %s", job.ID, sourceType)}
	}

	if len(torrentData) == 0 {
		return nil, permanentError{err: fmt.Errorf("import job %s has empty torrent payload", job.ID)}
	}

	tmpDir, err := os.MkdirTemp("", "moevideo-import-*")
	if err != nil {
		return nil, fmt.Errorf("create import temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	clientCfg := torrent.NewDefaultClientConfig()
	clientCfg.DataDir = filepath.Join(tmpDir, "torrent-data")
	clientCfg.NoUpload = true
	clientCfg.Seed = false
	clientCfg.NoDefaultPortForwarding = true
	clientCfg.ListenPort = 0

	client, err := torrent.NewClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	mi, err := metainfo.Load(bytes.NewReader(torrentData))
	if err != nil {
		return nil, permanentError{err: fmt.Errorf("parse torrent data: %w", err)}
	}
	tor, err := client.AddTorrent(mi)
	if err != nil {
		return nil, fmt.Errorf("add torrent: %w", err)
	}

	waitCtx, cancelWait := context.WithTimeout(ctx, 45*time.Second)
	defer cancelWait()
	select {
	case <-waitCtx.Done():
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("wait torrent metadata timeout")
		}
		return nil, waitCtx.Err()
	case <-tor.GotInfo():
	}

	files := tor.Files()
	if len(files) == 0 {
		return nil, permanentError{err: fmt.Errorf("torrent has no files")}
	}

	completedCount := 0
	failedCount := 0
	lastErr := ""
	selectedTotal := len(selected)

	for _, item := range selected {
		if err := w.markItemStatus(ctx, item.ID, "downloading", "", "", ""); err != nil {
			return nil, fmt.Errorf("mark import item downloading: %w", err)
		}

		if item.FileIndex < 0 || item.FileIndex >= len(files) {
			failedCount++
			lastErr = "selected file index does not exist in torrent"
			_ = w.markItemStatus(ctx, item.ID, "failed", lastErr, "", "")
			_ = w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal)
			continue
		}

		file := files[item.FileIndex]
		file.Download()

		importErr := w.importSelectedFile(ctx, job, item, file, categoryPtr, tags, visibility)
		if importErr != nil {
			failedCount++
			lastErr = truncateErr(importErr, 600)
			_ = w.markItemStatus(ctx, item.ID, "failed", lastErr, "", "")
			_ = w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal)
			continue
		}

		completedCount++
		if err := w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal); err != nil {
			return nil, fmt.Errorf("update import job progress: %w", err)
		}
	}

	status := "failed"
	switch {
	case completedCount > 0 && failedCount == 0:
		status = "succeeded"
		lastErr = ""
	case completedCount > 0 && failedCount > 0:
		status = "partial"
		if lastErr == "" {
			lastErr = "some files failed to import"
		}
	default:
		status = "failed"
		if lastErr == "" {
			lastErr = "all selected files failed to import"
		}
	}

	return &processResult{
		Status:        status,
		Completed:     completedCount,
		Failed:        failedCount,
		SelectedTotal: selectedTotal,
		ErrorMessage:  lastErr,
	}, nil
}

type ytDLPMetadata struct {
	Title              string  `json:"title"`
	Duration           float64 `json:"duration"`
	URL                string  `json:"url"`
	WebpageURL         string  `json:"webpage_url"`
	Extractor          string  `json:"extractor"`
	ExtractorKey       string  `json:"extractor_key"`
	Filesize           int64   `json:"filesize"`
	FilesizeApprox     int64   `json:"filesize_approx"`
	RequestedDownloads []struct {
		URL string `json:"url"`
	} `json:"requested_downloads"`
}

func (w *Worker) processURLJob(
	ctx context.Context,
	job claimedJob,
	selected []selectedItem,
	sourceURL string,
	categoryID *int64,
	tags []string,
	visibility string,
	metadataArgs []string,
	downloadArgs []string,
) (*processResult, error) {
	if strings.TrimSpace(sourceURL) == "" && len(selected) > 0 {
		sourceURL = strings.TrimSpace(selected[0].FilePath)
	}
	if strings.TrimSpace(sourceURL) == "" {
		return nil, permanentError{err: fmt.Errorf("import job %s has empty source_url", job.ID)}
	}

	completedCount := 0
	failedCount := 0
	lastErr := ""
	selectedTotal := len(selected)

	for _, item := range selected {
		if err := w.markItemStatus(ctx, item.ID, "downloading", "", "", ""); err != nil {
			return nil, fmt.Errorf("mark import item downloading: %w", err)
		}

		importErr := w.importURLItem(ctx, job, item, sourceURL, categoryID, tags, visibility, metadataArgs, downloadArgs)
		if importErr != nil {
			failedCount++
			lastErr = truncateErr(importErr, 600)
			_ = w.markItemStatus(ctx, item.ID, "failed", lastErr, "", "")
			_ = w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal)
			continue
		}

		completedCount++
		if err := w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal); err != nil {
			return nil, fmt.Errorf("update import job progress: %w", err)
		}
	}

	status := "failed"
	switch {
	case completedCount > 0 && failedCount == 0:
		status = "succeeded"
		lastErr = ""
	case completedCount > 0 && failedCount > 0:
		status = "partial"
		if lastErr == "" {
			lastErr = "some files failed to import"
		}
	default:
		status = "failed"
		if lastErr == "" {
			lastErr = "all selected files failed to import"
		}
	}

	return &processResult{
		Status:        status,
		Completed:     completedCount,
		Failed:        failedCount,
		SelectedTotal: selectedTotal,
		ErrorMessage:  lastErr,
	}, nil
}

func (w *Worker) importURLItem(
	ctx context.Context,
	job claimedJob,
	item selectedItem,
	sourceURL string,
	categoryID *int64,
	tags []string,
	visibility string,
	metadataArgs []string,
	downloadArgs []string,
) error {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return permanentError{err: fmt.Errorf("source_url is required")}
	}

	meta, err := w.extractURLMetadata(ctx, sourceURL, metadataArgs)
	if err != nil {
		return err
	}
	if w.app.Config.ImportURLMaxDur > 0 && meta.Duration > 0 && int64(meta.Duration) > w.app.Config.ImportURLMaxDur {
		return permanentError{err: fmt.Errorf("video duration exceeds limit (%ds)", w.app.Config.ImportURLMaxDur)}
	}
	metaSize := meta.Filesize
	if metaSize <= 0 {
		metaSize = meta.FilesizeApprox
	}
	if w.app.Config.ImportURLMaxFile > 0 && metaSize > 0 && metaSize > w.app.Config.ImportURLMaxFile {
		return permanentError{err: fmt.Errorf("video file size exceeds limit (%d MB)", w.app.Config.ImportURLMaxFile/1024/1024)}
	}

	tmpDir, err := os.MkdirTemp("", "moevideo-import-url-*")
	if err != nil {
		return fmt.Errorf("create import temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	localSourcePath, err := w.downloadURLSource(ctx, sourceURL, tmpDir, downloadArgs)
	if err != nil {
		return err
	}

	stat, err := os.Stat(localSourcePath)
	if err != nil {
		return fmt.Errorf("stat downloaded file: %w", err)
	}
	if stat.Size() <= 0 {
		return permanentError{err: fmt.Errorf("downloaded file is empty")}
	}
	if w.app.Config.ImportURLMaxFile > 0 && stat.Size() > w.app.Config.ImportURLMaxFile {
		return permanentError{err: fmt.Errorf("video file size exceeds limit (%d MB)", w.app.Config.ImportURLMaxFile/1024/1024)}
	}

	sourceName := buildURLSourceName(meta.Title, localSourcePath)
	if err := w.persistImportedVideo(ctx, job, item, localSourcePath, sourceName, categoryID, tags, visibility); err != nil {
		return err
	}

	resolvedMediaURL := strings.TrimSpace(meta.URL)
	if resolvedMediaURL == "" && len(meta.RequestedDownloads) > 0 {
		resolvedMediaURL = strings.TrimSpace(meta.RequestedDownloads[0].URL)
	}
	resolverName := strings.TrimSpace(meta.ExtractorKey)
	if resolverName == "" {
		resolverName = strings.TrimSpace(meta.Extractor)
	}
	resolverMetaJSON, _ := json.Marshal(map[string]interface{}{
		"title":        strings.TrimSpace(meta.Title),
		"duration_sec": int64(meta.Duration),
		"webpage_url":  strings.TrimSpace(meta.WebpageURL),
	})
	if err := w.updateURLResolverFields(
		ctx,
		job.ID,
		buildImportVideoTitle(sourceName),
		resolvedMediaURL,
		resolverName,
		string(resolverMetaJSON),
	); err != nil {
		return fmt.Errorf("update url resolver fields: %w", err)
	}
	return nil
}

func (w *Worker) extractURLMetadata(ctx context.Context, sourceURL string, extraArgs []string) (ytDLPMetadata, error) {
	timeout := w.app.Config.ImportURLTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"--dump-single-json", "--no-playlist", "--no-warnings"}
	args = append(args, extraArgs...)
	args = append(args, sourceURL)
	cmd := exec.CommandContext(runCtx, w.app.Config.YTDLPBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return ytDLPMetadata{}, fmt.Errorf("yt-dlp metadata timeout")
		}
		return ytDLPMetadata{}, fmt.Errorf("yt-dlp metadata failed: %w: %s", err, truncateOutput(out, 400))
	}

	var meta ytDLPMetadata
	if err := json.Unmarshal(out, &meta); err != nil {
		return ytDLPMetadata{}, permanentError{err: fmt.Errorf("parse yt-dlp metadata: %w", err)}
	}
	return meta, nil
}

func (w *Worker) downloadURLSource(ctx context.Context, sourceURL, tmpDir string, extraArgs []string) (string, error) {
	timeout := w.app.Config.ImportURLTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	outputTemplate := filepath.Join(tmpDir, "source.%(ext)s")
	args := []string{
		"--no-playlist",
		"--no-progress",
		"--no-warnings",
		"--restrict-filenames",
		"-o", outputTemplate,
	}
	args = append(args, extraArgs...)
	args = append(args, sourceURL)
	cmd := exec.CommandContext(runCtx, w.app.Config.YTDLPBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("yt-dlp download timeout")
		}
		return "", fmt.Errorf("yt-dlp download failed: %w: %s", err, truncateOutput(out, 400))
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "source.*"))
	if err != nil {
		return "", fmt.Errorf("resolve downloaded file: %w", err)
	}
	var localSourcePath string
	for _, candidate := range matches {
		info, statErr := os.Stat(candidate)
		if statErr != nil || info.IsDir() {
			continue
		}
		if strings.HasSuffix(candidate, ".part") || strings.HasSuffix(candidate, ".tmp") {
			continue
		}
		if localSourcePath == "" {
			localSourcePath = candidate
			continue
		}
		prev, prevErr := os.Stat(localSourcePath)
		if prevErr == nil && info.Size() > prev.Size() {
			localSourcePath = candidate
		}
	}
	if localSourcePath == "" {
		return "", permanentError{err: fmt.Errorf("yt-dlp did not produce a media file")}
	}

	ext := strings.ToLower(filepath.Ext(localSourcePath))
	if _, ok := allowedImportVideoExts[ext]; !ok {
		return "", permanentError{err: fmt.Errorf("downloaded format is not supported: %s", ext)}
	}
	return localSourcePath, nil
}

func (w *Worker) importSelectedFile(
	ctx context.Context,
	job claimedJob,
	item selectedItem,
	file *torrent.File,
	categoryID *int64,
	tags []string,
	visibility string,
) error {
	tmpDir, err := os.MkdirTemp("", "moevideo-import-file-*")
	if err != nil {
		return fmt.Errorf("create temp file dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ext := strings.ToLower(filepath.Ext(item.FilePath))
	if ext == "" {
		ext = ".mp4"
	}
	localSourcePath := filepath.Join(tmpDir, "source"+ext)

	reader := file.NewReader()
	defer reader.Close()
	fileCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	reader.SetContext(fileCtx)
	reader.SetResponsive()
	reader.SetReadahead(1 << 20)

	if err := os.MkdirAll(filepath.Dir(localSourcePath), 0o755); err != nil {
		return fmt.Errorf("prepare local source path: %w", err)
	}
	out, err := os.Create(localSourcePath)
	if err != nil {
		return fmt.Errorf("create local source file: %w", err)
	}
	if _, err := io.Copy(out, reader); err != nil {
		out.Close()
		return fmt.Errorf("download torrent file: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close local source file: %w", err)
	}

	return w.persistImportedVideo(ctx, job, item, localSourcePath, item.FilePath, categoryID, tags, visibility)
}

func (w *Worker) persistImportedVideo(
	ctx context.Context,
	job claimedJob,
	item selectedItem,
	localSourcePath string,
	sourcePath string,
	categoryID *int64,
	tags []string,
	visibility string,
) error {
	stat, err := os.Stat(localSourcePath)
	if err != nil {
		return fmt.Errorf("stat local source file: %w", err)
	}
	if stat.Size() <= 0 {
		return permanentError{err: fmt.Errorf("downloaded file is empty")}
	}

	ext := strings.ToLower(filepath.Ext(sourcePath))
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(localSourcePath))
	}
	if ext == "" {
		ext = ".mp4"
	}

	objectKey := buildImportObjectKey(job.UserID, job.ID, item.ID, sourcePath)
	contentType := videoMIMEByExtension(ext)
	if err := w.app.Storage.UploadFile(ctx, objectKey, contentType, localSourcePath); err != nil {
		return fmt.Errorf("upload imported file: %w", err)
	}

	durationSec, width, height, probeErr := media.ProbeVideoFileMetadata(ctx, w.app.Config.FFprobeBin, localSourcePath)
	if probeErr != nil {
		w.logger.Printf("import item %s probe metadata failed (soft): %v", item.ID, probeErr)
	}

	now := nowString()
	provider := w.app.Storage.Driver()
	bucket := ""
	if provider == "s3" {
		bucket = w.app.Storage.Bucket()
	}

	videoTitle := buildImportVideoTitle(sourcePath)
	videoID := uuid.NewString()
	mediaID := uuid.NewString()
	transcodeJobID := uuid.NewString()
	maxTranscodeAttempts := w.app.Config.TranscodeMaxTry
	if maxTranscodeAttempts <= 0 {
		maxTranscodeAttempts = 3
	}

	tx, err := w.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import persist tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
INSERT INTO media_objects (
	id, provider, bucket, object_key, original_filename, mime_type, size_bytes,
	checksum_sha256, duration_sec, width, height, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)`,
		mediaID,
		provider,
		nullableString(bucket),
		objectKey,
		filepath.Base(sourcePath),
		contentType,
		stat.Size(),
		durationSec,
		nullableInt(width),
		nullableInt(height),
		job.UserID,
		now,
	)
	if err != nil {
		return fmt.Errorf("insert media object: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO videos (
	id, uploader_id, title, description, category_id, cover_media_id, source_media_id,
	status, visibility, duration_sec, published_at, views_count, likes_count,
	favorites_count, comments_count, shares_count, hot_score, created_at, updated_at
) VALUES (?, ?, ?, '', ?, NULL, ?,
	'processing', ?, ?, NULL, 0, 0,
	0, 0, 0, 0, ?, ?)`,
		videoID,
		job.UserID,
		videoTitle,
		categoryID,
		mediaID,
		visibility,
		durationSec,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("insert video: %w", err)
	}

	if err := attachTagsTx(ctx, tx, videoID, tags, now); err != nil {
		return fmt.Errorf("attach tags: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO video_transcode_jobs (
	id, video_id, status, attempts, max_attempts, last_error,
	available_at, locked_at, started_at, finished_at, created_at, updated_at
) VALUES (?, ?, 'queued', 0, ?, NULL, ?, NULL, NULL, NULL, ?, ?)`,
		transcodeJobID,
		videoID,
		maxTranscodeAttempts,
		now,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("insert transcode job: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
UPDATE video_import_items
SET status = 'completed',
	file_path = ?,
	file_size_bytes = ?,
	error_message = NULL,
	media_object_id = ?,
	video_id = ?,
	updated_at = ?
WHERE id = ?`, sourcePath, stat.Size(), mediaID, videoID, now, item.ID)
	if err != nil {
		return fmt.Errorf("update import item completed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import persist tx: %w", err)
	}
	return nil
}

func (w *Worker) markItemStatus(ctx context.Context, itemID, status, errMsg, mediaObjectID, videoID string) error {
	_, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_items
SET status = ?,
	error_message = ?,
	media_object_id = ?,
	video_id = ?,
	updated_at = ?
WHERE id = ?`,
		status,
		nullableString(errMsg),
		nullableString(mediaObjectID),
		nullableString(videoID),
		nowString(),
		itemID,
	)
	return err
}

func (w *Worker) updateURLResolverFields(ctx context.Context, jobID, sourceFilename, resolvedMediaURL, resolverName, resolverMetaJSON string) error {
	_, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET source_filename = COALESCE(NULLIF(?, ''), source_filename),
	resolved_media_url = COALESCE(NULLIF(?, ''), resolved_media_url),
	resolver_name = COALESCE(NULLIF(?, ''), resolver_name),
	resolver_meta_json = COALESCE(NULLIF(?, ''), resolver_meta_json),
	updated_at = ?
WHERE id = ?`,
		sourceFilename,
		resolvedMediaURL,
		resolverName,
		resolverMetaJSON,
		nowString(),
		jobID,
	)
	return err
}

func (w *Worker) updateJobProgress(ctx context.Context, jobID string, completed, failed, total int) error {
	progress := 0.0
	if total > 0 {
		progress = (float64(completed+failed) / float64(total)) * 100
		if progress > 100 {
			progress = 100
		}
	}
	_, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET completed_files = ?,
	failed_files = ?,
	progress = ?,
	updated_at = ?
WHERE id = ?`, completed, failed, progress, nowString(), jobID)
	return err
}

func (w *Worker) markJobComplete(ctx context.Context, job claimedJob, result processResult) error {
	now := nowString()
	progress := 100.0
	if result.SelectedTotal > 0 {
		progress = (float64(result.Completed+result.Failed) / float64(result.SelectedTotal)) * 100
		if progress > 100 {
			progress = 100
		}
	}

	_, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET status = ?,
	completed_files = ?,
	failed_files = ?,
	progress = ?,
	error_message = ?,
	finished_at = ?,
	updated_at = ?
WHERE id = ?`,
		result.Status,
		result.Completed,
		result.Failed,
		progress,
		nullableString(result.ErrorMessage),
		now,
		now,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("mark import job complete: %w", err)
	}
	return nil
}

func (w *Worker) markJobFailure(ctx context.Context, job claimedJob, cause error) error {
	now := nowUTC()
	nowStr := util.FormatTime(now)
	msg := truncateErr(cause, 1000)
	var pErr permanentError
	retryable := !errors.As(cause, &pErr)
	shouldRetry := retryable && job.Attempts < job.MaxAttempts

	if shouldRetry {
		nextRun := util.FormatTime(now.Add(backoffForAttempt(job.Attempts)))
		_, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET status = 'queued',
	error_message = ?,
	available_at = ?,
	updated_at = ?
WHERE id = ?`, msg, nextRun, nowStr, job.ID)
		if err != nil {
			return fmt.Errorf("mark import retryable failure: %w", err)
		}
		w.logger.Printf("import job %s retry scheduled (attempt %d/%d): %s", job.ID, job.Attempts, job.MaxAttempts, msg)
		return nil
	}

	_, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET status = 'failed',
	error_message = ?,
	finished_at = ?,
	updated_at = ?
WHERE id = ?`, msg, nowStr, nowStr, job.ID)
	if err != nil {
		return fmt.Errorf("mark import final failure: %w", err)
	}
	w.logger.Printf("import job %s failed permanently: %s", job.ID, msg)
	return nil
}

type permanentError struct {
	err error
}

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
}

const (
	workerYTDLPModeSafe     = "safe"
	workerYTDLPModeAdvanced = "advanced"
)

var workerBlockedYTDLPArgs = map[string]struct{}{
	"--exec":                 {},
	"--exec-before-download": {},
	"-o":                     {},
	"--output":               {},
	"-p":                     {},
	"--paths":                {},
	"--config-locations":     {},
	"--batch-file":           {},
}

var workerBlockedYTDLPArgPrefixes = []string{
	"--exec=",
	"--exec-before-download=",
	"--output=",
	"--paths=",
	"--config-locations=",
	"--batch-file=",
}

func parseJobYTDLPArgs(modeRaw, metaJSONRaw, downJSONRaw string) ([]string, []string, error) {
	mode := strings.ToLower(strings.TrimSpace(modeRaw))
	if mode == "" {
		mode = workerYTDLPModeSafe
	}
	if mode != workerYTDLPModeSafe && mode != workerYTDLPModeAdvanced {
		return nil, nil, fmt.Errorf("unknown ytdlp_param_mode")
	}

	metaArgs, err := parseArgJSON(metaJSONRaw)
	if err != nil {
		return nil, nil, err
	}
	downArgs, err := parseArgJSON(downJSONRaw)
	if err != nil {
		return nil, nil, err
	}
	if err := validateWorkerYTDLPArgs(metaArgs); err != nil {
		return nil, nil, err
	}
	if err := validateWorkerYTDLPArgs(downArgs); err != nil {
		return nil, nil, err
	}
	return metaArgs, downArgs, nil
}

func parseArgJSON(raw string) ([]string, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return []string{}, nil
	}
	var args []string
	if err := json.Unmarshal([]byte(content), &args); err != nil {
		return nil, fmt.Errorf("invalid arg json")
	}
	out := make([]string, 0, len(args))
	for _, token := range args {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out, nil
}

func validateWorkerYTDLPArgs(args []string) error {
	for _, token := range args {
		normalized := strings.ToLower(strings.TrimSpace(token))
		if normalized == "" {
			continue
		}
		if len(normalized) > 2048 {
			return fmt.Errorf("yt-dlp arg is too long")
		}
		if _, ok := workerBlockedYTDLPArgs[normalized]; ok {
			return fmt.Errorf("blocked yt-dlp arg: %s", token)
		}
		for _, prefix := range workerBlockedYTDLPArgPrefixes {
			if strings.HasPrefix(normalized, prefix) {
				return fmt.Errorf("blocked yt-dlp arg: %s", token)
			}
		}
		if strings.HasPrefix(normalized, "-o") && len(normalized) > 2 {
			return fmt.Errorf("blocked yt-dlp arg: %s", token)
		}
		if strings.HasPrefix(normalized, "-p") && len(normalized) > 2 {
			return fmt.Errorf("blocked yt-dlp arg: %s", token)
		}
	}
	return nil
}

var importUnsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
var allowedImportVideoExts = map[string]struct{}{
	".mp4":  {},
	".mov":  {},
	".avi":  {},
	".webm": {},
	".mkv":  {},
	".flv":  {},
	".mpeg": {},
	".mpg":  {},
	".3gp":  {},
	".m4v":  {},
	".ts":   {},
}

func buildImportObjectKey(userID, jobID, itemID, sourcePath string) string {
	ext := strings.ToLower(filepath.Ext(sourcePath))
	base := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	base = strings.TrimSpace(base)
	if base == "" {
		base = "video"
	}
	base = importUnsafeChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-._")
	if base == "" {
		base = "video"
	}
	if len(base) > 64 {
		base = base[:64]
	}
	return filepath.ToSlash(filepath.Join("video", userID, "import-"+jobID, itemID, base+ext))
}

func buildImportVideoTitle(sourcePath string) string {
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)))
	if title == "" {
		title = "导入视频"
	}
	if len(title) > 120 {
		title = title[:120]
	}
	return title
}

func buildURLSourceName(title, localPath string) string {
	ext := strings.ToLower(filepath.Ext(localPath))
	if ext == "" {
		ext = ".mp4"
	}
	base := strings.TrimSpace(title)
	if base == "" {
		base = strings.TrimSpace(strings.TrimSuffix(filepath.Base(localPath), filepath.Ext(localPath)))
	}
	if base == "" {
		base = "导入视频"
	}
	base = importUnsafeChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-._")
	if base == "" {
		base = "video"
	}
	if len(base) > 120 {
		base = base[:120]
	}
	return base + ext
}

func videoMIMEByExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".flv":
		return "video/x-flv"
	case ".mpeg", ".mpg":
		return "video/mpeg"
	case ".3gp":
		return "video/3gpp"
	case ".m4v":
		return "video/x-m4v"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

func attachTagsTx(ctx context.Context, tx *sql.Tx, videoID string, tags []string, now string) error {
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
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

		if _, err := tx.ExecContext(ctx, `
INSERT INTO tags (name, use_count, created_at)
VALUES (?, 0, ?)
ON CONFLICT(name) DO NOTHING`, tag, now); err != nil {
			return err
		}

		var tagID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? LIMIT 1`, tag).Scan(&tagID); err != nil {
			return err
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

func parseTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
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

func nowUTC() time.Time {
	return util.NowUTC()
}

func nowString() string {
	return util.FormatTime(nowUTC())
}

func truncateErr(err error, limit int) string {
	msg := strings.TrimSpace(err.Error())
	if limit <= 0 || len(msg) <= limit {
		return msg
	}
	if limit < 120 {
		return msg[len(msg)-limit:]
	}
	head := limit / 3
	if head > 320 {
		head = 320
	}
	tail := limit - head - 20
	if tail < 80 {
		tail = 80
	}
	if tail > len(msg) {
		tail = len(msg)
	}
	return msg[:head] + "\n...[truncated]...\n" + msg[len(msg)-tail:]
}

func truncateOutput(out []byte, limit int) string {
	msg := strings.TrimSpace(string(out))
	if limit <= 0 || len(msg) <= limit {
		return msg
	}
	if limit < 120 {
		return msg[len(msg)-limit:]
	}
	head := limit / 3
	if head > 320 {
		head = 320
	}
	tail := limit - head - 20
	if tail < 80 {
		tail = 80
	}
	if tail > len(msg) {
		tail = len(msg)
	}
	return msg[:head] + "\n...[truncated]...\n" + msg[len(msg)-tail:]
}

func backoffForAttempt(attempt int) time.Duration {
	if attempt <= 0 {
		return 5 * time.Second
	}
	seconds := attempt * attempt * 5
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func nullableString(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nullableInt(v int64) interface{} {
	if v <= 0 {
		return nil
	}
	return v
}
