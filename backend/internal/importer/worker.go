package importer

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/google/uuid"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/logging"
	"moevideo/backend/internal/media"
	"moevideo/backend/internal/util"
)

type logger interface {
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

type Worker struct {
	app                 *app.App
	logger              logger
	pollInterval        time.Duration
	progressLogInterval time.Duration
	cancelMu            sync.Mutex
	runningJobCancels   map[string]context.CancelFunc
}

const importJobTempFolderName = "import-jobs"

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

func WithProgressLogInterval(interval time.Duration) Option {
	return func(w *Worker) {
		if interval > 0 {
			w.progressLogInterval = interval
		}
	}
}

func NewWorker(a *app.App, opts ...Option) *Worker {
	defaultLogger, _ := logging.New("info")
	w := &Worker{
		app:                 a,
		logger:              defaultLogger.WithPrefix("module=import"),
		pollInterval:        a.Config.ImportPoll,
		progressLogInterval: a.Config.ImportProgressLogInterval,
		runningJobCancels:   map[string]context.CancelFunc{},
	}
	if w.pollInterval <= 0 {
		w.pollInterval = time.Second
	}
	if w.progressLogInterval <= 0 {
		w.progressLogInterval = 5 * time.Second
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *Worker) CancelJob(jobID string) bool {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return false
	}
	w.cancelMu.Lock()
	cancel, ok := w.runningJobCancels[jobID]
	w.cancelMu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

func (w *Worker) registerJobCancel(jobID string, cancel context.CancelFunc) {
	w.cancelMu.Lock()
	w.runningJobCancels[jobID] = cancel
	w.cancelMu.Unlock()
}

func (w *Worker) unregisterJobCancel(jobID string) {
	w.cancelMu.Lock()
	delete(w.runningJobCancels, jobID)
	w.cancelMu.Unlock()
}

func (w *Worker) Run(ctx context.Context) {
	w.logger.Infof("import worker started poll_interval=%s progress_log_interval=%s", w.pollInterval, w.progressLogInterval)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		hasWork, err := w.RunOnce(ctx)
		if err != nil {
			w.logger.Errorf("import worker loop error: %v", err)
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
	w.logger.Infof("job claimed job_id=%s user_id=%s attempt=%d/%d", job.ID, job.UserID, job.Attempts, job.MaxAttempts)

	jobCtx, cancelJob := context.WithCancel(ctx)
	w.registerJobCancel(job.ID, cancelJob)
	defer func() {
		cancelJob()
		w.unregisterJobCancel(job.ID)
	}()

	result, err := w.processJob(jobCtx, *job)
	if err != nil {
		w.logger.Errorf("job processing failed job_id=%s user_id=%s err=%v", job.ID, job.UserID, err)
		if markErr := w.markJobFailure(ctx, *job, err); markErr != nil {
			return true, fmt.Errorf("import job %s failed and could not update state: %w (original: %v)", job.ID, markErr, err)
		}
		return true, nil
	}
	if err := w.markJobComplete(ctx, *job, *result); err != nil {
		return true, err
	}
	w.logger.Infof(
		"job completed job_id=%s user_id=%s status=%s completed=%d failed=%d selected_total=%d",
		job.ID,
		job.UserID,
		result.Status,
		result.Completed,
		result.Failed,
		result.SelectedTotal,
	)
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
    downloaded_bytes = 0,
    uploaded_bytes = 0,
    download_speed_bps = 0,
    upload_speed_bps = 0,
    transfer_updated_at = NULL,
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
		jobUserID         string
		sourceType        string
		torrentData       []byte
		sourceURL         sql.NullString
		resolvedMediaURL  sql.NullString
		resolverName      sql.NullString
		resolverMetaJSON  sql.NullString
		ytdlpMode         string
		ytdlpMeta         string
		ytdlpDown         string
		customTitle       string
		customTitlePrefix string
		customDescription string
		categoryID        sql.NullInt64
		tagsJSON          string
		visibility        string
	)
	err := w.app.DB.QueryRowContext(ctx, `
	SELECT user_id, source_type, torrent_data, source_url,
	       COALESCE(resolved_media_url, ''), COALESCE(resolver_name, ''), COALESCE(resolver_meta_json, ''),
	       COALESCE(ytdlp_param_mode, 'safe'),
	       COALESCE(ytdlp_metadata_args_json, '[]'),
	       COALESCE(ytdlp_download_args_json, '[]'),
	       COALESCE(custom_title, ''),
	       COALESCE(custom_title_prefix, ''),
	       COALESCE(custom_description, ''),
	       category_id, tags_json, visibility
	FROM video_import_jobs
	WHERE id = ?
	LIMIT 1`, job.ID).Scan(&jobUserID, &sourceType, &torrentData, &sourceURL, &resolvedMediaURL, &resolverName, &resolverMetaJSON, &ytdlpMode, &ytdlpMeta, &ytdlpDown, &customTitle, &customTitlePrefix, &customDescription, &categoryID, &tagsJSON, &visibility)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, permanentError{err: fmt.Errorf("import job %s not found", job.ID)}
		}
		return nil, fmt.Errorf("query import job: %w", err)
	}
	if jobUserID != job.UserID {
		return nil, permanentError{err: fmt.Errorf("import job %s owner mismatch", job.ID)}
	}
	w.logger.Infof(
		"job context loaded job_id=%s user_id=%s source_type=%s visibility=%s category_id=%v",
		job.ID,
		job.UserID,
		sourceType,
		visibility,
		categoryID,
	)

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
	w.logger.Infof("selected items ready job_id=%s selected_total=%d", job.ID, len(selected))

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
		return w.processURLJob(
			ctx,
			job,
			selected,
			strings.TrimSpace(sourceURL.String),
			categoryPtr,
			tags,
			visibility,
			customTitle,
			customDescription,
			metaArgs,
			downArgs,
			strings.TrimSpace(resolvedMediaURL.String),
			strings.TrimSpace(resolverName.String),
			strings.TrimSpace(resolverMetaJSON.String),
		)
	case "torrent":
	default:
		return nil, permanentError{err: fmt.Errorf("import job %s has unsupported source_type: %s", job.ID, sourceType)}
	}

	if len(torrentData) == 0 {
		return nil, permanentError{err: fmt.Errorf("import job %s has empty torrent payload", job.ID)}
	}

	tmpParent, err := w.ensureImportJobTempDir(job.ID, "torrent")
	if err != nil {
		return nil, fmt.Errorf("prepare import temp dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(tmpParent, "job-*")
	if err != nil {
		return nil, fmt.Errorf("create import temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	clientCfg := torrent.NewDefaultClientConfig()
	clientCfg.DataDir = filepath.Join(tmpDir, "torrent-data")
	clientCfg.NoUpload = !w.app.Config.ImportBTEnableUpload
	clientCfg.Seed = false
	clientCfg.NoDefaultPortForwarding = !w.app.Config.ImportBTEnablePortForward
	clientCfg.ListenPort = w.app.Config.ImportBTListenPort

	client, err := torrent.NewClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}
	w.logger.Infof(
		"torrent client initialized job_id=%s upload_enabled=%t listen_port=%d port_forward_enabled=%t reader_readahead_mb=%d speed_smooth_window_sec=%d",
		job.ID,
		!clientCfg.NoUpload,
		clientCfg.ListenPort,
		!clientCfg.NoDefaultPortForwarding,
		w.app.Config.ImportBTReaderReadaheadBytes/1024/1024,
		w.app.Config.ImportBTSpeedSmoothWindowSec,
	)
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
	w.logger.Infof("torrent metadata ready job_id=%s", job.ID)

	files := tor.Files()
	if len(files) == 0 {
		return nil, permanentError{err: fmt.Errorf("torrent has no files")}
	}
	w.logger.Infof("torrent files enumerated job_id=%s total_files=%d selected_files=%d", job.ID, len(files), len(selected))

	for _, item := range selected {
		if item.FileIndex >= 0 && item.FileIndex < len(files) {
			files[item.FileIndex].Download()
		}
	}

	completedCount := 0
	failedCount := 0
	lastErr := ""
	selectedTotal := len(selected)
	downloadedTotal := int64(0)

	for _, item := range selected {
		w.logger.Infof("torrent item start job_id=%s item_id=%s file_index=%d file_path=%s", job.ID, item.ID, item.FileIndex, item.FilePath)
		if err := w.markItemStatus(ctx, item.ID, "downloading", "", "", ""); err != nil {
			return nil, fmt.Errorf("mark import item downloading: %w", err)
		}

		if item.FileIndex < 0 || item.FileIndex >= len(files) {
			failedCount++
			lastErr = "selected file index does not exist in torrent"
			w.logger.Warnf("torrent item failed job_id=%s item_id=%s reason=%s", job.ID, item.ID, lastErr)
			_ = w.markItemStatus(ctx, item.ID, "failed", lastErr, "", "")
			_ = w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal)
			continue
		}

		file := files[item.FileIndex]

		importErr := w.importSelectedFile(
			ctx,
			job,
			item,
			file,
			tor,
			downloadedTotal,
			categoryPtr,
			tags,
			visibility,
			len(selected),
			customTitle,
			customTitlePrefix,
			customDescription,
		)
		downloadedNow := downloadedTotal + max(0, file.BytesCompleted())
		if downloadedNow > downloadedTotal {
			downloadedTotal = downloadedNow
		}
		if importErr != nil {
			failedCount++
			lastErr = truncateErr(importErr, 600)
			w.logger.Warnf("torrent item failed job_id=%s item_id=%s err=%s", job.ID, item.ID, lastErr)
			_ = w.markItemStatus(ctx, item.ID, "failed", lastErr, "", "")
			_ = w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal)
			continue
		}

		completedCount++
		w.logger.Infof("torrent item completed job_id=%s item_id=%s completed=%d failed=%d", job.ID, item.ID, completedCount, failedCount)
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
	customTitle string,
	customDescription string,
	metadataArgs []string,
	downloadArgs []string,
	userSelectedCandidateURL string,
	userSelectedResolverName string,
	userSelectedResolverMetaJSON string,
) (*processResult, error) {
	if strings.TrimSpace(sourceURL) == "" && len(selected) > 0 {
		sourceURL = strings.TrimSpace(selected[0].FilePath)
	}
	if strings.TrimSpace(sourceURL) == "" {
		return nil, permanentError{err: fmt.Errorf("import job %s has empty source_url", job.ID)}
	}
	w.logger.Infof("url import start job_id=%s source_url=%s selected_total=%d", job.ID, sourceURL, len(selected))

	completedCount := 0
	failedCount := 0
	lastErr := ""
	selectedTotal := len(selected)

	for _, item := range selected {
		w.logger.Infof("url item start job_id=%s item_id=%s", job.ID, item.ID)
		if err := w.markItemStatus(ctx, item.ID, "downloading", "", "", ""); err != nil {
			return nil, fmt.Errorf("mark import item downloading: %w", err)
		}

		importErr := w.importURLItem(
			ctx,
			job,
			item,
			sourceURL,
			categoryID,
			tags,
			visibility,
			customTitle,
			customDescription,
			metadataArgs,
			downloadArgs,
			userSelectedCandidateURL,
			userSelectedResolverName,
			userSelectedResolverMetaJSON,
		)
		if importErr != nil {
			failedCount++
			lastErr = truncateErr(importErr, 600)
			w.logger.Warnf("url item failed job_id=%s item_id=%s err=%s", job.ID, item.ID, lastErr)
			_ = w.markItemStatus(ctx, item.ID, "failed", lastErr, "", "")
			_ = w.updateJobProgress(ctx, job.ID, completedCount, failedCount, selectedTotal)
			continue
		}

		completedCount++
		w.logger.Infof("url item completed job_id=%s item_id=%s completed=%d failed=%d", job.ID, item.ID, completedCount, failedCount)
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
	customTitle string,
	customDescription string,
	metadataArgs []string,
	downloadArgs []string,
	userSelectedCandidateURL string,
	userSelectedResolverName string,
	userSelectedResolverMetaJSON string,
) error {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return permanentError{err: fmt.Errorf("source_url is required")}
	}

	forcedCandidateURL := strings.TrimSpace(userSelectedCandidateURL)
	if forcedCandidateURL != "" {
		resolverName := strings.TrimSpace(userSelectedResolverName)
		if resolverName == "" {
			resolverName = "page_manifest+user_selected"
		}
		contextMeta := map[string]interface{}{
			"page_url":               sourceURL,
			"selected_candidate_url": forcedCandidateURL,
			"selection_mode":         "user_selected",
		}
		if strings.TrimSpace(userSelectedResolverMetaJSON) != "" {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(userSelectedResolverMetaJSON), &parsed); err == nil {
				for key, value := range parsed {
					contextMeta[key] = value
				}
			}
		}
		w.logger.Infof("url resolver forced candidate job_id=%s item_id=%s resolver=%s candidate_url=%s", job.ID, item.ID, resolverName, forcedCandidateURL)
		return w.importURLFromResolvedSource(
			ctx,
			job,
			item,
			forcedCandidateURL,
			categoryID,
			tags,
			visibility,
			customTitle,
			customDescription,
			metadataArgs,
			downloadArgs,
			resolverName,
			contextMeta,
		)
	}

	w.logger.Infof("url resolver primary attempt job_id=%s item_id=%s source_url=%s", job.ID, item.ID, sourceURL)

	if err := w.importURLFromResolvedSource(
		ctx,
		job,
		item,
		sourceURL,
		categoryID,
		tags,
		visibility,
		customTitle,
		customDescription,
		metadataArgs,
		downloadArgs,
		"ytdlp",
		nil,
	); err == nil {
		return nil
	} else if !isUnsupportedURLMetadataError(err) {
		return err
	}

	if !w.app.Config.ImportPageResolverEnabled {
		return permanentError{err: fmt.Errorf("import.url.page_resolver_unavailable: unsupported url and resolver is disabled")}
	}
	w.logger.Infof("url fallback triggered job_id=%s item_id=%s reason=unsupported_url", job.ID, item.ID)

	candidates, resolverResult, resolveErr := w.resolvePageManifestCandidates(ctx, sourceURL)
	if resolveErr != nil {
		return permanentError{err: fmt.Errorf("import.url.page_resolver_unavailable: %v", resolveErr)}
	}
	w.logger.Infof(
		"url fallback candidates ready job_id=%s item_id=%s candidate_count=%d final_url=%s",
		job.ID,
		item.ID,
		len(candidates),
		strings.TrimSpace(resolverResult.FinalURL),
	)

	seen := make(map[string]struct{}, len(candidates)+1)
	seen[strings.ToLower(strings.TrimSpace(sourceURL))] = struct{}{}
	attemptErrors := make([]string, 0, len(candidates))
	for idx, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		w.logger.Infof("url fallback candidate try job_id=%s item_id=%s candidate_idx=%d candidate_url=%s", job.ID, item.ID, idx, candidate)

		contextMeta := map[string]interface{}{
			"page_url":       sourceURL,
			"page_final_url": strings.TrimSpace(resolverResult.FinalURL),
			"page_title":     strings.TrimSpace(resolverResult.Title),
			"page_reason":    strings.TrimSpace(resolverResult.Reason),
			"candidate_idx":  idx,
		}
		if ua := strings.TrimSpace(resolverResult.PageUserAgent); ua != "" {
			contextMeta["page_user_agent"] = ua
		}
		if referer := strings.TrimSpace(resolverResult.PageReferer); referer != "" {
			contextMeta["page_referer"] = referer
		}
		if origin := strings.TrimSpace(resolverResult.PageOrigin); origin != "" {
			contextMeta["page_origin"] = origin
		}
		if len(resolverResult.PageHeaders) > 0 {
			contextMeta["page_headers"] = resolverResult.PageHeaders
		}
		err := w.importURLFromResolvedSource(
			ctx,
			job,
			item,
			candidate,
			categoryID,
			tags,
			visibility,
			customTitle,
			customDescription,
			metadataArgs,
			downloadArgs,
			"page_manifest+yt-dlp",
			contextMeta,
		)
		if err == nil {
			w.logger.Infof("url fallback candidate succeeded job_id=%s item_id=%s candidate_idx=%d", job.ID, item.ID, idx)
			return nil
		}
		w.logger.Warnf("url fallback candidate failed job_id=%s item_id=%s candidate_idx=%d err=%v", job.ID, item.ID, idx, err)
		attemptErrors = append(attemptErrors, truncateErr(err, 180))
	}

	if len(attemptErrors) == 0 {
		return permanentError{err: fmt.Errorf("import.url.page_resolver_unavailable: no usable media candidates")}
	}
	return permanentError{err: fmt.Errorf("import.url.page_resolver_unavailable: fallback exhausted (%s)", strings.Join(attemptErrors, " | "))}
}

