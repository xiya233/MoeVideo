package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
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

func TestAdminSiteSettingsAndCategoryFlow(t *testing.T) {
	srv, container := newTestServer(t)
	adminID, adminAccess, _ := registerUser(t, srv, "settings-admin", "settings-admin@example.com", "password123")
	setUserRole(t, container.DB, adminID, "admin")

	status, patchResp := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/admin/site-settings", map[string]interface{}{
		"site_title":       "MoeVideo Stage",
		"site_description": "configurable site settings",
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
