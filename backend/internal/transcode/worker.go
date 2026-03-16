package transcode

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/logging"
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
	engine              Engine
	logger              logger
	pollInterval        time.Duration
	progressLogInterval time.Duration
	segmentSeconds      int64
}

type Option func(*Worker)

func WithEngine(engine Engine) Option {
	return func(w *Worker) {
		if engine != nil {
			w.engine = engine
		}
	}
}

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
		engine:              NewFFmpegEngine(a.Config.FFmpegBin, a.Config.FFprobeBin),
		logger:              defaultLogger.WithPrefix("module=transcode"),
		pollInterval:        a.Config.TranscodePoll,
		progressLogInterval: a.Config.TranscodeProgressLogInterval,
		segmentSeconds:      4,
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

func (w *Worker) Run(ctx context.Context) {
	w.logger.Infof("transcode worker started poll_interval=%s progress_log_interval=%s", w.pollInterval, w.progressLogInterval)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		hasWork, err := w.RunOnce(ctx)
		if err != nil {
			w.logger.Errorf("transcode worker loop error: %v", err)
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
	w.logger.Infof("job claimed job_id=%s video_id=%s attempt=%d/%d", job.ID, job.VideoID, job.Attempts, job.MaxAttempts)

	asset, err := w.processJob(ctx, *job)
	if err != nil {
		w.logger.Errorf("job processing failed job_id=%s video_id=%s err=%v", job.ID, job.VideoID, err)
		if markErr := w.markJobFailure(ctx, *job, err); markErr != nil {
			return true, fmt.Errorf("job %s failed and could not update state: %w (original: %v)", job.ID, markErr, err)
		}
		return true, nil
	}

	if err := w.markJobSuccess(ctx, *job, *asset); err != nil {
		return true, err
	}
	w.logger.Infof("job completed job_id=%s video_id=%s status=succeeded", job.ID, job.VideoID)
	return true, nil
}

type claimedJob struct {
	ID          string
	VideoID     string
	Attempts    int
	MaxAttempts int
}

func (w *Worker) claimJob(ctx context.Context) (*claimedJob, error) {
	now := nowString()

	tx, err := w.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	defer tx.Rollback()

	var job claimedJob
	err = tx.QueryRowContext(ctx, `
SELECT id, video_id, attempts, max_attempts
FROM video_transcode_jobs
WHERE status = 'queued' AND available_at <= ?
ORDER BY created_at ASC
LIMIT 1`,
		now,
	).Scan(&job.ID, &job.VideoID, &job.Attempts, &job.MaxAttempts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit empty claim tx: %w", err)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("query queued job: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
UPDATE video_transcode_jobs
SET status = 'processing',
    attempts = attempts + 1,
    locked_at = ?,
    started_at = COALESCE(started_at, ?),
    updated_at = ?
WHERE id = ? AND status = 'queued'`,
		now,
		now,
		now,
		job.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("claim queued job: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit race claim tx: %w", err)
		}
		return nil, nil
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim tx: %w", err)
	}

	job.Attempts++
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = max(1, w.app.Config.TranscodeMaxTry)
	}
	return &job, nil
}

type videoSource struct {
	VideoID       string
	UploaderID    string
	VideoStatus   string
	MediaProvider string
	MediaBucket   string
	MediaKey      string
}

type hlsAssetPayload struct {
	Provider        string
	Bucket          string
	MasterObjectKey string
	VariantsJSON    string
	SegmentSeconds  int64
	ThumbnailVTTKey string
	ThumbnailJPGKey string
	UploaderID      string
	Cover           *generatedMedia
	Preview         *generatedMedia
}

type generatedMedia struct {
	ObjectKey        string
	ContentType      string
	OriginalFilename string
	SizeBytes        int64
}

func (w *Worker) processJob(ctx context.Context, job claimedJob) (*hlsAssetPayload, error) {
	var (
		stageMu sync.RWMutex
		stage   = "load_source"
	)
	setStage := func(next string) {
		stageMu.Lock()
		stage = next
		stageMu.Unlock()
	}
	currentStage := func() string {
		stageMu.RLock()
		defer stageMu.RUnlock()
		return stage
	}
	stopHeartbeat := w.startProgressHeartbeat(ctx, job, currentStage)
	defer stopHeartbeat()

	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())

	var src videoSource
	err := w.app.DB.QueryRowContext(ctx, `
SELECT v.id, v.uploader_id, v.status,
       sm.provider, COALESCE(sm.bucket, ''), sm.object_key
FROM videos v
JOIN media_objects sm ON sm.id = v.source_media_id
WHERE v.id = ?
LIMIT 1`,
		job.VideoID,
	).Scan(
		&src.VideoID,
		&src.UploaderID,
		&src.VideoStatus,
		&src.MediaProvider,
		&src.MediaBucket,
		&src.MediaKey,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, permanentError{err: fmt.Errorf("video %s not found", job.VideoID)}
		}
		return nil, fmt.Errorf("query video source: %w", err)
	}
	if src.VideoStatus == "deleted" {
		return nil, permanentError{err: fmt.Errorf("video %s is deleted", job.VideoID)}
	}
	if src.MediaKey == "" {
		return nil, permanentError{err: fmt.Errorf("video %s source media key is empty", job.VideoID)}
	}
	w.logger.Infof(
		"video source loaded job_id=%s video_id=%s media_provider=%s media_key=%s",
		job.ID,
		job.VideoID,
		src.MediaProvider,
		src.MediaKey,
	)

	setStage("prepare_workspace")
	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())
	tmpDir, err := os.MkdirTemp("", "moevideo-transcode-*")
	if err != nil {
		return nil, fmt.Errorf("create transcode temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputPath := w.app.Storage.LocalObjectPath(src.MediaKey)
	if src.MediaProvider == "s3" {
		setStage("download_source")
		w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())
		inputPath = filepath.Join(tmpDir, "source.mp4")
		if err := w.app.Storage.DownloadObjectToPath(ctx, src.MediaProvider, src.MediaBucket, src.MediaKey, inputPath); err != nil {
			return nil, fmt.Errorf("download source media: %w", err)
		}
	} else if _, err := os.Stat(inputPath); err != nil {
		return nil, fmt.Errorf("local source media missing: %w", err)
	}

	outputDir := filepath.Join(tmpDir, "hls")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create hls output dir: %w", err)
	}

	setStage("transcode_hls")
	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())
	buildResult, err := w.engine.BuildHLS(ctx, inputPath, outputDir, w.segmentSeconds)
	if err != nil {
		return nil, fmt.Errorf("build hls: %w", err)
	}
	if len(buildResult.Variants) == 0 {
		return nil, permanentError{err: fmt.Errorf("transcode produced no variants")}
	}
	w.logger.Infof(
		"hls built job_id=%s video_id=%s variants=%d segment_seconds=%d master=%s",
		job.ID,
		job.VideoID,
		len(buildResult.Variants),
		buildResult.SegmentSeconds,
		buildResult.MasterPlaylist,
	)

	rootObjectKey := filepath.ToSlash(filepath.Join("hls", src.UploaderID, src.VideoID))
	files, err := listOutputFiles(outputDir)
	if err != nil {
		return nil, fmt.Errorf("list hls files: %w", err)
	}
	setStage("upload_hls")
	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s file_count=%d", job.ID, job.VideoID, currentStage(), len(files))
	for _, relPath := range files {
		fullPath := filepath.Join(outputDir, filepath.FromSlash(relPath))
		objectKey := filepath.ToSlash(filepath.Join(rootObjectKey, relPath))
		if err := w.app.Storage.UploadFile(ctx, objectKey, detectContentType(relPath), fullPath); err != nil {
			return nil, fmt.Errorf("upload hls object %s: %w", objectKey, err)
		}
	}

	enriched := make([]VariantInfo, 0, len(buildResult.Variants))
	for _, v := range buildResult.Variants {
		enriched = append(enriched, VariantInfo{
			Name:              v.Name,
			Width:             v.Width,
			Height:            v.Height,
			Bandwidth:         v.Bandwidth,
			PlaylistObjectKey: filepath.ToSlash(filepath.Join(rootObjectKey, v.PlaylistObjectKey)),
		})
	}
	variantsJSON, err := json.Marshal(enriched)
	if err != nil {
		return nil, fmt.Errorf("marshal variants: %w", err)
	}

	bucket := ""
	if w.app.Storage.Driver() == "s3" {
		bucket = w.app.Storage.Bucket()
	}

	supplementalDir := filepath.Join(tmpDir, "supplemental")
	if err := os.MkdirAll(supplementalDir, 0o755); err != nil {
		return nil, fmt.Errorf("create supplemental dir: %w", err)
	}

	var coverMedia *generatedMedia
	setStage("generate_cover")
	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())
	coverPath := filepath.Join(supplementalDir, "cover.jpg")
	if err := w.engine.GenerateCover(ctx, inputPath, coverPath); err != nil {
		w.logger.Warnf("transcode job %s stage=generate_cover failed (soft): %v", job.ID, err)
	} else {
		coverObjectKey := filepath.ToSlash(filepath.Join(rootObjectKey, "cover.jpg"))
		if err := w.app.Storage.UploadFile(ctx, coverObjectKey, "image/jpeg", coverPath); err != nil {
			w.logger.Warnf("transcode job %s stage=upload_cover failed (soft): %v", job.ID, err)
		} else if info, statErr := os.Stat(coverPath); statErr != nil {
			w.logger.Warnf("transcode job %s stage=stat_cover failed (soft): %v", job.ID, statErr)
		} else {
			coverMedia = &generatedMedia{
				ObjectKey:        coverObjectKey,
				ContentType:      "image/jpeg",
				OriginalFilename: "cover.jpg",
				SizeBytes:        info.Size(),
			}
		}
	}

	var previewMedia *generatedMedia
	setStage("generate_preview_webp")
	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())
	previewPath := filepath.Join(supplementalDir, "preview.webp")
	if err := w.engine.GeneratePreviewWebP(ctx, inputPath, previewPath); err != nil {
		w.logger.Warnf("transcode job %s stage=generate_preview_webp failed (soft): %v", job.ID, err)
	} else {
		previewObjectKey := filepath.ToSlash(filepath.Join(rootObjectKey, "preview.webp"))
		if err := w.app.Storage.UploadFile(ctx, previewObjectKey, "image/webp", previewPath); err != nil {
			w.logger.Warnf("transcode job %s stage=upload_preview_webp failed (soft): %v", job.ID, err)
		} else if info, statErr := os.Stat(previewPath); statErr != nil {
			w.logger.Warnf("transcode job %s stage=stat_preview_webp failed (soft): %v", job.ID, statErr)
		} else {
			previewMedia = &generatedMedia{
				ObjectKey:        previewObjectKey,
				ContentType:      "image/webp",
				OriginalFilename: "preview.webp",
				SizeBytes:        info.Size(),
			}
		}
	}

	thumbnailVTTKey := ""
	thumbnailJPGKey := ""
	thumbnailVTTPath := filepath.Join(supplementalDir, "thumbnails.vtt")
	thumbnailJPGPath := filepath.Join(supplementalDir, "sprite.jpg")
	setStage("generate_vtt_thumbnail")
	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())
	if err := w.engine.GenerateVTTThumbnail(ctx, inputPath, thumbnailVTTPath, thumbnailJPGPath); err != nil {
		w.logger.Warnf("transcode job %s stage=generate_vtt_thumbnail failed (soft): %v", job.ID, err)
	} else {
		vttObjectKey := filepath.ToSlash(filepath.Join(rootObjectKey, "thumbnails.vtt"))
		spriteObjectKey := filepath.ToSlash(filepath.Join(rootObjectKey, "sprite.jpg"))

		if err := w.app.Storage.UploadFile(ctx, vttObjectKey, "text/vtt", thumbnailVTTPath); err != nil {
			w.logger.Warnf("transcode job %s stage=upload_vtt_thumbnail failed (soft): %v", job.ID, err)
		} else if err := w.app.Storage.UploadFile(ctx, spriteObjectKey, "image/jpeg", thumbnailJPGPath); err != nil {
			w.logger.Warnf("transcode job %s stage=upload_sprite_thumbnail failed (soft): %v", job.ID, err)
		} else {
			thumbnailVTTKey = vttObjectKey
			thumbnailJPGKey = spriteObjectKey
		}
	}
	setStage("persist_result")
	w.logger.Infof("job stage start job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, currentStage())

	return &hlsAssetPayload{
		Provider:        w.app.Storage.Driver(),
		Bucket:          bucket,
		MasterObjectKey: filepath.ToSlash(filepath.Join(rootObjectKey, buildResult.MasterPlaylist)),
		VariantsJSON:    string(variantsJSON),
		SegmentSeconds:  buildResult.SegmentSeconds,
		ThumbnailVTTKey: thumbnailVTTKey,
		ThumbnailJPGKey: thumbnailJPGKey,
		UploaderID:      src.UploaderID,
		Cover:           coverMedia,
		Preview:         previewMedia,
	}, nil
}

func (w *Worker) markJobSuccess(ctx context.Context, job claimedJob, asset hlsAssetPayload) error {
	tx, err := w.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin success tx: %w", err)
	}
	defer tx.Rollback()

	now := nowString()

	_, err = tx.ExecContext(ctx, `
INSERT INTO video_hls_assets (
	video_id, provider, bucket, master_object_key, variants_json, segment_seconds,
	thumbnail_vtt_object_key, thumbnail_sprite_object_key, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(video_id) DO UPDATE SET
    provider = excluded.provider,
    bucket = excluded.bucket,
    master_object_key = excluded.master_object_key,
    variants_json = excluded.variants_json,
    segment_seconds = excluded.segment_seconds,
    thumbnail_vtt_object_key = excluded.thumbnail_vtt_object_key,
    thumbnail_sprite_object_key = excluded.thumbnail_sprite_object_key,
    updated_at = excluded.updated_at`,
		job.VideoID,
		asset.Provider,
		nullableString(asset.Bucket),
		asset.MasterObjectKey,
		asset.VariantsJSON,
		asset.SegmentSeconds,
		nullableString(asset.ThumbnailVTTKey),
		nullableString(asset.ThumbnailJPGKey),
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert hls assets: %w", err)
	}

	if asset.Cover != nil {
		coverMediaID, err := upsertGeneratedMediaTx(ctx, tx, asset.Provider, asset.Bucket, asset.UploaderID, *asset.Cover, now)
		if err != nil {
			w.logger.Warnf("transcode job %s: persist auto cover failed (soft): %v", job.ID, err)
		} else {
			if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET cover_media_id = COALESCE(cover_media_id, ?),
    updated_at = ?
WHERE id = ?`,
				coverMediaID,
				now,
				job.VideoID,
			); err != nil {
				w.logger.Warnf("transcode job %s: set auto cover failed (soft): %v", job.ID, err)
			}
		}
	}

	if asset.Preview != nil {
		previewMediaID, err := upsertGeneratedMediaTx(ctx, tx, asset.Provider, asset.Bucket, asset.UploaderID, *asset.Preview, now)
		if err != nil {
			w.logger.Warnf("transcode job %s: persist preview webp failed (soft): %v", job.ID, err)
		} else {
			if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET preview_media_id = ?,
    updated_at = ?
WHERE id = ?`,
				previewMediaID,
				now,
				job.VideoID,
			); err != nil {
				w.logger.Warnf("transcode job %s: set preview media failed (soft): %v", job.ID, err)
			}
		}
	}

	res, err := tx.ExecContext(ctx, `
UPDATE videos
SET status = 'published',
    published_at = COALESCE(published_at, ?),
    updated_at = ?
WHERE id = ? AND status != 'deleted'`,
		now,
		now,
		job.VideoID,
	)
	if err != nil {
		return fmt.Errorf("update video status to published: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("video %s not updated while marking transcode success", job.VideoID)
	}

	_, err = tx.ExecContext(ctx, `
UPDATE video_transcode_jobs
SET status = 'succeeded',
    last_error = NULL,
    locked_at = NULL,
    finished_at = ?,
    updated_at = ?
WHERE id = ?`,
		now,
		now,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("mark transcode job succeeded: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit success tx: %w", err)
	}
	w.logger.Infof(
		"job published job_id=%s video_id=%s master_object_key=%s thumbnail_vtt=%t thumbnail_sprite=%t",
		job.ID,
		job.VideoID,
		asset.MasterObjectKey,
		asset.ThumbnailVTTKey != "",
		asset.ThumbnailJPGKey != "",
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

	tx, err := w.app.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin fail tx: %w", err)
	}
	defer tx.Rollback()

	if shouldRetry {
		nextRun := util.FormatTime(now.Add(backoffForAttempt(job.Attempts)))
		_, err = tx.ExecContext(ctx, `
UPDATE video_transcode_jobs
SET status = 'queued',
    last_error = ?,
    available_at = ?,
    locked_at = NULL,
    updated_at = ?
WHERE id = ?`,
			msg,
			nextRun,
			nowStr,
			job.ID,
		)
		if err != nil {
			return fmt.Errorf("mark transcode retryable failure: %w", err)
		}
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE video_transcode_jobs
SET status = 'failed',
    last_error = ?,
    locked_at = NULL,
    finished_at = ?,
    updated_at = ?
WHERE id = ?`,
			msg,
			nowStr,
			nowStr,
			job.ID,
		)
		if err != nil {
			return fmt.Errorf("mark transcode final failure: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE videos
SET status = 'failed', updated_at = ?
WHERE id = ? AND status != 'deleted'`,
			nowStr,
			job.VideoID,
		); err != nil {
			return fmt.Errorf("mark video failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit fail tx: %w", err)
	}

	if shouldRetry {
		w.logger.Warnf("job retry scheduled job_id=%s video_id=%s attempt=%d/%d err=%s", job.ID, job.VideoID, job.Attempts, job.MaxAttempts, msg)
	} else {
		w.logger.Errorf("job failed permanently job_id=%s video_id=%s attempt=%d/%d err=%s", job.ID, job.VideoID, job.Attempts, job.MaxAttempts, msg)
	}
	return nil
}

func (w *Worker) startProgressHeartbeat(ctx context.Context, job claimedJob, stageFn func() string) func() {
	if w.progressLogInterval <= 0 {
		return func() {}
	}
	heartbeatCtx, cancel := context.WithCancel(ctx)
	var (
		wg sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		lastLogAt := time.Time{}
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case now := <-ticker.C:
				if !shouldLogByInterval(lastLogAt, now, w.progressLogInterval) {
					continue
				}
				stage := ""
				if stageFn != nil {
					stage = stageFn()
				}
				w.logger.Infof("transcode progress job_id=%s video_id=%s stage=%s", job.ID, job.VideoID, stage)
				lastLogAt = now
			}
		}
	}()
	return func() {
		cancel()
		wg.Wait()
	}
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

type permanentError struct {
	err error
}

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
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

func listOutputFiles(root string) ([]string, error) {
	files := make([]string, 0, 16)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func detectContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func upsertGeneratedMediaTx(ctx context.Context, tx *sql.Tx, provider, bucket, createdBy string, media generatedMedia, now string) (string, error) {
	mediaID := uuid.NewString()
	_, err := tx.ExecContext(ctx, `
INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, checksum_sha256, duration_sec, width, height, created_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL, 0, NULL, NULL, ?, ?)
ON CONFLICT(object_key) DO UPDATE SET
    mime_type = excluded.mime_type,
    size_bytes = excluded.size_bytes`,
		mediaID,
		provider,
		nullableString(bucket),
		media.ObjectKey,
		media.OriginalFilename,
		media.ContentType,
		media.SizeBytes,
		createdBy,
		now,
	)
	if err != nil {
		return "", err
	}

	var existingID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM media_objects WHERE object_key = ? LIMIT 1`, media.ObjectKey).Scan(&existingID); err != nil {
		return "", err
	}
	return existingID, nil
}

func nullableString(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