func (w *Worker) importURLFromResolvedSource(
	ctx context.Context,
	job claimedJob,
	item selectedItem,
	sourceURL string,
	categoryID *int64,
	tags []string,
	visibility string,
	customTitle string,
	customDescription string,
	metadataArgs []string,
	downloadArgs []string,
	resolverName string,
	resolverContext map[string]interface{},
) error {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return permanentError{err: fmt.Errorf("source_url is required")}
	}
	effectiveMetadataArgs := mergeYTDLPArgsWithResolverContext(metadataArgs, resolverContext)
	effectiveDownloadArgs := mergeYTDLPArgsWithResolverContext(downloadArgs, resolverContext)
	w.logger.Infof("url import stage=metadata_start job_id=%s item_id=%s resolver=%s source_url=%s", job.ID, item.ID, resolverName, sourceURL)

	meta, err := w.extractURLMetadata(ctx, sourceURL, effectiveMetadataArgs)
	if err != nil {
		w.logger.Warnf("url import stage=metadata_failed job_id=%s item_id=%s resolver=%s err=%v", job.ID, item.ID, resolverName, err)
		return err
	}
	w.logger.Infof(
		"url import stage=metadata_ok job_id=%s item_id=%s resolver=%s title=%q duration_sec=%.0f extractor=%s",
		job.ID,
		item.ID,
		resolverName,
		strings.TrimSpace(meta.Title),
		meta.Duration,
		strings.TrimSpace(meta.ExtractorKey),
	)
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

	tmpParent, err := w.ensureImportJobTempDir(job.ID, "url")
	if err != nil {
		return fmt.Errorf("prepare import temp dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(tmpParent, item.ID+"-*")
	if err != nil {
		return fmt.Errorf("create import temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	w.logger.Infof("url import stage=download_start job_id=%s item_id=%s resolver=%s", job.ID, item.ID, resolverName)
	localSourcePath, err := w.downloadURLSource(ctx, job.ID, sourceURL, tmpDir, effectiveDownloadArgs)
	if err != nil {
		w.logger.Warnf("url import stage=download_failed job_id=%s item_id=%s resolver=%s err=%v", job.ID, item.ID, resolverName, err)
		return err
	}
	w.logger.Infof("url import stage=download_ok job_id=%s item_id=%s resolver=%s local_file=%s", job.ID, item.ID, resolverName, localSourcePath)

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
	autoTitle := buildImportVideoTitle(sourceName)
	finalTitle := chooseURLImportTitle(customTitle, autoTitle)
	w.logger.Infof("url import stage=persist_start job_id=%s item_id=%s final_title=%q", job.ID, item.ID, finalTitle)
	if err := w.persistImportedVideo(ctx, job, item, localSourcePath, sourceName, finalTitle, strings.TrimSpace(customDescription), categoryID, tags, visibility); err != nil {
		return err
	}
	w.logger.Infof("url import stage=persist_ok job_id=%s item_id=%s", job.ID, item.ID)

	resolvedMediaURL := strings.TrimSpace(meta.URL)
	if resolvedMediaURL == "" && len(meta.RequestedDownloads) > 0 {
		resolvedMediaURL = strings.TrimSpace(meta.RequestedDownloads[0].URL)
	}
	if resolverName == "" {
		resolverName = "ytdlp"
	}
	resolverMeta := map[string]interface{}{
		"title":         strings.TrimSpace(meta.Title),
		"duration_sec":  int64(meta.Duration),
		"webpage_url":   strings.TrimSpace(meta.WebpageURL),
		"extractor":     strings.TrimSpace(meta.Extractor),
		"extractor_key": strings.TrimSpace(meta.ExtractorKey),
		"source_url":    sourceURL,
	}
	for key, value := range resolverContext {
		resolverMeta[key] = value
	}
	resolverMetaJSON, _ := json.Marshal(resolverMeta)
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
	w.logger.Infof("url import resolver fields updated job_id=%s item_id=%s resolver=%s resolved_media_url=%s", job.ID, item.ID, resolverName, resolvedMediaURL)
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
	w.logger.Debugf("url metadata command source_url=%s cmd=%s", sourceURL, formatCommand(w.app.Config.YTDLPBin, args))
	cmd := exec.CommandContext(runCtx, w.app.Config.YTDLPBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return ytDLPMetadata{}, fmt.Errorf("yt-dlp metadata timeout")
		}
		outText := truncateOutput(out, 400)
		if strings.Contains(strings.ToLower(string(out)), "unsupported url") {
			return ytDLPMetadata{}, unsupportedURLMetadataError{
				message: fmt.Sprintf("yt-dlp metadata failed: %v: %s", err, outText),
			}
		}
		return ytDLPMetadata{}, fmt.Errorf("yt-dlp metadata failed: %w: %s", err, outText)
	}
	w.logger.Debugf("url metadata raw_output source_url=%s summary=%s", sourceURL, truncateOutput(out, 1200))

	var meta ytDLPMetadata
	if err := json.Unmarshal(out, &meta); err != nil {
		return ytDLPMetadata{}, permanentError{err: fmt.Errorf("parse yt-dlp metadata: %w", err)}
	}
	return meta, nil
}

func (w *Worker) downloadURLSource(ctx context.Context, jobID, sourceURL, tmpDir string, extraArgs []string) (string, error) {
	timeout := w.app.Config.ImportURLTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	outputTemplate := filepath.Join(tmpDir, "source.%(ext)s")
	args := []string{
		"--no-playlist",
		"--newline",
		"--no-warnings",
		"--restrict-filenames",
		"--progress-template",
		"download:%(progress.downloaded_bytes)s",
		"-o", outputTemplate,
	}
	args = append(args, extraArgs...)
	args = append(args, sourceURL)
	w.logger.Debugf("url download command job_id=%s source_url=%s cmd=%s", jobID, sourceURL, formatCommand(w.app.Config.YTDLPBin, args))
	cmd := exec.CommandContext(runCtx, w.app.Config.YTDLPBin, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("yt-dlp download setup stdout: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("yt-dlp download setup stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("yt-dlp download failed to start: %w", err)
	}

	var (
		downloadedBytes atomic.Int64
		outputBuf       strings.Builder
		outputMu        sync.Mutex
		readWG          sync.WaitGroup
		sampleWG        sync.WaitGroup
	)

	appendLine := func(line string) {
		outputMu.Lock()
		if outputBuf.Len() < 16*1024 {
			outputBuf.WriteString(line)
			outputBuf.WriteByte('\n')
		}
		outputMu.Unlock()
	}
	scanReader := func(reader io.Reader) {
		defer readWG.Done()
		scanner := bufio.NewScanner(reader)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 2*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			appendLine(line)
			w.logger.Debugf("job_id=%s yt_dlp_line=%s", jobID, line)
			if bytes, ok := parseYTDLPDownloadedBytes(line); ok {
				current := downloadedBytes.Load()
				if bytes > current {
					downloadedBytes.Store(bytes)
				}
			}
		}
	}

	readWG.Add(2)
	go scanReader(stdoutPipe)
	go scanReader(stderrPipe)

	sampleCtx, stopSample := context.WithCancel(ctx)
	sampleWG.Add(1)
	go func() {
		defer sampleWG.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		lastAt := time.Now()
		lastDownloaded := int64(0)
		lastProgressLogAt := time.Time{}
		for {
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				currentDownloaded := downloadedBytes.Load()
				downloadSpeed := computeSpeedBPS(lastDownloaded, currentDownloaded, lastAt, now)
				if err := w.updateJobTransferMetrics(ctx, jobID, currentDownloaded, 0, downloadSpeed, 0); err != nil {
					w.logger.Warnf("job_id=%s update url transfer metrics failed: %v", jobID, err)
				}
				if shouldLogByInterval(lastProgressLogAt, now, w.progressLogInterval) {
					w.logger.Infof(
						"url transfer progress job_id=%s downloaded_bytes=%d download_speed_bps=%.2f",
						jobID,
						currentDownloaded,
						downloadSpeed,
					)
					lastProgressLogAt = now
				}
				lastAt = now
				lastDownloaded = currentDownloaded
			}
		}
	}()

	err = cmd.Wait()
	stopSample()
	sampleWG.Wait()
	readWG.Wait()
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("yt-dlp download timeout")
		}
		outputMu.Lock()
		outText := outputBuf.String()
		outputMu.Unlock()
		return "", fmt.Errorf("yt-dlp download failed: %w: %s", err, truncateOutput([]byte(outText), 400))
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

	finalDownloaded := downloadedBytes.Load()
	if info, statErr := os.Stat(localSourcePath); statErr == nil && info.Size() > finalDownloaded {
		finalDownloaded = info.Size()
	}
	if err := w.updateJobTransferMetrics(ctx, jobID, finalDownloaded, 0, 0, 0); err != nil {
		w.logger.Warnf("job_id=%s finalize url transfer metrics failed: %v", jobID, err)
	}
	w.logger.Infof("url transfer final job_id=%s downloaded_bytes=%d", jobID, finalDownloaded)
	return localSourcePath, nil
}

