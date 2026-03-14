package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func createActiveCategory(t *testing.T, container *app.App, slug, name string) int64 {
	t.Helper()

	res, err := container.DB.Exec(
		`INSERT INTO categories (slug, name, sort_order, is_active) VALUES (?, ?, 0, 1)`,
		slug,
		name,
	)
	if err != nil {
		t.Fatalf("insert category: %v", err)
	}
	categoryID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("read category id: %v", err)
	}
	return categoryID
}

func createPublishedVideoWithTags(
	t *testing.T,
	container *app.App,
	userID string,
	categoryID *int64,
	visibility string,
	title string,
	tags []string,
) string {
	t.Helper()

	if visibility == "" {
		visibility = "public"
	}
	now := util.FormatTime(time.Now().UTC())
	mediaID := "media-" + uuid.NewString()
	videoID := "video-" + uuid.NewString()
	objectKey := "videos/" + userID + "/seed/" + mediaID + ".mp4"

	if _, err := container.DB.Exec(
		`INSERT INTO media_objects (id, provider, bucket, object_key, original_filename, mime_type, size_bytes, duration_sec, created_by, created_at)
		 VALUES (?, 'local', '', ?, 'sample.mp4', 'video/mp4', 1024, 120, ?, ?)`,
		mediaID,
		objectKey,
		userID,
		now,
	); err != nil {
		t.Fatalf("insert media: %v", err)
	}

	var categoryArg interface{}
	if categoryID != nil {
		categoryArg = *categoryID
	}

	if _, err := container.DB.Exec(
		`INSERT INTO videos (id, uploader_id, title, description, category_id, source_media_id, status, visibility, duration_sec, published_at, created_at, updated_at)
		 VALUES (?, ?, ?, 'seed', ?, ?, 'published', ?, 120, ?, ?, ?)`,
		videoID,
		userID,
		title,
		categoryArg,
		mediaID,
		visibility,
		now,
		now,
		now,
	); err != nil {
		t.Fatalf("insert video: %v", err)
	}

	for _, rawTag := range tags {
		tag := strings.TrimSpace(rawTag)
		if tag == "" {
			continue
		}
		if _, err := container.DB.Exec(`INSERT INTO tags (name, use_count, created_at) VALUES (?, 0, ?) ON CONFLICT(name) DO NOTHING`, tag, now); err != nil {
			t.Fatalf("insert tag: %v", err)
		}
		var tagID int64
		if err := container.DB.QueryRow(`SELECT id FROM tags WHERE name = ? LIMIT 1`, tag).Scan(&tagID); err != nil {
			t.Fatalf("query tag id: %v", err)
		}
		res, err := container.DB.Exec(`INSERT OR IGNORE INTO video_tags (video_id, tag_id) VALUES (?, ?)`, videoID, tagID)
		if err != nil {
			t.Fatalf("attach tag: %v", err)
		}
		affected, _ := res.RowsAffected()
		if affected > 0 {
			if _, err := container.DB.Exec(`UPDATE tags SET use_count = use_count + 1 WHERE id = ?`, tagID); err != nil {
				t.Fatalf("increment tag use_count: %v", err)
			}
		}
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
		User              struct {
			ProfilePublic   bool `json:"profile_public"`
			PublicVideos    bool `json:"public_videos"`
			PublicFavorites bool `json:"public_favorites"`
			PublicFollowing bool `json:"public_following"`
			PublicFollowers bool `json:"public_followers"`
		} `json:"user"`
	}
	if err := json.Unmarshal(userResp.Data, &userData); err != nil {
		t.Fatalf("parse /users/{id} response: %v", err)
	}
	if userData.ProfileAccessible {
		t.Fatalf("expected profile_accessible=false when profile_public=0")
	}
	if userData.User.ProfilePublic {
		t.Fatalf("expected user.profile_public=false after disabling profile")
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
		"profile_public":   true,
		"public_videos":    true,
		"public_favorites": true,
		"public_following": true,
		"public_followers": true,
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusOK {
		t.Fatalf("enable public videos should succeed, got %d", status)
	}

	status, publicUserResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/users/"+ownerID, nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("get /users/{id} after enabling profile should succeed, got %d", status)
	}
	var publicUserData struct {
		ProfileAccessible bool `json:"profile_accessible"`
		User              struct {
			ProfilePublic   bool `json:"profile_public"`
			PublicVideos    bool `json:"public_videos"`
			PublicFavorites bool `json:"public_favorites"`
			PublicFollowing bool `json:"public_following"`
			PublicFollowers bool `json:"public_followers"`
		} `json:"user"`
	}
	if err := json.Unmarshal(publicUserResp.Data, &publicUserData); err != nil {
		t.Fatalf("parse public /users/{id} response: %v", err)
	}
	if !publicUserData.ProfileAccessible {
		t.Fatalf("expected profile_accessible=true after enabling profile")
	}
	if !publicUserData.User.ProfilePublic ||
		!publicUserData.User.PublicVideos ||
		!publicUserData.User.PublicFavorites ||
		!publicUserData.User.PublicFollowing ||
		!publicUserData.User.PublicFollowers {
		t.Fatalf("expected all public profile flags true, got %+v", publicUserData.User)
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
	categoryID := createActiveCategory(t, container, "eva-cat", "Eva Category")

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
		"category_id":     categoryID,
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
		Status string `json:"status"`
		Video  struct {
			CategoryID int64 `json:"category_id"`
		} `json:"video"`
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
	if detailData.Video.CategoryID != categoryID {
		t.Fatalf("expected video.category_id=%d, got %d", categoryID, detailData.Video.CategoryID)
	}
}

func TestCreateVideoRequiresCategoryID(t *testing.T) {
	srv, container := newTestServer(t)
	userID, access, _ := registerUser(t, srv, "video-no-category", "video-no-category@example.com", "password123")

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

	status, resp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos", map[string]interface{}{
		"title":           "processing video",
		"description":     "queued",
		"source_media_id": mediaID,
		"visibility":      "public",
	}, map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusBadRequest {
		t.Fatalf("missing category_id should return 400, got %d (%s)", status, resp.Message)
	}
}

