package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"moevideo/backend/internal/util"
)

func setUserRole(t *testing.T, database *sql.DB, userID, role string) {
	t.Helper()
	if _, err := database.Exec(`UPDATE users SET role = ?, updated_at = datetime('now') WHERE id = ?`, role, userID); err != nil {
		t.Fatalf("set user role: %v", err)
	}
}

func TestAdminEndpointPermission(t *testing.T) {
	srv, container := newTestServer(t)
	userID, accessToken, _ := registerUser(t, srv, "adminish", "adminish@example.com", "password123")

	status, _ := doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/overview", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("admin endpoint without token should return 401, got %d", status)
	}

	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/overview", nil, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if status != http.StatusForbidden {
		t.Fatalf("non-admin token should return 403, got %d", status)
	}

	setUserRole(t, container.DB, userID, "admin")
	status, _ = doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/overview", nil, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if status != http.StatusOK {
		t.Fatalf("admin token should return 200, got %d", status)
	}
}

func TestAdminWriteActionCreatesAuditLog(t *testing.T) {
	srv, container := newTestServer(t)
	adminID, adminAccess, _ := registerUser(t, srv, "boss", "boss@example.com", "password123")
	targetID, _, _ := registerUser(t, srv, "target", "target@example.com", "password123")
	setUserRole(t, container.DB, adminID, "admin")

	status, _ := doJSONRequest(t, srv, http.MethodPatch, fmt.Sprintf("/api/v1/admin/users/%s", targetID), map[string]interface{}{
		"status": "disabled",
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusOK {
		t.Fatalf("admin patch user should return 200, got %d", status)
	}

	var userStatus string
	if err := container.DB.QueryRow(`SELECT status FROM users WHERE id = ?`, targetID).Scan(&userStatus); err != nil {
		t.Fatalf("query user status: %v", err)
	}
	if userStatus != "disabled" {
		t.Fatalf("expected target status disabled, got %s", userStatus)
	}

	status, logsResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/audit-logs?limit=10", nil, map[string]string{
		"Authorization": "Bearer " + adminAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("admin list audit logs should return 200, got %d", status)
	}

	var payload struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	if err := json.Unmarshal(logsResp.Data, &payload); err != nil {
		t.Fatalf("parse audit logs response: %v", err)
	}
	if len(payload.Items) == 0 {
		t.Fatalf("expected audit logs items")
	}

	found := false
	for _, item := range payload.Items {
		if item.Action == "user.patch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected action user.patch in audit logs")
	}
}

func TestAdminClearFinishedImportJobs(t *testing.T) {
	srv, container := newTestServer(t)
	adminID, adminAccess, _ := registerUser(t, srv, "import-clear-admin", "import-clear-admin@example.com", "password123")
	normalUserID, normalAccess, _ := registerUser(t, srv, "import-clear-user", "import-clear-user@example.com", "password123")
	otherUserID, _, _ := registerUser(t, srv, "import-clear-other", "import-clear-other@example.com", "password123")
	setUserRole(t, container.DB, adminID, "admin")

	now := time.Now().UTC()
	nowStr := util.FormatTime(now)
	expiresAt := util.FormatTime(now.Add(24 * time.Hour))

	jobSeq := 0
	insertJob := func(ownerID, status string) string {
		t.Helper()
		jobSeq++
		jobID := fmt.Sprintf("import-job-%d", jobSeq)
		if _, err := container.DB.Exec(`
INSERT INTO video_import_jobs (
	id, user_id, source_type, status, tags_json, visibility,
	total_files, selected_files, completed_files, failed_files, progress,
	available_at, expires_at, created_at, updated_at
) VALUES (?, ?, 'url', ?, '[]', 'public', 1, 1, 0, 0, 0, ?, ?, ?, ?)`,
			jobID, ownerID, status, nowStr, expiresAt, nowStr, nowStr,
		); err != nil {
			t.Fatalf("insert import job %s: %v", jobID, err)
		}
		return jobID
	}

	insertJob(normalUserID, "succeeded")
	insertJob(normalUserID, "failed")
	insertJob(otherUserID, "partial")
	insertJob(normalUserID, "queued")
	insertJob(otherUserID, "downloading")

	status, _ := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/admin/imports", nil, map[string]string{
		"Authorization": "Bearer " + normalAccess,
	})
	if status != http.StatusForbidden {
		t.Fatalf("non-admin clear imports should return 403, got %d", status)
	}

	status, clearResp := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/admin/imports", nil, map[string]string{
		"Authorization": "Bearer " + adminAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("admin clear imports should return 200, got %d (%s)", status, clearResp.Message)
	}
	var clearData struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(clearResp.Data, &clearData); err != nil {
		t.Fatalf("parse clear imports response: %v", err)
	}
	if clearData.Deleted != 3 {
		t.Fatalf("expected deleted=3, got %d", clearData.Deleted)
	}

	var finishedCount int64
	if err := container.DB.QueryRow(
		`SELECT COUNT(1) FROM video_import_jobs WHERE status IN ('succeeded', 'partial', 'failed')`,
	).Scan(&finishedCount); err != nil {
		t.Fatalf("count finished import jobs: %v", err)
	}
	if finishedCount != 0 {
		t.Fatalf("expected no finished import jobs left, got %d", finishedCount)
	}

	var activeCount int64
	if err := container.DB.QueryRow(
		`SELECT COUNT(1) FROM video_import_jobs WHERE status IN ('draft', 'queued', 'downloading')`,
	).Scan(&activeCount); err != nil {
		t.Fatalf("count active import jobs: %v", err)
	}
	if activeCount != 2 {
		t.Fatalf("expected active import jobs to remain 2, got %d", activeCount)
	}

	var auditCount int64
	if err := container.DB.QueryRow(
		`SELECT COUNT(1) FROM admin_audit_logs WHERE action = 'imports.clear_all_finished'`,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit log action: %v", err)
	}
	if auditCount == 0 {
		t.Fatalf("expected imports.clear_all_finished audit log entry")
	}
}