func (w *Worker) importSelectedFile(
	ctx context.Context,
	job claimedJob,
	item selectedItem,
	file *torrent.File,
	tor *torrent.Torrent,
	baseDownloadedBytes int64,
	categoryID *int64,
	tags []string,
	visibility string,
	selectedTotal int,
	customTitle string,
	customTitlePrefix string,
	customDescription string,
) error {
	w.logger.Infof("torrent import file download start job_id=%s item_id=%s source_path=%s", job.ID, item.ID, item.FilePath)
	tmpParent, err := w.ensureImportJobTempDir(job.ID, "files")
	if err != nil {
		return fmt.Errorf("prepare temp file dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(tmpParent, item.ID+"-*")
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
	readahead := w.app.Config.ImportBTReaderReadaheadBytes
	if readahead <= 0 {
		readahead = 32 * 1024 * 1024
	}
	reader.SetReadahead(readahead)

	if err := os.MkdirAll(filepath.Dir(localSourcePath), 0o755); err != nil {
		return fmt.Errorf("prepare local source path: %w", err)
	}
	out, err := os.Create(localSourcePath)
	if err != nil {
		return fmt.Errorf("create local source file: %w", err)
	}

	stopSampler := w.startTorrentTransferSampler(ctx, job.ID, file, tor, baseDownloadedBytes)
	defer func() {
		stopSampler(0)
	}()

	if _, err := io.Copy(out, reader); err != nil {
		out.Close()
		return fmt.Errorf("download torrent file: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close local source file: %w", err)
	}
	stopSampler(0)
	w.logger.Infof("torrent import file download completed job_id=%s item_id=%s", job.ID, item.ID)

	autoTitle := buildImportVideoTitle(item.FilePath)
	finalTitle := chooseTorrentImportTitle(autoTitle, selectedTotal, customTitle, customTitlePrefix)
	w.logger.Infof("torrent import persist start job_id=%s item_id=%s final_title=%q", job.ID, item.ID, finalTitle)
	return w.persistImportedVideo(ctx, job, item, localSourcePath, item.FilePath, finalTitle, strings.TrimSpace(customDescription), categoryID, tags, visibility)
}

func (w *Worker) persistImportedVideo(
	ctx context.Context,
	job claimedJob,
	item selectedItem,
	localSourcePath string,
	sourcePath string,
	videoTitle string,
	videoDescription string,
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
		w.logger.Warnf("import item metadata probe failed (soft) job_id=%s item_id=%s err=%v", job.ID, item.ID, probeErr)
	}

	now := nowString()
	provider := w.app.Storage.Driver()
	bucket := ""
	if provider == "s3" {
		bucket = w.app.Storage.Bucket()
	}

	if strings.TrimSpace(videoTitle) == "" {
		videoTitle = buildImportVideoTitle(sourcePath)
	}
	videoDescription = strings.TrimSpace(videoDescription)
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
) VALUES (?, ?, ?, ?, ?, NULL, ?,
	'processing', ?, ?, NULL, 0, 0,
	0, 0, 0, 0, ?, ?)`,
		videoID,
		job.UserID,
		videoTitle,
		videoDescription,
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
	w.logger.Infof("import persist completed job_id=%s item_id=%s video_id=%s transcode_job_id=%s", job.ID, item.ID, videoID, transcodeJobID)
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

func (w *Worker) updateJobTransferMetrics(ctx context.Context, jobID string, downloadedBytes, uploadedBytes int64, downloadSpeedBPS, uploadSpeedBPS float64) error {
	now := nowString()
	_, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET downloaded_bytes = ?,
	uploaded_bytes = ?,
	download_speed_bps = ?,
	upload_speed_bps = ?,
	transfer_updated_at = ?,
	updated_at = ?
WHERE id = ?
  AND status = 'downloading'`,
		max(0, downloadedBytes),
		max(0, uploadedBytes),
		max(0.0, downloadSpeedBPS),
		max(0.0, uploadSpeedBPS),
		now,
		now,
		jobID,
	)
	return err
}

