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

func issueCaptcha(t *testing.T, srv *fiber.App, scene string) (captchaID, captchaCode string) {
	t.Helper()

	status, resp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/auth/captcha?scene="+url.QueryEscape(scene), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("issue captcha should return 200, got %d (%s)", status, resp.Message)
	}

	var payload struct {
		CaptchaID   string `json:"captcha_id"`
		CaptchaCode string `json:"captcha_code"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("parse captcha response: %v", err)
	}
	if payload.CaptchaID == "" || payload.CaptchaCode == "" {
		t.Fatalf("captcha response missing captcha_id/captcha_code")
	}
	return payload.CaptchaID, payload.CaptchaCode
}

func registerUser(t *testing.T, srv *fiber.App, username, email, password string) (userID, accessToken, refreshToken string) {
	t.Helper()

	captchaID, captchaCode := issueCaptcha(t, srv, "register")
	status, resp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/register", map[string]interface{}{
		"username":         username,
		"email":            email,
		"password":         password,
		"password_confirm": password,
		"captcha_id":       captchaID,
		"captcha_code":     captchaCode,
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

	loginCaptchaID, loginCaptchaCode := issueCaptcha(t, srv, "login")
	status, _ := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]interface{}{
		"account":      "alice",
		"password":     "password123",
		"captcha_id":   loginCaptchaID,
		"captcha_code": loginCaptchaCode,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("username login should succeed, got %d", status)
	}

	emailCaptchaID, emailCaptchaCode := issueCaptcha(t, srv, "login")
	status, loginByEmail := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]interface{}{
		"account":      "alice@example.com",
		"password":     "password123",
		"captcha_id":   emailCaptchaID,
		"captcha_code": emailCaptchaCode,
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

func TestRegisterDisabled(t *testing.T) {
	srv, container := newTestServer(t)

	if _, err := container.DB.Exec(`UPDATE site_settings SET register_enabled = 0, updated_at = datetime('now') WHERE id = 1`); err != nil {
		t.Fatalf("disable registration: %v", err)
	}

	captchaID, captchaCode := issueCaptcha(t, srv, "register")
	status, resp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/auth/register", map[string]interface{}{
		"username":         "blocked",
		"email":            "blocked@example.com",
		"password":         "password123",
		"password_confirm": "password123",
		"captcha_id":       captchaID,
		"captcha_code":     captchaCode,
	}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("register when disabled should return 403, got %d (%s)", status, resp.Message)
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

func TestListCommentsIncludesLikedForAuthenticatedViewer(t *testing.T) {
	srv, container := newTestServer(t)

	ownerID, ownerAccess, _ := registerUser(t, srv, "luna", "luna@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)

	status, topResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/comments", map[string]interface{}{
		"content": "top comment",
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
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
		"content":           "reply comment",
		"parent_comment_id": topData.ID,
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusCreated {
		t.Fatalf("create reply comment should succeed, got %d", status)
	}
	var replyData struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(replyResp.Data, &replyData); err != nil {
		t.Fatalf("parse reply comment response: %v", err)
	}

	_, viewerAccess, _ := registerUser(t, srv, "mike", "mike@example.com", "password123")

	status, _ = doJSONRequest(t, srv, http.MethodPut, "/api/v1/comments/"+topData.ID+"/like", map[string]interface{}{
		"active": true,
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusOK {
		t.Fatalf("viewer like top comment should succeed, got %d", status)
	}
	status, _ = doJSONRequest(t, srv, http.MethodPut, "/api/v1/comments/"+replyData.ID+"/like", map[string]interface{}{
		"active": true,
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusOK {
		t.Fatalf("viewer like reply comment should succeed, got %d", status)
	}

	status, viewerCommentsResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID+"/comments?limit=20", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("viewer comments list should succeed, got %d", status)
	}
	var viewerComments struct {
		Items []struct {
			ID      string `json:"id"`
			Liked   bool   `json:"liked"`
			Replies []struct {
				ID    string `json:"id"`
				Liked bool   `json:"liked"`
			} `json:"replies"`
		} `json:"items"`
	}
	if err := json.Unmarshal(viewerCommentsResp.Data, &viewerComments); err != nil {
		t.Fatalf("parse viewer comments response: %v", err)
	}

	foundTop := false
	foundReply := false
	for _, item := range viewerComments.Items {
		if item.ID != topData.ID {
			continue
		}
		foundTop = true
		if !item.Liked {
			t.Fatalf("expected top comment liked=true for authenticated viewer")
		}
		for _, reply := range item.Replies {
			if reply.ID != replyData.ID {
				continue
			}
			foundReply = true
			if !reply.Liked {
				t.Fatalf("expected reply liked=true for authenticated viewer")
			}
		}
	}
	if !foundTop {
		t.Fatalf("expected top comment %s in viewer list", topData.ID)
	}
	if !foundReply {
		t.Fatalf("expected reply comment %s in viewer list", replyData.ID)
	}

	status, anonCommentsResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID+"/comments?limit=20", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("anonymous comments list should succeed, got %d", status)
	}
	var anonComments struct {
		Items []struct {
			ID      string `json:"id"`
			Liked   bool   `json:"liked"`
			Replies []struct {
				ID    string `json:"id"`
				Liked bool   `json:"liked"`
			} `json:"replies"`
		} `json:"items"`
	}
	if err := json.Unmarshal(anonCommentsResp.Data, &anonComments); err != nil {
		t.Fatalf("parse anonymous comments response: %v", err)
	}

	for _, item := range anonComments.Items {
		if item.ID != topData.ID {
			continue
		}
		if item.Liked {
			t.Fatalf("expected top comment liked=false for anonymous viewer")
		}
		for _, reply := range item.Replies {
			if reply.ID == replyData.ID && reply.Liked {
				t.Fatalf("expected reply liked=false for anonymous viewer")
			}
		}
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

func TestUpdateMeProfile(t *testing.T) {
	srv, container := newTestServer(t)
	userID, access, _ := registerUser(t, srv, "iris", "iris@example.com", "password123")

	now := util.FormatTime(time.Now().UTC())
	avatarMediaID := "media-" + uuid.NewString()
	if _, err := container.DB.Exec(
		`INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, duration_sec, created_by, created_at)
		 VALUES (?, 'local', '', ?, 'avatar.webp', 'image/webp', 1024, 0, ?, ?)`,
		avatarMediaID,
		"images/"+userID+"/avatar.webp",
		userID,
		now,
	); err != nil {
		t.Fatalf("insert avatar media: %v", err)
	}

	status, patchResp := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/users/me", map[string]interface{}{
		"bio":             "hello moevideo",
		"avatar_media_id": avatarMediaID,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusOK {
		t.Fatalf("patch /users/me should succeed, got %d (%s)", status, patchResp.Message)
	}

	status, meResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/me", nil, map[string]string{
		"Authorization": "Bearer " + access,
	})
	if status != http.StatusOK {
		t.Fatalf("get /users/me should succeed, got %d (%s)", status, meResp.Message)
	}

	var meData struct {
		Bio       string `json:"bio"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(meResp.Data, &meData); err != nil {
		t.Fatalf("parse /users/me response: %v", err)
	}
	if meData.Bio != "hello moevideo" {
		t.Fatalf("expected bio updated, got %q", meData.Bio)
	}
	if meData.AvatarURL == "" {
		t.Fatalf("expected avatar_url to be set")
	}
}

