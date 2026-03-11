package transcode

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/config"
	"moevideo/backend/internal/db"
	"moevideo/backend/internal/storage"
	"moevideo/backend/internal/util"
)

type fakeEngine struct {
	err error
}

func (f fakeEngine) BuildHLS(_ context.Context, _ string, outputDir string, segmentSeconds int64) (*BuildResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	variantDir := filepath.Join(outputDir, "360p")
	if err := os.MkdirAll(variantDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(variantDir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(variantDir, "seg_000.ts"), []byte("segment"), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "master.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		return nil, err
	}
	return &BuildResult{
		MasterPlaylist: "master.m3u8",
		SegmentSeconds: segmentSeconds,
		Variants: []VariantInfo{
			{
				Name:              "360p",
				Width:             640,
				Height:            360,
				Bandwidth:         896000,
				PlaylistObjectKey: "360p/index.m3u8",
			},
		},
	}, nil
}

func (f fakeEngine) GenerateCover(_ context.Context, _ string, outputPath string) error {
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(outputPath, []byte("cover"), 0o644)
}

func (f fakeEngine) GeneratePreviewWebP(_ context.Context, _ string, outputPath string) error {
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(outputPath, []byte("preview"), 0o644)
}

func buildWorkerTestApp(t *testing.T) (*app.App, string) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := config.Config{
		Env:             "test",
		DBPath:          filepath.Join(tmpDir, "worker.db"),
		StorageDriver:   "local",
		LocalStorageDir: filepath.Join(tmpDir, "storage"),
		PublicBaseURL:   "http://localhost:8080",
		FFmpegBin:       "ffmpeg",
		FFprobeBin:      "ffprobe",
		TranscodePoll:   time.Millisecond,
		TranscodeMaxTry: 3,
	}

	if err := os.MkdirAll(cfg.LocalStorageDir, 0o755); err != nil {
		t.Fatalf("mkdir storage: %v", err)
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	storageSvc, err := storage.NewService(cfg)
	if err != nil {
		t.Fatalf("create storage svc: %v", err)
	}

	return &app.App{
		Config:  cfg,
		DB:      database,
		Storage: storageSvc,
	}, tmpDir
}

func seedTranscodeJob(t *testing.T, a *app.App, maxAttempts int) (videoID string) {
	t.Helper()

	userID := "user-" + uuid.NewString()
	mediaID := "media-" + uuid.NewString()
	videoID = "video-" + uuid.NewString()
	jobID := "job-" + uuid.NewString()
	now := util.FormatTime(time.Now().UTC())
	sourceObjectKey := "videos/" + userID + "/source.mp4"
	sourcePath := a.Storage.LocalObjectPath(sourceObjectKey)

	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("mp4-binary"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	if _, err := a.DB.Exec(
		`INSERT INTO users (id, username, email, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID,
		"worker_user",
		"worker_user@example.com",
		"hash",
		now,
		now,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := a.DB.Exec(
		`INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, created_by, created_at)
		 VALUES (?, 'local', '', ?, 'source.mp4', 'video/mp4', 1024, ?, ?)`,
		mediaID,
		sourceObjectKey,
		userID,
		now,
	); err != nil {
		t.Fatalf("insert media: %v", err)
	}

	if _, err := a.DB.Exec(
		`INSERT INTO videos (id, uploader_id, title, description, source_media_id, status, visibility, duration_sec, created_at, updated_at)
		 VALUES (?, ?, 'worker video', 'worker', ?, 'processing', 'public', 120, ?, ?)`,
		videoID,
		userID,
		mediaID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert video: %v", err)
	}

	if _, err := a.DB.Exec(
		`INSERT INTO video_transcode_jobs (id, video_id, status, attempts, max_attempts, available_at, created_at, updated_at)
		 VALUES (?, ?, 'queued', 0, ?, ?, ?, ?)`,
		jobID,
		videoID,
		maxAttempts,
		now,
		now,
		now,
	); err != nil {
		t.Fatalf("insert transcode job: %v", err)
	}

	return videoID
}

func TestWorkerRunOnceSuccess(t *testing.T) {
	appContainer, _ := buildWorkerTestApp(t)
	videoID := seedTranscodeJob(t, appContainer, 3)

	worker := NewWorker(appContainer, WithEngine(fakeEngine{}), WithPollInterval(time.Millisecond))
	hasWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once success should not fail: %v", err)
	}
	if !hasWork {
		t.Fatalf("expected worker to claim one job")
	}

	var videoStatus string
	if err := appContainer.DB.QueryRow(`SELECT status FROM videos WHERE id = ?`, videoID).Scan(&videoStatus); err != nil {
		t.Fatalf("query video status: %v", err)
	}
	if videoStatus != "published" {
		t.Fatalf("expected video to be published, got %q", videoStatus)
	}

	var jobStatus string
	if err := appContainer.DB.QueryRow(`SELECT status FROM video_transcode_jobs WHERE video_id = ?`, videoID).Scan(&jobStatus); err != nil {
		t.Fatalf("query job status: %v", err)
	}
	if jobStatus != "succeeded" {
		t.Fatalf("expected job succeeded, got %q", jobStatus)
	}

	var hlsCount int
	if err := appContainer.DB.QueryRow(`SELECT COUNT(1) FROM video_hls_assets WHERE video_id = ?`, videoID).Scan(&hlsCount); err != nil {
		t.Fatalf("query hls assets: %v", err)
	}
	if hlsCount != 1 {
		t.Fatalf("expected one hls asset row, got %d", hlsCount)
	}

	var (
		coverMediaID   sql.NullString
		previewMediaID sql.NullString
	)
	if err := appContainer.DB.QueryRow(`SELECT cover_media_id, preview_media_id FROM videos WHERE id = ?`, videoID).Scan(&coverMediaID, &previewMediaID); err != nil {
		t.Fatalf("query generated media ids: %v", err)
	}
	if !coverMediaID.Valid {
		t.Fatalf("expected cover media id to be generated")
	}
	if !previewMediaID.Valid {
		t.Fatalf("expected preview media id to be generated")
	}
}

func TestWorkerRunOnceFinalFailure(t *testing.T) {
	appContainer, _ := buildWorkerTestApp(t)
	videoID := seedTranscodeJob(t, appContainer, 1)

	worker := NewWorker(appContainer, WithEngine(fakeEngine{err: context.DeadlineExceeded}), WithPollInterval(time.Millisecond))
	hasWork, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once failure path should not return error: %v", err)
	}
	if !hasWork {
		t.Fatalf("expected worker to claim one job")
	}

	var videoStatus string
	if err := appContainer.DB.QueryRow(`SELECT status FROM videos WHERE id = ?`, videoID).Scan(&videoStatus); err != nil {
		t.Fatalf("query video status: %v", err)
	}
	if videoStatus != "failed" {
		t.Fatalf("expected video failed, got %q", videoStatus)
	}

	var jobStatus string
	if err := appContainer.DB.QueryRow(`SELECT status FROM video_transcode_jobs WHERE video_id = ?`, videoID).Scan(&jobStatus); err != nil {
		t.Fatalf("query job status: %v", err)
	}
	if jobStatus != "failed" {
		t.Fatalf("expected job failed, got %q", jobStatus)
	}
}