func (w *Worker) startTorrentTransferSampler(
	ctx context.Context,
	jobID string,
	file *torrent.File,
	tor *torrent.Torrent,
	baseDownloadedBytes int64,
) func(finalUploadSpeed float64) {
	sampleCtx, cancel := context.WithCancel(ctx)
	var (
		once sync.Once
		wg   sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		lastAt := time.Now()
		lastDownloaded := baseDownloadedBytes + max(0, file.BytesCompleted())
		lastUploaded := torrentUploadedBytes(tor)
		lastProgressLogAt := time.Time{}
		smoothWindowSec := w.app.Config.ImportBTSpeedSmoothWindowSec
		if smoothWindowSec <= 0 {
			smoothWindowSec = 5
		}
		downloadSmoother := newSpeedSmoother(smoothWindowSec)
		uploadSmoother := newSpeedSmoother(smoothWindowSec)

		for {
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				downloaded := baseDownloadedBytes + max(0, file.BytesCompleted())
				uploaded := torrentUploadedBytes(tor)
				rawDownloadSpeed := computeSpeedBPS(lastDownloaded, downloaded, lastAt, now)
				rawUploadSpeed := computeSpeedBPS(lastUploaded, uploaded, lastAt, now)
				displayDownloadSpeed := downloadSmoother.Add(rawDownloadSpeed)
				displayUploadSpeed := uploadSmoother.Add(rawUploadSpeed)
				if err := w.updateJobTransferMetrics(ctx, jobID, downloaded, uploaded, displayDownloadSpeed, displayUploadSpeed); err != nil {
					w.logger.Warnf("job_id=%s update torrent transfer metrics failed: %v", jobID, err)
				}
				if shouldLogByInterval(lastProgressLogAt, now, w.progressLogInterval) {
					w.logger.Infof(
						"torrent transfer progress job_id=%s downloaded_bytes=%d uploaded_bytes=%d download_speed_bps=%.2f upload_speed_bps=%.2f raw_download_speed_bps=%.2f raw_upload_speed_bps=%.2f",
						jobID,
						downloaded,
						uploaded,
						displayDownloadSpeed,
						displayUploadSpeed,
						rawDownloadSpeed,
						rawUploadSpeed,
					)
					lastProgressLogAt = now
				}
				lastAt = now
				lastDownloaded = downloaded
				lastUploaded = uploaded
			}
		}
	}()

	return func(finalUploadSpeed float64) {
		once.Do(func() {
			cancel()
			wg.Wait()
			downloaded := baseDownloadedBytes + max(0, file.BytesCompleted())
			uploaded := torrentUploadedBytes(tor)
			if err := w.updateJobTransferMetrics(ctx, jobID, downloaded, uploaded, 0, max(0, finalUploadSpeed)); err != nil {
				w.logger.Warnf("job_id=%s finalize torrent transfer metrics failed: %v", jobID, err)
			}
			w.logger.Infof("torrent transfer final job_id=%s downloaded_bytes=%d uploaded_bytes=%d", jobID, downloaded, uploaded)
		})
	}
}

