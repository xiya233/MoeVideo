package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"moevideo/backend/internal/app"
	"moevideo/backend/internal/auth"
	"moevideo/backend/internal/config"
	"moevideo/backend/internal/db"
	"moevideo/backend/internal/handlers"
	"moevideo/backend/internal/response"
	"moevideo/backend/internal/storage"
	"moevideo/backend/internal/util"
)

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newTestServer(t *testing.T) (*fiber.App, *app.App) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := config.Config{
		Env:              "test",
		HTTPAddr:         ":0",
		DBPath:           filepath.Join(tmpDir, "moevideo-test.db"),
		JWTSecret:        "test-secret",
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  24 * time.Hour,
		StorageDriver:    "local",
		LocalStorageDir:  filepath.Join(tmpDir, "storage"),
		PublicBaseURL:    "http://localhost:8080",
		MaxUploadBytes:   2 * 1024 * 1024 * 1024,
		UploadURLExpires: 15 * time.Minute,
		FFmpegBin:        "ffmpeg",
		FFprobeBin:       "ffprobe",
		TranscodePoll:    time.Second,
		TranscodeMaxTry:  3,
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	if err := os.MkdirAll(cfg.LocalStorageDir, 0o755); err != nil {
		t.Fatalf("mkdir local storage: %v", err)
	}
	storageSvc, err := storage.NewService(cfg)
	if err != nil {
		t.Fatalf("create storage service: %v", err)
	}

	container := &app.App{
		Config:  cfg,
		DB:      database,
		JWT:     auth.NewManager(cfg.JWTSecret),
		Storage: storageSvc,
	}

	server := fiber.New(fiber.Config{
		BodyLimit: int(cfg.MaxUploadBytes),
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if errors.Is(err, fiber.ErrRequestEntityTooLarge) {
				return response.Error(c, fiber.StatusRequestEntityTooLarge, "request entity too large")
			}
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				return response.Error(c, fiberErr.Code, fiberErr.Message)
			}
			return response.Error(c, fiber.StatusInternalServerError, "internal server error")
		},
	})
	api := server.Group("/api/v1")
	handlers.RegisterRoutes(api, container)
	server.Static("/media", cfg.LocalStorageDir)

	return server, container
}

func doJSONRequest(t *testing.T, srv *fiber.App, method, path string, body interface{}, headers map[string]string) (int, apiResponse) {
	t.Helper()

	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		payload = bytes.NewReader(b)
	}
	if payload == nil {
		payload = http.NoBody
	}

	req := httptest.NewRequest(method, path, payload)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := srv.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	var out apiResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		t.Fatalf("unmarshal response: %v, body=%s", err, string(bodyBytes))
	}
	return resp.StatusCode, out
}

func registerUser(t *testing.T, srv *fiber.App, username, email, password string) (userID, accessToken, refreshToken string) {
	t.Helper()

	status, resp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/register", map[string]interface{}{
		"username":         username,
		"email":            email,
		"password":         password,
		"password_confirm": password,
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("expected 201 register, got %d (%s)", status, resp.Message)
	}

	var payload struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("parse register response: %v", err)
	}
	return payload.User.ID, payload.Tokens.AccessToken, payload.Tokens.RefreshToken
}