func TestAdminSiteSettingsAndCategoryFlow(t *testing.T) {
	srv, container := newTestServer(t)
	adminID, adminAccess, _ := registerUser(t, srv, "settings-admin", "settings-admin@example.com", "password123")
	setUserRole(t, container.DB, adminID, "admin")

	status, patchResp := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/admin/site-settings", map[string]interface{}{
		"site_title":       "MoeVideo Stage",
		"site_description": "configurable site settings",
		"footer_links": map[string]interface{}{
			"about": []map[string]string{
				{"label": "加入我们", "url": "/about/join"},
				{"label": "联系我们", "url": "/about/contact"},
				{"label": "创作团队", "url": "/about/team"},
			},
			"support": []map[string]string{
				{"label": "反馈中心", "url": "/support/feedback"},
				{"label": "隐私设置", "url": "/support/privacy"},
				{"label": "上传规范", "url": "/support/upload-guidelines"},
			},
			"legal": []map[string]string{
				{"label": "用户协议", "url": "/legal/terms"},
				{"label": "隐私政策", "url": "/legal/privacy"},
				{"label": "版权声明", "url": "/legal/copyright"},
			},
			"updates": []map[string]string{
				{"label": "官方公告", "url": "/updates/news"},
				{"label": "活动中心", "url": "/updates/events"},
				{"label": "开发日志", "url": "/updates/changelog"},
			},
		},
		"register_enabled": false,
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusOK {
		t.Fatalf("admin patch site settings should return 200, got %d (%s)", status, patchResp.Message)
	}

	status, publicResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/site-settings/public", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("public site settings should return 200, got %d", status)
	}
	var publicData struct {
		SiteTitle       string `json:"site_title"`
		SiteDescription string `json:"site_description"`
		RegisterEnabled bool   `json:"register_enabled"`
		FooterLinks     map[string][]struct {
			Label string `json:"label"`
			URL   string `json:"url"`
		} `json:"footer_links"`
	}
	if err := json.Unmarshal(publicResp.Data, &publicData); err != nil {
		t.Fatalf("parse public site settings: %v", err)
	}
	if publicData.SiteTitle != "MoeVideo Stage" || publicData.SiteDescription != "configurable site settings" {
		t.Fatalf("unexpected public site settings payload: %+v", publicData)
	}
	if publicData.RegisterEnabled {
		t.Fatalf("register_enabled should be false")
	}
	if len(publicData.FooterLinks["legal"]) != 3 {
		t.Fatalf("footer legal links should be 3, got %d", len(publicData.FooterLinks["legal"]))
	}
	if publicData.FooterLinks["legal"][0].URL != "/legal/terms" {
		t.Fatalf("unexpected footer legal first url: %q", publicData.FooterLinks["legal"][0].URL)
	}

	status, ytdlpResp := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/admin/site-settings", map[string]interface{}{
		"ytdlp_param_mode":        "advanced",
		"ytdlp_metadata_args_raw": "--extractor-args \"generic:foo=bar\"",
		"ytdlp_download_args_raw": "--format best",
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusOK {
		t.Fatalf("patch ytdlp settings should return 200, got %d (%s)", status, ytdlpResp.Message)
	}

	status, ytdlpGetResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/site-settings", nil, map[string]string{
		"Authorization": "Bearer " + adminAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("admin get site settings should return 200, got %d", status)
	}
	var adminSettingsData struct {
		YTDLPParamMode   string `json:"ytdlp_param_mode"`
		YTDLPMetadataRaw string `json:"ytdlp_metadata_args_raw"`
		YTDLPDownloadRaw string `json:"ytdlp_download_args_raw"`
	}
	if err := json.Unmarshal(ytdlpGetResp.Data, &adminSettingsData); err != nil {
		t.Fatalf("parse admin site settings: %v", err)
	}
	if adminSettingsData.YTDLPParamMode != "advanced" {
		t.Fatalf("expected ytdlp_param_mode=advanced, got %s", adminSettingsData.YTDLPParamMode)
	}
	if !strings.Contains(adminSettingsData.YTDLPMetadataRaw, "generic:foo=bar") {
		t.Fatalf("unexpected ytdlp metadata args: %s", adminSettingsData.YTDLPMetadataRaw)
	}
	if !strings.Contains(adminSettingsData.YTDLPDownloadRaw, "--format") {
		t.Fatalf("unexpected ytdlp download args: %s", adminSettingsData.YTDLPDownloadRaw)
	}

	status, invalidFooterResp := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/admin/site-settings", map[string]interface{}{
		"footer_links": map[string]interface{}{
			"about": []map[string]string{
				{"label": "加入我们", "url": "/about/join"},
				{"label": "联系我们", "url": "/about/contact"},
				{"label": "创作团队", "url": "javascript:alert(1)"},
			},
			"support": []map[string]string{
				{"label": "反馈中心", "url": "/support/feedback"},
				{"label": "隐私设置", "url": "/support/privacy"},
				{"label": "上传规范", "url": "/support/upload-guidelines"},
			},
			"legal": []map[string]string{
				{"label": "用户协议", "url": "/legal/terms"},
				{"label": "隐私政策", "url": "/legal/privacy"},
				{"label": "版权声明", "url": "/legal/copyright"},
			},
			"updates": []map[string]string{
				{"label": "官方公告", "url": "/updates/news"},
				{"label": "活动中心", "url": "/updates/events"},
				{"label": "开发日志", "url": "/updates/changelog"},
			},
		},
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusBadRequest {
		t.Fatalf("invalid footer url should return 400, got %d (%s)", status, invalidFooterResp.Message)
	}

	status, catResp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/admin/site-settings/categories", map[string]interface{}{
		"slug":       "admin-created",
		"name":       "管理分类",
		"sort_order": 99,
		"is_active":  true,
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusCreated {
		t.Fatalf("create category should return 201, got %d (%s)", status, catResp.Message)
	}

	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(catResp.Data, &created); err != nil {
		t.Fatalf("parse category create response: %v", err)
	}
	if created.ID <= 0 {
		t.Fatalf("created category id should be > 0")
	}

	status, _ = doJSONRequest(t, srv, http.MethodDelete, fmt.Sprintf("/api/v1/admin/site-settings/categories/%d", created.ID), nil, map[string]string{
		"Authorization": "Bearer " + adminAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("delete unused category should return 200, got %d", status)
	}

	videoID := prepareVideoForUser(t, container, adminID)
	var catID int64
	if err := container.DB.QueryRow(`SELECT id FROM categories WHERE slug = 'animation' LIMIT 1`).Scan(&catID); err != nil {
		t.Fatalf("query category id: %v", err)
	}
	if _, err := container.DB.Exec(`UPDATE videos SET category_id = ?, updated_at = datetime('now') WHERE id = ?`, catID, videoID); err != nil {
		t.Fatalf("assign video category: %v", err)
	}
	status, conflictResp := doJSONRequest(t, srv, http.MethodDelete, fmt.Sprintf("/api/v1/admin/site-settings/categories/%d", catID), nil, map[string]string{
		"Authorization": "Bearer " + adminAccess,
	})
	if status != http.StatusConflict {
		t.Fatalf("delete used category should return 409, got %d (%s)", status, conflictResp.Message)
	}
	if !strings.Contains(strings.ToLower(conflictResp.Message), "in use") {
		t.Fatalf("expected in-use message, got %q", conflictResp.Message)
	}
}