func torrentUploadedBytes(tor *torrent.Torrent) int64 {
	if tor == nil {
		return 0
	}
	stats := tor.Stats()
	bytesWrittenData := stats.BytesWrittenData
	return bytesWrittenData.Int64()
}

func computeSpeedBPS(previous, current int64, from, to time.Time) float64 {
	if to.Before(from) {
		return 0
	}
	delta := current - previous
	if delta <= 0 {
		return 0
	}
	seconds := to.Sub(from).Seconds()
	if seconds <= 0 {
		return 0
	}
	return float64(delta) / seconds
}

type speedSmoother struct {
	values []float64
	next   int
	count  int
	sum    float64
}

func newSpeedSmoother(window int) *speedSmoother {
	if window <= 0 {
		window = 1
	}
	return &speedSmoother{
		values: make([]float64, window),
	}
}

func (s *speedSmoother) Add(value float64) float64 {
	if s == nil || len(s.values) == 0 {
		return max(0.0, value)
	}
	if s.count < len(s.values) {
		s.values[s.next] = value
		s.sum += value
		s.count++
		s.next = (s.next + 1) % len(s.values)
		return s.sum / float64(s.count)
	}
	s.sum -= s.values[s.next]
	s.values[s.next] = value
	s.sum += value
	s.next = (s.next + 1) % len(s.values)
	return s.sum / float64(len(s.values))
}