func TestAuthFlow(t *testing.T) {
	srv, _ := newTestServer(t)

	_, _, refresh := registerUser(t, srv, "alice", "alice@example.com", "password123")

	status, _ := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]interface{}{
		"account":  "alice",
		"password": "password123",
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("username login should succeed, got %d", status)
	}

	status, loginByEmail := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]interface{}{
		"account":  "alice@example.com",
		"password": "password123",
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("email login should succeed, got %d", status)
	}

	var loginData struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(loginByEmail.Data, &loginData); err != nil {
		t.Fatalf("parse login response: %v", err)
	}

	status, refreshResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/refresh", map[string]interface{}{
		"refresh_token": refresh,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("refresh should succeed, got %d", status)
	}
	var refreshed struct {
		Tokens struct {
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(refreshResp.Data, &refreshed); err != nil {
		t.Fatalf("parse refresh response: %v", err)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/logout", map[string]interface{}{
		"refresh_token": loginData.Tokens.RefreshToken,
	}, map[string]string{"Authorization": "Bearer " + loginData.Tokens.AccessToken})
	if status != http.StatusOK {
		t.Fatalf("logout should succeed, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/refresh", map[string]interface{}{
		"refresh_token": loginData.Tokens.RefreshToken,
	}, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked refresh should fail with 401, got %d", status)
	}
}

func prepareVideoForUser(t *testing.T, container *app.App, userID string) string {
	t.Helper()
	now := util.FormatTime(time.Now().UTC())
	mediaID := "media-" + uuid.NewString()
	videoID := "video-" + uuid.NewString()

	_, err := container.DB.Exec(
		`INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, duration_sec, created_by, created_at)
		 VALUES (?, 'local', '', ?, 'sample.mp4', 'video/mp4', 1024, 120, ?, ?)`,
		mediaID,
		"videos/"+userID+"/seed/sample.mp4",
		userID,
		now,
	)
	if err != nil {
		t.Fatalf("insert media: %v", err)
	}

	_, err = container.DB.Exec(
		`INSERT INTO videos (id, uploader_id, title, description, source_media_id, status, visibility, duration_sec, published_at, created_at, updated_at)
		 VALUES (?, ?, 'seed video', 'seed', ?, 'published', 'public', 120, ?, ?, ?)`,
		videoID,
		userID,
		mediaID,
		now,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert video: %v", err)
	}
	return videoID
}

func TestCommentNestedReplyRejected(t *testing.T) {
	srv, container := newTestServer(t)
	userID, access, _ := registerUser(t, srv, "bob", "bob@example.com", "password123")
	videoID := prepareVideoForUser(t, container, userID)

	status, topResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/comments", map[string]interface{}{
		"content": "top comment",
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusCreated {
		t.Fatalf("create top comment should succeed, got %d", status)
	}
	var topData struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(topResp.Data, &topData); err != nil {
		t.Fatalf("parse top comment response: %v", err)
	}

	status, replyResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/comments", map[string]interface{}{
		"content":           "first-level reply",
		"parent_comment_id": topData.ID,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusCreated {
		t.Fatalf("first-level reply should succeed, got %d", status)
	}
	var replyData struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(replyResp.Data, &replyData); err != nil {
		t.Fatalf("parse reply response: %v", err)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/comments", map[string]interface{}{
		"content":           "nested reply",
		"parent_comment_id": replyData.ID,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusBadRequest {
		t.Fatalf("nested reply should be rejected with 400, got %d", status)
	}
}

func TestVideoLikeIdempotent(t *testing.T) {
	srv, container := newTestServer(t)
	userID, access, _ := registerUser(t, srv, "carol", "carol@example.com", "password123")
	videoID := prepareVideoForUser(t, container, userID)

	for i := 0; i < 2; i++ {
		status, _ := doJSONRequest(t, srv, http.MethodPut, "/api/v1/videos/"+videoID+"/like", map[string]interface{}{"active": true}, map[string]string{"Authorization": "Bearer " + access})
		if status != http.StatusOK {
			t.Fatalf("like request %d should succeed, got %d", i+1, status)
		}
	}

	var likes int64
	if err := container.DB.QueryRow(`SELECT likes_count FROM videos WHERE id = ?`, videoID).Scan(&likes); err != nil {
		t.Fatalf("query likes count: %v", err)
	}
	if likes != 1 {
		t.Fatalf("likes_count expected 1, got %d", likes)
	}

	for i := 0; i < 2; i++ {
		status, _ := doJSONRequest(t, srv, http.MethodPut, "/api/v1/videos/"+videoID+"/like", map[string]interface{}{"active": false}, map[string]string{"Authorization": "Bearer " + access})
		if status != http.StatusOK {
			t.Fatalf("unlike request %d should succeed, got %d", i+1, status)
		}
	}

	if err := container.DB.QueryRow(`SELECT likes_count FROM videos WHERE id = ?`, videoID).Scan(&likes); err != nil {
		t.Fatalf("query likes count: %v", err)
	}
	if likes != 0 {
		t.Fatalf("likes_count expected 0, got %d", likes)
	}
}

func TestLocalUploadOverDefaultBodyLimit(t *testing.T) {
	srv, _ := newTestServer(t)
	_, access, _ := registerUser(t, srv, "dave", "dave@example.com", "password123")

	status, presignResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/uploads/presign", map[string]interface{}{
		"purpose":         "video",
		"filename":        "big-file.mp4",
		"content_type":    "video/mp4",
		"file_size_bytes": 5 * 1024 * 1024,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusCreated {
		t.Fatalf("presign should succeed, got %d (%s)", status, presignResp.Message)
	}

	var presignData struct {
		UploadURL string `json:"upload_url"`
	}
	if err := json.Unmarshal(presignResp.Data, &presignData); err != nil {
		t.Fatalf("parse presign response: %v", err)
	}

	parsedURL, err := url.Parse(presignData.UploadURL)
	if err != nil {
		t.Fatalf("parse upload url: %v", err)
	}

	body := bytes.Repeat([]byte("a"), 5*1024*1024)
	req := httptest.NewRequest(http.MethodPut, parsedURL.Path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "video/mp4")

	resp, err := srv.Test(req, -1)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read upload response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload should succeed with 200, got %d, body=%s", resp.StatusCode, string(respBody))
	}
}

func TestUploadPresignExtendedVideoFormats(t *testing.T) {
	srv, _ := newTestServer(t)
	_, access, _ := registerUser(t, srv, "doris", "doris@example.com", "password123")

	status, _ := doJSONRequest(t, srv, http.MethodPost, "/api/v1/uploads/presign", map[string]interface{}{
		"purpose":         "video",
		"filename":        "sample.mkv",
		"content_type":    "video/x-matroska",
		"file_size_bytes": 1024,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusCreated {
		t.Fatalf("mkv upload should be accepted, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/uploads/presign", map[string]interface{}{
		"purpose":         "video",
		"filename":        "type-empty.ts",
		"content_type":    "",
		"file_size_bytes": 1024,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusCreated {
		t.Fatalf("empty mime with allowed extension should be accepted, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/uploads/presign", map[string]interface{}{
		"purpose":         "video",
		"filename":        "evil.exe",
		"content_type":    "application/octet-stream",
		"file_size_bytes": 1024,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusBadRequest {
		t.Fatalf("unsupported extension should be rejected, got %d", status)
	}
}

func TestVideoProgressSaveAndClear(t *testing.T) {
	srv, container := newTestServer(t)
	userID, access, _ := registerUser(t, srv, "gina", "gina@example.com", "password123")
	videoID := prepareVideoForUser(t, container, userID)

	status, saveResp := doJSONRequest(t, srv, http.MethodPut, "/api/v1/videos/"+videoID+"/progress", map[string]interface{}{
		"position_sec": 45,
		"duration_sec": 120,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusOK {
		t.Fatalf("save progress should succeed, got %d (%s)", status, saveResp.Message)
	}
	var saveData struct {
		PositionSec int64 `json:"position_sec"`
	}
	if err := json.Unmarshal(saveResp.Data, &saveData); err != nil {
		t.Fatalf("parse save progress response: %v", err)
	}
	if saveData.PositionSec != 45 {
		t.Fatalf("expected saved progress 45, got %d", saveData.PositionSec)
	}

	status, detailResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID, nil, map[string]string{
		"Authorization": "Bearer " + access,
	})
	if status != http.StatusOK {
		t.Fatalf("owner detail should succeed, got %d", status)
	}
	var detailData struct {
		ViewerProgressSec int64 `json:"viewer_progress_sec"`
	}
	if err := json.Unmarshal(detailResp.Data, &detailData); err != nil {
		t.Fatalf("parse detail data: %v", err)
	}
	if detailData.ViewerProgressSec != 45 {
		t.Fatalf("expected viewer_progress_sec=45, got %d", detailData.ViewerProgressSec)
	}

	status, clearResp := doJSONRequest(t, srv, http.MethodPut, "/api/v1/videos/"+videoID+"/progress", map[string]interface{}{
		"position_sec": 119,
		"duration_sec": 120,
		"completed":    true,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusOK {
		t.Fatalf("clear progress should succeed, got %d (%s)", status, clearResp.Message)
	}
	var clearData struct {
		PositionSec int64 `json:"position_sec"`
	}
	if err := json.Unmarshal(clearResp.Data, &clearData); err != nil {
		t.Fatalf("parse clear progress response: %v", err)
	}
	if clearData.PositionSec != 0 {
		t.Fatalf("expected cleared progress 0, got %d", clearData.PositionSec)
	}

	status, detailResp = doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID, nil, map[string]string{
		"Authorization": "Bearer " + access,
	})
	if status != http.StatusOK {
		t.Fatalf("owner detail should still succeed, got %d", status)
	}
	if err := json.Unmarshal(detailResp.Data, &detailData); err != nil {
		t.Fatalf("parse detail data after clear: %v", err)
	}
	if detailData.ViewerProgressSec != 0 {
		t.Fatalf("expected viewer_progress_sec reset to 0, got %d", detailData.ViewerProgressSec)
	}
}

func TestVideoCardIncludesPreviewWebPURL(t *testing.T) {
	srv, container := newTestServer(t)
	userID, _, _ := registerUser(t, srv, "helen", "helen@example.com", "password123")
	videoID := prepareVideoForUser(t, container, userID)

	now := util.FormatTime(time.Now().UTC())
	previewMediaID := "media-" + uuid.NewString()
	if _, err := container.DB.Exec(
		`INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, duration_sec, created_by, created_at)
		 VALUES (?, 'local', '', ?, 'preview.webp', 'image/webp', 1234, 0, ?, ?)`,
		previewMediaID,
		"hls/"+userID+"/"+videoID+"/preview.webp",
		userID,
		now,
	); err != nil {
		t.Fatalf("insert preview media: %v", err)
	}
	if _, err := container.DB.Exec(`UPDATE videos SET preview_media_id = ?, updated_at = ? WHERE id = ?`, previewMediaID, now, videoID); err != nil {
		t.Fatalf("set preview media id: %v", err)
	}

	status, videosResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos?limit=10", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list videos should succeed, got %d", status)
	}
	var payload struct {
		Items []struct {
			ID             string `json:"id"`
			PreviewWebPURL string `json:"preview_webp_url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(videosResp.Data, &payload); err != nil {
		t.Fatalf("parse videos payload: %v", err)
	}
	if len(payload.Items) == 0 {
		t.Fatalf("expected at least one video card")
	}

	found := false
	for _, item := range payload.Items {
		if item.ID != videoID {
			continue
		}
		found = true
		if item.PreviewWebPURL == "" {
			t.Fatalf("expected preview_webp_url for target video")
		}
	}
	if !found {
		t.Fatalf("expected target video %s in list payload", videoID)
	}
}

func TestCreateVideoEnqueueProcessingAndDetailVisibility(t *testing.T) {
	srv, container := newTestServer(t)
	userID, access, _ := registerUser(t, srv, "eva", "eva@example.com", "password123")

	now := util.FormatTime(time.Now().UTC())
	mediaID := "media-" + uuid.NewString()
	_, err := container.DB.Exec(
		`INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, duration_sec, created_by, created_at)
		 VALUES (?, 'local', '', ?, 'source.mp4', 'video/mp4', 2048, 90, ?, ?)`,
		mediaID,
		"videos/"+userID+"/seed/source.mp4",
		userID,
		now,
	)
	if err != nil {
		t.Fatalf("insert source media: %v", err)
	}

	status, createResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos", map[string]interface{}{
		"title":           "processing video",
		"description":     "queued",
		"source_media_id": mediaID,
		"visibility":      "public",
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusCreated {
		t.Fatalf("create video should succeed, got %d (%s)", status, createResp.Message)
	}

	var createData struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(createResp.Data, &createData); err != nil {
		t.Fatalf("parse create video response: %v", err)
	}
	if createData.ID == "" {
		t.Fatalf("create response missing video id")
	}
	if createData.Status != "processing" {
		t.Fatalf("expected processing status, got %q", createData.Status)
	}

	var dbStatus string
	if err := container.DB.QueryRow(`SELECT status FROM videos WHERE id = ?`, createData.ID).Scan(&dbStatus); err != nil {
		t.Fatalf("query created video: %v", err)
	}
	if dbStatus != "processing" {
		t.Fatalf("expected db status processing, got %q", dbStatus)
	}

	var queuedJobs int
	if err := container.DB.QueryRow(`SELECT COUNT(1) FROM video_transcode_jobs WHERE video_id = ? AND status = 'queued'`, createData.ID).Scan(&queuedJobs); err != nil {
		t.Fatalf("query transcode jobs: %v", err)
	}
	if queuedJobs != 1 {
		t.Fatalf("expected one queued transcode job, got %d", queuedJobs)
	}

	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+createData.ID, nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("public should not access processing video detail, got %d", status)
	}

	status, ownerDetail := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+createData.ID, nil, map[string]string{
		"Authorization": "Bearer " + access,
	})
	if status != http.StatusOK {
		t.Fatalf("owner should access processing video detail, got %d", status)
	}
	var detailData struct {
		Status   string `json:"status"`
		Playback struct {
			Status string `json:"status"`
			Type   string `json:"type"`
		} `json:"playback"`
	}
	if err := json.Unmarshal(ownerDetail.Data, &detailData); err != nil {
		t.Fatalf("parse detail response: %v", err)
	}
	if detailData.Status != "processing" {
		t.Fatalf("expected detail status processing, got %q", detailData.Status)
	}
	if detailData.Playback.Status != "processing" {
		t.Fatalf("expected playback status processing, got %q", detailData.Playback.Status)
	}
	if detailData.Playback.Type != "" {
		t.Fatalf("expected empty playback type during processing, got %q", detailData.Playback.Type)
	}
}

func TestVideoDetailPlaybackModeCompat(t *testing.T) {
	srv, container := newTestServer(t)
	userID, _, _ := registerUser(t, srv, "fred", "fred@example.com", "password123")
	videoID := prepareVideoForUser(t, container, userID)

	status, detailResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("legacy video detail should be accessible, got %d", status)
	}
	var legacyData struct {
		SourceURL string `json:"source_url"`
		Playback  struct {
			Status string `json:"status"`
			Type   string `json:"type"`
			MP4URL string `json:"mp4_url"`
		} `json:"playback"`
	}
	if err := json.Unmarshal(detailResp.Data, &legacyData); err != nil {
		t.Fatalf("parse legacy detail: %v", err)
	}
	if legacyData.Playback.Status != "ready" {
		t.Fatalf("expected legacy playback status ready, got %q", legacyData.Playback.Status)
	}
	if legacyData.Playback.Type != "mp4" {
		t.Fatalf("expected legacy playback type mp4, got %q", legacyData.Playback.Type)
	}
	if legacyData.SourceURL == "" || legacyData.Playback.MP4URL == "" {
		t.Fatalf("expected legacy mp4 urls to be present")
	}

	variantsJSON := `[{"name":"360p","width":640,"height":360,"bandwidth":896000,"playlist_object_key":"hls/` + userID + `/` + videoID + `/360p/index.m3u8"}]`
	now := util.FormatTime(time.Now().UTC())
	_, err := container.DB.Exec(
		`INSERT INTO video_hls_assets (video_id, provider, bucket, master_object_key, variants_json, segment_seconds, created_at, updated_at)
		 VALUES (?, 'local', '', ?, ?, 4, ?, ?)`,
		videoID,
		"hls/"+userID+"/"+videoID+"/master.m3u8",
		variantsJSON,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert hls asset: %v", err)
	}

	status, hlsResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("hls video detail should be accessible, got %d", status)
	}
	var hlsData struct {
		Playback struct {
			Status       string `json:"status"`
			Type         string `json:"type"`
			HLSMasterURL string `json:"hls_master_url"`
			MP4URL       string `json:"mp4_url"`
			Variants     []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"variants"`
		} `json:"playback"`
	}
	if err := json.Unmarshal(hlsResp.Data, &hlsData); err != nil {
		t.Fatalf("parse hls detail: %v", err)
	}
	if hlsData.Playback.Status != "ready" {
		t.Fatalf("expected hls playback status ready, got %q", hlsData.Playback.Status)
	}
	if hlsData.Playback.Type != "hls" {
		t.Fatalf("expected playback type hls, got %q", hlsData.Playback.Type)
	}
	if hlsData.Playback.HLSMasterURL == "" {
		t.Fatalf("expected hls master url")
	}
	if hlsData.Playback.MP4URL == "" {
		t.Fatalf("expected mp4 fallback url")
	}
	if len(hlsData.Playback.Variants) == 0 || hlsData.Playback.Variants[0].URL == "" {
		t.Fatalf("expected at least one hls variant url")
	}
}