func TestMeCenterLists(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, _, _ := registerUser(t, srv, "jane", "jane@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)
	_, viewerAccess, _ := registerUser(t, srv, "kate", "kate@example.com", "password123")

	status, _ := doJSONRequest(t, srv, http.MethodPut, "/api/v1/users/"+ownerID+"/follow", map[string]interface{}{
		"active": true,
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusOK {
		t.Fatalf("follow should succeed, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPut, "/api/v1/videos/"+videoID+"/favorite", map[string]interface{}{
		"active": true,
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusOK {
		t.Fatalf("favorite should succeed, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPut, "/api/v1/videos/"+videoID+"/progress", map[string]interface{}{
		"position_sec": 35,
		"duration_sec": 120,
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusOK {
		t.Fatalf("save progress should succeed, got %d", status)
	}

	status, favoritesResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/me/favorites?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("favorites list should succeed, got %d", status)
	}
	var favoritesData struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(favoritesResp.Data, &favoritesData); err != nil {
		t.Fatalf("parse favorites response: %v", err)
	}
	if len(favoritesData.Items) == 0 || favoritesData.Items[0].ID != videoID {
		t.Fatalf("expected favorite video %s in list", videoID)
	}

	status, followingResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/me/following?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("following list should succeed, got %d", status)
	}
	var followingData struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(followingResp.Data, &followingData); err != nil {
		t.Fatalf("parse following response: %v", err)
	}
	if len(followingData.Items) == 0 || followingData.Items[0].ID != ownerID {
		t.Fatalf("expected followed user %s in list", ownerID)
	}

	status, continueResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/me/continue-watching?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("continue watching list should succeed, got %d", status)
	}
	var continueData struct {
		Items []struct {
			PositionSec int64 `json:"position_sec"`
			Video       struct {
				ID string `json:"id"`
			} `json:"video"`
		} `json:"items"`
	}
	if err := json.Unmarshal(continueResp.Data, &continueData); err != nil {
		t.Fatalf("parse continue watching response: %v", err)
	}
	if len(continueData.Items) == 0 || continueData.Items[0].Video.ID != videoID {
		t.Fatalf("expected continue watching video %s in list", videoID)
	}
	if continueData.Items[0].PositionSec != 35 {
		t.Fatalf("expected position_sec=35, got %d", continueData.Items[0].PositionSec)
	}
}

func TestUpdateMePrivacyFields(t *testing.T) {
	srv, _ := newTestServer(t)
	_, access, _ := registerUser(t, srv, "nina", "nina@example.com", "password123")

	status, patchResp := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/users/me", map[string]interface{}{
		"profile_public":   true,
		"public_videos":    true,
		"public_favorites": true,
		"public_following": true,
		"public_followers": true,
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusOK {
		t.Fatalf("patch /users/me privacy should succeed, got %d (%s)", status, patchResp.Message)
	}

	status, meResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/me", nil, map[string]string{
		"Authorization": "Bearer " + access,
	})
	if status != http.StatusOK {
		t.Fatalf("get /users/me should succeed, got %d (%s)", status, meResp.Message)
	}

	var meData struct {
		ProfilePublic   bool `json:"profile_public"`
		PublicVideos    bool `json:"public_videos"`
		PublicFavorites bool `json:"public_favorites"`
		PublicFollowing bool `json:"public_following"`
		PublicFollowers bool `json:"public_followers"`
	}
	if err := json.Unmarshal(meResp.Data, &meData); err != nil {
		t.Fatalf("parse /users/me response: %v", err)
	}
	if !meData.ProfilePublic || !meData.PublicVideos || !meData.PublicFavorites || !meData.PublicFollowing || !meData.PublicFollowers {
		t.Fatalf("expected all privacy flags true, got %+v", meData)
	}
}

func TestUserProfilePrivacyAndFollowers(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, ownerAccess, _ := registerUser(t, srv, "olivia", "olivia@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)
	viewerID, viewerAccess, _ := registerUser(t, srv, "peter", "peter@example.com", "password123")
	_ = viewerID

	status, _ := doJSONRequest(t, srv, http.MethodPut, "/api/v1/users/"+ownerID+"/follow", map[string]interface{}{
		"active": true,
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusOK {
		t.Fatalf("follow should succeed, got %d", status)
	}

	status, followersResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/me/followers?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + ownerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("list my followers should succeed, got %d", status)
	}
	var followersData struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(followersResp.Data, &followersData); err != nil {
		t.Fatalf("parse followers response: %v", err)
	}
	if len(followersData.Items) == 0 || followersData.Items[0].ID == "" {
		t.Fatalf("expected at least one follower")
	}

	status, _ = doJSONRequest(t, srv, http.MethodPatch, "/api/v1/users/me", map[string]interface{}{
		"profile_public": false,
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusOK {
		t.Fatalf("disable profile public should succeed, got %d", status)
	}

	status, userResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/"+ownerID, nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("get /users/{id} should succeed, got %d", status)
	}
	var userData struct {
		ProfileAccessible bool `json:"profile_accessible"`
	}
	if err := json.Unmarshal(userResp.Data, &userData); err != nil {
		t.Fatalf("parse /users/{id} response: %v", err)
	}
	if userData.ProfileAccessible {
		t.Fatalf("expected profile_accessible=false when profile_public=0")
	}

	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/"+ownerID+"/videos?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusForbidden {
		t.Fatalf("viewer should be forbidden to access private profile videos, got %d", status)
	}

	status, ownerVideosResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/"+ownerID+"/videos?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + ownerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("owner should access own videos regardless privacy, got %d", status)
	}
	var ownerVideosData struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(ownerVideosResp.Data, &ownerVideosData); err != nil {
		t.Fatalf("parse owner videos response: %v", err)
	}
	if len(ownerVideosData.Items) == 0 || ownerVideosData.Items[0].ID != videoID {
		t.Fatalf("expected owner video %s in own profile videos list", videoID)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPatch, "/api/v1/users/me", map[string]interface{}{
		"profile_public": true,
		"public_videos":  true,
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusOK {
		t.Fatalf("enable public videos should succeed, got %d", status)
	}

	status, viewerVideosResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/"+ownerID+"/videos?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("viewer should access videos when profile/videos are public, got %d", status)
	}
	var viewerVideosData struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(viewerVideosResp.Data, &viewerVideosData); err != nil {
		t.Fatalf("parse viewer videos response: %v", err)
	}
	if len(viewerVideosData.Items) == 0 || viewerVideosData.Items[0].ID != videoID {
		t.Fatalf("expected public video %s in viewer profile videos list", videoID)
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