func shouldLogByInterval(lastAt, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}
	if lastAt.IsZero() {
		return true
	}
	return now.Sub(lastAt) >= interval
}

func (w *Worker) importJobTempRoot(jobID string) string {
	return filepath.Join(w.app.Config.TaskTempDir, importJobTempFolderName, jobID)
}

func (w *Worker) ensureImportJobTempDir(jobID string, parts ...string) (string, error) {
	dir := w.importJobTempRoot(jobID)
	if len(parts) > 0 {
		all := make([]string, 0, len(parts)+1)
		all = append(all, dir)
		all = append(all, parts...)
		dir = filepath.Join(all...)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func formatCommand(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, bin)
	parts = append(parts, args...)
	return strings.Join(parts, " ")
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
WHERE id = ?
  AND status = 'downloading'`, completed, failed, progress, nowString(), jobID)
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

	res, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET status = ?,
	completed_files = ?,
	failed_files = ?,
	progress = ?,
	download_speed_bps = 0,
	upload_speed_bps = 0,
	transfer_updated_at = ?,
	error_message = ?,
	finished_at = ?,
	updated_at = ?
WHERE id = ?
  AND status = 'downloading'`,
		result.Status,
		result.Completed,
		result.Failed,
		progress,
		now,
		nullableString(result.ErrorMessage),
		now,
		now,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("mark import job complete: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		w.logger.Warnf("skip mark import job complete (status changed) job_id=%s", job.ID)
		return nil
	}
	w.logger.Infof(
		"job status updated job_id=%s status=%s completed=%d failed=%d progress=%.2f error=%q",
		job.ID,
		result.Status,
		result.Completed,
		result.Failed,
		progress,
		result.ErrorMessage,
	)
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
		res, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET status = 'queued',
	error_message = ?,
	downloaded_bytes = 0,
	uploaded_bytes = 0,
	download_speed_bps = 0,
	upload_speed_bps = 0,
	transfer_updated_at = NULL,
	available_at = ?,
	updated_at = ?
WHERE id = ?
  AND status = 'downloading'`, msg, nextRun, nowStr, job.ID)
		if err != nil {
			return fmt.Errorf("mark import retryable failure: %w", err)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			w.logger.Warnf("skip retry schedule (status changed) job_id=%s", job.ID)
			return nil
		}
		w.logger.Warnf("job retry scheduled job_id=%s attempt=%d/%d err=%s", job.ID, job.Attempts, job.MaxAttempts, msg)
		return nil
	}

	res, err := w.app.DB.ExecContext(ctx, `
UPDATE video_import_jobs
SET status = 'failed',
	error_message = ?,
	download_speed_bps = 0,
	upload_speed_bps = 0,
	transfer_updated_at = ?,
	finished_at = ?,
	updated_at = ?
WHERE id = ?
  AND status = 'downloading'`, msg, nowStr, nowStr, nowStr, job.ID)
	if err != nil {
		return fmt.Errorf("mark import final failure: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		w.logger.Warnf("skip final failure mark (status changed) job_id=%s", job.ID)
		return nil
	}
	w.logger.Errorf("job failed permanently job_id=%s attempt=%d/%d err=%s", job.ID, job.Attempts, job.MaxAttempts, msg)
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

func mergeYTDLPArgsWithResolverContext(baseArgs []string, resolverContext map[string]interface{}) []string {
	args := append([]string{}, baseArgs...)
	if len(resolverContext) == 0 {
		return args
	}

	pageUA := strings.TrimSpace(contextStringValue(resolverContext, "page_user_agent"))
	pageReferer := strings.TrimSpace(contextStringValue(resolverContext, "page_referer"))
	pageOrigin := strings.TrimSpace(contextStringValue(resolverContext, "page_origin"))
	pageHeaders := contextHeaderValues(resolverContext, "page_headers")
	if pageOrigin != "" {
		pageHeaders["Origin"] = pageOrigin
	}

	if pageUA != "" && !hasYTDLPFlag(args, "--user-agent") {
		args = append(args, "--user-agent", pageUA)
	}
	if pageReferer != "" && !hasYTDLPFlag(args, "--referer") {
		args = append(args, "--referer", pageReferer)
	}

	for _, headerKey := range []string{"Origin", "Accept", "Accept-Language"} {
		headerVal := strings.TrimSpace(pageHeaders[headerKey])
		if headerVal == "" {
			continue
		}
		if hasYTDLPHeader(args, headerKey) {
			continue
		}
		args = append(args, "--add-header", fmt.Sprintf("%s: %s", headerKey, headerVal))
	}

	return args
}

func contextStringValue(ctx map[string]interface{}, key string) string {
	value, ok := ctx[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func contextHeaderValues(ctx map[string]interface{}, key string) map[string]string {
	out := map[string]string{}
	raw, ok := ctx[key]
	if !ok || raw == nil {
		return out
	}
	switch headers := raw.(type) {
	case map[string]string:
		for headerKey, headerVal := range headers {
			normalizedKey := normalizeResolverHeaderKey(headerKey)
			if normalizedKey == "" {
				continue
			}
			out[normalizedKey] = strings.TrimSpace(headerVal)
		}
	case map[string]interface{}:
		for headerKey, headerVal := range headers {
			normalizedKey := normalizeResolverHeaderKey(headerKey)
			if normalizedKey == "" {
				continue
			}
			out[normalizedKey] = strings.TrimSpace(fmt.Sprintf("%v", headerVal))
		}
	}
	return out
}

func normalizeResolverHeaderKey(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "accept":
		return "Accept"
	case "accept-language":
		return "Accept-Language"
	case "origin":
		return "Origin"
	default:
		return ""
	}
}

func hasYTDLPFlag(args []string, flag string) bool {
	flag = strings.ToLower(strings.TrimSpace(flag))
	if flag == "" {
		return false
	}
	for i := 0; i < len(args); i++ {
		token := strings.ToLower(strings.TrimSpace(args[i]))
		if token == flag || strings.HasPrefix(token, flag+"=") {
			return true
		}
	}
	return false
}

func hasYTDLPHeader(args []string, headerName string) bool {
	targetKey := normalizeResolverHeaderKey(headerName)
	if targetKey == "" {
		return false
	}
	targetKey = strings.ToLower(targetKey)
	for i := 0; i < len(args); i++ {
		token := strings.TrimSpace(args[i])
		switch {
		case strings.EqualFold(token, "--add-header"), strings.EqualFold(token, "-H"):
			if i+1 >= len(args) {
				continue
			}
			if headerKeyEquals(args[i+1], targetKey) {
				return true
			}
		case strings.HasPrefix(strings.ToLower(token), "--add-header="):
			if headerKeyEquals(token[len("--add-header="):], targetKey) {
				return true
			}
		case strings.HasPrefix(strings.ToLower(token), "-h="):
			if headerKeyEquals(token[len("-h="):], targetKey) {
				return true
			}
		}
	}
	return false
}

func headerKeyEquals(rawHeader, targetLowerKey string) bool {
	header := strings.TrimSpace(rawHeader)
	if header == "" {
		return false
	}
	parts := strings.SplitN(header, ":", 2)
	if len(parts) != 2 {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(parts[0]))
	return key != "" && key == targetLowerKey
}

var ytdlpDownloadedBytesRe = regexp.MustCompile(`(?i)download:\s*([0-9]+(?:\.[0-9]+)?)`)
var ytdlpDownloadedBytesNumericRe = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*$`)

func parseYTDLPDownloadedBytes(line string) (int64, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return 0, false
	}
	if matches := ytdlpDownloadedBytesRe.FindStringSubmatch(trimmed); len(matches) == 2 {
		return parseDownloadedBytesToken(matches[1])
	}
	if matches := ytdlpDownloadedBytesNumericRe.FindStringSubmatch(trimmed); len(matches) == 2 {
		return parseDownloadedBytesToken(matches[1])
	}
	return 0, false
}

func parseDownloadedBytesToken(value string) (int64, bool) {
	token := strings.TrimSpace(value)
	if token == "" {
		return 0, false
	}
	if strings.Contains(token, ".") {
		parsed, err := strconv.ParseFloat(token, 64)
		if err != nil || parsed <= 0 {
			return 0, false
		}
		return int64(parsed), true
	}
	parsed, err := strconv.ParseInt(token, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
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

func chooseURLImportTitle(customTitle string, fallback string) string {
	title := strings.TrimSpace(customTitle)
	if title == "" {
		return fallback
	}
	if len(title) > 120 {
		title = title[:120]
	}
	return title
}

func chooseTorrentImportTitle(autoTitle string, selectedTotal int, customTitle string, customTitlePrefix string) string {
	title := autoTitle
	trimmedCustomTitle := strings.TrimSpace(customTitle)
	trimmedPrefix := strings.TrimSpace(customTitlePrefix)

	switch {
	case selectedTotal == 1 && trimmedCustomTitle != "":
		title = trimmedCustomTitle
	case selectedTotal > 1 && trimmedPrefix != "":
		title = trimmedPrefix + " - " + autoTitle
	}
	title = strings.TrimSpace(title)
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