func TestAdminFeaturedBannersFlow(t *testing.T) {
	srv, container := newTestServer(t)
	adminID, adminAccess, _ := registerUser(t, srv, "banner-admin", "banner-admin@example.com", "password123")
	setUserRole(t, container.DB, adminID, "admin")

	videoIDs := make([]string, 0, 5)
	for idx := 0; idx < 5; idx++ {
		videoID := createPublishedVideoWithTags(t, container, adminID, nil, "public", fmt.Sprintf("banner-%d", idx+1), nil)
		videoIDs = append(videoIDs, videoID)
		if _, err := container.DB.Exec(
			`UPDATE videos SET title = ?, hot_score = ?, updated_at = datetime('now') WHERE id = ?`,
			fmt.Sprintf("banner-%d", idx+1),
			500-idx,
			videoID,
		); err != nil {
			t.Fatalf("update banner video %d: %v", idx+1, err)
		}
	}

	status, putResp := doJSONRequest(t, srv, http.MethodPut, "/api/v1/admin/banners/featured", map[string]interface{}{
		"video_ids": videoIDs,
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusOK {
		t.Fatalf("set featured banners should return 200, got %d (%s)", status, putResp.Message)
	}

	status, getResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/admin/banners/featured", nil, map[string]string{
		"Authorization": "Bearer " + adminAccess,
	})
	if status != http.StatusOK {
		t.Fatalf("get featured banners should return 200, got %d (%s)", status, getResp.Message)
	}
	var bannersData struct {
		VideoIDs []string `json:"video_ids"`
	}
	if err := json.Unmarshal(getResp.Data, &bannersData); err != nil {
		t.Fatalf("parse featured banners response: %v", err)
	}
	if len(bannersData.VideoIDs) != 5 {
		t.Fatalf("expected 5 featured video ids, got %d", len(bannersData.VideoIDs))
	}
	if bannersData.VideoIDs[0] != videoIDs[0] {
		t.Fatalf("unexpected first featured video id: got %s want %s", bannersData.VideoIDs[0], videoIDs[0])
	}

	status, homeResp := doJSONRequest(t, srv, http.MethodGet, "/api/v1/home", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("home should return 200, got %d (%s)", status, homeResp.Message)
	}
	var homeData struct {
		FeaturedItems []struct {
			ID string `json:"id"`
		} `json:"featured_items"`
	}
	if err := json.Unmarshal(homeResp.Data, &homeData); err != nil {
		t.Fatalf("parse home response: %v", err)
	}
	if len(homeData.FeaturedItems) == 0 {
		t.Fatalf("home featured_items should not be empty")
	}
	if homeData.FeaturedItems[0].ID != videoIDs[0] {
		t.Fatalf("home featured first id mismatch: got %s want %s", homeData.FeaturedItems[0].ID, videoIDs[0])
	}

	status, dupResp := doJSONRequest(t, srv, http.MethodPut, "/api/v1/admin/banners/featured", map[string]interface{}{
		"video_ids": []string{videoIDs[0], videoIDs[0], videoIDs[2], videoIDs[3], videoIDs[4]},
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusBadRequest {
		t.Fatalf("duplicate featured ids should return 400, got %d (%s)", status, dupResp.Message)
	}
}

func TestAdminVideoSoftDeleteHardDeletesVideo(t *testing.T) {
	srv, container := newTestServer(t)
	adminID, adminAccess, _ := registerUser(t, srv, "video-admin", "video-admin@example.com", "password123")
	ownerID, _, _ := registerUser(t, srv, "video-owner-delete", "video-owner-delete@example.com", "password123")
	setUserRole(t, container.DB, adminID, "admin")

	videoID := prepareVideoForUser(t, container, ownerID)

	status, resp := doJSONRequest(t, srv, http.MethodPost, "/api/v1/admin/videos/"+videoID+"/actions", map[string]interface{}{
		"action": "soft_delete",
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusOK {
		t.Fatalf("admin video soft_delete should return 200, got %d (%s)", status, resp.Message)
	}

	var videoCount int64
	if err := container.DB.QueryRow(`SELECT COUNT(1) FROM videos WHERE id = ?`, videoID).Scan(&videoCount); err != nil {
		t.Fatalf("count videos: %v", err)
	}
	if videoCount != 0 {
		t.Fatalf("expected video to be hard deleted, got count=%d", videoCount)
	}

	status, _ = doJSONRequest(t, srv, http.MethodPost, "/api/v1/admin/videos/"+videoID+"/actions", map[string]interface{}{
		"action": "restore",
	}, map[string]string{"Authorization": "Bearer " + adminAccess})
	if status != http.StatusBadRequest {
		t.Fatalf("restore action should be rejected, got %d", status)
	}
}