func TestVideoRecommendationsRandomExcludeAndFallback(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, _, _ := registerUser(t, srv, "rec-owner", "rec-owner@example.com", "password123")

	catA := createActiveCategory(t, container, "rec-cat-a", "Rec Cat A")
	catB := createActiveCategory(t, container, "rec-cat-b", "Rec Cat B")

	currentID := createPublishedVideoWithTags(t, container, ownerID, &catA, "public", "current", nil)
	sameCategoryID := createPublishedVideoWithTags(t, container, ownerID, &catA, "public", "same-category", nil)
	otherOneID := createPublishedVideoWithTags(t, container, ownerID, &catB, "public", "other-one", nil)
	otherTwoID := createPublishedVideoWithTags(t, container, ownerID, &catB, "public", "other-two", nil)

	status, resp := doJSONRequest(
		t,
		srv,
		http.MethodGet,
		"/api/v1/videos/"+currentID+"/recommendations?random=1&limit=3&exclude_ids="+otherOneID,
		nil,
		nil,
	)
	if status != http.StatusOK {
		t.Fatalf("random recommendations should return 200, got %d (%s)", status, resp.Message)
	}

	var payload struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("parse random recommendations response: %v", err)
	}

	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 recommendation items, got %d", len(payload.Items))
	}

	ids := map[string]struct{}{}
	for _, item := range payload.Items {
		ids[item.ID] = struct{}{}
		if item.ID == currentID {
			t.Fatalf("recommendations must exclude current video")
		}
		if item.ID == otherOneID {
			t.Fatalf("recommendations must exclude exclude_ids video")
		}
	}
	if _, ok := ids[sameCategoryID]; !ok {
		t.Fatalf("recommendations should include same-category candidate")
	}
	if _, ok := ids[otherTwoID]; !ok {
		t.Fatalf("recommendations should include fallback candidate from other categories")
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

func TestDanmakuCreateAndList(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, ownerAccess, _ := registerUser(t, srv, "danmu-owner", "danmu-owner@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)

	status, _ := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/danmaku", map[string]interface{}{
		"content":  "hello",
		"time_sec": 3.5,
		"mode":     0,
		"color":    "#ffffff",
	}, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous create danmaku should return 401, got %d", status)
	}

	status, firstResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/danmaku", map[string]interface{}{
		"content":  "first",
		"time_sec": 8.2,
		"mode":     0,
		"color":    "#fff",
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusCreated {
		t.Fatalf("create first danmaku should return 201, got %d (%s)", status, firstResp.Message)
	}

	status, secondResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/danmaku", map[string]interface{}{
		"content":  "second",
		"time_sec": 2.1,
		"mode":     1,
		"color":    "#00AAFF",
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusCreated {
		t.Fatalf("create second danmaku should return 201, got %d (%s)", status, secondResp.Message)
	}

	var firstData struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal(firstResp.Data, &firstData); err != nil {
		t.Fatalf("parse first danmaku response: %v", err)
	}
	var secondData struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal(secondResp.Data, &secondData); err != nil {
		t.Fatalf("parse second danmaku response: %v", err)
	}

	status, loadResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID+"/danmaku", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list danmaku should return 200, got %d", status)
	}
	var loadData struct {
		Items []struct {
			ID      string  `json:"id"`
			TimeSec float64 `json:"time_sec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(loadResp.Data, &loadData); err != nil {
		t.Fatalf("parse danmaku list response: %v", err)
	}
	if len(loadData.Items) != 2 {
		t.Fatalf("expected 2 danmaku items, got %d", len(loadData.Items))
	}
	if loadData.Items[0].ID != secondData.Item.ID || loadData.Items[1].ID != firstData.Item.ID {
		t.Fatalf("expected danmaku ordered by time asc")
	}
	if loadData.Items[0].TimeSec >= loadData.Items[1].TimeSec {
		t.Fatalf("expected time_sec ascending order")
	}

	status, timelineResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID+"/danmaku/list?limit=1", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list danmaku timeline should return 200, got %d", status)
	}
	var timelineData struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(timelineResp.Data, &timelineData); err != nil {
		t.Fatalf("parse timeline response: %v", err)
	}
	if len(timelineData.Items) != 1 {
		t.Fatalf("expected 1 timeline item, got %d", len(timelineData.Items))
	}
	if timelineData.NextCursor == "" {
		t.Fatalf("expected timeline next_cursor when more data exists")
	}
}

func TestDanmakuPrivateVideoAccessControl(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, ownerAccess, _ := registerUser(t, srv, "private-owner", "private-owner@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)
	_, viewerAccess, _ := registerUser(t, srv, "private-viewer", "private-viewer@example.com", "password123")

	if _, err := container.DB.Exec(`UPDATE videos SET visibility = 'private', updated_at = ? WHERE id = ?`, util.FormatTime(time.Now().UTC()), videoID); err != nil {
		t.Fatalf("update video visibility: %v", err)
	}

	status, _ := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID+"/danmaku", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusNotFound {
		t.Fatalf("viewer should not access private danmaku list, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/danmaku", map[string]interface{}{
		"content":  "should fail",
		"time_sec": 1,
		"mode":     0,
		"color":    "#FFFFFF",
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusNotFound {
		t.Fatalf("viewer should not post private danmaku, got %d", status)
	}

	status, ownerResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/danmaku", map[string]interface{}{
		"content":  "owner allowed",
		"time_sec": 1,
		"mode":     0,
		"color":    "#FFFFFF",
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusCreated {
		t.Fatalf("owner should create private danmaku, got %d (%s)", status, ownerResp.Message)
	}
}

func TestURLImportStartCreatesQueuedJob(t *testing.T) {
	srv, container := newTestServer(t)
	_, accessToken, _ := registerUser(t, srv, "url-importer", "url-importer@example.com", "password123")
	categoryID := createActiveCategory(t, container, "url-import-cat", "URL Import Category")

	authHeader := map[string]string{"Authorization": "Bearer " + accessToken}

	status, _ := doJSONRequest(t, srv, http.MethodPost, "/api/v1/imports/url/start", map[string]interface{}{
		"url": "ftp://example.com/video",
	}, authHeader)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid url should return 400, got %d", status)
	}

	status, missingCategoryResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/imports/url/start", map[string]interface{}{
		"url": "https://example.com/video/no-category",
	}, authHeader)
	if status != http.StatusBadRequest {
		t.Fatalf("missing category_id should return 400, got %d (%s)", status, missingCategoryResp.Message)
	}

	status, startResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/imports/url/start", map[string]interface{}{
		"url":         "https://example.com/video/123",
		"category_id": categoryID,
		"title":       "URL 自定义标题",
		"description": "URL 自定义描述",
		"visibility":  "unlisted",
		"tags":        []string{"import", "url"},
	}, authHeader)
	if status != http.StatusOK {
		t.Fatalf("start url import should return 200, got %d (%s)", status, startResp.Message)
	}

	var startData struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(startResp.Data, &startData); err != nil {
		t.Fatalf("parse start url import response: %v", err)
	}
	if startData.JobID == "" {
		t.Fatalf("job_id should not be empty")
	}

	status, listResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/imports", nil, authHeader)
	if status != http.StatusOK {
		t.Fatalf("list imports should return 200, got %d (%s)", status, listResp.Message)
	}
	var listData struct {
		Items []struct {
			ID         string `json:"id"`
			SourceType string `json:"source_type"`
			SourceURL  string `json:"source_url"`
			Status     string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listResp.Data, &listData); err != nil {
		t.Fatalf("parse imports list response: %v", err)
	}
	if len(listData.Items) == 0 {
		t.Fatalf("imports list should not be empty")
	}
	if listData.Items[0].ID != startData.JobID {
		t.Fatalf("expected latest import job %s, got %s", startData.JobID, listData.Items[0].ID)
	}
	if listData.Items[0].SourceType != "url" {
		t.Fatalf("expected source_type=url, got %s", listData.Items[0].SourceType)
	}
	if listData.Items[0].SourceURL != "https://example.com/video/123" {
		t.Fatalf("unexpected source_url: %s", listData.Items[0].SourceURL)
	}
	if listData.Items[0].Status != "queued" {
		t.Fatalf("expected queued status, got %s", listData.Items[0].Status)
	}

	status, detailResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/imports/"+startData.JobID, nil, authHeader)
	if status != http.StatusOK {
		t.Fatalf("get import detail should return 200, got %d (%s)", status, detailResp.Message)
	}
	var detailData struct {
		Job struct {
			ID         string `json:"id"`
			SourceType string `json:"source_type"`
			SourceURL  string `json:"source_url"`
			Status     string `json:"status"`
		} `json:"job"`
		Items []struct {
			Selected bool `json:"selected"`
		} `json:"items"`
	}
	if err := json.Unmarshal(detailResp.Data, &detailData); err != nil {
		t.Fatalf("parse import detail response: %v", err)
	}
	if detailData.Job.ID != startData.JobID {
		t.Fatalf("detail job id mismatch: expected %s got %s", startData.JobID, detailData.Job.ID)
	}
	if detailData.Job.SourceType != "url" {
		t.Fatalf("expected detail source_type=url, got %s", detailData.Job.SourceType)
	}
	if detailData.Job.SourceURL != "https://example.com/video/123" {
		t.Fatalf("unexpected detail source_url: %s", detailData.Job.SourceURL)
	}
	if len(detailData.Items) != 1 || !detailData.Items[0].Selected {
		t.Fatalf("expected one selected import item")
	}

	var (
		customTitle       string
		customTitlePrefix sql.NullString
		customDescription string
	)
	if err := container.DB.QueryRow(
		`SELECT COALESCE(custom_title, ''), custom_title_prefix, COALESCE(custom_description, '') FROM video_import_jobs WHERE id = ?`,
		startData.JobID,
	).Scan(&customTitle, &customTitlePrefix, &customDescription); err != nil {
		t.Fatalf("query url import custom metadata: %v", err)
	}
	if customTitle != "URL 自定义标题" {
		t.Fatalf("unexpected custom_title: %s", customTitle)
	}
	if customTitlePrefix.Valid {
		t.Fatalf("url import custom_title_prefix should be NULL")
	}
	if customDescription != "URL 自定义描述" {
		t.Fatalf("unexpected custom_description: %s", customDescription)
	}
}

func TestListVideosFilterByTagAndListTags(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, _, _ := registerUser(t, srv, "tag-owner", "tag-owner@example.com", "password123")
	categoryID := createActiveCategory(t, container, "tag-cat", "Tag Cat")

	alphaVideoID := createPublishedVideoWithTags(t, container, ownerID, &categoryID, "public", "alpha video", []string{"alpha", "common"})
	betaVideoID := createPublishedVideoWithTags(t, container, ownerID, &categoryID, "public", "beta video", []string{"beta", "common"})
	privateAlphaID := createPublishedVideoWithTags(t, container, ownerID, &categoryID, "private", "private alpha", []string{"alpha"})

	status, alphaResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos?tag=alpha&limit=20", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list videos by tag should return 200, got %d (%s)", status, alphaResp.Message)
	}
	var alphaPayload struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(alphaResp.Data, &alphaPayload); err != nil {
		t.Fatalf("parse alpha videos payload: %v", err)
	}
	if len(alphaPayload.Items) != 1 || alphaPayload.Items[0].ID != alphaVideoID {
		t.Fatalf("expected only public alpha video, got %+v", alphaPayload.Items)
	}
	if alphaPayload.Items[0].ID == privateAlphaID {
		t.Fatalf("private alpha video must not appear in public tag list")
	}

	status, commonResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos?tag=common&limit=20", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list videos by common tag should return 200, got %d (%s)", status, commonResp.Message)
	}
	var commonPayload struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(commonResp.Data, &commonPayload); err != nil {
		t.Fatalf("parse common videos payload: %v", err)
	}
	if len(commonPayload.Items) != 2 {
		t.Fatalf("expected 2 videos for common tag, got %d", len(commonPayload.Items))
	}
	commonIDs := map[string]struct{}{}
	for _, item := range commonPayload.Items {
		commonIDs[item.ID] = struct{}{}
	}
	if _, ok := commonIDs[alphaVideoID]; !ok {
		t.Fatalf("common tag payload missing alpha video")
	}
	if _, ok := commonIDs[betaVideoID]; !ok {
		t.Fatalf("common tag payload missing beta video")
	}

	status, tagsResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/tags?limit=2", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list tags should return 200, got %d (%s)", status, tagsResp.Message)
	}
	var tagsPayload struct {
		Items []struct {
			Name        string `json:"name"`
			VideosCount int64  `json:"videos_count"`
			UseCount    int64  `json:"use_count"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(tagsResp.Data, &tagsPayload); err != nil {
		t.Fatalf("parse tags payload: %v", err)
	}
	if len(tagsPayload.Items) != 2 {
		t.Fatalf("expected 2 tags in first page, got %d", len(tagsPayload.Items))
	}
	if tagsPayload.NextCursor == "" {
		t.Fatalf("expected next_cursor for paged tags list")
	}

	status, tagsResp2 := doJSONRequest(t, srv, http.MethodGet, "/api/v1/tags?limit=2&cursor="+url.QueryEscape(tagsPayload.NextCursor), nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list tags second page should return 200, got %d (%s)", status, tagsResp2.Message)
	}
	var tagsPayload2 struct {
		Items []struct {
			Name        string `json:"name"`
			VideosCount int64  `json:"videos_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(tagsResp2.Data, &tagsPayload2); err != nil {
		t.Fatalf("parse second tags payload: %v", err)
	}
	if len(tagsPayload2.Items) == 0 {
		t.Fatalf("expected second page of tags to contain remaining items")
	}

	all := map[string]int64{}
	for _, item := range tagsPayload.Items {
		all[item.Name] = item.VideosCount
	}
	for _, item := range tagsPayload2.Items {
		all[item.Name] = item.VideosCount
	}
	if all["common"] != 2 {
		t.Fatalf("expected common videos_count=2, got %d", all["common"])
	}
	if all["alpha"] != 1 {
		t.Fatalf("expected alpha videos_count=1 (private excluded), got %d", all["alpha"])
	}
	if all["beta"] != 1 {
		t.Fatalf("expected beta videos_count=1, got %d", all["beta"])
	}
}

func TestStartTorrentImportPersistsCustomMetadata(t *testing.T) {
	srv, container := newTestServer(t)
	container.Config.ImportMaxFiles = 20
	userID, accessToken, _ := registerUser(t, srv, "torrent-meta", "torrent-meta@example.com", "password123")
	categoryID := createActiveCategory(t, container, "torrent-meta-cat", "Torrent Meta Category")
	now := util.FormatTime(time.Now().UTC())
	jobID := "import-job-" + uuid.NewString()
	itemID := "import-item-" + uuid.NewString()

	if _, err := container.DB.Exec(
		`INSERT INTO video_import_jobs (
			id, user_id, source_type, source_filename, info_hash, torrent_data, status,
			category_id, tags_json, visibility, attempts, max_attempts,
			total_files, selected_files, completed_files, failed_files, progress,
			available_at, started_at, finished_at, expires_at, error_message,
			created_at, updated_at
		) VALUES (?, ?, 'torrent', 'sample.torrent', 'deadbeef', X'00', 'draft',
			NULL, '[]', 'public', 0, 3,
			1, 0, 0, 0, 0,
			?, NULL, NULL, ?, NULL,
			?, ?)`,
		jobID,
		userID,
		now,
		util.FormatTime(time.Now().UTC().Add(24*time.Hour)),
		now,
		now,
	); err != nil {
		t.Fatalf("insert draft import job: %v", err)
	}
	if _, err := container.DB.Exec(
		`INSERT INTO video_import_items (
			id, job_id, file_index, file_path, file_size_bytes, selected, status,
			error_message, media_object_id, video_id, created_at, updated_at
		) VALUES (?, ?, 0, 'sample.mp4', 1024, 0, 'pending', NULL, NULL, NULL, ?, ?)`,
		itemID,
		jobID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert draft import item: %v", err)
	}

	status, missingCategoryResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/imports/torrent/start", map[string]interface{}{
		"job_id":                jobID,
		"selected_file_indexes": []int{0},
	}, map[string]string{"Authorization": "Bearer " + accessToken})
	if status != http.StatusBadRequest {
		t.Fatalf("missing category_id should return 400, got %d (%s)", status, missingCategoryResp.Message)
	}

	status, resp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/imports/torrent/start", map[string]interface{}{
		"job_id":                jobID,
		"selected_file_indexes": []int{0},
		"category_id":           categoryID,
		"title":                 "单文件标题",
		"title_prefix":          "批量前缀",
		"description":           "统一描述",
	}, map[string]string{"Authorization": "Bearer " + accessToken})
	if status != http.StatusOK {
		t.Fatalf("start torrent import should return 200, got %d (%s)", status, resp.Message)
	}

	var (
		customTitle       sql.NullString
		customTitlePrefix sql.NullString
		customDescription string
	)
	if err := container.DB.QueryRow(
		`SELECT custom_title, custom_title_prefix, COALESCE(custom_description, '') FROM video_import_jobs WHERE id = ?`,
		jobID,
	).Scan(&customTitle, &customTitlePrefix, &customDescription); err != nil {
		t.Fatalf("query torrent import metadata: %v", err)
	}
	if !customTitle.Valid || customTitle.String != "单文件标题" {
		t.Fatalf("unexpected custom_title: %+v", customTitle)
	}
	if !customTitlePrefix.Valid || customTitlePrefix.String != "批量前缀" {
		t.Fatalf("unexpected custom_title_prefix: %+v", customTitlePrefix)
	}
	if customDescription != "统一描述" {
		t.Fatalf("unexpected custom_description: %s", customDescription)
	}
}

func TestUpdateVideoByOwner(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, ownerAccess, _ := registerUser(t, srv, "video-owner", "video-owner@example.com", "password123")
	_, otherAccess, _ := registerUser(t, srv, "video-other", "video-other@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)

	status, resp := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/videos/"+videoID, map[string]interface{}{
		"title":       "新的标题",
		"description": "新的描述",
		"visibility":  "private",
		"tags":        []string{"anime", "clip", "anime"},
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusOK {
		t.Fatalf("owner update video should return 200, got %d (%s)", status, resp.Message)
	}

	var (
		title       string
		description string
		visibility  string
	)
	if err := container.DB.QueryRow(`SELECT title, description, visibility FROM videos WHERE id = ?`, videoID).Scan(&title, &description, &visibility); err != nil {
		t.Fatalf("query updated video: %v", err)
	}
	if title != "新的标题" {
		t.Fatalf("unexpected title: %s", title)
	}
	if description != "新的描述" {
		t.Fatalf("unexpected description: %s", description)
	}
	if visibility != "private" {
		t.Fatalf("unexpected visibility: %s", visibility)
	}

	rows, err := container.DB.Query(
		`SELECT t.name FROM video_tags vt JOIN tags t ON t.id = vt.tag_id WHERE vt.video_id = ? ORDER BY t.name ASC`,
		videoID,
	)
	if err != nil {
		t.Fatalf("query video tags: %v", err)
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			t.Fatalf("scan video tag: %v", err)
		}
		tags = append(tags, tag)
	}
	if len(tags) != 2 || tags[0] != "anime" || tags[1] != "clip" {
		t.Fatalf("unexpected tags after update: %+v", tags)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPatch, "/api/v1/videos/"+videoID, map[string]interface{}{
		"tags": []string{},
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusOK {
		t.Fatalf("owner clear tags should return 200, got %d", status)
	}
	var relCount int64
	if err := container.DB.QueryRow(`SELECT COUNT(1) FROM video_tags WHERE video_id = ?`, videoID).Scan(&relCount); err != nil {
		t.Fatalf("count video tags: %v", err)
	}
	if relCount != 0 {
		t.Fatalf("expected tags cleared, got %d", relCount)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPatch, "/api/v1/videos/"+videoID, map[string]interface{}{
		"title": "他人修改",
	}, map[string]string{"Authorization": "Bearer " + otherAccess})
	if status != http.StatusNotFound {
		t.Fatalf("non-owner update should return 404, got %d", status)
	}

	if _, err := container.DB.Exec(`UPDATE videos SET status = 'processing', updated_at = ? WHERE id = ?`, util.FormatTime(time.Now().UTC()), videoID); err != nil {
		t.Fatalf("set processing status: %v", err)
	}
	status, _ = doJSONRequest(t, srv, http.MethodPatch, "/api/v1/videos/"+videoID, map[string]interface{}{
		"description": "processing 可编辑",
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusOK {
		t.Fatalf("owner update processing video should return 200, got %d", status)
	}

	if _, err := container.DB.Exec(`UPDATE videos SET status = 'deleted', updated_at = ? WHERE id = ?`, util.FormatTime(time.Now().UTC()), videoID); err != nil {
		t.Fatalf("set deleted status: %v", err)
	}
	status, _ = doJSONRequest(t, srv, http.MethodPatch, "/api/v1/videos/"+videoID, map[string]interface{}{
		"title": "deleted 不可编辑",
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusNotFound {
		t.Fatalf("owner update deleted video should return 404, got %d", status)
	}
}

func TestUnlistedVideoDetailAndCommentsAccessibleByLink(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, _, _ := registerUser(t, srv, "unlisted-owner", "unlisted-owner@example.com", "password123")
	_, viewerAccess, _ := registerUser(t, srv, "unlisted-viewer", "unlisted-viewer@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)

	if _, err := container.DB.Exec(
		`UPDATE videos SET visibility = 'unlisted', updated_at = ? WHERE id = ?`,
		util.FormatTime(time.Now().UTC()),
		videoID,
	); err != nil {
		t.Fatalf("set unlisted visibility: %v", err)
	}

	status, _ := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("anon should access unlisted detail by link, got %d", status)
	}

	status, videosResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("list videos should return 200, got %d", status)
	}
	var videosData struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(videosResp.Data, &videosData); err != nil {
		t.Fatalf("parse videos list payload: %v", err)
	}
	for _, item := range videosData.Items {
		if item.ID == videoID {
			t.Fatalf("unlisted video should not appear in public videos list")
		}
	}

	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID+"/comments", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("viewer should access unlisted comments, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/comments", map[string]interface{}{
		"content": "viewer comment",
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusCreated {
		t.Fatalf("viewer should post comment on unlisted video, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/share", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("anon should share unlisted video, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPut, "/api/v1/videos/"+videoID+"/like", map[string]interface{}{
		"active": true,
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusOK {
		t.Fatalf("viewer should like unlisted video, got %d", status)
	}
}

func TestPrivateVideoCommentsAndDetailBlockedForViewer(t *testing.T) {
	srv, container := newTestServer(t)
	ownerID, ownerAccess, _ := registerUser(t, srv, "private-comments-owner", "private-comments-owner@example.com", "password123")
	_, viewerAccess, _ := registerUser(t, srv, "private-comments-viewer", "private-comments-viewer@example.com", "password123")
	videoID := prepareVideoForUser(t, container, ownerID)

	if _, err := container.DB.Exec(
		`UPDATE videos SET visibility = 'private', updated_at = ? WHERE id = ?`,
		util.FormatTime(time.Now().UTC()),
		videoID,
	); err != nil {
		t.Fatalf("set private visibility: %v", err)
	}

	status, _ := doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID, nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusNotFound {
		t.Fatalf("viewer should not access private detail, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID, nil, map[string]string{
		"Authorization": "Bearer " + ownerAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("owner should access private detail, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/videos/"+videoID+"/comments", nil, map[string]string{
		"Authorization": "Bearer " + viewerAccess,
	})
	if status != http.StatusNotFound {
		t.Fatalf("viewer should not access private comments, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/comments", map[string]interface{}{
		"content": "should fail",
	}, map[string]string{"Authorization": "Bearer " + viewerAccess})
	if status != http.StatusNotFound {
		t.Fatalf("viewer should not post private comments, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/videos/"+videoID+"/comments", map[string]interface{}{
		"content": "owner comment",
	}, map[string]string{"Authorization": "Bearer " + ownerAccess})
	if status != http.StatusCreated {
		t.Fatalf("owner should post private comments, got %d", status)
	}
}
